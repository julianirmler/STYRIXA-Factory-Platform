package core

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// themeModule ist ein Modul, das ein (Teil-)Theme liefert.
type themeModule struct {
	id    string
	files fstest.MapFS
}

func (m themeModule) ID() string               { return m.id }
func (themeModule) Requires() []string         { return nil }
func (themeModule) Migrations() fs.FS          { return nil }
func (themeModule) Mount(*http.ServeMux, Deps) {}
func (themeModule) AdminNav() []NavItem        { return nil }
func (m themeModule) PublicTemplates() fs.FS   { return m.files }
func file(s string) *fstest.MapFile            { return &fstest.MapFile{Data: []byte(s)} }
func get(path string, hdr ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return r
}

type fakeSettings map[string]string

func (f fakeSettings) Get(_ context.Context, k string) (string, error) { return f[k], nil }
func (f fakeSettings) Set(_ context.Context, k, v string) error        { f[k] = v; return nil }
func (f fakeSettings) GetAll(context.Context) (map[string]string, error) {
	return f, nil
}

func newTestPublic(t *testing.T, mods ...Module) (*Public, http.Handler) {
	t.Helper()
	p, err := NewPublic(fakeSettings{KeySiteTitle: "Bäckerei <Huber>", KeySiteRegion: "de-CH"}, mods)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	p.Mount(mux)
	mux.HandleFunc("GET /kaputt", func(http.ResponseWriter, *http.Request) { panic("geheimes Detail: SELECT * FROM core_users") })
	return p, p.Recover(SecurityHeaders(mux))
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPublicPages(t *testing.T) {
	_, h := newTestPublic(t)
	for _, tc := range []struct {
		path, cache string
		status      int
		want        []string
		etag        bool
	}{
		{"/", "no-cache", 200, []string{`<html lang="de-CH">`, "<h1>Bäckerei &lt;Huber&gt;</h1>"}, true},
		{"/gibt/es/nicht", "no-cache", 404, []string{"Seite nicht gefunden – Bäckerei &lt;Huber&gt;"}, false},
		{"/kaputt", "no-store", 500, []string{"Interner Fehler"}, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := serve(h, get(tc.path))
			if w.Code != tc.status {
				t.Fatalf("Status %d, erwartet %d", w.Code, tc.status)
			}
			body := w.Body.String()
			for _, s := range tc.want {
				if !strings.Contains(body, s) {
					t.Errorf("Körper enthält %q nicht:\n%s", s, body)
				}
			}
			if strings.Contains(body, "geheim") || strings.Contains(body, "SELECT") {
				t.Errorf("Antwort verrät Details:\n%s", body)
			}
			hd := w.Header()
			if got := hd.Get("Cache-Control"); got != tc.cache {
				t.Errorf("Cache-Control %q, erwartet %q", got, tc.cache)
			}
			if got := hd.Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type %q", got)
			}
			if (hd.Get("ETag") != "") != tc.etag {
				t.Errorf("ETag %q, erwartet vorhanden=%v", hd.Get("ETag"), tc.etag)
			}
			if hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Referrer-Policy") != "strict-origin-when-cross-origin" {
				t.Errorf("Sicherheits-Header fehlen: %v", hd)
			}
			if hd.Get("Content-Security-Policy") != "" {
				t.Errorf("öffentliche Seite mit Admin-CSP: %q", hd.Get("Content-Security-Policy"))
			}
		})
	}
}

func TestPublicETag(t *testing.T) {
	_, h := newTestPublic(t)
	etag := serve(h, get("/")).Header().Get("ETag")
	if len(etag) != 18 || etag[0] != '"' { // "<16 Hex>"
		t.Fatalf("ETag %q", etag)
	}
	for _, tc := range []struct {
		name, method, inm string
		want              int
	}{
		{"gleich", "GET", etag, 304},
		{"schwach", "GET", "W/" + etag, 304},
		{"Liste", "GET", `"abc", ` + etag, 304},
		{"Stern", "GET", "*", 304},
		{"HEAD", "HEAD", etag, 304},
		{"anderes", "GET", `"abc"`, 200},
		{"ohne", "GET", "", 200},
		{"ohne Anführungszeichen", "GET", strings.Trim(etag, `"`), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/", nil)
			if tc.inm != "" {
				r.Header.Set("If-None-Match", tc.inm)
			}
			w := serve(h, r)
			if w.Code != tc.want {
				t.Fatalf("Status %d, erwartet %d", w.Code, tc.want)
			}
			if w.Code == 304 && w.Body.Len() != 0 {
				t.Errorf("304 mit Körper")
			}
			if w.Header().Get("ETag") != etag || w.Header().Get("Cache-Control") != "no-cache" {
				t.Errorf("Header %v", w.Header())
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Errorf("HEAD mit Körper")
			}
		})
	}
}

