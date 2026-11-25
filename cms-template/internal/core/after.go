package core

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// Arbeit nach der Antwort (Session 1.6): Der Client bekommt seine Antwort
// vollständig, erst dann laufen aufgeschobene Aufgaben (z. B. Reset-Mail) bzw.
// ein fälliger Lazy-Job – in derselben Anfrage, ohne Goroutine.
//
//   - Server-Modus: AfterResponse puffert die Antwort, schließt sie mit
//     Content-Length ab und flusht sie, bevor die Arbeit beginnt.
//   - CGI-Modus: main schließt nach cgi.Serve stdout (Apache sieht das Ende der
//     Antwort) und ruft danach After.Run. Apache beendet den Prozess kurz darauf
//     (SIGTERM, nach 3 s SIGKILL) – deshalb AfterBudget.

// AfterBudget ist die Zeit für alle Arbeit nach einer Antwort zusammen.
const AfterBudget = 2500 * time.Millisecond

// maxBuffered: größere Antworten werden direkt gestreamt; dann läuft nach der
// Anfrage nur aufgeschobene Arbeit, kein Lazy-Job (der Client wartet sonst).
const maxBuffered = 1 << 20

type afterKey struct{}

// After sammelt aufgeschobene Arbeit einer Anfrage.
type After struct {
	fns []func(context.Context)
}

// Attach hängt a an die Anfrage, damit Defer dort sammeln kann.
func (a *After) Attach(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), afterKey{}, a))
}

// Pending meldet, ob Arbeit gesammelt wurde.
func (a *After) Pending() bool { return len(a.fns) > 0 }

// Run führt die gesammelte Arbeit aus (Panics werden geloggt, nicht weitergereicht).
func (a *After) Run(ctx context.Context) {
	fns := a.fns
	a.fns = nil
	for _, fn := range fns {
		func() {
			defer func() {
				if p := recover(); p != nil {
					slog.Error("nach der Antwort: panic", "panic", p)
				}
			}()
			fn(ctx)
		}()
	}
}

// Defer lässt fn laufen, nachdem die Antwort beim Client ist. Ohne After in der
// Anfrage (Handler-Tests) läuft fn sofort. fn darf nicht in die Antwort schreiben.
func Defer(r *http.Request, fn func(ctx context.Context)) {
	if a, ok := r.Context().Value(afterKey{}).(*After); ok {
		a.fns = append(a.fns, fn)
		return
	}
	fn(r.Context())
}

// AfterResponse ist die Middleware des Server-Modus. then läuft nach der
// aufgeschobenen Arbeit, aber nur, wenn keine angefallen ist (gemeinsames
// Budget) und die Antwort gepuffert abgeschlossen werden konnte.
func AfterResponse(next http.Handler, then func(ctx context.Context)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := &After{}
		bw := &bufferedWriter{w: w, head: r.Method == http.MethodHead}
		next.ServeHTTP(bw, a.Attach(r))
		complete := bw.finish()

		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), AfterBudget)
		defer cancel()
		switch {
		case a.Pending():
			a.Run(ctx)
		case complete && then != nil:
			then(ctx)
		}
	})
}

// bufferedWriter hält die Antwort zurück, bis der Handler fertig ist, damit
// sie mit Content-Length abgeschlossen werden kann. Flush oder sehr große
// Antworten schalten auf direktes Schreiben um.
type bufferedWriter struct {
	w           http.ResponseWriter
	head        bool
	status      int
	buf         bytes.Buffer
	passthrough bool
}

func (b *bufferedWriter) Header() http.Header { return b.w.Header() }

func (b *bufferedWriter) WriteHeader(code int) {
	switch {
	case b.passthrough || code < 200: // 1xx (z. B. 103) direkt weiterreichen
		b.w.WriteHeader(code)
	case b.status == 0:
		b.status = code
	}
}

func (b *bufferedWriter) Write(p []byte) (int, error) {
	if !b.passthrough {
		if b.buf.Len()+len(p) <= maxBuffered {
			if b.status == 0 {
				b.status = http.StatusOK
			}
			return b.buf.Write(p)
		}
		b.switchToPassthrough()
	}
	return b.w.Write(p)
}

func (b *bufferedWriter) Flush() {
	b.switchToPassthrough()
	_ = http.NewResponseController(b.w).Flush()
}

// Unwrap erlaubt http.ResponseController den Zugriff auf den echten Writer.
func (b *bufferedWriter) Unwrap() http.ResponseWriter { return b.w }

func (b *bufferedWriter) switchToPassthrough() {
	if b.passthrough {
		return
	}
	b.passthrough = true
	if b.status == 0 {
		b.status = http.StatusOK
	}
	b.w.WriteHeader(b.status)
	_, _ = b.w.Write(b.buf.Bytes())
	b.buf = bytes.Buffer{}
}

// finish schreibt die gepufferte Antwort und meldet, ob sie damit beim Client
// vollständig ist (Content-Length gesetzt bzw. ohne Körper).
func (b *bufferedWriter) finish() bool {
	if b.passthrough {
		return false
	}
	if b.status == 0 {
		b.status = http.StatusOK
	}
	h := b.w.Header()
	bodyAllowed := b.status != http.StatusNoContent && b.status != http.StatusNotModified
	if bodyAllowed && !b.head && h.Get("Content-Length") == "" && h.Get("Transfer-Encoding") == "" {
		h.Set("Content-Length", strconv.Itoa(b.buf.Len()))
	}
	b.w.WriteHeader(b.status)
	if b.buf.Len() > 0 {
		_, _ = b.w.Write(b.buf.Bytes())
	}
	return http.NewResponseController(b.w).Flush() == nil
}
