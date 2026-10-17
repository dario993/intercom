package app.intercom.android.audio

/** Writes into [out] the sum of [voices] scaled by [gains], clipped to 16 bit. A voice shorter than out is silence past its end. */
fun mix(voices: List<ShortArray>, gains: List<Float>, out: ShortArray) {
    for (i in out.indices) {
        var acc = 0f
        for (v in voices.indices) {
            if (i < voices[v].size) acc += voices[v][i] * gains[v]
        }
        out[i] = acc.coerceIn(-32768f, 32767f).toInt().toShort()
    }
}
