# Phase 4 – Optionale Module → M3 (Sessions 4.1–4.7)

Diese Datei wird über `CLAUDE.md` geladen, seit Phase 3 abgeschlossen ist. Sie ergänzt die Projektregeln, ersetzt sie nicht.

**Ziel:** Alle acht Module sind fertig. Bilder lassen sich hochladen und erscheinen responsiv, Seiten sind für Suchmaschinen und KI-Antworten vorbereitet, mehrere Benutzer arbeiten mit Rollen und 2FA, das Aussehen lässt sich über Design-Tokens und hochgeladene Themes (Spezifikation v1) anpassen. Google Analytics gibt es nur mit Einwilligung und standardmäßig aus. Alle 36 Kombinationen bauen, alle Tests sind grün.
**Umfang:** 7 Sessions in der Reihenfolge 4.1, 4.2, 4.3, 4.4, 4.5, 4.7, 4.6, jede einzeln mit `/clear` dazwischen. **Modell:** Sonnet. Für 4.4 (2FA) und 4.6 (ZIP-Prüfung) ist Opus sinnvoll, weil beide sicherheitskritisch sind.

## Ausgangslage (nach Phase 3)

Vorhanden – benutzen, nicht neu bauen:
- `internal/core`: Registry, `Deps{DB, Settings, Admin, Public}`, Admin-Layout mit `AdminTemplates`/`AdminAssets` (`{{asset "<modul>/<datei>"}}`, gehashte URLs unter `/admin/static/`), `AdminPage.StyleNonce`, Rollen `admin`/`editor` (`core.IsAdmin`, `CurrentRole`, `NavItem.Role`), CSRF per Feld `csrf` oder Header `X-CSRF-Token`, Lazy-Jobs (`core.RegisterJob`), `Public` mit `PublicTheme`, `PageSource`, `HitCounter`, `PublicPreviewer`.
- `PublicView` (Vertrag mit Themes, **Felder nur ergänzen**): `Lang`, `SiteTitle`, `Title`, `Status`, `Data`, `Body`, `Description`, `Path`.
- Content: Bild-Block mit `url`, `alt`, `caption` (URL nur `/…` ohne `//` oder `https://`), FAQ- und Answer-Blöcke liefern Daten für JSON-LD, reservierte Slugs enthalten u. a. `media`, `robots.txt`, `sitemap.xml`.
- `internal/modules/media/media.go` ist nur ein Platzhalter (Shop braucht `mod_media`).
- Deploy: Im FTP-/CGI-Modus liegt der Datenordner (`styrixa-data/`) unter dem Document-Root und ist per `.htaccess` komplett gesperrt. Apache liefert also nichts daraus direkt aus; alles geht durch das Binary.

## Phasenweite Regeln (zusätzlich zu CLAUDE.md)

