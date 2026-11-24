package shop

import (
	"context"
	"net/http"
)

// PaymentProvider – reine HTTP-Integrationen, keine CGO-SDKs.
// Geplant: stripe (Karte, EPS, Klarna, Apple/Google Pay), paypal, prepayment, invoice.
type PaymentProvider interface {
	ID() string
	Label() string
	// Start erzeugt die Zahlung; redirectURL leer = keine Weiterleitung (z. B. Vorkasse).
	Start(ctx context.Context, o OrderRef) (redirectURL, paymentRef string, err error)
	// Webhook verifiziert die Signatur und liefert den neuen Zahlungsstatus.
	Webhook(r *http.Request) (paymentRef, status string, err error)
}

// OrderRef ist die minimale Sicht, die ein Provider braucht.
type OrderRef struct {
	Number   string
	Total    Cents
	Currency string
	Email    string
	ReturnTo string
}

var providers = map[string]PaymentProvider{}

func registerProvider(p PaymentProvider) { providers[p.ID()] = p }

// prepayment: Vorkasse per Banküberweisung. IBAN/Verwendungszweck kommen aus den Shop-Einstellungen.
type prepayment struct{}

func (prepayment) ID() string    { return "prepayment" }
func (prepayment) Label() string { return "Vorkasse (Überweisung)" }
func (prepayment) Start(_ context.Context, o OrderRef) (string, string, error) {
	return "", o.Number, nil
}
func (prepayment) Webhook(*http.Request) (string, string, error) {
	return "", "", http.ErrNotSupported // Zahlungseingang wird im Admin manuell bestätigt
}

func init() { registerProvider(prepayment{}) }
