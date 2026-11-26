package core

import (
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// assetModule bringt statische Dateien und eine Seite mit Style-Nonce mit
// (wie das Content-Modul mit Editor.js, Session 3.2).
type assetModule struct{ fakeAdminModule }

func (assetModule) Assets() fs.FS {
	return fstest.MapFS{"editor.js": {Data: []byte("// editor")}, "editor.css": {Data: []byte("p{}")}}
}

func (m assetModule) Mount(mux *http.ServeMux, d Deps) {
	mux.HandleFunc("GET /admin/ed/seite", func(w http.ResponseWriter, r *http.Request) {
		d.Admin.Render(w, r, http.StatusOK, AdminPage{Title: "Editor", Template: "ed/seite.html", StyleNonce: true})
	})
	mux.HandleFunc("GET /admin/ed/ohne", func(w http.ResponseWriter, r *http.Request) {
		d.Admin.Render(w, r, http.StatusOK, AdminPage{Title: "Ohne", Template: "ed/seite.html"})
	})
}

func newAssetModule() assetModule {
	return assetModule{fakeAdminModule{id: "ed", files: fstest.MapFS{"seite.html": {Data: []byte(
		`<script src="{{asset "ed/editor.js"}}" nonce="{{.StyleNonce}}" defer></script><link rel="stylesheet" href="{{asset "ed/editor.css"}}">`)}}}}
}

func TestAdminModuleAssets(t *testing.T) {
	e := newAuthEnv(t, newAssetModule())
	tok := e.login(t, reqOpt{})
	body := e.req("GET", "/admin/ed/seite", nil, reqOpt{cookie: tok}).Body.String()
	src := regexp.MustCompile(`src="(/admin/static/ed-editor\.[0-9a-f]{10}\.js)"`).FindStringSubmatch(body)
	if src == nil || !strings.Contains(body, `href="/admin/static/ed-editor.`) {
		t.Fatalf("Modul-Dateien fehlen im Template:\n%s", body)
	}
	w := e.req("GET", src[1], nil, reqOpt{})
	if w.Code != 200 || w.Body.String() != "// editor" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("%s: %d %q %q", src[1], w.Code, w.Body, w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control %q", w.Header().Get("Cache-Control"))
	}
	if _, err := (&Admin{}).assetURL("ed/gibtsnicht.js"); err == nil {
		t.Error("unbekannte Datei ohne Fehler")
	}
}

func TestAdminStyleNonce(t *testing.T) {
	e := newAuthEnv(t, newAssetModule())
	tok := e.login(t, reqOpt{})
	nonceAttr := regexp.MustCompile(`nonce="([A-Za-z0-9_-]{22})"`)
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		w := e.req("GET", "/admin/ed/seite", nil, reqOpt{cookie: tok})
		m := nonceAttr.FindStringSubmatch(w.Body.String())
		if m == nil {
			t.Fatalf("keine Nonce im Template:\n%s", w.Body)
		}
		csp := w.Header().Get("Content-Security-Policy")
		want := adminCSP + "; style-src 'self' 'nonce-" + m[1] + "'; img-src 'self' data:"
		if csp != want {
			t.Errorf("CSP\n got %q\nwant %q", csp, want)
		}
		if strings.Contains(csp, "unsafe") || strings.Contains(csp, "script-src") {
			t.Errorf("CSP lockert Skripte oder erlaubt unsafe-*: %q", csp)
		}
		seen[m[1]] = true
	}
	if len(seen) != 2 {
		t.Error("Nonce ist nicht pro Anfrage neu")
	}
	// Ohne StyleNonce: unveränderte Admin-CSP, leere Nonce.
	w := e.req("GET", "/admin/ed/ohne", nil, reqOpt{cookie: tok})
	if w.Header().Get("Content-Security-Policy") != adminCSP || !strings.Contains(w.Body.String(), `nonce=""`) {
		t.Errorf("ohne StyleNonce: %q\n%s", w.Header().Get("Content-Security-Policy"), w.Body)
	}
}
