// factory-api – Build-Worker und Deploy-Dienst der STYRIXA Factory.
// Build-Endpunkt folgt; derzeit: Deploy fertiger Builds aus ./builds/<id>/.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/api"
	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/deploy"
)

// dirStore: ./builds/<id>/meta.json + ./builds/<id>/styrixa
type dirStore struct{ root string }

var reID = regexp.MustCompile(`^bld_[a-f0-9]{8,32}$`)

func (s dirStore) Artifact(id string) (deploy.Artifact, error) {
	if !reID.MatchString(id) {
		return deploy.Artifact{}, errors.New("ungültige Build-ID")
	}
	dir := filepath.Join(s.root, id)
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return deploy.Artifact{}, err
	}
	var a deploy.Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		return a, err
	}
	a.BinaryPath = filepath.Join(dir, "styrixa")
	return a, nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "Listen-Adresse")
	builds := flag.String("builds", "./builds", "Ordner mit fertigen Builds")
	flag.Parse()

	mux := http.NewServeMux()
	// TODO: Kunden-Authentifizierung als Middleware vor /api/*
	mux.Handle("POST /api/builds/{id}/deploy", &api.DeployHandler{Store: dirStore{root: *builds}})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Printf("factory-api lauscht auf %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
