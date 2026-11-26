package content

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Session 3.3: Auto-Save, gleichzeitiges Bearbeiten, Vorschau, Revisionen,
// HTML-Modus.

type draftResp struct {
	RevisionID int64  `json:"revision_id"`
	Notice     string `json:"notice"`
	Error      string `json:"error"`
}

// save speichert einen Absatz-Entwurf mit Basis-Revision und liefert die Antwort.
func (e *env) save(id int64, text string, base int64, auto bool) draftResp {
	e.t.Helper()
	t, _ := json.Marshal(text)
	w := e.draft(id, fmt.Sprintf(`{"title":"Seite","format":"blocks","body":%s,"base_revision_id":%d,"autosave":%t}`,
		doc(`{"type":"paragraph","data":{"text":`+string(t)+`}}`), base, auto))
	var r draftResp
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || w.Code != 200 {
		e.t.Fatalf("Entwurf %d: %d %s", id, w.Code, w.Body)
	}
	return r
}

func (e *env) revCount(id int64) int {
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM content_revisions WHERE page_id = ?`, id).Scan(&n)
	return n
}

func (e *env) latestRev(id int64) int64 {
	var n int64
	_ = e.db.QueryRow(`SELECT MAX(id) FROM content_revisions WHERE page_id = ?`, id).Scan(&n)
	return n
}

// Auto-Saves fassen sich zusammen – sie verdrängen sonst die Historie.
func TestAutosaveCoalesces(t *testing.T) {
	e := newEnv(t)
	now := time.Unix(1_800_000_000, 0)
	e.m.st.now = func() time.Time { return now }
	id := e.create("Seite", 0)
	base := e.latestRev(id) // leere erste Revision (manuell)

	a := e.save(id, "eins", base, true)
	if a.RevisionID == base {
		t.Fatal("Auto-Save überschreibt eine manuelle Revision")
	}
	now = now.Add(5 * time.Second)
	b := e.save(id, "zwei", a.RevisionID, true)
	if b.RevisionID != a.RevisionID || b.Notice != "" {
		t.Errorf("zweiter Auto-Save: neue Revision %d statt %d (%q)", b.RevisionID, a.RevisionID, b.Notice)
	}
	var body string
	var saved int64
	_ = e.db.QueryRow(`SELECT body, saved_at FROM content_revisions WHERE id = ?`, b.RevisionID).Scan(&body, &saved)
	if !strings.Contains(body, "zwei") || saved != now.Unix() {
		t.Errorf("zusammengefasster Auto-Save: %s / %d", body, saved)
	}

	// Manuelles Speichern ist ein fester Stand; der nächste Auto-Save beginnt neu.
	m := e.save(id, "drei", b.RevisionID, false)
	c := e.save(id, "vier", m.RevisionID, true)
	if m.RevisionID == b.RevisionID || c.RevisionID == m.RevisionID {
		t.Errorf("manuell/auto: %d %d %d", b.RevisionID, m.RevisionID, c.RevisionID)
	}

	// Veröffentlichte Revision wird nie überschrieben.
	e.mustPost(id, "publish")
	d := e.save(id, "fünf", c.RevisionID, true)
	if d.RevisionID == c.RevisionID {
		t.Error("Auto-Save überschreibt die veröffentlichte Revision")
	}
	e.expect("/seite", 200, "", "<p>vier</p>")

	// Nach 10 Minuten beginnt eine neue Auto-Save-Revision.
	now = now.Add(autosaveWindow * time.Second)
	f := e.save(id, "sechs", d.RevisionID, true)
	if f.RevisionID == d.RevisionID {
		t.Error("Auto-Save nach Ablauf des Fensters zusammengefasst")
	}

	// Anderer Autor: keine Zusammenfassung über Autoren hinweg.
	e.asEditor()
	g := e.save(id, "sieben", f.RevisionID, true)
	if g.RevisionID == f.RevisionID {
		t.Error("Auto-Save eines anderen Autors überschreibt fremde Revision")
	}
	if n := e.revCount(id); n != 7 {
		t.Errorf("%d Revisionen, erwartet 7", n)
	}
}

// Gleichzeitiges Bearbeiten: letzte Revision gewinnt, mit Hinweis.
func TestConcurrentEditNotice(t *testing.T) {
	e := newEnv(t)
	id := e.create("Seite", 0)
	base := e.latestRev(id)

	e.asEditor()
	other := e.save(id, "von Redaktion", base, false)
	e.asAdmin()
	mine := e.save(id, "vom Admin", base, true) // baut auf veralteter Basis auf
	if !strings.Contains(mine.Notice, "redaktion@example.at hat") || !strings.Contains(mine.Notice, "wiederherstellbar") {
		t.Errorf("Hinweis fehlt: %q", mine.Notice)
	}
	if mine.RevisionID == other.RevisionID || e.latestRev(id) != mine.RevisionID {
		t.Error("letzte Revision gewinnt nicht")
	}
	// Die überholte Revision bleibt erhalten.
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM content_revisions WHERE id = ?`, other.RevisionID).Scan(&n)
	if n != 1 {
		t.Error("überholte Revision verloren")
	}
	// Derselbe Benutzer in einem zweiten Fenster.
	if r := e.save(id, "zweites Fenster", other.RevisionID, false); !strings.Contains(r.Notice, "anderen Fenster") {
		t.Errorf("Hinweis eigenes Fenster: %q", r.Notice)
	}
	// Aktuelle Basis und alter Client ohne Basis: kein Hinweis.
	last := e.latestRev(id)
	if r := e.save(id, "weiter", last, false); r.Notice != "" {
		t.Errorf("Hinweis ohne Konflikt: %q", r.Notice)
	}
	if r := e.save(id, "ohne Basis", 0, false); r.Notice != "" {
		t.Errorf("Hinweis ohne Basis: %q", r.Notice)
	}
}

