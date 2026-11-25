package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// jobEnv: frische, installierte DB mit Uhr zum Verschieben.
type jobEnv struct {
	db   *sql.DB
	st   SettingsStore
	now  time.Time
	mu   sync.Mutex
	runs map[string]int
}

func newJobEnv(t *testing.T) *jobEnv {
	t.Helper()
	db := openTestDB(t, t.TempDir())
	ctx := context.Background()
	if err := Migrate(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	st := SettingsStore{DB: db}
	if err := st.Set(ctx, KeyInstalledAt, "2026-11-25T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	return &jobEnv{db: db, st: st, now: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC), runs: map[string]int{}}
}

func (e *jobEnv) jobs(list ...Job) *Jobs {
	return &Jobs{Deps: Deps{DB: e.db, Settings: e.st}, List: list,
		Now: func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now }}
}

func (e *jobEnv) advance(d time.Duration) { e.mu.Lock(); e.now = e.now.Add(d); e.mu.Unlock() }

// job zählt seine Läufe und liefert err.
func (e *jobEnv) job(name string, interval time.Duration, err error) Job {
	return Job{Name: name, Interval: interval, Run: func(context.Context, Deps) error {
		e.mu.Lock()
		e.runs[name]++
		e.mu.Unlock()
		return err
	}}
}

func lastError(t *testing.T, db *sql.DB, name string) sql.NullString {
	t.Helper()
	var s sql.NullString
	if err := db.QueryRow(`SELECT last_error FROM core_jobs WHERE name = ?`, name).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestJobsRunDue(t *testing.T) {
	e := newJobEnv(t)
	j := e.jobs(e.job("test.a", time.Hour, nil))
	ctx := context.Background()

	steps := []struct {
		name    string
		advance time.Duration
		want    string
	}{
		{"nie gelaufen → läuft", 0, "test.a"},
		{"sofort erneut → nichts fällig", 0, ""},
		{"vor Ablauf des Intervalls", 59 * time.Minute, ""},
		{"Intervall abgelaufen", time.Minute, "test.a"},
	}
	for _, s := range steps {
		e.advance(s.advance)
		got, err := j.RunDue(ctx, LazyBudget)
		if err != nil || got != s.want {
			t.Fatalf("%s: RunDue = %q, %v; erwartet %q", s.name, got, err, s.want)
		}
	}
	if e.runs["test.a"] != 2 {
		t.Fatalf("%d Läufe, erwartet 2", e.runs["test.a"])
	}
}

func TestJobsOnePerRequestOldestFirst(t *testing.T) {
	e := newJobEnv(t)
	ctx := context.Background()
	a, b := e.job("test.a", time.Hour, nil), e.job("test.b", time.Hour, nil)
	if got, _ := e.jobs(a).RunDue(ctx, LazyBudget); got != "test.a" {
		t.Fatalf("erster Lauf: %q", got)
	}
	e.advance(2 * time.Hour)
	// Beide fällig; b ist nie gelaufen und kommt zuerst, pro Aufruf nur einer.
	j := e.jobs(a, b)
	for _, want := range []string{"test.b", "test.a", ""} {
		if got, err := j.RunDue(ctx, LazyBudget); err != nil || got != want {
			t.Fatalf("RunDue = %q, %v; erwartet %q", got, err, want)
		}
	}
}

func TestJobsLockedSkipped(t *testing.T) {
	e := newJobEnv(t)
	ctx := context.Background()
	j := e.jobs(e.job("test.a", time.Hour, nil))
	// Zeile anlegen und Sperre eines anderen Prozesses simulieren.
	if _, err := e.db.Exec(`INSERT INTO core_jobs (name, interval_s, locked_until) VALUES ('test.a', 3600, ?)`,
		e.now.Add(30*time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if got, _ := j.RunDue(ctx, LazyBudget); got != "" {
		t.Fatalf("gesperrter Job lief: %q", got)
	}
	e.advance(time.Minute) // Sperre abgelaufen (abgebrochener Lauf)
	if got, _ := j.RunDue(ctx, LazyBudget); got != "test.a" {
		t.Fatalf("Job nach abgelaufener Sperre nicht gelaufen: %q", got)
	}
}

func TestJobsErrorRecorded(t *testing.T) {
	e := newJobEnv(t)
	ctx := context.Background()
	cases := []struct {
		name    string
		run     JobFunc
		wantErr string // "" = last_error NULL
	}{
		{"Fehler", func(context.Context, Deps) error { return errors.New("smtp kaputt") }, "smtp kaputt"},
		{"Panic", func(context.Context, Deps) error { panic("boom") }, "panic: boom"},
		{"Zeitbudget", func(ctx context.Context, _ Deps) error { <-ctx.Done(); return ctx.Err() }, "context deadline exceeded"},
		{"Erfolg löscht alten Fehler", func(context.Context, Deps) error { return nil }, ""},
	}
	for _, c := range cases {
		e.advance(2 * time.Hour)
		j := e.jobs(Job{Name: "test.err", Interval: time.Hour, Run: c.run})
		if got, err := j.RunDue(ctx, 50*time.Millisecond); err != nil || got != "test.err" {
			t.Fatalf("%s: RunDue = %q, %v", c.name, got, err)
		}
		le := lastError(t, e.db, "test.err")
		if (c.wantErr == "") != !le.Valid || !strings.Contains(le.String, c.wantErr) {
			t.Fatalf("%s: last_error = %+v, erwartet %q", c.name, le, c.wantErr)
		}
		if n := countRows(t, e.db, `SELECT count(*) FROM core_jobs WHERE name = 'test.err' AND last_run = ? AND locked_until IS NULL`, e.now.Unix()); n != 1 {
			t.Fatalf("%s: last_run/Sperre nicht gesetzt bzw. gelöst", c.name)
		}
		// Fehlgeschlagener Job läuft nicht bei der nächsten Anfrage gleich wieder.
		if got, _ := j.RunDue(ctx, LazyBudget); got != "" {
			t.Fatalf("%s: Job lief sofort erneut", c.name)
		}
	}
}

func TestJobsNotBeforeInstall(t *testing.T) {
	e := newJobEnv(t)
	if _, err := e.db.Exec(`DELETE FROM core_settings WHERE key = ?`, KeyInstalledAt); err != nil {
		t.Fatal(err)
	}
	if got, err := e.jobs(e.job("test.a", time.Hour, nil)).RunDue(context.Background(), LazyBudget); got != "" || err != nil {
		t.Fatalf("Job vor Installation: %q, %v", got, err)
	}
}

func TestJobsIntervalChange(t *testing.T) {
	e := newJobEnv(t)
	ctx := context.Background()
	e.jobs(e.job("test.a", time.Hour, nil)).RunDue(ctx, LazyBudget)
	e.advance(10 * time.Minute)
	if got, _ := e.jobs(e.job("test.a", 5*time.Minute, nil)).RunDue(ctx, LazyBudget); got != "test.a" {
		t.Fatalf("verkürztes Intervall nicht übernommen: %q", got)
	}
}

func TestRegisterJobRejects(t *testing.T) {
	ok := func(context.Context, Deps) error { return nil }
	cases := []struct {
		name     string
		interval time.Duration
		fn       JobFunc
	}{
		{"ohne-punkt", time.Hour, ok},
		{"Gross.name", time.Hour, ok},
		{"test.kurz", time.Second, ok},
		{"test.nil", time.Hour, nil},
		{"core.cleanup", time.Hour, ok}, // doppelt
	}
	for _, c := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RegisterJob(%q) ohne Panic", c.name)
				}
			}()
			RegisterJob(c.name, c.interval, c.fn)
		}()
	}
}

