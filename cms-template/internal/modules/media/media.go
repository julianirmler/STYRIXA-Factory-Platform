// Package media – Platzhalter, damit mod_shop+mod_media baut. Wird im Media-Schritt ausgebaut.
package media

import (
	"io/fs"
	"net/http"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

type module struct{}

func (module) ID() string                      { return "media" }
func (module) Requires() []string              { return nil }
func (module) Migrations() fs.FS               { return nil } // noch keine Tabellen
func (module) Mount(*http.ServeMux, core.Deps) {}
func (module) AdminNav() []core.NavItem {
	return []core.NavItem{{Label: "Medien", Path: "/admin/media", Icon: "image", Order: 30}}
}

func init() { core.Register(module{}) }
