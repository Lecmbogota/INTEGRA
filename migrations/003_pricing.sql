-- +goose Up
-- Precios, ofertas y el motor de tarifas replicado desde Odoo.

-- Reglas de tarifa leídas de product.pricelist.item.
--
-- Odoo bloquea por RPC los métodos que empiezan por guion bajo, así que
-- _get_products_price no es invocable y no se puede pedir el precio ya
-- calculado. Se replican las reglas y se computan aquí.
CREATE TABLE odoo_pricelists (
    id                 BIGSERIAL   PRIMARY KEY,
    odoo_connection_id BIGINT      NOT NULL REFERENCES odoo_connections(id) ON DELETE CASCADE,
    odoo_id            BIGINT      NOT NULL,
    name               TEXT        NOT NULL,
    currency           TEXT        NOT NULL,
    synced_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (odoo_connection_id, odoo_id)
);

CREATE TABLE odoo_pricelist_items (
    id            BIGSERIAL   PRIMARY KEY,
    pricelist_id  BIGINT      NOT NULL REFERENCES odoo_pricelists(id) ON DELETE CASCADE,
    odoo_id       BIGINT      NOT NULL,

    -- '3_global' | '2_product_category' | '1_product' | '0_product_variant'
    applied_on    TEXT        NOT NULL,
    -- 'fixed' | 'percentage' | 'formula'
    compute_price TEXT        NOT NULL,
    -- 'list_price' | 'standard_price' | 'pricelist'
    base          TEXT,

    odoo_categ_id       BIGINT,
    odoo_template_id    BIGINT,
    odoo_product_id     BIGINT,
    base_pricelist_id   BIGINT,

    fixed_price     NUMERIC(16,4),
    percent_price   NUMERIC(10,4),
    -- En Odoo un descuento negativo es un margen: -25 significa costo × 1,25.
    price_discount  NUMERIC(10,4) NOT NULL DEFAULT 0,
    price_surcharge NUMERIC(16,4) NOT NULL DEFAULT 0,
    price_round     NUMERIC(16,4),
    price_min_margin NUMERIC(16,4),
    price_max_margin NUMERIC(16,4),
    min_quantity    NUMERIC(16,4) NOT NULL DEFAULT 0,

    date_start    TIMESTAMPTZ,
    date_end      TIMESTAMPTZ,
    synced_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (pricelist_id, odoo_id)
);

-- El orden de evaluación de Odoo va de lo más específico a lo más general.
CREATE INDEX pricelist_items_lookup_idx
    ON odoo_pricelist_items (pricelist_id, applied_on, odoo_product_id, odoo_template_id, odoo_categ_id);

COMMENT ON COLUMN odoo_pricelist_items.price_discount IS
    'Descuento en porcentaje. Negativo = margen. En MDV la tarifa '
    'Predeterminado usa -25 (costo × 1,25) y Dynabook -17,65 (costo × 1,1765).';

-- Ajuste por canal sobre el precio base: absorber comisión, redondear, etc.
CREATE TABLE channel_price_rules (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    -- Ámbito: nulo = toda la cuenta. Si hay marca o categoría, gana la regla
    -- más específica.
    brand_id           BIGINT      REFERENCES brands(id) ON DELETE CASCADE,
    categ_path_prefix  TEXT,

    -- 'percent' | 'fixed'
    adjustment_type    TEXT        NOT NULL CHECK (adjustment_type IN ('percent', 'fixed')),
    adjustment_value   NUMERIC(16,4) NOT NULL,
    -- Redondeo final, por ejemplo a 100 pesos.
    round_to           NUMERIC(16,4),
    -- Margen mínimo sobre coste. Si no se cumple, el producto va a la cola de
    -- atención en vez de publicarse.
    min_margin_percent NUMERIC(10,4),

    priority           INTEGER     NOT NULL DEFAULT 100,
    active             BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX channel_price_rules_account_idx
    ON channel_price_rules (channel_account_id, priority) WHERE active;

-- Precio fijado a mano para una variante en una cuenta. Gana sobre todo lo demás.
CREATE TABLE price_overrides (
    id                 BIGSERIAL   PRIMARY KEY,
    variant_id         BIGINT      NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    price              NUMERIC(16,4) NOT NULL,
    reason             TEXT,
    created_by         BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (variant_id, channel_account_id)
);

-- Ofertas con vigencia.
--
-- Los canales no soportan lo mismo: WooCommerce y Falabella aceptan fechas de
-- inicio y fin nativas; Shopify usa compareAtPrice sin vigencia; MercadoLibre
-- no tiene precio "antes/después". Para los que no la soportan, el planificador
-- programa dos trabajos —aplicar y revertir— a partir de estas fechas.
CREATE TABLE offers (
    id                 BIGSERIAL   PRIMARY KEY,
    variant_id         BIGINT      NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    offer_price        NUMERIC(16,4) NOT NULL,
    starts_at          TIMESTAMPTZ NOT NULL,
    ends_at            TIMESTAMPTZ,

    -- Ciclo de vida: la oferta se aplica y luego se revierte.
    applied_at         TIMESTAMPTZ,
    reverted_at        TIMESTAMPTZ,
    active             BOOLEAN     NOT NULL DEFAULT TRUE,

    created_by         BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (ends_at IS NULL OR ends_at > starts_at)
);

CREATE INDEX offers_pending_idx ON offers (starts_at)
    WHERE active AND applied_at IS NULL;
CREATE INDEX offers_expiring_idx ON offers (ends_at)
    WHERE active AND applied_at IS NOT NULL AND reverted_at IS NULL;

-- Precio ya resuelto por variante y cuenta. Es lo que se publica.
CREATE TABLE effective_prices (
    variant_id         BIGINT      NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    -- PVP tachado cuando hay oferta.
    regular_price      NUMERIC(16,4) NOT NULL,
    -- Precio vigente: el de oferta si la hay, si no el regular.
    sale_price         NUMERIC(16,4),
    currency           TEXT        NOT NULL DEFAULT 'COP',

    -- De dónde salió, para poder explicarlo en la interfaz.
    source             TEXT        NOT NULL
                       CHECK (source IN ('pricelist', 'override', 'offer', 'list_price')),
    computed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (variant_id, channel_account_id)
);

-- +goose Down
DROP TABLE IF EXISTS effective_prices;
DROP TABLE IF EXISTS offers;
DROP TABLE IF EXISTS price_overrides;
DROP TABLE IF EXISTS channel_price_rules;
DROP TABLE IF EXISTS odoo_pricelist_items;
DROP TABLE IF EXISTS odoo_pricelists;
