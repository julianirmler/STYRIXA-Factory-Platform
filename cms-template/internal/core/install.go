package core

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base32"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// InstallCodeFile liegt im Datenordner und enthält den Einmal-Code für /install.
// Der Deploy der Factory schreibt ihn (und zeigt ihn dem Kunden an); fehlt er,
// erzeugt das CMS ihn selbst. Nach der Installation wird er gelöscht.
const InstallCodeFile = "install-code"

const (
	minPasswordRunes  = 12
	maxPasswordBytes  = 72 // bcrypt verarbeitet nur 72 Bytes
	defaultBcryptCost = 12
	maxFormBytes      = 64 << 10
)

var reGISA = regexp.MustCompile(`^[0-9]{1,12}$`)

//go:embed templates/install.html
var installTemplates embed.FS

var installTmpl = template.Must(template.ParseFS(installTemplates, "templates/install.html"))

// NewInstallCode erzeugt einen Einmal-Code der Form ABCD-EFGH-IJKL-MNOP
// (80 Bit aus crypto/rand, Base32 ohne verwechselbare Groß-/Kleinschreibung).
func NewInstallCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base32.StdEncoding.EncodeToString(b) // 16 Zeichen, ohne Padding
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16], nil
}

// NormalizeInstallCode entfernt Leerzeichen und Bindestriche und wandelt in
// Großbuchstaben – so ist die Eingabe tolerant gegenüber Abtippen.
func NormalizeInstallCode(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '-' || r == ' ' || r == '\t' || r == '\r' || r == '\n':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, s)
}

// EnsureInstallCode legt <dataDir>/install-code (0600) an, falls es fehlt, und
// liefert den Pfad. Starten mehrere Prozesse gleichzeitig, gewinnt einer (O_EXCL).
func EnsureInstallCode(dataDir string) (string, error) {
	path := filepath.Join(dataDir, InstallCodeFile)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	code, err := NewInstallCode()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("install-code: %w", err)
	}
	_, werr := f.WriteString(code + "\n")
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return "", fmt.Errorf("install-code: %w", werr)
	}
	return path, nil
}

// Installer stellt den Einrichtungs-Assistenten unter /install bereit.
type Installer struct {
	DB         *sql.DB
	DataDir    string
	BcryptCost int // 0 = 12; Tests setzen bcrypt.MinCost
}

func (in *Installer) settings() Settings { return SettingsStore{DB: in.DB} }

// Mount registriert GET/POST /install. POST ist zusätzlich gegen
// Cross-Origin-Anfragen geschützt (Standardbibliothek, Go ≥ 1.25).
func (in *Installer) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /install", in.get)
	mux.Handle("POST /install", http.NewCrossOriginProtection().Handler(http.HandlerFunc(in.post)))
}

// RequireInstalled leitet vor Abschluss der Installation jede Seite außer
// /healthz und /install auf /install um.
func (in *Installer) RequireInstalled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/install" {
			next.ServeHTTP(w, r)
			return
		}
		ok, err := Installed(r.Context(), in.settings())
		switch {
		case err != nil:
			slog.Error("install: Status nicht lesbar", "err", err)
			http.Error(w, "Interner Fehler – bitte später erneut versuchen.", http.StatusInternalServerError)
		case ok:
			next.ServeHTTP(w, r)
		case r.Method == http.MethodGet || r.Method == http.MethodHead:
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, r, "/install", http.StatusSeeOther)
		default:
			http.Error(w, "Die Website ist noch nicht eingerichtet.", http.StatusServiceUnavailable)
		}
	})
}

type installForm struct {
	Code, Title, Region, GISA, Email string
	siteURL                          string // aus der Anfrage, für Links in Mails
	password, password2              string
}

type installPage struct {
	Form    installForm
	Regions []string
	Errors  map[string]string
}

func (in *Installer) get(w http.ResponseWriter, r *http.Request) {
	if in.installedOr404(w, r) {
		return
	}
	if _, err := EnsureInstallCode(in.DataDir); err != nil {
		slog.Error("install: Code-Datei nicht anlegbar", "err", err)
		http.Error(w, "Interner Fehler – der Datenordner ist nicht beschreibbar.", http.StatusInternalServerError)
		return
	}
	in.render(w, http.StatusOK, "form", installPage{Form: installForm{Region: Regions[0]}, Regions: Regions})
}