func TestPreview(t *testing.T) {
	e := newEnv(t)
	id := e.create("Über uns", 0)
	first := e.save(id, "ERSTE FASSUNG", 0, false)
	e.save(id, "GEHEIMER ENTWURF", 0, false)
	e.expect("/ueber-uns", 404, "", "") // Entwurf nie öffentlich

	pv := fmt.Sprintf("/admin/content/pages/%d/preview", id)
	w := e.get(pv)
	b := w.Body.String()
	if w.Code != 200 || !strings.Contains(b, "<p>GEHEIMER ENTWURF</p>") || !strings.Contains(b, "Vorschau – nicht öffentlich") {
		t.Fatalf("Vorschau: %d\n%s", w.Code, b)
	}
	h := w.Header()
	if h.Get("X-Robots-Tag") != "noindex, nofollow" || h.Get("Cache-Control") != "no-store" {
		t.Errorf("Header: X-Robots-Tag=%q Cache-Control=%q", h.Get("X-Robots-Tag"), h.Get("Cache-Control"))
	}
	if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-eval") ||
		strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("CSP: %q", csp)
	}
	if w := e.get(fmt.Sprintf("%s?rev=%d", pv, first.RevisionID)); !strings.Contains(w.Body.String(), "ERSTE FASSUNG") {
		t.Errorf("Vorschau einer Revision: %d %s", w.Code, w.Body)
	}
	other := e.create("Andere", 0)
	for path, code := range map[string]int{
		fmt.Sprintf("%s?rev=%d", pv, e.latestRev(other)): 404, // Revision einer anderen Seite
		pv + "?rev=abc":                    404,
		"/admin/content/pages/999/preview": 404,
	} {
		if w := e.get(path); w.Code != code {
			t.Errorf("GET %s: %d, erwartet %d", path, w.Code, code)
		}
	}
	// Auch Editoren dürfen die Vorschau sehen.
	e.asEditor()
	if w := e.get(pv); w.Code != 200 {
		t.Errorf("Vorschau für Editoren: %d", w.Code)
	}
}

