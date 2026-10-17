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
