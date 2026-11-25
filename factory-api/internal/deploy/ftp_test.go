package deploy

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startPyFTPD startet pyftpdlib (optional mit TLS) in dir. Test wird
// übersprungen, wenn pyftpdlib nicht installiert ist.
func startPyFTPD(t *testing.T, dir string, withTLS bool) (port int, certPath string) {
	t.Helper()
	if err := exec.Command("python3", "-c", "import pyftpdlib").Run(); err != nil {
		t.Skip("pyftpdlib nicht installiert (pip install pyftpdlib pyopenssl)")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port = ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if withTLS {
		certPath = filepath.Join(t.TempDir(), "cert.pem")
		writeSelfSigned(t, certPath)
	}
	script := fmt.Sprintf(`
import sys
from pyftpdlib.authorizers import DummyAuthorizer
from pyftpdlib.servers import FTPServer
from pyftpdlib.handlers import FTPHandler, TLS_FTPHandler
a = DummyAuthorizer(); a.add_user("kunde", "geheim", %q, perm="elradfmwMT")
tls = %s
h = TLS_FTPHandler if tls else FTPHandler
if tls:
    h.certfile = %q; h.tls_control_required = True; h.tls_data_required = True
h.authorizer = a; h.passive_ports = range(%d, %d)
s = FTPServer(("127.0.0.1", %d), h); print("READY", flush=True); s.serve_forever()
`, dir, map[bool]string{true: "True", false: "False"}[withTLS], certPath, port+1, port+50, port)
	cmd := exec.Command("python3", "-c", script)
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	sc := bufio.NewScanner(out)
	if !sc.Scan() || sc.Text() != "READY" {
		t.Fatal("FTP-Server startet nicht")
	}
	return port, certPath
}

func writeSelfSigned(t *testing.T, path string) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "hoster-server-17.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"hoster-server-17.example"}}
	der, _ := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	kb, _ := x509.MarshalECPrivateKey(k)
	f, _ := os.Create(path)
	pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	f.Close()
}

func fakeBinary(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "styrixa")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// healthServer simuliert einen Apache-Webspace. rewrite=false: mod_rewrite fehlt.
func healthServer(t *testing.T) *httptest.Server { return fakeApache(t, true) }

func fakeApache(t *testing.T, rewrite bool) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/healthz", strings.HasSuffix(p, "/styrixa.cgi/healthz"):
			w.Write([]byte("ok"))
		case rewrite && strings.HasSuffix(p, "/ping"):
			w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestDeployFTPPlain(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, ".htaccess"), []byte("# WordPress\nRewriteEngine On\n"), 0o644)
	os.MkdirAll(filepath.Join(root, "styrixa-data"), 0o755)
	os.WriteFile(filepath.Join(root, "styrixa-data", "styrixa.db"), []byte("bestand"), 0o644)
	port, _ := startPyFTPD(t, root, false)
	hs := healthServer(t)

	var log []string
	res, err := DeployFTP(context.Background(),
		Artifact{Name: "kunde", BinaryPath: fakeBinary(t), GOOS: "linux", GOARCH: "amd64"},
		Target{AllowPrivateNetworks: true, Method: MethodFTP, Host: "127.0.0.1", Port: port, User: "kunde", TLS: FTPPlain, AllowPlaintext: true, PublicURL: hs.URL},
		Credentials{Password: "geheim"},
		func(e Event) { log = append(log, e.Level+": "+e.Msg) }, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(log, "\n"))
	}
	st, err := os.Stat(filepath.Join(root, "styrixa.cgi"))
	if err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("styrixa.cgi fehlt oder falsche Rechte: %v %v", st, err)
	}
	ht, _ := os.ReadFile(filepath.Join(root, ".htaccess"))
	if !strings.Contains(string(ht), "# BEGIN STYRIXA") {
		t.Error(".htaccess nicht geschrieben")
	}
	backups, _ := filepath.Glob(filepath.Join(root, ".htaccess.styrixa-backup-*"))
	if len(backups) != 1 {
		t.Error("WordPress-.htaccess nicht gesichert")
	}
	if db, _ := os.ReadFile(filepath.Join(root, "styrixa-data", "styrixa.db")); string(db) != "bestand" {
		t.Error("bestehende Datenbank verändert!")
	}
	if _, err := os.Stat(filepath.Join(root, "styrixa-data", ".htaccess")); err != nil {
		t.Error("Datenordner nicht geschützt")
	}
	if res.Method != MethodFTP {
		t.Error(res)
	}
	if res.InstallCode != "" {
		t.Error("bestehendes CMS bekommt keinen Installations-Code")
	}
	if _, err := os.Stat(filepath.Join(root, "styrixa-data", installCodeFile)); !os.IsNotExist(err) {
		t.Error("Code-Datei für bestehendes CMS angelegt")
	}
	for _, l := range log {
		if strings.Contains(l, "install-code") {
			t.Errorf("Code-Datei im Log erwähnt: %s", l)
		}
	}
}

