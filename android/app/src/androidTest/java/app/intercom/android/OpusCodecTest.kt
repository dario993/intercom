package app.intercom.android

import androidx.test.ext.junit.runners.AndroidJUnit4
import app.intercom.android.audio.FRAME_SAMPLES
import app.intercom.android.audio.OpusDecoder
import app.intercom.android.audio.OpusEncoder
import app.intercom.android.audio.SAMPLE_RATE
import app.intercom.android.audio.levelDb
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import kotlin.math.PI
import kotlin.math.sin

@RunWith(AndroidJUnit4::class)
class OpusCodecTest {
    @Test
    fun sineSurvivesRoundTrip() {
        var t = 0
        val packets = OpusEncoder().use { enc ->
            (0 until 50).flatMap {
                val frame = ShortArray(FRAME_SAMPLES) { (sin(2 * PI * 440 * t++ / SAMPLE_RATE) * 12000).toInt().toShort() }
                enc.encode(frame)
            }
        }
        assertTrue("got ${packets.size} packets", packets.size >= 45)
        assertTrue("packets are compressed", packets.all { it.size in 1..400 })

        val decoded = OpusDecoder().use { dec -> packets.mapNotNull { dec.decode(it) } }
        assertTrue("decoded ${decoded.size} frames", decoded.size >= 40)
        assertTrue(decoded.all { it.size == FRAME_SAMPLES })
        val level = levelDb(decoded[decoded.size - 5])
        assertTrue("level $level dB", level > -25f) // 12000/32768 sine ≈ -11.7 dBFS
    }

    @Test
    fun garbagePacketDoesNotCrashTheProcess() {
        OpusDecoder().use { dec ->
            runCatching { dec.decode(ByteArray(80) { 0x55 }) } // may return null, audio, or throw; must not kill the app
        }
    }
}
