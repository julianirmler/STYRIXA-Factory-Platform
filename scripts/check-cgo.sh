#!/usr/bin/env sh
# Scheitert, wenn eine Nicht-Standard-Abhängigkeit CGO braucht (C-Dateien oder import "C").
# Aufruf: scripts/check-cgo.sh <modulordner> [tags]
set -eu
dir=$1; tags=${2:-}
cd "$dir"
# go list getrennt ausführen, damit ein Fehler (z. B. fehlende go.sum) den Check scheitern lässt
list=$(CGO_ENABLED=1 go list -deps -tags "$tags" \
  -f '{{if not .Standard}}{{if or .CgoFiles .CFiles .CXXFiles .SwigFiles}}{{.ImportPath}}{{end}}{{end}}' ./...)
bad=$(printf '%s\n' "$list" | sed '/^$/d')
if [ -n "$bad" ]; then
  echo "FEHLER: Diese Pakete brauchen CGO (verboten, siehe CLAUDE.md Regel 1):"; echo "$bad"; exit 1
fi
echo "cgo-check $dir [$tags]: ok"
