-- +goose Up
-- Núcleo: marcas, canales, conexiones a Odoo y cuentas por canal.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------------------------------------------------------------- marcas

CREATE TABLE brands (
    id         BIGSERIAL   PRIMARY KEY,
    code       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    active     BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE brands IS
    'Marcas comerciales de MDV con identidad propia en los canales.';

-- El campo l10n_co_edi_brand de Odoo llega sucio: RingConn, Ringconn y
-- RINGCONN son la misma marca, y hay valores basura como el SKU "SPR-02".
-- Esta tabla traduce cada literal encontrado a una marca canónica, sin tener
-- que tocar los datos de Odoo.
CREATE TABLE brand_aliases (
    id            BIGSERIAL   PRIMARY KEY,
    brand_id      BIGINT      NOT NULL REFERENCES brands(id) ON DELETE CASCADE,
    -- Literal ya normalizado: minúsculas y sin espacios en los extremos.
    alias_norm    TEXT        NOT NULL UNIQUE,
    -- El literal tal cual apareció en Odoo, para poder auditar.
    alias_raw     TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX brand_aliases_brand_idx ON brand_aliases (brand_id);

COMMENT ON TABLE brand_aliases IS
    'Alias de marca encontrados en Odoo (l10n_co_edi_brand) y su marca canónica.';

-- ---------------------------------------------------------------- canales

CREATE TABLE channels (
    id         BIGSERIAL PRIMARY KEY,
    code       TEXT      NOT NULL UNIQUE,
    name       TEXT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO channels (code, name) VALUES
    ('mercadolibre', 'MercadoLibre'),
    ('falabella',    'Falabella Seller Center'),
    ('woocommerce',  'WooCommerce'),
    ('shopify',      'Shopify');

-- ---------------------------------------------------------- conexión a Odoo

CREATE TABLE odoo_connections (
    id            BIGSERIAL   PRIMARY KEY,
    name          TEXT        NOT NULL,
    base_url      TEXT        NOT NULL,
    database      TEXT        NOT NULL,
    username      TEXT        NOT NULL,
    -- Cifrado con AES-256-GCM. Nunca en claro, nunca en logs.
    api_key_enc   BYTEA       NOT NULL,
    -- Estrategia de lectura: de qué campo sale la marca, qué categorías se
    -- excluyen, qué tarifa usar. Se configura sin recompilar porque los
    -- nombres de campo dependen de la versión y de los módulos instalados.
    field_map     JSONB       NOT NULL DEFAULT '{}'::jsonb,
    timezone      TEXT        NOT NULL DEFAULT 'America/Bogota',
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    last_sync_at  TIMESTAMPTZ,
    -- Marca de agua de la lectura incremental por write_date.
    watermark     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (base_url, database)
);

COMMENT ON COLUMN odoo_connections.field_map IS
    'Configuración de lectura. Ejemplo real de MDV: '
    '{"brand": {"strategy": "field", "field": "l10n_co_edi_brand"}, '
    '"price": {"strategy": "pricelist", "pricelist_id": 1}, '
    '"exclude_categories": ["Activos Fijos", "All ca / Gastos"]}';

COMMENT ON COLUMN odoo_connections.watermark IS
    'Mayor write_date leído. La siguiente lectura arranca aquí en vez de '
    'traerse el catálogo entero.';

-- Almacenes de Odoo, replicados para poder decidir qué ve cada canal.
CREATE TABLE odoo_warehouses (
    id                  BIGSERIAL   PRIMARY KEY,
    odoo_connection_id  BIGINT      NOT NULL REFERENCES odoo_connections(id) ON DELETE CASCADE,
    odoo_id             BIGINT      NOT NULL,
    code                TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    lot_stock_id        BIGINT,
    active              BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (odoo_connection_id, odoo_id)
);

-- ------------------------------------------------------- cuentas por canal

CREATE TABLE channel_accounts (
    id            BIGSERIAL   PRIMARY KEY,
    brand_id      BIGINT      NOT NULL REFERENCES brands(id)   ON DELETE RESTRICT,
    channel_id    BIGINT      NOT NULL REFERENCES channels(id) ON DELETE RESTRICT,
    name          TEXT        NOT NULL,

    -- Credenciales y tokens OAuth, cifrados en reposo.
    credentials_enc BYTEA     NOT NULL,
    token_expires_at TIMESTAMPTZ,

    -- Throttling por cuenta, nunca global: agotar el cupo de una cuenta no
    -- debe frenar a las otras quince.
    rate_limit_rps  NUMERIC(6,2) NOT NULL DEFAULT 2.0,
    rate_limit_burst INTEGER     NOT NULL DEFAULT 5,

    -- Reserva de stock: unidades que no se publican para no sobrevender.
    stock_buffer    INTEGER     NOT NULL DEFAULT 0,
    -- Si el stock publicable llega a 0, ¿se pausa la publicación?
    pause_on_zero   BOOLEAN     NOT NULL DEFAULT TRUE,

    config        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    last_sync_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (brand_id, channel_id)
);

CREATE INDEX channel_accounts_channel_idx ON channel_accounts (channel_id) WHERE active;

COMMENT ON TABLE channel_accounts IS
    'Una fila por combinación marca × canal. Con 4 marcas y 4 canales, 16 filas.';

-- Qué almacenes alimentan el stock publicado en esta cuenta.
--
-- Sin filas para una cuenta = todos los almacenes, que es la decisión vigente
-- de MDV. Existe la tabla para poder restringir Falabella a sus tres bodegas
-- FB sin cambiar código, si la sobreventa lo acaba exigiendo.
CREATE TABLE channel_account_warehouses (
    channel_account_id BIGINT NOT NULL REFERENCES channel_accounts(id) ON DELETE CASCADE,
    odoo_warehouse_id  BIGINT NOT NULL REFERENCES odoo_warehouses(id)  ON DELETE CASCADE,

    PRIMARY KEY (channel_account_id, odoo_warehouse_id)
);

COMMENT ON TABLE channel_account_warehouses IS
    'Almacenes que alimentan cada cuenta. Sin filas = todos los almacenes.';

-- ---------------------------------------------------------------- usuarios

CREATE TABLE users (
    id            BIGSERIAL   PRIMARY KEY,
    email         TEXT        NOT NULL UNIQUE,
    name          TEXT        NOT NULL,
    password_hash TEXT        NOT NULL,
    role          TEXT        NOT NULL DEFAULT 'operator'
                  CHECK (role IN ('admin', 'operator', 'viewer')),
    active        BOOLEAN     NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS channel_account_warehouses;
DROP TABLE IF EXISTS channel_accounts;
DROP TABLE IF EXISTS odoo_warehouses;
DROP TABLE IF EXISTS odoo_connections;
DROP TABLE IF EXISTS channels;
DROP TABLE IF EXISTS brand_aliases;
DROP TABLE IF EXISTS brands;
