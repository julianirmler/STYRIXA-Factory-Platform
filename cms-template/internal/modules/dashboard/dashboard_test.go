package dashboard

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

type env struct {
	t   *testing.T
	db  *sql.DB
	h   http.Handler
	m   *module
	now time.Time
}

// newEnv: echte SQLite, Kern-Admin und -Public wie in main.go. Der Benutzer
// liegt für /admin/* direkt im Kontext (Login prüft der Kern selbst).
func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := core.OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := &module{}
	mods := []core.Module{m}
	if err := core.Migrate(context.Background(), db, mods); err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO core_users (email, password_hash, role) VALUES ('admin@example.at', 'x', ?)`, core.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	uid, _ := res.LastInsertId()
	admin, err := core.NewAdmin(db, nil, mods)
	if err != nil {
		t.Fatal(err)
	}
	public, err := core.NewPublic(nil, mods)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	admin.Mount(mux) // darf GET /admin nicht selbst registrieren (sonst Panic: doppeltes Muster)
	public.Mount(mux)
	m.Mount(mux, core.Deps{DB: db, Admin: admin, Public: public})
	e := &env{t: t, db: db, m: m, now: time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)}
	m.st.now = func() time.Time { return e.now }
	e.h = core.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			r = r.WithContext(core.WithUser(r.Context(), uid, core.RoleAdmin))
		}
		mux.ServeHTTP(w, r)
	}))
	return e
}

func (e *env) req(method, path, ua string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if ua != "" {
		r.Header.Set("User-Agent", ua)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) hits() map[string]int64 {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT day || ' ' || path, count FROM dashboard_hits`)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var k string
		var n int64
		_ = rows.Scan(&k, &n)
		out[k] = n
	}
	return out
}

func TestIsBot(t *testing.T) {
	for ua, want := range map[string]bool{
		browserUA: false,
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36": false,
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148":   false,
		"Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0":                                          false,
		"":   true,
		"  ": true,
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)":   true,
		"Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)":    true,
		"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2)": true,
		"facebookexternalhit/1.1 (+http://www.facebook.com/externalhit_uatext.php)":  true,
		"curl/8.7.1":                     true,
		"Wget/1.21":                      true,
		"python-requests/2.32":           true,
		"Go-http-client/1.1":             true,
		"Mozilla/5.0 HeadlessChrome/131": true,
		"Mozilla/5.0 (Linux; Android 11; moto g power (2022)) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131 Mobile Safari/537.36 Chrome-Lighthouse": true,
		"UptimeRobot/2.0": true,
	} {
		if got := isBot(ua); got != want {
			t.Errorf("isBot(%q) = %v, erwartet %v", ua, got, want)
		}
	}
}

// Abnahme 3.4: gezählt wird serverseitig, ohne Cookies und ohne Personenbezug.
func TestCountsPublicPagesOnly(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 3; i++ {
		w := e.req("GET", "/", browserUA)
		if w.Code != 200 {
			t.Fatalf("Startseite: %d", w.Code)
		}
		if c := w.Header().Values("Set-Cookie"); len(c) > 0 {
			t.Errorf("öffentliche Seite setzt Cookies: %v", c)
		}
	}
	// Nicht gezählt: Bots, leerer User-Agent, HEAD, Prefetch, 404, Admin.
	e.req("GET", "/", "Googlebot/2.1")
	e.req("GET", "/", "")
	e.req("HEAD", "/", browserUA)
	e.req("GET", "/", browserUA, "Sec-Purpose", "prefetch;prerender")
	e.req("GET", "/", browserUA, "Purpose", "prefetch")
	e.req("GET", "/gibt-es-nicht", browserUA)
	e.req("GET", "/admin", browserUA)
	e.req("GET", "/admin/content", browserUA)

	got := e.hits()
	if len(got) != 1 || got["2026-11-26 /"] != 3 {
		t.Errorf("Zählung: %v, erwartet nur 2026-11-26 / = 3", got)
	}

	// Tageswechsel in UTC: neue Zeile.
	e.now = time.Date(2026, 11, 27, 0, 0, 1, 0, time.UTC)
	e.req("GET", "/", browserUA)
	if got := e.hits(); got["2026-11-27 /"] != 1 || got["2026-11-26 /"] != 3 {
		t.Errorf("nach Tageswechsel: %v", got)
	}
}

