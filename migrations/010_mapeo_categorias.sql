-- +goose Up
-- El mapeo de categorías pasa a ser por CANAL, no por cuenta.
--
-- La categoría "Almacenamiento / Disco / SSD" corresponde a la misma categoría
-- de MercadoLibre en las cuatro marcas de MDV. Mapearla por cuenta obligaría a
-- repetir cada decisión cuatro veces, convirtiendo 39 mapeos en 156 sin ganar
-- nada. La granularidad por cuenta se conserva como excepción para el caso raro
-- en que una marca necesite una categoría distinta.

ALTER TABLE category_mappings
    ADD COLUMN channel_id BIGINT REFERENCES channels(id) ON DELETE CASCADE,
    -- Sugerencia automática pendiente de confirmar por una persona.
    ADD COLUMN sugerido_por TEXT,
    ADD COLUMN confianza NUMERIC(4,3),
    ADD COLUMN atributos_sugeridos JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN confirmado_at TIMESTAMPTZ,
    ADD COLUMN confirmado_por BIGINT REFERENCES users(id) ON DELETE SET NULL,
    ALTER COLUMN channel_account_id DROP NOT NULL;

-- La restricción antigua era (channel_account_id, odoo_categ_path) y no admite
-- filas con cuenta nula, que son justo las del mapeo por canal.
ALTER TABLE category_mappings
    DROP CONSTRAINT IF EXISTS category_mappings_channel_account_id_odoo_categ_path_key;

-- Un mapeo por canal para cada ruta de Odoo…
CREATE UNIQUE INDEX category_mappings_por_canal_idx
    ON category_mappings (channel_id, odoo_categ_path)
    WHERE channel_account_id IS NULL;

-- …y como mucho una excepción por cuenta.
CREATE UNIQUE INDEX category_mappings_por_cuenta_idx
    ON category_mappings (channel_account_id, odoo_categ_path)
    WHERE channel_account_id IS NOT NULL;

CREATE INDEX category_mappings_sin_confirmar_idx
    ON category_mappings (channel_id) WHERE confirmado_at IS NULL;

COMMENT ON COLUMN category_mappings.channel_account_id IS
    'Nulo = el mapeo aplica a todas las cuentas del canal. Con valor = '
    'excepción para esa cuenta concreta.';

COMMENT ON COLUMN category_mappings.sugerido_por IS
    'Origen de la sugerencia: "ml_predictor" para el predictor público de '
    'MercadoLibre, "manual" cuando la escribió una persona.';

COMMENT ON COLUMN category_mappings.confirmado_at IS
    'Hasta que no se confirma, la sugerencia no se usa para publicar. Una '
    'categoría equivocada en MercadoLibre arrastra historial y no se deshace '
    'borrando la publicación.';

-- +goose Down
DROP INDEX IF EXISTS category_mappings_sin_confirmar_idx;
DROP INDEX IF EXISTS category_mappings_por_cuenta_idx;
DROP INDEX IF EXISTS category_mappings_por_canal_idx;
ALTER TABLE category_mappings
    DROP COLUMN IF EXISTS confirmado_por,
    DROP COLUMN IF EXISTS confirmado_at,
    DROP COLUMN IF EXISTS atributos_sugeridos,
    DROP COLUMN IF EXISTS confianza,
    DROP COLUMN IF EXISTS sugerido_por,
    DROP COLUMN IF EXISTS channel_id;
