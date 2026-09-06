-- +goose Up
-- Cambio de propiedad de los datos: Odoo deja de ser la fuente de todo.
--
-- A partir de aquí, de Odoo solo se sincronizan SKU, nombre y stock. El resto
-- (precio, marca, descripción, categoría, exclusión, peso, barcode) es
-- propiedad de Integra: se edita en la interfaz y el sync nunca lo pisa.
--
-- Las columnas que venían de Odoo (cost, odoo_list_price, computed_price,
-- description_sale, categ_path…) se conservan como estaban: son el punto de
-- partida y la traza de dónde salió cada dato, pero dejan de actualizarse.

ALTER TABLE product_variants
    ADD COLUMN price            NUMERIC(16,4),
    ADD COLUMN price_updated_at TIMESTAMPTZ;

COMMENT ON COLUMN product_variants.price IS
    'PVP propiedad de Integra. Se edita en la interfaz y el sync no lo toca. '
    'Se sembró una única vez desde computed_price (tarifas de Odoo sobre coste).';

COMMENT ON COLUMN product_variants.computed_price IS
    'Sugerencia histórica calculada con las tarifas de Odoo. Congelada desde '
    'que el precio pasó a ser propiedad de Integra; solo sirve de referencia.';

-- Siembra única: el precio arranca con la sugerencia de tarifa ya calculada.
-- Desde aquí es un dato manual.
UPDATE product_variants
SET price = computed_price, price_updated_at = now()
WHERE computed_price IS NOT NULL;

COMMENT ON COLUMN products.excluded_reason IS
    'Exclusión del catálogo publicable, propiedad de Integra. Los valores '
    'históricos (archivado, no_vendible, categoria_no_comercial…) vienen de la '
    'clasificación automática antigua; desde la interfaz se marca "manual".';

-- +goose Down
ALTER TABLE product_variants
    DROP COLUMN IF EXISTS price_updated_at,
    DROP COLUMN IF EXISTS price;
