package app.intercom.android.audio

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JitterBufferTest {
    private fun JitterBuffer.add(vararg seqs: Int) = seqs.forEach { put(it, byteArrayOf(it.toByte())) }
    private fun JitterBuffer.next(): Int? = poll()?.get(0)?.toInt()?.and(0xFF)

    @Test
    fun waitsForPrebufferThenPlaysInOrder() {
        val jb = JitterBuffer(prebuffer = 3)
        jb.add(0, 1)
        assertNull(jb.poll())
        jb.add(2)
        assertEquals(0, jb.next())
        assertEquals(1, jb.next())
        assertEquals(2, jb.next())
    }

    @Test
    fun reordersLateArrivals() {
        val jb = JitterBuffer(prebuffer = 3)
        jb.add(0, 2, 1)
        assertEquals(listOf(0, 1, 2), List(3) { jb.next() })
    }

    @Test
    fun lostFrameGivesNull() {
        val jb = JitterBuffer(prebuffer = 3)
        jb.add(0, 1, 2, 4)
        assertEquals(listOf(0, 1, 2, null, 4), List(5) { jb.next() })
    }

    @Test
    fun framesOlderThanPlayheadAreDropped() {
        val jb = JitterBuffer(prebuffer = 3)
        jb.add(5, 6, 7)
        assertEquals(5, jb.next())
        jb.add(4) // too late
        assertEquals(6, jb.next())
        assertEquals(7, jb.next())
        assertNull(jb.poll())
    }

    @Test
    fun wrapsAround() {
        val jb = JitterBuffer(prebuffer = 3)
        jb.put(65534, byteArrayOf(1))
        jb.put(0, byteArrayOf(3))
        jb.put(65535, byteArrayOf(2))
        assertEquals(listOf(1, 2, 3), List(3) { jb.poll()!![0].toInt() })
    }

    @Test
    fun overflowSkipsAheadToNewestFrames() {
        val jb = JitterBuffer(prebuffer = 3, maxQueued = 25)
        jb.add(0, 1, 2)
        assertEquals(0, jb.next())
        (3..40).forEach { jb.add(it) } // 40 queued frames, keep the newest 25
        assertEquals(16, jb.next())
    }

    @Test
    fun seqDiffHandlesWrap() {
        assertEquals(1, seqDiff(0, 65535))
        assertEquals(-1, seqDiff(65535, 0))
        assertEquals(5, seqDiff(10, 5))
    }
}
