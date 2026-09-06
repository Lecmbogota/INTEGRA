package store

import (
	"context"
	"fmt"
	"time"
)

// Migración del trabajo humano entre conexiones de Odoo.
//
// El problema que resuelve: los productos se identifican por
// (conexión, id de plantilla en Odoo), así que apuntar Integra a otra
// instancia —una réplica local, una base restaurada, un Odoo nuevo— crea
// filas nuevas y deja huérfano todo lo que es propiedad de Integra: precios,
// descripciones, imágenes, atributos y marcas. Son horas de trabajo humano.
//
// El SKU sí es estable entre instancias, y por eso es la clave del traslado.

// ResultadoMigracion cuenta lo trasladado.
type ResultadoMigracion struct {
	Emparejados  int `json:"emparejados"`
	Precios      int `json:"precios"`
	Marcas       int `json:"marcas"`
	Contenidos   int `json:"contenidos"`
	Imagenes     int `json:"imagenes"`
	Atributos    int `json:"atributos"`
	SinPareja    int `json:"sin_pareja"`
	Simulado     bool `json:"simulado"`
}

// MigrarTrabajo traslada por SKU lo que es de Integra desde una conexión a
// otra. No toca nada que venga de Odoo (nombre, stock): eso lo repone el sync.
//
// Nunca sobrescribe: si el destino ya tiene precio propio, se respeta. Así
// migrar dos veces es inofensivo.
func (s *Store) MigrarTrabajo(ctx context.Context, origenID, destinoID int64, simular bool) (*ResultadoMigracion, error) {
	if origenID == destinoID {
		return nil, fmt.Errorf("el origen y el destino son la misma conexión")
	}
	res := &ResultadoMigracion{Simulado: simular}

	// Cuántas variantes del origen tienen pareja por SKU en el destino.
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM product_variants vo
		JOIN products po ON po.id = vo.product_id AND po.odoo_connection_id = $1
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		WHERE vo.sku IS NOT NULL`, origenID, destinoID).Scan(&res.Emparejados)
	if err != nil {
		return nil, fmt.Errorf("emparejando por SKU: %w", err)
	}

	err = s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM product_variants vo
		JOIN products po ON po.id = vo.product_id AND po.odoo_connection_id = $1
		WHERE vo.sku IS NOT NULL
		  AND NOT EXISTS (
		    SELECT 1 FROM product_variants vd
		    JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		    WHERE lower(vd.sku) = lower(vo.sku))`, origenID, destinoID).Scan(&res.SinPareja)
	if err != nil {
		return nil, err
	}

	if simular {
		// En simulación se cuenta cuánto hay que trasladar, sin escribir.
		return s.contarMigrable(ctx, origenID, destinoID, res)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. Datos de la variante: precio y lo físico.
	ct, err := tx.Exec(ctx, `
		UPDATE product_variants vd SET
		    price = COALESCE(vd.price, vo.price),
		    price_updated_at = COALESCE(vd.price_updated_at, vo.price_updated_at),
		    barcode = COALESCE(vd.barcode, vo.barcode),
		    weight = COALESCE(NULLIF(vd.weight, 0), vo.weight),
		    largo_cm = COALESCE(vd.largo_cm, vo.largo_cm),
		    ancho_cm = COALESCE(vd.ancho_cm, vo.ancho_cm),
		    alto_cm = COALESCE(vd.alto_cm, vo.alto_cm),
		    computed_price = COALESCE(vd.computed_price, vo.computed_price),
		    cost = COALESCE(NULLIF(vd.cost, 0), vo.cost),
		    updated_at = now()
		FROM product_variants vo
		JOIN products po ON po.id = vo.product_id AND po.odoo_connection_id = $1
		JOIN products pd2 ON pd2.odoo_connection_id = $2
		WHERE vd.product_id = pd2.id
		  AND lower(vd.sku) = lower(vo.sku) AND vo.sku IS NOT NULL
		  AND vd.price IS NULL`, origenID, destinoID)
	if err != nil {
		return nil, fmt.Errorf("migrando precios: %w", err)
	}
	res.Precios = int(ct.RowsAffected())

	// 2. Datos del producto: marca, ficha comercial y exclusión.
	ct, err = tx.Exec(ctx, `
		UPDATE products pd SET
		    brand_id = COALESCE(pd.brand_id, po.brand_id),
		    brand_raw = COALESCE(pd.brand_raw, po.brand_raw),
		    description_sale = COALESCE(pd.description_sale, po.description_sale),
		    condicion = CASE WHEN pd.condicion = 'nuevo' THEN po.condicion ELSE pd.condicion END,
		    garantia_meses = COALESCE(pd.garantia_meses, po.garantia_meses),
		    garantia_tipo = COALESCE(pd.garantia_tipo, po.garantia_tipo),
		    video_url = COALESCE(pd.video_url, po.video_url),
		    nota_interna = COALESCE(pd.nota_interna, po.nota_interna),
		    excluded_reason = COALESCE(pd.excluded_reason, po.excluded_reason),
		    updated_at = now()
		FROM products po
		JOIN product_variants vo ON vo.product_id = po.id AND vo.sku IS NOT NULL
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		WHERE po.odoo_connection_id = $1 AND pd.odoo_connection_id = $2
		  AND vd.product_id = pd.id
		  AND pd.brand_id IS NULL`, origenID, destinoID)
	if err != nil {
		return nil, fmt.Errorf("migrando marcas y ficha: %w", err)
	}
	res.Marcas = int(ct.RowsAffected())

	// 3. Contenido generado o escrito a mano.
	ct, err = tx.Exec(ctx, `
		INSERT INTO product_content
		    (product_id, titulos, descripcion, specs, confianza, avisos, editado, aprobado_at, generado_at, updated_at)
		SELECT DISTINCT ON (pd.id)
		       pd.id, c.titulos, c.descripcion, c.specs, c.confianza, c.avisos,
		       c.editado, c.aprobado_at, c.generado_at, now()
		FROM product_content c
		JOIN products po ON po.id = c.product_id AND po.odoo_connection_id = $1
		JOIN product_variants vo ON vo.product_id = po.id AND vo.sku IS NOT NULL
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		ON CONFLICT (product_id) DO NOTHING`, origenID, destinoID)
	if err != nil {
		return nil, fmt.Errorf("migrando contenido: %w", err)
	}
	res.Contenidos = int(ct.RowsAffected())

	// 4. Imágenes: se comparten, no se copian. La misma foto vale para el
	// producto viejo y el nuevo porque es el mismo producto.
	ct, err = tx.Exec(ctx, `
		INSERT INTO producto_imagenes (product_id, imagen_id, posicion, principal, verificacion, verificacion_nota, verificada_at)
		SELECT DISTINCT ON (pd.id, pi.imagen_id)
		       pd.id, pi.imagen_id, pi.posicion, pi.principal,
		       pi.verificacion, pi.verificacion_nota, pi.verificada_at
		FROM producto_imagenes pi
		JOIN products po ON po.id = pi.product_id AND po.odoo_connection_id = $1
		JOIN product_variants vo ON vo.product_id = po.id AND vo.sku IS NOT NULL
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		WHERE NOT EXISTS (SELECT 1 FROM producto_imagenes x WHERE x.product_id = pd.id)
		ON CONFLICT (product_id, imagen_id) DO NOTHING`, origenID, destinoID)
	if err != nil {
		return nil, fmt.Errorf("migrando imágenes: %w", err)
	}
	res.Imagenes = int(ct.RowsAffected())

	// 5. Atributos por canal.
	ct, err = tx.Exec(ctx, `
		INSERT INTO producto_atributos (product_id, channel_id, attribute_id, value_id, value_name, origen, updated_at)
		SELECT DISTINCT ON (pd.id, pa.channel_id, pa.attribute_id)
		       pd.id, pa.channel_id, pa.attribute_id, pa.value_id, pa.value_name, pa.origen, now()
		FROM producto_atributos pa
		JOIN products po ON po.id = pa.product_id AND po.odoo_connection_id = $1
		JOIN product_variants vo ON vo.product_id = po.id AND vo.sku IS NOT NULL
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		ON CONFLICT (product_id, channel_id, attribute_id) DO NOTHING`, origenID, destinoID)
	if err != nil {
		return nil, fmt.Errorf("migrando atributos: %w", err)
	}
	res.Atributos = int(ct.RowsAffected())

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return res, s.RecalcularAtencion(ctx)
}

