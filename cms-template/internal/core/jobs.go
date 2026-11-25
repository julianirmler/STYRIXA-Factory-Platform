package core

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"time"
)

// Lazy-Jobs und Cron-Endpunkt (Session 1.6, ADR-005). Es gibt keinen
// Hintergrundprozess: Nach einer Anfrage läuft höchstens ein fälliger Job
// (AfterBudget), /cron führt alle fälligen aus. Zustand und Sperren liegen in
// core_jobs – im CGI-Modus identisch zum Server-Modus.

const (
	// LazyBudget ist die Zeit, die ein Job nach einer Anfrage höchstens bekommt.
	LazyBudget = 2 * time.Second
	// CronBudget ist die Zeit pro Job beim Aufruf über /cron.
	CronBudget = 20 * time.Second
	// lockMargin hält die Sperre über das Budget hinaus, falls ein Job den
	// Kontext ignoriert. Danach gilt ein Lauf als abgebrochen.
	lockMargin = time.Minute

	PathCron        = "/cron"
	CronTokenHeader = "X-Styrixa-Cron-Token" // nie als Query-Parameter (landet in Access-Logs)

	maxErrorLen = 500
)

// JobFunc ist die Arbeit eines Jobs. ctx endet mit dem Zeitbudget.
type JobFunc func(ctx context.Context, d Deps) error

// Job ist ein registrierter Job.
type Job struct {
	Name     string
	Interval time.Duration
	Run      JobFunc
}

