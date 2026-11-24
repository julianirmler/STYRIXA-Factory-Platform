# factory-api

Go-Dienst der STYRIXA Factory. Baut ausschließlich mit `CGO_ENABLED=0`.

- `internal/deploy` – Deployment fertiger Builds per **SSH** (Release-Ordner, systemd, Rollback) oder **FTP(S)** (CGI-Modus für Shared-Webspace). Eigener schlanker FTPS-Client (wegen `SITE CHMOD`), einzige Abhängigkeit: `golang.org/x/crypto/ssh`.
- `internal/api` – `POST /api/builds/{id}/deploy`, Log-Stream per Server-Sent Events.
- `cmd/factory-api` – Server (`--addr`, `--builds`). Build-Endpunkt folgt.

**Wichtig:** `go.sum` ist bewusst nicht eingecheckt. Einmal `go mod tidy` ausführen – dabei werden die Modul-Checksummen gegen sum.golang.org verifiziert. Danach `go.sum` committen.

Vor Produktion: Kunden-Authentifizierung vor `/api/*` (TODO in `cmd/factory-api/main.go`).
