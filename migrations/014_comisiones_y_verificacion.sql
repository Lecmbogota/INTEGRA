-- +goose Up
-- Comisiones por canal y verificación visual de imágenes.

-- Cada canal cobra su comisión y, en algunos casos, un costo fijo por venta.
-- Publicar el mismo precio en todos regala ese margen: el precio publicado se
-- calcula por canal como (precio + costo_fijo) / (1 - comision/100), de modo
-- que lo que queda tras la comisión sea el precio definido en Integra.
--
-- Arrancan en cero a propósito: las comisiones reales dependen de la
-- categoría y del contrato de cada cuenta, así que las fija una persona en la
-- interfaz en vez de venir inventadas de fábrica.
ALTER TABLE channels
    ADD COLUMN comision_pct NUMERIC(5,2) NOT NULL DEFAULT 0,
    ADD COLUMN costo_fijo   NUMERIC(16,4) NOT NULL DEFAULT 0;

COMMENT ON COLUMN channels.comision_pct IS
    'Comisión porcentual del canal sobre el precio publicado. La define el '
    'operador; el precio por canal la compensa para conservar el margen.';

-- Resultado de la verificación visual con IA: ¿la foto corresponde al
-- producto? Vive en la asociación producto-imagen (no en la imagen) porque la
-- misma foto puede ser correcta para un producto y equivocada en otro.
ALTER TABLE producto_imagenes
    ADD COLUMN verificacion TEXT
        CHECK (verificacion IN ('corresponde', 'dudosa', 'no_corresponde')),
    ADD COLUMN verificacion_nota TEXT,
    ADD COLUMN verificada_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE producto_imagenes
    DROP COLUMN IF EXISTS verificada_at,
    DROP COLUMN IF EXISTS verificacion_nota,
    DROP COLUMN IF EXISTS verificacion;
ALTER TABLE channels
    DROP COLUMN IF EXISTS costo_fijo,
    DROP COLUMN IF EXISTS comision_pct;
