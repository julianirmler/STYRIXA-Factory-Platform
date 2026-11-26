# Architekturentscheidungen (ADRs)

Leitlinie für alle Entscheidungen: **die günstigste Lösung, die das Risiko vollständig beseitigt** – wenig Code, keine Server-Kosten, keine neuen Abhängigkeiten ohne Not.

| ADR | Thema | Status |
|---|---|---|
| 001 | Bilder ohne CGO | ✅ entschieden |
| 002 | Admin-Oberfläche | ✅ entschieden |
| 003 | Block-Editor | ✅ entschieden |
| 004 | Builds: vorgebaut statt Build-Server | ✅ entschieden, umgesetzt (`release.yml`) |
| 005 | Hintergrundaufgaben | ✅ entschieden |
| 006 | Updates der Kunden-CMS | ✅ entschieden |
| 007 | Zahlungen und Rechtliches im Shop | ✅ entschieden (ergänzt 25.11.2026: AT/EU, Produktarten, Widerrufsbutton, Labels), Stripe umgesetzt |
| 008 | Shared-Hoster ohne CGI | ✅ entschieden, umgesetzt (Hoster-Check); ergänzt durch ADR-012 |
| 009 | Nur reine Go-Abhängigkeiten | ✅ entschieden, umgesetzt (`make cgo`, CI) |
| 010 | Tests | ✅ entschieden |
| 011 | Factory-Konten und Abrechnung | ⚠️ Geschäftsmodell offen (einzige offene Entscheidung) |
| 012 | PHP-Brücke für Webspaces ohne CGI (z. B. easyname) | ✅ entschieden 26.11.2026, Umsetzung Session 2.4 – vorbehaltlich Hoster-Probe |

---

## 001 – Bilder ohne CGO

**Entscheidung:** Bilder werden **im Browser** beim Upload verkleinert und umgewandelt (Canvas `toBlob`), der Server speichert nur Dateien. Kein AVIF, keine serverseitige Bildverarbeitung, keine Bibliothek.

- Drei Breiten: 480, 960, 1920 px (größere Originale werden nicht gespeichert).
- Format: WebP (Qualität 0,82). Browser, die Canvas-WebP nicht erzeugen können (Safari), liefern stattdessen PNG – das wird erkannt (`blob.type`), dann wird JPEG (0,85) erzeugt, das alle Browser können.
- `<picture>`/`srcset` im Template, `loading="lazy"`, Breite/Höhe gesetzt (kein Layout-Springen).

**Warum:** Null Abhängigkeiten, ca. 100 Zeilen JS, funktioniert im CGI-Modus ohne CPU-Last am Webspace. AVIF bringt gegenüber WebP wenig und ist ohne CGO bzw. in Safari nicht sinnvoll erzeugbar.
**Folgen:** Redakteure mit Safari bekommen JPEG statt WebP (immer noch verkleinert). Prototyp-Session 0.3 entfällt.

## 002 – Admin-Oberfläche

**Entscheidung:** Server-gerendert mit Go `html/template` + **htmx** (eine vendorte JS-Datei, Lizenz 0BSD). Kein React, kein Node-Build im CMS.

- Jedes Modul bettet seine Templates und ggf. JS per `embed` im eigenen Paket ein → Build-Tag-Isolierung gilt automatisch auch fürs Frontend.
- Gemeinsames CSS: eine handgeschriebene, kleine Datei im Kern (CSS-Variablen aus dem Design-Modul, falls aktiv).
- `factory-ui` bleibt React (existiert bereits, ist kein Teil des Kunden-Binarys).

**Warum:** Spart die komplette Frontend-Build-Pipeline pro Modul (größter versteckter Zeitfresser), funktioniert ohne JS-Framework im CGI-Modus, kleineres Binary.

## 003 – Block-Editor

