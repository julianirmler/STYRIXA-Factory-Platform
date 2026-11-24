# Phase 0 – Fundament (Session 0.1)

Diese Datei wird über `CLAUDE.md` geladen, solange Phase 0 aktiv ist. Sie ergänzt die Projektregeln, ersetzt sie nicht.

**Modell:** Sonnet reicht. **Umfang:** eine Session. **Ziel:** CI auf GitHub grün und ein erster signierter Test-Release `v0.0.1` mit allen vorgebauten Paketen.

## Ausgangslage

Bereits vorhanden (nicht neu bauen, nur benutzen):
- `Makefile` (`make check`), `scripts/check-cgo.sh`, `scripts/matrix-build.sh`
- `.github/workflows/ci.yml` und `.github/workflows/release.yml`
- `cms-template/modules.json`, `cms-template/tools/matrix`, `cms-template/tools/sign`

Was fehlt:
1. `factory-api/go.sum` (wurde bewusst nicht ungeprüft committet)
2. Release-Signaturschlüssel: geheimer Teil als GitHub-Secret, öffentlicher Teil im CMS
3. Prüffunktion im CMS-Kern für signierte Releases (Grundlage für 2.1 und 6.3)
4. Erster Test-Release

## Sicherheitsregeln dieser Phase (zusätzlich zu CLAUDE.md)

- **Den geheimen Schlüssel niemals anzeigen, lesen oder ausgeben** – kein `cat`, kein `echo`, nicht in Logs, Commits oder Chat. Er geht ausschließlich per Umleitung (`<`) von der Datei an `gh secret set`.
- Die Schlüsseldatei liegt **außerhalb des Repos** (`../styrixa-signing.secret`) und wird nie committet.
- Bevor die Schlüsseldatei gelöscht wird: **den Nutzer fragen**, ob er sie gesichert hat (Passwortmanager). Ohne Sicherung lassen sich installierte CMS später nicht mehr mit Updates versorgen.
- `go.sum` nur über normalen `go mod tidy` mit Standard-`GOPROXY`/`GOSUMDB` erzeugen. Niemals `GONOSUMDB`, `GONOSUMCHECK`, `GOFLAGS=-modfile` oder `replace`-Tricks.
- Kein neues Feature, keine neue Abhängigkeit, keine Arbeit an späteren Sessions.

## Schritte

### 1. Voraussetzungen prüfen
```bash
go version            # >= 1.23
gh auth status        # angemeldet, Zugriff auf julianirmler/styrixa-factory-platform
python3 -c "import pyftpdlib" || pip install pyftpdlib pyopenssl
git status            # sauber, auf main
```
Fehlt etwas: dem Nutzer sagen, was zu tun ist, und warten.

### 2. `go.sum` erzeugen und `x/crypto` aktualisieren
```bash
cd factory-api
go get golang.org/x/crypto@latest
go mod tidy
```
- Erhöht `go get` die `go`-Zeile in `go.mod`, ist das in Ordnung (CI nutzt `go-version: stable`). Notieren.
- Danach `go test ./...` in `factory-api`.

### 3. Lokal alles grün
```bash
make check
```
Muss vollständig grün sein: Tests, `cgo-check` für beide Module, 36 Kombinationen gebaut, alle erwarteten Fehlschläge („(erwartet)“), gofmt, UI-Syntax.

### 4. Schlüsselpaar erzeugen und Secret setzen
```bash
cd cms-template
go run ./tools/sign -keygen -secret-file ../../styrixa-signing.secret
# Ausgabe = ÖFFENTLICHER Schlüssel (Base64, 44 Zeichen) → für Schritt 5 merken
gh secret set STYRIXA_SIGNING_KEY < ../../styrixa-signing.secret
gh secret list        # STYRIXA_SIGNING_KEY muss erscheinen
```
Dann den Nutzer bitten, `styrixa-signing.secret` (liegt neben dem Repo-Ordner) im Passwortmanager zu sichern. Erst nach seiner Bestätigung: `rm ../../styrixa-signing.secret`.

### 5. Öffentlichen Schlüssel + Prüffunktion im CMS-Kern
Neue Datei `cms-template/internal/core/release.go`:
- `const ReleasePublicKey = "<öffentlicher Schlüssel aus Schritt 4>"`
- `func VerifyRelease(sums, sigB64 []byte) error` – prüft `SHA256SUMS` gegen `SHA256SUMS.sig` (ed25519, nur Standardbibliothek)
- `func ChecksumFor(sums []byte, filename string) (string, error)` – liest den SHA-256 eines Pakets aus `SHA256SUMS`

