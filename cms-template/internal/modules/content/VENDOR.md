# Vendorte Dateien des Content-Moduls (Editor.js, ADR-003)

Nur UMD-Builds, byte-identisch zum npm-Paket (Pfad `package/dist/…`), Version im Dateinamen. Sie liegen per `embed` im Content-Paket – ohne `mod_content` sind sie nicht im Binary. `TestVendoredFilesMatchChecksums` prüft die Tabelle gegen die Dateien.

| Datei | Herkunft | Version | Lizenz | SHA-256 |
|---|---|---|---|---|
| `static/editorjs-2.31.7.umd.js` | npm `@editorjs/editorjs@2.31.7`, `dist/editorjs.umd.js` | 2.31.7 | Apache-2.0 | `2eb726ea7fd85055be6c9b236b1276631867dab8c55bf16459ea2595d9be47db` |
| `static/editorjs-header-2.8.9.umd.js` | npm `@editorjs/header@2.8.9`, `dist/header.umd.js` | 2.8.9 | MIT | `4a41c871c64dee885fe3eedd26654a759aac772cc974e4e9cded8cc944a35c57` |
| `static/editorjs-list-2.0.9.umd.js` | npm `@editorjs/list@2.0.9`, `dist/editorjs-list.umd.js` | 2.0.9 | MIT | `debc67131a021fc794652fabe1cab901eec73abfdc8859be51d612a3e709886e` |
| `static/editorjs-quote-2.7.6.umd.js` | npm `@editorjs/quote@2.7.6`, `dist/quote.umd.js` | 2.7.6 | MIT | `5148cdbf4318d30da8b1756b004e899773761755e143ab986fffc848328f41ee` |
| `static/editorjs-delimiter-1.4.2.umd.js` | npm `@editorjs/delimiter@1.4.2`, `dist/delimiter.umd.js` | 1.4.2 | MIT | `89db0b30a837bc06a9fd0b800793c207b9e84cf89a88763225b5989f4da1036e` |
| `static/editorjs-raw-2.5.1.umd.js` | npm `@editorjs/raw@2.5.1`, `dist/raw.umd.js` | 2.5.1 | MIT | `b8ab49704c7da336fd589e83953dd4ceb52a34e4ac132a65bc8b6714107dcf92` |
| `static/editorjs-marker-1.4.0.umd.js` | npm `@editorjs/marker@1.4.0`, `dist/marker.umd.js` | 1.4.0 | MIT | `2e00d5c67aa28122f24aecb1c5baf68b9f363fbcd8edb606ea00e0ad64459c39` |
| `static/editorjs-inline-code-1.5.2.umd.js` | npm `@editorjs/inline-code@1.5.2`, `dist/inline-code.umd.js` | 1.5.2 | MIT | `495eeb674d0848fd07810bd2335fa08c569ee05739bb15756d5774e433e32cd0` |

Eigene Dateien (nicht vendort): `static/editor.js` (Start, Speichern), `static/editor-blocks.js` (Bild, FAQ, Antwort zuerst, Call-to-Action, Liste ohne Checkliste), `static/editor-nonce.js` (CSP-Brücke), `static/editor.css`.

Prüfen:

```bash
cd internal/modules/content && shasum -a 256 static/editorjs-*.umd.js
```

Aktualisieren: `npm pack @editorjs/<paket>@<version>` (npm prüft die Registry-Integrität), die UMD-Datei aus `package/dist/` unter neuem Versionsnamen ablegen, alte löschen, `templates/page.html` und diese Tabelle anpassen. Vorher prüfen: kein `eval`/`new Function` (Admin-CSP erlaubt kein `unsafe-eval`).

## CSP

Alle Pakete fügen ihr CSS zur Laufzeit als `<style>` ein. Die Editor-Seite setzt deshalb `AdminPage.StyleNonce`: Der Kern erzeugt pro Anfrage eine Nonce und ergänzt die Admin-CSP um `style-src 'self' 'nonce-…'` – kein `unsafe-inline`, Skripte bleiben auf `'self'`. Editor.js bekommt die Nonce über `style.nonce`; den Plugins gibt `editor-nonce.js` sie beim Einfügen in `<head>` mit (auch später nachgeladene Styles wie die Tooltips).

Die Checkliste des List-Plugins rendert der Server nicht (400); `editor-blocks.js` blendet sie per Unterklasse aus Toolbox und Einstellungen aus.
