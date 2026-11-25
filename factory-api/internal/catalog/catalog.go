// Package catalog liest das neueste signierte GitHub-Release mit den
// vorgebauten CMS-Paketen (ADR-004), ordnet eine Modulauswahl dem passenden
// Paket zu und liefert Pakete erst nach Prüfung von Signatur und Prüfsumme aus.
//
// Vertrauenskette: SHA256SUMS ist mit ReleasePublicKey signiert; modules.json
// und jedes Paket müssen darin stehen und dem Hash entsprechen. Alles andere
// aus der GitHub-API (Tag, Download-URLs, Größen) ist nicht vertrauenswürdig.
package catalog

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/julianirmler/styrixa-factory-platform/factory-api/internal/deploy"
)

const (
	maxReleaseJSON = 4 << 20
	maxSums        = 1 << 20
	maxSig         = 1 << 10
	maxManifest    = 64 << 10
	maxPackage     = 128 << 20 // tar.gz
	maxBinary      = 256 << 20 // entpacktes Binary

	retryUnavailable = 10 * time.Second // Mindestabstand neuer Versuche ohne gültigen Katalog
)

var (
	// ErrUnavailable: kein gültiger Release geladen (GitHub nicht erreichbar oder Release ungültig).
	ErrUnavailable = errors.New("Katalog derzeit nicht verfügbar")
	// ErrNotFound: Paket-ID gibt es im aktuellen Release nicht.
	ErrNotFound = errors.New("Paket nicht gefunden")
	// ErrTampered: Paket entspricht nicht der signierten Prüfsumme.
	ErrTampered = errors.New("Paket entspricht nicht der signierten Prüfsumme")

	reModuleID = regexp.MustCompile(`^[a-z0-9]+$`)
	reTarget   = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)
	rePkgID    = regexp.MustCompile(`^([a-z0-9]+(?:-[a-z0-9]+)*)_([a-z0-9]+)_([a-z0-9]+)$`)
)

// Module ist ein Eintrag aus modules.json.
type Module struct {
	ID       string   `json:"id"`
	Nr       string   `json:"nr"`
	Name     string   `json:"name"`
	Required bool     `json:"required,omitempty"`
	Requires []string `json:"requires,omitempty"`
}

type manifest struct {
	Modules []Module `json:"modules"`
	Targets []string `json:"targets"`
}

// Package ist ein vorgebautes, signiertes CMS-Paket.
type Package struct {
	ID      string   `json:"id"` // <key>_<goos>_<goarch>, z. B. media-shop_linux_amd64
	File    string   `json:"file"`
	SHA256  string   `json:"sha256"`
	Size    int64    `json:"size"`
	Release string   `json:"release"`
	Modules []string `json:"modules"` // alle enthaltenen Module inkl. Pflichtmodule, sortiert
	GOOS    string   `json:"goos"`
	GOARCH  string   `json:"goarch"`

	url string
}

// SelectionError: Auswahl ist ungültig (Meldung für Kunden, deutsch).
type SelectionError struct{ Msg string }

func (e *SelectionError) Error() string { return e.Msg }

type asset struct {
	url  string
	size int64
}

type release struct {
	tag     string
	modules []Module
	targets []string
	sums    map[string]string
	assets  map[string]asset
}

// Catalog hält den zuletzt gültig geprüften Release im Speicher.
type Catalog struct {
	Repo      string        // "owner/name"
	APIBase   string        // Standard https://api.github.com
	PublicKey string        // Standard ReleasePublicKey
	CacheDir  string        // geprüfte Pakete und entpackte Binaries
	Token     string        // optional, nur für API-Anfragen (höheres Rate-Limit)
	HTTP      *http.Client  // Standard: Client mit 10 min Timeout
	TTL       time.Duration // Standard 10 min

	mu      sync.Mutex
	cur     *release
	checked time.Time
	loadMu  sync.Mutex
}

func (c *Catalog) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 10 * time.Minute
}

func (c *Catalog) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c *Catalog) publicKey() string {
	if c.PublicKey != "" {
		return c.PublicKey
	}
	return ReleasePublicKey
}

