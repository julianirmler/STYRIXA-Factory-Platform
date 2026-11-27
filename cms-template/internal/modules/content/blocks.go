package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Blockformat = Ausgabe von Editor.js: {time, blocks:[{id, type, data}], version}.

const (
	FormatBlocks = "blocks"
	FormatHTML   = "html" // seitenweiter HTML-Modus, nur Admin

	maxBodyBytes = 1 << 20 // 1 MB
	maxBlocks    = 500
	maxListDepth = 3
)

// Doc ist ein Editor.js-Dokument.
type Doc struct {
	Time    int64   `json:"time,omitempty"`
	Blocks  []Block `json:"blocks"`
	Version string  `json:"version,omitempty"`
}

// Block ist ein Editor.js-Block; Data wird je Typ dekodiert.
type Block struct {
	ID   string          `json:"id,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type (
	paragraphData struct {
		Text string `json:"text"`
	}
	headerData struct {
		Text  string `json:"text"`
		Level int    `json:"level"`
	}
	listData struct {
		Style string     `json:"style"` // ordered | unordered
		Items []listItem `json:"items"`
	}
	quoteData struct {
		Text    string `json:"text"`
		Caption string `json:"caption"`
	}
	imageData struct {
		File struct {
			URL string `json:"url"`
		} `json:"file"`
		URL     string `json:"url"` // einfache Variante ohne file-Objekt
		Caption string `json:"caption"`
		Alt     string `json:"alt"`
		// Ab Session 4.1 (Bild aus der Mediathek): Maße der url-Variante und
		// alle Varianten für srcset. Fehlen sie, bleibt es ein einfaches <img>.
		Width    int            `json:"width,omitempty"`
		Height   int            `json:"height,omitempty"`
		Variants []imageVariant `json:"variants,omitempty"`
	}
	imageVariant struct {
		URL   string `json:"url"`
		Width int    `json:"width"`
	}
	rawData struct {
		HTML string `json:"html"`
	}
	faqData struct {
		Items []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"items"`
	}
	answerData struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`  // Kurzantwort
		Details  string `json:"details"` // optional
	}
	ctaData struct {
		Text  string `json:"text"`
		Label string `json:"label"`
		URL   string `json:"url"`
	}
)

// listItem: Editor.js List v2 ({content, items}) oder älteres Format (String).
type listItem struct {
	Content string     `json:"content"`
	Items   []listItem `json:"items"`
}

func (li *listItem) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &li.Content)
	}
	type plain listItem // ohne diese Methode
	return json.Unmarshal(b, (*plain)(li))
}

func (u imageData) url() string {
	if u.File.URL != "" {
		return u.File.URL
	}
	return u.URL
}

// InputError ist ein Fehler im gespeicherten Inhalt – die Meldung geht so an
// den Kunden (deutsch). Forbidden = fehlende Berechtigung (403 statt 400).
type InputError struct {
	Msg       string
	Forbidden bool
}

func (e *InputError) Error() string { return e.Msg }

func badInput(format string, a ...any) error { return &InputError{Msg: fmt.Sprintf(format, a...)} }

var errRawForbidden = &InputError{Msg: "Raw-HTML dürfen nur Administratoren speichern.", Forbidden: true}

// Validate prüft einen Body vor dem Speichern. admin = Rolle des Speichernden,
// serverseitig ermittelt (core.IsAdmin). Unbekannte Blocktypen werden
// abgelehnt, nicht still verworfen.
func Validate(body, format string, admin bool) error {
	if len(body) > maxBodyBytes {
		return badInput("Der Inhalt ist zu groß (höchstens 1 MB).")
	}
	switch format {
	case FormatHTML:
		if !admin {
			return errRawForbidden
		}
		return nil
	case FormatBlocks:
	default:
		return badInput("Unbekanntes Format %q.", format)
	}
	doc, err := parseDoc(body)
	if err != nil {
		return err
	}
	for i, b := range doc.Blocks {
		v, err := decodeBlock(b)
		if err != nil {
			return fmt.Errorf("Block %d: %w", i+1, err)
		}
		if _, raw := v.(rawData); raw && !admin {
			return errRawForbidden
		}
	}
	return nil
}

func parseDoc(body string) (*Doc, error) {
	var doc Doc
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return nil, badInput("Der Inhalt ist kein gültiges Editor.js-JSON.")
	}
	if len(doc.Blocks) > maxBlocks {
		return nil, badInput("Zu viele Blöcke (höchstens %d).", maxBlocks)
	}
	return &doc, nil
}

// decodeBlock dekodiert und prüft einen Block; liefert die typisierten Daten.
func decodeBlock(b Block) (any, error) {
	data := b.Data
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		data = []byte("{}")
	}
	dec := func(v any) error {
		if err := json.Unmarshal(data, v); err != nil {
			return badInput("Daten des Blocks „%s“ sind ungültig.", b.Type)
		}
		return nil
	}
	switch b.Type {
	case "paragraph":
		var d paragraphData
		return d, dec(&d)
	case "header":
		var d headerData
		if err := dec(&d); err != nil {
			return nil, err
		}
		if d.Level < 2 || d.Level > 4 {
			return nil, badInput("Überschriften sind nur in den Ebenen 2 bis 4 erlaubt (Ebene 1 ist der Seitentitel).")
		}
		return d, nil
	case "list":
		var d listData
		if err := dec(&d); err != nil {
			return nil, err
		}
		if d.Style == "" {
			d.Style = "unordered"
		}
		if d.Style != "ordered" && d.Style != "unordered" {
			return nil, badInput("Unbekannte Listenart %q.", d.Style)
		}
		if listDepth(d.Items) > maxListDepth {
			return nil, badInput("Listen dürfen höchstens %d Ebenen tief sein.", maxListDepth)
		}
		return d, nil
	case "quote":
		var d quoteData
		return d, dec(&d)
	case "delimiter":
		return struct{}{}, nil
	case "image":
		var d imageData
		if err := dec(&d); err != nil {
			return nil, err
		}
		return d, checkImage(d)
	case "raw":
		var d rawData
		return d, dec(&d)
	case "faq":
		var d faqData
		return d, dec(&d)
	case "answer":
		var d answerData
		if err := dec(&d); err != nil {
			return nil, err
		}
		if plainText(SanitizeInline(d.Question)) == "" || plainText(SanitizeInline(d.Answer)) == "" {
			return nil, badInput("Ein Antwort-Block braucht Frage und Kurzantwort.")
		}
		return d, nil
	case "cta":
		var d ctaData
		if err := dec(&d); err != nil {
			return nil, err
		}
		if d.URL != "" {
			if _, _, ok := safeHref(d.URL); !ok {
				return nil, badInput("Die Link-Adresse des Call-to-Action ist nicht erlaubt (https://, http://, mailto:, tel:, / oder #).")
			}
		}
		return d, nil
	case "":
		return nil, badInput("Ein Block hat keinen Typ.")
	}
	return nil, badInput("Unbekannter Blocktyp „%s“ – der Inhalt wurde nicht gespeichert.", b.Type)
}

// Grenzen für Bilder aus der Mediathek (Session 4.1).
const (
	maxImageVariants = 3
	maxImageSide     = 10000
)

// checkImage prüft Maße und Varianten: höchstens drei, jede ein eigener Pfad
// (/…, kein //), Breiten 1–10000 und verschieden; Breite und Höhe nur
// gemeinsam.
func checkImage(d imageData) error {
	if (d.Width == 0) != (d.Height == 0) || d.Width < 0 || d.Height < 0 || d.Width > maxImageSide || d.Height > maxImageSide {
		return badInput("Die Bildmaße sind ungültig.")
	}
	if len(d.Variants) > maxImageVariants {
		return badInput("Ein Bild hat höchstens %d Größen.", maxImageVariants)
	}
	seen := map[int]bool{}
	for _, v := range d.Variants {
		if !ownPath(v.URL) || v.Width < 1 || v.Width > maxImageSide || seen[v.Width] {
			return badInput("Die Bildgrößen sind ungültig – bitte das Bild erneut aus der Mediathek wählen.")
		}
		seen[v.Width] = true
	}
	return nil
}

// ownPath: Pfad auf dieser Website (/…, nicht //…), ohne Leerzeichen und
// Kommas (srcset-Trenner).
func ownPath(u string) bool {
	v, ok := safeImageURL(u)
	return ok && v == u && strings.HasPrefix(u, "/") && !strings.HasPrefix(u, "//") && !strings.ContainsAny(u, " ,\t\n\r")
}

func listDepth(items []listItem) int {
	max := 0
	for _, it := range items {
		if d := listDepth(it.Items); d > max {
			max = d
		}
	}
	if len(items) == 0 {
		return 0
	}
	return max + 1
}

// IsInputError meldet, ob err ein Kunden-Eingabefehler ist.
func IsInputError(err error) (*InputError, bool) {
	var ie *InputError
	ok := errors.As(err, &ie)
	return ie, ok
}
