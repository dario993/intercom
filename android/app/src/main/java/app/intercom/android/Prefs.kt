package app.intercom.android

import android.content.Context
import androidx.core.content.edit

const val OUTPUT_AUTO = "auto"
const val OUTPUT_HEADSET = "headset"
const val OUTPUT_SPEAKER = "speaker"

class Prefs(context: Context) {
    private val p = context.getSharedPreferences("intercom", Context.MODE_PRIVATE)

    var host: String
        get() = p.getString("host", "") ?: ""
        set(v) = p.edit { putString("host", v) }
    var port: Int
        get() = p.getInt("port", 8080)
        set(v) = p.edit { putInt("port", v) }
    var username: String
        get() = p.getString("username", "") ?: ""
        set(v) = p.edit { putString("username", v) }

    /** Saved only when "ricorda credenziali" is on; stored encrypted. */
    var password: String?
        get() = p.getString("password", null)?.let(SecretBox::decrypt)
        set(v) = p.edit { if (v == null) remove("password") else putString("password", SecretBox.encrypt(v)) }

    var openMic: Boolean
        get() = p.getBoolean("openMic", false)
        set(v) = p.edit { putBoolean("openMic", v) }
    var voxThresholdDb: Float
        get() = p.getFloat("voxThresholdDb", -45f)
        set(v) = p.edit { putFloat("voxThresholdDb", v) }
    var output: String
        get() = p.getString("output", OUTPUT_AUTO) ?: OUTPUT_AUTO
        set(v) = p.edit { putString("output", v) }
    var keepScreenOn: Boolean
        get() = p.getBoolean("keepScreenOn", true)
        set(v) = p.edit { putBoolean("keepScreenOn", v) }

    fun volume(key: String): Float = p.getFloat("vol:$key", 1f)

    fun setVolume(key: String, v: Float) = p.edit { putFloat("vol:$key", v) }
}
