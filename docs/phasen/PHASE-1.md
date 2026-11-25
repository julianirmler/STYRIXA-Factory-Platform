# Phase 1 – CMS-Kern → M1 (Sessions 1.1–1.6)

Diese Datei wird über `CLAUDE.md` geladen, solange Phase 1 aktiv ist. Sie ergänzt die Projektregeln, ersetzt sie nicht.

**Ziel (M1):** Installierbares Kern-CMS mit Login und Admin-Oberfläche, im Server- und CGI-Modus, RAM < 20 MB.
**Umfang:** 6 Sessions, jede einzeln mit `/clear` dazwischen. 🟣 = Opus (1.1, 1.3), sonst Sonnet.

## Ausgangslage (nach Phase 0)

Vorhanden – benutzen, nicht neu bauen:
- `internal/core/module.go`: Registry, `Module`-Interface (`Migrations() fs.FS`, `Mount`, `AdminNav`), `Deps{DB *sql.DB}` – `DB` ist noch immer `nil`
- `internal/core/release.go`: `ReleasePublicKey`, `VerifyRelease`, `ChecksumFor` (für 6.3)
- `cmd/styrixa/main.go`: Server-/CGI-Erkennung, `/healthz`, `--addr`, `STYRIXA_DATA` (Datenordner, 0700)
- Shop: Migrationen `001_shop.sql`, `002_payments.sql`, `SQLPaymentStore` (nutzt `BeginTx`), Stripe noch aus Umgebungsvariablen (`configureProviders`)
- `make check`, CI und Release-Workflow laufen grün; Release `v0.0.1` existiert

`cms-template/go.mod` hat noch **keine** Abhängigkeiten. Phase 1 bringt die beiden erlaubten hinzu (ADR-009): `modernc.org/sqlite` (1.1) und `golang.org/x/crypto` für bcrypt (1.2). Jeweils `go get …` + `go mod tidy` mit Standard-`GOPROXY`/`GOSUMDB`, danach `go mod verify` und `make cgo` – gleiche Regeln wie für `factory-api/go.sum` in Phase 0.

## Phasenweite Regeln (zusätzlich zu CLAUDE.md)

- **Kern-Tabellen** heißen `core_…` und liegen in `internal/core/migrations/` – der Kern migriert sich selbst wie ein Modul mit der ID `core`, immer zuerst.
- **Kein Zustand im Prozess.** Sessions, Rate-Limits, Job-Sperren, CSRF-Tokens: alles in SQLite. Was im Server-Modus funktioniert, muss im CGI-Modus (ein Prozess pro Anfrage) identisch funktionieren.
- **Geheimnisse** (Passwort-Hashes, Stripe-Schlüssel, SMTP-Passwort, Reset-Tokens, Cron-Token) nie loggen, nie vollständig im Admin anzeigen (maskieren, z. B. `sk_live_…4f2a`). Reset- und Session-Tokens nur als SHA-256 speichern.
- **Fehlermeldungen für Kunden auf Deutsch**, Logs dürfen englisch sein. Nach außen keine internen Details (Stacktraces, SQL).
- **Logging:** `log/slog` (Standardbibliothek), Text-Handler auf stderr. Im CGI-Modus landet stderr im Apache-Error-Log – deshalb sparsam.
- Keine Arbeit an späteren Phasen (kein Content-Datenmodell, keine Benutzerrollen außer „Admin“ – RBAC ist 4.4).

---

## Session 1.1 🟣 – SQLite, Migrationen, `Deps.DB`

**Abnahme:** Migrationen idempotent; zwei Prozesse parallel (CGI-Szenario) ohne „database is locked“; Integrationstest für `shop.SQLPaymentStore`.

1. `go get modernc.org/sqlite@latest && go mod tidy && go mod verify` in `cms-template`; ADR-009-Liste stimmt bereits.
2. Neue Datei `internal/core/db.go`:
   - `func OpenDB(dataDir string) (*sql.DB, error)` öffnet `<dataDir>/styrixa.db` mit Treiber `sqlite` und DSN-Pragmas:
     `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate`
   - `_txlock=immediate` ist wichtig: Schreib-Transaktionen nehmen die Sperre sofort; sonst gibt es bei parallelen CGI-Prozessen `SQLITE_BUSY` beim Upgrade von Lese- auf Schreibsperre, das `busy_timeout` **nicht** abfängt.
   - `db.SetMaxOpenConns(1)` im CGI-Modus reicht; im Server-Modus mehrere Leser zulassen, aber Pragmas müssen für jede Verbindung gelten (deshalb im DSN, nicht per `Exec`).
   - Datei mit Rechten 0600 anlegen.
