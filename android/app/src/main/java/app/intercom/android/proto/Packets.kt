package app.intercom.android.proto

import java.nio.ByteBuffer

const val KIND_VOICE = 1
const val KIND_KEEPALIVE = 2
const val CHAN_ALL = 0
const val CHAN_PRIVATE = 0xFFFF

private const val HEADER = 7

/** Client → server: u8 kind | u32 session | u16 seq | payload, big-endian. */
fun encodeClientPacket(kind: Int, session: Long, seq: Int, payload: ByteArray = ByteArray(0)): ByteArray =
    ByteBuffer.allocate(HEADER + payload.size)
        .put(kind.toByte())
        .putInt(session.toInt())
        .putShort(seq.toShort())
        .put(payload)
        .array()

/** Server → client: u8 kind | u16 talkerId | u16 channelId | u16 seq | payload. */
class ServerPacket(val kind: Int, val talkerId: Int, val channelId: Int, val seq: Int, val payload: ByteArray)

fun decodeServerPacket(data: ByteArray, length: Int = data.size): ServerPacket? {
    if (length < HEADER) return null
    val b = ByteBuffer.wrap(data, 0, length)
    val kind = b.get().toInt() and 0xFF
    val talker = b.short.toInt() and 0xFFFF
    val channel = b.short.toInt() and 0xFFFF
    val seq = b.short.toInt() and 0xFFFF
    return ServerPacket(kind, talker, channel, seq, data.copyOfRange(HEADER, length))
}
