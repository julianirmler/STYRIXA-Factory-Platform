package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Login, Sessions, CSRF und Passwort-Reset des Admin-Bereichs (Session 1.3).
// Alles liegt in SQLite – im CGI-Modus lebt kein Prozess länger als eine Anfrage.

const (
	SessionCookie = "styrixa_session"
	CSRFField     = "csrf"         // verstecktes Formularfeld
	CSRFHeader    = "X-CSRF-Token" // für htmx: hx-headers='{"X-CSRF-Token": "…"}'

	PathLogin  = "/admin/login"
	PathLogout = "/admin/logout"
	PathForgot = "/admin/passwort-vergessen"
	PathReset  = "/admin/passwort-neu"

	sessionIdle      = 12 * time.Hour
	sessionMax       = 30 * 24 * time.Hour
	lastSeenInterval = time.Minute // last_seen seltener schreiben spart im CGI-Modus Schreibzugriffe
	resetTTL         = 30 * time.Minute

	msgBadLogin = "E-Mail oder Passwort ist falsch."
	msgBadCSRF  = "Sicherheitsprüfung fehlgeschlagen. Bitte die Seite neu laden und erneut versuchen."
	msgInternal = "Interner Fehler – bitte später erneut versuchen."
)

// dummyHash12 ist ein bcrypt-Hash (Kosten 12) eines zufälligen, verworfenen
// Passworts. Bei unbekannter E-Mail wird dagegen geprüft, damit die Antwortzeit
// nicht verrät, ob es das Konto gibt.
const dummyHash12 = "$2a$12$p9wB1AAPocQzMQy/G0ySqOZdGK0ivnhqGW778QqzzxKKn3GTP5T5G"

//go:embed templates/auth.html
var authTemplates embed.FS

var authTmpl = template.Must(template.ParseFS(authTemplates, "templates/auth.html"))

// Auth stellt Login/Logout/Reset bereit und schützt /admin/*.
type Auth struct {
	DB         *sql.DB
	Settings   Settings
	Mail       Mailer           // nil = kein Reset-Versand
	BcryptCost int              // 0 = 12; Tests setzen bcrypt.MinCost
	Now        func() time.Time // nil = time.Now; Tests verschieben die Zeit
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func bcryptCost(c int) int {
	if c == 0 {
		return defaultBcryptCost
	}
	return c
}

var (
	dummyMu     sync.Mutex
	dummyHashes = map[int][]byte{}
)

// dummyHash liefert einen Hash mit denselben Kosten wie echte Passwörter.
// Für die Standardkosten eine Konstante; andere Kosten (Tests) einmal erzeugt.
func dummyHash(cost int) []byte {
	if cost == defaultBcryptCost {
		return []byte(dummyHash12)
	}
	dummyMu.Lock()
	defer dummyMu.Unlock()
	if h, ok := dummyHashes[cost]; ok {
		return h
	}
	h, err := bcrypt.GenerateFromPassword([]byte(randomToken()), cost)
	if err != nil {
		return []byte(dummyHash12)
	}
	dummyHashes[cost] = h
	return h
}

// randomToken: 32 Byte aus crypto/rand, Base64url ohne Padding.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("styrixa: crypto/rand: " + err.Error()) // ohne Zufall keine sicheren Tokens
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// ---- Kontext für Handler hinter Require ----

type sessionCtxKey struct{}

type session struct {
	UserID int64
	CSRF   string
	hash   string
}

// CurrentUser liefert die ID des angemeldeten Admins (nur hinter Require).
func CurrentUser(ctx context.Context) (int64, bool) {
	s, ok := ctx.Value(sessionCtxKey{}).(*session)
	if !ok {
		return 0, false
	}
	return s.UserID, true
}

// CSRFToken liefert das CSRF-Token der Session für Formulare (Feld „csrf“)
// und htmx (Header X-CSRF-Token). Leer außerhalb von Require.
func CSRFToken(ctx context.Context) string {
	if s, ok := ctx.Value(sessionCtxKey{}).(*session); ok {
		return s.CSRF
	}
	return ""
}

// ---- Routen und Middleware ----

// Mount registriert Login, Logout, Reset und eine vorläufige Admin-Startseite
// (die Admin-Shell folgt in 1.4).
func (a *Auth) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+PathLogin, a.loginForm)
	mux.HandleFunc("POST "+PathLogin, a.login)
	mux.HandleFunc("POST "+PathLogout, a.logout)
	mux.HandleFunc("GET "+PathForgot, a.forgotForm)
	mux.HandleFunc("POST "+PathForgot, a.forgot)
	mux.HandleFunc("GET "+PathReset, a.resetForm)
	mux.HandleFunc("POST "+PathReset, a.reset)
	mux.HandleFunc("GET /admin", a.home)
	mux.HandleFunc("GET /admin/{$}", a.home)
}

