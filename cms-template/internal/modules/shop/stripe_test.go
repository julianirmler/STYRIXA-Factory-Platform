package shop

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/julianirmler/styrixa-cms/internal/core"
)

const (
	testSK = "sk_test_51ABCdef"
	testWH = "whsec_testsecret"
)

var fixedNow = time.Unix(1764000000, 0)

func newTestStripe(t *testing.T, h http.HandlerFunc) *Stripe {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s, err := NewStripe(StripeConfig{SecretKey: testSK, WebhookSecret: testWH, BaseURL: srv.URL, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func sign(body string, ts int64, secret string) string {
	m := hmac.New(sha256.New, []byte(secret))
	fmt.Fprintf(m, "%d.%s", ts, body)
	return fmt.Sprintf("t=%d,v1=%s", ts, hex.EncodeToString(m.Sum(nil)))
}

func TestNewStripeRejectsBadKeys(t *testing.T) {
	if _, err := NewStripe(StripeConfig{SecretKey: "pk_test_x", WebhookSecret: testWH}); err == nil {
		t.Error("Publishable Key darf nicht akzeptiert werden")
	}
	if _, err := NewStripe(StripeConfig{SecretKey: testSK, WebhookSecret: "abc"}); err == nil {
		t.Error("ungültiges Webhook-Secret akzeptiert")
	}
}

func TestStartCreatesCheckoutSession(t *testing.T) {
	var calls atomic.Int32
	var keys []string
	s := newTestStripe(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if r.Method != "POST" || r.URL.Path != "/v1/checkout/sessions" {
			t.Errorf("unerwartet: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+testSK {
			t.Error("Authorization fehlt")
		}
		if n == 1 { // erster Versuch: Serverfehler → Wiederholung mit gleichem Key
			w.WriteHeader(500)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(body))
		want := map[string]string{
			"mode":                                   "payment",
			"client_reference_id":                    "STX-2026-000123",
			"metadata[order_number]":                 "STX-2026-000123",
			"line_items[0][price_data][currency]":    "eur",
			"line_items[0][price_data][unit_amount]": "3740",
			"line_items[0][quantity]":                "1",
			"customer_email":                         "kunde@example.at",
			"success_url":                            "https://kunde.at/shop/kasse/danke?bestellung=STX-2026-000123",
			"locale":                                 "de",
		}
		for k, v := range want {
			if f.Get(k) != v {
				t.Errorf("%s = %q, erwartet %q", k, f.Get(k), v)
			}
		}
		fmt.Fprint(w, `{"id":"cs_test_1","url":"https://checkout.stripe.com/c/pay/cs_test_1"}`)
	})
	redirect, ref, err := s.Start(context.Background(), OrderRef{
		Number: "STX-2026-000123", Total: 3740, Currency: "EUR", Email: "kunde@example.at",
		SuccessURL: "https://kunde.at/shop/kasse/danke?bestellung=STX-2026-000123", CancelURL: "https://kunde.at/shop/warenkorb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref != "cs_test_1" || !strings.HasPrefix(redirect, "https://checkout.stripe.com/") {
		t.Fatalf("ref=%s redirect=%s", ref, redirect)
	}
	if calls.Load() != 2 || keys[0] != "checkout-STX-2026-000123" || keys[0] != keys[1] {
		t.Fatalf("Wiederholung/Idempotenz falsch: %d Aufrufe, Keys %v", calls.Load(), keys)
	}
	if !s.TestMode() {
		t.Error("Testmodus nicht erkannt")
	}
}

func TestClientErrorIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	s := newTestStripe(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"parameter_invalid_integer","message":"Invalid integer"}}`)
	})
	_, _, err := s.Start(context.Background(), OrderRef{Number: "X", Total: 100})
	var se *StripeError
	if !errors.As(err, &se) || se.Status != 400 || se.Code != "parameter_invalid_integer" {
		t.Fatalf("StripeError erwartet, bekam %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("4xx wurde wiederholt (%d)", calls.Load())
	}
	if strings.Contains(err.Error(), testSK) {
		t.Fatal("Secret Key in Fehlermeldung")
	}
}

func TestRefund(t *testing.T) {
	s := newTestStripe(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f, _ := url.ParseQuery(string(body))
		if r.URL.Path != "/v1/refunds" || f.Get("payment_intent") != "pi_1" || f.Get("amount") != "500" ||
			r.Header.Get("Idempotency-Key") != "refund-STX-1-2" {
			t.Errorf("Refund-Request falsch: %s %v %s", r.URL.Path, f, r.Header.Get("Idempotency-Key"))
		}
		fmt.Fprint(w, `{"id":"re_1","status":"succeeded"}`)
	})
	id, err := s.Refund(context.Background(), "STX-1", "pi_1", 500, 2)
	if err != nil || id != "re_1" {
		t.Fatal(id, err)
	}
}

func TestVerifySignature(t *testing.T) {
	body := `{"id":"evt_1"}`
	ts := fixedNow.Unix()
	good := sign(body, ts, testWH)
	cases := []struct {
		name   string
		header string
		ok     bool
	}{
		{"gültig", good, true},
		{"mehrere v1, eine gültig (Secret-Rotation)", "t=" + fmt.Sprint(ts) + ",v1=deadbeef," + strings.Split(good, ",")[1], true},
		{"falsches Secret", sign(body, ts, "whsec_other"), false},
		{"veraltet (Replay)", sign(body, ts-600, testWH), false},
		{"aus der Zukunft", sign(body, ts+600, testWH), false},
		{"leer", "", false},
		{"ohne v1", fmt.Sprintf("t=%d", ts), false},
		{"nur v0", strings.Replace(good, "v1=", "v0=", 1), false},
	}
	for _, c := range cases {
		err := VerifySignature([]byte(body), c.header, testWH, fixedNow, stripeSigTolerance)
		if (err == nil) != c.ok {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
	if VerifySignature([]byte(body+" "), good, testWH, fixedNow, stripeSigTolerance) == nil {
		t.Error("veränderter Body akzeptiert")
	}
}

func webhookReq(body string, sig string) *http.Request {
	r := httptest.NewRequest("POST", "/shop/webhook/stripe", strings.NewReader(body))
	r.Header.Set("Stripe-Signature", sig)
	return r
}

func TestWebhookParsesEvents(t *testing.T) {
	s := newTestStripe(t, func(http.ResponseWriter, *http.Request) {})
	ts := fixedNow.Unix()
	cases := []struct {
		body   string
		status string
		check  func(*PaymentUpdate) bool
	}{
		{`{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","payment_status":"paid","payment_intent":"pi_1","client_reference_id":"STX-1","amount_total":3740,"currency":"eur"}}}`,
			PayPaid, func(u *PaymentUpdate) bool {
				return u.OrderNumber == "STX-1" && u.PaymentIntent == "pi_1" && u.PaymentRef == "cs_1" && u.Amount == 3740 && u.Currency == "EUR"
			}},
		{`{"id":"evt_2","type":"checkout.session.completed","data":{"object":{"id":"cs_1","payment_status":"unpaid","client_reference_id":"STX-1","amount_total":3740,"currency":"eur"}}}`,
			PayProcessing, nil},
		{`{"id":"evt_3","type":"checkout.session.async_payment_failed","data":{"object":{"id":"cs_1","client_reference_id":"STX-1"}}}`, PayFailed, nil},
		{`{"id":"evt_4","type":"checkout.session.expired","data":{"object":{"id":"cs_1","metadata":{"order_number":"STX-9"}}}}`,
			PayExpired, func(u *PaymentUpdate) bool { return u.OrderNumber == "STX-9" }},
		{`{"id":"evt_5","type":"charge.refunded","data":{"object":{"payment_intent":"pi_1","amount":3740,"amount_refunded":1000,"refunded":false,"currency":"eur"}}}`,
			PayPartiallyRefunded, func(u *PaymentUpdate) bool { return u.PaymentIntent == "pi_1" && u.Amount == 1000 }},
		{`{"id":"evt_6","type":"charge.refunded","data":{"object":{"payment_intent":"pi_1","amount":3740,"amount_refunded":3740,"refunded":true,"currency":"eur"}}}`, PayRefunded, nil},
		{`{"id":"evt_7","type":"customer.created","data":{"object":{}}}`, PayIgnored, nil},
	}
	for _, c := range cases {
		u, err := s.Webhook(webhookReq(c.body, sign(c.body, ts, testWH)))
		if err != nil {
			t.Fatalf("%s: %v", c.body[:40], err)
		}
		if u.Status != c.status || (c.check != nil && !c.check(u)) {
			t.Errorf("%s → %+v", c.body[:60], u)
		}
	}
	if _, err := s.Webhook(webhookReq(cases[0].body, sign(cases[0].body, ts, "whsec_fake"))); !errors.Is(err, ErrBadSignature) {
		t.Errorf("gefälschter Webhook akzeptiert: %v", err)
	}
}

func TestDecide(t *testing.T) {
	base := OrderPayment{Number: "STX-1", Status: "pending", PaymentStatus: PayOpen, Total: 3740, Currency: "EUR"}
	paid := PaymentUpdate{Status: PayPaid, Amount: 3740, Currency: "EUR"}
	with := func(f func(*OrderPayment)) OrderPayment { o := base; f(&o); return o }
	cases := []struct {
		name     string
		o        OrderPayment
		u        PaymentUpdate
		pay, ord string
		outcome  Outcome
	}{
		{"bezahlt", base, paid, PayPaid, "paid", OutcomeApplied},
		{"Betrag zu niedrig", base, PaymentUpdate{Status: PayPaid, Amount: 1, Currency: "EUR"}, PayOpen, "pending", OutcomeFlagged},
		{"falsche Währung", base, PaymentUpdate{Status: PayPaid, Amount: 3740, Currency: "CHF"}, PayOpen, "pending", OutcomeFlagged},
		{"doppelt bezahlt", with(func(o *OrderPayment) { o.PaymentStatus, o.Status = PayPaid, "paid" }), paid, PayPaid, "paid", OutcomeNoop},
		{"Versand bleibt", with(func(o *OrderPayment) { o.Status = "shipped" }), paid, PayPaid, "shipped", OutcomeApplied},
		{"storniert + bezahlt", with(func(o *OrderPayment) { o.Status = "cancelled" }), paid, PayPaid, "cancelled", OutcomeFlagged},
		{"asynchron", base, PaymentUpdate{Status: PayProcessing, Amount: 3740, Currency: "eur"}, PayProcessing, "pending", OutcomeApplied},
		{"Fehlschlag nach bezahlt ignoriert", with(func(o *OrderPayment) { o.PaymentStatus, o.Status = PayPaid, "paid" }), PaymentUpdate{Status: PayFailed}, PayPaid, "paid", OutcomeNoop},
		{"abgelaufen", base, PaymentUpdate{Status: PayExpired}, PayExpired, "pending", OutcomeApplied},
		{"Teilerstattung", with(func(o *OrderPayment) { o.PaymentStatus, o.Status = PayPaid, "paid" }), PaymentUpdate{Status: PayPartiallyRefunded, Amount: 1000}, PayPartiallyRefunded, "paid", OutcomeApplied},
		{"Vollerstattung", with(func(o *OrderPayment) { o.PaymentStatus, o.Status = PayPaid, "completed" }), PaymentUpdate{Status: PayRefunded}, PayRefunded, "refunded", OutcomeApplied},
		{"Teil nach Voll ignoriert", with(func(o *OrderPayment) { o.PaymentStatus, o.Status = PayRefunded, "refunded" }), PaymentUpdate{Status: PayPartiallyRefunded}, PayRefunded, "refunded", OutcomeNoop},
		{"unbekannt", base, PaymentUpdate{Status: PayIgnored}, PayOpen, "pending", OutcomeIgnored},
	}
	for _, c := range cases {
		d := decide(c.o, c.u)
		if d.PaymentStatus != c.pay || d.OrderStatus != c.ord || d.Outcome != c.outcome {
			t.Errorf("%s: %+v", c.name, d)
		}
	}
}

// fakeStore zählt angewendete Ereignisse.
type fakeStore struct {
	got []PaymentUpdate
	err error
}

func (f *fakeStore) ApplyPayment(_ context.Context, u PaymentUpdate) (Outcome, error) {
	f.got = append(f.got, u)
	return OutcomeApplied, f.err
}

func TestWebhookHandler(t *testing.T) {
	s := newTestStripe(t, func(http.ResponseWriter, *http.Request) {})
	registerProvider(s)
	t.Cleanup(func() { delete(providers, "stripe") })
	mux := http.NewServeMux()
	m := &module{}
	m.Mount(mux, core.Deps{})
	body := `{"id":"evt_1","type":"checkout.session.completed","data":{"object":{"id":"cs_1","payment_status":"paid","client_reference_id":"STX-1","amount_total":100,"currency":"eur"}}}`
	send := func(sig string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, webhookReq(body, sig))
		return rec.Code
	}
	if c := send(sign(body, fixedNow.Unix(), testWH)); c != 503 {
		t.Errorf("ohne Datenbank: %d (erwartet 503 → Stripe wiederholt)", c)
	}
	fs := &fakeStore{}
	m.store = fs
	if c := send("t=1,v1=00"); c != 400 {
		t.Errorf("falsche Signatur: %d", c)
	}
	if c := send(sign(body, fixedNow.Unix(), testWH)); c != 200 || len(fs.got) != 1 || fs.got[0].OrderNumber != "STX-1" {
		t.Errorf("gültig: %d %+v", c, fs.got)
	}
	fs.err = ErrOrderNotFound
	if c := send(sign(body, fixedNow.Unix(), testWH)); c != 200 {
		t.Errorf("unbekannte Bestellung soll quittiert werden: %d", c)
	}
	fs.err = errors.New("db weg")
	if c := send(sign(body, fixedNow.Unix(), testWH)); c != 500 {
		t.Errorf("DB-Fehler soll Wiederholung auslösen: %d", c)
	}
	if c := (func() int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("POST", "/shop/webhook/prepayment", nil))
		return rec.Code
	})(); c != 404 {
		t.Errorf("Vorkasse hat keinen Webhook: %d", c)
	}
}
