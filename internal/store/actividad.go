package store

import (
	"context"
	"fmt"
	"time"
)

// Actividad: qué se está haciendo ahora y qué se hizo antes.
//
// Integra trabaja sola en segundo plano —publica, ajusta precios, ingiere
// pedidos, avisa despachos— y hasta ahora eso no se veía por ninguna parte:
// el único rastro era un contador de «pendientes en cola» en el resumen por
// canal. Quien pulsaba un botón no sabía si su trabajo estaba corriendo, en
// cola detrás de otros trescientos, o fallado desde hacía una hora. Y la
// auditoría, que sí se escribía, no la pintaba ninguna pantalla.

// TareaEnCurso agrupa la cola por tipo de trabajo.
type TareaEnCurso struct {
	Tipo       string `json:"tipo"`
	Corriendo  int    `json:"corriendo"`
	Pendientes int    `json:"pendientes"`
	Fallidos   int    `json:"fallidos"`
	Hechos     int    `json:"hechos"`
	// Desde es cuándo empezó lo más antiguo que sigue vivo: es lo que
	// distingue «va lento» de «lleva parado desde ayer».
	Desde *time.Time `json:"desde"`
	// UltimoError es el del fallo más reciente de ese tipo.
	UltimoError string `json:"ultimo_error"`
}

// Evento es una línea de la historia: algo que pasó y cuándo.
type Evento struct {
	Cuando time.Time `json:"cuando"`
	// Clase agrupa el origen: trabajo, persona o aviso.
	Clase   string `json:"clase"`
	Titulo  string `json:"titulo"`
	Detalle string `json:"detalle"`
	Quien   string `json:"quien"`
	Malo    bool   `json:"malo"`
}

// ColaPorTipo resume lo que la cola tiene entre manos.
//
// «Hechos» cuenta solo las últimas 24 horas: el total histórico crece sin
// parar y no dice nada útil, mientras que «cuántos salieron hoy» sí.
func (s *Store) ColaPorTipo(ctx context.Context) ([]TareaEnCurso, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT kind,
		       count(*) FILTER (WHERE status = 'running')::int,
		       count(*) FILTER (WHERE status = 'pending')::int,
		       count(*) FILTER (WHERE status = 'failed')::int,
		       count(*) FILTER (WHERE status = 'done' AND updated_at > now() - interval '24 hours')::int,
		       min(created_at) FILTER (WHERE status IN ('pending','running')),
		       COALESCE((array_agg(last_error ORDER BY updated_at DESC)
		                 FILTER (WHERE status = 'failed' AND last_error IS NOT NULL))[1], '')
		FROM jobs
		WHERE status IN ('pending','running','failed')
		   OR (status = 'done' AND updated_at > now() - interval '24 hours')
		GROUP BY kind
		ORDER BY count(*) FILTER (WHERE status IN ('running','pending')) DESC, kind`)
	if err != nil {
		return nil, fmt.Errorf("resumiendo la cola: %w", err)
	}
	defer filas.Close()

	out := []TareaEnCurso{}
	for filas.Next() {
		var t TareaEnCurso
		if err := filas.Scan(&t.Tipo, &t.Corriendo, &t.Pendientes, &t.Fallidos,
			&t.Hechos, &t.Desde, &t.UltimoError); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, filas.Err()
}

// Historia devuelve la línea de tiempo, mezclando las tres cosas que dejan
// rastro: lo que hizo una persona, lo que falló y los avisos que se abrieron.
//
// Los trabajos que salieron bien NO entran uno a uno: una publicación de
// catálogo son cientos de líneas idénticas que entierran lo único que hay que
// leer. Los que fallaron sí, porque cada uno pide una decisión.
func (s *Store) Historia(ctx context.Context, limite int) ([]Evento, error) {
	if limite <= 0 || limite > 200 {
		limite = 60
	}
	filas, err := s.pool.Query(ctx, `
		(
		  SELECT a.created_at, 'persona', a.action || ' · ' || a.entity,
		         COALESCE(a.entity_id, ''), COALESCE(u.name, u.email, 'sistema'), false
		  FROM audit_logs a
		  LEFT JOIN users u ON u.id = a.user_id
		)
		UNION ALL
		(
		  SELECT j.updated_at, 'trabajo', j.kind,
		         COALESCE(left(j.last_error, 200), ''), '', true
		  FROM jobs j
		  WHERE j.status = 'failed'
		)
		UNION ALL
		(
		  SELECT al.created_at, 'aviso', al.kind, al.message, '',
		         al.severity IN ('error','critical')
		  FROM alerts al
		)
		ORDER BY 1 DESC
		LIMIT $1`, limite)
	if err != nil {
		return nil, fmt.Errorf("leyendo la historia: %w", err)
	}
	defer filas.Close()

	out := []Evento{}
	for filas.Next() {
		var e Evento
		if err := filas.Scan(&e.Cuando, &e.Clase, &e.Titulo, &e.Detalle, &e.Quien, &e.Malo); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, filas.Err()
}

// CancelarPendientes retira de la cola lo que aún no ha empezado.
//
// Solo lo pendiente: un trabajo en curso ya está hablando con el canal, y
// marcarlo cancelado aquí no desharía la llamada —dejaría la base diciendo que
// no se envió algo que sí se envió, que es peor que esperar los pocos segundos
// que tarda—. Con tipo vacío se cancela todo lo pendiente.
func (s *Store) CancelarPendientes(ctx context.Context, tipo string) (int64, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if tipo == "" {
		tag, err = s.pool.Exec(ctx, `
			UPDATE jobs SET status = 'cancelled', updated_at = now()
			WHERE status = 'pending'`)
	} else {
		tag, err = s.pool.Exec(ctx, `
			UPDATE jobs SET status = 'cancelled', updated_at = now()
			WHERE status = 'pending' AND kind = $1`, tipo)
	}
	if err != nil {
		return 0, fmt.Errorf("cancelando lo pendiente: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ReintentarFallidos devuelve a la cola lo que agotó sus intentos.
//
// El contador vuelve a cero: sin eso el trabajo se reclamaría y moriría en el
// acto por seguir agotado, y la pantalla enseñaría un botón que no hace nada.
func (s *Store) ReintentarFallidos(ctx context.Context, tipo string) (int64, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	sql := `
		UPDATE jobs SET status = 'pending', attempts = 0, run_at = now(),
		                last_error = NULL, locked_until = NULL, updated_at = now()
		WHERE status = 'failed'`
	// La clave única solo cubre lo vivo, así que un fallido cuyo trabajo ya se
	// reencoló chocaría al revivir: se descarta en vez de romper la tanda.
	guarda := ` AND NOT EXISTS (
			SELECT 1 FROM jobs v
			WHERE v.unique_key = jobs.unique_key AND v.status IN ('pending','running'))`
	if tipo == "" {
		tag, err = s.pool.Exec(ctx, sql+guarda)
	} else {
		tag, err = s.pool.Exec(ctx, sql+` AND kind = $1`+guarda, tipo)
	}
	if err != nil {
		return 0, fmt.Errorf("reintentando lo fallido: %w", err)
	}
	return tag.RowsAffected(), nil
}
