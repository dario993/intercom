package app.intercom.android

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.core.content.ContextCompat
import app.intercom.android.audio.AudioEngine
import app.intercom.android.net.ConnState
import app.intercom.android.net.Connection
import app.intercom.android.proto.CHAN_ALL
import app.intercom.android.proto.CHAN_PRIVATE
import app.intercom.android.proto.ChannelInfo
import app.intercom.android.proto.ClientMessages
import app.intercom.android.proto.Perms
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.Target
import app.intercom.android.proto.UserStatus
import app.intercom.android.talk.TalkButton
import app.intercom.android.talk.TalkingTracker
import app.intercom.android.talk.TxDecider
import app.intercom.android.talk.chooseTarget
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

enum class Screen { CONNECT, MAIN, SETTINGS }

/** Key of the master volume in [UiState.volumes]; channel volumes use the channel id. */
const val MASTER = -1

data class UiState(
    val screen: Screen = Screen.CONNECT,
    val conn: ConnState? = null,
    val error: String? = null,
    val host: String = "",
    val port: Int = 8080,
    val username: String = "",
    val serverName: String = "",
    val userId: Int = 0,
    val channels: List<ChannelInfo> = emptyList(),
    val perms: Perms = Perms(allCall = false, privateCall = false),
    val talk: Set<Int> = emptySet(),
    val listen: Set<Int> = emptySet(),
    val users: List<UserStatus> = emptyList(),
    val activity: Map<Int, List<String>> = emptyMap(),
    val incoming: String? = null,
    val buttonMode: TalkButton.Mode = TalkButton.Mode.IDLE,
    val onAir: Boolean = false,
    val holdingAll: Boolean = false,
    val holdingUser: Int? = null,
    val locked: Boolean = false,
    val micAllowed: Boolean = true,
    val volumes: Map<Int, Float> = emptyMap(),
    val openMic: Boolean = false,
    val voxThresholdDb: Float = -45f,
    val output: String = OUTPUT_AUTO,
    val keepScreenOn: Boolean = true,
)

/** App-wide intercom session: owns the connection and the audio engine, exposes [state] to the UI. */
class IntercomController(private val app: Context) {
    val prefs = Prefs(app)
    private val _state = MutableStateFlow(
        UiState(
            host = prefs.host, port = prefs.port, username = prefs.username, openMic = prefs.openMic,
            voxThresholdDb = prefs.voxThresholdDb, output = prefs.output, keepScreenOn = prefs.keepScreenOn,
        ),
    )
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val main = Handler(Looper.getMainLooper())
    private val lock = Any() // guards button and tx, shared by UI thread and capture thread
    private val button = TalkButton()
    private val tx = TxDecider()
    private val talking = TalkingTracker()
    @Volatile private var holdAll = false
    @Volatile private var holdUser: Int? = null
    @Volatile private var conn: Connection? = null
    private var connGen = 0
    private var engine: AudioEngine? = null

    private fun update(f: (UiState) -> UiState) = _state.update(f)

    fun autoConnect() {
        val pass = prefs.password ?: return
        if (conn == null && prefs.host.isNotBlank()) connect(prefs.host, prefs.port, prefs.username, pass, remember = true)
    }

    fun connect(host: String, port: Int, username: String, password: String, remember: Boolean) {
        disconnect()
        prefs.host = host
        prefs.port = port
        prefs.username = username
        prefs.password = if (remember) password else null
        update { it.copy(conn = ConnState.Connecting, error = null, host = host, port = port, username = username) }
        val gen = ++connGen
        conn = Connection(host, port, username, password, Build.MODEL, object : Connection.Listener {
            override fun onState(state: ConnState) {
                main.post { if (gen == connGen) onConnState(state) }
            }

            override fun onMessage(msg: ServerMessage) {
                main.post { if (gen == connGen) onServerMessage(msg) }
            }

            override fun onVoice(p: ServerPacket) {
                engine?.onPacket(p)
            }
        }).also { it.start() }
    }

