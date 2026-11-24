# Umsetzungsplan STYRIXA Factory Platform

Visuelle Übersicht: [Whiteboard „STYRIXA Factory – Umsetzungsplan“](https://claude.ai/artifact/JAChJcDmP9yXWaPg9YVgUe)

**Prinzip:** Erst ein dünner Durchstich durchs ganze System (Factory baut ein echtes CMS und deployt es), dann die Module ausbauen. Jede Session ist eine abgeschlossene Claude-Code-Aufgabe mit Tests als Abnahme und endet mit Commit und `/clear`.

**Legende:** 🟣 = Opus empfohlen (Architektur, Sicherheit, knifflige Logik), sonst Sonnet.
**Umfang:** 44 Sessions, ca. 1–1,6 Mio. Output-Tokens, ca. 90–150 Mio. verarbeitete Tokens (überwiegend Cache).

**Startprompt für jede Session:**
> Lies CLAUDE.md und docs/UMSETZUNGSPLAN.md. Setze Session X.Y um. Abnahme laut Plan, Tests grün in allen betroffenen Tag-Kombinationen, Session im Plan abhaken, dann committen.

---

## Bereits erledigt

- [x] Configurator-Mockup mit 8 Modulen und Auslieferungsschritt (`factory-ui/src/StyrixaFactoryConfigurator.jsx`)
- [x] Kern-Registry `core.Module`, Build-Tag-Muster mit Guard-Dateien
- [x] Shop-Gerüst: Schema, DACH-Steuern mit Gültigkeit, Warenkorb-Berechnung, Zahlungs-Interface, Product-JSON-LD
- [x] Deploy per SSH (Releases, systemd, Rollback) und FTPS (CGI-Modus), Fingerprint-Pinning, SSRF-Schutz, SSE-API
- [x] CMS: CGI-Modus, `/healthz`, `--addr` / `STYRIXA_DATA`
- [x] Stripe per HTTPS-API (Teil von 5.4): Checkout Session, signierte Webhooks, Idempotenz, Betragsprüfung, Erstattungen

---

## Vorab: Entscheidungen (in Session 0.3 als ADRs festhalten)

| # | Frage | Vorschlag |
|---|---|---|
| ADR-1 | Bildoptimierung ohne CGO | Umwandlung im Browser beim Upload (WebP/AVIF per WASM), Server speichert nur. Prototyp in 0.3 |
| ADR-2 | Admin-UI-Technik | React + Vite; jedes Modul baut seinen UI-Teil separat und bettet ihn im eigenen Go-Paket ein |
| ADR-3 | Hintergrundaufgaben | Keine Daemons; Lazy-Jobs bei Anfrage + optionaler Cron-Endpunkt (CGI-tauglich) |
| ADR-4 | Updates der Kunden-CMS | Factory baut gleiche Config mit neuer Version neu; CMS prüft ed25519-Signatur und tauscht sich über den Deploy-Weg aus |
| ADR-5 | Geschäftsmodell Factory | offen: kostenlos / pro Build / Abo / nur STYRIXA-Kunden – bestimmt Umfang von Phase 6 |

---

## Phase 0 – Fundament

- [ ] **0.1** `CLAUDE.md` finalisieren, Makefile (`test`, `build`, `matrix`), `go.sum` für beide Module erzeugen und committen
  - *Abnahme:* `make test` läuft lokal grün
- [ ] **0.2** GitHub Actions: vet/test, Build-Matrix über Tag-Kombinationen × 4 Plattformen, Negativ-Test (Modul ohne Abhängigkeit muss scheitern), UI-Build
  - *Abnahme:* CI grün; ein absichtlich falscher Cross-Modul-Import lässt CI scheitern
- [ ] **0.3** Prototyp Bildumwandlung im Browser (WASM) + ADR-1 bis ADR-5 in `docs/adr/`
  - *Abnahme:* Messwerte (Qualität, Dateigröße, Dauer für 5 typische Fotos) dokumentiert

## Phase 1 – CMS-Kern → Meilenstein M1

- [ ] 🟣 **1.1** SQLite via `modernc.org/sqlite` (WAL, `busy_timeout`, Foreign Keys), Migrations-Runner pro Modul (`schema_migrations(module, version)`), Transaktions-Helfer, `core.Deps.DB` befüllen
  - *Abnahme:* Migrationen idempotent; Test mit und ohne `mod_shop`; paralleler Zugriff zweier Prozesse (CGI-Szenario) ohne „database is locked“
- [ ] 🟣 **1.2** Settings-Store, `/install`-Wizard (Seitentitel, Region de-AT/DE/CH, GISA-Nummer mit Formatprüfung, Admin-Konto mit bcrypt), Sperre nach Installation
  - *Abnahme:* zweiter Aufruf von `/install` → 404; E2E-Test des Wizards
- [ ] 🟣 **1.3** Login, Sessions (HttpOnly, Secure, SameSite), CSRF-Schutz, Login-Rate-Limit (DB-basiert, CGI-tauglich), Passwort-Reset
  - *Abnahme:* Tests gegen Session-Fixation, CSRF, Brute-Force
- [ ] 🟣 **1.4** Admin-Shell (React) mit Lademechanismus für Modul-UIs (ADR-2), Navigation aus `AdminNav()`
  - *Abnahme:* UI eines Moduls erscheint nur, wenn dessen Build-Tag gesetzt ist; Binary ohne Modul enthält dessen UI-Bundle nicht
- [ ] **1.5** Öffentliches Rendering: Routing, Go-Templates, Theme-Fallback, Caching-Header, 404/500-Seiten
  - *Abnahme:* Startseite im Server- und CGI-Modus identisch
- [ ] **1.6** Lazy-Jobs + Cron-Endpunkt (ADR-3), strukturiertes Logging, `/healthz` mit DB-Check
  - *Abnahme:* Job läuft im CGI-Modus ohne Hintergrundprozess

**M1:** Installierbares Kern-CMS mit Login und leerer Admin-Oberfläche, RAM < 20 MB.

## Phase 2 – Durchstich Factory → Meilenstein M2

- [ ] 🟣 **2.1** Build-Worker: Warteschlange, abgeschotteter `go build` (Container ohne Netz, vendored deps), Cache nach Hash(Config + Template-Commit)
  - *Abnahme:* zwei identische Builds → identischer Binary-Hash
- [ ] **2.2** `POST /api/builds` mit SSE-Log, `.tar.gz` + SHA-256 + ed25519-Signatur, `meta.json` für den Deploy
  - *Abnahme:* Signatur verifizierbar; Deploy-Endpunkt findet den Build
- [ ] **2.3** `factory-ui` als Vite-Projekt (Tailwind, lucide-react), Configurator an echten Build angebunden
  - *Abnahme:* Klick auf „Build“ liefert echtes Paket zum Download
- [ ] **2.4** Echter Deploy aus dem UI (`MOCK_DEPLOY=false`); Test auf echtem VPS und echtem Shared-Webspace
  - *Abnahme:* CMS läuft auf beiden, `/install` erreichbar

**M2:** Ende-zu-Ende: auswählen → bauen → deployen → installieren.

## Phase 3 – Pflichtmodule

- [ ] 🟣 **3.1** Content: Datenmodell (Seiten, Revisionen, Entwürfe), Blöcke als JSON-Schema, serverseitige Renderer
  - *Abnahme:* jeder Blocktyp mit Renderer-Test
- [ ] 🟣 **3.2** Block-Editor: Drag-and-Drop, Grundblöcke (Text, Bild, Spalten, CTA, FAQ, Answer-First)
  - *Abnahme:* Seite ohne Code baubar
- [ ] 🟣 **3.3** HTML Studio (CodeMirror), Wechsel Block ↔ HTML, Auto-Save, Vorschau, Revisionen wiederherstellen
  - *Abnahme:* kein Datenverlust bei Tab-Schließen oder gleichzeitiger Bearbeitung
- [ ] **3.4** Dashboard: Besucherstatistik ohne Cookies (aggregiert, DSGVO-konform), Status, Schnellzugriffe
  - *Abnahme:* kein Cookie-Banner nötig; keine personenbezogenen Daten gespeichert
- [ ] **3.5–3.6** Barrierefreiheit (WCAG 2.1 AA) im Admin, Playwright-E2E für Kernabläufe
  - *Abnahme:* E2E läuft in CI

## Phase 4 – Optionale Module → Meilenstein M3

- [ ] **4.1** Media: Upload, Ordner, Alt-Texte, Dateiliste
- [ ] **4.2** Media: Bildpipeline nach ADR-1, `srcset`, Platzhalter
- [ ] **4.3** SEO: Meta-Felder, Sitemap, robots.txt, 301-Redirects
- [ ] **4.4** GEO: JSON-LD (Organization/LocalBusiness mit GISA, FAQ, Article), `llms.txt`, Answer-First-Prüfung im Editor
- [ ] **4.5** Users: RBAC (Admin, Redakteur, Autor, Kunde), Übernahme des Kern-Admins
- [ ] **4.6** Users: 2FA (TOTP), Aktivitätsprotokoll
- [ ] **4.7** Design: Theme-Tokens → CSS-Variablen, Fonts lokal gehostet (DSGVO), Template-Auswahl
- [ ] 🟣 **4.8** Themes: Spezifikation `theme.json` v1, ZIP-Prüfung (Zip-Slip, Größen-/Dateianzahl-Limits, keine ausführbaren Dateien, Symlinks verboten)
- [ ] **4.9** Themes: Aktivierung per Klick, Vorschau, Rollback; Abhängigkeit `mod_themes → mod_design`

**M3:** Alle 7 Module fertig, in jeder Kombination baubar und getestet.

## Phase 5 – Shop komplett

*(Kann direkt nach Phase 3 vorgezogen werden – dann 4.1/4.2 Media vorher.)*

- [ ] **5.1** Produkt- und Kategorie-Admin, Varianten, Lager, Preisverlauf
- [ ] **5.2** Katalog- und Produktseiten, JSON-LD, Grundpreis, 30-Tage-Tiefstpreis bei Rabatten
- [ ] **5.3** Warenkorb (Cookie-Session) und Checkout (Gast oder Kundenkonto bei `mod_users`)
- [ ] 🟣 **5.4** Stripe per HTTP-API (Karte, EPS, Klarna), signierte Webhooks, Idempotenz
  - *Stand:* API-Client, Webhook-Prüfung, Statuslogik und SQL-Store fertig + getestet. **Offen:** Checkout-Handler (`POST /shop/kasse` → `Start`), Danke-Seite, Erstattung im Admin, Schlüssel aus Settings statt Umgebung (nach 1.2), Go-Integrationstest des SQL-Stores (nach 1.1)
- [ ] **5.5** PayPal, Vorkasse, Zahlungsabgleich im Admin
- [ ] **5.6** Rechnungs-PDF (reines Go), fortlaufende Nummern, Gutschriften, Bestellmails (SMTP)
- [ ] **5.7** AGB-/Widerrufs-Verankerung, Pflicht-Checkbox, OSS-Schalter, Steuer-Admin
- *Abnahme Phase 5:* Testbestellung end-to-end mit Stripe-Testmodus, Rechnung korrekt je Steuersatz

## Phase 6 – Factory produktreif → Meilenstein M4

- [ ] **6.1** Kundenkonten, Login, Projektverwaltung (mehrere CMS pro Kunde)
- [ ] **6.2** Build-Verlauf, gespeicherte Deploy-Ziele (Fingerprints ja, Passwörter nein), verschlüsselte Deploy-Keys
- [ ] 🟣 **6.3** Update-Kanal (ADR-4): neue CMS-Version → Neubau → signierte Selbstaktualisierung
- [ ] **6.4** Abrechnung / Freischaltung nach ADR-5
- [ ] **6.5** Monitoring, Rate-Limits und Kontingente für den Build-Worker

## Phase 7 – Härtung & Launch → Meilenstein M5

- [ ] 🟣 **7.1** Security-Review in frischer Session ohne Vorwissen (OWASP Top 10, Uploads, HTML Studio, Deploy-API, Theme-ZIPs)
- [ ] **7.2** Lasttests Server-Modus vs. CGI-Modus, RAM-Ziel < 20 MB nachweisen
- [ ] **7.3** Kunden-Doku, Installationsanleitungen pro Hoster-Typ
- [ ] **7.4** Beta mit 2–3 echten Websites (z. B. STYRIXA-Website selbst)

---

## Risiken

| Risiko | Gegenmaßnahme |
|---|---|
| AVIF/WebP ohne CGO | ADR-1 früh klären (Session 0.3) |
| Block-Editor wird zum Zeitfresser | Grundblöcke zuerst, Erweiterungen später; Opus für 3.1–3.3 |
| Shared-Hoster ohne CGI/.htaccess | Deploy meldet klar; SSH als Empfehlung; Hoster-Kompatibilitätsliste in 7.3 |
| Abhängigkeiten nicht reines Go | CLAUDE.md-Regel 1, CI-Build mit `CGO_ENABLED=0` |
| Rechtliches im Shop (Steuern, Preisangaben) | Werte konfigurierbar, Hinweis „vom Betreiber zu prüfen“, keine Steuerberatung im Produkt |
