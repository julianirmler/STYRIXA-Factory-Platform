// Package media ist Modul 03 (mod_media): Mediathek mit virtuellen Ordnern,
// Alt-Texten und responsiven Varianten. Bilder werden im Browser verkleinert
// und umgewandelt (ADR-001); der Server prüft und speichert nur Dateien.
// Es wird nur über cmd/styrixa/mod_media.go (//go:build mod_media) importiert.
package media

import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed templates/*.html
var templates embed.FS

//go:embed static/*.js static/*.css
var static embed.FS

type module struct {
	d  core.Deps
	st *store
}

func (*module) ID() string         { return "media" }
func (*module) Requires() []string { return nil }
func (*module) Migrations() fs.FS {
	sub, _ := fs.Sub(migrations, "migrations")
	return sub
}

func (*module) Templates() fs.FS {
	sub, _ := fs.Sub(templates, "templates")
	return sub
}

func (*module) Assets() fs.FS {
	sub, _ := fs.Sub(static, "static")
	return sub
}

// AdminExtensions: Bildwähler für den Bild-Block des Editors (core.ExtEditor).
func (*module) AdminExtensions() map[string][]string {
	return map[string][]string{core.ExtEditor: {"media-upload.js", "media-picker.js", "media.css"}}
}

func (*module) AdminNav() []core.NavItem {
	return []core.NavItem{{Label: "Medien", Path: "/admin/media", Icon: "image", Role: core.RoleEditor, Order: 30}}
}

func (m *module) Mount(mux *http.ServeMux, d core.Deps) {
	m.d = d
	if d.DB != nil {
		m.st = &store{db: d.DB, dir: filepath.Join(d.DataDir, "media"), now: time.Now}
	}
	mux.HandleFunc("GET /admin/media", m.library)
	mux.HandleFunc("GET /admin/media/{$}", m.library)
	mux.HandleFunc("GET /admin/media/api", m.api)
	mux.HandleFunc("POST /admin/media/upload", m.upload)
	mux.HandleFunc("POST /admin/media/folders", m.createFolder)
	mux.HandleFunc("POST /admin/media/folders/{id}/rename", m.renameFolder)
	mux.HandleFunc("POST /admin/media/folders/{id}/delete", m.deleteFolder)
	mux.HandleFunc("GET /admin/media/files/{id}", m.fileDetail)
	mux.HandleFunc("POST /admin/media/files/{id}", m.updateFile)
	mux.HandleFunc("POST /admin/media/files/{id}/delete", m.deleteFile)
	mux.HandleFunc("GET /media/{file}", m.serve)
}

func init() { core.Register(&module{}) }
