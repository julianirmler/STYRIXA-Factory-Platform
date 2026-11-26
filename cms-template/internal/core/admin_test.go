package core

import (
	"context"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeAdminModule bringt eigene Admin-Templates mit (wie ein echtes Modul per embed).
type fakeAdminModule struct {
	id    string
	nav   []NavItem
	files fstest.MapFS
}

func (m fakeAdminModule) ID() string          { return m.id }
func (fakeAdminModule) Requires() []string    { return nil }
func (fakeAdminModule) Migrations() fs.FS     { return nil }
func (m fakeAdminModule) AdminNav() []NavItem { return m.nav }
func (m fakeAdminModule) Templates() fs.FS    { return m.files }
func (m fakeAdminModule) Mount(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /admin/"+m.id+"/liste", func(w http.ResponseWriter, r *http.Request) {
		d.Admin.Render(w, r, http.StatusOK, AdminPage{Title: "Liste", Template: m.id + "/pages/liste.html",
			Data: map[string]string{"Name": `<script>alert(1)</script>`}})
	})
	mux.HandleFunc("GET /admin/"+m.id+"/kaputt", func(w http.ResponseWriter, r *http.Request) {
		d.Admin.Render(w, r, http.StatusOK, AdminPage{Title: "X", Template: m.id + "/gibtsnicht.html"})
	})
	mux.HandleFunc("GET /admin/"+m.id+"/bald", AdminPlaceholder(d, "Kommt bald"))
}

var testMods = []Module{
	fakeAdminModule{id: "zeta", nav: []NavItem{{Label: "Zeta", Path: "/admin/zeta/liste", Order: 50}},
		files: fstest.MapFS{"pages/liste.html": {Data: []byte(`{{define "zeile"}}<li>{{.}}</li>{{end}}<ul data-marker="zeta-liste">{{template "zeile" .Data.Name}}</ul>`)}}},
	fakeAdminModule{id: "alpha", nav: []NavItem{{Label: "Alpha", Path: "/admin/alpha/liste", Order: 10}},
		files: fstest.MapFS{"pages/liste.html": {Data: []byte(`{{define "zeile"}}<p>{{.}}</p>{{end}}<div data-marker="alpha-liste">{{template "zeile" .Data.Name}}</div>`)}}},
}

func TestAdminLayout(t *testing.T) {
	e := newAuthEnv(t, testMods...)
	tok := e.login(t, reqOpt{})
	w := e.req("GET", "/admin/zeta/liste", nil, reqOpt{cookie: tok})
	if w.Code != 200 {
		t.Fatalf("Status %d: %s", w.Code, w.Body)
	}
	body := w.Body.String()
	csrf := e.csrf(t, tok)
	for _, want := range []string{
		`<title>Liste · Tischlerei Muster</title>`, // Seitentitel aus Settings
		`data-marker="zeta-liste"`,                 // Modul-Template im Layout
		`<li>&lt;script&gt;`,                       // Daten werden escaped
		testEmail,
		`name="csrf" value="` + csrf + `"`,              // Logout-Formular
		`hx-headers='{"X-CSRF-Token": "` + csrf + `"}'`, // htmx-Header
		`<a href="/admin/zeta/liste" aria-current="page">Zeta</a>`,
		`/admin/static/htmx-2.0.11.min.`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fehlt: %s", want)
		}
	}
	if strings.Contains(body, "<script>alert") {
		t.Error("nicht escaped")
	}
	// Navigation nach Order: Übersicht (0), Alpha (10), Zeta (50)
	order := regexp.MustCompile(`<a href="(/admin[^"]*)"`).FindAllStringSubmatch(body, -1)
	var got []string
	for _, m := range order {
		got = append(got, m[1])
	}
	if strings.Join(got, " ") != "/admin /admin/alpha/liste /admin/zeta/liste" {
		t.Errorf("Navigation: %v", got)
	}
	h := w.Header()
	if h.Get("Cache-Control") != "no-store" || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'self'") ||
		h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("Header: %v", h)
	}
}

func TestAdminTemplatesNamespaced(t *testing.T) {
	// Beide Module definieren „zeile“ – jedes bekommt sein eigenes.
	e := newAuthEnv(t, testMods...)
	tok := e.login(t, reqOpt{})
	if b := e.req("GET", "/admin/alpha/liste", nil, reqOpt{cookie: tok}).Body.String(); !strings.Contains(b, `<p>&lt;script`) {
		t.Errorf("alpha: %s", b)
	}
}

