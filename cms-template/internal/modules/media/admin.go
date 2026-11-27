package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

const msgInternal = "Es ist ein Fehler aufgetreten. Bitte später erneut versuchen."

// pick ist das Bild-Objekt des JS-Vertrags (core.ExtEditor) und der API.
type pick struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	URL      string        `json:"url"`
	Thumb    string        `json:"thumb"`
	Alt      string        `json:"alt"`
	Width    int           `json:"width"`
	Height   int           `json:"height"`
	Variants []pickVariant `json:"variants"`
}

type pickVariant struct {
	URL   string `json:"url"`
	Width int    `json:"width"`
}

func toPick(f file) pick {
	p := pick{ID: f.ID, Name: f.Name, URL: f.src(), Thumb: f.thumb(), Alt: f.Alt, Width: f.Width, Height: f.Height}
	for _, v := range f.Variants {
		p.Variants = append(p.Variants, pickVariant{URL: f.url(v.Width), Width: v.Width})
	}
	// src-Maße = Maße der src-Variante (für width/height am <img>)
	for _, v := range f.Variants {
		if f.url(v.Width) == p.URL {
			p.Width, p.Height = v.Width, v.Height
		}
	}
	return p
}

func (m *module) ready(w http.ResponseWriter) bool {
	if m.st == nil || m.d.Admin == nil {
		http.Error(w, "Diese Seite ist noch nicht verfügbar.", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func pathID(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0
	}
	return id
}

func formID(v string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || id < 0 {
		return 0
	}
	return id
}

func libraryURL(folder int64) string {
	if folder == 0 {
		return "/admin/media"
	}
	return "/admin/media?folder=" + strconv.FormatInt(folder, 10)
}

// ---------- Mediathek ----------

type tile struct {
	pick
	FullWidth  int // größte Variante (Originalgröße nach Verkleinerung)
	FullHeight int
	Decorative bool
	Size       string
	EditURL    string
}

type libraryView struct {
	Folder     int64
	FolderName string
	Trail      []folder
	Folders    []folder
	Files      []tile
	More       bool
	Next       string
	Query      string
	Error      string
	IsAdmin    bool
	MaxBytes   int
}

func (m *module) library(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	m.renderLibrary(w, r, http.StatusOK, formID(r.URL.Query().Get("folder")), "")
}

func (m *module) renderLibrary(w http.ResponseWriter, r *http.Request, status int, folderID int64, errMsg string) {
	ctx := r.Context()
	q := r.URL.Query()
	offset := int(formID(q.Get("offset")))
	v := libraryView{Folder: folderID, Query: strings.TrimSpace(q.Get("q")), Error: errMsg, IsAdmin: core.IsAdmin(ctx), MaxBytes: maxVariantBytes}
	err := func() error {
		if folderID != 0 {
			t, err := m.st.trail(ctx, folderID)
			if err != nil {
				return err
			}
			v.Trail = t
			if len(t) > 0 {
				v.FolderName = t[len(t)-1].Name
			}
		}
		var err error
		if v.Query == "" {
			if v.Folders, err = m.st.children(ctx, folderID); err != nil {
				return err
			}
		}
		files, more, err := m.st.files(ctx, folderID, v.Query, offset)
		if err != nil {
			return err
		}
		for _, f := range files {
			v.Files = append(v.Files, tile{pick: toPick(f), FullWidth: f.Width, FullHeight: f.Height, Decorative: f.Decorative, Size: formatBytes(f.Bytes),
				EditURL: "/admin/media/files/" + strconv.FormatInt(f.ID, 10)})
		}
		if more {
			nq := url.Values{"offset": {strconv.Itoa(offset + pageSize)}}
			if folderID != 0 {
				nq.Set("folder", strconv.FormatInt(folderID, 10))
			}
			if v.Query != "" {
				nq.Set("q", v.Query)
			}
			v.More, v.Next = true, "/admin/media?"+nq.Encode()
		}
		return nil
	}()
	if errors.Is(err, errNotFound) {
		http.Redirect(w, r, "/admin/media", http.StatusSeeOther)
		return
	}
	if err != nil {
		slog.Error("media: Mediathek", "err", err)
		status, v.Error = http.StatusInternalServerError, msgInternal
	}
	m.d.Admin.Render(w, r, status, core.AdminPage{Title: "Medien", Template: "media/library.html", Data: v})
}

// ---------- API für den Bildwähler ----------

type apiFolder struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type apiView struct {
	Folder  int64       `json:"folder"`
	Trail   []apiFolder `json:"trail"`
	Folders []apiFolder `json:"folders"`
	Files   []pick      `json:"files"`
	More    bool        `json:"more"`
}

func (m *module) api(w http.ResponseWriter, r *http.Request) {
	if m.st == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Die Mediathek ist nicht verfügbar."})
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	folderID, query, offset := formID(q.Get("folder")), strings.TrimSpace(q.Get("q")), int(formID(q.Get("offset")))
	v := apiView{Folder: folderID, Trail: []apiFolder{}, Folders: []apiFolder{}, Files: []pick{}}
	err := func() error {
		t, err := m.st.trail(ctx, folderID)
		if err != nil {
			return err
		}
		for _, f := range t {
			v.Trail = append(v.Trail, apiFolder{f.ID, f.Name})
		}
		if query == "" {
			cs, err := m.st.children(ctx, folderID)
			if err != nil {
				return err
			}
			for _, f := range cs {
				v.Folders = append(v.Folders, apiFolder{f.ID, f.Name})
			}
		}
		files, more, err := m.st.files(ctx, folderID, query, offset)
		for _, f := range files {
			v.Files = append(v.Files, toPick(f))
		}
		v.More = more
		return err
	}()
	if errors.Is(err, errNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Den Ordner gibt es nicht mehr."})
		return
	}
	if err != nil {
		slog.Error("media: API", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": msgInternal})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// ---------- Upload ----------

// upload nimmt die im Browser erzeugten Varianten entgegen (multipart:
// Felder name, alt, decorative, folder_id und 1–3 Teile „variant“). Das
// CSRF-Token kommt im Header X-CSRF-Token, damit der Kern den Body nicht
// vorab einliest. Antwort JSON: pick bzw. {error}.
func (m *module) upload(w http.ResponseWriter, r *http.Request) {
	if m.st == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Die Mediathek ist nicht verfügbar."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	meta, vs, msg := readUpload(r)
	if msg == "" {
		msg = checkVariants(vs)
	}
	if msg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		return
	}
	uid, _ := core.CurrentUser(r.Context())
	f, err := m.st.create(r.Context(), meta, vs, uid)
	var in errInput
	if errors.As(err, &in) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": string(in)})
		return
	}
	if err != nil {
		slog.Error("media: Upload", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Das Bild konnte nicht gespeichert werden. Bitte später erneut versuchen."})
		return
	}
	writeJSON(w, http.StatusCreated, toPick(f))
}

// readUpload liest den Multipart-Body als Strom: Felder höchstens 1 KB,
// Varianten höchstens maxVariantBytes, höchstens drei. msg ≠ "": Kundenfehler.
func readUpload(r *http.Request) (meta, []variant, string) {
	var mt meta
	var vs []variant
	mr, err := r.MultipartReader()
	if err != nil {
		return mt, nil, "Ungültige Anfrage – bitte das Bild über die Mediathek hochladen."
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return mt, nil, tooLarge(err, "Die Anfrage ist unvollständig. Bitte erneut versuchen.")
		}
		name := part.FormName()
		if name == "variant" {
			if len(vs) == len(widths) {
				return mt, nil, "Zu viele Bildgrößen (höchstens drei)."
			}
			b, err := io.ReadAll(io.LimitReader(part, maxVariantBytes+1))
			if err != nil {
				return mt, nil, tooLarge(err, "Die Anfrage ist unvollständig. Bitte erneut versuchen.")
			}
			if len(b) > maxVariantBytes {
				return mt, nil, fmt.Sprintf("Eine Bildgröße ist zu groß (höchstens %d MB).", maxVariantBytes>>20)
			}
			mime, wd, ht, ok := sniff(b)
			if !ok {
				return mt, nil, "Nur WebP- und JPEG-Bilder sind erlaubt (die Mediathek wandelt Fotos beim Hochladen automatisch um)."
			}
			vs = append(vs, variant{Mime: mime, Width: wd, Height: ht, Data: b})
			continue
		}
		b, err := io.ReadAll(io.LimitReader(part, 1025))
		if err != nil {
			return mt, nil, tooLarge(err, "Die Anfrage ist unvollständig. Bitte erneut versuchen.")
		}
		if len(b) > 1024 {
			return mt, nil, "Ein Eingabefeld ist zu lang."
		}
		switch val := string(b); name {
		case "name":
			mt.Name = val
		case "alt":
			mt.Alt = val
		case "decorative":
			mt.Decorative = val == "1"
		case "folder_id":
			mt.FolderID = formID(val)
		}
	}
	return mt, vs, ""
}

func tooLarge(err error, otherwise string) string {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return fmt.Sprintf("Die Anfrage ist zu groß (höchstens %d MB).", maxUploadBytes>>20)
	}
	return otherwise
}

// ---------- Ordner ----------

func (m *module) createFolder(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	parent := formID(r.PostFormValue("parent_id"))
	_, err := m.st.createFolder(r.Context(), parent, r.PostFormValue("name"))
	m.afterFolder(w, r, parent, parent, err)
}

func (m *module) renameFolder(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id := pathID(r)
	m.afterFolder(w, r, id, id, m.st.renameFolder(r.Context(), id, r.PostFormValue("name")))
}

func (m *module) deleteFolder(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id := pathID(r)
	parent, err := m.st.deleteFolder(r.Context(), id)
	m.afterFolder(w, r, id, parent, err)
}

// afterFolder: Erfolg → zurück in den Ordner to; Eingabefehler → Ordner at
// mit Meldung (400).
func (m *module) afterFolder(w http.ResponseWriter, r *http.Request, at, to int64, err error) {
	var in errInput
	switch {
	case err == nil:
		http.Redirect(w, r, libraryURL(to), http.StatusSeeOther)
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
	case errors.As(err, &in):
		m.renderLibrary(w, r, http.StatusBadRequest, at, string(in))
	default:
		slog.Error("media: Ordner", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
	}
}

// ---------- Datei ----------

type fileView struct {
	File       tile
	Trail      []folder
	FolderID   int64
	Folders    []folderOption
	Variants   []pickVariant
	Created    string
	Refs       []string
	RefsError  bool
	IsAdmin    bool
	Error      string
	Name, Alt  string
	Decorative bool
}

func (m *module) fileDetail(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	m.renderFile(w, r, http.StatusOK, pathID(r), nil, "")
}

func (m *module) renderFile(w http.ResponseWriter, r *http.Request, status int, id int64, form *meta, errMsg string) {
	ctx := r.Context()
	f, err := m.st.file(ctx, id)
	if errors.Is(err, errNotFound) {
		http.NotFound(w, r)
		return
	}
	var v fileView
	if err == nil {
		v = fileView{File: tile{pick: toPick(f), FullWidth: f.Width, FullHeight: f.Height, Decorative: f.Decorative, Size: formatBytes(f.Bytes)},
			FolderID: f.FolderID.Int64, Created: f.CreatedAt.Local().Format("02.01.2006 15:04"),
			IsAdmin: core.IsAdmin(ctx), Error: errMsg, Name: f.Name, Alt: f.Alt, Decorative: f.Decorative}
		v.Variants = v.File.Variants
		if form != nil {
			v.Name, v.Alt, v.Decorative, v.FolderID = form.Name, form.Alt, form.Decorative, form.FolderID
		}
		v.Trail, err = m.st.trail(ctx, f.FolderID.Int64)
	}
	if err == nil {
		v.Folders, err = m.st.allFolders(ctx)
	}
	if err != nil {
		slog.Error("media: Datei", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
		return
	}
	if v.Refs, err = m.refs(ctx, f); err != nil {
		slog.Error("media: Verwendung", "err", err)
		v.RefsError = true
	}
	m.d.Admin.Render(w, r, status, core.AdminPage{Title: f.Name, Template: "media/file.html", Data: v})
}

// refs fragt alle Module, die Medien verwenden (core.MediaReferrer), wo die
// Datei vorkommt.
func (m *module) refs(ctx context.Context, f file) ([]string, error) {
	var out []string
	for _, mod := range m.d.Modules {
		mr, ok := mod.(core.MediaReferrer)
		if !ok {
			continue
		}
		r, err := mr.MediaRefs(ctx, "/media/"+f.Key+"-")
		if err != nil {
			return nil, err
		}
		out = append(out, r...)
	}
	return out, nil
}

func (m *module) updateFile(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	id := pathID(r)
	mt := meta{Name: r.PostFormValue("name"), Alt: r.PostFormValue("alt"),
		Decorative: r.PostFormValue("decorative") == "1", FolderID: formID(r.PostFormValue("folder_id"))}
	err := m.st.update(r.Context(), id, mt)
	var in errInput
	switch {
	case err == nil:
		http.Redirect(w, r, "/admin/media/files/"+strconv.FormatInt(id, 10)+"?saved=1", http.StatusSeeOther)
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
	case errors.As(err, &in):
		m.renderFile(w, r, http.StatusBadRequest, id, &mt, string(in))
	default:
		slog.Error("media: Datei speichern", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
	}
}

// deleteFile: nur Admins – gelöschte Bilder fehlen sofort auf allen Seiten,
// die sie verwenden.
func (m *module) deleteFile(w http.ResponseWriter, r *http.Request) {
	if !m.ready(w) {
		return
	}
	if !core.IsAdmin(r.Context()) {
		http.Error(w, "Bilder dürfen nur Administratoren löschen.", http.StatusForbidden)
		return
	}
	f, err := m.st.remove(r.Context(), pathID(r))
	switch {
	case err == nil:
		http.Redirect(w, r, libraryURL(f.FolderID.Int64), http.StatusSeeOther)
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
	default:
		slog.Error("media: Datei löschen", "err", err)
		http.Error(w, msgInternal, http.StatusInternalServerError)
	}
}

// formatBytes: deutsche Schreibweise, z. B. „1,4 MB“.
func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return strings.Replace(fmt.Sprintf("%.1f MB", float64(n)/(1<<20)), ".", ",", 1)
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", (n+512)>>10)
	}
	return fmt.Sprintf("%d Bytes", n)
}
