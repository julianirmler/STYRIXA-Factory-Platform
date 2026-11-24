package shop

import "testing"

func TestTaxFromGross(t *testing.T) {
	cases := []struct {
		gross Cents
		bp    int64
		want  Cents
	}{
		{1200, 2000, 200}, // 12,00 € @20 % → 2,00 €
		{1100, 1000, 100}, // 11,00 € @10 % → 1,00 €
		{1049, 490, 49},   // 10,49 € @4,9 % → 0,49 €
		{1190, 1900, 190}, // DE 19 %
		{500, 0, 0},
	}
	for _, c := range cases {
		if got := TaxFromGross(c.gross, c.bp); got != c.want {
			t.Errorf("TaxFromGross(%d,%d)=%d, want %d", c.gross, c.bp, got, c.want)
		}
	}
}

func TestCalculateMixedRatesWithCoupon(t *testing.T) {
	lines := []Line{
		{SKU: "WEIN-075", Qty: 2, UnitGross: 1490, RateBP: 2000}, // 29,80 € @20 %
		{SKU: "BROT-01", Qty: 1, UnitGross: 520, RateBP: 490},    //  5,20 € @4,9 %
	}
	tot := Calculate(lines, 590, 2000, &Coupon{Kind: "percent", Value: 1000}) // 10 %
	if tot.Subtotal != 3500 || tot.Discount != 350 || tot.Total != 3740 {
		t.Fatalf("unexpected totals %+v", tot)
	}
	var sum Cents
	for _, v := range tot.TaxByBP {
		sum += v
	}
	if sum != tot.Tax {
		t.Fatalf("tax split %v != %d", tot.TaxByBP, tot.Tax)
	}
}

func TestFormat(t *testing.T) {
	if s := Cents(123456).Format("EUR", "de-AT"); s != "€ 1.234,56" {
		t.Error(s)
	}
	if s := Cents(123456).Format("CHF", "de-CH"); s != "CHF 1'234.56" {
		t.Error(s)
	}
	if s := Cents(990).Format("EUR", "de-DE"); s != "9,90 €" {
		t.Error(s)
	}
}
