package content

import (
	"errors"
	"strings"
	"testing"
)

// XSS-Suite (Abnahme Session 3.1): Text wird escaped, gefährliche URLs
// entfernt, nur kanonische Allowlist-Tags kommen durch.
func TestSanitizeInline(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"Text", "Hallo Welt", "Hallo Welt"},
		{"Entities bleiben Text", "Fisch &amp; Chips &lt;b&gt;", "Fisch &amp; Chips &lt;b&gt;"},
		{"nbsp", "a&nbsp;b", "a b"},
		{"erlaubte Tags", "<b>f</b><strong>s</strong><i>k</i><em>e</em><u>u</u><mark>m</mark><code>c</code>x<br>y<br/>z<br />",
			"<b>f</b><strong>s</strong><i>k</i><em>e</em><u>u</u><mark>m</mark><code>c</code>x<br>y<br>z<br>"},
		{"Großschreibung", "<B>f</B>", "<b>f</b>"},
		{"Attribute fallen weg", `<mark class="cdx-marker" style="color:red" onclick="x()">m</mark>`, "<mark>m</mark>"},
		{"script", `<script>alert(1)</script>`, "&lt;script&gt;alert(1)&lt;/script&gt;"},
		{"img onerror", `<img src=x onerror=alert(1)>`, "&lt;img src=x onerror=alert(1)&gt;"},
		{"svg", `<svg onload=alert(1)>`, "&lt;svg onload=alert(1)&gt;"},
		{"style-Tag", `<style>*{}</style>`, "&lt;style&gt;*{}&lt;/style&gt;"},
		{"Link https", `<a href="https://example.at/x?a=1&amp;b=2">L</a>`, `<a href="https://example.at/x?a=1&amp;b=2" rel="noopener">L</a>`},
		{"Link http", `<a href="http://example.at">L</a>`, `<a href="http://example.at" rel="noopener">L</a>`},
		{"Link relativ", `<a href="/kontakt">K</a>`, `<a href="/kontakt">K</a>`},
		{"Link Anker", `<a href="#faq">F</a>`, `<a href="#faq">F</a>`},
		{"mailto", `<a href="mailto:office@example.at">M</a>`, `<a href="mailto:office@example.at">M</a>`},
		{"tel", `<a href="tel:+43316123">T</a>`, `<a href="tel:+43316123">T</a>`},
		{"target fällt weg", `<a href="/x" target="_blank" rel="opener">X</a>`, `<a href="/x">X</a>`},
		{"javascript", `<a href="javascript:alert(1)">X</a>`, "X"},
		{"Entity-verschleiert", `<a href="&#106;avascript:alert(1)">X</a>`, "X"},
		{"Hex-Entity", `<a href="&#x6A;&#x61;vascript:alert(1)">X</a>`, "X"},
		{"Groß/klein", `<a href="JaVaScRiPt:alert(1)">X</a>`, "X"},
		{"Tab im Schema", "<a href=\"java\tscript:alert(1)\">X</a>", "X"},
		{"Entity-Tab", `<a href="java&#9;script:alert(1)">X</a>`, "X"},
		{"Leerzeichen davor", `<a href="  javascript:alert(1)">X</a>`, "X"},
		{"Steuerzeichen", "<a href=\"\x01javascript:alert(1)\">X</a>", "X"},
		{"data", `<a href="data:text/html,<script>alert(1)</script>">X</a>`, "X"},
		{"vbscript", `<a href="vbscript:msgbox(1)">X</a>`, "X"},
		{"protokoll-relativ", `<a href="//evil.example">X</a>`, "X"},
		{"Backslash-Host", `<a href="/\evil.example">X</a>`, "X"},
		{"Tab zu //", "<a href=\"/\t/evil.example\">X</a>", "X"},
		{"ohne href", `<a>X</a>`, "X"},
		{"Attribut-Ausbruch", `<a href="https://example.at/"onmouseover="alert(1)">X</a>`, `<a href="https://example.at/" rel="noopener">X</a>`},
		{"Anführungszeichen im Wert", `<a href='/x"onmouseover="alert(1)'>X</a>`, `<a href="/x&#34;onmouseover=&#34;alert(1)">X</a>`},
		{"offenes Anführungszeichen", `<a href="/x>X</a>`, `&lt;a href=&#34;/x&gt;X`},
		{"unquoted", `<a href=/x>X</a>`, `<a href="/x">X</a>`},
		{"Tag mit Leerzeichen", `< b>x</b>`, "&lt; b&gt;x"},
		{"unvollständiges Tag", `<b`, "&lt;b"},
		{"nicht geschlossen", `<b>fett <i>kursiv`, "<b>fett <i>kursiv</i></b>"},
		{"verwaistes Schließ-Tag", `x</b></a>y`, "xy"},
		{"falsch verschachtelt", `<b><i>x</b>y</i>`, "<b><i>x</i></b>y"},
		{"Link in Link", `<a href="/a">1<a href="/b">2</a>3</a>`, `<a href="/a">1</a><a href="/b">2</a>3`},
		{"entfernter Link schließt nicht", `<b><a href="javascript:x">X</a></b>`, "<b>X</b>"},
		{"Schließ-Tag mit Attribut", `<b>x</b onclick=1>`, "<b>x&lt;/b onclick=1&gt;</b>"},
		{"tief verschachtelt", strings.Repeat("<b>", 3) + "x", "<b><b><b>x</b></b></b>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeInline(tc.in)
			if got != tc.want {
				t.Errorf("\n in:   %q\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
			// Unabhängig vom Erwartungswert: nie ausführbares Markup.
			l := strings.ToLower(got)
			for _, bad := range []string{"<script", "<img", "<svg", "<style", "javascript:", "vbscript:", "data:", " on", "style="} {
				if strings.Contains(l, bad) && !strings.Contains(tc.want, bad) {
					t.Errorf("gefährlich: %q in %q", bad, got)
				}
			}
		})
	}
}

