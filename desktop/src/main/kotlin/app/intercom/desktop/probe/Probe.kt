package app.intercom.desktop.probe

import app.intercom.android.net.ConnState
import app.intercom.android.net.Connection
import app.intercom.android.proto.ClientMessages
import app.intercom.android.proto.KIND_VOICE
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.Target
import app.intercom.desktop.audio.FRAME_BYTES
import app.intercom.desktop.audio.FRAME_SAMPLES
import app.intercom.desktop.audio.OpusDecoder
import app.intercom.desktop.audio.OpusEncoder
import app.intercom.desktop.audio.SAMPLE_RATE
import app.intercom.desktop.audio.SoundDevices
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import javax.sound.sampled.SourceDataLine
import javax.sound.sampled.TargetDataLine
import kotlin.concurrent.thread
import kotlin.math.PI
import kotlin.math.sin

/*
 * Task 2 risk probe, removed once the real audio engine exists.
 *   --list                                    sound cards
 *   --loop [--seconds N]                      mic -> Opus -> Opus -> headphones, prints buffering and codec cost
 *   --net HOST PORT USER PASS [--tone]        joins the server: plays what phones say, Enter toggles talking
 * Common: --in NAME  --out NAME  --buf MS (Java Sound line buffer, default 40)
 */
fun main(args: Array<String>) {
    val opt = Options(args.toList())
    when {
        opt.flag("--list") -> list()
        opt.flag("--net") -> net(opt)
        opt.flag("--bench") -> bench()
        else -> loop(opt)
    }
}

private class Options(val a: List<String>) {
    fun flag(n: String) = n in a
    fun value(n: String): String? = a.indexOf(n).takeIf { it >= 0 }?.let { a.getOrNull(it + 1) }
    fun after(n: String, count: Int): List<String> = a.indexOf(n).let { a.subList(it + 1, it + 1 + count) }
    val bufMs get() = value("--buf")?.toInt() ?: 40
}

private fun list() {
    println("Ingressi:"); SoundDevices.inputs().forEach { println("  $it") }
    println("Uscite:"); SoundDevices.outputs().forEach { println("  $it") }
}

/** CPU cost of Opus alone, after JIT warm-up: must stay well under the 20 ms a frame lasts. */
private fun bench() {
    println("JVM: ${System.getProperty("java.vm.name")} ${System.getProperty("java.version")} ${System.getProperty("os.arch")}")
    val pcm = ShortArray(FRAME_SAMPLES) { (sin(2 * PI * 440 * it / SAMPLE_RATE) * 6000).toInt().toShort() }
    val enc = OpusEncoder()
    val dec = OpusDecoder()
    repeat(6) { round ->
        var encNs = 0L; var decNs = 0L
        repeat(250) {
            val t0 = System.nanoTime()
            val p = enc.encode(pcm)[0]
            val t1 = System.nanoTime()
            dec.decode(p)
            encNs += t1 - t0; decNs += System.nanoTime() - t1
        }
        println("giro ${round + 1}: codifica %.2f ms, decodifica %.2f ms per frame".format(encNs / 250 / 1e6, decNs / 250 / 1e6))
    }
}

private fun msToBytes(ms: Int) = SAMPLE_RATE / 1000 * ms * 2

private fun openLines(opt: Options): Pair<TargetDataLine, SourceDataLine> {
    val mic = SoundDevices.openInput(opt.value("--in"), msToBytes(opt.bufMs))
    val out = SoundDevices.openOutput(opt.value("--out"), msToBytes(opt.bufMs))
    println("Ingresso: ${mic.device}${if (mic.fellBack) " (scheda richiesta non trovata)" else ""}, buffer ${mic.line.bufferSize / 96} ms")
    println("Uscita:   ${out.device}${if (out.fellBack) " (scheda richiesta non trovata)" else ""}, buffer ${out.line.bufferSize / 96} ms")
    return mic.line to out.line
}

private fun toBytes(pcm: ShortArray, dst: ByteArray) =
    ByteBuffer.wrap(dst).order(ByteOrder.LITTLE_ENDIAN).asShortBuffer().put(pcm)

private fun toShorts(src: ByteArray) =
    ShortArray(FRAME_SAMPLES).also { ByteBuffer.wrap(src).order(ByteOrder.LITTLE_ENDIAN).asShortBuffer().get(it) }

/** Frames written but not yet played, in ms: the output side of the delay. */
private fun queuedOutMs(out: SourceDataLine, written: Long) = (written - out.longFramePosition) * 1000 / SAMPLE_RATE

