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
	return insertRevision(ctx, q, pageID, title, format, body, author, now, false)
}

func insertRevision(ctx context.Context, q querier, pageID int64, title, format, body string, author, now int64, auto bool) (int64, error) {
	var a any
	if author != 0 {
		a = author
	}
	res, err := q.ExecContext(ctx, `INSERT INTO content_revisions (page_id, title, format, body, author_id, created_at, saved_at, autosave)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, pageID, title, format, body, a, now, now, auto)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// autosaveWindow: so lange überschreibt ein Auto-Save die eigene letzte
// Auto-Save-Revision; danach beginnt eine neue (≈ 6 Revisionen je Stunde).
const autosaveWindow = 10 * 60

// draftSave beschreibt einen Speichervorgang (POST …/draft).
type draftSave struct {
	Title, Format, Body string
	Author              int64
	Base                int64 // Revision, auf der der Editor aufbaut; 0 = unbekannt
	Auto                bool
}

// newerRevision ist eine Revision, die seit Base von anderswo gespeichert
// wurde. Sie wird trotzdem überholt (letzte Revision gewinnt), der Editor
// bekommt einen Hinweis.
type newerRevision struct {
	ID          int64
	AuthorID    int64
	AuthorEmail string
	SavedAt     int64
}

// saveDraft speichert eine Revision. Validate muss vorher gelaufen sein.
// Auto-Saves fassen sich zusammen (siehe autosaveWindow), wenn die neueste
// Revision ein Auto-Save desselben Autors ist, auf dem der Editor aufbaut, und
// sie nicht veröffentlicht ist.
func (s *store) saveDraft(ctx context.Context, id int64, d draftSave) (rev int64, newer *newerRevision, err error) {
	title, err := checkTitle(d.Title)
	if err != nil {
		return 0, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, err
	}
	defer rollback(tx, &err)
	p, err := getPage(ctx, tx, id)
	if err != nil {
		return 0, nil, err
	}
	var last struct {
		id, author, created int64
		auto                bool
	}
	err = tx.QueryRowContext(ctx, `SELECT r.id, IFNULL(r.author_id, 0), r.created_at, r.autosave FROM content_revisions r
		WHERE r.page_id = ? ORDER BY r.id DESC LIMIT 1`, id).Scan(&last.id, &last.author, &last.created, &last.auto)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, nil, err
	}
	if d.Base != 0 && last.id != 0 && last.id != d.Base {
		newer = &newerRevision{ID: last.id, AuthorID: last.author}
		if err = tx.QueryRowContext(ctx, `SELECT r.saved_at, IFNULL(u.email, '') FROM content_revisions r
			LEFT JOIN core_users u ON u.id = r.author_id WHERE r.id = ?`, last.id).Scan(&newer.SavedAt, &newer.AuthorEmail); err != nil {
			return 0, nil, err
		}
	}
	now := s.unix()
	if d.Auto && newer == nil && last.auto && last.author != 0 && last.author == d.Author &&
		last.id != p.PubRev && now-last.created < autosaveWindow {
		rev = last.id
		_, err = tx.ExecContext(ctx, `UPDATE content_revisions SET title = ?, format = ?, body = ?, saved_at = ? WHERE id = ?`,
			title, d.Format, d.Body, now, rev)
	} else {
		rev, err = insertRevision(ctx, tx, id, title, d.Format, d.Body, d.Author, now, d.Auto)
	}
	if err != nil {
		return 0, nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE content_pages SET title = ?, updated_at = ? WHERE id = ?`, title, now, id); err != nil {
		return 0, nil, err
	}
	return rev, newer, tx.Commit()
}

// revision ist eine Zeile der Revisionsliste.
type revision struct {
	ID          int64
	Title       string
	Format      string
	Body        string
	AuthorEmail string
	SavedAt     int64
	Auto        bool
}

