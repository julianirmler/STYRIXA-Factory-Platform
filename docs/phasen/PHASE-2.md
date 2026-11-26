# Phase 2 – Durchstich Factory → M2 (Sessions 2.1–2.3)

Diese Datei wird über `CLAUDE.md` geladen, solange Phase 2 aktiv ist. Sie ergänzt die Projektregeln, ersetzt sie nicht.

**Ziel (M2):** Ende-zu-Ende ohne Simulation: Module im UI auswählen → passendes signiertes Paket herunterladen **oder** per SSH/FTPS deployen → `/install` im Browser abschließen. Einmal nachgewiesen auf einem echten VPS und einem echten Shared-Webspace.
**Umfang:** 2.1 und 2.2 erledigt, offen 2.3 – jede einzeln mit `/clear` dazwischen. **Modell: Sonnet** (kein Opus nötig).

## Ausgangslage (nach Session 2.1)

Vorhanden – benutzen, nicht neu bauen:
- `factory-api/internal/catalog`: neuestes GitHub-Release, `SHA256SUMS` + ed25519-Signatur, signierte `modules.json`, lazy Cache mit Hash-Prüfung bei jedem Abruf
- Endpunkte: `GET /api/catalog` → `{release, modules, targets}` · `GET /api/packages/select?modules=media,shop&target=linux/amd64` → `Package` + `downloadUrl` · `GET /api/packages/{id}/download` · `POST /api/packages/{id}/deploy` (SSE: `log`, `identity`, `result`, `error`)
- `Package` = `{id, file, sha256, size, release, modules, goos, goarch}`; Paket-ID `<key>_<goos>_<goarch>`
- `factory-api/internal/deploy`: SSH + FTPS inkl. Hoster-Check, Rollback, Fingerprint-Pinning, SSRF-Schutz, Einmal-Code (`Result.InstallCode`)
- CMS (M1): `/install` mit Einmal-Code aus `<Datenordner>/install-code`, `/healthz` mit DB-Check, CGI-Modus
- `factory-ui/src/StyrixaFactoryConfigurator.jsx`: Mockup mit fest eingebauten Modulen, simuliertem Build und `MOCK_DEPLOY = true`; noch **kein** Vite-Projekt

**Offen aus 2.1 (zuerst erledigen, siehe 2.2 Schritt 0):** Commit `Session 2.1 …` ist evtl. noch nicht gepusht; die Releases `v0.0.1`/`v0.1.0` werden vom Katalog abgelehnt (keine signierte `modules.json`) → neuer Release nötig.

## Phasenweite Regeln (zusätzlich zu CLAUDE.md)

- **factory-api läuft in Phase 2 nur lokal** (`127.0.0.1`). Ohne Kunden-Login (kommt in 6.1) nie öffentlich erreichbar machen – jeder könnte sonst Deployments auslösen.
- **Zugangsdaten tippt ausschließlich der Nutzer in das lokale Factory-UI im Browser.** Claude Code fragt nie nach Passwörtern, Schlüsseln oder Install-Codes im Chat, schreibt sie nie in Dateien, Umgebungsvariablen, Shell-Befehle oder Logs. Fingerprints vergleicht der Nutzer selbst (Hoster-Panel bzw. `ssh-keyscan` auf einem vertrauenswürdigen Weg).
- **Eine Quelle der Wahrheit:** Welche Module, Abhängigkeiten und Plattformen es gibt, kommt aus `GET /api/catalog` (= signierte `modules.json`). Das UI kennt nur Darstellung (Icon, Beschreibung, Stichworte) pro Modul-ID – mit Fallback für unbekannte IDs.
- **Gleicher Ursprung statt CORS:** Im Betrieb liefert `factory-api` das gebaute UI selbst aus (`--ui`), in der Entwicklung leitet der Vite-Proxy `/api` weiter. Kein CORS-Header in der API.
- **UI-Abhängigkeiten (abschließend):** `react`, `react-dom`, `lucide-react`; Entwicklung: `vite`, `@vitejs/plugin-react`, `tailwindcss`, `@tailwindcss/vite`. Sonst nichts (kein Router, kein State-Manager, kein HTTP-Client, keine UI-Bibliothek). `package-lock.json` wird committet, CI nutzt `npm ci`. Keine externen Schriften/CDNs (DSGVO).
- **Tests ohne Browser (ADR-010):** UI-Logik als reines JS in `src/lib/` mit `node --test` (eingebaut, keine Abhängigkeit). Kein Vitest, kein Playwright.
- Keine Arbeit an späteren Phasen (kein Kunden-Login, keine gespeicherten Deploy-Ziele, kein Selbst-Update).

