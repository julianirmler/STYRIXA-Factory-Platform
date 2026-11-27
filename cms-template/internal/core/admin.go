package core

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Admin-Shell (Session 1.4, ADR-002): html/template + vendortes htmx.
// Layout, Navigation und statische Dateien liegen im Kern; jedes Modul bettet
// seine eigenen Templates per embed im Modulpaket ein. Nicht gewählte Module
// sind nicht im Binary – ihre Templates also auch nicht.

//go:embed admin/static/* admin/templates/*.html admin/templates/core/*.html
var adminFS embed.FS

// htmxFile ist die vendorte htmx-Version (siehe admin/VENDOR.md).
const htmxFile = "htmx-2.0.11.min.js"

const (
	pathAdminStatic = "/admin/static/"
	// CSP des Admins: nur eigene Dateien, keine Inline-Skripte oder -Styles.
	adminCSP = "default-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; frame-ancestors 'none'"
)

// AdminTemplates implementiert ein Modul, das Admin-Seiten mitbringt.
// Templates() liefert *.html-Dateien (auch in Unterordnern); der Kern legt jede
// Datei unter dem Namen „<modul-id>/<pfad>“ ab, z. B. „shop/placeholder.html“.
// Eine Datei ist der Inhalt einer Seite, das Layout kommt vom Kern.
type AdminTemplates interface {
	Templates() fs.FS
}

// AdminAssets implementiert ein Modul, das statische Admin-Dateien (JS, CSS)
// mitbringt – per embed im Modulpaket, also nur im Binary, wenn das Modul
// gewählt ist (ADR-002). Assets() liefert Dateien auf oberster Ebene; im
// Template heißen sie „<modul-id>/<datei>“, z. B. {{asset "content/editor.js"}}.
// Sie liegen unter /admin/static/ (wie die Kern-Dateien ohne Login abrufbar –
// nichts Geheimes) mit Inhalts-Hash im Namen.
type AdminAssets interface {
	Assets() fs.FS
}

// ExtEditor ist der Erweiterungspunkt der Editor-Seite (Content). Ein Skript
// an diesem Punkt darf window.StyrixaMediaPicker setzen:
//
//	{ pick(): Promise<Bild | null> }
//	Bild = {url, alt, width, height, variants: [{url, width}]}
//
// Der Bild-Block bietet dann „Aus Mediathek wählen“ an (Media, Session 4.1).
const ExtEditor = "editor"

// AdminExtensions implementiert ein Modul, das Skripte an Erweiterungspunkte
// fremder Admin-Seiten hängt, ohne dass sich die Module kennen. Die Werte
// sind Namen eigener Assets (AdminAssets, ohne „<id>/“), z. B.
// {core.ExtEditor: {"media-picker.js", "media.css"}}. Im Template der Seite:
//
//	{{range extensions "editor"}}{{if .Style}}<link rel="stylesheet" href="{{.URL}}">
//	{{else}}<script src="{{.URL}}" defer></script>{{end}}{{end}}
type AdminExtensions interface {
	AdminExtensions() map[string][]string
}

// ExtAsset ist eine Datei an einem Erweiterungspunkt.
type ExtAsset struct {
	URL   string
	Style bool // .css → <link rel="stylesheet">, sonst <script>
}

// AdminRenderer rendert eine Seite im Admin-Layout. Module erhalten ihn über
// Deps.Admin; er ist nil, wenn kein Admin gemountet ist (Tests).
type AdminRenderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, p AdminPage)
}

// AdminHome implementiert höchstens ein Modul (Dashboard, Session 3.4): Es
// registriert GET /admin selbst, der Kern lässt dann seine schlichte
// Willkommensseite weg. Ohne ein solches Modul bleibt sie (Tests, Builds ohne
// mod_dashboard).
type AdminHome interface {
	AdminHome()
}

// AdminPage beschreibt eine Admin-Seite.
type AdminPage struct {
	Title    string // Überschrift und Teil des <title>
	Template string // „<modul-id>/<datei>“
	Data     any    // im Template als .Data
	// StyleNonce erlaubt <style>-Elemente mit der Nonce .StyleNonce (pro
	// Anfrage neu) und data:-Bilder, z. B. für vendorte Skripte, die ihr CSS
	// selbst einfügen (Editor.js). Skripte bleiben auf 'self' beschränkt.
	StyleNonce bool
}

// AdminPlaceholder liefert einen Handler für eine Seite, die noch nicht gebaut
// ist. Er benutzt das Kern-Template, das Modul braucht dafür keines.
func AdminPlaceholder(d Deps, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.Admin == nil {
			http.Error(w, "Diese Seite ist noch nicht verfügbar.", http.StatusNotImplemented)
			return
		}
		d.Admin.Render(w, r, http.StatusOK, AdminPage{Title: title, Template: "core/placeholder.html"})
	}
}

