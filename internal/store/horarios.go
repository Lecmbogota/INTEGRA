package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Horario es una tarea programada: qué hacer, a qué hora y qué días.
type Horario struct {
	ID         int64      `json:"id"`
	Nombre     string     `json:"nombre"`
	CuentaID   *int64     `json:"cuenta_id"`
	Canal      string     `json:"canal"`
	Hora       string     `json:"hora"`     // HH:MM local
	Zona       string     `json:"zona"`
	Dias       []int16    `json:"dias"`     // 1=lunes … 7=domingo; vacío = todos
	Alcance    string     `json:"alcance"`  // full | price | stock
	Activo     bool       `json:"activo"`
	UltimaAt   *time.Time `json:"ultima_ejecucion"`
	ProximaAt  *time.Time `json:"proxima_ejecucion"`
}

func (s *Store) ListarHorarios(ctx context.Context) ([]Horario, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT h.id, h.name, h.channel_account_id, COALESCE(ch.code,''),
		       to_char(h.run_at_time, 'HH24:MI'), h.timezone, h.weekdays,
		       h.scope, h.active, h.last_run_at, h.next_run_at
		FROM sync_schedules h
		LEFT JOIN channel_accounts a ON a.id = h.channel_account_id
		LEFT JOIN channels ch ON ch.id = a.channel_id
		ORDER BY h.run_at_time, h.id`)
	if err != nil {
		return nil, fmt.Errorf("listando horarios: %w", err)
	}
	defer filas.Close()

	var out []Horario
	for filas.Next() {
		var h Horario
		if err := filas.Scan(&h.ID, &h.Nombre, &h.CuentaID, &h.Canal, &h.Hora,
			&h.Zona, &h.Dias, &h.Alcance, &h.Activo, &h.UltimaAt, &h.ProximaAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, filas.Err()
}

// GuardarHorario crea o actualiza una tarea programada y calcula su próxima
// ejecución.
func (s *Store) GuardarHorario(ctx context.Context, h Horario) (int64, error) {
	if _, err := time.LoadLocation(h.Zona); err != nil {
		return 0, fmt.Errorf("zona horaria inválida: %s", h.Zona)
	}
	if _, err := time.Parse("15:04", h.Hora); err != nil {
		return 0, fmt.Errorf("hora inválida: %s (formato HH:MM)", h.Hora)
	}
	switch h.Alcance {
	case "full", "price", "stock":
	default:
		return 0, fmt.Errorf("alcance inválido: %s", h.Alcance)
	}
	if h.Dias == nil {
		h.Dias = []int16{}
	}

	proxima, err := ProximaEjecucion(h, time.Now())
	if err != nil {
		return 0, err
	}
	var cuenta any
	if h.CuentaID != nil {
		cuenta = *h.CuentaID
	}

	if h.ID != 0 {
		_, err := s.pool.Exec(ctx, `
			UPDATE sync_schedules
			SET name = $2, channel_account_id = $3, run_at_time = $4::time,
			    timezone = $5, weekdays = $6, scope = $7, active = $8,
			    next_run_at = $9, updated_at = now()
			WHERE id = $1`,
			h.ID, h.Nombre, cuenta, h.Hora, h.Zona, h.Dias, h.Alcance, h.Activo, proxima)
		return h.ID, err
	}

	var id int64
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sync_schedules
		    (name, channel_account_id, run_at_time, timezone, weekdays, scope, active, next_run_at)
		VALUES ($1,$2,$3::time,$4,$5,$6,$7,$8) RETURNING id`,
		h.Nombre, cuenta, h.Hora, h.Zona, h.Dias, h.Alcance, h.Activo, proxima).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("creando el horario: %w", err)
	}
	return id, nil
}

