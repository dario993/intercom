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
