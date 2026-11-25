package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

// M1: Binary mit allen Modulen, Startseite und Admin je 100× abrufen, RSS < 20 MB.
// Nur unter Linux (Ziel-Plattform der VPS, /proc): Die RSS unter macOS enthält
// gemeinsam genutzte Systembibliotheken und ist nicht vergleichbar. Läuft in der CI.
func TestServerRSSBelow20MB(t *testing.T) {
	if runtime.GOOS != "linux" && os.Getenv("STYRIXA_RSS_ANY_OS") == "" {
		t.Skip("RSS-Messung nur unter Linux (/proc)")
	}
	if testing.Short() {
		t.Skip("baut ein Binary")
	}
	matrix := exec.Command("go", "run", "./tools/matrix")
	matrix.Dir = "../.." // liest modules.json aus cms-template/
	out, err := matrix.Output()
	if err != nil {
		t.Fatalf("tools/matrix: %v", err)
	}
	var tags string // längste Kombination = alle Module
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f := strings.Fields(line); len(f) == 2 && len(f[1]) > len(tags) {
			tags = f[1]
		}
	}
	bin := filepath.Join(t.TempDir(), "styrixa")
	build := exec.Command("go", "build", "-trimpath", "-tags", strings.ReplaceAll(tags, ",", " "), "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	dataDir := t.TempDir()
	db, err := core.OpenDB(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := core.Migrate(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	if err := (core.SettingsStore{DB: db}).Set(context.Background(), core.KeyInstalledAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	db.Close()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	cmd := exec.Command(bin, "--addr", addr)
	cmd.Env = append(os.Environ(), "STYRIXA_DATA="+dataDir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	base := "http://" + addr
	for i := 0; ; i++ { // warten, bis der Server lauscht
		if res, err := http.Get(base + "/healthz"); err == nil {
			res.Body.Close()
			break
		}
		if i > 50 {
			t.Fatal("Server startet nicht")
		}
		time.Sleep(100 * time.Millisecond)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for i := 0; i < 100; i++ {
		for _, p := range []string{"/", "/admin"} {
			res, err := client.Get(base + p)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, res.Body)
			res.Body.Close()
		}
	}
	if runtime.GOOS != "linux" {
		return // nur Ablauf geprüft (STYRIXA_RSS_ANY_OS)
	}
	status, err := os.ReadFile("/proc/" + strconv.Itoa(cmd.Process.Pid) + "/status")
	if err != nil {
		t.Fatal(err)
	}
	var kb int
	for _, line := range strings.Split(string(status), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[0] == "VmRSS:" {
			kb, _ = strconv.Atoi(f[1])
		}
	}
	t.Logf("RSS nach 200 Anfragen (%s): %d KB", tags, kb)
	if kb == 0 || kb >= 20*1024 {
		t.Fatalf("RSS %d KB, Grenze 20480 KB", kb)
	}
}
