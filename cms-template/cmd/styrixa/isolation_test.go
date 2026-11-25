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
		shopMarker = `data-styrixa-template="shop-admin-placeholder"`
		shopTmpl   = "../../internal/modules/shop/templates/placeholder.html"
		coreMarker = "Dieser Bereich ist noch in Arbeit" // Kern-Template: immer enthalten
	)
	src, err := os.ReadFile(shopTmpl)
	if err != nil || !bytes.Contains(src, []byte(shopMarker)) {
		t.Fatalf("Marker fehlt in %s (Test an das Template anpassen): %v", shopTmpl, err)
	}

	for _, tc := range []struct {
		tags     string
		wantShop bool
	}{
		{"", false},
		{"mod_media", false},
		{"mod_media mod_shop", true},
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
				t.Fatalf("Shop-Template im Binary: %v, erwartet %v", got, tc.wantShop)
			}
		})
	}
}
