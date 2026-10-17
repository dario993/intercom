package app.intercom.android.talk

import app.intercom.android.proto.Target

/** What this frame is sent to, or null for off-air. All-call and private hold win over the PARLA button. */
fun chooseTarget(
    holdAll: Boolean,
    holdUser: Int?,
    mode: TalkButton.Mode,
    openMic: Boolean,
    voice: Boolean,
    hasTalkChannels: Boolean,
): Target? = when {
    holdAll -> Target.All
    holdUser != null -> Target.User(holdUser)
    !hasTalkChannels -> null
    mode == TalkButton.Mode.HOLD -> Target.Channels
    mode == TalkButton.Mode.LATCHED && (openMic || voice) -> Target.Channels
    else -> null
}

/** Remembers the current transmission and turns target changes into ptt messages: (active, target), stop before start. */
class TxDecider {
    var target: Target? = null
        private set

    fun update(next: Target?): List<Pair<Boolean, Target>> {
        if (next == target) return emptyList()
        val out = buildList {
            target?.let { add(false to it) }
            next?.let { add(true to it) }
        }
        target = next
        return out
    }

    fun reset() {
        target = null
    }
}
