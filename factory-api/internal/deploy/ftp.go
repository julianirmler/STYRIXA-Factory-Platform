package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Schlanker FTP/FTPS-Client (RFC 959, 2228, 4217, 2428) – reines Go.
// Eigenimplementierung, weil SITE CHMOD (für CGI zwingend) in gängigen
// Go-Bibliotheken nicht verfügbar ist.
// ---------------------------------------------------------------------------

type ftpClient struct {
	ctrl     net.Conn
	tp       *textproto.Conn
	tlsCfg   *tls.Config // nil = unverschlüsselt
	host     string
	dialer   net.Dialer
	deadline time.Time // Gesamtfrist des Deployments (aus ctx)
	broken   error     // Steuerverbindung nach Netzwerkfehler unbrauchbar
}

// ftpReplyTimeout: so lange wartet der Client höchstens auf die Antwort
// auf einen Transfer-Befehl (150/125). Ohne eigene Frist würde ein
// hängender Server erst nach der Gesamtfrist (10 min) auffallen.
var ftpReplyTimeout = 60 * time.Second

func certFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// ftpTLSConfig prüft das Zertifikat regulär. Schlägt das fehl (bei Shared
// Hosting häufig: Zertifikat lautet auf den Servernamen des Hosters), darf der
// Kunde den Fingerprint bestätigen – dann wird genau dieses Zertifikat gepinnt.
func ftpTLSConfig(host, pinned string, idErr **UnknownIdentityError) *tls.Config {
	cfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		ClientSessionCache: tls.NewLRUClientSessionCache(16), // Session-Reuse für Datenkanal
		InsecureSkipVerify: true,                             // Prüfung erfolgt unten selbst
	}
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return errors.New("kein Server-Zertifikat")
		}
		leaf := cs.PeerCertificates[0]
		fp := certFingerprint(leaf.Raw)
		if pinned != "" {
			if fp == pinned {
				return nil
			}
			*idErr = &UnknownIdentityError{Kind: "tls-certificate", Fingerprint: fp, Mismatch: true}
			return *idErr
		}
		inter := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			inter.AddCert(c)
		}
		if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter}); err == nil {
			return nil
		}
		*idErr = &UnknownIdentityError{Kind: "tls-certificate", Fingerprint: fp}
		return *idErr
	}
	return cfg
}

func ftpDial(ctx context.Context, t Target, cred Credentials) (*ftpClient, error) {
	c := &ftpClient{host: strings.Trim(t.Host, "[]"), dialer: guardedDialer(t.AllowPrivateNetworks)}
	var idErr *UnknownIdentityError
	if t.TLS != FTPPlain {
		c.tlsCfg = ftpTLSConfig(c.host, t.Fingerprint, &idErr)
	}
	addr := net.JoinHostPort(c.host, strconv.Itoa(t.Port))
	raw, err := c.dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c.ctrl = raw
	if t.TLS == FTPImplicitTLS {
		tc := tls.Client(raw, c.tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			raw.Close()
			if idErr != nil {
				return nil, idErr
			}
			return nil, err
		}
		c.ctrl = tc
	}
	if dl, ok := ctx.Deadline(); ok {
		c.deadline = dl
		_ = raw.SetDeadline(dl)
	}
	c.tp = textproto.NewConn(c.ctrl)
	if _, _, err := c.tp.ReadResponse(220); err != nil {
		c.close()
		return nil, fmt.Errorf("FTP-Begrüßung: %w", err)
	}
	if t.TLS == FTPExplicitTLS {
		if _, err := c.cmd(234, "AUTH TLS"); err != nil {
			c.close()
			return nil, fmt.Errorf("Server unterstützt kein FTPS (AUTH TLS): %w", err)
		}
		tc := tls.Client(raw, c.tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			raw.Close()
			if idErr != nil {
				return nil, idErr
			}
			return nil, err
		}
		c.ctrl = tc
		c.tp = textproto.NewConn(tc)
	}
	if c.tlsCfg != nil {
		if _, err := c.cmd(200, "PBSZ 0"); err != nil {
			c.close()
			return nil, err
		}
		if _, err := c.cmd(200, "PROT P"); err != nil {
			c.close()
			return nil, err
		}
	}
	code, msg, err := c.raw("USER %s", t.User)
	if err != nil {
		c.close()
		return nil, err
	}
	if code == 331 {
		code, msg, err = c.raw("PASS %s", cred.Password)
		if err != nil {
			c.close()
			return nil, err
		}
	}
	if code != 230 && code != 202 {
		c.close()
		return nil, fmt.Errorf("FTP-Login fehlgeschlagen: %d %s", code, msg)
	}
	if _, err := c.cmd(200, "TYPE I"); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *ftpClient) close() {
	if c.tp != nil {
		_, _, _ = c.raw("QUIT")
	}
	c.ctrl.Close()
}