---

## Session 2.1 – Release-Katalog ✅ erledigt

Siehe `docs/UMSETZUNGSPLAN.md`. Offener Punkt (Downgrade-Schutz) bleibt für 6.3.

## Session 2.2 – `factory-ui` als Vite-Projekt, echte Auswahl, echter Download ✅ erledigt

**Abnahme:** Mit lokaler `factory-api` gegen echtes GitHub: Auswahl im UI → Download startet sofort (erster Abruf eines noch nicht gecachten Pakets ≤ 5 s, danach sofort); heruntergeladene Datei = gewähltes Paket (Dateiname/ID passt zur Auswahl, SHA-256 = Wert aus `SHA256SUMS`).

### 0. Offenes aus 2.1
```bash
git status && git log --oneline origin/main..main   # ungepushte Commits?
git push origin main && gh run watch                 # CI grün
git tag v0.2.0 && git push origin v0.2.0 && gh run watch   # Release mit signierter modules.json
go run ./factory-api/cmd/factory-api &               # lokal, Standard 127.0.0.1:8090
curl -fsS 127.0.0.1:8090/api/catalog | head -c 400   # release = v0.2.0
curl -fsS '127.0.0.1:8090/api/packages/select?modules=media,shop&target=linux/amd64'
```
Ist das Repo privat: `GITHUB_TOKEN` (fein granular, nur „Contents: read“ auf dieses Repo) setzen und prüfen, dass auch `download` funktioniert – Release-Assets privater Repos brauchen den API-Asset-Weg. Den Token nicht anzeigen oder committen; der Nutzer setzt ihn selbst in seiner Shell.

### 1. Vite-Projekt anlegen
In `factory-ui/`:
- `package.json` (`"type": "module"`, `"private": true`, Skripte `dev`, `build`, `preview`, `test` = `node --test src/lib/`), `"engines": {"node": ">=20"}`
- `vite.config.js`: Plugins `react()` und `tailwindcss()`; `server.proxy` `/api` → `http://127.0.0.1:8090` (SSE läuft durch den Vite-Proxy)
- `index.html` (`lang="de"`, Titel „STYRIXA Factory“), `src/main.jsx`, `src/index.css` mit `@import "tailwindcss";`
- `.gitignore`: `node_modules/`, `dist/`
- `npm install` der Pakete aus den Phasenregeln → `package-lock.json` committen

### 2. Logik aus der Komponente lösen
- `src/lib/selection.js` (reine Funktionen): Pflichtmodule immer aktiv; `toggle(selected, id, modules)` aktiviert fehlende Abhängigkeiten bzw. deaktiviert abhängige Module und liefert einen deutschen Hinweistext; `queryFor(selected, target)` → sortierte Query für `/api/packages/select`
- `src/lib/api.js`: `getCatalog()`, `selectPackage(mods, target, signal)`, `streamDeploy(id, body, handlers)`; Fehlertexte der API (deutsch, `text/plain`) direkt anzeigen
- `src/lib/sse.js`: SSE-Parser für `fetch`-Streams, robust gegen Ereignisse über Chunk-Grenzen und `\r\n`
- `src/lib/moduleMeta.js`: Icon/Beschreibung/Stichworte je Modul-ID (aus dem Mockup übernommen), Fallback für unbekannte IDs
- Tests `src/lib/*.test.js` mit `node:test` + `node:assert`: Pflichtmodule, Abhängigkeit an/aus (`shop` → `media`, `themes` → `design`), Abwählen einer Abhängigkeit entfernt Abhängige, unbekannte IDs ignoriert, Query-Reihenfolge stabil, SSE über zerteilte Chunks