func (s *Store) BorrarHorario(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sync_schedules WHERE id = $1`, id)
	return err
}

// HorariosVencidos devuelve las tareas cuyo momento ya pasó.
//
// El planificador las reclama una a una y avanza next_run_at ANTES de
// ejecutar: si el proceso muere a mitad, la tarea no se repite en bucle.
func (s *Store) HorariosVencidos(ctx context.Context) ([]Horario, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT h.id, h.name, h.channel_account_id, COALESCE(ch.code,''),
		       to_char(h.run_at_time, 'HH24:MI'), h.timezone, h.weekdays,
		       h.scope, h.active, h.last_run_at, h.next_run_at
		FROM sync_schedules h
		LEFT JOIN channel_accounts a ON a.id = h.channel_account_id
		LEFT JOIN channels ch ON ch.id = a.channel_id
		WHERE h.active AND h.next_run_at IS NOT NULL AND h.next_run_at <= now()
		ORDER BY h.next_run_at`)
	if err != nil {
		return nil, fmt.Errorf("buscando horarios vencidos: %w", err)
	}
	defer filas.Close()

	var out []Horario
	for filas.Next() {
		var h Horario
		if err := filas.Scan(&h.ID, &h.Nombre, &h.CuentaID, &h.Canal, &h.Hora,
			&h.Zona, &h.Dias, &h.Alcance, &h.Activo, &h.UltimaAt, &h.ProximaAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, filas.Err()
}

// MarcarHorarioEjecutado avanza la marca y programa la siguiente vez.
func (s *Store) MarcarHorarioEjecutado(ctx context.Context, h Horario) error {
	proxima, err := ProximaEjecucion(h, time.Now())
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE sync_schedules SET last_run_at = now(), next_run_at = $2, updated_at = now()
		WHERE id = $1`, h.ID, proxima)
	return err
}

// ProximaEjecucion calcula cuándo toca la siguiente corrida.
//
// Se resuelve en la zona horaria del horario, no en la del servidor: una tarea
// "a las 2 de la mañana" tiene que correr a las 2 en Bogotá aunque el servidor
// esté en Fráncfort.
func ProximaEjecucion(h Horario, desde time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(h.Zona)
	if err != nil {
		return time.Time{}, fmt.Errorf("zona horaria inválida: %s", h.Zona)
	}
	hm, err := time.Parse("15:04", h.Hora)
	if err != nil {
		return time.Time{}, fmt.Errorf("hora inválida: %s", h.Hora)
	}

	local := desde.In(loc)
	cand := time.Date(local.Year(), local.Month(), local.Day(),
		hm.Hour(), hm.Minute(), 0, 0, loc)
	if !cand.After(local) {
		cand = cand.AddDate(0, 0, 1)
	}
	// Se buscan hasta ocho días para cubrir cualquier combinación de días.
	for i := 0; i < 8; i++ {
		if diaPermitido(cand, h.Dias) {
			return cand.UTC(), nil
		}
		cand = cand.AddDate(0, 0, 1)
	}
	return time.Time{}, fmt.Errorf("el horario %q no tiene ningún día válido", h.Nombre)
}

// diaPermitido usa 1=lunes … 7=domingo (ISO), no la numeración de Go, que
// empieza en domingo=0 y confunde a quien configura desde la interfaz.
func diaPermitido(t time.Time, dias []int16) bool {
	if len(dias) == 0 {
		return true
	}
	iso := int16(t.Weekday())
	if iso == 0 {
		iso = 7
	}
	for _, d := range dias {
		if d == iso {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ alertas

type Alerta struct {
	ID        int64      `json:"id"`
	Tipo      string     `json:"tipo"`
	Severidad string     `json:"severidad"`
	Canal     string     `json:"canal"`
	Mensaje   string     `json:"mensaje"`
	CreadaAt  time.Time  `json:"creada_at"`
	VistaAt   *time.Time `json:"vista_at"`
}

// CrearAlerta registra un aviso operativo.
//
// Deduplica por (tipo, cuenta, mensaje) mientras siga sin reconocerse: un
// token vencido genera un fallo por minuto, y no tiene sentido llenar la
// bandeja con el mismo aviso mil veces.
func (s *Store) CrearAlerta(ctx context.Context, tipo, severidad string, cuentaID *int64, mensaje string, detalle any) error {
	var cuenta any
	if cuentaID != nil {
		cuenta = *cuentaID
	}
	var det []byte
	if detalle != nil {
		var err error
		if det, err = json.Marshal(detalle); err != nil {
			return err
		}
	}

	var existe int64
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM alerts
		WHERE kind = $1 AND message = $2 AND acknowledged_at IS NULL
		  AND channel_account_id IS NOT DISTINCT FROM $3
		LIMIT 1`, tipo, mensaje, cuenta).Scan(&existe)
	if err == nil {
		return nil // ya está abierta
	}
	if err != pgx.ErrNoRows {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO alerts (kind, severity, channel_account_id, message, detail)
		VALUES ($1,$2,$3,$4,$5)`, tipo, severidad, cuenta, mensaje, det)
	if err != nil {
		return fmt.Errorf("creando alerta: %w", err)
	}
	return nil
}

func (s *Store) ListarAlertas(ctx context.Context, incluirVistas bool) ([]Alerta, error) {
	cond := "WHERE a.acknowledged_at IS NULL"
	if incluirVistas {
		cond = ""
	}
	filas, err := s.pool.Query(ctx, `
		SELECT a.id, a.kind, a.severity, COALESCE(ch.code,''), a.message,
		       a.created_at, a.acknowledged_at
		FROM alerts a
		LEFT JOIN channel_accounts ca ON ca.id = a.channel_account_id
		LEFT JOIN channels ch ON ch.id = ca.channel_id `+cond+`
		ORDER BY a.created_at DESC LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("listando alertas: %w", err)
	}
	defer filas.Close()

	var out []Alerta
	for filas.Next() {
		var a Alerta
		if err := filas.Scan(&a.ID, &a.Tipo, &a.Severidad, &a.Canal,
			&a.Mensaje, &a.CreadaAt, &a.VistaAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, filas.Err()
}

// ReconocerAlerta la marca como vista para que deje de contar y pueda volver
// a dispararse si el problema reaparece.
func (s *Store) ReconocerAlerta(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE alerts SET acknowledged_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) ContarAlertas(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM alerts WHERE acknowledged_at IS NULL`).Scan(&n)
	return n, err
}
