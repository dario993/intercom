package app.intercom.android.talk

import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.Target

/** Who is talking where, from the server's "talking" messages. */
class TalkingTracker {
    private val active = LinkedHashMap<Int, ServerMessage.Talking>()

    fun apply(m: ServerMessage.Talking) {
        if (m.active) active[m.userId] = m else active.remove(m.userId)
    }

    fun clear() = active.clear()

    /** channelId → names talking on it, in arrival order. */
    fun activity(): Map<Int, List<String>> {
        val out = LinkedHashMap<Int, MutableList<String>>()
        for (t in active.values) {
            if (t.target == Target.Channels) t.channelIds.forEach { out.getOrPut(it) { mutableListOf() } += t.name }
        }
        return out
    }

    /** Text for the latest all-call or private call addressed to this user, if one is live. */
    fun banner(): String? = active.values.lastOrNull { it.target != Target.Channels }?.let {
        if (it.target == Target.All) "Chiamata a tutti da ${it.name}" else "Chiamata privata da ${it.name}"
    }
}
