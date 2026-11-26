package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Abnahme 1.6: Ein fälliger Job läuft im CGI-Modus nach der Antwort – im
// Prozess der Anfrage, ohne Hintergrundprozess. Belegt über den Kern-Job
// core.cleanup, der eine abgelaufene Session löscht.
func TestLazyJobRunsInCGIMode(t *testing.T) {
	if testing.Short() {
		t.Skip("baut ein Binary")
	}
	bin := filepath.Join(t.TempDir(), "styrixa")
	build := exec.Command("go", "build", "-trimpath", "-tags", buildTags(), "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	db, err := core.OpenDB(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mods, err := core.Modules()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := core.Migrate(ctx, db, mods); err != nil {
		t.Fatal(err)
	}
	if err := (core.SettingsStore{DB: db}).Set(ctx, core.KeyInstalledAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO core_users (id, email, password_hash) VALUES (1, 'a@example.at', 'x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO core_sessions VALUES ('abgelaufen', 1, ?, ?, ?, 'c')`, now-100, now-10, now-1); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(&cgi.Handler{Path: bin, Env: []string{"STYRIXA_DATA=" + dataDir}})
	t.Cleanup(srv.Close)
	get := func() {
		res, err := http.Get(srv.URL + "/")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("Startseite: %d", res.StatusCode)
		}
	}
	lastRun := func() (sql.NullInt64, sql.NullString) {
		var lr sql.NullInt64
		var le sql.NullString
		err := db.QueryRow(`SELECT last_run, last_error FROM core_jobs WHERE name = 'core.cleanup'`).Scan(&lr, &le)
		if err != nil && err != sql.ErrNoRows {
			t.Fatal(err)
		}
		return lr, le
	}

	// Pro Anfrage läuft höchstens ein fälliger Job; mit Modul-Jobs (z. B.
	// content.prune_revisions) ist core.cleanup also spätestens nach so vielen
	// Anfragen an der Reihe, wie es Jobs gibt.
	var lr sql.NullInt64
	var le sql.NullString
	for range core.RegisteredJobs() {
		get()
		if lr, le = lastRun(); lr.Valid {
			break
		}
	}
	if !lr.Valid || le.Valid {
		t.Fatalf("core.cleanup nach CGI-Anfrage: last_run=%v last_error=%v", lr, le)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM core_sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("abgelaufene Session nicht gelöscht – Job hat nicht gearbeitet")
	}

	for range core.RegisteredJobs() {
		get() // alle übrigen Jobs abarbeiten; core.cleanup ist nicht mehr fällig
	}
	if lr2, _ := lastRun(); lr2 != lr {
		t.Fatalf("Job erneut gelaufen: %v → %v", lr, lr2)
	}
}
