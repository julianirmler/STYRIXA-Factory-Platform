package shop

import (
	"encoding/json"
	"strconv"
)

// ProductJSONLD erzeugt Schema.org Product/Offer für Produktseiten (SEO & GEO).
type ProductLD struct {
	Name, Description, SKU, GTIN, Brand, URL, Currency string
	Images                                             []string
	Price                                              Cents
	InStock                                            bool
}

func (p ProductLD) JSON() ([]byte, error) {
	avail := "https://schema.org/OutOfStock"
	if p.InStock {
		avail = "https://schema.org/InStock"
	}
	doc := map[string]any{
		"@context":    "https://schema.org",
		"@type":       "Product",
		"name":        p.Name,
		"description": p.Description,
		"sku":         p.SKU,
		"image":       p.Images,
		"offers": map[string]any{
			"@type":         "Offer",
			"url":           p.URL,
			"priceCurrency": p.Currency,
			"price":         decimal(p.Price),
			"availability":  avail,
		},
	}
	if p.GTIN != "" {
		doc["gtin"] = p.GTIN
	}
	if p.Brand != "" {
		doc["brand"] = map[string]string{"@type": "Brand", "name": p.Brand}
	}
	return json.Marshal(doc)
}

// decimal: Schema.org erwartet Punkt als Dezimaltrenner, z. B. "12.90".
func decimal(c Cents) string {
	return strconv.FormatInt(int64(c)/100, 10) + "." + pad2(int64(c)%100)
}
