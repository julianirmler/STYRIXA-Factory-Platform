package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/deploy"
)

type memStore map[string]deploy.Artifact

func (m memStore) Artifact(id string) (deploy.Artifact, error) {
	a, ok := m[id]
	if !ok {
		return a, errors.New("nicht gefunden")
	}
	return a, nil
}

func TestDeployEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /api/builds/{id}/deploy", &DeployHandler{Store: memStore{
		"bld_1": {Name: "k", GOOS: "linux", GOARCH: "amd64", BinaryPath: "/dev/null"},
	}})

	post := func(id, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/api/builds/"+id+"/deploy", strings.NewReader(body)))
		return rec
	}

	if rec := post("bld_x", `{}`); rec.Code != 404 {
		t.Errorf("unbekannter Build: %d", rec.Code)
	}
	if rec := post("bld_1", `{"target":{"method":"ssh","host":"x","user":"a;b"}}`); rec.Code != 422 {
		t.Errorf("Validierung: %d", rec.Code)
	}
	// allowPrivateNetworks kann nicht per JSON gesetzt werden
	if rec := post("bld_1", `{"target":{"method":"ftp","host":"x","user":"u","publicUrl":"https://x.at","AllowPrivateNetworks":true}}`); rec.Code != 400 {
		t.Errorf("unbekanntes Feld: %d", rec.Code)
	}
	// SSRF-Sperre greift auch über die API
	rec := post("bld_1", `{"target":{"method":"ftp","host":"127.0.0.1","user":"u","publicUrl":"https://x.at"},"password":"p"}`)
	body := rec.Body.String()
	if rec.Header().Get("Content-Type") != "text/event-stream" || !strings.Contains(body, "event: error") || !strings.Contains(body, "keine öffentliche Adresse") {
		t.Fatalf("SSE-Fehler erwartet:\n%s", body)
	}
	if strings.Contains(body, `"p"`) {
		t.Fatal("Passwort taucht im Stream auf")
	}
}
