package core

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

type testModule struct {
	id  string
	fs  fs.FS
	req []string
}

func (m testModule) ID() string               { return m.id }
func (m testModule) Requires() []string       { return m.req }
func (m testModule) Migrations() fs.FS        { return m.fs }
func (testModule) Mount(*http.ServeMux, Deps) {}
func (testModule) AdminNav() []NavItem        { return nil }

func mapFS(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, body := range files {
		out[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return out
}

var baseFiles = map[string]string{
	"001_init.sql": `CREATE TABLE t_items (id INTEGER PRIMARY KEY, n INTEGER NOT NULL);
CREATE INDEX idx_t_items_n ON t_items(n);`,
	"002_more.sql": `ALTER TABLE t_items ADD COLUMN note TEXT NOT NULL DEFAULT '';`,
}

func openTestDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := OpenDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func countRows(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOpenDB(t *testing.T) {
	dir := t.TempDir()
	db := openTestDB(t, dir)

	pragmas := []struct {
		name, want string
	}{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
		{"synchronous", "1"}, // NORMAL
	}
	for _, p := range pragmas {
		var got string
		if err := db.QueryRow("PRAGMA " + p.name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if !strings.EqualFold(got, p.want) {
			t.Errorf("PRAGMA %s = %q, want %q", p.name, got, p.want)
		}
	}
	st, err := os.Stat(filepath.Join(dir, DBFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Errorf("Dateirechte %o, erwartet 600", perm)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := openTestDB(t, t.TempDir())
	mods := []Module{
		testModule{id: "alpha", fs: mapFS(baseFiles)},
		testModule{id: "beta", fs: mapFS(map[string]string{"001_beta.sql": `CREATE TABLE beta_x (id INTEGER PRIMARY KEY);`})},
		testModule{id: "empty"}, // Modul ohne Migrationen (wie media)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := Migrate(ctx, db, mods); err != nil {
			t.Fatalf("Lauf %d: %v", i+1, err)
		}
	}
	if n := countRows(t, db, `SELECT count(*) FROM schema_migrations WHERE module != 'core'`); n != 3 {
		t.Fatalf("schema_migrations hat %d Zeilen, erwartet 3", n)
	}
	// 002 wurde angewendet: Spalte note existiert
	if _, err := db.Exec(`INSERT INTO t_items (n, note) VALUES (1, 'x')`); err != nil {
		t.Fatal(err)
	}

	// Neue Migration anhängen → wird angewendet, Rest bleibt
	more := map[string]string{"003_extra.sql": `CREATE TABLE t_extra (id INTEGER PRIMARY KEY);`}
	for k, v := range baseFiles {
		more[k] = v
	}
	mods[0] = testModule{id: "alpha", fs: mapFS(more)}
	if err := Migrate(ctx, db, mods); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, `SELECT count(*) FROM schema_migrations WHERE module = 'alpha'`); n != 3 {
		t.Fatalf("alpha hat %d Migrationen, erwartet 3", n)
	}
}

func TestMigrateFailedMigrationRollsBack(t *testing.T) {
	db := openTestDB(t, t.TempDir())
	bad := testModule{id: "alpha", fs: mapFS(map[string]string{
		"001_init.sql": `CREATE TABLE t_ok (id INTEGER PRIMARY KEY); CREATE TABLE t_ok (id INTEGER);`,
	})}
	if err := Migrate(context.Background(), db, []Module{bad}); err == nil {
		t.Fatal("fehlerhafte Migration wurde akzeptiert")
	}
	if n := countRows(t, db, `SELECT count(*) FROM sqlite_master WHERE name = 't_ok'`); n != 0 {
		t.Fatal("Teil einer fehlgeschlagenen Migration ist geblieben")
	}
	if n := countRows(t, db, `SELECT count(*) FROM schema_migrations WHERE module != 'core'`); n != 0 {
		t.Fatal("fehlgeschlagene Migration als angewendet markiert")
	}
}

func TestMigrateRejects(t *testing.T) {
	changed := map[string]string{"001_init.sql": baseFiles["001_init.sql"] + "\n-- geändert", "002_more.sql": baseFiles["002_more.sql"]}
	missing := map[string]string{"002_more.sql": baseFiles["002_more.sql"]}

	tests := []struct {
		name  string
		first map[string]string // vorher angewendet (nil = nichts)
		then  map[string]string
		want  string
	}{
		{"geänderte Migration", baseFiles, changed, "geändert"},
		{"fehlende Migration", baseFiles, missing, "fehlt"},
		{"eingeschobene Migration", map[string]string{"002_more.sql": `CREATE TABLE t_late (id INTEGER PRIMARY KEY);`},
			map[string]string{"001_init.sql": `SELECT 1;`, "002_more.sql": `CREATE TABLE t_late (id INTEGER PRIMARY KEY);`}, "älter"},
		{"ungültiger Dateiname", nil, map[string]string{"1_init.sql": "SELECT 1;"}, "Dateiname"},
		{"Version 000", nil, map[string]string{"000_init.sql": "SELECT 1;"}, "000"},
		{"doppelte Version", nil, map[string]string{"001_a.sql": "SELECT 1;", "001_b.sql": "SELECT 1;"}, "doppelt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestDB(t, t.TempDir())
			ctx := context.Background()
			if tt.first != nil {
				if err := Migrate(ctx, db, []Module{testModule{id: "alpha", fs: mapFS(tt.first)}}); err != nil {
					t.Fatal(err)
				}
			}
			err := Migrate(ctx, db, []Module{testModule{id: "alpha", fs: mapFS(tt.then)}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, erwartet Fehler mit %q", err, tt.want)
			}
		})
	}
}

func TestMigrateIgnoresRemovedModule(t *testing.T) {
	db := openTestDB(t, t.TempDir())
	ctx := context.Background()
	alpha := testModule{id: "alpha", fs: mapFS(baseFiles)}
	if err := Migrate(ctx, db, []Module{alpha}); err != nil {
		t.Fatal(err)
	}
	// Binary ohne alpha (andere Modulauswahl) darf nicht scheitern
	if err := Migrate(ctx, db, []Module{testModule{id: "beta"}}); err != nil {
		t.Fatal(err)
	}
}

// --- Zwei echte Prozesse (CGI-Szenario) ---

const (
	envChildDir = "STYRIXA_TEST_CHILD_DIR"
	childWrites = 1000
)

// TestHelperProcess läuft nur als Kindprozess von TestTwoProcesses.
func TestHelperProcess(t *testing.T) {
	dir := os.Getenv(envChildDir)
	if dir == "" {
		t.Skip("nur als Kindprozess")
	}
	if err := childWork(dir); err != nil {
		fmt.Fprintln(os.Stderr, "child:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func childWork(dir string) error {
	db, err := OpenDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // wie im CGI-Modus
	ctx := context.Background()
	if err := Migrate(ctx, db, []Module{testModule{id: "alpha", fs: mapFS(baseFiles)}}); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pid := strconv.Itoa(os.Getpid())
	for i := 0; i < childWrites; i++ {
		// Lesen, dann schreiben in derselben Transaktion – das klassische
		// „database is locked“-Muster ohne BEGIN IMMEDIATE.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %d: %w", i, err)
		}
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM t_items`).Scan(&n); err != nil {
			tx.Rollback()
			return fmt.Errorf("select %d: %w", i, err)
		}
		if _, err := tx.Exec(`INSERT INTO t_items (n, note) VALUES (?, ?)`, n, pid); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert %d: %w", i, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %d: %w", i, err)
		}
	}
	return nil
}

func TestTwoProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("startet Kindprozesse")
	}
	dir := t.TempDir()
	const procs = 4
	cmds := make([]*exec.Cmd, procs)
	outs := make([]*strings.Builder, procs)
	for i := range cmds {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), envChildDir+"="+dir)
		outs[i] = &strings.Builder{}
		cmd.Stdout, cmd.Stderr = outs[i], outs[i]
		cmds[i] = cmd
	}
	for _, c := range cmds { // möglichst gleichzeitig starten
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for i, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatalf("Prozess %d: %v\n%s", i+1, err, outs[i])
		}
	}

	db := openTestDB(t, dir)
	if n := countRows(t, db, `SELECT count(*) FROM t_items`); n != procs*childWrites {
		t.Fatalf("t_items hat %d Zeilen, erwartet %d", n, procs*childWrites)
	}
	if n := countRows(t, db, `SELECT count(DISTINCT note) FROM t_items`); n != procs {
		t.Fatalf("Zeilen von %d Prozessen, erwartet %d", n, procs)
	}
	if n := countRows(t, db, `SELECT count(*) FROM schema_migrations WHERE module != 'core'`); n != len(baseFiles) {
		t.Fatalf("schema_migrations hat %d Zeilen, erwartet %d", n, len(baseFiles))
	}
	// Serialisierung belegt: jeder gelesene Zählerstand kommt genau einmal vor
	if n := countRows(t, db, `SELECT count(DISTINCT n) FROM t_items`); n != procs*childWrites {
		t.Fatalf("%d verschiedene Zählerstände, erwartet %d (Transaktionen nicht serialisiert)", n, procs*childWrites)
	}
}
