package app.intercom.desktop.audio

import io.github.jaredmdobson.concentus.OpusApplication
import io.github.jaredmdobson.concentus.OpusDecoder as ConcentusDecoder
import io.github.jaredmdobson.concentus.OpusEncoder as ConcentusEncoder

const val SAMPLE_RATE = 48000
const val FRAME_SAMPLES = 960 // 20 ms, mono

/** Opus encoder in pure Java (Concentus), fed one 20 ms PCM frame at a time. Same shape as the Android one. */
class OpusEncoder(bitrate: Int = 32000) : AutoCloseable {
    private val enc = ConcentusEncoder(SAMPLE_RATE, 1, OpusApplication.OPUS_APPLICATION_VOIP).apply { this.bitrate = bitrate }
    private val buf = ByteArray(1275) // largest Opus packet

    /** Always one packet per frame: Concentus has no warm-up delay, unlike MediaCodec. */
    fun encode(pcm: ShortArray): List<ByteArray> {
        val n = enc.encode(pcm, 0, FRAME_SAMPLES, buf, 0, buf.size)
        return listOf(buf.copyOf(n))
    }

    override fun close() = Unit
}

/** Opus decoder for one talker. Bad packets give null instead of throwing. */
class OpusDecoder : AutoCloseable {
    private val dec = ConcentusDecoder(SAMPLE_RATE, 1)

    fun decode(packet: ByteArray): ShortArray? = try {
        val out = ShortArray(FRAME_SAMPLES)
        if (dec.decode(packet, 0, packet.size, out, 0, FRAME_SAMPLES, false) > 0) out else null
    } catch (e: Exception) { // Concentus throws OpusException, but also index errors on garbage input
        null
    }

    /** Packet loss concealment for one missing frame (MediaCodec on Android cannot do this). */
    fun conceal(): ShortArray? = try {
        val out = ShortArray(FRAME_SAMPLES)
        if (dec.decode(null, 0, 0, out, 0, FRAME_SAMPLES, false) > 0) out else null
    } catch (e: Exception) {
        null
    }

    override fun close() = Unit
}
