package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/catalog"
	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/deploy"
)

type memStore map[string]deploy.Artifact

func (m memStore) Artifact(_ context.Context, id string) (deploy.Artifact, error) {
	a, ok := m[id]
	if !ok {
		return a, catalog.ErrNotFound
	}
	return a, nil
}

func TestDeployEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /api/packages/{id}/deploy", &DeployHandler{Store: memStore{
		"core_linux_amd64": {Name: "k", GOOS: "linux", GOARCH: "amd64", BinaryPath: "/dev/null"},
	}})

	post := func(id, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/packages/"+id+"/deploy", strings.NewReader(body)))
		return rec
	}

	if rec := post("shop_linux_amd64", `{"target":{"method":"ssh","host":"example.at","user":"u"}}`); rec.Code != 404 {
		t.Errorf("unbekanntes Paket: %d", rec.Code)
	}
	if rec := post("core_linux_amd64", `{"target":{"method":"ssh","host":"x","user":"a;b"}}`); rec.Code != 422 {
		t.Errorf("Validierung: %d", rec.Code)
	}
	// allowPrivateNetworks kann nicht per JSON gesetzt werden
	if rec := post("core_linux_amd64", `{"target":{"method":"ftp","host":"x","user":"u","publicUrl":"https://x.at","AllowPrivateNetworks":true}}`); rec.Code != 400 {
		t.Errorf("unbekanntes Feld: %d", rec.Code)
	}
	// SSRF-Sperre greift auch über die API
	rec := post("core_linux_amd64", `{"target":{"method":"ftp","host":"127.0.0.1","user":"u","publicUrl":"https://x.at"},"password":"p"}`)
	body := rec.Body.String()
	if rec.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, "event: error") || !strings.Contains(body, "keine öffentliche Adresse") {
		t.Fatalf("SSE-Fehler erwartet:\n%s", body)
	}
	if strings.Contains(body, `"p"`) {
		t.Fatal("Passwort taucht im Stream auf")
	}
}
