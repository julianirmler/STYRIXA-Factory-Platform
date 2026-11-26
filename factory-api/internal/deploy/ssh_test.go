package deploy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testSSHServer: minimaler SSH-Server (exec + direct-tcpip), Befehle laufen
// per sh -c mit HOME=home. Ersetzt einen echten Server im Test.
func testSSHServer(t *testing.T, home string) (addr string, fp string) {
	t.Helper()
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(hk)
	cfg := &ssh.ServerConfig{PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
		if c.User() == "deploy" && string(pw) == "geheim" {
			return nil, nil
		}
		return nil, errors.New("nein")
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go serveSSH(nc, cfg, home)
		}
	}()
	return ln.Addr().String(), ssh.FingerprintSHA256(signer.PublicKey())
}

func serveSSH(nc net.Conn, cfg *ssh.ServerConfig, home string) {
	_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for nch := range chans {
		switch nch.ChannelType() {
		case "session":
			ch, rq, _ := nch.Accept()
			go func() {
				defer ch.Close()
				for r := range rq {
					if r.Type != "exec" {
						r.Reply(false, nil)
						continue
					}
					cmdLen := binary.BigEndian.Uint32(r.Payload[:4])
					cmd := exec.Command("sh", "-c", string(r.Payload[4:4+cmdLen]))
					cmd.Dir, cmd.Env = home, []string{"HOME=" + home, "PATH=/usr/bin:/bin:/usr/local/bin"}
					stdin, _ := cmd.StdinPipe()
					cmd.Stdout, cmd.Stderr = ch, ch.Stderr()
					r.Reply(true, nil)
					cmd.Start()
					go func() { io.Copy(stdin, ch); stdin.Close() }()
					code := 0
					if err := cmd.Wait(); err != nil {
						code = 1
						var ee *exec.ExitError
						if errors.As(err, &ee) {
							code = ee.ExitCode()
						}
					}
					st := make([]byte, 4)
					binary.BigEndian.PutUint32(st, uint32(code))
					ch.SendRequest("exit-status", false, st)
					return
				}
			}()
		case "direct-tcpip":
			var p struct {
				Host  string
				Port  uint32
				OHost string
				OPort uint32
			}
			ssh.Unmarshal(nch.ExtraData(), &p)
			up, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
			if err != nil {
				nch.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			ch, rq, _ := nch.Accept()
			go ssh.DiscardRequests(rq)
			go func() { io.Copy(ch, up); ch.Close() }()
			go func() { io.Copy(up, ch); up.Close() }()
		default:
			nch.Reject(ssh.UnknownChannelType, "nein")
		}
	}
}

func buildFakeApp(t *testing.T, fail bool) string {
	out := filepath.Join(t.TempDir(), "styrixa")
	args := []string{"build", "-o", out}
	if fail {
		args = append(args, "-ldflags", "-X main.fail=1")
	}
	cmd := exec.Command("go", append(args, "./testdata/fakeapp")...)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return out
}

func freePort(t *testing.T) int {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestDeploySSHWithRollback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("nur Linux")
	}
	home := t.TempDir()
	addr, fp := testSSHServer(t, home)
	host, ps, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(ps)
	appPort := freePort(t)
	t.Cleanup(func() { // gestartete App beenden
		if b, err := os.ReadFile(filepath.Join(home, "styrixa/shared/styrixa.pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				syscall.Kill(pid, syscall.SIGTERM)
			}
		}
	})

	tgt := Target{AllowPrivateNetworks: true, Method: MethodSSH, Host: host, Port: port, User: "deploy", AppPort: appPort, PublicURL: "https://kunde.at"}
	cred := Credentials{Password: "geheim"}
	art := Artifact{Name: "kunde", BinaryPath: buildFakeApp(t, false), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Host-Key unbestätigt → Rückfrage
	_, err := DeploySSH(ctx, art, tgt, cred, nil, nil)
	var idErr *UnknownIdentityError
	if !errors.As(err, &idErr) || idErr.Fingerprint != fp {
		t.Fatalf("Host-Key-Rückfrage erwartet, bekam %v", err)
	}
	tgt.Fingerprint = fp

	// Erstes Deployment
	var lines []string
	logf := func(e Event) { lines = append(lines, fmt.Sprintf("[%s] %s %s", e.Step, e.Level, e.Msg)) }
	res, err := DeploySSH(ctx, art, tgt, cred, logf, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, strings.Join(lines, "\n"))
	}
	first, _ := os.Readlink(filepath.Join(home, "styrixa/current"))
	if first != "releases/"+res.Release {
		t.Fatalf("current = %q", first)
	}
	if res.Snippets["caddy"] == "" {
		t.Error("Caddy-Snippet fehlt")
	}
	code, _ := os.ReadFile(filepath.Join(home, "styrixa/shared/data", installCodeFile))
	if res.InstallCode == "" || parseInstallCode(code) != res.InstallCode {
		t.Fatalf("Installations-Code %q, Datei %q", res.InstallCode, code)
	}
	if strings.Contains(strings.Join(lines, "\n"), res.InstallCode) {
		t.Error("Installations-Code im Deploy-Log")
	}

	// Zweites Deployment mit kaputtem Binary → Rollback
	time.Sleep(1100 * time.Millisecond) // neuer Release-Zeitstempel
	broken := art
	broken.BinaryPath = buildFakeApp(t, true)
	lines = nil
	if _, err := DeploySSH(ctx, broken, tgt, cred, logf, nil); err == nil {
		t.Fatal("Fehler erwartet")
	}
	now, _ := os.Readlink(filepath.Join(home, "styrixa/current"))
	if now != first {
		t.Fatalf("Rollback fehlgeschlagen: current=%q, erwartet %q\n%s", now, first, strings.Join(lines, "\n"))
	}
	if !strings.Contains(strings.Join(lines, "\n"), "Vorheriges Release wieder aktiv") {
		t.Errorf("Rollback nicht geloggt:\n%s", strings.Join(lines, "\n"))
	}

	// Falsche Plattform wird abgelehnt
	wrong := art
	wrong.GOARCH = "riscv64"
	if _, err := DeploySSH(ctx, wrong, tgt, cred, nil, nil); err == nil || !strings.Contains(err.Error(), "Plattform") {
		t.Fatalf("Plattform-Fehler erwartet, bekam %v", err)
	}
}

