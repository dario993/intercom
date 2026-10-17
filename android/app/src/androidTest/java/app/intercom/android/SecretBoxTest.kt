package app.intercom.android

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class SecretBoxTest {
    @Test
    fun roundTripAndTamper() {
        val enc = SecretBox.encrypt("segreta")
        assertFalse(enc.contains("segreta"))
        assertEquals("segreta", SecretBox.decrypt(enc))
        assertNull(SecretBox.decrypt("AAAA" + enc.drop(4)))
    }
}
