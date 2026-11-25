package core

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	testEmail    = "admin@example.at"
	testPassword = "ein-langes-passwort"
)

type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

type sentMail struct{ to, subject, body string }

func (f *fakeMailer) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentMail{to, subject, body})
	return nil
}

type authEnv struct {
	a     *Auth
	h     http.Handler
	db    *sql.DB
	mail  *fakeMailer
	now   time.Time
	user  int64
	mutex sync.Mutex
}

func (e *authEnv) advance(d time.Duration) { e.mutex.Lock(); e.now = e.now.Add(d); e.mutex.Unlock() }

// newAuthEnv: frische DB, ein Admin, Handler-Kette wie in main.go.
func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	db := openTestDB(t, t.TempDir())
	ctx := context.Background()
	if err := Migrate(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	res, err := db.Exec(`INSERT INTO core_users (email, password_hash) VALUES (?, ?)`, testEmail, string(hash))
	if err != nil {
		t.Fatal(err)
	}
	st := SettingsStore{DB: db}
	_ = st.Set(ctx, KeySiteURL, "https://example.at")
	_ = st.Set(ctx, KeySiteTitle, "Tischlerei Muster")
	e := &authEnv{db: db, mail: &fakeMailer{}, now: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	e.user, _ = res.LastInsertId()
	e.a = &Auth{DB: db, Settings: st, Mail: e.mail, BcryptCost: bcrypt.MinCost,
		Now: func() time.Time { e.mutex.Lock(); defer e.mutex.Unlock(); return e.now }}
	mux := http.NewServeMux()
	e.a.Mount(mux)
	mux.HandleFunc("GET /admin/test", func(w http.ResponseWriter, r *http.Request) {
		id, _ := CurrentUser(r.Context())
		fmt.Fprintf(w, "user=%d", id)
	})
	mux.HandleFunc("POST /admin/test", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("geschrieben")) })
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("öffentlich")) })
	e.h = http.NewCrossOriginProtection().Handler(e.a.Require(mux))
	return e
}

type reqOpt struct {
	cookie string
	ip     string
	hdr    map[string]string
}

func (e *authEnv) req(method, path string, form url.Values, o reqOpt) *httptest.ResponseRecorder {
	body := ""
	if form != nil {
		body = form.Encode()
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "example.at"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if o.cookie != "" {
		r.AddCookie(&http.Cookie{Name: SessionCookie, Value: o.cookie})
	}
	if o.ip != "" {
		r.RemoteAddr = o.ip + ":40000"
	}
	for k, v := range o.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func (e *authEnv) login(t *testing.T, o reqOpt) string {
	t.Helper()
	w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}}, o)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("Login: %d %s", w.Code, w.Body)
	}
	c := sessionCookie(t, w)
	if c == nil || c.Value == "" {
		t.Fatal("Login: kein Session-Cookie")
	}
	return c.Value
}

func (e *authEnv) csrf(t *testing.T, token string) string {
	t.Helper()
	var csrf string
	if err := e.db.QueryRow(`SELECT csrf FROM core_sessions WHERE token_hash = ?`, hashToken(token)).Scan(&csrf); err != nil {
		t.Fatal(err)
	}
	return csrf
}

func TestLoginCookieAndStorage(t *testing.T) {
	e := newAuthEnv(t)
	w := e.req("POST", PathLogin, url.Values{"email": {"ADMIN@example.at"}, "password": {testPassword}}, reqOpt{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin" {
		t.Fatalf("Login (E-Mail in Großbuchstaben): %d %q", w.Code, w.Header().Get("Location"))
	}
	c := sessionCookie(t, w)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("Cookie-Attribute: %+v", c)
	}
	if len(c.Value) < 43 {
		t.Fatalf("Token zu kurz: %d", len(c.Value))
	}
	// In der DB nur der Hash, nie das Token
	var n int
	e.db.QueryRow(`SELECT count(*) FROM core_sessions WHERE token_hash = ?`, c.Value).Scan(&n)
	if n != 0 {
		t.Fatal("Token im Klartext gespeichert")
	}
	e.db.QueryRow(`SELECT count(*) FROM core_sessions WHERE token_hash = ?`, hashToken(c.Value)).Scan(&n)
	if n != 1 {
		t.Fatal("Session-Hash fehlt")
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: c.Value}); w.Code != 200 || w.Body.String() != fmt.Sprintf("user=%d", e.user) {
		t.Fatalf("mit Session: %d %s", w.Code, w.Body)
	}
	if w := e.req("GET", "/admin", nil, reqOpt{cookie: c.Value}); w.Code != 200 || !strings.Contains(w.Body.String(), testEmail) {
		t.Fatalf("Admin-Startseite: %d", w.Code)
	}
}

