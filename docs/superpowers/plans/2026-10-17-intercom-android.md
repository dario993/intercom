# Intercom Android (Tappa 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** App Android client dell'intercom: login al server Go, canali parla/ascolta, pulsante PARLA (pressione / doppio tocco VOX o microfono aperto), chiamata a tutti, chiamate private, mixer, lucchetto, impostazioni, audio Opus in tempo reale anche a schermo spento.

**Architecture:** Un `IntercomController` a livello di applicazione tiene lo stato (`StateFlow<UiState>`) e collega `Connection` (WebSocket di controllo + UDP audio), `AudioEngine` (cattura → VOX → Opus → UDP; UDP → jitter buffer → Opus → mix → altoparlante) e `IntercomService` (servizio in primo piano con wake lock). La logica pura (pacchetti, messaggi, mixer, VOX, jitter buffer, gesti del pulsante, scelta della trasmissione) è in classi senza dipendenze Android, testate su JVM. L'interfaccia è Jetpack Compose e osserva solo `UiState`.

**Tech Stack:** Kotlin (built-in AGP 9), Android Gradle Plugin 9.3.1, Gradle 9.3.1, Jetpack Compose (BOM), Material 3, OkHttp 4.12 (WebSocket), `org.json` di sistema, `MediaCodec` Opus, `AudioRecord`/`AudioTrack`, `NsdManager`, Android Keystore. Test: JUnit 4 + `org.json` + MockWebServer (JVM), AndroidX Test (emulatore).

**Spec:** `docs/superpowers/specs/2026-10-17-intercom-tappa1-design.md` (sezioni 2, 3, 5, 6). Il server Go di riferimento è in `server/` (piano 1, già completato).

## Global Constraints

- minSdk 29, targetSdk/compileSdk 36. applicationId e namespace `app.intercom.android`.
- Gradle va eseguito con `export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"` e `ANDROID_HOME=$HOME/Library/Android/sdk`; tutti i comandi gradle partono da `android/`.
- Protocollo identico al server: UDP big-endian, client→server `u8 kind | u32 session | u16 seq | payload`, server→client `u8 kind | u16 talkerId | u16 channelId | u16 seq | payload`; `kind` 1 = voce, 2 = keepalive; channelId `0` = chiamata a tutti, `0xFFFF` = privata. `session` è un uint32 → in Kotlin `Long`.
- Messaggi WebSocket JSON con i nomi campo della spec (`type`, `userId`, `udpPort`, `canTalk`, `allCall`, `channelIds`, …).
- Audio: 48 kHz, mono, PCM 16 bit, frame da 20 ms (960 campioni), Opus ~32 kbit/s.
- Ping WebSocket ogni 3 s, keepalive UDP ogni 5 s; backoff di riconnessione 1, 2, 4, 5, 5… s.
- VOX: soglia in dBFS (predefinito −45, regolabile −60…−20), trasmissione mantenuta 800 ms dopo l'ultima voce.
- Testi dell'interfaccia in italiano. Grafica originale: nessun layout, colore o asset di Unity Intercom (niente anello a spicchi, niente cerchio giallo centrale).
- Nessuna dipendenza oltre a quelle elencate in Task 1.

## Review Focus

1. Rete persa mentre il VOX è agganciato → alla riconnessione il microfono resta **chiuso** e i canali parla/ascolta sono ripristinati (verifica in Task 7, passo e2e "modalità aereo").
2. Pacchetti voce non Opus o corrotti dal server → l'app non va in crash, la voce viene saltata (Task 7: il client di prova invia byte fittizi).
3. Server irraggiungibile o IP sbagliato al primo login → errore chiaro, nessuna rotella infinita (test `firstConnectUnreachableFails`, Task 4).
4. Permesso microfono negato → l'app funziona in solo ascolto e lo dice (Task 7, passo e2e con `pm revoke`).
5. Numeri di sequenza che passano da 65535 a 0 → ordine corretto nel jitter buffer (test `wrapsAround`, Task 2).

---

### Task 1: Progetto Gradle e protocollo (`proto`)

**Files:**
- Create: `android/settings.gradle.kts`, `android/build.gradle.kts`, `android/gradle.properties`, `android/app/build.gradle.kts`
- Create: `android/app/src/main/AndroidManifest.xml`
- Create: `android/app/src/main/java/app/intercom/android/proto/Packets.kt`
- Create: `android/app/src/main/java/app/intercom/android/proto/Messages.kt`
- Test: `android/app/src/test/java/app/intercom/android/proto/PacketsTest.kt`
- Test: `android/app/src/test/java/app/intercom/android/proto/MessagesTest.kt`
- Modify: `.gitignore`

**Interfaces:**
- Produces (package `app.intercom.android.proto`):
  - `const val KIND_VOICE = 1`, `KIND_KEEPALIVE = 2`, `CHAN_ALL = 0`, `CHAN_PRIVATE = 0xFFFF`
  - `fun encodeClientPacket(kind: Int, session: Long, seq: Int, payload: ByteArray = ByteArray(0)): ByteArray`
  - `class ServerPacket(val kind: Int, val talkerId: Int, val channelId: Int, val seq: Int, val payload: ByteArray)`
  - `fun decodeServerPacket(data: ByteArray, length: Int = data.size): ServerPacket?`
  - `data class ChannelInfo(id: Int, name: String, color: String, canTalk: Boolean, canListen: Boolean)`, `data class Perms(allCall, privateCall)`, `data class UserStatus(id, name, online, talking)`
  - `sealed interface Target { Channels; All; data class User(userId: Int) }`
  - `sealed interface ServerMessage { Welcome(userId, session: Long, udpPort, serverName, channels, perms); Config(channels, perms); Users(users); Talking(userId, name, active, target, channelIds); Kicked(reason); Error(code, message); Pong }`
  - `fun parseServerMessage(text: String): ServerMessage?`
  - `object ClientMessages { hello(username, password, device); state(talk, listen); ptt(active, target); ping() }` — tutti restituiscono `String` JSON

- [ ] **Step 1: Struttura Gradle**

```bash
cd /Users/dario/Documents/APP/Intercom
mkdir -p android/app/src/main/java/app/intercom/android/proto android/app/src/test/java/app/intercom/android/proto
printf 'android/.gradle/\nandroid/build/\nandroid/app/build/\nandroid/local.properties\nandroid/.idea/\nandroid/.kotlin/\n*.iml\n' >> .gitignore
printf 'sdk.dir=%s/Library/Android/sdk\n' "$HOME" > android/local.properties
```

`android/settings.gradle.kts`:

```kotlin
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "Intercom"
include(":app")
```

`android/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application") version "9.3.1" apply false
    id("org.jetbrains.kotlin.plugin.compose") version "2.2.20" apply false
}
```

`android/gradle.properties`:

```properties
org.gradle.jvmargs=-Xmx4g -Dfile.encoding=UTF-8
android.useAndroidX=true
kotlin.code.style=official
```

`android/app/build.gradle.kts`:

```kotlin
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
}

android {
    namespace = "app.intercom.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "app.intercom.android"
        minSdk = 29
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    buildFeatures {
        compose = true
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2025.10.00")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.activity:activity-compose:1.11.0")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.4")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20240303") // real org.json on the JVM; android.jar only has stubs
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")

    androidTestImplementation("androidx.test.ext:junit:1.3.0")
    androidTestImplementation("androidx.test:runner:1.7.0")
}
```

`android/app/src/main/AndroidManifest.xml` (provvisorio, completato in Task 6):

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">
    <application android:label="Intercom" />
