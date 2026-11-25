// factory-api – Release-Katalog und Deploy-Dienst der STYRIXA Factory.
// Liefert vorgebaute, signierte CMS-Pakete aus dem neuesten GitHub-Release
// (ADR-004) als Download oder per SSH/FTPS-Deploy aus.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/api"
	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/catalog"
	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/ui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8090", "Listen-Adresse")
	repo := flag.String("repo", "julianirmler/STYRIXA-Factory-Platform", "GitHub-Repository mit den Releases")
	cache := flag.String("cache", "./cache", "Ordner für geprüfte Pakete")
	uiDir := flag.String("ui", "", "Ordner mit dem gebauten factory-ui (leer = kein UI)")
	flag.Parse()

	if err := os.MkdirAll(*cache, 0o700); err != nil {
		slog.Error("cache dir", "err", err)
		os.Exit(1)
	}
	cat := &catalog.Catalog{Repo: *repo, CacheDir: *cache, Token: os.Getenv("GITHUB_TOKEN")}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := cat.Load(ctx); err != nil {
		slog.Warn("no valid release yet, retrying on demand", "err", err)
	}
	cancel()

	mux := http.NewServeMux()
	// TODO: Kunden-Authentifizierung als Middleware vor /api/*
	cat.Routes(mux)
	mux.Handle("POST /api/packages/{id}/deploy", &api.DeployHandler{Store: cat})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	if *uiDir != "" {
		if _, err := os.Stat(*uiDir + "/index.html"); err != nil {
			slog.Error("ui dir", "err", err)
			os.Exit(1)
		}
		mux.Handle("/", ui.Handler(*uiDir))
	}

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	slog.Info("factory-api listening", "addr", *addr, "repo", *repo, "ui", *uiDir)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server", "err", err)
		os.Exit(1)
	}
}
