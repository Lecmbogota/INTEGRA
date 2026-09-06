-- +goose Up
-- Banco de imágenes propio de Integra.
--
-- Las imágenes de Odoo no sirven para publicar: son miniaturas WebP de unos
-- 2,5 KB, con resoluciones entre 139×152 y 255×158, y varias se repiten entre
-- productos distintos. Los marketplaces piden 500×500 como mínimo.
--
-- Por eso Integra guarda las suyas en vez de leer las de Odoo. La tabla
-- product_images original se creó pensando en referenciar las de Odoo; se
-- rehace con lo que hace falta de verdad.

DROP TABLE IF EXISTS product_images;

CREATE TABLE imagenes (
    id           BIGSERIAL   PRIMARY KEY,

    -- Direccionamiento por contenido: el mismo fichero subido dos veces ocupa
    -- una sola vez y se reutiliza entre productos. Con fotos de fabricante
    -- compartidas entre variantes, esto ahorra bastante.
    sha256       TEXT        NOT NULL UNIQUE,
    ruta         TEXT        NOT NULL,

    formato      TEXT        NOT NULL,
    ancho        INTEGER     NOT NULL,
    alto         INTEGER     NOT NULL,
    bytes        INTEGER     NOT NULL,

    -- 'subida' | 'banco_fabricante' | 'url'
    origen       TEXT        NOT NULL DEFAULT 'subida',
    origen_ref   TEXT,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX imagenes_origen_idx ON imagenes (origen);

COMMENT ON COLUMN imagenes.sha256 IS
    'Hash del contenido original. Es la clave de deduplicación y también el '
    'nombre del fichero en disco.';

-- Relación producto × imagen, con orden.
CREATE TABLE producto_imagenes (
    product_id BIGINT      NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    imagen_id  BIGINT      NOT NULL REFERENCES imagenes(id) ON DELETE CASCADE,
    posicion   INTEGER     NOT NULL DEFAULT 0,
    -- La principal es la que sale en los listados de las tiendas.
    principal  BOOLEAN     NOT NULL DEFAULT FALSE,
    alt        TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (product_id, imagen_id)
);

CREATE INDEX producto_imagenes_orden_idx ON producto_imagenes (product_id, posicion);

-- Como mucho una imagen principal por producto.
CREATE UNIQUE INDEX producto_imagenes_principal_idx
    ON producto_imagenes (product_id) WHERE principal;

-- Derivadas ya procesadas para cada canal.
--
-- Se guardan en vez de generarse al vuelo porque los canales descargan la
-- imagen varias veces y regenerar un JPEG de 1200×1200 en cada petición sería
-- absurdo.
CREATE TABLE imagen_derivadas (
    imagen_id  BIGINT      NOT NULL REFERENCES imagenes(id) ON DELETE CASCADE,
    -- 'cuadrada_1200' | 'web_800' | 'miniatura_300'
    variante   TEXT        NOT NULL,
    ruta       TEXT        NOT NULL,
    formato    TEXT        NOT NULL,
    ancho      INTEGER     NOT NULL,
    alto       INTEGER     NOT NULL,
    bytes      INTEGER     NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    PRIMARY KEY (imagen_id, variante)
);

-- +goose Down
DROP TABLE IF EXISTS imagen_derivadas;
DROP TABLE IF EXISTS producto_imagenes;
DROP TABLE IF EXISTS imagenes;

-- Se restaura la tabla original para que la reversión deje el esquema como
-- estaba tras la migración 002.
CREATE TABLE product_images (
    id           BIGSERIAL   PRIMARY KEY,
    product_id   BIGINT      NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    position     INTEGER     NOT NULL DEFAULT 0,
    content_hash TEXT        NOT NULL,
    mime_type    TEXT,
    bytes        INTEGER,
    url          TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (product_id, position)
);