</manifest>
```

- [ ] **Step 2: Gradle wrapper e prima build**

```bash
cd /Users/dario/Documents/APP/Intercom/android
export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
G=$(ls -d ~/.gradle/wrapper/dists/gradle-9.3.1-bin/*/gradle-9.3.1 | head -1)/bin/gradle
$G wrapper --gradle-version 9.3.1
./gradlew assembleDebug
```

Expected: `BUILD SUCCESSFUL`. Se una versione di dipendenza non si risolve, usa la più vicina disponibile e registra un Ruling.

- [ ] **Step 3: Scrivi i test che falliscono**

`android/app/src/test/java/app/intercom/android/proto/PacketsTest.kt`:

```kotlin
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
```

`android/app/src/test/java/app/intercom/android/proto/MessagesTest.kt`:

```kotlin
package app.intercom.android.proto

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class MessagesTest {
    @Test
    fun parsesWelcomeWithUint32Session() {
        val m = parseServerMessage(
            """{"type":"welcome","userId":2,"session":4000000000,"udpPort":8081,"serverName":"Teatro",
               "channels":[{"id":1,"name":"Regia","color":"#ff0000","canTalk":true,"canListen":false}],
               "perms":{"allCall":true,"privateCall":false}}"""
        ) as ServerMessage.Welcome
        assertEquals(4000000000L, m.session)
        assertEquals(8081, m.udpPort)
        assertEquals(listOf(ChannelInfo(1, "Regia", "#ff0000", canTalk = true, canListen = false)), m.channels)
        assertEquals(Perms(allCall = true, privateCall = false), m.perms)
    }

    @Test
    fun parsesTalkingTargets() {
        val priv = parseServerMessage("""{"type":"talking","userId":3,"name":"Anna","active":true,"target":{"kind":"user","userId":2},"channelIds":[]}""")
        assertEquals(ServerMessage.Talking(3, "Anna", true, Target.User(2), emptyList()), priv)
        val ch = parseServerMessage("""{"type":"talking","userId":3,"name":"Anna","active":false,"target":{"kind":"channels"},"channelIds":[1,2]}""")
        assertEquals(ServerMessage.Talking(3, "Anna", false, Target.Channels, listOf(1, 2)), ch)
    }

    @Test
    fun parsesUsersKickedErrorPong() {
        assertEquals(
            ServerMessage.Users(listOf(UserStatus(2, "Anna", online = true, talking = false))),
            parseServerMessage("""{"type":"users","users":[{"id":2,"name":"Anna","online":true,"talking":false}]}"""),
        )
        assertEquals(ServerMessage.Kicked("replaced"), parseServerMessage("""{"type":"kicked","reason":"replaced"}"""))
        assertEquals(ServerMessage.Error("bad_credentials", "x"), parseServerMessage("""{"type":"error","code":"bad_credentials","message":"x"}"""))
        assertEquals(ServerMessage.Pong, parseServerMessage("""{"type":"pong"}"""))
    }

    @Test
    fun garbageAndUnknownTypesAreIgnored() {
        assertNull(parseServerMessage("not json"))
        assertNull(parseServerMessage("""{"type":"future"}"""))
    }

    @Test
    fun buildsClientMessages() {
        val state = JSONObject(ClientMessages.state(setOf(1), listOf(1, 2)))
        assertEquals("state", state.getString("type"))
        assertEquals(2, state.getJSONArray("listen").length())
        val ptt = JSONObject(ClientMessages.ptt(true, Target.User(5)))
        assertEquals(true, ptt.getBoolean("active"))
        assertEquals("user", ptt.getJSONObject("target").getString("kind"))
        assertEquals(5, ptt.getJSONObject("target").getInt("userId"))
        assertEquals("anna", JSONObject(ClientMessages.hello("anna", "pw", "Pixel")).getString("username"))
    }
}
```

- [ ] **Step 4: Verifica che falliscano**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: FAIL, compilazione: `Unresolved reference: encodeClientPacket`

- [ ] **Step 5: Implementa**

`android/app/src/main/java/app/intercom/android/proto/Packets.kt`:

```kotlin
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
```

`android/app/src/main/java/app/intercom/android/proto/Messages.kt`:

```kotlin
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
```

- [ ] **Step 6: Verifica che passino**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: `BUILD SUCCESSFUL`, 9 test passati.

- [ ] **Step 7: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add .gitignore android
git commit -m "feat(android): gradle project and protocol package

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Logica audio pura (mixer, VOX, jitter buffer)

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/audio/Mixer.kt`
- Create: `android/app/src/main/java/app/intercom/android/audio/Vox.kt`
- Create: `android/app/src/main/java/app/intercom/android/audio/JitterBuffer.kt`
- Test: `android/app/src/test/java/app/intercom/android/audio/MixerTest.kt`, `VoxTest.kt`, `JitterBufferTest.kt`

**Interfaces:**
- Produces (package `app.intercom.android.audio`):
  - `fun mix(voices: List<ShortArray>, gains: List<Float>, out: ShortArray)`
  - `fun levelDb(frame: ShortArray): Float`; `class Vox(thresholdDb: Float = -45f, hangFrames: Int = 40)` con `@Volatile var thresholdDb`, `fun process(frame: ShortArray): Boolean`, `fun reset()`
  - `fun seqDiff(a: Int, b: Int): Int`; `class JitterBuffer(prebuffer: Int = 3, maxQueued: Int = 25)` con `fun put(seq: Int, payload: ByteArray)`, `fun poll(): ByteArray?`

- [ ] **Step 1: Scrivi i test che falliscono**

`android/app/src/test/java/app/intercom/android/audio/MixerTest.kt`:

```kotlin
package app.intercom.android.audio

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class MixerTest {
    @Test
    fun sumsWithGains() {
        val out = ShortArray(3)
        mix(listOf(shortArrayOf(100, 200, 300), shortArrayOf(10, 20)), listOf(1f, 0.5f), out)
        assertArrayEquals(shortArrayOf(105, 210, 300), out) // shorter voice counts as silence
    }

    @Test
    fun clipsInsteadOfWrapping() {
        val out = ShortArray(2)
        mix(listOf(shortArrayOf(30000, -30000), shortArrayOf(30000, -30000)), listOf(1f, 1f), out)
        assertArrayEquals(shortArrayOf(32767, -32768), out)
    }

    @Test
    fun noVoicesIsSilence() {
        val out = shortArrayOf(5, 5)
        mix(emptyList(), emptyList(), out)
        assertArrayEquals(shortArrayOf(0, 0), out)
    }
}
```

`android/app/src/test/java/app/intercom/android/audio/VoxTest.kt`:

```kotlin
package app.intercom.android.audio

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VoxTest {
    private val loud = ShortArray(960) { 16384 }   // -6 dBFS
    private val quiet = ShortArray(960) { 16 }     // about -66 dBFS

    @Test
    fun levelOfHalfScaleIsMinus6dB() {
        assertEquals(-6.02f, levelDb(loud), 0.01f)
        assertEquals(-120f, levelDb(ShortArray(960)), 0f)
    }

    @Test
    fun opensOnVoiceAndHolds800ms() {
        val vox = Vox(thresholdDb = -45f, hangFrames = 40)
        assertFalse(vox.process(quiet))
        assertTrue(vox.process(loud))
        repeat(39) { assertTrue("frame $it after voice", vox.process(quiet)) }
        assertFalse(vox.process(quiet)) // 40th quiet frame = 800 ms later
    }

    @Test
    fun thresholdIsAdjustable() {
        val vox = Vox(thresholdDb = -3f)
        assertFalse(vox.process(loud))
        vox.thresholdDb = -10f
        assertTrue(vox.process(loud))
    }
}
```

`android/app/src/test/java/app/intercom/android/audio/JitterBufferTest.kt`:

```kotlin
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
```

- [ ] **Step 2: Verifica che falliscano**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: FAIL, `Unresolved reference: mix`

- [ ] **Step 3: Implementa**

`android/app/src/main/java/app/intercom/android/audio/Mixer.kt`:

```kotlin
package app.intercom.android.audio

/** Writes into [out] the sum of [voices] scaled by [gains], clipped to 16 bit. A voice shorter than out is silence past its end. */
fun mix(voices: List<ShortArray>, gains: List<Float>, out: ShortArray) {
    for (i in out.indices) {
        var acc = 0f
        for (v in voices.indices) {
            if (i < voices[v].size) acc += voices[v][i] * gains[v]
        }
        out[i] = acc.coerceIn(-32768f, 32767f).toInt().toShort()
    }
}
```

`android/app/src/main/java/app/intercom/android/audio/Vox.kt`:

```kotlin
package app.intercom.android.audio

import kotlin.math.log10
import kotlin.math.sqrt

/** RMS level of a frame in dBFS; silence is -120. */
fun levelDb(frame: ShortArray): Float {
    if (frame.isEmpty()) return -120f
    var sum = 0.0
    for (s in frame) {
        val x = s / 32768.0
        sum += x * x
    }
    val rms = sqrt(sum / frame.size)
    return if (rms <= 0.0) -120f else (20 * log10(rms)).toFloat()
}

/** Voice gate: open while frames reach [thresholdDb], and for [hangFrames] frames (20 ms each) after the last one that did. */
class Vox(@Volatile var thresholdDb: Float = -45f, private val hangFrames: Int = 40) {
    private var hang = 0

    fun process(frame: ShortArray): Boolean {
        if (levelDb(frame) >= thresholdDb) hang = hangFrames else if (hang > 0) hang--
        return hang > 0
    }

    fun reset() {
        hang = 0
    }
}
```

`android/app/src/main/java/app/intercom/android/audio/JitterBuffer.kt`:

```kotlin
package app.intercom.android.audio

/** Signed distance a - b between 16-bit sequence numbers, correct across the 65535 → 0 wrap. */
fun seqDiff(a: Int, b: Int): Int = ((a - b + 32768) and 0xFFFF) - 32768

/**
 * Reorder buffer for one talker. Starts playing once [prebuffer] frames are queued (60 ms), then
 * yields one frame per 20 ms tick: the payload, or null when that frame is missing (play silence).
 * Not thread-safe: the caller synchronizes put and poll.
 */
class JitterBuffer(private val prebuffer: Int = 3, private val maxQueued: Int = 25) {
    private val frames = HashMap<Int, ByteArray>()
    private var next = 0
    private var playing = false

    fun put(seq: Int, payload: ByteArray) {
        if (playing && seqDiff(seq, next) < 0) return // arrived after its slot was played
        if (!playing && (frames.isEmpty() || seqDiff(seq, next) < 0)) next = seq
        frames[seq] = payload
        while (frames.size > maxQueued) { // fell too far behind: drop the oldest and move the playhead past it
            val oldest = frames.keys.minBy { seqDiff(it, next) }
            frames.remove(oldest)
            next = (oldest + 1) and 0xFFFF
        }
    }

    fun poll(): ByteArray? {
        if (!playing) {
            if (frames.size < prebuffer) return null
            playing = true
        }
        val f = frames.remove(next)
        next = (next + 1) and 0xFFFF
        return f
    }
}
```

- [ ] **Step 4: Verifica che passino**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: `BUILD SUCCESSFUL`, tutti i test passati.

- [ ] **Step 5: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): mixer, VOX gate and jitter buffer

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Logica di trasmissione (pulsante PARLA, destinatari, chi sta parlando)

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/talk/TalkButton.kt`
- Create: `android/app/src/main/java/app/intercom/android/talk/Transmit.kt`
- Create: `android/app/src/main/java/app/intercom/android/talk/TalkingTracker.kt`
- Test: `android/app/src/test/java/app/intercom/android/talk/TalkButtonTest.kt`, `TransmitTest.kt`, `TalkingTrackerTest.kt`

**Interfaces:**
- Consumes: `Target`, `ServerMessage.Talking` (Task 1).
- Produces (package `app.intercom.android.talk`):
  - `class TalkButton(tapMs: Long = 250, doubleTapMs: Long = 350)` con `enum class Mode { IDLE, HOLD, LATCHED }`, `val mode`, `fun down(now: Long)`, `fun up(now: Long)`, `fun reset()`
  - `fun chooseTarget(holdAll: Boolean, holdUser: Int?, mode: TalkButton.Mode, openMic: Boolean, voice: Boolean, hasTalkChannels: Boolean): Target?`
  - `class TxDecider` con `val target: Target?`, `fun update(next: Target?): List<Pair<Boolean, Target>>`, `fun reset()`
  - `class TalkingTracker` con `fun apply(m: ServerMessage.Talking)`, `fun clear()`, `fun activity(): Map<Int, List<String>>`, `fun banner(): String?`

- [ ] **Step 1: Scrivi i test che falliscono**

`android/app/src/test/java/app/intercom/android/talk/TalkButtonTest.kt`:

```kotlin
package app.intercom.android.talk

import app.intercom.android.talk.TalkButton.Mode.HOLD
import app.intercom.android.talk.TalkButton.Mode.IDLE
import app.intercom.android.talk.TalkButton.Mode.LATCHED
import org.junit.Assert.assertEquals
import org.junit.Test

class TalkButtonTest {
    private val b = TalkButton(tapMs = 250, doubleTapMs = 350)

    @Test
    fun holdTalksUntilRelease() {
        b.down(0)
        assertEquals(HOLD, b.mode)
        b.up(2000)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun singleTapDoesNotLatch() {
        b.down(0); b.up(100)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun doubleTapLatches() {
        b.down(0); b.up(100)
        b.down(300)
        assertEquals(HOLD, b.mode) // second press talks immediately
        b.up(380)
        assertEquals(LATCHED, b.mode)
    }

    @Test
    fun slowSecondTapDoesNotLatch() {
        b.down(0); b.up(100)
        b.down(600); b.up(700)
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun tapWhileLatchedUnlatches() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.down(1000); b.up(1100)
        assertEquals(IDLE, b.mode)
        b.down(1300); b.up(1400) // a single tap right after unlatching must not relatch
        assertEquals(IDLE, b.mode)
    }

    @Test
    fun longPressWhileLatchedKeepsLatch() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.down(1000); b.up(2500)
        assertEquals(LATCHED, b.mode)
    }

    @Test
    fun resetClearsLatch() {
        b.down(0); b.up(100); b.down(300); b.up(380)
        b.reset()
        assertEquals(IDLE, b.mode)
    }
}
```

`android/app/src/test/java/app/intercom/android/talk/TransmitTest.kt`:

```kotlin
package app.intercom.android.talk

import app.intercom.android.proto.Target
import app.intercom.android.talk.TalkButton.Mode.HOLD
import app.intercom.android.talk.TalkButton.Mode.IDLE
import app.intercom.android.talk.TalkButton.Mode.LATCHED
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TransmitTest {
    @Test
    fun chooseTargetPriorities() {
        assertEquals(Target.All, chooseTarget(true, 5, HOLD, false, false, true))
        assertEquals(Target.User(5), chooseTarget(false, 5, IDLE, false, false, false))
        assertEquals(Target.Channels, chooseTarget(false, null, HOLD, false, false, true))
        assertNull(chooseTarget(false, null, HOLD, false, false, false)) // no PARLA channel active
        assertNull(chooseTarget(false, null, IDLE, true, true, true))
    }

    @Test
    fun latchedFollowsVoxOrOpenMic() {
        assertNull(chooseTarget(false, null, LATCHED, openMic = false, voice = false, hasTalkChannels = true))
        assertEquals(Target.Channels, chooseTarget(false, null, LATCHED, openMic = false, voice = true, hasTalkChannels = true))
        assertEquals(Target.Channels, chooseTarget(false, null, LATCHED, openMic = true, voice = false, hasTalkChannels = true))
    }

    @Test
    fun txDeciderEmitsStopThenStart() {
        val tx = TxDecider()
        assertEquals(listOf(true to Target.Channels), tx.update(Target.Channels))
        assertEquals(emptyList<Pair<Boolean, Target>>(), tx.update(Target.Channels))
        assertEquals(listOf(false to Target.Channels, true to Target.All), tx.update(Target.All))
        assertEquals(listOf(false to Target.All), tx.update(null))
        tx.update(Target.Channels)
        tx.reset() // after a reconnect the server knows nothing: next start must be sent again
        assertEquals(listOf(true to Target.Channels), tx.update(Target.Channels))
    }
}
```

`android/app/src/test/java/app/intercom/android/talk/TalkingTrackerTest.kt`:

```kotlin
package app.intercom.android.talk

import app.intercom.android.proto.ServerMessage.Talking
import app.intercom.android.proto.Target
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class TalkingTrackerTest {
    @Test
    fun channelActivityAndStop() {
        val t = TalkingTracker()
        t.apply(Talking(2, "Anna", true, Target.Channels, listOf(1, 2)))
        t.apply(Talking(3, "Bruno", true, Target.Channels, listOf(1)))
        assertEquals(mapOf(1 to listOf("Anna", "Bruno"), 2 to listOf("Anna")), t.activity())
        assertNull(t.banner())
        t.apply(Talking(2, "Anna", false, Target.Channels, listOf(1, 2)))
        assertEquals(mapOf(1 to listOf("Bruno")), t.activity())
    }

    @Test
    fun allCallAndPrivateShowBanner() {
        val t = TalkingTracker()
        t.apply(Talking(2, "Anna", true, Target.All, emptyList()))
        assertEquals("Chiamata a tutti da Anna", t.banner())
        t.apply(Talking(3, "Bruno", true, Target.User(9), emptyList()))
        assertEquals("Chiamata privata da Bruno", t.banner())
        t.clear()
        assertNull(t.banner())
    }
}
```

- [ ] **Step 2: Verifica che falliscano**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: FAIL, `Unresolved reference: TalkButton`

- [ ] **Step 3: Implementa**

`android/app/src/main/java/app/intercom/android/talk/TalkButton.kt`:

```kotlin
package app.intercom.android.talk

/**
 * Gestures of the PARLA button. Press = talk while held; two quick taps = latch (VOX or open mic,
 * chosen in settings); a tap while latched = unlatch. Times are milliseconds from any monotonic clock.
 */
class TalkButton(private val tapMs: Long = 250, private val doubleTapMs: Long = 350) {
    enum class Mode { IDLE, HOLD, LATCHED }

    var mode = Mode.IDLE
        private set
    private var downAt = 0L
    private var lastTapUpAt = NEVER
    private var latchedAtDown = false

    fun down(now: Long) {
        downAt = now
        latchedAtDown = mode == Mode.LATCHED
        if (!latchedAtDown) mode = Mode.HOLD
    }

    fun up(now: Long) {
        val tap = now - downAt <= tapMs
        when {
            latchedAtDown -> if (tap) mode = Mode.IDLE // a long press while latched keeps the latch
            tap && downAt - lastTapUpAt <= doubleTapMs -> {
                mode = Mode.LATCHED
                lastTapUpAt = NEVER
            }
            else -> {
                mode = Mode.IDLE
                lastTapUpAt = if (tap) now else NEVER
            }
        }
    }

    fun reset() {
        mode = Mode.IDLE
        lastTapUpAt = NEVER
    }

    private companion object {
        const val NEVER = Long.MIN_VALUE / 2
    }
}
```

`android/app/src/main/java/app/intercom/android/talk/Transmit.kt`:

```kotlin
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
```

`android/app/src/main/java/app/intercom/android/talk/TalkingTracker.kt`:

```kotlin
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
```

- [ ] **Step 4: Verifica che passino**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 5: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): talk button gestures and transmission logic

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Connessione al server (WebSocket + UDP, riconnessione)

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/net/Connection.kt`
- Test: `android/app/src/test/java/app/intercom/android/net/ConnectionTest.kt`

**Interfaces:**
- Consumes: tutto `proto` (Task 1).
- Produces (package `app.intercom.android.net`):
  - `sealed interface ConnState { Connecting; Online; Reconnecting(attempt: Int); Failed(message: String, clearPassword: Boolean) }`
  - `fun backoffMs(attempt: Int): Long`, `fun kickMessage(reason: String): String`
  - `class Connection(host: String, port: Int, username: String, password: String, device: String, listener: Connection.Listener)` con `interface Listener { onState(ConnState); onMessage(ServerMessage); onVoice(ServerPacket) }`, `fun start()`, `fun send(text: String)`, `fun sendVoice(payload: ByteArray)`, `fun close()`
  - Le callback arrivano su thread di rete (OkHttp / UDP), mai sul main thread. `Welcome` arriva sia come `onMessage` sia come `onState(Online)`. `Error`/`Kicked` diventano solo `onState(Failed)`.

- [ ] **Step 1: Scrivi i test che falliscono**

`android/app/src/test/java/app/intercom/android/net/ConnectionTest.kt`:

```kotlin
package app.intercom.android.net

