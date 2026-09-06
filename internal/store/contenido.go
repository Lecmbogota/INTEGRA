package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// FuenteContenido es lo que hace falta para generar el contenido de un producto.
type FuenteContenido struct {
	ProductID int64
	Nombre    string
	Marca     string
	CategPath string
	SKU       string
	Peso      float64
}

// ProductosParaGenerar devuelve la mercancía publicable cuyo contenido falta.
//
// Se excluyen las filas ya editadas a mano: regenerar sobre ellas destruiría
// trabajo humano, que es lo más caro de todo el proceso.
func (s *Store) ProductosParaGenerar(ctx context.Context, soloFaltantes bool) ([]FuenteContenido, error) {
	cond := "p.excluded_reason IS NULL AND p.active"
	if soloFaltantes {
		cond += ` AND NOT EXISTS (
			SELECT 1 FROM product_content c
			WHERE c.product_id = p.id AND (c.editado OR c.aprobado_at IS NOT NULL))`
	}

	filas, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, COALESCE(b.name, COALESCE(p.brand_raw,'')),
		       COALESCE(p.categ_path,''), COALESCE(v.sku,''), COALESCE(v.weight,0)
		FROM products p
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN LATERAL (
		    SELECT sku, weight FROM product_variants
		    WHERE product_id = p.id AND active ORDER BY id LIMIT 1
		) v ON TRUE
		WHERE `+cond+`
		ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("listando productos para generar: %w", err)
	}
	defer filas.Close()

	var out []FuenteContenido
	for filas.Next() {
		var f FuenteContenido
		if err := filas.Scan(&f.ProductID, &f.Nombre, &f.Marca, &f.CategPath, &f.SKU, &f.Peso); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, filas.Err()
}

// GuardarContenido persiste un borrador generado.
//
// El ON CONFLICT respeta explícitamente las filas editadas: la condición WHERE
// del DO UPDATE impide sobrescribir lo que una persona ya tocó.
func (s *Store) GuardarContenido(ctx context.Context, productoID int64,
	titulos map[string]string, descripcion string, specs any, confianza string, avisos []string) error {

	jt, err := json.Marshal(titulos)
	if err != nil {
		return err
	}
	js, err := json.Marshal(specs)
	if err != nil {
		return err
	}
	if avisos == nil {
		avisos = []string{}
	}
	ja, err := json.Marshal(avisos)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO product_content
		    (product_id, titulos, descripcion, specs, confianza, avisos, generado_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		ON CONFLICT (product_id) DO UPDATE
		SET titulos = EXCLUDED.titulos, descripcion = EXCLUDED.descripcion,
		    specs = EXCLUDED.specs, confianza = EXCLUDED.confianza,
		    avisos = EXCLUDED.avisos, generado_at = now(), updated_at = now()
		WHERE NOT product_content.editado AND product_content.aprobado_at IS NULL`,
		productoID, jt, nulo(descripcion), js, confianza, ja)
	if err != nil {
		return fmt.Errorf("guardando contenido del producto %d: %w", productoID, err)
	}
	return nil
}

// ResumenContenido cuenta el estado de la generación.
type ResumenContenido struct {
	Total      int `json:"total"`
	Alta       int `json:"confianza_alta"`
	Media      int `json:"confianza_media"`
	Baja       int `json:"confianza_baja"`
	Aprobados  int `json:"aprobados"`
	Editados   int `json:"editados"`
	SinGenerar int `json:"sin_generar"`
}

func (s *Store) ResumenContenido(ctx context.Context) (*ResumenContenido, error) {
	var r ResumenContenido
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM product_content),
		  (SELECT count(*) FROM product_content WHERE confianza = 'alta'),
		  (SELECT count(*) FROM product_content WHERE confianza = 'media'),
		  (SELECT count(*) FROM product_content WHERE confianza = 'baja'),
		  (SELECT count(*) FROM product_content WHERE aprobado_at IS NOT NULL),
		  (SELECT count(*) FROM product_content WHERE editado),
		  (SELECT count(*) FROM products p
		     WHERE p.excluded_reason IS NULL AND p.active
		       AND NOT EXISTS (SELECT 1 FROM product_content c WHERE c.product_id = p.id))
	`).Scan(&r.Total, &r.Alta, &r.Media, &r.Baja, &r.Aprobados, &r.Editados, &r.SinGenerar)
	if err != nil {
		return nil, fmt.Errorf("resumen de contenido: %w", err)
	}
	return &r, nil
}

// MuestraVariada devuelve productos de categorías distintas para probar la IA.
//
// Uno por rama de categoría, y no los n primeros: si todos salen del mismo
// estante, una ficha buena no demuestra nada sobre las otras cuatrocientas.
func (s *Store) MuestraVariada(ctx context.Context, n int) ([]FuenteContenido, error) {
	// Se agrupa por MARCA y no por categoría: en el catálogo de MDV casi todo
	// cuelga de la misma rama, así que agrupar por categoría devolvía una sola
	// fila. La marca sí reparte —discos, audífonos, scooters, impresoras— que
	// es exactamente la variedad que hace falta para juzgar al modelo.
	filas, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (b.id)
		       p.id, p.name, COALESCE(b.name, ''), COALESCE(p.categ_path, ''),
		       COALESCE(v.sku, ''), COALESCE(v.weight, 0)
		FROM products p
		JOIN product_variants v ON v.product_id = p.id AND v.active
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE p.active AND p.excluded_reason IS NULL AND COALESCE(v.sku, '') <> ''
		ORDER BY b.id, p.id
		LIMIT $1`, n)
	if err != nil {
		return nil, fmt.Errorf("tomando una muestra variada: %w", err)
	}
	defer filas.Close()

	var out []FuenteContenido
	for filas.Next() {
		var f FuenteContenido
		if err := filas.Scan(&f.ProductID, &f.Nombre, &f.Marca, &f.CategPath, &f.SKU, &f.Peso); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no hay ningún producto con SKU sobre el que probar")
	}
	return out, nil
}

// FuenteContenidoPorSKU busca un producto concreto para probar con él.
func (s *Store) FuenteContenidoPorSKU(ctx context.Context, sku string) (*FuenteContenido, error) {
	var f FuenteContenido
	err := s.pool.QueryRow(ctx, `
		SELECT p.id, p.name, COALESCE(b.name, ''), COALESCE(p.categ_path, ''),
		       COALESCE(v.sku, ''), COALESCE(v.weight, 0)
		FROM products p
		JOIN product_variants v ON v.product_id = p.id
		LEFT JOIN brands b ON b.id = p.brand_id
		WHERE upper(btrim(v.sku)) = upper(btrim($1))
		LIMIT 1`, sku).Scan(&f.ProductID, &f.Nombre, &f.Marca, &f.CategPath, &f.SKU, &f.Peso)
	if err != nil {
		return nil, fmt.Errorf("no encuentro el SKU %q: %w", sku, err)
	}
	return &f, nil
}
