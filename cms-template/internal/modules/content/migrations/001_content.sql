-- Content (Session 3.1): Seiten mit Hierarchie, Revisionen, Weiterleitungen.
-- Zeiten als Unix-Sekunden (UTC).

-- path ist der volle Pfad ohne führenden „/“ (z. B. „leistungen/webdesign“) und
-- die Wahrheit für die öffentliche Auflösung; slug ist das letzte Segment.
-- Entwurf = neueste Revision; veröffentlicht = published_revision_id (bewusst
-- ohne FK, sonst zirkulärer Bezug; Konsistenz sichern Code und CHECK).
CREATE TABLE content_pages (
	id                    INTEGER PRIMARY KEY,
	parent_id             INTEGER REFERENCES content_pages(id) ON DELETE RESTRICT,
	slug                  TEXT    NOT NULL,
	path                  TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	title                 TEXT    NOT NULL,
	status                TEXT    NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
	is_home               INTEGER NOT NULL DEFAULT 0 CHECK (is_home IN (0, 1)),
	published_revision_id INTEGER,
	created_at            INTEGER NOT NULL,
	updated_at            INTEGER NOT NULL,
	CHECK ((status = 'published') = (published_revision_id IS NOT NULL))
);
CREATE INDEX content_pages_parent ON content_pages(parent_id);
CREATE UNIQUE INDEX content_pages_home ON content_pages(is_home) WHERE is_home = 1;

CREATE TABLE content_revisions (
	id         INTEGER PRIMARY KEY,
	page_id    INTEGER NOT NULL REFERENCES content_pages(id) ON DELETE CASCADE,
	title      TEXT    NOT NULL,
	format     TEXT    NOT NULL CHECK (format IN ('blocks', 'html')),
	body       TEXT    NOT NULL,
	author_id  INTEGER REFERENCES core_users(id) ON DELETE SET NULL,
	created_at INTEGER NOT NULL
);
CREATE INDEX content_revisions_page ON content_revisions(page_id, id);

-- Alte Pfade → Seite. Das Ziel ist immer der aktuelle path der Seite, also
-- entstehen keine Ketten. SEO/GEO: keine toten Links nach Umbenennen/Umziehen.
CREATE TABLE content_redirects (
	from_path TEXT    PRIMARY KEY COLLATE NOCASE,
	page_id   INTEGER NOT NULL REFERENCES content_pages(id) ON DELETE CASCADE
);
CREATE INDEX content_redirects_page ON content_redirects(page_id);