import app.intercom.android.proto.KIND_KEEPALIVE
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.encodeClientPacket
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit.MILLISECONDS
import java.util.concurrent.TimeUnit.SECONDS

class ConnectionTest {
    private val server = MockWebServer()
    private val udp = DatagramSocket(0, InetAddress.getLoopbackAddress()).apply { soTimeout = 3000 }
    private var conn: Connection? = null

    @After
    fun tearDown() {
        conn?.close()
        udp.close()
        server.shutdown()
    }

    private class Recorder : Connection.Listener {
        val states = LinkedBlockingQueue<ConnState>()
        val messages = LinkedBlockingQueue<ServerMessage>()
        override fun onState(state: ConnState) { states += state }
        override fun onMessage(msg: ServerMessage) { messages += msg }
        override fun onVoice(p: ServerPacket) {}
    }

    private val welcome get() =
        """{"type":"welcome","userId":2,"session":4000000000,"udpPort":${udp.localPort},"serverName":"Test","channels":[],"perms":{"allCall":false,"privateCall":false}}"""

    /** A server that answers hello with [reply], then optionally closes the socket. */
    private fun upgrade(reply: () -> String, closeAfter: Boolean = false, hellos: LinkedBlockingQueue<String>? = null) =
        MockResponse().withWebSocketUpgrade(object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                if (!text.contains("\"hello\"")) return
                hellos?.add(text)
                webSocket.send(reply())
                if (closeAfter) webSocket.close(1000, "bye")
            }
        })

    private fun connect(rec: Recorder, port: Int = server.port) =
        Connection("127.0.0.1", port, "anna", "pw", "test", rec).also { conn = it; it.start() }

    @Test
    fun loginSendsHelloThenKeepalive() {
        val hellos = LinkedBlockingQueue<String>()
        server.enqueue(upgrade({ welcome }, hellos = hellos))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertTrue(hellos.poll(3, SECONDS)!!.contains("\"username\":\"anna\""))
        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        assertEquals(4000000000L, (rec.messages.poll(3, SECONDS) as ServerMessage.Welcome).session)
        val pkt = DatagramPacket(ByteArray(64), 64)
        udp.receive(pkt)
        assertArrayEquals(encodeClientPacket(KIND_KEEPALIVE, 4000000000L, 0), pkt.data.copyOf(pkt.length))
    }

    @Test
    fun loginErrorFailsWithoutRetry() {
        server.enqueue(upgrade({ """{"type":"error","code":"bad_credentials","message":"Nome utente o password errati"}""" }, closeAfter = true))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Failed("Nome utente o password errati", clearPassword = true), rec.states.poll(3, SECONDS))
        assertNull(rec.states.poll(1500, MILLISECONDS)) // no reconnect attempt
    }

    @Test
    fun firstConnectUnreachableFails() {
        val closedPort = ServerSocket(0).use { it.localPort }
        val rec = Recorder()
        connect(rec, closedPort)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        val failed = rec.states.poll(8, SECONDS) as ConnState.Failed
        assertTrue(failed.message, failed.message.contains("non raggiungibile"))
    }

    @Test
    fun dropAfterLoginReconnects() {
        server.enqueue(upgrade({ welcome }, closeAfter = true))
        server.enqueue(upgrade({ welcome }))
        server.start()
        val rec = Recorder()
        connect(rec)

        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Reconnecting(1), rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(5, SECONDS))
    }

    @Test
    fun kickIsFinal() {
        server.enqueue(upgrade({ welcome }))
        server.start()
        val rec = Recorder()
        connect(rec)
        assertEquals(ConnState.Connecting, rec.states.poll(3, SECONDS))
        assertEquals(ConnState.Online, rec.states.poll(3, SECONDS))
        // the server side of the socket is not reachable from here, so feed the message directly
        conn!!.handleForTest("""{"type":"kicked","reason":"replaced"}""")
        assertEquals(ConnState.Failed("Collegato da un altro dispositivo", clearPassword = false), rec.states.poll(3, SECONDS))
    }

    @Test
    fun backoffSequence() {
        assertEquals(listOf(1000L, 2000L, 4000L, 5000L, 5000L), (0..4).map(::backoffMs))
    }
}
```

- [ ] **Step 2: Verifica che falliscano**

Run: `cd android && ./gradlew testDebugUnitTest --tests '*ConnectionTest*'`
Expected: FAIL, `Unresolved reference: Connection`

- [ ] **Step 3: Implementa**

`android/app/src/main/java/app/intercom/android/net/Connection.kt`:

```kotlin
package app.intercom.android.net

import app.intercom.android.proto.ClientMessages
import app.intercom.android.proto.KIND_KEEPALIVE
import app.intercom.android.proto.KIND_VOICE
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.decodeServerPacket
import app.intercom.android.proto.encodeClientPacket
import app.intercom.android.proto.parseServerMessage
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.io.IOException
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.InetSocketAddress
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import kotlin.concurrent.thread

sealed interface ConnState {
    data object Connecting : ConnState
    data object Online : ConnState
    data class Reconnecting(val attempt: Int) : ConnState
    /** Final: login refused, kicked, or never reached. No retry. */
    data class Failed(val message: String, val clearPassword: Boolean) : ConnState
}

/** Delay before reconnect attempt n (0-based): 1, 2, 4, then 5 s. */
fun backoffMs(attempt: Int): Long = minOf(5000L, 1000L shl minOf(attempt, 3))

fun kickMessage(reason: String): String = when (reason) {
    "replaced" -> "Collegato da un altro dispositivo"
    "admin" -> "Disconnesso dall'amministratore"
    "disabled" -> "Utente disabilitato"
    else -> "Disconnesso dal server"
}

/**
 * One login to the server: WebSocket for control, UDP for audio. Reconnects on its own after a drop;
 * stops for good on login errors, kicks, or if the very first connection fails.
 * Listener callbacks run on network threads.
 */
class Connection(
    private val host: String,
    private val port: Int,
    private val username: String,
    private val password: String,
    private val device: String,
    private val listener: Listener,
) {
    interface Listener {
        fun onState(state: ConnState)
        fun onMessage(msg: ServerMessage)
        fun onVoice(p: ServerPacket)
    }

    private val http = OkHttpClient.Builder().connectTimeout(5, TimeUnit.SECONDS).readTimeout(0, TimeUnit.MILLISECONDS).build()
    private val timer = Executors.newSingleThreadScheduledExecutor()
    private val seq = AtomicInteger()

    @Volatile private var ws: WebSocket? = null
    @Volatile private var udp: DatagramSocket? = null
    @Volatile private var udpTarget: InetSocketAddress? = null
    @Volatile private var session = 0L
    @Volatile private var closed = false
    @Volatile private var everOnline = false
    @Volatile private var generation = 0
    private var attempt = 0

    fun start() {
        listener.onState(ConnState.Connecting)
        timer.scheduleWithFixedDelay({ send(ClientMessages.ping()) }, 3, 3, TimeUnit.SECONDS)
        timer.scheduleWithFixedDelay({ sendUdp(KIND_KEEPALIVE, ByteArray(0)) }, 5, 5, TimeUnit.SECONDS)
        connect()
    }

    /** Sends a control message; dropped while not logged in. */
    fun send(text: String) {
        if (session != 0L) ws?.send(text)
    }

    fun sendVoice(payload: ByteArray) = sendUdp(KIND_VOICE, payload)

    fun close() {
        closed = true
        session = 0
        ws?.close(1000, null)
        udp?.close()
        timer.shutdownNow()
    }

    internal fun handleForTest(text: String) = handle(text)

    @Synchronized
    private fun connect() {
        if (closed) return
        val gen = ++generation
        val url = "ws://${if (':' in host) "[$host]" else host}:$port/ws"
        ws = http.newWebSocket(Request.Builder().url(url).build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                webSocket.send(ClientMessages.hello(username, password, device))
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                if (gen == generation) handle(text)
            }

            override fun onClosing(webSocket: WebSocket, code: Int, reason: String) {
                webSocket.close(1000, null)
                lost(gen)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) = lost(gen)
        })
    }

    private fun handle(text: String) {
        when (val msg = parseServerMessage(text) ?: return) {
            is ServerMessage.Welcome -> {
                session = msg.session
                attempt = 0
                everOnline = true
                openUdp(msg.udpPort)
                listener.onMessage(msg)
                listener.onState(ConnState.Online)
            }
            is ServerMessage.Error -> fail(msg.message, clearPassword = msg.code == "bad_credentials")
            is ServerMessage.Kicked -> fail(kickMessage(msg.reason), clearPassword = false)
            else -> listener.onMessage(msg)
        }
    }

    private fun openUdp(udpPort: Int) {
        udp?.close()
        val sock = DatagramSocket()
        udpTarget = InetSocketAddress(InetAddress.getByName(host), udpPort)
        udp = sock
        thread(name = "udp-rx", isDaemon = true) {
            val buf = ByteArray(1500)
            val pkt = DatagramPacket(buf, buf.size)
            while (true) {
                try {
                    sock.receive(pkt)
                } catch (e: IOException) {
                    return@thread
                }
                decodeServerPacket(buf, pkt.length)?.let(listener::onVoice)
            }
        }
        sendUdp(KIND_KEEPALIVE, ByteArray(0))
    }

    private fun sendUdp(kind: Int, payload: ByteArray) {
        val sock = udp ?: return
        val target = udpTarget ?: return
        val s = session
        if (s == 0L) return
        val n = if (kind == KIND_VOICE) seq.getAndIncrement() and 0xFFFF else 0
        val b = encodeClientPacket(kind, s, n, payload)
        try {
            sock.send(DatagramPacket(b, b.size, target))
        } catch (e: IOException) {
            // network down: the reconnect logic will notice on the WebSocket side
        }
    }

    @Synchronized
    private fun lost(gen: Int) {
        if (closed || gen != generation) return
        generation++ // ignore further callbacks from this socket
        session = 0
        udp?.close()
        udp = null
        if (!everOnline) return fail("Server non raggiungibile ($host:$port)", clearPassword = false)
        attempt++
        listener.onState(ConnState.Reconnecting(attempt))
        timer.schedule({ connect() }, backoffMs(attempt - 1), TimeUnit.MILLISECONDS)
    }

    @Synchronized
    private fun fail(message: String, clearPassword: Boolean) {
        if (closed) return
        close()
        listener.onState(ConnState.Failed(message, clearPassword))
    }
}
```

- [ ] **Step 4: Verifica che passino**

Run: `cd android && ./gradlew testDebugUnitTest`
Expected: `BUILD SUCCESSFUL`, tutti i test (compresi i 6 di `ConnectionTest`) passati.

- [ ] **Step 5: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): server connection with reconnect

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Codec Opus (MediaCodec) e credenziali cifrate

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/audio/OpusCodec.kt`
- Create: `android/app/src/main/java/app/intercom/android/SecretBox.kt`
- Test: `android/app/src/androidTest/java/app/intercom/android/OpusCodecTest.kt`
- Test: `android/app/src/androidTest/java/app/intercom/android/SecretBoxTest.kt`

**Interfaces:**
- Produces:
  - `const val SAMPLE_RATE = 48000`, `const val FRAME_SAMPLES = 960` (package `app.intercom.android.audio`)
  - `class OpusEncoder(bitrate: Int = 32000) : AutoCloseable` con `fun encode(pcm: ShortArray): List<ByteArray>`
  - `class OpusDecoder : AutoCloseable` con `fun decode(packet: ByteArray): ShortArray?` (960 campioni o null se non ancora pronto; può lanciare `IllegalStateException`/`MediaCodec.CodecException` su pacchetti corrotti)
  - `object SecretBox { fun encrypt(plain: String): String; fun decrypt(enc: String): String? }` (package `app.intercom.android`)

