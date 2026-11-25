// Package ui liefert das gebaute factory-ui (Vite-dist) aus demselben
// Ursprung wie die API aus – dadurch braucht die API keine CORS-Header.
package ui

import (
	"net/http"
	"os"
	"strings"
)

// CSP erlaubt nur eigene Ressourcen; Bilder zusätzlich als data:-URL.
const CSP = "default-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Handler liefert die Dateien aus dir aus. os.DirFS lehnt Pfade mit ".."
// ab, Verzeichnislisten gibt es nicht. /api/ bleibt der API vorbehalten.
func Handler(dir string) http.Handler {
	files := http.FileServerFS(os.DirFS(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") || (p != "/" && strings.HasSuffix(p, "/")) {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", CSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(p, "/assets/") {
			// Vite hängt einen Inhalts-Hash an den Dateinamen
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
