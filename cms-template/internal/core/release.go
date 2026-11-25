package core

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ReleasePublicKey ist der öffentliche ed25519-Schlüssel (Base64), mit dem
// jeder STYRIXA-Release signiert ist. Der geheime Teil liegt nur als
// GitHub-Secret STYRIXA_SIGNING_KEY vor (siehe tools/sign).
const ReleasePublicKey = "/jnumNHg1fOwsE++oFInqJa7p46kaNoBUAc4ENeLICI="

// VerifyRelease prüft, ob sigB64 (Inhalt von SHA256SUMS.sig) eine gültige
// Signatur von sums (Inhalt von SHA256SUMS) mit ReleasePublicKey ist.
func VerifyRelease(sums, sigB64 []byte) error {
	return verifyWithKey(ReleasePublicKey, sums, sigB64)
}

func verifyWithKey(pubB64 string, sums, sigB64 []byte) error {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("release: ungültiger öffentlicher Schlüssel")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("release: Signatur fehlt oder ist beschädigt")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), sums, sig) {
		return errors.New("release: Signatur ungültig")
	}
	return nil
}

// ChecksumFor liefert den SHA-256 (hex, Kleinbuchstaben) von filename aus
// einer SHA256SUMS-Datei im Format von sha256sum ("<hex>  <datei>" bzw.
// "<hex> *<datei>" im Binärmodus).
func ChecksumFor(sums []byte, filename string) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		sum, name, ok := strings.Cut(line, " ")
		if !ok || len(name) < 2 || (name[0] != ' ' && name[0] != '*') {
			continue
		}
		if name[1:] != filename {
			continue
		}
		if b, err := hex.DecodeString(sum); err != nil || len(b) != 32 {
			return "", fmt.Errorf("release: ungültige Prüfsumme für %s", filename)
		}
		return strings.ToLower(sum), nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("release: keine Prüfsumme für %s", filename)
}
