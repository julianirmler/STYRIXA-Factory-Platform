-- Dashboard (Session 3.4): Seitenaufrufe je Tag und Pfad – sonst nichts.
-- Keine IP, kein User-Agent, keine Cookies, keine Uhrzeit: nur Summen, die
-- sich keiner Person zuordnen lassen (kein Cookie-Banner nötig).
-- day = Kalendertag in UTC als „YYYY-MM-DD“.
CREATE TABLE dashboard_hits (
	day   TEXT    NOT NULL,
	path  TEXT    NOT NULL,
	count INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (day, path)
) WITHOUT ROWID;
