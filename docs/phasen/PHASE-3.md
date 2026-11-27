# Phase 3 – Pflichtmodule Content + Dashboard (Sessions 3.1–3.5)

Diese Datei wird über `CLAUDE.md` geladen, sobald Phase 2 abgeschlossen ist (Release `v0.3.0`). Sie ergänzt die Projektregeln, ersetzt sie nicht.

**Ziel:** Mit dem CMS lassen sich echte Seiten bauen: Blöcke im Editor anlegen, als Entwurf speichern, veröffentlichen, öffentlich abrufen – sicher gegen eingeschleusten Code, ohne Datenverlust, mit cookiefreier Besucherzählung. Ein Smoke-Test prüft den ganzen Weg in CI.
**Umfang:** 5 Sessions, jede einzeln mit `/clear` dazwischen. **Modell:** 3.1 Opus (🟣, Sicherheitskern), 3.2–3.5 Sonnet.

## Ausgangslage (nach Phase 2)

Vorhanden – benutzen, nicht neu bauen:
- `internal/core`: Modul-Registry (`Register`, `Modules`, `Deps{DB, Settings, Admin}`), Migrationen je Modul, Admin-Layout (`html/template` + htmx, `AdminRenderer`), Auth/Sessions, `/install`, Lazy-Jobs, `Public` mit Theme-Schnittstelle (`PublicTheme`, `PublicView`), Mail, Rate-Limit.
- `internal/core/public.go`: `GET /{$}` = Startseite des Kerns, `/` = 404 des Kerns.
- `modules.json`: `content` (02) und `dashboard` (01) sind **Pflichtmodule** → `tools/matrix` setzt `mod_content` und `mod_dashboard` in **jeder** Kombination. Es gibt noch kein Paket `internal/modules/content` und keine `cmd/styrixa/mod_content.go`; das Dashboard liegt derzeit als Platzhalter im Kern (`core/dashboard.html`).
- Vorbild für ein Modul: `internal/modules/shop` (Migrationen per `embed`, Admin-Templates im Modulpaket, Tests).

## Phasenweite Regeln (zusätzlich zu CLAUDE.md)

- **Build-Tag-Isolierung gilt auch für Pflichtmodule:** `internal/modules/content/` nur über `cmd/styrixa/mod_content.go` (`//go:build mod_content`) importieren; `isolation_test.go` um einen Content-Marker erweitern.
- **Kein HTML-Parser als Abhängigkeit** (ADR-009, `golang.org/x/net/html` ist nicht erlaubt). Inline-HTML aus Editor.js wird mit einer eigenen **Allowlist** behandelt (siehe 3.1) – nie umgekehrt („Blocklist“).
- **Raw-HTML ist Vertrauenssache:** Raw-Blöcke und der HTML-Modus werden ungefiltert ausgegeben, dürfen aber **nur von Admins** gespeichert werden. Die Rolle wird beim Speichern serverseitig geprüft, nicht nur im UI.
- **Entwürfe sind nie öffentlich.** Öffentlich ist ausschließlich die veröffentlichte Revision. Vorschau nur eingeloggt und mit `X-Robots-Tag: noindex`.
- **CGI-tauglich:** keine In-Memory-Caches als Wahrheit; jede Anfrage liest aus SQLite.
- **Editor.js und offizielle Plugins** werden vendort (UMD-Builds, kein Build-Schritt, Version im Dateinamen, Eintrag in `VENDOR.md` mit Lizenz und SHA-256). Keine anderen JS-Bibliotheken.
- Keine Arbeit an Phase 4 (Medien-Upload, SEO-Modul, Themes, Shop-Seiten).

---

## Session 3.1 – Content: Datenmodell, Renderer, Slugs 🟣 Opus

**Abnahme:** Renderer-Test je Blocktyp grün; XSS-Testsuite grün (Text-Blöcke werden escaped, gefährliche URLs entfernt); Entwurf nicht öffentlich, veröffentlichte Seite unter ihrem Slug erreichbar, alter Slug leitet mit 301 um; `make check` grün.

### 1. Modul anlegen
- `internal/modules/content/` (`ID() = "content"`, keine `Requires`), `cmd/styrixa/mod_content.go` mit `//go:build mod_content`.
- `Makefile`/Tests: Alle `go build`/`go test`-Aufrufe für das CMS laufen mit den Pflicht-Tags (`mod_content mod_dashboard`) – sonst fehlen Seiten in Tests. `isolation_test.go` anpassen.

### 2. Datenmodell (`migrations/001_content.sql`)
- `content_pages`: `id`, `slug` (UNIQUE, NOCASE), `title`, `status` (`draft` | `published`), `is_home` (höchstens eine Seite = 1, per Teil-Index), `published_revision_id` (NULL bis zur ersten Veröffentlichung), `created_at`, `updated_at`.
- `content_revisions`: `id`, `page_id` (FK, ON DELETE CASCADE), `title`, `format` (`blocks` | `html`), `body` (Editor.js-JSON bzw. HTML), `author_id`, `created_at`. Entwurf = neueste Revision; veröffentlicht = `published_revision_id`.
- `content_redirects`: `from_slug` (PK, NOCASE) → `page_id`. Beim Ändern eines Slugs wird der alte eingetragen (SEO/GEO: keine toten Links).
- Aufräumen: höchstens 50 Revisionen je Seite, ältere per Lazy-Job löschen – die veröffentlichte nie.

