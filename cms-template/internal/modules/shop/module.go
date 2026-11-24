// Package shop ist Modul 08 (mod_shop): Produkte, Varianten, Lager, Bestellungen,
// DACH-Steuern, Versand, Gutscheine, Rechnungen, Product-JSON-LD.
// Es wird nur über cmd/styrixa/mod_shop.go (//go:build mod_shop) importiert.
package shop

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

//go:embed migrations/*.sql
var migrations embed.FS

type module struct{ d core.Deps }

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
	ref, status, err := p.Webhook(r)
	if err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}
	_, _ = ref, status // TODO: shop_orders.payment_status aktualisieren + Event loggen
	w.WriteHeader(http.StatusNoContent)
}

func todo(what string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{"todo": what})
	}
}

func init() { core.Register(&module{}) }
