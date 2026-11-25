package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// newTestInstaller: frische DB mit Kern-Migrationen, Code-Datei wie vom Deploy.
func newTestInstaller(t *testing.T) (*Installer, http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	db := openTestDB(t, dir)
	if err := Migrate(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	code := "ABCD-EFGH-IJKL-MNOP"
	if err := os.WriteFile(filepath.Join(dir, InstallCodeFile), []byte(code+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := &Installer{DB: db, DataDir: dir, BcryptCost: bcrypt.MinCost}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("startseite")) })
	in.Mount(mux)
	return in, in.RequireInstalled(mux), code
}

func validForm(code string) url.Values {
	return url.Values{
		"code":      {code},
		"title":     {"Tischlerei Muster"},
		"region":    {"de-AT"},
		"gisa":      {"12345678"},
		"email":     {"admin@example.at"},
		"password":  {"ein-langes-passwort"},
		"password2": {"ein-langes-passwort"},
	}
}

func do(h http.Handler, method, path string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	r := httptest.NewRequest(method, path, body)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestInstallFlow(t *testing.T) {
	in, h, code := newTestInstaller(t)
	ctx := context.Background()

	// Vor der Installation: alles außer /healthz und /install → /install
	if w := do(h, "GET", "/", nil); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/install" {
		t.Fatalf("Startseite vor Installation: %d %q", w.Code, w.Header().Get("Location"))
	}
	if w := do(h, "POST", "/shop/kasse", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST vor Installation: %d", w.Code)
	}
	if w := do(h, "GET", "/healthz", nil); w.Code != 200 {
		t.Fatalf("/healthz: %d", w.Code)
	}
	w := do(h, "GET", "/install", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Installations-Code") {
		t.Fatalf("GET /install: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("Wizard darf nicht gecacht werden")
	}

	// Code tolerant gegenüber Kleinschreibung und fehlenden Bindestrichen
	form := validForm(strings.ToLower(strings.ReplaceAll(code, "-", "")))
	w = do(h, "POST", "/install", form)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Fertig eingerichtet") {
		t.Fatalf("POST /install: %d\n%s", w.Code, w.Body)
	}

	// Angelegt
	s := SettingsStore{DB: in.DB}
	all, err := s.GetAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all[KeySiteTitle] != "Tischlerei Muster" || all[KeySiteRegion] != "de-AT" || all[KeySiteGISA] != "12345678" || all[KeyInstalledAt] == "" {
		t.Fatalf("Settings: %v", all)
	}
	if all[KeySiteURL] != "http://example.com" { // httptest-Host, ohne TLS
		t.Fatalf("site.url: %q", all[KeySiteURL])
	}
	var email, hash string
	if err := in.DB.QueryRow(`SELECT email, password_hash FROM core_users`).Scan(&email, &hash); err != nil {
		t.Fatal(err)
	}
	if email != "admin@example.at" || bcrypt.CompareHashAndPassword([]byte(hash), []byte("ein-langes-passwort")) != nil {
		t.Fatalf("Benutzer falsch angelegt: %s", email)
	}
	if _, err := os.Stat(filepath.Join(in.DataDir, InstallCodeFile)); !os.IsNotExist(err) {
		t.Error("Code-Datei nach Installation nicht gelöscht")
	}

	// Zweiter Aufruf → 404 (GET und POST, auch mit gültigen Daten)
	if w := do(h, "GET", "/install", nil); w.Code != http.StatusNotFound {
		t.Fatalf("zweites GET /install: %d", w.Code)
	}
	if w := do(h, "POST", "/install", validForm(code)); w.Code != http.StatusNotFound {
		t.Fatalf("zweites POST /install: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(in.DataDir, InstallCodeFile)); !os.IsNotExist(err) {
		t.Error("GET nach Installation hat wieder eine Code-Datei angelegt")
	}
	if w := do(h, "GET", "/", nil); w.Code != 200 || w.Body.String() != "startseite" {
		t.Fatalf("Startseite nach Installation: %d", w.Code)
	}
}

func TestInstallInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(url.Values)
		field string // erwartete Meldung
	}{
		{"falscher Code", func(v url.Values) { v.Set("code", "AAAA-BBBB-CCCC-DDDD") }, "Installations-Code stimmt nicht"},
		{"leerer Code", func(v url.Values) { v.Set("code", "") }, "Installations-Code stimmt nicht"},
		{"kurzes Passwort", func(v url.Values) { v.Set("password", "elf-zeichen"); v.Set("password2", "elf-zeichen") }, "mindestens 12 Zeichen"},
		{"Passwort zu lang für bcrypt", func(v url.Values) { p := strings.Repeat("x", 73); v.Set("password", p); v.Set("password2", p) }, "höchstens 72 Bytes"},
		{"Passwörter verschieden", func(v url.Values) { v.Set("password2", "ein-anderes-passwort") }, "stimmen nicht überein"},
		{"ungültige Region", func(v url.Values) { v.Set("region", "en-US") }, "gültige Region"},
		{"GISA mit Buchstaben", func(v url.Values) { v.Set("gisa", "12A45") }, "nur aus Ziffern"},
		{"ungültige E-Mail", func(v url.Values) { v.Set("email", "admin@") }, "gültige E-Mail"},
		{"E-Mail mit Namen", func(v url.Values) { v.Set("email", "Admin <admin@example.at>") }, "gültige E-Mail"},
		{"leerer Titel", func(v url.Values) { v.Set("title", "  ") }, "Seitentitel angeben"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, h, code := newTestInstaller(t)
			form := validForm(code)
			tt.edit(form)
			w := do(h, "POST", "/install", form)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), tt.field) {
				t.Fatalf("%d, erwartet 422 mit %q\n%s", w.Code, tt.field, w.Body)
			}
			if strings.Contains(w.Body.String(), "ein-langes-passwort") {
				t.Error("Passwort im Formular zurückgegeben")
			}
			if ok, _ := Installed(context.Background(), SettingsStore{DB: in.DB}); ok {
				t.Fatal("trotz Fehler installiert")
			}
		})
	}
}