func TestSecureCookieOnlyOffForLocalhost(t *testing.T) {
	e := newAuthEnv(t)
	for _, tc := range []struct {
		host   string
		secure bool
	}{
		{"example.at", true}, {"localhost:8080", false}, {"127.0.0.1:8080", false}, {"[::1]:8080", false}, {"localhost.example.at", true},
	} {
		r := httptest.NewRequest("POST", PathLogin, strings.NewReader(url.Values{"email": {testEmail}, "password": {testPassword}}.Encode()))
		r.Host = tc.host
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, r)
		if c := sessionCookie(t, w); c == nil || c.Secure != tc.secure {
			t.Errorf("Host %s: Cookie %+v, Secure erwartet %v", tc.host, c, tc.secure)
		}
	}
}

func TestRequireWithoutSession(t *testing.T) {
	e := newAuthEnv(t)
	cases := []struct {
		name, method, path string
		hdr                map[string]string
		code               int
	}{
		{"GET → Login", "GET", "/admin/test", nil, http.StatusSeeOther},
		{"Admin-Start", "GET", "/admin", nil, http.StatusSeeOther},
		{"Modul-Route", "GET", "/admin/shop/orders", nil, http.StatusSeeOther},
		{"POST → 401", "POST", "/admin/test", nil, http.StatusUnauthorized},
		{"htmx → 401", "GET", "/admin/test", map[string]string{"HX-Request": "true"}, http.StatusUnauthorized},
		{"Login offen", "GET", PathLogin, nil, http.StatusOK},
		{"Reset-Anfrage offen", "GET", PathForgot, nil, http.StatusOK},
		{"öffentlich", "GET", "/", nil, http.StatusOK},
		{"unbekanntes Token", "GET", "/admin/test", map[string]string{"Cookie": SessionCookie + "=erfunden"}, http.StatusSeeOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := e.req(tc.method, tc.path, nil, reqOpt{hdr: tc.hdr})
			if w.Code != tc.code {
				t.Fatalf("%d, erwartet %d", w.Code, tc.code)
			}
			if tc.code == http.StatusSeeOther && w.Header().Get("Location") != PathLogin {
				t.Fatalf("Location %q", w.Header().Get("Location"))
			}
		})
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{hdr: map[string]string{"HX-Request": "true"}}); w.Header().Get("HX-Redirect") != PathLogin {
		t.Fatal("htmx: HX-Redirect fehlt")
	}
}

// Abnahme: Session-Fixation
func TestSessionFixation(t *testing.T) {
	e := newAuthEnv(t)
	// Ein Angreifer schiebt ein selbst gewähltes Token unter …
	planted := "vom-angreifer-gesetzt"
	after := e.login(t, reqOpt{cookie: planted})
	if after == planted {
		t.Fatal("Login übernimmt das mitgebrachte Token")
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: planted}); w.Code != http.StatusSeeOther {
		t.Fatalf("untergeschobenes Token gültig: %d", w.Code)
	}
	// … und auch ein echtes altes Token wird beim erneuten Login ersetzt und ungültig.
	second := e.login(t, reqOpt{cookie: after})
	if second == after {
		t.Fatal("erneuter Login liefert dasselbe Token")
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: after}); w.Code != http.StatusSeeOther {
		t.Fatalf("altes Token nach erneutem Login gültig: %d", w.Code)
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: second}); w.Code != 200 {
		t.Fatalf("neues Token ungültig: %d", w.Code)
	}
}

