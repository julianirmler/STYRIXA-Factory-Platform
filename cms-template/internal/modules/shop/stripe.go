package shop

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Stripe-Anbindung direkt über die HTTPS-API (https://api.stripe.com/v1),
// ohne SDK. Ablauf:
//
//  1. Start: Checkout Session anlegen (Idempotency-Key = Bestellnummer)
//     → Kunde zahlt auf der Stripe-Seite (Karte, EPS, Klarna, Apple/Google Pay –
//     welche Methoden erscheinen, steuert das Stripe-Dashboard).
//  2. Webhook: Signatur prüfen (HMAC-SHA256, Zeitfenster), Ereignis übersetzen.
//     Nur der Webhook setzt eine Bestellung auf „bezahlt“ – nie der Rücksprung.
//  3. Erstattung: Refund über die API.

const (
	stripeAPI             = "https://api.stripe.com"
	stripeSigTolerance    = 5 * time.Minute
	stripeMaxWebhookBytes = 1 << 20
)

// StripeConfig – Schlüssel kommen aus den Shop-Einstellungen bzw. der Umgebung,
// niemals aus dem Code oder Logs.
type StripeConfig struct {
	SecretKey     string // sk_live_… / sk_test_… (oder eingeschränkter Schlüssel rk_…)
	WebhookSecret string // whsec_…
	APIVersion    string // optional, z. B. aus dem Stripe-Dashboard; leer = Kontostandard
	BaseURL       string // nur für Tests
	HTTPClient    *http.Client
	Now           func() time.Time // nur für Tests
}

type Stripe struct{ cfg StripeConfig }

func NewStripe(cfg StripeConfig) (*Stripe, error) {
	if !strings.HasPrefix(cfg.SecretKey, "sk_") && !strings.HasPrefix(cfg.SecretKey, "rk_") {
		return nil, errors.New("stripe: ungültiger Secret Key (erwartet sk_… oder rk_…)")
	}
	if !strings.HasPrefix(cfg.WebhookSecret, "whsec_") {
		return nil, errors.New("stripe: ungültiges Webhook-Secret (erwartet whsec_…)")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = stripeAPI
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Stripe{cfg: cfg}, nil
}

func (*Stripe) ID() string    { return "stripe" }
func (*Stripe) Label() string { return "Karte, EPS, Klarna, Apple Pay, Google Pay" }

// TestMode: mit Testschlüssel wird kein echtes Geld bewegt (im Admin anzeigen).
func (s *Stripe) TestMode() bool { return strings.Contains(s.cfg.SecretKey, "_test_") }

// StripeError ist eine Fehlerantwort der API.
type StripeError struct {
	Status  int
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *StripeError) Error() string {
	return fmt.Sprintf("stripe: HTTP %d %s %s: %s", e.Status, e.Type, e.Code, e.Message)
}

// call sendet einen form-kodierten Request. Netzwerkfehler und 5xx/429 werden mit
// demselben Idempotency-Key wiederholt – Stripe führt die Aktion dann nur einmal aus.
func (s *Stripe) call(ctx context.Context, method, path string, form url.Values, idemKey string, out any) error {
	body := form.Encode()
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt*attempt) * 500 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, s.cfg.BaseURL+path, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.cfg.SecretKey)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "STYRIXA-CMS/1 (+https://styrixa.at)")
		if idemKey != "" {
			req.Header.Set("Idempotency-Key", idemKey)
		}
		if s.cfg.APIVersion != "" {
			req.Header.Set("Stripe-Version", s.cfg.APIVersion)
		}
		resp, err := s.cfg.HTTPClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return json.Unmarshal(raw, out)
		}
		var env struct {
			Error StripeError `json:"error"`
		}
		_ = json.Unmarshal(raw, &env)
		env.Error.Status = resp.StatusCode
		last = &env.Error
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			return last // 4xx: Wiederholen bringt nichts
		}
	}
	return last
}

type stripeSession struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	PaymentStatus string `json:"payment_status"`
	PaymentIntent string `json:"payment_intent"`
	ClientRefID   string `json:"client_reference_id"`
	AmountTotal   int64  `json:"amount_total"`
	Currency      string `json:"currency"`
	Metadata      struct {
		OrderNumber string `json:"order_number"`
	} `json:"metadata"`
}

// Start legt eine Checkout Session an. Die Bestellung wird als eine Position mit
// dem Endbetrag übertragen: Positionen, Rabatte und USt sind im Shop bereits
// gerechnet, die Rechnung erstellt der Shop – so gibt es keine Rundungsdifferenzen.
func (s *Stripe) Start(ctx context.Context, o OrderRef) (string, string, error) {
	if o.Total <= 0 {
		return "", "", errors.New("stripe: Betrag muss größer 0 sein")
	}
	cur := strings.ToLower(o.Currency)
	if cur == "" {
		cur = "eur"
	}
	loc := o.Locale
	if loc == "" {
		loc = "de"
	}
	f := url.Values{}
	f.Set("mode", "payment")
	f.Set("locale", loc)
	f.Set("success_url", o.SuccessURL)
	f.Set("cancel_url", o.CancelURL)
	f.Set("client_reference_id", o.Number)
	f.Set("metadata[order_number]", o.Number)
	f.Set("payment_intent_data[metadata][order_number]", o.Number)
	f.Set("payment_intent_data[description]", "Bestellung "+o.Number)
	if o.Email != "" {
		f.Set("customer_email", o.Email)
	}
	f.Set("line_items[0][quantity]", "1")
	f.Set("line_items[0][price_data][currency]", cur)
	f.Set("line_items[0][price_data][unit_amount]", strconv.FormatInt(int64(o.Total), 10))
	f.Set("line_items[0][price_data][product_data][name]", "Bestellung "+o.Number)

	var sess stripeSession
	if err := s.call(ctx, http.MethodPost, "/v1/checkout/sessions", f, "checkout-"+o.Number, &sess); err != nil {
		return "", "", err
	}
	if sess.URL == "" || sess.ID == "" {
		return "", "", errors.New("stripe: Antwort ohne Checkout-URL")
	}
	return sess.URL, sess.ID, nil
}

