package store

import (
	"context"
	"fmt"
	"time"
)

// RegistroLlamadaAPI representa una fila en channel_api_calls.
type RegistroLlamadaAPI struct {
	ID                 int64      `json:"id"`
	ChannelAccountID   *int64     `json:"channel_account_id,omitempty"`
	CanalCodigo        *string    `json:"canal_codigo,omitempty"`
	SyncRunID          *int64     `json:"sync_run_id,omitempty"`
	Method             string     `json:"method"`
	Endpoint           string     `json:"endpoint"`
	StatusCode         int        `json:"status_code"`
	DurationMS         int        `json:"duration_ms"`
	RequestBody        *string    `json:"request_body,omitempty"`
	ResponseBody       *string    `json:"response_body,omitempty"`
	Error              *string    `json:"error,omitempty"`
	RateLimitRemaining *int       `json:"rate_limit_remaining,omitempty"`
	RateLimitResetAt   *time.Time `json:"rate_limit_reset_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

// RegistrarLlamadaAPI guarda el registro de una petición HTTP realizada a un canal.
func (s *Store) RegistrarLlamadaAPI(
	ctx context.Context,
	cuentaID *int64,
	syncRunID *int64,
	method, endpoint string,
	statusCode, durationMS int,
	reqBody, respBody, errStr string,
	rateRemaining *int,
	rateResetAt *time.Time,
) error {
	var reqParam, respParam, errParam *string
	if reqBody != "" {
		reqParam = &reqBody
	}
	if respBody != "" {
		respParam = &respBody
	}
	if errStr != "" {
		errParam = &errStr
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO channel_api_calls
		    (channel_account_id, sync_run_id, method, endpoint, status_code,
		     duration_ms, request_body, response_body, error, rate_limit_remaining, rate_limit_reset_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		cuentaID, syncRunID, method, endpoint, statusCode,
		durationMS, reqParam, respParam, errParam, rateRemaining, rateResetAt)
	return err
}

// LlamadasRecientesAPI consulta el historial de llamadas a las APIs de los canales.
func (s *Store) LlamadasRecientesAPI(ctx context.Context, cuentaID *int64, soloErrores bool, limite int) ([]RegistroLlamadaAPI, error) {
	if limite <= 0 {
		limite = 100
	}

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.channel_account_id, ch.code, c.sync_run_id, c.method, c.endpoint,
		       c.status_code, c.duration_ms, c.request_body, c.response_body, c.error,
		       c.rate_limit_remaining, c.rate_limit_reset_at, c.created_at
		FROM channel_api_calls c
		LEFT JOIN channel_accounts a ON a.id = c.channel_account_id
		LEFT JOIN channels ch ON ch.id = a.channel_id
		WHERE ($1::bigint IS NULL OR c.channel_account_id = $1)
		  AND (NOT $2::bool OR c.status_code >= 400 OR c.error IS NOT NULL)
		ORDER BY c.created_at DESC
		LIMIT $3`, cuentaID, soloErrores, limite)
	if err != nil {
		return nil, fmt.Errorf("consultando llamadas API: %w", err)
	}
	defer rows.Close()

	var out []RegistroLlamadaAPI
	for rows.Next() {
		var r RegistroLlamadaAPI
		if err := rows.Scan(&r.ID, &r.ChannelAccountID, &r.CanalCodigo, &r.SyncRunID,
			&r.Method, &r.Endpoint, &r.StatusCode, &r.DurationMS,
			&r.RequestBody, &r.ResponseBody, &r.Error,
			&r.RateLimitRemaining, &r.RateLimitResetAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
