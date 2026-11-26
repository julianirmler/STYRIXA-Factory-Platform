package deploy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// strictFTPS imitiert einen FTPS-Server, der den TLS-Handshake auf dem
// Datenkanal verlangt, bevor er 150 sendet (gesehen bei easyname).
// mode: "" normal, "silent" = keine Antwort auf STOR, "no226" = Daten
// annehmen, aber den Abschluss nie bestätigen.
type strictFTPS struct {
	port int
	fp   string
	mode string

	mu       sync.Mutex
	received map[string][]byte
}

func startStrictFTPS(t *testing.T, mode string) *strictFTPS {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ftp.hoster.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	cfg := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	s := &strictFTPS{port: ln.Addr().(*net.TCPAddr).Port, fp: certFingerprint(der), mode: mode, received: map[string][]byte{}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c, cfg)
		}
	}()
	return s
}

func (s *strictFTPS) serve(conn net.Conn, cfg *tls.Config) {
	defer conn.Close()
	var c net.Conn = conn
	r := bufio.NewReader(c)
	say := func(f string, a ...any) { fmt.Fprintf(c, f+"\r\n", a...) }
	say("220 hoster ftp")
	var dataLn net.Listener
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
		switch strings.ToUpper(cmd) {
		case "AUTH":
			say("234 AUTH TLS ok")
			tc := tls.Server(conn, cfg)
			if tc.Handshake() != nil {
				return
			}
			c, r = tc, bufio.NewReader(tc)
		case "USER":
			say("331 Passwort bitte")
		case "PASS":
			say("230 ok")
		case "EPSV":
			dataLn, _ = net.Listen("tcp", "127.0.0.1:0")
			say("229 Entering Extended Passive Mode (|||%d|)", dataLn.Addr().(*net.TCPAddr).Port)
		case "PASV":
			say("502 nein")
		case "STOR":
			if s.mode == "silent" {
				continue // nie antworten
			}
			dc, err := dataLn.Accept()
			dataLn.Close()
			if err != nil {
				return
			}
			td := tls.Server(dc, cfg)
			if err := td.Handshake(); err != nil { // erst Handshake …
				say("425 TLS-Handshake fehlgeschlagen")
				dc.Close()
				continue
			}
			say("150 Opening data connection") // … dann 150
			// Wie TLS-1.3-Session-Tickets: Daten vom Server, die der Client
			// beim Upload nie liest. Schließt er hart, folgt ein TCP-Reset.
			td.Write(bytes.Repeat([]byte{0}, 64<<10))
			b, err := slowReadAll(td) // wie ein entfernter Server: Puffer füllt sich
			td.Close()
			if err != nil { // wie easyname: Abbruch statt 226
				say("450 Transfer aborted. Link to file server lost (%v)", err)
				continue
			}
			s.mu.Lock()
			s.received[arg] = b
			s.mu.Unlock()
			if s.mode == "no226" {
				continue
			}
			say("226 Transfer complete")
		case "PWD":
			say(`257 "/html" is the current directory`)
		case "CWD":
			if arg == "/html" || arg == "/html/factory-test" || arg == "factory-test" {
				say("250 ok")
			} else {
				say("550 %s: No such file or directory", arg)
			}
		case "QUIT":
			say("221 Tschüss")
			return
		default:
			say("200 ok")
		}
	}
}

func dialStrict(t *testing.T, s *strictFTPS, ctx context.Context) *ftpClient {
	t.Helper()
	c, err := ftpDial(ctx, Target{AllowPrivateNetworks: true, Method: MethodFTP, Host: "127.0.0.1", Port: s.port,
		User: "kunde", TLS: FTPExplicitTLS, Fingerprint: s.fp}, Credentials{Password: "geheim"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.close)
	return c
}

// Regression: Upload hing 10 min, weil der Client erst nach 150 den
// TLS-Handshake des Datenkanals begann (easyname, 2026-11-26).
func TestFTPSDataHandshakeBefore150(t *testing.T) {
	s := startStrictFTPS(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := dialStrict(t, s, ctx)
	payload := bytes.Repeat([]byte("styrixa"), 200_000) // ~1,4 MB
	start := time.Now()
	if err := c.stor(ctx, "/factory-test/styrixa.cgi", bytes.NewReader(payload)); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Upload dauerte %v", d)
	}
	s.mu.Lock()
	got := s.received["/factory-test/styrixa.cgi"]
	s.mu.Unlock()
	if !bytes.Equal(got, payload) {
		t.Fatalf("Server erhielt %d Bytes, erwartet %d", len(got), len(payload))
	}
}

// Antwortet der Server auf STOR nicht, gibt es nach ftpReplyTimeout eine
// verständliche Meldung statt erst nach der Gesamtfrist.
func TestFTPReplyTimeout(t *testing.T) {
	old := ftpReplyTimeout
	ftpReplyTimeout = 500 * time.Millisecond
	t.Cleanup(func() { ftpReplyTimeout = old })

	for mode, want := range map[string]string{"silent": "antwortet nicht auf STOR", "no226": "226 fehlt"} {
		s := startStrictFTPS(t, mode)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		c := dialStrict(t, s, ctx)
		start := time.Now()
		err := c.stor(ctx, "/x/styrixa.cgi", strings.NewReader("x"))
		cancel()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: err = %v", mode, err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Errorf("%s: Fehler erst nach %v", mode, d)
		}
		// Aufräumen über die tote Verbindung darf nicht hängen
		start = time.Now()
		c.dele("/x/styrixa.cgi")
		c.rmd("/x")
		if d := time.Since(start); d > 100*time.Millisecond {
			t.Errorf("%s: Aufräumen hing %v", mode, d)
		}
		if c.broken == nil {
			t.Errorf("%s: Verbindung nicht als unbrauchbar markiert", mode)
		}
	}
}

// Gesehen bei easyname: Login startet in /html. "/factory-test" existiert
// nicht und darf nicht stillschweigend angelegt werden.
func TestFTPCheckDocroot(t *testing.T) {
	s := startStrictFTPS(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := dialStrict(t, s, ctx)
	for _, ok := range []string{"", "/html/factory-test", "factory-test"} {
		if err := c.checkDocroot(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	err := c.checkDocroot("/factory-test")
	if err == nil || !strings.Contains(err.Error(), "existiert nicht") || !strings.Contains(err.Error(), "/html/factory-test") {
		t.Fatalf("err = %v", err)
	}
}

// slowReadAll liest in kleinen Stücken mit Pausen, damit beim Schließen
// durch den Client noch Daten im Empfangspuffer des Servers liegen.
func slowReadAll(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		time.Sleep(2 * time.Millisecond)
	}
}