### 3. Blockformat und Validierung
- Editor.js-Ausgabe `{time, blocks:[{id, type, data}], version}`. Grenzen: Body ≤ 1 MB, ≤ 500 Blöcke, Listen-Tiefe ≤ 3.
- Blocktypen: `paragraph`, `header` (Ebene **2–4**; H1 = Seitentitel), `list` (ordered/unordered, verschachtelt), `quote`, `delimiter`, `image` (URL + Bildunterschrift + Alt-Text), `raw` (nur Admin), sowie eigene Blöcke `faq` (Liste Frage/Antwort), `answer` (Answer-First: Frage + Kurzantwort + optional Details), `cta` (Text, Link-Label, URL).
- Unbekannte Typen werden **nicht gespeichert** (400 mit deutscher Meldung) – nicht still verworfen.

### 4. Inline-Sanitizer (Herzstück, eigene Datei + eigene Tests)
1. Alles mit `html.EscapeString` escapen.
2. Nur diese Tags zurückwandeln, exakt in dieser Form: `<b> <strong> <i> <em> <u> <mark> <code> <br>` und `<a href="…">`.
3. Erlaubte `href`: `https://`, `http://`, `mailto:`, `tel:`, relativ ab `/` oder `#`. Alles andere (z. B. `javascript:`, `data:`, `vbscript:`, auch verschleiert über Entities, Groß-/Kleinschreibung, Leerzeichen, Steuerzeichen) → Link entfernen, Text behalten. Externe Links bekommen `rel="noopener"`.
4. Tag-Balance herstellen (offene Tags schließen, verwaiste Schließ-Tags entfernen).
5. Keine Attribute außer `href`; keine `style`, keine `on*`.

### 5. Renderer
- `Render(body, format) (template.HTML, error)`, je Blocktyp eine Funktion; Ausgabe semantisch (`<h2 id="…">`, `<ul>/<ol>`, `<figure><img alt loading="lazy"><figcaption>`, `<blockquote>`, `<hr>`, FAQ als `<details><summary>`).
- Überschriften bekommen stabile Anker-IDs (Slug-Funktion, Duplikate `-2`, `-3`).
- FAQ- und Answer-Blöcke liefern zusätzlich Daten für `FAQPage`-JSON-LD (Ausgabe übernimmt in Phase 4 das SEO-Modul; hier nur die Daten bereitstellen und testen).
- Bild-URLs: nur relativ (`/media/…`) oder `https://`; sonst Block auslassen.

### 6. Slugs
- Aus dem Titel: klein, `ä→ae ö→oe ü→ue ß→ss`, Akzente entfernen, nur `[a-z0-9-]`, mehrfache `-` zusammenfassen, ≤ 80 Zeichen.
- Reserviert (nicht vergebbar): `admin`, `install`, `healthz`, `cron`, `api`, `static`, `assets`, `media`, `shop`, `feed`, `robots.txt`, `sitemap.xml` – Liste als Konstante, Test prüft, dass jede Kern-/Modulroute darin vorkommt.
- Kollision → `-2`, `-3` …; Slug ist editierbar (mit Redirect, siehe oben).

### 7. Öffentliche Auslieferung über den Kern
- Neue Schnittstelle in `internal/core` (z. B. `PageSource` mit `Page(ctx, path) (PublicView, bool, error)`). `Public` fragt bei `GET /{$}` und beim Catch-all zuerst alle Module, die sie implementieren; erst danach 404. Spezifischere Modulrouten (Go-1.22-Mux) haben Vorrang.
- Content liefert: Startseite (`is_home`), `/{slug}`, 301 für Einträge in `content_redirects`. Kein direkter Import zwischen Modulen.
- Templates: Content liefert nur den Inhalt (`content`-Block); Layout kommt vom Kern bzw. Theme. `PublicView` nur um Felder **ergänzen** (z. B. `Body template.HTML`, `Description string`).

### 8. Admin (minimal – das Editor-UI kommt in 3.2)
- `GET /admin/content` (Liste: Titel, Slug, Status, geändert), `POST /admin/content/pages` (neu aus Titel), `POST /admin/content/pages/{id}/draft` (JSON speichern → neue Revision, validiert), `POST /admin/content/pages/{id}/publish`, `POST /admin/content/pages/{id}/unpublish`, `POST /admin/content/pages/{id}/delete`. CSRF wie im Kern, deutsche Fehlermeldungen.
- Navigationseintrag „Seiten“.

### 9. Tests
- Tabellengetrieben: je Blocktyp gültige Eingabe → erwartetes HTML.
- XSS-Suite (eigene Testdatei): `<script>`, `<img src=x onerror=…>`, `<a href="javascript:…">`, `&#106;avascript:`, `JaVaScRiPt:`, `java\tscript:`, `data:text/html`, Attribut-Ausbruch per `"`, verschachtelte und unbalancierte Tags, Header-Ebene 1/7, unbekannter Typ, Raw-Block durch Nicht-Admin.
- Slugs: Umlaute, Sonderzeichen, Länge, reserviert, Kollision.
- Handler (`httptest`): Entwurf → 404 öffentlich; veröffentlicht → 200 mit Inhalt; Slug geändert → 301; Startseite; Löschen → 404.