- [ ] **Step 1: Avvia l'emulatore**

```bash
S=$HOME/Library/Android/sdk
$S/emulator/emulator -avd Pixel_8_Pro_API_35 -no-snapshot-save -no-boot-anim > /dev/null 2>&1 &
$S/platform-tools/adb wait-for-device
until [ "$($S/platform-tools/adb shell getprop sys.boot_completed | tr -d '\r')" = "1" ]; do sleep 2; done; echo booted
```

Expected: `booted`

- [ ] **Step 2: Scrivi i test che falliscono**

`android/app/src/androidTest/java/app/intercom/android/OpusCodecTest.kt`:

```kotlin
package app.intercom.android

import androidx.test.ext.junit.runners.AndroidJUnit4
import app.intercom.android.audio.FRAME_SAMPLES
import app.intercom.android.audio.OpusDecoder
import app.intercom.android.audio.OpusEncoder
import app.intercom.android.audio.SAMPLE_RATE
import app.intercom.android.audio.levelDb
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import kotlin.math.PI
import kotlin.math.sin

@RunWith(AndroidJUnit4::class)
class OpusCodecTest {
    @Test
    fun sineSurvivesRoundTrip() {
        var t = 0
        val packets = OpusEncoder().use { enc ->
            (0 until 50).flatMap {
                val frame = ShortArray(FRAME_SAMPLES) { (sin(2 * PI * 440 * t++ / SAMPLE_RATE) * 12000).toInt().toShort() }
                enc.encode(frame)
            }
        }
        assertTrue("got ${packets.size} packets", packets.size >= 45)
        assertTrue("packets are compressed", packets.all { it.size in 1..400 })

        val decoded = OpusDecoder().use { dec -> packets.mapNotNull { dec.decode(it) } }
        assertTrue("decoded ${decoded.size} frames", decoded.size >= 40)
        assertTrue(decoded.all { it.size == FRAME_SAMPLES })
        val level = levelDb(decoded[decoded.size - 5])
        assertTrue("level $level dB", level > -25f) // 12000/32768 sine ≈ -11.7 dBFS
    }

    @Test
    fun garbagePacketDoesNotCrashTheProcess() {
        OpusDecoder().use { dec ->
            runCatching { dec.decode(ByteArray(80) { 0x55 }) } // may return null, audio, or throw; must not kill the app
        }
    }
}
```

`android/app/src/androidTest/java/app/intercom/android/SecretBoxTest.kt`:

```kotlin
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
```

- [ ] **Step 3: Verifica che falliscano**

Run: `cd android && ./gradlew connectedDebugAndroidTest`
Expected: FAIL, `Unresolved reference: OpusEncoder`

- [ ] **Step 4: Implementa**

`android/app/src/main/java/app/intercom/android/audio/OpusCodec.kt`:

```kotlin
package app.intercom.android.audio

import android.media.MediaCodec
import android.media.MediaFormat
import java.nio.ByteBuffer
import java.nio.ByteOrder

const val SAMPLE_RATE = 48000
const val FRAME_SAMPLES = 960 // 20 ms, mono

/** System Opus encoder (Android 10+), fed one 20 ms PCM frame at a time. */
class OpusEncoder(bitrate: Int = 32000) : AutoCloseable {
    private val codec = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_AUDIO_OPUS).apply {
        val f = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_OPUS, SAMPLE_RATE, 1)
        f.setInteger(MediaFormat.KEY_BIT_RATE, bitrate)
        configure(f, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
        start()
    }
    private val info = MediaCodec.BufferInfo()
    private var ptsUs = 0L

    /** Returns the packets that came out (usually one; none while the codec warms up). */
    fun encode(pcm: ShortArray): List<ByteArray> {
        val inIdx = codec.dequeueInputBuffer(10_000)
        if (inIdx >= 0) {
            val buf = codec.getInputBuffer(inIdx)!!
            buf.clear()
            buf.order(ByteOrder.nativeOrder()).asShortBuffer().put(pcm)
            codec.queueInputBuffer(inIdx, 0, pcm.size * 2, ptsUs, 0)
            ptsUs += 20_000
        }
        val out = ArrayList<ByteArray>(1)
        while (true) {
            val idx = codec.dequeueOutputBuffer(info, 0)
            if (idx == MediaCodec.INFO_TRY_AGAIN_LATER) break
            if (idx < 0) continue // format or buffers changed
            if (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG == 0 && info.size > 0) {
                val b = codec.getOutputBuffer(idx)!!
                b.position(info.offset)
                b.limit(info.offset + info.size)
                out += ByteArray(info.size).also { b.get(it) }
            }
            codec.releaseOutputBuffer(idx, false)
        }
        return out
    }

    override fun close() {
        codec.stop()
        codec.release()
    }
}

/**
 * System Opus decoder for one talker. A missing frame is played as silence by the caller:
 * MediaCodec exposes no packet-loss concealment.
 */
class OpusDecoder : AutoCloseable {
    private val codec = MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_AUDIO_OPUS).apply {
        val f = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_OPUS, SAMPLE_RATE, 1)
        f.setByteBuffer("csd-0", ByteBuffer.wrap(opusHead()))
        f.setByteBuffer("csd-1", leLong(0)) // pre-skip (ns)
        f.setByteBuffer("csd-2", leLong(80_000_000)) // seek pre-roll (ns)
        configure(f, null, null, 0)
        start()
    }
    private val info = MediaCodec.BufferInfo()
    private val ready = ArrayDeque<ShortArray>()
    private var ptsUs = 0L

    fun decode(packet: ByteArray): ShortArray? {
        val inIdx = codec.dequeueInputBuffer(5_000)
        if (inIdx >= 0) {
            codec.getInputBuffer(inIdx)!!.apply { clear(); put(packet) }
            codec.queueInputBuffer(inIdx, 0, packet.size, ptsUs, 0)
            ptsUs += 20_000
        }
        while (true) {
            // ponytail: waits up to 5 ms for the first output; software Opus answers in well under that.
            val idx = codec.dequeueOutputBuffer(info, if (ready.isEmpty()) 5_000 else 0)
            if (idx == MediaCodec.INFO_TRY_AGAIN_LATER) break
            if (idx < 0) continue
            if (info.size > 0) {
                val sb = codec.getOutputBuffer(idx)!!.apply {
                    position(info.offset)
                    limit(info.offset + info.size)
                }.order(ByteOrder.nativeOrder()).asShortBuffer()
                val frame = ShortArray(FRAME_SAMPLES)
                sb.get(frame, 0, minOf(sb.remaining(), FRAME_SAMPLES))
                ready.addLast(frame)
            }
            codec.releaseOutputBuffer(idx, false)
        }
        return ready.removeFirstOrNull()
    }

    override fun close() {
        codec.stop()
        codec.release()
    }

    private companion object {
        /** RFC 7845 identification header: mono, 48 kHz, no pre-skip, mapping family 0. */
        fun opusHead(): ByteArray = ByteBuffer.allocate(19).order(ByteOrder.LITTLE_ENDIAN).apply {
            put("OpusHead".toByteArray())
            put(1.toByte()) // version
            put(1.toByte()) // channels
            putShort(0) // pre-skip
            putInt(SAMPLE_RATE)
            putShort(0) // output gain
            put(0.toByte()) // mapping family
        }.array()

        fun leLong(v: Long): ByteBuffer = ByteBuffer.wrap(ByteBuffer.allocate(8).order(ByteOrder.LITTLE_ENDIAN).putLong(v).array())
    }
}
```

`android/app/src/main/java/app/intercom/android/SecretBox.kt`:

```kotlin
package app.intercom.android

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** Encrypts small strings (the saved password) with an AES-GCM key that never leaves the Android Keystore. */
object SecretBox {
    private const val ALIAS = "intercom-credentials"

    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getKey(ALIAS, null) as? SecretKey)?.let { return it }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .build(),
        )
        return gen.generateKey()
    }

    fun encrypt(plain: String): String {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        return Base64.encodeToString(c.iv + c.doFinal(plain.toByteArray()), Base64.NO_WRAP)
    }

    /** Null if the data was tampered with or the key is gone (e.g. app data restored on another phone). */
    fun decrypt(enc: String): String? = runCatching {
        val b = Base64.decode(enc, Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, b, 0, 12))
        String(c.doFinal(b, 12, b.size - 12))
    }.getOrNull()
}
```

- [ ] **Step 5: Verifica che passino**

Run: `cd android && ./gradlew connectedDebugAndroidTest testDebugUnitTest`
Expected: `BUILD SUCCESSFUL`; `OpusCodecTest` (2) e `SecretBoxTest` (1) passati sull'emulatore.

- [ ] **Step 6: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): Opus codec via MediaCodec and keystore-encrypted secrets

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Motore audio, servizio, preferenze, ricerca server e controller

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/audio/AudioEngine.kt`
- Create: `android/app/src/main/java/app/intercom/android/IntercomService.kt`
- Create: `android/app/src/main/java/app/intercom/android/Prefs.kt`
- Create: `android/app/src/main/java/app/intercom/android/Discovery.kt`
- Create: `android/app/src/main/java/app/intercom/android/IntercomController.kt`
- Create: `android/app/src/main/java/app/intercom/android/IntercomApp.kt`
- Create: `android/app/src/main/res/drawable/ic_mic.xml`, `android/app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml`, `android/app/src/main/res/values/colors.xml`
- Modify: `android/app/src/main/AndroidManifest.xml` (sostituzione completa)

**Interfaces:**
- Consumes: `Connection`, `ConnState` (Task 4); `Vox`, `JitterBuffer`, `mix` (Task 2); `OpusEncoder/Decoder`, `SecretBox` (Task 5); `TalkButton`, `chooseTarget`, `TxDecider`, `TalkingTracker` (Task 3); `proto` (Task 1).
- Produces:
  - `class AudioEngine(context, capture: Boolean, onFrame: (Boolean) -> Boolean, sendVoice: (ByteArray) -> Unit, gain: (Int) -> Float)` con `val vox: Vox`, `start()`, `stop()`, `onPacket(ServerPacket)`, `applyOutput(output: String)`
  - `const val OUTPUT_AUTO = "auto"`, `OUTPUT_HEADSET = "headset"`, `OUTPUT_SPEAKER = "speaker"`; `class Prefs(context)`
  - `data class FoundServer(name, host, port)`; `class Discovery(context)` con `val servers: StateFlow<List<FoundServer>>`, `start()`, `stop()`
  - `enum class Screen { CONNECT, MAIN, SETTINGS }`, `const val MASTER = -1`, `data class UiState(...)` (campi sotto)
  - `class IntercomController(app: Context)` con `val state: StateFlow<UiState>` e le azioni: `autoConnect()`, `connect(host, port, username, password, remember)`, `logout()`, `toggleTalk(id)`, `toggleListen(id)`, `talkDown()`, `talkUp()`, `allCall(pressed)`, `privateCall(userId, pressed)`, `setLocked(locked)`, `setVolume(key, value)`, `setOpenMic(open)`, `setVoxThreshold(db)`, `setOutput(o)`, `setKeepScreenOn(on)`, `openSettings()`, `closeSettings()`, `refreshMicPermission()`
  - `class IntercomApp : Application` con `val controller: IntercomController`

- [ ] **Step 1: Manifest e risorse**

`android/app/src/main/AndroidManifest.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android">

    <uses-permission android:name="android.permission.INTERNET" />
    <uses-permission android:name="android.permission.ACCESS_NETWORK_STATE" />
    <uses-permission android:name="android.permission.ACCESS_WIFI_STATE" />
    <uses-permission android:name="android.permission.RECORD_AUDIO" />
    <uses-permission android:name="android.permission.MODIFY_AUDIO_SETTINGS" />
    <uses-permission android:name="android.permission.WAKE_LOCK" />
    <uses-permission android:name="android.permission.POST_NOTIFICATIONS" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_MICROPHONE" />
    <uses-permission android:name="android.permission.FOREGROUND_SERVICE_MEDIA_PLAYBACK" />

    <application
        android:name=".IntercomApp"
        android:icon="@mipmap/ic_launcher"
        android:label="Intercom"
        android:theme="@android:style/Theme.Material.NoActionBar"
        android:usesCleartextTraffic="true">

        <activity
            android:name=".MainActivity"
            android:exported="true"
            android:launchMode="singleTask"
            android:windowSoftInputMode="adjustResize">
            <intent-filter>
                <action android:name="android.intent.action.MAIN" />
                <category android:name="android.intent.category.LAUNCHER" />
            </intent-filter>
        </activity>

        <service
            android:name=".IntercomService"
            android:exported="false"
            android:foregroundServiceType="microphone|mediaPlayback" />
    </application>
</manifest>
```

`android/app/src/main/res/drawable/ic_mic.xml`:

```xml
<vector xmlns:android="http://schemas.android.com/apk/res/android"
    android:width="24dp" android:height="24dp" android:viewportWidth="24" android:viewportHeight="24">
    <path android:fillColor="#FFFFFFFF"
        android:pathData="M12,1c-4.97,0 -9,4.03 -9,9v7c0,1.66 1.34,3 3,3h3v-8H5v-2c0,-3.87 3.13,-7 7,-7s7,3.13 7,7v2h-4v8h4v1h-7v2h6c1.66,0 3,-1.34 3,-3V10c0,-4.97 -4.03,-9 -9,-9z" />