3. Neue Datei `internal/core/migrate.go`:
   - Tabelle `schema_migrations(module TEXT, version INTEGER, applied_at TEXT, PRIMARY KEY(module, version))`
   - `func Migrate(ctx, db, mods []Module) error`: zuerst `core`, dann Module in der Reihenfolge von `Modules()`. Dateien `NNN_name.sql` lexikografisch; Version = führende Zahl. Jede Datei in **einer** Transaktion zusammen mit ihrem `schema_migrations`-Eintrag.
   - Bereits angewendete Versionen überspringen. Fehlt eine Datei, deren Version schon in der DB steht, oder ist eine ältere neu hinzugekommen: Fehler (Migrationen werden nie geändert, nur angehängt).
   - Gleichzeitiger Start zweier Prozesse: Die Transaktion (`immediate`) serialisiert; nach Sperrerhalt erneut prüfen, ob die Version inzwischen angewendet wurde.
4. `main.go`: `OpenDB` → `Migrate` → `core.Deps{DB: db}`. Migrationen laufen im CGI-Modus bei jeder Anfrage – deshalb muss „nichts zu tun“ schnell sein (eine Abfrage auf `schema_migrations`).
5. Tests (`internal/core/db_test.go`, `migrate_test.go`, tabellengetrieben, `t.TempDir()`):
   - zweimal `Migrate` → zweiter Lauf ändert nichts
   - geänderte/fehlende/eingeschobene Migration → Fehler
   - Parallelität: **zwei echte Prozesse** (Testbinary per `os.Args[0]` mit Umgebungsvariable als Kind starten, wie das Muster `TestHelperProcess`), die gleichzeitig migrieren und je 200 Schreib-Transaktionen machen → kein Fehler, alle Zeilen da
   - `shop.SQLPaymentStore` gegen echte SQLite mit beiden Shop-Migrationen: gültige Zahlung, doppeltes Ereignis (Idempotenz), unbekannte Bestellung, Betragsabweichung → `OutcomeFlagged`
6. `/healthz` bleibt in dieser Session unverändert (DB-Check kommt in 1.6).

**Stolperstein:** WAL braucht ein lokales Dateisystem. Manche Shared-Hoster legen Webspace auf NFS – dann schlägt WAL fehl oder ist unsicher. `OpenDB` soll dann mit klarer Meldung abbrechen (nicht stillschweigend auf `DELETE`-Journal fallen). In 2.3 auf echtem Webspace prüfen und ggf. ADR ergänzen.

## Session 1.2 – Settings-Store, `/install`, Stripe aus Settings

**Abnahme:** zweiter Aufruf von `/install` → 404; Handler-Test des Wizards.

1. `go get golang.org/x/crypto@latest && go mod tidy && go mod verify` (für `bcrypt`).
2. Migration `core/001_core.sql`: `core_settings(key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT)`, `core_users(id, email UNIQUE, password_hash, created_at)`.
3. `internal/core/settings.go`: `Get/Set/GetAll` mit Schlüsseln als Konstanten (`site.title`, `site.region`, `site.gisa`, `shop.stripe.secret_key`, …). Kein Cache über die Anfrage hinaus.
4. Wizard `GET/POST /install` (`html/template`, Deutsch): Seitentitel, Region (de-AT Standard, de-DE, de-CH), GISA-Nummer (optional, nur bei de-AT angezeigt, Format prüfen: Ziffern), Admin-E-Mail + Passwort (min. 12 Zeichen, `bcrypt` Kosten 12).
   - Ein einziger Schreibvorgang in einer Transaktion; Abschluss setzt `core.installed_at`.
   - Ist `core.installed_at` gesetzt → `/install` antwortet **404** (nicht 403 – nichts verraten). Prüfung in derselben Transaktion wie das Anlegen, damit zwei gleichzeitige Installationen nicht beide durchgehen.
   - Solange nicht installiert: jede andere Seite außer `/healthz` leitet auf `/install` um.
5. Stripe: `configureProviders` liest Schlüssel aus den Settings statt aus `os.Getenv`. Kommunikation Shop → Kern nur über `core` (Settings-Interface in `Deps`), kein Import zwischen Modulen. Umgebungsvariablen entfallen (in der Doku vermerken).
6. Tests: Handler-Test mit `httptest` (Formular → angelegt → zweiter Aufruf 404), ungültige Eingaben (kurzes Passwort, ungültige Region), gleichzeitige Installation.

**Offen – vorher fragen:** Wer die Installation starten darf. Ohne Schutz kann jeder, der die frische URL zuerst aufruft, Admin werden. Günstigste Variante: Einmal-Code, den der Deploy (SSH/FTP) in den Datenordner schreibt und die Factory dem Kunden anzeigt. Das betrifft `factory-api/internal/deploy` → **Nutzer fragen**, bevor umgesetzt wird.

