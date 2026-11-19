// Command intercom-server runs the intercom server and its admin page.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"intercom/internal/config"
	"intercom/internal/discovery"
	"intercom/internal/hub"
	"intercom/internal/tally"
	"intercom/internal/web"
)

func main() {
	cfgPath := flag.String("config", "intercom-config.json", "percorso del file di configurazione")
	ipFlag := flag.String("ip", "", `IP su cui ascoltare: vuoto = chiedi se ce n'è più d'uno, "tutti" = tutti`)
	flag.Parse()

	addrs := lanAddrs()
	bind, err := chooseAddr(*ipFlag, addrs)
	if err != nil {
		log.Fatal(err)
	}
	var bindIP net.IP
	var ifaces []net.Interface
	if bind != nil {
		bindIP, ifaces, addrs = bind.ip, []net.Interface{bind.iface}, []lanAddr{*bind}
		log.Printf("In ascolto solo su %s (%s)", bind.ip, bind.iface.Name)
	}

	store, err := config.Open(*cfgPath)
	if err != nil {
		log.Fatalf("configurazione %s non leggibile: %v", *cfgPath, err)
	}
	var st config.Settings
	store.View(func(c *config.Config) { st = c.Settings })

	h := hub.New(store)
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: bindIP, Port: st.UDPPort})
	if err != nil {
		log.Fatalf("porta UDP %d non disponibile: %v", st.UDPPort, err)
	}
	h.UDPPort = st.UDPPort
	h.SendUDP = func(a *net.UDPAddr, b []byte) { udp.WriteToUDP(b, a) }
	go func() { log.Fatalf("UDP: %v", readLoop(udp, h.HandleUDP)) }()
	go func() {
		for range time.Tick(time.Second) {
			h.Reap(5 * time.Second)
		}
	}()

	tc := &tally.Client{OnChange: h.SetTally}
	tc.Configure(st.Tally.Enabled, st.Tally.Host)

	var ann discovery.Announcer
	if err := ann.Announce(st.ServerName, st.HTTPPort, st.UDPPort, ifaces); err != nil {
		log.Printf("annuncio mDNS non disponibile (i telefoni dovranno usare l'IP): %v", err)
	}
	defer ann.Close()

	srv := web.New(store, h)
	srv.OnTally = func(t config.TallySettings) { tc.Configure(t.Enabled, t.Host) }
	srv.OnSettings = func(s config.Settings) {
		// Ports only change on restart, so keep announcing the ones in use.
		if err := ann.Announce(s.ServerName, st.HTTPPort, st.UDPPort, ifaces); err != nil {
			log.Printf("annuncio mDNS: %v", err)
		}
	}

	host := ""
	if bindIP != nil {
		host = bindIP.String()
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(st.HTTPPort)))
	if err != nil {
		log.Fatalf("porta HTTP %d non disponibile: %v", st.HTTPPort, err)
	}
	for _, a := range addrs {
		log.Printf("Pagina web: http://%s:%d", a.ip, st.HTTPPort)
	}
	log.Fatal(http.Serve(ln, srv.Handler()))
}

type lanAddr struct {
	ip    net.IP
	iface net.Interface
}

// lanAddrs lists the IPv4 addresses of the interfaces that are up, loopback excluded.
func lanAddrs() []lanAddr {
	var out []lanAddr
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		as, _ := ifc.Addrs()
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
				out = append(out, lanAddr{n.IP.To4(), ifc})
			}
		}
	}
	return out
}

// parseChoice reads a menu answer: "0"/"tutti", a menu number or an IP. nil = all addresses.
func parseChoice(s string, addrs []lanAddr) (*lanAddr, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || strings.EqualFold(s, "tutti") || s == "0.0.0.0" {
		return nil, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n >= 1 && n <= len(addrs) {
			return &addrs[n-1], true
		}
		return nil, false
	}
	for i := range addrs {
		if addrs[i].ip.String() == s {
			return &addrs[i], true
		}
	}
	return nil, false
}

const chooseTimeout = 15 * time.Second

// chooseAddr picks the address to listen on (nil = all): from -ip, or by asking on the
// console when there are several. Without a console, or with no answer, it listens on all.
func chooseAddr(flagIP string, addrs []lanAddr) (*lanAddr, error) {
	if flagIP != "" {
		if a, ok := parseChoice(flagIP, addrs); ok {
			return a, nil
		}
		var ips []string
		for _, a := range addrs {
			ips = append(ips, a.ip.String())
		}
		return nil, fmt.Errorf("l'IP %s non è di questo PC; disponibili: %s", flagIP, strings.Join(ips, ", "))
	}
	if fi, err := os.Stdin.Stat(); len(addrs) < 2 || err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return nil, nil
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	deadline := time.After(chooseTimeout)
	for {
		fmt.Println("Su quale indirizzo ascoltare?")
		fmt.Println("  0) tutti")
		for i, a := range addrs {
			fmt.Printf("  %d) %-15s (%s)\n", i+1, a.ip, a.iface.Name)
		}
		fmt.Printf("Scelta [0, automatico tra %d s]: ", int(chooseTimeout.Seconds()))
		select {
		case s := <-lines:
			if a, ok := parseChoice(s, addrs); ok {
				return a, nil
			}
			fmt.Println("Scelta non valida.")
		case <-deadline:
			fmt.Println("\nNessuna risposta: ascolto su tutti gli indirizzi.")
			return nil, nil
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
