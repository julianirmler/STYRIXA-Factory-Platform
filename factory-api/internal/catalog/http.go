package catalog

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Routes hängt die Katalog-Endpunkte an mux:
//
//	GET /api/catalog                                         → {release, modules, targets}
//	GET /api/packages/select?modules=media,shop&target=linux/amd64 → Package + downloadUrl
//	GET /api/packages/{id}/download                          → geprüftes tar.gz
func (c *Catalog) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/catalog", c.handleCatalog)
	mux.HandleFunc("GET /api/packages/select", c.handleSelect)
	mux.HandleFunc("GET /api/packages/{id}/download", c.handleDownload)
}

func (c *Catalog) handleCatalog(w http.ResponseWriter, r *http.Request) {
	tag, mods, targets, err := c.Modules(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, map[string]any{"release": tag, "modules": mods, "targets": targets})
}

func (c *Catalog) handleSelect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var mods []string
	for _, m := range strings.Split(q.Get("modules"), ",") {
		if m = strings.TrimSpace(m); m != "" {
			mods = append(mods, m)
		}
	}
	p, err := c.Select(r.Context(), mods, q.Get("target"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, struct {
		Package
		DownloadURL string `json:"downloadUrl"`
	}{p, "/api/packages/" + p.ID + "/download"})
}

func (c *Catalog) handleDownload(w http.ResponseWriter, r *http.Request) {
	p, err := c.Lookup(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	path, err := c.Open(r.Context(), p)
	if err != nil {
		slog.Error("package rejected", "package", p.ID, "err", err)
		writeErr(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+p.File+`"`)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", `"`+p.SHA256+`"`)
	http.ServeContent(w, r, p.File, time.Time{}, f)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(v)
}

// writeErr übersetzt Fehler in Status + deutsche Meldung ohne interne Details.
func writeErr(w http.ResponseWriter, err error) {
	var sel *SelectionError
	switch {
	case errors.As(err, &sel):
		http.Error(w, sel.Msg, http.StatusUnprocessableEntity)
	case errors.Is(err, ErrNotFound):
		http.Error(w, "Paket nicht gefunden", http.StatusNotFound)
	case errors.Is(err, ErrUnavailable):
		http.Error(w, "Katalog derzeit nicht verfügbar", http.StatusServiceUnavailable)
	case errors.Is(err, ErrTampered):
		http.Error(w, "Paket konnte nicht geprüft werden", http.StatusBadGateway)
	default:
		slog.Error("catalog request failed", "err", err)
		http.Error(w, "Paket derzeit nicht verfügbar", http.StatusBadGateway)
	}
}
