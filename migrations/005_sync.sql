-- +goose Up
-- Ejecuciones, programación, cola de atención y observabilidad.

CREATE TYPE sync_run_status AS ENUM (
    'queued', 'running', 'completed', 'completed_with_errors', 'failed', 'cancelled'
);

CREATE TYPE sync_trigger AS ENUM ('scheduled', 'manual', 'webhook', 'retry');

CREATE TABLE sync_runs (
    id                 BIGSERIAL       PRIMARY KEY,
    trigger            sync_trigger    NOT NULL,
    status             sync_run_status NOT NULL DEFAULT 'queued',

    -- Alcance: todo nulo = catálogo completo a todas las cuentas.
    odoo_connection_id BIGINT REFERENCES odoo_connections(id) ON DELETE SET NULL,
    brand_id           BIGINT REFERENCES brands(id)           ON DELETE SET NULL,
    channel_account_id BIGINT REFERENCES channel_accounts(id) ON DELETE SET NULL,
    variant_id         BIGINT REFERENCES product_variants(id) ON DELETE SET NULL,

    -- En simulación no se escribe nada en los canales.
    dry_run            BOOLEAN     NOT NULL DEFAULT FALSE,

    items_total        INTEGER     NOT NULL DEFAULT 0,
    items_changed      INTEGER     NOT NULL DEFAULT 0,
    items_skipped      INTEGER     NOT NULL DEFAULT 0,
    items_failed       INTEGER     NOT NULL DEFAULT 0,

    started_at         TIMESTAMPTZ,
    finished_at        TIMESTAMPTZ,
    error              TEXT,

    triggered_by       BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sync_runs_recent_idx ON sync_runs (created_at DESC);
CREATE INDEX sync_runs_active_idx ON sync_runs (status)
    WHERE status IN ('queued', 'running');

CREATE TYPE sync_item_outcome AS ENUM (
    'created', 'updated', 'price_updated', 'stock_updated',
    'paused', 'skipped_unchanged', 'needs_attention', 'failed'
);

CREATE TABLE sync_run_items (
    id                 BIGSERIAL   PRIMARY KEY,
    sync_run_id        BIGINT      NOT NULL REFERENCES sync_runs(id) ON DELETE CASCADE,
    variant_id         BIGINT      REFERENCES product_variants(id) ON DELETE SET NULL,
    channel_account_id BIGINT      REFERENCES channel_accounts(id) ON DELETE SET NULL,

    outcome            sync_item_outcome NOT NULL,
    -- 'content' | 'price' | 'stock'
    dimension          TEXT,
    message            TEXT,
    duration_ms        INTEGER,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sync_run_items_run_idx     ON sync_run_items (sync_run_id);
CREATE INDEX sync_run_items_outcome_idx ON sync_run_items (sync_run_id, outcome);

-- Horarios editables desde la interfaz.
--
-- River permite trabajos periódicos, pero se declaran al arrancar el worker y
-- no sirven para cambiar la hora desde la UI. En su lugar, un tick cada minuto
-- lee esta tabla y encola lo que venció.
CREATE TABLE sync_schedules (
    id                 BIGSERIAL   PRIMARY KEY,
    name               TEXT        NOT NULL,

    odoo_connection_id BIGINT REFERENCES odoo_connections(id) ON DELETE CASCADE,
    brand_id           BIGINT REFERENCES brands(id)           ON DELETE CASCADE,
    channel_account_id BIGINT REFERENCES channel_accounts(id) ON DELETE CASCADE,

    -- Hora local del día, con su zona. Se guarda así en vez de en UTC para que
    -- "todos los días a las 6:00" siga siendo a las 6:00 aunque cambie el huso.
    run_at_time        TIME        NOT NULL,
    timezone           TEXT        NOT NULL DEFAULT 'America/Bogota',
    -- Días de la semana: 0 = domingo. Vacío = todos los días.
    weekdays           SMALLINT[]  NOT NULL DEFAULT '{}',

    -- Qué sincroniza: 'full' | 'price' | 'stock'
    scope              TEXT        NOT NULL DEFAULT 'full'
                       CHECK (scope IN ('full', 'price', 'stock')),

    active             BOOLEAN     NOT NULL DEFAULT TRUE,
    last_run_at        TIMESTAMPTZ,
    next_run_at        TIMESTAMPTZ,

    created_by         BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sync_schedules_due_idx ON sync_schedules (next_run_at) WHERE active;

-- Productos que no se pueden publicar y por qué.
--
-- Con los datos actuales de MDV esta cola nace enorme: 626 de 632 productos
-- sin descripción de venta, 88 sin referencia interna y 436 sin precio usable.
CREATE TABLE attention_queue (
    id                 BIGSERIAL   PRIMARY KEY,
    variant_id         BIGINT      NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    channel_account_id BIGINT      REFERENCES channel_accounts(id) ON DELETE CASCADE,

    -- 'missing_sku' | 'missing_description' | 'missing_price' | 'price_below_cost'
    -- | 'missing_category_mapping' | 'missing_required_attribute' | 'title_too_long'
    -- | 'missing_image' | 'channel_rejected'
    reason             TEXT        NOT NULL,
    detail             TEXT,
    -- Para poder ordenar por lo que más ventas desbloquea.
    severity           TEXT        NOT NULL DEFAULT 'blocking'
                       CHECK (severity IN ('blocking', 'warning')),

    resolved_at        TIMESTAMPTZ,
    resolved_by        BIGINT      REFERENCES users(id) ON DELETE SET NULL,

    first_seen_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (variant_id, channel_account_id, reason)
);

CREATE INDEX attention_queue_open_idx
    ON attention_queue (reason, severity) WHERE resolved_at IS NULL;

-- Bitácora de llamadas a las APIs de los canales.
CREATE TABLE channel_api_calls (
    id                 BIGSERIAL   PRIMARY KEY,
    channel_account_id BIGINT      REFERENCES channel_accounts(id) ON DELETE CASCADE,
    sync_run_id        BIGINT      REFERENCES sync_runs(id)        ON DELETE CASCADE,

    method             TEXT        NOT NULL,
    endpoint           TEXT        NOT NULL,
    status_code        INTEGER,
    duration_ms        INTEGER,

    -- Cuerpos con secretos redactados antes de escribir.
    request_body       TEXT,
    response_body      TEXT,
    error              TEXT,

    -- Cabeceras de cupo que devuelve el canal, para el panel de rate limit.
    rate_limit_remaining INTEGER,
    rate_limit_reset_at  TIMESTAMPTZ,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX channel_api_calls_account_idx ON channel_api_calls (channel_account_id, created_at DESC);
CREATE INDEX channel_api_calls_errors_idx  ON channel_api_calls (created_at DESC)
    WHERE status_code >= 400;

CREATE TABLE audit_logs (
    id          BIGSERIAL   PRIMARY KEY,
    user_id     BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    action      TEXT        NOT NULL,
    entity      TEXT        NOT NULL,
    entity_id   TEXT,
    before      JSONB,
    after       JSONB,
    ip          INET,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_entity_idx ON audit_logs (entity, entity_id, created_at DESC);
CREATE INDEX audit_logs_user_idx   ON audit_logs (user_id, created_at DESC);

CREATE TABLE alerts (
    id                 BIGSERIAL   PRIMARY KEY,
    -- 'sync_failed' | 'token_expiring' | 'high_error_rate' | 'rate_limit_exhausted'
    kind               TEXT        NOT NULL,
    severity           TEXT        NOT NULL DEFAULT 'error'
                       CHECK (severity IN ('info', 'warning', 'error', 'critical')),
    channel_account_id BIGINT      REFERENCES channel_accounts(id) ON DELETE CASCADE,
    message            TEXT        NOT NULL,
    detail             JSONB,

    notified_at        TIMESTAMPTZ,
    acknowledged_at    TIMESTAMPTZ,
    acknowledged_by    BIGINT      REFERENCES users(id) ON DELETE SET NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX alerts_open_idx ON alerts (created_at DESC) WHERE acknowledged_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS channel_api_calls;
DROP TABLE IF EXISTS attention_queue;
DROP TABLE IF EXISTS sync_schedules;
DROP TABLE IF EXISTS sync_run_items;
DROP TABLE IF EXISTS sync_runs;
DROP TYPE  IF EXISTS sync_item_outcome;
DROP TYPE  IF EXISTS sync_trigger;
DROP TYPE  IF EXISTS sync_run_status;
