# STYRIXA Factory Platform

Custom-CMS-Generator von **STYRIXA Digital Technology Studio**. Nutzer wählen Module, die Factory kompiliert daraus ein CGO-freies Go-Single-Binary mit eingebetteter SQLite (`modernc.org/sqlite`) und liefert es als `.tar.gz` aus.

## Struktur

| Ordner | Inhalt | Stand |
|---|---|---|
| `factory-ui/` | React + Vite + Tailwind Configurator (Modulauswahl, Ziel-OS, Build) | UI-Mockup (`src/StyrixaFactoryConfigurator.jsx`), Build simuliert |
| `factory-api/` | Go-Backend: nimmt Build-Config entgegen, führt `go build -tags …` aus, liefert `.tar.gz` | geplant |
| `cms-template/` | STYRIXA-CMS-Quellcode, Module per `//go:build mod_<name>` | Kern-Registry, Shop-Modul (Gerüst), Media-Platzhalter |

## Module

| Nr | Modul | Build-Tag | |
|---|---|---|---|
| 01 | Dashboard | `mod_dashboard` | Pflicht |
| 02 | Content Management | `mod_content` | Pflicht |
| 03 | Media Management | `mod_media` | optional |
| 04 | SEO & GEO Management | `mod_seo` | optional |
| 05 | User Management | `mod_users` | optional |
| 06 | Design Management | `mod_design` | optional |
| 07 | Theme Management | `mod_themes` | optional, benötigt `mod_design` |
| 08 | Shop Management | `mod_shop` | optional, benötigt `mod_media` |

## Build-Regeln

- Immer `CGO_ENABLED=0`.
- Ein Modul wird ausschließlich über `cms-template/cmd/styrixa/mod_<name>.go` (mit `//go:build mod_<name>`) importiert und meldet sich in `init()` per `core.Register()` an.
- Fehlende Abhängigkeiten scheitern bereits beim Kompilieren (Guard-Dateien wie `guard_shop.go`) und zusätzlich zur Laufzeit in `core.Modules()`.

```bash
cd cms-template
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" \
  -tags "mod_media mod_shop" -o dist/styrixa ./cmd/styrixa
```

## Hinweis Shop

Steuersätze (AT/DE/CH, inkl. AT 4,9 % ab 1.7.2026) liegen mit Gültigkeitsdatum in `shop_tax_rates`. Die Startwerte sind vom Betreiber zu prüfen und ersetzen keine Steuerberatung.
