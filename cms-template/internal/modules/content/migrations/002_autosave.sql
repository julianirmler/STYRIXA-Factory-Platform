-- Content (Session 3.3): Auto-Save.
-- autosave = 1: automatisch gespeichert. Solche Revisionen fasst der Server
-- zusammen (gleicher Autor, < 10 min, nicht veröffentlicht), damit Auto-Saves
-- alle 5 s die 50 aufbewahrten Revisionen nicht verdrängen.
-- saved_at = letzter Schreibzeitpunkt (≠ created_at nach dem Zusammenfassen).
ALTER TABLE content_revisions ADD COLUMN autosave INTEGER NOT NULL DEFAULT 0 CHECK (autosave IN (0, 1));
ALTER TABLE content_revisions ADD COLUMN saved_at INTEGER NOT NULL DEFAULT 0;
UPDATE content_revisions SET saved_at = created_at;
