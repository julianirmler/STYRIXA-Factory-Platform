//go:build smoke

// Smoke-Test (ADR-010, Session 3.5): der ganze Weg mit dem echten Binary –
// bauen, starten, /install, Login, Seite anlegen, Blöcke speichern,
// veröffentlichen, öffentlich abrufen. Ein Test statt Browser-Tests.
//
//	go test -tags "smoke mod_content mod_dashboard" -run '^TestSmoke$' ./cmd/styrixa
//
// Der Tag „smoke“ hält den Test aus `make test` heraus (er baut ein Binary und
// startet einen Prozess); das Binary selbst wird mit den Pflicht-Tags gebaut.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Pflichtmodule laut modules.json („required“).
const smokeTags = "mod_content mod_dashboard"

func TestSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	dir := t.TempDir()

	// 1. Binary bauen – wie ein Release: CGO aus, Pflicht-Tags.
	bin := filepath.Join(dir, "styrixa")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-tags", smokeTags, "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("Build: %v\n%s", err, out)
	}

	// 2. Auf Zufallsport starten (Server-Modus, eigener Datenordner).
	addr := freeAddr(t)
	dataDir := filepath.Join(dir, "data")
	logs := &lockedBuffer{}
	srv := exec.Command(bin, "--addr", addr)
	srv.Env = append(withoutEnv(os.Environ(), "GATEWAY_INTERFACE", "STYRIXA_ADDR"), "STYRIXA_DATA="+dataDir)
	srv.Stdout, srv.Stderr = logs, logs
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	exited := make(chan struct{}) // geschlossen, sobald der Prozess beendet ist
	go func() { _ = srv.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		<-exited
		if t.Failed() {
			t.Logf("Server-Log:\n%s", logs.String())
		}
	})
	base := "http://" + addr
	c := newSmokeClient(t, base)
	waitHealthy(ctx, t, c, exited)

	// 3. /install mit dem Code aus dem Datenordner.
	if r := c.get("/"); r.code != http.StatusSeeOther || r.location != "/install" {
		t.Fatalf("vor der Installation: / → %d %q, erwartet 303 → /install", r.code, r.location)
	}
	if r := c.get("/install"); r.code != http.StatusOK {
		t.Fatalf("GET /install: %d", r.code)
	}
	code, err := os.ReadFile(filepath.Join(dataDir, "install-code"))
	if err != nil {
		t.Fatalf("Installations-Code: %v", err)
	}
	const email, password = "smoke@example.at", "smoke-test-passwort-123"
	r := c.form("/install", "code", strings.TrimSpace(string(code)), "title", "Smoke-Site", "region", "de-AT",
		"email", email, "password", password, "password2", password)
	if r.code != http.StatusOK {
		t.Fatalf("POST /install: %d\n%s", r.code, r.body)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "install-code")); !os.IsNotExist(err) {
		t.Fatalf("Code-Datei nach der Installation noch vorhanden (err=%v)", err)
	}

	// 4. Login.
	if r := c.form("/admin/login", "email", email, "password", password); r.code != http.StatusSeeOther || r.location != "/admin" {
		t.Fatalf("Login: %d %q\n%s", r.code, r.location, r.body)
	}
	r = c.get("/admin/content")
	if r.code != http.StatusOK {
		t.Fatalf("GET /admin/content: %d", r.code)
	}
	m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(r.body)
	if m == nil {
		t.Fatal("kein CSRF-Token im Admin-Layout")
	}
	c.csrf = m[1]

	// 5. Seite anlegen, Blöcke speichern, veröffentlichen.
	r = c.form("/admin/content/pages", "title", "Über uns")
	var id int64
	if r.code != http.StatusSeeOther {
		t.Fatalf("Seite anlegen: %d\n%s", r.code, r.body)
	}
	if _, err := fmt.Sscanf(r.location, "/admin/content/pages/%d", &id); err != nil {
		t.Fatalf("Seite anlegen: Location %q", r.location)
	}
	pageURL := fmt.Sprintf("/admin/content/pages/%d", id)
	body := `{"time":1,"version":"2.31.0","blocks":[` +
		`{"id":"h","type":"header","data":{"text":"Wer wir sind","level":2}},` +
		`{"id":"p","type":"paragraph","data":{"text":"Ein <b>Familienbetrieb</b> aus der Steiermark.<script>alert(1)</script>"}},` +
		`{"id":"l","type":"list","data":{"style":"unordered","items":["Beratung","Umsetzung"]}}]}`
	draft, _ := json.Marshal(map[string]any{"title": "Über uns", "format": "blocks", "body": json.RawMessage(body)})
	if r := c.do("POST", pageURL+"/draft", "application/json", string(draft)); r.code != http.StatusOK {
		t.Fatalf("Entwurf speichern: %d\n%s", r.code, r.body)
	}

	public := newSmokeClient(t, base) // Besucher ohne Session
	const path = "/ueber-uns"
	if r := public.get(path); r.code != http.StatusNotFound {
		t.Fatalf("Entwurf öffentlich: %s → %d, erwartet 404", path, r.code)
	}
	if r := c.form(pageURL + "/publish"); r.code != http.StatusSeeOther {
		t.Fatalf("Veröffentlichen: %d\n%s", r.code, r.body)
	}

	// 6. Öffentlich abrufen und Inhalt prüfen.
	r = public.get(path)
	if r.code != http.StatusOK {
		t.Fatalf("öffentlich %s: %d\n%s", path, r.code, r.body)
	}
	for _, want := range []string{"Über uns", `<h2 id="wer-wir-sind">Wer wir sind</h2>`,
		"<b>Familienbetrieb</b>", "&lt;script&gt;", "<li>Beratung</li>"} {
		if !strings.Contains(r.body, want) {
			t.Errorf("öffentliche Seite enthält %q nicht:\n%s", want, r.body)
		}
	}
	if strings.Contains(r.body, "<script>alert(1)") {
		t.Error("eingeschleustes <script> wird ausgeliefert")
	}
	if r.setCookie {
		t.Error("öffentliche Seite setzt ein Cookie")
	}

	// 7. Nach der Installation ist /install verschwunden.
	if r := public.get("/install"); r.code != http.StatusNotFound {
		t.Fatalf("GET /install nach der Installation: %d, erwartet 404", r.code)
	}
}