// Admin ist die Admin-Shell. Templates werden beim Start einmal geparst – sie
// stammen aus dem Binary, sind also kein Zustand, der veralten kann.
type Admin struct {
	db       *sql.DB
	settings Settings
	nav      []NavItem
	pages    map[string]*template.Template
	assets   map[string]adminAsset // URL-Dateiname → Inhalt
	urls     map[string]string     // Quelldateiname → URL
	exts     map[string][]ExtAsset // Erweiterungspunkt → Dateien (AdminExtensions)
	home     string                // ID des Moduls mit AdminHome, "" = Kern-Startseite
}

// adminAsset verweist auf eine eingebettete Datei. Der Inhalt bleibt im
// Binary (embed) und wird pro Anfrage gestreamt – nicht beim Start in den
// Heap kopiert (≈ 440 KB mit Editor.js, doppelt über das GC-Ziel; M1: RSS < 20 MB).
type adminAsset struct {
	fsys  fs.FS
	name  string
	size  int64
	ctype string
}

// NewAdmin baut die Shell aus Kern-Layout und den Templates aller Module.
func NewAdmin(db *sql.DB, settings Settings, mods []Module) (*Admin, error) {
	a := &Admin{db: db, settings: settings, pages: map[string]*template.Template{},
		assets: map[string]adminAsset{}, urls: map[string]string{}}
	for _, m := range mods {
		if _, ok := m.(AdminHome); ok {
			if a.home != "" {
				return nil, fmt.Errorf("admin: Module %q und %q liefern beide die Admin-Startseite", a.home, m.ID())
			}
			a.home = m.ID()
		}
	}
	if err := a.loadAssets(mods); err != nil {
		return nil, err
	}
	a.exts = map[string][]ExtAsset{}
	for _, m := range mods {
		ae, ok := m.(AdminExtensions)
		if !ok {
			continue
		}
		for point, files := range ae.AdminExtensions() {
			for _, f := range files {
				u, err := a.assetURL(m.ID() + "/" + f)
				if err != nil {
					return nil, fmt.Errorf("admin: Erweiterung %q von %s: %w", point, m.ID(), err)
				}
				a.exts[point] = append(a.exts[point], ExtAsset{URL: u, Style: path.Ext(f) == ".css"})
			}
		}
	}

	a.nav = []NavItem{{Label: "Übersicht", Path: "/admin", Role: RoleEditor, Order: 0}}
	for _, m := range mods {
		a.nav = append(a.nav, m.AdminNav()...)
	}
	sort.SliceStable(a.nav, func(i, j int) bool { return a.nav[i].Order < a.nav[j].Order })

	layout, err := template.New("layout.html").Funcs(template.FuncMap{"asset": a.assetURL, "extensions": a.extensionURLs}).
		ParseFS(adminFS, "admin/templates/layout.html")
	if err != nil {
		return nil, fmt.Errorf("admin: Layout: %w", err)
	}
	coreFS, _ := fs.Sub(adminFS, "admin/templates/core")
	if err := a.addPages(layout, CoreID, coreFS); err != nil {
		return nil, err
	}
	for _, m := range mods {
		at, ok := m.(AdminTemplates)
		if !ok {
			continue
		}
		if fsys := at.Templates(); fsys != nil {
			if err := a.addPages(layout, m.ID(), fsys); err != nil {
				return nil, err
			}
		}
	}
	return a, nil
}

// addPages parst jede *.html-Datei von fsys als Inhalt („content“) in eine
// eigene Kopie des Layouts. So kollidieren gleichnamige Hilfs-Templates
// verschiedener Module nicht.
func (a *Admin) addPages(layout *template.Template, id string, fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		t, err := layout.Clone()
		if err != nil {
			return err
		}
		name := id + "/" + p
		if _, err := t.New("content").Parse(string(src)); err != nil {
			return fmt.Errorf("admin: Template %s: %w", name, err)
		}
		a.pages[name] = t
		return nil
	})
}

// loadAssets legt alle statischen Dateien unter einem Namen mit Inhalts-Hash ab
// (admin.css → admin.3f2a…9c.css). Damit dürfen Browser sie für immer cachen.
// Modul-Dateien heißen im Template „<id>/<datei>“, in der URL „<id>-<datei>“.
func (a *Admin) loadAssets(mods []Module) error {
	core, _ := fs.Sub(adminFS, "admin/static")
	if err := a.addAssets(core, ""); err != nil {
		return err
	}
	for _, m := range mods {
		am, ok := m.(AdminAssets)
		if !ok {
			continue
		}
		if fsys := am.Assets(); fsys != nil {
			if err := a.addAssets(fsys, m.ID()); err != nil {
				return fmt.Errorf("admin: Dateien von %s: %w", m.ID(), err)
			}
		}
	}
	return nil
}

