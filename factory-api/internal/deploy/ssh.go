package deploy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// KeepReleases: so viele Releases bleiben für Rollbacks auf dem Server.
const KeepReleases = 3

// SSHDialer ist austauschbar (Tests).
type SSHDialer func(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error)

func guardedSSHDial(allowPrivate bool) SSHDialer {
	return func(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
		d := guardedDialer(allowPrivate)
		return sshDialWith(ctx, d, addr, cfg)
	}
}

func sshDialWith(ctx context.Context, d net.Dialer, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

// DeploySSH führt das komplette SSH-Deployment aus.
//
// Layout auf dem Server (Beispiel RemoteDir = "styrixa"):
//
//	~/styrixa/releases/20261124T081500Z/styrixa
//	~/styrixa/current -> releases/20261124T081500Z
//	~/styrixa/shared/data/styrixa.db   (bleibt über Releases erhalten)
//	~/styrixa/shared/styrixa.env
func DeploySSH(ctx context.Context, art Artifact, t Target, cred Credentials, log Logger, dial SSHDialer) (*Result, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if dial == nil {
		dial = guardedSSHDial(t.AllowPrivateNetworks)
	}

	// 1. Verbinden + Host-Key prüfen ------------------------------------------
	auth, err := sshAuth(cred)
	if err != nil {
		return nil, err
	}
	var idErr *UnknownIdentityError
	cfg := &ssh.ClientConfig{
		User:    t.User,
		Auth:    auth,
		Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fp := ssh.FingerprintSHA256(key)
			switch {
			case t.Fingerprint == "":
				idErr = &UnknownIdentityError{Kind: "ssh-host-key", Fingerprint: fp}
			case t.Fingerprint != fp:
				idErr = &UnknownIdentityError{Kind: "ssh-host-key", Fingerprint: fp, Mismatch: true}
			default:
				return nil
			}
			return idErr
		},
	}
	addr := net.JoinHostPort(strings.Trim(t.Host, "[]"), strconv.Itoa(t.Port))
	log.emit("connect", "info", "Verbinde mit %s als %s …", addr, t.User)
	client, err := dial(ctx, addr, cfg)
	if idErr != nil {
		return nil, idErr
	}
	if err != nil {
		return nil, fmt.Errorf("SSH-Verbindung fehlgeschlagen: %w", err)
	}
	defer client.Close()
	log.emit("connect", "ok", "Verbunden, Host-Key %s bestätigt", t.Fingerprint)

	run := func(cmd string) (string, error) { return sshRun(ctx, client, cmd, nil) }

	// 2. Plattform prüfen ----------------------------------------------------
	uname, err := run("uname -sm")
	if err != nil {
		return nil, fmt.Errorf("uname: %w", err)
	}
	goos, goarch := platformFromUname(uname)
	if goos != art.GOOS || goarch != art.GOARCH {
		return nil, fmt.Errorf("Zielserver ist %s/%s, das Build ist %s/%s – bitte passende Plattform wählen",
			goos, goarch, art.GOOS, art.GOARCH)
	}
	log.emit("platform", "ok", "Server-Plattform %s/%s passt", goos, goarch)

	home, err := run(`printf %s "$HOME"`)
	if err != nil {
		return nil, err
	}
	base := t.RemoteDir
	switch {
	case base == "~":
		base = home
	case strings.HasPrefix(base, "~/"):
		base = home + "/" + strings.TrimPrefix(base, "~/")
	case !strings.HasPrefix(base, "/"):
		base = home + "/" + base
	}
	base = strings.TrimRight(base, "/")
	q := shellQuote
	rel := releaseID(time.Now())
	relDir := base + "/releases/" + rel

	// 3. Ordner + Upload -------------------------------------------------------
	if _, err := run(fmt.Sprintf("mkdir -p %s %s %s && chmod 700 %s",
		q(relDir), q(base+"/shared/data"), q(base+"/shared/themes"), q(base+"/shared"))); err != nil {
		return nil, fmt.Errorf("Ordner anlegen: %w", err)
	}
	f, err := os.Open(art.BinaryPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, _ := f.Stat()
	log.emit("upload", "info", "Lade Binary hoch (%s) …", humanBytes(st.Size()))
	if _, err := sshRun(ctx, client,
		fmt.Sprintf("cat > %s.part && chmod 755 %s.part && mv -f %s.part %s",
			q(relDir+"/styrixa"), q(relDir+"/styrixa"), q(relDir+"/styrixa"), q(relDir+"/styrixa")), f); err != nil {
		return nil, fmt.Errorf("Upload: %w", err)
	}
	if _, err := run(fmt.Sprintf("ln -sfn ../shared/themes %s", q(relDir+"/themes"))); err != nil {
		return nil, err
	}
	if _, err := run(fmt.Sprintf("test -f %[1]s || printf 'STYRIXA_ADDR=127.0.0.1:%[2]d\\n' > %[1]s; chmod 600 %[1]s",
		q(base+"/shared/styrixa.env"), t.AppPort)); err != nil {
		return nil, err
	}
	log.emit("upload", "ok", "Release %s hochgeladen", rel)

	// 4. Umschalten (atomar) ---------------------------------------------------
	prev, _ := run(fmt.Sprintf("readlink %s 2>/dev/null || true", q(base+"/current")))
	prev = strings.TrimSpace(prev)
	if err := switchCurrent(run, base, "releases/"+rel, goos); err != nil {
		return nil, err
	}
	log.emit("switch", "ok", "current → releases/%s", rel)

	// 5. Dienst (neu) starten ------------------------------------------------
	service := ServiceName(art.Name)
	mode, hints, err := restartService(run, log, base, service, t.AppPort, goos)
	if err != nil {
		rollback(run, log, base, prev, service, t.AppPort, goos, mode)
		return nil, err
	}

	// 6. Health-Check über den SSH-Tunnel (kein offener Port nötig) ------------
	tunnel := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", t.AppPort))
		}},
	}
	log.emit("health", "info", "Health-Check 127.0.0.1:%d/healthz …", t.AppPort)
	if err := healthOverSSH(ctx, client, tunnel, run, t.AppPort); err != nil {
		log.emit("health", "error", "%v", err)
		rollback(run, log, base, prev, service, t.AppPort, goos, mode)
		return nil, err
	}
	log.emit("health", "ok", "App antwortet")

	// 7. Alte Releases aufräumen ------------------------------------------------
	cleanupReleases(run, log, base, rel)

	url := t.PublicURL
	domain := strings.TrimPrefix(strings.TrimPrefix(url, "https://"), "http://")
	domain = strings.TrimRight(domain, "/")
	if domain == "" {
		domain = "kunde.example"
	}
	hints = append(hints,
		fmt.Sprintf("Die App lauscht auf 127.0.0.1:%d. Domain per Reverse-Proxy (Caddy/nginx) darauf zeigen lassen.", t.AppPort),
		"Erster Aufruf: /install – Seitentitel, Region, GISA-Nummer und Admin-Account einrichten.")
	return &Result{
		Method:  MethodSSH,
		Release: rel,
		URL:     url,
		Hints:   hints,
		Snippets: map[string]string{
			"caddy": CaddySnippet(domain, t.AppPort),
			"nginx": NginxSnippet(domain, t.AppPort),
		},
	}, nil
}