func (in *Installer) post(w http.ResponseWriter, r *http.Request) {
	if in.installedOr404(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Ungültiges Formular.", http.StatusBadRequest)
		return
	}
	f := installForm{
		Code:      strings.TrimSpace(r.PostFormValue("code")),
		Title:     strings.TrimSpace(r.PostFormValue("title")),
		Region:    r.PostFormValue("region"),
		GISA:      strings.TrimSpace(r.PostFormValue("gisa")),
		Email:     strings.TrimSpace(r.PostFormValue("email")),
		password:  r.PostFormValue("password"),
		password2: r.PostFormValue("password2"),
		// /install ist durch den Einmal-Code geschützt – Host dieser Anfrage ist vertrauenswürdig.
		siteURL: (&url.URL{Scheme: requestScheme(r), Host: r.Host}).String(),
	}
	errs := f.validate()
	if !in.codeMatches(f.Code) {
		errs["code"] = "Der Installations-Code stimmt nicht."
	}
	if len(errs) > 0 {
		// Hat inzwischen eine parallele Installation gewonnen (Code-Datei schon
		// gelöscht), nichts verraten: wie nach der Installation 404.
		if in.installedOr404(w, r) {
			return
		}
		in.render(w, http.StatusUnprocessableEntity, "form", installPage{Form: f, Regions: Regions, Errors: errs})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(f.password), bcryptCost(in.BcryptCost))
	if err != nil {
		slog.Error("install: bcrypt", "err", err)
		http.Error(w, "Interner Fehler – bitte erneut versuchen.", http.StatusInternalServerError)
		return
	}
	switch err := in.install(r.Context(), f, string(hash)); {
	case errors.Is(err, errAlreadyInstalled):
		http.NotFound(w, r)
		return
	case err != nil:
		slog.Error("install: Speichern fehlgeschlagen", "err", err)
		http.Error(w, "Interner Fehler – bitte erneut versuchen.", http.StatusInternalServerError)
		return
	}
	if err := os.Remove(filepath.Join(in.DataDir, InstallCodeFile)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("install: Code-Datei nicht gelöscht (ist nach der Installation wirkungslos)", "err", err)
	}
	slog.Info("install: abgeschlossen")
	in.render(w, http.StatusOK, "done", installPage{Form: f})
}

func (f installForm) validate() map[string]string {
	errs := map[string]string{}
	if f.Title == "" {
		errs["title"] = "Bitte einen Seitentitel angeben."
	} else if utf8.RuneCountInString(f.Title) > 200 {
		errs["title"] = "Der Seitentitel darf höchstens 200 Zeichen lang sein."
	}
	if !ValidRegion(f.Region) {
		errs["region"] = "Bitte eine gültige Region wählen."
	}
	if f.Region == "de-AT" && f.GISA != "" && !reGISA.MatchString(f.GISA) {
		errs["gisa"] = "Die GISA-Zahl besteht nur aus Ziffern."
	}
	if a, err := mail.ParseAddress(f.Email); err != nil || a.Name != "" || a.Address != f.Email || len(f.Email) > 254 {
		errs["email"] = "Bitte eine gültige E-Mail-Adresse angeben."
	}
	if field, msg := validatePassword(f.password, f.password2); field != "" {
		errs[field] = msg
	}
	return errs
}

// validatePassword prüft die Passwortregeln (Installation und Reset). Liefert
// das betroffene Feld („password“ oder „password2“) und die Meldung.
func validatePassword(pw, pw2 string) (field, msg string) {
	switch {
	case utf8.RuneCountInString(pw) < minPasswordRunes:
		return "password", fmt.Sprintf("Das Passwort muss mindestens %d Zeichen lang sein.", minPasswordRunes)
	case len(pw) > maxPasswordBytes:
		return "password", fmt.Sprintf("Das Passwort darf höchstens %d Bytes lang sein.", maxPasswordBytes)
	case pw != pw2:
		return "password2", "Die Passwörter stimmen nicht überein."
	}
	return "", ""
}

// codeMatches vergleicht in konstanter Zeit mit dem Inhalt der Code-Datei.
func (in *Installer) codeMatches(given string) bool {
	b, err := os.ReadFile(filepath.Join(in.DataDir, InstallCodeFile))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("install: Code-Datei nicht lesbar", "err", err)
		}
		return false
	}
	want := NormalizeInstallCode(string(b))
	got := NormalizeInstallCode(given)
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

var errAlreadyInstalled = errors.New("bereits installiert")

// install schreibt alles in einer Transaktion. BEGIN IMMEDIATE (DSN) serialisiert
// parallele Installationen; die Prüfung auf installed_at in derselben
// Transaktion lässt genau eine durch.
func (in *Installer) install(ctx context.Context, f installForm, hash string) (err error) {
	tx, err := in.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	done, err := getSetting(ctx, tx, KeyInstalledAt)
	if err != nil {
		return err
	}
	if done != "" {
		return errAlreadyInstalled
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO core_users (email, password_hash, role) VALUES (?, ?, ?)`, f.Email, hash, RoleAdmin); err != nil {
		return fmt.Errorf("core_users: %w", err)
	}
	gisa := ""
	if f.Region == "de-AT" {
		gisa = f.GISA
	}
	for _, kv := range [][2]string{
		{KeySiteTitle, f.Title},
		{KeySiteRegion, f.Region},
		{KeySiteGISA, gisa},
		{KeySiteURL, f.siteURL},
		{KeyInstalledAt, time.Now().UTC().Format(time.RFC3339)},
	} {
		if err = setSetting(ctx, tx, kv[0], kv[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (in *Installer) installedOr404(w http.ResponseWriter, r *http.Request) bool {
	ok, err := Installed(r.Context(), in.settings())
	if err != nil {
		slog.Error("install: Status nicht lesbar", "err", err)
		http.Error(w, "Interner Fehler – bitte später erneut versuchen.", http.StatusInternalServerError)
		return true
	}
	if ok {
		http.NotFound(w, r) // 404 statt 403: nicht verraten, dass es /install gibt
		return true
	}
	return false
}

func (in *Installer) render(w http.ResponseWriter, status int, name string, p installPage) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := installTmpl.ExecuteTemplate(w, name, p); err != nil {
		slog.Error("install: Template", "err", err)
	}
}
