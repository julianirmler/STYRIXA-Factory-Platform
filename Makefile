# STYRIXA Factory Platform – lokale Befehle (identisch zur CI)
.PHONY: test cgo matrix ui check release-local

test:
	cd cms-template && go vet ./... && go test ./...
	cd factory-api && go vet ./... && go test ./...

cgo:
	scripts/check-cgo.sh cms-template "$$(cd cms-template && go run ./tools/matrix | awk '{print length($$2), $$2}' | sort -n | tail -1 | cut -d' ' -f2 | tr , ' ')"
	scripts/check-cgo.sh factory-api

matrix:
	scripts/matrix-build.sh

ui:
	cd factory-ui && npm ci && npm run build && npm test

check: test cgo matrix ui
	@test -z "$$(gofmt -l cms-template factory-api)" || (gofmt -l cms-template factory-api; echo "gofmt!"; exit 1)
	@echo "Alles grün."

# Release-Pakete für die aktuelle Plattform lokal bauen
release-local:
	scripts/matrix-build.sh "$$(go env GOOS)/$$(go env GOARCH)" dist
