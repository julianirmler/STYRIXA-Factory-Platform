// sign signiert eine Datei (z. B. SHA256SUMS) mit ed25519.
// Schlüssel: Umgebungsvariable STYRIXA_SIGNING_KEY = Base64 des 32-Byte-Seeds (GitHub-Secret).
//
//	go run ./tools/sign SHA256SUMS          → SHA256SUMS.sig
//	go run ./tools/sign -keygen             → neues Schlüsselpaar ausgeben (einmalig)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	keygen := flag.Bool("keygen", false, "Schlüsselpaar erzeugen")
	flag.Parse()
	if *keygen {
		pub, priv, _ := ed25519.GenerateKey(rand.Reader)
		fmt.Println("STYRIXA_SIGNING_KEY (geheim, als GitHub-Secret):", base64.StdEncoding.EncodeToString(priv.Seed()))
		fmt.Println("Öffentlicher Schlüssel (in CMS und factory-api einbetten):", base64.StdEncoding.EncodeToString(pub))
		return
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv("STYRIXA_SIGNING_KEY"))
	if err != nil || len(seed) != ed25519.SeedSize || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "sign: STYRIXA_SIGNING_KEY (Base64-Seed) und genau eine Datei nötig")
		os.Exit(1)
	}
	data, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	if err := os.WriteFile(flag.Arg(0)+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
