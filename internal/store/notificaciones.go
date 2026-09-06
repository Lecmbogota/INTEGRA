package store

import (
	"context"
	"fmt"
)

// DestinoNotificacion es a dónde salen las alertas.
//
// La configuración viaja cifrada porque contiene secretos: la contraseña SMTP
// da acceso a enviar correo en nombre de la empresa.
type DestinoNotificacion struct {
	ID           int64
	Tipo         string // email, telegram, webhook
	Nombre       string
	ConfigEnc    []byte
	MinSeveridad string
}

// severidades ordena de menor a mayor urgencia, para poder comparar contra el
// mínimo de cada destino.
var severidades = map[string]int{"info": 0, "warning": 1, "error": 2, "critical": 3}

// DestinosActivos devuelve a dónde hay que mandar los avisos.
func (s *Store) DestinosActivos(ctx context.Context) ([]DestinoNotificacion, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT id, kind, name, config_enc, min_severity
		FROM notification_destinations WHERE active ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listando destinos de notificación: %w", err)
	}
	defer filas.Close()

	var out []DestinoNotificacion
	for filas.Next() {
		var d DestinoNotificacion
		if err := filas.Scan(&d.ID, &d.Tipo, &d.Nombre, &d.ConfigEnc, &d.MinSeveridad); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, filas.Err()
}

// AlertaPendiente es un aviso que aún no se ha mandado a ningún sitio.
type AlertaPendiente struct {
	ID        int64
	Tipo      string
	Severidad string
	Mensaje   string
}

// AlertasSinNotificar devuelve las alertas abiertas que todavía no se
// enviaron y que alcanzan la severidad mínima pedida.
//
// El límite evita que un incidente que abra cientos de alertas de golpe
// produzca un envío masivo de correo: se mandan por tandas y el resto sale en
// la siguiente pasada del planificador.
func (s *Store) AlertasSinNotificar(ctx context.Context, minSeveridad string, limite int) ([]AlertaPendiente, error) {
	if limite <= 0 || limite > 50 {
		limite = 20
	}
	min, ok := severidades[minSeveridad]
	if !ok {
		min = severidades["error"]
	}
	// Las severidades que superan el mínimo, resueltas aquí para que la
	// consulta no dependa de un orden textual que no existe en la base.
	var aceptadas []string
	for sev, n := range severidades {
		if n >= min {
			aceptadas = append(aceptadas, sev)
		}
	}

	filas, err := s.pool.Query(ctx, `
		SELECT id, kind, severity, message
		FROM alerts
		WHERE notified_at IS NULL AND acknowledged_at IS NULL
		  AND severity = ANY($1)
		ORDER BY created_at
		LIMIT $2`, aceptadas, limite)
	if err != nil {
		return nil, fmt.Errorf("listando alertas sin notificar: %w", err)
	}
	defer filas.Close()

	var out []AlertaPendiente
	for filas.Next() {
		var a AlertaPendiente
		if err := filas.Scan(&a.ID, &a.Tipo, &a.Severidad, &a.Mensaje); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, filas.Err()
}

// MarcarAlertasNotificadas sella lo ya enviado.
//
// Se llama solo después de un envío con éxito: sellarlas antes convertiría
// un fallo del servidor de correo en una alerta que nadie ve nunca.
func (s *Store) MarcarAlertasNotificadas(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE alerts SET notified_at = now() WHERE id = ANY($1)`, ids)
	return err
}

// AnotarEnvioNotificacion guarda cómo fue el último intento de cada destino,
// para que un servidor de correo mal configurado se vea en el panel en vez de
// fallar en silencio para siempre.
func (s *Store) AnotarEnvioNotificacion(ctx context.Context, destinoID int64, err error) error {
	var causa any
	if err != nil {
		causa = err.Error()
	}
	_, e := s.pool.Exec(ctx, `
		UPDATE notification_destinations
		SET last_sent_at = CASE WHEN $2::text IS NULL THEN now() ELSE last_sent_at END,
		    last_error = $2, updated_at = now()
		WHERE id = $1`, destinoID, causa)
	return e
}