var (
	jobRegistry = map[string]Job{}
	jobName     = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z0-9_.]+$`) // "<modul>.<name>"
)

// RegisterJob meldet einen Job an; Module rufen es in init() auf.
// name hat die Form "<modul-id>.<name>", z. B. "shop.abandoned_carts".
func RegisterJob(name string, interval time.Duration, fn JobFunc) {
	if !jobName.MatchString(name) {
		panic("styrixa: ungültiger Job-Name " + name)
	}
	if interval < time.Minute || fn == nil {
		panic("styrixa: Job " + name + " braucht Intervall ≥ 1 min und eine Funktion")
	}
	if _, dup := jobRegistry[name]; dup {
		panic("styrixa: Job doppelt registriert: " + name)
	}
	jobRegistry[name] = Job{Name: name, Interval: interval, Run: fn}
}

// RegisteredJobs liefert alle registrierten Jobs, nach Namen sortiert.
func RegisteredJobs() []Job {
	out := make([]Job, 0, len(jobRegistry))
	for _, j := range jobRegistry {
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

// Jobs führt fällige Jobs aus.
type Jobs struct {
	Deps Deps             // Deps.DB und Deps.Settings werden benötigt
	List []Job            // nil = RegisteredJobs(); Tests setzen eigene
	Now  func() time.Time // nil = time.Now
}

func (j *Jobs) now() time.Time {
	if j.Now != nil {
		return j.Now()
	}
	return time.Now()
}

func (j *Jobs) list() []Job {
	if j.List != nil {
		return j.List
	}
	return RegisteredJobs()
}

// RunDue führt höchstens einen fälligen Job aus (den am längsten überfälligen)
// und liefert seinen Namen ("" = nichts fällig). Vor der Installation läuft nichts.
// Kosten ohne fälligen Job: zwei kurze Abfragen.
func (j *Jobs) RunDue(ctx context.Context, budget time.Duration) (string, error) {
	ran, _, err := j.run(ctx, budget, 1)
	if len(ran) == 0 {
		return "", err
	}
	return ran[0], err
}

// RunAllDue führt alle fälligen Jobs aus und meldet, wie viele fehlschlugen.
func (j *Jobs) RunAllDue(ctx context.Context, budget time.Duration) (ran []string, failed int, err error) {
	return j.run(ctx, budget, 0)
}

type jobState struct {
	interval int64
	lastRun  sql.NullInt64
	locked   sql.NullInt64
}

// run führt bis zu max fällige Jobs aus (0 = alle).
func (j *Jobs) run(ctx context.Context, budget time.Duration, max int) (ran []string, failed int, err error) {
	jobs := j.list()
	if len(jobs) == 0 {
		return nil, 0, nil
	}
	db := j.Deps.DB
	if j.Deps.Settings != nil {
		if ok, err := Installed(ctx, j.Deps.Settings); err != nil || !ok {
			return nil, 0, err
		}
	}
	states, err := readJobStates(ctx, db)
	if err != nil {
		return nil, 0, err
	}
	now := j.now().Unix()

	// Fällige Jobs, die nicht gesperrt sind; nie gelaufene zuerst, dann die ältesten.
	type cand struct {
		job     Job
		lastRun int64
	}
	var due []cand
	for _, job := range jobs {
		iv := int64(job.Interval / time.Second)
		st, known := states[job.Name]
		if !known || st.interval != iv {
			// Einmalig anlegen bzw. geändertes Intervall übernehmen.
			if _, err := db.ExecContext(ctx,
				`INSERT INTO core_jobs (name, interval_s) VALUES (?, ?)
				 ON CONFLICT(name) DO UPDATE SET interval_s = excluded.interval_s`, job.Name, iv); err != nil {
				return ran, failed, fmt.Errorf("jobs %s: %w", job.Name, err)
			}
		}
		if st.locked.Valid && st.locked.Int64 >= now {
			continue
		}
		if st.lastRun.Valid && st.lastRun.Int64+iv > now {
			continue
		}
		last := int64(-1)
		if st.lastRun.Valid {
			last = st.lastRun.Int64
		}
		due = append(due, cand{job, last})
	}
	sort.SliceStable(due, func(a, b int) bool { return due[a].lastRun < due[b].lastRun })

	for _, c := range due {
		if max > 0 && len(ran) >= max {
			break
		}
		if ctx.Err() != nil {
			break
		}
		claimed, err := j.claim(ctx, c.job, budget)
		if err != nil {
			return ran, failed, err
		}
		if !claimed {
			continue // ein anderer Prozess war schneller
		}
		ran = append(ran, c.job.Name)
		if !j.execute(ctx, c.job, budget) {
			failed++
		}
	}
	return ran, failed, nil
}

func readJobStates(ctx context.Context, db *sql.DB) (map[string]jobState, error) {
	rows, err := db.QueryContext(ctx, `SELECT name, interval_s, last_run, locked_until FROM core_jobs`)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	defer rows.Close()
	out := map[string]jobState{}
	for rows.Next() {
		var name string
		var st jobState
		if err := rows.Scan(&name, &st.interval, &st.lastRun, &st.locked); err != nil {
			return nil, fmt.Errorf("jobs: %w", err)
		}
		out[name] = st
	}
	return out, rows.Err()
}

// claim sperrt den Job, wenn er noch fällig und frei ist. Die Bedingung steht
// im UPDATE selbst: Von parallelen Prozessen bekommt genau einer die Zeile.
func (j *Jobs) claim(ctx context.Context, job Job, budget time.Duration) (bool, error) {
	now := j.now().Unix()
	res, err := j.Deps.DB.ExecContext(ctx,
		`UPDATE core_jobs SET locked_until = ?
		 WHERE name = ? AND (locked_until IS NULL OR locked_until < ?)
		   AND (last_run IS NULL OR last_run + interval_s <= ?)`,
		now+int64((budget+lockMargin)/time.Second), job.Name, now, now)
	if err != nil {
		return false, fmt.Errorf("jobs %s: %w", job.Name, err)
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// execute führt den Job im Zeitbudget aus und hält das Ergebnis fest.
// last_run wird auch bei Fehlern gesetzt – sonst liefe ein kaputter Job bei
// jeder Anfrage erneut.
func (j *Jobs) execute(ctx context.Context, job Job, budget time.Duration) (ok bool) {
	start := j.now().Unix()
	jctx, cancel := context.WithTimeout(ctx, budget)
	err := safeRun(jctx, job, j.Deps)
	cancel()

	var lastErr any // NULL bei Erfolg
	if err != nil {
		msg := err.Error()
		if len(msg) > maxErrorLen {
			msg = msg[:maxErrorLen]
		}
		lastErr = msg
		slog.Error("job fehlgeschlagen", "job", job.Name, "err", msg)
	}
	// Ergebnis auch dann schreiben, wenn ctx inzwischen abgelaufen ist.
	wctx, wcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer wcancel()
	if _, werr := j.Deps.DB.ExecContext(wctx,
		`UPDATE core_jobs SET last_run = ?, locked_until = NULL, last_error = ? WHERE name = ?`,
		start, lastErr, job.Name); werr != nil {
		slog.Error("job: Ergebnis nicht gespeichert", "job", job.Name, "err", werr)
		return false
	}
	return err == nil
}

func safeRun(ctx context.Context, job Job, d Deps) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return job.Run(ctx, d)
}

// ---- Cron-Endpunkt ----

// KeyCronTokenHash ist der SHA-256 (hex) des Cron-Tokens. Leer = /cron aus.
const KeyCronTokenHash = "core.cron.token_sha256"

// NewCronToken erzeugt einen neuen Cron-Token, speichert nur seinen Hash und
// liefert den Token genau einmal zurück (zur Anzeige für den Kunden).
func NewCronToken(ctx context.Context, s Settings) (string, error) {
	token := randomToken()
	if err := s.Set(ctx, KeyCronTokenHash, hashToken(token)); err != nil {
		return "", err
	}
	return token, nil
}

// Mount hängt GET /cron ein. Aufruf vom Hoster-Cron:
//
//	curl -fsS -H 'X-Styrixa-Cron-Token: <token>' https://example.at/cron
//
// Falscher, fehlender oder nicht eingerichteter Token → 404 (nichts verraten).
func (j *Jobs) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathCron, j.cron)
}

func (j *Jobs) cron(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !j.cronTokenOK(r) {
		http.NotFound(w, r)
		return
	}
	_, failed, err := j.RunAllDue(r.Context(), CronBudget)
	if err != nil || failed > 0 {
		if err != nil {
			slog.Error("cron", "err", err)
		}
		// Details stehen in core_jobs.last_error und im Log, nicht in der Antwort.
		http.Error(w, "Fehler", http.StatusInternalServerError)
		return
	}
	w.Write([]byte("ok"))
}

func (j *Jobs) cronTokenOK(r *http.Request) bool {
	got := r.Header.Get(CronTokenHeader)
	if got == "" || j.Deps.Settings == nil {
		return false
	}
	want, err := j.Deps.Settings.Get(r.Context(), KeyCronTokenHash)
	if err != nil || want == "" {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) == 1
}

// ---- Kern-Job ----

func init() {
	RegisterJob("core.cleanup", time.Hour, cleanupJob)
}

// cleanupJob löscht abgelaufene Sessions, Reset-Tokens und alte Fehlversuche.
func cleanupJob(ctx context.Context, d Deps) error {
	now := time.Now().Unix()
	stmts := []struct {
		q    string
		args []any
	}{
		{`DELETE FROM core_sessions WHERE expires_at <= ? OR last_seen <= ?`, []any{now, now - int64(sessionIdle/time.Second)}},
		{`DELETE FROM core_password_resets WHERE expires_at <= ? OR used_at IS NOT NULL`, []any{now}},
		{`DELETE FROM core_login_attempts WHERE at <= ?`, []any{now - int64(attemptWindow/time.Second)}},
	}
	var errs []error
	for _, s := range stmts {
		if _, err := d.DB.ExecContext(ctx, s.q, s.args...); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
