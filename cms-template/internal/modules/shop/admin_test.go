package shop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Die Admin-Routen des Shops rendern eine Platzhalterseite im Kern-Layout.
func TestAdminPlaceholderInLayout(t *testing.T) {
	m := &module{}
	admin, err := core.NewAdmin(nil, nil, []core.Module{m})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	m.Mount(mux, core.Deps{Admin: admin})
	for _, tc := range []struct{ method, path, want string }{
		{"GET", "/admin/shop/orders", "<h1>Shop · Bestellungen</h1>"},
		{"GET", "/admin/shop/products", `<a href="/admin/shop/products" aria-current="page">`},
		{"POST", "/admin/shop/products", "Produkt anlegen"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		b := w.Body.String()
		if w.Code != 200 || !strings.Contains(b, tc.want) || !strings.Contains(b, `data-styrixa-template="shop-admin-placeholder"`) {
			t.Errorf("%s %s: %d\n%s", tc.method, tc.path, w.Code, b)
		}
	}
}
