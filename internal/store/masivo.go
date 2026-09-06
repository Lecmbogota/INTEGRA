package store

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Edición masiva: aplicar un cambio a muchos productos de una vez.
//
// Existe por un problema muy concreto: 257 productos del catálogo de MDV no
// tienen precio, y ponerlos uno a uno son horas de clics. Con «precio = coste
// × 1,35» se resuelven 157 en una operación.

// OperacionMasiva describe qué hacer con la selección.
type OperacionMasiva struct {
	// Tipo es la operación. Ver las constantes de abajo.
	Tipo string `json:"tipo"`
	// Factor multiplica el coste o el precio, según la operación.
	Factor float64 `json:"factor"`
	// Valor es el precio fijo o el texto (marca) según la operación.
	Valor string `json:"valor"`
	// Redondeo aplica precio comercial: 0 = ninguno, 100 = al centenar,
	// 900 = terminar en 900 (el clásico $ 119.900).
	Redondeo int `json:"redondeo"`
	// Simular calcula el efecto sin escribir nada.
	Simular bool `json:"simular"`
}

// Tipos de operación.
const (
	MasivoPrecioDesdeCoste = "precio_desde_coste" // precio = coste × Factor
	MasivoPrecioFijo       = "precio_fijo"        // precio = Valor
	MasivoPrecioAjustar    = "precio_ajustar"     // precio = precio × Factor
	MasivoBorrarPrecio     = "precio_borrar"
	MasivoMarca            = "marca"
	MasivoExcluir          = "excluir"
	MasivoIncluir          = "incluir"
)

// ResultadoMasivo resume el efecto de la operación.
type ResultadoMasivo struct {
	Afectados int            `json:"afectados"`
	Omitidos  int            `json:"omitidos"`
	Motivo    string         `json:"motivo_omision"`
	Muestra   []CambioMuestra `json:"muestra"`
	Simulado  bool           `json:"simulado"`
}

// CambioMuestra enseña unos pocos cambios concretos, para que quien pulsa el
// botón vea qué va a pasar antes de que pase.
type CambioMuestra struct {
	SKU    string  `json:"sku"`
	Nombre string  `json:"nombre"`
	Antes  *float64 `json:"antes"`
	Despues *float64 `json:"despues"`
}

// EditarMasivo aplica una operación a las variantes indicadas.
//
// Siempre calcula el efecto primero y solo escribe si Simular es falso: la
// misma llamada sirve para la vista previa y para la ejecución, así que lo
// que se previsualiza es exactamente lo que se aplica.
func (s *Store) EditarMasivo(ctx context.Context, ids []int64, op OperacionMasiva) (*ResultadoMasivo, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no hay ningún producto seleccionado")
	}
	if len(ids) > 2000 {
		return nil, fmt.Errorf("demasiados productos de una vez (%d, máximo 2000)", len(ids))
	}

	switch op.Tipo {
	case MasivoPrecioDesdeCoste, MasivoPrecioAjustar:
		if op.Factor <= 0 {
			return nil, fmt.Errorf("el factor debe ser mayor que cero")
		}
	case MasivoPrecioFijo:
		if op.Valor == "" {
			return nil, fmt.Errorf("falta el precio")
		}
	case MasivoMarca:
		// El valor vacío quita la marca, que es una operación legítima.
	case MasivoBorrarPrecio, MasivoExcluir, MasivoIncluir:
	default:
		return nil, fmt.Errorf("operación desconocida: %s", op.Tipo)
	}

	switch op.Tipo {
	case MasivoPrecioDesdeCoste, MasivoPrecioFijo, MasivoPrecioAjustar, MasivoBorrarPrecio:
		return s.masivoPrecio(ctx, ids, op)
	case MasivoMarca:
		return s.masivoMarca(ctx, ids, op)
	default:
		return s.masivoExclusion(ctx, ids, op)
	}
}