// raw sendet einen Befehl und liefert Code + Text (ohne Erwartung).
func (c *ftpClient) raw(format string, args ...any) (int, string, error) {
	line := fmt.Sprintf(format, args...)
	if strings.ContainsAny(line, "\r\n") {
		return 0, "", errors.New("ungültige Zeichen im FTP-Befehl")
	}
	if c.broken != nil {
		return 0, "", c.broken
	}
	if err := c.tp.PrintfLine("%s", line); err != nil {
		c.broken = err
		return 0, "", err
	}
	code, msg, err := c.tp.ReadResponse(0)
	var pe *textproto.Error
	if errors.As(err, &pe) {
		return pe.Code, pe.Msg, nil
	}
	if err != nil {
		// Nach Timeout/Abbruch ist der Antwortstrom nicht mehr synchron:
		// weitere Befehle würden auf längst verlorene Antworten warten.
		c.broken = err
	}
	return code, msg, err
}

func (c *ftpClient) cmd(expect int, format string, args ...any) (string, error) {
	code, msg, err := c.raw(format, args...)
	return c.check(expect, code, msg, err)
}

// read liest eine (weitere) Antwort ohne neuen Befehl, z. B. 226 nach Transfer.
func (c *ftpClient) read(expect int) (string, error) {
	if c.broken != nil {
		return "", c.broken
	}
	code, msg, err := c.tp.ReadResponse(0)
	var pe *textproto.Error
	if errors.As(err, &pe) {
		code, msg, err = pe.Code, pe.Msg, nil
	} else if err != nil {
		c.broken = err
	}
	return c.check(expect, code, msg, err)
}

func (c *ftpClient) check(expect, code int, msg string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if code == expect || (expect == 200 && code/100 == 2) || (expect == 226 && code == 250) {
		return msg, nil
	}
	return msg, &textproto.Error{Code: code, Msg: msg}
}

var rePasv = regexp.MustCompile(`(\d+),(\d+),(\d+),(\d+),(\d+),(\d+)`)

// dataConn öffnet einen passiven Datenkanal (EPSV, sonst PASV).
// Die IP aus PASV wird ignoriert – immer die Adresse der Steuerverbindung
// (NAT-sicher und verhindert FTP-Bounce auf fremde Hosts).
func (c *ftpClient) dataConn(ctx context.Context) (net.Conn, error) {
	var port int
	if code, msg, err := c.raw("EPSV"); err == nil && code == 229 {
		if i, j := strings.Index(msg, "(|||"), strings.LastIndex(msg, "|)"); i >= 0 && j > i+4 {
			port, _ = strconv.Atoi(msg[i+4 : j])
		}
	}
	if port == 0 {
		msg, err := c.cmd(227, "PASV")
		if err != nil {
			return nil, err
		}
		m := rePasv.FindStringSubmatch(msg)
		if m == nil {
			return nil, fmt.Errorf("PASV-Antwort unverständlich: %s", msg)
		}
		p1, _ := strconv.Atoi(m[5])
		p2, _ := strconv.Atoi(m[6])
		port = p1*256 + p2
	}
	host, _, _ := net.SplitHostPort(c.ctrl.RemoteAddr().String())
	conn, err := c.dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	if c.tlsCfg == nil {
		return conn, nil
	}
	return tls.Client(conn, c.tlsCfg), nil
}