// Die Tabelle kann gar nichts Personenbezogenes aufnehmen.
func TestNoPersonalDataStored(t *testing.T) {
	e := newEnv(t)
	rows, err := e.db.Query(`SELECT name FROM pragma_table_info('dashboard_hits') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		_ = rows.Scan(&c)
		cols = append(cols, c)
	}
	if strings.Join(cols, ",") != "day,path,count" {
		t.Errorf("Spalten von dashboard_hits: %v – nur day, path, count erlaubt", cols)
	}
	var n int
	_ = e.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'dashboard_%' AND type = 'table'`).Scan(&n)
	if n != 1 {
		t.Errorf("%d Dashboard-Tabellen, erwartet 1", n)
	}
	// Ein Aufruf mit Query-String speichert nur den Pfad.
	e.req("GET", "/?utm_source=newsletter&email=max@example.at", browserUA)
	for k := range e.hits() {
		if strings.Contains(k, "utm") || strings.Contains(k, "@") {
			t.Errorf("Query-String gespeichert: %q", k)
		}
	}
}

func TestCountLimits(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, p := range []string{"", "ohne-slash", "/admin", "/admin/x", "/" + strings.Repeat("a", maxPathLen), "/\xff"} {
		if err := e.m.CountHit(ctx, core.Hit{Path: p, UserAgent: browserUA}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.m.CountHit(ctx, core.Hit{Path: "/kontakt", UserAgent: browserUA}); err != nil {
		t.Fatal(err)
	}
	if got := e.hits(); len(got) != 1 || got["2026-11-26 /kontakt"] != 1 {
		t.Errorf("Grenzen: %v", got)
	}
}

func TestDashboardPage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	add := func(daysAgo int, path string, n int) {
		day := e.now.AddDate(0, 0, -daysAgo).Format(dayFormat)
		if _, err := e.db.Exec(`INSERT INTO dashboard_hits (day, path, count) VALUES (?, ?, ?)`, day, path, n); err != nil {
			t.Fatal(err)
		}
	}
	add(0, "/", 5)
	add(6, "/kontakt", 2)  // noch in 7 Tagen
	add(7, "/kontakt", 10) // nur in 30 Tagen
	add(29, "/ueber-uns", 4)
	add(30, "/alt", 100) // außerhalb
	if _, err := e.db.Exec(`INSERT INTO core_jobs (name, interval_s, last_run) VALUES ('content.prune_revisions', 86400, ?)`,
		e.now.Unix()); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.m.st.total(ctx, 7); n != 7 {
		t.Errorf("7 Tage: %d", n)
	}
	if n, _ := e.m.st.total(ctx, 30); n != 21 {
		t.Errorf("30 Tage: %d", n)
	}

	w := e.req("GET", "/admin", browserUA)
	b := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("/admin: %d\n%s", w.Code, b)
	}
	for _, want := range []string{
		`data-styrixa-template="dashboard-home"`,
		`<span class="dash-num">7</span> letzte 7 Tage`,
		`<span class="dash-num">21</span> letzte 30 Tage`,
		`<a href="/kontakt"><code>/kontakt</code></a></td><td class="dash-count">12</td>`, // Top-Seite zuerst
		`href="/admin/content#title"`,                                                     // Schnellzugriff „Neue Seite“
		`href="/admin/content"`,
		`<code>dev</code>`, // Version
		`<code>content.prune_revisions</code> am 26.11.2026`,
		`Do 26.11.`, `max="5" value="5"`,
		"admin@example.at",
		"/admin/static/dashboard-dashboard.",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("fehlt: %s", want)
		}
	}
	if strings.Contains(b, "/alt") {
		t.Error("Aufrufe älter als 30 Tage angezeigt")
	}
	if strings.Contains(b, "Dieser Bereich ist noch in Arbeit") {
		t.Error("Kern-Platzhalter statt Dashboard")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("Admin-CSP fehlt")
	}
	if w := e.req("GET", "/admin/", browserUA); w.Code != 200 || !strings.Contains(w.Body.String(), "dashboard-home") {
		t.Errorf("/admin/: %d", w.Code)
	}
}

func TestPruneHits(t *testing.T) {
	e := newEnv(t)
	for _, ago := range []int{0, keepDays - 1, keepDays, keepDays + 30} {
		day := e.now.AddDate(0, 0, -ago).Format(dayFormat)
		if _, err := e.db.Exec(`INSERT INTO dashboard_hits (day, path, count) VALUES (?, '/', 1)`, day); err != nil {
			t.Fatal(err)
		}
	}
	n, err := e.m.st.prune(context.Background())
	if err != nil || n != 2 || len(e.hits()) != 2 {
		t.Errorf("prune: %d gelöscht, %d übrig, %v", n, len(e.hits()), err)
	}
	found := false
	for _, j := range core.RegisteredJobs() {
		found = found || j.Name == "dashboard.prune_hits"
	}
	if !found {
		t.Error("Job dashboard.prune_hits nicht registriert")
	}
}

func TestFormatBytes(t *testing.T) {
	for n, want := range map[int64]string{512: "512 Bytes", 2048: "2,0 KB", 1468006: "1,4 MB", 3 << 30: "3,0 GB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, erwartet %q", n, got, want)
		}
	}
}
