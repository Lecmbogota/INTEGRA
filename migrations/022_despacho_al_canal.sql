-- +goose Up
-- Confirmar al canal que el pedido salió.
--
-- Hasta ahora AckOrder estaba implementado en los cuatro conectores y no lo
-- llamaba nadie: MDV despachaba y el marketplace nunca se enteraba. Mercado
-- Libre y Falabella miden el tiempo hasta el despacho y, pasado el plazo,
-- cancelan, reembolsan al comprador y bajan la reputación del vendedor, lo que
-- además reduce la exposición de todas las publicaciones.

ALTER TABLE channel_orders
    -- Cuándo se validó el albarán en Odoo. Es la señal de que salió de bodega,
    -- y se guarda aparte de la confirmación al canal porque son dos momentos
    -- distintos: entre uno y otro puede fallar la llamada al canal.
    ADD COLUMN odoo_done_at       TIMESTAMPTZ,

    -- Guía y transportadora. Son opcionales por diseño: en Mercado Envíos y en
    -- Falabella la logística la pone el canal y no hay guía que mandarle, así
    -- que exigirlas impediría confirmar justo los pedidos que más corren.
    ADD COLUMN tracking_number    TEXT,
    ADD COLUMN carrier            TEXT,

    -- Cuándo se confirmó al canal. Nulo significa «todavía no lo sabe», que es
    -- exactamente lo que había que poder ver.
    ADD COLUMN dispatched_at      TIMESTAMPTZ,
    ADD COLUMN dispatch_error     TEXT,
    ADD COLUMN dispatch_attempts  INTEGER NOT NULL DEFAULT 0;

-- Los candidatos a confirmar: montados en Odoo y todavía sin avisar al canal.
-- El índice parcial los deja en un puñado de filas aunque la tabla crezca a
-- cientos de miles, porque un pedido solo está aquí hasta que se confirma.
CREATE INDEX channel_orders_por_despachar_idx
    ON channel_orders (odoo_sale_order_id)
    WHERE odoo_sale_order_id IS NOT NULL AND dispatched_at IS NULL;

COMMENT ON COLUMN channel_orders.dispatched_at IS
    'Cuándo se confirmó el despacho AL CANAL. No es cuándo salió de bodega: '
    'eso es odoo_done_at.';

-- +goose Down
DROP INDEX IF EXISTS channel_orders_por_despachar_idx;
ALTER TABLE channel_orders
    DROP COLUMN IF EXISTS odoo_done_at,
    DROP COLUMN IF EXISTS tracking_number,
    DROP COLUMN IF EXISTS carrier,
    DROP COLUMN IF EXISTS dispatched_at,
    DROP COLUMN IF EXISTS dispatch_error,
    DROP COLUMN IF EXISTS dispatch_attempts;