- **Kein Modul kennt ein anderes beim Namen.** Wo ein Modul etwas eines anderen nutzt (Bildwähler im Editor, Head-Tags, JSON-LD, Theme-Tokens), definiert `internal/core` eine neutrale Schnittstelle; der Nutzer prüft per Typ-Assertion über `Modules()`, ob jemand sie implementiert. Ohne das Modul funktioniert der Nutzer weiter (Rückfall).
- **Theme-Vertrag = Spezifikation v1** (Skill `styrixa-theme`): Pflicht-Hooks `{{template "stx:head" .}}` und `{{template "stx:foot" .}}`, Daten `.Site.*`, `.Page.*`, `.Menu`, `.Year`, `.CSRF`, Funktionen `asset`, `t`, `money`, Block-Klassen `.stx-*`, Tokens als `--stx-…`-Variablen. Was der Kern dafür braucht, kommt schrittweise (Hooks in 4.2, Daten/Klassen/Tokens in 4.5, Upload in 4.6). Das bisherige Kern-Layout bleibt bis dahin gültig, `PublicView` bekommt nur neue Felder.
- **Uploads:** CSRF per Header `X-CSRF-Token` (nicht per Formularfeld – sonst liest `csrfOK` den ganzen Multipart-Body, bevor der Handler sein Limit setzen kann). Immer zuerst `http.MaxBytesReader`, dann Inhalt prüfen (Magic Bytes, nicht Dateiendung oder `Content-Type` des Browsers). Gespeichert wird unter einem Zufallsnamen, nie unter dem Namen des Browsers. Schreiben atomar (temporäre Datei + `os.Rename`); scheitert die DB, werden die Dateien wieder gelöscht.
- **Öffentlich ausgelieferte Dateien** (Medien, Theme-Assets) bekommen `X-Content-Type-Options: nosniff`, einen festen `Content-Type` aus der DB bzw. Erlaubnisliste und `Content-Security-Policy: default-src 'none'; sandbox`. Dateien mit Inhalts-Hash oder Zufalls-ID im Namen: `Cache-Control: public, max-age=31536000, immutable`.
- **Keine Bildverarbeitung am Server** (ADR-001). Der Server liest nur Kopfdaten (Breite/Höhe) zur Prüfung: JPEG über `image/jpeg.DecodeConfig`, WebP über einen eigenen Kopf-Parser (VP8/VP8L/VP8X, ca. 40 Zeilen). Kein `golang.org/x/image`.
- **Externe Verbindungen:** Öffentliche Seiten laden nichts von fremden Servern. Einzige Ausnahme ist `gtag.js` nach Einwilligung (4.7, ADR-013).
- **CGI:** Jede Anfrage liest aus SQLite. Sitemap, `robots.txt` und `llms.txt` werden pro Anfrage aus der DB erzeugt (mit ETag), nicht als Datei vorgehalten.
- Neue Settings-Schlüssel als Konstanten mit Modul-Präfix (`media.…`, `seo.…`, `design.…`), Geheimnisse (TOTP-Schlüssel) nie loggen.
- Isolationstest (`cmd/styrixa/isolation_test.go`) je neuem Modul um einen Marker erweitern; Templates und JS nur per `embed` im Modulpaket.
- Keine Arbeit an Phase 5 (Shop-Seiten, Rechtstexte, Impressum-Generator).

---

## Session 4.1 – Media: Upload, Ordner, Alt-Texte, `srcset`

**Abnahme:** Bild im Browser hochladen → drei (bzw. passend weniger) WebP- oder JPEG-Varianten liegen im Datenordner, öffentlich unter `/media/…` mit `srcset`, Breite/Höhe und Alt-Text; ungültige Dateien (falsche Magic Bytes, SVG, zu groß, zu viele Varianten) → 400 mit deutscher Meldung; Bild-Block im Editor wählt oder lädt Bilder aus der Mediathek, ohne `mod_media` bleibt das URL-Feld; `make check` grün.

### Entscheidungen (Nutzer, 27.11.2026)
- **Nur Rasterbilder:** WebP und JPEG, wie sie der Browser nach ADR-001 erzeugt. Kein SVG (XSS), keine PDFs.
- **Ordner virtuell in der DB:** Dateien liegen flach unter `<data>/media/`; Verschieben ändert keine URL.
- **Bild-Block nutzt die Mediathek** über eine Kern-Schnittstelle; ohne `mod_media` bleibt alles wie bisher.

### 1. Kern
- `Deps.DataDir string` (Datenordner; `main` setzt ihn, in Tests `t.TempDir()`).
- Erweiterungspunkte für Admin-Skripte: Schnittstelle `AdminExtensions` (`AdminExtensions() map[string][]string` = Punkt → Asset-Namen des eigenen Moduls). Konstante `core.ExtEditor = "editor"`. Template-Funktion `{{range extensions "editor"}}<script src="{{.}}"></script>{{end}}` liefert die gehashten URLs aller Module. Unbekannte Asset-Namen → Fehler beim Start.
- JS-Vertrag (in `core` dokumentiert): Ein Skript am Punkt `editor` darf `window.StyrixaMediaPicker = { pick(): Promise<Bild | null> }` setzen. `Bild = {url, alt, width, height, variants: [{url, width}]}`.

