package app.intercom.android.audio

import kotlin.math.log10
import kotlin.math.sqrt

/** RMS level of a frame in dBFS; silence is -120. */
fun levelDb(frame: ShortArray): Float {
    if (frame.isEmpty()) return -120f
    var sum = 0.0
    for (s in frame) {
        val x = s / 32768.0
        sum += x * x
    }
    val rms = sqrt(sum / frame.size)
    return if (rms <= 0.0) -120f else (20 * log10(rms)).toFloat()
}

/** Voice gate: open while frames reach [thresholdDb], and for [hangFrames] frames (20 ms each) after the last one that did. */
class Vox(@Volatile var thresholdDb: Float = -45f, private val hangFrames: Int = 40) {
    private var hang = 0

    fun process(frame: ShortArray): Boolean {
        if (levelDb(frame) >= thresholdDb) hang = hangFrames else if (hang > 0) hang--
        return hang > 0
    }

    fun reset() {
        hang = 0
    }
}
