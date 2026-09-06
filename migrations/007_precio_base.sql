-- +goose Up
-- Precio base calculado, a nivel de variante.
--
-- effective_prices guarda el precio POR CUENTA DE CANAL y exige una cuenta
-- existente. Pero el precio que sale de aplicar la tarifa de Odoo es anterior
-- a cualquier canal: existe aunque no haya ni una cuenta configurada, y es la
-- base sobre la que después cada canal aplica su ajuste.
--
-- Forzarlo dentro de effective_prices habría obligado a inventar una cuenta
-- ficticia. Es una propiedad de la variante y aquí es donde le corresponde.

ALTER TABLE product_variants
    ADD COLUMN computed_price     NUMERIC(16,4),
    ADD COLUMN price_source       TEXT,
    ADD COLUMN price_rule_id      BIGINT,
    ADD COLUMN price_explanation  TEXT,
    ADD COLUMN price_computed_at  TIMESTAMPTZ;

COMMENT ON COLUMN product_variants.computed_price IS
    'Precio resultante de aplicar las tarifas de Odoo sobre el coste. '
    'Nulo cuando ninguna regla aplica, que en MDV le ocurre a las categorías '
    'no cubiertas por las 4 reglas existentes.';

COMMENT ON COLUMN product_variants.price_explanation IS
    'Traza legible del cálculo, para poder justificar en la interfaz por qué '
    'un producto sale a un precio y no a otro.';

CREATE INDEX product_variants_con_precio_idx
    ON product_variants (id) WHERE computed_price IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS product_variants_con_precio_idx;
ALTER TABLE product_variants
    DROP COLUMN IF EXISTS price_computed_at,
    DROP COLUMN IF EXISTS price_explanation,
    DROP COLUMN IF EXISTS price_rule_id,
    DROP COLUMN IF EXISTS price_source,
    DROP COLUMN IF EXISTS computed_price;