### 2. Datenmodell (`migrations/001_media.sql`)
- `media_folders`: `id`, `parent_id` (NULL = Wurzel, FK ON DELETE RESTRICT), `name` (1–80 Zeichen, eindeutig je Elternordner, NOCASE), `created_at`. Tiefe ≤ 3.
- `media_files`: `id`, `folder_id` (NULL = Wurzel), `key` (128 Bit Zufall, Base32 klein, UNIQUE – Teil der URL), `name` (Anzeigename aus dem Dateinamen, ≤ 120 Zeichen, nur Anzeige), `alt` (≤ 250), `decorative` (0/1), `mime` (`image/webp` | `image/jpeg`), `width`, `height` (größte Variante), `bytes` (Summe), `created_by`, `created_at`, `updated_at`.
- `media_variants`: `file_id` (FK ON DELETE CASCADE), `width`, `height`, `bytes`, PK (`file_id`, `width`). Dateiname auf der Platte: `<key>-<width>.<webp|jpg>`.

### 3. Upload im Browser (`static/media-upload.js`, ADR-001)
- Datei wählen oder ziehen → `createImageBitmap` → Canvas je Zielbreite 480, 960, 1920; nur Breiten **≤ Originalbreite**, ist das Original schmäler als 480, genau eine Variante in Originalbreite.
- `toBlob("image/webp", 0.82)`; liefert der Browser `image/png` (Safari), alle Varianten als `image/jpeg` (0.85). Alle Varianten einer Datei haben dasselbe Format.
- Alt-Text ist Pflicht, außer „Dekoratives Bild“ ist angehakt (dann `alt=""`). Der Server prüft das ebenfalls.
- Senden per `fetch` (multipart: `variant_<breite>`, `name`, `alt`, `decorative`, `folder_id`) mit Header `X-CSRF-Token`; Fortschritt und Fehlermeldung in `role="status"`.

### 4. Server-Prüfung
- `MaxBytesReader` 12 MB je Anfrage, je Variante ≤ 4 MB, 1–3 Varianten, nur Breiten aus {480, 960, 1920} oder genau eine Variante < 480.
- Magic Bytes (`RIFF….WEBP` bzw. `FF D8 FF`), Format für alle Varianten gleich, Breite aus dem Dateikopf = angegebene Breite, Seitenverhältnis aller Varianten gleich (±1 px), Höhe ≤ 4 × Breite und umgekehrt. Alles andere → 400 mit deutscher Meldung.
- Speicherplatz: Dateien in `<data>/media/` mit 0600 bzw. Ordner 0700; Zielname nur aus `key` und Breite gebildet (kein Pfad aus Eingaben).

### 5. Auslieferung
- `GET /media/{file}` mit `{file}` = `^[a-z2-7]{26}-(\d{2,4})\.(webp|jpg)$`, sonst 404 (kein DB-Zugriff). Variante muss in der DB stehen. Header siehe Phasenregeln; `http.ServeContent` (Range, `If-Modified-Since`) mit Datei-Handle. Im CGI-Modus genauso (Browser cachen dank `immutable`).
- Öffentlich ist jede hochgeladene Datei sofort abrufbar (wie bei üblichen CMS); der Zufalls-Key verhindert das Erraten.