## Session 1.3 🟣 – Login, Sessions, CSRF, Rate-Limit, Passwort-Reset

**Abnahme:** Tests gegen Session-Fixation, CSRF, Brute-Force.

1. Migration `core/002_auth.sql`: `core_sessions(token_hash PK, user_id, created_at, last_seen, expires_at, csrf)`, `core_login_attempts(key, at)`, `core_password_resets(token_hash PK, user_id, expires_at, used_at)`.
2. Sessions: 32 Byte aus `crypto/rand`, Cookie `styrixa_session` mit `HttpOnly`, `Secure` (außer bei `127.0.0.1`/`localhost` für Tests), `SameSite=Lax`, `Path=/`. In der DB nur SHA-256 des Tokens. Ablauf: 12 h Inaktivität, 30 Tage absolut.
   - **Bei Login immer neues Token** (Fixation), bei Logout Zeile löschen, bei Passwortänderung alle Sessions des Benutzers löschen.
3. CSRF: zwei Schichten, beide Standardbibliothek:
   - `http.CrossOriginProtection` (Go ≥ 1.25) um alle Nicht-GET-Routen
   - pro Session ein Token (`core_sessions.csrf`), in Formularen als verstecktes Feld, für htmx als `hx-headers`; Vergleich mit `subtle.ConstantTimeCompare`
4. Rate-Limit in der DB: max. 5 Fehlversuche pro E-Mail und 20 pro IP in 15 min → 429 mit deutscher Meldung. Gleiche Antwortzeit und gleiche Meldung für „unbekannte E-Mail“ und „falsches Passwort“ (bcrypt auch bei unbekannter E-Mail gegen einen Dummy-Hash ausführen). Alte Einträge beim Schreiben mit löschen (kein Scheduler).
5. Passwort-Reset: `net/smtp` mit STARTTLS, Zugangsdaten aus Settings. Token 32 Byte, 30 min gültig, einmalig. Antwort auf „Reset anfordern“ immer gleich, egal ob die E-Mail existiert.
6. Auth-Middleware des Kerns umschließt `/admin/*` (Module mounten weiterhin unter `/admin/<modul>/…`).
7. Tests: Fixation (Token vor/nach Login verschieden), fehlendes/falsches CSRF-Token → 403, fremder `Origin` → 403, 6. Fehlversuch → 429, abgelaufenes/benutztes Reset-Token, Session nach Passwortänderung ungültig. SMTP mit einem In-Test-Server (`net.Listen` + minimaler SMTP-Dialog), keine echte Mail.

## Session 1.4 – Admin-Shell (html/template + htmx)

**Abnahme:** Template eines Moduls nur im Binary, wenn sein Build-Tag gesetzt ist (Test).

1. htmx vendoren: eine Datei `internal/core/admin/static/htmx.min.js` (Version und SHA-256 in einem Kommentar am Dateianfang bzw. in `internal/core/admin/VENDOR.md`), Lizenz 0BSD. Per `embed` ausliefern unter `/admin/static/…` mit `Cache-Control: public, max-age=31536000, immutable` und Versionsnummer im Dateinamen.
2. Layout im Kern: Navigation aus allen `AdminNav()` (nach `Order` sortiert), Kopfzeile mit Seitentitel aus Settings, Logout. Eine kleine handgeschriebene CSS-Datei (ADR-002), keine Icons-Bibliothek nötig (Icon-Name bleibt Feld für später).
3. Modul-Templates: optionales Interface im Kern, z. B. `type AdminTemplates interface { Templates() fs.FS }`. Module betten ihre Templates per `embed` im eigenen Paket ein. Der Kern parst sie unter dem Präfix `<modul-id>/…`.
4. Shop: die `todo(...)`-Handler für Admin-Routen rendern eine Platzhalterseite im Layout (keine Shop-Funktionen – das ist Phase 5).
5. Test der Build-Tag-Isolierung in `scripts/` oder als Go-Test mit `exec.Command("go", "build", …)`: Binary ohne `mod_shop` bauen → enthält den Marker-String aus einem Shop-Template **nicht**; mit `mod_media mod_shop` → enthält ihn. In `make check` einhängen.

## Session 1.5 – Öffentliches Rendering

**Abnahme:** Startseite im Server- und CGI-Modus identisch.