**Entscheidung:** **Editor.js** (Apache-2.0, vendort, kein Build-Schritt) statt eigenem Drag-and-Drop-Editor. Speichert Blöcke bereits als JSON – passt 1:1 zum Datenmodell. Gerendert wird serverseitig in Go.

- Grundblöcke: Absatz, Überschrift, Liste, Bild (Upload über ADR-001), Zitat, Trenner, Raw-HTML, plus eigene einfache Blöcke: FAQ, Answer-First, CTA.
- „HTML Studio“ = Raw-HTML-Block + seitenweiter HTML-Modus als `<textarea>` mit Monospace. Kein CodeMirror (später optional).
- Auto-Save: alle 5 s per htmx/fetch als Entwurf, Revisionen in der DB.

**Warum:** Ein eigener Editor ist der größte Zeitfresser des Projekts; Editor.js liefert Drag-and-Drop, Tastatursteuerung und Block-Toolbar fertig.

## 004 – Builds: vorgebaut statt Build-Server

**Entscheidung:** Bei jedem Release baut GitHub Actions **alle gültigen Modul-Kombinationen** für alle Plattformen (derzeit 36 × 4 = 144 Pakete), signiert `SHA256SUMS` mit ed25519 und hängt alles an das GitHub-Release. Die Factory wählt nur das passende Paket aus.

- Quelle der Wahrheit für Module und Abhängigkeiten: `cms-template/modules.json` → `tools/matrix`.
- Kein Build-Worker, keine Sandbox, keine Warteschlange, kein Build-Cache, keine Build-Server-Kosten. „Build“ dauert für den Kunden < 1 s.
- Schwelle zum Umdenken: Jedes neue unabhängige optionale Modul verdoppelt die Pakete. Ab ca. 300 Kombinationen pro Plattform auf Build-on-Demand umstellen (der Code aus `release.yml` ist dafür die Basis).

**Warum:** Eliminiert Phase-2-Sessions 2.1/2.2 fast vollständig und jede Sicherheitsfrage rund um Code-Ausführung auf dem Server.

## 005 – Hintergrundaufgaben

**Entscheidung:** Keine Daemons. Lazy-Jobs bei Anfragen (z. B. Sitemap neu erzeugen, wenn älter als X und sich Inhalte geändert haben) + optionaler Cron-Endpunkt `/cron`. CGI-tauglich.

**Ergänzung (Session 1.6, Nutzerentscheidung):** Der Cron-Token kommt **nur im Header** `X-Styrixa-Cron-Token` (`curl -fsS -H 'X-Styrixa-Cron-Token: …' https://…/cron`), nie in der URL – sonst landet er in den Access-Logs von Hoster und Proxy. In den Settings steht nur sein SHA-256 (`core.cron.token_sha256`); ohne Token ist `/cron` 404.

## 006 – Updates der Kunden-CMS

**Entscheidung:** Das CMS liest das Manifest des neuesten GitHub-Releases, zeigt im Admin „Update verfügbar“ und tauscht sich per Klick selbst aus: Paket für die eigene Modul-Kombination laden → Signatur prüfen (öffentlicher Schlüssel im Binary) → Binary ersetzen. Server-Modus: neues Binary daneben, umbenennen, Neustart über systemd. CGI-Modus: Datei ersetzen, fertig.

**Warum:** Kein Update-Server nötig; nutzt die vorgebauten Releases aus ADR-004.

## 007 – Zahlungen und Rechtliches im Shop

