// Package deploy liefert ein gebautes STYRIXA-Binary direkt auf den Server
// oder Webspace des Kunden aus. Zwei Wege:
//
//   - SSH  (VPS, Managed Server, Hosting mit SSH-Zugang): Release-Ordner,
//     atomarer Symlink-Wechsel, systemd-User-Service, Health-Check, Rollback.
//   - FTP(S) (klassischer Shared-Webspace mit Apache): CGI-Modus – das Binary
//     wird pro Anfrage von Apache gestartet; .htaccess leitet alles darauf.
//
// Zugangsdaten werden nur für die Dauer eines Deployments im Speicher gehalten
// und weder geloggt noch gespeichert. Server-Identität wird immer geprüft:
// SSH-Host-Key bzw. TLS-Zertifikat per Fingerprint, den der Kunde bestätigt.
package deploy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Method string

const (
	MethodSSH Method = "ssh"
	MethodFTP Method = "ftp"
)

// FTPTLS beschreibt die Transportverschlüsselung für FTP.
type FTPTLS string

const (
	FTPExplicitTLS FTPTLS = "explicit" // AUTH TLS auf Port 21 (Standard)
	FTPImplicitTLS FTPTLS = "implicit" // FTPS auf Port 990
	FTPPlain       FTPTLS = "none"     // unverschlüsselt – nur mit AllowPlaintext
)

