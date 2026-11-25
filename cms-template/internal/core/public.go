package core

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
)

// Öffentliches Rendering (Session 1.5): Startseite, 404 und 500 als Templates.
// Die Gestaltung kommt aus einem Theme-Modul (PublicTheme, Design/Themes in
// 4.5/4.6); was es nicht liefert, kommt aus den Standard-Templates des Kerns.

//go:embed public/templates/*.html
var publicFS embed.FS

// Namen der öffentlichen Templates. Ein Theme darf jede Teilmenge liefern.
const (
	PublicLayout   = "layout.html" // Rahmen; ruft {{template "content" .}}
	PublicHome     = "home.html"
	PublicNotFound = "404.html"
	PublicError    = "500.html"
)

var publicPages = []string{PublicHome, PublicNotFound, PublicError}

// PublicTheme implementiert ein Modul, das die öffentlichen Seiten gestaltet.
// PublicTemplates liefert Dateien mit den Namen oben (im Wurzelordner); eine
// Seite ist der Inhalt („content“), layout.html der Rahmen. Fehlende Dateien
// ergänzt der Kern. Höchstens ein Modul im Binary darf ein Theme liefern.
type PublicTheme interface {
	PublicTemplates() fs.FS
}

// PublicView ist die Daten-Schnittstelle der öffentlichen Templates – also
// auch der Vertrag mit Themes. Felder nur hinzufügen, nie umbenennen.
type PublicView struct {
	Lang      string // de-AT | de-DE | de-CH (aus site.region)
	SiteTitle string
	Title     string // Seitentitel; leer = Startseite
	Status    int
	Data      any
}

// Public rendert die öffentlichen Seiten. Templates werden einmal beim Start
// geparst – sie stammen aus dem Binary, sind also kein veraltender Zustand.
type Public struct {
	settings Settings
	pages    map[string]*template.Template
}

// NewPublic baut die Templates aus Kern-Standard und höchstens einem Theme.
func NewPublic(settings Settings, mods []Module) (*Public, error) {
	coreFS, _ := fs.Sub(publicFS, "public/templates")
	var theme fs.FS
	var themeID string
	for _, m := range mods {
		pt, ok := m.(PublicTheme)
		if !ok {
			continue
		}
		if themeID != "" {
			return nil, fmt.Errorf("public: Module %q und %q liefern beide ein Theme", themeID, m.ID())
		}
		theme, themeID = pt.PublicTemplates(), m.ID()
	}

	// pick liefert die Datei aus dem Theme, sonst aus dem Kern.
	pick := func(name string) (string, string, error) {
		if theme != nil {
			b, err := fs.ReadFile(theme, name)
			if err == nil {
				return string(b), themeID + "/" + name, nil
			}
			if !errors.Is(err, fs.ErrNotExist) {
				return "", "", fmt.Errorf("public: Theme %s: %w", themeID, err)
			}
		}
		b, err := fs.ReadFile(coreFS, name)
		return string(b), "core/" + name, err
	}

	src, origin, err := pick(PublicLayout)
	if err != nil {
		return nil, err
	}
	layout, err := template.New(PublicLayout).Parse(src)
	if err != nil {
		return nil, fmt.Errorf("public: %s: %w", origin, err)
	}
	p := &Public{settings: settings, pages: map[string]*template.Template{}}
	for _, name := range publicPages {
		src, origin, err := pick(name)
		if err != nil {
			return nil, err
		}
		t, err := layout.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := t.New("content").Parse(src); err != nil {
			return nil, fmt.Errorf("public: %s: %w", origin, err)
		}
		p.pages[name] = t
	}
	return p, nil
}

// Mount registriert die Startseite und den Auffang-Handler für alles, was
// keine andere Route trifft (→ 404).
func (p *Public) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", p.home)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { p.Error(w, r, http.StatusNotFound) })
}

func (p *Public) home(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, PublicHome, "")
}

// Error antwortet mit der 404- oder 500-Seite (andere Status: 500-Seite mit
// diesem Status). Unter /admin/* ohne Theme, als schlichter Text.
func (p *Public) Error(w http.ResponseWriter, r *http.Request, status int) {
	if isAdminPath(r.URL.Path) {
		msg := "Seite nicht gefunden."
		if status != http.StatusNotFound {
			msg = msgInternal
		}
		http.Error(w, msg, status)
		return
	}
	name, title := PublicError, "Interner Fehler"
	if status == http.StatusNotFound {
		name, title = PublicNotFound, "Seite nicht gefunden"
	}
	p.render(w, r, status, name, title)
}

func (p *Public) render(w http.ResponseWriter, r *http.Request, status int, name, title string) {
	ctx := r.Context()
	v := PublicView{Lang: Regions[0], Title: title, Status: status}
	if p.settings != nil {
		all, err := p.settings.GetAll(ctx)
		if err != nil {
			slog.Error("public: Settings nicht lesbar", "err", err)
			p.fail(w, r)
			return
		}
		v.SiteTitle = all[KeySiteTitle]
		if ValidRegion(all[KeySiteRegion]) {
			v.Lang = all[KeySiteRegion]
		}
	}
	var buf bytes.Buffer // erst komplett rendern: bei Fehlern eine saubere 500
	if err := p.pages[name].ExecuteTemplate(&buf, PublicLayout, v); err != nil {
		slog.Error("public: Template", "template", name, "err", err)
		p.fail(w, r)
		return
	}
	cacheControl := "no-cache"
	if status >= 500 {
		cacheControl = "no-store"
	}
	writeHTML(w, r, status, cacheControl, buf.Bytes())
}

// fail rendert die 500-Seite ohne Settings. Scheitert auch sie (Theme-Fehler),
// bleibt schlichter Text – nie Details nach außen.
func (p *Public) fail(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	v := PublicView{Lang: Regions[0], Title: "Interner Fehler", Status: http.StatusInternalServerError}
	if err := p.pages[PublicError].ExecuteTemplate(&buf, PublicLayout, v); err != nil {
		slog.Error("public: 500-Template", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	writeHTML(w, r, http.StatusInternalServerError, "no-store", buf.Bytes())
}

// writeHTML schreibt eine HTML-Antwort. Bei 200 mit ETag (SHA-256 des Körpers,
// gekürzt); passt If-None-Match, antwortet sie mit 304 ohne Körper.
func writeHTML(w http.ResponseWriter, r *http.Request, status int, cacheControl string, body []byte) {
	h := w.Header()
	h.Set("Cache-Control", cacheControl)
	if status == http.StatusOK {
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:8]) + `"`
		h.Set("ETag", etag)
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && etagMatch(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

// etagMatch: schwacher Vergleich nach RFC 9110 §13.1.2 (If-None-Match).
func etagMatch(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == etag {
			return true
		}
	}
	return false
}

// Recover fängt Panics ab und antwortet mit der 500-Seite (ohne Details).
// Im CGI-Modus würde der Prozess sonst ohne Antwort abbrechen.
func (p *Public) Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			slog.Error("panic", "path", r.URL.Path, "err", v, "stack", string(debug.Stack()))
			p.Error(w, r, http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders setzt die Sicherheits-Header für alle Antworten; unter
// /admin/* zusätzlich CSP und no-store. Handler dürfen sie verschärfen oder
// (statische Admin-Dateien: immutable) gezielt überschreiben.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if isAdminPath(r.URL.Path) {
			h.Set("Content-Security-Policy", adminCSP)
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
