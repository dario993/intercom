package app.intercom.android.audio

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class MixerTest {
    @Test
    fun sumsWithGains() {
        val out = ShortArray(3)
        mix(listOf(shortArrayOf(100, 200, 300), shortArrayOf(10, 20)), listOf(1f, 0.5f), out)
        assertArrayEquals(shortArrayOf(105, 210, 300), out) // shorter voice counts as silence
    }

    @Test
    fun clipsInsteadOfWrapping() {
        val out = ShortArray(2)
        mix(listOf(shortArrayOf(30000, -30000), shortArrayOf(30000, -30000)), listOf(1f, 1f), out)
        assertArrayEquals(shortArrayOf(32767, -32768), out)
    }

    @Test
    fun noVoicesIsSilence() {
        val out = shortArrayOf(5, 5)
        mix(emptyList(), emptyList(), out)
        assertArrayEquals(shortArrayOf(0, 0), out)
    }
}
