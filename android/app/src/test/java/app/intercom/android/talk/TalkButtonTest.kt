package app.intercom.android.talk

import app.intercom.android.talk.TalkButton.Mode.HOLD
import app.intercom.android.talk.TalkButton.Mode.IDLE
import app.intercom.android.talk.TalkButton.Mode.LATCHED
import org.junit.Assert.assertEquals
import org.junit.Test

class TalkButtonTest {
    private val b = TalkButton(tapMs = 250, doubleTapMs = 350)

    @Test
    fun holdTalksUntilRelease() {
        b.down(0)
        assertEquals(HOLD, b.mode)
        b.up(2000)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun singleTapDoesNotLatch() {
        b.down(0); b.up(100)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun doubleTapLatches() {
        b.down(0); b.up(100)
        b.down(300)
        assertEquals(HOLD, b.mode) // second press talks immediately
        b.up(380)
        assertEquals(LATCHED, b.mode)
    }

    @Test
    fun slowSecondTapDoesNotLatch() {
        b.down(0); b.up(100)
        b.down(600); b.up(700)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun tapWhileLatchedUnlatches() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.down(1000); b.up(1100)
        assertEquals(IDLE, b.mode)
        b.down(1300); b.up(1400) // a single tap right after unlatching must not relatch
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun longPressWhileLatchedKeepsLatch() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.down(1000); b.up(2500)
        assertEquals(LATCHED, b.mode)
    }

    @Test
    fun resetClearsLatch() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.reset()
        assertEquals(IDLE, b.mode)
    }
}