1. Öffentlicher Router im Kern: Startseite, 404 und 500 als Templates mit Theme-Fallback (Kern-Standard-Template, solange kein Design-/Themes-Modul eins liefert – Schnittstelle in `core`, Umsetzung in 4.5/4.6).
2. Caching: `ETag` (SHA-256 des Antwortkörpers, gekürzt) + `If-None-Match` → 304; `Cache-Control: no-cache` für HTML (immer revalidieren), nie für `/admin/*` (`no-store`).
3. Sicherheits-Header für alle Antworten: `X-Content-Type-Options: nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `Content-Security-Policy` für den Admin (nur `'self'`).
4. Test: Startseite einmal über `httptest.NewServer(mux)` und einmal über `net/http/cgi.Handler` (startet das gebaute Binary wirklich als CGI) abrufen → Status, relevante Header und Körper identisch. 500-Seite verrät keine Details.

## Session 1.6 – Lazy-Jobs, Cron-Endpunkt, Logging, `/healthz`

**Abnahme:** Job läuft im CGI-Modus ohne Hintergrundprozess.

1. Migration `core/003_jobs.sql`: `core_jobs(name PK, interval_s, last_run, locked_until, last_error)`.
2. `core.RegisterJob(name, interval, func(ctx, Deps) error)`; Module registrieren Jobs in `init()`.
3. Lazy-Ausführung: höchstens **ein** fälliger Job pro Anfrage, Sperre per `UPDATE core_jobs SET locked_until=… WHERE name=? AND (locked_until IS NULL OR locked_until < now)` (betroffene Zeilen = 1 → ausführen). Zeitbudget (z. B. 2 s, `context.WithTimeout`). Im CGI-Modus **nach** dem Schreiben der Antwort, aber vor Prozessende; keine Goroutine, die die Anfrage überlebt.
4. Cron-Endpunkt nach ADR-005 (`/cron?token=…`), Token in Settings, Vergleich in konstanter Zeit, führt alle fälligen Jobs aus.
   **Vorher fragen:** Ein Token in der URL landet in Access-Logs von Hoster und Proxy. Günstige Alternative: Header `X-Styrixa-Cron-Token` (geht mit `curl -H` in jedem Hoster-Cron). Abweichung von ADR-005 → Nutzer entscheidet.
5. `/healthz` prüft zusätzlich `SELECT 1` und dass alle Migrationen angewendet sind; Antwort bleibt `ok` (Hoster-Check der Factory erwartet genau das) bzw. 503 ohne Details.
6. Tests: Job läuft über `cgi.Handler`-Aufruf, zwei parallele Prozesse → Job genau einmal ausgeführt, Fehler landet in `last_error`, Cron mit falschem Token → 404.
7. **M1 messen:** Binary mit allen Modulen bauen, starten, Startseite und Admin je 100× abrufen, RSS ablesen (`ps -o rss= -p <pid>`) – muss < 20 MB sein. Wert im Umsetzungsplan notieren.

---

## Abnahme Phase 1 / M1 (alles muss erfüllt sein)

- [ ] 1.1 Migrationen idempotent, Zwei-Prozess-Test grün, `SQLPaymentStore`-Integrationstest
- [x] 1.2 `/install` funktioniert, zweiter Aufruf 404, Stripe-Schlüssel aus Settings
- [x] 1.3 Tests gegen Fixation, CSRF, Brute-Force grün
- [x] 1.4 Build-Tag-Isolierung der Admin-Templates per Test belegt, in `make check`
- [x] 1.5 Startseite Server = CGI (Test)
- [ ] 1.6 Job im CGI-Modus, `/healthz` mit DB-Check, RAM < 20 MB gemessen
- [ ] `make check` und CI grün, `go.sum` von `cms-template` ohne Prüf-Umgehung erzeugt

## Abschluss jeder Session

1. Session in `docs/UMSETZUNGSPLAN.md` abhaken, Abweichungen notieren.
2. Committen (eine Session = ein oder wenige Commits), pushen, CI grün.
3. Nach Session 1.6: in `CLAUDE.md` „Aktuelle Phase“ auf `@docs/phasen/PHASE-2.md` umstellen (Datei existiert ggf. noch nicht – Nutzer informieren). Optional Release `v0.1.0` taggen (M1).

## Bekannte Stolpersteine

| Problem | Lösung |
|---|---|
| `database is locked` trotz `busy_timeout` | `_txlock=immediate` fehlt oder Pragmas per `Exec` statt im DSN gesetzt (gelten dann nur für eine Verbindung) |
| WAL-Fehler auf Shared-Webspace | Netzwerk-Dateisystem; Datenordner muss lokal sein → klare Fehlermeldung, siehe 1.1 |
| `make cgo` schlägt nach `go get` fehl | falsches SQLite-Paket (`mattn/go-sqlite3` braucht CGO) – nur `modernc.org/sqlite` |
| Test über `cgi.Handler` findet kein Binary | im Test zuerst mit `go build -o $(t.TempDir())/styrixa` bauen |
| Secure-Cookie fehlt im Browser lokal | `Secure` nur für `localhost`/`127.0.0.1` weglassen, nie generell |
| `http.CrossOriginProtection` blockiert htmx | htmx sendet `Origin` korrekt; Problem ist meist ein Proxy, der `Host` umschreibt |