// Refund erstattet amount (0 = Restbetrag) eines PaymentIntents.
// attempt unterscheidet mehrere Teil-Erstattungen derselben Bestellung (Idempotenz).
func (s *Stripe) Refund(ctx context.Context, orderNumber, paymentIntent string, amount Cents, attempt int) (string, error) {
	f := url.Values{}
	f.Set("payment_intent", paymentIntent)
	f.Set("metadata[order_number]", orderNumber)
	if amount > 0 {
		f.Set("amount", strconv.FormatInt(int64(amount), 10))
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	key := fmt.Sprintf("refund-%s-%d", orderNumber, attempt)
	if err := s.call(ctx, http.MethodPost, "/v1/refunds", f, key, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// ErrBadSignature: Webhook nicht von Stripe oder zu alt (Replay).
var ErrBadSignature = errors.New("stripe: ungültige Webhook-Signatur")

// VerifySignature prüft den Header Stripe-Signature: "t=<unix>,v1=<hex>[,v1=…]".
// Signiert ist "<t>.<body>" mit HMAC-SHA256 und dem Webhook-Secret.
func VerifySignature(body []byte, header, secret string, now time.Time, tolerance time.Duration) error {
	var ts int64
	var sigs [][]byte
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			if b, err := hex.DecodeString(v); err == nil {
				sigs = append(sigs, b)
			}
		}
	}
	if ts == 0 || len(sigs) == 0 {
		return ErrBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > tolerance || d < -tolerance {
		return fmt.Errorf("%w: Zeitstempel außerhalb des Toleranzfensters", ErrBadSignature)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(ts, 10)))
	mac.Write([]byte("."))
	mac.Write(body)
	want := mac.Sum(nil)
	for _, s := range sigs {
		if hmac.Equal(s, want) {
			return nil
		}
	}
	return ErrBadSignature
}

type stripeEvent struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Data struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

type stripeCharge struct {
	PaymentIntent  string `json:"payment_intent"`
	Amount         int64  `json:"amount"`
	AmountRefunded int64  `json:"amount_refunded"`
	Refunded       bool   `json:"refunded"`
	Currency       string `json:"currency"`
}

// Webhook liest, prüft und übersetzt ein Stripe-Ereignis.
func (s *Stripe) Webhook(r *http.Request) (*PaymentUpdate, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, stripeMaxWebhookBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > stripeMaxWebhookBytes {
		return nil, errors.New("stripe: Webhook zu groß")
	}
	if err := VerifySignature(body, r.Header.Get("Stripe-Signature"), s.cfg.WebhookSecret, s.cfg.Now(), stripeSigTolerance); err != nil {
		return nil, err
	}
	return parseStripeEvent(body)
}

func parseStripeEvent(body []byte) (*PaymentUpdate, error) {
	var ev stripeEvent
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&ev); err != nil {
		return nil, fmt.Errorf("stripe: Ereignis unlesbar: %w", err)
	}
	u := &PaymentUpdate{Provider: "stripe", EventID: ev.ID, EventType: ev.Type, Status: PayIgnored}

	switch ev.Type {
	case "checkout.session.completed",
		"checkout.session.async_payment_succeeded",
		"checkout.session.async_payment_failed",
		"checkout.session.expired":
		var cs stripeSession
		if err := json.Unmarshal(ev.Data.Object, &cs); err != nil {
			return nil, err
		}
		u.OrderNumber = cs.ClientRefID
		if u.OrderNumber == "" {
			u.OrderNumber = cs.Metadata.OrderNumber
		}
		u.PaymentRef, u.PaymentIntent = cs.ID, cs.PaymentIntent
		u.Amount, u.Currency = Cents(cs.AmountTotal), strings.ToUpper(cs.Currency)
		switch ev.Type {
		case "checkout.session.completed":
			switch cs.PaymentStatus {
			case "paid", "no_payment_required":
				u.Status = PayPaid
			case "unpaid":
				u.Status = PayProcessing // asynchrone Methode, Ergebnis folgt
			}
		case "checkout.session.async_payment_succeeded":
			u.Status = PayPaid
		case "checkout.session.async_payment_failed":
			u.Status = PayFailed
		case "checkout.session.expired":
			u.Status = PayExpired
		}
	case "charge.refunded":
		var ch stripeCharge
		if err := json.Unmarshal(ev.Data.Object, &ch); err != nil {
			return nil, err
		}
		u.PaymentIntent = ch.PaymentIntent
		u.Amount, u.Currency = Cents(ch.AmountRefunded), strings.ToUpper(ch.Currency)
		if ch.Refunded || ch.AmountRefunded >= ch.Amount {
			u.Status = PayRefunded
		} else {
			u.Status = PayPartiallyRefunded
		}
	}
	return u, nil
}