func sshAuth(c Credentials) ([]ssh.AuthMethod, error) {
	var m []ssh.AuthMethod
	if len(c.PrivateKeyPEM) > 0 {
		signer, err := ssh.ParsePrivateKey(c.PrivateKeyPEM)
		if err != nil {
			return nil, fmt.Errorf("privater Schlüssel ungültig (passwortgeschützte Schlüssel werden nicht unterstützt): %w", err)
		}
		m = append(m, ssh.PublicKeys(signer))
	}
	if c.Password != "" {
		pw := c.Password
		m = append(m, ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range qs {
					ans[i] = pw
				}
				return ans, nil
			}))
	}
	if len(m) == 0 {
		return nil, errors.New("Passwort oder Deploy-Key erforderlich")
	}
	return m, nil
}

// sshRun führt einen Befehl aus; stdin optional (für den Upload).
func sshRun(ctx context.Context, c *ssh.Client, cmd string, stdin interface{ Read([]byte) (int, error) }) (string, error) {
	s, err := c.NewSession()
	if err != nil {
		return "", err
	}
	defer s.Close()
	var out, errb bytes.Buffer
	s.Stdout, s.Stderr = &out, &errb
	if stdin != nil {
		s.Stdin = stdin
	}
	done := make(chan error, 1)
	go func() { done <- s.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = s.Signal(ssh.SIGKILL)
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return out.String(), fmt.Errorf("%w: %s", err, strings.TrimSpace(errb.String()))
		}
		return out.String(), nil
	}
}

func platformFromUname(s string) (goos, goarch string) {
	f := strings.Fields(strings.TrimSpace(s))
	if len(f) < 2 {
		return "unknown", "unknown"
	}
	goos = strings.ToLower(f[0])
	switch f[1] {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		goarch = f[1]
	}
	return goos, goarch
}

func switchCurrent(run func(string) (string, error), base, target, goos string) error {
	q := shellQuote
	cmd := fmt.Sprintf("ln -sfn %s %s && mv -Tf %s %s",
		q(target), q(base+"/current.tmp"), q(base+"/current.tmp"), q(base+"/current"))
	if goos != "linux" { // BSD-mv kennt kein -T
		cmd = fmt.Sprintf("ln -sfn %s %s", q(target), q(base+"/current"))
	}
	if _, err := run(cmd); err != nil {
		return fmt.Errorf("Symlink umschalten: %w", err)
	}
	return nil
}

