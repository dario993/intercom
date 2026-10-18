package app.intercom.core.tally

import app.intercom.core.proto.ServerMessage

enum class TallyStyle { ON_AIR, PREVIEW, OTHER, GREY, DISCONNECTED }

data class TallyBanner(val style: TallyStyle, val text: String)

/**
 * The banner on top of the main screen, or null for none. Once a user has a banner it stays while the
 * link is down, as DISCONNESSO: an operator must never read a stale "not on air".
 */
fun tallyBanner(t: ServerMessage.Tally?, online: Boolean): TallyBanner? = when {
    t == null || t.state == "off" -> null
    !online -> TallyBanner(TallyStyle.DISCONNECTED, "DISCONNESSO")
    else -> TallyBanner(
        when (t.state) {
            "program" -> TallyStyle.ON_AIR
            "preview" -> TallyStyle.PREVIEW
            "other" -> TallyStyle.OTHER
            else -> TallyStyle.GREY
        },
        t.text,
    )
}
