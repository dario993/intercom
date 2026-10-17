package app.intercom.android.proto

import org.json.JSONArray
import org.json.JSONException
import org.json.JSONObject

data class ChannelInfo(val id: Int, val name: String, val color: String, val canTalk: Boolean, val canListen: Boolean)

data class Perms(val allCall: Boolean, val privateCall: Boolean)

data class UserStatus(val id: Int, val name: String, val online: Boolean, val talking: Boolean)

sealed interface Target {
    data object Channels : Target
    data object All : Target
    data class User(val userId: Int) : Target
}

sealed interface ServerMessage {
    data class Welcome(
        val userId: Int,
        val session: Long,
        val udpPort: Int,
        val serverName: String,
        val channels: List<ChannelInfo>,
        val perms: Perms,
    ) : ServerMessage

    data class Config(val channels: List<ChannelInfo>, val perms: Perms) : ServerMessage
    data class Users(val users: List<UserStatus>) : ServerMessage
    data class Talking(val userId: Int, val name: String, val active: Boolean, val target: Target, val channelIds: List<Int>) : ServerMessage
    data class Kicked(val reason: String) : ServerMessage
    data class Error(val code: String, val message: String) : ServerMessage
    data object Pong : ServerMessage
}

/** Parses one control message; malformed JSON and unknown types give null. */
fun parseServerMessage(text: String): ServerMessage? = try {
    val o = JSONObject(text)
    when (o.optString("type")) {
        "welcome" -> ServerMessage.Welcome(
            o.getInt("userId"), o.getLong("session"), o.getInt("udpPort"), o.optString("serverName"),
            channels(o.optJSONArray("channels")), perms(o.optJSONObject("perms")),
        )
        "config" -> ServerMessage.Config(channels(o.optJSONArray("channels")), perms(o.optJSONObject("perms")))
        "users" -> ServerMessage.Users(
            o.optJSONArray("users").objects().map {
                UserStatus(it.getInt("id"), it.getString("name"), it.optBoolean("online"), it.optBoolean("talking"))
            },
        )
        "talking" -> ServerMessage.Talking(
            o.getInt("userId"), o.optString("name"), o.optBoolean("active"),
            target(o.optJSONObject("target")), o.optJSONArray("channelIds").ints(),
        )
        "kicked" -> ServerMessage.Kicked(o.optString("reason"))
        "error" -> ServerMessage.Error(o.optString("code"), o.optString("message"))
        "pong" -> ServerMessage.Pong
        else -> null
    }
} catch (e: JSONException) {
    null
}

private fun JSONArray?.objects(): List<JSONObject> = if (this == null) emptyList() else List(length()) { getJSONObject(it) }

private fun JSONArray?.ints(): List<Int> = if (this == null) emptyList() else List(length()) { getInt(it) }

private fun channels(a: JSONArray?) = a.objects().map {
    ChannelInfo(it.getInt("id"), it.getString("name"), it.optString("color", "#3b82f6"), it.optBoolean("canTalk"), it.optBoolean("canListen"))
}

private fun perms(o: JSONObject?) = Perms(o?.optBoolean("allCall") ?: false, o?.optBoolean("privateCall") ?: false)

private fun target(o: JSONObject?): Target = when (o?.optString("kind")) {
    "all" -> Target.All
    "user" -> Target.User(o.optInt("userId"))
    else -> Target.Channels
}

private fun Target.toJson(): JSONObject = when (this) {
    Target.Channels -> JSONObject().put("kind", "channels")
    Target.All -> JSONObject().put("kind", "all")
    is Target.User -> JSONObject().put("kind", "user").put("userId", userId)
}

object ClientMessages {
    fun hello(username: String, password: String, device: String): String =
        JSONObject().put("type", "hello").put("username", username).put("password", password).put("device", device).toString()

    fun state(talk: Collection<Int>, listen: Collection<Int>): String =
        JSONObject().put("type", "state").put("talk", JSONArray(talk)).put("listen", JSONArray(listen)).toString()

    fun ptt(active: Boolean, target: Target): String =
        JSONObject().put("type", "ptt").put("active", active).put("target", target.toJson()).toString()

    fun ping(): String = """{"type":"ping"}"""
}
