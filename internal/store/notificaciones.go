package store

import (
	"context"
	"fmt"
	"time"
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

// DestinoListado es un destino tal como se ve en la pantalla de avisos.
//
// No lleva la configuración: dentro va la contraseña SMTP, y un secreto que
// sale del servidor acaba en el historial del navegador, en un registro de
// proxy o en una captura de pantalla. Solo se manda hacia dentro.
type DestinoListado struct {
	ID            int64      `json:"id"`
	Tipo          string     `json:"tipo"`
	Nombre        string     `json:"nombre"`
	MinSeveridad  string     `json:"min_severidad"`
	Activo        bool       `json:"activo"`
	UltimoEnvio   *time.Time `json:"ultimo_envio"`
	UltimoError   string     `json:"ultimo_error"`
	Destinatarios []string   `json:"destinatarios"`
}

// ListarDestinos devuelve todos los destinos, activos o no.
//
// Los inactivos también se muestran: un destino apagado explica por qué no
// llegan los avisos, y esconderlo deja el mismo silencio que no tener ninguno.
func (s *Store) ListarDestinos(ctx context.Context) ([]DestinoListado, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT id, kind, name, min_severity, active, last_sent_at, COALESCE(last_error, '')
		FROM notification_destinations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listando destinos de aviso: %w", err)
	}
	defer filas.Close()

	out := []DestinoListado{}
	for filas.Next() {
		var d DestinoListado
		if err := filas.Scan(&d.ID, &d.Tipo, &d.Nombre, &d.MinSeveridad,
			&d.Activo, &d.UltimoEnvio, &d.UltimoError); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, filas.Err()
}

// DestinoPorID trae un destino con su configuración cifrada, para poder
// probarlo o conservar la contraseña cuando se edita el resto.
func (s *Store) DestinoPorID(ctx context.Context, id int64) (DestinoNotificacion, error) {
	var d DestinoNotificacion
	err := s.pool.QueryRow(ctx, `
		SELECT id, kind, name, config_enc, min_severity
		FROM notification_destinations WHERE id = $1`, id).
		Scan(&d.ID, &d.Tipo, &d.Nombre, &d.ConfigEnc, &d.MinSeveridad)
	if err != nil {
		return DestinoNotificacion{}, fmt.Errorf("leyendo el destino %d: %w", id, err)
	}
	return d, nil
}

// GuardarDestino da de alta un destino o reemplaza uno existente.
//
// Devuelve su identificador. La configuración llega ya cifrada: este paquete
// no sabe descifrar y no debe saberlo.
func (s *Store) GuardarDestino(ctx context.Context, id int64, tipo, nombre, minSeveridad string,
	activo bool, configEnc []byte) (int64, error) {
	if id == 0 {
		var nuevo int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO notification_destinations (kind, name, config_enc, min_severity, active)
			VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			tipo, nombre, configEnc, minSeveridad, activo).Scan(&nuevo)
		if err != nil {
			return 0, fmt.Errorf("creando el destino de aviso: %w", err)
		}
		return nuevo, nil
	}

	// Al editar se limpia last_error: el que había describe la configuración
	// anterior, y dejarlo hace creer que el destino recién corregido sigue roto.
	tag, err := s.pool.Exec(ctx, `
		UPDATE notification_destinations
		SET name = $2, config_enc = $3, min_severity = $4, active = $5,
		    last_error = NULL, updated_at = now()
		WHERE id = $1`, id, nombre, configEnc, minSeveridad, activo)
	if err != nil {
		return 0, fmt.Errorf("guardando el destino %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return 0, fmt.Errorf("no existe el destino %d", id)
	}
	return id, nil
}

// BorrarDestino lo elimina.
func (s *Store) BorrarDestino(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM notification_destinations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("borrando el destino %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no existe el destino %d", id)
	}
	return nil
}
