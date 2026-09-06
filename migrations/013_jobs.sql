-- +goose Up
-- Cola de trabajos (Fase 2).
--
-- Publicar el catálogo son miles de llamadas a APIs ajenas con cupos y fallos
-- transitorios: nada de eso puede vivir en una petición HTTP. Todo efecto
-- hacia un canal pasa por esta cola, que da reintentos con backoff,
-- idempotencia y supervivencia a la muerte del worker.
--
-- Va sobre PostgreSQL y no sobre un broker aparte a propósito: con el volumen
-- de MDV (cientos de trabajos por corrida) FOR UPDATE SKIP LOCKED sobra, y es
-- una pieza menos que desplegar, respaldar y vigilar.

CREATE TABLE jobs (
    id           BIGSERIAL   PRIMARY KEY,
    kind         TEXT        NOT NULL,
    payload      JSONB       NOT NULL DEFAULT '{}'::jsonb,

    status       TEXT        NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','running','done','failed','cancelled')),
    priority     INTEGER     NOT NULL DEFAULT 100,

    attempts     INTEGER     NOT NULL DEFAULT 0,
    max_attempts INTEGER     NOT NULL DEFAULT 5,
    run_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Lease del worker: si expira con el trabajo en running, el worker murió
    -- y el trabajo vuelve a la cola.
    locked_until TIMESTAMPTZ,
    last_error   TEXT,

    -- Idempotencia: el mismo trabajo lógico (p. ej. "publicar variante 42 en
    -- la cuenta 3") no se encola dos veces mientras siga vivo.
    unique_key   TEXT,

    channel_account_id BIGINT REFERENCES channel_accounts(id) ON DELETE CASCADE,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX jobs_unique_key_vivo_idx ON jobs (unique_key)
    WHERE unique_key IS NOT NULL AND status IN ('pending','running');

-- El índice de la cola cubre exactamente la consulta de reclamo.
CREATE INDEX jobs_cola_idx ON jobs (priority, run_at, id) WHERE status = 'pending';
CREATE INDEX jobs_huerfanos_idx ON jobs (locked_until) WHERE status = 'running';
CREATE INDEX jobs_kind_idx ON jobs (kind, status);

-- +goose Down
DROP TABLE IF EXISTS jobs;