func (s *Store) masivoPrecio(ctx context.Context, ids []int64, op OperacionMasiva) (*ResultadoMasivo, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT v.id, COALESCE(v.sku,''), p.name, v.price, COALESCE(v.cost,0)
		FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1) ORDER BY p.name`, ids)
	if err != nil {
		return nil, fmt.Errorf("leyendo la selección: %w", err)
	}
	defer filas.Close()

	type cambio struct {
		id     int64
		sku    string
		nombre string
		antes  *float64
		nuevo  *float64
	}
	var cambios []cambio
	res := &ResultadoMasivo{Simulado: op.Simular}

	for filas.Next() {
		var c cambio
		var coste float64
		if err := filas.Scan(&c.id, &c.sku, &c.nombre, &c.antes, &coste); err != nil {
			return nil, err
		}

		switch op.Tipo {
		case MasivoPrecioDesdeCoste:
			// Sin coste no hay nada que multiplicar: se omite en vez de poner
			// cero, que sería un precio de venta catastrófico.
			if coste <= 0 {
				res.Omitidos++
				res.Motivo = "sin coste registrado en Odoo"
				continue
			}
			v := redondear(coste*op.Factor, op.Redondeo)
			c.nuevo = &v
		case MasivoPrecioFijo:
			v, err := aFloat(op.Valor)
			if err != nil {
				return nil, err
			}
			v = redondear(v, op.Redondeo)
			c.nuevo = &v
		case MasivoPrecioAjustar:
			if c.antes == nil {
				res.Omitidos++
				res.Motivo = "aún no tienen precio que ajustar"
				continue
			}
			v := redondear(*c.antes*op.Factor, op.Redondeo)
			c.nuevo = &v
		case MasivoBorrarPrecio:
			c.nuevo = nil
		}
		cambios = append(cambios, c)
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}

	res.Afectados = len(cambios)
	for i, c := range cambios {
		if i >= 5 {
			break
		}
		res.Muestra = append(res.Muestra, CambioMuestra{
			SKU: c.sku, Nombre: c.nombre, Antes: c.antes, Despues: c.nuevo,
		})
	}
	if op.Simular {
		return res, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, c := range cambios {
		if _, err := tx.Exec(ctx, `
			UPDATE product_variants
			SET price = $2, price_updated_at = now(), updated_at = now()
			WHERE id = $1`, c.id, c.nuevo); err != nil {
			return nil, fmt.Errorf("actualizando %s: %w", c.sku, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// El precio cambia lo que bloquea la publicación: se recalcula la cola.
	if err := s.RecalcularAtencion(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Store) masivoMarca(ctx context.Context, ids []int64, op OperacionMasiva) (*ResultadoMasivo, error) {
	res := &ResultadoMasivo{Simulado: op.Simular}
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM product_variants WHERE id = ANY($1)`, ids).Scan(&res.Afectados)
	if err != nil {
		return nil, err
	}
	if op.Simular {
		return res, nil
	}

	marcaID, err := s.ResolverMarca(ctx, op.Valor)
	if err != nil {
		return nil, err
	}
	var marca any
	if marcaID != 0 {
		marca = marcaID
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE products SET brand_id = $2, brand_raw = $3, updated_at = now()
		WHERE id IN (SELECT product_id FROM product_variants WHERE id = ANY($1))`,
		ids, marca, nulo(op.Valor))
	if err != nil {
		return nil, fmt.Errorf("aplicando la marca: %w", err)
	}
	return res, s.RecalcularAtencion(ctx)
}

func (s *Store) masivoExclusion(ctx context.Context, ids []int64, op OperacionMasiva) (*ResultadoMasivo, error) {
	res := &ResultadoMasivo{Simulado: op.Simular}
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM product_variants WHERE id = ANY($1)`, ids).Scan(&res.Afectados)
	if err != nil {
		return nil, err
	}
	if op.Simular {
		return res, nil
	}

	var motivo any
	if op.Tipo == MasivoExcluir {
		motivo = "manual"
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE products SET excluded_reason = $2, updated_at = now()
		WHERE id IN (SELECT product_id FROM product_variants WHERE id = ANY($1))`,
		ids, motivo)
	if err != nil {
		return nil, fmt.Errorf("aplicando la exclusión: %w", err)
	}
	return res, s.RecalcularAtencion(ctx)
}

// IDsDeFiltro devuelve todos los identificadores que cumplen un filtro, para
// poder aplicar una operación a «toda la búsqueda» y no solo a la página
// visible — que es donde estaba el trabajo tedioso de verdad.
func (s *Store) IDsDeFiltro(ctx context.Context, f FiltroProductos) ([]int64, error) {
	cond := []string{"v.active"}
	if f.VerExcluidos {
		cond = append(cond, "p.excluded_reason IS NOT NULL")
	} else {
		cond = append(cond, "p.active", "p.excluded_reason IS NULL")
	}
	args := []any{}

	if q := strings.TrimSpace(f.Busqueda); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		cond = append(cond, fmt.Sprintf(
			"(lower(p.name) LIKE $%d OR lower(COALESCE(v.sku,'')) LIKE $%d)", len(args), len(args)))
	}
	if m := strings.TrimSpace(f.Marca); m != "" {
		args = append(args, m)
		cond = append(cond, fmt.Sprintf("b.code = $%d", len(args)))
	}
	if f.SoloProblemas {
		cond = append(cond,
			"EXISTS (SELECT 1 FROM attention_queue a WHERE a.variant_id = v.id AND a.resolved_at IS NULL)")
	}
	if f.SoloSinPrecio {
		cond = append(cond, "v.price IS NULL")
	}

	filas, err := s.pool.Query(ctx, `
		SELECT v.id FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE `+strings.Join(cond, " AND ")+`
		ORDER BY p.name LIMIT 2000`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolviendo la selección: %w", err)
	}
	defer filas.Close()

	var out []int64
	for filas.Next() {
		var id int64
		if err := filas.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, filas.Err()
}

// redondear aplica precio comercial.
//
// Con 900 el precio termina en 900 hacia abajo ($ 119.900 y no $ 120.000):
// redondear hacia arriba subiría el precio sin que nadie lo decidiera, y en
// una operación masiva ese descuido se multiplica por cientos.
func redondear(v float64, modo int) float64 {
	if v <= 0 {
		return 0
	}
	switch {
	case modo <= 0:
		return math.Round(v*100) / 100
	case modo == 900:
		miles := math.Floor(v / 1000)
		if miles < 1 {
			return math.Round(v/100) * 100
		}
		return miles*1000 + 900
	default:
		return math.Round(v/float64(modo)) * float64(modo)
	}
}

func aFloat(s string) (float64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", "."))
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, fmt.Errorf("precio inválido: %q", s)
	}
	if v < 0 {
		return 0, fmt.Errorf("el precio no puede ser negativo")
	}
	return v, nil
}
