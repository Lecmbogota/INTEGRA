-- +goose Up
-- Marca de exclusión del catálogo publicable.
--
-- El maestro de Odoo mezcla mercancía con partidas contables: en MDV conviven
-- lectores de código de barras con "ARANCEL", "Arriendo Oficina Principal" y
-- "Aperol Spritz", todos como product.product porque el módulo de compras los
-- necesita para facturar.
--
-- No se filtran en la lectura sino que se marcan aquí. Descartarlos en silencio
-- escondería los casos dudosos: hay al menos un portátil real configurado como
-- no almacenable, y si no se sincronizara nadie se enteraría de que existe.

ALTER TABLE products
    ADD COLUMN excluded_reason TEXT;

COMMENT ON COLUMN products.excluded_reason IS
    'Nulo = mercancía publicable. Con valor = por qué queda fuera: '
    'archivado, no_vendible, no_almacenable, categoria_no_comercial.';

-- El índice parcial cubre la consulta habitual del catálogo, que es la que
-- pide solo lo publicable.
CREATE INDEX products_publicables_idx
    ON products (odoo_connection_id, brand_id)
    WHERE excluded_reason IS NULL AND active;

CREATE INDEX products_excluidos_idx
    ON products (excluded_reason) WHERE excluded_reason IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS products_excluidos_idx;
DROP INDEX IF EXISTS products_publicables_idx;
ALTER TABLE products DROP COLUMN IF EXISTS excluded_reason;