// revisions liefert die Revisionen einer Seite, neueste zuerst (ohne Body).
func (s *store) revisions(ctx context.Context, id int64) ([]revision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.title, r.format, IFNULL(u.email, ''), r.saved_at, r.autosave
		FROM content_revisions r LEFT JOIN core_users u ON u.id = r.author_id
		WHERE r.page_id = ? ORDER BY r.id DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []revision
	for rows.Next() {
		var r revision
		if err := rows.Scan(&r.ID, &r.Title, &r.Format, &r.AuthorEmail, &r.SavedAt, &r.Auto); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var errRevNotFound = &InputError{Msg: "Diese Revision gibt es nicht (mehr)."}

// revision liefert eine Revision der Seite id samt Body.
func (s *store) revision(ctx context.Context, id, revID int64) (revision, error) {
	var r revision
	err := s.db.QueryRowContext(ctx, `SELECT id, title, format, body, saved_at, autosave FROM content_revisions
		WHERE id = ? AND page_id = ?`, revID, id).Scan(&r.ID, &r.Title, &r.Format, &r.Body, &r.SavedAt, &r.Auto)
	if errors.Is(err, sql.ErrNoRows) {
		return r, errRevNotFound
	}
	return r, err
}

// restore legt eine neue Revision mit dem Inhalt von revID an – die Historie
// bleibt vollständig. Die Rolle prüft Validate wie beim Speichern: Raw-HTML
// und HTML-Modus darf nur ein Admin zurückholen.
func (s *store) restore(ctx context.Context, id, revID, author int64, admin bool) (int64, error) {
	old, err := s.revision(ctx, id, revID)
	if err != nil {
		return 0, err
	}
	if err := Validate(old.Body, old.Format, admin); err != nil {
		return 0, err
	}
	rev, _, err := s.saveDraft(ctx, id, draftSave{Title: old.Title, Format: old.Format, Body: old.Body, Author: author})
	return rev, err
}

// switchFormat wechselt den Entwurf in den Block- oder HTML-Modus (nur
// Admin, prüft der Handler). Blöcke → HTML übernimmt den gerenderten Inhalt;
// HTML → Blöcke beginnt leer (der HTML-Stand bleibt als Revision).
func (s *store) switchFormat(ctx context.Context, id int64, format string, author int64) error {
	if format != FormatBlocks && format != FormatHTML {
		return badInput("Unbekanntes Format %q.", format)
	}
	if _, err := s.page(ctx, id); err != nil {
		return err
	}
	d, err := s.latestDraft(ctx, id)
	if err != nil || d.Format == format {
		return err
	}
	body := emptyDoc
	if format == FormatHTML {
		h, err := Render(d.Body, d.Format)
		if err != nil {
			return err
		}
		body = string(h)
	}
	title := d.Title
	if title == "" {
		p, err := s.page(ctx, id)
		if err != nil {
			return err
		}
		title = p.Title
	}
	_, _, err = s.saveDraft(ctx, id, draftSave{Title: title, Format: format, Body: body, Author: author})
	return err
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

// draft ist die neueste Revision einer Seite (= Entwurf).
type draft struct {
	RevID         int64
	Title, Format string
	Body          string
}

// latestDraft liefert die neueste Revision; ohne Revision ein leeres Dokument.
func (s *store) latestDraft(ctx context.Context, id int64) (draft, error) {
	d := draft{Format: FormatBlocks, Body: emptyDoc}
	err := s.db.QueryRowContext(ctx, `SELECT id, title, format, body FROM content_revisions
		WHERE page_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&d.RevID, &d.Title, &d.Format, &d.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	return d, err
}

// mediaRefs: Titel der Seiten, deren neueste oder veröffentlichte Revision
// needle enthält (Medien-URL, Session 4.1). instr statt LIKE: kein Escapen
// von % und _ nötig.
func (s *store) mediaRefs(ctx context.Context, needle string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.title FROM content_pages p
		 WHERE EXISTS (SELECT 1 FROM content_revisions r WHERE r.page_id = p.id
		   AND (r.id = p.published_revision_id OR r.id = (SELECT max(id) FROM content_revisions WHERE page_id = p.id))
		   AND instr(r.body, ?) > 0)
		 ORDER BY p.path`, needle)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
