package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CuentaCanal es la cuenta (global, todas las marcas) de un canal de venta.
type CuentaCanal struct {
	ID          int64      `json:"id"`
	CanalCodigo string     `json:"canal"`
	CanalNombre string     `json:"canal_nombre"`
	Nombre      string     `json:"nombre"`
	Activa      bool       `json:"activa"`
	UltimoSync  *time.Time `json:"ultimo_sync"`
	// Resultado de la última prueba de conexión, guardado en config.
	ProbadaAt  *time.Time `json:"probada_at"`
	ProbadaOK  *bool      `json:"probada_ok"`
	ProbadaMsg string     `json:"probada_msg"`
	// Suelo de coste de la cuenta: margen mínimo exigido y si publicar por
	// debajo de él frena el envío o solo levanta el aviso.
	MinMargenPct float64 `json:"min_margen_pct"`
	Bloquear     bool    `json:"bloquear_bajo_costo"`
}

// GuardarCuenta crea o reemplaza la cuenta global de un canal. La credencial
// llega ya cifrada: este paquete nunca ve secretos en claro.
func (s *Store) GuardarCuenta(ctx context.Context, canalCodigo, nombre string, credencialCifrada []byte) (int64, error) {
	var canalID int64
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM channels WHERE code = $1`, canalCodigo).Scan(&canalID)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("no existe el canal %q", canalCodigo)
	}
	if err != nil {
		return 0, err
	}

	// Upsert manual: el índice parcial (brand_id IS NULL AND active) no sirve
	// como objetivo de ON CONFLICT.
	var id int64
	err = s.pool.QueryRow(ctx, `
		SELECT id FROM channel_accounts
		WHERE channel_id = $1 AND brand_id IS NULL AND active`, canalID).Scan(&id)
	switch err {
	case nil:
		_, err = s.pool.Exec(ctx, `
			UPDATE channel_accounts
			SET name = $2, credentials_enc = $3,
			    config = config - 'probada_at' - 'probada_ok' - 'probada_msg',
			    updated_at = now()
			WHERE id = $1`, id, nombre, credencialCifrada)
		if err != nil {
			return 0, fmt.Errorf("actualizando la cuenta: %w", err)
		}
		return id, nil
	case pgx.ErrNoRows:
		err = s.pool.QueryRow(ctx, `
			INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc)
			VALUES (NULL, $1, $2, $3) RETURNING id`,
			canalID, nombre, credencialCifrada).Scan(&id)
		if err != nil {
			return 0, fmt.Errorf("creando la cuenta: %w", err)
		}
		return id, nil
	default:
		return 0, err
	}
}

// ListarCuentas devuelve la cuenta global de cada canal (si existe).
func (s *Store) ListarCuentas(ctx context.Context) ([]CuentaCanal, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT a.id, ch.code, ch.name, a.name, a.active, a.last_sync_at,
		       a.config->>'probada_at', a.config->>'probada_ok', COALESCE(a.config->>'probada_msg',''),
		       a.min_margen_pct, a.bloquear_bajo_costo
		FROM channel_accounts a
		JOIN channels ch ON ch.id = a.channel_id
		WHERE a.brand_id IS NULL AND a.active
		ORDER BY ch.id`)
	if err != nil {
		return nil, fmt.Errorf("listando cuentas: %w", err)
	}
	defer filas.Close()

	var out []CuentaCanal
	for filas.Next() {
		var c CuentaCanal
		var probadaAt, probadaOK *string
		if err := filas.Scan(&c.ID, &c.CanalCodigo, &c.CanalNombre, &c.Nombre,
			&c.Activa, &c.UltimoSync, &probadaAt, &probadaOK, &c.ProbadaMsg,
			&c.MinMargenPct, &c.Bloquear); err != nil {
			return nil, err
		}
		if probadaAt != nil {
			if ts, err := time.Parse(time.RFC3339, *probadaAt); err == nil {
				c.ProbadaAt = &ts
			}
		}
		if probadaOK != nil {
			ok := *probadaOK == "true"
			c.ProbadaOK = &ok
		}
		out = append(out, c)
	}
	return out, filas.Err()
}

