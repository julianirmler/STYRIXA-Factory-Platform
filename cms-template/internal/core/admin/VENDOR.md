# Vendorte Dateien der Admin-Shell

| Datei | Herkunft | Version | Lizenz | SHA-256 |
|---|---|---|---|---|
| `static/htmx-2.0.11.min.js` | npm `htmx.org@2.0.11`, `dist/htmx.min.js` (unverändert) | 2.0.11 | 0BSD | `d6fdc75f204e6bdefa99b69bf1e6d4ac69b8a364f77929f45c13476b4000f717` |

Die Datei bleibt byte-identisch zum Original, damit die Prüfsumme nachprüfbar ist.

Prüfen:

```bash
shasum -a 256 internal/core/admin/static/htmx-2.0.11.min.js
```

Aktualisieren: `npm pack htmx.org@<version>` (npm prüft die Registry-Integrität), `package/dist/htmx.min.js` als `static/htmx-<version>.min.js` ablegen, alte Datei löschen, Konstante `htmxFile` in `internal/core/admin.go` und diese Tabelle anpassen. Der Dateiname trägt die Version, die ausgelieferte URL zusätzlich einen Inhalts-Hash – Browser-Caches (`immutable`) laden nach einem Update also sicher neu.

htmx-Konfiguration (`<meta name="htmx-config">` im Layout): `includeIndicatorStyles: false` (keine Inline-Styles, die CSP erlaubt nur `'self'`), `allowEval: false`, `allowScriptTags: false`.
