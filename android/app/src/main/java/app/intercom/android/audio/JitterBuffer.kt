package app.intercom.android.audio

/** Signed distance a - b between 16-bit sequence numbers, correct across the 65535 → 0 wrap. */
fun seqDiff(a: Int, b: Int): Int = ((a - b + 32768) and 0xFFFF) - 32768

/**
 * Reorder buffer for one talker. Starts playing once [prebuffer] frames are queued (60 ms), then
 * yields one frame per 20 ms tick: the payload, or null when that frame is missing (play silence).
 * Not thread-safe: the caller synchronizes put and poll.
 */
class JitterBuffer(private val prebuffer: Int = 3, private val maxQueued: Int = 25) {
    private val frames = HashMap<Int, ByteArray>()
    private var next = 0
    private var playing = false

    fun put(seq: Int, payload: ByteArray) {
        if (playing && seqDiff(seq, next) < 0) return // arrived after its slot was played
        if (!playing && (frames.isEmpty() || seqDiff(seq, next) < 0)) next = seq
        frames[seq] = payload
        while (frames.size > maxQueued) { // fell too far behind: drop the oldest and move the playhead past it
            val oldest = frames.keys.minBy { seqDiff(it, next) }
            frames.remove(oldest)
            next = (oldest + 1) and 0xFFFF
        }
    }

    fun poll(): ByteArray? {
        if (!playing) {
            if (frames.size < prebuffer) return null
            playing = true
        }
        if (frames.isEmpty()) { // talker paused (its seq stops while off air): re-anchor and prebuffer on the next packet
            playing = false
            return null
        }
        val f = frames.remove(next)
        next = (next + 1) and 0xFFFF
        return f
    }
}
