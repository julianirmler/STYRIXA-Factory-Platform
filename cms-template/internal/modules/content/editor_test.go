package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Session 3.2: Editor.js vendort, Editor-Seite, Rundlauf Editor → Server → öffentlich.

// Die vendorten Dateien sind byte-identisch zu VENDOR.md, jede ist gelistet
// und wird von der Editor-Seite geladen.
func TestVendoredFilesMatchChecksums(t *testing.T) {
	md, err := os.ReadFile("VENDOR.md")
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `static/([^`]+)` \\|.*\\| `([0-9a-f]{64})` \\|$").FindAllStringSubmatch(string(md), -1)
	listed := map[string]bool{}
	for _, r := range rows {
		b, err := fs.ReadFile(static, "static/"+r[1])
		if err != nil {
			t.Errorf("%s: %v", r[1], err)
			continue
		}
		if sum := sha256.Sum256(b); hex.EncodeToString(sum[:]) != r[2] {
			t.Errorf("%s: SHA-256 %x, VENDOR.md sagt %s", r[1], sum, r[2])
		}
		listed[r[1]] = true
	}
	tmpl, err := fs.ReadFile(templates, "templates/page.html")
	if err != nil {
		t.Fatal(err)
	}
	vendored, _ := fs.Glob(static, "static/editorjs-*.js")
	if len(vendored) != 8 || len(listed) != len(vendored) {
		t.Errorf("vendort %d Dateien, gelistet %d", len(vendored), len(listed))
	}
	for _, f := range vendored {
		name := strings.TrimPrefix(f, "static/")
		if !listed[name] {
			t.Errorf("%s fehlt in VENDOR.md", name)
		}
		if !strings.Contains(string(tmpl), `{{asset "content/`+name+`"}}`) {
			t.Errorf("%s wird in page.html nicht geladen", name)
		}
	}
}

var nonceRe = regexp.MustCompile(`<script src="/admin/static/content-editor-nonce\.[0-9a-f]{10}\.js" nonce="([A-Za-z0-9_-]{22})" defer>`)

func TestEditorPage(t *testing.T) {
	e := newEnv(t)
	id := e.create("Über uns", 0)
	// Inhalt, der aus einem Attribut ausbrechen will, bleibt Daten.
	e.mustDraft(id, "Über uns", `{"type":"paragraph","data":{"text":"\"></div><script>alert(1)</script>"}}`)
	w := e.get(fmt.Sprintf("/admin/content/pages/%d", id))
	b := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("%d\n%s", w.Code, b)
	}
	m := regexp.MustCompile(`data-body="([^"]*)"`).FindStringSubmatch(b)
	if m == nil || strings.Contains(b, "<script>alert") {
		t.Fatalf("data-body fehlt oder nicht escaped:\n%s", b)
	}
	if got := html.UnescapeString(m[1]); !strings.Contains(got, `"blocks":[{"type":"paragraph"`) || !strings.Contains(got, `<script>alert(1)</script>`) {
		t.Errorf("data-body enthält nicht den Entwurf: %s", got)
	}
	for _, want := range []string{
		`data-draft-url="/admin/content/pages/1/draft"`,
		`data-allow-raw="1"`, // Admin
		`/admin/static/content-editorjs-raw-2.5.1.`,
		`/admin/static/content-editorjs-2.31.7.`,
		`/admin/static/content-editor-blocks.`,
		`id="editor-save"`,
		`data-save-first`,
		`value="Über uns"`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("fehlt: %s", want)
		}
	}
	// Die Nonce der Brücke ist die der CSP – und gilt nur für Styles.
	n := nonceRe.FindStringSubmatch(b)
	if n == nil {
		t.Fatal("editor-nonce.js ohne Nonce")
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "style-src 'self' 'nonce-"+n[1]+"'") || strings.Contains(csp, "unsafe") {
		t.Errorf("CSP passt nicht zur Nonce: %q", csp)
	}

	// Editor: kein Raw-Block (der Server lehnt ihn ohnehin ab, siehe TestDraftValidationAndRoles).
	e.asEditor()
	b = e.get(fmt.Sprintf("/admin/content/pages/%d", id)).Body.String()
	if strings.Contains(b, "data-allow-raw") || strings.Contains(b, "editorjs-raw") {
		t.Error("Raw-HTML-Werkzeug für Editoren sichtbar")
	}
	if !strings.Contains(b, "content-editorjs-2.31.7.") {
		t.Error("Editor fehlt für Editoren")
	}

	// HTML-Modus (3.3): Textarea statt Block-Editor, keine Editor.js-Dateien.
	e.asAdmin()
	if w := e.draft(id, `{"title":"Über uns","format":"html","body":"<p>Hallo</p><script>x()</script>"}`); w.Code != 200 {
		t.Fatalf("HTML-Entwurf: %d %s", w.Code, w.Body)
	}
	b = e.get(fmt.Sprintf("/admin/content/pages/%d", id)).Body.String()
	for _, want := range []string{`data-mode="html"`, `<textarea id="html-body"`, `&lt;p&gt;Hallo&lt;/p&gt;&lt;script&gt;`, `value="blocks"`, `/admin/static/content-editor.`} {
		if !strings.Contains(b, want) {
			t.Errorf("HTML-Modus: fehlt %s", want)
		}
	}
	if strings.Contains(b, `id="editorjs"`) || strings.Contains(b, "editorjs-2.31.7") || strings.Contains(b, "<script>x()") {
		t.Errorf("HTML-Modus: Block-Editor geladen oder HTML nicht escaped\n%s", b)
	}
	// Editoren sehen den HTML-Code nicht als bearbeitbares Feld.
	e.asEditor()
	b = e.get(fmt.Sprintf("/admin/content/pages/%d", id)).Body.String()
	if !strings.Contains(b, "nur Administratoren") || strings.Contains(b, "html-body") || strings.Contains(b, "/format") {
		t.Errorf("HTML-Modus für Editoren bearbeitbar\n%s", b)
	}
}

