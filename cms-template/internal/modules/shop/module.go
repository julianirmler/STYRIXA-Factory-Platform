// Package shop ist Modul 08 (mod_shop): Produkte, Varianten, Lager, Bestellungen,
// DACH-Steuern, Versand, Gutscheine, Rechnungen, Product-JSON-LD.
// Es wird nur über cmd/styrixa/mod_shop.go (//go:build mod_shop) importiert.
package shop

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

type module struct {
	d     core.Deps
	store PaymentStore
}

func (*module) ID() string         { return "shop" }
func (*module) Requires() []string { return []string{"media"} }
func (*module) Migrations() fs.FS {
	sub, _ := fs.Sub(migrations, "migrations")
	return sub
}

func (*module) AdminNav() []core.NavItem {
	return []core.NavItem{
		{Label: "Shop · Bestellungen", Path: "/admin/shop/orders", Icon: "shopping-cart", Order: 40},
		{Label: "Shop · Produkte", Path: "/admin/shop/products", Icon: "package", Order: 41},
		{Label: "Shop · Gutscheine", Path: "/admin/shop/coupons", Icon: "ticket", Order: 42},
		{Label: "Shop · Einstellungen", Path: "/admin/shop/settings", Icon: "settings", Order: 43},
	}
}

func (m *module) Mount(mux *http.ServeMux, d core.Deps) {
	m.d = d
	if d.DB != nil {
		m.store = SQLPaymentStore{DB: d.DB}
	}
	configureProviders()
	// Admin (Auth-Middleware des Kerns umschließt /admin/*)
	mux.HandleFunc("GET /admin/shop/orders", todo("Bestellübersicht"))
	mux.HandleFunc("GET /admin/shop/orders/{number}", todo("Bestelldetail"))
	mux.HandleFunc("POST /admin/shop/orders/{number}/status", todo("Status ändern"))
	mux.HandleFunc("GET /admin/shop/products", todo("Produktliste"))
	mux.HandleFunc("POST /admin/shop/products", todo("Produkt anlegen"))
	mux.HandleFunc("GET /admin/shop/coupons", todo("Gutscheine"))
	mux.HandleFunc("GET /admin/shop/settings", todo("Shop-Einstellungen"))

	// Öffentlich
	mux.HandleFunc("GET /shop", todo("Katalog"))
	mux.HandleFunc("GET /shop/produkt/{slug}", todo("Produktseite + JSON-LD"))
	mux.HandleFunc("POST /shop/warenkorb", todo("Warenkorb"))
	mux.HandleFunc("POST /shop/kasse", todo("Checkout"))
	mux.HandleFunc("POST /shop/webhook/{provider}", m.webhook)
}

func (m *module) webhook(w http.ResponseWriter, r *http.Request) {
	p, ok := providers[r.PathValue("provider")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	u, err := p.Webhook(r)
	switch {
	case errors.Is(err, ErrNoWebhook):
		http.NotFound(w, r)
		return
	case err != nil:
		// Ungültige Signatur o. Ä.: keine Details nach außen
		log.Printf("shop: webhook %s abgelehnt: %v", p.ID(), err)
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	if m.store == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable) // Anbieter wiederholt später
		return
	}
	out, err := m.store.ApplyPayment(r.Context(), *u)
	switch {
	case errors.Is(err, ErrOrderNotFound):
		// z. B. Ereignis eines anderen Systems auf demselben Stripe-Konto – quittieren, nicht wiederholen lassen
		log.Printf("shop: webhook %s %s: Bestellung %q unbekannt", p.ID(), u.EventType, u.OrderNumber)
	case err != nil:
		log.Printf("shop: webhook %s %s: %v", p.ID(), u.EventType, err)
		http.Error(w, "retry", http.StatusInternalServerError) // Anbieter wiederholt
		return
	case out == OutcomeFlagged:
		log.Printf("shop: webhook %s %s für %s zur Prüfung markiert", p.ID(), u.EventType, u.OrderNumber)
	}
	w.WriteHeader(http.StatusOK)
}

// configureProviders aktiviert Zahlungsanbieter, deren Zugangsdaten vorhanden sind.
// Bis der Settings-Store steht (Session 1.2), kommen sie aus der Umgebung.
func configureProviders() {
	sk, wh := os.Getenv("STYRIXA_STRIPE_SECRET_KEY"), os.Getenv("STYRIXA_STRIPE_WEBHOOK_SECRET")
	if sk == "" && wh == "" {
		return
	}
	st, err := NewStripe(StripeConfig{SecretKey: sk, WebhookSecret: wh, APIVersion: os.Getenv("STYRIXA_STRIPE_API_VERSION")})
	if err != nil {
		log.Printf("shop: Stripe nicht aktiviert: %v", err)
		return
	}
	registerProvider(st)
	if st.TestMode() {
		log.Printf("shop: Stripe aktiv (TESTMODUS)")
	}
}

func todo(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{"todo": what})
	}
}

func init() { core.Register(&module{}) }
