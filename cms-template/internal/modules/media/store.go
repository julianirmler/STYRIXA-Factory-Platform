package media

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	maxFolderDepth = 3
	maxFolderName  = 80
	maxFileName    = 120
	maxAlt         = 250
	pageSize       = 60 // Dateien je Seite im Admin und im Bildwähler
)

// fileRE: öffentlicher Dateiname <key>-<breite>.<webp|jpg>. Alles andere ist
// 404, ohne die Datenbank zu fragen.
var fileRE = regexp.MustCompile(`^([a-z2-7]{26})-([1-9][0-9]{0,3})\.(webp|jpg)$`)

var keyEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// newKey: 128 Bit Zufall → 26 Zeichen [a-z2-7].
func newKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return strings.ToLower(keyEnc.EncodeToString(b))
}

// errInput ist ein Eingabefehler; die Meldung geht so an den Kunden.
type errInput string

func (e errInput) Error() string { return string(e) }

var errNotFound = errors.New("media: nicht gefunden")

type folder struct {
	ID       int64
	ParentID sql.NullInt64
	Name     string
}

type file struct {
	ID         int64
	FolderID   sql.NullInt64
	Key        string
	Name       string
	Alt        string
	Decorative bool
	Mime       string
	Width      int
	Height     int
	Bytes      int64
	CreatedAt  time.Time
	Variants   []fileVariant
}

type fileVariant struct{ Width, Height int }

// url liefert die öffentliche URL einer Variante.
func (f file) url(width int) string {
	return "/media/" + f.Key + "-" + strconv.Itoa(width) + "." + ext(f.Mime)
}

// src ist die Variante für src: die größte ≤ 960 px, sonst die kleinste.
func (f file) src() string {
	if len(f.Variants) == 0 {
		return ""
	}
	best := f.Variants[0].Width
	for _, v := range f.Variants {
		if v.Width <= 960 {
			best = v.Width
		}
	}
	return f.url(best)
}

// thumb ist die kleinste Variante (Raster im Admin).
func (f file) thumb() string {
	if len(f.Variants) == 0 {
		return ""
	}
	return f.url(f.Variants[0].Width)
}

type store struct {
	db  *sql.DB
	dir string // <data>/media
	now func() time.Time
}

func (s *store) time() string { return s.now().UTC().Format(time.RFC3339) }

// cleanName macht aus einer Eingabe einen Anzeigenamen: ohne Steuerzeichen,
// Leerraum zusammengefasst, höchstens max Zeichen.
func cleanName(s string, max int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = strings.TrimSpace(string([]rune(s)[:max]))
	}
	return s
}

// ---------- Ordner ----------

func (s *store) folder(ctx context.Context, id int64) (folder, error) {
	var f folder
	err := s.db.QueryRowContext(ctx, `SELECT id, parent_id, name FROM media_folders WHERE id = ?`, id).
		Scan(&f.ID, &f.ParentID, &f.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return f, errNotFound
	}
	return f, err
}

