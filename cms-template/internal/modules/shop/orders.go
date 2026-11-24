package shop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// OrderPayment ist die Sicht auf eine Bestellung, die zur Zahlungsverarbeitung nötig ist.
type OrderPayment struct {
	Number        string
	Status        string // shop_orders.status
	PaymentStatus string
	PaymentRef    string
	PaymentIntent string
	Total         Cents
	Currency      string
}

// Outcome beschreibt, was ein Ereignis bewirkt hat (für Log und Admin).
type Outcome string

const (
	OutcomeApplied   Outcome = "applied"
	OutcomeDuplicate Outcome = "duplicate" // Ereignis schon verarbeitet (Stripe stellt mehrfach zu)
	OutcomeNoop      Outcome = "noop"      // kein Statuswechsel nötig
	OutcomeFlagged   Outcome = "flagged"   // zur manuellen Prüfung markiert
	OutcomeIgnored   Outcome = "ignored"
)

var ErrOrderNotFound = errors.New("shop: Bestellung nicht gefunden")

// Decision ist das Ergebnis von decide – rein, ohne Seiteneffekte, voll testbar.
type Decision struct {
	PaymentStatus string
	OrderStatus   string
	Outcome       Outcome
	Note          string
}

// decide legt fest, wie ein Zahlungsereignis eine Bestellung verändert.
// Grundsätze:
//   - Ein Betrag/eine Währung, die nicht zur Bestellung passt, setzt nie „bezahlt“,
//     sondern wird zur Prüfung markiert.
//   - Eine bezahlte Bestellung wird durch Fehlschlag/Ablauf nicht zurückgesetzt.
//   - Zahlung auf stornierte Bestellung → markieren (Erstattung nötig).
func decide(o OrderPayment, u PaymentUpdate) Decision {
	d := Decision{PaymentStatus: o.PaymentStatus, OrderStatus: o.Status, Outcome: OutcomeNoop}
	switch u.Status {
	case PayPaid, PayProcessing:
		if u.Amount != o.Total || !strings.EqualFold(u.Currency, o.Currency) {
			d.Outcome = OutcomeFlagged
			d.Note = fmt.Sprintf("Betrag/Währung weicht ab: Stripe %d %s, Bestellung %d %s",
				u.Amount, u.Currency, o.Total, o.Currency)
			return d
		}
		if u.Status == PayProcessing {
			if o.PaymentStatus == PayOpen || o.PaymentStatus == "" {
				d.PaymentStatus, d.Outcome = PayProcessing, OutcomeApplied
			}
			return d
		}
		if o.PaymentStatus == PayPaid {
			return d
		}
		d.PaymentStatus, d.Outcome = PayPaid, OutcomeApplied
		switch o.Status {
		case "pending":
			d.OrderStatus = "paid"
		case "cancelled":
			d.Outcome, d.Note = OutcomeFlagged, "Zahlung auf stornierte Bestellung eingegangen – bitte erstatten"
		}
	case PayFailed, PayExpired:
		if o.PaymentStatus == PayPaid || o.PaymentStatus == PayRefunded || o.PaymentStatus == PayPartiallyRefunded {
			return d
		}
		d.PaymentStatus, d.Outcome = u.Status, OutcomeApplied
	case PayRefunded:
		if o.PaymentStatus == PayRefunded {
			return d
		}
		d.PaymentStatus, d.OrderStatus, d.Outcome = PayRefunded, "refunded", OutcomeApplied
	case PayPartiallyRefunded:
		if o.PaymentStatus == PayRefunded {
			return d
		}
		d.PaymentStatus, d.Outcome = PayPartiallyRefunded, OutcomeApplied
		d.Note = fmt.Sprintf("Teilerstattung, insgesamt erstattet: %d", u.Amount)
	default:
		d.Outcome = OutcomeIgnored
	}
	return d
}

// PaymentStore wendet Zahlungsereignisse atomar an.
type PaymentStore interface {
	ApplyPayment(ctx context.Context, u PaymentUpdate) (Outcome, error)
}

// SQLPaymentStore – Umsetzung auf shop_orders / shop_payment_events (SQLite).
type SQLPaymentStore struct{ DB *sql.DB }

func (s SQLPaymentStore) ApplyPayment(ctx context.Context, u PaymentUpdate) (out Outcome, err error) {
	if s.DB == nil {
		return "", errors.New("shop: keine Datenbank")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1. Idempotenz: Ereignis-ID nur einmal
	res, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO shop_payment_events (provider, event_id, event_type) VALUES (?, ?, ?)`,
		u.Provider, u.EventID, u.EventType)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return OutcomeDuplicate, tx.Commit()
	}
	if u.Status == PayIgnored {
		return OutcomeIgnored, tx.Commit()
	}

	// 2. Bestellung finden (Nummer, sonst PaymentIntent)
	var o OrderPayment
	var ref, intent sql.NullString
	q := `SELECT number, status, payment_status, payment_ref, payment_intent, total_cents, currency FROM shop_orders `
	row := tx.QueryRowContext(ctx, q+`WHERE number = ?`, u.OrderNumber)
	if u.OrderNumber == "" {
		row = tx.QueryRowContext(ctx, q+`WHERE payment_intent = ? AND payment_provider = ?`, u.PaymentIntent, u.Provider)
	}
	if err = row.Scan(&o.Number, &o.Status, &o.PaymentStatus, &ref, &intent, &o.Total, &o.Currency); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrOrderNotFound
		}
		return "", err
	}
	o.PaymentRef, o.PaymentIntent = ref.String, intent.String

	// Gehört die Zahlung wirklich zu dieser Bestellung?
	if u.PaymentRef != "" && o.PaymentRef != "" && u.PaymentRef != o.PaymentRef {
		d := Decision{Outcome: OutcomeFlagged, Note: "Zahlungsreferenz passt nicht zur Bestellung"}
		if err = s.logEvent(ctx, tx, o.Number, u, d); err != nil {
			return "", err
		}
		return d.Outcome, tx.Commit()
	}

	// 3. Entscheiden und schreiben
	d := decide(o, u)
	if d.Outcome == OutcomeApplied {
		if u.PaymentIntent != "" && o.PaymentIntent == "" {
			o.PaymentIntent = u.PaymentIntent
		}
		if _, err = tx.ExecContext(ctx, `UPDATE shop_orders
			SET payment_status = ?, status = ?, payment_intent = ?, updated_at = datetime('now')
			WHERE number = ?`, d.PaymentStatus, d.OrderStatus, nullIfEmpty(o.PaymentIntent), o.Number); err != nil {
			return "", err
		}
	}
	if err = s.logEvent(ctx, tx, o.Number, u, d); err != nil {
		return "", err
	}
	return d.Outcome, tx.Commit()
}

func (SQLPaymentStore) logEvent(ctx context.Context, tx *sql.Tx, number string, u PaymentUpdate, d Decision) error {
	data := fmt.Sprintf("%s %s → %s (%s) %s", u.Provider, u.EventType, u.Status, d.Outcome, d.Note)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO shop_order_events (order_id, kind, data)
		 SELECT id, 'payment', ? FROM shop_orders WHERE number = ?`, strings.TrimSpace(data), number)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
