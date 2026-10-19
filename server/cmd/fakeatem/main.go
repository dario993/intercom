// Command fakeatem simulates a Blackmagic ATEM switcher so the tally can be tried without hardware.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"intercom/internal/tally"
)

func main() {
	addr := flag.String("addr", ":9910", "indirizzo UDP su cui ascoltare")
	flag.Parse()
	f, err := tally.NewFakeATEM(*addr)
	if err != nil {
		log.Fatalf("porta non disponibile: %v", err)
	}
	defer f.Close()
	fmt.Printf("Finto ATEM su UDP %d. Ingressi CAM 1–8 e MP1 (3010).\n", f.Port())
	help := `Comandi (al posto di N, A, B scrivi il numero della camera):
  pgm 3     mette in onda CAM 3
  pvw 4     mette CAM 4 in preview
  mix 1 2   CAM 1 e CAM 2 in onda insieme (dissolvenza)
  ftb       nero on/off
  quit      esce`
	fmt.Println(help)

	sc := bufio.NewScanner(os.Stdin)
	for fmt.Print("> "); sc.Scan(); fmt.Print("> ") {
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		var ids []uint16
		for _, s := range fields[1:] {
			n, err := strconv.Atoi(s)
			if err != nil || n <= 0 || n > 0xffff {
				ids = nil
				break
			}
			ids = append(ids, uint16(n))
		}
		switch {
		case fields[0] == "quit":
			return
		case fields[0] == "pgm" && len(ids) == 1:
			f.Set(func(s *tally.Snapshot) { s.Program, s.ProgramSource = ids, ids[0] })
		case fields[0] == "pvw" && len(ids) == 1:
			f.Set(func(s *tally.Snapshot) { s.Preview = ids })
		case fields[0] == "mix" && len(ids) == 2:
			f.Set(func(s *tally.Snapshot) { s.Program = ids })
		case fields[0] == "ftb":
			f.Set(func(s *tally.Snapshot) { s.Black = !s.Black })
		default:
			fmt.Println("Comando non valido.\n" + help)
			continue
		}
		fmt.Println("OK")
	}
}
