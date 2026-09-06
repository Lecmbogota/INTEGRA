package store

import (
	"context"
	"fmt"
	"math"
)

// Canal es la configuración comercial de un canal de venta.
type Canal struct {
	Codigo      string  `json:"codigo"`
	Nombre      string  `json:"nombre"`
	ComisionPct float64 `json:"comision_pct"`
	CostoFijo   float64 `json:"costo_fijo"`
}

func (s *Store) Canales(ctx context.Context) ([]Canal, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT code, name, comision_pct, costo_fijo FROM channels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("listando canales: %w", err)
	}
	defer filas.Close()

	var out []Canal
	for filas.Next() {
		var c Canal
		if err := filas.Scan(&c.Codigo, &c.Nombre, &c.ComisionPct, &c.CostoFijo); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, filas.Err()
}

// ActualizarCanal fija la comisión y el costo fijo de un canal.
func (s *Store) ActualizarCanal(ctx context.Context, codigo string, comisionPct, costoFijo float64) error {
	if comisionPct < 0 || comisionPct >= 100 {
		return fmt.Errorf("la comisión debe estar entre 0 y 99.99")
	}
	if costoFijo < 0 {
		return fmt.Errorf("el costo fijo no puede ser negativo")
	}
	et, err := s.pool.Exec(ctx, `
		UPDATE channels SET comision_pct = $2, costo_fijo = $3 WHERE code = $1`,
		codigo, comisionPct, costoFijo)
	if err != nil {
		return fmt.Errorf("guardando el canal: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe el canal %q", codigo)
	}
	return nil
}

// PrecioParaCanal compensa la comisión del canal: lo que queda tras aplicarla
// al precio publicado es (aproximadamente) el precio base definido en Integra.
//
// Se redondea hacia arriba al centenar de pesos: los precios con cola rara
// ($4.119.483) delatan un cálculo automático y en COP nadie los usa.
func PrecioParaCanal(base float64, c Canal) float64 {
	if base <= 0 {
		return base
	}
	bruto := (base + c.CostoFijo) / (1 - c.ComisionPct/100)
	return math.Ceil(bruto/100) * 100
}