private fun loop(opt: Options) {
    val seconds = opt.value("--seconds")?.toInt() ?: 15
    val (mic, out) = openLines(opt)
    val enc = OpusEncoder()
    val dec = OpusDecoder()
    val buf = ByteArray(FRAME_BYTES)
    mic.start(); out.start()
    var written = 0L
    var codecNs = 0L; var inQueued = 0L; var outQueued = 0L; var underruns = 0; var n = 0
    val end = System.nanoTime() + seconds * 1_000_000_000L
    println("Parla nel microfono: dovresti sentirti in cuffia. Durata $seconds s.")
    while (System.nanoTime() < end) {
        mic.read(buf, 0, buf.size)
        inQueued += mic.available() / 96
        val t0 = System.nanoTime()
        val pcm = dec.decode(enc.encode(toShorts(buf))[0]) ?: ShortArray(FRAME_SAMPLES)
        codecNs += System.nanoTime() - t0
        if (out.available() >= out.bufferSize - 96) underruns++ // output had (almost) run dry
        toBytes(pcm, buf)
        out.write(buf, 0, buf.size)
        written += FRAME_SAMPLES
        outQueued += queuedOutMs(out, written)
        if (++n % 50 == 0) {
            println(
                "1 s: codifica+decodifica %.2f ms/frame, in coda ingresso %d ms, uscita %d ms, svuotamenti uscita %d"
                    .format(codecNs / n / 1e6, inQueued / n, outQueued / n, underruns),
            )
        }
    }
    println("Stima ritardo software (senza scheda audio né driver): ~${20 + inQueued / n + outQueued / n} ms")
    mic.close(); out.close()
}

private fun net(opt: Options) {
    val (host, port, user, pass) = opt.after("--net", 4)
    val tone = opt.flag("--tone")
    val (mic, out) = openLines(opt)
    val received = AtomicInteger(); val decoded = AtomicInteger(); val failed = AtomicInteger()
    val talkers = ConcurrentHashMap<Int, Pair<OpusDecoder, ConcurrentLinkedQueue<ShortArray>>>()
    lateinit var conn: Connection
    val talk = AtomicBoolean()
    var talkChannels = emptyList<Int>()

    conn = Connection(host, port.toInt(), user, pass, "regia-probe", object : Connection.Listener {
        override fun onState(state: ConnState) = println("Stato: $state")

        override fun onMessage(msg: ServerMessage) {
            when (msg) {
                is ServerMessage.Welcome -> {
                    talkChannels = msg.channels.filter { it.canTalk }.map { it.id }
                    val listen = msg.channels.filter { it.canListen }.map { it.id }
                    conn.send(ClientMessages.state(talkChannels, listen))
                    println("Collegato a '${msg.serverName}': parla su $talkChannels, ascolta $listen. Invio = parla/smetti.")
                }
                is ServerMessage.Talking -> println("${msg.name} ${if (msg.active) "parla" else "ha smesso"}")
                else -> Unit
            }
        }

        override fun onVoice(p: ServerPacket) {
            if (p.kind != KIND_VOICE) return
            received.incrementAndGet()
            val (dec, q) = talkers.getOrPut(p.talkerId) { OpusDecoder() to ConcurrentLinkedQueue() }
            val pcm = dec.decode(p.payload)
            if (pcm == null) failed.incrementAndGet() else { decoded.incrementAndGet(); q.add(pcm) }
        }
    }).also { it.start() }

    out.start()
    thread(name = "play", isDaemon = true) {
        val bytes = ByteArray(FRAME_BYTES)
        val mix = IntArray(FRAME_SAMPLES)
        while (true) {
            mix.fill(0)
            for ((_, q) in talkers.values) {
                while (q.size > 4) q.poll() // keep the delay bounded: drop the oldest beyond 80 ms
                q.poll()?.forEachIndexed { i, s -> mix[i] += s }
            }
            toBytes(ShortArray(FRAME_SAMPLES) { mix[it].coerceIn(-32768, 32767).toShort() }, bytes)
            out.write(bytes, 0, bytes.size) // blocks: paces the loop at 20 ms
        }
    }

    mic.start()
    thread(name = "capture", isDaemon = true) {
        val enc = OpusEncoder()
        val bytes = ByteArray(FRAME_BYTES)
        var frame = 0L
        var sent = false
        while (true) {
            mic.read(bytes, 0, bytes.size)
            val pcm = if (tone) ShortArray(FRAME_SAMPLES) {
                (sin(2 * PI * 440 * (frame * FRAME_SAMPLES + it) / SAMPLE_RATE) * 6000).toInt().toShort()
            } else toShorts(bytes)
            frame++
            val on = talk.get()
            if (on != sent) {
                conn.send(ClientMessages.ptt(on, Target.Channels))
                sent = on
            }
            if (on) enc.encode(pcm).forEach { conn.sendVoice(it) }
        }
    }

    thread(name = "stats", isDaemon = true) {
        while (true) {
            Thread.sleep(5000)
            println("Pacchetti ricevuti ${received.get()}, decodificati ${decoded.get()}, rifiutati ${failed.get()}")
        }
    }

    while (true) {
        readlnOrNull() ?: break
        val on = !talk.get()
        talk.set(on)
        println(if (on) ">>> IN ONDA" else "<<< fuori onda")
    }
    conn.close()
}
