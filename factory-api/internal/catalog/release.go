package catalog

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
// jeder STYRIXA-Release signiert ist. Muss identisch zu
// cms-template/internal/core/release.go sein (Test TestPublicKeyMatchesCMS).
const ReleasePublicKey = "/jnumNHg1fOwsE++oFInqJa7p46kaNoBUAc4ENeLICI="

// verifySignature prüft sigB64 (Inhalt von SHA256SUMS.sig) über sums.
func verifySignature(pubB64 string, sums, sigB64 []byte) error {
	pub, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("ungültiger öffentlicher Schlüssel")
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("Signatur fehlt oder ist beschädigt")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), sums, sig) {
		return errors.New("Signatur ungültig")
	}
	return nil
}

// parseSums liest eine SHA256SUMS-Datei im Format von sha256sum
// ("<hex>  <datei>" bzw. "<hex> *<datei>") in eine Map Dateiname → hex.
func parseSums(sums []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		sum, name, ok := strings.Cut(line, " ")
		if !ok || len(name) < 2 || (name[0] != ' ' && name[0] != '*') {
			return nil, fmt.Errorf("SHA256SUMS: ungültige Zeile %q", line)
		}
		name = name[1:]
		if b, err := hex.DecodeString(sum); err != nil || len(b) != 32 {
			return nil, fmt.Errorf("SHA256SUMS: ungültige Prüfsumme für %s", name)
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("SHA256SUMS: %s doppelt", name)
		}
		out[name] = strings.ToLower(sum)
	}
	return out, sc.Err()
}
