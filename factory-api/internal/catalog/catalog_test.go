package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- Fake-GitHub ----

type fakeGitHub struct {
	srv  *httptest.Server
	mu   sync.Mutex
	tag  string
	file map[string][]byte
	hits atomic.Int64
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{tag: "v9.9.9", file: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		type asset struct {
			Name string `json:"name"`
			Size int    `json:"size"`
			URL  string `json:"browser_download_url"`
		}
		var as []asset
		for n, b := range f.file {
			as = append(as, asset{n, len(b), f.srv.URL + "/dl/" + n})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "assets": as})
	})
	mux.HandleFunc("GET /dl/{name}", func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		b, ok := f.file[r.PathValue("name")]
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) set(name string, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b == nil {
		delete(f.file, name)
	} else {
		f.file[name] = b
	}
}

// ---- Test-Release ----

var testManifest = []byte(`{
  "modules": [
    { "id": "dashboard", "nr": "01", "name": "Dashboard", "required": true },
    { "id": "content",   "nr": "02", "name": "Content Management", "required": true },
    { "id": "media",     "nr": "03", "name": "Media Management" },
    { "id": "seo",       "nr": "04", "name": "SEO & GEO Management" },
    { "id": "design",    "nr": "06", "name": "Design Management" },
    { "id": "themes",    "nr": "07", "name": "Theme Management", "requires": ["design"] },
    { "id": "shop",      "nr": "08", "name": "Shop Management",  "requires": ["media"] }
  ],
  "targets": ["linux/amd64", "windows/amd64"]
}`)

type tarEntry struct {
	name string
	typ  byte
	body []byte
	link string
	size int64 // 0 = len(body)
}

func tgz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		size := e.size
		if size == 0 && typ == tar.TypeReg {
			size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: typ, Size: size, Mode: 0o755, Linkname: e.link}); err != nil {
			t.Fatal(err)
		}
		tw.Write(e.body)
	}
	tw.Close() // Fehler bei absichtlich falscher Größe egal
	zw.Close()
	return buf.Bytes()
}

func binary(id string) []byte { return []byte("#!/bin/sh\necho styrixa " + id + "\n") }

// packages des Test-Releases (bewusst nicht alle Kombinationen)
var testPackages = []string{"core_linux_amd64", "media_linux_amd64", "media-shop_linux_amd64", "design-themes_linux_amd64", "core_windows_amd64"}

type releaseOpts struct {
	unsignedManifest bool // modules.json nicht in SHA256SUMS
}

// publish baut einen gültig signierten Release in f und liefert den öffentlichen Schlüssel.
func publish(t *testing.T, f *fakeGitHub, priv ed25519.PrivateKey, o releaseOpts) {
	t.Helper()
	var sums strings.Builder
	add := func(name string, b []byte) {
		f.set(name, b)
		h := sha256.Sum256(b)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	for _, id := range testPackages {
		name := "styrixa"
		if strings.HasSuffix(id, "windows_amd64") {
			name += ".exe"
		}
		add("styrixa_"+id+".tar.gz", tgz(t, tarEntry{name: name, body: binary(id)}))
	}
	if o.unsignedManifest {
		f.set("modules.json", testManifest)
	} else {
		add("modules.json", testManifest)
	}
	s := []byte(sums.String())
	f.set("SHA256SUMS", s)
	f.set("SHA256SUMS.sig", []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, s))+"\n"))
}

func newKey(t *testing.T) (string, ed25519.PrivateKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), priv
}

// setup: Fake-GitHub mit gültigem Release + geladener Katalog.
func setup(t *testing.T) (*Catalog, *fakeGitHub, ed25519.PrivateKey) {
	t.Helper()
	pub, priv := newKey(t)
	f := newFakeGitHub(t)
	publish(t, f, priv, releaseOpts{})
	c := &Catalog{Repo: "o/r", APIBase: f.srv.URL, PublicKey: pub, CacheDir: t.TempDir(), HTTP: f.srv.Client()}
	if err := c.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c, f, priv
}

// ---- Tests ----

