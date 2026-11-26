// Package dashboard ist Pflichtmodul 01 (mod_dashboard): Admin-Startseite mit
// cookiefreier Besucherzählung (Tag × Pfad), Status und Schnellzugriffen.
// Es wird nur über cmd/styrixa/mod_dashboard.go (//go:build mod_dashboard)
// importiert.
package dashboard

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed templates/*.html
var templates embed.FS

//go:embed static/*.css
var static embed.FS

type module struct {
	d  core.Deps
	st *store
}

func (*module) ID() string         { return "dashboard" }
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

// Der Navigationseintrag „Übersicht“ (/admin) kommt vom Kern.
func (*module) AdminNav() []core.NavItem { return nil }

// AdminHome: Dieses Modul liefert GET /admin (core.AdminHome).
func (*module) AdminHome() {}

func (m *module) Mount(mux *http.ServeMux, d core.Deps) {
	m.d = d
	if d.DB != nil {
		m.st = &store{db: d.DB}
	}
	mux.HandleFunc("GET /admin", m.home)
	mux.HandleFunc("GET /admin/{$}", m.home)
}

// CountHit implementiert core.HitCounter.
func (m *module) CountHit(ctx context.Context, h core.Hit) error {
	if m.st == nil {
		return nil
	}
	return m.st.count(ctx, h)
}

type homeView struct {
	Views7, Views30 int64
	Days            []dayCount
	MaxDay          int64 // für <meter max>
	Top             []pageCount
	Version         string
	DBSize          string
	Job             *core.JobRun
	JobAt           string
	Error           string
}

func (m *module) home(w http.ResponseWriter, r *http.Request) {
	if m.st == nil || m.d.Admin == nil {
		http.Error(w, "Diese Seite ist noch nicht verfügbar.", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	v := homeView{Version: core.Version}
	err := m.load(ctx, &v)
	code := http.StatusOK
	if err != nil {
		slog.Error("dashboard", "err", err)
		code, v.Error = http.StatusInternalServerError, "Die Statistik ist gerade nicht lesbar – bitte später erneut versuchen."
	}
	m.d.Admin.Render(w, r, code, core.AdminPage{Title: "Übersicht", Template: "dashboard/home.html", Data: v})
}

func (m *module) load(ctx context.Context, v *homeView) (err error) {
	if v.Views7, err = m.st.total(ctx, 7); err != nil {
		return err
	}
	if v.Views30, err = m.st.total(ctx, 30); err != nil {
		return err
	}
	if v.Days, err = m.st.daily(ctx, 7); err != nil {
		return err
	}
	for _, d := range v.Days {
		v.MaxDay = max(v.MaxDay, d.Count)
	}
	if v.Top, err = m.st.topPages(ctx, 30); err != nil {
		return err
	}
	size, err := m.st.dbSize(ctx)
	if err != nil {
		return err
	}
	v.DBSize = formatBytes(size)
	job, ok, err := core.LastJobRun(ctx, m.st.db)
	if err != nil {
		return err
	}
	if ok {
		v.Job, v.JobAt = &job, job.At.Format("02.01.2006 15:04")
	}
	return nil
}

// formatBytes: deutsche Schreibweise, z. B. „1,4 MB“.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return decimal(float64(n)/(1<<30)) + " GB"
	case n >= 1<<20:
		return decimal(float64(n)/(1<<20)) + " MB"
	case n >= 1<<10:
		return decimal(float64(n)/(1<<10)) + " KB"
	}
	return fmt.Sprintf("%d Bytes", n)
}

func decimal(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	for i := range s {
		if s[i] == '.' {
			return s[:i] + "," + s[i+1:]
		}
	}
	return s
}

func init() {
	core.Register(&module{})
	core.RegisterJob("dashboard.prune_hits", 24*time.Hour, func(ctx context.Context, d core.Deps) error {
		_, err := (&store{db: d.DB}).prune(ctx)
		return err
	})
}