func TestPublicThemeFallback(t *testing.T) {
	theme := themeModule{id: "themes", files: fstest.MapFS{
		"home.html": file(`<p data-theme="home">{{.SiteTitle}}</p>`),
	}}
	_, h := newTestPublic(t, theme)
	home := serve(h, get("/")).Body.String()
	if !strings.Contains(home, `data-theme="home"`) || !strings.Contains(home, `<html lang="de-CH">`) {
		t.Errorf("Theme-Startseite im Kern-Layout erwartet:\n%s", home)
	}
	if nf := serve(h, get("/x")).Body.String(); !strings.Contains(nf, "<h1>Seite nicht gefunden</h1>") {
		t.Errorf("404 aus dem Kern erwartet:\n%s", nf)
	}

	theme.files["layout.html"] = file(`<body data-theme="layout">{{template "content" .}}</body>`)
	_, h = newTestPublic(t, theme)
	if got := serve(h, get("/x")).Body.String(); !strings.Contains(got, `data-theme="layout"`) || !strings.Contains(got, "Seite nicht gefunden") {
		t.Errorf("Kern-404 im Theme-Layout erwartet:\n%s", got)
	}
}

func TestPublicThemeErrors(t *testing.T) {
	a := themeModule{id: "a", files: fstest.MapFS{}}
	b := themeModule{id: "b", files: fstest.MapFS{}}
	if _, err := NewPublic(nil, []Module{a, b}); err == nil {
		t.Error("zwei Themes ohne Fehler")
	}
	bad := themeModule{id: "a", files: fstest.MapFS{"home.html": file(`{{if}}`)}}
	if _, err := NewPublic(nil, []Module{bad}); err == nil {
		t.Error("ungültiges Template ohne Fehler")
	}
}

// Scheitert ein Theme-Template erst beim Ausführen, kommt die 500-Seite –
// ohne Fehlertext, Template-Namen oder Teile der kaputten Seite.
func TestPublicRenderErrorHidesDetails(t *testing.T) {
	theme := themeModule{id: "themes", files: fstest.MapFS{
		"home.html": file(`TEILAUSGABE {{template "fehlt-geheim"}}`),
	}}
	_, h := newTestPublic(t, theme)
	w := serve(h, get("/"))
	body := w.Body.String()
	if w.Code != 500 || !strings.Contains(body, "<h1>Interner Fehler</h1>") {
		t.Fatalf("Status %d:\n%s", w.Code, body)
	}
	if strings.Contains(body, "geheim") || strings.Contains(body, "TEILAUSGABE") || w.Header().Get("ETag") != "" {
		t.Errorf("500 verrät Details:\n%s", body)
	}

	// Auch die 500-Seite selbst kaputt → schlichter Text.
	theme.files["500.html"] = file(`{{template "auch-weg"}}`)
	_, h = newTestPublic(t, theme)
	w = serve(h, get("/"))
	if w.Code != 500 || strings.TrimSpace(w.Body.String()) != msgInternal {
		t.Errorf("Status %d, Körper %q", w.Code, w.Body.String())
	}
}

func TestSecurityHeadersAdmin(t *testing.T) {
	p, _ := newTestPublic(t)
	mux := http.NewServeMux()
	p.Mount(mux)
	mux.HandleFunc("GET /admin/static/x.css", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	})
	h := p.Recover(SecurityHeaders(mux))

	w := serve(h, get("/admin/gibt-es-nicht"))
	if w.Code != 404 || strings.Contains(w.Body.String(), "<html") {
		t.Errorf("Admin-404 ohne Theme erwartet: %d %q", w.Code, w.Body.String())
	}
	hd := w.Header()
	if hd.Get("Content-Security-Policy") != adminCSP || hd.Get("Cache-Control") != "no-store" ||
		hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Admin-Header: %v", hd)
	}
	if got := serve(h, get("/admin/static/x.css")).Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("statische Admin-Datei: Cache-Control %q", got)
	}
}

// pageModule ist ein Modul, das öffentliche Seiten liefert (wie Content).
type pageModule struct {
	themeModule
	pages map[string]PublicPage
	err   error
	asked *[]string
}

func (m pageModule) PublicPage(_ context.Context, path string) (PublicPage, bool, error) {
	*m.asked = append(*m.asked, path)
	p, ok := m.pages[path]
	return p, ok, m.err
}

