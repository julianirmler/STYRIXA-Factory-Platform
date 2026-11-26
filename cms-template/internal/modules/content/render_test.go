package content

import (
	"reflect"
	"strings"
	"testing"
)

func doc(blocks ...string) string {
	return `{"time":1,"blocks":[` + strings.Join(blocks, ",") + `],"version":"2.31.0"}`
}

// Abnahme Session 3.1: Renderer-Test je Blocktyp.
func TestRenderBlocks(t *testing.T) {
	for _, tc := range []struct{ name, block, want string }{
		{"paragraph", `{"id":"a","type":"paragraph","data":{"text":"Hallo <b>Welt</b> &amp; mehr"}}`,
			"<p>Hallo <b>Welt</b> &amp; mehr</p>\n"},
		{"paragraph leer", `{"type":"paragraph","data":{"text":"  "}}`, ""},
		{"header h2", `{"type":"header","data":{"text":"Unsere Öffnungszeiten","level":2}}`,
			`<h2 id="unsere-oeffnungszeiten">Unsere Öffnungszeiten</h2>` + "\n"},
		{"header h4", `{"type":"header","data":{"text":"<i>Kurz</i>","level":4}}`, `<h4 id="kurz"><i>Kurz</i></h4>` + "\n"},
		{"list unordered", `{"type":"list","data":{"style":"unordered","items":[{"content":"A","items":[]},{"content":"<b>B</b>","items":[{"content":"B1","items":[]}]}]}}`,
			"<ul><li>A</li><li><b>B</b><ul><li>B1</li></ul></li></ul>\n"},
		{"list ordered, String-Items", `{"type":"list","data":{"style":"ordered","items":["eins","zwei"]}}`,
			"<ol><li>eins</li><li>zwei</li></ol>\n"},
		{"quote", `{"type":"quote","data":{"text":"Gut <script>","caption":"Kundin","alignment":"left"}}`,
			"<figure><blockquote><p>Gut &lt;script&gt;</p></blockquote><figcaption>Kundin</figcaption></figure>\n"},
		{"quote ohne caption", `{"type":"quote","data":{"text":"Gut"}}`, "<blockquote><p>Gut</p></blockquote>\n"},
		{"delimiter", `{"type":"delimiter","data":{}}`, "<hr>\n"},
		{"image", `{"type":"image","data":{"file":{"url":"/media/werkstatt.webp"},"caption":"Unsere <b>Werkstatt</b>","alt":"Werkstatt \"innen\""}}`,
			`<figure><img src="/media/werkstatt.webp" alt="Werkstatt &#34;innen&#34;" loading="lazy"><figcaption>Unsere <b>Werkstatt</b></figcaption></figure>` + "\n"},
		{"image https", `{"type":"image","data":{"url":"https://cdn.example.at/a.jpg","alt":""}}`,
			`<figure><img src="https://cdn.example.at/a.jpg" alt="" loading="lazy"></figure>` + "\n"},
		{"image http ausgelassen", `{"type":"image","data":{"url":"http://example.at/a.jpg"}}`, ""},
		{"image javascript ausgelassen", `{"type":"image","data":{"url":"javascript:alert(1)"}}`, ""},
		{"image data ausgelassen", `{"type":"image","data":{"url":"data:image/svg+xml,<svg onload=alert(1)>"}}`, ""},
		{"image // ausgelassen", `{"type":"image","data":{"url":"//evil.example/a.jpg"}}`, ""},
		{"raw", `{"type":"raw","data":{"html":"<iframe src=\"https://maps.example\"></iframe>"}}`,
			`<iframe src="https://maps.example"></iframe>` + "\n"},
		{"faq", `{"type":"faq","data":{"items":[{"question":"Liefern Sie?","answer":"Ja, in <b>ganz</b> Österreich."},{"question":"","answer":"leer"}]}}`,
			`<section class="faq"><details><summary>Liefern Sie?</summary><div>Ja, in <b>ganz</b> Österreich.</div></details></section>` + "\n"},
		{"answer", `{"type":"answer","data":{"question":"Was kostet ein Tisch?","answer":"Ab 890 €.","details":"Je nach <i>Holz</i>."}}`,
			`<section class="answer"><h2 id="was-kostet-ein-tisch">Was kostet ein Tisch?</h2><p class="answer-short">Ab 890 €.</p><div class="answer-details"><p>Je nach <i>Holz</i>.</p></div></section>` + "\n"},
		{"cta", `{"type":"cta","data":{"text":"Jetzt anfragen","label":"Kontakt <>","url":"/kontakt"}}`,
			`<p class="cta">Jetzt anfragen <a class="cta-button" href="/kontakt">Kontakt &lt;&gt;</a></p>` + "\n"},
		{"cta extern", `{"type":"cta","data":{"text":"","label":"Termin","url":"https://cal.example.at"}}`,
			`<p class="cta"><a class="cta-button" href="https://cal.example.at" rel="noopener">Termin</a></p>` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Render(doc(tc.block), FormatBlocks)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("\n got:  %q\n want: %q", got, tc.want)
			}
		})
	}
}

func TestRenderAnchorsAndFAQ(t *testing.T) {
	r, err := RenderPage(doc(
		`{"type":"header","data":{"text":"Preise","level":2}}`,
		`{"type":"header","data":{"text":"Preise","level":3}}`,
		`{"type":"header","data":{"text":"Preise-2","level":3}}`,
		`{"type":"header","data":{"text":"!!!","level":2}}`,
		`{"type":"faq","data":{"items":[{"question":"<b>Wann?</b>","answer":"Mo&ndash;Fr"}]}}`,
		`{"type":"answer","data":{"question":"Wo?","answer":"In <a href=\"/graz\">Graz</a>."}}`,
	), FormatBlocks)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<h2 id="preise">`, `<h3 id="preise-2">Preise</h3>`, `<h3 id="preise-2-2">Preise-2</h3>`, `<h2 id="abschnitt">`} {
		if !strings.Contains(string(r.HTML), want) {
			t.Errorf("fehlt %s in\n%s", want, r.HTML)
		}
	}
	want := []FAQItem{{"Wann?", "Mo–Fr"}, {"Wo?", "In Graz."}}
	if !reflect.DeepEqual(r.FAQ, want) {
		t.Errorf("FAQ = %#v", r.FAQ)
	}
}

func TestRenderHTMLMode(t *testing.T) {
	got, err := Render(`<section><script src="/a.js"></script></section>`, FormatHTML)
	if err != nil || string(got) != `<section><script src="/a.js"></script></section>` {
		t.Errorf("HTML-Modus muss ungefiltert sein: %q %v", got, err)
	}
	if _, err := Render(doc(`{"type":"embed","data":{}}`), FormatBlocks); err == nil {
		t.Error("unbekannter Typ beim Rendern")
	}
}