func TestLogout(t *testing.T) {
	e := newAuthEnv(t)
	tok := e.login(t, reqOpt{})
	if w := e.req("POST", PathLogout, url.Values{}, reqOpt{cookie: tok}); w.Code != http.StatusForbidden {
		t.Fatalf("Logout ohne CSRF: %d", w.Code)
	}
	w := e.req("POST", PathLogout, url.Values{CSRFField: {e.csrf(t, tok)}}, reqOpt{cookie: tok})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != PathLogin {
		t.Fatalf("Logout: %d", w.Code)
	}
	if c := sessionCookie(t, w); c == nil || c.MaxAge >= 0 {
		t.Fatalf("Cookie nicht gelöscht: %+v", c)
	}
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != http.StatusSeeOther {
		t.Fatalf("Session nach Logout gültig: %d", w.Code)
	}
}

// Abnahme: CSRF
func TestCSRF(t *testing.T) {
	e := newAuthEnv(t)
	tok := e.login(t, reqOpt{})
	csrf := e.csrf(t, tok)
	other := e.csrf(t, e.login(t, reqOpt{})) // Token einer anderen Session
	cases := []struct {
		name string
		form url.Values
		hdr  map[string]string
		code int
	}{
		{"ohne Token", url.Values{}, nil, http.StatusForbidden},
		{"falsches Token", url.Values{CSRFField: {"falsch"}}, nil, http.StatusForbidden},
		{"Token anderer Session", url.Values{CSRFField: {other}}, nil, http.StatusForbidden},
		{"leeres Token", url.Values{CSRFField: {""}}, nil, http.StatusForbidden},
		{"Formularfeld", url.Values{CSRFField: {csrf}}, nil, http.StatusOK},
		{"htmx-Header", url.Values{}, map[string]string{CSRFHeader: csrf}, http.StatusOK},
		{"fremder Origin trotz Token", url.Values{CSRFField: {csrf}}, map[string]string{"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"fremder Origin ohne Sec-Fetch-Site", url.Values{CSRFField: {csrf}}, map[string]string{"Origin": "https://evil.example"}, http.StatusForbidden},
		{"gleicher Origin", url.Values{CSRFField: {csrf}}, map[string]string{"Origin": "https://example.at", "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := e.req("POST", "/admin/test", tc.form, reqOpt{cookie: tok, hdr: tc.hdr}); w.Code != tc.code {
				t.Fatalf("%d, erwartet %d: %s", w.Code, tc.code, w.Body)
			}
		})
	}
	// Login-Formular selbst: fremder Origin → 403 (Login-CSRF)
	w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}},
		reqOpt{hdr: map[string]string{"Sec-Fetch-Site": "cross-site"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("Login cross-site: %d", w.Code)
	}
}

// Abnahme: Brute-Force
func TestBruteForcePerEmail(t *testing.T) {
	e := newAuthEnv(t)
	bad := url.Values{"email": {testEmail}, "password": {"falsches-passwort"}}
	for i := 1; i <= maxPerEmail; i++ {
		// jeweils andere IP: nur das E-Mail-Limit greift
		w := e.req("POST", PathLogin, bad, reqOpt{ip: fmt.Sprintf("203.0.113.%d", i)})
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), msgBadLogin) {
			t.Fatalf("Versuch %d: %d", i, w.Code)
		}
	}
	good := url.Values{"email": {testEmail}, "password": {testPassword}}
	w := e.req("POST", PathLogin, good, reqOpt{ip: "203.0.113.99"})
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "15 Minuten") || sessionCookie(t, w) != nil {
		t.Fatalf("6. Versuch (richtiges Passwort): %d", w.Code)
	}
	// andere E-Mail ist nicht betroffen
	if w := e.req("POST", PathLogin, url.Values{"email": {"x@example.at"}, "password": {"x"}}, reqOpt{ip: "203.0.113.99"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("andere E-Mail: %d", w.Code)
	}
	// nach 15 Minuten wieder möglich, alte Einträge werden beim Schreiben gelöscht
	e.advance(attemptWindow + time.Second)
	e.login(t, reqOpt{ip: "203.0.113.99"})
	var n int
	e.db.QueryRow(`SELECT count(*) FROM core_login_attempts WHERE at <= ?`, e.now.Add(-attemptWindow).Unix()).Scan(&n)
	if n != 0 {
		t.Fatalf("%d alte Einträge nicht aufgeräumt", n)
	}
}

