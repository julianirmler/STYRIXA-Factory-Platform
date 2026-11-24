// STYRIXA CMS – Einstiegspunkt.
//
// Betriebsarten (automatisch erkannt):
//   - Server: ./styrixa --addr 127.0.0.1:8080   (VPS/SSH-Deployment, systemd)
//   - CGI:    Apache startet styrixa.cgi pro Anfrage (Shared-Webspace/FTP-Deployment)
//
// Datenordner: STYRIXA_DATA (Standard ./data) – enthält styrixa.db.
// DB-Öffnung via modernc.org/sqlite, Migrationen und /install folgen im Kern-Schritt.
package main

import (
	"flag"
	"log"
	"net/http"
	"net/http/cgi"
	"os"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

func main() {
	addr := flag.String("addr", envOr("STYRIXA_ADDR", "127.0.0.1:8080"), "Listen-Adresse")
	flag.Parse()

	mods, err := core.Modules()
	if err != nil {
		log.Fatal(err)
	}
	dataDir := envOr("STYRIXA_DATA", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("ok"))
	})
	deps := core.Deps{} // DB folgt im Kern-Schritt
	for _, m := range mods {
		m.Mount(mux, deps)
	}

	if os.Getenv("GATEWAY_INTERFACE") != "" { // von Apache als CGI gestartet
		if err := cgi.Serve(mux); err != nil {
			log.Fatal(err)
		}
		return
	}

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("STYRIXA CMS lauscht auf %s (%d Module, Daten: %s)", *addr, len(mods), dataDir)
	log.Fatal(srv.ListenAndServe())
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