// Target beschreibt das Ziel. Wird aus dem UI-Formular befüllt.
type Target struct {
	Method    Method `json:"method"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	User      string `json:"user"`
	RemoteDir string `json:"remoteDir"` // SSH: App-Ordner (rel. zu ~), FTP: Document-Root
	PublicURL string `json:"publicUrl"` // für den Health-Check, z. B. https://kunde.at

	// SSH
	AppPort int `json:"appPort"` // lokaler Port der App hinter dem Reverse-Proxy

	// FTP
	TLS            FTPTLS `json:"tls"`
	AllowPlaintext bool   `json:"allowPlaintext"`
	DataDir        string `json:"dataDir"` // relativ zum Document-Root, Standard "styrixa-data"

	// Vom Kunden bestätigter Fingerprint (SHA256:...). Leer beim ersten Versuch:
	// dann liefert Deploy einen *UnknownIdentityError zur Bestätigung im UI.
	Fingerprint string `json:"fingerprint"`

	// Nur für Tests/Entwicklung: private Netze erlauben. Nie aus JSON befüllt.
	AllowPrivateNetworks bool `json:"-"`
}

// Credentials – niemals serialisieren oder loggen.
type Credentials struct {
	Password      string
	PrivateKeyPEM []byte // OpenSSH- oder PKCS#8-Schlüssel (z. B. STYRIXA-Deploy-Key)
}

func (Credentials) String() string { return "Credentials{***}" }

// Artifact ist das Ergebnis eines Factory-Builds.
type Artifact struct {
	Name       string // Paketname, z. B. "kunde-cms"
	BinaryPath string // lokaler Pfad des Binarys auf dem Build-Worker
	GOOS       string
	GOARCH     string
	Modules    []string
}

// Event ist eine Zeile im Deploy-Log (wird per SSE ans UI gestreamt).
type Event struct {
	Time  time.Time `json:"time"`
	Step  string    `json:"step"`
	Level string    `json:"level"` // info | warn | ok | error
	Msg   string    `json:"msg"`
}

type Logger func(Event)

func (l Logger) emit(step, level, format string, a ...any) {
	if l != nil {
		l(Event{Time: time.Now(), Step: step, Level: level, Msg: fmt.Sprintf(format, a...)})
	}
}

// Result fasst ein erfolgreiches Deployment zusammen.
type Result struct {
	Method   Method            `json:"method"`
	Release  string            `json:"release"`
	URL      string            `json:"url"`
	Hints    []string          `json:"hints,omitempty"`
	Snippets map[string]string `json:"snippets,omitempty"` // z. B. Caddy/nginx-Konfiguration

	// Einmal-Code für /install; leer, wenn das CMS schon installiert ist.
	// Geheimnis: nur im Result an das UI, nie ins Deploy-Log (Event).
	InstallCode string `json:"installCode,omitempty"`
}

// UnknownIdentityError: Server-Identität ist (noch) nicht bestätigt.
// Das UI zeigt den Fingerprint an; nach Bestätigung erneut mit Target.Fingerprint deployen.
type UnknownIdentityError struct {
	Kind        string // "ssh-host-key" | "tls-certificate"
	Fingerprint string
	Mismatch    bool // true = abweichend vom bestätigten Wert → möglicher Angriff
}

func (e *UnknownIdentityError) Error() string {
	if e.Mismatch {
		return fmt.Sprintf("%s hat sich geändert (%s) – Deployment abgebrochen", e.Kind, e.Fingerprint)
	}
	return fmt.Sprintf("%s nicht bestätigt: %s", e.Kind, e.Fingerprint)
}

var (
	reHost = regexp.MustCompile(`^[A-Za-z0-9.-]+$|^\[[0-9A-Fa-f:]+\]$`)
	reUser = regexp.MustCompile(`^[A-Za-z0-9._@+-]+$`)
	rePath = regexp.MustCompile(`^[A-Za-z0-9._/~+-]*$`)
	reName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// Validate prüft Eingaben streng – sie landen in Shell- und FTP-Befehlen.
func (t *Target) Validate() error {
	var errs []error
	if t.Method != MethodSSH && t.Method != MethodFTP {
		errs = append(errs, errors.New("method muss ssh oder ftp sein"))
	}
	if !reHost.MatchString(t.Host) || strings.HasPrefix(t.Host, "-") {
		errs = append(errs, errors.New("ungültiger Host (ohne Protokoll, z. B. ftp.kunde.at)"))
	}
	if !reUser.MatchString(t.User) {
		errs = append(errs, errors.New("ungültiger Benutzername"))
	}
	if err := validPath(t.RemoteDir); err != nil {
		errs = append(errs, fmt.Errorf("remoteDir: %w", err))
	}
	if t.Port < 0 || t.Port > 65535 {
		errs = append(errs, errors.New("ungültiger Port"))
	}
	switch t.Method {
	case MethodSSH:
		if t.Port == 0 {
			t.Port = 22
		}
		if t.AppPort == 0 {
			t.AppPort = 8080
		}
		if t.AppPort < 1024 || t.AppPort > 65535 {
			errs = append(errs, errors.New("appPort muss zwischen 1024 und 65535 liegen"))
		}
		if t.RemoteDir == "" {
			t.RemoteDir = "styrixa"
		}
	case MethodFTP:
		if t.TLS == "" {
			t.TLS = FTPExplicitTLS
		}
		if t.TLS == FTPPlain && !t.AllowPlaintext {
			errs = append(errs, errors.New("unverschlüsseltes FTP muss ausdrücklich erlaubt werden (allowPlaintext)"))
		}
		if t.Port == 0 {
			t.Port = 21
			if t.TLS == FTPImplicitTLS {
				t.Port = 990
			}
		}
		if t.DataDir == "" {
			t.DataDir = "styrixa-data"
		}
		if err := validPath(t.DataDir); err != nil || strings.HasPrefix(t.DataDir, "/") {
			errs = append(errs, errors.New("dataDir muss ein relativer Ordner sein"))
		}
		if t.PublicURL == "" {
			errs = append(errs, errors.New("publicUrl wird für den Health-Check benötigt"))
		}
	}
	if t.PublicURL != "" && !strings.HasPrefix(t.PublicURL, "https://") && !strings.HasPrefix(t.PublicURL, "http://") {
		errs = append(errs, errors.New("publicUrl muss mit https:// beginnen"))
	}
	return errors.Join(errs...)
}

func validPath(p string) error {
	if !rePath.MatchString(p) {
		return errors.New("nur Buchstaben, Ziffern und . _ / ~ + - erlaubt")
	}
	if strings.HasPrefix(p, "-") {
		return errors.New("darf nicht mit - beginnen")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return errors.New(".. ist nicht erlaubt")
		}
	}
	if strings.Contains(p, "~") && p != "~" && !strings.HasPrefix(p, "~/") {
		return errors.New("~ nur am Anfang erlaubt")
	}
	return nil
}

// ServiceName leitet einen sicheren systemd-Namen aus dem Paketnamen ab.
func ServiceName(pkg string) string {
	s := strings.ToLower(pkg)
	s = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if !reName.MatchString(s) {
		s = "cms"
	}
	return "styrixa-" + s
}

// shellQuote setzt einen Wert sicher in einfache Anführungszeichen.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// releaseID ist der Name eines Release-Ordners (sortierbar).
func releaseID(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

var reRelease = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)