// SueloCosto es el margen mínimo de una cuenta y si ese suelo frena el envío.
type SueloCosto struct {
	MinMargenPct float64 `json:"min_margen_pct"`
	Bloquear     bool    `json:"bloquear_bajo_costo"`
}

// SueloCostoDeCuenta lee el suelo de coste configurado en una cuenta.
func (s *Store) SueloCostoDeCuenta(ctx context.Context, cuentaID int64) (SueloCosto, error) {
	var sc SueloCosto
	err := s.pool.QueryRow(ctx, `
		SELECT min_margen_pct, bloquear_bajo_costo FROM channel_accounts WHERE id = $1`,
		cuentaID).Scan(&sc.MinMargenPct, &sc.Bloquear)
	if err == pgx.ErrNoRows {
		return sc, fmt.Errorf("no existe la cuenta %d", cuentaID)
	}
	return sc, err
}

// ActualizarSueloCosto fija el margen mínimo de la cuenta y si publicar por
// debajo de él se frena o solo se avisa.
func (s *Store) ActualizarSueloCosto(ctx context.Context, cuentaID int64, sc SueloCosto) error {
	if sc.MinMargenPct < 0 || sc.MinMargenPct >= 1000 {
		return fmt.Errorf("el margen mínimo debe estar entre 0 y 999,99")
	}
	et, err := s.pool.Exec(ctx, `
		UPDATE channel_accounts
		SET min_margen_pct = $2, bloquear_bajo_costo = $3, updated_at = now()
		WHERE id = $1`, cuentaID, sc.MinMargenPct, sc.Bloquear)
	if err != nil {
		return fmt.Errorf("guardando el suelo de coste: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe la cuenta %d", cuentaID)
	}
	return nil
}

// CredencialesDeCuenta devuelve la credencial cifrada de una cuenta.
func (s *Store) CredencialesDeCuenta(ctx context.Context, id int64) (canalCodigo string, cifrada []byte, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT ch.code, a.credentials_enc
		FROM channel_accounts a JOIN channels ch ON ch.id = a.channel_id
		WHERE a.id = $1 AND a.active`, id).Scan(&canalCodigo, &cifrada)
	if err == pgx.ErrNoRows {
		err = fmt.Errorf("no existe la cuenta %d", id)
	}
	return
}

// ActualizarCredenciales reemplaza la credencial cifrada de una cuenta sin
// tocar nada más. Lo usan los canales que rotan tokens (MercadoLibre canjea
// el refresh token en cada uso); no borra el resultado de la última prueba
// porque la credencial sigue siendo la misma cuenta, solo renovada.
func (s *Store) ActualizarCredenciales(ctx context.Context, id int64, credencialCifrada []byte) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE channel_accounts SET credentials_enc = $2, updated_at = now()
		WHERE id = $1`, id, credencialCifrada)
	if err != nil {
		return fmt.Errorf("actualizando la credencial de la cuenta %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no existe la cuenta %d", id)
	}
	return nil
}

// CupoDeCuenta devuelve el ritmo máximo de llamadas al canal de esa cuenta.
//
// Se lee aparte de las credenciales porque no es un secreto y porque el
// limitador se comparte entre todos los adaptadores de la cuenta, mientras
// que las credenciales se releen en cada trabajo.
func (s *Store) CupoDeCuenta(ctx context.Context, id int64) (rps float64, burst int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT rate_limit_rps, rate_limit_burst FROM channel_accounts WHERE id = $1`,
		id).Scan(&rps, &burst)
	if err == pgx.ErrNoRows {
		return 0, 0, fmt.Errorf("no existe la cuenta %d", id)
	}
	return rps, burst, err
}

// AnotarPrueba guarda el resultado de la última prueba de conexión.
func (s *Store) AnotarPrueba(ctx context.Context, id int64, ok bool, msg string) error {
	extra, err := json.Marshal(map[string]any{
		"probada_at":  time.Now().Format(time.RFC3339),
		"probada_ok":  fmt.Sprint(ok),
		"probada_msg": msg,
	})
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE channel_accounts SET config = config || $2::jsonb, updated_at = now()
		WHERE id = $1`, id, extra)
	return err
}
