-- +goose Up
-- Cuenta global por canal.
--
-- El esquema original exigía una cuenta por (marca, canal) — el modelo de 16
-- cuentas. La decisión operativa fue una sola cuenta por canal donde publican
-- todas las marcas: brand_id pasa a ser opcional y NULL significa "todas".
-- El modelo por marca sigue disponible si algún día se necesita.

ALTER TABLE channel_accounts ALTER COLUMN brand_id DROP NOT NULL;

-- Una sola cuenta global activa por canal. El UNIQUE(brand_id, channel_id)
-- original no cubre los NULL (son distintos entre sí), de ahí este parcial.
CREATE UNIQUE INDEX channel_accounts_global_por_canal_idx
    ON channel_accounts (channel_id) WHERE brand_id IS NULL AND active;

-- +goose Down
DROP INDEX IF EXISTS channel_accounts_global_por_canal_idx;
-- No se restaura el NOT NULL: podría haber filas con NULL ya creadas.