### 3. Komponente auf echte Daten umstellen
- Beim Laden `GET /api/catalog`: Module, Abhängigkeiten, Plattformen und Release-Tag (im Header statt fest „cms v0.1.0“). Lade- und Fehlerzustand (503 → „Katalog derzeit nicht verfügbar“ + „Erneut versuchen“).
- Bei jeder Änderung von Auswahl oder Plattform `select` aufrufen (150 ms Entprellung, vorherige Anfrage per `AbortController` abbrechen). Anzeigen: Paket-ID, Dateiname, **echte Größe**, SHA-256 (erste 12 Zeichen, voll im `title`), Release.
- **Entfernen:** geschätzte Größen (`sizeKb`, `BASE_SIZE_KB`), simulierter Build (`BUILD_STEPS`, Log-Skript), angezeigter `go build`-Befehl – die Factory baut nicht (ADR-004).
- Haupt-Button = **„Herunterladen“** als `<a href={downloadUrl} download>`; daneben Hinweis „Signiert und geprüft“.
- Deploy-Bereich bleibt in dieser Session simuliert (`MOCK_DEPLOY`), nutzt aber schon die echte Paket-ID.

### 4. factory-api liefert das UI aus
- Flag `--ui <ordner>` (Standard leer = kein UI): `http.FileServer` auf `/`, `/api/*` und `/healthz` unberührt
- Header für UI-Antworten: `Content-Security-Policy: default-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`
- `index.html` mit `Cache-Control: no-cache`, gehashte Dateien unter `/assets/` mit `public, max-age=31536000, immutable`
- Go-Test (`httptest`, temporärer Ordner mit `index.html` + `assets/x.js`): Index ausgeliefert, Header gesetzt, `/api/catalog` weiter von der API, Pfad-Ausbruch (`/../`) unmöglich

### 5. Build und CI
- `Makefile` Ziel `ui`: `cd factory-ui && npm ci && npm run build && npm test` (ersetzt den esbuild-Syntaxcheck)
- `.github/workflows/ci.yml`: `actions/setup-node@v4` (`node-version: lts/*`, `cache: npm`, `cache-dependency-path: factory-ui/package-lock.json`) vor `make ui`
- `CLAUDE.md` Abschnitt „Befehle“: esbuild-Zeile durch `cd factory-ui && npm ci && npm run dev` (mit laufender factory-api) ersetzen

### 6. Abnahme nachweisen
```bash
cd factory-ui && npm run build && cd ..
go run ./factory-api/cmd/factory-api --ui ./factory-ui/dist   # http://127.0.0.1:8090
```
Im Browser `media` + `shop`, Plattform `linux/amd64` wählen → herunterladen → `shasum -a 256` der Datei = angezeigter/`SHA256SUMS`-Wert; Dateiname `styrixa_media-shop_linux_amd64.tar.gz`. Zweite Auswahl (z. B. nur Pflichtmodule, `darwin/arm64`) ebenso. Zeiten notieren.

## Session 2.3 – Echter Deploy auf VPS und Shared-Webspace

**Abnahme:** CMS läuft auf einem echten VPS (SSH) und einem echten Shared-Webspace (FTPS); auf beiden `/install` mit dem angezeigten Einmal-Code abgeschlossen und Login funktioniert.

