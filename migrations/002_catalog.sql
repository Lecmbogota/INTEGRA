-- +goose Up
-- Catálogo normalizado leído de Odoo.

-- product.template de Odoo.
CREATE TABLE products (
    id                 BIGSERIAL   PRIMARY KEY,
    odoo_connection_id BIGINT      NOT NULL REFERENCES odoo_connections(id) ON DELETE CASCADE,
    odoo_template_id   BIGINT      NOT NULL,

    name               TEXT        NOT NULL,
    description_sale   TEXT,
    -- Marca resuelta vía brand_aliases. Nula cuando el literal de Odoo no
    -- corresponde a ninguna marca conocida (o viene vacío, como en 199 de
    -- los 632 productos de MDV).
    brand_id           BIGINT      REFERENCES brands(id) ON DELETE SET NULL,
    brand_raw          TEXT,

    odoo_categ_id      BIGINT,
    categ_path         TEXT,

    sale_ok            BOOLEAN     NOT NULL DEFAULT TRUE,
    is_storable        BOOLEAN     NOT NULL DEFAULT TRUE,
    active             BOOLEAN     NOT NULL DEFAULT TRUE,

    odoo_write_date    TIMESTAMPTZ,
    synced_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (odoo_connection_id, odoo_template_id)
);

CREATE INDEX products_brand_idx      ON products (brand_id);
CREATE INDEX products_write_date_idx ON products (odoo_write_date DESC);
CREATE INDEX products_publishable_idx ON products (odoo_connection_id)
    WHERE active AND sale_ok;

-- product.product de Odoo.
--
-- Hoy el catálogo de MDV es 1 plantilla = 1 variante, pero la separación se
-- mantiene: añadirla después obligaría a migrar todas las publicaciones ya
-- creadas en los canales.
CREATE TABLE product_variants (
    id               BIGSERIAL   PRIMARY KEY,
    product_id       BIGINT      NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    odoo_product_id  BIGINT      NOT NULL,

    sku              TEXT,
    barcode          TEXT,
    -- Valores de atributo (talla, color…) tal como los da Odoo.
    attributes       JSONB       NOT NULL DEFAULT '{}'::jsonb,

    -- Coste en la moneda de la compañía (COP en MDV). Es la base real del
    -- cálculo de precio: las tarifas de Odoo se computan sobre standard_price.
    cost             NUMERIC(16,4),
    -- list_price de Odoo. Se guarda por trazabilidad, NO para publicar:
    -- en MDV el 58% vale 1,00 y el resto está en USD sin convertir.
    odoo_list_price  NUMERIC(16,4),

    weight           NUMERIC(12,4),
    volume           NUMERIC(12,4),
    uom              TEXT,

    active           BOOLEAN     NOT NULL DEFAULT TRUE,
    odoo_write_date  TIMESTAMPTZ,
    synced_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (product_id, odoo_product_id)
);

CREATE INDEX product_variants_sku_idx     ON product_variants (sku) WHERE sku IS NOT NULL;
CREATE INDEX product_variants_barcode_idx ON product_variants (barcode) WHERE barcode IS NOT NULL;
CREATE INDEX product_variants_product_idx ON product_variants (product_id);

COMMENT ON COLUMN product_variants.odoo_list_price IS
    'list_price de Odoo, solo para auditoría. El precio publicable se calcula '
    'en effective_prices a partir de cost y de las reglas de tarifa.';

-- Stock desglosado por almacén.
--
-- No se guarda un único qty_available porque ese campo suma todos los
-- almacenes internos, incluidos Muestras y Garantías, y el stock consignado
-- en las tres bodegas de Falabella. Guardándolo desglosado, cada cuenta puede
-- sumar los almacenes que le correspondan.
CREATE TABLE variant_stock (
    variant_id        BIGINT      NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    odoo_warehouse_id BIGINT      NOT NULL REFERENCES odoo_warehouses(id)  ON DELETE CASCADE,

    qty_on_hand       NUMERIC(16,4) NOT NULL DEFAULT 0,
    qty_forecast      NUMERIC(16,4) NOT NULL DEFAULT 0,
    qty_free          NUMERIC(16,4) NOT NULL DEFAULT 0,

    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (variant_id, odoo_warehouse_id)
);

CREATE INDEX variant_stock_warehouse_idx ON variant_stock (odoo_warehouse_id);

COMMENT ON TABLE variant_stock IS
    'Stock por variante y almacén. La cantidad publicada en una cuenta es la '
    'suma de sus almacenes menos el stock_buffer de la cuenta.';

CREATE TABLE product_images (
    id          BIGSERIAL   PRIMARY KEY,
    product_id  BIGINT      NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    position    INTEGER     NOT NULL DEFAULT 0,
    -- Hash del contenido: evita reenviar al canal una imagen que no cambió.
    content_hash TEXT       NOT NULL,
    mime_type   TEXT,
    bytes       INTEGER,
    -- URL pública una vez subida al canal, o ruta en almacenamiento propio.
    url         TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (product_id, position)
);

-- +goose Down
DROP TABLE IF EXISTS product_images;
DROP TABLE IF EXISTS variant_stock;
DROP TABLE IF EXISTS product_variants;
DROP TABLE IF EXISTS products;
