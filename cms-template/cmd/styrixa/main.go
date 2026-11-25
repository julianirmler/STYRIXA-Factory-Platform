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
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"net/http/cgi"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

func main() {
	addr := flag.String("addr", envOr("STYRIXA_ADDR", "127.0.0.1:8080"), "Listen-Adresse")
	flag.Parse()
	cgiMode := os.Getenv("GATEWAY_INTERFACE") != "" // von Apache als CGI gestartet

	// Logs auf stderr. Im CGI-Modus landen sie im Apache-Error-Log jeder
	// Anfrage – dort nur Warnungen und Fehler.
	level := slog.LevelInfo
	if cgiMode {
		level = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	mods, err := core.Modules()
	if err != nil {
		fatal("Module", err)
	}
	dataDir := envOr("STYRIXA_DATA", "data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		fatal("Datenordner", err)
	}

	db, err := core.OpenDB(dataDir)
	if err != nil {
		fatal("Datenbank", err)
	}
	defer db.Close()
	if cgiMode {
		db.SetMaxOpenConns(1) // ein Prozess pro Anfrage – mehr Verbindungen bringen nichts
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = core.Migrate(ctx, db, mods)
	cancel()
	if err != nil {
		fatal("Migrationen", err)
	}

	if !cgiMode {
		settings := core.SettingsStore{DB: db}
		// Download-Variante ohne Factory-Deploy: Code-Datei gleich beim Start anlegen.
		// Nur den Pfad melden – der Code selbst ist ein Geheimnis.
		if ok, err := core.Installed(context.Background(), settings); err == nil && !ok {
			if path, err := core.EnsureInstallCode(dataDir); err == nil {
				slog.Info("Installation offen: /install aufrufen, Installations-Code steht in " + path)
			}
		}
	}

	handler, jobs, err := newHandler(db, dataDir, mods)
	if err != nil {
		fatal("Handler", err)
	}

	if cgiMode {
		serveCGI(handler, jobs, db, mods)
		return
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           core.AfterResponse(handler, lazyJob(jobs)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("STYRIXA CMS lauscht", "addr", *addr, "module", len(mods), "daten", dataDir)
	fatal("Server", srv.ListenAndServe())
}

// serveCGI beantwortet genau eine Anfrage und erledigt danach – ohne
// Hintergrundprozess – aufgeschobene Arbeit oder höchstens einen fälligen Job.
func serveCGI(handler http.Handler, jobs *core.Jobs, db *sql.DB, mods []core.Module) {
	// Hoster-Check der Factory ruft …/styrixa.cgi/healthz in einem Prüfordner auf:
	// dann ist nur PATH_INFO aussagekräftig, nicht der volle Pfad.
	healthOnly := os.Getenv("PATH_INFO") == "/healthz"
	if healthOnly {
		handler = core.SecurityHeaders(core.Health(db, mods))
	}
	after := &core.After{}
	err := cgi.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, after.Attach(r))
	}))
	if err != nil {
		fatal("CGI", err)
	}
	if healthOnly || os.Getenv("PATH_INFO") == core.PathCron {
		return
	}
	// Antwort abschließen: Apache sieht EOF und liefert sie aus. Danach schickt
	// Apache SIGTERM (nach 3 s SIGKILL) – ignorieren, damit die Arbeit im
	// Budget (core.AfterBudget) fertig wird und ihr Ergebnis speichern kann.
	signal.Ignore(syscall.SIGTERM)
	os.Stdout.Close()

	ctx, cancel := context.WithTimeout(context.Background(), core.AfterBudget)
	defer cancel()
	if after.Pending() {
		after.Run(ctx)
		return
	}
	lazyJob(jobs)(ctx)
}

// lazyJob führt höchstens einen fälligen Job aus (nach einer Antwort).
func lazyJob(jobs *core.Jobs) func(context.Context) {
	return func(ctx context.Context) {
		if _, err := jobs.RunDue(ctx, core.LazyBudget); err != nil {
			slog.Error("lazy-job", "err", err)
		}
	}
}

func fatal(what string, err error) {
	slog.Error(what, "err", err)
	os.Exit(1)
}

// newHandler baut den kompletten Handler – identisch für Server- und CGI-Modus.
// Arbeit nach der Antwort (core.AfterResponse bzw. serveCGI) hängt main darum.
func newHandler(db *sql.DB, dataDir string, mods []core.Module) (http.Handler, *core.Jobs, error) {
	settings := core.SettingsStore{DB: db}
	installer := &core.Installer{DB: db, DataDir: dataDir}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", core.Health(db, mods))
	installer.Mount(mux)
	auth := &core.Auth{DB: db, Settings: settings, Mail: core.SMTPMailer{Settings: settings}}
	auth.Mount(mux)
	admin, err := core.NewAdmin(db, settings, mods)
	if err != nil {
		return nil, nil, err
	}
	admin.Mount(mux)
	public, err := core.NewPublic(settings, mods)
	if err != nil {
		return nil, nil, err
	}
	public.Mount(mux) // Startseite + 404 für alles, was keine andere Route trifft
	deps := core.Deps{DB: db, Settings: settings, Admin: admin}
	for _, m := range mods {
		m.Mount(mux, deps)
	}
	jobs := &core.Jobs{Deps: deps}
	jobs.Mount(mux) // GET /cron mit Header X-Styrixa-Cron-Token
	// Von innen nach außen: Login + CSRF-Token für /admin/*, Cross-Origin-Schutz
	// für alle Nicht-GET-Anfragen (Anfragen ohne Origin/Sec-Fetch-Site wie der
	// Stripe-Webhook kommen durch), Umleitung auf /install vor der Einrichtung,
	// Sicherheits-Header für jede Antwort, 500-Seite statt Absturz bei Panics.
	h := installer.RequireInstalled(http.NewCrossOriginProtection().Handler(auth.Require(mux)))
	return public.Recover(core.SecurityHeaders(h)), jobs, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
