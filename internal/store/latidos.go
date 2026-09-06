package store

import (
	"context"
	"fmt"
	"time"
)

// Latido es la última señal de vida de un proceso que trabaja a solas.
//
// Atrasado se calcula en la consulta y no aquí porque el reloj que importa es
// el de PostgreSQL: worker, API y contenedor de copias son tres procesos, y
// comparar la hora de uno con la de otro haría depender la vigilancia de que
// los tres relojes coincidan.
type Latido struct {
	Componente string        `json:"componente"`
	Visto      time.Time     `json:"visto"`
	Periodo    time.Duration `json:"-"`
	Detalle    string        `json:"detalle,omitempty"`
	Atrasado   bool          `json:"atrasado"`
}

// Componentes vigilados. El nombre es la clave de la tabla: cambiarlo deja
// huérfana la fila anterior y el proceso se echa en falta para siempre.
const (
	ComponenteWorker = "worker"
	// Lo escribe el contenedor de copias con psql, no código Go: el volcado
	// es un contenedor de PostgreSQL, no un proceso de Integra.
	ComponenteCopias = "copias"
)

// toleranciaLatido es cuántos periodos se dejan pasar antes de dar por muerto
// a un proceso.
//
// Tres, y no uno, porque un latido perdido es lo normal: un reinicio, un
// despliegue o una consulta lenta bastan para saltarse uno. Dar la alarma al
// primero llenaría el correo de falsos avisos y en dos semanas nadie los
// miraría, que es exactamente la enfermedad que esto viene a curar.
const toleranciaLatido = 3

// Ventana es cuánto se tolera sin noticias antes de dar el proceso por
// muerto. Va aquí para que quien redacta la alerta diga el mismo plazo que
// aplica la consulta.
func (l Latido) Ventana() time.Duration { return l.Periodo * toleranciaLatido }

// RegistrarLatido deja constancia de que el componente sigue vivo.
//
// El periodo lo declara quien late: es lo que permite vigilar con el mismo
// código a un worker que late cada minuto y a una copia de seguridad que late
// una vez al día.
func (s *Store) RegistrarLatido(ctx context.Context, componente string, periodo time.Duration, detalle string) error {
	segundos := int(periodo.Seconds())
	if segundos < 1 {
		segundos = 1
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO process_heartbeats (component, beat_at, period_secs, detail)
		VALUES ($1, now(), $2, $3)
		ON CONFLICT (component) DO UPDATE
		SET beat_at = now(), period_secs = EXCLUDED.period_secs, detail = EXCLUDED.detail`,
		componente, segundos, detalle)
	if err != nil {
		return fmt.Errorf("registrando el latido de %s: %w", componente, err)
	}
	return nil
}

// Latidos devuelve el estado de todos los procesos vigilados.
func (s *Store) Latidos(ctx context.Context) ([]Latido, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT component, beat_at, period_secs, COALESCE(detail, ''),
		       now() - beat_at > make_interval(secs => period_secs * $1)
		FROM process_heartbeats
		ORDER BY component`, toleranciaLatido)
	if err != nil {
		return nil, fmt.Errorf("consultando los latidos: %w", err)
	}
	defer filas.Close()

	var out []Latido
	for filas.Next() {
		var l Latido
		var segundos int
		if err := filas.Scan(&l.Componente, &l.Visto, &segundos, &l.Detalle, &l.Atrasado); err != nil {
			return nil, err
		}
		l.Periodo = time.Duration(segundos) * time.Second
		out = append(out, l)
	}
	return out, filas.Err()
}

// LatidosAtrasados devuelve solo los procesos que dejaron de dar señales.
func (s *Store) LatidosAtrasados(ctx context.Context) ([]Latido, error) {
	todos, err := s.Latidos(ctx)
	if err != nil {
		return nil, err
	}
	var out []Latido
	for _, l := range todos {
		if l.Atrasado {
			out = append(out, l)
		}
	}
	return out, nil
}
