package app.intercom.core.tally

import app.intercom.core.proto.ServerMessage.Tally
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TallyBannerTest {
    @Test
    fun offOrMissingShowsNothingEvenOffline() {
        assertNull(tallyBanner(null, online = true))
        assertNull(tallyBanner(null, online = false))
        assertNull(tallyBanner(Tally("off", ""), online = false))
    }

    @Test
    fun statesMapToStyles() {
        assertEquals(TallyBanner(TallyStyle.ON_AIR, "● IN ONDA"), tallyBanner(Tally("program", "● IN ONDA"), true))
        assertEquals(TallyBanner(TallyStyle.PREVIEW, "PREVIEW"), tallyBanner(Tally("preview", "PREVIEW"), true))
        assertEquals(TallyBanner(TallyStyle.OTHER, "IN ONDA: CAM 3 Mario"), tallyBanner(Tally("other", "IN ONDA: CAM 3 Mario"), true))
        assertEquals(TallyBanner(TallyStyle.GREY, "NERO · nessuno in onda"), tallyBanner(Tally("black", "NERO · nessuno in onda"), true))
        assertEquals(TallyBanner(TallyStyle.GREY, "TALLY NON DISPONIBILE"), tallyBanner(Tally("unavailable", "TALLY NON DISPONIBILE"), true))
        assertEquals(TallyBanner(TallyStyle.GREY, "x"), tallyBanner(Tally("future", "x"), true))
    }

    @Test
    fun offlineBeatsEveryState() {
        for (s in listOf("program", "preview", "other", "black", "unavailable")) {
            assertEquals(TallyBanner(TallyStyle.DISCONNECTED, "DISCONNESSO"), tallyBanner(Tally(s, "x"), online = false))
        }
    }
}
