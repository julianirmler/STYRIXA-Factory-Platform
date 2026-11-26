package content

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// Admin (minimal, Session 3.1): Liste, Anlegen, Einstellungen, Entwurf per
// JSON, Veröffentlichen, Zurückziehen, Löschen; Block-Editor seit 3.2.
// Login und CSRF prüft der Kern (Auth.Require) für alles unter /admin/*.

const msgInternal = "Interner Fehler – bitte später erneut versuchen."

type pageRow struct {
	ID                  int64
	Title, Path, Status string
	Published, Home     bool
	Updated             string
	Indent              string
	ParentID            int64
	Slug                string
	PublicURL           string
}

type option struct {
	ID   int64
	Path string
}

type listView struct {
	Pages   []pageRow
	Parents []option
	Error   string
	Title   string // Formularwert nach Fehler
}

type editView struct {
	Page      pageRow
	Parents   []option
	Error     string
	Editor    editorView
	Revisions []revRow
}

// revRow ist eine Zeile der Revisionsliste.
type revRow struct {
	ID         int64
	Saved      string
	Author     string
	Title      string
	HTML       bool
	Auto       bool
	Current    bool // neueste Revision = Entwurf im Editor
	Published  bool
	PreviewURL string
	RestoreURL string
	CanRestore bool // HTML-Modus nur für Admins (Raw-Blöcke prüft der Server)
}

// editorView füttert den Block-Editor (Session 3.2). Body geht als
// data-Attribut an die Seite (html/template escapt), nicht als Inline-Skript.
type editorView struct {
	Title     string
	Body      string // Editor.js-JSON
	RevID     int64  // Basis für die Konfliktprüfung beim Speichern
	HTMLMode  bool   // neueste Revision im HTML-Modus: Textarea statt Block-Editor
	AllowRaw  bool   // Raw-HTML-Block und HTML-Modus nur für Admins (serverseitig geprüft in Validate)
	DraftURL  string
	PageURL   string // /admin/content/pages/{id}
	Published bool
}

func fmtTime(unix int64) string { return time.Unix(unix, 0).Format("02.01.2006 15:04") }

func row(p page) pageRow {
	r := pageRow{ID: p.ID, Title: p.Title, Path: "/" + p.Path, Status: "Entwurf", Published: p.Status == statusPublished,
		Home: p.IsHome, Updated: fmtTime(p.UpdatedAt), Indent: strings.Repeat("– ", depth(p.Path)-1),
		ParentID: p.ParentID, Slug: p.Slug}
	if r.Published {
		r.Status = "Veröffentlicht"
		r.PublicURL = r.Path
		if p.IsHome {
			r.PublicURL = "/"
		}
	}
	return r
}

