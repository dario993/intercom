package app.intercom.android.audio

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import android.os.Build
import android.os.Process
import android.os.SystemClock
import app.intercom.android.OUTPUT_AUTO
import app.intercom.android.OUTPUT_SPEAKER
import app.intercom.android.proto.KIND_VOICE
import app.intercom.android.proto.ServerPacket
import java.util.concurrent.ConcurrentHashMap
import kotlin.concurrent.thread

/**
 * Capture: mic → VOX → (if [onFrame] says on air) Opus → [sendVoice], every 20 ms.
 * Playback: per-talker jitter buffer → Opus → gain by channel → mix → speaker, every 20 ms.
 */
class AudioEngine(
    context: Context,
    private val capture: Boolean,
    private val onFrame: (voiceDetected: Boolean) -> Boolean,
    private val sendVoice: (ByteArray) -> Unit,
    private val gain: (channelId: Int) -> Float,
    /** The mic stopped on its own (recorder died or could not start); called once, on the capture thread. */
    private val onCaptureLost: () -> Unit = {},
) {
    val vox = Vox()
    private val am = context.getSystemService(AudioManager::class.java)
    private val voices = ConcurrentHashMap<Int, Voice>()
    private val threads = mutableListOf<Thread>()
    @Volatile private var running = false

    private class Voice {
        var decoder = OpusDecoder()
        val jitter = JitterBuffer()
        @Volatile var channelId = 0
        @Volatile var lastPacketAt = SystemClock.elapsedRealtime()
    }

    fun start() {
        running = true
        am.mode = AudioManager.MODE_IN_COMMUNICATION
        threads += thread(name = "audio-out") { playLoop() }
        if (capture) threads += thread(name = "audio-in") { captureLoop() }
    }

    fun stop() {
        running = false
        threads.forEach { it.join(500) }
        threads.clear()
        voices.values.forEach { it.decoder.close() }
        voices.clear()
        if (Build.VERSION.SDK_INT >= 31) am.clearCommunicationDevice() else @Suppress("DEPRECATION") { am.isSpeakerphoneOn = false }
        am.mode = AudioManager.MODE_NORMAL
    }

    fun onPacket(p: ServerPacket) {
        if (!running || p.kind != KIND_VOICE) return
        val v = voices.computeIfAbsent(p.talkerId) { Voice() }
        synchronized(v) {
            v.jitter.put(p.seq, p.payload)
            v.channelId = p.channelId
            v.lastPacketAt = SystemClock.elapsedRealtime()
        }
    }

    /** auto: headset/Bluetooth if connected, else loudspeaker. headset: headset or earpiece. speaker: loudspeaker. */
    fun applyOutput(output: String) {
        if (Build.VERSION.SDK_INT >= 31) {
            val devs = am.availableCommunicationDevices
            val headsets = setOf(
                AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET,
                AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET,
            )
            val speaker = devs.firstOrNull { it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER }
            val pick = when (output) {
                OUTPUT_SPEAKER -> speaker
                OUTPUT_AUTO -> devs.firstOrNull { it.type in headsets } ?: speaker
                else -> devs.firstOrNull { it.type in headsets }
            }
            if (pick != null) am.setCommunicationDevice(pick) else am.clearCommunicationDevice()
        } else {
            // ponytail: Android 10-11 only switch loudspeaker on/off; Bluetooth routing is left to the system there.
            @Suppress("DEPRECATION")
            am.isSpeakerphoneOn = output == OUTPUT_SPEAKER || (output == OUTPUT_AUTO && !am.isWiredHeadsetOn)
        }
    }

    private fun playLoop() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO)
        val min = AudioTrack.getMinBufferSize(SAMPLE_RATE, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val track = AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(min, FRAME_SAMPLES * 2 * 4))
            .setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
        track.play()
        val out = ShortArray(FRAME_SAMPLES)
        val pcm = ArrayList<ShortArray>()
        val gains = ArrayList<Float>()
        try {
            while (running) {
                pcm.clear()
                gains.clear()
                val now = SystemClock.elapsedRealtime()
                val it = voices.entries.iterator()
                while (it.hasNext()) {
                    val v = it.next().value
                    if (now - v.lastPacketAt > 500) { // talker silent for 500 ms: drop it from the mix
                        it.remove()
                        v.decoder.close()
                        continue
                    }
                    val packet = synchronized(v) { v.jitter.poll() } ?: continue
                    val frame = try {
                        v.decoder.decode(packet)
                    } catch (e: IllegalStateException) { // corrupt packet put the codec in error state (CodecException included)
                        v.decoder.close()
                        v.decoder = OpusDecoder()
                        null
                    }
                    if (frame != null) {
                        pcm += frame
                        gains += gain(v.channelId)
                    }
                }
                mix(pcm, gains, out)
                track.write(out, 0, out.size) // blocks once the buffer is full: paces the loop at 20 ms
            }
        } finally {
            track.stop()
            track.release()
        }
    }

    private fun captureLoop() {
        try {
            captureFrames()
        } catch (e: Exception) { // e.g. AudioRecord refused to start: must not crash the app
            android.util.Log.w("AudioEngine", "capture failed", e)
        } finally {
            if (running) onCaptureLost()
        }
    }

    @SuppressLint("MissingPermission") // only started when RECORD_AUDIO is granted
    private fun captureFrames() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO)
        val min = AudioRecord.getMinBufferSize(SAMPLE_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val rec = AudioRecord.Builder()
            .setAudioSource(MediaRecorder.AudioSource.VOICE_COMMUNICATION)
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_IN_MONO)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(min, FRAME_SAMPLES * 2 * 4))
            .build()
        if (AcousticEchoCanceler.isAvailable()) AcousticEchoCanceler.create(rec.audioSessionId)?.enabled = true
        if (NoiseSuppressor.isAvailable()) NoiseSuppressor.create(rec.audioSessionId)?.enabled = true
        val frame = ShortArray(FRAME_SAMPLES)
        val encoder = OpusEncoder()
        rec.startRecording()
        try {
            while (running) {
                var n = 0
                while (n < FRAME_SAMPLES) {
                    val r = rec.read(frame, n, FRAME_SAMPLES - n)
                    if (r < 0) return // recorder died; finally cleans up
                    n += r
                }
                if (onFrame(vox.process(frame))) encoder.encode(frame).forEach(sendVoice)
            }
        } finally {
            runCatching { rec.stop() }
            rec.release()
            encoder.close()
        }
    }
}
