package app.intercom.android.audio

import android.media.MediaCodec
import android.media.MediaFormat
import java.nio.ByteBuffer
import java.nio.ByteOrder

const val SAMPLE_RATE = 48000
const val FRAME_SAMPLES = 960 // 20 ms, mono

/** System Opus encoder (Android 10+), fed one 20 ms PCM frame at a time. */
class OpusEncoder(bitrate: Int = 32000) : AutoCloseable {
    private val codec = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_AUDIO_OPUS).apply {
        val f = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_OPUS, SAMPLE_RATE, 1)
        f.setInteger(MediaFormat.KEY_BIT_RATE, bitrate)
        configure(f, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
        start()
    }
    private val info = MediaCodec.BufferInfo()
    private var ptsUs = 0L

    /** Returns the packets that came out (usually one; none while the codec warms up). */
    fun encode(pcm: ShortArray): List<ByteArray> {
        val inIdx = codec.dequeueInputBuffer(10_000)
        if (inIdx >= 0) {
            val buf = codec.getInputBuffer(inIdx)!!
            buf.clear()
            buf.order(ByteOrder.nativeOrder()).asShortBuffer().put(pcm)
            codec.queueInputBuffer(inIdx, 0, pcm.size * 2, ptsUs, 0)
            ptsUs += 20_000
        }
        val out = ArrayList<ByteArray>(1)
        while (true) {
            val idx = codec.dequeueOutputBuffer(info, 0)
            if (idx == MediaCodec.INFO_TRY_AGAIN_LATER) break
            if (idx < 0) continue // format or buffers changed
            if (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG == 0 && info.size > 0) {
                val b = codec.getOutputBuffer(idx)!!
                b.position(info.offset)
                b.limit(info.offset + info.size)
                out += ByteArray(info.size).also { b.get(it) }
            }
            codec.releaseOutputBuffer(idx, false)
        }
        return out
    }

    override fun close() {
        runCatching { codec.stop() } // throws if a bad packet left the codec in its error state
        codec.release()
    }
}

/**
 * System Opus decoder for one talker. A missing frame is played as silence by the caller:
 * MediaCodec exposes no packet-loss concealment.
 */
class OpusDecoder : AutoCloseable {
    private val codec = MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_AUDIO_OPUS).apply {
        val f = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_OPUS, SAMPLE_RATE, 1)
        f.setByteBuffer("csd-0", ByteBuffer.wrap(opusHead()))
        f.setByteBuffer("csd-1", leLong(0)) // pre-skip (ns)
        f.setByteBuffer("csd-2", leLong(80_000_000)) // seek pre-roll (ns)
        configure(f, null, null, 0)
        start()
    }
    private val info = MediaCodec.BufferInfo()
    private val ready = ArrayDeque<ShortArray>()
    private var ptsUs = 0L

    fun decode(packet: ByteArray): ShortArray? {
        val inIdx = codec.dequeueInputBuffer(5_000)
        if (inIdx >= 0) {
            codec.getInputBuffer(inIdx)!!.apply { clear(); put(packet) }
            codec.queueInputBuffer(inIdx, 0, packet.size, ptsUs, 0)
            ptsUs += 20_000
        }
        while (true) {
            // ponytail: waits up to 5 ms for the first output; software Opus answers in well under that.
            val idx = codec.dequeueOutputBuffer(info, if (ready.isEmpty()) 5_000 else 0)
            if (idx == MediaCodec.INFO_TRY_AGAIN_LATER) break
            if (idx < 0) continue
            if (info.size > 0) {
                val sb = codec.getOutputBuffer(idx)!!.apply {
                    position(info.offset)
                    limit(info.offset + info.size)
                }.order(ByteOrder.nativeOrder()).asShortBuffer()
                val frame = ShortArray(FRAME_SAMPLES)
                sb.get(frame, 0, minOf(sb.remaining(), FRAME_SAMPLES))
                ready.addLast(frame)
            }
            codec.releaseOutputBuffer(idx, false)
        }
        return ready.removeFirstOrNull()
    }

    override fun close() {
        runCatching { codec.stop() } // throws if a bad packet left the codec in its error state
        codec.release()
    }

    private companion object {
        /** RFC 7845 identification header: mono, 48 kHz, no pre-skip, mapping family 0. */
        fun opusHead(): ByteArray = ByteBuffer.allocate(19).order(ByteOrder.LITTLE_ENDIAN).apply {
            put("OpusHead".toByteArray())
            put(1.toByte()) // version
            put(1.toByte()) // channels
            putShort(0) // pre-skip
            putInt(SAMPLE_RATE)
            putShort(0) // output gain
            put(0.toByte()) // mapping family
        }.array()

        fun leLong(v: Long): ByteBuffer = ByteBuffer.wrap(ByteBuffer.allocate(8).order(ByteOrder.LITTLE_ENDIAN).putLong(v).array())
    }
}
