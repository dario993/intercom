package app.intercom.android.talk

/**
 * Gestures of the PARLA button. Press = talk while held; two quick taps = latch (VOX or open mic,
 * chosen in settings); a tap while latched = unlatch. Times are milliseconds from any monotonic clock.
 */
class TalkButton(private val tapMs: Long = 250, private val doubleTapMs: Long = 350) {
    enum class Mode { IDLE, HOLD, LATCHED }

    var mode = Mode.IDLE
        private set
    private var downAt = 0L
    private var lastTapUpAt = NEVER
    private var latchedAtDown = false

    fun down(now: Long) {
        downAt = now
        latchedAtDown = mode == Mode.LATCHED
        if (!latchedAtDown) mode = Mode.HOLD
    }

    fun up(now: Long) {
        val tap = now - downAt <= tapMs
        when {
            latchedAtDown -> if (tap) mode = Mode.IDLE // a long press while latched keeps the latch
            tap && downAt - lastTapUpAt <= doubleTapMs -> {
                mode = Mode.LATCHED
                lastTapUpAt = NEVER
            }
            else -> {
                mode = Mode.IDLE
                lastTapUpAt = if (tap) now else NEVER
            }
        }
    }

    fun reset() {
        mode = Mode.IDLE
        lastTapUpAt = NEVER
    }

    private companion object {
        const val NEVER = Long.MIN_VALUE / 2
    }
}
