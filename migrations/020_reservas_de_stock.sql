-- +goose Up
-- El libro de unidades apartadas por una venta.
--
-- Entre que un canal vende y el siguiente sync con Odoo, los otros tres
-- canales siguen ofreciendo unidades que ya no existen. La ventana no dura
-- minutos: el pedido se crea en Odoo en borrador a propósito, así que Odoo
-- tampoco baja su stock hasta que una persona lo confirma y lo despacha.
--
-- El descuento se hace sobre variant_stock, que es de donde sale el stock
-- publicable, pero el sync reemplaza esa tabla entera en cada pasada. Por eso
-- lo apartado se asienta aquí: este libro es lo que permite volver a aplicarlo
-- después de cada sync y devolverlo exacto si el canal cancela.

CREATE TABLE order_stock_reservations (
    id                    BIGSERIAL PRIMARY KEY,
    channel_order_id      BIGINT    NOT NULL REFERENCES channel_orders(id) ON DELETE CASCADE,
    channel_order_line_id BIGINT    REFERENCES channel_order_lines(id) ON DELETE SET NULL,
    variant_id            BIGINT    NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    odoo_warehouse_id     BIGINT    NOT NULL REFERENCES odoo_warehouses(id) ON DELETE CASCADE,
    qty                   NUMERIC(16,4) NOT NULL CHECK (qty > 0),

    -- Vencimiento: la válvula de seguridad.
    --
    -- El cierre correcto de un asiento es cuando Odoo mueve de verdad esa
    -- unidad, es decir cuando alguien confirma y despacha el pedido. Integra
    -- crea el sale.order en borrador y hoy no sigue su estado posterior, así
    -- que no puede detectar ese momento. Sin vencimiento, el asiento seguiría
    -- restando para siempre POR ENCIMA del descuento que ya hizo Odoo, y el
    -- stock publicado se hundiría una unidad por venta, sin retorno y sin que
    -- nadie pudiera saber por qué.
    --
    -- Una semana es lo que se tarda como mucho en despachar un pedido de
    -- marketplace. Pasado ese plazo el asiento se cierra solo: si el pedido ya
    -- se despachó, Odoo ya refleja la baja; y si no, el stock vuelve a estar
    -- disponible, que es preferible a que se hunda para siempre.
    --
    -- PENDIENTE DE DECISIÓN: seguir el estado del sale.order en Odoo (o su
    -- albarán) y cerrar el asiento con ese hecho en vez de por reloj.
    expires_at            TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '7 days',

    released_at           TIMESTAMPTZ,
    released_reason       TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Las tres consultas del libro (reaplicar tras el sync, listar lo de un
-- pedido, cerrar al cancelar) miran solo los asientos abiertos.
CREATE INDEX order_stock_reservations_abiertas_idx
    ON order_stock_reservations (channel_order_id) WHERE released_at IS NULL;

-- La reaplicación agrupa por variante y bodega sobre lo abierto.
CREATE INDEX order_stock_reservations_variante_idx
    ON order_stock_reservations (variant_id, odoo_warehouse_id) WHERE released_at IS NULL;

-- Los vencidos, que hay que cerrar en cada pasada.
CREATE INDEX order_stock_reservations_vencidas_idx
    ON order_stock_reservations (expires_at) WHERE released_at IS NULL;

-- Una línea de pedido no puede apartar dos veces de la misma bodega: es la
-- red que impide contar doble si una ingesta se repite.
CREATE UNIQUE INDEX order_stock_reservations_linea_bodega_idx
    ON order_stock_reservations (channel_order_line_id, odoo_warehouse_id)
    WHERE channel_order_line_id IS NOT NULL AND released_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS order_stock_reservations;
