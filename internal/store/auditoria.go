package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// RegistroAuditoria representa una fila en audit_logs.
type RegistroAuditoria struct {
	ID        int64           `json:"id"`
	UserID    *int64          `json:"user_id,omitempty"`
	UserName  *string         `json:"user_name,omitempty"`
	Action    string          `json:"action"`
	Entity    string          `json:"entity"`
	EntityID  *string         `json:"entity_id,omitempty"`
	Before    json.RawMessage `json:"before,omitempty"`
	After     json.RawMessage `json:"after,omitempty"`
	IP        *string         `json:"ip,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// FiltroAuditoria define parámetros de búsqueda para los logs.
type FiltroAuditoria struct {
	Entity   string
	EntityID string
	UserID   *int64
	Limite   int
	Offset   int
}

// RegistrarAuditoria inserta un nuevo evento en audit_logs.
func (s *Store) RegistrarAuditoria(ctx context.Context, userID *int64, action, entity, entityID string, before, after any, ip string) error {
	var beforeJSON, afterJSON []byte
	var err error

	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return fmt.Errorf("serializando before: %w", err)
		}
	}
	if after != nil {
		afterJSON, err = json.Marshal(after)
		if err != nil {
			return fmt.Errorf("serializando after: %w", err)
		}
	}

	var ipParam *string
	if ip != "" {
		ipParam = &ip
	}
	var entityIDParam *string
	if entityID != "" {
		entityIDParam = &entityID
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_logs (user_id, action, entity, entity_id, before, after, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7::inet)`,
		userID, action, entity, entityIDParam, beforeJSON, afterJSON, ipParam)
	return err
}

// ListarAuditoria consulta los registros de auditoría ordenados del más reciente al más antiguo.
func (s *Store) ListarAuditoria(ctx context.Context, f FiltroAuditoria) ([]RegistroAuditoria, int, error) {
	if f.Limite <= 0 {
		f.Limite = 50
	}

	var total int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_logs a
		WHERE ($1 = '' OR a.entity = $1)
		  AND ($2 = '' OR a.entity_id = $2)
		  AND ($3::bigint IS NULL OR a.user_id = $3)`,
		f.Entity, f.EntityID, f.UserID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("contando auditoría: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.user_id, u.name, a.action, a.entity, a.entity_id,
		       a.before, a.after, host(a.ip), a.created_at
		FROM audit_logs a
		LEFT JOIN users u ON u.id = a.user_id
		WHERE ($1 = '' OR a.entity = $1)
		  AND ($2 = '' OR a.entity_id = $2)
		  AND ($3::bigint IS NULL OR a.user_id = $3)
		ORDER BY a.created_at DESC
		LIMIT $4 OFFSET $5`,
		f.Entity, f.EntityID, f.UserID, f.Limite, f.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("consultando auditoría: %w", err)
	}
	defer rows.Close()

	var out []RegistroAuditoria
	for rows.Next() {
		var a RegistroAuditoria
		var bJSON, aJSON []byte
		if err := rows.Scan(&a.ID, &a.UserID, &a.UserName, &a.Action, &a.Entity, &a.EntityID,
			&bJSON, &aJSON, &a.IP, &a.CreatedAt); err != nil {
			return nil, 0, err
		}
		if len(bJSON) > 0 {
			a.Before = json.RawMessage(bJSON)
		}
		if len(aJSON) > 0 {
			a.After = json.RawMessage(aJSON)
		}
		out = append(out, a)
	}

	return out, total, rows.Err()
}
