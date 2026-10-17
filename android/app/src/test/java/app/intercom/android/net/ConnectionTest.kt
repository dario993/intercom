package app.intercom.android.net

import app.intercom.android.proto.KIND_KEEPALIVE
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.encodeClientPacket
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit.MILLISECONDS
import java.util.concurrent.TimeUnit.SECONDS

class ConnectionTest {
    private val server = MockWebServer()
    private val udp = DatagramSocket(0, InetAddress.getLoopbackAddress()).apply { soTimeout = 3000 }
    private var conn: Connection? = null

    @After
    fun tearDown() {
        conn?.close()
        udp.close()
        server.shutdown()
    }

    private class Recorder : Connection.Listener {
        val states = LinkedBlockingQueue<ConnState>()
        val messages = LinkedBlockingQueue<ServerMessage>()
        override fun onState(state: ConnState) { states += state }
        override fun onMessage(msg: ServerMessage) { messages += msg }
        override fun onVoice(p: ServerPacket) {}
    }

    private val welcome get() =
        """{"type":"welcome","userId":2,"session":4000000000,"udpPort":${udp.localPort},"serverName":"Test","channels":[],"perms":{"allCall":false,"privateCall":false}}"""

    /** A server that answers hello with [reply], then optionally closes the socket. */
    private fun upgrade(reply: () -> String, closeAfter: Boolean = false, hellos: LinkedBlockingQueue<String>? = null) =
        MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                if (!text.contains("\"hello\"")) return
                hellos?.add(text)
                webSocket.send(reply())
                if (closeAfter) webSocket.close(1000, "bye")
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(1000, null) // answer the client's close so shutdown() doesn't hang
            }
        })

    private fun connect(rec: Recorder, port: Int = server.port) =
        Connection("127.0.0.1", port, "anna", "pw", "test", rec).also { conn = it; it.start() }

    @Test
    fun loginSendsHelloThenKeepalive() {
        val hellos = LinkedBlockingQueue<String>()
        server.enqueue(upgrade({ welcome }, hellos = hellos))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertTrue(hellos.poll(3, SECONDS)!!.contains("\"username\":\"anna\""))
        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        assertEquals(4000000000L, (rec.messages.poll(3, SECONDS) as ServerMessage.Welcome).session)
        val pkt = DatagramPacket(ByteArray(64), 64)
        udp.receive(pkt)
        assertArrayEquals(encodeClientPacket(KIND_KEEPALIVE, 4000000000L, 0), pkt.data.copyOf(pkt.length))
    }

    @Test
    fun loginErrorFailsWithoutRetry() {
        server.enqueue(upgrade({ """{"type":"error","code":"bad_credentials","message":"Nome utente o password errati"}""" }, closeAfter = true))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Failed("Nome utente o password errati", clearPassword = true), rec.states.poll(3, SECONDS))
        assertNull(rec.states.poll(1500, MILLISECONDS)) // no reconnect attempt
    }

    @Test
    fun firstConnectUnreachableFails() {
        val closedPort = ServerSocket(0).use { it.localPort }
        val rec = Recorder()
        connect(rec, closedPort)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        val failed = rec.states.poll(8, SECONDS) as ConnState.Failed
        assertTrue(failed.message, failed.message.contains("non raggiungibile"))
    }

    @Test
    fun dropAfterLoginReconnects() {
        server.enqueue(upgrade({ welcome }, closeAfter = true))
        server.enqueue(upgrade({ welcome }))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Reconnecting(1), rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(5, SECONDS))
    }

    @Test
    fun handshakeThatNeverAnswersFails() {
        server.enqueue(MockResponse().setSocketPolicy(okhttp3.mockwebserver.SocketPolicy.NO_RESPONSE)) // wrong port: TCP accepts, nothing answers
        server.start()
        val rec = Recorder()
        conn = Connection("127.0.0.1", server.port, "anna", "pw", "test", rec, handshakeMs = 1500).also { it.start() }

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        val failed = rec.states.poll(5, SECONDS) as ConnState.Failed
        assertTrue(failed.message, failed.message.contains("non risponde"))
    }

    @Test
    fun silentServerIsTreatedAsLost() {
        server.enqueue(upgrade({ welcome })) // never answers ping: like walking out of Wi-Fi range
        server.start()
        val rec = Recorder()
        conn = Connection("127.0.0.1", server.port, "anna", "pw", "test", rec, silenceMs = 1500).also { it.start() }

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Reconnecting(1), rec.states.poll(6, SECONDS))
    }

    @Test
    fun kickIsFinal() {
        server.enqueue(upgrade({ welcome }))
        server.start()
        val rec = Recorder()
        connect(rec)
        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        // the server side of the socket is not reachable from here, so feed the message directly
        conn!!.handleForTest("""{"type":"kicked","reason":"replaced"}""")
        assertEquals(ConnState.Failed("Collegato da un altro dispositivo", clearPassword = false), rec.states.poll(3, SECONDS))
    }

    @Test
    fun backoffSequence() {
        assertEquals(listOf(1000L, 2000L, 4000L, 5000L, 5000L), (0..4).map(::backoffMs))
    }
}
