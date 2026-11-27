-- Mediathek (Session 4.1). Ordner sind rein virtuell: Dateien liegen flach
-- unter <data>/media/<key>-<breite>.<webp|jpg>; Verschieben ändert keine URL.

CREATE TABLE media_folders (
	id         INTEGER PRIMARY KEY,
	parent_id  INTEGER REFERENCES media_folders(id) ON DELETE RESTRICT,
	name       TEXT    NOT NULL COLLATE NOCASE,
	created_at TEXT    NOT NULL
);
-- Name eindeutig je Elternordner (NULL = Wurzel zählt als eigener Elternordner).
CREATE UNIQUE INDEX media_folders_name ON media_folders (ifnull(parent_id, 0), name);

CREATE TABLE media_files (
	id         INTEGER PRIMARY KEY,
	folder_id  INTEGER REFERENCES media_folders(id) ON DELETE RESTRICT,
	key        TEXT    NOT NULL UNIQUE,              -- 128 Bit Zufall, Base32 klein (26 Zeichen)
	name       TEXT    NOT NULL,                     -- nur Anzeige
	alt        TEXT    NOT NULL DEFAULT '',
	decorative INTEGER NOT NULL DEFAULT 0 CHECK (decorative IN (0, 1)),
	mime       TEXT    NOT NULL CHECK (mime IN ('image/webp', 'image/jpeg')),
	width      INTEGER NOT NULL,                     -- größte Variante
	height     INTEGER NOT NULL,
	bytes      INTEGER NOT NULL,                     -- Summe aller Varianten
	created_by INTEGER,
	created_at TEXT    NOT NULL,
	updated_at TEXT    NOT NULL
);
CREATE INDEX media_files_folder ON media_files (folder_id, created_at);

CREATE TABLE media_variants (
	file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
	width   INTEGER NOT NULL,
	height  INTEGER NOT NULL,
	bytes   INTEGER NOT NULL,
	PRIMARY KEY (file_id, width)
) WITHOUT ROWID;