func isAdminPath(p string) bool { return p == "/admin" || strings.HasPrefix(p, "/admin/") }

func isPublicAdminPath(p string) bool {
	return p == PathLogin || p == PathForgot || p == PathReset
}

// Require umschließt /admin/*: ohne gültige Session → Login; bei Nicht-GET
// zusätzlich das CSRF-Token der Session (Formularfeld oder Header).
// Andere Pfade reicht es unverändert durch.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isAdminPath(r.URL.Path) || isPublicAdminPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		s, err := a.lookup(r)
		if err != nil {
			slog.Error("auth: Session nicht lesbar", "err", err)
			http.Error(w, msgInternal, http.StatusInternalServerError)
			return
		}
		if s == nil {
			switch {
			case r.Header.Get("HX-Request") == "true":
				w.Header().Set("HX-Redirect", PathLogin)
				http.Error(w, "Bitte anmelden.", http.StatusUnauthorized)
			case r.Method == http.MethodGet || r.Method == http.MethodHead:
				http.Redirect(w, r, PathLogin, http.StatusSeeOther)
			default:
				http.Error(w, "Bitte anmelden.", http.StatusUnauthorized)
			}
			return
		}
		if !safeMethod(r.Method) && !csrfOK(r, s.CSRF) {
			http.Error(w, msgBadCSRF, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, s)))
	})
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func csrfOK(r *http.Request, want string) bool {
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.PostFormValue(CSRFField) // auch multipart (Uploads), Grenzen der Standardbibliothek
	}
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// lookup liest die Session aus dem Cookie. Abgelaufene Sessions werden gelöscht.
func (a *Auth) lookup(r *http.Request) (*session, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	ctx := r.Context()
	h := hashToken(c.Value)
	var s session
	var lastSeen, expires int64
	err = a.DB.QueryRowContext(ctx,
		`SELECT user_id, csrf, last_seen, expires_at FROM core_sessions WHERE token_hash = ?`, h).
		Scan(&s.UserID, &s.CSRF, &lastSeen, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := a.now().Unix()
	if now >= expires || now-lastSeen >= int64(sessionIdle/time.Second) {
		_, err := a.DB.ExecContext(ctx, `DELETE FROM core_sessions WHERE token_hash = ?`, h)
		return nil, err
	}
	if now-lastSeen >= int64(lastSeenInterval/time.Second) {
		if _, err := a.DB.ExecContext(ctx, `UPDATE core_sessions SET last_seen = ? WHERE token_hash = ?`, now, h); err != nil {
			return nil, err
		}
	}
	s.hash = h
	return &s, nil
}

func (a *Auth) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   !isLocalHost(r.Host), // lokal ohne TLS testbar, sonst immer Secure
		SameSite: http.SameSiteLaxMode,
	})
}

// ---- Login / Logout ----

type authPage struct {
	Title, Email, Error, Token, CSRF string
	Errors                           map[string]string
}

