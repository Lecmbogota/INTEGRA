package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FilaPlantilla es una línea del archivo de actualización masiva.
//
// Lleva el estado actual —precio y promoción vigente— porque una plantilla que
// llega vacía obliga a consultar cada producto en pantalla antes de escribir
// nada. Con el valor actual al lado, la hoja se revisa como una lista de
// precios y solo se tocan las casillas que cambian.
type FilaPlantilla struct {
	VarianteID int64
	SKU        string
	Nombre     string
	Marca      string
	Precio     *float64
	Stock      float64

	// Promoción vigente o programada, si la hay.
	PromoCanal   string
	PromoPrecio  *float64
	PromoInicia  *time.Time
	PromoTermina *time.Time
}

// FilasParaPlantilla devuelve el catálogo entero que cumple el filtro.
//
// A diferencia de ListarProductos no pagina: una plantilla que se corta a las
// 500 filas es peor que ninguna, porque el operador no tiene forma de notar
// que le faltan productos hasta que alguien pregunta por qué no se actualizó
// uno.
func (s *Store) FilasParaPlantilla(ctx context.Context, f FiltroProductos) ([]FilaPlantilla, error) {
	// Las condiciones son las mismas que ListarProductos, y a propósito: la
	// plantilla tiene que traer EXACTAMENTE lo que el operador está viendo en
	// pantalla. Si aquí se filtrara distinto, descargaría un archivo con
	// productos que no pidió —o sin los que sí— sin que nada se lo advierta.
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
	// Por code, no por name: es lo que manda el desplegable de la pantalla.
	if m := strings.TrimSpace(f.Marca); m != "" {
		args = append(args, m)
		cond = append(cond, fmt.Sprintf("b.code = $%d", len(args)))
	}
	if c := strings.TrimSpace(f.Categoria); c != "" {
		args = append(args, c)
		cond = append(cond, fmt.Sprintf("p.categ_path = $%d", len(args)))
	}
	if f.SoloProblemas {
		cond = append(cond,
			"EXISTS (SELECT 1 FROM attention_queue a WHERE a.variant_id = v.id AND a.resolved_at IS NULL)")
	}
	if f.SoloSinPrecio {
		cond = append(cond, "v.price IS NULL")
	}

	// La promoción que se muestra es la más próxima a estar activa: primero la
	// vigente, y si no hay, la siguiente programada. Las terminadas no salen —
	// reimprimir una promoción del mes pasado en una plantilla de trabajo solo
	// invita a reactivarla sin querer.
	consulta := `
		SELECT v.id, COALESCE(v.sku, ''), p.name, COALESCE(b.name, ''),
		       v.price,
		       COALESCE((SELECT sum(st.qty_on_hand) FROM variant_stock st
		                 WHERE st.variant_id = v.id), 0),
		       COALESCE(ch.code, ''), o.offer_price, o.starts_at, o.ends_at
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN LATERAL (
		    SELECT o.* FROM offers o
		    WHERE o.variant_id = v.id AND o.active
		      AND (o.ends_at IS NULL OR o.ends_at > now())
		    ORDER BY (o.starts_at <= now()) DESC, o.starts_at
		    LIMIT 1
		) o ON TRUE
		LEFT JOIN channel_accounts a ON a.id = o.channel_account_id
		LEFT JOIN channels ch ON ch.id = a.channel_id
		WHERE ` + strings.Join(cond, " AND ") + `
		ORDER BY COALESCE(b.name, ''), p.name`

	filas, err := s.pool.Query(ctx, consulta, args...)
	if err != nil {
		return nil, fmt.Errorf("leyendo el catálogo para la plantilla: %w", err)
	}
	defer filas.Close()

	var out []FilaPlantilla
	for filas.Next() {
		var f FilaPlantilla
		if err := filas.Scan(&f.VarianteID, &f.SKU, &f.Nombre, &f.Marca,
			&f.Precio, &f.Stock, &f.PromoCanal, &f.PromoPrecio,
			&f.PromoInicia, &f.PromoTermina); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, filas.Err()
}

// VarianteResuelta es lo que hace falta saber de un SKU para validar su fila.
type VarianteResuelta struct {
	ID     int64
	Nombre string
	Precio *float64
}

// ResolverSKUs traduce los SKU de una plantilla en una sola consulta.
//
// Uno por uno serían cientos de idas y vueltas a la base para un archivo
// mediano, y la validación tiene que ser rápida: el operador está esperando
// delante de la pantalla a que le digan si su hoja está bien.
//
// El precio actual viene en el mismo viaje porque se necesita para comprobar
// que la promoción rebaja de verdad, incluso en las filas donde el operador no
// tocó el precio de venta.
func (s *Store) ResolverSKUs(ctx context.Context, skus []string) (map[string]VarianteResuelta, error) {
	if len(skus) == 0 {
		return map[string]VarianteResuelta{}, nil
	}
	filas, err := s.pool.Query(ctx, `
		SELECT upper(btrim(v.sku)), v.id, p.name, v.price
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		WHERE v.active AND upper(btrim(v.sku)) = ANY($1)`, normalizarSKUs(skus))
	if err != nil {
		return nil, fmt.Errorf("resolviendo los SKU de la plantilla: %w", err)
	}
	defer filas.Close()

	out := map[string]VarianteResuelta{}
	for filas.Next() {
		var sku string
		var v VarianteResuelta
		if err := filas.Scan(&sku, &v.ID, &v.Nombre, &v.Precio); err != nil {
			return nil, err
		}
		out[sku] = v
	}
	return out, filas.Err()
}

// ClaveSKU normaliza un SKU igual que ResolverSKUs, para poder buscar en el
// mapa que devuelve.
func ClaveSKU(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// CuentasPorCanal mapea el código de canal a la cuenta conectada.
//
// La plantilla pide un canal ("mercadolibre"), no un identificador de cuenta:
// nadie va a escribir un número en una hoja de cálculo, y si un canal tiene
// más de una cuenta conectada eso se resuelve aquí y no en el archivo.
func (s *Store) CuentasPorCanal(ctx context.Context) (map[string]int64, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT ch.code, a.id
		FROM channel_accounts a
		JOIN channels ch ON ch.id = a.channel_id
		WHERE a.active
		ORDER BY a.id`)
	if err != nil {
		return nil, fmt.Errorf("listando cuentas por canal: %w", err)
	}
	defer filas.Close()

	out := map[string]int64{}
	for filas.Next() {
		var code string
		var id int64
		if err := filas.Scan(&code, &id); err != nil {
			return nil, err
		}
		// Si hay varias cuentas del mismo canal se queda la primera: el orden
		// por id la hace estable entre cargas, que es lo que importa para que
		// dos plantillas iguales produzcan el mismo resultado.
		if _, ya := out[code]; !ya {
			out[code] = id
		}
	}
	return out, filas.Err()
}

func normalizarSKUs(skus []string) []string {
	out := make([]string, 0, len(skus))
	for _, s := range skus {
		out = append(out, strings.ToUpper(strings.TrimSpace(s)))
	}
	return out
}
