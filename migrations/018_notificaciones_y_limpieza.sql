-- +goose Up
-- Destinos de notificación y limpieza de tablas muertas.

-- A dónde salen las alertas. Sin esto son un panel que nadie mira.
--
-- La configuración va cifrada porque contiene secretos: el token de un bot de
-- Telegram o la contraseña SMTP dan acceso a enviar en nombre de la empresa.
CREATE TABLE notification_destinations (
    id           BIGSERIAL PRIMARY KEY,
    kind         TEXT      NOT NULL CHECK (kind IN ('telegram', 'webhook', 'email')),
    name         TEXT      NOT NULL,
    config_enc   BYTEA     NOT NULL,

    -- Severidad mínima que se envía. Mandar los avisos informativos a
    -- WhatsApp entrena a la gente a ignorar las notificaciones.
    min_severity TEXT      NOT NULL DEFAULT 'error'
                 CHECK (min_severity IN ('info', 'warning', 'error', 'critical')),
    active       BOOLEAN   NOT NULL DEFAULT TRUE,

    last_sent_at TIMESTAMPTZ,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON COLUMN notification_destinations.config_enc IS
    'Configuración cifrada con la clave maestra. Contiene el token del bot, la '
    'URL del webhook o las credenciales SMTP.';

CREATE INDEX notification_destinations_activos_idx
    ON notification_destinations (kind) WHERE active;

-- ---------------------------------------------------------------- limpieza
--
-- Estas cuatro tablas quedaron muertas por decisiones de diseño posteriores,
-- no por olvido. Dejarlas confunde a quien lea el esquema dentro de seis
-- meses: parecen funcionalidad pendiente cuando en realidad son un camino
-- que se descartó.

-- attribute_mappings mapeaba un atributo de ODOO a uno del canal. No sirve:
-- el catálogo de MDV no usa atributos de Odoo (0 de 722 plantillas) y los
-- requisitos dependen de la CATEGORÍA del canal, no de la cuenta. Lo
-- sustituyen channel_category_attributes y producto_atributos (migración 016).
DROP TABLE IF EXISTS attribute_value_mappings;
DROP TABLE IF EXISTS attribute_mappings;

-- odoo_pricelists cacheaba las tarifas de Odoo para recalcular precios en
-- cada sync. Dejó de tener sentido cuando el precio pasó a ser propiedad de
-- Integra (migración 012): el motor de tarifas solo sembró el valor inicial y
-- quedó congelado.
DROP TABLE IF EXISTS odoo_pricelist_items;
DROP TABLE IF EXISTS odoo_pricelists;

-- +goose Down
DROP TABLE IF EXISTS notification_destinations;
-- Las tablas eliminadas no se restauran: su contenido era caché reconstruible
-- y su diseño quedó superado.
