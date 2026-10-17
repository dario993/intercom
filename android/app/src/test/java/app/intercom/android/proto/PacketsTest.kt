package app.intercom.android.proto

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PacketsTest {
    @Test
    fun clientPacketIsBigEndian() {
        val b = encodeClientPacket(KIND_VOICE, 0xDEADBEEFL, 513, byteArrayOf(9, 8, 7))
        val want = byteArrayOf(1, 0xDE.toByte(), 0xAD.toByte(), 0xBE.toByte(), 0xEF.toByte(), 2, 1, 9, 8, 7)
        assertArrayEquals(want, b)
    }

    @Test
    fun keepaliveHasNoPayload() {
        assertEquals(7, encodeClientPacket(KIND_KEEPALIVE, 1, 0).size)
    }

    @Test
    fun serverPacketDecodes() {
        val data = byteArrayOf(1, 0, 10, 0xFF.toByte(), 0xFF.toByte(), 0, 7, 1, 99)
        val p = decodeServerPacket(data, 8)!! // trailing byte beyond length is ignored
        assertEquals(KIND_VOICE, p.kind)
        assertEquals(10, p.talkerId)
        assertEquals(CHAN_PRIVATE, p.channelId)
        assertEquals(7, p.seq)
        assertArrayEquals(byteArrayOf(1), p.payload)
    }

    @Test
    fun shortPacketIsRejected() {
        assertNull(decodeServerPacket(byteArrayOf(1, 2, 3)))
    }
}
