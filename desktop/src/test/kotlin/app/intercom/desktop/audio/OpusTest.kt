package app.intercom.desktop.audio

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.PI
import kotlin.math.sin
import kotlin.math.sqrt

class OpusTest {
    private fun tone(frame: Int) = ShortArray(FRAME_SAMPLES) {
        val t = (frame * FRAME_SAMPLES + it).toDouble() / SAMPLE_RATE
        (sin(2 * PI * 1000 * t) * 8000).toInt().toShort()
    }

    private fun rms(s: ShortArray) = sqrt(s.sumOf { it.toDouble() * it } / s.size)

    @Test
    fun toneSurvivesEncodeDecode() {
        val enc = OpusEncoder()
        val dec = OpusDecoder()
        var last = ShortArray(0)
        for (f in 0 until 25) {
            val packets = enc.encode(tone(f))
            assertEquals(1, packets.size)
            assertTrue("32 kbit/s is ~80 bytes per frame, got ${packets[0].size}", packets[0].size in 20..200)
            last = dec.decode(packets[0])!!
        }
        assertEquals(FRAME_SAMPLES, last.size)
        val ratio = rms(last) / rms(tone(24))
        assertTrue("energy ratio $ratio", ratio in 0.7..1.3)
    }

    @Test
    fun garbageGivesNull() {
        assertNull(OpusDecoder().decode(byteArrayOf(0x03, 0x00))) // code-3 packet declaring zero frames: invalid
    }

    @Test
    fun concealmentGivesAFrame() {
        val dec = OpusDecoder()
        dec.decode(OpusEncoder().encode(tone(0))[0])
        assertEquals(FRAME_SAMPLES, dec.conceal()!!.size)
    }
}