### Vorbereitung durch den Nutzer (Claude Code fragt danach, bevor es losgeht)
- **VPS:** Linux mit SSH, ein **eigener Benutzer ohne root** (für `systemd --user`), möglichst `loginctl enable-linger <benutzer>` bereits aktiv; Architektur bekannt (`uname -m`). Optional eine Test-Domain mit Caddy oder nginx.
- **Webspace:** FTPS-Zugang, Apache mit `.htaccess`, eine **leere Test-Subdomain** mit eigenem Document-Root (z. B. `factory-test.<domain>`). **Nie** einen Ordner mit einer echten Website verwenden.

### 1. Echte Deploy-Anbindung im UI
- `MOCK_DEPLOY` und den gesamten Simulationscode entfernen; Deploy nur noch über `streamDeploy` (`POST /api/packages/{id}/deploy`)
- Ereignis `identity` → bestehender Fingerprint-Dialog → nach Bestätigung erneut senden mit `fingerprint`; bei `mismatch: true` roter Warnhinweis, Bestätigung erst nach erneutem ausdrücklichem Klick
- Ereignis `result` → Erfolg mit URL, Hinweisen, Caddy/nginx-Snippet und **Install-Code groß mit Kopier-Button** samt Hinweis „Nur einmal angezeigt – jetzt notieren“; Ereignis `error` → Meldung + „Erneut versuchen“
- Passwortfeld nach dem Absenden leeren; Zugangsdaten nie in `localStorage`
- Plattform-Vorgabe: SSH = gewählte Plattform (Server prüft per `uname`), FTP = nur `linux/*` zulassen
- Tests in `src/lib/` für den Ablauf als Zustandsfunktion (identity → confirm → result/error)

### 2. VPS-Test (SSH)
Lokal: `npm run build` und `factory-api --ui ./factory-ui/dist` starten. Der Nutzer wählt im UI die Plattform des VPS, gibt Host, Benutzer und Passwort/Deploy-Key ein, vergleicht den Fingerprint und deployt. Danach `/install` öffnen – über die Domain (Snippet aus dem Ergebnis) oder per Tunnel: `ssh -L 8080:127.0.0.1:8080 <benutzer>@<vps>` → `http://127.0.0.1:8080/install`. Einmal-Code eingeben, Wizard abschließen, einloggen.
Danach **zweites Deploy** desselben Pakets prüfen: neues Release unter `releases/`, Datenbank und Login bleiben, kein neuer Install-Code, `/install` bleibt 404.

### 3. Webspace-Test (FTPS, CGI-Modus)
Deploy auf die leere Test-Subdomain. Erwartet: Hoster-Check bestanden (oder klare Diagnose), dann `/install` über die Domain. Zusätzlich prüfen und im Plan notieren:
```bash
curl -s -o /dev/null -w '%{http_code}\n' https://<test-subdomain>/styrixa-data/styrixa.db     # 403
curl -s -o /dev/null -w '%{http_code}\n' https://<test-subdomain>/styrixa-data/install-code   # 403 (vor der Installation!)
curl -s -o /dev/null -w '%{time_total}\n' https://<test-subdomain>/                          # Antwortzeit CGI
curl -s https://<test-subdomain>/healthz                                                     # ok
```
**SQLite auf Webspace (offen aus 1.1):** Scheitert `OpenDB`/WAL auf dem Webspace (z. B. Netzwerk-Dateisystem), **anhalten**, Fehlermeldung und Hoster notieren und den Nutzer fragen. Keine eigenmächtige Ausweichlösung (z. B. anderes Journal) – das braucht eine Entscheidung im ADR.

### 4. Ergebnisse festhalten
In `docs/UMSETZUNGSPLAN.md` bei 2.3: Hoster (Name, Tarif), Plattform, CGI ja/nein, WAL ok, Antwortzeiten, Auffälligkeiten. Fehler im Deploy-Code werden mit Test behoben (Integrationstests in `internal/deploy` erweitern), nicht nur am echten System umgangen.

