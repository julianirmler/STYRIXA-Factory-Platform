# factory-api

Go-Dienst der STYRIXA Factory. Baut ausschließlich mit `CGO_ENABLED=0`. Die Factory baut nicht selbst: Sie liefert die vorgebauten, signierten Pakete des neuesten GitHub-Releases aus (ADR-004).

- `internal/catalog`: Release-Katalog. Er liest `releases/latest`, prüft `SHA256SUMS` mit der ed25519-Signatur `SHA256SUMS.sig` (`ReleasePublicKey`, identisch zum CMS) und akzeptiert `modules.json` und jedes Paket nur mit passendem signiertem Hash. Pakete werden beim ersten Abruf geladen und geprüft und liegen dann unter `--cache/<sha256>.tar.gz`. Auch bei jedem Cache-Treffer wird erneut geprüft.
- `internal/deploy`: Deployment per **SSH** (Release-Ordner, systemd, Rollback) oder **FTP(S)** (CGI-Modus für Shared-Webspace). Eigener schlanker FTPS-Client (wegen `SITE CHMOD`), einzige Abhängigkeit: `golang.org/x/crypto/ssh`.
- `internal/api`: Deploy-Endpunkt, Log-Stream per Server-Sent Events.
- `cmd/factory-api`: Server (`--addr`, `--repo`, `--cache`; optional `GITHUB_TOKEN` für ein höheres API-Limit).

## Endpunkte

| Methode + Pfad | Ergebnis |
|---|---|
| `GET /api/catalog` | `{release, modules, targets}` aus der signierten `modules.json` |
| `GET /api/packages/select?modules=media,shop&target=linux/amd64` | passendes Paket (`id`, `sha256`, `size`, `modules`, `downloadUrl`). Pflichtmodule werden ergänzt; ungültige Auswahl → 422 |
| `GET /api/packages/{id}/download` | geprüftes tar.gz; Prüfsumme falsch → 502 |
| `POST /api/packages/{id}/deploy` | Deploy per SSH/FTPS (SSE), siehe `internal/api/deploy.go` |

Vor Produktion: Kunden-Authentifizierung vor `/api/*` (TODO in `cmd/factory-api/main.go`).
