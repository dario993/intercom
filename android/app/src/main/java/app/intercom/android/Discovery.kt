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
