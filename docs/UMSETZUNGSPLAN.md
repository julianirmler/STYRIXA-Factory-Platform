# Umsetzungsplan STYRIXA Factory Platform

Visuelle Übersicht: [Whiteboard „STYRIXA Factory – Umsetzungsplan“](https://claude.ai/artifact/JAChJcDmP9yXWaPg9YVgUe) · Entscheidungen: [`docs/adr/README.md`](adr/README.md)

**Prinzip:** Günstigste Lösung, die das Risiko beseitigt. Erst ein Durchstich (fertiges CMS wird ausgewählt, deployt, installiert), dann die Module. Jede Session ist eine abgeschlossene Claude-Code-Aufgabe mit Tests als Abnahme und endet mit Commit und `/clear`.

**Legende:** 🟣 = Opus (nur wo Sicherheit oder Architektur es verlangen), sonst Sonnet.
**Umfang:** **34 Sessions** (vorher 44), ca. **0,6–1,0 Mio. Output-Tokens**, ca. 55–95 Mio. verarbeitet (überwiegend Cache).

**Startprompt für jede Session:**
> Lies CLAUDE.md, docs/adr/README.md und docs/UMSETZUNGSPLAN.md. Setze Session X.Y um. Abnahme laut Plan, `make check` grün, Session abhaken, committen.

---

## Bereits erledigt

- [x] Configurator-Mockup mit 8 Modulen und Auslieferungsschritt (`factory-ui/src/StyrixaFactoryConfigurator.jsx`)
- [x] Kern-Registry `core.Module`, Build-Tag-Muster mit Guard-Dateien
- [x] Shop-Gerüst: Schema, DACH-Steuern mit Gültigkeit, Warenkorb-Berechnung, Product-JSON-LD
- [x] Stripe per HTTPS-API: Checkout Session, signierte Webhooks, Idempotenz, Betragsprüfung, Erstattungen
- [x] Deploy per SSH (Releases, systemd, Rollback) und FTPS (CGI-Modus), Fingerprint-Pinning, SSRF-Schutz, SSE-API
- [x] **Hoster-Check vor FTP-Deploy** – Website bleibt unberührt, wenn CGI oder mod_rewrite fehlen (ADR-008)
- [x] CMS: CGI-Modus, `/healthz`, `--addr` / `STYRIXA_DATA`
- [x] `modules.json` + `tools/matrix`: alle gültigen Modul-Kombinationen aus einer Quelle
- [x] `make check`: Tests, CGO-Prüfung, Build aller 36 Kombinationen, Negativtests (ADR-009)
- [x] GitHub Actions: CI (alle Kombinationen × 4 Plattformen) und Release (144 vorgebaute, signierte Pakete) (ADR-004)
- [x] Alle Architekturentscheidungen getroffen (`docs/adr/README.md`) – offen nur das Geschäftsmodell (ADR-011)

---

## Phase 0 – Fundament (1 Session)

- [x] **0.1** `go.sum` für `factory-api` erzeugen (verifiziert), Signaturschlüssel erzeugen und als GitHub-Secret setzen, `core.ReleasePublicKey` + `VerifyRelease`, ersten Test-Release `v0.0.1` taggen – **Anleitung: [`docs/phasen/PHASE-0.md`](phasen/PHASE-0.md)**
  - *Abnahme:* CI grün; Release enthält 144 Pakete + `SHA256SUMS` + `SHA256SUMS.sig` + `modules.json`
  - *Erledigt 2026-11-25, Abweichungen:*
    - `factory-api/go.mod`: `go 1.23` → `go 1.26.0` (durch `go get golang.org/x/crypto@latest`), `x/crypto` v0.31.0 → v0.59.0
    - Fehler im Release-Workflow behoben: `scripts/matrix-build.sh` schrieb bei relativem Ausgabeordner nach `cms-template/dist`, `upload-artifact` fand nichts. Ausgabeordner wird jetzt vor dem `cd` absolut gemacht. Tag `v0.0.1` danach auf den korrigierten Commit neu gesetzt (erster Lauf hatte keinen Release erzeugt).
    - `/healthz`-Test mit `styrixa_media-shop_darwin_arm64` statt `linux_amd64` (Prüfung lief auf macOS); Prüfsumme beider Pakete ok
    - Repo wurde auf GitHub in `STYRIXA-Factory-Platform` umbenannt; `origin` angepasst

## Phase 1 – CMS-Kern → M1 (6 Sessions)

**Anleitung: [`docs/phasen/PHASE-1.md`](phasen/PHASE-1.md)**

- [x] 🟣 **1.1** SQLite via `modernc.org/sqlite` (WAL, `busy_timeout`, Foreign Keys), Migrations-Runner pro Modul (`schema_migrations(module, version)`), `core.Deps.DB` befüllen; Go-Integrationstest für `shop.SQLPaymentStore`
  - *Abnahme:* Migrationen idempotent; zwei Prozesse parallel (CGI-Szenario) ohne „database is locked“
  - *Erledigt 2026-11-25, Abweichungen:*
    - `journal_mode(WAL)` steht **nicht** im DSN: Das Umschalten wartet nicht auf `busy_timeout`, zwei gleichzeitig startende Prozesse auf einer neuen DB bekamen sofort `SQLITE_BUSY`. Jetzt `ensureWAL` (einmalig umschalten, bei `SQLITE_BUSY` bis 5 s wiederholen; WAL bleibt in der Datei gespeichert).
    - Paralleltest mit 4 Prozessen × 1000 Transaktionen statt 2 × 200 – erst damit scheitert die Gegenprobe ohne `_txlock=immediate` zuverlässig.
    - `schema_migrations` speichert zusätzlich `name` und `checksum` (SHA-256), damit geänderte Migrationen erkannt werden.
    - `cms-template/go.mod`: `go 1.23` → `go 1.26.0`, `modernc.org/sqlite` v1.63.0
    - Gemessen: Server-Modus mit `mod_media mod_shop` nach Start 14,8 MB RSS
    - Für 5.4 offen: Zahlung auf stornierte Bestellung wird nur markiert, `payment_status` bleibt `open` (siehe `orders_db_test.go`)
- [x] **1.2** Settings-Store, `/install`-Wizard (Seitentitel, Region de-AT/DE/CH, GISA-Nummer, Admin-Konto mit bcrypt), Sperre nach Installation; Stripe-Schlüssel aus Settings statt Umgebung
  - *Abnahme:* zweiter Aufruf von `/install` → 404; Handler-Test des Wizards
  - *Erledigt 2026-11-25, Abweichungen:*
    - **Install-Schutz per Einmal-Code** (Nutzerentscheidung): `/install` verlangt den Code aus `<Datenordner>/install-code` (80 Bit, Format `ABCD-EFGH-IJKL-MNOP`, Vergleich in konstanter Zeit). SSH-/FTP-Deploy (`factory-api/internal/deploy/installcode.go`) legt ihn bei Erstinstallation an (0600), übernimmt einen vorhandenen bei wiederholtem Deploy und liefert ihn in `Result.InstallCode` (nicht im Deploy-Log); `factory-ui` zeigt ihn an. Ohne Deploy (Download) erzeugt das CMS die Datei selbst; der Server-Modus loggt nur den Pfad. Nach der Installation wird die Datei gelöscht.
    - Kern-Migrationen werden in `Migrate` immer zuerst angewendet (Modul-ID `core`, reserviert); bestehende Tests zählen `schema_migrations` ohne `core`.
    - Stripe wird **pro Webhook-Anfrage** aus den Settings gebaut statt einmal in `Mount` (`configureProviders` entfällt, jetzt `stripeFromSettings`): kein Zustand im Prozess, im Server-Modus gilt ein geänderter Schlüssel ohne Neustart. `STYRIXA_STRIPE_*` entfallen (README).
    - `POST /install` zusätzlich hinter `http.CrossOriginProtection`; Formular mit Passwort-Wiederholung; Passwort max. 72 Bytes (bcrypt-Grenze). GISA-Feld nur bei de-AT sichtbar (CSS `:has`, kein JS), für andere Regionen ignoriert.
    - Vor der Installation: GET → 303 auf `/install`, andere Methoden → 503 „noch nicht eingerichtet“.
    - `cms-template/go.mod`: `golang.org/x/crypto` v0.59.0, `golang.org/x/sys` v0.49.0 → v0.50.0
- [ ] 🟣 **1.3** Login, Sessions (HttpOnly, Secure, SameSite=Lax), CSRF-Token, Login-Rate-Limit in der DB (CGI-tauglich), Passwort-Reset per Mail (`net/smtp`)
  - *Abnahme:* Tests gegen Session-Fixation, CSRF, Brute-Force
- [ ] **1.4** Admin-Shell mit `html/template` + htmx (ADR-002): Layout, Navigation aus `AdminNav()`, Modul-Templates per `embed` im Modulpaket
  - *Abnahme:* Template eines Moduls nur im Binary, wenn sein Build-Tag gesetzt ist (Test)
- [ ] **1.5** Öffentliches Rendering: Routing, Templates, Theme-Fallback, Caching-Header (`ETag`), 404/500
  - *Abnahme:* Startseite im Server- und CGI-Modus identisch
- [ ] **1.6** Lazy-Jobs + Cron-Endpunkt (ADR-005), Logging, `/healthz` mit DB-Check
  - *Abnahme:* Job läuft im CGI-Modus ohne Hintergrundprozess

**M1:** Installierbares Kern-CMS mit Login und Admin-Oberfläche, RAM < 20 MB.

## Phase 2 – Durchstich Factory → M2 (3 Sessions)

- [ ] **2.1** factory-api: Release-Katalog (neuestes GitHub-Release lesen, `SHA256SUMS` + Signatur prüfen), Auswahl Konfiguration → Paket, Download-Endpunkt; `deploy.Artifact` aus dem Paket
  - *Abnahme:* manipuliertes Paket wird abgelehnt; Auswahl in < 1 s
- [ ] **2.2** `factory-ui` als Vite-Projekt, Module aus `modules.json`, „Build“ = Paket auswählen, echter Download
  - *Abnahme:* Download startet sofort, Paket entspricht der Auswahl
- [ ] **2.3** Echter Deploy aus dem UI (`MOCK_DEPLOY=false`); einmal auf echtem VPS und echtem Shared-Webspace
  - *Abnahme:* CMS läuft auf beiden, `/install` erreichbar

**M2:** Ende-zu-Ende: auswählen → deployen → installieren.

## Phase 3 – Pflichtmodule (5 Sessions)

- [ ] 🟣 **3.1** Content: Datenmodell (Seiten, Revisionen, Entwürfe), Block-JSON im Editor.js-Format, Go-Renderer pro Blocktyp, Slugs
  - *Abnahme:* Renderer-Test je Blocktyp, XSS-Test (Text-Blöcke werden escaped)
- [ ] **3.2** Editor.js einbinden (ADR-003): vendort, Grundblöcke + FAQ/Answer-First/CTA, Speichern per htmx
  - *Abnahme:* Seite ohne Code bau- und sortierbar
- [ ] **3.3** HTML-Modus (Textarea), Auto-Save alle 5 s, Vorschau, Revisionen wiederherstellen
  - *Abnahme:* kein Datenverlust bei Tab-Schließen; Revision wiederherstellbar
- [ ] **3.4** Dashboard: Seitenaufrufe serverseitig zählen (Tag × Pfad, keine Cookies, keine IPs), Status, Schnellzugriffe
  - *Abnahme:* kein Cookie-Banner nötig; keine personenbezogenen Daten gespeichert
- [ ] **3.5** Smoke-Test (ADR-010): Binary starten → `/install` → Login → Seite anlegen → öffentlich abrufen; in CI
  - *Abnahme:* Smoke-Test läuft in CI

## Phase 4 – Optionale Module → M3 (6 Sessions)

- [ ] **4.1** Media: Upload, Ordner, Alt-Texte; Verkleinerung/Umwandlung im Browser (ADR-001), `srcset`
- [ ] **4.2** SEO: Meta-Felder, Sitemap, robots.txt, 301-Redirects
- [ ] **4.3** GEO: JSON-LD (Organization/LocalBusiness mit GISA, FAQ, Article), `llms.txt`, Answer-First-Hinweis im Editor
- [ ] **4.4** Users: RBAC (Admin, Redakteur, Autor, Kunde), 2FA per TOTP (RFC 6238, Standardbibliothek), Aktivitätsprotokoll
- [ ] **4.5** Design: Theme-Tokens → CSS-Variablen, Fonts lokal (DSGVO), Template-Auswahl
- [ ] **4.6** Themes: `theme.json` v1, ZIP-Prüfung (Zip-Slip, Größen- und Dateianzahl-Limits, keine ausführbaren Dateien, keine Symlinks), Aktivierung, Rollback; Guard `mod_themes → mod_design`

**M3:** Alle 8 Module fertig, alle 36 Kombinationen gebaut und getestet.

## Phase 5 – Shop komplett (5 Sessions)

*(Kann direkt nach Phase 3 vorgezogen werden – dann 4.1 Media vorher.)*

- [ ] **5.1** Produkt- und Kategorie-Admin, Varianten, Lager, Preisverlauf
- [ ] **5.2** Katalog- und Produktseiten, JSON-LD, Grundpreis, 30-Tage-Tiefstpreis bei Rabatten
- [ ] **5.3** Warenkorb, Checkout (`POST /shop/kasse` → Stripe `Start` oder Vorkasse), Danke-Seite („Zahlung wird geprüft“), Lager bei Zahlung reduzieren
- [ ] **5.4** Rechnungs-PDF (eigener minimaler PDF-Schreiber, ADR-007), fortlaufende Nummern, Gutschriften, Bestellmails, Erstattung per Klick im Admin
- [ ] **5.5** Rechtstexte verknüpfen + Pflicht-Checkbox, Steuer-Admin, Hinweis „vom Betreiber zu prüfen“
- *Abnahme Phase 5:* Testbestellung end-to-end im Stripe-Testmodus (inkl. PayPal/EPS über Stripe), Rechnung korrekt je Steuersatz

## Phase 6 – Factory produktreif → M4 (4 Sessions)

- [ ] **6.1** Kundenkonten per Magic-Link (ADR-011), Projekte (mehrere CMS pro Kunde)
- [ ] **6.2** Gespeicherte Deploy-Ziele (Fingerprints ja, Passwörter nein), verschlüsselte Deploy-Keys
- [ ] 🟣 **6.3** Selbst-Update im CMS (ADR-006): Release-Manifest lesen, Signatur prüfen, Binary tauschen (Server- und CGI-Modus)
- [ ] **6.4** Abrechnung per Stripe Checkout – **entfällt**, solange die Factory nur für STYRIXA-Kunden mit Freischaltung von Hand läuft (ADR-011)

## Phase 7 – Härtung & Launch → M5 (3 Sessions)

- [ ] 🟣 **7.1** Security-Review in frischer Session ohne Vorwissen (OWASP Top 10, Uploads, HTML-Modus, Deploy-API, Theme-ZIPs, Self-Update)
- [ ] **7.2** Kunden-Doku: Installation per SSH und FTP, Stripe einrichten, Rechtstexte
- [ ] **7.3** Beta mit 2–3 echten Websites (z. B. STYRIXA-Website selbst)

---

## Risiken – alle mit Gegenmaßnahme erledigt

| Risiko | Lösung (günstigste) | Status |
|---|---|---|
| AVIF/WebP ohne CGO | Umwandlung im Browser (WebP, Safari: JPEG), kein AVIF, kein Server-Code – ADR-001 | ✅ entschieden |
| Block-Editor als Zeitfresser | Editor.js statt Eigenbau, Admin mit htmx statt React – ADR-002/003 | ✅ entschieden |
| Shared-Hoster ohne CGI/.htaccess | Automatischer Hoster-Check vor jeder Änderung, klare Diagnose – ADR-008 | ✅ umgesetzt + getestet |
| Abhängigkeiten nicht reines Go | `make cgo` + CI-Build aller Kombinationen mit `CGO_ENABLED=0` – ADR-009 | ✅ umgesetzt + getestet |
| Rechtliches im Shop | Nur Stripe Checkout (inkl. PayPal), Rechtstexte als Seiten, Pflicht-Checkbox, klarer Betreiber-Hinweis – ADR-007 | ✅ entschieden |
| Build-Server (Kosten, Sicherheit, Aufwand) | Alle Kombinationen per GitHub Actions vorgebaut und signiert – ADR-004 | ✅ umgesetzt |
| Archivierte PDF-Bibliothek | Eigener minimaler PDF-Schreiber – ADR-007 | ✅ entschieden |
| Browser-Test-Pflege | Ein Smoke-Test statt Playwright-Suite – ADR-010 | ✅ entschieden |

**Bleibt:** Rechtliche Verantwortung für Shop-Inhalte, Steuersätze und Rechtstexte liegt beim Betreiber (wie bei jedem Shop-System) – wird im Produkt klar ausgewiesen. Geschäftsmodell der Factory (ADR-011) ist deine Entscheidung.

**Gestrichen gegenüber Version 1:** Bild-Prototyp (0.3), Build-Worker/Sandbox/Cache (2.1–2.2), eigener Block-Editor + CodeMirror (3.2–3.3 vereinfacht), Playwright-Suite (3.5–3.6), zweite Media- und Users-Session, PayPal-Integration (5.5), Monitoring Build-Worker (6.5), Lasttests (7.2).