func TestAdminDashboardAndPlaceholder(t *testing.T) {
	e := newAuthEnv(t, testMods...)
	tok := e.login(t, reqOpt{})
	for _, tc := range []struct{ path, want string }{
		{"/admin", `<a href="/admin" aria-current="page">Übersicht</a>`},
		{"/admin/", "Willkommen"},
		{"/admin/zeta/bald", "<h1>Kommt bald</h1>"},
	} {
		w := e.req("GET", tc.path, nil, reqOpt{cookie: tok})
		if w.Code != 200 || !strings.Contains(w.Body.String(), tc.want) {
			t.Errorf("%s: %d, %q fehlt", tc.path, w.Code, tc.want)
		}
	}
}

func TestAdminMissingTemplate(t *testing.T) {
	e := newAuthEnv(t, testMods...)
	tok := e.login(t, reqOpt{})
	w := e.req("GET", "/admin/zeta/kaputt", nil, reqOpt{cookie: tok})
	if w.Code != 500 || strings.Contains(w.Body.String(), "gibtsnicht") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestAdminBadModuleTemplate(t *testing.T) {
	bad := fakeAdminModule{id: "bad", files: fstest.MapFS{"x.html": {Data: []byte(`{{ .Data `)}}}
	if _, err := NewAdmin(nil, nil, []Module{bad}); err == nil || !strings.Contains(err.Error(), "bad/x.html") {
		t.Fatalf("erwartet Parse-Fehler, bekommen %v", err)
	}
}

func TestAdminStatic(t *testing.T) {
	e := newAuthEnv(t)
	a, _ := NewAdmin(nil, nil, nil)
	for _, name := range []string{"admin.css", htmxFile} {
		u, err := a.assetURL(name)
		if err != nil {
			t.Fatal(err)
		}
		// ohne Login abrufbar (nichts Geheimes), für immer cachebar
		w := e.req("GET", u, nil, reqOpt{})
		if w.Code != 200 || w.Body.Len() < 100 {
			t.Fatalf("%s: %d", u, w.Code)
		}
		if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("%s: Cache-Control %q", u, w.Header().Get("Cache-Control"))
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "css") && !strings.Contains(ct, "javascript") {
			t.Errorf("%s: Content-Type %q", u, ct)
		}
	}
	if w := e.req("GET", "/admin/static/"+htmxFile, nil, reqOpt{}); w.Code != 404 {
		t.Errorf("ungehashter Name: %d", w.Code)
	}
	if w := e.req("GET", "/admin/static/../auth.go", nil, reqOpt{}); w.Code == 200 {
		t.Error("Pfad außerhalb von static ausgeliefert")
	}
}

// Session 3.1: Die Rolle kommt bei jeder Anfrage aus core_users (kein Cache);
// Editoren sehen nur Navigationseinträge mit Role = RoleEditor.
func TestAdminNavByRole(t *testing.T) {
	mods := append([]Module{
		fakeAdminModule{id: "seiten", nav: []NavItem{{Label: "Seiten", Path: "/admin/seiten/liste", Role: RoleEditor, Order: 20}},
			files: fstest.MapFS{"pages/liste.html": {Data: []byte(`<p data-marker="seiten">{{.Data.Name}}</p>`)}}},
	}, testMods...)
	e := newAuthEnv(t, mods...)
	tok := e.login(t, reqOpt{})
	nav := func() string {
		w := e.req("GET", "/admin/seiten/liste", nil, reqOpt{cookie: tok})
		if w.Code != 200 {
			t.Fatalf("Status %d", w.Code)
		}
		var got []string
		for _, m := range regexp.MustCompile(`<li><a href="(/admin[^"]*)"`).FindAllStringSubmatch(w.Body.String(), -1) {
			got = append(got, m[1])
		}
		return strings.Join(got, " ")
	}
	if got := nav(); got != "/admin /admin/alpha/liste /admin/seiten/liste /admin/zeta/liste" {
		t.Errorf("Admin-Navigation: %s", got)
	}
	if _, err := e.db.Exec(`UPDATE core_users SET role = 'editor' WHERE id = ?`, e.user); err != nil {
		t.Fatal(err)
	}
	if got := nav(); got != "/admin /admin/seiten/liste" {
		t.Errorf("Editor-Navigation: %s", got)
	}
	if _, err := e.db.Exec(`UPDATE core_users SET role = 'chef' WHERE id = ?`, e.user); err == nil {
		t.Error("unbekannte Rolle wurde gespeichert")
	}
}

func TestRoleHelpers(t *testing.T) {
	ctx := context.Background()
	if CurrentRole(ctx) != "" || IsAdmin(ctx) {
		t.Error("ohne Session keine Rolle")
	}
	if !IsAdmin(WithUser(ctx, 1, RoleAdmin)) || IsAdmin(WithUser(ctx, 1, RoleEditor)) {
		t.Error("IsAdmin")
	}
	if id, _ := CurrentUser(WithUser(ctx, 7, RoleEditor)); id != 7 {
		t.Error("CurrentUser")
	}
}