    fun logout() {
        prefs.password = null // an explicit logout must not auto-login on next start
        disconnect()
        update { it.copy(screen = Screen.CONNECT, conn = null, error = null) }
    }

    private fun disconnect() {
        connGen++
        conn?.close()
        conn = null
        engine?.stop()
        engine = null
        app.stopService(Intent(app, IntercomService::class.java))
        synchronized(lock) {
            button.reset()
            tx.reset()
        }
        holdAll = false
        holdUser = null
        talking.clear()
        update {
            it.copy(
                activity = emptyMap(), incoming = null, buttonMode = TalkButton.Mode.IDLE, onAir = false,
                holdingAll = false, holdingUser = null, locked = false,
            )
        }
    }

    private fun onConnState(s: ConnState) {
        when (s) {
            is ConnState.Failed -> {
                if (s.clearPassword) prefs.password = null
                disconnect()
                update { it.copy(screen = Screen.CONNECT, conn = null, error = s.message) }
            }
            is ConnState.Reconnecting -> {
                synchronized(lock) { button.reset() } // a latched mic stays closed when the link comes back
                holdAll = false
                holdUser = null
                talking.clear()
                update {
                    it.copy(
                        conn = s, buttonMode = TalkButton.Mode.IDLE, holdingAll = false, holdingUser = null,
                        activity = emptyMap(), incoming = null,
                    )
                }
            }
            else -> update { it.copy(conn = s) }
        }
    }

    private fun onServerMessage(m: ServerMessage) {
        when (m) {
            is ServerMessage.Welcome -> {
                val first = _state.value.screen == Screen.CONNECT
                synchronized(lock) { tx.reset() } // the server has no transmission for this new session
                update {
                    it.copy(
                        screen = if (first) Screen.MAIN else it.screen,
                        serverName = m.serverName, userId = m.userId, channels = m.channels, perms = m.perms,
                        talk = if (first) emptySet() else keep(it.talk, m.channels) { c -> c.canTalk },
                        listen = if (first) m.channels.filter { c -> c.canListen }.map { c -> c.id }.toSet()
                        else keep(it.listen, m.channels) { c -> c.canListen },
                        volumes = loadVolumes(m.channels),
                    )
                }
                sendState()
                if (engine == null) startAudio()
            }
            is ServerMessage.Config -> {
                update {
                    it.copy(
                        channels = m.channels, perms = m.perms, volumes = loadVolumes(m.channels),
                        talk = keep(it.talk, m.channels) { c -> c.canTalk },
                        listen = keep(it.listen, m.channels) { c -> c.canListen },
                    )
                }
                if (_state.value.talk.isEmpty()) resetButton()
                sendState()
            }
            is ServerMessage.Users -> update { it.copy(users = m.users) }
            is ServerMessage.Talking -> {
                talking.apply(m)
                update { it.copy(activity = talking.activity(), incoming = talking.banner()) }
            }
            else -> Unit
        }
    }

    private fun keep(ids: Set<Int>, channels: List<ChannelInfo>, ok: (ChannelInfo) -> Boolean): Set<Int> =
        ids.filter { id -> channels.any { it.id == id && ok(it) } }.toSet()

    private fun sendState() {
        val s = _state.value
        conn?.send(ClientMessages.state(s.talk, s.listen))
    }

