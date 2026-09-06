package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// CategoriaPorMapear es una rama de Odoo que aún no tiene equivalente en un canal.
type CategoriaPorMapear struct {
	CategPath string `json:"categoria_odoo"`
	Productos int    `json:"productos"`
	// TituloEjemplo es el del producto con más inventario de la rama. Se usa
	// para consultar al predictor: cuanto más representativo, mejor sugerencia.
	TituloEjemplo string  `json:"titulo_ejemplo"`
	ValorParado   float64 `json:"valor_inventario"`
}

// CategoriasParaMapear devuelve las ramas pendientes, ordenadas por el valor de
// inventario que desbloquearían.
//
// El orden es deliberado: mapear la categoría con 640 unidades paradas vale
// mucho más que la que tiene un producto suelto, y con 39 ramas por delante
// conviene saber por dónde empezar.
func (s *Store) CategoriasParaMapear(ctx context.Context, canalCodigo string, soloPendientes bool) ([]CategoriaPorMapear, error) {
	cond := ""
	if soloPendientes {
		cond = `AND NOT EXISTS (
			SELECT 1 FROM category_mappings cm
			JOIN channels ch ON ch.id = cm.channel_id
			WHERE ch.code = $1 AND cm.odoo_categ_path = p.categ_path)`
	}

	filas, err := s.pool.Query(ctx, `
		SELECT p.categ_path,
		       count(DISTINCT p.id)::int,
		       COALESCE(sum(st.q * v.cost), 0),
		       (ARRAY_AGG(
		          COALESCE(c.titulos->>'mercadolibre', p.name)
		          ORDER BY st.q * v.cost DESC NULLS LAST
		        ))[1]
		FROM products p
		JOIN product_variants v ON v.product_id = p.id AND v.active
		LEFT JOIN product_content c ON c.product_id = p.id
		LEFT JOIN LATERAL (
		    SELECT COALESCE(sum(qty_on_hand), 0) q FROM variant_stock s WHERE s.variant_id = v.id
		) st ON TRUE
		WHERE p.excluded_reason IS NULL AND p.active AND p.categ_path IS NOT NULL
		  AND v.computed_price IS NOT NULL `+cond+`
		GROUP BY p.categ_path
		ORDER BY sum(st.q * v.cost) DESC NULLS LAST, count(DISTINCT p.id) DESC`, canalCodigo)
	if err != nil {
		return nil, fmt.Errorf("listando categorías por mapear: %w", err)
	}
	defer filas.Close()

	var out []CategoriaPorMapear
	for filas.Next() {
		var c CategoriaPorMapear
		if err := filas.Scan(&c.CategPath, &c.Productos, &c.ValorParado, &c.TituloEjemplo); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, filas.Err()
}

// GuardarSugerenciaCategoria registra una propuesta pendiente de confirmar.
//
// No se sobrescribe lo ya confirmado: una vez que una persona validó el mapeo,
// una nueva ejecución del predictor no debe deshacerlo.
func (s *Store) GuardarSugerenciaCategoria(ctx context.Context, canalCodigo, categPath,
	categoriaCanalID, categoriaCanalNombre, origen string, confianza float64, atributos any) error {

	ja, err := json.Marshal(atributos)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO category_mappings
		    (channel_id, channel_account_id, odoo_categ_path,
		     channel_category_id, channel_category_name,
		     sugerido_por, confianza, atributos_sugeridos)
		SELECT ch.id, NULL, $2, $3, $4, $5, $6, $7
		FROM channels ch WHERE ch.code = $1
		ON CONFLICT (channel_id, odoo_categ_path) WHERE channel_account_id IS NULL
		DO UPDATE SET
		    channel_category_id = EXCLUDED.channel_category_id,
		    channel_category_name = EXCLUDED.channel_category_name,
		    sugerido_por = EXCLUDED.sugerido_por,
		    confianza = EXCLUDED.confianza,
		    atributos_sugeridos = EXCLUDED.atributos_sugeridos,
		    updated_at = now()
		WHERE category_mappings.confirmado_at IS NULL`,
		canalCodigo, categPath, categoriaCanalID, categoriaCanalNombre, origen, confianza, ja)
	if err != nil {
		return fmt.Errorf("guardando sugerencia de %q: %w", categPath, err)
	}
	return nil
}

// MapeoCategoria es una fila de la pantalla de mapeo.
type MapeoCategoria struct {
	ID           int64    `json:"id"`
	Canal        string   `json:"canal"`
	CategPath    string   `json:"categoria_odoo"`
	CategoriaID  string   `json:"categoria_canal_id"`
	CategoriaNom string   `json:"categoria_canal_nombre"`
	Origen       string   `json:"origen"`
	Confianza    *float64 `json:"confianza"`
	Confirmado   bool     `json:"confirmado"`
	Productos    int      `json:"productos"`
	ValorParado  float64  `json:"valor_inventario"`
	Atributos    []struct {
		ID          string `json:"id"`
		Nombre      string `json:"name"`
		ValorNombre string `json:"value_name"`
	} `json:"atributos_sugeridos"`
}

func (s *Store) ListarMapeos(ctx context.Context, canalCodigo string) ([]MapeoCategoria, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT cm.id, ch.code, cm.odoo_categ_path, cm.channel_category_id,
		       COALESCE(cm.channel_category_name,''), COALESCE(cm.sugerido_por,''),
		       cm.confianza, (cm.confirmado_at IS NOT NULL),
		       cm.atributos_sugeridos,
		       COALESCE(x.productos, 0), COALESCE(x.valor, 0)
		FROM category_mappings cm
		JOIN channels ch ON ch.id = cm.channel_id
		LEFT JOIN LATERAL (
		    SELECT count(DISTINCT p.id)::int AS productos,
		           COALESCE(sum(st.q * v.cost), 0) AS valor
		    FROM products p
		    JOIN product_variants v ON v.product_id = p.id AND v.active
		    LEFT JOIN LATERAL (
		        SELECT COALESCE(sum(qty_on_hand),0) q FROM variant_stock s WHERE s.variant_id = v.id
		    ) st ON TRUE
		    WHERE p.categ_path = cm.odoo_categ_path
		      AND p.excluded_reason IS NULL AND p.active
		) x ON TRUE
		WHERE ch.code = $1 AND cm.channel_account_id IS NULL
		ORDER BY x.valor DESC NULLS LAST`, canalCodigo)
	if err != nil {
		return nil, fmt.Errorf("listando mapeos: %w", err)
	}
	defer filas.Close()

	var out []MapeoCategoria
	for filas.Next() {
		var m MapeoCategoria
		var attrs []byte
		if err := filas.Scan(&m.ID, &m.Canal, &m.CategPath, &m.CategoriaID, &m.CategoriaNom,
			&m.Origen, &m.Confianza, &m.Confirmado, &attrs, &m.Productos, &m.ValorParado); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(attrs, &m.Atributos); err != nil {
			return nil, fmt.Errorf("atributos mal guardados en el mapeo %d: %w", m.ID, err)
		}
		out = append(out, m)
	}
	return out, filas.Err()
}

// ConfirmarMapeo valida una sugerencia. A partir de aquí se usa para publicar.
func (s *Store) ConfirmarMapeo(ctx context.Context, id int64, usuarioID *int64) error {
	ct, err := s.pool.Exec(ctx, `
		UPDATE category_mappings
		SET confirmado_at = now(), confirmado_por = $2, updated_at = now()
		WHERE id = $1 AND channel_category_id IS NOT NULL AND channel_category_id <> ''`,
		id, usuarioID)
	if err != nil {
		return fmt.Errorf("confirmando el mapeo %d: %w", id, err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("el mapeo %d no existe o no tiene categoría asignada", id)
	}
	return nil
}
