package content

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

type env struct {
	t      *testing.T
	db     *sql.DB
	h      http.Handler
	m      *module
	admin  int64
	editor int64
	as     int64 // angemeldeter Benutzer für /admin/*
	role   string
}

// newEnv: echte SQLite mit allen Migrationen, Kern-Admin und -Public wie in
// main.go. Login/CSRF prüft der Kern selbst (eigene Tests) – hier wird der
// Benutzer direkt in den Kontext gelegt.
func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := core.OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := &module{}
	mods := []core.Module{m}
	ctx := context.Background()
	if err := core.Migrate(ctx, db, mods); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, m: m}
	for _, u := range []struct {
		id   *int64
		mail string
		role string
	}{{&e.admin, "admin@example.at", core.RoleAdmin}, {&e.editor, "redaktion@example.at", core.RoleEditor}} {
		res, err := db.Exec(`INSERT INTO core_users (email, password_hash, role) VALUES (?, 'x', ?)`, u.mail, u.role)
		if err != nil {
			t.Fatal(err)
		}
		*u.id, _ = res.LastInsertId()
	}
	e.as, e.role = e.admin, core.RoleAdmin
	admin, err := core.NewAdmin(db, nil, mods)
	if err != nil {
		t.Fatal(err)
	}
	public, err := core.NewPublic(nil, mods)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	admin.Mount(mux)
	public.Mount(mux)
	m.Mount(mux, core.Deps{DB: db, Admin: admin})
	e.h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			r = r.WithContext(core.WithUser(r.Context(), e.as, e.role))
		}
		mux.ServeHTTP(w, r)
	})
	return e
}

func (e *env) asEditor() { e.as, e.role = e.editor, core.RoleEditor }
func (e *env) asAdmin()  { e.as, e.role = e.admin, core.RoleAdmin }