// Session 3.1: Startseite und Catch-all fragen zuerst die PageSources.
func TestPublicPageSource(t *testing.T) {
	var asked []string
	m := pageModule{themeModule: themeModule{id: "content"}, asked: &asked, pages: map[string]PublicPage{
		"":          {View: PublicView{Title: "Willkommen", Body: "<p>Start</p>"}},
		"ueber-uns": {View: PublicView{Title: "Über uns", Body: "<p>Wir <b>sind</b> da.</p>", Description: "Kurz & gut"}},
		"alt":       {RedirectTo: "/leistungen/neu"},
		"boese":     {RedirectTo: "//evil.example"},
	}}
	_, h := newTestPublic(t, m)
	mux := h

	for _, tc := range []struct {
		method, path string
		status       int
		want, loc    string
	}{
		{"GET", "/", 200, "<h1>Willkommen</h1>\n<p>Start</p>", ""},
		{"GET", "/ueber-uns", 200, "<p>Wir <b>sind</b> da.</p>", ""},
		{"GET", "/ueber-uns", 200, `<meta name="description" content="Kurz &amp; gut">`, ""},
		{"HEAD", "/ueber-uns", 200, "", ""},
		{"GET", "/alt?x=1", 301, "", "/leistungen/neu?x=1"},
		{"GET", "/boese", 500, "Interner Fehler", ""},
		{"GET", "/gibtsnicht", 404, "Seite nicht gefunden", ""},
		{"POST", "/ueber-uns", 404, "", ""},
	} {
		w := serve(mux, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Location") != tc.loc {
			t.Errorf("%s %s: %d Location=%q\n%s", tc.method, tc.path, w.Code, w.Header().Get("Location"), w.Body)
		}
	}
	asked = nil
	serve(mux, get("/admin/irgendwas"))
	serve(mux, httptest.NewRequest("POST", "/ueber-uns", nil))
	if len(asked) != 0 {
		t.Errorf("PageSource für Admin-/POST-Pfad gefragt: %v", asked)
	}

	// Ohne zuständige Quelle bleibt die Kern-Startseite.
	_, h = newTestPublic(t, pageModule{themeModule: themeModule{id: "content"}, asked: &asked})
	if w := serve(h, get("/")); w.Code != 200 || !strings.Contains(w.Body.String(), "Inhalte folgen in Kürze") {
		t.Errorf("Kern-Startseite: %d %s", w.Code, w.Body)
	}
}

// counterModule zählt wie das Dashboard (HitCounter), hier nur im Speicher.
type counterModule struct {
	hits *[]Hit
}

func (counterModule) ID() string                 { return "dashboard" }
func (counterModule) Requires() []string         { return nil }
func (counterModule) Migrations() fs.FS          { return nil }
func (counterModule) Mount(*http.ServeMux, Deps) {}
func (counterModule) AdminNav() []NavItem        { return nil }

func (m counterModule) CountHit(_ context.Context, h Hit) error {
	*m.hits = append(*m.hits, h)
	return nil
}

// Session 3.4: Der Kern meldet nur erfolgreich ausgelieferte öffentliche Seiten.
func TestPublicHitCounter(t *testing.T) {
	var asked []string
	var hits []Hit
	src := pageModule{themeModule: themeModule{id: "content"}, asked: &asked, pages: map[string]PublicPage{
		"ueber-uns": {View: PublicView{Title: "Über uns"}},
		"alt":       {RedirectTo: "/ueber-uns"},
	}}
	_, h := newTestPublic(t, src, counterModule{hits: &hits})
	for _, r := range []*http.Request{
		get("/", "User-Agent", "Browser/1"),
		get("/ueber-uns?utm=x", "User-Agent", "Browser/2"),
		get("/ueber-uns", "Sec-Purpose", "prefetch"), // Vorab-Laden
		httptest.NewRequest("HEAD", "/ueber-uns", nil),
		get("/alt"),        // 301
		get("/gibtsnicht"), // 404
		get("/admin/x"),    // Admin
		get("/kaputt"),     // 500
		httptest.NewRequest("POST", "/ueber-uns", nil),
	} {
		serve(h, r)
	}
	want := []Hit{{Path: "/", UserAgent: "Browser/1"}, {Path: "/ueber-uns", UserAgent: "Browser/2"}}
	if len(hits) != len(want) || hits[0] != want[0] || hits[1] != want[1] {
		t.Errorf("gezählt: %+v, erwartet %+v", hits, want)
	}
}
