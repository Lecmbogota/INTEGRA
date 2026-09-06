-- +goose Up
-- El suelo de coste: por debajo de aquí no se publica.
--
-- Hasta ahora el margen mínimo solo existía si alguien había creado una regla
-- de canal Y le había puesto min_margin_percent. Sin regla no había ninguna
-- comprobación: un coste que sube en Odoo, o un precio tecleado con un dígito
-- de menos, salía a los cuatro canales y se descubría al cuadrar el mes, con
-- la mercancía ya despachada.
--
-- El suelo pasa a ser de la cuenta, no de la regla, para que exista siempre.

-- 0 significa «que al menos cubra el coste», no «sin comprobación»: es el
-- único valor que no inventa un margen que el dueño no ha decidido y a la vez
-- impide la venta a pérdida. Subirlo al margen real de MDV es cambiar un
-- número en la pantalla de cuentas.
ALTER TABLE channel_accounts
    ADD COLUMN min_margen_pct NUMERIC(6,3) NOT NULL DEFAULT 0
        CHECK (min_margen_pct >= 0 AND min_margen_pct < 1000);

-- Bloquear es lo seguro para el negocio —el canal se queda con el precio
-- bueno que ya tenía publicado en vez de recibir el malo—, pero un coste mal
-- cargado en Odoo pararía el catálogo entero, así que hay interruptor: con
-- FALSE el aviso sigue saliendo y el envío no se detiene.
ALTER TABLE channel_accounts
    ADD COLUMN bloquear_bajo_costo BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE channel_accounts DROP COLUMN IF EXISTS bloquear_bajo_costo;
ALTER TABLE channel_accounts DROP COLUMN IF EXISTS min_margen_pct;
