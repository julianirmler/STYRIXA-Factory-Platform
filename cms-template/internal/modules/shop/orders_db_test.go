package shop

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Integrationstest gegen echte SQLite mit den Shop-Migrationen.
func newShopDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := core.OpenDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := core.Migrate(context.Background(), db, []core.Module{&module{}}); err != nil {
		t.Fatal(err)
	}
	return db
}

func insertOrder(t *testing.T, db *sql.DB, number, status string, total Cents, ref string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO shop_orders
		(number, status, customer_email, billing_json, shipping_json, tax_country,
		 subtotal_cents, tax_cents, total_cents, payment_provider, payment_ref)
		VALUES (?, ?, 'kunde@example.at', '{}', '{}', 'AT', ?, 0, ?, 'stripe', ?)`,
		number, status, total, total, ref)
	if err != nil {
		t.Fatal(err)
	}
}

type orderRow struct {
	status, paymentStatus, intent string
	events                        int
}

func readOrder(t *testing.T, db *sql.DB, number string) orderRow {
	t.Helper()
	var r orderRow
	var intent sql.NullString
	err := db.QueryRow(`SELECT o.status, o.payment_status, o.payment_intent,
		(SELECT count(*) FROM shop_order_events e WHERE e.order_id = o.id)
		FROM shop_orders o WHERE number = ?`, number).Scan(&r.status, &r.paymentStatus, &intent, &r.events)
	if err != nil {
		t.Fatal(err)
	}
	r.intent = intent.String
	return r
}

func TestSQLPaymentStore(t *testing.T) {
	paid := func(eventID, number string, amount Cents) PaymentUpdate {
		return PaymentUpdate{Provider: "stripe", EventID: eventID, EventType: "checkout.session.completed",
			OrderNumber: number, PaymentRef: "cs_1", PaymentIntent: "pi_1", Status: PayPaid, Amount: amount, Currency: "eur"}
	}

	tests := []struct {
		name        string
		orderStatus string
		updates     []PaymentUpdate
		wantOut     []Outcome
		wantErr     error
		want        orderRow
	}{
		{
			name:        "gültige Zahlung",
			orderStatus: "pending",
			updates:     []PaymentUpdate{paid("evt_1", "STX-1", 4990)},
			wantOut:     []Outcome{OutcomeApplied},
			want:        orderRow{status: "paid", paymentStatus: PayPaid, intent: "pi_1", events: 1},
		},
		{
			name:        "doppeltes Ereignis wird nur einmal verarbeitet",
			orderStatus: "pending",
			updates:     []PaymentUpdate{paid("evt_1", "STX-1", 4990), paid("evt_1", "STX-1", 4990)},
			wantOut:     []Outcome{OutcomeApplied, OutcomeDuplicate},
			want:        orderRow{status: "paid", paymentStatus: PayPaid, intent: "pi_1", events: 1},
		},
		{
			name:        "Betrag weicht ab → markiert, nicht bezahlt",
			orderStatus: "pending",
			updates:     []PaymentUpdate{paid("evt_1", "STX-1", 100)},
			wantOut:     []Outcome{OutcomeFlagged},
			want:        orderRow{status: "pending", paymentStatus: PayOpen, events: 1},
		},
		{
			name:        "falsche Zahlungsreferenz → markiert",
			orderStatus: "pending",
			updates: []PaymentUpdate{{Provider: "stripe", EventID: "evt_1", EventType: "checkout.session.completed",
				OrderNumber: "STX-1", PaymentRef: "cs_fremd", Status: PayPaid, Amount: 4990, Currency: "eur"}},
			wantOut: []Outcome{OutcomeFlagged},
			want:    orderRow{status: "pending", paymentStatus: PayOpen, events: 1},
		},
		{
			name:        "Erstattung über PaymentIntent ohne Bestellnummer",
			orderStatus: "pending",
			updates: []PaymentUpdate{
				paid("evt_1", "STX-1", 4990),
				{Provider: "stripe", EventID: "evt_2", EventType: "charge.refunded", PaymentIntent: "pi_1",
					Status: PayRefunded, Amount: 4990, Currency: "eur"},
			},
			wantOut: []Outcome{OutcomeApplied, OutcomeApplied},
			want:    orderRow{status: "refunded", paymentStatus: PayRefunded, intent: "pi_1", events: 2},
		},
		{
			// Derzeitiges Verhalten: Markierte Ereignisse ändern die Bestellung nicht,
			// nur der Ereignis-Eintrag („bitte erstatten“) entsteht. Ob payment_status
			// hier „paid“ werden soll, ist in Session 5.4 (Erstattungen) zu klären.
			name:        "Zahlung auf stornierte Bestellung → markiert",
			orderStatus: "cancelled",
			updates:     []PaymentUpdate{paid("evt_1", "STX-1", 4990)},
			wantOut:     []Outcome{OutcomeFlagged},
			want:        orderRow{status: "cancelled", paymentStatus: PayOpen, events: 1},
		},
		{
			name:        "unbekannte Bestellung",
			orderStatus: "pending",
			updates:     []PaymentUpdate{paid("evt_1", "STX-999", 4990)},
			wantErr:     ErrOrderNotFound,
			want:        orderRow{status: "pending", paymentStatus: PayOpen},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := newShopDB(t)
			insertOrder(t, db, "STX-1", tt.orderStatus, 4990, "cs_1")
			store := SQLPaymentStore{DB: db}
			ctx := context.Background()
			for i, u := range tt.updates {
				out, err := store.ApplyPayment(ctx, u)
				if tt.wantErr != nil {
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("Ereignis %d: err = %v, want %v", i+1, err, tt.wantErr)
					}
					continue
				}
				if err != nil {
					t.Fatalf("Ereignis %d: %v", i+1, err)
				}
				if out != tt.wantOut[i] {
					t.Fatalf("Ereignis %d: Outcome %q, want %q", i+1, out, tt.wantOut[i])
				}
			}
			if got := readOrder(t, db, "STX-1"); got != tt.want {
				t.Fatalf("Bestellung = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Ein fehlgeschlagenes Ereignis (unbekannte Bestellung) darf nicht als
// verarbeitet gelten – sonst ginge eine spätere Wiederholung verloren.
func TestSQLPaymentStoreRollbackKeepsEventRetryable(t *testing.T) {
	db := newShopDB(t)
	store := SQLPaymentStore{DB: db}
	ctx := context.Background()
	u := PaymentUpdate{Provider: "stripe", EventID: "evt_1", EventType: "checkout.session.completed",
		OrderNumber: "STX-1", PaymentRef: "cs_1", Status: PayPaid, Amount: 4990, Currency: "eur"}
	if _, err := store.ApplyPayment(ctx, u); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM shop_payment_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("Ereignis nach Rollback als verarbeitet gespeichert")
	}
	insertOrder(t, db, "STX-1", "pending", 4990, "cs_1")
	if out, err := store.ApplyPayment(ctx, u); err != nil || out != OutcomeApplied {
		t.Fatalf("Wiederholung: out = %q, err = %v", out, err)
	}
}