func (c *ftpClient) transfer(ctx context.Context, cmd string, fn func(net.Conn) (int64, error)) error {
	dc, err := c.dataConn(ctx)
	if err != nil {
		return err
	}
	// TLS-Handshake auf dem Datenkanal sofort starten (wie curl), nicht erst
	// nach 150: Manche Server (z. B. easyname) senden 150 erst, wenn der
	// Handshake steht – sonst warten Client und Server ewig aufeinander.
	hs := make(chan error, 1)
	if tc, ok := dc.(*tls.Conn); ok {
		go func() { hs <- tc.HandshakeContext(ctx) }()
	} else {
		hs <- nil
	}
	code, msg, err := c.rawWithin("%s", cmd)
	if err == nil && code != 150 && code != 125 {
		err = &textproto.Error{Code: code, Msg: msg}
	}
	if err != nil {
		dc.Close()
		<-hs
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return fmt.Errorf("Server antwortet nicht auf %s (Datenkanal/Firewall?): %w", strings.Fields(cmd)[0], err)
		}
		return err
	}
	if err := <-hs; err != nil {
		dc.Close()
		return fmt.Errorf("TLS-Datenkanal: %w", err)
	}
	n, ferr := fn(dc)
	cerr := closeData(dc, ferr == nil)
	if ferr != nil {
		ferr = fmt.Errorf("Datenübertragung nach %s abgebrochen: %w", humanBytes(n), ferr)
	}
	// Auch auf den Abschluss (226) nicht länger als ftpReplyTimeout warten
	_ = c.ctrl.SetReadDeadline(c.replyDeadline())
	defer c.ctrl.SetReadDeadline(c.deadline)
	if _, err := c.read(226); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			err = fmt.Errorf("Server bestätigt die Übertragung nicht (226 fehlt): %w", err)
		}
		return errors.Join(ferr, err)
	}
	return errors.Join(ferr, cerr)
}

// closeData beendet den Datenkanal sauber: erst die Schreibseite schließen
// (bei TLS close_notify), dann bis EOF mitlesen, dann schließen. Hart
// geschlossen schickt das Betriebssystem bei ungelesenen Daten – z. B.
// TLS-1.3-Session-Tickets des Servers – einen TCP-Reset, und der Server
// verwirft den Upload („450 Transfer aborted“, easyname 2026-11-26).
func closeData(dc net.Conn, graceful bool) error {
	if graceful {
		var werr error
		switch c := dc.(type) {
		case *tls.Conn:
			werr = c.CloseWrite()
		case interface{ CloseWrite() error }:
			werr = c.CloseWrite()
		}
		if werr == nil {
			_ = dc.SetReadDeadline(time.Now().Add(ftpDrainTimeout))
			_, _ = io.Copy(io.Discard, dc)
		}
	}
	return dc.Close()
}

// ftpDrainTimeout: so lange wartet closeData höchstens auf das Ende des Datenkanals.
var ftpDrainTimeout = 30 * time.Second

func (c *ftpClient) replyDeadline() time.Time {
	dl := time.Now().Add(ftpReplyTimeout)
	if !c.deadline.IsZero() && c.deadline.Before(dl) {
		dl = c.deadline
	}
	return dl
}

// rawWithin wie raw, aber mit eigener Lesefrist für die Antwort.
func (c *ftpClient) rawWithin(format string, args ...any) (int, string, error) {
	_ = c.ctrl.SetReadDeadline(c.replyDeadline())
	defer c.ctrl.SetReadDeadline(c.deadline)
	return c.raw(format, args...)
}

func (c *ftpClient) stor(ctx context.Context, path string, r io.Reader) error {
	return c.transfer(ctx, "STOR "+path, func(conn net.Conn) (int64, error) { return io.Copy(conn, r) })
}