**Entscheidung:**
- **Nur Stripe Checkout.** Karten, EPS, Klarna, Apple/Google Pay **und PayPal** laufen darüber (PayPal ist bei Stripe für Unternehmen in AT/DE/CH verfügbar, Freischaltung im Stripe-Dashboard). Keine eigene PayPal-Integration.
- Kartendaten berühren nie den Server (gehostete Bezahlseite) → minimaler PCI-Aufwand für Kunden.
- Vorkasse bleibt als zweite Zahlart (null Code beim Anbieter).
- **Rechtstexte** (AGB, Widerrufsbelehrung, Datenschutz, Impressum, Versand & Zahlung, ggf. Barrierefreiheit) sind normale Seiten im Content-Modul mit Versionierung. **Widerrufsbelehrung und Muster-Widerrufsformular** werden aus dem gesetzlichen Muster (FAGG Anhang I) **vorbefüllt**; AGB und Datenschutz liefert der Betreiber. Das Impressum wird aus den Betreiberangaben erzeugt.
- **Rahmen (ergänzt 25.11.2026):** Verkauf an Verbraucher in **Österreich und der EU**; Produktarten **Waren, Downloads und Dienstleistungen**; **altersbeschränkte Waren** (16+/18+) möglich. Anforderungen mit Quellen: `docs/shop/RECHTSRAHMEN-AT.md`.
- **Shop-Bereitschaftsprüfung:** Der Checkout bleibt gesperrt, bis alle Pflichtangaben und -seiten vorhanden sind – das CMS macht einen unvollständigen Shop technisch unmöglich, statt nur zu warnen.
- **Widerrufsbutton (§ 13a FAGG)** und **EU-Gewährleistungs-/Garantielabel** sind Pflicht-Bestandteile, keine Theme-Optionen: Themes müssen den Hook ausgeben.
- **EU-Steuern:** bei aktivem OSS Steuersatz des Lieferlands; **einheitliche Bruttopreise** in allen Ländern (die USt wird je Zielland herausgerechnet) – einfachste Preisdarstellung, kein Preis-Umrechnen nach Adresseingabe. Kleinunternehmerregelung als Schalter.
- Steuersätze und 30-Tage-Tiefstpreis: konfigurierbar bzw. automatisch aus dem Preisverlauf (vorhanden).
- Rechnung: festes Layout mit den Pflichtangaben einer Rechnung nach UStG. PDF-Erzeugung mit einem **eigenen minimalen PDF-Schreiber** (ca. 150 Zeilen, Standardschrift Helvetica mit WinAnsi-Kodierung – deckt ä ö ü ß € ab). Grund: Die verbreiteten Go-Bibliotheken der fpdf-Linie sind archiviert; eine Rechnung braucht nur Text und eine Tabelle.
- Im Admin ein deutlicher Hinweis: Steuer- und Rechtswerte sind vom Betreiber zu prüfen; STYRIXA leistet keine Rechts- oder Steuerberatung.

**Warum:** Eine Zahlungsintegration statt drei, keine Rechtstext-Engine. Das rechtliche Restrisiko liegt – wie bei jedem Shop-System – beim Betreiber und wird klar kommuniziert.

## 008 – Shared-Hoster ohne CGI

**Entscheidung (umgesetzt):** Das FTP-Deployment macht **zuerst einen Hoster-Check** in einem Prüfordner (`_styrixa-check-…`): CGI direkt, dann über mod_rewrite. Erst wenn beides funktioniert, wird die Website angefasst. Bei Fehlschlag bleibt der Webspace unverändert, der Kunde erhält eine konkrete Diagnose („kein CGI“ bzw. „mod_rewrite fehlt“) und die Empfehlung SSH. Das geprüfte Programm wird übernommen, kein zweiter Upload.

**Warum:** Ersetzt die manuell gepflegte Hoster-Kompatibilitätsliste durch eine automatische Prüfung bei jedem Kunden.

**Ergänzung 26.11.2026:** Fehlt CGI, prüft der Hoster-Check als Nächstes die PHP-Brücke (ADR-012) und wählt sie automatisch. Erst wenn auch sie nicht geht, kommt die Diagnose mit Empfehlung SSH/vServer.

## 009 – Nur reine Go-Abhängigkeiten

**Entscheidung (umgesetzt):** `scripts/check-cgo.sh` (`make cgo`) bricht ab, sobald eine Nicht-Standard-Abhängigkeit C-Code enthält; die CI baut zusätzlich alle Kombinationen mit `CGO_ENABLED=0` für alle Plattformen und prüft, dass Kombinationen mit fehlender Abhängigkeit scheitern. Neue Abhängigkeiten nur mit Eintrag hier.

