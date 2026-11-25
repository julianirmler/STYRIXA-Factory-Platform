-- Kern: Lazy-Jobs (Session 1.6). Zeiten als Unix-Sekunden (UTC).
-- Zeilen legt core.Jobs beim ersten Sehen eines registrierten Jobs an.
CREATE TABLE core_jobs (
	name         TEXT PRIMARY KEY,
	interval_s   INTEGER NOT NULL,
	last_run     INTEGER,          -- Start des letzten Laufs (auch bei Fehler)
	locked_until INTEGER,          -- gesetzt, solange ein Prozess den Job ausführt
	last_error   TEXT              -- NULL = letzter Lauf erfolgreich
);
