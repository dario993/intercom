// Package discovery announces the server on the LAN via mDNS/DNS-SD.
package discovery

import (
	"strconv"
	"sync"

	"github.com/grandcat/zeroconf"
)

const Service = "_intercom._tcp"

type Announcer struct {
	mu  sync.Mutex
	srv *zeroconf.Server
}

// Announce (re)publishes the service; calling it again replaces the previous announcement.
func (a *Announcer) Announce(name string, httpPort, udpPort int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		a.srv.Shutdown()
		a.srv = nil
	}
	txt := []string{"udp=" + strconv.Itoa(udpPort), "name=" + name}
	srv, err := zeroconf.Register(name, Service, "local.", httpPort, txt, nil)
	if err != nil {
		return err
	}
	a.srv = srv
	return nil
}

func (a *Announcer) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srv != nil {
		a.srv.Shutdown()
		a.srv = nil
	}
}
