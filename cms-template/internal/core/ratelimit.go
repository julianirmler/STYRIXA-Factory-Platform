package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Rate-Limit für Login und Passwort-Reset, vollständig in SQLite
// (core_login_attempts) – im CGI-Modus gibt es keinen Prozess, der zählen könnte.
const (
	attemptWindow   = 15 * time.Minute
	maxPerEmail     = 5
	maxPerIP        = 20
	msgTooManyLogin = "Zu viele Fehlversuche. Bitte in 15 Minuten erneut versuchen."
	msgTooManyReset = "Zu viele Anfragen. Bitte in 15 Minuten erneut versuchen."
)

// limitKeys liefert die Zähler-Schlüssel und ihre Grenzen für E-Mail und IP.
// prefix trennt Login („“) und Reset („reset-“).
func limitKeys(prefix, email, ip string) []limitKey {
	return []limitKey{
		{prefix + "email:" + strings.ToLower(strings.TrimSpace(email)), maxPerEmail},
		{prefix + "ip:" + ip, maxPerIP},
	}
}

type limitKey struct {
	key string
	max int
}

// tooMany meldet, ob einer der Zähler seine Grenze im Fenster erreicht hat.
func tooMany(ctx context.Context, q dbtx, now time.Time, keys []limitKey) (bool, error) {
	since := now.Add(-attemptWindow).Unix()
	for _, k := range keys {
		var n int
		if err := q.QueryRowContext(ctx,
			`SELECT count(*) FROM core_login_attempts WHERE key = ? AND at > ?`, k.key, since).Scan(&n); err != nil {
			return false, fmt.Errorf("ratelimit: %w", err)
		}
		if n >= k.max {
			return true, nil
		}
	}
	return false, nil
}

// recordAttempt zählt einen Versuch für alle Schlüssel und räumt dabei alte
// Einträge ab (kein Scheduler nötig).
func recordAttempt(ctx context.Context, q dbtx, now time.Time, keys []limitKey) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM core_login_attempts WHERE at <= ?`,
		now.Add(-attemptWindow).Unix()); err != nil {
		return fmt.Errorf("ratelimit: %w", err)
	}
	for _, k := range keys {
		if _, err := q.ExecContext(ctx, `INSERT INTO core_login_attempts (key, at) VALUES (?, ?)`,
			k.key, now.Unix()); err != nil {
			return fmt.Errorf("ratelimit: %w", err)
		}
	}
	return nil
}

// clearAttempts löscht die Zähler eines Schlüssels (nach erfolgreichem Login
// für die E-Mail – der IP-Zähler bleibt, sonst könnte ein Angreifer mit einem
// eigenen Konto sein IP-Limit zurücksetzen).
func clearAttempts(ctx context.Context, q dbtx, key string) error {
	_, err := q.ExecContext(ctx, `DELETE FROM core_login_attempts WHERE key = ?`, key)
	return err
}
