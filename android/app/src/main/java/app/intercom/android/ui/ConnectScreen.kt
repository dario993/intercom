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
import androidx.compose.material3.OutlinedButton
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
    onCancel: () -> Unit,
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
        if (connecting) {
            OutlinedButton(onClick = onCancel, modifier = Modifier.fillMaxWidth().height(56.dp)) {
                Text("Annulla", fontWeight = FontWeight.Bold)
            }
        }
    }
}
