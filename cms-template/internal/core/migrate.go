package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// Migrationsdateien heißen NNN_name.sql (z. B. 001_shop.sql).
var migrationName = regexp.MustCompile(`^(\d{3})_[a-z0-9_]+\.sql$`)

type migration struct {
	module   string
	version  int
	name     string
	sql      string
	checksum string
}

type appliedMigration struct {
	name, checksum string
}

const schemaMigrationsDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
	module     TEXT    NOT NULL,
	version    INTEGER NOT NULL,
	name       TEXT    NOT NULL,
	checksum   TEXT    NOT NULL,
	applied_at TEXT    NOT NULL DEFAULT (datetime('now')),
	PRIMARY KEY (module, version)
)`

// Migrate wendet alle ausstehenden Migrationen der Module an – in der
// Reihenfolge von mods, innerhalb eines Moduls nach Version.
//
// Regeln: Migrationen werden nie geändert, nur angehängt. Eine geänderte,
// fehlende oder nachträglich eingeschobene Migration ist ein Fehler.
// Jede Datei läuft in einer eigenen Transaktion zusammen mit ihrem Eintrag in
// schema_migrations. Laufen mehrere Prozesse gleichzeitig (CGI), wendet genau
// einer die Datei an; die anderen warten (busy_timeout) und überspringen sie.
//
// Gibt es nichts zu tun, kostet ein Aufruf zwei kurze Abfragen – wichtig, weil im
// CGI-Modus jede Anfrage migriert.
func Migrate(ctx context.Context, db *sql.DB, mods []Module) error {
	var all []migration
	for _, m := range mods {
		ms, err := loadMigrations(m.ID(), m.Migrations())
		if err != nil {
			return err
		}
		all = append(all, ms...)
	}

	applied, err := readApplied(ctx, db)
	if err != nil {
		return err
	}
	pending, err := plan(all, applied)
	if err != nil || len(pending) == 0 {
		return err
	}
	for _, m := range pending {
		if err := apply(ctx, db, m); err != nil {
			return err
		}
	}
	return nil
}

func loadMigrations(module string, fsys fs.FS) ([]migration, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate %s: %w", module, err)
	}
	var out []migration
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		mm := migrationName.FindStringSubmatch(e.Name())
		if mm == nil {
			return nil, fmt.Errorf("migrate %s: ungültiger Dateiname %q (erwartet NNN_name.sql)", module, e.Name())
		}
		v, _ := strconv.Atoi(mm[1])
		if v == 0 {
			return nil, fmt.Errorf("migrate %s: Version 000 ist nicht erlaubt (%s)", module, e.Name())
		}
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("migrate %s: Version %03d doppelt (%s, %s)", module, v, prev, e.Name())
		}
		seen[v] = e.Name()
		b, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, fmt.Errorf("migrate %s: %w", module, err)
		}
		sum := sha256.Sum256(b)
		out = append(out, migration{module: module, version: v, name: e.Name(), sql: string(b), checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func readApplied(ctx context.Context, db *sql.DB) (map[string]map[int]appliedMigration, error) {
	if _, err := db.ExecContext(ctx, schemaMigrationsDDL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT module, version, name, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	defer rows.Close()
	out := map[string]map[int]appliedMigration{}
	for rows.Next() {
		var mod string
		var v int
		var a appliedMigration
		if err := rows.Scan(&mod, &v, &a.name, &a.checksum); err != nil {
			return nil, fmt.Errorf("migrate: %w", err)
		}
		if out[mod] == nil {
			out[mod] = map[int]appliedMigration{}
		}
		out[mod][v] = a
	}
	return out, rows.Err()
}

// plan prüft die Dateien gegen den Stand der Datenbank und liefert die
// ausstehenden Migrationen. Module, die im Binary fehlen, werden nicht geprüft:
// Ihre Tabellen bleiben einfach liegen (z. B. nach Wechsel der Modulauswahl).
func plan(all []migration, applied map[string]map[int]appliedMigration) ([]migration, error) {
	files := map[string]map[int]migration{}
	for _, m := range all {
		if files[m.module] == nil {
			files[m.module] = map[int]migration{}
		}
		files[m.module][m.version] = m
	}
	for mod, versions := range applied {
		fm, present := files[mod]
		if !present {
			continue
		}
		for v, a := range versions {
			f, ok := fm[v]
			switch {
			case !ok:
				return nil, fmt.Errorf("migrate %s: angewendete Migration %03d (%s) fehlt im Binary", mod, v, a.name)
			case f.checksum != a.checksum:
				return nil, fmt.Errorf("migrate %s: Migration %s wurde nach dem Anwenden geändert – neue Migration anlegen statt bestehende zu ändern", mod, f.name)
			}
		}
	}
	var pending []migration
	for _, m := range all {
		if _, done := applied[m.module][m.version]; done {
			continue
		}
		for v := range applied[m.module] {
			if v > m.version {
				return nil, fmt.Errorf("migrate %s: Migration %s ist älter als bereits angewendete Version %03d – Migrationen nur anhängen", m.module, m.name, v)
			}
		}
		pending = append(pending, m)
	}
	return pending, nil
}

func apply(ctx context.Context, db *sql.DB, m migration) (err error) {
	tx, err := db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (DSN): serialisiert parallele Prozesse
	if err != nil {
		return fmt.Errorf("migrate %s/%s: %w", m.module, m.name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	// Nach Erhalt der Sperre erneut prüfen: Ein anderer Prozess kann sie schon angewendet haben.
	var checksum string
	switch err = tx.QueryRowContext(ctx,
		`SELECT checksum FROM schema_migrations WHERE module = ? AND version = ?`, m.module, m.version).Scan(&checksum); {
	case err == nil:
		if checksum != m.checksum {
			return fmt.Errorf("migrate %s: Migration %s wurde nach dem Anwenden geändert", m.module, m.name)
		}
		return tx.Commit()
	case err != sql.ErrNoRows:
		return fmt.Errorf("migrate %s/%s: %w", m.module, m.name, err)
	}
	if _, err = tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migrate %s/%s: %w", m.module, m.name, err)
	}
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (module, version, name, checksum) VALUES (?, ?, ?, ?)`,
		m.module, m.version, m.name, m.checksum); err != nil {
		return fmt.Errorf("migrate %s/%s: %w", m.module, m.name, err)
	}
	return tx.Commit()
}