</vector>
```

`android/app/src/main/res/values/colors.xml`:

```xml
<resources>
    <color name="launcher_background">#FF0E1013</color>
</resources>
```

`android/app/src/main/res/mipmap-anydpi-v26/ic_launcher.xml`:

```xml
<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/launcher_background" />
    <foreground>
        <inset android:drawable="@drawable/ic_mic" android:inset="30%" />
    </foreground>
</adaptive-icon>
```

- [ ] **Step 2: `Prefs.kt` e `Discovery.kt`**

`android/app/src/main/java/app/intercom/android/Prefs.kt`:

```kotlin
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
```

`android/app/src/main/java/app/intercom/android/Discovery.kt`:

```kotlin
package app.intercom.android

import android.content.Context
import android.net.nsd.NsdManager
import android.net.nsd.NsdServiceInfo
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import java.util.concurrent.ConcurrentHashMap

data class FoundServer(val name: String, val host: String, val port: Int)

/** Intercom servers announced on the LAN (mDNS service _intercom._tcp). */
class Discovery(context: Context) {
    private val nsd = context.getSystemService(NsdManager::class.java)
    private val found = ConcurrentHashMap<String, FoundServer>()
    private val _servers = MutableStateFlow<List<FoundServer>>(emptyList())
    val servers: StateFlow<List<FoundServer>> = _servers
    private var active = false

    private val listener = object : NsdManager.DiscoveryListener {
        override fun onServiceFound(info: NsdServiceInfo) {
            // ponytail: legacy resolveService handles one resolve at a time; enough for the usual single server.
            @Suppress("DEPRECATION")
            nsd.resolveService(info, object : NsdManager.ResolveListener {
                override fun onServiceResolved(r: NsdServiceInfo) {
                    @Suppress("DEPRECATION")
                    val host = r.host?.hostAddress ?: return
                    found[r.serviceName] = FoundServer(r.serviceName, host, r.port)
                    publish()
                }

                override fun onResolveFailed(i: NsdServiceInfo, errorCode: Int) {}
            })
        }

        override fun onServiceLost(info: NsdServiceInfo) {
            found.remove(info.serviceName)
            publish()
        }

        override fun onDiscoveryStarted(serviceType: String) {}
        override fun onDiscoveryStopped(serviceType: String) {}
        override fun onStartDiscoveryFailed(serviceType: String, errorCode: Int) {}
        override fun onStopDiscoveryFailed(serviceType: String, errorCode: Int) {}
    }

    private fun publish() {
        _servers.value = found.values.sortedBy { it.name }
    }

    fun start() {
        if (active) return
        active = true
        nsd.discoverServices("_intercom._tcp", NsdManager.PROTOCOL_DNS_SD, listener)
    }

    fun stop() {
        if (!active) return
        active = false
        runCatching { nsd.stopServiceDiscovery(listener) }
    }
}
```

- [ ] **Step 3: `AudioEngine.kt`**

`android/app/src/main/java/app/intercom/android/audio/AudioEngine.kt`:

```kotlin
package app.intercom.android.audio

import android.annotation.SuppressLint
import android.content.Context
import android.media.AudioAttributes
import android.media.AudioDeviceInfo
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioRecord
import android.media.AudioTrack
import android.media.MediaRecorder
import android.media.audiofx.AcousticEchoCanceler
import android.media.audiofx.NoiseSuppressor
import android.os.Build
import android.os.Process
import android.os.SystemClock
import app.intercom.android.OUTPUT_AUTO
import app.intercom.android.OUTPUT_SPEAKER
import app.intercom.android.proto.KIND_VOICE
import app.intercom.android.proto.ServerPacket
import java.util.concurrent.ConcurrentHashMap
import kotlin.concurrent.thread

/**
 * Capture: mic → VOX → (if [onFrame] says on air) Opus → [sendVoice], every 20 ms.
 * Playback: per-talker jitter buffer → Opus → gain by channel → mix → speaker, every 20 ms.
 */
class AudioEngine(
    context: Context,
    private val capture: Boolean,
    private val onFrame: (voiceDetected: Boolean) -> Boolean,
    private val sendVoice: (ByteArray) -> Unit,
    private val gain: (channelId: Int) -> Float,
) {
    val vox = Vox()
    private val am = context.getSystemService(AudioManager::class.java)
    private val voices = ConcurrentHashMap<Int, Voice>()
    private val threads = mutableListOf<Thread>()
    @Volatile private var running = false

    private class Voice {
        var decoder = OpusDecoder()
        val jitter = JitterBuffer()
        @Volatile var channelId = 0
        @Volatile var lastPacketAt = SystemClock.elapsedRealtime()
    }

    fun start() {
        running = true
        am.mode = AudioManager.MODE_IN_COMMUNICATION
        threads += thread(name = "audio-out") { playLoop() }
        if (capture) threads += thread(name = "audio-in") { captureLoop() }
    }

    fun stop() {
        running = false
        threads.forEach { it.join(500) }
        threads.clear()
        voices.values.forEach { it.decoder.close() }
        voices.clear()
        if (Build.VERSION.SDK_INT >= 31) am.clearCommunicationDevice() else @Suppress("DEPRECATION") { am.isSpeakerphoneOn = false }
        am.mode = AudioManager.MODE_NORMAL
    }

    fun onPacket(p: ServerPacket) {
        if (!running || p.kind != KIND_VOICE) return
        val v = voices.computeIfAbsent(p.talkerId) { Voice() }
        synchronized(v) {
            v.jitter.put(p.seq, p.payload)
            v.channelId = p.channelId
            v.lastPacketAt = SystemClock.elapsedRealtime()
        }
    }

    /** auto: headset/Bluetooth if connected, else loudspeaker. headset: headset or earpiece. speaker: loudspeaker. */
    fun applyOutput(output: String) {
        if (Build.VERSION.SDK_INT >= 31) {
            val devs = am.availableCommunicationDevices
            val headsets = setOf(
                AudioDeviceInfo.TYPE_BLUETOOTH_SCO, AudioDeviceInfo.TYPE_BLE_HEADSET,
                AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET,
            )
            val speaker = devs.firstOrNull { it.type == AudioDeviceInfo.TYPE_BUILTIN_SPEAKER }
            val pick = when (output) {
                OUTPUT_SPEAKER -> speaker
                OUTPUT_AUTO -> devs.firstOrNull { it.type in headsets } ?: speaker
                else -> devs.firstOrNull { it.type in headsets }
            }
            if (pick != null) am.setCommunicationDevice(pick) else am.clearCommunicationDevice()
        } else {
            // ponytail: Android 10-11 only switch loudspeaker on/off; Bluetooth routing is left to the system there.
            @Suppress("DEPRECATION")
            am.isSpeakerphoneOn = output == OUTPUT_SPEAKER || (output == OUTPUT_AUTO && !am.isWiredHeadsetOn)
        }
    }

    private fun playLoop() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO)
        val min = AudioTrack.getMinBufferSize(SAMPLE_RATE, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val track = AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_VOICE_COMMUNICATION)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(min, FRAME_SAMPLES * 2 * 4))
            .setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY)
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()
        track.play()
        val out = ShortArray(FRAME_SAMPLES)
        val pcm = ArrayList<ShortArray>()
        val gains = ArrayList<Float>()
        try {
            while (running) {
                pcm.clear()
                gains.clear()
                val now = SystemClock.elapsedRealtime()
                val it = voices.entries.iterator()
                while (it.hasNext()) {
                    val v = it.next().value
                    if (now - v.lastPacketAt > 500) { // talker silent for 500 ms: drop it from the mix
                        it.remove()
                        v.decoder.close()
                        continue
                    }
                    val packet = synchronized(v) { v.jitter.poll() } ?: continue
                    val frame = try {
                        v.decoder.decode(packet)
                    } catch (e: IllegalStateException) { // corrupt packet put the codec in error state (CodecException included)
                        v.decoder.close()
                        v.decoder = OpusDecoder()
                        null
                    }
                    if (frame != null) {
                        pcm += frame
                        gains += gain(v.channelId)
                    }
                }
                mix(pcm, gains, out)
                track.write(out, 0, out.size) // blocks once the buffer is full: paces the loop at 20 ms
            }
        } finally {
            track.stop()
            track.release()
        }
    }

    @SuppressLint("MissingPermission") // only started when RECORD_AUDIO is granted
    private fun captureLoop() {
        Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO)
        val min = AudioRecord.getMinBufferSize(SAMPLE_RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT)
        val rec = AudioRecord.Builder()
            .setAudioSource(MediaRecorder.AudioSource.VOICE_COMMUNICATION)
            .setAudioFormat(
                AudioFormat.Builder()
                    .setSampleRate(SAMPLE_RATE)
                    .setChannelMask(AudioFormat.CHANNEL_IN_MONO)
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .build(),
            )
            .setBufferSizeInBytes(maxOf(min, FRAME_SAMPLES * 2 * 4))
            .build()
        if (AcousticEchoCanceler.isAvailable()) AcousticEchoCanceler.create(rec.audioSessionId)?.enabled = true
        if (NoiseSuppressor.isAvailable()) NoiseSuppressor.create(rec.audioSessionId)?.enabled = true
        val frame = ShortArray(FRAME_SAMPLES)
        val encoder = OpusEncoder()
        rec.startRecording()
        try {
            while (running) {
                var n = 0
                while (n < FRAME_SAMPLES) {
                    val r = rec.read(frame, n, FRAME_SAMPLES - n)
                    if (r < 0) return // recorder died; finally cleans up
                    n += r
                }
                if (onFrame(vox.process(frame))) encoder.encode(frame).forEach(sendVoice)
            }
        } finally {
            rec.stop()
            rec.release()
            encoder.close()
        }
    }
}
```

- [ ] **Step 4: `IntercomService.kt` e `IntercomApp.kt`**

`android/app/src/main/java/app/intercom/android/IntercomService.kt`:

```kotlin
package app.intercom.android

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.net.wifi.WifiManager
import android.os.IBinder
import android.os.PowerManager
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat

/** Keeps the process, CPU and Wi-Fi awake while connected, so audio keeps flowing with the screen off. */
class IntercomService : Service() {
    private var wake: PowerManager.WakeLock? = null
    private var wifi: WifiManager.WifiLock? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CHANNEL, "Intercom attivo", NotificationManager.IMPORTANCE_LOW))
        val open = PendingIntent.getActivity(this, 0, Intent(this, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val notification = NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_mic)
            .setContentTitle("Intercom collegato")
            .setContentText("L'audio resta attivo anche a schermo spento")
            .setContentIntent(open)
            .setOngoing(true)
            .build()
        val mic = ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED
        val type = ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK or
            (if (mic) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0)
        ServiceCompat.startForeground(this, 1, notification, type)
        if (wake == null) {
            wake = getSystemService(PowerManager::class.java)
                .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "intercom:audio").apply { acquire() }
            @Suppress("DEPRECATION")
            wifi = getSystemService(WifiManager::class.java)
                .createWifiLock(WifiManager.WIFI_MODE_FULL_LOW_LATENCY, "intercom:audio").apply { acquire() }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        wake?.release()
        wifi?.release()
        super.onDestroy()
    }

    private companion object {
        const val CHANNEL = "intercom"
    }
}
```

`android/app/src/main/java/app/intercom/android/IntercomApp.kt`:

```kotlin
package app.intercom.android

import android.app.Application

class IntercomApp : Application() {
    val controller by lazy { IntercomController(this) }
}
```

- [ ] **Step 5: `IntercomController.kt`**

`android/app/src/main/java/app/intercom/android/IntercomController.kt`:

```kotlin
package app.intercom.android

import android.Manifest
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import androidx.core.content.ContextCompat
import app.intercom.android.audio.AudioEngine
import app.intercom.android.net.ConnState
import app.intercom.android.net.Connection
import app.intercom.android.proto.CHAN_ALL
import app.intercom.android.proto.CHAN_PRIVATE
import app.intercom.android.proto.ChannelInfo
import app.intercom.android.proto.ClientMessages
import app.intercom.android.proto.Perms
import app.intercom.android.proto.ServerMessage
import app.intercom.android.proto.ServerPacket
import app.intercom.android.proto.Target
import app.intercom.android.proto.UserStatus
import app.intercom.android.talk.TalkButton
import app.intercom.android.talk.TalkingTracker
import app.intercom.android.talk.TxDecider
import app.intercom.android.talk.chooseTarget
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

enum class Screen { CONNECT, MAIN, SETTINGS }

/** Key of the master volume in [UiState.volumes]; channel volumes use the channel id. */
const val MASTER = -1

