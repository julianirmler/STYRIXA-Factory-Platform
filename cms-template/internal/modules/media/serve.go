package media

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// serve liefert eine Variante öffentlich aus: GET /media/<key>-<breite>.<ext>.
// Der Name ist zufällig und ändert sich nie → Browser cachen für immer. Der
// Inhaltstyp kommt aus der Datenbank (geprüft beim Upload, nosniff); direkt
// als Dokument geöffnet läuft darin nichts (CSP sandbox).
func (m *module) serve(w http.ResponseWriter, r *http.Request) {
	match := fileRE.FindStringSubmatch(r.PathValue("file"))
	if match == nil || m.st == nil {
		http.NotFound(w, r)
		return
	}
	width, _ := strconv.Atoi(match[2])
	mime, ok, err := m.st.lookup(r.Context(), match[1], width, match[3])
	if err != nil {
		slog.Error("media: Auslieferung", "err", err)
		http.Error(w, "Interner Fehler.", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(m.st.dir, match[0]))
	if errors.Is(err, os.ErrNotExist) {
		slog.Warn("media: Datei fehlt im Datenordner", "file", match[0])
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("media: Datei nicht lesbar", "err", err)
		http.Error(w, "Interner Fehler.", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "Interner Fehler.", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}
