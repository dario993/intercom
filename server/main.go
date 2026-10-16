// Command intercom-server runs the intercom server and its admin page.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"intercom/internal/config"
	"intercom/internal/discovery"
	"intercom/internal/hub"
	"intercom/internal/web"
)

func main() {
	cfgPath := flag.String("config", "intercom-config.json", "percorso del file di configurazione")
	flag.Parse()

	store, err := config.Open(*cfgPath)
	if err != nil {
		log.Fatalf("configurazione %s non leggibile: %v", *cfgPath, err)
	}
	var st config.Settings
	store.View(func(c *config.Config) { st = c.Settings })

	h := hub.New(store)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{Port: st.UDPPort})
	if err != nil {
		log.Fatalf("porta UDP %d non disponibile: %v", st.UDPPort, err)
	}
	h.UDPPort = st.UDPPort
	h.SendUDP = func(a *net.UDPAddr, b []byte) { udp.WriteToUDP(b, a) }
	go func() { log.Fatalf("UDP: %v", readLoop(udp, h.HandleUDP)) }()
	go func() {
		for range time.Tick(2 * time.Second) {
			h.Reap(10 * time.Second)
		}
	}()

	var ann discovery.Announcer
	if err := ann.Announce(st.ServerName, st.HTTPPort, st.UDPPort); err != nil {
		log.Printf("annuncio mDNS non disponibile (i telefoni dovranno usare l'IP): %v", err)
	}
	defer ann.Close()

	srv := web.New(store, h)
	srv.OnSettings = func(s config.Settings) {
		// Ports only change on restart, so keep announcing the ones in use.
		if err := ann.Announce(s.ServerName, st.HTTPPort, st.UDPPort); err != nil {
			log.Printf("annuncio mDNS: %v", err)
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", st.HTTPPort))
	if err != nil {
		log.Fatalf("porta HTTP %d non disponibile: %v", st.HTTPPort, err)
	}
	printAddresses(st.HTTPPort)
	log.Fatal(http.Serve(ln, srv.Handler()))
}

func printAddresses(port int) {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && ip.IP.To4() != nil {
			log.Printf("Pagina web: http://%s:%d", ip.IP, port)
		}
	}
}

// readLoop feeds datagrams to handle until the socket is closed; other read errors
// (such as ICMP port-unreachable surfacing as ECONNRESET on Windows) are logged and skipped.
func readLoop(r interface {
	ReadFromUDP([]byte) (int, *net.UDPAddr, error)
}, handle func(*net.UDPAddr, []byte)) error {
	buf := make([]byte, 1500)
	for {
		n, addr, err := r.ReadFromUDP(buf)
		if errors.Is(err, net.ErrClosed) {
			return err
		}
		if err != nil {
			log.Printf("UDP: %v", err)
			continue
		}
		handle(addr, buf[:n])
	}
}