// Abnahme 3.3: Revision wiederherstellbar – als neue Revision.
func TestRestoreRevision(t *testing.T) {
	e := newEnv(t)
	id := e.create("Seite", 0)
	old := e.save(id, "ALTER STAND", 0, false)
	e.save(id, "NEUER STAND", 0, false)
	before := e.revCount(id)

	e.mustPost(id, fmt.Sprintf("revisions/%d/restore", old.RevisionID))
	if n := e.revCount(id); n != before+1 {
		t.Errorf("%d Revisionen nach Wiederherstellen, erwartet %d", n, before+1)
	}
	d, _ := e.m.st.latestDraft(t.Context(), id)
	if !strings.Contains(d.Body, "ALTER STAND") || d.RevID == old.RevisionID {
		t.Errorf("wiederhergestellt: %+v", d)
	}
	e.mustPost(id, "publish")
	e.expect("/seite", 200, "", "<p>ALTER STAND</p>")

	// Revisionsliste: Zeit, Autor, Markierungen, Aktionen.
	b := e.get(fmt.Sprintf("/admin/content/pages/%d", id)).Body.String()
	for _, want := range []string{"Revisionen", "admin@example.at", "aktueller Entwurf · veröffentlicht",
		fmt.Sprintf("/revisions/%d/restore", old.RevisionID), fmt.Sprintf("/preview?rev=%d", old.RevisionID)} {
		if !strings.Contains(b, want) {
			t.Errorf("Revisionsliste: fehlt %q", want)
		}
	}

	other := e.create("Andere", 0)
	if w := e.post(id, fmt.Sprintf("revisions/%d/restore", e.latestRev(other))); w.Code != 404 {
		t.Errorf("fremde Revision wiederhergestellt: %d", w.Code)
	}

	// Editoren dürfen HTML-Modus und Raw-Blöcke nicht zurückholen.
	html := e.draft(id, `{"title":"Seite","format":"html","body":"<p>roh</p>"}`)
	var hr draftResp
	_ = json.Unmarshal(html.Body.Bytes(), &hr)
	raw := e.draft(id, `{"title":"Seite","format":"blocks","body":`+doc(`{"type":"raw","data":{"html":"<b>x</b>"}}`)+`}`)
	var rr draftResp
	_ = json.Unmarshal(raw.Body.Bytes(), &rr)
	e.save(id, "normal", 0, false)
	e.asEditor()
	for _, rev := range []int64{hr.RevisionID, rr.RevisionID} {
		if w := e.post(id, fmt.Sprintf("revisions/%d/restore", rev)); w.Code != http.StatusForbidden {
			t.Errorf("Editor stellt Revision %d wieder her: %d", rev, w.Code)
		}
	}
}

// HTML-Modus: nur Admin, Blöcke → HTML übernimmt den Inhalt.
func TestSwitchFormat(t *testing.T) {
	e := newEnv(t)
	id := e.create("Seite", 0)
	e.mustDraft(id, "Seite", `{"type":"header","data":{"text":"Hallo","level":2}}`, para)

	e.asEditor()
	if w := e.post(id, "format", "format", "html"); w.Code != http.StatusForbidden {
		t.Errorf("Editor wechselt in den HTML-Modus: %d", w.Code)
	}
	e.asAdmin()
	if w := e.post(id, "format", "format", "xml"); w.Code != 400 {
		t.Errorf("unbekanntes Format: %d", w.Code)
	}
	e.mustPost(id, "format", "format", "html")
	d, _ := e.m.st.latestDraft(t.Context(), id)
	if d.Format != FormatHTML || !strings.Contains(d.Body, `<h2 id="hallo">Hallo</h2>`) || !strings.Contains(d.Body, "<b>Graz</b>") {
		t.Errorf("Blöcke → HTML: %+v", d)
	}
	n := e.revCount(id)
	e.mustPost(id, "format", "format", "html") // schon HTML: nichts zu tun
	if e.revCount(id) != n {
		t.Error("gleicher Modus legt Revision an")
	}
	e.mustPost(id, "publish")
	e.expect("/seite", 200, "", `<h2 id="hallo">Hallo</h2>`)

	e.mustPost(id, "format", "format", "blocks")
	d, _ = e.m.st.latestDraft(t.Context(), id)
	if d.Format != FormatBlocks || d.Body != emptyDoc {
		t.Errorf("HTML → Blöcke: %+v", d)
	}
	e.expect("/seite", 200, "", `<h2 id="hallo">Hallo</h2>`) // veröffentlicht bleibt
}