// restartService nutzt systemd --user, sonst nohup als Fallback.
func restartService(run func(string) (string, error), log Logger, base, service string, port int, goos string) (mode string, hints []string, err error) {
	q := shellQuote
	if goos == "linux" {
		if _, e := run("systemctl --user show-environment >/dev/null 2>&1"); e == nil {
			unit := SystemdUserUnit(service, base, port)
			if _, err = run(fmt.Sprintf("mkdir -p ~/.config/systemd/user && printf '%%s' %s > ~/.config/systemd/user/%s.service",
				q(unit), service)); err != nil {
				return "systemd", nil, err
			}
			if _, err = run(fmt.Sprintf("systemctl --user daemon-reload && systemctl --user enable %[1]s >/dev/null 2>&1; systemctl --user restart %[1]s", service)); err != nil {
				return "systemd", nil, fmt.Errorf("systemd-Start: %w", err)
			}
			log.emit("service", "ok", "systemd-User-Service %s gestartet", service)
			if out, _ := run(`loginctl show-user "$(id -un)" -p Linger 2>/dev/null`); !strings.Contains(out, "Linger=yes") {
				who, _ := run("id -un")
				hints = append(hints, "Damit der Dienst nach Logout und Neustart weiterläuft, einmalig als Admin ausführen: sudo loginctl enable-linger "+strings.TrimSpace(who))
				log.emit("service", "warn", "Linger ist nicht aktiv – Dienst stoppt evtl. nach Logout")
			}
			return "systemd", hints, nil
		}
	}
	// Fallback: nohup + PID-Datei
	pid := q(base + "/shared/styrixa.pid")
	cmd := fmt.Sprintf(`if [ -f %[1]s ]; then kill "$(cat %[1]s)" 2>/dev/null; sleep 1; fi; `+
		`cd %[2]s && { STYRIXA_DATA=%[3]s nohup ./styrixa --addr 127.0.0.1:%[4]d >> %[5]s 2>&1 < /dev/null & echo $! > %[1]s; }`,
		pid, q(base+"/current"), q(base+"/shared/data"), port, q(base+"/shared/styrixa.log"))
	if _, err = run(cmd); err != nil {
		return "nohup", nil, fmt.Errorf("Start: %w", err)
	}
	log.emit("service", "warn", "systemd --user nicht verfügbar – mit nohup gestartet (kein Autostart nach Reboot)")
	return "nohup", []string{"Kein systemd verfügbar: Für Autostart nach Neustart einen @reboot-Cronjob oder den Hoster-Prozessmanager nutzen."}, nil
}

func rollback(run func(string) (string, error), log Logger, base, prev, service string, port int, goos, mode string) {
	if prev == "" {
		log.emit("rollback", "warn", "Kein vorheriges Release vorhanden – nichts zurückzurollen")
		return
	}
	log.emit("rollback", "warn", "Rollback auf %s …", prev)
	if err := switchCurrent(run, base, prev, goos); err != nil {
		log.emit("rollback", "error", "%v", err)
		return
	}
	if mode == "systemd" {
		_, _ = run("systemctl --user restart " + service)
	} else {
		_, _, _ = restartService(run, nil, base, service, port, "none")
	}
	log.emit("rollback", "ok", "Vorheriges Release wieder aktiv")
}

func cleanupReleases(run func(string) (string, error), log Logger, base, keep string) {
	out, err := run(fmt.Sprintf("ls -1 %s", shellQuote(base+"/releases")))
	if err != nil {
		return
	}
	var rels []string
	for _, r := range strings.Fields(out) {
		if reRelease.MatchString(r) {
			rels = append(rels, r)
		}
	}
	// Namen sind zeitlich sortierbar; neueste zuerst
	for i := 0; i < len(rels); i++ {
		for j := i + 1; j < len(rels); j++ {
			if rels[j] > rels[i] {
				rels[i], rels[j] = rels[j], rels[i]
			}
		}
	}
	for i, r := range rels {
		if i < KeepReleases || r == keep {
			continue
		}
		if _, err := run("rm -rf " + shellQuote(base+"/releases/"+r)); err == nil {
			log.emit("cleanup", "info", "Altes Release %s entfernt", r)
		}
	}
}

func humanBytes(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", n/1024)
}

// healthOverSSH prüft per SSH-Tunnel; ist Port-Forwarding am Server deaktiviert,
// wird curl/wget direkt auf dem Server verwendet.
func healthOverSSH(ctx context.Context, c *ssh.Client, tunnel *http.Client, run func(string) (string, error), port int) error {
	if conn, err := c.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		conn.Close()
		return waitHealthy(ctx, tunnel, "http://styrixa.local/healthz", 15, time.Second)
	} else {
		var oce *ssh.OpenChannelError
		if !errors.As(err, &oce) || oce.Reason == ssh.ConnectionFailed {
			// Port noch nicht offen (App startet) – normal weiter versuchen
			return waitHealthy(ctx, tunnel, "http://styrixa.local/healthz", 15, time.Second)
		}
	}
	u := fmt.Sprintf("http://127.0.0.1:%d/healthz", port)
	var last error
	for i := 0; i < 15; i++ {
		_, err := run(fmt.Sprintf("curl -fsS -m 3 %[1]s >/dev/null 2>&1 || wget -q -T 3 -O /dev/null %[1]s", u))
		if err == nil {
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("health-check %s fehlgeschlagen: %w", u, last)
}
