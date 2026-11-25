package main

import (
	"context"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Abnahme Session 1.5: Die Startseite ist im Server- und im CGI-Modus
// identisch. Server = newHandler hinter httptest.NewServer; CGI = das echte
// Binary, von net/http/cgi pro Anfrage als Prozess gestartet (wie Apache).
func TestPublicServerEqualsCGI(t *testing.T) {
	if testing.Short() {
		t.Skip("baut ein Binary")
	}
	bin := filepath.Join(t.TempDir(), "styrixa")
	build := exec.Command("go", "build", "-trimpath", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Ein installierter Datenordner für beide Modi.
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
	st := core.SettingsStore{DB: db}
	for k, v := range map[string]string{
		core.KeyInstalledAt: time.Now().UTC().Format(time.RFC3339),
		core.KeySiteTitle:   "Tischlerei Grünwald & Söhne",
		core.KeySiteRegion:  "de-AT",
	} {
		if err := st.Set(ctx, k, v); err != nil {
			t.Fatal(err)
		}
	}

	h, err := newHandler(db, dataDir, mods)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	cgiSrv := httptest.NewServer(&cgi.Handler{Path: bin, Env: []string{"STYRIXA_DATA=" + dataDir}})
	t.Cleanup(cgiSrv.Close)

	headers := []string{"Content-Type", "Content-Length", "Cache-Control", "ETag",
		"X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy", "Location"}
	fetch := func(base, method, path, inm string) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		res, err := http.DefaultTransport.RoundTrip(req) // keine Weiterleitungen folgen
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	first, home := fetch(server.URL, "GET", "/", "")
	etag := first.Header.Get("ETag")
	if first.StatusCode != 200 || etag == "" || !strings.Contains(home, "Tischlerei Grünwald &amp; Söhne") ||
		!strings.Contains(home, `<html lang="de-AT">`) {
		t.Fatalf("Startseite im Server-Modus: %d, ETag %q\n%s", first.StatusCode, etag, home)
	}

	for _, tc := range []struct {
		name, method, path, inm string
		status                  int
	}{
		{"Startseite", "GET", "/", "", 200},
		{"Startseite HEAD", "HEAD", "/", "", 200},
		{"Startseite 304", "GET", "/", etag, 304},
		{"404", "GET", "/gibt/es/nicht", "", 404},
		{"Admin ohne Login", "GET", "/admin", "", 303},
		{"healthz", "GET", "/healthz", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sres, sbody := fetch(server.URL, tc.method, tc.path, tc.inm)
			cres, cbody := fetch(cgiSrv.URL, tc.method, tc.path, tc.inm)
			if sres.StatusCode != tc.status || cres.StatusCode != tc.status {
				t.Fatalf("Status Server %d, CGI %d, erwartet %d\nCGI-Körper: %s", sres.StatusCode, cres.StatusCode, tc.status, cbody)
			}
			for _, k := range headers {
				if s, c := sres.Header.Get(k), cres.Header.Get(k); s != c {
					t.Errorf("%s: Server %q, CGI %q", k, s, c)
				}
			}
			if sbody != cbody {
				t.Errorf("Körper verschieden\nServer:\n%s\nCGI:\n%s", sbody, cbody)
			}
		})
	}
}
