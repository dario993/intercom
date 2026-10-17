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
