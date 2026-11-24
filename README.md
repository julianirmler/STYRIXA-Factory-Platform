# STYRIXA Factory Platform

Custom-CMS-Generator von **STYRIXA Digital Technology Studio**. Nutzer wählen Module, die Factory kompiliert daraus ein CGO-freies Go-Single-Binary mit eingebetteter SQLite (`modernc.org/sqlite`) und liefert es als `.tar.gz` aus.

## Struktur

| Ordner | Inhalt | Stand |
|---|---|---|
| `factory-ui/` | React + Vite + Tailwind Configurator (Modulauswahl, Ziel-OS, Build) | UI-Mockup (`src/StyrixaFactoryConfigurator.jsx`), Build simuliert |
| `factory-api/` | Go-Backend: Build-Worker und Deploy-Dienst (SSH / FTPS) | Deploy fertig + getestet, Build-Endpunkt geplant |
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

## Auslieferung / Deployment

Nach dem Build wählt der Kunde in Schritt 4:

| Weg | Für | Ablauf |
|---|---|---|
| **Download** | Selbst-Installation | `.tar.gz` herunterladen |
| **SSH** | VPS, Managed Server, Hosting mit SSH | Plattform-Check (`uname`) → `releases/<zeitstempel>/` → atomarer Wechsel von `current` → systemd-User-Service (Fallback nohup) → Health-Check per SSH-Tunnel → bei Fehler automatischer Rollback; 3 Releases bleiben erhalten |
| **FTP(S)** | Shared-Webspace mit Apache | Binary als `styrixa.cgi` + `.htaccess` (CGI-Routing), geschützter Datenordner `styrixa-data/`, bestehende `.htaccess` wird gesichert, `styrixa.db` nie überschrieben → Health-Check über die Website-URL → bei Fehler vorherige `styrixa.cgi` zurück |

Sicherheit:
- Server-Identität wird immer geprüft: SSH-Host-Key bzw. FTPS-Zertifikat per SHA-256-Fingerprint, den der Kunde im UI bestätigt (danach gepinnt; Abweichung = Abbruch).
- Zugangsdaten nur für die Dauer des Deployments im Speicher, nie geloggt oder gespeichert.
- SSRF-Schutz: Die Factory verbindet sich nur mit öffentlichen IP-Adressen (kein localhost, keine privaten Netze, keine Cloud-Metadaten).
- Unverschlüsseltes FTP nur mit ausdrücklicher Freigabe.

API: `POST /api/builds/{id}/deploy` streamt das Deploy-Log als Server-Sent Events (`log`, `identity`, `result`, `error`).

Das CMS erkennt den CGI-Modus automatisch (`GATEWAY_INTERFACE`) und bietet `/healthz`.

```bash
cd factory-api
go mod tidy          # erzeugt go.sum (Checksummen werden gegen sum.golang.org geprüft)
go get -u golang.org/x/crypto && go mod tidy   # aktuelle Sicherheitsfixes
go test ./...        # FTP-Tests brauchen: pip install pyftpdlib pyopenssl
```

## Hinweis Shop

Steuersätze (AT/DE/CH, inkl. AT 4,9 % ab 1.7.2026) liegen mit Gültigkeitsdatum in `shop_tax_rates`. Die Startwerte sind vom Betreiber zu prüfen und ersetzen keine Steuerberatung.
