-- +goose Up
-- La verificación visual con IA se retira entera. Integra no juzga si una
-- foto muestra su producto: eso lo decide quien la sube, mirándola. Los
-- veredictos que quedaban se descartan con las columnas, y con ellos los
-- avisos «la portada no cuadra» que habían generado.
ALTER TABLE producto_imagenes
    DROP COLUMN IF EXISTS verificada_at,
    DROP COLUMN IF EXISTS verificacion_nota,
    DROP COLUMN IF EXISTS verificacion;

DELETE FROM attention_queue WHERE reason = 'image_mismatch';

-- +goose Down
ALTER TABLE producto_imagenes
    ADD COLUMN verificacion TEXT
        CHECK (verificacion IN ('corresponde', 'dudosa', 'no_corresponde')),
    ADD COLUMN verificacion_nota TEXT,
    ADD COLUMN verificada_at TIMESTAMPTZ;
