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
            Screen.CONNECT -> ConnectScreen(s, found, onConnect, onCancel = c::logout)
            Screen.MAIN -> MainScreen(s, c, onOpenAppSettings)
            Screen.SETTINGS -> SettingsScreen(s, c)
        }
    }
}
