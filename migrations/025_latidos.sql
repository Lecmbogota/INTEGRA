-- +goose Up
-- El latido de los procesos que trabajan sin que nadie los mire.
--
-- El worker y el volcado de copias son los dos que hacen su trabajo a solas, y
-- ninguno de los dos puede avisar de su propia muerte: si el worker se para,
-- deja de entrar un solo pedido de los cuatro canales y se sigue vendiendo con
-- el stock congelado del último sync; si el volcado deja de correr, no se nota
-- hasta el día en que hace falta restaurar. Por eso dejan constancia de que
-- siguen vivos y es otro proceso —la API, que sí está en pie— quien echa en
-- falta el latido y levanta la alerta.

CREATE TABLE process_heartbeats (
    component   TEXT PRIMARY KEY,

    beat_at     TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Cada cuánto promete latir este componente. Lo declara quien late, no
    -- quien vigila: el worker late cada minuto y la copia una vez al día, y un
    -- plazo cableado en la vigilancia serviría mal a uno de los dos.
    period_secs INTEGER   NOT NULL CHECK (period_secs > 0),

    -- Texto libre de quien late, para que la alerta diga algo útil (la última
    -- copia hecha, su tamaño). No lo interpreta nadie.
    detail      TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- El worker se da por vivo en el momento de migrar.
--
-- Sin esta fila, un worker que nunca llega a arrancar no se echa en falta:
-- la vigilancia solo puede extrañar un latido que alguna vez existió. Con
-- ella, un despliegue en el que el contenedor del worker no levanta se avisa
-- solo, a los pocos minutos.
INSERT INTO process_heartbeats (component, period_secs, detail)
VALUES ('worker', 60, 'sembrado por la migración')
ON CONFLICT (component) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS process_heartbeats;
