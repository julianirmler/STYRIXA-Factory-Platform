-- STYRIXA Shop – Zahlungen v2

-- Stripe-PaymentIntent (für Erstattungen und Zuordnung von charge.refunded)
ALTER TABLE shop_orders ADD COLUMN payment_intent TEXT;
CREATE INDEX idx_shop_orders_payment_intent ON shop_orders(payment_intent);

-- Idempotenz: Jedes Anbieter-Ereignis wird genau einmal verarbeitet
CREATE TABLE shop_payment_events (
    provider    TEXT NOT NULL,
    event_id    TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (provider, event_id)
);
