#!/usr/bin/env sh
# Baut alle gültigen Modul-Kombinationen (CGO_ENABLED=0) und prüft, dass ungültige scheitern.
# Aufruf: scripts/matrix-build.sh [GOOS/GOARCH] [ausgabeordner]
# Ohne Ausgabeordner wird nur kompiliert (CI-Prüfung), mit Ausgabeordner entstehen Release-Pakete.
set -eu
target=${1:-$(go env GOOS)/$(go env GOARCH)}
out=${2:-}
goos=${target%/*}; goarch=${target#*/}
cd "$(dirname "$0")/../cms-template"
ext=""; [ "$goos" = windows ] && ext=".exe"
n=0
go run ./tools/matrix | while read -r key tags; do
  bin=$(mktemp -d)/styrixa$ext
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags "-s -w" -tags "$tags" -o "$bin" ./cmd/styrixa
  if [ -n "$out" ]; then
    mkdir -p "$out"
    tar -czf "$out/styrixa_${key}_${goos}_${goarch}.tar.gz" -C "$(dirname "$bin")" "styrixa$ext"
  fi
  rm -rf "$(dirname "$bin")"
  echo "ok   $goos/$goarch $key"
done
# Negativtest: fehlende Abhängigkeit muss den Build abbrechen (nur Module mit Guard-Datei)
go run ./tools/matrix -invalid | while read -r key tags violators; do
  needs_guard=0
  for id in $(echo "$violators" | tr , ' '); do
    [ -f "cmd/styrixa/guard_$id.go" ] && needs_guard=1
  done
  [ $needs_guard = 1 ] || continue   # Modul existiert noch nicht → nichts zu prüfen
  if CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -tags "$tags" -o /dev/null ./cmd/styrixa 2>/dev/null; then
    echo "FEHLER: ungültige Kombination $key baut trotzdem"; exit 1
  fi
  echo "fail $goos/$goarch $key (erwartet)"
done