// ---- Hilfen ----

type smokeClient struct {
	t    *testing.T
	base string
	hc   *http.Client
	csrf string
}

type smokeResp struct {
	code      int
	location  string
	body      string
	setCookie bool
}

func newSmokeClient(t *testing.T, base string) *smokeClient {
	jar, _ := cookiejar.New(nil)
	return &smokeClient{t: t, base: base, hc: &http.Client{
		Jar:     jar,
		Timeout: 10 * time.Second,
		// Weiterleitungen selbst prüfen (303 nach Formularen, 301 bei Slugs).
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *smokeClient) do(method, path, ctype, body string) smokeResp {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	// Ein Browser-User-Agent: Bots (auch Go-Clients) zählt das Dashboard nicht.
	req.Header.Set("User-Agent", "Mozilla/5.0 (STYRIXA Smoke-Test)")
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.csrf != "" && method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return smokeResp{code: res.StatusCode, location: res.Header.Get("Location"), body: string(b),
		setCookie: len(res.Header.Values("Set-Cookie")) > 0}
}

func (c *smokeClient) get(path string) smokeResp { c.t.Helper(); return c.do("GET", path, "", "") }

func (c *smokeClient) form(path string, kv ...string) smokeResp {
	c.t.Helper()
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return c.do("POST", path, "application/x-www-form-urlencoded", v.Encode())
}

// freeAddr liefert einen gerade freien Port auf 127.0.0.1.
func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// waitHealthy wartet, bis /healthz antwortet – oder der Prozess endet.
func waitHealthy(ctx context.Context, t *testing.T, c *smokeClient, exited <-chan struct{}) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		res, err := c.hc.Get(c.base + "/healthz")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-exited:
			t.Fatal("Server beendet sich vor /healthz")
		case <-ctx.Done():
			t.Fatalf("Server nicht bereit: %v", err)
		case <-tick.C:
		}
	}
}

func withoutEnv(env []string, keys ...string) []string {
	out := env[:0:0]
outer:
	for _, kv := range env {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// lockedBuffer sammelt Stdout/Stderr des Servers (zwei Schreiber).
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