// editorOutput ist, was Editor.js 2.31 mit den vendorten Plugins und den
// eigenen Blöcken beim Speichern liefert (im Browser aufgezeichnet, gekürzt):
// List v2 mit meta, Quote mit alignment, Inline-Markup der Werkzeuge mit
// Klassen-Attributen, &nbsp; und &amp; wie im contenteditable.
const editorOutput = `{"time":1795722833547,"blocks":[
{"id":"a1","type":"paragraph","data":{"text":"Tischlerei &amp; Werkstatt, <mark class=\"cdx-marker\">seit</mark> 1987 –&nbsp;<code class=\"inline-code\">Holz</code> <a href=\"https://example.org\">Link</a>"}},
{"id":"a2","type":"header","data":{"text":"Unsere <i>Werkstatt</i>","level":3}},
{"id":"a3","type":"list","data":{"style":"ordered","meta":{"start":1,"counterType":"numeric"},"items":[{"content":"Massivholz","meta":{},"items":[{"content":"Eiche","meta":{},"items":[]}]},{"content":"Montage","meta":{},"items":[]}]}},
{"id":"a4","type":"quote","data":{"text":"Holz lebt.","caption":"Meister","alignment":"left"}},
{"id":"a5","type":"delimiter","data":{}},
{"id":"a6","type":"image","data":{"url":"/media/werkstatt.webp","alt":"Hobelbank","caption":"In <b>Graz</b>"}},
{"id":"a7","type":"faq","data":{"items":[{"question":"Wie lange dauert es?","answer":"<b>Meist</b> zwei Wochen."}]}},
{"id":"a8","type":"answer","data":{"question":"Was kostet ein Tisch?","answer":"Ab 1.800 Euro.","details":""}},
{"id":"a9","type":"cta","data":{"text":"Fragen?","label":"Jetzt anfragen","url":"/kontakt"}},
{"id":"b1","type":"raw","data":{"html":"<div class=\"karte\"></div>"}}
],"version":"2.31.7"}`

// Abnahme 3.2: Der gespeicherte Body ist von 3.1 validiert und wird
// öffentlich korrekt gerendert – in der Reihenfolge des Editors.
func TestEditorOutputRoundTrip(t *testing.T) {
	e := newEnv(t)
	id := e.create("Über uns", 0)
	if w := e.draft(id, `{"title":"Über uns","format":"blocks","body":`+editorOutput+`}`); w.Code != 200 {
		t.Fatalf("Entwurf: %d %s", w.Code, w.Body)
	}
	e.mustPost(id, "publish")
	w := e.get("/ueber-uns")
	b := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("%d\n%s", w.Code, b)
	}
	want := []string{
		"<p>Tischlerei &amp; Werkstatt, <mark>seit</mark> 1987 –\u00a0<code>Holz</code> <a href=\"https://example.org\" rel=\"noopener\">Link</a></p>",
		`<h3 id="unsere-werkstatt">Unsere <i>Werkstatt</i></h3>`,
		`<ol><li>Massivholz<ol><li>Eiche</li></ol></li><li>Montage</li></ol>`,
		`<figure><blockquote><p>Holz lebt.</p></blockquote><figcaption>Meister</figcaption></figure>`,
		`<hr>`,
		`<figure><img src="/media/werkstatt.webp" alt="Hobelbank" loading="lazy" decoding="async"><figcaption>In <b>Graz</b></figcaption></figure>`,
		`<summary>Wie lange dauert es?</summary><div><b>Meist</b> zwei Wochen.</div>`,
		`<h2 id="was-kostet-ein-tisch">Was kostet ein Tisch?</h2><p class="answer-short">Ab 1.800 Euro.</p>`,
		`<a class="cta-button" href="/kontakt">Jetzt anfragen</a>`,
		`<div class="karte"></div>`,
	}
	last := -1
	for _, s := range want {
		i := strings.Index(b, s)
		if i < 0 {
			t.Errorf("fehlt: %s", s)
			continue
		}
		if i < last {
			t.Errorf("Reihenfolge: %s steht zu weit vorn", s)
		}
		last = i
	}
	if strings.Contains(b, "cdx-marker") || strings.Contains(b, "inline-code") {
		t.Error("Klassen-Attribute der Inline-Werkzeuge nicht entfernt")
	}

	// Checkliste blendet das UI aus; kommt sie trotzdem, lehnt der Server ab.
	check := strings.Replace(editorOutput, `"style":"ordered"`, `"style":"checklist"`, 1)
	if w := e.draft(id, `{"title":"Über uns","format":"blocks","body":`+check+`}`); w.Code != 400 {
		t.Errorf("Checkliste: %d %s", w.Code, w.Body)
	}
}