func (a *Admin) addAssets(fsys fs.FS, id string) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		f, err := fsys.Open(e.Name())
		if err != nil {
			return err
		}
		h := sha256.New()
		size, err := io.Copy(h, f)
		f.Close()
		if err != nil {
			return err
		}
		sum := h.Sum(nil)
		ext := path.Ext(e.Name())
		key, url := e.Name(), strings.TrimSuffix(e.Name(), ext)+"."+hex.EncodeToString(sum[:5])+ext
		if id != "" {
			key, url = id+"/"+key, id+"-"+url
		}
		if _, dup := a.assets[url]; dup {
			return fmt.Errorf("doppelte Datei %s", url)
		}
		a.assets[url] = adminAsset{fsys: fsys, name: e.Name(), size: size, ctype: mime.TypeByExtension(ext)}
		a.urls[key] = pathAdminStatic + url
	}
	return nil
}

func (a *Admin) assetURL(name string) (string, error) {
	u, ok := a.urls[name]
	if !ok {
		return "", fmt.Errorf("admin: unbekannte Datei %q", name)
	}
	return u, nil
}

// extensionURLs liefert die Dateien aller Module an einem Erweiterungspunkt
// (Modul-Reihenfolge); leer, wenn niemand ihn nutzt.
func (a *Admin) extensionURLs(point string) []ExtAsset { return a.exts[point] }

// Mount registriert Startseite und statische Dateien.
func (a *Admin) Mount(mux *http.ServeMux) {
	if a.home == "" {
		mux.HandleFunc("GET /admin", a.dashboard)
		mux.HandleFunc("GET /admin/{$}", a.dashboard)
	}
	mux.HandleFunc("GET "+pathAdminStatic+"{file}", a.static)
}

func (a *Admin) dashboard(w http.ResponseWriter, r *http.Request) {
	a.Render(w, r, http.StatusOK, AdminPage{Title: "Übersicht", Template: "core/dashboard.html"})
}

func (a *Admin) static(w http.ResponseWriter, r *http.Request) {
	f, ok := a.assets[r.PathValue("file")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.ctype)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.FormatInt(f.size, 10))
	if r.Method == http.MethodHead {
		return
	}
	src, err := f.fsys.Open(f.name)
	if err != nil {
		slog.Error("admin: Datei nicht lesbar", "file", f.name, "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	defer src.Close()
	io.Copy(w, src)
}

type adminNavView struct {
	NavItem
	Active bool
}

type adminView struct {
	SiteTitle, Title, Email, CSRF, HtmxConfig string
	StyleNonce                                string // nur bei AdminPage.StyleNonce
	Nav                                       []adminNavView
	Data                                      any
}

// htmxConfig: keine Inline-Styles (CSP), kein eval, keine Skripte aus Antworten.
const htmxConfig = `{"includeIndicatorStyles":false,"allowEval":false,"allowScriptTags":false}`

// Render rendert p im Layout. Nur hinter Auth.Require aufrufen.
func (a *Admin) Render(w http.ResponseWriter, r *http.Request, status int, p AdminPage) {
	ctx := r.Context()
	t, ok := a.pages[p.Template]
	if !ok {
		slog.Error("admin: Template fehlt", "template", p.Template)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	v := adminView{Title: p.Title, CSRF: CSRFToken(ctx), HtmxConfig: htmxConfig, Data: p.Data}
	csp := adminCSP
	if p.StyleNonce {
		v.StyleNonce = newNonce()
		csp = adminCSPWithStyleNonce(v.StyleNonce)
	}
	if id, ok := CurrentUser(ctx); ok {
		if err := a.db.QueryRowContext(ctx, `SELECT email FROM core_users WHERE id = ?`, id).Scan(&v.Email); err != nil {
			slog.Error("admin: Benutzer nicht lesbar", "err", err)
			http.Error(w, msgInternal, http.StatusInternalServerError)
			return
		}
	}
	if a.settings != nil {
		if title, err := a.settings.Get(ctx, KeySiteTitle); err == nil {
			v.SiteTitle = title
		}
	}
	if v.SiteTitle == "" {
		v.SiteTitle = "STYRIXA"
	}
	editor := CurrentRole(ctx) == RoleEditor
	for _, n := range a.nav {
		if editor && n.Role != RoleEditor {
			continue // Mindestrolle Admin (leer) – für Editoren unsichtbar
		}
		active := r.URL.Path == n.Path || (n.Path != "/admin" && strings.HasPrefix(r.URL.Path, n.Path+"/"))
		v.Nav = append(v.Nav, adminNavView{NavItem: n, Active: active})
	}

	var buf bytes.Buffer // erst komplett rendern: bei Fehlern eine saubere 500
	if err := t.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		slog.Error("admin: Template", "template", p.Template, "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// newNonce liefert 128 Bit Zufall, Base64url ohne Padding – pro Anfrage neu
// (CSP-Nonce; keine Zeichen, die html/template in Attributen maskiert).
func newNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand scheitert nicht (Go ≥ 1.24 bricht sonst ab)
	return base64.RawURLEncoding.EncodeToString(b)
}

// adminCSPWithStyleNonce: wie adminCSP, zusätzlich <style> mit dieser Nonce
// und data:-Bilder (vendortes CSS nutzt SVG-Masken als data:-URL).
func adminCSPWithStyleNonce(nonce string) string {
	return adminCSP + "; style-src 'self' 'nonce-" + nonce + "'; img-src 'self' data:"
}
