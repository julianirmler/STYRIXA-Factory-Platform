package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Schlüssel in core_settings. Module lesen ihre Einstellungen über Deps.Settings
// mit diesen Konstanten – nie über eigene Tabellen-Zugriffe auf core_settings.
const (
	KeyInstalledAt = "core.installed_at" // RFC 3339 (UTC); gesetzt = Installation abgeschlossen

	KeySiteTitle  = "site.title"
	KeySiteRegion = "site.region" // de-AT | de-DE | de-CH
	KeySiteGISA   = "site.gisa"   // GISA-Zahl (nur de-AT), optional
	KeySiteURL    = "site.url"    // z. B. https://example.at – Basis für Links in Mails (von /install gesetzt)

	KeySMTPHost     = "mail.smtp.host"
	KeySMTPPort     = "mail.smtp.port" // Standard 587 (STARTTLS)
	KeySMTPUser     = "mail.smtp.user"
	KeySMTPPassword = "mail.smtp.password" // Geheimnis: nie loggen, im Admin maskieren
	KeyMailFrom     = "mail.from"          // Absenderadresse

	KeyStripeSecretKey     = "shop.stripe.secret_key"     // Geheimnis: nie loggen, im Admin maskieren
	KeyStripeWebhookSecret = "shop.stripe.webhook_secret" // Geheimnis
	KeyStripeAPIVersion    = "shop.stripe.api_version"    // optional
)

// Regionen, die das CMS kennt. Die erste ist der Standard.
var Regions = []string{"de-AT", "de-DE", "de-CH"}

// ValidRegion meldet, ob r eine bekannte Region ist.
func ValidRegion(r string) bool {
	for _, x := range Regions {
		if r == x {
			return true
		}
	}
	return false
}

// Settings ist der Zugriff auf core_settings. Fehlt ein Schlüssel, liefert Get
// einen leeren String und keinen Fehler.
//
// Es gibt bewusst keinen Cache: Im CGI-Modus lebt ein Prozess nur eine
// Anfrage, im Server-Modus würde ein Cache Änderungen anderer Prozesse verpassen.
type Settings interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
	GetAll(ctx context.Context) (map[string]string, error)
}

// SettingsStore implementiert Settings auf der SQLite-Datenbank.
type SettingsStore struct{ DB *sql.DB }

var _ Settings = SettingsStore{}

// dbtx ist das Gemeinsame von *sql.DB und *sql.Tx.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s SettingsStore) Get(ctx context.Context, key string) (string, error) {
	return getSetting(ctx, s.DB, key)
}

func (s SettingsStore) Set(ctx context.Context, key, value string) error {
	return setSetting(ctx, s.DB, key, value)
}

func (s SettingsStore) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT key, value FROM core_settings`)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("settings: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

func getSetting(ctx context.Context, q dbtx, key string) (string, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM core_settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("settings %s: %w", key, err) // nur der Schlüssel, nie der Wert
	}
	return v, nil
}

func setSetting(ctx context.Context, q dbtx, key, value string) error {
	_, err := q.ExecContext(ctx,
		`INSERT INTO core_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("settings %s: %w", key, err)
	}
	return nil
}

// Installed meldet, ob /install abgeschlossen wurde.
func Installed(ctx context.Context, s Settings) (bool, error) {
	v, err := s.Get(ctx, KeyInstalledAt)
	return v != "", err
}