func TestBruteForcePerIP(t *testing.T) {
	e := newAuthEnv(t)
	for i := 1; i <= maxPerIP; i++ {
		w := e.req("POST", PathLogin, url.Values{"email": {fmt.Sprintf("u%d@example.at", i)}, "password": {"x"}}, reqOpt{ip: "198.51.100.7"})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Versuch %d: %d", i, w.Code)
		}
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}}, reqOpt{ip: "198.51.100.7"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("21. Versuch von derselben IP: %d", w.Code)
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}}, reqOpt{ip: "198.51.100.8"}); w.Code != http.StatusSeeOther {
		t.Fatalf("andere IP: %d", w.Code)
	}
	// hinter dem lokalen Proxy zählt die weitergereichte IP
	for i := 0; i < maxPerIP; i++ {
		e.req("POST", PathLogin, url.Values{"email": {fmt.Sprintf("p%d@example.at", i)}, "password": {"x"}},
			reqOpt{ip: "127.0.0.1", hdr: map[string]string{"X-Forwarded-For": "192.0.2.50"}})
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}},
		reqOpt{ip: "127.0.0.1", hdr: map[string]string{"X-Forwarded-For": "192.0.2.51"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("anderer Besucher hinter dem Proxy blockiert: %d", w.Code)
	}
}

func TestBruteForceParallel(t *testing.T) {
	e := newAuthEnv(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	codes := map[int]int{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {"falsch"}}, reqOpt{ip: fmt.Sprintf("203.0.113.%d", i)})
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if codes[http.StatusUnauthorized] != maxPerEmail || codes[http.StatusTooManyRequests] != 20-maxPerEmail {
		t.Fatalf("parallele Versuche überholen das Limit: %v", codes)
	}
}

func TestLoginSameAnswerUnknownEmail(t *testing.T) {
	e := newAuthEnv(t)
	wrong := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {"falsches-passwort"}}, reqOpt{})
	unknown := e.req("POST", PathLogin, url.Values{"email": {"wer@example.at"}, "password": {"falsches-passwort"}}, reqOpt{})
	norm := func(s string) string { return strings.ReplaceAll(s, "wer@example.at", testEmail) }
	if wrong.Code != unknown.Code || wrong.Body.String() != norm(unknown.Body.String()) {
		t.Fatalf("Antworten unterscheiden sich: %d vs %d", wrong.Code, unknown.Code)
	}
	if h := dummyHash(defaultBcryptCost); bcrypt.CompareHashAndPassword(h, []byte("x")) == nil {
		t.Fatal("Dummy-Hash passt")
	} else if c, _ := bcrypt.Cost(h); c != defaultBcryptCost {
		t.Fatalf("Dummy-Hash hat Kosten %d", c)
	}
}