func TestCheckExecutable(t *testing.T) {
	tests := []struct {
		out     string
		runErr  error
		wantErr string
	}{
		{"rc=0\n", nil, ""},
		{"rc=2\n", nil, ""}, // Programm lief, nur Flag-Fehler
		{"rc=126\n", nil, "noexec"},
		{"rc=127\n", nil, "noexec"},
		{"", errors.New("Verbindung weg"), "Startprüfung"},
	}
	for _, tt := range tests {
		var got string
		err := checkExecutable(func(cmd string) (string, error) { got = cmd; return tt.out, tt.runErr }, "/home/x/my app/releases/1")
		if !strings.Contains(got, `cd '/home/x/my app/releases/1' && ./styrixa -h`) {
			t.Errorf("Befehl nicht gequotet: %s", got)
		}
		if tt.wantErr == "" && err != nil || tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%q: err = %v, want %q", tt.out, err, tt.wantErr)
		}
	}
}

// Echter Fall: Binary ohne Ausführungsrecht (wie noexec) → Abbruch vor dem Umschalten.
func TestCheckExecutableRealShell(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "styrixa"), []byte("\x7fELF kaputt"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(cmd string) (string, error) {
		b, err := exec.Command("sh", "-c", cmd).Output()
		return string(b), err
	}
	if err := checkExecutable(run, dir); err == nil || !strings.Contains(err.Error(), "noexec") {
		t.Fatalf("err = %v", err)
	}
	app := buildFakeApp(t, false)
	b, _ := os.ReadFile(app)
	os.Remove(filepath.Join(dir, "styrixa"))
	if err := os.WriteFile(filepath.Join(dir, "styrixa"), b, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkExecutable(run, dir); err != nil {
		t.Fatalf("lauffähiges Binary abgelehnt: %v", err)
	}
}
