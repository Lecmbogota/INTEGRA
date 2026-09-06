-- +goose Up
-- Lo que la publicación necesita saber por variante, y no por producto.
--
-- Las cuatro columnas de esta migración cierran deuda que la auditoría del
-- 2026-09-06 encontró repartida por cuatro áreas distintas. Van juntas porque
-- todas son lo mismo: estado de la publicación que se guardaba en el sitio
-- equivocado, o no se guardaba.

-- 1. content_hash por variante.
--
-- El hash del contenido vivía solo en product_channel_listings, que es una
-- fila por producto y cuenta. Con dos variantes del mismo producto, cada una
-- pisaba el hash de la otra en cada planificación: ninguna coincidía nunca con
-- su catálogo y las dos se reenviaban sin fin, quemando cupo de API en el
-- endpoint más caro. Hoy no se dispara porque el catálogo sincronizado no
-- tiene productos multivariante, pero el día que entre uno, arde.
--
-- Sacar el SKU y el peso del hash habría sido la alternativa sin migración, y
-- es peor: un cambio de código de barras dejaría de reenviar la ficha.
ALTER TABLE variant_channel_listings ADD COLUMN content_hash TEXT;

-- Se hereda lo que ya está publicado a nivel de producto. Sin esto, la
-- primera planificación tras migrar vería todos los hashes vacíos y
-- republicaría el catálogo entero contra los cuatro canales.
UPDATE variant_channel_listings v
   SET content_hash = l.content_hash
  FROM product_channel_listings l
 WHERE l.id = v.listing_id AND v.content_hash IS NULL;

-- 2. El identificador de inventario de Shopify.
--
-- Shopify no ajusta el stock sobre la variante sino sobre su inventory_item,
-- que hay que resolver con una llamada aparte. Sin guardarlo, cada envío de
-- stock gasta dos peticiones en vez de una, y son los envíos más frecuentes
-- que hace la plataforma.
ALTER TABLE variant_channel_listings ADD COLUMN inventory_item_id TEXT;

-- 3. Seguimiento del feed de Falabella.
--
-- Falabella publica de forma asíncrona: se envía un feed, responde un
-- identificador y el resultado real se consulta después. Sin persistirlo, el
-- adaptador daba por publicado lo que solo estaba encolado, y un feed
-- rechazado no dejaba rastro en ninguna parte. La consulta del estado
-- (EstadoFeed) existía y no la llamaba nadie porque no había dónde guardar el
-- identificador que necesita.
ALTER TABLE product_channel_listings ADD COLUMN last_feed_id TEXT;
ALTER TABLE product_channel_listings ADD COLUMN last_feed_status TEXT;
ALTER TABLE product_channel_listings ADD COLUMN last_feed_checked_at TIMESTAMPTZ;

-- Los feeds que todavía no tienen veredicto, que es lo único que hay que
-- volver a consultar.
CREATE INDEX product_channel_listings_feed_pendiente_idx
    ON product_channel_listings (last_feed_checked_at NULLS FIRST)
    WHERE last_feed_id IS NOT NULL
      AND (last_feed_status IS NULL OR last_feed_status NOT IN ('Finished', 'Canceled'));

-- 4. Cuándo y por qué cayó un producto en Odoo.
--
-- El sync ya marca inactivo lo que se archiva o se borra en el origen, pero
-- sin fecha ni motivo no se puede responder a "esto por qué dejó de
-- venderse", que es justo la pregunta que llega cuando alguien nota que un
-- producto desapareció de un marketplace.
ALTER TABLE product_variants ADD COLUMN odoo_baja_at TIMESTAMPTZ;
ALTER TABLE product_variants ADD COLUMN odoo_baja_motivo TEXT
    CHECK (odoo_baja_motivo IN ('archivado', 'borrado'));

COMMENT ON COLUMN product_variants.odoo_baja_at IS
    'Momento en que el sync detectó que el product.product dejó de ser mercancía en Odoo. NULL mientras esté vivo.';

CREATE INDEX product_variants_baja_idx
    ON product_variants (odoo_baja_at DESC) WHERE odoo_baja_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS product_variants_baja_idx;
ALTER TABLE product_variants DROP COLUMN IF EXISTS odoo_baja_motivo;
ALTER TABLE product_variants DROP COLUMN IF EXISTS odoo_baja_at;

DROP INDEX IF EXISTS product_channel_listings_feed_pendiente_idx;
ALTER TABLE product_channel_listings DROP COLUMN IF EXISTS last_feed_checked_at;
ALTER TABLE product_channel_listings DROP COLUMN IF EXISTS last_feed_status;
ALTER TABLE product_channel_listings DROP COLUMN IF EXISTS last_feed_id;

ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS inventory_item_id;
-- El content_hash por variante se pierde: al revertir vuelve a mandar el de
-- producto, que es lo que había antes.
ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS content_hash;