### 6. Admin (`/admin/media`, Nav „Medien“, `Role: RoleEditor`)
- Raster mit Vorschaubild (480er-Variante), Name, Alt-Text-Hinweis („fehlt“ nie möglich, „dekorativ“ markiert), Ordnerleiste mit Brotkrumen, Suche nach Name/Alt-Text.
- Ordner anlegen, umbenennen, löschen (nur leer → sonst 400), Datei verschieben, Alt-Text/Name bearbeiten (htmx).
- Löschen nur für Admins (403 serverseitig). Vorher Hinweis auf Verwendung: neue Kern-Schnittstelle `MediaReferrer` (`MediaRefs(ctx, url string) ([]string, error)` → Titel der Seiten, die die URL enthalten). Content implementiert sie mit `LIKE` über die neueste und die veröffentlichte Revision. Gelöscht werden DB-Zeilen und Dateien.
- Admin-CSP: `img-src 'self'` reicht; das Upload-Skript braucht `blob:` für die Vorschau nicht, wenn es Canvas direkt anzeigt – sonst `img-src 'self' blob:` nur auf der Medienseite über ein neues `AdminPage`-Feld.

### 7. Bildwähler und `srcset` im Content-Modul
- `media` registriert `static/media-picker.js` am Punkt `editor`: Dialog (`<dialog>`) mit Ordnern, Suche, Raster und „Hochladen“ (nutzt `media-upload.js`), liefert `Bild`.
- Content-Template `page.html` bindet `{{range extensions "editor"}}` ein. `ImageBlock` zeigt „Aus Mediathek wählen“, sobald `window.StyrixaMediaPicker` existiert, und übernimmt `url`, `alt`, `width`, `height`, `variants`. URL-Eingabe bleibt möglich.
- `imageData` bekommt optional `width`, `height`, `variants [{url, width}]`. Validierung: höchstens 3 Varianten, jede URL eigener Pfad (`/…`, kein `//`), Breite 1–4000, Breite/Höhe beide oder keine. Renderer: `<img src="<größte ≤ 960, sonst kleinste>" srcset="… 480w, … 960w, … 1920w" sizes="(min-width: 48rem) 42rem, 100vw" width height alt loading="lazy" decoding="async">`. Ohne Varianten bleibt die Ausgabe wie bisher.

### 8. Tests
- Tabellengetrieben: Magic Bytes, WebP-/JPEG-Kopf (echte Mini-Dateien als Testdaten unter `testdata/`), Grenzen (Größe, Anzahl, Breiten, Seitenverhältnis, Format gemischt), Alt-Pflicht, Dateinamen-Muster, Pfad-Ausbruch unmöglich.
- Handler (`httptest`): Upload → Dateien + Zeilen; Auslieferung mit Headern (`nosniff`, CSP `sandbox`, `immutable`, Range); unbekannte Variante 404; Löschen durch Editor 403, durch Admin entfernt Dateien; Ordner nicht leer → 400; CSRF fehlt → 403.
- Content: Bild-Block mit Varianten → `srcset`, ungültige Varianten-URL → 400; `MediaRefs` findet Seiten.
- Isolationstest: Marker in `media-picker.js`; ohne `mod_media` weder Picker noch Medien-Templates im Binary.
- Im Browser (lokal): Upload in Chrome und Safari (WebP bzw. JPEG), Bild im Editor wählen, öffentlich `srcset` prüfen.

---

## Session 4.2 – SEO: Meta-Felder, Sitemap, robots.txt, Redirects, Search Console

**Abnahme:** wie im Umsetzungsplan (Meta-Tag nur mit hinterlegtem Code, ungültige Eingaben abgelehnt), zusätzlich: gültige `sitemap.xml` mit allen veröffentlichten Seiten, `robots.txt`, eigene 301-Regeln greifen vor dem 404.

### Kern
- **Head- und Foot-Hooks:** Schnittstelle `PublicHead` (`PublicHead(ctx, v PublicView) (template.HTML, error)`) und `PublicFoot` (gleiche Form). Der Kern definiert die Templates `stx:head` (Charset, Viewport, Titel, Description, Canonical, danach alle `PublicHead`-Beiträge) und `stx:foot` (alle `PublicFoot`-Beiträge) und ruft sie im Kern-Layout auf. Beiträge dürfen nur aus `html/template` stammen.
- **Seiten-Metadaten:** Schnittstelle `PageMeta` (Content implementiert): `PageMeta(ctx, path) (Meta, bool, error)` mit `Title`, `Description`, `NoIndex`, `UpdatedAt`; `SitemapSource` (`SitemapEntries(ctx) ([]SitemapEntry, error)`: Pfad, letzte Änderung).
- `PublicView` + `Canonical string` (absolute URL aus `site.url` + Pfad), `NoIndex bool`.

