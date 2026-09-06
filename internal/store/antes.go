package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Las consultas de este archivo existen para una sola cosa: leer el valor
// anterior justo antes de pisarlo, que es lo que convierte el registro de
// auditoría en algo que sirve para resolver una discusión. Un «alguien tocó el
// precio» sin el número de antes no responde a «¿de cuánto a cuánto?».

// EstadoVariante es la foto de lo auditable de una variante: su precio y si su
// producto está excluido del catálogo publicable.
type EstadoVariante struct {
	Precio   *float64 `json:"precio"`
	Excluido bool     `json:"excluido"`
}

// EstadoPrevioVariante lee precio y exclusión antes de una edición.
func (s *Store) EstadoPrevioVariante(ctx context.Context, varianteID int64) (EstadoVariante, error) {
	var e EstadoVariante
	err := s.pool.QueryRow(ctx, `
		SELECT v.price, p.excluded_reason IS NOT NULL
		FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = $1`, varianteID).Scan(&e.Precio, &e.Excluido)
	if err == pgx.ErrNoRows {
		return e, fmt.Errorf("no existe la variante %d", varianteID)
	}
	return e, err
}

// ReglaPrecioPorID devuelve una regla concreta, activa o no: al editarla hay
// que poder registrar cómo estaba, y ReglasPrecioDeCuenta solo trae las
// activas.
func (s *Store) ReglaPrecioPorID(ctx context.Context, id int64) (*ReglaPrecioCanal, error) {
	var r ReglaPrecioCanal
	err := s.pool.QueryRow(ctx, `
		SELECT id, channel_account_id, brand_id, categ_path_prefix, adjustment_type,
		       adjustment_value, round_to, min_margin_percent, priority, active,
		       created_at, updated_at
		FROM channel_price_rules WHERE id = $1`, id).Scan(
		&r.ID, &r.ChannelAccountID, &r.BrandID, &r.CategPathPrefix, &r.AdjustmentType,
		&r.AdjustmentValue, &r.RoundTo, &r.MinMarginPercent, &r.Priority, &r.Active,
		&r.CreatedAt, &r.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultando la regla de precio %d: %w", id, err)
	}
	return &r, nil
}

// OverridePrecioDe devuelve el precio manual vigente de una variante en una
// cuenta, o nil si no hay ninguno.
func (s *Store) OverridePrecioDe(ctx context.Context, varianteID, cuentaID int64) (*OverridePrecio, error) {
	var o OverridePrecio
	err := s.pool.QueryRow(ctx, `
		SELECT id, variant_id, channel_account_id, price, COALESCE(reason,''), created_by, created_at
		FROM price_overrides WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID).Scan(&o.ID, &o.VariantID, &o.ChannelAccountID, &o.Price,
		&o.Reason, &o.CreatedBy, &o.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultando el override de la variante %d: %w", varianteID, err)
	}
	return &o, nil
}

// OfertaPorID devuelve una promoción concreta.
func (s *Store) OfertaPorID(ctx context.Context, id int64) (*OfertaCanal, error) {
	var o OfertaCanal
	err := s.pool.QueryRow(ctx, `
		SELECT id, variant_id, channel_account_id, offer_price, starts_at, ends_at,
		       applied_at, reverted_at, active, created_by, created_at
		FROM offers WHERE id = $1`, id).Scan(&o.ID, &o.VariantID, &o.ChannelAccountID,
		&o.OfferPrice, &o.StartsAt, &o.EndsAt, &o.AppliedAt, &o.RevertedAt, &o.Active,
		&o.CreatedBy, &o.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultando la oferta %d: %w", id, err)
	}
	return &o, nil
}

// CanalPorCodigo devuelve la configuración comercial de un canal. La comisión
// entra directa en el precio publicado, así que cambiarla es un cambio de
// precio de todo el canal y se audita como tal.
func (s *Store) CanalPorCodigo(ctx context.Context, codigo string) (*Canal, error) {
	var c Canal
	err := s.pool.QueryRow(ctx, `
		SELECT code, name, comision_pct, costo_fijo FROM channels WHERE code = $1`,
		codigo).Scan(&c.Codigo, &c.Nombre, &c.ComisionPct, &c.CostoFijo)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consultando el canal %q: %w", codigo, err)
	}
	return &c, nil
}
