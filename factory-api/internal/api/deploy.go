// Package api enthält die HTTP-Endpunkte der Factory.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/deploy"
)

// ArtifactStore liefert fertige Builds (wird vom Build-Worker befüllt).
type ArtifactStore interface {
	Artifact(buildID string) (deploy.Artifact, error)
}

// DeployHandler: POST /api/builds/{id}/deploy
//
// Request (JSON):
//
//	{"target": {...deploy.Target}, "password": "…", "privateKey": "-----BEGIN …"}
//
// Antwort: text/event-stream mit Events
//
//	event: log       data: deploy.Event
//	event: identity  data: {"kind","fingerprint","mismatch"}  → im UI bestätigen, erneut senden
//	event: result    data: deploy.Result
//	event: error     data: {"message"}
//
// Zugangsdaten leben nur für die Dauer des Requests im Speicher; sie werden
// weder geloggt noch gespeichert. Der Endpunkt muss hinter der Kunden-
// Authentifizierung liegen (Build gehört dem eingeloggten Kunden).
type DeployHandler struct {
	Store ArtifactStore

	mu     sync.Mutex
	active map[string]bool
}

type deployRequest struct {
	Target     deploy.Target `json:"target"`
	Password   string        `json:"password"`
	PrivateKey string        `json:"privateKey"`
}

func (h *DeployHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	art, err := h.Store.Artifact(id)
	if err != nil {
		http.Error(w, "Build nicht gefunden", http.StatusNotFound)
		return
	}
	var req deployRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "Ungültige Anfrage", http.StatusBadRequest)
		return
	}
	req.Target.AllowPrivateNetworks = false // nie vom Client steuerbar
	if err := req.Target.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if !h.lock(id) {
		http.Error(w, "Für diesen Build läuft bereits ein Deployment", http.StatusConflict)
		return
	}
	defer h.unlock(id)

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming nicht unterstützt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	var wmu sync.Mutex
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		wmu.Lock()
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		fl.Flush()
		wmu.Unlock()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	cred := deploy.Credentials{Password: req.Password, PrivateKeyPEM: []byte(req.PrivateKey)}
	logf := func(e deploy.Event) { send("log", e) }

	var res *deploy.Result
	switch req.Target.Method {
	case deploy.MethodSSH:
		res, err = deploy.DeploySSH(ctx, art, req.Target, cred, logf, nil)
	case deploy.MethodFTP:
		res, err = deploy.DeployFTP(ctx, art, req.Target, cred, logf, nil)
	}

	var idErr *deploy.UnknownIdentityError
	switch {
	case errors.As(err, &idErr):
		send("identity", map[string]any{"kind": idErr.Kind, "fingerprint": idErr.Fingerprint, "mismatch": idErr.Mismatch})
	case err != nil:
		send("error", map[string]string{"message": err.Error()})
	default:
		send("result", res)
	}
}

func (h *DeployHandler) lock(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		h.active = map[string]bool{}
	}
	if h.active[id] {
		return false
	}
	h.active[id] = true
	return true
}

func (h *DeployHandler) unlock(id string) {
	h.mu.Lock()
	delete(h.active, id)
	h.mu.Unlock()
}
