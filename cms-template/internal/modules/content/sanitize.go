package content

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// Inline-Sanitizer für Editor.js-Text (Session 3.1, Herzstück der XSS-Abwehr).
//
// Kein HTML-Parser als Abhängigkeit (ADR-009) und kein Blocklist-Ansatz:
// Alles wird escaped; nur Tags, die exakt einem erlaubten Muster folgen,
// werden in kanonischer Form ohne Attribute neu geschrieben. Der einzige
// erhaltene Attributwert ist ein geprüfter href. Ein Browser sieht also nur
// Markup, das dieser Code selbst erzeugt hat.

// inlineTags sind die erlaubten Inline-Tags; br ist leer (kein Schließ-Tag).
var inlineTags = map[string]bool{
	"b": true, "strong": true, "i": true, "em": true, "u": true,
	"mark": true, "code": true, "br": true, "a": true,
}

// SanitizeInline liefert sicheres Inline-HTML. Unbekannte oder kaputte Tags
// bleiben als Text sichtbar (escaped), Attribute außer href fallen weg, offene
// Tags werden geschlossen, verwaiste Schließ-Tags entfernt.
func SanitizeInline(s string) string {
	var out strings.Builder
	var stack []string
	text := 0 // Beginn des noch nicht geschriebenen Texts

	flush := func(end int) {
		if end > text {
			// Editor.js liefert innerHTML: Entities erst lösen, dann einheitlich escapen.
			out.WriteString(html.EscapeString(html.UnescapeString(s[text:end])))
		}
	}
	closeTo := func(i int) { // schließt stack[i:] von innen nach außen
		for k := len(stack) - 1; k >= i; k-- {
			out.WriteString("</" + stack[k] + ">")
		}
		stack = stack[:i]
	}
	find := func(name string) int {
		for k := len(stack) - 1; k >= 0; k-- {
			if stack[k] == name {
				return k
			}
		}
		return -1
	}

	for i := 0; i < len(s); {
		if s[i] != '<' {
			i++
			continue
		}
		t, n, ok := parseTag(s[i:])
		if !ok || !inlineTags[t.name] {
			i++ // „<“ ist Text und wird escaped
			continue
		}
		flush(i)
		i += n
		text = i
		switch {
		case t.name == "br":
			if !t.closing {
				out.WriteString("<br>")
			}
		case t.closing:
			if k := find(t.name); k >= 0 {
				closeTo(k) // dazwischen offene Tags mit schließen
			} // sonst verwaist: entfernen
		case t.name == "a":
			if k := find("a"); k >= 0 {
				closeTo(k) // keine Links in Links
			}
			href, external, ok := safeHref(t.attrs["href"])
			if !ok {
				continue // Link entfernen, Text behalten; </a> ist dann verwaist
			}
			out.WriteString(`<a href="` + html.EscapeString(href) + `"`)
			if external {
				out.WriteString(` rel="noopener"`)
			}
			out.WriteString(">")
			stack = append(stack, "a")
		default:
			out.WriteString("<" + t.name + ">")
			stack = append(stack, t.name)
		}
	}
	flush(len(s))
	closeTo(0)
	return out.String()
}

type tag struct {
	name    string
	closing bool
	attrs   map[string]string // Namen klein; erster Wert gewinnt
}

// parseTag erkennt am Anfang von s ein Tag der Form <name attr="…" …> oder
// </name>. Alles, was davon abweicht (Leerzeichen nach „<“, fehlendes „>“,
// offenes Anführungszeichen …), ist kein Tag.
func parseTag(s string) (t tag, n int, ok bool) {
	i := 1
	if i < len(s) && s[i] == '/' {
		t.closing = true
		i++
	}
	start := i
	for i < len(s) && isAlnum(s[i]) {
		i++
	}
	if i == start || !isLetter(s[start]) {
		return t, 0, false
	}
	t.name = strings.ToLower(s[start:i])
	t.attrs = map[string]string{}
	for {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return t, 0, false
		}
		switch {
		case s[i] == '>':
			return t, i + 1, true
		case s[i] == '/' && i+1 < len(s) && s[i+1] == '>':
			return t, i + 2, true
		case t.closing:
			return t, 0, false // Schließ-Tags ohne Attribute
		}
		as := i
		for i < len(s) && !isSpace(s[i]) && !strings.ContainsRune(`=>/"'<`, rune(s[i])) {
			i++
		}
		if i == as {
			return t, 0, false
		}
		name := strings.ToLower(s[as:i])
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		val := ""
		if i < len(s) && s[i] == '=' {
			i++
			for i < len(s) && isSpace(s[i]) {
				i++
			}
			if i >= len(s) {
				return t, 0, false
			}
			if q := s[i]; q == '"' || q == '\'' {
				end := strings.IndexByte(s[i+1:], q)
				if end < 0 {
					return t, 0, false
				}
				val = s[i+1 : i+1+end]
				i += end + 2
			} else {
				vs := i
				for i < len(s) && !isSpace(s[i]) && s[i] != '>' {
					i++
				}
				val = s[vs:i]
			}
		}
		if _, dup := t.attrs[name]; !dup {
			t.attrs[name] = val
		}
	}
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isAlnum(c byte) bool  { return isLetter(c) || c >= '0' && c <= '9' }
func isSpace(c byte) bool  { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// safeHref prüft einen href gegen die Allowlist: https://, http://, mailto:,
// tel:, relativ ab „/“ (nicht „//“ oder „/\“ – das wären fremde Hosts) oder „#“.
// Vor der Prüfung werden Entities gelöst und Leer- sowie Steuerzeichen
// entfernt; zurückgegeben wird genau der geprüfte Wert. So sieht der Browser
// dieselbe URL wie die Prüfung, egal wie sie verschleiert war.
func safeHref(raw string) (href string, external, ok bool) {
	v := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) {
			return -1
		}
		return r
	}, html.UnescapeString(raw))
	l := strings.ToLower(v)
	switch {
	case v == "":
		return "", false, false
	case strings.HasPrefix(l, "https://"), strings.HasPrefix(l, "http://"):
		return v, true, true
	case strings.HasPrefix(l, "mailto:"), strings.HasPrefix(l, "tel:"):
		return v, false, true
	case strings.HasPrefix(v, "#"):
		return v, false, true
	case strings.HasPrefix(v, "/") && !strings.HasPrefix(v, "//") && !strings.HasPrefix(v, `/\`):
		return v, false, true
	}
	return "", false, false
}

// safeImageURL: nur relativ ab „/“ (z. B. /media/…) oder https://.
func safeImageURL(raw string) (string, bool) {
	v, _, ok := safeHref(raw)
	l := strings.ToLower(v)
	if !ok || !(strings.HasPrefix(l, "https://") || strings.HasPrefix(v, "/")) {
		return "", false
	}
	return v, true
}

var tagRE = regexp.MustCompile(`<[^>]*>`)

// plainText macht aus bereinigtem Inline-HTML reinen Text (für JSON-LD und
// Anker). Nur auf Ausgaben von SanitizeInline anwenden.
func plainText(sanitized string) string {
	return strings.TrimSpace(html.UnescapeString(tagRE.ReplaceAllString(sanitized, "")))
}