    private fun micGranted() =
        ContextCompat.checkSelfPermission(app, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    private fun startAudio() {
        val mic = micGranted()
        update { it.copy(micAllowed = mic) }
        ContextCompat.startForegroundService(app, Intent(app, IntercomService::class.java))
        engine = AudioEngine(app, capture = mic, onFrame = ::onFrame, sendVoice = { conn?.sendVoice(it) }, gain = ::gainFor, onCaptureLost = ::onCaptureLost).also {
            it.vox.thresholdDb = prefs.voxThresholdDb
            it.start()
            it.applyOutput(prefs.output)
        }
    }

    /** Called on resume: the user may have granted or revoked the microphone in system settings. */
    fun refreshMicPermission() {
        if (engine != null && micGranted() != _state.value.micAllowed) {
            engine?.stop()
            engine = null
            startAudio()
        }
    }

    /** Capture thread, every 20 ms: decides whether this frame goes on air and tells the server when that changes. */
    private fun onFrame(voice: Boolean): Boolean {
        val s = _state.value
        val target: Target?
        val changes: List<Pair<Boolean, Target>>
        synchronized(lock) {
            target = chooseTarget(holdAll, holdUser, button.mode, s.openMic, voice, s.talk.isNotEmpty())
            changes = tx.update(target)
        }
        for ((active, t) in changes) conn?.send(ClientMessages.ptt(active, t))
        val onAir = target != null
        if (onAir != s.onAir) update { it.copy(onAir = onAir) }
        return onAir
    }

    /** Capture thread died: tell the server we stopped and fall back to listen-only (onResume retries the mic). */
    private fun onCaptureLost() {
        val stops = synchronized(lock) { tx.update(null) }
        for ((active, t) in stops) conn?.send(ClientMessages.ptt(active, t))
        main.post { update { it.copy(micAllowed = false, onAir = false) } }
    }

    private fun gainFor(channelId: Int): Float {
        val v = _state.value.volumes
        val master = v[MASTER] ?: 1f
        return if (channelId == CHAN_ALL || channelId == CHAN_PRIVATE) master else master * (v[channelId] ?: 1f)
    }

    private fun volumeKey(key: Int) = "${prefs.host}:${prefs.username}:$key"

    private fun loadVolumes(channels: List<ChannelInfo>): Map<Int, Float> =
        (channels.map { it.id } + MASTER).associateWith { prefs.volume(volumeKey(it)) }

    private fun resetButton() {
        synchronized(lock) { button.reset() }
        update { it.copy(buttonMode = TalkButton.Mode.IDLE) }
    }

    private fun Set<Int>.toggle(id: Int) = if (id in this) this - id else this + id

    fun toggleTalk(id: Int) {
        if (_state.value.locked) return
        update { it.copy(talk = it.talk.toggle(id)) }
        if (_state.value.talk.isEmpty()) resetButton()
        sendState()
    }

    fun toggleListen(id: Int) {
        if (_state.value.locked) return
        update { it.copy(listen = it.listen.toggle(id)) }
        sendState()
    }

    fun talkDown() {
        val m = synchronized(lock) {
            button.down(SystemClock.uptimeMillis())
            button.mode
        }
        update { it.copy(buttonMode = m) }
    }

    fun talkUp() {
        val m = synchronized(lock) {
            button.up(SystemClock.uptimeMillis())
            button.mode
        }
        update { it.copy(buttonMode = m) }
    }

    fun allCall(pressed: Boolean) {
        holdAll = pressed
        update { it.copy(holdingAll = pressed) }
    }

    fun privateCall(userId: Int, pressed: Boolean) {
        holdUser = if (pressed) userId else null
        update { it.copy(holdingUser = if (pressed) userId else null) }
    }

    fun setLocked(locked: Boolean) = update { it.copy(locked = locked) }

    fun setVolume(key: Int, value: Float) {
        prefs.setVolume(volumeKey(key), value)
        update { it.copy(volumes = it.volumes + (key to value)) }
    }

    fun setOpenMic(open: Boolean) {
        prefs.openMic = open
        update { it.copy(openMic = open) }
    }

    fun setVoxThreshold(db: Float) {
        prefs.voxThresholdDb = db
        engine?.vox?.thresholdDb = db
        update { it.copy(voxThresholdDb = db) }
    }

    fun setOutput(output: String) {
        prefs.output = output
        engine?.applyOutput(output)
        update { it.copy(output = output) }
    }

    fun setKeepScreenOn(on: Boolean) {
        prefs.keepScreenOn = on
        update { it.copy(keepScreenOn = on) }
    }

    fun openSettings() = update { it.copy(screen = Screen.SETTINGS) }

    fun closeSettings() = update { it.copy(screen = Screen.MAIN) }
}
