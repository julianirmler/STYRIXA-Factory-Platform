package shop

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ErrNoTaxRate: kein Satz für Land/Klasse/Datum hinterlegt.
var ErrNoTaxRate = errors.New("shop: kein Steuersatz hinterlegt")

// TaxRate liest den am Stichtag gültigen Satz in Basispunkten aus shop_tax_rates.
func TaxRate(ctx context.Context, db *sql.DB, country, class string, at time.Time) (int64, error) {
	day := at.Format("2006-01-02")
	var bp int64
	err := db.QueryRowContext(ctx, `
		SELECT rate_bp FROM shop_tax_rates
		WHERE country = ? AND tax_class = ? AND valid_from <= ?
		  AND (valid_to IS NULL OR valid_to >= ?)
		ORDER BY valid_from DESC LIMIT 1`, country, class, day, day).Scan(&bp)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoTaxRate
	}
	return bp, err
}

// TaxFromGross rechnet die enthaltene USt aus einem Bruttobetrag heraus.
// 12,00 € brutto bei 20 % → 2,00 € USt.
func TaxFromGross(gross Cents, rateBP int64) Cents {
	if rateBP == 0 {
		return 0
	}
	net := divRound(int64(gross)*10000, 10000+rateBP)
	return gross - Cents(net)
}

// TaxCountry bestimmt, nach welchem Land besteuert wird.
// Standard: Sitz des Shops. Mit OSS (EU-B2C-Fernverkauf über der Schwelle):
// Bestimmungsland, sofern EU. Für CH als Ziel gilt aus EU-Sicht Ausfuhr –
// das muss der Betreiber steuerlich klären; hier wird nicht automatisch 0 % gesetzt.
func TaxCountry(shopCountry, destCountry string, ossEnabled bool) string {
	if ossEnabled && isEU(destCountry) && isEU(shopCountry) {
		return destCountry
	}
	return shopCountry
}

func isEU(c string) bool {
	switch c {
	case "AT", "BE", "BG", "CY", "CZ", "DE", "DK", "EE", "ES", "FI", "FR", "GR", "HR", "HU",
		"IE", "IT", "LT", "LU", "LV", "MT", "NL", "PL", "PT", "RO", "SE", "SI", "SK":
		return true
	}
	return false
}
