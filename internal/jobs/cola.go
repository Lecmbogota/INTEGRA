// Package jobs es la cola de trabajos sobre PostgreSQL.
//
// Todo efecto hacia un canal (publicar, actualizar precio, ingerir órdenes)
// pasa por aquí: la cola aporta reintentos con backoff exponencial,
// idempotencia por clave y recuperación de trabajos cuyo worker murió.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Cola struct{ pool *pgxpool.Pool }

func NuevaCola(pool *pgxpool.Pool) *Cola { return &Cola{pool: pool} }

// Pool expone el pool para consultas puntuales sobre la tabla de trabajos
// (lo usan las pruebas de integración para comprobar lo que quedó encolado).
func (c *Cola) Pool() *pgxpool.Pool { return c.pool }

// Trabajo es lo que recibe un manejador.
type Trabajo struct {
	ID          int64
	Kind        string
	Payload     json.RawMessage
	Intentos    int
	MaxIntentos int
	CuentaID    *int64
}

// Opciones afina un encolado. El valor cero es razonable para todo.
type Opciones struct {
	// UniqueKey deduplica: si ya hay un trabajo vivo (pending o running) con
	// la misma clave, Encolar no crea otro y devuelve 0.
	UniqueKey string
	// RunAt retrasa la ejecución. Cero = ahora.
	RunAt time.Time
	// Priority ordena la cola: menor número, antes se atiende. Cero = 100.
	Priority int
	// MaxAttempts corta los reintentos. Cero = 5.
	MaxAttempts int
	// CuentaID ata el trabajo a una cuenta de canal (se borra en cascada).
	CuentaID int64
}

// Encolar crea un trabajo y devuelve su id, o 0 si la clave única ya estaba
// viva (no es un error: significa que el trabajo ya está pedido).
func (c *Cola) Encolar(ctx context.Context, kind string, payload any, op Opciones) (int64, error) {
	cuerpo, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("serializando el payload de %s: %w", kind, err)
	}
	if op.Priority == 0 {
		op.Priority = 100
	}
	if op.MaxAttempts == 0 {
		op.MaxAttempts = 5
	}
	// El reloj que manda es el de PostgreSQL: Reclamar compara run_at contra
	// now() de la base, así que sellar aquí con time.Now() dejaría invisible
	// un trabajo recién creado si el reloj de la aplicación va adelantado
	// (pasa de verdad: la VM de Docker deriva unos cientos de ms).
	var runAt any
	if !op.RunAt.IsZero() {
		runAt = op.RunAt
	}
	var clave any
	if op.UniqueKey != "" {
		clave = op.UniqueKey
	}
	var cuenta any
	if op.CuentaID != 0 {
		cuenta = op.CuentaID
	}

	var id int64
	err = c.pool.QueryRow(ctx, `
		INSERT INTO jobs (kind, payload, priority, max_attempts, run_at, unique_key, channel_account_id)
		VALUES ($1, $2, $3, $4, COALESCE($5::timestamptz, now()), $6, $7)
		ON CONFLICT DO NOTHING
		RETURNING id`,
		kind, cuerpo, op.Priority, op.MaxAttempts, runAt, clave, cuenta).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // ya estaba encolado con la misma clave
	}
	if err != nil {
		return 0, fmt.Errorf("encolando %s: %w", kind, err)
	}
	return id, nil
}

