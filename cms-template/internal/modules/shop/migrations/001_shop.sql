-- STYRIXA Shop – Schema v1 (SQLite, modernc.org/sqlite)
-- Geldbeträge: INTEGER in Cent. Steuersätze: INTEGER in Basispunkten (2000 = 20,00 %).
-- Preise werden BRUTTO gespeichert (B2C-Standard im DACH-Raum).

CREATE TABLE shop_products (
    id               INTEGER PRIMARY KEY,
    slug             TEXT NOT NULL UNIQUE,
    title            TEXT NOT NULL,
    summary          TEXT NOT NULL DEFAULT '',        -- Answer-First-Kurzbeschreibung (GEO)
    body_html        TEXT NOT NULL DEFAULT '',
    brand            TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    tax_class        TEXT NOT NULL DEFAULT 'standard', -- standard | reduced | reduced13 | food | zero
    product_type     TEXT NOT NULL DEFAULT 'physical' CHECK (product_type IN ('physical','digital','service')),
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE shop_variants (
    id                INTEGER PRIMARY KEY,
    product_id        INTEGER NOT NULL REFERENCES shop_products(id) ON DELETE CASCADE,
    sku               TEXT NOT NULL UNIQUE,
    gtin              TEXT NOT NULL DEFAULT '',         -- EAN, für Product-Schema
    name              TEXT NOT NULL DEFAULT '',         -- z. B. "0,75 l" oder "Größe M"
    price_cents       INTEGER NOT NULL CHECK (price_cents >= 0),
    compare_at_cents  INTEGER,                          -- Streichpreis (Preisangaben: 30-Tage-Tiefstpreis beachten)
    stock             INTEGER NOT NULL DEFAULT 0,
    track_stock       INTEGER NOT NULL DEFAULT 1,
    weight_g          INTEGER NOT NULL DEFAULT 0,
    content_amount    REAL,                             -- Grundpreis: Inhalt, z. B. 0.75
    content_unit      TEXT,                             -- Grundpreis: Einheit, z. B. 'l', 'kg'
    position          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_shop_variants_product ON shop_variants(product_id);

-- Preisverlauf für den 30-Tage-Tiefstpreis bei Rabatten (Omnibus-Richtlinie)
CREATE TABLE shop_price_history (
    variant_id   INTEGER NOT NULL REFERENCES shop_variants(id) ON DELETE CASCADE,
    price_cents  INTEGER NOT NULL,
    changed_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE shop_categories (
    id        INTEGER PRIMARY KEY,
    parent_id INTEGER REFERENCES shop_categories(id) ON DELETE SET NULL,
    slug      TEXT NOT NULL UNIQUE,
    title     TEXT NOT NULL,
    position  INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE shop_product_categories (
    product_id  INTEGER NOT NULL REFERENCES shop_products(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL REFERENCES shop_categories(id) ON DELETE CASCADE,
    PRIMARY KEY (product_id, category_id)
);

-- Verweis auf Medien aus mod_media (deshalb Requires: media)
CREATE TABLE shop_product_media (
    product_id INTEGER NOT NULL REFERENCES shop_products(id) ON DELETE CASCADE,
    media_id   INTEGER NOT NULL,
    position   INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (product_id, media_id)
);

-- Steuersätze mit Gültigkeit: Satzänderungen ohne neues Binary
CREATE TABLE shop_tax_rates (
    country     TEXT NOT NULL,          -- ISO 3166-1 alpha-2
    tax_class   TEXT NOT NULL,
    rate_bp     INTEGER NOT NULL,       -- Basispunkte
    valid_from  TEXT NOT NULL,          -- YYYY-MM-DD
    valid_to    TEXT,                   -- NULL = offen
    PRIMARY KEY (country, tax_class, valid_from)
);

-- Ausgangswerte (vom Betreiber zu prüfen; Stand Nov. 2026)
INSERT INTO shop_tax_rates (country, tax_class, rate_bp, valid_from) VALUES
    ('AT','standard', 2000, '2016-01-01'),
    ('AT','reduced',  1000, '2016-01-01'),
    ('AT','reduced13',1300, '2016-01-01'),
    ('AT','food',     1000, '2016-01-01'),
    ('DE','standard', 1900, '2021-01-01'),
    ('DE','reduced',   700, '2021-01-01'),
    ('DE','food',      700, '2021-01-01'),
    ('CH','standard',  810, '2024-01-01'),
    ('CH','reduced',   260, '2024-01-01'),
    ('CH','food',      260, '2024-01-01');
-- AT: 4,9 % für Grundnahrungsmittel nach Anlage 3 UStG ab 1.7.2026
UPDATE shop_tax_rates SET valid_to = '2026-06-30' WHERE country='AT' AND tax_class='food';
INSERT INTO shop_tax_rates (country, tax_class, rate_bp, valid_from) VALUES ('AT','food', 490, '2026-07-01');

CREATE TABLE shop_shipping_methods (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL,           -- "Österreichische Post", "DHL", "Abholung Graz"
    carrier          TEXT NOT NULL DEFAULT '',
    countries        TEXT NOT NULL DEFAULT 'AT', -- CSV: AT,DE,CH
    price_cents      INTEGER NOT NULL DEFAULT 0,
    free_from_cents  INTEGER,
    max_weight_g     INTEGER,
    tax_class        TEXT NOT NULL DEFAULT 'standard',
    active           INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE shop_coupons (
    code             TEXT PRIMARY KEY COLLATE NOCASE,
    kind             TEXT NOT NULL CHECK (kind IN ('percent','fixed','free_shipping')),
    value            INTEGER NOT NULL DEFAULT 0,  -- percent: Basispunkte, fixed: Cent
    min_order_cents  INTEGER NOT NULL DEFAULT 0,
    valid_from       TEXT,
    valid_to         TEXT,
    max_uses         INTEGER,
    used             INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE shop_orders (
    id               INTEGER PRIMARY KEY,
    number           TEXT NOT NULL UNIQUE,       -- z. B. STX-2026-000123
    status           TEXT NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','paid','processing','shipped','completed','cancelled','refunded')),
    customer_email   TEXT NOT NULL,
    customer_user_id INTEGER,                    -- gesetzt, wenn mod_users aktiv und Kunde eingeloggt
    billing_json     TEXT NOT NULL,
    shipping_json    TEXT NOT NULL,
    tax_country      TEXT NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'EUR',
    subtotal_cents   INTEGER NOT NULL,
    discount_cents   INTEGER NOT NULL DEFAULT 0,
    shipping_cents   INTEGER NOT NULL DEFAULT 0,
    tax_cents        INTEGER NOT NULL,
    total_cents      INTEGER NOT NULL,
    coupon_code      TEXT,
    shipping_method  TEXT,
    payment_provider TEXT NOT NULL,
    payment_ref      TEXT,
    payment_status   TEXT NOT NULL DEFAULT 'open',
    tracking_number  TEXT,
    note             TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at       TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_shop_orders_status ON shop_orders(status, created_at);

-- Positionen als Snapshot: spätere Preis-/Titeländerungen ändern alte Bestellungen nicht
CREATE TABLE shop_order_items (
    id               INTEGER PRIMARY KEY,
    order_id         INTEGER NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
    variant_id       INTEGER,
    sku              TEXT NOT NULL,
    title            TEXT NOT NULL,
    qty              INTEGER NOT NULL CHECK (qty > 0),
    unit_price_cents INTEGER NOT NULL,
    tax_rate_bp      INTEGER NOT NULL,
    line_total_cents INTEGER NOT NULL,
    line_tax_cents   INTEGER NOT NULL
);

-- Fortlaufende, lückenlose Rechnungsnummern
CREATE TABLE shop_invoice_counter (year INTEGER PRIMARY KEY, last INTEGER NOT NULL);
CREATE TABLE shop_invoices (
    id        INTEGER PRIMARY KEY,
    order_id  INTEGER NOT NULL UNIQUE REFERENCES shop_orders(id),
    number    TEXT NOT NULL UNIQUE,
    kind      TEXT NOT NULL DEFAULT 'invoice' CHECK (kind IN ('invoice','credit_note')),
    issued_at TEXT NOT NULL DEFAULT (datetime('now')),
    pdf_path  TEXT
);

CREATE TABLE shop_order_events (
    order_id   INTEGER NOT NULL REFERENCES shop_orders(id) ON DELETE CASCADE,
    at         TEXT NOT NULL DEFAULT (datetime('now')),
    kind       TEXT NOT NULL,   -- status_change, payment, note, email_sent
    data       TEXT NOT NULL DEFAULT ''
);
