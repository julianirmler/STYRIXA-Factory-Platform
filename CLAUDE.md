# CLAUDE.md – STYRIXA Factory Platform

Projektregeln für Claude Code. Vor jeder Session lesen, zusammen mit `docs/adr/README.md` und `docs/UMSETZUNGSPLAN.md`.

**Leitlinie:** Immer die günstigste Lösung, die das Problem vollständig löst – Standardbibliothek vor Abhängigkeit, vorhandenes Werkzeug vor Eigenbau, kein Server, wo ein statisches Artefakt reicht.

## Aktuelle Phase

@docs/phasen/PHASE-4.md

## Was das ist

Die Factory liefert aus einer Modulauswahl ein maßgeschneidertes **STYRIXA CMS** als CGO-freies Go-Single-Binary mit eingebetteter SQLite aus – als Download oder per Deploy (SSH / FTPS). Alle gültigen Modul-Kombinationen werden bei jedem Release in GitHub Actions **vorgebaut und signiert** (ADR-004); die Factory baut nicht selbst.

| Ordner | Inhalt | Go-Modul |
|---|---|---|
| `cms-template/` | Quellcode des generierten CMS | `github.com/julianirmler/styrixa-cms` |
| `factory-api/` | Release-Katalog + Deploy-Dienst | `github.com/julianirmler/styrixa-factory-platform/factory-api` |
| `factory-ui/` | React + Vite + Tailwind Configurator | – |

## Harte Regeln (nie brechen)

1. **Immer `CGO_ENABLED=0`.** Keine Abhängigkeit, die CGO braucht. SQLite nur über `modernc.org/sqlite`. Vor jeder neuen Abhängigkeit prüfen: reines Go? Gepflegt? Lizenz MIT/BSD/Apache?
2. **Build-Tag-Isolierung.** Jedes Modul liegt in `cms-template/internal/modules/<id>/` und wird **ausschließlich** über `cms-template/cmd/styrixa/mod_<id>.go` mit `//go:build mod_<id>` importiert. Kein Modul importiert ein anderes Modul direkt; Kommunikation nur über `internal/core` (Interfaces, Registry, Events). Abhängigkeiten: `Requires()` **und** Guard-Datei `guard_<id>.go` (`//go:build mod_<id> && !mod_<dep>`).
3. **Admin = `html/template` + htmx, kein React/Node im CMS** (ADR-002). Templates und JS eines Moduls liegen per `embed` im Modulpaket – nicht gewählte Module haben keinen UI-Code im Binary. Editor = vendortes Editor.js (ADR-003). Keine serverseitige Bildverarbeitung (ADR-001).
4. **CGI-tauglich.** Das CMS muss im CGI-Modus (`GATEWAY_INTERFACE`) funktionieren – auch hinter der PHP-Brücke (ADR-012), die nur ein Transport ist, nie PHP-Logik: keine Goroutinen, die über eine Anfrage hinaus leben, keine In-Memory-Caches als Wahrheit, keine Scheduler. Zeitgesteuertes über Lazy-Jobs oder Cron-Endpunkt (`internal/core`).
5. **Geld = `Cents` (int64), Steuersätze = Basispunkte.** Nie float für Beträge.
6. **Sicherheit vor Bequemlichkeit.** Zugangsdaten nie loggen oder speichern. Server-Identität immer per Fingerprint prüfen. Deploy-Ziele nur öffentliche IPs (SSRF-Schutz in `factory-api/internal/deploy/netguard.go`). Eingaben, die in Shell-/FTP-Befehle gehen, strikt validieren und quoten.
7. **Keine ungeprüfte `go.sum` committen.** Checksummen müssen gegen sum.golang.org verifiziert sein.
8. **Neue Abhängigkeiten nur mit ADR.** Erlaubt: `modernc.org/sqlite`, `golang.org/x/crypto`; JS vendort ohne Build-Schritt: htmx, Editor.js. Neues Modul = Eintrag in `cms-template/modules.json` (+ Guard-Datei bei Abhängigkeiten).

## Befehle

```bash
# CMS
cd cms-template
go vet ./... && go test ./...
CGO_ENABLED=0 go build -tags "mod_media mod_shop" -o dist/styrixa ./cmd/styrixa

# Factory-API
cd factory-api
go mod tidy                # einmalig / nach neuen Abhängigkeiten
go test ./...              # FTP-Tests brauchen: pip install pyftpdlib pyopenssl

# UI (Entwicklung, mit laufender factory-api auf 127.0.0.1:8090)
cd factory-ui && npm ci && npm run dev

# UI gebaut über die factory-api ausliefern
cd factory-ui && npm run build && cd ../factory-api && go run ./cmd/factory-api --ui ../factory-ui/dist
```

Vor jedem Commit: **`make check`** (gofmt, Tests, CGO-Prüfung, Build aller gültigen Kombinationen, Negativtests für fehlende Abhängigkeiten, UI-Build + `node --test`). Die CI macht dasselbe zusätzlich für alle 4 Plattformen.

## Konventionen

- Sprache: Code, Bezeichner und Commit-Betreff auf Englisch oder Deutsch einheitlich pro Datei; **UI-Texte, Fehlermeldungen für Kunden und Doku auf Deutsch** (de-AT als Standard, de-DE/de-CH berücksichtigen).
- Migrationen: `internal/modules/<id>/migrations/NNN_name.sql`, Tabellen mit Modul-Präfix (`shop_…`). Nie bestehende Migration ändern – immer neue anlegen.
- HTTP: Go-1.22-ServeMux-Muster (`"GET /pfad/{id}"`). Admin-Routen unter `/admin/<modul>/…`, öffentliche nach Modul sinnvoll (deutsche Slugs erlaubt, z. B. `/shop/warenkorb`).
- Tests: Tabellengetrieben; Integrationstests gegen echte Protokolle (siehe FTP/SSH-Tests in `factory-api/internal/deploy`); Handler mit `httptest`; ein Smoke-Test statt Browser-Tests (ADR-010).
- Commits klein, eine Session = ein oder wenige Commits, Betreff im Imperativ.

## Arbeitsweise pro Session

1. `docs/UMSETZUNGSPLAN.md` lesen, Session-Nummer und Abnahmekriterium bestätigen.
2. Bei größeren Sessions zuerst Plan-Modus.
3. Umsetzen + Tests. Abnahmekriterium aus dem Plan muss nachweisbar erfüllt sein.
4. Im Plan die Session abhaken (`- [x]`) und Abweichungen notieren.
5. Committen. Danach `/clear`.

Entscheidungen stehen in `docs/adr/README.md`. Weicht eine Aufgabe davon ab oder fehlt eine Entscheidung: nicht eigenmächtig entscheiden – nachfragen.