// ---- Parallelität: echte Prozesse wie im CGI-Modus ----

const envJobChild = "STYRIXA_TEST_JOB_DIR"

// TestJobHelperProcess läuft nur als Kindprozess von TestJobsParallelProcesses.
func TestJobHelperProcess(t *testing.T) {
	dir := os.Getenv(envJobChild)
	if dir == "" {
		t.Skip("nur als Kindprozess")
	}
	err := func() error {
		db, err := OpenDB(dir)
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		ctx := context.Background()
		if err := Migrate(ctx, db, nil); err != nil {
			return err
		}
		j := &Jobs{Deps: Deps{DB: db, Settings: SettingsStore{DB: db}}, List: []Job{{
			Name: "test.once", Interval: time.Hour,
			Run: func(ctx context.Context, d Deps) error {
				time.Sleep(50 * time.Millisecond) // Überlappung erzwingen
				_, err := d.DB.ExecContext(ctx, `INSERT INTO t_runs (pid) VALUES (?)`, os.Getpid())
				return err
			},
		}}}
		_, err = j.RunDue(ctx, LazyBudget)
		return err
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "child:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestJobsParallelProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("startet Kindprozesse")
	}
	dir := t.TempDir()
	e := func() *sql.DB {
		db := openTestDB(t, dir)
		if err := Migrate(context.Background(), db, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE t_runs (pid INTEGER)`); err != nil {
			t.Fatal(err)
		}
		if err := (SettingsStore{DB: db}).Set(context.Background(), KeyInstalledAt, "2026-11-25T10:00:00Z"); err != nil {
			t.Fatal(err)
		}
		return db
	}()

	const procs = 8
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*strings.Builder, procs)
	for i := range cmds {
		c := exec.Command(os.Args[0], "-test.run=^TestJobHelperProcess$", "-test.count=1")
		c.Env = append(os.Environ(), envJobChild+"="+dir)
		outs[i] = &strings.Builder{}
		c.Stdout, c.Stderr = outs[i], outs[i]
		cmds[i] = c
	}
	for _, c := range cmds {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("Prozess %d: %v\n%s", i+1, err, outs[i])
		}
	}
	if n := countRows(t, e, `SELECT count(*) FROM t_runs`); n != 1 {
		t.Fatalf("Job %d-mal ausgeführt, erwartet genau einmal", n)
	}
}

// ---- Cron-Endpunkt ----

func TestCron(t *testing.T) {
	e := newJobEnv(t)
	ctx := context.Background()
	failing := false
	j := e.jobs(Job{Name: "test.cron", Interval: time.Hour, Run: func(context.Context, Deps) error {
		e.mu.Lock()
		e.runs["test.cron"]++
		e.mu.Unlock()
		if failing {
			return errors.New("kaputt")
		}
		return nil
	}}, e.job("test.second", time.Hour, nil))
	mux := http.NewServeMux()
	j.Mount(mux)
	get := func(header, query string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", PathCron+query, nil)
		if header != "" {
			r.Header.Set(CronTokenHeader, header)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}

	if w := get("irgendwas", ""); w.Code != http.StatusNotFound {
		t.Fatalf("ohne eingerichteten Token: %d", w.Code)
	}
	token, err := NewCronToken(ctx, e.st)
	if err != nil {
		t.Fatal(err)
	}
	if stored, _ := e.st.Get(ctx, KeyCronTokenHash); stored == token || stored != hashToken(token) {
		t.Fatal("Cron-Token nicht nur als SHA-256 gespeichert")
	}
	for _, c := range []struct{ name, header, query string }{
		{"ohne Header", "", ""},
		{"falscher Token", token + "x", ""},
		{"Token nur in der URL", "", "?token=" + url.QueryEscape(token)},
	} {
		if w := get(c.header, c.query); w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", c.name, w.Code)
		}
	}
	if len(e.runs) != 0 {
		t.Fatalf("Jobs ohne gültigen Token gelaufen: %v", e.runs)
	}

	w := get(token, "")
	if w.Code != http.StatusOK || w.Body.String() != "ok" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("gültiger Token: %d %q", w.Code, w.Body)
	}
	if e.runs["test.cron"] != 1 || e.runs["test.second"] != 1 {
		t.Fatalf("Cron führt nicht alle fälligen Jobs aus: %v", e.runs)
	}
	if w := get(token, ""); w.Code != http.StatusOK || e.runs["test.cron"] != 1 {
		t.Fatalf("nichts fällig: %d, Läufe %v", w.Code, e.runs)
	}

	e.advance(2 * time.Hour)
	failing = true
	w = get(token, "")
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "kaputt") {
		t.Fatalf("fehlgeschlagener Job: %d %q (keine Details nach außen)", w.Code, w.Body)
	}
	if le := lastError(t, e.db, "test.cron"); le.String != "kaputt" {
		t.Fatalf("last_error = %+v", le)
	}
}

// ---- Kern-Job ----

func TestCleanupJob(t *testing.T) {
	e := newJobEnv(t)
	now := time.Now().Unix()
	if _, err := e.db.Exec(`INSERT INTO core_users (id, email, password_hash) VALUES (1, 'a@example.at', 'x')`); err != nil {
		t.Fatal(err)
	}
	seed := []string{
		fmt.Sprintf(`INSERT INTO core_sessions VALUES ('aktiv', 1, %d, %d, %d, 'c')`, now, now, now+3600),
		fmt.Sprintf(`INSERT INTO core_sessions VALUES ('abgelaufen', 1, %d, %d, %d, 'c')`, now-100, now-10, now-1),
		fmt.Sprintf(`INSERT INTO core_sessions VALUES ('inaktiv', 1, %d, %d, %d, 'c')`, now-86400, now-13*3600, now+3600),
		fmt.Sprintf(`INSERT INTO core_password_resets VALUES ('offen', 1, %d, NULL)`, now+600),
		fmt.Sprintf(`INSERT INTO core_password_resets VALUES ('benutzt', 1, %d, %d)`, now+600, now),
		fmt.Sprintf(`INSERT INTO core_password_resets VALUES ('alt', 1, %d, NULL)`, now-1),
		fmt.Sprintf(`INSERT INTO core_login_attempts VALUES ('ip:1', %d)`, now),
		fmt.Sprintf(`INSERT INTO core_login_attempts VALUES ('ip:1', %d)`, now-16*60),
	}
	for _, q := range seed {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanupJob(context.Background(), Deps{DB: e.db}); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM core_sessions WHERE token_hash = 'aktiv'`:        1,
		`SELECT count(*) FROM core_sessions`:                                   1,
		`SELECT count(*) FROM core_password_resets WHERE token_hash = 'offen'`: 1,
		`SELECT count(*) FROM core_password_resets`:                            1,
		`SELECT count(*) FROM core_login_attempts`:                             1,
	} {
		if n := countRows(t, e.db, q); n != want {
			t.Errorf("%s = %d, erwartet %d", q, n, want)
		}
	}
}

// ---- /healthz ----

func TestHealth(t *testing.T) {
	db := openTestDB(t, t.TempDir())
	ctx := context.Background()
	mod := testModule{id: "alpha", fs: mapFS(baseFiles)}
	if err := Migrate(ctx, db, []Module{mod}); err != nil {
		t.Fatal(err)
	}
	extra := map[string]string{"099_neu.sql": `CREATE TABLE t_neu (id INTEGER);`}
	for k, v := range baseFiles {
		extra[k] = v
	}
	cases := []struct {
		name string
		mods []Module
		want int
		body string
	}{
		{"alles angewendet", []Module{mod}, http.StatusOK, "ok"},
		{"Migration ausstehend", []Module{testModule{id: "alpha", fs: mapFS(extra)}}, http.StatusServiceUnavailable, "nicht bereit\n"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		Health(db, c.mods)(w, httptest.NewRequest("GET", "/healthz", nil))
		if w.Code != c.want || w.Body.String() != c.body || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %q", c.name, w.Code, w.Body)
		}
	}
	db.Close()
	w := httptest.NewRecorder()
	Health(db, []Module{mod})(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "closed") {
		t.Errorf("geschlossene DB: %d %q", w.Code, w.Body)
	}
}
