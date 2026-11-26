package content

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Über uns", "ueber-uns"},
		{"Öffnungszeiten & Anfahrt", "oeffnungszeiten-anfahrt"},
		{"Straße", "strasse"},
		{"ÄÖÜ äöü", "aeoeue-aeoeue"},
		{"Café Crème – Brûlée", "cafe-creme-brulee"},
		{"Łódź, Škoda, Ærø", "lodz-skoda-aero"},
		{"  --Hallo!!   Welt?? ", "hallo-welt"},
		{"C++ & C#", "c-c"},
		{"2026: Neu", "2026-neu"},
		{"日本語", ""},
		{"", ""},
		{strings.Repeat("a", 100), strings.Repeat("a", 80)},
		{strings.Repeat("a", 79) + " b", strings.Repeat("a", 79)},
	} {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, erwartet %q", tc.in, got, tc.want)
		}
		if got := Slugify(tc.in); got != "" && !validSlug.MatchString(got) {
			t.Errorf("Slugify(%q) = %q ist kein gültiger Slug", tc.in, got)
		}
	}
	if got := withSuffix(strings.Repeat("a", 80), 12); len(got) != 80 || !strings.HasSuffix(got, "-12") {
		t.Errorf("withSuffix: %q", got)
	}
}

// Jede Route, die Kern oder Module registrieren, muss als erstes Segment
// reserviert sein – sonst könnte eine Seite sie verdecken (oder umgekehrt).
// Durchsucht die Quellen aller Module, auch nicht einkompilierter.
func TestReservedCoversRoutes(t *testing.T) {
	route := regexp.MustCompile(`HandleFunc\("(?:[A-Z]+ )?/([^/{"\s]+)`)
	pathConst := regexp.MustCompile(`(?m)^\s*Path\w*\s*=\s*"/([^/"]+)`)
	found := map[string]string{}
	for _, root := range []string{"../../../internal", "../../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, re := range []*regexp.Regexp{route, pathConst} {
				for _, m := range re.FindAllStringSubmatch(string(b), -1) {
					found[m[1]] = p
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, must := range []string{"admin", "healthz", "install", "cron", "shop"} {
		if found[must] == "" {
			t.Fatalf("Route /%s nicht gefunden – Suchmuster passt nicht mehr", must)
		}
	}
	for seg, file := range found {
		if !isReserved(seg) {
			t.Errorf("Route /%s (%s) fehlt in Reserved", seg, file)
		}
	}
}
