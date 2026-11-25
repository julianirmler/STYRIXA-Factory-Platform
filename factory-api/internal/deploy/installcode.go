package deploy

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"regexp"
	"strings"
)

// Einmal-Code für /install des CMS (Session 1.2). Der Deploy legt ihn bei einer
// Erstinstallation im Datenordner ab und gibt ihn im Result zurück, damit die
// Factory ihn dem Kunden anzeigt. Ohne Code kann niemand sonst, der die frische
// URL zuerst aufruft, Admin werden. Das CMS löscht die Datei nach der
// Installation.
//
// Format und Dateiname müssen zu cms-template/internal/core/install.go passen.
const installCodeFile = "install-code"

var reInstallCode = regexp.MustCompile(`^[A-Z2-7]{4}(-[A-Z2-7]{4}){3}$`)

func newInstallCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base32.StdEncoding.EncodeToString(b)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16], nil
}

// parseInstallCode prüft den Inhalt einer Code-Datei; leer = keiner/ungültig.
func parseInstallCode(b []byte) string {
	s := strings.TrimSpace(string(b))
	if !reInstallCode.MatchString(s) {
		return ""
	}
	return s
}

// installCodeCmd liefert den Shell-Befehl für SSH: Vorhandene Code-Datei
// ausgeben (Deploy vor abgeschlossener Installation wiederholt), sonst bei
// einer Erstinstallation (noch keine styrixa.db) eine neue anlegen (0600).
// Bei bestehender Datenbank ohne Code-Datei ist das CMS schon installiert.
func installCodeCmd(dataDir, code string) string {
	return fmt.Sprintf(`cd %[1]s && if [ -f %[2]s ]; then cat %[2]s; elif [ ! -e styrixa.db ]; then (umask 077 && printf '%%s\n' %[3]s > %[2]s.part && mv -f %[2]s.part %[2]s) && cat %[2]s; fi`,
		shellQuote(dataDir), installCodeFile, shellQuote(code))
}

func installHint(code, installURL string) string {
	if code == "" {
		return "Das CMS ist bereits eingerichtet – Anmeldung unter /admin."
	}
	return "Erster Aufruf: " + installURL + " – mit dem angezeigten Installations-Code Seitentitel, Region, GISA-Zahl und Admin-Konto einrichten."
}
