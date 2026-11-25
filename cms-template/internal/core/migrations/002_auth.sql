-- Kern: Sessions, Login-Fehlversuche, Passwort-Reset (Session 1.3)
-- Zeiten als Unix-Sekunden (UTC). Tokens nur als SHA-256 (hex).
CREATE TABLE core_sessions (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES core_users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	expires_at INTEGER NOT NULL, -- absolutes Ende (created_at + 30 Tage)
	csrf       TEXT NOT NULL
);
CREATE INDEX core_sessions_user ON core_sessions(user_id);

CREATE TABLE core_login_attempts (
	key TEXT NOT NULL, -- "email:<adresse>" | "ip:<adresse>" | "reset-…"
	at  INTEGER NOT NULL
);
CREATE INDEX core_login_attempts_key_at ON core_login_attempts(key, at);
CREATE INDEX core_login_attempts_at ON core_login_attempts(at);

CREATE TABLE core_password_resets (
	token_hash TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES core_users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
);
CREATE INDEX core_password_resets_user ON core_password_resets(user_id);
