-- +goose Up
-- «Retirar de la venta» a mano deja la publicación pausada con motivo
-- «manual», distinto de la pausa por catálogo (que Planificar reabre sola) y
-- de la retirada por el canal. El código lo escribía desde hace días, pero la
-- restricción de la columna seguía admitiendo solo los dos primeros: cada
-- retiro manual fallaba en la cola con «violates check constraint».
ALTER TABLE variant_channel_listings
    DROP CONSTRAINT IF EXISTS variant_channel_listings_pausada_motivo_check,
    ADD CONSTRAINT variant_channel_listings_pausada_motivo_check
        CHECK (pausada_motivo IN ('catalogo', 'canal', 'manual'));

-- +goose Down
UPDATE variant_channel_listings SET pausada_motivo = 'catalogo' WHERE pausada_motivo = 'manual';
ALTER TABLE variant_channel_listings
    DROP CONSTRAINT IF EXISTS variant_channel_listings_pausada_motivo_check,
    ADD CONSTRAINT variant_channel_listings_pausada_motivo_check
        CHECK (pausada_motivo IN ('catalogo', 'canal'));
