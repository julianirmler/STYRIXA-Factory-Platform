package media

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// ---------- Testbilder ----------

// webpL baut ein WebP mit VP8L-Kopf (verlustfrei) der Größe w×h. Der Inhalt
// nach dem Kopf ist Füllmaterial – der Server liest nur den Kopf.
func webpL(w, h int) []byte {
	payload := make([]byte, 5, 64)
	payload[0] = 0x2F
	binary.LittleEndian.PutUint32(payload[1:], uint32(w-1)|uint32(h-1)<<14)
	payload = append(payload, make([]byte, 27)...)
	return riff("VP8L", payload)
}

// webpLossy baut einen VP8-Kopf (verlustbehaftet).
func webpLossy(w, h int) []byte {
	p := []byte{0x10, 0x02, 0x00, 0x9D, 0x01, 0x2A, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(p[6:], uint16(w))
	binary.LittleEndian.PutUint16(p[8:], uint16(h))
	return riff("VP8 ", append(p, make([]byte, 20)...))
}

// webpX baut einen VP8X-Kopf (erweitert, z. B. mit Alpha).
func webpX(w, h int) []byte {
	p := make([]byte, 10, 40)
	p[0] = 0x10 // Alpha
	p[4], p[5], p[6] = byte(w-1), byte((w-1)>>8), byte((w-1)>>16)
	p[7], p[8], p[9] = byte(h-1), byte((h-1)>>8), byte((h-1)>>16)
	return riff("VP8X", append(p, make([]byte, 20)...))
}

func riff(chunk string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(4+8+len(payload)))
	b.WriteString("WEBP")
	b.WriteString(chunk)
	binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	return b.Bytes()
}