Test `release_test.go` (tabellengetrieben), mit einem **im Test erzeugten** Schlüsselpaar (nicht dem echten):
- gültige Signatur → ok
- veränderte `SHA256SUMS` → Fehler
- falscher Schlüssel, leere/kaputte Signatur → Fehler
- `ChecksumFor`: vorhanden, fehlend, Format `sha256sum` (zwei Leerzeichen)

Dazu ein Test, der sicherstellt, dass `ReleasePublicKey` gültiges Base64 mit 32 Byte ist.

### 6. Committen und CI prüfen
```bash
make check
git add factory-api/go.mod factory-api/go.sum cms-template/internal/core/release.go cms-template/internal/core/release_test.go
git commit -m "Phase 0: go.sum, Release-Schlüssel, Signaturprüfung im Kern"
git push origin main
gh run watch          # CI muss grün werden (check + cross für 4 Plattformen)
```
Schlägt die CI fehl: Ursache aus `gh run view --log-failed` beheben, nicht Tests abschwächen.

### 7. Test-Release `v0.0.1`
```bash
git tag v0.0.1
git push origin v0.0.1
gh run watch          # Workflow „Release“
```
Prüfen:
```bash
mkdir -p /tmp/stx-release && cd /tmp/stx-release
gh release download v0.0.1 -R julianirmler/styrixa-factory-platform -p 'SHA256SUMS*' -p 'modules.json'
gh release view v0.0.1 -R julianirmler/styrixa-factory-platform --json assets --jq '[.assets[].name | select(endswith(".tar.gz"))] | length'   # 144
go run <repo>/cms-template/tools/sign -verify -pub "<öffentlicher Schlüssel>" SHA256SUMS                                                         # gültig
gh release download v0.0.1 -R julianirmler/styrixa-factory-platform -p 'styrixa_media-shop_linux_amd64.tar.gz'
sha256sum -c --ignore-missing SHA256SUMS                                                                                                     # OK
tar -xzf styrixa_media-shop_linux_amd64.tar.gz && (STYRIXA_DATA=./data ./styrixa --addr 127.0.0.1:18080 & sleep 1; curl -fsS 127.0.0.1:18080/healthz; kill %1)
```

## Abnahme (alles muss erfüllt sein)

- [ ] `factory-api/go.sum` committet, erzeugt ohne Prüf-Umgehung
- [ ] `make check` lokal grün
- [ ] CI-Workflow auf GitHub grün (Job `check` + 4 × `cross`)
- [ ] Secret `STYRIXA_SIGNING_KEY` gesetzt; Schlüsseldatei vom Nutzer gesichert und lokal gelöscht
- [ ] `core.ReleasePublicKey` + `VerifyRelease` + `ChecksumFor` mit Tests
- [ ] Release `v0.0.1`: 144 Pakete, `SHA256SUMS`, `SHA256SUMS.sig`, `modules.json`
- [ ] Signatur mit `tools/sign -verify` gültig, Prüfsumme eines Pakets stimmt, Binary antwortet auf `/healthz`

## Abschluss

1. In `docs/UMSETZUNGSPLAN.md` Session **0.1** abhaken, Abweichungen notieren (z. B. geänderte `go`-Version).
2. In `CLAUDE.md` die Zeile unter „Aktuelle Phase“ auf `@docs/phasen/PHASE-1.md` umstellen (Datei existiert ggf. noch nicht – dann den Nutzer informieren, dass sie vor Phase 1 angelegt werden muss).
3. Committen, pushen, `/clear`.

## Bekannte Stolpersteine

| Problem | Lösung |
|---|---|
| `gh secret set` → 403 | `gh auth refresh -s repo` bzw. Token mit Repo-Rechten |
| Release-Job: „Resource not accessible by integration“ | Repo → Settings → Actions → General → Workflow permissions: „Read and write“ |
| `go mod tidy` will `go`-Version anheben | erlauben; CI nutzt `stable` |
| Release-Job läuft lange | normal (36 Builds je Plattform, parallel); bei privatem Repo zählen Actions-Minuten zum Kontingent |
| FTP-Tests in CI scheitern | Python-Setup im Workflow prüfen (`pyftpdlib pyopenssl`) |
| Tag versehentlich falsch | `gh release delete v0.0.1 --yes && git push --delete origin v0.0.1 && git tag -d v0.0.1`, dann neu |
