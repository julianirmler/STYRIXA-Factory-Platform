package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Der Client hat die vollständige Antwort, bevor die Arbeit danach endet.
func TestAfterResponseClientNotBlocked(t *testing.T) {
	big := strings.Repeat("x", 50_000) // > 2 KB: ohne Puffer würde net/http chunked senden
	cases := []struct {
		name     string
		deferred bool // Handler ruft Defer auf; sonst läuft then
	}{
		{"aufgeschobene Arbeit", true},
		{"Lazy-Job (then)", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			release := make(chan struct{})
			done := make(chan string, 2)
			work := func(context.Context) { <-release; done <- "arbeit" }
			h := AfterResponse(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.deferred {
					Defer(r, work)
				}
				w.Header().Set("Content-Type", "text/plain")
				io.WriteString(w, big)
			}), func(ctx context.Context) {
				if c.deferred {
					done <- "then trotz aufgeschobener Arbeit"
					return
				}
				work(ctx)
			})
			srv := httptest.NewServer(h)
			defer srv.Close()

			got := make(chan error, 1)
			go func() {
				res, err := http.Get(srv.URL)
				if err == nil {
					var b []byte
					b, err = io.ReadAll(res.Body)
					res.Body.Close()
					if err == nil && (len(b) != len(big) || res.Header.Get("Content-Length") != strconv.Itoa(len(big))) {
						err = io.ErrUnexpectedEOF
					}
				}
				got <- err
			}()
			select {
			case err := <-got:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Client wartet auf die Arbeit nach der Antwort")
			}
			close(release)
			if msg := <-done; msg != "arbeit" {
				t.Fatal(msg)
			}
		})
	}
}

// Gestreamte Antworten (Flush) sind nicht abgeschlossen – dann kein Lazy-Job.
func TestAfterResponseStreamingSkipsJob(t *testing.T) {
	ran := false
	h := AfterResponse(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "teil 1")
		http.NewResponseController(w).Flush()
		io.WriteString(w, "teil 2")
	}), func(context.Context) { ran = true })
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(b) != "teil 1teil 2" || ran {
		t.Fatalf("Körper %q, Job gelaufen: %v", b, ran)
	}
}

// Status, Header und Sonderfälle bleiben, wie der Handler sie setzt.
func TestAfterResponsePreservesResponse(t *testing.T) {
	cases := []struct {
		name   string
		method string
		h      http.HandlerFunc
		status int
		cl     string
	}{
		{"ohne Körper", "GET", func(w http.ResponseWriter, r *http.Request) {}, 200, "0"},
		{"Umleitung", "GET", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/x", http.StatusSeeOther) }, 303, ""},
		{"304", "GET", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }, 304, ""},
		{"eigene Content-Length", "GET", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "3")
			w.WriteHeader(201)
			io.WriteString(w, "abc")
		}, 201, "3"},
		{"HEAD", "HEAD", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
		}, 200, "5"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		AfterResponse(c.h, nil).ServeHTTP(w, httptest.NewRequest(c.method, "/", nil))
		if w.Code != c.status {
			t.Errorf("%s: Status %d, erwartet %d", c.name, w.Code, c.status)
		}
		if c.cl != "" && w.Header().Get("Content-Length") != c.cl {
			t.Errorf("%s: Content-Length %q, erwartet %q", c.name, w.Header().Get("Content-Length"), c.cl)
		}
		if c.status == 304 && w.Header().Get("Content-Length") != "" {
			t.Errorf("304 mit Content-Length")
		}
	}
}

// Reset-Mail wird erst nach der Antwort ermittelt und gesendet – bekannte und
// unbekannte E-Mail verhalten sich bis zur Antwort gleich.
func TestForgotDeferredUntilAfterResponse(t *testing.T) {
	e := newAuthEnv(t)
	for _, email := range []string{"unbekannt@example.at", testEmail} {
		a := &After{}
		r := httptest.NewRequest("POST", PathForgot, strings.NewReader(url.Values{"email": {email}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = "203.0.113.9:4000"
		w := httptest.NewRecorder()
		e.h.ServeHTTP(w, a.Attach(r))
		if w.Code != http.StatusOK || !a.Pending() {
			t.Fatalf("%s: %d, aufgeschoben: %v", email, w.Code, a.Pending())
		}
		if len(e.mail.sent) != 0 {
			t.Fatalf("%s: Mail vor Ende der Antwort gesendet", email)
		}
		if n := countRows(t, e.db, `SELECT count(*) FROM core_password_resets`); n != 0 {
			t.Fatalf("%s: Reset-Token vor Ende der Antwort angelegt", email)
		}
		a.Run(context.Background())
	}
	if len(e.mail.sent) != 1 || e.mail.sent[0].to != testEmail {
		t.Fatalf("Mails nach der Antwort: %+v", e.mail.sent)
	}
}
