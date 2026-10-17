package app.intercom.android.net

import app.intercom.android.proto.ClientMessages
import app.intercom.android.proto.KIND_KEEPALIVE
import app.intercom.android.proto.KIND_VOICE
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.decodeServerPacket
import app.intercom.android.proto.encodeClientPacket
import app.intercom.android.proto.parseServerMessage
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.io.IOException
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread

sealed interface ConnState {
    data object Connecting : ConnState
    data object Online : ConnState
    data class Reconnecting(val attempt: Int) : ConnState
    /** Final: login refused, kicked, or never reached. No retry. */
    data class Failed(val message: String, val clearPassword: Boolean) : ConnState
}

/** Delay before reconnect attempt n (0-based): 1, 2, 4, then 5 s. */
fun backoffMs(attempt: Int): Long = minOf(5000L, 1000L shl minOf(attempt, 3))

fun kickMessage(reason: String): String = when (reason) {
    "replaced" -> "Collegato da un altro dispositivo"
    "admin" -> "Disconnesso dall'amministratore"
    "disabled" -> "Utente disabilitato"
    else -> "Disconnesso dal server"
}

/**
 * One login to the server: WebSocket for control, UDP for audio. Reconnects on its own after a drop;
 * stops for good on login errors, kicks, or if the very first connection fails.
 * Listener callbacks run on network threads.
 */
class Connection(
    private val host: String,
    private val port: Int,
    private val username: String,
    private val password: String,
    private val device: String,
    private val listener: Listener,
    /** No message from the server for this long while logged in = link lost (the server pongs every ping). */
    private val silenceMs: Long = 10_000,
) {
    interface Listener {
        fun onState(state: ConnState)
        fun onMessage(msg: ServerMessage)
        fun onVoice(p: ServerPacket)
    }

    private val http = OkHttpClient.Builder().connectTimeout(5, TimeUnit.SECONDS).readTimeout(0, TimeUnit.MILLISECONDS).build()
    private val timer = Executors.newSingleThreadScheduledExecutor()
    private val seq = AtomicInteger()

    @Volatile private var ws: WebSocket? = null
    @Volatile private var udp: DatagramSocket? = null
    @Volatile private var udpTarget: InetSocketAddress? = null
    @Volatile private var session = 0L
    @Volatile private var closed = false
    @Volatile private var everOnline = false
    @Volatile private var generation = 0
    @Volatile private var lastRxAt = 0L
    private var attempt = 0

    fun start() {
        listener.onState(ConnState.Connecting)
        timer.scheduleWithFixedDelay({ checkSilence(); send(ClientMessages.ping()) }, 3, 3, TimeUnit.SECONDS)
        timer.scheduleWithFixedDelay({ sendUdp(KIND_KEEPALIVE, ByteArray(0)) }, 5, 5, TimeUnit.SECONDS)
        connect()
    }

    /** Sends a control message; dropped while not logged in. */
    fun send(text: String) {
        if (session != 0L) ws?.send(text)
    }

    fun sendVoice(payload: ByteArray) = sendUdp(KIND_VOICE, payload)

    fun close() {
        closed = true
        session = 0
        ws?.close(1000, null)
        udp?.close()
        timer.shutdownNow()
    }

    internal fun handleForTest(text: String) = handle(text)

    @Synchronized
    private fun connect() {
        if (closed) return
        val gen = ++generation
        val url = "ws://${if (':' in host) "[$host]" else host}:$port/ws"
        ws = http.newWebSocket(Request.Builder().url(url).build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                webSocket.send(ClientMessages.hello(username, password, device))
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (gen == generation) handle(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(1000, null)
                lost(gen)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) = lost(gen)
        })
    }

    private fun handle(text: String) {
        lastRxAt = System.nanoTime()
        when (val msg = parseServerMessage(text) ?: return) {
            is ServerMessage.Welcome -> {
                session = msg.session
                attempt = 0
                everOnline = true
                openUdp(msg.udpPort)
                listener.onMessage(msg)
                listener.onState(ConnState.Online)
            }
            is ServerMessage.Error -> fail(msg.message, clearPassword = msg.code == "bad_credentials")
            is ServerMessage.Kicked -> fail(kickMessage(msg.reason), clearPassword = false)
            else -> listener.onMessage(msg)
        }
    }

    private fun openUdp(udpPort: Int) {
        udp?.close()
        val sock = DatagramSocket()
        udpTarget = InetSocketAddress(InetAddress.getByName(host), udpPort)
        udp = sock
        thread(name = "udp-rx", isDaemon = true) {
            val buf = ByteArray(1500)
            val pkt = DatagramPacket(buf, buf.size)
            while (true) {
                try {
                    sock.receive(pkt)
                } catch (e: IOException) {
                    return@thread
                }
                decodeServerPacket(buf, pkt.length)?.let(listener::onVoice)
            }
        }
        sendUdp(KIND_KEEPALIVE, ByteArray(0))
    }

    private fun sendUdp(kind: Int, payload: ByteArray) {
        val sock = udp ?: return
        val target = udpTarget ?: return
        val s = session
        if (s == 0L) return
        val n = if (kind == KIND_VOICE) seq.getAndIncrement() and 0xFFFF else 0
        val b = encodeClientPacket(kind, s, n, payload)
        try {
            sock.send(DatagramPacket(b, b.size, target))
        } catch (e: IOException) {
            // network down: the reconnect logic will notice on the WebSocket side
        }
    }

    /** A dead Wi-Fi link often gives no socket error for minutes: notice it from the missing pongs. */
    private fun checkSilence() {
        if (session == 0L || (System.nanoTime() - lastRxAt) / 1_000_000 <= silenceMs) return
        val gen = generation
        ws?.cancel()
        lost(gen)
    }

    @Synchronized
    private fun lost(gen: Int) {
        if (closed || gen != generation) return
        generation++ // ignore further callbacks from this socket
        session = 0
        udp?.close()
        udp = null
        if (!everOnline) return fail("Server non raggiungibile ($host:$port)", clearPassword = false)
        attempt++
        listener.onState(ConnState.Reconnecting(attempt))
        timer.schedule({ connect() }, backoffMs(attempt - 1), TimeUnit.MILLISECONDS)
    }

    @Synchronized
    private fun fail(message: String, clearPassword: Boolean) {
        if (closed) return
        close()
        listener.onState(ConnState.Failed(message, clearPassword))
    }
}