---

## Session 2.4 – PHP-Brücke für Webspaces ohne CGI (Ziel: easyname)

**Anlass:** 26.11.2026 – easyname-Webhosting lehnt `Options +ExecCGI` ab (HTTP 500), CGI ist dort nicht möglich. Entscheidung: ADR-012. **Modell: Sonnet.**
**Abnahme:** CMS läuft auf easyname-Webhosting über die PHP-Brücke; `/install` abgeschlossen, Login ok; `styrixa-data/*`, Binary und Konfiguration liefern 403/404; Brücken-Integrationstest in CI zeigt Server-Modus = Brücke.

### 0. Hoster-Probe (Nutzer, vor jedem Code)
Der Nutzer lädt `tools/hoster-probe/styrixa-probe.php` unter zufälligem Namen auf die easyname-Test-Subdomain, öffnet sie im Browser, kopiert die Ausgabe in den Chat und **löscht die Datei**. Ergebnis im Plan notieren.
- `proc_open: ja` **und** `exec_own_binary: ja` → weiter mit Schritt 1.
- sonst **anhalten**: Tarif ungeeignet. Nutzer informieren (Alternativen: easyname vServer mit SSH-Deployment, Webspace mit CGI) – keine Umgehung bauen.
- `sqlite_wal: NEIN` → anhalten, Nutzer fragen (betrifft auch CGI-Hosting, offen seit 1.1).

### 1. Die Brücke (`factory-api/internal/deploy/bridge/index.php`, per `embed`)
- Platzhalter für **absoluten** Binary- und Datenpfad, beim Deploy ersetzt (`var_export`, kein String-Basteln). Keine anderen Eingaben im Befehl.
- `proc_open([$bin], [0=>pipe r, 1=>pipe w, 2=>pipe w], $pipes, $cwd, $env)` – **Array-Form, keine Shell**.
- `$env`: `GATEWAY_INTERFACE=CGI/1.1`, `SERVER_PROTOCOL`, `REQUEST_METHOD`, `REQUEST_URI`, `QUERY_STRING`, `CONTENT_TYPE`, `CONTENT_LENGTH`, `SERVER_NAME`, `SERVER_PORT`, `HTTPS`, `REMOTE_ADDR`, `PATH_INFO` (= Pfad ohne Query), `SCRIPT_NAME=""`, alle Anfrage-Header als `HTTP_*` (aus `getallheaders()` bzw. `$_SERVER`), `STYRIXA_DATA`, `STYRIXA_MODE=cgi`, `PATH=/usr/bin:/bin`. **Keine** PHP-Umgebung durchreichen.
- Body: `php://input` blockweise nach stdin kopieren, dann stdin schließen. Antwort: Kopfzeilen bis zur Leerzeile lesen → `Status:` per `http_response_code`, übrige per `header(..., false)` (mehrere `Set-Cookie` erhalten), dann Body mit `fpassthru` streamen. stderr begrenzt (64 KB) in `error_log`.
- Fehlerfälle: Binary fehlt/startet nicht → schlichte 503-Seite auf Deutsch, Details nur ins Log. `set_time_limit` auf den Wert des Hosters belassen.
- Höchstens ca. 100 Zeilen, ohne Funktionen außer `proc_open`, `stream_*`, `header`.

### 2. Ablage auf dem Webspace
- Liegt der Document-Root **unterhalb** des FTP-Startordners (z. B. `/html` bzw. Subdomain-Ordner), kommen Binary und Daten in einen Geschwisterordner **außerhalb** des Webs: `<ftp-root>/styrixa-app/<site>/bin/styrixa` und `…/data/`. Sonst Fallback in `styrixa-data/` mit Sperr-`.htaccess` (wie bisher).
- `.htaccess` im Document-Root: Anfragen, die keine Datei sind, an `index.php` (mod_rewrite); falls Rewrite nicht erlaubt: `FallbackResource /index.php`. Sperre für `styrixa-data/`, `*.db*`, `install-code`.
- Absolute Pfade ermittelt die Brücke im Prüfordner selbst (`__DIR__`) und meldet sie an den Deploy zurück – nicht raten.

