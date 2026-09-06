-- +goose Up
-- Campos que faltaban para completar la ficha comercial.
--
-- No son adornos: cada uno bloquea o degrada una publicación real.
--   · dimensiones → sin ellas Falabella y MercadoLibre calculan mal el envío,
--     porque cobran por peso volumétrico y no por el peso real.
--   · condición → MercadoLibre la exige; hoy el adaptador la manda fija en
--     "new", lo que sería mentir si algo es reacondicionado.
--   · garantía → obligatoria en muchas categorías de MercadoLibre y factor de
--     conversión en todas.
--   · vídeo → MercadoLibre lo admite y convierte notablemente mejor.

ALTER TABLE product_variants
    ADD COLUMN largo_cm  NUMERIC(10,2),
    ADD COLUMN ancho_cm  NUMERIC(10,2),
    ADD COLUMN alto_cm   NUMERIC(10,2);

COMMENT ON COLUMN product_variants.largo_cm IS
    'Dimensiones del paquete, no del producto: los canales cobran el envío '
    'por peso volumétrico (largo × ancho × alto / 5000).';

ALTER TABLE products
    ADD COLUMN condicion       TEXT NOT NULL DEFAULT 'nuevo'
        CHECK (condicion IN ('nuevo', 'usado', 'reacondicionado')),
    ADD COLUMN garantia_meses  INTEGER,
    ADD COLUMN garantia_tipo   TEXT
        CHECK (garantia_tipo IN ('fabricante', 'vendedor', 'sin_garantia')),
    ADD COLUMN video_url       TEXT,
    -- Nota interna: nunca sale a ningún canal. Es para el equipo.
    ADD COLUMN nota_interna    TEXT;

COMMENT ON COLUMN products.nota_interna IS
    'Anotación del equipo. NUNCA se publica en ningún canal.';

-- +goose Down
ALTER TABLE products
    DROP COLUMN IF EXISTS nota_interna,
    DROP COLUMN IF EXISTS video_url,
    DROP COLUMN IF EXISTS garantia_tipo,
    DROP COLUMN IF EXISTS garantia_meses,
    DROP COLUMN IF EXISTS condicion;
ALTER TABLE product_variants
    DROP COLUMN IF EXISTS alto_cm,
    DROP COLUMN IF EXISTS ancho_cm,
    DROP COLUMN IF EXISTS largo_cm;