data class UiState(
    val screen: Screen = Screen.CONNECT,
    val conn: ConnState? = null,
    val error: String? = null,
    val host: String = "",
    val port: Int = 8080,
    val username: String = "",
    val serverName: String = "",
    val userId: Int = 0,
    val channels: List<ChannelInfo> = emptyList(),
    val perms: Perms = Perms(allCall = false, privateCall = false),
    val talk: Set<Int> = emptySet(),
    val listen: Set<Int> = emptySet(),
    val users: List<UserStatus> = emptyList(),
    val activity: Map<Int, List<String>> = emptyMap(),
    val incoming: String? = null,
    val buttonMode: TalkButton.Mode = TalkButton.Mode.IDLE,
    val onAir: Boolean = false,
    val holdingAll: Boolean = false,
    val holdingUser: Int? = null,
    val locked: Boolean = false,
    val micAllowed: Boolean = true,
    val volumes: Map<Int, Float> = emptyMap(),
    val openMic: Boolean = false,
    val voxThresholdDb: Float = -45f,
    val output: String = OUTPUT_AUTO,
    val keepScreenOn: Boolean = true,
)

/** App-wide intercom session: owns the connection and the audio engine, exposes [state] to the UI. */
class IntercomController(private val app: Context) {
    val prefs = Prefs(app)
    private val _state = MutableStateFlow(
        UiState(
            host = prefs.host, port = prefs.port, username = prefs.username, openMic = prefs.openMic,
            voxThresholdDb = prefs.voxThresholdDb, output = prefs.output, keepScreenOn = prefs.keepScreenOn,
        ),
    )
    val state: StateFlow<UiState> = _state.asStateFlow()

    private val main = Handler(Looper.getMainLooper())
    private val lock = Any() // guards button and tx, shared by UI thread and capture thread
    private val button = TalkButton()
    private val tx = TxDecider()
    private val talking = TalkingTracker()
    @Volatile private var holdAll = false
    @Volatile private var holdUser: Int? = null
    @Volatile private var conn: Connection? = null
    private var connGen = 0
    private var engine: AudioEngine? = null

    private fun update(f: (UiState) -> UiState) = _state.update(f)

    fun autoConnect() {
        val pass = prefs.password ?: return
        if (conn == null && prefs.host.isNotBlank()) connect(prefs.host, prefs.port, prefs.username, pass, remember = true)
    }

    fun connect(host: String, port: Int, username: String, password: String, remember: Boolean) {
        disconnect()
        prefs.host = host
        prefs.port = port
        prefs.username = username
        prefs.password = if (remember) password else null
        update { it.copy(conn = ConnState.Connecting, error = null, host = host, port = port, username = username) }
        val gen = ++connGen
        conn = Connection(host, port, username, password, Build.MODEL, object : Connection.Listener {
            override fun onState(state: ConnState) {
                main.post { if (gen == connGen) onConnState(state) }
            }

            override fun onMessage(msg: ServerMessage) {
                main.post { if (gen == connGen) onServerMessage(msg) }
            }

            override fun onVoice(p: ServerPacket) {
                engine?.onPacket(p)
            }
        }).also { it.start() }
    }

    fun logout() {
        prefs.password = null // an explicit logout must not auto-login on next start
        disconnect()
        update { it.copy(screen = Screen.CONNECT, conn = null, error = null) }
    }

    private fun disconnect() {
        connGen++
        conn?.close()
        conn = null
        engine?.stop()
        engine = null
        app.stopService(Intent(app, IntercomService::class.java))
        synchronized(lock) {
            button.reset()
            tx.reset()
        }
        holdAll = false
        holdUser = null
        talking.clear()
        update {
            it.copy(
                activity = emptyMap(), incoming = null, buttonMode = TalkButton.Mode.IDLE, onAir = false,
                holdingAll = false, holdingUser = null, locked = false,
            )
        }
    }

    private fun onConnState(s: ConnState) {
        when (s) {
            is ConnState.Failed -> {
                if (s.clearPassword) prefs.password = null
                disconnect()
                update { it.copy(screen = Screen.CONNECT, conn = null, error = s.message) }
            }
            is ConnState.Reconnecting -> {
                synchronized(lock) { button.reset() } // a latched mic stays closed when the link comes back
                holdAll = false
                holdUser = null
                talking.clear()
                update {
                    it.copy(
                        conn = s, buttonMode = TalkButton.Mode.IDLE, holdingAll = false, holdingUser = null,
                        activity = emptyMap(), incoming = null,
                    )
                }
            }
            else -> update { it.copy(conn = s) }
        }
    }

    private fun onServerMessage(m: ServerMessage) {
        when (m) {
            is ServerMessage.Welcome -> {
                val first = _state.value.screen == Screen.CONNECT
                synchronized(lock) { tx.reset() } // the server has no transmission for this new session
                update {
                    it.copy(
                        screen = if (first) Screen.MAIN else it.screen,
                        serverName = m.serverName, userId = m.userId, channels = m.channels, perms = m.perms,
                        talk = if (first) emptySet() else keep(it.talk, m.channels) { c -> c.canTalk },
                        listen = if (first) m.channels.filter { c -> c.canListen }.map { c -> c.id }.toSet()
                        else keep(it.listen, m.channels) { c -> c.canListen },
                        volumes = loadVolumes(m.channels),
                    )
                }
                sendState()
                if (engine == null) startAudio()
            }
            is ServerMessage.Config -> {
                update {
                    it.copy(
                        channels = m.channels, perms = m.perms, volumes = loadVolumes(m.channels),
                        talk = keep(it.talk, m.channels) { c -> c.canTalk },
                        listen = keep(it.listen, m.channels) { c -> c.canListen },
                    )
                }
                if (_state.value.talk.isEmpty()) resetButton()
                sendState()
            }
            is ServerMessage.Users -> update { it.copy(users = m.users) }
            is ServerMessage.Talking -> {
                talking.apply(m)
                update { it.copy(activity = talking.activity(), incoming = talking.banner()) }
            }
            else -> Unit
        }
    }

    private fun keep(ids: Set<Int>, channels: List<ChannelInfo>, ok: (ChannelInfo) -> Boolean): Set<Int> =
        ids.filter { id -> channels.any { it.id == id && ok(it) } }.toSet()

    private fun sendState() {
        val s = _state.value
        conn?.send(ClientMessages.state(s.talk, s.listen))
    }

    private fun micGranted() =
        ContextCompat.checkSelfPermission(app, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED

    private fun startAudio() {
        val mic = micGranted()
        update { it.copy(micAllowed = mic) }
        ContextCompat.startForegroundService(app, Intent(app, IntercomService::class.java))
        engine = AudioEngine(app, capture = mic, onFrame = ::onFrame, sendVoice = { conn?.sendVoice(it) }, gain = ::gainFor).also {
            it.vox.thresholdDb = prefs.voxThresholdDb
            it.start()
            it.applyOutput(prefs.output)
        }
    }

    /** Called on resume: the user may have granted or revoked the microphone in system settings. */
    fun refreshMicPermission() {
        if (engine != null && micGranted() != _state.value.micAllowed) {
            engine?.stop()
            engine = null
            startAudio()
        }
    }

    /** Capture thread, every 20 ms: decides whether this frame goes on air and tells the server when that changes. */
    private fun onFrame(voice: Boolean): Boolean {
        val s = _state.value
        val target: Target?
        val changes: List<Pair<Boolean, Target>>
        synchronized(lock) {
            target = chooseTarget(holdAll, holdUser, button.mode, s.openMic, voice, s.talk.isNotEmpty())
            changes = tx.update(target)
        }
        for ((active, t) in changes) conn?.send(ClientMessages.ptt(active, t))
        val onAir = target != null
        if (onAir != s.onAir) update { it.copy(onAir = onAir) }
        return onAir
    }

    private fun gainFor(channelId: Int): Float {
        val v = _state.value.volumes
        val master = v[MASTER] ?: 1f
        return if (channelId == CHAN_ALL || channelId == CHAN_PRIVATE) master else master * (v[channelId] ?: 1f)
    }

    private fun volumeKey(key: Int) = "${prefs.host}:${prefs.username}:$key"

    private fun loadVolumes(channels: List<ChannelInfo>): Map<Int, Float> =
        (channels.map { it.id } + MASTER).associateWith { prefs.volume(volumeKey(it)) }

    private fun resetButton() {
        synchronized(lock) { button.reset() }
        update { it.copy(buttonMode = TalkButton.Mode.IDLE) }
    }

    private fun Set<Int>.toggle(id: Int) = if (id in this) this - id else this + id

    fun toggleTalk(id: Int) {
        if (_state.value.locked) return
        update { it.copy(talk = it.talk.toggle(id)) }
        if (_state.value.talk.isEmpty()) resetButton()
        sendState()
    }

    fun toggleListen(id: Int) {
        if (_state.value.locked) return
        update { it.copy(listen = it.listen.toggle(id)) }
        sendState()
    }

    fun talkDown() {
        val m = synchronized(lock) {
            button.down(SystemClock.uptimeMillis())
            button.mode
        }
        update { it.copy(buttonMode = m) }
    }

    fun talkUp() {
        val m = synchronized(lock) {
            button.up(SystemClock.uptimeMillis())
            button.mode
        }
        update { it.copy(buttonMode = m) }
    }

    fun allCall(pressed: Boolean) {
        holdAll = pressed
        update { it.copy(holdingAll = pressed) }
    }

    fun privateCall(userId: Int, pressed: Boolean) {
        holdUser = if (pressed) userId else null
        update { it.copy(holdingUser = if (pressed) userId else null) }
    }

    fun setLocked(locked: Boolean) = update { it.copy(locked = locked) }

    fun setVolume(key: Int, value: Float) {
        prefs.setVolume(volumeKey(key), value)
        update { it.copy(volumes = it.volumes + (key to value)) }
    }

    fun setOpenMic(open: Boolean) {
        prefs.openMic = open
        update { it.copy(openMic = open) }
    }

    fun setVoxThreshold(db: Float) {
        prefs.voxThresholdDb = db
        engine?.vox?.thresholdDb = db
        update { it.copy(voxThresholdDb = db) }
    }

    fun setOutput(output: String) {
        prefs.output = output
        engine?.applyOutput(output)
        update { it.copy(output = output) }
    }

    fun setKeepScreenOn(on: Boolean) {
        prefs.keepScreenOn = on
        update { it.copy(keepScreenOn = on) }
    }

    fun openSettings() = update { it.copy(screen = Screen.SETTINGS) }

    fun closeSettings() = update { it.copy(screen = Screen.MAIN) }
}
```

- [ ] **Step 6: Verifica che compili e che i test restino verdi**

Run: `cd android && ./gradlew assembleDebug testDebugUnitTest lintDebug`
Expected: `BUILD SUCCESSFUL`; lint senza errori (gli avvisi vanno letti ma non bloccano). La build fallisce su `MainActivity` mancante solo se lint la richiede: in quel caso esegui questo passo dopo Task 7 Step 1 e registra un Ruling.

- [ ] **Step 7: Commit**

```bash
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): audio engine, foreground service, prefs, discovery, controller

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Interfaccia (Compose) e verifica end-to-end sull'emulatore

**Files:**
- Create: `android/app/src/main/java/app/intercom/android/MainActivity.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/Theme.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/App.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/ConnectScreen.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/MainScreen.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/Sheets.kt`
- Create: `android/app/src/main/java/app/intercom/android/ui/SettingsScreen.kt`

**Interfaces:**
- Consumes: `IntercomController`, `UiState`, `Screen`, `MASTER`, `OUTPUT_*` (Task 6); `Discovery`, `FoundServer` (Task 6); `ConnState` (Task 4); `TalkButton.Mode` (Task 3); `ChannelInfo` (Task 1).
- Produces: l'app installabile `app-debug.apk`.

Grafica: tema scuro (sfondo `#0E1013`, superfici `#181B20`/`#22262D`); ambra `#F5A524` = PARLA, verde acqua `#2EC4B6` = ASCOLTA, rosso `#E5484D` = in onda, verde `#46A758` = VOX. Canali in **griglia a 2 colonne di schede**; pulsante PARLA **rettangolare arrotondato, largo quanto lo schermo**, in basso; barra icone sotto.

- [ ] **Step 1: `Theme.kt`, `App.kt`, `MainActivity.kt`**

`android/app/src/main/java/app/intercom/android/ui/Theme.kt`:

```kotlin
package app.intercom.android.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

val Amber = Color(0xFFF5A524)
val ListenTeal = Color(0xFF2EC4B6)
val OnAirRed = Color(0xFFE5484D)
val VoxGreen = Color(0xFF46A758)
val Muted = Color(0xFF9AA3AD)

private val scheme = darkColorScheme(
    primary = Amber,
    onPrimary = Color.Black,
    secondary = ListenTeal,
    background = Color(0xFF0E1013),
    surface = Color(0xFF181B20),
    surfaceVariant = Color(0xFF22262D),
    surfaceContainerLow = Color(0xFF181B20),
    onBackground = Color(0xFFE8EAED),
    onSurface = Color(0xFFE8EAED),
    error = OnAirRed,
)

@Composable
fun IntercomTheme(content: @Composable () -> Unit) = MaterialTheme(colorScheme = scheme, content = content)

/** Channel color from the server ("#rrggbb"); falls back to amber on anything else. */
fun parseColor(hex: String): Color = runCatching { Color(android.graphics.Color.parseColor(hex)) }.getOrDefault(Amber)
```

`android/app/src/main/java/app/intercom/android/ui/App.kt`:

