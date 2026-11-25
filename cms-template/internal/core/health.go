package core

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"
)

// Health antwortet auf den Hoster-Check der Factory: genau „ok“, wenn die
// Datenbank antwortet und alle Migrationen angewendet sind, sonst 503 ohne Details.
func Health(db *sql.DB, mods []Module) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		var one int
		err := db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
		pending := 0
		if err == nil {
			pending, err = MigrationsPending(ctx, db, mods)
		}
		if err != nil || pending > 0 {
			slog.Warn("healthz fehlgeschlagen", "err", err, "pending", pending)
			http.Error(w, "nicht bereit", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	}
}
