package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// AtributoCanal es un atributo que exige una categoría del canal.
type AtributoCanal struct {
	AttributeID   string          `json:"attribute_id"`
	Nombre        string          `json:"nombre"`
	Obligatorio   bool            `json:"obligatorio"`
	TipoDato      string          `json:"tipo_dato"`
	Unidad        string          `json:"unidad"`
	ValoresValidos []ValorAtributo `json:"valores_validos"`
}

type ValorAtributo struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
}

// GuardarAtributosCategoria refresca la caché de una categoría del canal.
func (s *Store) GuardarAtributosCategoria(ctx context.Context, canalCodigo, categoriaID string, attrs []AtributoCanal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var canalID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM channels WHERE code = $1`, canalCodigo).Scan(&canalID); err != nil {
		return fmt.Errorf("no existe el canal %q", canalCodigo)
	}

	for _, a := range attrs {
		// Una lista nil se serializa como `null`, no como `[]`: se normaliza
		// para que la columna guarde siempre un array.
		lista := a.ValoresValidos
		if lista == nil {
			lista = []ValorAtributo{}
		}
		valores, err := json.Marshal(lista)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO channel_category_attributes
			    (channel_id, category_id, attribute_id, attribute_name,
			     required, value_type, allowed_values, unit, refreshed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8, now())
			ON CONFLICT (channel_id, category_id, attribute_id) DO UPDATE
			SET attribute_name = EXCLUDED.attribute_name, required = EXCLUDED.required,
			    value_type = EXCLUDED.value_type, allowed_values = EXCLUDED.allowed_values,
			    unit = EXCLUDED.unit, refreshed_at = now()`,
			canalID, categoriaID, a.AttributeID, a.Nombre,
			a.Obligatorio, a.TipoDato, valores, nulo(a.Unidad))
		if err != nil {
			return fmt.Errorf("guardando atributo %s: %w", a.AttributeID, err)
		}
	}
	return tx.Commit(ctx)
}

// AtributosDeCategoria devuelve lo que pide una categoría, obligatorios
// primero. Con soloObligatorios se limita a los que bloquean la publicación.
func (s *Store) AtributosDeCategoria(ctx context.Context, canalCodigo, categoriaID string, soloObligatorios bool) ([]AtributoCanal, error) {
	cond := ""
	if soloObligatorios {
		cond = "AND a.required"
	}
	filas, err := s.pool.Query(ctx, `
		SELECT a.attribute_id, a.attribute_name, a.required, a.value_type,
		       COALESCE(a.unit,''), a.allowed_values
		FROM channel_category_attributes a
		JOIN channels ch ON ch.id = a.channel_id
		WHERE ch.code = $1 AND a.category_id = $2 `+cond+`
		ORDER BY a.required DESC, a.attribute_name`, canalCodigo, categoriaID)
	if err != nil {
		return nil, fmt.Errorf("leyendo atributos de la categoría: %w", err)
	}
	defer filas.Close()

	var out []AtributoCanal
	for filas.Next() {
		var a AtributoCanal
		var valores []byte
		if err := filas.Scan(&a.AttributeID, &a.Nombre, &a.Obligatorio,
			&a.TipoDato, &a.Unidad, &valores); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(valores, &a.ValoresValidos); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, filas.Err()
}

// ValorAtributoProducto es el valor asignado a un producto.
type ValorAtributoProducto struct {
	AttributeID string `json:"attribute_id"`
	Nombre      string `json:"nombre"`
	ValueID     string `json:"value_id"`
	ValueName   string `json:"value_name"`
	Origen      string `json:"origen"`
	Obligatorio bool   `json:"obligatorio"`
}

// AtributosDeProducto devuelve, para un producto y canal, todos los atributos
// que pide su categoría con el valor que tenga cada uno (vacío si falta).
func (s *Store) AtributosDeProducto(ctx context.Context, productoID int64, canalCodigo string) ([]ValorAtributoProducto, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT a.attribute_id, a.attribute_name, a.required,
		       COALESCE(pa.value_id,''), COALESCE(pa.value_name,''), COALESCE(pa.origen,'')
		FROM products p
		JOIN category_mappings cm ON cm.odoo_categ_path = p.categ_path
		     AND cm.channel_account_id IS NULL AND cm.confirmado_at IS NOT NULL
		JOIN channels ch ON ch.id = cm.channel_id AND ch.code = $2
		JOIN channel_category_attributes a
		     ON a.channel_id = ch.id AND a.category_id = cm.channel_category_id
		LEFT JOIN producto_atributos pa
		     ON pa.product_id = p.id AND pa.channel_id = ch.id
		     AND pa.attribute_id = a.attribute_id
		WHERE p.id = $1
		ORDER BY a.required DESC, a.attribute_name`, productoID, canalCodigo)
	if err != nil {
		return nil, fmt.Errorf("leyendo atributos del producto: %w", err)
	}
	defer filas.Close()

	var out []ValorAtributoProducto
	for filas.Next() {
		var v ValorAtributoProducto
		if err := filas.Scan(&v.AttributeID, &v.Nombre, &v.Obligatorio,
			&v.ValueID, &v.ValueName, &v.Origen); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, filas.Err()
}

// GuardarAtributoProducto fija el valor de un atributo.
//
// Con respetarManual, un valor escrito por una persona no se pisa: es la
// misma garantía que protege las descripciones editadas a mano.
func (s *Store) GuardarAtributoProducto(ctx context.Context, productoID int64, canalCodigo,
	attrID, valueID, valueName, origen string, respetarManual bool) error {

	cond := ""
	if respetarManual {
		cond = "WHERE producto_atributos.origen <> 'manual'"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO producto_atributos
		    (product_id, channel_id, attribute_id, value_id, value_name, origen, updated_at)
		SELECT $1, ch.id, $3, $4, $5, $6, now() FROM channels ch WHERE ch.code = $2
		ON CONFLICT (product_id, channel_id, attribute_id) DO UPDATE
		SET value_id = EXCLUDED.value_id, value_name = EXCLUDED.value_name,
		    origen = EXCLUDED.origen, updated_at = now() `+cond,
		productoID, canalCodigo, attrID, nulo(valueID), valueName, origen)
	if err != nil {
		return fmt.Errorf("guardando atributo %s: %w", attrID, err)
	}
	return nil
}