func (s *Store) contarMigrable(ctx context.Context, origenID, destinoID int64, res *ResultadoMigracion) (*ResultadoMigracion, error) {
	err := s.pool.QueryRow(ctx, `
		SELECT
		  count(*) FILTER (WHERE vo.price IS NOT NULL AND vd.price IS NULL),
		  count(*) FILTER (WHERE po.brand_id IS NOT NULL AND pd.brand_id IS NULL),
		  count(DISTINCT po.id) FILTER (WHERE EXISTS (SELECT 1 FROM product_content c WHERE c.product_id = po.id)),
		  count(DISTINCT po.id) FILTER (WHERE EXISTS (SELECT 1 FROM producto_imagenes i WHERE i.product_id = po.id))
		FROM product_variants vo
		JOIN products po ON po.id = vo.product_id AND po.odoo_connection_id = $1
		JOIN product_variants vd ON lower(vd.sku) = lower(vo.sku)
		JOIN products pd ON pd.id = vd.product_id AND pd.odoo_connection_id = $2
		WHERE vo.sku IS NOT NULL`, origenID, destinoID).
		Scan(&res.Precios, &res.Marcas, &res.Contenidos, &res.Imagenes)
	if err != nil {
		return nil, fmt.Errorf("contando lo migrable: %w", err)
	}
	return res, nil
}