// trail liefert den Ordner und alle Eltern, Wurzel zuerst.
func (s *store) trail(ctx context.Context, id int64) ([]folder, error) {
	var out []folder
	for i := 0; i <= maxFolderDepth && id != 0; i++ {
		f, err := s.folder(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append([]folder{f}, out...)
		id = f.ParentID.Int64
	}
	return out, nil
}

func (s *store) children(ctx context.Context, parent int64) ([]folder, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, parent_id, name FROM media_folders WHERE ifnull(parent_id, 0) = ? ORDER BY name`, parent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []folder
	for rows.Next() {
		var f folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func (s *store) createFolder(ctx context.Context, parent int64, name string) (int64, error) {
	name = cleanName(name, maxFolderName)
	if name == "" {
		return 0, errInput("Bitte einen Ordnernamen eingeben.")
	}
	if parent != 0 {
		t, err := s.trail(ctx, parent)
		if errors.Is(err, errNotFound) {
			return 0, errInput("Der übergeordnete Ordner existiert nicht mehr.")
		}
		if err != nil {
			return 0, err
		}
		if len(t) >= maxFolderDepth {
			return 0, errInput(fmt.Sprintf("Ordner dürfen höchstens %d Ebenen tief sein.", maxFolderDepth))
		}
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO media_folders (parent_id, name, created_at) VALUES (?, ?, ?)`,
		nullID(parent), name, s.time())
	if isUnique(err) {
		return 0, errInput("Einen Ordner mit diesem Namen gibt es hier schon.")
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *store) renameFolder(ctx context.Context, id int64, name string) error {
	name = cleanName(name, maxFolderName)
	if name == "" {
		return errInput("Bitte einen Ordnernamen eingeben.")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE media_folders SET name = ? WHERE id = ?`, name, id)
	if isUnique(err) {
		return errInput("Einen Ordner mit diesem Namen gibt es hier schon.")
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// deleteFolder löscht nur leere Ordner (keine Unterordner, keine Dateien).
func (s *store) deleteFolder(ctx context.Context, id int64) (parent int64, err error) {
	f, err := s.folder(ctx, id)
	if err != nil {
		return 0, err
	}
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT count(*) FROM media_folders WHERE parent_id = ?) + (SELECT count(*) FROM media_files WHERE folder_id = ?)`,
		id, id).Scan(&n); err != nil {
		return 0, err
	}
	if n > 0 {
		return 0, errInput("Der Ordner ist nicht leer – bitte zuerst Dateien und Unterordner verschieben oder löschen.")
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM media_folders WHERE id = ?`, id)
	return f.ParentID.Int64, err
}

// allFolders liefert alle Ordner mit vollem Pfad (für „Verschieben nach“).
func (s *store) allFolders(ctx context.Context) ([]folderOption, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, parent_id, name FROM media_folders`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]folder{}
	for rows.Next() {
		var f folder
		if err := rows.Scan(&f.ID, &f.ParentID, &f.Name); err != nil {
			return nil, err
		}
		byID[f.ID] = f
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]folderOption, 0, len(byID))
	for _, f := range byID {
		label := f.Name
		for p, i := f.ParentID, 0; p.Valid && i < maxFolderDepth; i++ {
			pf := byID[p.Int64]
			label, p = pf.Name+" / "+label, pf.ParentID
		}
		out = append(out, folderOption{ID: f.ID, Label: label})
	}
	sortOptions(out)
	return out, nil
}

type folderOption struct {
	ID    int64
	Label string
}

func sortOptions(o []folderOption) {
	slices.SortFunc(o, func(a, b folderOption) int {
		return strings.Compare(strings.ToLower(a.Label), strings.ToLower(b.Label))
	})
}

// ---------- Dateien ----------

const fileCols = `id, folder_id, key, name, alt, decorative, mime, width, height, bytes, created_at`

func scanFile(sc interface{ Scan(...any) error }) (file, error) {
	var f file
	var created string
	err := sc.Scan(&f.ID, &f.FolderID, &f.Key, &f.Name, &f.Alt, &f.Decorative, &f.Mime, &f.Width, &f.Height, &f.Bytes, &created)
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return f, err
}

