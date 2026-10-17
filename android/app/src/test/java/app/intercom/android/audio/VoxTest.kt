package app.intercom.android.audio

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VoxTest {
    private val loud = ShortArray(960) { 16384 }   // -6 dBFS
    private val quiet = ShortArray(960) { 16 }     // about -66 dBFS

    @Test
    fun levelOfHalfScaleIsMinus6dB() {
        assertEquals(-6.02f, levelDb(loud), 0.01f)
        assertEquals(-120f, levelDb(ShortArray(960)), 0f)
    }

    @Test
    fun opensOnVoiceAndHolds800ms() {
        val vox = Vox(thresholdDb = -45f, hangFrames = 40)
        assertFalse(vox.process(quiet))
        assertTrue(vox.process(loud))
        repeat(39) { assertTrue("frame $it after voice", vox.process(quiet)) }
        assertFalse(vox.process(quiet)) // 40th quiet frame = 800 ms later
    }

    @Test
    fun thresholdIsAdjustable() {
        val vox = Vox(thresholdDb = -3f)
        assertFalse(vox.process(loud))
        vox.thresholdDb = -10f
        assertTrue(vox.process(loud))
    }
}
