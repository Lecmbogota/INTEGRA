package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// DatosPreview reúne todo lo que hace falta para proyectar un producto hacia
// los canales: lo leído de Odoo, lo calculado por Integra y lo generado.
type DatosPreview struct {
	VarianteID  int64             `json:"variante_id"`
	SKU         string            `json:"sku"`
	Barcode     string            `json:"barcode"`
	NombreOdoo  string            `json:"nombre_odoo"`
	Marca       string            `json:"marca"`
	CategPath   string            `json:"categoria_odoo"`
	Titulos     map[string]string `json:"titulos"`
	Descripcion string            `json:"descripcion"`
	Specs       []struct {
		Clave string `json:"clave"`
		Valor string `json:"valor"`
	} `json:"specs"`
	Confianza string   `json:"confianza"`
	Avisos    []string `json:"avisos"`

	Precio float64 `json:"precio"`
	Coste  float64 `json:"coste"`
	Stock  int     `json:"stock"`
	Peso   float64 `json:"peso"`

	// CategoriaCanal queda vacía mientras no exista el mapeo. Es el estado
	// actual de todo el catálogo, y por eso MercadoLibre y Falabella salen
	// bloqueados en la vista previa.
	CategoriasCanal map[string]string `json:"categorias_canal"`
	Imagenes        []string          `json:"imagenes"`
}

// DatosParaPreview arma la entrada de proyección de una variante.
func (s *Store) DatosParaPreview(ctx context.Context, varianteID int64) (*DatosPreview, error) {
	var d DatosPreview
	var productoID int64
	var titulosJSON, specsJSON, avisosJSON []byte
	var precio *float64

	err := s.pool.QueryRow(ctx, `
		SELECT v.id, p.id, COALESCE(v.sku,''), COALESCE(v.barcode,''),
		       p.name, COALESCE(b.name, COALESCE(p.brand_raw,'')), COALESCE(p.categ_path,''),
		       COALESCE(c.titulos, '{}'::jsonb),
		       COALESCE(c.descripcion, COALESCE(p.description_sale,'')),
		       COALESCE(c.specs, '[]'::jsonb),
		       COALESCE(c.confianza, 'baja'),
		       COALESCE(c.avisos, '[]'::jsonb),
		       v.price, COALESCE(v.cost,0), COALESCE(v.weight,0),
		       COALESCE((SELECT sum(st.qty_on_hand) FROM variant_stock st WHERE st.variant_id = v.id), 0)::int
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN product_content c ON c.product_id = p.id
		WHERE v.id = $1`, varianteID).
		Scan(&d.VarianteID, &productoID, &d.SKU, &d.Barcode, &d.NombreOdoo, &d.Marca, &d.CategPath,
			&titulosJSON, &d.Descripcion, &specsJSON, &d.Confianza, &avisosJSON,
			&precio, &d.Coste, &d.Peso, &d.Stock)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no existe la variante %d", varianteID)
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo datos de vista previa: %w", err)
	}

	if precio != nil {
		d.Precio = *precio
	}
	if err := json.Unmarshal(titulosJSON, &d.Titulos); err != nil {
		return nil, fmt.Errorf("títulos mal guardados: %w", err)
	}
	if err := json.Unmarshal(specsJSON, &d.Specs); err != nil {
		return nil, fmt.Errorf("specs mal guardadas: %w", err)
	}
	if err := json.Unmarshal(avisosJSON, &d.Avisos); err != nil {
		return nil, fmt.Errorf("avisos mal guardados: %w", err)
	}

	// Mapeos de categoría por canal. Hoy la tabla está vacía: es justo el
	// trabajo que la vista previa sirve para dimensionar.
	d.CategoriasCanal = map[string]string{}
	// Solo cuentan los mapeos CONFIRMADOS: una sugerencia sin validar no debe
	// hacer que la vista previa diga que el producto es publicable.
	filas, err := s.pool.Query(ctx, `
		SELECT ch.code, cm.channel_category_id
		FROM category_mappings cm
		JOIN channels ch ON ch.id = cm.channel_id
		WHERE cm.odoo_categ_path = $1
		  AND cm.channel_account_id IS NULL
		  AND cm.confirmado_at IS NOT NULL
		  AND cm.channel_category_id <> ''`, d.CategPath)
	if err != nil {
		return nil, fmt.Errorf("leyendo mapeos de categoría: %w", err)
	}
	defer filas.Close()
	for filas.Next() {
		var canal, cat string
		if err := filas.Scan(&canal, &cat); err != nil {
			return nil, err
		}
		d.CategoriasCanal[canal] = cat
	}

	if err := filas.Err(); err != nil {
		return nil, err
	}

	// Las imágenes salen del banco de Integra, no de Odoo: las de Odoo son
	// miniaturas de 150 px que ningún canal acepta. Se envían solo las que
	// llegan al mínimo publicable; si no hay ninguna, la vista previa muestra
	// el hueco, que es la verdad.
	imgs, err := s.ImagenesDeProducto(ctx, productoID)
	if err != nil {
		return nil, err
	}
	d.Imagenes = []string{}
	for _, i := range imgs {
		d.Imagenes = append(d.Imagenes, "/imagenes/"+i.SHA256+"/cuadrada_1200")
	}

	return &d, nil
}

// PrimeraVarianteConContenido devuelve una variante representativa, para que
// la vista previa tenga algo que enseñar al abrirse.
func (s *Store) PrimeraVarianteConContenido(ctx context.Context) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		SELECT v.id
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		JOIN product_content c ON c.product_id = p.id
		WHERE p.excluded_reason IS NULL AND p.active
		  AND v.price IS NOT NULL AND c.confianza = 'alta'
		ORDER BY v.id LIMIT 1`).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}
