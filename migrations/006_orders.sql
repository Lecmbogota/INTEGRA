-- +goose Up
-- Órdenes que llegan de los canales y su creación en Odoo.

CREATE TYPE order_sync_status AS ENUM (
    'received',       -- leída del canal, aún no llevada a Odoo
    'mapped',         -- clientes y productos resueltos
    'created_in_odoo',
    'failed',
    'ignored'         -- cancelada o de prueba
);

CREATE TABLE channel_orders (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    external_order_id  TEXT        NOT NULL,
    external_number    TEXT,
    -- Estado tal como lo nombra el canal, sin normalizar.
    channel_status     TEXT,

    ordered_at         TIMESTAMPTZ NOT NULL,
    currency           TEXT        NOT NULL DEFAULT 'COP',
    total_amount       NUMERIC(16,4) NOT NULL,
    shipping_amount    NUMERIC(16,4) NOT NULL DEFAULT 0,
    tax_amount         NUMERIC(16,4) NOT NULL DEFAULT 0,

    buyer_name         TEXT,
    buyer_document     TEXT,
    buyer_email        TEXT,
    buyer_phone        TEXT,
    shipping_address   JSONB,

    -- Carga completa del canal, para poder reprocesar sin volver a pedirla.
    raw_payload        JSONB       NOT NULL,

    status             order_sync_status NOT NULL DEFAULT 'received',
    odoo_sale_order_id BIGINT,
    odoo_partner_id    BIGINT,
    sync_error         TEXT,
    sync_attempts      INTEGER     NOT NULL DEFAULT 0,
    synced_at          TIMESTAMPTZ,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Idempotencia de la ingesta: releer el mismo pedido no lo duplica.
    UNIQUE (channel_account_id, external_order_id)
);

CREATE INDEX channel_orders_pending_idx ON channel_orders (status, ordered_at)
    WHERE status IN ('received', 'mapped', 'failed');
CREATE INDEX channel_orders_recent_idx  ON channel_orders (ordered_at DESC);

CREATE TABLE channel_order_lines (
    id                  BIGSERIAL   PRIMARY KEY,
    channel_order_id    BIGINT      NOT NULL REFERENCES channel_orders(id) ON DELETE CASCADE,

    external_line_id    TEXT,
    external_variant_id TEXT,
    channel_sku         TEXT,

    -- Resuelta por SKU. Nula si el pedido trae un artículo que Integra no
    -- conoce, caso que va a la cola de atención en vez de fallar el pedido.
    variant_id          BIGINT      REFERENCES product_variants(id) ON DELETE SET NULL,

    title               TEXT,
    quantity            NUMERIC(16,4) NOT NULL,
    unit_price          NUMERIC(16,4) NOT NULL,
    total_price         NUMERIC(16,4) NOT NULL,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX channel_order_lines_order_idx   ON channel_order_lines (channel_order_id);
CREATE INDEX channel_order_lines_variant_idx ON channel_order_lines (variant_id);

-- Marca de agua de la ingesta por cuenta: desde cuándo pedir pedidos nuevos.
CREATE TABLE order_ingest_state (
    channel_account_id BIGINT      PRIMARY KEY REFERENCES channel_accounts(id) ON DELETE CASCADE,
    watermark          TIMESTAMPTZ,
    cursor             TEXT,
    last_run_at        TIMESTAMPTZ,
    last_error         TEXT,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS order_ingest_state;
DROP TABLE IF EXISTS channel_order_lines;
DROP TABLE IF EXISTS channel_orders;
DROP TYPE  IF EXISTS order_sync_status;
