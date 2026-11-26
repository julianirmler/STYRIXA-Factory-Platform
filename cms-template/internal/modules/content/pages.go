package content

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Datenzugriff für Seiten, Revisionen und Weiterleitungen. Jede Anfrage liest
// aus SQLite (CGI: kein Cache als Wahrheit). Schreibende Vorgänge laufen in
// einer Transaktion; BEGIN IMMEDIATE (DSN des Kerns) serialisiert sie.

const (
	maxTitleLen       = 200
	keepRevisions     = 50
	emptyDoc          = `{"blocks":[]}`
	statusDraft       = "draft"
	statusPublished   = "published"
	msgPageNotFound   = "Die Seite gibt es nicht (mehr)."
	msgPathTaken      = "Diese Adresse ist bereits vergeben."
	msgPathReserved   = "Diese Adresse ist für das System reserviert."
	msgHasChildren    = "Die Seite hat Unterseiten. Bitte zuerst die Unterseiten löschen oder verschieben."
	msgBadParent      = "Eine Seite kann nicht unter sich selbst oder einer ihrer Unterseiten liegen."
	msgTooDeep        = "Seiten dürfen höchstens 5 Ebenen tief verschachtelt sein."
	msgPathTooLong    = "Die Adresse ist zu lang (höchstens 255 Zeichen)."
	msgTitleMissing   = "Bitte einen Titel angeben."
	msgTitleTooLong   = "Der Titel ist zu lang (höchstens 200 Zeichen)."
	msgSlugInvalid    = "Die Adresse darf nur Kleinbuchstaben, Ziffern und Bindestriche enthalten."
	msgParentNotFound = "Die gewählte Elternseite gibt es nicht."
)

var errNotFound = &InputError{Msg: msgPageNotFound}

type store struct {
	db  *sql.DB
	now func() time.Time // nil = time.Now
}

func (s *store) unix() int64 {
	if s.now != nil {
		return s.now().Unix()
	}
	return time.Now().Unix()
}

// page ist eine Zeile aus content_pages.
type page struct {
	ID        int64
	ParentID  int64 // 0 = oberste Ebene
	Slug      string
	Path      string
	Title     string
	Status    string
	IsHome    bool
	PubRev    int64 // 0 = nicht veröffentlicht
	UpdatedAt int64
}

const pageCols = `id, IFNULL(parent_id, 0), slug, path, title, status, is_home, IFNULL(published_revision_id, 0), updated_at`

type scanner interface{ Scan(...any) error }

func scanPage(row scanner) (page, error) {
	var p page
	err := row.Scan(&p.ID, &p.ParentID, &p.Slug, &p.Path, &p.Title, &p.Status, &p.IsHome, &p.PubRev, &p.UpdatedAt)
	return p, err
}

type querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func getPage(ctx context.Context, q querier, id int64) (page, error) {
	p, err := scanPage(q.QueryRowContext(ctx, `SELECT `+pageCols+` FROM content_pages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	return p, err
}

func (s *store) page(ctx context.Context, id int64) (page, error) { return getPage(ctx, s.db, id) }

// pages liefert alle Seiten, nach Pfad sortiert (Eltern vor Kindern).
func (s *store) pages(ctx context.Context) ([]page, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+pageCols+` FROM content_pages ORDER BY path COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []page
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func checkTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	switch {
	case title == "":
		return "", badInput(msgTitleMissing)
	case utf8.RuneCountInString(title) > maxTitleLen:
		return "", badInput(msgTitleTooLong)
	}
	return title, nil
}

func depth(path string) int { return strings.Count(path, "/") + 1 }

func joinPath(parent, slug string) string {
	if parent == "" {
		return slug
	}
	return parent + "/" + slug
}

func pathExists(ctx context.Context, q querier, path string, except int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_pages WHERE path = ? AND id <> ?`, path, except).Scan(&n)
	return n > 0, err
}

// parentPath liefert den Pfad der Elternseite ("" für die oberste Ebene).
func parentPath(ctx context.Context, q querier, parentID int64) (string, error) {
	if parentID == 0 {
		return "", nil
	}
	p, err := getPage(ctx, q, parentID)
	if errors.Is(err, errNotFound) {
		return "", badInput(msgParentNotFound)
	}
	return p.Path, err
}

// create legt eine Seite mit leerer erster Revision an. Der Slug kommt aus dem
// Titel; bei Kollision unter derselben Elternseite -2, -3 …
func (s *store) create(ctx context.Context, title string, parentID, author int64) (id int64, err error) {
	if title, err = checkTitle(title); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx, &err)
	pp, err := parentPath(ctx, tx, parentID)
	if err != nil {
		return 0, err
	}
	if pp != "" && depth(pp)+1 > maxDepth {
		return 0, badInput(msgTooDeep)
	}
	base := Slugify(title)
	if base == "" {
		base = fallback
	}
	if pp == "" && isReserved(base) {
		base = withSuffix(base, 2)
	}
	slug := base
	for n := 2; ; n++ {
		taken, err := pathExists(ctx, tx, joinPath(pp, slug), 0)
		if err != nil {
			return 0, err
		}
		if !taken && !(pp == "" && isReserved(slug)) {
			break
		}
		slug = withSuffix(base, n)
	}
	path := joinPath(pp, slug)
	if len(path) > maxPathLen {
		return 0, badInput(msgPathTooLong)
	}
	now := s.unix()
	var parent any
	if parentID != 0 {
		parent = parentID
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO content_pages (parent_id, slug, path, title, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, parent, slug, path, title, now, now)
	if err != nil {
		return 0, err
	}
	if id, err = res.LastInsertId(); err != nil {
		return 0, err
	}
	if _, err = addRevision(ctx, tx, id, title, FormatBlocks, emptyDoc, author, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM content_redirects WHERE from_path = ?`, path); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func addRevision(ctx context.Context, q querier, pageID int64, title, format, body string, author, now int64) (int64, error) {
	var a any
	if author != 0 {
		a = author
	}
	res, err := q.ExecContext(ctx, `INSERT INTO content_revisions (page_id, title, format, body, author_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, pageID, title, format, body, a, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// saveDraft speichert eine neue Revision. Validate muss vorher gelaufen sein.
func (s *store) saveDraft(ctx context.Context, id int64, title, format, body string, author int64) (rev int64, err error) {
	if title, err = checkTitle(title); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx, &err)
	if _, err = getPage(ctx, tx, id); err != nil {
		return 0, err
	}
	now := s.unix()
	if rev, err = addRevision(ctx, tx, id, title, format, body, author, now); err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content_pages SET title = ?, updated_at = ? WHERE id = ?`, title, now, id); err != nil {
		return 0, err
	}
	return rev, tx.Commit()
}

// publish macht die neueste Revision öffentlich.
func (s *store) publish(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE content_pages SET status = 'published', updated_at = ?,
		published_revision_id = (SELECT MAX(id) FROM content_revisions WHERE page_id = content_pages.id)
		WHERE id = ? AND EXISTS (SELECT 1 FROM content_revisions WHERE page_id = content_pages.id)`, s.unix(), id)
	return affected(res, err)
}

func (s *store) unpublish(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE content_pages SET status = 'draft', published_revision_id = NULL, updated_at = ?
		WHERE id = ?`, s.unix(), id)
	return affected(res, err)
}

func (s *store) delete(ctx context.Context, id int64) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx, &err)
	if _, err = getPage(ctx, tx, id); err != nil {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_pages WHERE parent_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return badInput(msgHasChildren)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM content_pages WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// settings ändert Slug, Elternseite und Startseiten-Markierung. Ändert sich
// der Pfad, ziehen alle Unterseiten mit, und jeder alte Pfad leitet weiter.
func (s *store) settings(ctx context.Context, id int64, slugInput string, parentID int64, home bool) (err error) {
	slug := strings.TrimSpace(slugInput)
	if !validSlug.MatchString(slug) {
		if slug = Slugify(slug); slug == "" {
			return badInput(msgSlugInvalid)
		}
	}
	if len(slug) > maxSlugLen {
		return badInput(msgPathTooLong)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx, &err)
	p, err := getPage(ctx, tx, id)
	if err != nil {
		return err
	}
	pp, err := parentPath(ctx, tx, parentID)
	if err != nil {
		return err
	}
	if parentID == id || pp == p.Path || strings.HasPrefix(strings.ToLower(pp), strings.ToLower(p.Path)+"/") {
		return badInput(msgBadParent)
	}
	if pp == "" && isReserved(slug) {
		return badInput(msgPathReserved)
	}
	newPath := joinPath(pp, slug)
	if newPath != p.Path {
		if err = movePath(ctx, tx, p, newPath); err != nil {
			return err
		}
	}
	var parent any
	if parentID != 0 {
		parent = parentID
	}
	now := s.unix()
	if home && !p.IsHome {
		if _, err = tx.ExecContext(ctx, `UPDATE content_pages SET is_home = 0 WHERE is_home = 1`); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content_pages SET parent_id = ?, slug = ?, is_home = ?, updated_at = ? WHERE id = ?`,
		parent, slug, home, now, id); err != nil {
		return err
	}
	return tx.Commit()
}

// movePath verschiebt p samt Unterseiten nach newPath und trägt die alten Pfade
// als Weiterleitungen ein.
func movePath(ctx context.Context, tx *sql.Tx, p page, newPath string) error {
	if !strings.EqualFold(newPath, p.Path) {
		taken, err := pathExists(ctx, tx, newPath, p.ID)
		if err != nil {
			return err
		}
		if taken {
			return badInput(msgPathTaken)
		}
	}
	// Tiefe und Länge des ganzen Teilbaums nach dem Umzug prüfen.
	var maxD, maxL int
	rows, err := tx.QueryContext(ctx, `SELECT path FROM content_pages WHERE id = ? OR path LIKE ? ESCAPE '\'`, p.ID, likePrefix(p.Path))
	if err != nil {
		return err
	}
	for rows.Next() {
		var old string
		if err := rows.Scan(&old); err != nil {
			rows.Close()
			return err
		}
		np := newPath + old[len(p.Path):]
		maxD, maxL = max(maxD, depth(np)), max(maxL, len(np))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	switch {
	case maxD > maxDepth:
		return badInput(msgTooDeep)
	case maxL > maxPathLen:
		return badInput(msgPathTooLong)
	}
	sub := `(id = ? OR path LIKE ? ESCAPE '\')`
	if !strings.EqualFold(newPath, p.Path) { // nur Groß/klein geändert: kein Redirect nötig
		if _, err := tx.ExecContext(ctx, `INSERT INTO content_redirects (from_path, page_id)
			SELECT path, id FROM content_pages WHERE `+sub+`
			ON CONFLICT(from_path) DO UPDATE SET page_id = excluded.page_id`, p.ID, likePrefix(p.Path)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE content_pages SET path = ? || substr(path, ?) WHERE `+sub,
		newPath, len(p.Path)+1, p.ID, likePrefix(p.Path)); err != nil {
		return err
	}
	// Ein Pfad, auf dem jetzt eine echte Seite liegt, leitet nicht mehr um.
	_, err = tx.ExecContext(ctx, `DELETE FROM content_redirects WHERE from_path IN (SELECT path FROM content_pages)`)
	return err
}

// likePrefix: LIKE-Muster für alle Unterseiten von path. Slugs enthalten nur
// [a-z0-9-], escaped wird trotzdem.
func likePrefix(path string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(path) + "/%"
}

// published liefert die veröffentlichte Revision zu einem Pfad.
type published struct {
	page
	RevTitle, Format, Body string
}

const publishedSQL = `SELECT p.id, IFNULL(p.parent_id, 0), p.slug, p.path, p.title, p.status, p.is_home,
	IFNULL(p.published_revision_id, 0), p.updated_at, r.title, r.format, r.body
	FROM content_pages p JOIN content_revisions r ON r.id = p.published_revision_id
	WHERE p.status = 'published' AND `

func scanPublished(row *sql.Row) (published, bool, error) {
	var x published
	err := row.Scan(&x.ID, &x.ParentID, &x.Slug, &x.Path, &x.Title, &x.Status, &x.IsHome, &x.PubRev, &x.UpdatedAt,
		&x.RevTitle, &x.Format, &x.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return x, false, nil
	}
	return x, err == nil, err
}

func (s *store) home(ctx context.Context) (published, bool, error) {
	return scanPublished(s.db.QueryRowContext(ctx, publishedSQL+`p.is_home = 1`))
}

// lookup sucht eine Seite unter path. Ist sie ein Entwurf, gilt sie als nicht
// vorhanden – auch Weiterleitungen auf Entwürfe gibt es nicht.
func (s *store) lookup(ctx context.Context, path string) (x published, redirect bool, ok bool, err error) {
	var n int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM content_pages WHERE path = ?`, path).Scan(&n); err != nil {
		return x, false, false, err
	}
	if n > 0 {
		x, ok, err = scanPublished(s.db.QueryRowContext(ctx, publishedSQL+`p.path = ?`, path))
		return x, false, ok, err
	}
	x, ok, err = scanPublished(s.db.QueryRowContext(ctx, publishedSQL+
		`p.id = (SELECT page_id FROM content_redirects WHERE from_path = ?)`, path))
	return x, ok, ok, err
}

// prune löscht je Seite alle Revisionen außer den 50 neuesten – die
// veröffentlichte nie.
func (s *store) prune(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM content_revisions WHERE id IN (
		SELECT id FROM (SELECT id, ROW_NUMBER() OVER (PARTITION BY page_id ORDER BY id DESC) AS n FROM content_revisions)
		WHERE n > ?)
		AND id NOT IN (SELECT published_revision_id FROM content_pages WHERE published_revision_id IS NOT NULL)`, keepRevisions)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		if err != nil {
			return err
		}
		return errNotFound
	}
	return nil
}

func rollback(tx *sql.Tx, err *error) {
	if *err != nil {
		_ = tx.Rollback()
	}
}