func (c *ftpClient) retr(ctx context.Context, path string) ([]byte, error) {
	var buf bytes.Buffer
	err := c.transfer(ctx, "RETR "+path, func(conn net.Conn) (int64, error) {
		return io.Copy(&buf, io.LimitReader(conn, 1<<20))
	})
	return buf.Bytes(), err
}

func (c *ftpClient) exists(path string) bool {
	code, _, err := c.raw("SIZE %s", path)
	return err == nil && code == 213
}

func (c *ftpClient) mkdirAll(path string) {
	cur := ""
	if strings.HasPrefix(path, "/") {
		cur = "/"
	}
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg == "" {
			continue
		}
		if cur != "" && cur != "/" {
			cur += "/"
		}
		cur += seg
		_, _, _ = c.raw("MKD %s", cur) // 550 = existiert bereits
	}
}

func (c *ftpClient) rename(from, to string) error {
	if _, err := c.cmd(350, "RNFR %s", from); err != nil {
		return err
	}
	_, err := c.cmd(250, "RNTO %s", to)
	return err
}

func (c *ftpClient) dele(path string) { _, _, _ = c.raw("DELE %s", path) }
func (c *ftpClient) rmd(path string)  { _, _, _ = c.raw("RMD %s", path) }

// checkDocroot prüft, ob der Document-Root existiert, ohne ihn anzulegen.
// Ein falscher Pfad (z. B. /factory-test statt /html/factory-test, weil der
// Login in /html startet) würde sonst Ordner außerhalb der Website erzeugen.
func (c *ftpClient) checkDocroot(root string) error {
	if root == "" {
		return nil
	}
	start := "/"
	if _, msg, err := c.raw("PWD"); err == nil {
		if i, j := strings.Index(msg, `"`), strings.LastIndex(msg, `"`); i >= 0 && j > i {
			start = msg[i+1 : j]
		}
	}
	code, _, err := c.raw("CWD %s", root)
	if err != nil {
		return err
	}
	if code/100 != 2 {
		hint := ""
		if start != "/" && !strings.HasPrefix(root, start+"/") {
			hint = fmt.Sprintf(" Nach dem Login steht man in %s – gemeint ist vermutlich %s/%s.", start, strings.TrimRight(start, "/"), strings.TrimLeft(root, "/"))
		}
		return fmt.Errorf("Document-Root %s existiert nicht.%s Ordner der Subdomain im Hoster-Panel prüfen", root, hint)
	}
	_, _, _ = c.raw("CWD %s", start) // zurück, relative Pfade bleiben gültig
	return nil
}

func (c *ftpClient) chmod(mode, path string) error {
	_, err := c.cmd(200, "SITE CHMOD %s %s", mode, path)
	return err
}

// ---------------------------------------------------------------------------
// Deployment auf Shared-Webspace (CGI-Modus)
// ---------------------------------------------------------------------------