### 3. Hoster-Check erweitern (`ftp.go`)
Reihenfolge: Testdatei erreichbar → **CGI** (wie bisher) → wenn Diagnose „kein CGI“: **Brücke im Prüfordner** (Brücke + Binary hochladen, `…/ping` über Rewrite/Fallback → Brücke → Binary `/healthz`) → sonst Diagnose mit Empfehlung SSH/vServer. Erst nach bestandenem Check wird die Website angefasst. `Result.Mode` = `cgi` | `php-bridge`, im UI angezeigt.
Rollback und Wiederholungs-Deploy wie im CGI-Modus (vorheriges Binary behalten, Daten und Install-Code bleiben).

### 4. Tests
- Go-Integrationstest: `php -S 127.0.0.1:<port> -t <tmp>` mit der echten Brücke + echtem CMS-Binary (`go build`), Vergleich mit dem Server-Modus wie in `cmd/styrixa/public_test.go`: GET/HEAD, POST-Formular (Login), Redirect, mehrere `Set-Cookie`, 404, 413/große Bodys, Query-Strings, Umlaute im Pfad. Übersprungen, wenn `php` fehlt; **in CI vorhanden** (ubuntu-latest).
- Sicherheitstests: Header-Injection (`\r\n` in Headern) erreicht die Antwort nicht; Pfad-/Shell-Zeichen in der URL ändern den Befehl nicht; Binary und Datenordner per HTTP nicht abrufbar.
- `ftp_test.go`: Hoster-Check wählt CGI, wenn vorhanden; sonst Brücke; sonst Diagnose (Fake-Apache-Varianten).

### 5. Echter Test auf easyname
Deploy auf die leere Test-Subdomain → `/install` → Login. Prüfen und notieren: Modus, Antwortzeit (`curl -w '%{time_total}'` für `/` und `/admin`), 403/404 für Daten und Binary, zweites Deploy behält Daten. Bei Fehlern Ursache mit Test im Code beheben.
**Hinweis für den Nutzer:** In den easyname-AGB bzw. beim Support klären, ob das Ausführen eigener Programme über PHP im Tarif erlaubt ist und welche Prozess-/CPU-Grenzen gelten.

---

## Abnahme Phase 2 / M2 (alles muss erfüllt sein)

- [x] 2.1 Katalog: manipuliertes Paket abgelehnt, Auswahl < 1 s
- [x] Release `v0.2.0` (oder neuer) mit signierter `modules.json`; Katalog lädt ihn von echtem GitHub
- [x] 2.2 `factory-ui` ist Vite-Projekt, Module/Plattformen kommen aus `/api/catalog`, keine Schätzungen/Simulation des Builds mehr
- [x] 2.2 Download der gewählten Kombination startet sofort, SHA-256 stimmt
- [x] 2.2 `factory-api --ui` liefert das UI mit CSP aus (Go-Test), `node --test` für `src/lib/` grün, `make ui` in CI
- [ ] 2.3 Deploy ohne Simulation inkl. Fingerprint-Dialog und Install-Code-Anzeige
- [x] 2.3 VPS: `/install` abgeschlossen, Login ok, zweites Deploy behält Daten (easyname Cloud Server mit Plesk, 26.11.2026)
- [ ] 2.3 Webspace (mit CGI, falls vorhanden): Hoster-Check ok, `/install` abgeschlossen, Login ok, `styrixa-data/*` liefert 403
- [x] 2.4 Hoster-Probe auf easyname dokumentiert (Ergebnis: eigenes Binary nicht ausführbar → Brücke dort nicht möglich)
- [ ] 2.4 easyname über PHP-Brücke: `/install` abgeschlossen, Login ok, Daten/Binary nicht abrufbar, Modus `php-bridge` im UI
- [ ] 2.4 Brücken-Integrationstest (PHP-Server vs. Server-Modus) und Sicherheitstests in CI
- [ ] `make check` und CI grün

