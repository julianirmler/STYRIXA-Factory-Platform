package media

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"slices"
)

// Prüfung hochgeladener Varianten. Der Server verarbeitet keine Bilder
// (ADR-001) – er liest nur die Kopfdaten, um Format und Maße zu prüfen.

const (
	mimeWebP = "image/webp"
	mimeJPEG = "image/jpeg"

	maxVariantBytes = 4 << 20  // je Variante
	maxUploadBytes  = 12 << 20 // je Anfrage (3 Varianten + Felder)
	maxDimension    = 4000     // Pixel, Sicherheitsgrenze für Kopfdaten
	maxAspect       = 4        // höchstens 4 : 1 bzw. 1 : 4
)

// widths sind die Zielbreiten aus ADR-001, aufsteigend.
var widths = []int{480, 960, 1920}

// ext ist die Dateiendung je Format (auch Teil der öffentlichen URL).
func ext(mime string) string {
	if mime == mimeWebP {
		return "webp"
	}
	return "jpg"
}

// variant ist eine geprüfte, hochgeladene Bildvariante.
type variant struct {
	Mime          string
	Width, Height int
	Data          []byte
}

// sniff erkennt Format und Maße allein aus den Bytes – Dateiname und der
// Content-Type des Browsers zählen nicht. ok = false: kein WebP/JPEG oder
// Kopf kaputt.
func sniff(b []byte) (mime string, w, h int, ok bool) {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return "", 0, 0, false
		}
		w, h = cfg.Width, cfg.Height
		mime = mimeJPEG
	case len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		w, h, ok = webpSize(b)
		if !ok {
			return "", 0, 0, false
		}
		mime = mimeWebP
	default:
		return "", 0, 0, false
	}
	if w < 1 || h < 1 || w > maxDimension || h > maxDimension {
		return "", 0, 0, false
	}
	return mime, w, h, true
}

// webpSize liest die Maße aus dem ersten Chunk (VP8, VP8L oder VP8X), siehe
// https://developers.google.com/speed/webp/docs/riff_container.
func webpSize(b []byte) (w, h int, ok bool) {
	if len(b) < 30 {
		return 0, 0, false
	}
	// RIFF-Größe muss zur Datei passen (Browser schreiben sie exakt).
	if riff := binary.LittleEndian.Uint32(b[4:8]); int64(riff)+8 != int64(len(b)) {
		return 0, 0, false
	}
	chunk, size := string(b[12:16]), binary.LittleEndian.Uint32(b[16:20])
	if int64(size) > int64(len(b)-20) {
		return 0, 0, false
	}
	p := b[20:]
	switch chunk {
	case "VP8 ": // verlustbehaftet: Frame-Tag (3), Startcode 9D 01 2A, 14-Bit-Maße
		if size < 10 || p[3] != 0x9D || p[4] != 0x01 || p[5] != 0x2A || p[0]&1 != 0 {
			return 0, 0, false
		}
		w = int(binary.LittleEndian.Uint16(p[6:8]) & 0x3FFF)
		h = int(binary.LittleEndian.Uint16(p[8:10]) & 0x3FFF)
	case "VP8L": // verlustfrei: Signatur 0x2F, je 14 Bit Breite−1 und Höhe−1
		if size < 5 || p[0] != 0x2F {
			return 0, 0, false
		}
		bits := binary.LittleEndian.Uint32(p[1:5])
		w = int(bits&0x3FFF) + 1
		h = int((bits>>14)&0x3FFF) + 1
		if bits>>29 != 0 { // Versionsbits müssen 0 sein
			return 0, 0, false
		}
	case "VP8X": // erweitert: Flags (4), je 24 Bit Breite−1 und Höhe−1
		if size < 10 {
			return 0, 0, false
		}
		w = int(uint32(p[4])|uint32(p[5])<<8|uint32(p[6])<<16) + 1
		h = int(uint32(p[7])|uint32(p[8])<<8|uint32(p[9])<<16) + 1
	default:
		return 0, 0, false
	}
	return w, h, w > 0 && h > 0
}

// checkVariants prüft eine Upload-Menge: 1–3 Varianten, ein Format, Breiten
// 480/960/1920 von unten lückenlos (oder genau eine schmälere Variante),
// gleiches Seitenverhältnis (±1 px), höchstens 4 : 1. vs ist danach
// aufsteigend nach Breite sortiert. Die Meldung geht so an den Kunden.
func checkVariants(vs []variant) string {
	if len(vs) == 0 {
		return "Es wurde kein Bild übertragen."
	}
	if len(vs) > len(widths) {
		return "Zu viele Bildgrößen (höchstens drei)."
	}
	slices.SortFunc(vs, func(a, b variant) int { return a.Width - b.Width })
	for _, v := range vs[1:] {
		if v.Mime != vs[0].Mime {
			return "Alle Bildgrößen müssen dasselbe Format haben."
		}
	}
	if !(len(vs) == 1 && vs[0].Width < widths[0]) {
		for i, v := range vs {
			if v.Width != widths[i] {
				return "Unerwartete Bildbreiten – bitte das Bild erneut über die Mediathek hochladen."
			}
		}
	}
	big := vs[len(vs)-1]
	if big.Width > maxAspect*big.Height || big.Height > maxAspect*big.Width {
		return "Das Seitenverhältnis ist zu extrem (höchstens 4 : 1)."
	}
	for _, v := range vs {
		// v.Height ≈ v.Width × H / W, ±1 px (Rundung im Browser)
		if d := v.Height*big.Width - big.Height*v.Width; d > big.Width || d < -big.Width {
			return "Die Bildgrößen haben unterschiedliche Seitenverhältnisse."
		}
	}
	return ""
}