// DeployFTP lädt das Binary als styrixa.cgi in den Document-Root, legt den
// geschützten Datenordner an und schreibt die .htaccess. Eine vorhandene
// .htaccess wird vorher gesichert; eine vorhandene styrixa.db nie angefasst.
func DeployFTP(ctx context.Context, art Artifact, t Target, cred Credentials, log Logger, httpClient *http.Client) (*Result, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if art.GOOS != "linux" {
		return nil, fmt.Errorf("Shared-Webspace läuft praktisch immer auf Linux – bitte linux/amd64 bauen (aktuell %s/%s)", art.GOOS, art.GOARCH)
	}
	if cred.Password == "" {
		return nil, errors.New("FTP-Passwort erforderlich")
	}
	if t.TLS == FTPPlain {
		log.emit("connect", "warn", "Unverschlüsseltes FTP – Passwort und Dateien gehen im Klartext über das Netz")
	}
	log.emit("connect", "info", "Verbinde mit %s:%d (%s) …", t.Host, t.Port, t.TLS)
	c, err := ftpDial(ctx, t, cred)
	if err != nil {
		return nil, err
	}
	defer c.close()
	log.emit("connect", "ok", "Angemeldet als %s", t.User)

	root := strings.TrimRight(t.RemoteDir, "/")
	if err := c.checkDocroot(root); err != nil {
		log.emit("connect", "error", "%v", err)
		return nil, err
	}
	p := func(name string) string {
		if root == "" {
			return name
		}
		return root + "/" + name
	}

	if httpClient == nil {
		httpClient = guardedHTTPClient(t.AllowPrivateNetworks)
	}
	rel := releaseID(time.Now())

	// 1. Hoster-Check in einem Prüfordner – die bestehende Website bleibt unberührt,
	//    bis feststeht, dass CGI und mod_rewrite funktionieren.
	f, err := os.Open(art.BinaryPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, _ := f.Stat()
	probeName := "_styrixa-check-" + strings.ToLower(rel)
	probe := p(probeName)
	c.mkdirAll(probe)
	cleanupProbe := func() {
		for _, n := range []string{"styrixa.cgi", ".htaccess", probeFile} {
			c.dele(probe + "/" + n)
		}
		c.rmd(probe)
		if c.broken != nil {
			log.emit("check", "warn", "Prüfordner %s konnte nicht entfernt werden (Verbindung abgebrochen) – bitte per FTP löschen", probe)
		}
	}
	base := strings.TrimRight(t.PublicURL, "/") + "/" + probeName

	// 1a. Zeigt die Website-URL überhaupt auf diesen Ordner? Erst eine kleine
	//     Textdatei – sonst würde ein falsch zugeordneter Ordner als
	//     „kein CGI“ gemeldet (easyname, 2026-11-26).
	if err := c.stor(ctx, probe+"/"+probeFile, strings.NewReader(probeToken(probeName))); err != nil {
		cleanupProbe()
		return nil, fmt.Errorf("Upload: %w", err)
	}
	if err := checkServesDir(ctx, httpClient, base, probeName); err != nil {
		cleanupProbe()
		log.emit("check", "error", "%v", err)
		return nil, fmt.Errorf("Hoster-Check fehlgeschlagen – an der Website wurde nichts verändert: %w", err)
	}
	log.emit("check", "ok", "Website-URL zeigt auf den Document-Root")

	log.emit("check", "info", "Hoster-Check: lade Programm in Prüfordner (%s) …", humanBytes(st.Size()))
	if err := c.stor(ctx, probe+"/styrixa.cgi", f); err != nil {
		cleanupProbe()
		return nil, fmt.Errorf("Upload: %w", err)
	}
	if err := c.chmod("755", probe+"/styrixa.cgi"); err != nil {
		log.emit("check", "warn", "SITE CHMOD nicht unterstützt (%v) – Rechte 755 ggf. im Hoster-Panel setzen", err)
	}
	if err := c.stor(ctx, probe+"/.htaccess", strings.NewReader(ProbeHtaccess)); err != nil {
		cleanupProbe()
		return nil, err
	}
	if err := probeHoster(ctx, httpClient, base); err != nil {
		cleanupProbe()
		log.emit("check", "error", "%v", err)
		return nil, fmt.Errorf("Hoster-Check fehlgeschlagen – an der Website wurde nichts verändert: %w", err)
	}
	log.emit("check", "ok", "Hoster unterstützt CGI und mod_rewrite")

	// 2. Datenordner + Schutz
	c.mkdirAll(p(t.DataDir))
	c.mkdirAll(p("themes"))
	if err := c.stor(ctx, p(t.DataDir+"/.htaccess"), strings.NewReader(DenyAllHtaccess)); err != nil {
		cleanupProbe()
		return nil, fmt.Errorf("Datenordner schützen: %w", err)
	}
	dbExists := c.exists(p(t.DataDir + "/styrixa.db"))
	if dbExists {
		log.emit("data", "ok", "Bestehende styrixa.db gefunden – bleibt unverändert")
	}
	// Einmal-Code für /install: vorhandenen übernehmen (Deploy vor abgeschlossener
	// Installation wiederholt), sonst bei Erstinstallation neu anlegen.
	codePath := p(t.DataDir + "/" + installCodeFile)
	var installCode string
	if c.exists(codePath) {
		if b, err := c.retr(ctx, codePath); err == nil {
			installCode = parseInstallCode(b)
		}
	} else if !dbExists {
		code, err := newInstallCode()
		if err != nil {
			cleanupProbe()
			return nil, err
		}
		if err := c.stor(ctx, codePath, strings.NewReader(code+"\n")); err != nil {
			cleanupProbe()
			return nil, fmt.Errorf("Installations-Code: %w", err)
		}
		_ = c.chmod("600", codePath) // best effort; der Datenordner ist zusätzlich per .htaccess gesperrt
		installCode = code
	}

	// 3. Geprüftes Programm übernehmen (kein zweiter Upload)
	if err := c.rename(probe+"/styrixa.cgi", p("styrixa.cgi.part")); err != nil {
		cleanupProbe()
		return nil, fmt.Errorf("Übernehmen aus Prüfordner: %w", err)
	}
	cleanupProbe()
	_ = c.chmod("755", p("styrixa.cgi.part")) // Rechte bleiben beim Umbenennen meist erhalten; sicherheitshalber erneut
	if c.exists(p("styrixa.cgi")) {
		_, _, _ = c.raw("DELE %s", p("styrixa.cgi.prev"))
		_ = c.rename(p("styrixa.cgi"), p("styrixa.cgi.prev"))
	}
	if err := c.rename(p("styrixa.cgi.part"), p("styrixa.cgi")); err != nil {
		return nil, fmt.Errorf("Umbenennen: %w", err)
	}
	log.emit("upload", "ok", "styrixa.cgi aktiv")

	// 4. .htaccess sichern + schreiben
	if c.exists(p(".htaccess")) {
		old, err := c.retr(ctx, p(".htaccess"))
		if err == nil && !bytes.Contains(old, []byte("# BEGIN STYRIXA")) {
			backup := p(".htaccess.styrixa-backup-" + rel)
			if err := c.stor(ctx, backup, bytes.NewReader(old)); err != nil {
				return nil, fmt.Errorf(".htaccess sichern: %w", err)
			}
			log.emit("htaccess", "warn", "Bestehende .htaccess gesichert als %s", backup)
		}
	}
	if err := c.stor(ctx, p(".htaccess"), strings.NewReader(RootHtaccess(t.DataDir))); err != nil {
		return nil, fmt.Errorf(".htaccess schreiben: %w", err)
	}
	log.emit("htaccess", "ok", "CGI-Routing eingerichtet")

	// 5. Health-Check über die öffentliche URL
	url := strings.TrimRight(t.PublicURL, "/") + "/healthz"
	log.emit("health", "info", "Health-Check %s …", url)
	if err := waitHealthy(ctx, httpClient, url, 6, 2*time.Second); err != nil {
		log.emit("health", "error", "%v", err)
		if c.exists(p("styrixa.cgi.prev")) {
			_, _, _ = c.raw("DELE %s", p("styrixa.cgi"))
			if c.rename(p("styrixa.cgi.prev"), p("styrixa.cgi")) == nil {
				log.emit("rollback", "warn", "Vorherige styrixa.cgi wiederhergestellt")
			}
		}
		return nil, fmt.Errorf("%w – prüfen: Apache mit mod_cgi/mod_rewrite? .htaccess erlaubt (AllowOverride)? Bei nginx-Hosting ist der CGI-Modus nicht möglich", err)
	}
	log.emit("health", "ok", "Website antwortet")

	return &Result{
		Method:  MethodFTP,
		Release: rel,
		URL:     t.PublicURL,
		Hints: []string{
			"CGI-Modus: Das CMS startet pro Anfrage – ideal für kleine bis mittlere Websites. Für viel Traffic SSH/VPS nutzen.",
			installHint(installCode, strings.TrimRight(t.PublicURL, "/")+"/install"),
		},
		InstallCode: installCode,
	}, nil
}

// ProbeHtaccess: minimale Konfiguration für den Hoster-Check.
const ProbeHtaccess = `Options +ExecCGI
AddHandler cgi-script .cgi
<IfModule mod_rewrite.c>
RewriteEngine On
RewriteRule ^ping$ styrixa.cgi/healthz [L]
</IfModule>
`

// probeFile liegt während des Hoster-Checks im Prüfordner.
const probeFile = "styrixa-probe.txt"

func probeToken(probeName string) string { return "styrixa-probe " + probeName }

// checkServesDir ruft die Textdatei über die Website-URL ab.
func checkServesDir(ctx context.Context, client *http.Client, base, probeName string) error {
	url := base + "/" + probeFile
	var last string
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("User-Agent", "STYRIXA-Factory-Deploy/1")
		resp, err := client.Do(req)
		if err != nil {
			last = err.Error()
		} else {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && strings.TrimSpace(string(b)) == probeToken(probeName) {
				return nil
			}
			last = fmt.Sprintf("HTTP %d%s", resp.StatusCode, htmlTitle(b))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("die Website-URL zeigt nicht auf den Document-Root – eine dort abgelegte Testdatei ist unter %s nicht abrufbar (%s). "+
		"Im Hoster-Panel prüfen, welchem Ordner die Domain zugeordnet ist (Änderungen brauchen ggf. einige Minuten)", url, last)
}

// cgiDiagnosis unterscheidet die typischen Ursachen, wenn das Programm nicht
// als CGI läuft (gesehen bei easyname, 2026-11-26):
//   - Testdatei plötzlich HTTP 500 → .htaccess-Zeile „Options +ExecCGI“ verboten
//   - styrixa.cgi kommt als Datei zurück → kein CGI-Handler (mod_cgi fehlt)
func cgiDiagnosis(ctx context.Context, client *http.Client, base string) string {
	get := func(url string) (int, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req.Header.Set("User-Agent", "STYRIXA-Factory-Deploy/1")
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
		return resp.StatusCode, b
	}
	if code, _ := get(base + "/" + probeFile); code == http.StatusInternalServerError {
		return "der Hoster erlaubt die nötige .htaccess-Einstellung „Options +ExecCGI“ nicht (sogar eine Textdatei liefert dann HTTP 500) – " +
			"dieser Tarif unterstützt kein CGI, bitte SSH-Deployment oder anderen Tarif nutzen"
	}
	if code, b := get(base + "/styrixa.cgi"); code == http.StatusOK && bytes.HasPrefix(b, []byte("\x7fELF")) {
		return "der Hoster führt .cgi-Dateien nicht aus, sondern liefert sie als Download aus (kein mod_cgi) – " +
			"dieser Tarif unterstützt kein CGI, bitte SSH-Deployment oder anderen Tarif nutzen"
	}
	return ""
}

// probeHoster prüft erst CGI direkt, dann über mod_rewrite – mit klarer Diagnose.
func probeHoster(ctx context.Context, client *http.Client, base string) error {
	direct := waitHealthy(ctx, client, base+"/styrixa.cgi/healthz", 3, time.Second)
	if direct != nil {
		if why := cgiDiagnosis(ctx, client, base); why != "" {
			return fmt.Errorf("%s: %w", why, direct)
		}
		return fmt.Errorf("das Programm wird nicht als CGI ausgeführt (Apache mit mod_cgi und .htaccess nötig; "+
			"bei reinem nginx-Hosting bitte SSH-Deployment nutzen): %w", direct)
	}
	if err := waitHealthy(ctx, client, base+"/ping", 2, time.Second); err != nil {
		return fmt.Errorf("CGI funktioniert, aber mod_rewrite fehlt oder ist in .htaccess nicht erlaubt (AllowOverride): %w", err)
	}
	return nil
}
