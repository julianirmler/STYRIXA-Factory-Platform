package deploy

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstallCodeCmd(t *testing.T) {
	run := func(dir, code string) string {
		out, err := exec.Command("sh", "-c", installCodeCmd(dir, code)).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return parseInstallCode(out)
	}
	dir := filepath.Join(t.TempDir(), "daten mit 'leerzeichen'")
	os.MkdirAll(dir, 0o700)
	c1, _ := newInstallCode()
	c2, _ := newInstallCode()

	// Erstinstallation: Code wird angelegt (0600) und zurückgegeben
	if got := run(dir, c1); got != c1 {
		t.Fatalf("Erstinstallation: %q, erwartet %q", got, c1)
	}
	if st, err := os.Stat(filepath.Join(dir, installCodeFile)); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("Code-Datei: %v %v", st, err)
	}
	// Wiederholter Deploy vor der Installation: bestehender Code bleibt
	os.WriteFile(filepath.Join(dir, "styrixa.db"), nil, 0o600) // Health-Check hat die DB angelegt
	if got := run(dir, c2); got != c1 {
		t.Fatalf("Wiederholung: %q, erwartet alten Code %q", got, c1)
	}
	// Nach der Installation (Code-Datei gelöscht, DB da): kein neuer Code
	os.Remove(filepath.Join(dir, installCodeFile))
	if got := run(dir, c2); got != "" {
		t.Fatalf("nach Installation: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, installCodeFile)); !os.IsNotExist(err) {
		t.Fatal("Code-Datei für installiertes CMS angelegt")
	}
}

func TestParseInstallCode(t *testing.T) {
	c, _ := newInstallCode()
	for in, want := range map[string]string{c + "\n": c, "": "", "abcd-efgh-ijkl-mnop": "", "'; rm -rf /": ""} {
		if got := parseInstallCode([]byte(in)); got != want {
			t.Errorf("%q → %q", in, got)
		}
	}
}