func (a *Auth) loginForm(w http.ResponseWriter, r *http.Request) {
	if s, err := a.lookup(r); err == nil && s != nil {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	a.render(w, http.StatusOK, "login", authPage{})
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Ungültiges Formular.", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	password := r.PostFormValue("password")
	ctx := r.Context()
	keys := limitKeys("", email, ClientIP(r))

	// Versuch vor der Passwortprüfung zählen – in einer Transaktion (IMMEDIATE),
	// damit parallele Anfragen das Limit nicht überholen.
	ipRow, blocked, err := a.reserveAttempt(ctx, keys)
	if err != nil {
		slog.Error("auth: Rate-Limit", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	if blocked {
		a.render(w, http.StatusTooManyRequests, "login", authPage{Email: email, Error: msgTooManyLogin})
		return
	}

	var userID int64
	var hash []byte
	err = a.DB.QueryRowContext(ctx, `SELECT id, password_hash FROM core_users WHERE email = ?`, email).Scan(&userID, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		userID, hash = 0, dummyHash(bcryptCost(a.BcryptCost))
	} else if err != nil {
		slog.Error("auth: Benutzer nicht lesbar", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	if len(password) > maxPasswordBytes {
		password = "" // bcrypt lehnt > 72 Bytes ab; trotzdem rechnen (gleiche Zeit)
	}
	ok := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil && userID != 0
	if !ok {
		a.render(w, http.StatusUnauthorized, "login", authPage{Email: email, Error: msgBadLogin})
		return
	}

	// Erfolg: Fehlversuche der E-Mail löschen, eigener IP-Eintrag zählt nicht.
	if err := clearAttempts(ctx, a.DB, keys[0].key); err != nil {
		slog.Warn("auth: Fehlversuche nicht gelöscht", "err", err)
	}
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM core_login_attempts WHERE rowid = ?`, ipRow); err != nil {
		slog.Warn("auth: Fehlversuch nicht gelöscht", "err", err)
	}
	// Session-Fixation: immer ein neues Token, ein mitgebrachtes wird verworfen.
	token, err := a.newSession(ctx, r, userID)
	if err != nil {
		slog.Error("auth: Session nicht anlegbar", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	a.setCookie(w, r, token, int(sessionMax/time.Second))
	slog.Info("auth: Login", "user", userID)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// reserveAttempt prüft das Limit und zählt den Versuch atomar. Liefert die
// rowid des IP-Eintrags (für das Zurücknehmen bei Erfolg).
func (a *Auth) reserveAttempt(ctx context.Context, keys []limitKey) (ipRow int64, blocked bool, err error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	now := a.now()
	if blocked, err = tooMany(ctx, tx, now, keys); err != nil || blocked {
		return 0, blocked, err
	}
	if err = recordAttempt(ctx, tx, now, keys); err != nil {
		return 0, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT max(rowid) FROM core_login_attempts WHERE key = ?`, keys[1].key).Scan(&ipRow); err != nil {
		return 0, false, err
	}
	return ipRow, false, tx.Commit()
}

func (a *Auth) newSession(ctx context.Context, r *http.Request, userID int64) (string, error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
		if _, err := tx.ExecContext(ctx, `DELETE FROM core_sessions WHERE token_hash = ?`, hashToken(c.Value)); err != nil {
			return "", err
		}
	}
	now := a.now().Unix()
	// abgelaufene Sessions des Benutzers mit aufräumen (kein Scheduler)
	if _, err := tx.ExecContext(ctx, `DELETE FROM core_sessions WHERE user_id = ? AND (expires_at <= ? OR last_seen <= ?)`,
		userID, now, now-int64(sessionIdle/time.Second)); err != nil {
		return "", err
	}
	token := randomToken()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO core_sessions (token_hash, user_id, created_at, last_seen, expires_at, csrf) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, now, now, now+int64(sessionMax/time.Second), randomToken()); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// logout liegt hinter Require (Session + CSRF nötig).
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if s, ok := r.Context().Value(sessionCtxKey{}).(*session); ok {
		if _, err := a.DB.ExecContext(r.Context(), `DELETE FROM core_sessions WHERE token_hash = ?`, s.hash); err != nil {
			slog.Error("auth: Logout", "err", err)
			http.Error(w, msgInternal, http.StatusInternalServerError)
			return
		}
	}
	a.setCookie(w, r, "", -1)
	http.Redirect(w, r, PathLogin, http.StatusSeeOther)
}

// home ist die vorläufige Admin-Startseite bis zur Admin-Shell (1.4).
func (a *Auth) home(w http.ResponseWriter, r *http.Request) {
	id, _ := CurrentUser(r.Context())
	var email string
	if err := a.DB.QueryRowContext(r.Context(), `SELECT email FROM core_users WHERE id = ?`, id).Scan(&email); err != nil {
		slog.Error("auth: Benutzer nicht lesbar", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusOK, "home", authPage{Email: email, CSRF: CSRFToken(r.Context())})
}

// ---- Passwort ----

// SetPassword setzt ein neues Passwort und beendet alle Sessions des Benutzers.
func (a *Auth) SetPassword(ctx context.Context, userID int64, password string) error {
	if field, msg := validatePassword(password, password); field != "" {
		return errors.New(msg)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost(a.BcryptCost))
	if err != nil {
		return err
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setPasswordTx(ctx, tx, userID, string(hash)); err != nil {
		return err
	}
	return tx.Commit()
}

func setPasswordTx(ctx context.Context, tx *sql.Tx, userID int64, hash string) error {
	res, err := tx.ExecContext(ctx, `UPDATE core_users SET password_hash = ? WHERE id = ?`, hash, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("auth: Benutzer %d nicht gefunden", userID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM core_sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM core_password_resets WHERE user_id = ?`, userID)
	return err
}

// ---- Passwort-Reset ----

func (a *Auth) forgotForm(w http.ResponseWriter, r *http.Request) {
	a.render(w, http.StatusOK, "forgot", authPage{})
}

// forgot antwortet immer gleich – egal, ob die E-Mail existiert oder die Mail
// verschickt werden konnte.
func (a *Auth) forgot(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Ungültiges Formular.", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(r.PostFormValue("email"))
	ctx := r.Context()
	_, blocked, err := a.reserveAttempt(ctx, limitKeys("reset-", email, ClientIP(r)))
	if err != nil {
		slog.Error("auth: Rate-Limit", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	if blocked {
		a.render(w, http.StatusTooManyRequests, "forgot", authPage{Email: email, Error: msgTooManyReset})
		return
	}
	if err := a.sendReset(ctx, email); err != nil {
		slog.Error("auth: Reset-Mail nicht gesendet", "err", err) // nie Token oder Zugangsdaten
	}
	a.render(w, http.StatusOK, "forgot-sent", authPage{})
}

func (a *Auth) sendReset(ctx context.Context, email string) error {
	if email == "" {
		return nil
	}
	var userID int64
	var addr string
	err := a.DB.QueryRowContext(ctx, `SELECT id, email FROM core_users WHERE email = ?`, email).Scan(&userID, &addr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	base, err := a.Settings.Get(ctx, KeySiteURL)
	if err != nil {
		return err
	}
	if base == "" || a.Mail == nil {
		return errors.New("site.url oder Mailversand nicht eingerichtet")
	}
	token := randomToken()
	now := a.now().Unix()
	if _, err := a.DB.ExecContext(ctx, `DELETE FROM core_password_resets WHERE expires_at <= ?`, now); err != nil {
		return err
	}
	if _, err := a.DB.ExecContext(ctx,
		`INSERT INTO core_password_resets (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		hashToken(token), userID, now+int64(resetTTL/time.Second)); err != nil {
		return err
	}
	link := strings.TrimRight(base, "/") + PathReset + "?token=" + url.QueryEscape(token)
	title, _ := a.Settings.Get(ctx, KeySiteTitle)
	body := "Hallo,\n\nfür das Admin-Konto " + addr + " auf „" + title + "“ wurde ein neues Passwort angefordert.\n\n" +
		"Über diesen Link kannst du es innerhalb von 30 Minuten einmalig festlegen:\n\n" + link + "\n\n" +
		"Wenn du das nicht warst, ignoriere diese Mail – dein Passwort bleibt unverändert.\n"
	return a.Mail.Send(ctx, addr, "Passwort zurücksetzen", body)
}

// resetValid prüft ein Token, ohne es zu verbrauchen (für die Formularanzeige).
func (a *Auth) resetValid(ctx context.Context, token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	var n int
	err := a.DB.QueryRowContext(ctx,
		`SELECT count(*) FROM core_password_resets WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashToken(token), a.now().Unix()).Scan(&n)
	return n == 1, err
}

func (a *Auth) resetForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	ok, err := a.resetValid(r.Context(), token)
	if err != nil {
		slog.Error("auth: Reset-Token nicht lesbar", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	if !ok {
		a.render(w, http.StatusNotFound, "reset-invalid", authPage{})
		return
	}
	a.render(w, http.StatusOK, "reset", authPage{Token: token})
}

var errResetInvalid = errors.New("reset-token ungültig")

func (a *Auth) reset(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Ungültiges Formular.", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	token := r.PostFormValue("token")
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	if field, msg := validatePassword(pw, pw2); field != "" {
		ok, err := a.resetValid(ctx, token)
		if err != nil || !ok {
			a.render(w, http.StatusNotFound, "reset-invalid", authPage{})
			return
		}
		a.render(w, http.StatusUnprocessableEntity, "reset", authPage{Token: token, Errors: map[string]string{field: msg}})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost(a.BcryptCost))
	if err != nil {
		slog.Error("auth: bcrypt", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	switch err := a.redeemReset(ctx, token, string(hash)); {
	case errors.Is(err, errResetInvalid):
		a.render(w, http.StatusNotFound, "reset-invalid", authPage{})
		return
	case err != nil:
		slog.Error("auth: Reset", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	a.setCookie(w, r, "", -1)
	slog.Info("auth: Passwort per Reset geändert")
	a.render(w, http.StatusOK, "reset-done", authPage{})
}

// redeemReset verbraucht das Token und setzt das Passwort in einer Transaktion.
func (a *Auth) redeemReset(ctx context.Context, token, hash string) error {
	if token == "" {
		return errResetInvalid
	}
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := a.now().Unix()
	var userID int64
	err = tx.QueryRowContext(ctx,
		`UPDATE core_password_resets SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ? RETURNING user_id`,
		now, hashToken(token), now).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return errResetInvalid
	}
	if err != nil {
		return err
	}
	if err := setPasswordTx(ctx, tx, userID, hash); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *Auth) render(w http.ResponseWriter, status int, name string, p authPage) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer") // Reset-Token in der URL nicht weitergeben
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := authTmpl.ExecuteTemplate(w, name, p); err != nil {
		slog.Error("auth: Template", "err", err)
	}
}
