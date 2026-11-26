// Package content ist Pflichtmodul 02 (mod_content): Seiten mit Hierarchie,
// Revisionen und Entwürfen, Editor.js-Blöcke, XSS-sicherer Renderer, Slugs
// mit Weiterleitungen. Es wird nur über cmd/styrixa/mod_content.go
// (//go:build mod_content) importiert.
package content

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Admin-Templates nur im Content-Paket: ohne mod_content nicht im Binary (ADR-002).
//
//go:embed templates/*.html
var templates embed.FS

// Editor.js (vendort, siehe VENDOR.md) und die eigenen Blöcke – ebenfalls nur
// mit mod_content im Binary.
//
//go:embed static/*.js static/*.css
var static embed.FS

type module struct {
	d  core.Deps
	st *store
}

func (*module) ID() string         { return "content" }
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

func (*module) AdminNav() []core.NavItem {
	return []core.NavItem{{Label: "Seiten", Path: "/admin/content", Icon: "file-text", Role: core.RoleEditor, Order: 10}}
}

func (m *module) Mount(mux *http.ServeMux, d core.Deps) {
	m.d = d
	if d.DB != nil {
		m.st = &store{db: d.DB}
	}
	mux.HandleFunc("GET /admin/content", m.list)
	mux.HandleFunc("GET /admin/content/{$}", m.list)
	mux.HandleFunc("POST /admin/content/pages", m.create)
	mux.HandleFunc("GET /admin/content/pages/{id}", m.edit)
	mux.HandleFunc("POST /admin/content/pages/{id}/draft", m.draft)
	mux.HandleFunc("POST /admin/content/pages/{id}/settings", m.saveSettings)
	mux.HandleFunc("POST /admin/content/pages/{id}/publish", m.action((*store).publish))
	mux.HandleFunc("POST /admin/content/pages/{id}/unpublish", m.action((*store).unpublish))
	mux.HandleFunc("POST /admin/content/pages/{id}/delete", m.remove)
	mux.HandleFunc("GET /admin/content/pages/{id}/preview", m.preview)
	mux.HandleFunc("POST /admin/content/pages/{id}/format", m.switchFormat)
	mux.HandleFunc("POST /admin/content/pages/{id}/revisions/{rev}/restore", m.restore)
	// Öffentliche Seiten liefert der Kern über PageSource (PublicPage).
}

// PublicPage implementiert core.PageSource: Startseite, /pfad/zur/seite und
// 301 für alte Pfade. Entwürfe sind nie öffentlich.
func (m *module) PublicPage(ctx context.Context, path string) (core.PublicPage, bool, error) {
	if m.st == nil {
		return core.PublicPage{}, false, nil
	}
	if path == "" {
		x, ok, err := m.st.home(ctx)
		if !ok || err != nil {
			return core.PublicPage{}, false, err
		}
		return view(x, "/")
	}
	clean := strings.Trim(path, "/")
	if !plausiblePath(clean) {
		return core.PublicPage{}, false, nil // erspart die Datenbank
	}
	x, redirect, ok, err := m.st.lookup(ctx, clean)
	if !ok || err != nil {
		return core.PublicPage{}, false, err
	}
	canonical := "/" + x.Path
	if x.IsHome {
		canonical = "/"
	}
	if redirect || "/"+path != canonical {
		return core.PublicPage{RedirectTo: canonical}, true, nil
	}
	return view(x, canonical)
}

// plausiblePath: 1–5 Segmente, jedes ein gültiger Slug (Groß/klein egal –
// abweichende Schreibung leitet auf die kanonische um).
func plausiblePath(p string) bool {
	if p == "" || len(p) > maxPathLen {
		return false
	}
	segs := strings.Split(p, "/")
	if len(segs) > maxDepth {
		return false
	}
	for _, s := range segs {
		if !validSlug.MatchString(strings.ToLower(s)) {
			return false
		}
	}
	return true
}

func view(x published, canonical string) (core.PublicPage, bool, error) {
	body, err := Render(x.Body, x.Format)
	if err != nil {
		return core.PublicPage{}, false, err
	}
	return core.PublicPage{View: core.PublicView{Title: x.RevTitle, Body: body, Path: canonical}}, true, nil
}

func init() {
	core.Register(&module{})
	core.RegisterJob("content.prune_revisions", 24*time.Hour, func(ctx context.Context, d core.Deps) error {
		_, err := (&store{db: d.DB}).prune(ctx)
		return err
	})
}
