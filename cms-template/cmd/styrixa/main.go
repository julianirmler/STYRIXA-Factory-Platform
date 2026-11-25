// STYRIXA CMS – Einstiegspunkt.
//
// Betriebsarten (automatisch erkannt):
//   - Server: ./styrixa --addr 127.0.0.1:8080   (VPS/SSH-Deployment, systemd)
//   - CGI:    Apache startet styrixa.cgi pro Anfrage (Shared-Webspace/FTP-Deployment)
//
// Datenordner: STYRIXA_DATA (Standard ./data) – enthält styrixa.db (SQLite, WAL).
// Migrationen laufen bei jedem Start, im CGI-Modus also bei jeder Anfrage.
// Weitere Umgebungsvariablen: nur STYRIXA_ADDR. Alles andere (z. B. Stripe-Schlüssel,
// SMTP-Zugang für den Passwort-Reset) steht in den Settings (core_settings).
package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
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
	cgiMode := os.Getenv("GATEWAY_INTERFACE") != "" // von Apache als CGI gestartet

	db, err := core.OpenDB(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if cgiMode {
		db.SetMaxOpenConns(1) // ein Prozess pro Anfrage – mehr Verbindungen bringen nichts
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = core.Migrate(ctx, db, mods)
	cancel()
	if err != nil {
		log.Fatal(err)
	}

	settings := core.SettingsStore{DB: db}
	installer := &core.Installer{DB: db, DataDir: dataDir}
	if !cgiMode {
		// Download-Variante ohne Factory-Deploy: Code-Datei gleich beim Start anlegen.
		// Nur den Pfad melden – der Code selbst ist ein Geheimnis.
		if ok, err := core.Installed(context.Background(), settings); err == nil && !ok {
			if path, err := core.EnsureInstallCode(dataDir); err == nil {
				slog.Info("Installation offen: /install aufrufen, Installations-Code steht in " + path)
			}
		}
	}

	mux := http.NewServeMux()
	health := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("ok"))
	}
	mux.HandleFunc("GET /healthz", health)
	installer.Mount(mux)
	auth := &core.Auth{DB: db, Settings: settings, Mail: core.SMTPMailer{Settings: settings}}
	auth.Mount(mux)
	admin, err := core.NewAdmin(db, settings, mods)
	if err != nil {
		log.Fatal(err)
	}
	admin.Mount(mux)
	deps := core.Deps{DB: db, Settings: settings, Admin: admin}
	for _, m := range mods {
		m.Mount(mux, deps)
	}
	// Von innen nach außen: Login + CSRF-Token für /admin/*, Cross-Origin-Schutz
	// für alle Nicht-GET-Anfragen (Anfragen ohne Origin/Sec-Fetch-Site wie der
	// Stripe-Webhook kommen durch), Umleitung auf /install vor der Einrichtung.
	handler := installer.RequireInstalled(http.NewCrossOriginProtection().Handler(auth.Require(mux)))

	if cgiMode {
		// Hoster-Check der Factory ruft …/styrixa.cgi/healthz in einem Prüfordner auf:
		// dann ist nur PATH_INFO aussagekräftig, nicht der volle Pfad.
		h := handler
		if os.Getenv("PATH_INFO") == "/healthz" {
			h = http.HandlerFunc(health)
		}
		if err := cgi.Serve(h); err != nil {
			log.Fatal(err)
		}
		return
	}

	srv := &http.Server{Addr: *addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("STYRIXA CMS lauscht auf %s (%d Module, Daten: %s)", *addr, len(mods), dataDir)
	log.Fatal(srv.ListenAndServe())
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
