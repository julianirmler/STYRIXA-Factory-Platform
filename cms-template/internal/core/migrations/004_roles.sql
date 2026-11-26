-- Kern: Rollen (Session 3.1). Bestehende Konten bleiben Admin.
-- admin  = alles, inkl. Raw-HTML und Einstellungen
-- editor = Inhalte bearbeiten, kein Raw-HTML (Anlegen von Editoren: Users-Modul, Phase 4)
ALTER TABLE core_users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'editor'));
