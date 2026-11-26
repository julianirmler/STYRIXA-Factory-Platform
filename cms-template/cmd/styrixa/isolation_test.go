package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Abnahme Session 1.4: Das Admin-Template eines Moduls steckt nur dann im
// Binary, wenn sein Build-Tag gesetzt ist (ADR-002). Baut echte Binaries.
func TestAdminTemplateBuildTagIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("baut mehrere Binaries")
	}
	const (
		shopMarker    = `data-styrixa-template="shop-admin-placeholder"`
		shopTmpl      = "../../internal/modules/shop/templates/placeholder.html"
		contentMarker = `data-styrixa-template="content-list"`
		contentTmpl   = "../../internal/modules/content/templates/list.html"
		editorMarker  = "window.StyrixaBlocks" // eigene Editor.js-Blöcke (Session 3.2)
		editorJS      = "../../internal/modules/content/static/editor-blocks.js"
		coreMarker    = "Dieser Bereich ist noch in Arbeit" // Kern-Template: immer enthalten
	)
	for tmpl, marker := range map[string]string{shopTmpl: shopMarker, contentTmpl: contentMarker, editorJS: editorMarker} {
		src, err := os.ReadFile(tmpl)
		if err != nil || !bytes.Contains(src, []byte(marker)) {
			t.Fatalf("Marker fehlt in %s (Test an das Template anpassen): %v", tmpl, err)
		}
	}

	// Auch Pflichtmodule sind isoliert: ohne mod_content kein Content-Code.
	const req = "mod_content mod_dashboard"
	for _, tc := range []struct {
		tags                  string
		wantShop, wantContent bool
	}{
		{"", false, false},
		{req, false, true},
		{req + " mod_media", false, true},
		{req + " mod_media mod_shop", true, true},
		{"mod_media mod_shop", true, false},
	} {
		t.Run("tags="+tc.tags, func(t *testing.T) {
			t.Parallel()
			bin := filepath.Join(t.TempDir(), "styrixa")
			cmd := exec.Command("go", "build", "-trimpath", "-tags", tc.tags, "-o", bin, ".")
			cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
			b, err := os.ReadFile(bin)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(b, []byte(coreMarker)) {
				t.Fatal("Kern-Template fehlt im Binary – Suche wäre ohne Aussagekraft")
			}
			if got := bytes.Contains(b, []byte(shopMarker)); got != tc.wantShop {
				t.Errorf("Shop-Template im Binary: %v, erwartet %v", got, tc.wantShop)
			}
			if got := bytes.Contains(b, []byte(contentMarker)); got != tc.wantContent {
				t.Errorf("Content-Template im Binary: %v, erwartet %v", got, tc.wantContent)
			}
			if got := bytes.Contains(b, []byte(editorMarker)); got != tc.wantContent {
				t.Errorf("Editor-Skripte im Binary: %v, erwartet %v", got, tc.wantContent)
			}
		})
	}
}
