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
