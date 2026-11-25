package core

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestReleasePublicKeyValid(t *testing.T) {
	b, err := base64.StdEncoding.DecodeString(ReleasePublicKey)
	if err != nil {
		t.Fatalf("ReleasePublicKey ist kein gültiges Base64: %v", err)
	}
	if len(b) != ed25519.PublicKeySize {
		t.Fatalf("ReleasePublicKey hat %d Byte, erwartet %d", len(b), ed25519.PublicKeySize)
	}
}

func TestVerifyRelease(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("aaaa  styrixa_media_linux_amd64.tar.gz\n")
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sums)) + "\n")
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	tests := []struct {
		name    string
		pub     string
		sums    []byte
		sig     []byte
		wantErr bool
	}{
		{"gültig", pubB64, sums, sig, false},
		{"SHA256SUMS verändert", pubB64, []byte("bbbb  styrixa_media_linux_amd64.tar.gz\n"), sig, true},
		{"falscher Schlüssel", base64.StdEncoding.EncodeToString(otherPub), sums, sig, true},
		{"leere Signatur", pubB64, sums, nil, true},
		{"kein Base64", pubB64, sums, []byte("!!!nicht-base64!!!"), true},
		{"Signatur zu kurz", pubB64, sums, []byte(base64.StdEncoding.EncodeToString([]byte("kurz"))), true},
		{"ungültiger Schlüssel", "abc", sums, sig, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyWithKey(tt.pub, tt.sums, tt.sig)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}

	// Der echte Schlüssel darf eine fremde Signatur nicht akzeptieren.
	if err := VerifyRelease(sums, sig); err == nil {
		t.Fatal("VerifyRelease akzeptiert Signatur eines fremden Schlüssels")
	}
}

func TestChecksumFor(t *testing.T) {
	h1 := strings.Repeat("ab", 32)
	h2 := strings.Repeat("CD", 32)
	sums := []byte(h1 + "  styrixa_media_linux_amd64.tar.gz\n" +
		h2 + " *styrixa_shop_darwin_arm64.tar.gz\r\n" +
		"zzzz  styrixa_kaputt.tar.gz\n" +
		"\n")

	tests := []struct {
		name    string
		file    string
		want    string
		wantErr bool
	}{
		{"vorhanden (zwei Leerzeichen)", "styrixa_media_linux_amd64.tar.gz", h1, false},
		{"Binärmodus, CRLF, Großbuchstaben", "styrixa_shop_darwin_arm64.tar.gz", strings.ToLower(h2), false},
		{"fehlend", "styrixa_users_linux_amd64.tar.gz", "", true},
		{"Teilname zählt nicht", "media_linux_amd64.tar.gz", "", true},
		{"ungültiger Hash", "styrixa_kaputt.tar.gz", "", true},
		{"leerer Name", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ChecksumFor(sums, tt.file)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