## Abschluss jeder Session

1. Session in `docs/UMSETZUNGSPLAN.md` abhaken, Abweichungen und Messwerte notieren.
2. Committen (eine Session = ein oder wenige Commits), pushen, CI grün.
3. Nach Session 2.4: Release `v0.3.0` taggen (M2), in `CLAUDE.md` „Aktuelle Phase“ auf `@docs/phasen/PHASE-3.md` umstellen (Datei existiert ggf. noch nicht – Nutzer informieren).

## Bekannte Stolpersteine

| Problem | Lösung |
|---|---|
| Katalog: „modules.json nicht signiert“ | Release vor 2.1 – neuen Tag pushen (`v0.2.0`) |
| GitHub-API 403/429 | ohne Token nur 60 Anfragen/h → `GITHUB_TOKEN` setzen (Nutzer, nur lesend) |
| Download privater Release-Assets scheitert | `browser_download_url` braucht bei privaten Repos den API-Asset-Weg mit `Accept: application/octet-stream` |
| SSE kommt im Dev-Modus gebündelt an | Vite-Proxy puffert nicht; prüfen, dass die API `Flush()` aufruft und kein Kompressions-Middleware dazwischen ist |
| CORS-Fehler | UI nicht über eine andere Origin öffnen: Dev über Vite (Proxy), sonst über `factory-api --ui` |
| `systemctl --user` fehlt / Dienst stoppt nach Logout | eigener Benutzer statt root, `sudo loginctl enable-linger <benutzer>`; sonst greift der nohup-Fallback (Hinweis im Ergebnis) |
| Plattform-Fehler beim SSH-Deploy | VPS ist arm64 (z. B. ARM-Instanzen) → `linux/arm64` wählen |
| FTPS-Zertifikat passt nicht zur Domain | normal bei Shared Hosting – Fingerprint mit dem Hoster-Panel vergleichen, dann bestätigen |
| Hoster-Check: „kein CGI“ | ab 2.4 automatisch PHP-Brücke; scheitert auch sie → SSH/vServer |
| Hoster-Check: „mod_rewrite fehlt“ | Brücke nutzt `FallbackResource`; sonst Tarif ungeeignet |
| Brücke: Antwort leer / 500 | `error_log` des Hosters prüfen; meist `proc_open` gesperrt oder Binary ohne Ausführungsrecht (`SITE CHMOD 755`) |
| `SITE CHMOD` nicht unterstützt | Rechte 755 für `styrixa.cgi` im Hoster-Panel setzen, Deploy wiederholen |
| Port 8080 auf dem VPS belegt | im UI anderen internen App-Port wählen |
| VPS mit Plesk: Port 80/443 belegt (nginx) | kein Caddy/`ufw`; Weiterleitung in `/var/www/vhosts/system/<domain>/conf/vhost_nginx.conf` (`location ~ ^/ { proxy_pass … }`), deployen als Plesk-Systembenutzer mit `/bin/bash` |
| `systemctl --user`: „Failed to connect to bus“, Dienst läuft nur per `nohup` | `sudo apt install libpam-systemd dbus-user-session`, `sudo loginctl enable-linger <benutzer>` |
| Fingerprint im Dialog ≠ `ssh-keygen` in der Konsole | anderer Schlüsseltyp (ECDSA statt ED25519) – alle prüfen: `for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done` |
| Let's Encrypt scheitert über IPv6 / landet beim alten Server | alte A/AAAA-Einträge (auch `*`) im easyname-DNS löschen, TTL (1 h) abwarten |
