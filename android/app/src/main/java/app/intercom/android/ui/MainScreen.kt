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
