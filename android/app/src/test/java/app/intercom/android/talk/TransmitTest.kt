package app.intercom.android.talk

import app.intercom.android.proto.Target
import app.intercom.android.talk.TalkButton.Mode.HOLD
import app.intercom.android.talk.TalkButton.Mode.IDLE
import app.intercom.android.talk.TalkButton.Mode.LATCHED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TransmitTest {
    @Test
    fun chooseTargetPriorities() {
        assertEquals(Target.All, chooseTarget(true, 5, HOLD, false, false, true))
        assertEquals(Target.User(5), chooseTarget(false, 5, IDLE, false, false, false))
        assertEquals(Target.Channels, chooseTarget(false, null, HOLD, false, false, true))
        assertNull(chooseTarget(false, null, HOLD, false, false, false)) // no PARLA channel active
        assertNull(chooseTarget(false, null, IDLE, true, true, true))
    }

    @Test
    fun latchedFollowsVoxOrOpenMic() {
        assertNull(chooseTarget(false, null, LATCHED, openMic = false, voice = false, hasTalkChannels = true))
        assertEquals(Target.Channels, chooseTarget(false, null, LATCHED, openMic = false, voice = true, hasTalkChannels = true))
        assertEquals(Target.Channels, chooseTarget(false, null, LATCHED, openMic = true, voice = false, hasTalkChannels = true))
    }

    @Test
    fun txDeciderEmitsStopThenStart() {
        val tx = TxDecider()
        assertEquals(listOf(true to Target.Channels), tx.update(Target.Channels))
        assertEquals(emptyList<Pair<Boolean, Target>>(), tx.update(Target.Channels))
        assertEquals(listOf(false to Target.Channels, true to Target.All), tx.update(Target.All))
        assertEquals(listOf(false to Target.All), tx.update(null))
        tx.update(Target.Channels)
        tx.reset() // after a reconnect the server knows nothing: next start must be sent again
        assertEquals(listOf(true to Target.Channels), tx.update(Target.Channels))
    }
}