// Load lädt und prüft das neueste Release. Bei Fehler bleibt der bisherige
// gültige Stand erhalten.
func (c *Catalog) Load(ctx context.Context) error {
	r, err := c.fetchRelease(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = time.Now()
	if err != nil {
		return err
	}
	c.cur = r
	return nil
}

// current liefert den gültigen Release; nach Ablauf der TTL wird im
// Hintergrund neu geladen (der alte Stand gilt so lange weiter).
func (c *Catalog) current(ctx context.Context) (*release, error) {
	c.mu.Lock()
	r, age := c.cur, time.Since(c.checked)
	c.mu.Unlock()
	if r != nil {
		if age > c.ttl() && c.loadMu.TryLock() {
			go func() {
				defer c.loadMu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				if err := c.Load(ctx); err != nil {
					slog.Warn("catalog refresh failed, keeping previous release", "err", err)
				}
			}()
		}
		return r, nil
	}
	c.loadMu.Lock()
	defer c.loadMu.Unlock()
	c.mu.Lock()
	r, checked := c.cur, c.checked
	c.mu.Unlock()
	if r != nil {
		return r, nil
	}
	if !checked.IsZero() && time.Since(checked) < retryUnavailable {
		return nil, ErrUnavailable
	}
	if err := c.Load(ctx); err != nil {
		slog.Warn("catalog load failed", "err", err)
		return nil, ErrUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur, nil
}

func (c *Catalog) fetchRelease(ctx context.Context) (*release, error) {
	base := c.APIBase
	if base == "" {
		base = "https://api.github.com"
	}
	var meta struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	b, err := c.get(ctx, strings.TrimRight(base, "/")+"/repos/"+c.Repo+"/releases/latest", maxReleaseJSON, true)
	if err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, fmt.Errorf("release: %w", err)
	}
	r := &release{tag: meta.TagName, assets: map[string]asset{}}
	for _, a := range meta.Assets {
		r.assets[a.Name] = asset{url: a.URL, size: a.Size}
	}
	fetch := func(name string, limit int64) ([]byte, error) {
		a, ok := r.assets[name]
		if !ok {
			return nil, fmt.Errorf("release %s: %s fehlt", r.tag, name)
		}
		return c.get(ctx, a.url, limit, false)
	}

	sums, err := fetch("SHA256SUMS", maxSums)
	if err != nil {
		return nil, err
	}
	sig, err := fetch("SHA256SUMS.sig", maxSig)
	if err != nil {
		return nil, err
	}
	if err := verifySignature(c.publicKey(), sums, sig); err != nil {
		return nil, fmt.Errorf("release %s: %w", r.tag, err)
	}
	if r.sums, err = parseSums(sums); err != nil {
		return nil, fmt.Errorf("release %s: %w", r.tag, err)
	}
	want, ok := r.sums["modules.json"]
	if !ok {
		return nil, fmt.Errorf("release %s: modules.json nicht signiert (nicht in SHA256SUMS)", r.tag)
	}
	mb, err := fetch("modules.json", maxManifest)
	if err != nil {
		return nil, err
	}
	if h := sha256.Sum256(mb); hex.EncodeToString(h[:]) != want {
		return nil, fmt.Errorf("release %s: modules.json: %w", r.tag, ErrTampered)
	}
	var m manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("release %s: modules.json: %w", r.tag, err)
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("release %s: modules.json: %w", r.tag, err)
	}
	r.modules, r.targets = m.Modules, m.Targets
	return r, nil
}

func (m manifest) validate() error {
	ids := map[string]bool{}
	for _, x := range m.Modules {
		if !reModuleID.MatchString(x.ID) || ids[x.ID] {
			return fmt.Errorf("ungültige oder doppelte Modul-ID %q", x.ID)
		}
		ids[x.ID] = true
	}
	for _, x := range m.Modules {
		for _, d := range x.Requires {
			if !ids[d] {
				return fmt.Errorf("%s verlangt unbekanntes Modul %q", x.ID, d)
			}
		}
	}
	for _, t := range m.Targets {
		if !reTarget.MatchString(t) {
			return fmt.Errorf("ungültige Plattform %q", t)
		}
	}
	return nil
}

// get lädt url mit Größenlimit. api=true setzt die GitHub-API-Header und ggf.
// den Token (nie für Asset-Downloads).
func (c *Catalog) get(ctx context.Context, url string, limit int64, api bool) ([]byte, error) {
	resp, err := c.do(ctx, url, api)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: größer als %d Bytes", url, limit)
	}
	return b, nil
}

func (c *Catalog) do(ctx context.Context, url string, api bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "styrixa-factory")
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