func TestDeployFTPSCertificatePinning(t *testing.T) {
	root := t.TempDir()
	port, _ := startPyFTPD(t, root, true)
	hs := healthServer(t)
	art := Artifact{Name: "kunde", BinaryPath: fakeBinary(t), GOOS: "linux", GOARCH: "amd64"}
	tgt := Target{AllowPrivateNetworks: true, Method: MethodFTP, Host: "127.0.0.1", Port: port, User: "kunde", PublicURL: hs.URL} // explicit TLS
	cred := Credentials{Password: "geheim"}

	// 1. Selbstsigniertes Zertifikat → Bestätigung nötig
	_, err := DeployFTP(context.Background(), art, tgt, cred, nil, nil)
	var idErr *UnknownIdentityError
	if !errors.As(err, &idErr) || idErr.Kind != "tls-certificate" || idErr.Mismatch {
		t.Fatalf("UnknownIdentityError erwartet, bekam %v", err)
	}
	// 2. Falscher Fingerprint → Abbruch als Mismatch
	tgt.Fingerprint = "SHA256:falsch"
	if _, err := DeployFTP(context.Background(), art, tgt, cred, nil, nil); !errors.As(err, &idErr) || !idErr.Mismatch {
		t.Fatalf("Mismatch erwartet, bekam %v", err)
	}
	// 3. Bestätigter Fingerprint → Deployment über FTPS (Steuer- und Datenkanal verschlüsselt)
	tgt.Fingerprint = idErr.Fingerprint
	var log []string
	logf := func(e Event) { log = append(log, e.Msg) }
	res, err := DeployFTP(context.Background(), art, tgt, cred, logf, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "styrixa.cgi")); err != nil {
		t.Fatal(err)
	}
	// Erstinstallation: Einmal-Code liegt im Datenordner und steht im Result, nicht im Log
	b, _ := os.ReadFile(filepath.Join(root, "styrixa-data", installCodeFile))
	if res.InstallCode == "" || parseInstallCode(b) != res.InstallCode {
		t.Fatalf("Installations-Code %q, Datei %q", res.InstallCode, b)
	}
	if strings.Contains(strings.Join(log, "\n"), res.InstallCode) {
		t.Error("Installations-Code im Deploy-Log")
	}
	// Wiederholter Deploy vor der Installation liefert denselben Code
	res2, err := DeployFTP(context.Background(), art, tgt, cred, nil, nil)
	if err != nil || res2.InstallCode != res.InstallCode {
		t.Fatalf("zweiter Deploy: %q %v", res2.InstallCode, err)
	}
}

func TestDeployFTPRollbackOnFailedHealth(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "styrixa.cgi"), []byte("alte version"), 0o755)
	port, _ := startPyFTPD(t, root, false)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := DeployFTP(ctx,
		Artifact{Name: "kunde", BinaryPath: fakeBinary(t), GOOS: "linux", GOARCH: "amd64"},
		Target{AllowPrivateNetworks: true, Method: MethodFTP, Host: "127.0.0.1", Port: port, User: "kunde", TLS: FTPPlain, AllowPlaintext: true, PublicURL: bad.URL},
		Credentials{Password: "geheim"}, nil, &http.Client{Timeout: time.Second})
	if err == nil {
		t.Fatal("Fehler erwartet")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "styrixa.cgi")); string(b) != "alte version" {
		t.Fatalf("Rollback hat nicht gegriffen: %q", b)
	}
}

func TestDeployFTPProbeFailureLeavesSiteUntouched(t *testing.T) {
	for _, c := range []struct {
		name   string
		server func(*testing.T) *httptest.Server
		want   string
	}{
		{"kein CGI", func(t *testing.T) *httptest.Server {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
			t.Cleanup(s.Close)
			return s
		}, "nicht als CGI"},
		{"kein mod_rewrite", func(t *testing.T) *httptest.Server { return fakeApache(t, false) }, "mod_rewrite"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			os.WriteFile(filepath.Join(root, ".htaccess"), []byte("# WordPress\n"), 0o644)
			os.WriteFile(filepath.Join(root, "index.php"), []byte("<?php // bestehende Website"), 0o644)
			port, _ := startPyFTPD(t, root, false)
			srv := c.server(t)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := DeployFTP(ctx,
				Artifact{Name: "kunde", BinaryPath: fakeBinary(t), GOOS: "linux", GOARCH: "amd64"},
				Target{AllowPrivateNetworks: true, Method: MethodFTP, Host: "127.0.0.1", Port: port, User: "kunde", TLS: FTPPlain, AllowPlaintext: true, PublicURL: srv.URL},
				Credentials{Password: "geheim"}, nil, &http.Client{Timeout: time.Second})
			if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "nichts verändert") {
				t.Fatalf("erwartet Fehler mit %q, bekam %v", c.want, err)
			}
			entries, _ := os.ReadDir(root)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if strings.Join(names, ",") != ".htaccess,index.php" {
				t.Fatalf("Webspace verändert: %v", names)
			}
			if b, _ := os.ReadFile(filepath.Join(root, ".htaccess")); string(b) != "# WordPress\n" {
				t.Fatal(".htaccess verändert")
			}
		})
	}
}
