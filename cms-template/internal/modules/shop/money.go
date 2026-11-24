package shop

import (
	"strconv"
	"strings"
)

// Cents ist ein Geldbetrag in der kleinsten Einheit. Niemals float für Geld.
type Cents int64

// Format liefert DACH-Formatierung, z. B. "€ 1.234,56" (de-AT) oder "CHF 1'234.56" (de-CH).
func (c Cents) Format(currency, locale string) string {
	neg := c < 0
	if neg {
		c = -c
	}
	euros, cents := int64(c)/100, int64(c)%100
	thou, dec := ".", ","
	if locale == "de-CH" {
		thou, dec = "'", "."
	}
	s := groupThousands(strconv.FormatInt(euros, 10), thou) + dec + pad2(cents)
	sym := map[string]string{"EUR": "€", "CHF": "CHF"}[currency]
	if sym == "" {
		sym = currency
	}
	out := sym + " " + s
	if locale == "de-DE" {
		out = s + " " + sym
	}
	if neg {
		out = "-" + out
	}
	return out
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

func groupThousands(s, sep string) string {
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteString(sep)
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// divRound teilt mit kaufmännischer Rundung (half away from zero).
func divRound(a, b int64) int64 {
	if (a < 0) != (b < 0) {
		return (a - b/2) / b
	}
	return (a + b/2) / b
}
