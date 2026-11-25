package core

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
	"math/big"
	"mime/quotedprintable"
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// smtpServer ist ein minimaler SMTP-Server für Tests: EHLO, STARTTLS, AUTH
// PLAIN, MAIL, RCPT, DATA, QUIT. Keine echte Mail verlässt den Test.
type smtpServer struct {
	ln       net.Listener
	tls      *tls.Config
	noTLS    bool // STARTTLS nicht anbieten
	mu       sync.Mutex
	auth     []string // dekodierte AUTH-PLAIN-Daten
	authNoTL bool     // AUTH kam ohne TLS
	rcpt     []string
	data     []string
}

func newTestCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func startSMTP(t *testing.T, noTLS bool) (*smtpServer, *x509.CertPool) {
	t.Helper()
	cert, pool := newTestCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpServer{ln: ln, tls: &tls.Config{Certificates: []tls.Certificate{cert}}, noTLS: noTLS}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s, pool
}

func (s *smtpServer) port() string { return strconv.Itoa(s.ln.Addr().(*net.TCPAddr).Port) }

func (s *smtpServer) serve(c net.Conn) {
	defer func() { c.Close() }()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(c)
	reply := func(l string) { io.WriteString(c, l+"\r\n") }
	secure := false
	reply("220 test ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		switch cmd {
		case "EHLO", "HELO":
			if !secure && !s.noTLS {
				reply("250-test")
				reply("250 STARTTLS")
			} else {
				reply("250-test")
				reply("250 AUTH PLAIN")
			}
		case "STARTTLS":
			reply("220 los")
			tc := tls.Server(c, s.tls)
			if err := tc.Handshake(); err != nil {
				return
			}
			c, r, secure = tc, bufio.NewReader(tc), true
		case "AUTH":
			parts := strings.Fields(line)
			b, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			s.mu.Lock()
			s.auth = append(s.auth, string(b))
			if !secure {
				s.authNoTL = true
			}
			s.mu.Unlock()
			reply("235 ok")
		case "MAIL":
			reply("250 ok")
		case "RCPT":
			s.mu.Lock()
			s.rcpt = append(s.rcpt, line)
			s.mu.Unlock()
			reply("250 ok")
		case "DATA":
			reply("354 weiter")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = append(s.data, b.String())
			s.mu.Unlock()
			reply("250 angenommen")
		case "QUIT":
			reply("221 tschüss")
			return
		default:
			reply("502 unbekannt")
		}
	}
}

func smtpSettings(t *testing.T, port string) Settings {
	t.Helper()
	db := openTestDB(t, t.TempDir())
	if err := Migrate(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	st := SettingsStore{DB: db}
	for k, v := range map[string]string{
		KeySMTPHost: "127.0.0.1", KeySMTPPort: port, KeySMTPUser: "web@example.at",
		KeySMTPPassword: "smtp-geheim", KeyMailFrom: "STYRIXA <web@example.at>",
		KeySiteURL: "https://example.at", KeySiteTitle: "Tischlerei Müller",
	} {
		if err := st.Set(context.Background(), k, v); err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestSMTPMailerSTARTTLS(t *testing.T) {
	srv, pool := startSMTP(t, false)
	m := SMTPMailer{Settings: smtpSettings(t, srv.port()), TLSConfig: &tls.Config{RootCAs: pool}}
	if err := m.Send(context.Background(), "admin@example.at", "Passwort zurücksetzen", "Grüße – äöü ß €\nZeile 2\n"); err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.auth) != 1 || srv.auth[0] != "\x00web@example.at\x00smtp-geheim" || srv.authNoTL {
		t.Fatalf("AUTH: %q (ohne TLS: %v)", srv.auth, srv.authNoTL)
	}
	if len(srv.rcpt) != 1 || !strings.Contains(srv.rcpt[0], "<admin@example.at>") {
		t.Fatalf("RCPT: %q", srv.rcpt)
	}
	msg := srv.data[0]
	hdr, body, _ := strings.Cut(msg, "\r\n\r\n")
	for _, want := range []string{"From: \"STYRIXA\" <web@example.at>", "To: <admin@example.at>", "Subject: =?utf-8?q?Passwort_zur=C3=BCcksetzen?=", "Content-Type: text/plain; charset=utf-8"} {
		if !strings.Contains(hdr, want) {
			t.Errorf("Header fehlt %q in:\n%s", want, hdr)
		}
	}
	dec, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if !strings.Contains(string(dec), "Grüße – äöü ß €") {
		t.Errorf("Text: %q", dec)
	}
}

func TestSMTPMailerRefusesWithoutSTARTTLS(t *testing.T) {
	srv, _ := startSMTP(t, true)
	m := SMTPMailer{Settings: smtpSettings(t, srv.port())}
	err := m.Send(context.Background(), "admin@example.at", "x", "x")
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("erwartet STARTTLS-Fehler, bekam %v", err)
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.auth) != 0 || len(srv.data) != 0 {
		t.Fatal("ohne TLS wurden Zugangsdaten oder Daten gesendet")
	}
}

func TestSMTPMailerUntrustedCert(t *testing.T) {
	srv, _ := startSMTP(t, false)
	m := SMTPMailer{Settings: smtpSettings(t, srv.port())} // Systemzertifikate kennen das Testzertifikat nicht
	if err := m.Send(context.Background(), "admin@example.at", "x", "x"); err == nil {
		t.Fatal("unbekanntes Zertifikat akzeptiert")
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.auth) != 0 {
		t.Fatal("Zugangsdaten trotz ungültigem Zertifikat gesendet")
	}
}

func TestSMTPMailerNotConfigured(t *testing.T) {
	st := smtpSettings(t, "25")
	_ = st.Set(context.Background(), KeySMTPHost, "")
	if err := (SMTPMailer{Settings: st}).Send(context.Background(), "a@example.at", "x", "x"); err != ErrMailNotConfigured {
		t.Fatalf("%v", err)
	}
}

// Ende-zu-Ende: Reset-Anfrage → echte SMTP-Sitzung → Link einlösen.
func TestPasswordResetViaSMTP(t *testing.T) {
	srv, pool := startSMTP(t, false)
	e := newAuthEnv(t)
	st := smtpSettings(t, srv.port())
	all, _ := st.GetAll(context.Background())
	for k, v := range all {
		_ = e.a.Settings.Set(context.Background(), k, v)
	}
	e.a.Mail = SMTPMailer{Settings: e.a.Settings, TLSConfig: &tls.Config{RootCAs: pool}}
	if w := e.req("POST", PathForgot, url.Values{"email": {testEmail}}, reqOpt{}); w.Code != 200 {
		t.Fatalf("%d", w.Code)
	}
	srv.mu.Lock()
	if len(srv.data) != 1 {
		srv.mu.Unlock()
		t.Fatalf("%d Mails", len(srv.data))
	}
	_, body, _ := strings.Cut(srv.data[0], "\r\n\r\n")
	srv.mu.Unlock()
	dec, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	tok := resetToken(t, string(dec))
	pw := "ganz-neues-passwort"
	if w := e.req("POST", PathReset, url.Values{"token": {tok}, "password": {pw}, "password2": {pw}}, reqOpt{}); w.Code != 200 {
		t.Fatalf("Reset: %d", w.Code)
	}
}
