-- +goose Up
-- Saber si lo que Integra da por publicado sigue vivo en el canal.
--
-- Hasta aquí la fila de variant_channel_listings solo contaba lo que Integra
-- envió, nunca lo que el canal tiene. Las dos consecuencias caras salían de
-- ahí: una publicación que MercadoLibre da de baja por infracción seguía
-- contando como publicada y no se recreaba jamás, y un producto archivado en
-- Odoo se quedaba a la venta con la última cantidad conocida, congelada.

-- Por qué está pausada, y cuándo.
--
-- El motivo no es decorativo: distingue la pausa que decidió Integra —porque
-- el producto dejó de ser mercancía en Odoo— de la que decidió el canal. La
-- primera se levanta sola cuando el producto vuelve al catálogo; la segunda no
-- se toca nunca desde aquí, porque reabrir una publicación que el marketplace
-- cerró por infracción es lo que convierte un aviso en una sanción.
ALTER TABLE variant_channel_listings ADD COLUMN pausada_at TIMESTAMPTZ;
ALTER TABLE variant_channel_listings ADD COLUMN pausada_motivo TEXT
    CHECK (pausada_motivo IN ('catalogo', 'canal'));

COMMENT ON COLUMN variant_channel_listings.pausada_motivo IS
    '"catalogo": la pausó Integra al salir el producto del catálogo publicable, y se reanuda sola al volver. '
    '"canal": la retiró el propio canal, y solo una persona decide si se reabre.';

-- Lo último que el canal dijo de esta publicación, y cuándo se preguntó.
--
-- conciliada_at es además el reloj que reparte el trabajo: la conciliación
-- consulta por tandas las más viejas, así que una cuenta con miles de
-- publicaciones no gasta su cupo de API de una sentada.
ALTER TABLE variant_channel_listings ADD COLUMN estado_canal TEXT;
ALTER TABLE variant_channel_listings ADD COLUMN conciliada_at TIMESTAMPTZ;

CREATE INDEX variant_listings_conciliar_idx
    ON variant_channel_listings (channel_account_id, conciliada_at NULLS FIRST)
    WHERE status IN ('published', 'paused');

-- Lo que se quedó abierto en el canal sin producto detrás: es lo que hay que
-- pausar, y lo que la vigilancia cuenta para avisar.
CREATE INDEX variant_listings_pausadas_idx
    ON variant_channel_listings (channel_account_id, pausada_motivo)
    WHERE pausada_motivo IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS variant_listings_pausadas_idx;
DROP INDEX IF EXISTS variant_listings_conciliar_idx;
ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS conciliada_at;
ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS estado_canal;
ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS pausada_motivo;
ALTER TABLE variant_channel_listings DROP COLUMN IF EXISTS pausada_at;