// Raw-HTML und Blocktypen auf Block-Ebene.
func TestValidateSecurity(t *testing.T) {
	raw := `{"blocks":[{"type":"raw","data":{"html":"<script>x()</script>"}}]}`
	for _, tc := range []struct {
		name, body, format string
		admin              bool
		wantErr            string
		forbidden          bool
	}{
		{"Raw durch Admin", raw, FormatBlocks, true, "", false},
		{"Raw durch Editor", raw, FormatBlocks, false, "nur Administratoren", true},
		{"HTML-Modus durch Editor", "<p>x</p>", FormatHTML, false, "nur Administratoren", true},
		{"HTML-Modus durch Admin", "<p>x</p>", FormatHTML, true, "", false},
		{"Header Ebene 1", `{"blocks":[{"type":"header","data":{"text":"x","level":1}}]}`, FormatBlocks, true, "Ebenen 2 bis 4", false},
		{"Header Ebene 7", `{"blocks":[{"type":"header","data":{"text":"x","level":7}}]}`, FormatBlocks, true, "Ebenen 2 bis 4", false},
		{"unbekannter Typ", `{"blocks":[{"type":"embed","data":{"source":"https://x"}}]}`, FormatBlocks, true, "Unbekannter Blocktyp „embed“", false},
		{"ohne Typ", `{"blocks":[{"data":{}}]}`, FormatBlocks, true, "keinen Typ", false},
		{"kein JSON", `{"blocks":[`, FormatBlocks, true, "kein gültiges", false},
		{"falsche Daten", `{"blocks":[{"type":"header","data":{"level":"zwei"}}]}`, FormatBlocks, true, "ungültig", false},
		{"unbekanntes Format", `{}`, "markdown", true, "Unbekanntes Format", false},
		{"zu groß", `{"blocks":[],"x":"` + strings.Repeat("a", maxBodyBytes) + `"}`, FormatBlocks, true, "zu groß", false},
		{"zu viele Blöcke", `{"blocks":[` + strings.TrimSuffix(strings.Repeat(`{"type":"delimiter"},`, maxBlocks+1), ",") + `]}`, FormatBlocks, true, "Zu viele", false},
		{"Liste zu tief", `{"blocks":[{"type":"list","data":{"style":"unordered","items":[{"content":"1","items":[{"content":"2","items":[{"content":"3","items":[{"content":"4"}]}]}]}]}}]}`, FormatBlocks, true, "Ebenen tief", false},
		{"Liste Tiefe 3", `{"blocks":[{"type":"list","data":{"style":"unordered","items":[{"content":"1","items":[{"content":"2","items":[{"content":"3"}]}]}]}}]}`, FormatBlocks, true, "", false},
		{"Checkliste", `{"blocks":[{"type":"list","data":{"style":"checklist","items":[]}}]}`, FormatBlocks, true, "Listenart", false},
		{"CTA javascript", `{"blocks":[{"type":"cta","data":{"text":"x","label":"Los","url":"javascript:alert(1)"}}]}`, FormatBlocks, true, "nicht erlaubt", false},
		{"Bild mit Varianten", `{"blocks":[{"type":"image","data":{"url":"/media/a-960.webp","alt":"x","width":960,"height":640,"variants":[{"url":"/media/a-480.webp","width":480},{"url":"/media/a-960.webp","width":960}]}}]}`, FormatBlocks, true, "", false},
		{"Bild Variante extern", `{"blocks":[{"type":"image","data":{"url":"/media/a-960.webp","variants":[{"url":"https://evil.example/a.webp","width":480}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild Variante //", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"//evil.example/a.webp","width":480}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild Variante javascript", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"javascript:alert(1)","width":480}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild Variante mit Komma (srcset-Ausbruch)", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"/a.webp 1w, /b.webp","width":480}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild Variante Breite 0", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"/a.webp","width":0}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild Variante doppelt", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"/a.webp","width":480},{"url":"/b.webp","width":480}]}}]}`, FormatBlocks, true, "Bildgrößen sind ungültig", false},
		{"Bild zu viele Varianten", `{"blocks":[{"type":"image","data":{"url":"/a","variants":[{"url":"/1","width":1},{"url":"/2","width":2},{"url":"/3","width":3},{"url":"/4","width":4}]}}]}`, FormatBlocks, true, "höchstens 3", false},
		{"Bild nur Breite", `{"blocks":[{"type":"image","data":{"url":"/a","width":960}}]}`, FormatBlocks, true, "Bildmaße", false},
		{"Bild negative Höhe", `{"blocks":[{"type":"image","data":{"url":"/a","width":960,"height":-1}}]}`, FormatBlocks, true, "Bildmaße", false},
		{"Answer ohne Antwort", `{"blocks":[{"type":"answer","data":{"question":"Was?"}}]}`, FormatBlocks, true, "Frage und Kurzantwort", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.body, tc.format, tc.admin)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unerwarteter Fehler: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Fehler %v, erwartet %q", err, tc.wantErr)
			}
			ie, ok := IsInputError(err)
			if !ok || ie.Forbidden != tc.forbidden {
				t.Errorf("InputError=%v Forbidden=%v", ok, ie != nil && ie.Forbidden)
			}
			var target *InputError
			if !errors.As(err, &target) {
				t.Error("kein InputError")
			}
		})
	}
}