func TestSessionExpiry(t *testing.T) {
	t.Run("12 h Inaktivität", func(t *testing.T) {
		e := newAuthEnv(t)
		tok := e.login(t, reqOpt{})
		e.advance(sessionIdle - time.Minute)
		if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != 200 {
			t.Fatalf("knapp vor Ablauf: %d", w.Code)
		}
		e.advance(sessionIdle - time.Minute) // seit letzter Anfrage < 12 h → weiterhin gültig
		if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != 200 {
			t.Fatalf("Aktivität verlängert nicht: %d", w.Code)
		}
		e.advance(sessionIdle)
		if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != http.StatusSeeOther {
			t.Fatalf("nach 12 h Inaktivität: %d", w.Code)
		}
		var n int
		e.db.QueryRow(`SELECT count(*) FROM core_sessions`).Scan(&n)
		if n != 0 {
			t.Fatal("abgelaufene Session nicht gelöscht")
		}
	})
	t.Run("30 Tage absolut", func(t *testing.T) {
		e := newAuthEnv(t)
		tok := e.login(t, reqOpt{})
		for d := time.Duration(0); d < sessionMax-11*time.Hour; d += 11 * time.Hour {
			e.advance(11 * time.Hour)
			if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != 200 {
				t.Fatalf("nach %v: %d", d, w.Code)
			}
		}
		e.advance(11 * time.Hour)
		if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != http.StatusSeeOther {
			t.Fatalf("nach 30 Tagen: %d", w.Code)
		}
	})
}

func TestPasswordChangeEndsSessions(t *testing.T) {
	e := newAuthEnv(t)
	t1, t2 := e.login(t, reqOpt{}), e.login(t, reqOpt{})
	if err := e.a.SetPassword(context.Background(), e.user, "ganz-neues-passwort"); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{t1, t2} {
		if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: tok}); w.Code != http.StatusSeeOther {
			t.Fatalf("Session nach Passwortänderung gültig: %d", w.Code)
		}
	}
	if err := e.a.SetPassword(context.Background(), e.user, "kurz"); err == nil {
		t.Fatal("kurzes Passwort akzeptiert")
	}
}

func resetToken(t *testing.T, body string) string {
	t.Helper()
	i := strings.Index(body, PathReset+"?token=")
	if i < 0 {
		t.Fatalf("kein Link in der Mail: %q", body)
	}
	rest := body[i+len(PathReset+"?token="):]
	if j := strings.IndexAny(rest, "\r\n "); j >= 0 {
		rest = rest[:j]
	}
	tok, err := url.QueryUnescape(rest)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestPasswordReset(t *testing.T) {
	e := newAuthEnv(t)
	old := e.login(t, reqOpt{})

	w := e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{})
	unknown := e.req("POST", PathForgot, url.Values{"email": {"wer@example.at"}}, reqOpt{})
	if w.Code != 200 || unknown.Code != 200 || w.Body.String() != unknown.Body.String() {
		t.Fatalf("Antworten unterscheiden sich: %d/%d", w.Code, unknown.Code)
	}
	if len(e.mail.sent) != 1 || e.mail.sent[0].to != testEmail {
		t.Fatalf("Mails: %+v", e.mail.sent)
	}
	body := e.mail.sent[0].body
	if !strings.Contains(body, "https://example.at"+PathReset+"?token=") {
		t.Fatalf("Link nicht auf site.url: %q", body)
	}
	tok := resetToken(t, body)
	var n int
	e.db.QueryRow(`SELECT count(*) FROM core_password_resets WHERE token_hash = ?`, tok).Scan(&n)
	if n != 0 {
		t.Fatal("Reset-Token im Klartext gespeichert")
	}

	if w := e.req("GET", PathReset+"?token="+url.QueryEscape(tok), nil, reqOpt{}); w.Code != 200 || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("Formular: %d %q", w.Code, w.Header().Get("Referrer-Policy"))
	}
	if w := e.req("POST", PathReset, url.Values{"token": {tok}, "password": {"kurz"}, "password2": {"kurz"}}, reqOpt{}); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("kurzes Passwort: %d", w.Code)
	}
	newPW := url.Values{"token": {tok}, "password": {"ganz-neues-passwort"}, "password2": {"ganz-neues-passwort"}}
	if w := e.req("POST", PathReset, newPW, reqOpt{}); w.Code != 200 {
		t.Fatalf("Reset: %d %s", w.Code, w.Body)
	}
	// alte Session ungültig, altes Passwort ungültig, neues gültig
	if w := e.req("GET", "/admin/test", nil, reqOpt{cookie: old}); w.Code != http.StatusSeeOther {
		t.Fatalf("alte Session nach Reset: %d", w.Code)
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}}, reqOpt{}); w.Code != http.StatusUnauthorized {
		t.Fatalf("altes Passwort: %d", w.Code)
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {"ganz-neues-passwort"}}, reqOpt{}); w.Code != http.StatusSeeOther {
		t.Fatalf("neues Passwort: %d", w.Code)
	}
	// Token ist einmalig
	if w := e.req("POST", PathReset, newPW, reqOpt{}); w.Code != http.StatusNotFound {
		t.Fatalf("Token zweimal: %d", w.Code)
	}
	if w := e.req("GET", PathReset+"?token="+url.QueryEscape(tok), nil, reqOpt{}); w.Code != http.StatusNotFound {
		t.Fatalf("Formular mit benutztem Token: %d", w.Code)
	}
}