func TestInstallGISAOnlyForAustria(t *testing.T) {
	in, h, code := newTestInstaller(t)
	form := validForm(code)
	form.Set("region", "de-DE")
	form.Set("gisa", "kein-gisa") // wird für DE ignoriert, nicht geprüft
	if w := do(h, "POST", "/install", form); w.Code != 200 {
		t.Fatalf("%d\n%s", w.Code, w.Body)
	}
	if v, _ := (SettingsStore{DB: in.DB}).Get(context.Background(), KeySiteGISA); v != "" {
		t.Fatalf("GISA für de-DE gespeichert: %q", v)
	}
}

func TestInstallCrossOrigin(t *testing.T) {
	_, h, code := newTestInstaller(t)
	w := do(h, "POST", "/install", validForm(code), "Origin", "https://angreifer.example", "Sec-Fetch-Site", "cross-site")
	if w.Code != http.StatusForbidden {
		t.Fatalf("fremder Origin: %d", w.Code)
	}
}

func TestInstallCreatesCodeFile(t *testing.T) {
	in, h, _ := newTestInstaller(t)
	path := filepath.Join(in.DataDir, InstallCodeFile)
	os.Remove(path) // Download-Variante: kein Deploy hat den Code geschrieben
	if w := do(h, "GET", "/install", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("Code-Datei: %v %v", st, err)
	}
	b, _ := os.ReadFile(path)
	if n := len(NormalizeInstallCode(string(b))); n != 16 {
		t.Fatalf("Code hat %d Zeichen", n)
	}
	// Zweiter GET erzeugt keinen neuen Code
	do(h, "GET", "/install", nil)
	if b2, _ := os.ReadFile(path); string(b2) != string(b) {
		t.Fatal("Code bei erneutem Aufruf geändert")
	}
	if w := do(h, "POST", "/install", validForm(string(b))); w.Code != 200 {
		t.Fatalf("Installation mit erzeugtem Code: %d", w.Code)
	}
}

// Zwei gleichzeitige Installationen: genau eine gewinnt, die andere bekommt 404.
func TestInstallConcurrent(t *testing.T) {
	for round := 0; round < 5; round++ {
		in, h, code := newTestInstaller(t)
		const n = 8
		codes := make([]int, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				form := validForm(code)
				form.Set("email", "admin"+string(rune('a'+i))+"@example.at")
				<-start
				codes[i] = do(h, "POST", "/install", form).Code
			}(i)
		}
		close(start)
		wg.Wait()
		ok := 0
		for _, c := range codes {
			switch c {
			case 200:
				ok++
			case 404:
			default:
				t.Fatalf("unerwarteter Status %d (%v)", c, codes)
			}
		}
		if ok != 1 {
			t.Fatalf("%d Installationen erfolgreich (%v)", ok, codes)
		}
		if n := countRows(t, in.DB, `SELECT count(*) FROM core_users`); n != 1 {
			t.Fatalf("%d Admin-Konten angelegt", n)
		}
	}
}

func TestSettingsStore(t *testing.T) {
	db := openTestDB(t, t.TempDir())
	ctx := context.Background()
	if err := Migrate(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	s := SettingsStore{DB: db}
	if v, err := s.Get(ctx, KeySiteTitle); v != "" || err != nil {
		t.Fatalf("fehlender Schlüssel: %q %v", v, err)
	}
	for _, v := range []string{"Erster", "Zweiter"} {
		if err := s.Set(ctx, KeySiteTitle, v); err != nil {
			t.Fatal(err)
		}
	}
	if v, _ := s.Get(ctx, KeySiteTitle); v != "Zweiter" {
		t.Fatalf("Überschreiben: %q", v)
	}
	s.Set(ctx, KeySiteRegion, "de-CH")
	all, err := s.GetAll(ctx)
	if err != nil || len(all) != 2 || all[KeySiteRegion] != "de-CH" {
		t.Fatalf("GetAll: %v %v", all, err)
	}
	if ok, _ := Installed(ctx, s); ok {
		t.Fatal("ohne installed_at als installiert gemeldet")
	}
	// Kern-Migration ist unter Modul „core“ eingetragen
	var mod string
	if err := db.QueryRow(`SELECT module FROM schema_migrations WHERE name = '001_core.sql'`).Scan(&mod); err != nil || mod != CoreID {
		t.Fatalf("core-Migration: %q %v", mod, err)
	}
}

func TestNormalizeInstallCode(t *testing.T) {
	for in, want := range map[string]string{
		"ABCD-EFGH-IJKL-MNOP\n": "ABCDEFGHIJKLMNOP",
		" abcd efgh-ijkl mnop ": "ABCDEFGHIJKLMNOP",
		"":                      "",
	} {
		if got := NormalizeInstallCode(in); got != want {
			t.Errorf("%q → %q, erwartet %q", in, got, want)
		}
	}
	c, err := NewInstallCode()
	if err != nil || len(c) != 19 || NormalizeInstallCode(c) == c {
		t.Fatalf("NewInstallCode: %q %v", c, err)
	}
}