### SEO-Modul (`mod_seo`)
- Meta-Felder je Seite: SEO-Titel (≤ 70), Description (≤ 160, mit Zeichenzähler), „nicht indexieren“. Speicherung in `seo_page_meta(path …)` oder als Content-Seitenfeld – **vor der Session klären** (siehe offene Fragen).
- `GET /sitemap.xml` (`encoding/xml`, nur veröffentlichte, indexierbare Seiten, `lastmod`), `GET /robots.txt` (Standard: alles erlaubt außer `/admin/`, Sitemap-Zeile; Zusatzzeilen im Admin mit strenger Zeilenprüfung).
- Redirects: `seo_redirects(from_path PK NOCASE, to_path, created_at, hits)` – Ziel eigener Pfad oder `https://`, keine Ketten, keine Schleifen; greifen über eine Kern-Schnittstelle vor dem 404 (`RedirectSource`), Content-eigene Redirects (Slug-Änderung) bleiben.
- Search Console wie im Umsetzungsplan; Code-Format `^[A-Za-z0-9_-]{10,100}$`.

## Session 4.3 – GEO: JSON-LD, `llms.txt`, Answer-First-Hinweis

**Abnahme:** JSON-LD jeder Seite ist gültiges JSON (Test mit `encoding/json`) und enthält Organization bzw. LocalBusiness (mit GISA bei de-AT), `FAQPage` bei FAQ-Blöcken und `Article` bei Inhaltsseiten; `GET /llms.txt` listet Seiten mit Kurzbeschreibung; der Editor zeigt einen Hinweis, wenn eine Seite keinen Answer-First-Block hat.
- Ausgabe über `PublicHead` als `<script type="application/ld+json">` mit `json.Marshal` und anschließendem Escapen von `<`, `>`, `&` (`<` …) – kein Template-Text im JSON.
- Firmendaten (Name, Adresse, Telefon, Öffnungszeiten, `LocalBusiness`-Typ) im SEO-Admin, GISA aus `site.gisa`.
- FAQ-/Answer-Daten liefert Content über eine Kern-Schnittstelle `StructuredData` (die Daten existieren seit 3.1).
- Hinweis im Editor: kleines Skript am Punkt `editor` (aus 4.1), kein Blockieren des Speicherns.

## Session 4.4 – Users: RBAC, 2FA (TOTP), Aktivitätsprotokoll 🟣 empfohlen

**Abnahme:** Rollen werden serverseitig für jede Route durchgesetzt (Tabellentest Rolle × Route); TOTP nach RFC 6238 mit den Testvektoren aus Anhang B; Anmeldung mit 2FA nur mit gültigem Code; Protokoll enthält An-/Abmeldung, Benutzer- und Rollenänderungen.
- Rollen: `admin`, `editor` (= Redakteur), neu `author` (nur eigene Seiten, kein Veröffentlichen) und `customer` (kein Admin-Zugang; vorbereitet für 5.9). Migration im Kern, Rollenprüfung im Kern (`NavItem.Role` → Mindestrolle, neue Hilfe `core.RequireRole`).
- Routen anderer Module für Editoren/Autoren sperren (offener Punkt aus 3.1): Jede Admin-Route bekommt eine Mindestrolle; Standard ist Admin.
- TOTP: SHA-1, 6 Stellen, 30 s, ±1 Schritt, Schlüssel 160 Bit, Wiederverwendung eines Codes verhindert, Wiederherstellungscodes (10 Stück, nur als Hash). QR-Code nicht als Bibliothek: `otpauth://`-URI als Text zum Kopieren; QR optional über einen eigenen kleinen Encoder – **vor der Session klären**.
- Aktivitätsprotokoll: `users_activity(id, at, user_id, action, target)` ohne IP-Adresse; Aufbewahrung 180 Tage per Lazy-Job.