func TestPasswordResetInvalidTokens(t *testing.T) {
	e := newAuthEnv(t)
	e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{})
	tok := resetToken(t, e.mail.sent[0].body)
	e.advance(resetTTL + time.Second)
	pw := "ganz-neues-passwort"
	for name, token := range map[string]string{"abgelaufen": tok, "erfunden": "erfunden", "leer": ""} {
		t.Run(name, func(t *testing.T) {
			if w := e.req("POST", PathReset, url.Values{"token": {token}, "password": {pw}, "password2": {pw}}, reqOpt{}); w.Code != http.StatusNotFound {
				t.Fatalf("%d", w.Code)
			}
		})
	}
	if w := e.req("POST", PathLogin, url.Values{"email": {testEmail}, "password": {testPassword}}, reqOpt{}); w.Code != http.StatusSeeOther {
		t.Fatalf("Passwort wurde trotzdem geändert: %d", w.Code)
	}
}

func TestPasswordResetRateLimit(t *testing.T) {
	e := newAuthEnv(t)
	for i := 0; i < maxPerEmail; i++ {
		if w := e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{ip: fmt.Sprintf("203.0.113.%d", i)}); w.Code != 200 {
			t.Fatalf("Anfrage %d: %d", i, w.Code)
		}
	}
	if w := e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{ip: "203.0.113.200"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("6. Anfrage: %d", w.Code)
	}
	if len(e.mail.sent) != maxPerEmail {
		t.Fatalf("%d Mails", len(e.mail.sent))
	}
	// Reset-Anfragen zählen nicht gegen das Login-Limit
	e.login(t, reqOpt{ip: "203.0.113.1"})
}

func TestPasswordResetWithoutSiteURL(t *testing.T) {
	e := newAuthEnv(t)
	_ = e.a.Settings.Set(context.Background(), KeySiteURL, "")
	if w := e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{}); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	if len(e.mail.sent) != 0 {
		t.Fatal("Mail ohne site.url gesendet (Link würde dem Host-Header folgen)")
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		remote, xff, want string
	}{
		{"203.0.113.5:1234", "", "203.0.113.5"},
		{"203.0.113.5:1234", "198.51.100.1", "203.0.113.5"}, // fremder Absender: Header ignoriert
		{"127.0.0.1:1234", "", "127.0.0.1"},
		{"127.0.0.1:1234", "198.51.100.1", "198.51.100.1"},
		{"127.0.0.1:1234", "6.6.6.6, 198.51.100.1", "198.51.100.1"}, // gefälschter erster Eintrag
		{"[::1]:1234", "2001:db8::1", "2001:db8::1"},
		{"127.0.0.1:1234", "kein-ip", "127.0.0.1"},
		{"[2001:db8::5]:1234", "198.51.100.1", "2001:db8::5"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if got := ClientIP(r); got != tc.want {
			t.Errorf("%s / %q: %q, erwartet %q", tc.remote, tc.xff, got, tc.want)
		}
	}
}
