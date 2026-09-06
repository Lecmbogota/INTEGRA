-- +goose Up
-- Publicaciones en los canales y mapeos de metadatos.

CREATE TYPE listing_status AS ENUM (
    'pending',    -- nunca publicado
    'draft',      -- creado en el canal pero sin activar
    'published',
    'paused',
    'error',
    'deleted'     -- desaparecido del canal
);

CREATE TABLE product_channel_listings (
    id                 BIGSERIAL      PRIMARY KEY,
    product_id         BIGINT         NOT NULL REFERENCES products(id)         ON DELETE CASCADE,
    channel_account_id BIGINT         NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    external_id        TEXT,
    external_url       TEXT,
    status             listing_status NOT NULL DEFAULT 'pending',

    -- Hash del contenido: título, descripción, imágenes, atributos, categoría.
    -- Separado de precio y stock para no reenviar la publicación entera cuando
    -- solo cambió una cantidad.
    content_hash       TEXT,

    last_published_at  TIMESTAMPTZ,
    last_error         TEXT,
    last_error_at      TIMESTAMPTZ,
    error_count        INTEGER        NOT NULL DEFAULT 0,

    created_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ    NOT NULL DEFAULT now(),

    UNIQUE (product_id, channel_account_id)
);

-- Un external_id no puede repetirse dentro de la misma cuenta.
CREATE UNIQUE INDEX product_listings_external_idx
    ON product_channel_listings (channel_account_id, external_id)
    WHERE external_id IS NOT NULL;

CREATE INDEX product_listings_status_idx
    ON product_channel_listings (channel_account_id, status);

CREATE TABLE variant_channel_listings (
    id                 BIGSERIAL      PRIMARY KEY,
    listing_id         BIGINT         NOT NULL REFERENCES product_channel_listings(id) ON DELETE CASCADE,
    variant_id         BIGINT         NOT NULL REFERENCES product_variants(id)         ON DELETE CASCADE,
    channel_account_id BIGINT         NOT NULL REFERENCES channel_accounts(id)         ON DELETE CASCADE,

    external_variant_id TEXT,
    channel_sku         TEXT,
    status              listing_status NOT NULL DEFAULT 'pending',

    -- Los dos hashes baratos. Cambiar stock toca solo el endpoint de stock.
    price_hash          TEXT,
    stock_hash          TEXT,

    published_price     NUMERIC(16,4),
    published_qty       INTEGER,

    last_price_push_at  TIMESTAMPTZ,
    last_stock_push_at  TIMESTAMPTZ,

    created_at          TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ    NOT NULL DEFAULT now(),

    UNIQUE (variant_id, channel_account_id)
);

CREATE INDEX variant_listings_listing_idx ON variant_channel_listings (listing_id);

COMMENT ON TABLE variant_channel_listings IS
    'Con 4 marcas × 4 canales sobre el catálogo actual son unos 10.000 registros; '
    'el diseño asume crecer a cientos de miles.';

-- ---------------------------------------------------------------- mapeos

CREATE TABLE category_mappings (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    -- Se mapea por ruta de categoría de Odoo, no por id: las rutas sobreviven
    -- a que alguien recree una categoría.
    odoo_categ_path    TEXT        NOT NULL,
    channel_category_id   TEXT     NOT NULL,
    channel_category_name TEXT,

    -- Valores por defecto de los atributos obligatorios de esa categoría.
    default_attributes JSONB       NOT NULL DEFAULT '{}'::jsonb,

    created_by         BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (channel_account_id, odoo_categ_path)
);

CREATE TABLE attribute_mappings (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_account_id BIGINT      NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,

    odoo_attribute     TEXT        NOT NULL,
    channel_attribute_id TEXT      NOT NULL,
    required           BOOLEAN     NOT NULL DEFAULT FALSE,
    default_value      TEXT,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (channel_account_id, odoo_attribute)
);

-- Talla "M" en Odoo puede ser MEDIUM en MercadoLibre y M en Falabella.
CREATE TABLE attribute_value_mappings (
    id                   BIGSERIAL PRIMARY KEY,
    attribute_mapping_id BIGINT    NOT NULL REFERENCES attribute_mappings(id) ON DELETE CASCADE,
    odoo_value           TEXT      NOT NULL,
    channel_value_id     TEXT      NOT NULL,
    channel_value_name   TEXT,

    UNIQUE (attribute_mapping_id, odoo_value)
);

-- Árbol de categorías del canal, cacheado para poder mapear sin pedirlo cada vez.
CREATE TABLE channel_category_cache (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_id         BIGINT      NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_category_id TEXT       NOT NULL,
    parent_id          TEXT,
    name               TEXT        NOT NULL,
    path               TEXT,
    is_leaf            BOOLEAN     NOT NULL DEFAULT FALSE,
    -- Atributos obligatorios de la categoría, tal como los declara el canal.
    required_attributes JSONB      NOT NULL DEFAULT '[]'::jsonb,
    refreshed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (channel_id, channel_category_id)
);

CREATE INDEX channel_category_cache_name_idx ON channel_category_cache (channel_id, name);

-- +goose Down
DROP TABLE IF EXISTS channel_category_cache;
DROP TABLE IF EXISTS attribute_value_mappings;
DROP TABLE IF EXISTS attribute_mappings;
DROP TABLE IF EXISTS category_mappings;
DROP TABLE IF EXISTS variant_channel_listings;
DROP TABLE IF EXISTS product_channel_listings;
DROP TYPE  IF EXISTS listing_status;