func (e *env) do(method, path, ctype, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if ctype != "" {
		r.Header.Set("Content-Type", ctype)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) get(path string) *httptest.ResponseRecorder { return e.do("GET", path, "", "") }

func (e *env) form(path string, kv ...string) *httptest.ResponseRecorder {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return e.do("POST", path, "application/x-www-form-urlencoded", v.Encode())
}

// create legt eine Seite an und liefert ihre ID.
func (e *env) create(title string, parent int64) int64 {
	e.t.Helper()
	w := e.form("/admin/content/pages", "title", title, "parent_id", fmt.Sprint(parent))
	var id int64
	if w.Code != http.StatusSeeOther {
		e.t.Fatalf("anlegen %q: %d %s", title, w.Code, w.Body)
	}
	fmt.Sscanf(w.Header().Get("Location"), "/admin/content/pages/%d", &id)
	return id
}

func (e *env) draft(id int64, payload string) *httptest.ResponseRecorder {
	return e.do("POST", fmt.Sprintf("/admin/content/pages/%d/draft", id), "application/json", payload)
}

func (e *env) mustDraft(id int64, title string, blocks ...string) {
	e.t.Helper()
	b, _ := json.Marshal(title)
	if w := e.draft(id, `{"title":`+string(b)+`,"format":"blocks","body":`+doc(blocks...)+`}`); w.Code != 200 {
		e.t.Fatalf("Entwurf %d: %d %s", id, w.Code, w.Body)
	}
}

func (e *env) post(id int64, action string, kv ...string) *httptest.ResponseRecorder {
	return e.form(fmt.Sprintf("/admin/content/pages/%d/%s", id, action), kv...)
}

func (e *env) mustPost(id int64, action string, kv ...string) {
	e.t.Helper()
	if w := e.post(id, action, kv...); w.Code != http.StatusSeeOther {
		e.t.Fatalf("%s %d: %d %s", action, id, w.Code, w.Body)
	}
}

// expect prüft Status, Location und (optional) einen Ausschnitt des Körpers.
func (e *env) expect(path string, code int, loc, contains string) {
	e.t.Helper()
	w := e.get(path)
	if w.Code != code || w.Header().Get("Location") != loc || !strings.Contains(w.Body.String(), contains) {
		e.t.Errorf("GET %s: %d Location=%q, erwartet %d %q mit %q\n%s", path, w.Code, w.Header().Get("Location"), code, loc, contains, w.Body)
	}
}

const para = `{"type":"paragraph","data":{"text":"Seit 1987 in <b>Graz</b>."}}`

// Abnahme: Entwurf nicht öffentlich, veröffentlicht erreichbar, Löschen → 404.
func TestDraftPublishDelete(t *testing.T) {
	e := newEnv(t)
	id := e.create("Über uns", 0)
	e.expect("/ueber-uns", 404, "", "Seite nicht gefunden")

	e.mustDraft(id, "Über uns", para)
	e.expect("/ueber-uns", 404, "", "") // Entwurf bleibt privat

	e.mustPost(id, "publish")
	e.expect("/ueber-uns", 200, "", "<h1>Über uns</h1>\n<p>Seit 1987 in <b>Graz</b>.</p>")

	// Neuer Entwurf ändert die öffentliche Seite nicht.
	e.mustDraft(id, "Über uns (neu)", `{"type":"paragraph","data":{"text":"GEHEIMER ENTWURF"}}`)
	if w := e.get("/ueber-uns"); strings.Contains(w.Body.String(), "GEHEIMER") || !strings.Contains(w.Body.String(), "1987") {
		t.Errorf("Entwurf öffentlich sichtbar:\n%s", w.Body)
	}
	e.mustPost(id, "publish")
	e.expect("/ueber-uns", 200, "", "<h1>Über uns (neu)</h1>\n<p>GEHEIMER ENTWURF</p>")

	e.mustPost(id, "unpublish")
	e.expect("/ueber-uns", 404, "", "")
	e.mustPost(id, "publish")

	e.mustPost(id, "delete")
	e.expect("/ueber-uns", 404, "", "")
	if w := e.post(id, "publish"); w.Code != 404 {
		t.Errorf("Veröffentlichen einer gelöschten Seite: %d", w.Code)
	}
	if w := e.draft(id, `{"title":"x","body":{"blocks":[]}}`); w.Code != 404 {
		t.Errorf("Entwurf einer gelöschten Seite: %d", w.Code)
	}
}

// Abnahme: alter Slug leitet mit 301 um – auch Unterseiten, ohne Ketten.
func TestSlugsHierarchyRedirects(t *testing.T) {
	e := newEnv(t)
	leist := e.create("Leistungen", 0)
	web := e.create("Webdesign", leist)
	for _, id := range []int64{leist, web} {
		e.mustDraft(id, "Seite", para)
		e.mustPost(id, "publish")
	}
	e.expect("/leistungen/webdesign", 200, "", "Graz")
	e.expect("/webdesign", 404, "", "")

	// Elternteil umbenennen: Unterseite zieht mit, alte Pfade → 301.
	e.mustPost(leist, "settings", "slug", "angebot", "parent_id", "0")
	e.expect("/angebot/webdesign", 200, "", "Graz")
	e.expect("/leistungen", 301, "/angebot", "")
	e.expect("/leistungen/webdesign?utm=x", 301, "/angebot/webdesign?utm=x", "")

	// Unterseite nach oben verschieben: beide alten Pfade zeigen direkt aufs neue Ziel.
	e.mustPost(web, "settings", "slug", "webdesign", "parent_id", "0")
	e.expect("/webdesign", 200, "", "Graz")
	e.expect("/leistungen/webdesign", 301, "/webdesign", "")
	e.expect("/angebot/webdesign", 301, "/webdesign", "")

	// Kanonische Schreibweise.
	e.expect("/Angebot", 301, "/angebot", "")
	e.expect("/angebot/", 301, "/angebot", "")
	e.expect("/angebot/gibtsnicht", 404, "", "")
	e.expect("/ungültig_pfad", 404, "", "")

	// Ein alter Pfad wird frei, sobald eine neue Seite ihn bekommt.
	neu := e.create("Leistungen", 0)
	e.mustDraft(neu, "Neue Leistungen", para)
	e.mustPost(neu, "publish")
	e.expect("/leistungen", 200, "", "Neue Leistungen")

	// Weiterleitung auf einen Entwurf gibt es nicht.
	e.mustPost(web, "unpublish")
	e.expect("/angebot/webdesign", 404, "", "")

	for _, tc := range []struct {
		name string
		id   int64
		kv   []string
		code int
		msg  string
	}{
		{"Zyklus", leist, []string{"slug", "angebot", "parent_id", fmt.Sprint(leist)}, 400, msgBadParent},
		{"reserviert", web, []string{"slug", "admin", "parent_id", "0"}, 400, msgPathReserved},
		{"vergeben", web, []string{"slug", "angebot", "parent_id", "0"}, 400, msgPathTaken},
		{"leer", web, []string{"slug", "!!!", "parent_id", "0"}, 400, "nur Kleinbuchstaben"},
		{"Elternteil fehlt", web, []string{"slug", "x", "parent_id", "999"}, 400, msgParentNotFound},
		{"unbekannte Seite", 999, []string{"slug", "x", "parent_id", "0"}, 404, msgPageNotFound},
	} {
		if w := e.post(tc.id, "settings", tc.kv...); w.Code != tc.code || !strings.Contains(w.Body.String(), tc.msg) {
			t.Errorf("%s: %d\n%s", tc.name, w.Code, w.Body)
		}
	}
	// Reserviert gilt nur auf oberster Ebene; Unterseite in sich selbst verschieben geht nicht.
	e.mustPost(web, "settings", "slug", "admin", "parent_id", fmt.Sprint(leist))
	e.expect("/angebot/admin", 404, "", "") // Entwurf
	if w := e.post(leist, "settings", "slug", "angebot", "parent_id", fmt.Sprint(web)); w.Code != 400 {
		t.Errorf("Elternteil unter eigene Unterseite: %d", w.Code)
	}
	// Eingabe mit Umlauten wird zum Slug.
	e.mustPost(web, "settings", "slug", "Web Design Ö", "parent_id", fmt.Sprint(leist))
	if p, _ := e.m.st.page(context.Background(), web); p.Path != "angebot/web-design-oe" {
		t.Errorf("Pfad = %q", p.Path)
	}

	// Löschen mit Unterseiten ist gesperrt.
	if w := e.post(leist, "delete"); w.Code != 400 || !strings.Contains(w.Body.String(), "Unterseiten") {
		t.Errorf("Löschen mit Unterseiten: %d", w.Code)
	}
}

func TestSlugCollisionsAndReserved(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	want := map[string]string{}
	for _, tc := range []struct{ title, path string }{
		{"Über uns", "ueber-uns"}, {"Über uns", "ueber-uns-2"}, {"Über  uns!", "ueber-uns-3"},
		{"Admin", "admin-2"}, {"Shop", "shop-2"}, {"日本語", "seite"}, {"???", "seite-2"},
	} {
		id := e.create(tc.title, 0)
		p, err := e.m.st.page(ctx, id)
		if err != nil || p.Path != tc.path {
			t.Errorf("%q → %q, erwartet %q", tc.title, p.Path, tc.path)
		}
		want[tc.path] = tc.title
	}
	// Unterseiten-Kollision nur innerhalb derselben Elternseite.
	parent := e.create("Team", 0)
	a := e.create("Über uns", parent)
	if p, _ := e.m.st.page(ctx, a); p.Path != "team/ueber-uns" {
		t.Errorf("Unterseite: %q", p.Path)
	}
	if w := e.form("/admin/content/pages", "title", "  "); w.Code != 400 || !strings.Contains(w.Body.String(), msgTitleMissing) {
		t.Errorf("leerer Titel: %d", w.Code)
	}
	// Tiefe ≤ 5.
	id := parent
	for i := 2; i <= maxDepth; i++ {
		id = e.create(fmt.Sprint("Ebene ", i), id)
	}
	if w := e.form("/admin/content/pages", "title", "Zu tief", "parent_id", fmt.Sprint(id)); w.Code != 400 || !strings.Contains(w.Body.String(), "5 Ebenen") {
		t.Errorf("zu tief: %d", w.Code)
	}
}

func TestHomePage(t *testing.T) {
	e := newEnv(t)
	e.expect("/", 200, "", "Inhalte folgen in Kürze") // Kern-Startseite
	a := e.create("Willkommen", 0)
	b := e.create("Start neu", 0)
	e.mustDraft(a, "Willkommen", para)
	e.mustPost(a, "settings", "slug", "willkommen", "parent_id", "0", "home", "1")
	e.expect("/", 200, "", "Inhalte folgen") // Entwurf: weiter Kern-Startseite
	e.mustPost(a, "publish")
	e.expect("/", 200, "", "<h1>Willkommen</h1>")
	e.expect("/willkommen", 301, "/", "")

	// Nur eine Startseite.
	e.mustDraft(b, "Start neu", para)
	e.mustPost(b, "publish")
	e.mustPost(b, "settings", "slug", "start-neu", "parent_id", "0", "home", "1")
	e.expect("/", 200, "", "<h1>Start neu</h1>")
	e.expect("/willkommen", 200, "", "<h1>Willkommen</h1>")
	e.mustPost(b, "unpublish")
	e.expect("/", 200, "", "Inhalte folgen in Kürze")
}

// Raw-HTML und HTML-Modus: Rolle wird beim Speichern serverseitig geprüft.
func TestDraftValidationAndRoles(t *testing.T) {
	e := newEnv(t)
	id := e.create("Kontakt", 0)
	raw := `{"title":"Kontakt","body":{"blocks":[{"type":"raw","data":{"html":"<iframe src=\"https://maps.example\"></iframe>"}}]}}`
	html := `{"title":"Kontakt","format":"html","body":"<section>frei</section>"}`

	e.asEditor()
	for _, tc := range []struct {
		name, payload string
		code          int
		msg           string
	}{
		{"Raw durch Editor", raw, 403, "nur Administratoren"},
		{"HTML durch Editor", html, 403, "nur Administratoren"},
		{"unbekannter Typ", `{"title":"K","body":{"blocks":[{"type":"embed","data":{}}]}}`, 400, "Unbekannter Blocktyp"},
		{"Header 1", `{"title":"K","body":{"blocks":[{"type":"header","data":{"text":"x","level":1}}]}}`, 400, "Ebenen 2 bis 4"},
		{"kein Titel", `{"title":"","body":{"blocks":[]}}`, 400, msgTitleMissing},
		{"kaputtes JSON", `{"title":`, 400, "kein gültiges JSON"},
		{"unbekanntes Feld", `{"title":"K","body":{"blocks":[]},"author_id":1}`, 400, "kein gültiges JSON"},
		{"HTML-Body kein Text", `{"title":"K","format":"html","body":{"x":1}}`, 400, "muss „body“ ein Text"},
		{"Editor normal", `{"title":"Kontakt","body":` + doc(para) + `}`, 200, "revision_id"},
	} {
		w := e.draft(id, tc.payload)
		if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.msg) || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body)
		}
	}
	e.asAdmin()
	if w := e.draft(id, raw); w.Code != 200 {
		t.Fatalf("Raw durch Admin: %d %s", w.Code, w.Body)
	}
	e.mustPost(id, "publish")
	e.expect("/kontakt", 200, "", `<iframe src="https://maps.example"></iframe>`)
	if w := e.draft(id, html); w.Code != 200 {
		t.Fatalf("HTML durch Admin: %d %s", w.Code, w.Body)
	}
	e.mustPost(id, "publish")
	e.expect("/kontakt", 200, "", "<section>frei</section>")

	// Autor wird gespeichert.
	var author int64
	_ = e.db.QueryRow(`SELECT author_id FROM content_revisions WHERE page_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&author)
	if author != e.admin {
		t.Errorf("author_id = %d", author)
	}
}

func TestAdminPages(t *testing.T) {
	e := newEnv(t)
	parent := e.create("Leistungen", 0)
	e.create("Webdesign <b>", parent)
	w := e.get("/admin/content")
	b := w.Body.String()
	for _, want := range []string{`data-styrixa-template="content-list"`, "<code>/leistungen/webdesign-b</code>", "Webdesign &lt;b&gt;", "– ", "Entwurf", `<option value="1">/leistungen</option>`} {
		if w.Code != 200 || !strings.Contains(b, want) {
			t.Errorf("Liste: fehlt %q (%d)", want, w.Code)
		}
	}
	w = e.get(fmt.Sprintf("/admin/content/pages/%d", parent))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `data-styrixa-template="content-page"`) || strings.Contains(w.Body.String(), `<option value="2"`) {
		t.Errorf("Bearbeiten: %d\n%s", w.Code, w.Body) // eigene Unterseite ist als Elternteil nicht wählbar
	}
	for _, p := range []string{"/admin/content/pages/999", "/admin/content/pages/abc"} {
		if w := e.get(p); w.Code != 404 {
			t.Errorf("GET %s: %d", p, w.Code)
		}
	}
	e.asEditor()
	if w := e.get("/admin/content"); w.Code != 200 || !strings.Contains(w.Body.String(), `<a href="/admin/content" aria-current="page">Seiten</a>`) {
		t.Errorf("Editor sieht „Seiten“ nicht: %d", w.Code)
	}
}

// Aufräumen: 50 neueste Revisionen bleiben, die veröffentlichte immer.
func TestPruneRevisions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id := e.create("Alt", 0)
	other := e.create("Andere", 0)
	e.mustPost(id, "publish") // veröffentlicht = erste (älteste) Revision
	var pub int64
	_ = e.db.QueryRow(`SELECT published_revision_id FROM content_pages WHERE id = ?`, id).Scan(&pub)
	for i := 0; i < 60; i++ {
		e.mustDraft(id, fmt.Sprint("Version ", i), para)
	}
	n, err := e.m.st.prune(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var left, hasPub, otherLeft int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM content_revisions WHERE page_id = ?`, id).Scan(&left)
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM content_revisions WHERE id = ?`, pub).Scan(&hasPub)
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM content_revisions WHERE page_id = ?`, other).Scan(&otherLeft)
	if n != 10 || left != 51 || hasPub != 1 || otherLeft != 1 {
		t.Errorf("gelöscht %d, übrig %d, veröffentlichte da %d, andere %d", n, left, hasPub, otherLeft)
	}
	e.expect("/alt", 200, "", "<h1>Alt</h1>")
	if !hasJob("content.prune_revisions") {
		t.Error("Job nicht registriert")
	}
}

func hasJob(name string) bool {
	for _, j := range core.RegisteredJobs() {
		if j.Name == name {
			return true
		}
	}
	return false
}
