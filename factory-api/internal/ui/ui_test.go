package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "dist")
	must(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><title>STYRIXA Factory</title>"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "assets", "x.js"), []byte("console.log(1)"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("geheim"), 0o644))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"release":"v0"}`)) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle("/", Handler(dir))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandler(t *testing.T) {
	srv := setup(t)
	tests := []struct {
		path, wantBody, wantCache string
		wantStatus                int
		ui                        bool
	}{
		{"/", "STYRIXA Factory", "no-cache", 200, true},
		{"/assets/x.js", "console.log(1)", "public, max-age=31536000, immutable", 200, true},
		{"/api/catalog", `{"release":"v0"}`, "", 200, false},
		{"/healthz", "ok", "", 200, false},
		{"/api/gibtsnicht", "", "", 404, false},
		{"/assets/", "", "", 404, false},
		{"/fehlt.js", "", "", 404, true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			res, err := http.Get(srv.URL + tt.path)
			must(t, err)
			defer res.Body.Close()
			body := new(strings.Builder)
			_, _ = io.Copy(body, res.Body)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("status %d, want %d", res.StatusCode, tt.wantStatus)
			}
			if tt.wantBody != "" && !strings.Contains(body.String(), tt.wantBody) {
				t.Errorf("body %q, want %q", body, tt.wantBody)
			}
			if tt.wantStatus == 200 && tt.ui {
				for k, v := range map[string]string{
					"Content-Security-Policy": CSP,
					"X-Content-Type-Options":  "nosniff",
					"Referrer-Policy":         "no-referrer",
					"Cache-Control":           tt.wantCache,
				} {
					if got := res.Header.Get(k); got != v {
						t.Errorf("%s = %q, want %q", k, got, v)
					}
				}
			}
			if !tt.ui && res.Header.Get("Content-Security-Policy") != "" {
				t.Error("API-Antwort trägt UI-Header")
			}
		})
	}
}

// Pfad-Ausbruch: roh gesendete Pfade, die der Client nicht normalisiert.
func TestNoPathEscape(t *testing.T) {
	h := Handler(filepath.Join(setupDir(t), "dist"))
	for _, p := range []string{"/../secret.txt", "/assets/../../secret.txt", "/..%2fsecret.txt", "/%2e%2e/secret.txt"} {
		req := httptest.NewRequest("GET", "/", nil)
		req.URL.Path = ""
		req.URL.RawPath = ""
		req.RequestURI = p
		u, err := req.URL.Parse(p)
		must(t, err)
		req.URL = u
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), "geheim") {
			t.Errorf("%s: Datei außerhalb des UI-Ordners ausgeliefert", p)
		}
	}
}

func setupDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "dist"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "dist", "index.html"), []byte("x"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "secret.txt"), []byte("geheim"), 0o644))
	return root
}