```kotlin
package app.intercom.android.ui

import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalView
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.intercom.android.FoundServer
import app.intercom.android.IntercomController
import app.intercom.android.Screen
import kotlinx.coroutines.flow.StateFlow

@Composable
fun App(
    c: IntercomController,
    servers: StateFlow<List<FoundServer>>,
    onConnect: (host: String, port: Int, user: String, pass: String, remember: Boolean) -> Unit,
    onOpenAppSettings: () -> Unit,
) {
    val s by c.state.collectAsStateWithLifecycle()
    val found by servers.collectAsStateWithLifecycle()
    val view = LocalView.current
    SideEffect { view.keepScreenOn = s.keepScreenOn && s.screen != Screen.CONNECT }
    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        when (s.screen) {
            Screen.CONNECT -> ConnectScreen(s, found, onConnect)
            Screen.MAIN -> MainScreen(s, c, onOpenAppSettings)
            Screen.SETTINGS -> SettingsScreen(s, c)
        }
    }
}
```

`android/app/src/main/java/app/intercom/android/MainActivity.kt`:

```kotlin
package app.intercom.android

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import android.media.AudioManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Settings
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import app.intercom.android.ui.App
import app.intercom.android.ui.IntercomTheme

class MainActivity : ComponentActivity() {
    private lateinit var discovery: Discovery
    private var afterPermissions: (() -> Unit)? = null
    private val permissions = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        afterPermissions?.invoke() // connect even if refused: without the mic the app works listen-only
        afterPermissions = null
    }

    private val controller get() = (application as IntercomApp).controller

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        volumeControlStream = AudioManager.STREAM_VOICE_CALL // hardware volume keys drive the intercom audio
        discovery = Discovery(this)
        if (savedInstanceState == null) controller.autoConnect()
        setContent {
            IntercomTheme {
                App(
                    controller,
                    discovery.servers,
                    onConnect = { host, port, user, pass, remember ->
                        withPermissions { controller.connect(host, port, user, pass, remember) }
                    },
                    onOpenAppSettings = {
                        startActivity(Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", packageName, null)))
                    },
                )
            }
        }
    }

    private fun withPermissions(then: () -> Unit) {
        val wanted = buildList {
            add(Manifest.permission.RECORD_AUDIO)
            if (Build.VERSION.SDK_INT >= 33) add(Manifest.permission.POST_NOTIFICATIONS)
        }.filter { ContextCompat.checkSelfPermission(this, it) != PackageManager.PERMISSION_GRANTED }
        if (wanted.isEmpty()) {
            then()
        } else {
            afterPermissions = then
            permissions.launch(wanted.toTypedArray())
        }
    }

    override fun onStart() {
        super.onStart()
        discovery.start()
    }

    override fun onStop() {
        discovery.stop()
        super.onStop()
    }

    override fun onResume() {
        super.onResume()
        controller.refreshMicPermission()
    }
}
```

- [ ] **Step 2: `ConnectScreen.kt`**

`android/app/src/main/java/app/intercom/android/ui/ConnectScreen.kt`:

```kotlin
package app.intercom.android.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import app.intercom.android.FoundServer
import app.intercom.android.UiState
import app.intercom.android.net.ConnState

@Composable
fun ConnectScreen(
    s: UiState,
    servers: List<FoundServer>,
    onConnect: (host: String, port: Int, user: String, pass: String, remember: Boolean) -> Unit,
) {
    var host by rememberSaveable { mutableStateOf(s.host) }
    var port by rememberSaveable { mutableStateOf(s.port.toString()) }
    var user by rememberSaveable { mutableStateOf(s.username) }
    var pass by rememberSaveable { mutableStateOf("") }
    var remember by rememberSaveable { mutableStateOf(true) }
    val connecting = s.conn == ConnState.Connecting

    Column(
        Modifier.fillMaxSize().safeDrawingPadding().imePadding().verticalScroll(rememberScrollState()).padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Intercom", style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.Black, color = Amber)
        Text("Server in rete", style = MaterialTheme.typography.titleSmall, color = Muted)
        if (servers.isEmpty()) Text("Ricerca in corso…", color = Muted)
        servers.forEach { srv ->
            OutlinedCard(onClick = { host = srv.host; port = srv.port.toString() }, modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.padding(14.dp)) {
                    Text(srv.name, fontWeight = FontWeight.Bold)
                    Text("${srv.host}:${srv.port}", style = MaterialTheme.typography.bodySmall, color = Muted)
                }
            }
        }
        OutlinedTextField(host, { host = it }, label = { Text("Indirizzo server") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(
            port, { port = it.filter(Char::isDigit) }, label = { Text("Porta") }, singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number), modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(user, { user = it }, label = { Text("Nome utente") }, singleLine = true, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(
            pass, { pass = it }, label = { Text("Password") }, singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password), modifier = Modifier.fillMaxWidth(),
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Checkbox(remember, { remember = it })
            Text("Ricorda credenziali")
        }
        s.error?.let { Text(it, color = MaterialTheme.colorScheme.error) }
        Button(
            onClick = { onConnect(host.trim(), port.toIntOrNull() ?: 8080, user.trim(), pass, remember) },
            enabled = !connecting && host.isNotBlank() && user.isNotBlank() && pass.isNotEmpty(),
            modifier = Modifier.fillMaxWidth().height(56.dp),
        ) {
            Text(if (connecting) "Connessione…" else "Connetti", fontWeight = FontWeight.Bold)
        }
    }
}
```

- [ ] **Step 3: `MainScreen.kt`**

`android/app/src/main/java/app/intercom/android/ui/MainScreen.kt`:

```kotlin
package app.intercom.android.ui

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.Campaign
import androidx.compose.material.icons.filled.Groups
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.LockOpen
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.intercom.android.IntercomController
import app.intercom.android.UiState
import app.intercom.android.net.ConnState
import app.intercom.android.proto.ChannelInfo
import app.intercom.android.talk.TalkButton
import kotlinx.coroutines.delay
import java.time.LocalTime
import java.time.format.DateTimeFormatter

private enum class Sheet { MIXER, USERS }

@Composable
fun MainScreen(s: UiState, c: IntercomController, onOpenAppSettings: () -> Unit) {
    var sheet by remember { mutableStateOf<Sheet?>(null) }
    Column(Modifier.fillMaxSize().safeDrawingPadding()) {
        Header(s, onLogout = c::logout)
        s.incoming?.let { Banner(it, OnAirRed) }
        if (s.conn is ConnState.Reconnecting) Banner("Riconnessione al server…", Amber)
        if (!s.micAllowed) {
            Row(
                Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceVariant).padding(horizontal = 16.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text("Microfono non consentito: solo ascolto", Modifier.weight(1f), style = MaterialTheme.typography.bodySmall)
                TextButton(onClick = onOpenAppSettings) { Text("Consenti") }
            }
        }
        LazyVerticalGrid(
            columns = GridCells.Fixed(2),
            modifier = Modifier.weight(1f).padding(horizontal = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(s.channels, key = { it.id }) { ch ->
                ChannelCard(
                    ch, talk = ch.id in s.talk, listen = ch.id in s.listen, speakers = s.activity[ch.id].orEmpty(),
                    enabled = !s.locked, onTalk = { c.toggleTalk(ch.id) }, onListen = { c.toggleListen(ch.id) },
                )
            }
        }
        TalkButtonView(s, onDown = c::talkDown, onUp = c::talkUp)
        BottomBar(s, c, onSheet = { sheet = it })
    }
    when (sheet) {
        Sheet.MIXER -> MixerSheet(s, c, onDismiss = { sheet = null })
        Sheet.USERS -> UsersSheet(s, c, onDismiss = { sheet = null })
        null -> Unit
    }
}

@Composable
private fun Header(s: UiState, onLogout: () -> Unit) {
    val dot = when (s.conn) {
        ConnState.Online -> VoxGreen
        is ConnState.Reconnecting -> Amber
        else -> OnAirRed
    }
    var now by remember { mutableStateOf(LocalTime.now()) }
    LaunchedEffect(Unit) {
        while (true) {
            now = LocalTime.now()
            delay(1000)
        }
    }
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Box(Modifier.size(12.dp).background(dot, CircleShape))
        Spacer(Modifier.width(10.dp))
        Column(Modifier.weight(1f)) {
            Text(s.username, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            Text(s.serverName, style = MaterialTheme.typography.bodySmall, color = Muted)
        }
        Text(now.format(DateTimeFormatter.ofPattern("HH:mm")), fontSize = 28.sp, fontWeight = FontWeight.Light)
        IconButton(onClick = onLogout, enabled = !s.locked) {
            Icon(Icons.AutoMirrored.Filled.Logout, contentDescription = "Esci")
        }
    }
}

@Composable
private fun Banner(text: String, color: Color) {
    Text(
        text, Modifier.fillMaxWidth().background(color).padding(horizontal = 16.dp, vertical = 8.dp),
        color = Color.Black, fontWeight = FontWeight.Bold,
    )
}

@Composable
private fun ChannelCard(
    ch: ChannelInfo, talk: Boolean, listen: Boolean, speakers: List<String>, enabled: Boolean,
    onTalk: () -> Unit, onListen: () -> Unit,
) {
    val color = parseColor(ch.color)
    val live = speakers.isNotEmpty()
    Surface(
        shape = RoundedCornerShape(14.dp),
        color = MaterialTheme.colorScheme.surface,
        border = BorderStroke(if (live) 2.dp else 1.dp, if (live) color else MaterialTheme.colorScheme.surfaceVariant),
    ) {
        Column(Modifier.padding(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(Modifier.size(10.dp).background(color, CircleShape))
                Spacer(Modifier.width(8.dp))
                Text(ch.name, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Text(
                if (live) "▶ " + speakers.joinToString() else " ",
                style = MaterialTheme.typography.bodySmall, color = color, maxLines = 1, overflow = TextOverflow.Ellipsis,
            )
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                Toggle("PARLA", talk, Amber, enabled && ch.canTalk, onTalk, Modifier.weight(1f))
                Toggle("ASCOLTA", listen, ListenTeal, enabled && ch.canListen, onListen, Modifier.weight(1f))
            }
        }
    }
}

@Composable
private fun Toggle(label: String, on: Boolean, color: Color, enabled: Boolean, onClick: () -> Unit, modifier: Modifier) {
    val fg = if (on) Color.Black else MaterialTheme.colorScheme.onSurface.copy(alpha = if (enabled) 1f else 0.3f)
    Box(
        modifier.height(40.dp).clip(RoundedCornerShape(8.dp))
            .background(if (on) color else MaterialTheme.colorScheme.surfaceVariant)
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = fg, style = MaterialTheme.typography.labelMedium, fontWeight = FontWeight.Bold)
    }
}

@Composable
private fun TalkButtonView(s: UiState, onDown: () -> Unit, onUp: () -> Unit) {
    val enabled = s.talk.isNotEmpty() && s.micAllowed
    val latchName = if (s.openMic) "microfono aperto" else "VOX"
    data class Look(val bg: Color, val title: String, val sub: String)
    val look = when {
        !enabled -> Look(
            MaterialTheme.colorScheme.surfaceVariant, "PARLA",
            if (!s.micAllowed) "microfono non consentito" else "attiva PARLA su almeno un canale",
        )
        s.buttonMode == TalkButton.Mode.HOLD -> Look(OnAirRed, "IN ONDA", "rilascia per chiudere")
        s.buttonMode == TalkButton.Mode.LATCHED && s.openMic -> Look(OnAirRed, "MICROFONO APERTO", "tocca per chiudere")
        s.buttonMode == TalkButton.Mode.LATCHED -> Look(
            if (s.onAir) VoxGreen else VoxGreen.copy(alpha = 0.45f), "VOX",
            if (s.onAir) "in onda" else "in attesa della voce · tocca per chiudere",
        )
        else -> Look(Amber, "PARLA", "tieni premuto · doppio tocco: $latchName")
    }
    Box(
        Modifier.fillMaxWidth().padding(horizontal = 12.dp, vertical = 10.dp).height(128.dp)
            .clip(RoundedCornerShape(24.dp)).background(look.bg)
            .pointerInput(enabled) {
                if (!enabled) return@pointerInput
                awaitEachGesture {
                    awaitFirstDown()
                    onDown()
                    waitForUpOrCancellation()
                    onUp()
                }
            },
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(look.title, fontSize = 34.sp, fontWeight = FontWeight.Black, color = if (enabled) Color.Black else Muted)
            Text(look.sub, style = MaterialTheme.typography.bodySmall, color = if (enabled) Color.Black else Muted)
        }
    }
}

@Composable
private fun BottomBar(s: UiState, c: IntercomController, onSheet: (Sheet) -> Unit) {
    Row(Modifier.fillMaxWidth().padding(bottom = 6.dp), horizontalArrangement = Arrangement.SpaceEvenly) {
        if (s.perms.allCall) {
            HoldIcon(Icons.Filled.Campaign, "Tutti", active = s.holdingAll, enabled = !s.locked && s.micAllowed, onPress = c::allCall)
        }
        BarIcon(
            if (s.locked) Icons.Filled.Lock else Icons.Filled.LockOpen,
            if (s.locked) "Tieni premuto" else "Blocca",
            highlight = s.locked,
            onLongClick = { c.setLocked(false) },
            onClick = { if (!s.locked) c.setLocked(true) },
        )
        BarIcon(Icons.Filled.Tune, "Mixer", enabled = !s.locked) { onSheet(Sheet.MIXER) }
        BarIcon(Icons.Filled.Groups, "Utenti", enabled = !s.locked) { onSheet(Sheet.USERS) }
        BarIcon(Icons.Filled.Settings, "Impostazioni", enabled = !s.locked) { c.openSettings() }
    }
}

@Composable
private fun BarIcon(
    icon: ImageVector, label: String, enabled: Boolean = true, highlight: Boolean = false,
    onLongClick: (() -> Unit)? = null, onClick: () -> Unit,
) {
    val tint = when {
        highlight -> Amber
        enabled -> MaterialTheme.colorScheme.onSurface
        else -> MaterialTheme.colorScheme.onSurface.copy(alpha = 0.3f)
    }
    Column(
        Modifier.clip(RoundedCornerShape(12.dp))
            .combinedClickable(enabled = enabled, onClick = onClick, onLongClick = onLongClick)
            .padding(horizontal = 10.dp, vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = label, tint = tint, modifier = Modifier.size(28.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = tint)
    }
}

/** Icon that transmits while held (all-call). */
@Composable
private fun HoldIcon(icon: ImageVector, label: String, active: Boolean, enabled: Boolean, onPress: (Boolean) -> Unit) {
    val tint = if (enabled) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurface.copy(alpha = 0.3f)
    Column(
        Modifier.clip(RoundedCornerShape(12.dp)).background(if (active) OnAirRed else Color.Transparent)
            .pointerInput(enabled) {
                if (!enabled) return@pointerInput
                awaitEachGesture {
                    awaitFirstDown()
                    onPress(true)
                    waitForUpOrCancellation()
                    onPress(false)
                }
            }
            .padding(horizontal = 10.dp, vertical = 6.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = label, tint = tint, modifier = Modifier.size(28.dp))
        Text(label, style = MaterialTheme.typography.labelSmall, color = tint)
    }
}
```