// ConexionResumen describe una conexión y cuánto trabajo cuelga de ella.
type ConexionResumen struct {
	ID         int64      `json:"id"`
	Nombre     string     `json:"nombre"`
	BaseURL    string     `json:"base_url"`
	Database   string     `json:"database"`
	Usuario    string     `json:"usuario"`
	Timezone   string     `json:"timezone"`
	Activa     bool       `json:"activa"`
	UltimaSync *time.Time `json:"ultima_sync"`
	Productos  int        `json:"productos"`
	ConPrecio  int        `json:"con_precio"`
	ConImagen  int        `json:"con_imagen"`
}

func (s *Store) ResumenConexiones(ctx context.Context) ([]ConexionResumen, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT c.id, c.name, c.base_url, c.database, c.username, c.timezone,
		       c.active, c.last_sync_at,
		       count(DISTINCT p.id)::int,
		       count(DISTINCT v.id) FILTER (WHERE v.price IS NOT NULL)::int,
		       count(DISTINCT pi.product_id)::int
		FROM odoo_connections c
		LEFT JOIN products p ON p.odoo_connection_id = c.id
		LEFT JOIN product_variants v ON v.product_id = p.id
		LEFT JOIN producto_imagenes pi ON pi.product_id = p.id
		GROUP BY c.id
		ORDER BY c.id`)
	if err != nil {
		return nil, fmt.Errorf("listando conexiones: %w", err)
	}
	defer filas.Close()

	var out []ConexionResumen
	for filas.Next() {
		var c ConexionResumen
		if err := filas.Scan(&c.ID, &c.Nombre, &c.BaseURL, &c.Database,
			&c.Usuario, &c.Timezone, &c.Activa, &c.UltimaSync,
			&c.Productos, &c.ConPrecio, &c.ConImagen); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, filas.Err()
}

// BorrarConexion elimina una conexión y su catálogo.
//
// Es destructivo por diseño: lo que cuelga de una conexión que ya no existe
// no sirve para nada. Migrar antes es responsabilidad de quien lo ejecuta, y
// por eso el comando lo exige explícitamente.
func (s *Store) BorrarConexion(ctx context.Context, id int64) (int, error) {
	var activa bool
	if err := s.pool.QueryRow(ctx,
		`SELECT active FROM odoo_connections WHERE id = $1`, id).Scan(&activa); err != nil {
		return 0, fmt.Errorf("no existe la conexión %d", id)
	}
	if activa {
		return 0, fmt.Errorf("la conexión %d está activa: no se puede borrar la que está en uso", id)
	}

	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM products WHERE odoo_connection_id = $1`, id).Scan(&n); err != nil {
		return 0, err
	}
	// products y odoo_warehouses caen en cascada con la conexión.
	if _, err := s.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, id); err != nil {
		return 0, fmt.Errorf("borrando la conexión: %w", err)
	}
	return n, nil
}