func (m *module) ready(w http.ResponseWriter) bool {
	if m.st == nil || m.d.Admin == nil {
		http.Error(w, "Diese Seite ist noch nicht verfügbar.", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func pageID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// status ordnet einen Fehler einem HTTP-Status und einer Kundenmeldung zu.
func status(err error) (int, string) {
	if errors.Is(err, errNotFound) {
		return http.StatusNotFound, msgPageNotFound
	}
	if errors.Is(err, errRevNotFound) {
		return http.StatusNotFound, err.Error()
	}
	if ie, ok := IsInputError(err); ok {
		if ie.Forbidden {
			return http.StatusForbidden, err.Error()
		}
		return http.StatusBadRequest, err.Error()
	}
	slog.Error("content", "err", err)
	return http.StatusInternalServerError, msgInternal
}

func (m *module) allPages(r *http.Request) ([]page, []option, error) {
	ps, err := m.st.pages(r.Context())
	if err != nil {
		return nil, nil, err
	}
	opts := make([]option, 0, len(ps))
	for _, p := range ps {
		opts = append(opts, option{ID: p.ID, Path: "/" + p.Path})
	}
	return ps, opts, nil
}

func (m *module) renderList(w http.ResponseWriter, r *http.Request, code int, v listView) {
	ps, opts, err := m.allPages(r)
	if err != nil {
		code, v.Error = status(err)
	}
	for _, p := range ps {
		v.Pages = append(v.Pages, row(p))
	}
	v.Parents = opts
	m.d.Admin.Render(w, r, code, core.AdminPage{Title: "Seiten", Template: "content/list.html", Data: v})
}

func (m *module) list(w http.ResponseWriter, r *http.Request) {
	if m.ready(w) {
		m.renderList(w, r, http.StatusOK, listView{})
	}
}

func (m *module) create(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	parent, _ := strconv.ParseInt(r.PostFormValue("parent_id"), 10, 64)
	author, _ := core.CurrentUser(r.Context())
	id, err := m.st.create(r.Context(), r.PostFormValue("title"), parent, author)
	if err != nil {
		code, msg := status(err)
		m.renderList(w, r, code, listView{Error: msg, Title: r.PostFormValue("title")})
		return
	}
	http.Redirect(w, r, "/admin/content/pages/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (m *module) renderEdit(w http.ResponseWriter, r *http.Request, id int64, code int, msg string) {
	p, err := m.st.page(r.Context(), id)
	if err != nil {
		code, msg := status(err)
		m.renderList(w, r, code, listView{Error: msg})
		return
	}
	_, opts, err := m.allPages(r)
	if err != nil {
		code, msg = status(err)
	}
	// Als Elternseite wählbar: alles außer der Seite selbst und ihren Unterseiten.
	parents := opts[:0:0]
	for _, o := range opts {
		if o.ID != id && !strings.HasPrefix(strings.ToLower(o.Path)+"/", strings.ToLower("/"+p.Path)+"/") {
			parents = append(parents, o)
		}
	}
	d, err := m.st.latestDraft(r.Context(), id)
	if err != nil {
		code, msg = status(err)
	}
	admin := core.IsAdmin(r.Context())
	base := pageURL(id)
	ev := editorView{Title: d.Title, Body: d.Body, RevID: d.RevID, HTMLMode: d.Format == FormatHTML,
		AllowRaw: admin, DraftURL: base + "/draft", PageURL: base, Published: p.Status == statusPublished}
	if ev.Title == "" {
		ev.Title = p.Title
	}
	revs, err := m.st.revisions(r.Context(), id)
	if err != nil {
		code, msg = status(err)
	}
	rows := make([]revRow, 0, len(revs))
	for i, rv := range revs {
		rid := strconv.FormatInt(rv.ID, 10)
		rows = append(rows, revRow{ID: rv.ID, Saved: fmtTime(rv.SavedAt), Author: rv.AuthorEmail, Title: rv.Title,
			HTML: rv.Format == FormatHTML, Auto: rv.Auto, Current: i == 0, Published: rv.ID == p.PubRev,
			PreviewURL: base + "/preview?rev=" + rid, RestoreURL: base + "/revisions/" + rid + "/restore",
			CanRestore: admin || rv.Format != FormatHTML})
	}
	m.d.Admin.Render(w, r, code, core.AdminPage{Title: "Seite · " + p.Title, Template: "content/page.html",
		Data: editView{Page: row(p), Parents: parents, Error: msg, Editor: ev, Revisions: rows}, StyleNonce: true})
}

func pageURL(id int64) string { return "/admin/content/pages/" + strconv.FormatInt(id, 10) }

func (m *module) edit(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id, ok := pageID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	m.renderEdit(w, r, id, http.StatusOK, "")
}

func (m *module) saveSettings(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id, ok := pageID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	parent, _ := strconv.ParseInt(r.PostFormValue("parent_id"), 10, 64)
	err := m.st.settings(r.Context(), id, r.PostFormValue("slug"), parent, r.PostFormValue("home") == "1")
	if err != nil {
		code, msg := status(err)
		m.renderEdit(w, r, id, code, msg)
		return
	}
	http.Redirect(w, r, "/admin/content/pages/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// action: Formular-POST auf eine Seite, danach zurück zur Seite.
func (m *module) action(fn func(*store, context.Context, int64) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !m.ready(w) {
			return
		}
		id, ok := pageID(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		if err := fn(m.st, r.Context(), id); err != nil {
			code, msg := status(err)
			m.renderEdit(w, r, id, code, msg)
			return
		}
		http.Redirect(w, r, "/admin/content/pages/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	}
}

func (m *module) remove(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id, ok := pageID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := m.st.delete(r.Context(), id); err != nil {
		code, msg := status(err)
		if code == http.StatusNotFound {
			m.renderList(w, r, code, listView{Error: msg})
			return
		}
		m.renderEdit(w, r, id, code, msg)
		return
	}
	http.Redirect(w, r, "/admin/content", http.StatusSeeOther)
}

// draftRequest ist der JSON-Körper von POST …/draft. body ist bei format
// „blocks“ das Editor.js-Objekt, bei „html“ ein String.
type draftRequest struct {
	Title  string          `json:"title"`
	Format string          `json:"format"`
	Body   json.RawMessage `json:"body"`
	Base   int64           `json:"base_revision_id"` // ab 3.3: Revision, auf der der Editor aufbaut
	Auto   bool            `json:"autosave"`         // ab 3.3: automatisch gespeichert
}

// draftResponse ist die Antwort von POST …/draft.
type draftResponse struct {
	RevisionID int64  `json:"revision_id"`
	Notice     string `json:"notice,omitempty"` // Hinweis auf eine überholte fremde Revision
}

func (m *module) draft(w http.ResponseWriter, r *http.Request) {
	if m.st == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Diese Funktion ist noch nicht verfügbar."})
		return
	}
	id, ok := pageID(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": msgPageNotFound})
		return
	}
	var req draftRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes+64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		msg := "Die Anfrage ist kein gültiges JSON."
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			msg = "Der Inhalt ist zu groß (höchstens 1 MB)."
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	if req.Format == "" {
		req.Format = FormatBlocks
	}
	body := string(req.Body)
	if req.Format == FormatHTML {
		if err := json.Unmarshal(req.Body, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Im HTML-Modus muss „body“ ein Text sein."})
			return
		}
	}
	ctx := r.Context()
	err := Validate(body, req.Format, core.IsAdmin(ctx))
	var resp draftResponse
	var newer *newerRevision
	if err == nil {
		author, _ := core.CurrentUser(ctx)
		resp.RevisionID, newer, err = m.st.saveDraft(ctx, id, draftSave{Title: req.Title, Format: req.Format, Body: body,
			Author: author, Base: req.Base, Auto: req.Auto})
		if newer != nil {
			resp.Notice = conflictNotice(newer, author)
		}
	}
	if err != nil {
		code, msg := status(err)
		writeJSON(w, code, map[string]string{"error": msg})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// conflictNotice: gleichzeitiges Bearbeiten – die letzte Revision gewinnt,
// der Editor erfährt, dass er eine neuere fremde Fassung überholt hat.
func conflictNotice(n *newerRevision, author int64) string {
	who := "In einem anderen Fenster wurde"
	if n.AuthorID != author {
		who = "Jemand anderes hat"
		if n.AuthorEmail != "" {
			who = n.AuthorEmail + " hat"
		}
	}
	return who + " am " + fmtTime(n.SavedAt) + " eine neuere Fassung gespeichert. Sie wurde durch diesen Stand ersetzt und ist unter „Revisionen“ wiederherstellbar."
}

// preview zeigt den Entwurf (oder ?rev=…) im öffentlichen Layout – nur
// eingeloggt (/admin/*), nie indexierbar.
func (m *module) preview(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	if m.d.Public == nil {
		http.Error(w, "Die Vorschau ist noch nicht verfügbar.", http.StatusServiceUnavailable)
		return
	}
	id, ok := pageID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	p, err := m.st.page(ctx, id)
	if err != nil {
		code, msg := status(err)
		http.Error(w, msg, code)
		return
	}
	var title, format, body string
	if q := r.URL.Query().Get("rev"); q != "" {
		revID, perr := strconv.ParseInt(q, 10, 64)
		if perr != nil || revID <= 0 {
			http.NotFound(w, r)
			return
		}
		rv, err := m.st.revision(ctx, id, revID)
		if err != nil {
			code, msg := status(err)
			http.Error(w, msg, code)
			return
		}
		title, format, body = rv.Title, rv.Format, rv.Body
	} else {
		d, err := m.st.latestDraft(ctx, id)
		if err != nil {
			code, msg := status(err)
			http.Error(w, msg, code)
			return
		}
		title, format, body = d.Title, d.Format, d.Body
	}
	html, err := Render(body, format)
	if err != nil {
		code, msg := status(err)
		http.Error(w, msg, code)
		return
	}
	if title == "" {
		title = p.Title
	}
	path := "/" + p.Path
	if p.IsHome {
		path = "/"
	}
	m.d.Public.Preview(w, r, core.PublicView{Title: title, Path: path, Body: previewNote + html})
}

// previewNote steht über dem Inhalt der Vorschau (Inline-Style: die Vorschau-CSP
// erlaubt ihn, Themes kennen keine Klasse dafür).
const previewNote = `<p role="note" style="padding:.5rem 1rem;border-radius:4px;background:#fff3cd;color:#5c4400">` +
	`Vorschau – nicht öffentlich. Öffentlich ist nur, was veröffentlicht wurde.</p>` + "\n"

func (m *module) restore(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id, ok := pageID(r)
	revID, err := strconv.ParseInt(r.PathValue("rev"), 10, 64)
	if !ok || err != nil || revID <= 0 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	author, _ := core.CurrentUser(ctx)
	if _, err := m.st.restore(ctx, id, revID, author, core.IsAdmin(ctx)); err != nil {
		code, msg := status(err)
		m.renderEdit(w, r, id, code, msg)
		return
	}
	http.Redirect(w, r, pageURL(id), http.StatusSeeOther)
}

// switchFormat: Block- ↔ HTML-Modus, nur Admin (serverseitig geprüft).
func (m *module) switchFormat(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id, ok := pageID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()
	if !core.IsAdmin(ctx) {
		m.renderEdit(w, r, id, http.StatusForbidden, "Den HTML-Modus dürfen nur Administratoren verwenden.")
		return
	}
	author, _ := core.CurrentUser(ctx)
	if err := m.st.switchFormat(ctx, id, r.PostFormValue("format"), author); err != nil {
		code, msg := status(err)
		m.renderEdit(w, r, id, code, msg)
		return
	}
	http.Redirect(w, r, pageURL(id), http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
