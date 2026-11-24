// sign erzeugt Release-Schlüssel, signiert und prüft Dateien (ed25519).
//
//	go run ./tools/sign -keygen -secret-file ../signing.secret
//	    → schreibt den GEHEIMEN Schlüssel (Base64-Seed) nur in die Datei (Rechte 0600)
//	    → gibt nur den ÖFFENTLICHEN Schlüssel aus
//	go run ./tools/sign FILE
//	    → FILE.sig, Schlüssel aus STYRIXA_SIGNING_KEY (Base64-Seed, GitHub-Secret)
//	go run ./tools/sign -verify -pub <Base64> FILE
//	    → prüft FILE.sig gegen den öffentlichen Schlüssel
//
// Der geheime Schlüssel wird nie auf stdout/stderr ausgegeben.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	keygen := flag.Bool("keygen", false, "Schlüsselpaar erzeugen")
	secretFile := flag.String("secret-file", "", "Zieldatei für den geheimen Schlüssel (nur mit -keygen)")
	verify := flag.Bool("verify", false, "Signatur prüfen")
	pub := flag.String("pub", "", "öffentlicher Schlüssel (Base64, nur mit -verify)")
	flag.Parse()

	var err error
	switch {
	case *keygen:
		err = runKeygen(*secretFile)
	case *verify:
		err = runVerify(*pub, flag.Args())
	default:
		err = runSign(flag.Args())
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func runKeygen(secretFile string) error {
	if secretFile == "" {
		return errors.New("-secret-file ist Pflicht (der geheime Schlüssel wird nie ausgegeben)")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(secretFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("%w (vorhandene Datei wird nicht überschrieben)", err)
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func runSign(args []string) error {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("STYRIXA_SIGNING_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize || len(args) != 1 {
		return errors.New("STYRIXA_SIGNING_KEY (Base64-Seed) und genau eine Datei nötig")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	return os.WriteFile(args[0]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}

func runVerify(pubB64 string, args []string) error {
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubB64))
	if err != nil || len(pub) != ed25519.PublicKeySize || len(args) != 1 {
		return errors.New("-pub <Base64-Schlüssel> und genau eine Datei nötig")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sigB64, err := os.ReadFile(args[0] + ".sig")
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || !ed25519.Verify(pub, data, sig) {
		return errors.New("Signatur UNGÜLTIG")
	}
	fmt.Println("Signatur gültig:", args[0])
	return nil
}