// AtributosParaPublicar devuelve los atributos con valor listos para enviar,
// y la lista de obligatorios que siguen vacíos. Con faltantes no se publica:
// el canal rechazaría la ficha entera.
func (s *Store) AtributosParaPublicar(ctx context.Context, productoID int64, canalCodigo string) (map[string]string, []string, error) {
	vals, err := s.AtributosDeProducto(ctx, productoID, canalCodigo)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	var faltan []string
	for _, v := range vals {
		if strings.TrimSpace(v.ValueName) == "" {
			if v.Obligatorio {
				faltan = append(faltan, v.Nombre)
			}
			continue
		}
		out[v.AttributeID] = v.ValueName
	}
	return out, faltan, nil
}

// FuenteAtributos es lo que Integra sabe de un producto para deducir valores.
type FuenteAtributos struct {
	ProductoID int64
	Nombre     string
	Marca      string
	CategPath  string
	CategCanal string
	SKU        string
	Specs      []struct {
		Clave string `json:"clave"`
		Valor string `json:"valor"`
	}
}

// ProductosParaAtributos lista la mercancía publicable con categoría mapeada
// en un canal, con las specs ya extraídas del nombre.
func (s *Store) ProductosParaAtributos(ctx context.Context, canalCodigo string) ([]FuenteAtributos, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT p.id, p.name, COALESCE(b.name,''), COALESCE(p.categ_path,''),
		       cm.channel_category_id,
		       COALESCE((SELECT v.sku FROM product_variants v
		                 WHERE v.product_id = p.id AND v.active ORDER BY v.id LIMIT 1),''),
		       COALESCE(c.specs, '[]'::jsonb)
		FROM products p
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN product_content c ON c.product_id = p.id
		JOIN category_mappings cm ON cm.odoo_categ_path = p.categ_path
		     AND cm.channel_account_id IS NULL AND cm.confirmado_at IS NOT NULL
		JOIN channels ch ON ch.id = cm.channel_id AND ch.code = $1
		WHERE p.active AND p.excluded_reason IS NULL
		ORDER BY p.name`, canalCodigo)
	if err != nil {
		return nil, fmt.Errorf("listando productos para atributos: %w", err)
	}
	defer filas.Close()

	var out []FuenteAtributos
	for filas.Next() {
		var f FuenteAtributos
		var specs []byte
		if err := filas.Scan(&f.ProductoID, &f.Nombre, &f.Marca, &f.CategPath,
			&f.CategCanal, &f.SKU, &specs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(specs, &f.Specs); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, filas.Err()
}

// ResumenAtributos cuenta cuántos productos están listos por atributos.
type ResumenAtributos struct {
	Canal              string `json:"canal"`
	ConCategoria       int    `json:"con_categoria"`
	Completos          int    `json:"completos"`
	FaltanObligatorios int    `json:"faltan_obligatorios"`
}

func (s *Store) ResumenAtributos(ctx context.Context, canalCodigo string) (*ResumenAtributos, error) {
	r := &ResumenAtributos{Canal: canalCodigo}
	err := s.pool.QueryRow(ctx, `
		WITH prods AS (
		    SELECT p.id, cm.channel_category_id, ch.id AS canal_id
		    FROM products p
		    JOIN category_mappings cm ON cm.odoo_categ_path = p.categ_path
		         AND cm.channel_account_id IS NULL AND cm.confirmado_at IS NOT NULL
		    JOIN channels ch ON ch.id = cm.channel_id AND ch.code = $1
		    WHERE p.active AND p.excluded_reason IS NULL
		), faltantes AS (
		    SELECT pr.id,
		           count(*) FILTER (
		               WHERE a.required AND (pa.value_name IS NULL OR pa.value_name = '')
		           ) AS faltan
		    FROM prods pr
		    JOIN channel_category_attributes a
		         ON a.channel_id = pr.canal_id AND a.category_id = pr.channel_category_id
		    LEFT JOIN producto_atributos pa
		         ON pa.product_id = pr.id AND pa.channel_id = pr.canal_id
		         AND pa.attribute_id = a.attribute_id
		    GROUP BY pr.id
		)
		SELECT (SELECT count(*) FROM prods),
		       (SELECT count(*) FROM faltantes WHERE faltan = 0),
		       (SELECT count(*) FROM faltantes WHERE faltan > 0)`, canalCodigo).
		Scan(&r.ConCategoria, &r.Completos, &r.FaltanObligatorios)
	if err != nil {
		return nil, fmt.Errorf("resumen de atributos: %w", err)
	}
	return r, nil
}

// CategoriasMapeadas devuelve las categorías del canal en uso, para saber de
// cuáles hay que traer los atributos.
func (s *Store) CategoriasMapeadas(ctx context.Context, canalCodigo string) ([]string, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT DISTINCT cm.channel_category_id
		FROM category_mappings cm
		JOIN channels ch ON ch.id = cm.channel_id
		WHERE ch.code = $1 AND cm.channel_account_id IS NULL
		  AND cm.confirmado_at IS NOT NULL AND cm.channel_category_id <> ''
		ORDER BY 1`, canalCodigo)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var out []string
	for filas.Next() {
		var c string
		if err := filas.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, filas.Err()
}
