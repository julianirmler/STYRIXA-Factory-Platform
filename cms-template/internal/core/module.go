// Package core enthält den immer kompilierten Kern des STYRIXA CMS.
// Module melden sich über Register() an; der Kern kennt keine Module beim Namen.
package core

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
)

// NavItem ist ein Eintrag in der Admin-Navigation.
type NavItem struct {
	Label string
	Path  string
	Icon  string // lucide-Name, z. B. "shopping-cart"
	Role  string // Mindestrolle: RoleEditor = auch Editoren; leer = nur Admins
	Order int
}

// Deps sind die Abhängigkeiten, die der Kern jedem Modul übergibt.
type Deps struct {
	DB       *sql.DB
	Settings Settings        // Einstellungen aus core_settings; nil nur in Tests
	Admin    AdminRenderer   // Admin-Layout für Modulseiten; nil nur in Tests
	Public   PublicPreviewer // öffentliches Layout für Vorschauen (ab 3.3); nil nur in Tests
	DataDir  string          // Datenordner (STYRIXA_DATA) für Dateien der Module, z. B. Medien (ab 4.1)
	Modules  []Module        // alle Module im Binary – für Typ-Assertions auf Kern-Schnittstellen (ab 4.1)
}

// MediaReferrer implementiert ein Modul, das Medien-URLs in eigenen Inhalten
// verwendet (Content, Session 4.1). Die Mediathek fragt vor dem Löschen nach,
// wo eine Datei noch vorkommt. url ist ein Pfad wie „/media/<key>-“ (Präfix);
// das Ergebnis sind lesbare Fundstellen, z. B. Seitentitel.
type MediaReferrer interface {
	MediaRefs(ctx context.Context, urlPrefix string) ([]string, error)
}

// Module ist der Vertrag zwischen Kern und Modul.
type Module interface {
	ID() string                       // "shop"
	Requires() []string               // andere Modul-IDs, z. B. {"media"}
	Migrations() fs.FS                // *.sql, lexikografisch sortiert ausgeführt
	Mount(mux *http.ServeMux, d Deps) // Admin- und Public-Routen
	AdminNav() []NavItem
}

var registry = map[string]Module{}

// Register wird aus init() der (build-tag-geschützten) Modul-Pakete aufgerufen.
func Register(m Module) {
	if m.ID() == CoreID {
		panic("styrixa: Modul-ID „core“ ist für den Kern reserviert")
	}
	if _, dup := registry[m.ID()]; dup {
		panic("styrixa: Modul doppelt registriert: " + m.ID())
	}
	registry[m.ID()] = m
}

// Has meldet, ob ein Modul im Binary enthalten ist.
func Has(id string) bool { _, ok := registry[id]; return ok }

// Modules liefert alle Module in stabiler Reihenfolge und prüft Abhängigkeiten.
func Modules() ([]Module, error) {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Module, 0, len(ids))
	for _, id := range ids {
		m := registry[id]
		for _, dep := range m.Requires() {
			if !Has(dep) {
				return nil, fmt.Errorf("modul %q benötigt %q (build-tag mod_%s fehlt)", id, dep, dep)
			}
		}
		out = append(out, m)
	}
	return out, nil
}
