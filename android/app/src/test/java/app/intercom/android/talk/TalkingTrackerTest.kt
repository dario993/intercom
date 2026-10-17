package app.intercom.android.talk

import app.intercom.android.proto.ServerMessage.Talking
import app.intercom.android.proto.Target
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TalkingTrackerTest {
    @Test
    fun channelActivityAndStop() {
        val t = TalkingTracker()
        t.apply(Talking(2, "Anna", true, Target.Channels, listOf(1, 2)))
        t.apply(Talking(3, "Bruno", true, Target.Channels, listOf(1)))
        assertEquals(mapOf(1 to listOf("Anna", "Bruno"), 2 to listOf("Anna")), t.activity())
        assertNull(t.banner())
        t.apply(Talking(2, "Anna", false, Target.Channels, listOf(1, 2)))
        assertEquals(mapOf(1 to listOf("Bruno")), t.activity())
    }

    @Test
    fun allCallAndPrivateShowBanner() {
        val t = TalkingTracker()
        t.apply(Talking(2, "Anna", true, Target.All, emptyList()))
        assertEquals("Chiamata a tutti da Anna", t.banner())
        t.apply(Talking(3, "Bruno", true, Target.User(9), emptyList()))
        assertEquals("Chiamata privata da Bruno", t.banner())
        t.clear()
        assertNull(t.banner())
    }
}