Erlaubte Abhängigkeiten: `modernc.org/sqlite`, `golang.org/x/crypto`. Sonst nur Standardbibliothek. Vendort (JS, ohne Build-Schritt): htmx, Editor.js.

## 010 – Tests

**Entscheidung:** Go-Handler-Tests (`httptest`) für alle Abläufe + Integrationstests gegen echte Protokolle (FTP/SSH/SQLite) + **ein** Smoke-Test, der ein gebautes Binary startet, `/install` durchläuft und eine Seite speichert. Keine Playwright-Suite (Browser-Tests sind teuer in Pflege und CI-Zeit).

## 011 – Factory-Konten und Abrechnung

**Entscheidung:** Login per **Magic-Link** (E-Mail mit Einmal-Link, keine Passwörter, kein Passwort-Reset). Abrechnung über **Stripe Checkout** – der Code aus dem Shop wird wiederverwendet.

**Offen (deine Entscheidung):** Geschäftsmodell – kostenlos für STYRIXA-Kunden, Einmalpreis pro CMS oder Abo (inkl. Updates). Günstigste Variante zum Start: **nur für STYRIXA-Kunden, Freischaltung von Hand** – dann entfällt Session 6.4 vorerst ganz.

## 012 – PHP-Brücke für Webspaces ohne CGI (z. B. easyname)

**Anlass:** Test 26.11.2026 auf easyname-Webhosting: `.htaccess` mit `Options +ExecCGI` liefert HTTP 500 – der Tarif führt kein CGI aus. Viele österreichische Shared-Hoster bieten nur PHP.

**Entscheidung:** Ist CGI nicht verfügbar, wird das CMS über eine **PHP-Brücke** betrieben: eine kleine `index.php` (ca. 80 Zeilen, keine Anwendungslogik) startet das unveränderte Go-Binary pro Anfrage per `proc_open` und spricht mit ihm genau das CGI-Protokoll, das das CMS schon kann (`GATEWAY_INTERFACE`).

- Die Brücke startet **nur das eine, beim Deploy festgelegte Binary** (absoluter Pfad, `proc_open` mit Array → keine Shell, keine Eingaben im Befehl). Anfrage-Kopfzeilen werden als `HTTP_*` übergeben, der Body per stdin, die Antwort (Status, Header, Body) zurückgegeben.
- Binary und Datenordner liegen **außerhalb des Document-Roots**, wenn der FTP-Zugang das erlaubt; sonst im per `.htaccess` gesperrten `styrixa-data/`.
- `.htaccess` leitet alle Anfragen, die keine Datei sind, an `index.php` (mod_rewrite, sonst `FallbackResource`).
- Der Hoster-Check wählt automatisch: **CGI → PHP-Brücke → Diagnose**. Die Brücke wird wie CGI vorher im Prüfordner getestet; die Website bleibt bis dahin unberührt.
- Alle Regeln aus CLAUDE.md Nr. 4 (CGI-tauglich) gelten unverändert; es entsteht **kein PHP-Code mit Logik**, keine zweite Codebasis.

**Voraussetzung (vor der Umsetzung prüfen):** `tools/hoster-probe/styrixa-probe.php` meldet auf dem Webspace `proc_open: ja`, `exec_own_binary: ja` und SQLite-WAL nicht `NEIN`. Schlägt die Probe fehl, ist dieser Tarif nicht geeignet → **easyname vServer** (SSH-Deployment, schon fertig) oder ein Webspace mit CGI.

**Warum:** Günstigste Lösung, die die verbreitetsten PHP-Webspaces abdeckt, ohne das CMS in PHP neu zu schreiben. Kosten: ein Prozessstart pro Anfrage über PHP (vergleichbar mit CGI).
