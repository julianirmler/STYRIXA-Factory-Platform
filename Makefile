# STYRIXA Factory Platform – lokale Befehle (identisch zur CI)
.PHONY: test smoke cgo matrix ui check release-local

# Pflichtmodule (modules.json „required“): ohne sie fehlen Seiten in den CMS-Tests.
CMS_TAGS := mod_content mod_dashboard

test:
	cd cms-template && go vet -tags "$(CMS_TAGS)" ./... && go test -tags "$(CMS_TAGS)" ./...
	cd factory-api && go vet ./... && go test ./...

# Smoke-Test (ADR-010): echtes Binary bauen, starten, /install → Login → Seite
# anlegen → veröffentlichen → öffentlich abrufen. Eigener Tag, darum nicht in „test“.
smoke:
	cd cms-template && go vet -tags "smoke $(CMS_TAGS)" ./cmd/styrixa && go test -count=1 -v -tags "smoke $(CMS_TAGS)" -run '^TestSmoke$$' ./cmd/styrixa

cgo:
	scripts/check-cgo.sh cms-template "$$(cd cms-template && go run ./tools/matrix | awk '{print length($$2), $$2}' | sort -n | tail -1 | cut -d' ' -f2 | tr , ' ')"
	scripts/check-cgo.sh factory-api

matrix:
	scripts/matrix-build.sh

ui:
	cd factory-ui && npm ci && npm run build && npm test
	# Eigene Editor-Skripte des CMS (ohne Build-Schritt): nur Syntax prüfen.
	for f in cms-template/internal/modules/content/static/editor*.js; do node --check "$$f" || exit 1; done

check: test smoke cgo matrix ui
	@test -z "$$(gofmt -l cms-template factory-api)" || (gofmt -l cms-template factory-api; echo "gofmt!"; exit 1)
	@echo "Alles grün."

# Release-Pakete für die aktuelle Plattform lokal bauen
release-local:
	scripts/matrix-build.sh "$$(go env GOOS)/$$(go env GOARCH)" dist
