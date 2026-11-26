package core

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Version des CMS. Release-Builds setzen sie per
// -ldflags "-X github.com/julianirmler/styrixa-cms/internal/core.Version=v…"
// (scripts/matrix-build.sh, STYRIXA_VERSION); sonst „dev“.
var Version = "dev"

// JobRun ist der zuletzt gestartete Lazy-Job (für das Dashboard).
type JobRun struct {
	Name  string
	At    time.Time
	Error string // "" = erfolgreich
}

// LastJobRun liefert den zuletzt gestarteten Job; ok = false, wenn noch keiner lief.
func LastJobRun(ctx context.Context, db *sql.DB) (run JobRun, ok bool, err error) {
	var at int64
	err = db.QueryRowContext(ctx, `SELECT name, last_run, IFNULL(last_error, '') FROM core_jobs
		WHERE last_run IS NOT NULL ORDER BY last_run DESC, name LIMIT 1`).Scan(&run.Name, &at, &run.Error)
	if errors.Is(err, sql.ErrNoRows) {
		return run, false, nil
	}
	if err != nil {
		return run, false, err
	}
	run.At = time.Unix(at, 0)
	return run, true, nil
}