## Session 4.5 – Design: Theme-Tokens, lokale Fonts, Template-Auswahl

**Abnahme:** Tokens aus dem Admin erscheinen als `--stx-…`-Variablen im `<head>`; Kontrastprüfung (WCAG AA 4,5 : 1) lehnt zu schwache Paare ab; Fonts werden nur vom eigenen Server geladen; die öffentlichen Seiten erfüllen den Datenvertrag der Theme-Spezifikation v1.
- Datenvertrag v1 im Kern ergänzen: `.Site` (`Title`, `Lang`, `Region`, `Logo`, `Legal`), `.Page` (`Title`, `Slug`, `Summary`, `Content`), `.Menu` (aus der Seitenhierarchie, Quelle über Kern-Schnittstelle), `.Year`, `.CSRF`; Funktionen `asset`, `t`, `money`. Bestehende Felder bleiben.
- Renderer-Klassen auf `.stx-*` umstellen (`stx-paragraph`, `stx-heading`, `stx-image`, …); bisherige Klassen (`faq`, `answer`, `cta`) parallel lassen, bis das Kern-Layout umgestellt ist.
- Tokens (Farben, Schriften, Radius, Abstand, Container) in `design_tokens` bzw. Settings, Ausgabe über `PublicHead` als `<style>:root{…}</style>` mit strenger Wertprüfung (Farben `#RRGGBB`, Längen `^\d+(\.\d+)?(px|rem)$`, Schriftnamen aus einer Liste der hochgeladenen Fonts).
- Fonts: Upload von WOFF2 (Magic Bytes `wOF2`, ≤ 1 MB), Auslieferung wie Medien, `@font-face` mit `font-display: swap`, Preload der ersten Schrift.
- Template-Auswahl: mitgelieferte Kern-Layouts (z. B. „Schlicht“, „Breit“) als `PublicTheme`-Vorlagen; das Design-Modul liefert sie, solange kein hochgeladenes Theme aktiv ist.

## Session 4.7 – Google Analytics 4 mit Einwilligung (ADR-013)

**Abnahme:** wie im Umsetzungsplan.
- Banner und Einstellungen über `PublicFoot` (Vanilla-JS-Datei aus `mod_seo`, ausgeliefert unter `/seo/consent.<hash>.js`, kein Inline-Skript). Der Link „Cookie-Einstellungen“ kommt aus `stx:foot`, damit jedes Theme ihn hat.
- Öffentliche CSP: bisher gibt es keine – in dieser Session für öffentliche Seiten einführen (`script-src 'self'` + Google-Domains nur bei aktiver Mess-ID). Vorher prüfen, dass Raw-Blöcke mit Inline-Skripten dadurch nicht unbemerkt brechen (Hinweis im Admin) – **vor der Session klären**.
- Test ohne Browser: Antwort-HTML ohne Einwilligung enthält keine Google-URL außer in der Datei, die erst nach Klick lädt; JS-Logik mit `node --test` (Cookie setzen, Widerruf löscht `_ga*`).

## Session 4.6 – Themes: ZIP-Upload, Prüfung, Aktivierung, Rollback 🟣 empfohlen

