package app.intercom.android.proto

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class MessagesTest {
    @Test
    fun parsesWelcomeWithUint32Session() {
        val m = parseServerMessage(
            """{"type":"welcome","userId":2,"session":4000000000,"udpPort":8081,"serverName":"Teatro",
               "channels":[{"id":1,"name":"Regia","color":"#ff0000","canTalk":true,"canListen":false}],
               "perms":{"allCall":true,"privateCall":false}}"""
        ) as ServerMessage.Welcome
        assertEquals(4000000000L, m.session)
        assertEquals(8081, m.udpPort)
        assertEquals(listOf(ChannelInfo(1, "Regia", "#ff0000", canTalk = true, canListen = false)), m.channels)
        assertEquals(Perms(allCall = true, privateCall = false), m.perms)
    }

    @Test
    fun parsesTalkingTargets() {
        val priv = parseServerMessage("""{"type":"talking","userId":3,"name":"Anna","active":true,"target":{"kind":"user","userId":2},"channelIds":[]}""")
        assertEquals(ServerMessage.Talking(3, "Anna", true, Target.User(2), emptyList()), priv)
        val ch = parseServerMessage("""{"type":"talking","userId":3,"name":"Anna","active":false,"target":{"kind":"channels"},"channelIds":[1,2]}""")
        assertEquals(ServerMessage.Talking(3, "Anna", false, Target.Channels, listOf(1, 2)), ch)
    }

    @Test
    fun parsesUsersKickedErrorPong() {
        assertEquals(
            ServerMessage.Users(listOf(UserStatus(2, "Anna", online = true, talking = false))),
            parseServerMessage("""{"type":"users","users":[{"id":2,"name":"Anna","online":true,"talking":false}]}"""),
        )
        assertEquals(ServerMessage.Kicked("replaced"), parseServerMessage("""{"type":"kicked","reason":"replaced"}"""))
        assertEquals(ServerMessage.Error("bad_credentials", "x"), parseServerMessage("""{"type":"error","code":"bad_credentials","message":"x"}"""))
        assertEquals(ServerMessage.Pong, parseServerMessage("""{"type":"pong"}"""))
    }

    @Test
    fun garbageAndUnknownTypesAreIgnored() {
        assertNull(parseServerMessage("not json"))
        assertNull(parseServerMessage("""{"type":"future"}"""))
    }

    @Test
    fun buildsClientMessages() {
        val state = JSONObject(ClientMessages.state(setOf(1), listOf(1, 2)))
        assertEquals("state", state.getString("type"))
        assertEquals(2, state.getJSONArray("listen").length())
        val ptt = JSONObject(ClientMessages.ptt(true, Target.User(5)))
        assertEquals(true, ptt.getBoolean("active"))
        assertEquals("user", ptt.getJSONObject("target").getString("kind"))
        assertEquals(5, ptt.getJSONObject("target").getInt("userId"))
        assertEquals("anna", JSONObject(ClientMessages.hello("anna", "pw", "Pixel")).getString("username"))
    }
}