// Modules liefert Release-Tag, Module und Plattformen des aktuellen Releases.
func (c *Catalog) Modules(ctx context.Context) (tag string, mods []Module, targets []string, err error) {
	r, err := c.current(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	return r.tag, r.modules, r.targets, nil
}

// Select ordnet eine Modulauswahl und Plattform (z. B. "linux/amd64") dem
// passenden Paket zu. Pflichtmodule werden ergänzt.
func (c *Catalog) Select(ctx context.Context, modules []string, target string) (Package, error) {
	r, err := c.current(ctx)
	if err != nil {
		return Package{}, err
	}
	if !slices.Contains(r.targets, target) {
		return Package{}, &SelectionError{fmt.Sprintf("Plattform %q wird nicht angeboten (verfügbar: %s)", target, strings.Join(r.targets, ", "))}
	}
	byID := map[string]Module{}
	on := map[string]bool{}
	for _, m := range r.modules {
		byID[m.ID] = m
		if m.Required {
			on[m.ID] = true
		}
	}
	for _, id := range modules {
		if _, ok := byID[id]; !ok {
			return Package{}, &SelectionError{fmt.Sprintf("Unbekanntes Modul %q", id)}
		}
		on[id] = true
	}
	var picked []string
	for id := range on {
		for _, d := range byID[id].Requires {
			if !on[d] {
				return Package{}, &SelectionError{fmt.Sprintf("Modul „%s“ benötigt „%s“", byID[id].Name, byID[d].Name)}
			}
		}
		if !byID[id].Required {
			picked = append(picked, id)
		}
	}
	slices.Sort(picked)
	key := strings.Join(picked, "-")
	if key == "" {
		key = "core"
	}
	goos, goarch, _ := strings.Cut(target, "/")
	p, err := r.pkg(key + "_" + goos + "_" + goarch)
	if errors.Is(err, ErrNotFound) {
		return Package{}, &SelectionError{"Für diese Auswahl gibt es kein Paket im aktuellen Release"}
	}
	return p, err
}

// Lookup liefert das Paket mit der ID aus dem aktuellen Release.
func (c *Catalog) Lookup(ctx context.Context, id string) (Package, error) {
	r, err := c.current(ctx)
	if err != nil {
		return Package{}, err
	}
	return r.pkg(id)
}

func (r *release) pkg(id string) (Package, error) {
	m := rePkgID.FindStringSubmatch(id)
	if m == nil {
		return Package{}, ErrNotFound
	}
	file := "styrixa_" + id + ".tar.gz"
	sum, ok := r.sums[file]
	a, ok2 := r.assets[file]
	if !ok || !ok2 {
		return Package{}, ErrNotFound
	}
	var mods []string
	for _, x := range r.modules {
		if x.Required {
			mods = append(mods, x.ID)
		}
	}
	if m[1] != "core" {
		mods = append(mods, strings.Split(m[1], "-")...)
	}
	slices.Sort(mods)
	return Package{ID: id, File: file, SHA256: sum, Size: a.size, Release: r.tag, Modules: mods, GOOS: m[2], GOARCH: m[3], url: a.url}, nil
}

// Open liefert den Pfad des geprüften tar.gz im Cache. Fehlt es oder stimmt
// der Hash nicht (mehr), wird es neu geladen; ein Paket mit falschem Hash
// wird nie übernommen.
func (c *Catalog) Open(ctx context.Context, p Package) (string, error) {
	if c.CacheDir == "" {
		return "", errors.New("catalog: CacheDir fehlt")
	}
	if err := os.MkdirAll(c.CacheDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(c.CacheDir, p.SHA256+".tar.gz")
	if h, err := hashFile(path); err == nil {
		if h == p.SHA256 {
			return path, nil
		}
		slog.Warn("cached package does not match checksum, refetching", "package", p.ID)
		os.Remove(path)
	}
	resp, err := c.do(ctx, p.url, false)
	if err != nil {
		return "", fmt.Errorf("%s laden: %w", p.File, err)
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(c.CacheDir, ".dl-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name()) // nach erfolgreichem Rename ein No-op
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxPackage+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("%s laden: %w", p.File, err)
	}
	if n > maxPackage || hex.EncodeToString(h.Sum(nil)) != p.SHA256 {
		return "", fmt.Errorf("%s: %w", p.File, ErrTampered)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// Artifact prüft das Paket und entpackt das Binary für den Deploy.
func (c *Catalog) Artifact(ctx context.Context, id string) (deploy.Artifact, error) {
	p, err := c.Lookup(ctx, id)
	if err != nil {
		return deploy.Artifact{}, err
	}
	tgz, err := c.Open(ctx, p)
	if err != nil {
		return deploy.Artifact{}, err
	}
	name := "styrixa"
	if p.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Join(c.CacheDir, p.SHA256)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return deploy.Artifact{}, err
	}
	bin := filepath.Join(dir, name)
	// Bei jedem Deploy frisch aus dem eben geprüften tar.gz entpacken, damit
	// nie ein ungeprüftes Binary aus dem Cache ausgeliefert wird.
	if err := extractBinary(tgz, name, bin); err != nil {
		return deploy.Artifact{}, fmt.Errorf("%s: %w", p.File, err)
	}
	return deploy.Artifact{Name: "styrixa", BinaryPath: bin, GOOS: p.GOOS, GOARCH: p.GOARCH, Modules: p.Modules}, nil
}

// extractBinary erwartet genau eine reguläre Datei namens name im Archiv.
func extractBinary(tgz, name, dst string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	hdr, err := tr.Next()
	if err != nil {
		return fmt.Errorf("Archiv leer oder beschädigt: %w", err)
	}
	if hdr.Typeflag != tar.TypeReg || hdr.Name != name {
		return fmt.Errorf("unerwarteter Archiveintrag %q", hdr.Name)
	}
	if hdr.Size > maxBinary {
		return errors.New("Binary zu groß")
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".bin-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(tr, maxBinary+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != hdr.Size || n > maxBinary {
		return errors.New("Binary unvollständig oder zu groß")
	}
	if _, err := tr.Next(); err != io.EOF {
		return errors.New("Archiv enthält mehr als das Binary")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