func TestPublicKeyMatchesCMS(t *testing.T) {
	b, err := os.ReadFile("../../../cms-template/internal/core/release.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const ReleasePublicKey = "([^"]+)"`).FindSubmatch(b)
	if m == nil || string(m[1]) != ReleasePublicKey {
		t.Fatalf("ReleasePublicKey weicht vom CMS ab: %q", m)
	}
}

func TestSelect(t *testing.T) {
	c, _, _ := setup(t)
	tests := []struct {
		mods    []string
		target  string
		wantID  string
		wantErr string
	}{
		{nil, "linux/amd64", "core_linux_amd64", ""},
		{[]string{"shop", "media"}, "linux/amd64", "media-shop_linux_amd64", ""},
		{[]string{"dashboard", "content", "media"}, "linux/amd64", "media_linux_amd64", ""}, // Pflichtmodule dürfen mitkommen
		{[]string{"themes", "design"}, "linux/amd64", "design-themes_linux_amd64", ""},
		{nil, "windows/amd64", "core_windows_amd64", ""},
		{[]string{"shop"}, "linux/amd64", "", "benötigt „Media Management“"},
		{[]string{"themes"}, "linux/amd64", "", "benötigt „Design Management“"},
		{[]string{"blog"}, "linux/amd64", "", "Unbekanntes Modul"},
		{nil, "linux/mips", "", "wird nicht angeboten"},
		{nil, "", "", "wird nicht angeboten"},
		{[]string{"seo"}, "linux/amd64", "", "kein Paket"}, // gültig, aber nicht im Release
	}
	for _, tt := range tests {
		p, err := c.Select(context.Background(), tt.mods, tt.target)
		if tt.wantErr != "" {
			var sel *SelectionError
			if !errors.As(err, &sel) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%v %s: Fehler %q erwartet, bekommen %v", tt.mods, tt.target, tt.wantErr, err)
			}
			continue
		}
		if err != nil || p.ID != tt.wantID {
			t.Errorf("%v %s: %q %v, erwartet %q", tt.mods, tt.target, p.ID, err, tt.wantID)
		}
	}
	p, _ := c.Select(context.Background(), []string{"shop", "media"}, "linux/amd64")
	if !slices.Equal(p.Modules, []string{"content", "dashboard", "media", "shop"}) || p.Release != "v9.9.9" || p.GOOS != "linux" || len(p.SHA256) != 64 {
		t.Errorf("Paketdaten: %+v", p)
	}
}

// Abnahme: Auswahl in < 1 s – und ohne Netzwerkzugriff.
func TestSelectFast(t *testing.T) {
	c, f, _ := setup(t)
	mux := http.NewServeMux()
	c.Routes(mux)
	before := f.hits.Load()
	start := time.Now()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/packages/select?modules=media,shop&target=linux/amd64", nil))
	d := time.Since(start)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if d > time.Second {
		t.Fatalf("Auswahl dauerte %v", d)
	}
	if f.hits.Load() != before {
		t.Error("Auswahl greift auf GitHub zu")
	}
	var got struct {
		ID, DownloadURL string
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ID != "media-shop_linux_amd64" || got.DownloadURL != "/api/packages/media-shop_linux_amd64/download" {
		t.Errorf("Antwort: %s", rec.Body)
	}
	t.Logf("Auswahl: %v", d)
}

func TestHandlers(t *testing.T) {
	c, _, _ := setup(t)
	mux := http.NewServeMux()
	c.Routes(mux)
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", url, nil))
		return rec
	}
	tests := []struct {
		url  string
		code int
	}{
		{"/api/catalog", 200},
		{"/api/packages/select?target=linux/amd64", 200},
		{"/api/packages/select?modules=shop&target=linux/amd64", 422},
		{"/api/packages/select?modules=media", 422},
		{"/api/packages/media_linux_amd64/download", 200},
		{"/api/packages/seo_linux_amd64/download", 404},
		{"/api/packages/..%2Fetc_linux_amd64/download", 404},
	}
	for _, tt := range tests {
		if rec := get(tt.url); rec.Code != tt.code {
			t.Errorf("%s: %d, erwartet %d (%s)", tt.url, rec.Code, tt.code, rec.Body)
		}
	}
	rec := get("/api/catalog")
	var cat struct {
		Release string
		Modules []Module
		Targets []string
	}
	json.Unmarshal(rec.Body.Bytes(), &cat)
	if cat.Release != "v9.9.9" || len(cat.Modules) != 7 || len(cat.Targets) != 2 {
		t.Errorf("Katalog: %s", rec.Body)
	}
}

func TestDownloadAndArtifact(t *testing.T) {
	c, f, _ := setup(t)
	mux := http.NewServeMux()
	c.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/packages/media-shop_linux_amd64/download", nil))
	if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), f.file["styrixa_media-shop_linux_amd64.tar.gz"]) {
		t.Fatalf("Download: %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="styrixa_media-shop_linux_amd64.tar.gz"` {
		t.Errorf("Content-Disposition: %s", cd)
	}
	// zweiter Abruf aus dem Cache, ohne GitHub
	before := f.hits.Load()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/packages/media-shop_linux_amd64/download", nil))
	if rec.Code != 200 || f.hits.Load() != before {
		t.Errorf("Cache: %d, %d Zugriffe", rec.Code, f.hits.Load()-before)
	}

	for _, id := range []string{"media-shop_linux_amd64", "core_windows_amd64"} {
		art, err := c.Artifact(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(art.BinaryPath)
		st, _ := os.Stat(art.BinaryPath)
		if !bytes.Equal(b, binary(id)) || st.Mode().Perm() != 0o755 {
			t.Errorf("%s: Binary %q, Modus %v", id, b, st.Mode())
		}
		if id == "media-shop_linux_amd64" && (art.GOOS != "linux" || art.GOARCH != "amd64" || !slices.Contains(art.Modules, "shop")) {
			t.Errorf("Artifact: %+v", art)
		}
		if id == "core_windows_amd64" && filepath.Base(art.BinaryPath) != "styrixa.exe" {
			t.Errorf("Windows-Binary: %s", art.BinaryPath)
		}
	}
	if _, err := c.Artifact(context.Background(), "seo_linux_amd64"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unbekanntes Paket: %v", err)
	}
}

// Abnahme: manipuliertes Paket wird abgelehnt.
func TestTamperedPackageRejected(t *testing.T) {
	c, f, _ := setup(t)
	mux := http.NewServeMux()
	c.Routes(mux)
	const id = "media_linux_amd64"
	const file = "styrixa_" + id + ".tar.gz"
	good := f.file[file]
	evil := tgz(t, tarEntry{name: "styrixa", body: []byte("#!/bin/sh\nrm -rf ~\n")})
	f.set(file, evil)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/packages/"+id+"/download", nil))
	if rec.Code != http.StatusBadGateway || bytes.Contains(rec.Body.Bytes(), []byte("rm -rf")) {
		t.Fatalf("Download manipuliert: %d %q", rec.Code, rec.Body)
	}
	if _, err := c.Artifact(context.Background(), id); !errors.Is(err, ErrTampered) {
		t.Fatalf("Artifact manipuliert: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(c.CacheDir, "*")); len(left) != 0 {
		t.Fatalf("manipuliertes Paket im Cache: %v", left)
	}

	// manipulierte Datei im Cache wird verworfen und neu geprüft geladen
	f.set(file, good)
	p, _ := c.Lookup(context.Background(), id)
	path, err := c.Open(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, evil, 0o600)
	f.set(file, evil) // Quelle ebenfalls manipuliert → ablehnen
	if _, err := c.Artifact(context.Background(), id); !errors.Is(err, ErrTampered) {
		t.Fatalf("Cache + Quelle manipuliert: %v", err)
	}
	f.set(file, good)
	art, err := c.Artifact(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(art.BinaryPath); !bytes.Equal(b, binary(id)) {
		t.Fatalf("Binary nach Neuladen: %q", b)
	}
}

func TestLoadRejectsInvalidRelease(t *testing.T) {
	_, otherKey := newKey(t)
	tests := []struct {
		name    string
		mutate  func(*fakeGitHub, ed25519.PrivateKey)
		wantErr string
	}{
		{"Prüfsumme nach Signatur geändert", func(f *fakeGitHub, _ ed25519.PrivateKey) {
			s := f.file["SHA256SUMS"]
			s = bytes.Replace(s, s[:4], []byte("0000"), 1)
			f.set("SHA256SUMS", s)
		}, "Signatur ungültig"},
		{"Signatur fehlt", func(f *fakeGitHub, _ ed25519.PrivateKey) { f.set("SHA256SUMS.sig", nil) }, "SHA256SUMS.sig fehlt"},
		{"Signatur kaputt", func(f *fakeGitHub, _ ed25519.PrivateKey) { f.set("SHA256SUMS.sig", []byte("kaputt")) }, "beschädigt"},
		{"fremder Schlüssel", func(f *fakeGitHub, _ ed25519.PrivateKey) { publish(t, f, otherKey, releaseOpts{}) }, "Signatur ungültig"},
		{"SHA256SUMS fehlt", func(f *fakeGitHub, _ ed25519.PrivateKey) { f.set("SHA256SUMS", nil) }, "SHA256SUMS fehlt"},
		{"modules.json manipuliert", func(f *fakeGitHub, _ ed25519.PrivateKey) {
			f.set("modules.json", bytes.Replace(testManifest, []byte(`"name": "Shop Management",  "requires": ["media"]`), []byte(`"name": "Shop Management"`), 1))
		}, "signierten Prüfsumme"},
		{"modules.json nicht signiert", func(f *fakeGitHub, priv ed25519.PrivateKey) { publish(t, f, priv, releaseOpts{unsignedManifest: true}) }, "modules.json nicht signiert"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, f, priv := setup(t)
			f.mu.Lock()
			f.tag = "v10.0.0"
			f.mu.Unlock()
			tt.mutate(f, priv)
			err := c.Load(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Fehler %q erwartet, bekommen %v", tt.wantErr, err)
			}
			// bisheriger gültiger Stand bleibt
			p, err := c.Select(context.Background(), []string{"media"}, "linux/amd64")
			if err != nil || p.Release != "v9.9.9" {
				t.Fatalf("alter Stand verloren: %+v %v", p, err)
			}
		})
	}
}

func TestUnavailable(t *testing.T) {
	pub, _ := newKey(t)
	f := newFakeGitHub(t) // leeres Release
	c := &Catalog{Repo: "o/r", APIBase: f.srv.URL, PublicKey: pub, CacheDir: t.TempDir(), HTTP: f.srv.Client()}
	mux := http.NewServeMux()
	c.Routes(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/catalog", nil))
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "SHA256SUMS") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	// kein erneuter GitHub-Zugriff bei jeder Anfrage
	before := f.hits.Load()
	c.Select(context.Background(), nil, "linux/amd64")
	if f.hits.Load() != before {
		t.Error("Katalog lädt ohne Pause neu")
	}
}

func TestExtractBinaryRejects(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
	}{
		{"Symlink", []tarEntry{{name: "styrixa", typ: tar.TypeSymlink, link: "/bin/sh"}}},
		{"Pfad", []tarEntry{{name: "../styrixa", body: []byte("x")}}},
		{"Unterordner", []tarEntry{{name: "bin/styrixa", body: []byte("x")}}},
		{"Zusatzdatei", []tarEntry{{name: "styrixa", body: []byte("x")}, {name: ".htaccess", body: []byte("y")}}},
		{"leer", nil},
		{"zu groß", []tarEntry{{name: "styrixa", size: maxBinary + 1}}},
		{"abgeschnitten", []tarEntry{{name: "styrixa", body: []byte("x"), size: 100}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "p.tar.gz")
			os.WriteFile(src, tgz(t, tt.entries...), 0o600)
			dst := filepath.Join(dir, "styrixa")
			if err := extractBinary(src, "styrixa", dst); err == nil {
				t.Fatal("Archiv angenommen")
			}
			if _, err := os.Stat(dst); err == nil {
				t.Fatal("Binary trotz Fehler angelegt")
			}
		})
	}
}

// Nach Ablauf der TTL gilt der alte Stand weiter, bis der neue geprüft ist.
func TestRefreshAfterTTL(t *testing.T) {
	c, f, priv := setup(t)
	c.TTL = time.Nanosecond
	f.mu.Lock()
	f.tag = "v10.0.0"
	f.mu.Unlock()
	publish(t, f, priv, releaseOpts{})
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := c.Select(context.Background(), nil, "linux/amd64")
		if err != nil {
			t.Fatal(err)
		}
		if p.Release == "v10.0.0" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("neuer Release nicht übernommen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.loadMu.Lock() // laufende Aktualisierung abwarten
	c.loadMu.Unlock()
}
