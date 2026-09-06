-- +goose Up
-- Atributos obligatorios por categoría de canal.
--
-- MercadoLibre y Falabella rechazan una publicación si le faltan los
-- atributos que su categoría marca como obligatorios (marca, modelo,
-- capacidad…). Sin esto, sus adaptadores fallarían en el primer envío por
-- bien escritos que estén.
--
-- El esquema original traía `attribute_mappings`, que mapea un atributo de
-- Odoo a uno del canal. No sirve aquí: el catálogo de MDV no usa atributos de
-- Odoo (0 de 722 plantillas), y los requisitos no dependen de la cuenta sino
-- de la CATEGORÍA. Estas dos tablas modelan el problema real; la vieja se
-- deja intacta por si algún día hay variantes con atributos de verdad.

-- Qué pide cada categoría del canal. Es una caché de su API: se refresca, no
-- se edita a mano.
CREATE TABLE channel_category_attributes (
    id           BIGSERIAL PRIMARY KEY,
    channel_id   BIGINT    NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    category_id  TEXT      NOT NULL,

    attribute_id   TEXT    NOT NULL,
    attribute_name TEXT    NOT NULL,
    -- required: sin él la publicación se rechaza. catalog: además lo exige
    -- el catálogo unificado del canal.
    required     BOOLEAN   NOT NULL DEFAULT FALSE,
    value_type   TEXT      NOT NULL DEFAULT 'string',
    -- Valores admitidos cuando el atributo es de lista cerrada:
    -- [{"id":"...","name":"..."}]. Vacío = texto libre.
    allowed_values JSONB   NOT NULL DEFAULT '[]'::jsonb,
    unit          TEXT,

    refreshed_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (channel_id, category_id, attribute_id)
);

CREATE INDEX channel_category_attributes_cat_idx
    ON channel_category_attributes (channel_id, category_id);
CREATE INDEX channel_category_attributes_req_idx
    ON channel_category_attributes (channel_id, category_id) WHERE required;

COMMENT ON TABLE channel_category_attributes IS
    'Caché de los atributos que exige cada categoría del canal. Se refresca '
    'desde su API; no se edita a mano.';

-- El valor concreto de cada producto para cada atributo.
--
-- origen dice de dónde salió, que es lo que permite confiar o revisar:
-- deducido (de las specs del nombre), predictor (lo dedujo el canal),
-- manual (lo escribió una persona), defecto (valor por defecto de la marca).
CREATE TABLE producto_atributos (
    product_id   BIGINT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    channel_id   BIGINT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    attribute_id TEXT   NOT NULL,

    value_id     TEXT,
    value_name   TEXT   NOT NULL,
    origen       TEXT   NOT NULL DEFAULT 'manual'
                 CHECK (origen IN ('deducido','predictor','manual','defecto')),

    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (product_id, channel_id, attribute_id)
);

COMMENT ON COLUMN producto_atributos.origen IS
    'De dónde salió el valor. Lo manual nunca se sobrescribe al rededucir: '
    'es la misma garantía que protege las descripciones editadas.';

-- +goose Down
DROP TABLE IF EXISTS producto_atributos;
DROP TABLE IF EXISTS channel_category_attributes;
