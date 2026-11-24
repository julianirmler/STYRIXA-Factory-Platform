package shop

// Line ist eine Warenkorb-Position mit bereits ermitteltem Steuersatz.
type Line struct {
	SKU       string
	Title     string
	Qty       int64
	UnitGross Cents
	RateBP    int64
}

// Coupon (vereinfachte Sicht auf shop_coupons).
type Coupon struct {
	Code          string
	Kind          string // percent | fixed | free_shipping
	Value         int64
	MinOrderCents Cents
}

// Totals ist das Ergebnis der Berechnung – wird 1:1 in shop_orders geschrieben.
type Totals struct {
	Subtotal Cents
	Discount Cents
	Shipping Cents
	Tax      Cents
	Total    Cents
	TaxByBP  map[int64]Cents // für die Rechnung: USt je Satz ausweisen
}

// Calculate berechnet Summen. Rabatte werden anteilig auf die Steuersätze
// verteilt, damit die ausgewiesene USt je Satz korrekt bleibt.
func Calculate(lines []Line, shipping Cents, shippingRateBP int64, c *Coupon) Totals {
	t := Totals{TaxByBP: map[int64]Cents{}}
	grossByBP := map[int64]Cents{}
	for _, l := range lines {
		g := l.UnitGross * Cents(l.Qty)
		t.Subtotal += g
		grossByBP[l.RateBP] += g
	}

	if c != nil && t.Subtotal >= c.MinOrderCents {
		switch c.Kind {
		case "percent":
			t.Discount = Cents(divRound(int64(t.Subtotal)*c.Value, 10000))
		case "fixed":
			t.Discount = min(Cents(c.Value), t.Subtotal)
		case "free_shipping":
			shipping = 0
		}
	}

	// Rabatt proportional auf Satz-Töpfe verteilen; Rundungsrest auf den größten Topf.
	if t.Discount > 0 && t.Subtotal > 0 {
		var given Cents
		var biggest int64
		for bp, g := range grossByBP {
			share := Cents(divRound(int64(t.Discount)*int64(g), int64(t.Subtotal)))
			grossByBP[bp] -= share
			given += share
			if g > grossByBP[biggest] {
				biggest = bp
			}
		}
		grossByBP[biggest] -= t.Discount - given
	}

	grossByBP[shippingRateBP] += shipping
	t.Shipping = shipping

	for bp, g := range grossByBP {
		tax := TaxFromGross(g, bp)
		t.TaxByBP[bp] = tax
		t.Tax += tax
	}
	t.Total = t.Subtotal - t.Discount + t.Shipping
	return t
}
