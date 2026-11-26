package content

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	maxSlugLen = 80  // je Segment
	maxPathLen = 255 // voller Pfad a/b/c
	maxDepth   = 5   // Ebenen
	fallback   = "seite"
)

// Reserved sind erste Pfadsegmente, die Kern oder Module selbst belegen. Eine
// Seite auf oberster Ebene darf keinen dieser Slugs tragen (slug_test.go prüft,
// dass jede registrierte Route hier steht). Unterseiten sind nicht betroffen.
var Reserved = []string{
	"admin", "install", "healthz", "cron", "api", "static", "assets",
	"media", "shop", "feed", "robots.txt", "sitemap.xml",
}

func isReserved(slug string) bool {
	for _, r := range Reserved {
		if strings.EqualFold(slug, r) {
			return true
		}
	}
	return false
}

// validSlug: ein Segment aus [a-z0-9] mit einzelnen „-“ dazwischen.
var validSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// translit bildet Buchstaben mit Akzent auf ASCII ab (nach strings.ToLower).
var translit = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'ß': "ss",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a", 'æ': "ae",
	'ç': "c", 'ć': "c", 'č': "c", 'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ī': "i", 'į': "i", 'ı': "i",
	'ł': "l", 'ľ': "l", 'ĺ': "l", 'ñ': "n", 'ń': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ø': "o", 'ō': "o", 'ő': "o", 'œ': "oe",
	'ŕ': "r", 'ř': "r", 'ś': "s", 'š': "s", 'ş': "s", 'ș': "s", 'ť': "t", 'ţ': "t", 'ț': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ū': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ý': "y", 'ÿ': "y", 'ź': "z", 'ż': "z", 'ž': "z", 'þ': "th", 'ð': "d",
}

// Slugify macht aus einem Titel ein URL-Segment: klein, Umlaute
// ausgeschrieben, Akzente entfernt, nur [a-z0-9-], höchstens 80 Zeichen.
// Liefert "" für Titel ohne verwertbare Zeichen.
func Slugify(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		var s string
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			s = string(r)
		case translit[r] != "":
			s = translit[r]
		default:
			dash = b.Len() > 0
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(s)
	}
	out := b.String()
	if len(out) > maxSlugLen {
		out = strings.TrimRight(out[:maxSlugLen], "-")
	}
	return out
}

// withSuffix hängt „-n“ an und hält die Längengrenze ein (n ≥ 2).
func withSuffix(slug string, n int) string {
	suf := "-" + strconv.Itoa(n)
	if len(slug)+len(suf) > maxSlugLen {
		slug = strings.TrimRight(slug[:maxSlugLen-len(suf)], "-")
	}
	return slug + suf
}

// anchors vergibt stabile Anker-IDs für Überschriften (Duplikate -2, -3 …).
type anchors map[string]int

func (a anchors) id(text string) string {
	base := Slugify(text)
	if base == "" {
		base = "abschnitt"
	}
	a[base]++
	if n := a[base]; n > 1 {
		id := withSuffix(base, n)
		for a[id] > 0 { // „x-2“ gab es schon als eigene Überschrift
			a[base]++
			id = withSuffix(base, a[base])
		}
		a[id]++
		return id
	}
	return base
}