---

## Session 3.2 – Editor.js einbinden (ADR-003)

**Abnahme:** Seite ohne Code bau- und sortierbar; gespeicherter JSON-Body ist von 3.1 validiert und wird öffentlich korrekt gerendert.
- Editor.js + offizielle Plugins (Header, List, Quote, Delimiter, Raw, Image mit URL-Eingabe – Upload folgt mit dem Media-Modul) vendoren, nur im Content-Paket per `embed`.
- Eigene Blöcke FAQ, Answer-First, CTA als kleine Vanilla-JS-Plugins im Modul.
- Speichern per `fetch` an `…/draft`, Rückmeldung per htmx-Swap. Admin-CSP prüfen (kein `unsafe-eval`; falls ein Plugin es braucht: anderes Plugin oder eigener Block).
- Inline-Werkzeuge auf die Allowlist aus 3.1 beschränken (fett, kursiv, Link, Markierung, Code).

## Session 3.3 – HTML-Modus, Auto-Save, Vorschau, Revisionen

**Abnahme:** kein Datenverlust beim Schließen des Tabs; Revision wiederherstellbar.
- Seitenweiter HTML-Modus als `<textarea>` (Monospace), nur Admin (siehe Regeln).
- Auto-Save alle 5 s **nur bei Änderung** als Entwurf; `beforeunload`-Warnung bei ungespeicherten Änderungen; gleichzeitiges Bearbeiten: letzte Revision gewinnt, Hinweis bei fremder neuerer Revision.
- Vorschau `/admin/content/pages/{id}/preview` (eingeloggt, `noindex`).
- Revisionsliste mit Zeit/Autor, „Wiederherstellen“ = neue Revision mit altem Inhalt.

## Session 3.4 – Dashboard

**Abnahme:** kein Cookie-Banner nötig; keine personenbezogenen Daten gespeichert.
- Dashboard aus dem Kern in `internal/modules/dashboard` (Pflichtmodul, `mod_dashboard`) verschieben.
- Zählung serverseitig: Tabelle `dashboard_hits(day, path, count)` per UPSERT – ohne Cookies, ohne IP, ohne User-Agent-Speicherung. Bots per einfacher UA-Liste und Admin-Pfade ausschließen. Zählung über einen Kern-Hook (kein Modul importiert ein anderes).
- Anzeige: Aufrufe letzte 7/30 Tage, Top-Seiten, Status (Version, DB-Größe, letzter Lazy-Job), Schnellzugriffe (neue Seite, Seiten).

## Session 3.5 – Smoke-Test (ADR-010)

**Abnahme:** Smoke-Test läuft in CI.
- Ein Go-Test (eigener Build-Tag `smoke`): Binary mit Pflicht-Tags bauen → auf Zufallsport starten → `/install` mit Code aus dem Datenordner → Login → Seite anlegen, Blöcke speichern, veröffentlichen → öffentlich abrufen und Inhalt prüfen → `/install` = 404.
- Eigener CI-Schritt (nur `linux/amd64`), Laufzeit < 1 min.

---

## Abnahme Phase 3 (alles muss erfüllt sein)

- [x] 3.1 Renderer-Test je Blocktyp, XSS-Suite grün, Entwurf nicht öffentlich, Slug-Redirect 301
- [x] 3.2 Seite ohne Code im Editor bau- und sortierbar
- [x] 3.3 Auto-Save, Vorschau, Revision wiederherstellbar, kein Datenverlust
- [x] 3.4 Dashboard ohne Cookies/personenbezogene Daten
- [x] 3.5 Smoke-Test in CI
- [x] `make check` und CI grün

## Abschluss jeder Session

1. Session in `docs/UMSETZUNGSPLAN.md` abhaken, Abweichungen notieren.
2. Committen (eine Session = ein oder wenige Commits), pushen, CI grün.
3. Nach Session 3.5: in `CLAUDE.md` „Aktuelle Phase“ auf `@docs/phasen/PHASE-4.md` umstellen (Datei existiert ggf. noch nicht – Nutzer informieren).

## Offene Fragen (vor der jeweiligen Session klären, nicht eigenmächtig entscheiden)

- ~~Seitenhierarchie (Unterseiten, `/leistungen/webdesign`) – in 3.1 nur flache Slugs. Hierarchie jetzt oder in Phase 4?~~ **Entschieden 26.11.2026: jetzt (in 3.1 umgesetzt).**
- Mehrsprachigkeit (de/en) – vorerst nicht vorgesehen.
- ~~Rollen: Reicht „Admin“ für alle Content-Rechte bis zum Users-Modul (Phase 4)?~~ **Entschieden 26.11.2026: Rollenspalte `admin`/`editor` schon in 3.1; Anlegen von Editoren und Routen-Sperren mit dem Users-Modul.**
