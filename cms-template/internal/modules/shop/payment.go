package shop

import (
	"context"
	"errors"
	"net/http"
)

// PaymentProvider – reine HTTP-Integrationen, keine SDKs, keine CGO.
// Vorhanden: stripe (Karte, EPS, Klarna, Apple/Google Pay via Checkout), prepayment.
// Geplant: paypal.
type PaymentProvider interface {
	ID() string
	Label() string
	// Start erzeugt die Zahlung. redirectURL leer = keine Weiterleitung (z. B. Vorkasse).
	// paymentRef wird in shop_orders.payment_ref gespeichert.
	Start(ctx context.Context, o OrderRef) (redirectURL, paymentRef string, err error)
	// Webhook verifiziert die Signatur und übersetzt das Ereignis.
	// ErrNoWebhook: Anbieter hat keine Webhooks.
	Webhook(r *http.Request) (*PaymentUpdate, error)
}

// OrderRef ist die minimale Sicht, die ein Anbieter zum Start braucht.
type OrderRef struct {
	Number     string
	Total      Cents
	Currency   string // ISO 4217, z. B. "EUR"
	Email      string
	Locale     string // z. B. "de"
	SuccessURL string // Rücksprung nach Zahlung (zeigt nur „wird geprüft“ – Status kommt per Webhook)
	CancelURL  string
}

// Zahlungsstatus (shop_orders.payment_status).
const (
	PayOpen              = "open"
	PayProcessing        = "processing" // asynchrone Methoden, z. B. SEPA-Lastschrift
	PayPaid              = "paid"
	PayFailed            = "failed"
	PayExpired           = "expired"
	PayRefunded          = "refunded"
	PayPartiallyRefunded = "partially_refunded"
	PayIgnored           = "ignored" // Ereignis ohne Bedeutung für den Shop
)

// PaymentUpdate ist ein geprüftes Ereignis eines Zahlungsanbieters.
type PaymentUpdate struct {
	Provider      string
	EventID       string // für Idempotenz (jedes Ereignis nur einmal verarbeiten)
	EventType     string
	OrderNumber   string // kann leer sein (z. B. Erstattung) → Zuordnung über PaymentIntent
	PaymentRef    string // z. B. Stripe-Checkout-Session-ID
	PaymentIntent string
	Status        string // Pay*
	Amount        Cents  // bezahlter bzw. erstatteter Betrag laut Anbieter
	Currency      string
}

var ErrNoWebhook = errors.New("anbieter hat keine webhooks")

var providers = map[string]PaymentProvider{}

func registerProvider(p PaymentProvider) { providers[p.ID()] = p }

// prepayment: Vorkasse per Banküberweisung. IBAN/Verwendungszweck kommen aus den Shop-Einstellungen.
type prepayment struct{}

func (prepayment) ID() string    { return "prepayment" }
func (prepayment) Label() string { return "Vorkasse (Überweisung)" }
func (prepayment) Start(_ context.Context, o OrderRef) (string, string, error) {
	return "", o.Number, nil
}
func (prepayment) Webhook(*http.Request) (*PaymentUpdate, error) {
	return nil, ErrNoWebhook // Zahlungseingang wird im Admin manuell bestätigt
}

func init() { registerProvider(prepayment{}) }