- [ ] **Step 4: `Sheets.kt` (mixer e utenti)**

`android/app/src/main/java/app/intercom/android/ui/Sheets.kt`:

```kotlin
package app.intercom.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.waitForUpOrCancellation
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Slider
import androidx.compose.material3.SliderDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.intercom.android.IntercomController
import app.intercom.android.MASTER
import app.intercom.android.UiState
import kotlin.math.roundToInt

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MixerSheet(s: UiState, c: IntercomController, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 32.dp)) {
            Text("Mixer", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            Spacer(Modifier.height(8.dp))
            VolumeRow("Generale", s.volumes[MASTER] ?: 1f, Amber) { c.setVolume(MASTER, it) }
            s.channels.filter { it.canListen }.forEach { ch ->
                VolumeRow(ch.name, s.volumes[ch.id] ?: 1f, parseColor(ch.color)) { c.setVolume(ch.id, it) }
            }
        }
    }
}

@Composable
private fun VolumeRow(label: String, value: Float, color: Color, onChange: (Float) -> Unit) {
    Column(Modifier.padding(vertical = 6.dp)) {
        Row {
            Text(label, Modifier.weight(1f))
            Text("${(value * 100).roundToInt()}%", color = Muted)
        }
        Slider(
            value = value, onValueChange = onChange, valueRange = 0f..2f,
            colors = SliderDefaults.colors(thumbColor = color, activeTrackColor = color),
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UsersSheet(s: UiState, c: IntercomController, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(bottom = 32.dp)) {
            Text("Utenti", Modifier.padding(horizontal = 20.dp), style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
            if (s.perms.privateCall) {
                Text(
                    "Tieni premuto su un utente per parlargli in privato",
                    Modifier.padding(horizontal = 20.dp, vertical = 4.dp), style = MaterialTheme.typography.bodySmall, color = Muted,
                )
            }
            LazyColumn {
                items(s.users.filter { it.id != s.userId }, key = { it.id }) { u ->
                    val canCall = s.perms.privateCall && u.online && s.micAllowed
                    val calling = s.holdingUser == u.id
                    Row(
                        Modifier.fillMaxWidth()
                            .background(if (calling) OnAirRed else Color.Transparent)
                            .pointerInput(canCall, u.id) {
                                if (!canCall) return@pointerInput
                                awaitEachGesture {
                                    awaitFirstDown()
                                    c.privateCall(u.id, true)
                                    waitForUpOrCancellation()
                                    c.privateCall(u.id, false)
                                }
                            }
                            .padding(horizontal = 20.dp, vertical = 16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        val dot = when {
                            u.talking -> OnAirRed
                            u.online -> VoxGreen
                            else -> Muted
                        }
                        Box(Modifier.size(10.dp).background(dot, CircleShape))
                        Spacer(Modifier.width(12.dp))
                        Text(u.name, Modifier.weight(1f), fontWeight = FontWeight.Bold)
                        Text(
                            when {
                                calling -> "in onda"
                                u.talking -> "sta parlando"
                                u.online -> "online"
                                else -> "offline"
                            },
                            color = if (calling) Color.Black else Muted,
                        )
                    }
                }
            }
        }
    }
}
```

- [ ] **Step 5: `SettingsScreen.kt`**

`android/app/src/main/java/app/intercom/android/ui/SettingsScreen.kt`:

```kotlin
package app.intercom.android.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import app.intercom.android.IntercomController
import app.intercom.android.OUTPUT_AUTO
import app.intercom.android.OUTPUT_HEADSET
import app.intercom.android.OUTPUT_SPEAKER
import app.intercom.android.UiState
import kotlin.math.roundToInt

@Composable
fun SettingsScreen(s: UiState, c: IntercomController) {
    BackHandler { c.closeSettings() }
    val ctx = LocalContext.current
    val version = ctx.packageManager.getPackageInfo(ctx.packageName, 0).versionName
    Column(Modifier.fillMaxSize().safeDrawingPadding().verticalScroll(rememberScrollState())) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = c::closeSettings) { Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Indietro") }
            Text("Impostazioni", style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
        }
        Column(Modifier.padding(horizontal = 20.dp)) {
            Section("Doppio tocco su PARLA")
            Choice("VOX: trasmette solo quando parli", !s.openMic) { c.setOpenMic(false) }
            Choice("Microfono sempre aperto", s.openMic) { c.setOpenMic(true) }

            Section("Sensibilità VOX")
            Slider(value = s.voxThresholdDb, onValueChange = c::setVoxThreshold, valueRange = -60f..-20f)
            Text(
                "Soglia ${s.voxThresholdDb.roundToInt()} dB · a sinistra si apre con voci più deboli",
                style = MaterialTheme.typography.bodySmall, color = Muted,
            )

            Section("Uscita audio")
            Choice("Automatica (cuffia se collegata, altrimenti altoparlante)", s.output == OUTPUT_AUTO) { c.setOutput(OUTPUT_AUTO) }
            Choice("Cuffia / Bluetooth", s.output == OUTPUT_HEADSET) { c.setOutput(OUTPUT_HEADSET) }
            Choice("Altoparlante", s.output == OUTPUT_SPEAKER) { c.setOutput(OUTPUT_SPEAKER) }

            Section("Schermo")
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Schermo sempre acceso", Modifier.weight(1f))
                Switch(checked = s.keepScreenOn, onCheckedChange = c::setKeepScreenOn)
            }

            Section("Informazioni")
            Text("Server: ${s.serverName} (${s.host}:${s.port})")
            Text("Utente: ${s.username}")
            Text("Versione app: $version")
            Spacer(Modifier.height(32.dp))
        }
    }
}

@Composable
private fun Section(title: String) {
    Text(
        title, Modifier.padding(top = 20.dp, bottom = 4.dp),
        style = MaterialTheme.typography.titleSmall, color = Amber, fontWeight = FontWeight.Bold,
    )
}

@Composable
private fun Choice(label: String, selected: Boolean, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().selectable(selected, onClick = onClick).padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected = selected, onClick = onClick)
        Text(label)
    }
}
```

- [ ] **Step 5b: Build completa**

Run: `cd android && ./gradlew assembleDebug testDebugUnitTest lintDebug`
Expected: `BUILD SUCCESSFUL`

- [ ] **Step 6: Server di prova per l'emulatore**

L'emulatore raggiunge il Mac su `10.0.2.2`. Usa porte diverse dal server dell'utente (8080/8081) per non toccare la sua configurazione.

```bash
D=/Users/dario/Documents/APP/Intercom/.superpowers/e2e; mkdir -p $D; rm -f $D/c.json $D/cookies
printf '{"settings":{"serverName":"E2E","httpPort":18080,"udpPort":18081},"nextId":1}' > $D/c.json
cd /Users/dario/Documents/APP/Intercom/server && (./dist/intercom-server-mac -config $D/c.json > $D/server.log 2>&1 &)
sleep 1; J=$D/cookies; B=localhost:18080/api
curl -s -c $J -X POST $B/setup -d '{"password":"admin1"}'
curl -s -b $J -X POST $B/channels -d '{"name":"Regia","color":"#f5a524"}'
curl -s -b $J -X POST $B/channels -d '{"name":"Luci","color":"#2ec4b6"}'
curl -s -b $J -X POST $B/users -d '{"name":"anna","password":"pw","enabled":true,"allCall":true,"privateCall":true,"channels":[{"channelId":1,"talk":true,"listen":true},{"channelId":2,"talk":true,"listen":true}]}'
curl -s -b $J -X POST $B/users -d '{"name":"bruno","password":"pw","enabled":true,"allCall":true,"privateCall":true,"channels":[{"channelId":1,"talk":true,"listen":true},{"channelId":2,"talk":false,"listen":true}]}'
```

Expected: cinque `{}`.

- [ ] **Step 7: Installa, accedi e verifica le funzioni sull'emulatore**

```bash
A=$HOME/Library/Android/sdk/platform-tools/adb
cd /Users/dario/Documents/APP/Intercom/android && ./gradlew installDebug
$A shell pm grant app.intercom.android android.permission.RECORD_AUDIO
$A shell pm grant app.intercom.android android.permission.POST_NOTIFICATIONS
$A shell am start -n app.intercom.android/.MainActivity
```

Guida l'interfaccia con `adb shell input tap/text/swipe` usando le coordinate da `adb shell uiautomator dump /sdcard/ui.xml && adb pull /sdcard/ui.xml` e controlla con screenshot (`adb exec-out screencap -p > shot.png`, poi Read dell'immagine). Verifica e annota nel registro:

1. Schermata di connessione: inserisci `10.0.2.2`, porta `18080`, `bruno` / `pw`, Connetti → schermata principale con canali Regia e Luci, pallino verde, Luci con PARLA disabilitato (bruno può solo ascoltare). `curl -s -b $J $B/live` mostra bruno online.
2. Ricezione: `cd server && go run ./cmd/testclient -server 127.0.0.1:18080 -user anna -pass pw -channel 1 -talk 3s -listen 1s` → durante i 3 s la scheda Regia mostra "▶ anna" (screenshot). I pacchetti sono byte fittizi, non Opus: l'app **non deve andare in crash** (`adb logcat -d | grep -E "FATAL|AndroidRuntime"` vuoto).
3. Trasmissione: tocca PARLA su Regia, poi in un terminale `go run ./cmd/testclient -server 127.0.0.1:18080 -user anna -pass pw -channel 1 -listen 6s &` e subito `adb shell input swipe X Y X Y 3000` sul pulsante PARLA (pressione di 3 s) → il client di anna stampa circa 150 pacchetti da bruno (`map[2:~150]`); durante la pressione il pulsante è rosso "IN ONDA".
4. Doppio tocco: due `input tap` ravvicinati sul pulsante → "VOX" verde; un tocco → torna "PARLA". Nelle Impostazioni scegli "Microfono sempre aperto" → doppio tocco → "MICROFONO APERTO"; tocco → chiuso.
5. Chiamata a tutti: con anna in ascolto sul canale 1, tieni premuto "Tutti" 2 s → anna riceve pacchetti con canale 0 (verifica con `-listen` e il conteggio).
6. Lucchetto: tocca il lucchetto → i toggle canali non rispondono, PARLA sì; pressione lunga → sbloccato.
7. Riconnessione (Review Focus 1): aggancia il VOX, poi `$A shell cmd connectivity airplane-mode enable`, attendi 3 s, `... airplane-mode disable` → banner "Riconnessione al server…", poi pallino verde, PARLA su Regia ancora attivo, pulsante tornato "PARLA" (non VOX).
8. Solo ascolto (Review Focus 4): Esci, `$A shell pm revoke app.intercom.android android.permission.RECORD_AUDIO`, riavvia l'app e accedi → banner "Microfono non consentito: solo ascolto", pulsante PARLA grigio; il passo 2 funziona ancora.
9. Credenziali errate: Esci, accedi con password `sbagliata` → messaggio "Nome utente o password errati" sulla schermata di connessione.

- [ ] **Step 8: Pulizia e commit**

```bash
pkill -f "intercom-server-mac -config /Users/dario/Documents/APP/Intercom/.superpowers/e2e"
cd /Users/dario/Documents/APP/Intercom
git add android
git commit -m "feat(android): Compose UI and end-to-end verification

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