func jpegImg(t testing.TB, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{200, 10, 10, 255})
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngImg(t testing.TB, w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// ---------- Kopfdaten und Varianten ----------

func TestSniff(t *testing.T) {
	broken := webpL(10, 10)
	binary.LittleEndian.PutUint32(broken[4:], 999) // RIFF-Größe passt nicht
	for _, tc := range []struct {
		name string
		b    []byte
		mime string
		w, h int
	}{
		{"WebP verlustfrei", webpL(480, 320), mimeWebP, 480, 320},
		{"WebP verlustbehaftet", webpLossy(960, 640), mimeWebP, 960, 640},
		{"WebP erweitert", webpX(1920, 1280), mimeWebP, 1920, 1280},
		{"JPEG", jpegImg(t, 48, 32), mimeJPEG, 48, 32},
		{"PNG abgelehnt", pngImg(t, 4, 4), "", 0, 0},
		{"SVG abgelehnt", []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`), "", 0, 0},
		{"HTML abgelehnt", []byte("<!doctype html><script>alert(1)</script>"), "", 0, 0},
		{"JPEG-Magie ohne Bild", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0}, "", 0, 0},
		{"RIFF-Größe falsch", broken, "", 0, 0},
		{"WebP zu breit", webpX(4001, 10), "", 0, 0},
		{"leer", nil, "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mime, w, h, ok := sniff(tc.b)
			if ok != (tc.mime != "") || mime != tc.mime || w != tc.w || h != tc.h {
				t.Errorf("sniff = %q %d×%d %v, erwartet %q %d×%d", mime, w, h, ok, tc.mime, tc.w, tc.h)
			}
		})
	}
}

func TestCheckVariants(t *testing.T) {
	v := func(mime string, w, h int) variant { return variant{Mime: mime, Width: w, Height: h} }
	for _, tc := range []struct {
		name string
		vs   []variant
		want string
	}{
		{"drei Breiten", []variant{v(mimeWebP, 1920, 1280), v(mimeWebP, 480, 320), v(mimeWebP, 960, 640)}, ""},
		{"zwei Breiten", []variant{v(mimeJPEG, 480, 270), v(mimeJPEG, 960, 540)}, ""},
		{"eine schmale", []variant{v(mimeWebP, 300, 200)}, ""},
		{"nur 480", []variant{v(mimeWebP, 480, 320)}, ""},
		{"Rundung ±1", []variant{v(mimeWebP, 480, 271), v(mimeWebP, 960, 541), v(mimeWebP, 1920, 1081)}, ""},
		{"keine", nil, "kein Bild"},
		{"vier", []variant{v(mimeWebP, 480, 1), v(mimeWebP, 960, 2), v(mimeWebP, 1920, 4), v(mimeWebP, 300, 1)}, "Zu viele"},
		{"gemischt", []variant{v(mimeWebP, 480, 320), v(mimeJPEG, 960, 640)}, "dasselbe Format"},
		{"Lücke", []variant{v(mimeWebP, 480, 320), v(mimeWebP, 1920, 1280)}, "Unerwartete Bildbreiten"},
		{"falsche Breite", []variant{v(mimeWebP, 500, 320), v(mimeWebP, 960, 640)}, "Unerwartete Bildbreiten"},
		{"zwei schmale", []variant{v(mimeWebP, 300, 200), v(mimeWebP, 480, 320)}, "Unerwartete Bildbreiten"},
		{"Seitenverhältnis", []variant{v(mimeWebP, 480, 480), v(mimeWebP, 960, 640)}, "Seitenverhältnisse"},
		{"zu breit", []variant{v(mimeWebP, 480, 100)}, "zu extrem"},
		{"zu hoch", []variant{v(mimeWebP, 300, 1201)}, "zu extrem"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := checkVariants(tc.vs)
			if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
				t.Errorf("checkVariants = %q, erwartet %q", got, tc.want)
			}
		})
	}
}

// ---------- Handler ----------

// referrer ersetzt im Test das Content-Modul (core.MediaReferrer).
type referrer struct{ refs map[string][]string }

func (referrer) ID() string                                                { return "fake" }
func (referrer) Requires() []string                                        { return nil }
func (referrer) Migrations() fs.FS                                         { return nil }
func (referrer) Mount(*http.ServeMux, core.Deps)                           {}
func (referrer) AdminNav() []core.NavItem                                  { return nil }
func (r referrer) MediaRefs(_ context.Context, p string) ([]string, error) { return r.refs[p], nil }

type env struct {
	t      *testing.T
	db     *sql.DB
	h      http.Handler
	m      *module
	dir    string
	ref    referrer
	as     int64
	role   string
	admin  int64
	editor int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	db, err := core.OpenDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := &module{}
	ref := referrer{refs: map[string][]string{}}
	mods := []core.Module{m, ref}
	if err := core.Migrate(context.Background(), db, mods); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: db, m: m, dir: filepath.Join(dir, "media"), ref: ref}
	for _, u := range []struct {
		id         *int64
		mail, role string
	}{{&e.admin, "admin@example.at", core.RoleAdmin}, {&e.editor, "redaktion@example.at", core.RoleEditor}} {
		res, err := db.Exec(`INSERT INTO core_users (email, password_hash, role) VALUES (?, 'x', ?)`, u.mail, u.role)
		if err != nil {
			t.Fatal(err)
		}
		*u.id, _ = res.LastInsertId()
	}
	e.as, e.role = e.admin, core.RoleAdmin
	admin, err := core.NewAdmin(db, nil, mods)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	admin.Mount(mux)
	m.Mount(mux, core.Deps{DB: db, Admin: admin, DataDir: dir, Modules: mods})
	m.st.now = func() time.Time { return time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC) }
	e.h = core.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") {
			r = r.WithContext(core.WithUser(r.Context(), e.as, e.role))
		}
		mux.ServeHTTP(w, r)
	}))
	return e
}

func (e *env) do(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) get(path string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return e.do(r)
}

func (e *env) form(path string, kv ...string) *httptest.ResponseRecorder {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.do(r)
}

// upload schickt Felder (name, alt, …) und Varianten wie media-upload.js.
func (e *env) upload(fields map[string]string, variants ...[]byte) *httptest.ResponseRecorder {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for i, v := range variants {
		fw, _ := mw.CreateFormFile("variant", fmt.Sprintf("%d.webp", i))
		fw.Write(v)
	}
	mw.Close()
	r := httptest.NewRequest("POST", "/admin/media/upload", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return e.do(r)
}

func (e *env) mustUpload(fields map[string]string, variants ...[]byte) pick {
	e.t.Helper()
	w := e.upload(fields, variants...)
	if w.Code != http.StatusCreated {
		e.t.Fatalf("Upload: %d %s", w.Code, w.Body)
	}
	var p pick
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func three() [][]byte { return [][]byte{webpL(480, 320), webpL(960, 640), webpL(1920, 1280)} }

// Abnahme 4.1: Upload → Varianten im Datenordner, öffentlich unter /media/…
// mit sicheren Headern; Löschen nur durch Admins.
func TestUploadServeDelete(t *testing.T) {
	e := newEnv(t)
	vs := three()
	p := e.mustUpload(map[string]string{"name": "Weinberg im Herbst", "alt": "Weinberg bei Sonnenuntergang"}, vs[2], vs[0], vs[1])
	if p.Width != 960 || p.Height != 640 || !fileRE.MatchString(strings.TrimPrefix(p.URL, "/media/")) || !strings.HasSuffix(p.URL, "-960.webp") ||
		len(p.Variants) != 3 || p.Variants[0].Width != 480 || p.Alt != "Weinberg bei Sonnenuntergang" || p.Name != "Weinberg im Herbst" {
		t.Fatalf("Antwort: %+v", p)
	}
	for i, v := range p.Variants {
		path := filepath.Join(e.dir, strings.TrimPrefix(v.URL, "/media/"))
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s: Rechte %v", path, st.Mode().Perm())
		}
		w := e.get(v.URL)
		h := w.Header()
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), vs[i]) {
			t.Errorf("GET %s: %d, %d Bytes", v.URL, w.Code, w.Body.Len())
		}
		for k, want := range map[string]string{
			"Content-Type": "image/webp", "X-Content-Type-Options": "nosniff",
			"Content-Security-Policy": "default-src 'none'; sandbox", "Cache-Control": "public, max-age=31536000, immutable",
		} {
			if h.Get(k) != want {
				t.Errorf("%s %s = %q, erwartet %q", v.URL, k, h.Get(k), want)
			}
		}
		if h.Get("Set-Cookie") != "" {
			t.Error("Set-Cookie bei Medien")
		}
	}
	// Range und HEAD
	if w := e.get(p.URL, "Range", "bytes=0-3"); w.Code != http.StatusPartialContent || w.Body.String() != "RIFF" {
		t.Errorf("Range: %d %q", w.Code, w.Body)
	}
	if w := e.do(httptest.NewRequest("HEAD", p.URL, nil)); w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("HEAD: %d %d", w.Code, w.Body.Len())
	}
	key := strings.TrimSuffix(strings.TrimPrefix(p.URL, "/media/"), "-960.webp")
	for _, path := range []string{
		"/media/" + key + "-960.jpg",                      // falsche Endung
		"/media/" + key + "-1000.webp",                    // keine solche Variante
		"/media/" + strings.Repeat("a", 26) + "-960.webp", // unbekannter Schlüssel
		"/media/..%2Fstyrixa.db",                          // Pfad-Ausbruch
		"/media/" + key + "-960.webp.bak",
		"/media/styrixa.db",
	} {
		if w := e.get(path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, erwartet 404", path, w.Code)
		}
	}

	var id int64
	e.db.QueryRow(`SELECT id FROM media_files`).Scan(&id)
	del := fmt.Sprintf("/admin/media/files/%d/delete", id)
	e.as, e.role = e.editor, core.RoleEditor
	if w := e.form(del); w.Code != http.StatusForbidden {
		t.Errorf("Löschen durch Editor: %d", w.Code)
	}
	if w := e.get(fmt.Sprintf("/admin/media/files/%d", id)); w.Code != 200 || strings.Contains(w.Body.String(), "Bild löschen") {
		t.Errorf("Detailseite für Editor: %d, Löschen-Knopf sichtbar: %v", w.Code, strings.Contains(w.Body.String(), "Bild löschen"))
	}
	e.as, e.role = e.admin, core.RoleAdmin
	if w := e.form(del); w.Code != http.StatusSeeOther {
		t.Fatalf("Löschen: %d %s", w.Code, w.Body)
	}
	if w := e.get(p.URL); w.Code != http.StatusNotFound {
		t.Errorf("nach Löschen: %d", w.Code)
	}
	if left, _ := os.ReadDir(e.dir); len(left) != 0 {
		t.Errorf("Dateien übrig: %v", left)
	}
	var n int
	e.db.QueryRow(`SELECT count(*) FROM media_variants`).Scan(&n)
	if n != 0 {
		t.Errorf("%d Varianten übrig", n)
	}
	if w := e.form(del); w.Code != http.StatusNotFound {
		t.Errorf("zweites Löschen: %d", w.Code)
	}
}

func TestUploadJPEGAndSmall(t *testing.T) {
	e := newEnv(t)
	p := e.mustUpload(map[string]string{"decorative": "1", "alt": "wird ignoriert"}, jpegImg(t, 480, 270), jpegImg(t, 960, 540))
	if !strings.HasSuffix(p.URL, "-960.jpg") || p.Alt != "" || p.Name != "Bild" {
		t.Errorf("JPEG: %+v", p)
	}
	if w := e.get(p.URL); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("JPEG ausliefern: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	small := e.mustUpload(map[string]string{"alt": "Logo"}, webpX(200, 100))
	if !strings.HasSuffix(small.URL, "-200.webp") || small.Width != 200 || len(small.Variants) != 1 {
		t.Errorf("kleines Bild: %+v", small)
	}
}

func TestUploadRejects(t *testing.T) {
	e := newEnv(t)
	folder, _ := e.m.st.createFolder(context.Background(), 0, "Produkte")
	ok := map[string]string{"alt": "Hof"}
	big := webpL(480, 320)
	big = riff("VP8L", append(big[20:], make([]byte, maxVariantBytes)...))
	for _, tc := range []struct {
		name     string
		fields   map[string]string
		variants [][]byte
		want     string
	}{
		{"SVG", ok, [][]byte{[]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)}, "Nur WebP- und JPEG"},
		{"PNG", ok, [][]byte{pngImg(t, 480, 320)}, "Nur WebP- und JPEG"},
		{"HTML als .webp", ok, [][]byte{[]byte("<html><script>alert(1)</script>")}, "Nur WebP- und JPEG"},
		{"ohne Bild", ok, nil, "kein Bild"},
		{"vier Varianten", ok, append(three(), webpL(300, 200)), "Zu viele"},
		{"gemischte Formate", ok, [][]byte{webpL(480, 270), jpegImg(t, 960, 540)}, "dasselbe Format"},
		{"falsche Breite", ok, [][]byte{webpL(500, 320), webpL(960, 640)}, "Unerwartete"},
		{"Variante zu groß", ok, [][]byte{big}, "zu groß"},
		{"Alt-Text fehlt", map[string]string{"alt": "  "}, three(), "Alternativtext"},
		{"Ordner fehlt", map[string]string{"alt": "x", "folder_id": "999"}, three(), "Ordner existiert nicht"},
		{"Feld zu lang", map[string]string{"alt": strings.Repeat("a", 2000)}, three(), "zu lang"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := e.upload(tc.fields, tc.variants...)
			var body struct{ Error string }
			json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != http.StatusBadRequest || !strings.Contains(body.Error, tc.want) {
				t.Errorf("%d %q, erwartet 400 mit %q", w.Code, body.Error, tc.want)
			}
		})
	}
	var n int
	e.db.QueryRow(`SELECT count(*) FROM media_files`).Scan(&n)
	if entries, _ := os.ReadDir(e.dir); n != 0 || len(entries) != 0 {
		t.Errorf("abgelehnte Uploads hinterlassen %d Zeilen, %d Dateien", n, len(entries))
	}
	// Anfrage über dem Gesamtlimit
	w := e.upload(ok, bytes.Repeat([]byte{0}, maxUploadBytes+1))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "zu groß") {
		t.Errorf("Gesamtlimit: %d %s", w.Code, w.Body)
	}
	// Mit gültigem Ordner klappt es.
	p := e.mustUpload(map[string]string{"alt": "x", "folder_id": fmt.Sprint(folder)}, three()...)
	var fid int64
	e.db.QueryRow(`SELECT folder_id FROM media_files WHERE id = ?`, p.ID).Scan(&fid)
	if fid != folder {
		t.Errorf("Ordner %d, erwartet %d", fid, folder)
	}
}

func TestFolders(t *testing.T) {
	e := newEnv(t)
	loc := func(w *httptest.ResponseRecorder) string { return w.Header().Get("Location") }
	if w := e.form("/admin/media/folders", "name", "  Produkte  "); w.Code != http.StatusSeeOther || loc(w) != "/admin/media" {
		t.Fatalf("anlegen: %d %q", w.Code, loc(w))
	}
	var top int64
	e.db.QueryRow(`SELECT id FROM media_folders WHERE name = 'Produkte'`).Scan(&top)
	if w := e.form("/admin/media/folders", "name", "produkte"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "gibt es hier schon") {
		t.Errorf("doppelt (NOCASE): %d", w.Code)
	}
	if w := e.form("/admin/media/folders", "name", ""); w.Code != http.StatusBadRequest {
		t.Errorf("leer: %d", w.Code)
	}
	parent := top
	for _, name := range []string{"Wein", "Rot"} {
		w := e.form("/admin/media/folders", "name", name, "parent_id", fmt.Sprint(parent))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body)
		}
		e.db.QueryRow(`SELECT id FROM media_folders WHERE name = ?`, name).Scan(&parent)
	}
	if w := e.form("/admin/media/folders", "name", "Zu tief", "parent_id", fmt.Sprint(parent)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Ebenen") {
		t.Errorf("Tiefe 4: %d", w.Code)
	}
	if w := e.form("/admin/media/folders", "name", "Wein"); w.Code != http.StatusSeeOther {
		t.Errorf("gleicher Name auf anderer Ebene: %d", w.Code)
	}
	// Brotkrumen und Unterordner
	body := e.get(fmt.Sprintf("/admin/media?folder=%d", parent)).Body.String()
	if !strings.Contains(body, ">Produkte</a> / <a") || !strings.Contains(body, `aria-current="page">Rot</a>`) {
		t.Errorf("Brotkrumen fehlen:\n%s", body)
	}
	// Nicht leer → 400
	p := e.mustUpload(map[string]string{"alt": "Flasche", "folder_id": fmt.Sprint(parent)}, three()...)
	if w := e.form(fmt.Sprintf("/admin/media/folders/%d/delete", top)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nicht leer") {
		t.Errorf("nicht leer: %d", w.Code)
	}
	// Datei verschieben, URL bleibt gleich
	if w := e.form(fmt.Sprintf("/admin/media/files/%d", p.ID), "name", "Flasche", "alt", "Flasche Zweigelt", "folder_id", "0"); w.Code != http.StatusSeeOther {
		t.Fatalf("verschieben: %d %s", w.Code, w.Body)
	}
	if w := e.get(p.URL); w.Code != 200 {
		t.Errorf("URL nach Verschieben: %d", w.Code)
	}
	if w := e.form(fmt.Sprintf("/admin/media/folders/%d/delete", parent)); w.Code != http.StatusSeeOther {
		t.Errorf("leeren Ordner löschen: %d %s", w.Code, w.Body)
	}
	if w := e.form(fmt.Sprintf("/admin/media/folders/%d/rename", top), "name", "Shop"); w.Code != http.StatusSeeOther {
		t.Errorf("umbenennen: %d", w.Code)
	}
	if w := e.form("/admin/media/folders/999/rename", "name", "x"); w.Code != http.StatusNotFound {
		t.Errorf("unbekannter Ordner: %d", w.Code)
	}
	// Alt-Text-Pflicht beim Bearbeiten
	if w := e.form(fmt.Sprintf("/admin/media/files/%d", p.ID), "name", "x", "alt", ""); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "Alternativtext") {
		t.Errorf("ohne Alt-Text: %d", w.Code)
	}
}

func TestLibraryAndAPI(t *testing.T) {
	e := newEnv(t)
	folder, _ := e.m.st.createFolder(context.Background(), 0, "Team & <Leute>")
	p := e.mustUpload(map[string]string{"name": `Chefin "Anna"`, "alt": "Porträt <b>Anna</b>"}, three()...)
	e.ref.refs["/media/"+strings.Split(strings.TrimPrefix(p.URL, "/media/"), "-")[0]+"-"] = []string{"Über uns"}

	body := e.get("/admin/media").Body.String()
	for _, want := range []string{`data-styrixa-template="media-library"`, `src="` + p.Variants[0].URL + `"`, "Chefin &#34;Anna&#34;", "Team &amp; &lt;Leute&gt;", "1920 × 1280"} {
		if !strings.Contains(body, want) {
			t.Errorf("Mediathek ohne %q", want)
		}
	}
	detail := e.get(fmt.Sprintf("/admin/media/files/%d", p.ID)).Body.String()
	for _, want := range []string{"Über uns", "Bild löschen", "noch verwendet", p.Variants[2].URL} {
		if !strings.Contains(detail, want) {
			t.Errorf("Detailseite ohne %q:\n%s", want, detail)
		}
	}
	if strings.Contains(detail, "<b>Anna</b>") {
		t.Error("Alt-Text nicht escaped")
	}

	var api apiView
	w := e.get("/admin/media/api")
	if err := json.Unmarshal(w.Body.Bytes(), &api); err != nil || w.Code != 200 {
		t.Fatalf("API: %d %v", w.Code, err)
	}
	if len(api.Folders) != 1 || api.Folders[0].ID != folder || len(api.Files) != 1 || api.Files[0].URL != p.URL || len(api.Files[0].Variants) != 3 {
		t.Errorf("API: %+v", api)
	}
	json.Unmarshal(e.get("/admin/media/api?q=anna").Body.Bytes(), &api)
	if len(api.Files) != 1 || len(api.Folders) != 0 {
		t.Errorf("Suche: %+v", api)
	}
	json.Unmarshal(e.get("/admin/media/api?q=gibtsnicht").Body.Bytes(), &api)
	if len(api.Files) != 0 {
		t.Errorf("Suche ohne Treffer: %+v", api)
	}
	if w := e.get("/admin/media/api?folder=999"); w.Code != http.StatusNotFound {
		t.Errorf("unbekannter Ordner: %d", w.Code)
	}
	json.Unmarshal(e.get(fmt.Sprintf("/admin/media/api?folder=%d", folder)).Body.Bytes(), &api)
	if len(api.Files) != 0 || len(api.Trail) != 1 {
		t.Errorf("Ordneransicht: %+v", api)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"  a\tb\n c ":            "a b c",
		"x\x00y":                 "x y",
		strings.Repeat("ä", 200): strings.Repeat("ä", maxFileName),
		"\xff\xfeOK":             "OK",
	} {
		if got := cleanName(in, maxFileName); got != want {
			t.Errorf("cleanName(%q) = %q, erwartet %q", in, got, want)
		}
	}
	if k := newKey(); !fileRE.MatchString(k + "-480.webp") {
		t.Errorf("Schlüssel %q passt nicht zum Dateimuster", k)
	}
}