func (s *store) file(ctx context.Context, id int64) (file, error) {
	f, err := scanFile(s.db.QueryRowContext(ctx, `SELECT `+fileCols+` FROM media_files WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, errNotFound
	}
	if err != nil {
		return f, err
	}
	err = s.loadVariants(ctx, []*file{&f})
	return f, err
}

// files listet Dateien eines Ordners (q = "") oder sucht in Name und
// Alt-Text über alle Ordner. Neueste zuerst; more = es gibt weitere.
func (s *store) files(ctx context.Context, folderID int64, q string, offset int) (out []file, more bool, err error) {
	query := `SELECT ` + fileCols + ` FROM media_files WHERE ifnull(folder_id, 0) = ?`
	args := []any{folderID}
	if q = strings.TrimSpace(q); q != "" {
		query = `SELECT ` + fileCols + ` FROM media_files WHERE instr(lower(name), lower(?)) > 0 OR instr(lower(alt), lower(?)) > 0`
		args = []any{q, q}
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize+1, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(out) > pageSize {
		out, more = out[:pageSize], true
	}
	ptrs := make([]*file, len(out))
	for i := range out {
		ptrs[i] = &out[i]
	}
	return out, more, s.loadVariants(ctx, ptrs)
}

func (s *store) loadVariants(ctx context.Context, fs []*file) error {
	for _, f := range fs {
		rows, err := s.db.QueryContext(ctx, `SELECT width, height FROM media_variants WHERE file_id = ? ORDER BY width`, f.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v fileVariant
			if err := rows.Scan(&v.Width, &v.Height); err != nil {
				rows.Close()
				return err
			}
			f.Variants = append(f.Variants, v)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// meta sind die bearbeitbaren Angaben einer Datei.
type meta struct {
	Name       string
	Alt        string
	Decorative bool
	FolderID   int64
}

func (s *store) checkMeta(ctx context.Context, m *meta) error {
	m.Name = cleanName(m.Name, maxFileName)
	if m.Name == "" {
		m.Name = "Bild"
	}
	m.Alt = cleanName(m.Alt, maxAlt)
	if m.Decorative {
		m.Alt = ""
	} else if m.Alt == "" {
		return errInput("Bitte einen Alternativtext eingeben – oder „Dekoratives Bild“ anhaken, wenn das Bild nichts aussagt.")
	}
	if m.FolderID != 0 {
		if _, err := s.folder(ctx, m.FolderID); errors.Is(err, errNotFound) {
			return errInput("Der gewählte Ordner existiert nicht mehr.")
		} else if err != nil {
			return err
		}
	}
	return nil
}

// create speichert die Varianten (aufsteigend sortiert, geprüft) und legt den
// Eintrag an. Dateien werden atomar geschrieben; scheitert die Datenbank,
// werden sie wieder gelöscht.
func (s *store) create(ctx context.Context, m meta, vs []variant, userID int64) (file, error) {
	if err := s.checkMeta(ctx, &m); err != nil {
		return file{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return file{}, err
	}
	key := newKey()
	f := file{Key: key, Name: m.Name, Alt: m.Alt, Decorative: m.Decorative, FolderID: nullID(m.FolderID), Mime: vs[0].Mime}
	var written []string
	cleanup := func() {
		for _, p := range written {
			os.Remove(p)
		}
	}
	for _, v := range vs {
		p := filepath.Join(s.dir, filepath.Base(strings.TrimPrefix(f.url(v.Width), "/media/")))
		if err := writeAtomic(p, v.Data); err != nil {
			cleanup()
			return file{}, err
		}
		written = append(written, p)
		f.Bytes += int64(len(v.Data))
		f.Variants = append(f.Variants, fileVariant{v.Width, v.Height})
	}
	big := vs[len(vs)-1]
	f.Width, f.Height = big.Width, big.Height

	err := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		now := s.time()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO media_files (folder_id, key, name, alt, decorative, mime, width, height, bytes, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.FolderID, key, f.Name, f.Alt, f.Decorative, f.Mime, f.Width, f.Height, f.Bytes, nullID(userID), now, now)
		if err != nil {
			return err
		}
		if f.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, v := range vs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO media_variants (file_id, width, height, bytes) VALUES (?, ?, ?, ?)`,
				f.ID, v.Width, v.Height, len(v.Data)); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	if err != nil {
		cleanup()
		return file{}, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, s.time())
	return f, nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		os.Remove(tmp.Name())
	}
	return werr
}

func (s *store) update(ctx context.Context, id int64, m meta) error {
	if err := s.checkMeta(ctx, &m); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE media_files SET name = ?, alt = ?, decorative = ?, folder_id = ?, updated_at = ? WHERE id = ?`,
		m.Name, m.Alt, m.Decorative, nullID(m.FolderID), s.time(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errNotFound
	}
	return nil
}

// remove löscht Eintrag und Dateien. Fehlt eine Datei schon, ist das egal.
func (s *store) remove(ctx context.Context, id int64) (file, error) {
	f, err := s.file(ctx, id)
	if err != nil {
		return f, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id); err != nil {
		return f, err
	}
	for _, v := range f.Variants {
		p := filepath.Join(s.dir, strings.TrimPrefix(f.url(v.Width), "/media/"))
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return f, err
		}
	}
	return f, nil
}

// lookup liefert den Inhaltstyp einer öffentlichen Variante; ok = false: gibt es nicht.
func (s *store) lookup(ctx context.Context, key string, width int, extension string) (mime string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT f.mime FROM media_files f JOIN media_variants v ON v.file_id = f.id WHERE f.key = ? AND v.width = ?`,
		key, width).Scan(&mime)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return mime, ext(mime) == extension, nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
