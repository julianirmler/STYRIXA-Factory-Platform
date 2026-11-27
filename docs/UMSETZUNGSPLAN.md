# Umsetzungsplan STYRIXA Factory Platform

Visuelle Übersicht: [Whiteboard „STYRIXA Factory – Umsetzungsplan“](https://claude.ai/artifact/JAChJcDmP9yXWaPg9YVgUe) · Entscheidungen: [`docs/adr/README.md`](adr/README.md)

**Prinzip:** Günstigste Lösung, die das Risiko beseitigt. Erst ein Durchstich (fertiges CMS wird ausgewählt, deployt, installiert), dann die Module. Jede Session ist eine abgeschlossene Claude-Code-Aufgabe mit Tests als Abnahme und endet mit Commit und `/clear`.

**Legende:** 🟣 = Opus (nur wo Sicherheit oder Architektur es verlangen), sonst Sonnet.
**Umfang:** **41 Sessions** (vorher 44, dann 34; +5 Shop nach AT/EU-Recht am 25.11.2026, +1 PHP-Brücke für easyname am 26.11.2026, +1 Google Analytics mit Einwilligung am 27.11.2026), ca. **0,7–1,15 Mio. Output-Tokens**, ca. 65–110 Mio. verarbeitet (überwiegend Cache).

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
- [x] 🟣 **1.3** Login, Sessions (HttpOnly, Secure, SameSite=Lax), CSRF-Token, Login-Rate-Limit in der DB (CGI-tauglich), Passwort-Reset per Mail (`net/smtp`)
  - *Abnahme:* Tests gegen Session-Fixation, CSRF, Brute-Force
  - *Erledigt 2026-11-25, Abweichungen/Entscheidungen:*
    - **URLs** (Nutzerentscheidung): `/admin/login`, `POST /admin/logout`, `/admin/passwort-vergessen`, `/admin/passwort-neu?token=…`; dazu eine vorläufige Admin-Startseite `GET /admin` mit Logout (wird in 1.4 durch die Admin-Shell ersetzt).
    - **Client-IP** (Nutzerentscheidung): letzter Eintrag in `X-Forwarded-For` nur, wenn die Verbindung von Loopback kommt (Caddy/nginx des SSH-Deploys), sonst `RemoteAddr` bzw. `REMOTE_ADDR` (CGI). `core.ClientIP`.
    - **Reset-Link** (Nutzerentscheidung): neues Setting `site.url`, von `/install` aus der (durch den Einmal-Code geschützten) Anfrage gespeichert – nie aus dem Host-Header der Reset-Anfrage. Ohne `site.url` wird keine Mail gesendet, Antwort bleibt gleich.
    - SMTP-Settings: `mail.smtp.host`, `mail.smtp.port` (Standard 587), `mail.smtp.user`, `mail.smtp.password`, `mail.from`. STARTTLS ist Pflicht (sonst kein Versand, kein AUTH), Zertifikat wird geprüft. Eingabe im Admin folgt mit den Admin-Settings.
    - `http.CrossOriginProtection` jetzt global um den ganzen Handler (lässt GET und Anfragen ohne `Origin`/`Sec-Fetch-Site` wie den Stripe-Webhook durch), damit auch das Login-Formular gegen Login-CSRF geschützt ist.
    - Rate-Limit: Versuch wird **vor** der bcrypt-Prüfung in einer `IMMEDIATE`-Transaktion gezählt, damit parallele Anfragen das Limit nicht überholen (Test mit 20 parallelen Versuchen). Bei Erfolg werden die Fehlversuche der E-Mail und der eigene IP-Eintrag gelöscht. Reset-Anfragen haben eigene Zähler (gleiche Grenzen, gegen Mail-Flut).
    - Dummy-Hash für unbekannte E-Mails ist eine Konstante mit Kosten 12 (gleiche Rechenzeit wie echte Konten).
    - `last_seen` wird höchstens einmal pro Minute geschrieben (spart im CGI-Modus einen Schreibzugriff pro Anfrage).
    - Passwortänderung gibt es vorerst nur über den Reset (`Auth.SetPassword` löscht alle Sessions und Reset-Tokens); ein Formular dafür kommt mit der Admin-Shell.
    - Offen für 1.6: Die Reset-Mail wird synchron in der Anfrage gesendet (keine Goroutine im CGI-Modus) – die Antwortzeit kann verraten, ob es das Konto gibt. Abhilfe wäre Versand als Lazy-Job.
- [x] **1.4** Admin-Shell mit `html/template` + htmx (ADR-002): Layout, Navigation aus `AdminNav()`, Modul-Templates per `embed` im Modulpaket
  - *Abnahme:* Template eines Moduls nur im Binary, wenn sein Build-Tag gesetzt ist (Test)
  - *Erledigt 2026-11-25, Abweichungen/Entscheidungen:*
    - Abnahmetest `cmd/styrixa/isolation_test.go` baut echte Binaries (ohne Tags, `mod_media`, `mod_media mod_shop`) und sucht den Marker aus `shop/templates/placeholder.html`; läuft über `go test ./...` und damit in `make check` und CI (kein eigenes Make-Ziel nötig).
    - htmx 2.0.11 per `npm pack` (Registry-Integrität geprüft), byte-identisch als `internal/core/admin/static/htmx-2.0.11.min.js`; Version, Lizenz und SHA-256 in `internal/core/admin/VENDOR.md` statt als Kommentar in der Datei (sonst wäre die Prüfsumme nicht mehr die des Originals).
    - Statische Dateien unter `/admin/static/<name>.<hash>.<ext>` (Inhalts-Hash zusätzlich zur Version, gilt auch für `admin.css`), `immutable`, **ohne Login** abrufbar (nichts Geheimes).
    - Admin-CSP schon jetzt: `default-src 'self'` ohne Inline-Skripte/-Styles; dafür htmx-Konfiguration `includeIndicatorStyles:false, allowEval:false, allowScriptTags:false`, Indikator-Styles in `admin.css`. CSRF-Token für htmx per `hx-headers` am `<body>`.
    - Module rendern über `Deps.Admin` (`core.AdminRenderer`); optionales Interface `core.AdminTemplates`. Jedes Modul-Template wird in eine eigene Kopie des Layouts geparst (Name `<modul-id>/<pfad>`) – gleichnamige Hilfs-Templates verschiedener Module kollidieren nicht. `core.AdminPlaceholder` für noch leere Seiten (Media nutzt ihn für `/admin/media`).
    - Die vorläufige Startseite aus 1.3 ist ersetzt (`GET /admin` = Übersicht der Shell). Shop-Admin-Routen rendern eine Platzhalterseite im Layout (auch die POST-Routen, Status 200).
    - Offen: Formular „Passwort ändern“ im Admin (in 1.3 für die Shell vorgemerkt) ist noch nicht gebaut.
- [x] **1.5** Öffentliches Rendering: Routing, Templates, Theme-Fallback, Caching-Header (`ETag`), 404/500
  - *Abnahme:* Startseite im Server- und CGI-Modus identisch
  - *Erledigt 2026-11-25, Abweichungen/Entscheidungen:*
    - Abnahmetest `cmd/styrixa/public_test.go`: `main.go` baut den Handler jetzt in `newHandler` (gleich für beide Modi); Server = `httptest.NewServer(newHandler(…))`, CGI = echtes Binary über `net/http/cgi.Handler`. Verglichen werden Status, Header (Content-Type/-Length, Cache-Control, ETag, Sicherheits-Header, Location) und Körper für Startseite (GET, HEAD, 304), 404, `/admin` ohne Login und `/healthz`.
    - Der Test fand einen echten Unterschied: Die CGI-Abkürzung für `PATH_INFO=/healthz` lief an allen Middlewares vorbei (keine Sicherheits-Header) – behoben.
    - **Theme-Schnittstelle** `core.PublicTheme` (`PublicTemplates() fs.FS`) mit `layout.html`, `home.html`, `404.html`, `500.html`; ein Theme darf eine Teilmenge liefern, der Rest kommt aus `internal/core/public/templates/`. Höchstens ein Modul im Binary darf ein Theme liefern (sonst Startfehler) – für 4.5/4.6 ggf. neu entscheiden, wenn Design und Themes zusammenspielen. Daten-Vertrag: `core.PublicView` (Lang, SiteTitle, Title, Status, Data).
    - ETag = erste 8 Byte von SHA-256 des Körpers (16 Hex), nur bei 200; `If-None-Match` mit schwachem Vergleich, Listen und `*`. HTML: `no-cache`, 500: `no-store`, `/admin/*`: `no-store` (Middleware-Standard; statische Admin-Dateien überschreiben mit `immutable`).
    - `core.SecurityHeaders` (nosniff, `Referrer-Policy: strict-origin-when-cross-origin`, Admin-CSP für `/admin/*`) und `Public.Recover` (Panic → 500-Seite, Stack nur ins Log) umschließen den ganzen Handler. Login/Install behalten ihre strengere `no-referrer`-Policy und eigene CSP (Inline-Styles).
    - Alles, was keine Route trifft, bekommt die Theme-404 (auch falsche Methode statt 405); unter `/admin/*` schlichter Text ohne Theme. 500 bei Template-Fehler zeigt keine Teilausgabe; scheitert auch die 500-Seite, kommt Klartext.
    - Für Phase 5 offen: Module bekommen noch keinen Zugriff auf die Theme-404 (`Deps.Public` o. ä.), z. B. für unbekannte Produkte.
- [x] **1.6** Lazy-Jobs + Cron-Endpunkt (ADR-005), Logging, `/healthz` mit DB-Check
  - *Abnahme:* Job läuft im CGI-Modus ohne Hintergrundprozess
  - *Erledigt 2026-11-25, Abweichungen/Entscheidungen:*
    - Abnahmetest `cmd/styrixa/jobs_test.go`: echtes Binary über `net/http/cgi.Handler`; der Kern-Job `core.cleanup` (stündlich: abgelaufene Sessions, benutzte/abgelaufene Reset-Tokens, alte Fehlversuche) läuft nach der Antwort im selben Prozess und löscht eine abgelaufene Session; zweite Anfrage → nicht erneut. Parallelität: 8 echte Prozesse → Job genau einmal (`internal/core/jobs_test.go`).
    - **Cron-Token nur im Header** `X-Styrixa-Cron-Token` (Nutzerentscheidung, ADR-005 ergänzt). Gespeichert nur als SHA-256 (`core.cron.token_sha256`, `core.NewCronToken`); ohne eingerichteten Token, mit falschem oder mit Token in der URL → 404. Fehlgeschlagener Job → 500 „Fehler“ ohne Details.
    - **Offen (Nutzerentscheidung):** Der Kunde bekommt den Cron-Token erst mit der Admin-Settings-Seite (neuen Token erzeugen, einmal anzeigen). Bis dahin ist `/cron` aus, Lazy-Jobs reichen.
    - „Nach der Antwort“ als gemeinsamer Mechanismus `core.After`/`core.Defer`: CGI – nach `cgi.Serve` stdout schließen (Apache liefert aus), `SIGTERM` ignorieren (Apache beendet CGI-Prozesse nach der Antwort mit SIGTERM, nach 3 s SIGKILL), Budget `core.AfterBudget` = 2,5 s. Server – `core.AfterResponse` puffert die Antwort (bis 1 MB), schließt sie mit `Content-Length` ab und flusht, dann folgt die Arbeit in derselben Anfrage (keine Goroutine). Gestreamte Antworten → kein Lazy-Job.
    - Pro Anfrage **entweder** aufgeschobene Arbeit **oder** ein Lazy-Job (gemeinsames Budget). Jobs erst nach der Installation. `last_run` wird auch bei Fehler gesetzt (sonst liefe ein kaputter Job bei jeder Anfrage); Sperre = Budget + 1 min, danach gilt ein Lauf als abgebrochen. Job-Namen `<modul>.<name>`, Intervall ≥ 1 min. Kosten ohne fälligen Job: zwei kurze Abfragen.
    - **Reset-Mail nach der Antwort** (Nutzerentscheidung, offener Punkt aus 1.3 erledigt): Nutzer-Lookup, Token und SMTP laufen per `core.Defer` – die Antwortzeit verrät nicht mehr, ob es das Konto gibt. Der Token wird dafür nirgends zwischengespeichert.
    - `/healthz` = `core.Health`: `SELECT 1` + `core.MigrationsPending` = 0 → `ok`, sonst 503 „nicht bereit“ ohne Details.
    - Logging: `slog`-Text-Handler auf stderr; im CGI-Modus nur `WARN`+ (Apache-Error-Log). `log.Fatal` ersetzt.
    - **M1-Messung:** macOS arm64, alle Module, nach 200 Anfragen: RSS 22 MB, davon gemeinsam genutzte Systembibliotheken; Physical Footprint 10,2 MB, Go-Heap ≤ 4 MB. Die RSS unter macOS ist nicht vergleichbar, darum misst `cmd/styrixa/rss_test.go` unter Linux (läuft in `make test` in der CI, Grenze 20 MB; eigener CI-Schritt zeigt den Wert). **Linux (CI, ubuntu-latest, amd64), alle 8 Module, nach 200 Anfragen: RSS 18,9 MB (18 936 KB) < 20 MB ✓**

**M1:** Installierbares Kern-CMS mit Login und Admin-Oberfläche, RAM < 20 MB.

## Phase 2 – Durchstich Factory → M2 (4 Sessions)

- [x] **2.1** factory-api: Release-Katalog (neuestes GitHub-Release lesen, `SHA256SUMS` + Signatur prüfen), Auswahl Konfiguration → Paket, Download-Endpunkt; `deploy.Artifact` aus dem Paket
  - *Abnahme:* manipuliertes Paket wird abgelehnt; Auswahl in < 1 s
  - *Erledigt 2026-11-25, Abweichungen/Entscheidungen:*
    - Neues Paket `factory-api/internal/catalog`. Abnahmetests: `TestTamperedPackageRejected` (Download → 502, Deploy-Artifact → `ErrTampered`, nichts im Cache; manipulierte Cache-Datei wird verworfen) und `TestSelectFast` (Auswahl über HTTP ≈ 60 µs, ohne GitHub-Zugriff). Dazu Tests für manipulierte/fehlende Signatur, fremden Schlüssel, manipulierte `modules.json` (alter Stand bleibt jeweils gültig), ungültige Auswahl und tar-Angriffe (Symlink, Pfade, Zusatzdateien, Größe).
    - **`modules.json` wird mitsigniert** (Rückfrage ohne Präferenz → Empfehlung): `release.yml` nimmt sie in `SHA256SUMS` auf. Der Katalog lehnt Releases ohne signierte `modules.json` ab – **die vorhandenen Releases `v0.0.1`/`v0.1.0` werden deshalb abgelehnt** (geprüft gegen echtes GitHub: Signatur ok, dann „modules.json nicht signiert“). Release **`v0.2.0`** mit signierter `modules.json` getaggt. Gegen echtes GitHub geprüft: Katalog lädt, `select` (media+shop, linux/amd64) in 0,8 ms, Shop ohne Media → 422. Erster Download 1,6 s (von GitHub, 6,1 MB), zweiter 13 ms (Cache). SHA-256 = Eintrag in `SHA256SUMS`.
    - `ReleasePublicKey` als Kopie in `catalog/release.go` (eigenes Go-Modul); `TestPublicKeyMatchesCMS` vergleicht mit `cms-template/internal/core/release.go`.
    - Endpunkte: `GET /api/catalog`, `GET /api/packages/select?modules=…&target=os/arch`, `GET /api/packages/{id}/download`; Deploy-Route `POST /api/builds/{id}/deploy` → **`POST /api/packages/{id}/deploy`** (Paket-ID `<key>_<os>_<arch>`, Fetch-URL im UI-Mockup angepasst). `dirStore`/`--builds` entfallen; neu `--repo`, `--cache`, optional `GITHUB_TOKEN`. `api.ArtifactStore.Artifact` bekommt einen `context`; der Deploy-Handler prüft die Eingaben jetzt vor dem Laden des Pakets.
    - Cache lazy: Ein Paket wird beim ersten Abruf von GitHub geladen (temp → Hash prüfen → rename nach `<cache>/<sha256>.tar.gz`). Bei jedem Abruf wird neu gehasht; das Binary wird für jeden Deploy frisch aus dem geprüften Archiv entpackt. Katalog im Speicher, TTL 10 min, Aktualisierung im Hintergrund, alter Stand gilt bis zum geprüften neuen. Ohne gültigen Katalog höchstens ein GitHub-Versuch pro 10 s.
    - Nicht umgesetzt: Schutz gegen Downgrade (ein älterer, gültig signierter Release als „latest“). Bei Bedarf mit 6.3 (Updates) klären.
    - `factory-api` loggt jetzt mit `slog`.
- [x] **2.2** `factory-ui` als Vite-Projekt, Module aus `modules.json`, „Build“ = Paket auswählen, echter Download
  - *Abnahme:* Download startet sofort, Paket entspricht der Auswahl
  - *Erledigt 2026-11-25, Abweichungen/Messwerte:*
    - Gegen echtes GitHub (Release `v0.2.0`, öffentliches Repo, kein Token nötig), `factory-api --ui ../factory-ui/dist`, Auswahl im UI:
      - `media` + `shop`, `linux/amd64` → `styrixa_media-shop_linux_amd64.tar.gz`, 5,8 MB; erster Abruf 1,26 s, zweiter 15 ms; SHA-256 `888daf5e…0ac6` = UI = `SHA256SUMS` ✓
      - nur Pflichtmodule, `darwin/arm64` → `styrixa_core_darwin_arm64.tar.gz`, 5,2 MB; erster Abruf 1,46 s, zweiter 10 ms; SHA-256 `d3fea1ef…24c8` = UI = `SHA256SUMS` ✓
      - Download-Prüfung per `curl` auf genau die `href` des Buttons (`<a download>`, gleicher Ursprung); UI ohne Konsolenfehler unter der CSP.
    - Vite 8, React 19, Tailwind 4, lucide-react 1. Dev-Proxy geprüft (`/api/catalog`, 422-Meldung kommt durch). Achtung: `vite` lauscht auf `localhost` (IPv6), nicht `127.0.0.1`.
    - Logik in `src/lib/` (`selection.js`, `api.js`, `sse.js`, `moduleMeta.js`), 18 Tests mit `node --test`. Abhängigkeiten werden transitiv aufgelöst. `npm test` = `node --test src/lib/*.test.js` (Shell-Glob statt Ordner, läuft so auf jeder Node-Version ≥ 20).
    - Entfernt: geschätzte Größen, simulierter Build, `go build`-Befehl, Schritt „Paket“ (Paketname/Region – vorgebaute Pakete haben keine Kundenkonfiguration; Region wählt der Installer). Deploy bleibt simuliert (`MOCK_DEPLOY`), nutzt aber schon Paket-ID und echte Größe; Deploy-Code nutzt bereits `lib/api.js` + `lib/sse.js`.
    - `factory-api`: neues Paket `internal/ui` (`http.FileServerFS(os.DirFS)`, keine Verzeichnislisten, `/api/*` nie aus dem UI-Ordner); `--ui` prüft beim Start, dass `index.html` existiert.
    - `go run ./factory-api/cmd/factory-api` aus dem Repo-Wurzelordner geht nicht (kein `go.work`) – aus `factory-api/` starten (`CLAUDE.md` angepasst).
- [x] **2.3** Echter Deploy aus dem UI (`MOCK_DEPLOY=false`); einmal auf echtem VPS ~~und echtem Shared-Webspace~~
  - *Entscheidung 26.11.2026:* Webspace-Abnahme für M2 **gestrichen** – easyname-Webhosting kann weder CGI noch eigene Binaries; kein CGI-Webspace verfügbar. FTPS-Deploy bleibt im Code, Abnahme wird mit einem CGI-fähigen Tarif nachgeholt.
  - *Abnahme:* CMS läuft auf beiden, `/install` erreichbar
  - *Stand 26.11.2026:* FTPS-Upload zu easyname läuft (Fixes: TLS-Handshake auf dem Datenkanal vor `150`, sauberes Schließen gegen „450 Transfer aborted“, Prüfung der Website-URL vor dem CGI-Test). Ergebnis: **easyname-Webhosting erlaubt kein CGI** → Webspace-Abnahme wandert nach 2.4.
  - *VPS-Abnahme 26.11.2026 ✓:* easyname Cloud Server XS Linux (1 vCore, 2 GB, Wien), Ubuntu 22.04, `linux/amd64`, **mit Plesk** (nginx vor Apache auf 80/443). Deploy per SSH als Plesk-Systembenutzer der Domain (`styrixa.at_<id>`, Shell `/bin/bash`, nicht chrooted) nach `/var/www/vhosts/styrixa.at/styrixa/` (außerhalb von `httpdocs`), App-Port 8081, Dienst über `systemd --user` (Linger). `/install` mit Einmal-Code abgeschlossen, `https://styrixa.at` mit Let's Encrypt. Zweites Deploy: neues Release unter `releases/` (2 Stück), kein neuer Install-Code, `/install` = 404, Dienst läuft weiter.
    - Plesk-Einbindung: Weiterleitung über `/var/www/vhosts/system/<domain>/conf/vhost_nginx.conf` mit `location ~ ^/ { proxy_pass http://127.0.0.1:<port>; … }` (Regex-Location, damit Plesks `location /` nicht kollidiert; ACME-Pfad bleibt frei), danach `plesk sbin httpdmng --reconfigure-domain <domain>`. Caddy und `ufw` **nicht** neben Plesk betreiben (Port-Konflikt bzw. sperrt Plesk-Ports).
    - Auffällig 1: Das easyname-Image hat **kein `libpam-systemd`** → `user@<uid>.service` startet nicht, `systemctl --user` scheitert, die Factory fällt still auf `nohup` zurück (kein Autostart). Lösung: `apt install libpam-systemd dbus-user-session`. → **Behoben:** Der SSH-Deploy prüft bei nicht erreichbarem `systemctl --user`, ob `pam_systemd.so` fehlt, und gibt dann `sudo apt install libpam-systemd dbus-user-session && sudo loginctl enable-linger <benutzer>` als Hinweis aus (Test `TestSystemdUserDiagnosis`, Integrationstest).
    - Auffällig 2: Die Factory handelt den **ECDSA**-Host-Key aus, `ssh-keygen -lf …ed25519…` in der Konsole zeigt aber ED25519 → Fingerprints passen scheinbar nicht. → **Behoben:** Client setzt `HostKeyAlgorithms` mit ED25519 zuerst (ohne `ssh-rsa`/SHA-1), der Dialog zeigt den Schlüsseltyp und den passenden `ssh-keygen -lf …`-Befehl (Test `TestSSHPrefersED25519HostKey`).
    - Auffällig 3: Nach einem Deploy leert das UI das Schlüsselfeld; ein zweites Deploy ohne erneutes Einfügen scheitert erst am Server (`attempted methods [none]`). → **Behoben:** `validateDeploy` lehnt leere bzw. nur aus Leerzeichen bestehende Passwörter und leere/unvollständige Schlüssel (ohne BEGIN/END-Zeile) mit deutschem Hinweis ab.
    - DNS-Falle: easyname legt beim Zuweisen der Domain zusätzlich A/AAAA-Einträge (`@`, `*`, `www`, `plesk`, `srv`) auf den alten Webhosting-Server an (`185.51.8.52` / `2a01:aee0:0:11::11`). Let's Encrypt prüft über IPv6 und scheitert, bis die AAAA-Einträge gelöscht sind. Hängende Plesk-LE-Bestellung („Order … is gone“) lässt sich durch Verschieben der Bestelldatei aus `/usr/local/psa/var/modules/letsencrypt/orders` lösen.
- [ ] **2.4** *(zurückgestellt 26.11.2026 – einziges Ziel easyname scheidet laut Probe aus; wieder aufnehmen, sobald ein Kunde einen PHP-only-Webspace **mit** ausführbaren Binaries hat)* PHP-Brücke für Webspaces ohne CGI (ADR-012), Ziel **easyname**: Hoster-Probe, `index.php`-Brücke, Hoster-Check wählt CGI → PHP-Brücke → Diagnose, Binary/Daten außerhalb des Document-Roots
  - *Abnahme:* CMS läuft auf easyname-Webhosting über die Brücke, `/install` abgeschlossen, `styrixa-data/*` und das Binary nicht abrufbar; Brücken-Test (PHP-Server) = Server-Modus in CI
  - *Hoster-Probe easyname 26.11.2026 (Webhosting, Test-Subdomain):* PHP 8.3 FPM, Linux x86_64, `proc_open` ja, Systembefehle (`/bin/echo`) ja, **eigenes Binary im Webspace: NEIN** (Start verweigert – vermutlich `noexec`-Mount), `sqlite_wal` ja, `open_basedir` = Subdomain-Ordner + phpsession/phptmp/`/tmp`/`/usr/bin`, `max_execution_time` 300 s. → **Tarif für die PHP-Brücke ungeeignet; angehalten, keine Umgehung.** Nebenbefund: Wegen `open_basedir` läge auch ein Geschwisterordner außerhalb des Document-Roots nicht im erlaubten Bereich. Probe-Fix: Ein fehlgeschlagener WAL-Test (`NEIN (…)`) hat zuvor fälschlich „möglich“ ergeben.

**M2:** Ende-zu-Ende: auswählen → deployen → installieren. ✅ 26.11.2026 auf VPS nachgewiesen (Release `v0.3.0`).

## Phase 3 – Pflichtmodule (5 Sessions)

- [x] 🟣 **3.1** Content: Datenmodell (Seiten, Revisionen, Entwürfe), Block-JSON im Editor.js-Format, Go-Renderer pro Blocktyp, Slugs
  - *Abnahme:* Renderer-Test je Blocktyp, XSS-Test (Text-Blöcke werden escaped)
  - *Erledigt 2026-11-26, Abweichungen/Entscheidungen:*
    - **Seitenhierarchie schon jetzt** (Nutzerentscheidung): `content_pages.parent_id` + `path` (voller Pfad, UNIQUE NOCASE, Wahrheit für die Auflösung), Tiefe ≤ 5, Pfad ≤ 255 Zeichen. Slug-/Elternwechsel verschiebt den ganzen Teilbaum in einer Transaktion; jeder alte Pfad landet in `content_redirects` (`from_path` → `page_id`, Ziel = aktueller Pfad, keine Ketten). Reservierte Slugs gelten nur auf oberster Ebene; Löschen mit Unterseiten → 400. Kanonisch: abweichende Groß-/Kleinschreibung, `/`-Endung und der eigene Pfad der Startseite → 301.
    - **Rollenspalte jetzt** (Nutzerentscheidung): Kern-Migration `004_roles.sql` (`core_users.role` = `admin` | `editor`, bestehende Konten = Admin), Rolle wird pro Anfrage mit der Session gelesen; `core.IsAdmin`/`CurrentRole`/`WithUser`. Raw-Blöcke und HTML-Modus nur für Admins (403, serverseitig). Navigation filtert nach `NavItem.Role`. **Offen für Phase 4 (Users-Modul):** Editoren anlegen und Routen anderer Module für Editoren sperren – bis dahin kann kein Editor-Konto entstehen.
    - Kern: `core.PageSource` (`PublicPage(ctx, path) (PublicPage, bool, error)`, 301 über `RedirectTo`), `PublicView` ergänzt um `Body`, `Description`, `Path`; neues Kern-Template `page.html` (Theme-überschreibbar), Layout setzt `<meta name="description">`.
    - Admin zusätzlich `GET /admin/content/pages/{id}` und `POST …/{id}/settings` (Slug, Elternseite, Startseite) – sonst wären Slug-Änderung und Hierarchie nicht bedienbar. `…/draft` nimmt JSON `{title, format, body}` (unbekannte Felder → 400) und antwortet JSON.
    - Sanitizer als eigener Tokenizer: Text wird erst entschlüsselt (Editor.js liefert `&amp;`, `&nbsp;`), dann escaped; erlaubte Tags kanonisch neu geschrieben. Bild-URLs ohne `//`; CTA-URL wird schon beim Speichern geprüft.
    - Tests: `make test` und CI-RSS-Schritt laufen mit `-tags "mod_content mod_dashboard"`; Binary-Tests (`public_test`, `jobs_test`) übernehmen die Tags des Testlaufs (`buildtags_*_test.go`). Prüfung der reservierten Slugs per Quelltext-Scan aller `HandleFunc`-Routen und `Path…`-Konstanten.
    - Revisionen-Aufräumen als Lazy-Job `content.prune_revisions` (täglich).
    - `TestLazyJobRunsInCGIMode` angepasst: Mit dem ersten Modul-Job läuft `core.cleanup` nicht mehr zwingend bei der ersten Anfrage (pro Anfrage höchstens ein Job) – der Test fragt jetzt bis zu einmal je registriertem Job an.
- [x] **3.2** Editor.js einbinden (ADR-003): vendort, Grundblöcke + FAQ/Answer-First/CTA, Speichern per htmx
  - *Abnahme:* Seite ohne Code bau- und sortierbar
  - *Erledigt 2026-11-26, Abweichungen/Entscheidungen:*
    - Vendort (npm pack, UMD byte-identisch, `internal/modules/content/VENDOR.md` mit SHA-256, Test `TestVendoredFilesMatchChecksums`): Editor.js 2.31.7 (Apache-2.0), Header 2.8.9, List 2.0.9, Quote 2.7.6, Delimiter 1.4.2, Raw 2.5.1, Marker 1.4.0, Inline-Code 1.5.2 (MIT). Kein `eval`/`new Function`.
    - **Bild als eigener Block** statt `@editorjs/image` (Nutzerentscheidung): Das offizielle Plugin ist auf Upload ausgelegt und hat kein Alt-Text-Feld. Eigener Block: URL, Alt-Text, Bildunterschrift; Vorschau nur für eigene Pfade (`/…`, Admin-CSP `img-src 'self'`).
    - Eigene Blöcke (`static/editor-blocks.js`, Vanilla-JS): Bild, FAQ, Antwort zuerst, Call-to-Action; Inline-Felder werden mit dem Editor.js-Sanitizer auf die Server-Allowlist bereinigt, URL-Felder zeigen Hinweise. **Liste:** Unterklasse blendet die Checkliste aus (Plugin bietet sie fest an, Server lehnt sie mit 400 ab), `maxLevel: 3`, nur Zahlen-Zählweise.
    - Inline-Werkzeuge: fett, kursiv, Link, Markierung, Code (Klassen-Attribute von Marker/Inline-Code entfernt der Server). Überschriften 2–4. Raw-Werkzeug nur für Admins geladen (Server prüft weiterhin selbst). Editor-UI auf Deutsch (i18n).
    - **CSP:** Alle Editor.js-Dateien fügen ihr CSS als `<style>` ein. Neu im Kern: `AdminPage.StyleNonce` → Nonce pro Anfrage, CSP ergänzt `style-src 'self' 'nonce-…'; img-src 'self' data:` (data: für SVG-Masken im Editor.js-CSS). Kein `unsafe-inline`/`unsafe-eval`, Skripte bleiben `'self'`. Editor.js bekommt die Nonce über `style.nonce`, die Plugins über `static/editor-nonce.js` (gibt `<style>` beim Einfügen in `<head>` die Nonce mit). Nonce als Base64url (html/template maskiert `+`).
    - Neu im Kern: `core.AdminAssets` (`Assets() fs.FS`) – Module bringen statische Admin-Dateien per `embed` mit; im Template `{{asset "content/…"}}`, URL `/admin/static/content-<datei>.<hash>.<ext>`. Isolationstest prüft, dass die Editor-Skripte ohne `mod_content` nicht im Binary sind.
    - **Speichern per `fetch` statt htmx-Swap:** Der Draft-Endpunkt aus 3.1 spricht JSON; die Rückmeldung (gespeichert/Fehlermeldung des Servers) steht in einer `role="status"`-Zeile. Ein htmx-Swap hätte einen zweiten, HTML-liefernden Endpunkt gebraucht. Speichern auch mit Strg/⌘+S. Änderungserkennung per Vergleich mit dem zuletzt gespeicherten Stand (Grundlage für Auto-Save in 3.3). „Veröffentlichen“ speichert ungespeicherte Änderungen vorher als Entwurf.
    - Sortieren über das Block-Menü (Nach oben/unten); Drag-and-Drop hat Editor.js 2.x nicht eingebaut (nur über Fremd-Plugin – nicht vendort).
    - Seiten im HTML-Modus zeigen statt des Block-Editors einen Hinweis (HTML-Editor folgt in 3.3), damit nichts überschrieben wird.
    - Tests: Editor-Seite (data-body escaped, Nonce = CSP, Raw nur für Admins, HTML-Modus), Rundlauf mit aufgezeichneter Editor.js-2.31-Ausgabe aller Blocktypen → validiert, veröffentlicht, öffentlich in Editor-Reihenfolge gerendert; Kern: Modul-Assets, Style-Nonce pro Anfrage. `make ui` prüft die eigenen Editor-Skripte mit `node --check`. Im Browser (lokal) durchgespielt: Blöcke anlegen, formatieren, verschieben, speichern, Fehler bei `javascript:`-CTA, veröffentlichen, öffentlich ansehen.
    - Nebenbei: Checkboxen im Admin nicht mehr auf 100 % Breite (`admin.css`).
- [x] **3.3** HTML-Modus (Textarea), Auto-Save alle 5 s, Vorschau, Revisionen wiederherstellen
  - *Abnahme:* kein Datenverlust bei Tab-Schließen; Revision wiederherstellbar
  - *Erledigt 2026-11-26, Abweichungen/Entscheidungen:*
    - **Auto-Saves zusammenfassen** (Nutzerentscheidung): Migration `002_autosave.sql` (`content_revisions.autosave`, `saved_at`). Ein Auto-Save überschreibt die neueste Revision, wenn sie ein Auto-Save desselben Autors ist, auf dem der Editor aufbaut, < 10 min alt und nicht veröffentlicht – sonst neue Revision. Manuelles Speichern, Veröffentlichen-vorher-Speichern, Wiederherstellen und Moduswechsel sind feste Stände. Sonst hätten Auto-Saves alle 5 s die 50 aufbewahrten Revisionen in Minuten verdrängt.
    - **Moduswechsel** (Nutzerentscheidung, nur Admin, 403 serverseitig): `POST …/{id}/format`. Blöcke → HTML legt das gerenderte HTML als neue Revision an; HTML → Blöcke beginnt nach Rückfrage leer, der HTML-Stand bleibt als Revision. Editoren sehen HTML-Seiten nur als Hinweis + Vorschau.
    - Auto-Save alle 5 s nur bei geändertem Stand (Vergleich, ohne Editor.js-`time`), nie parallel, einen vom Server abgelehnten Stand nicht erneut. Beim Verlassen des Tabs (`visibilitychange`) sofort sichern (`fetch keepalive` bis 60 KB), zusätzlich `beforeunload`-Warnung bei ungespeicherten Änderungen oder laufendem Speichern.
    - **Gleichzeitiges Bearbeiten:** `…/draft` nimmt zusätzlich `base_revision_id` und `autosave`; weicht die neueste Revision von der Basis ab, gewinnt trotzdem die neue, die Antwort enthält `notice` (wer, wann; „in einem anderen Fenster“ bei eigenem Konto), der Editor zeigt ihn dauerhaft an.
    - **Vorschau** `GET …/{id}/preview[?rev=N]` im öffentlichen Layout über neue Kern-Schnittstelle `core.PublicPreviewer` (`Deps.Public`, `Public.Preview`): `X-Robots-Tag: noindex, nofollow`, `no-store`, eigene CSP (Inline-Styles fürs Layout/Themes, Skripte nur `'self'` – Inline-Skripte aus Raw-HTML laufen auf dem Admin-Ursprung nicht; Bilder/Frames per https). Hinweisleiste „Vorschau – nicht öffentlich“. Vorher wird ungespeichert Geändertes gesichert; blockiert der Browser das Pop-up, öffnet sie im selben Tab.
    - **Revisionen:** Liste auf der Seite (Zeit, Autor, Titel, aktueller Entwurf/veröffentlicht/automatisch/HTML, Vorschau, „Wiederherstellen“ = `POST …/revisions/{rev}/restore` → neue Revision). Die Rolle prüft `Validate` wie beim Speichern (Editoren holen kein Raw/HTML zurück, 403). Fehlende/fremde Revision → 404. Formulare mit `data-save-first` sichern vorher, `data-confirm` fragt nach (auch „Seite löschen“ neu mit Rückfrage); Einstellungen-Formular sichert jetzt ebenfalls vorher.
    - Tests: `revisions_test.go` (Zusammenfassen inkl. Zeitfenster/Autor/veröffentlicht, Konflikthinweis, Vorschau inkl. Header/CSP/fremde Revision, Wiederherstellen inkl. Rollen, Moduswechsel), `TestEditorPage` für HTML-Modus erweitert. Im Browser (lokal) durchgespielt: Auto-Save und Zusammenfassen, Vorschau, HTML-Modus mit Auto-Save und `beforeunload`, Wiederherstellen.
- [x] **3.4** Dashboard: Seitenaufrufe serverseitig zählen (Tag × Pfad, keine Cookies, keine IPs), Status, Schnellzugriffe
  - *Abnahme:* kein Cookie-Banner nötig; keine personenbezogenen Daten gespeichert
  - *Erledigt 2026-11-26, Abweichungen/Entscheidungen:*
    - Neues Pflichtmodul `internal/modules/dashboard` (`cmd/styrixa/mod_dashboard.go`, `//go:build mod_dashboard`), Migration `001_hits.sql`: `dashboard_hits(day, path, count)` – nur diese drei Spalten (Test prüft das Schema), UPSERT je Aufruf. Tag = UTC-Kalendertag (keine Zeitzonendaten im Binary nötig; Anzeige weist „UTC“ aus). Query-Strings werden nie gespeichert.
    - **Kern-Hooks statt Modul-Importe:** `core.HitCounter` (`CountHit(ctx, core.Hit{Path, UserAgent})`) – der Kern meldet nur erfolgreich ausgelieferte öffentliche Seiten (Startseite + PageSource-Seiten, nur GET, kein Prefetch/Prerender); keine 404, keine 301, kein `/admin`, keine 500. Synchron in der Anfrage (ein UPSERT): `core.Defer` hätte nach jeder öffentlichen Seite die Lazy-Jobs verdrängt. `core.AdminHome`: Ein Modul registriert `GET /admin` selbst, der Kern behält seine schlichte Willkommensseite nur als Rückfall (Builds/Tests ohne `mod_dashboard`).
    - Bots: einfache UA-Liste (`bot`, `crawl`, `spider`, `curl`, `python`, `headless`, `lighthouse`, Link-Vorschauen u. a.) und leerer User-Agent zählen nicht. Der User-Agent wird nur geprüft, nie gespeichert.
    - Anzeige: Aufrufe 7/30 Tage, Verlauf der letzten 7 Tage als `<meter>` (CSP: keine Inline-Styles nötig), Top 10 Seiten (30 Tage), Status (Version, DB-Größe per `PRAGMA page_count × page_size`, letzter Lazy-Job über neues `core.LastJobRun`), Schnellzugriffe „Neue Seite anlegen“ / „Alle Seiten“. CSS als Modul-Asset `dashboard.css`.
    - **Version:** neues `core.Version` (Standard `dev`); `scripts/matrix-build.sh` setzt es per `-ldflags -X` aus `STYRIXA_VERSION`, `release.yml` übergibt den Git-Tag.
    - Aufräumen: Lazy-Job `dashboard.prune_hits` (täglich) löscht Tage älter als 400 Tage.
    - Tests: Bot-Erkennung, Zählung nur öffentlicher Seiten ohne `Set-Cookie`, Tageswechsel, Schema ohne Personenbezug, Grenzen (Pfadlänge, ungültiges UTF-8, Admin), Dashboard-Seite (7/30-Tage-Fenster, Top-Seiten, Status, Schnellzugriffe), Aufräumen; Kern: `TestPublicHitCounter` (welche Antworten zählen). Isolationstest um den Dashboard-Marker erweitert. Im Browser (lokal) geprüft: Zählung, Googlebot nicht gezählt, kein Cookie, Anzeige.
- [x] **3.5** Smoke-Test (ADR-010): Binary starten → `/install` → Login → Seite anlegen → öffentlich abrufen; in CI
  - *Abnahme:* Smoke-Test läuft in CI
  - *Erledigt 2026-11-27, Abweichungen/Entscheidungen:*
    - `cms-template/cmd/styrixa/smoke_test.go` (`//go:build smoke`): baut das Binary mit `CGO_ENABLED=0` und den Pflicht-Tags, startet es im Server-Modus auf einem freien Port mit eigenem Datenordner, wartet auf `/healthz`, prüft `/` → 303 `/install`, installiert mit dem Code aus `data/install-code` (Datei danach gelöscht), Login, CSRF-Token aus dem Admin-Layout, Seite „Über uns“ anlegen, Blöcke (Überschrift, Absatz mit `<b>` und eingeschleustem `<script>`, Liste) als Entwurf speichern, Entwurf öffentlich 404, veröffentlichen, `/ueber-uns` als Besucher ohne Session 200 mit Inhalt (Anker-ID, Inline-Allowlist, `<script>` escaped, kein `Set-Cookie`), zum Schluss `/install` = 404. Server-Log wird nur bei Fehlschlag ausgegeben.
    - Neues Ziel `make smoke` (auch Teil von `make check`), eigener CI-Schritt im `check`-Job (`ubuntu-latest` = linux/amd64, `timeout-minutes: 2`). Laufzeit lokal ≈ 3 s inkl. Build.

## Phase 4 – Optionale Module → M3 (7 Sessions)

- [x] **4.1** Media: Upload, Ordner, Alt-Texte; Verkleinerung/Umwandlung im Browser (ADR-001), `srcset`
  - *Abnahme:* siehe `docs/phasen/PHASE-4.md` (Varianten im Datenordner, `/media/…` mit `srcset`, ungültige Dateien → 400, Bildwähler im Editor)
  - *Erledigt 2026-11-27, Abweichungen/Entscheidungen:*
    - **Nutzerentscheidungen:** nur WebP/JPEG (kein SVG, kein PDF), Ordner virtuell in der DB (Dateien flach unter `<data>/media/<key>-<breite>.<webp|jpg>`, 128-Bit-Zufallsschlüssel), Bildwähler über Kern-Schnittstelle. Phasenanleitung `PHASE-4.md` vorher eigener Commit.
    - **Kern:** `Deps.DataDir`, `Deps.Modules` (Typ-Assertions auf Kern-Schnittstellen), `core.AdminExtensions` + Template-Funktion `extensions` (Erweiterungspunkt `core.ExtEditor`, liefert `ExtAsset{URL, Style}` – auch CSS, weil der Dialog ein Stylesheet braucht; unbekannte Assets → Fehler beim Start), `core.MediaReferrer` (Content implementiert es per `instr` über neueste und veröffentlichte Revision).
    - **Breiten:** 480/960/1920 von unten lückenlos, nur ≤ Original; Originale < 480 px als genau eine Variante. Ein 1200-px-Original wird also zu 480 + 960 (größere Zwischenbreiten werden nicht gespeichert, wie ADR-001). Server prüft Magic Bytes, Kopfmaße (JPEG über `image/jpeg.DecodeConfig`, WebP über eigenen VP8/VP8L/VP8X-Kopfparser), Format einheitlich, Seitenverhältnis ±1 px, ≤ 4 : 1, ≤ 4 MB je Variante, ≤ 12 MB je Anfrage; Multipart wird gestreamt. JPEG-Rückfall füllt Transparenz weiß.
    - **Auslieferung** `GET /media/{file}`: Name per Regex vor jedem DB-Zugriff, Typ aus der DB, `nosniff`, `CSP: default-src 'none'; sandbox`, `immutable`, `http.ServeContent` (Range/HEAD). Keine Admin-CSP-Lockerung nötig (kein `blob:`), Vorschau erst nach dem Upload über `/media/…`.
    - **Admin:** Upload per `fetch` (JSON, CSRF im Header), Ordner/Metadaten als normale Formulare mit Redirect statt htmx-Swap. Alt-Text Pflicht außer „dekorativ“ (Server prüft). Löschen nur Admins (403), Detailseite zeigt Verwendung und warnt. Geänderter Alt-Text wirkt nur auf neu eingefügte Bild-Blöcke (Blöcke speichern ihren eigenen Text).
    - **Content:** Bild-Block speichert optional `width`, `height`, `variants`; Validierung (≤ 3, nur eigene Pfade ohne Leerzeichen/Komma, Breiten eindeutig), Renderer setzt `srcset`, `sizes`, `width`/`height`, `decoding="async"` (alle Bilder). Wird die URL von Hand geändert, verwirft der Block die Varianten.
    - Tests: Kopfparser/Varianten tabellengetrieben, Upload/Auslieferung/Löschen/Ordner/API mit `httptest`, Content-Rundlauf mit `srcset`, XSS-Fälle für Varianten, `TestAdminExtensions`, Isolationstest mit Media-Markern; `make ui` prüft auch die Media-Skripte. Im Browser (lokal, Chrome) durchgespielt: Upload 2400 px → drei echte WebP-Varianten, Safari-Fall simuliert (Canvas liefert PNG → JPEG), Alt-Pflicht, Bildwähler im Editor, Auto-Save, Veröffentlichen, öffentliches `srcset`, Verwendungsanzeige. **Nicht** in echtem Safari geprüft.
- [ ] **4.2** SEO: Meta-Felder, Sitemap, robots.txt, 301-Redirects, **Google Search Console**
  - Search Console: Bestätigungscode im Admin eintragen (nur der Wert aus `<meta name="google-site-verification" content="…">`, Format streng geprüft) → Meta-Tag im `<head>` über `stx:head`; Hinweis auf die Alternative per DNS-TXT-Eintrag; Sitemap-URL zum Einreichen anzeigen. Setzt keine Cookies, lädt nichts von Google → keine Einwilligung nötig.
  - *Abnahme:* Meta-Tag erscheint nur mit hinterlegtem Code; ungültige Eingaben (HTML, Anführungszeichen) werden abgelehnt.
- [ ] **4.3** GEO: JSON-LD (Organization/LocalBusiness mit GISA, FAQ, Article), `llms.txt`, Answer-First-Hinweis im Editor
- [ ] **4.4** Users: RBAC (Admin, Redakteur, Autor, Kunde), 2FA per TOTP (RFC 6238, Standardbibliothek), Aktivitätsprotokoll
- [ ] **4.5** Design: Theme-Tokens → CSS-Variablen, Fonts lokal (DSGVO), Template-Auswahl
- [ ] **4.7** Google Analytics 4 mit Einwilligung (ADR-013), im SEO-Modul (`mod_seo`), **standardmäßig aus**
  - Mess-ID (`G-XXXXXXXXXX`, Format geprüft) im Admin. Erst dann erscheint ein **Einwilligungsbanner** (Opt-in, „Ablehnen“ gleichwertig zu „Akzeptieren“, keine vorausgewählten Häkchen, Website ohne Einwilligung voll nutzbar). `gtag.js` wird **erst nach Zustimmung** nachgeladen – vorher keine einzige Anfrage an Google (auch kein Consent-Mode-„advanced“-Ping).
  - Entscheidung speichern: First-Party-Cookie mit Version, 12 Monate; jederzeit widerrufbar über den Link „Cookie-Einstellungen“ im Footer (Pflicht-Hook im Theme); bei Widerruf GA-Cookies löschen. IP-Kürzung/Datenschutz-Einstellungen von GA4 als Hinweis im Admin.
  - CSP erlaubt Google-Domains nur, wenn GA aktiv ist. Textbaustein für die Datenschutzerklärung (Zweck, Empfänger Google, Drittlandübermittlung auf Basis des EU-US Data Privacy Framework, Speicherdauer, Widerruf) wird im Admin angezeigt.
  - Die eigene cookiefreie Zählung (3.4) bleibt unabhängig davon aktiv.
  - *Abnahme:* Test ohne Einwilligung = 0 Anfragen an Google-Domains und kein GA-Cookie; nach Zustimmung `gtag.js` geladen; nach Widerruf wieder 0; Banner gleichwertig bedienbar mit Tastatur.
- [ ] **4.6** Themes: `theme.json` v1, ZIP-Prüfung (Zip-Slip, Größen- und Dateianzahl-Limits, keine ausführbaren Dateien, keine Symlinks), Aktivierung, Rollback; Guard `mod_themes → mod_design`

**M3:** Alle 8 Module fertig, alle 36 Kombinationen gebaut und getestet.

## Phase 5 – Shop komplett nach österreichischem und EU-Recht (10 Sessions)

Anforderungskatalog mit Quellen: [`docs/shop/RECHTSRAHMEN-AT.md`](shop/RECHTSRAHMEN-AT.md) – die `R-…`-IDs sind die Abnahmekriterien. Rahmen (Entscheidung 25.11.2026): Verkauf an Verbraucher in **Österreich und der EU**, **Waren, Downloads und Dienstleistungen**, **altersbeschränkte Waren** möglich, gesetzliche Muster **vorbefüllt**. Voraussetzung: Phase 3 (Pflichtseiten sind Content-Seiten) und 4.1 Media.

- [ ] **5.1** Rechtsrahmen im Kern: Betreiberangaben (ECG/UGB/GewO/MedienG) in den Settings, erzeugtes Impressum, AStG-Angabe, Kleinunternehmer-/Kleinstunternehmen-Schalter, Pflichtseiten-Register mit **vorbefüllter Widerrufsbelehrung + Muster-Widerrufsformular**, Pflicht-Hooks im Theme (Impressum-Link, Widerrufsbutton), **Shop-Bereitschaftsprüfung** (Checkout gesperrt, solange etwas fehlt), Versionierung der Rechtstexte
  - *Abnahme:* R-IMP-1…5, R-INF-4, R-SYS-1…3; Test: kein OS-Plattform-Link irgendwo
- [ ] **5.2** Produkt- und Kategorie-Admin: Produkttypen Ware/Download/Dienstleistung, Varianten, Lager, Preisverlauf, Grundpreis, Lieferzeit (≤ 30 Tage), Altersgrenze 16/18, Herstellergarantie (Dauer, Marke, Modell), Speditionsware, private Download-Dateien im Datenordner
  - *Abnahme:* alle Felder mit Validierung; Download-Dateien nicht öffentlich abrufbar (Server- und CGI-Modus)
- [ ] **5.3** Katalog- und Produktseiten: Preis „inkl. USt.“ bzw. Kleinunternehmer-Hinweis, Versandkosten-Link, Grundpreis, 30-Tage-Tiefstpreis, Lieferzeit, **EU-Gewährleistungslabel** (zentral) und **Garantielabel** (je Produkt), Alters- und Widerrufs-Hinweise je Produkttyp, JSON-LD
  - *Abnahme:* R-PRE-1…4, R-LBL-1/2/4, R-INF-1/5/6
- [ ] 🟣 **5.4** Steuern EU/OSS: Steuersätze aller EU-Länder mit Gültigkeit (Startwerte, im Admin pflegbar), Satz nach Lieferland bei aktivem OSS, **einheitliche Bruttopreise** in allen Ländern (USt je Zielland herausgerechnet), Kleinunternehmerregelung, OSS-Export je Quartal (CSV)
  - *Abnahme:* R-STE-1/2; tabellengetriebene Tests AT/DE/IT × Waren/Lebensmittel × OSS an/aus × Kleinunternehmer
- [ ] 🟣 **5.5** Warenkorb und Checkout (FAGG § 8): Liefergebiete + Zahlungsarten zu Beginn, Rechnungsadresse EU-weit (Geoblocking), Zusammenfassung **unmittelbar** über dem Button **„zahlungspflichtig bestellen“**, nur aktive Checkboxen (AGB, Download-Zustimmung, Dienstleistungs-Verlangen, Altersbestätigung) mit Zeitstempel, Stripe/Vorkasse, Danke-Seite („Zahlung wird geprüft“), Lager bei Zahlung
  - *Abnahme:* R-BST-1…9, R-INF-8; Test: keine vorausgewählte Checkbox, Button-Text exakt
- [ ] **5.6** Bestellbestätigung auf dauerhaftem Datenträger: Mail mit Firmenangaben und Labels, PDF „Vertragsunterlagen“ (Bestellung, AGB-Version, Widerrufsbelehrung, Muster-Formular, abgegebene Zustimmungen) über den eigenen PDF-Schreiber; Downloads erst nach Zahlung über signierte, ablaufende Links
  - *Abnahme:* R-BEST-1…3, R-LBL-3, R-IMP-3; Mail-Versand CGI-tauglich
- [ ] **5.7** Widerrufsbutton (§ 13a FAGG): hervorgehobener Button **„Vertrag widerrufen“** ohne Login, Formular nur mit Name/Bestellnummer/E-Mail (+ optional Artikel, Grund), **„Widerruf bestätigen“**, **sofortige** neutrale Eingangsbestätigung mit Datum und Uhrzeit, Admin-Posteingang mit Status und Mitteilung an den Kunden
  - *Abnahme:* R-WID-1…7; Handler-Tests inkl. Gastbestellung und Teilwiderruf
- [ ] **5.8** Rechnungen und Gutschriften: Pflichtangaben UStG, fortlaufende Nummern, Satz je Zielland (OSS), Kleinunternehmer-Hinweis, Gutschrift bei Erstattung, Erstattung per Klick (Stripe), offener Punkt aus 1.1 (Zahlung auf stornierte Bestellung)
  - *Abnahme:* R-RE-1; Rechnung korrekt je Steuersatz und Land
- [ ] **5.9** Kundendaten und DSGVO: Gastbestellung als Standard, Kundenkonto optional mit `mod_users`, Auskunft (JSON-Export), Anonymisierung unter Wahrung der 7-Jahres-Aufbewahrung, Textbausteine für die Datenschutzerklärung (Stripe, Versand)
  - *Abnahme:* R-DSG-1…3, R-BAR-1
- [ ] **5.10** Compliance-Abnahme: Testbestellungen je Produkttyp (Ware AT, Ware DE mit OSS, Download, Dienstleistung, Wein 18+) im Stripe-Testmodus inkl. EPS/PayPal, Widerruf, Erstattung, Gutschrift; automatischer Prüftest aller `R-…`; Betreiber-Checkliste aus dem Katalog in die Kunden-Doku übernehmen
  - *Abnahme:* alle `R-…` grün oder begründet als Betreiberpflicht markiert

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
| Shared-Hoster ohne CGI/.htaccess | Automatischer Hoster-Check vor jeder Änderung; ohne CGI automatisch PHP-Brücke (easyname), sonst klare Diagnose – ADR-008/012 | ✅ CGI umgesetzt, PHP-Brücke in 2.4 |
| Abhängigkeiten nicht reines Go | `make cgo` + CI-Build aller Kombinationen mit `CGO_ENABLED=0` – ADR-009 | ✅ umgesetzt + getestet |
| Rechtliches im Shop | Anforderungskatalog `docs/shop/RECHTSRAHMEN-AT.md` (FAGG inkl. Widerrufsbutton, EU-Labels, PrAG, ECG/UGB/MedienG, Geoblocking, OSS, BaFG, DSGVO), Shop-Bereitschaftsprüfung sperrt unvollständige Shops, vorbefüllte gesetzliche Muster, Betreiber-Checkliste – ADR-007 | ✅ entschieden |
| Build-Server (Kosten, Sicherheit, Aufwand) | Alle Kombinationen per GitHub Actions vorgebaut und signiert – ADR-004 | ✅ umgesetzt |
| Archivierte PDF-Bibliothek | Eigener minimaler PDF-Schreiber – ADR-007 | ✅ entschieden |
| Browser-Test-Pflege | Ein Smoke-Test statt Playwright-Suite – ADR-010 | ✅ entschieden |

**Bleibt:** Rechtliche Verantwortung für Shop-Inhalte, Steuersätze und Rechtstexte liegt beim Betreiber (wie bei jedem Shop-System) – wird im Produkt klar ausgewiesen. Geschäftsmodell der Factory (ADR-011) ist deine Entscheidung.

**Gestrichen gegenüber Version 1:** Bild-Prototyp (0.3), Build-Worker/Sandbox/Cache (2.1–2.2), eigener Block-Editor + CodeMirror (3.2–3.3 vereinfacht), Playwright-Suite (3.5–3.6), zweite Media- und Users-Session, PayPal-Integration (5.5), Monitoring Build-Worker (6.5), Lasttests (7.2).