// Reclamar toma hasta n trabajos listos y los marca en ejecución con un lease.
//
// FOR UPDATE SKIP LOCKED hace que varios workers puedan reclamar a la vez sin
// pisarse ni bloquearse. El intento se consume al reclamar, no al terminar:
// así un trabajo que mata al worker una y otra vez no reintenta para siempre.
// Reclamar toma hasta n trabajos pendientes de los tipos indicados.
//
// El filtro por tipo no es un detalle: sin él, un worker reclamaba cualquier
// trabajo aunque no tuviera manejador y lo daba por fallido —«no hay manejador
// registrado»—, gastándole un intento a un trabajo que otro proceso sí sabía
// atender. Con dos workers especializados, cada uno destruía el trabajo del
// otro; en las pruebas, el worker de la máquina se comía los trabajos de
// prueba y las dejaba esperando a algo que ya nadie iba a procesar.
//
// Sin tipos se reclama de todos, que es como se comportaba antes.
func (c *Cola) Reclamar(ctx context.Context, n int, lease time.Duration, tipos ...string) ([]Trabajo, error) {
	filtro, args := "", []any{n, lease}
	if len(tipos) > 0 {
		args = append(args, tipos)
		filtro = " AND kind = ANY($3)"
	}
	filas, err := c.pool.Query(ctx, `
		UPDATE jobs SET status = 'running', attempts = attempts + 1,
		       locked_until = now() + $2, updated_at = now()
		WHERE id IN (
		    SELECT id FROM jobs
		    WHERE status = 'pending' AND run_at <= now()`+filtro+`
		    ORDER BY priority, run_at, id
		    LIMIT $1
		    FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, payload, attempts, max_attempts, channel_account_id`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("reclamando trabajos: %w", err)
	}
	defer filas.Close()

	var out []Trabajo
	for filas.Next() {
		var t Trabajo
		if err := filas.Scan(&t.ID, &t.Kind, &t.Payload, &t.Intentos, &t.MaxIntentos, &t.CuentaID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, filas.Err()
}

// Completar marca un trabajo como hecho.
func (c *Cola) Completar(ctx context.Context, id int64) error {
	_, err := c.pool.Exec(ctx, `
		UPDATE jobs SET status = 'done', locked_until = NULL, last_error = NULL, updated_at = now()
		WHERE id = $1 AND status = 'running'`, id)
	return err
}

// TopeDeEspera acota cuánto puede hacernos esperar un canal. Es el mismo techo
// que ya tenía el backoff: un Retry-After absurdo —o mal leído— no puede
// aparcar un trabajo durante días.
const TopeDeEspera = 2 * time.Hour

// Fallar registra el error y decide: reintento con backoff exponencial
// (30s, 1m, 2m, 4m… hasta 2h) o fallo definitivo si se agotaron los intentos.
//
// `espera` es el mínimo que pidió quien falló, y no lo sabe el backoff: cuando
// un canal responde 429 con Retry-After de una hora, volver a los 30 s no
// publica nada, alarga el bloqueo y gasta los cinco intentos en ocho minutos,
// de modo que el catálogo entero acaba en `failed` justo cuando el canal
// vuelve a aceptar escrituras. Cero significa «lo que diga el backoff».
func (c *Cola) Fallar(ctx context.Context, id int64, causa string, espera time.Duration) error {
	if espera > TopeDeEspera {
		espera = TopeDeEspera
	}
	if espera < 0 {
		espera = 0
	}
	_, err := c.pool.Exec(ctx, `
		UPDATE jobs SET
		    status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		    run_at = CASE WHEN attempts >= max_attempts THEN run_at
		             ELSE now() + GREATEST(
		                  make_interval(secs => LEAST(30 * power(2, attempts - 1), 7200)),
		                  make_interval(secs => $3)) END,
		    locked_until = NULL, last_error = $2, updated_at = now()
		WHERE id = $1 AND status = 'running'`, id, causa, espera.Seconds())
	return err
}

// EsperaPedida saca de un error cuánto pidió esperar el canal.
//
// Se resuelve por interfaz y no por el tipo concreto para que la cola no
// importe internal/channel: aquí solo hay trabajos, no canales. La cumple
// channel.Error con su RetryAfter.
func EsperaPedida(err error) time.Duration {
	var e interface{ EsperaAntesDeReintentar() time.Duration }
	if errors.As(err, &e) {
		return e.EsperaAntesDeReintentar()
	}
	return 0
}

// RecuperarHuerfanos devuelve a la cola los trabajos cuyo lease expiró: su
// worker murió sin completar ni fallar. Los que ya agotaron intentos quedan
// en failed con la causa anotada.
func (c *Cola) RecuperarHuerfanos(ctx context.Context) (int, error) {
	ct, err := c.pool.Exec(ctx, `
		UPDATE jobs SET
		    status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		    last_error = COALESCE(last_error, '') || ' [el worker murió con el trabajo en curso]',
		    locked_until = NULL, updated_at = now()
		WHERE status = 'running' AND locked_until < now()`)
	if err != nil {
		return 0, fmt.Errorf("recuperando trabajos huérfanos: %w", err)
	}
	return int(ct.RowsAffected()), nil
}

// Pendientes cuenta lo que espera en cola, para el panel y los logs.
func (c *Cola) Pendientes(ctx context.Context) (int, error) {
	var n int
	err := c.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE status = 'pending'`).Scan(&n)
	return n, err
}
