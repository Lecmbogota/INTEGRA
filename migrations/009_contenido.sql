-- +goose Up
-- Contenido generado: títulos por canal y descripción.
--
-- Va en su propia tabla y no en columnas de products por dos razones: el
-- contenido tiene su propio ciclo de vida (generado → revisado → aprobado) que
-- no coincide con el de la sincronización, y una edición humana no debe
-- perderse porque alguien vuelva a lanzar el generador.

CREATE TABLE product_content (
    product_id   BIGINT      PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,

    -- Título por canal: {"mercadolibre": "…", "shopify": "…"}. Cada canal tiene
    -- su límite de caracteres, así que no puede haber un título único.
    titulos      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    descripcion  TEXT,
    -- Especificaciones extraídas del nombre, para poder mapearlas después a los
    -- atributos obligatorios que exige cada canal.
    specs        JSONB       NOT NULL DEFAULT '[]'::jsonb,

    confianza    TEXT        NOT NULL DEFAULT 'baja'
                 CHECK (confianza IN ('alta', 'media', 'baja')),
    avisos       JSONB       NOT NULL DEFAULT '[]'::jsonb,

    -- editado protege el trabajo humano: al regenerar se respetan estas filas.
    editado      BOOLEAN     NOT NULL DEFAULT FALSE,
    aprobado_at  TIMESTAMPTZ,
    aprobado_por BIGINT      REFERENCES users(id) ON DELETE SET NULL,

    generado_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- La cola de revisión: lo pendiente de aprobar, y primero lo más fiable
-- porque es lo que se despacha más rápido.
CREATE INDEX product_content_pendientes_idx
    ON product_content (confianza) WHERE aprobado_at IS NULL;

COMMENT ON COLUMN product_content.editado IS
    'Cierto cuando una persona tocó el texto. Regenerar no sobrescribe estas '
    'filas: perder una descripción escrita a mano sería inaceptable.';

COMMENT ON COLUMN product_content.confianza IS
    'alta: familia, marca y 2+ especificaciones. baja: el nombre de Odoo no '
    'da material suficiente y hace falta escribirlo a mano.';

-- +goose Down
DROP TABLE IF EXISTS product_content;