**Abnahme:** ein mit dem Skill `styrixa-theme` gebautes ZIP wird ohne Nacharbeit angenommen und aktiviert; die Negativ-Suite (Zip-Slip `../`, absolute Pfade, Symlinks, versteckte Dateien, unerlaubte Endungen, > 500 Dateien, > 20 MB entpackt, Zip-Bombe mit hohem Kompressionsverhältnis, fehlende Pflicht-Hooks, externe URLs, `on*=`-Attribute, `<script>` in SVG, ungültiges `theme.json`) lehnt jedes Beispiel mit deutscher Meldung ab; Rollback auf das vorherige Theme mit einem Klick.
- Guard `mod_themes → mod_design` (`guard_themes.go`), `modules.json` hat die Abhängigkeit schon.
- Prüfung nur mit `archive/zip`; Größe aus dem Header **und** beim Lesen begrenzen (`io.LimitReader`). Templates werden beim Hochladen probeweise geparst und mit Beispieldaten ausgeführt.
- Ablage: `<data>/themes/<id>/<version>/`, aktive Version in den Settings; der Kern lädt das aktive Theme pro Anfrage aus dem Datenordner (CGI-tauglich; geparst wird pro Prozess einmal, Wahrheit bleibt die Platte).
- Assets unter `/theme/<id>/<version>/…` mit `immutable`; SVG mit `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; sandbox`.
- Kern-Schnittstelle `PublicTheme` muss dafür zur Laufzeit wechseln können (bisher einmal beim Start) – Umbau in dieser Session.

---

## Abnahme Phase 4 (alles muss erfüllt sein)

- [x] 4.1 Upload mit Varianten, Prüfung, `srcset`, Bildwähler im Editor
- [ ] 4.2 Meta-Felder, Sitemap, robots.txt, Redirects, Search-Console-Tag
- [ ] 4.3 JSON-LD gültig, `llms.txt`, Answer-First-Hinweis
- [ ] 4.4 Rollen je Route, TOTP mit RFC-Testvektoren, Aktivitätsprotokoll
- [ ] 4.5 Tokens als CSS-Variablen, lokale Fonts, Datenvertrag v1
- [ ] 4.7 GA nur nach Einwilligung, 0 Google-Anfragen ohne Zustimmung
- [ ] 4.6 Theme-ZIP aus dem Skill angenommen, Negativ-Suite grün, Rollback
- [ ] Alle 36 Kombinationen gebaut und getestet, `make check` und CI grün (M3)

## Abschluss jeder Session

1. Session in `docs/UMSETZUNGSPLAN.md` abhaken, Abweichungen notieren.
2. Committen (eine Session = ein oder wenige Commits), pushen, CI grün.
3. Nach der letzten Session: Release `v0.4.0`, in `CLAUDE.md` „Aktuelle Phase“ auf `@docs/phasen/PHASE-5.md` umstellen (Datei existiert ggf. noch nicht – Nutzer informieren).

## Offene Fragen (vor der jeweiligen Session klären, nicht eigenmächtig entscheiden)

- ~~4.1 Dateitypen, Ordner, Bildwähler~~ **Entschieden 27.11.2026:** nur WebP/JPEG, Ordner virtuell, Bildwähler über Kern-Schnittstelle.
- 4.1: Logos mit Transparenz – in Safari wird nach ADR-001 JPEG erzeugt (Transparenz geht verloren). PNG als dritte Ausnahme zulassen oder Logo später über das Design-Modul (4.5) hochladen?
- 4.2: Meta-Felder als Spalten im Content-Modul (einfacher, aber SEO-Daten in Content) oder eigene Tabelle `seo_page_meta` im SEO-Modul (sauber getrennt, Pfadänderungen müssen nachgezogen werden)?
- 4.4: QR-Code für die 2FA-Einrichtung – nur `otpauth://`-Text zum Kopieren oder eigener QR-Encoder (~300 Zeilen, keine Abhängigkeit)?
- 4.5: Woher kommt `.Menu` – automatisch aus der Seitenhierarchie (oberste Ebene) oder eigener Menü-Editor?
- 4.5: `.Site.Legal` ist bis Phase 5.1 leer bzw. kommt aus freien Links im Design-Admin?
- 4.7: Öffentliche CSP für alle Seiten einführen (bricht Inline-Skripte in Raw-Blöcken) oder nur bei aktivem GA?
