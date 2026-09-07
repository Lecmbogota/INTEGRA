package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Resumen alimenta el panel principal.
type Resumen struct {
	Productos      int        `json:"productos"`
	Variantes      int        `json:"variantes"`
	Marcas         int        `json:"marcas"`
	ConSKU         int        `json:"con_sku"`
	ConPrecio      int        `json:"con_precio"`
	ConStock       int        `json:"con_stock"`
	ConDescripcion int        `json:"con_descripcion"`
	Publicables    int        `json:"publicables"`
	EnAtencion     int        `json:"en_atencion"`
	UltimaSync     *time.Time `json:"ultima_sincronizacion"`
	StockTotal     float64    `json:"stock_total"`
	Excluidos      int        `json:"excluidos"`
}

func (s *Store) Resumen(ctx context.Context) (*Resumen, error) {
	var r Resumen
	// Todos los conteos miran solo mercancía publicable. Contar los gastos y
	// activos fijos como "productos" daba un catálogo de 632 cuando la
	// mercancía real son 452, y ensuciaba todos los porcentajes.
	const soloMercancia = `p.excluded_reason IS NULL AND p.active`

	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM products p WHERE `+soloMercancia+`),
		  (SELECT count(*) FROM product_variants v JOIN products p ON p.id = v.product_id
		     WHERE v.active AND `+soloMercancia+`),
		  (SELECT count(DISTINCT p.brand_id) FROM products p WHERE `+soloMercancia+` AND p.brand_id IS NOT NULL),
		  (SELECT count(*) FROM product_variants v JOIN products p ON p.id = v.product_id
		     WHERE v.active AND `+soloMercancia+` AND v.sku IS NOT NULL),
		  (SELECT count(*) FROM product_variants v JOIN products p ON p.id = v.product_id
		     WHERE `+soloMercancia+` AND v.price IS NOT NULL),
		  (SELECT count(DISTINCT vs.variant_id) FROM variant_stock vs
		     JOIN product_variants v ON v.id = vs.variant_id
		     JOIN products p ON p.id = v.product_id
		     WHERE vs.qty_on_hand > 0 AND `+soloMercancia+`),
		  (SELECT count(*) FROM products p
		     LEFT JOIN product_content c ON c.product_id = p.id
		     WHERE `+soloMercancia+`
		       AND NULLIF(TRIM(COALESCE(c.descripcion, p.description_sale, '')), '') IS NOT NULL),
		  (SELECT count(*) FROM attention_queue WHERE resolved_at IS NULL),
		  (SELECT max(last_sync_at) FROM odoo_connections),
		  (SELECT COALESCE(sum(vs.qty_on_hand), 0) FROM variant_stock vs
		     JOIN product_variants v ON v.id = vs.variant_id
		     JOIN products p ON p.id = v.product_id WHERE `+soloMercancia+`),
		  (SELECT count(*) FROM products WHERE excluded_reason IS NOT NULL)
	`).Scan(&r.Productos, &r.Variantes, &r.Marcas, &r.ConSKU, &r.ConPrecio,
		&r.ConStock, &r.ConDescripcion, &r.EnAtencion, &r.UltimaSync,
		&r.StockTotal, &r.Excluidos)
	if err != nil {
		return nil, fmt.Errorf("calculando el resumen: %w", err)
	}

	// Publicable = tiene todo lo que exigen los cuatro canales.
	err = s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN product_content c ON c.product_id = p.id
		WHERE v.active AND p.active AND p.excluded_reason IS NULL
		  AND v.sku IS NOT NULL
		  AND NULLIF(TRIM(COALESCE(c.descripcion, p.description_sale, '')), '') IS NOT NULL
		  AND v.price IS NOT NULL
		  AND EXISTS (SELECT 1 FROM variant_stock st WHERE st.variant_id = v.id AND st.qty_on_hand > 0)
	`).Scan(&r.Publicables)
	if err != nil {
		return nil, fmt.Errorf("contando publicables: %w", err)
	}
	return &r, nil
}

// FilaProducto es una fila de la tabla de catálogo.
type FilaProducto struct {
	ID             int64    `json:"id"`
	SKU            string   `json:"sku"`
	Nombre         string   `json:"nombre"`
	Marca          string   `json:"marca"`
	Categoria      string   `json:"categoria"`
	Precio         *float64 `json:"precio"`
	PrecioSugerido *float64 `json:"precio_sugerido"`
	Barcode        string   `json:"barcode"`
	Peso           float64  `json:"peso"`
	Descripcion    string   `json:"descripcion"`
	Excluido       bool     `json:"excluido"`
	Stock          float64  `json:"stock"`
	Problemas      []string `json:"problemas"`
	// Detalles explica cada problema, por su código. Un aviso sin motivo no
	// se puede juzgar ni resolver.
	Detalles map[string]string `json:"detalles"`

	// Canales donde el producto tiene ficha, con su estado. Vacío significa
	// que está pendiente de publicar, que es lo que no se podía ver en la
	// lista: había que ir canal por canal a adivinarlo.
	Publicado []PublicadoEn `json:"publicado"`

	// Ficha comercial completa.
	Titulos       map[string]string `json:"titulos"`
	LargoCm       float64           `json:"largo_cm"`
	AnchoCm       float64           `json:"ancho_cm"`
	AltoCm        float64           `json:"alto_cm"`
	Condicion     string            `json:"condicion"`
	GarantiaMeses *int              `json:"garantia_meses"`
	GarantiaTipo  string            `json:"garantia_tipo"`
	VideoURL      string            `json:"video_url"`
	NotaInterna   string            `json:"nota_interna"`

	// PortadaSHA es el sha256 de la imagen principal del producto (vacío si
	// no tiene). La lista lo trae para que las vistas de mosaico e iconos
	// pinten la foto sin pedir la galería de cada producto una a una.
	PortadaSHA string `json:"portada_sha"`
}

// FiltroProductos acota la consulta del catálogo.
// PublicadoEn dice en qué canal vive una ficha y cómo está.
type PublicadoEn struct {
	Canal  string `json:"canal"`
	Estado string `json:"estado"`
}

type FiltroProductos struct {
	Busqueda      string
	Marca         string
	Categoria     string
	SoloProblemas bool
	VerExcluidos  bool
	// SoloSinPrecio acota a lo que aún no tiene PVP. Existe sobre todo para
	// la edición masiva: aplicar «precio = coste × factor» a una selección
	// que incluya productos ya tarifados los sobrescribiría en silencio.
	SoloSinPrecio bool
	// SoloSinFoto acota a lo que no tiene ninguna imagen. Sin foto no publica
	// ningún canal, así que es lo primero que hay que resolver de un producto
	// nuevo.
	SoloSinFoto bool
	// SoloSinDescripcion: sin ella tampoco sale a ningún canal.
	SoloSinDescripcion bool
	// SoloSinEAN acota a lo que le falta el código de barras. Solo Falabella
	// lo exige, pero es el dato que más falta y el que más tarde se descubre.
	SoloSinEAN bool
	// SoloConPromo acota a lo que tiene una oferta vigente ahora mismo.
	SoloConPromo bool
	// SoloSinPublicar acota a lo que no tiene ficha en ningún canal. Es el
	// filtro que contesta «¿qué me falta por subir?», que antes obligaba a
	// comparar dos pantallas a ojo.
	SoloSinPublicar bool
	// Orden es la columna por la que se ordena y en qué sentido. Sin esto la
	// lista salía siempre por nombre y no había forma de ver, por ejemplo, lo
	// más caro o lo que se quedó sin stock.
	Orden  string
	Desc   bool
	Limite int
	Offset int
}

// ordenes traduce lo que pide la pantalla a SQL. Es una lista cerrada: el
// nombre de columna se concatena en la consulta y aceptar texto libre sería
// abrir una inyección por la puerta de atrás.
var ordenes = map[string]string{
	"nombre": "p.name",
	"sku":    "v.sku",
	"marca":  "b.name",
	"precio": "v.price",
	"stock":  "(SELECT sum(st.qty_on_hand) FROM variant_stock st WHERE st.variant_id = v.id)",
}

func (s *Store) ListarProductos(ctx context.Context, f FiltroProductos) ([]FilaProducto, int, error) {
	if f.Limite <= 0 || f.Limite > 500 {
		f.Limite = 100
	}

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
	if f.SoloSinFoto {
		cond = append(cond, `NOT EXISTS (
			SELECT 1 FROM producto_imagenes pi WHERE pi.product_id = p.id)`)
	}
	if f.SoloSinDescripcion {
		cond = append(cond,
			`NULLIF(TRIM(COALESCE(c.descripcion, p.description_sale, '')), '') IS NULL`)
	}
	if f.SoloSinEAN {
		cond = append(cond, `COALESCE(v.barcode, '') = ''`)
	}
	if f.SoloConPromo {
		cond = append(cond, `EXISTS (
			SELECT 1 FROM offers o
			WHERE o.variant_id = v.id AND o.active AND o.reverted_at IS NULL
			  AND o.starts_at <= now() AND (o.ends_at IS NULL OR o.ends_at > now()))`)
	}
	if f.SoloSinPublicar {
		cond = append(cond, `NOT EXISTS (
			SELECT 1 FROM variant_channel_listings vcl
			JOIN channel_accounts ca ON ca.id = vcl.channel_account_id AND ca.active
			WHERE vcl.variant_id = v.id)`)
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
	where := "WHERE " + strings.Join(cond, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("contando productos: %w", err)
	}

	args = append(args, f.Limite, f.Offset)
	sql := `
		SELECT v.id, COALESCE(v.sku,''), p.name, COALESCE(b.name,''), COALESCE(p.categ_path,''),
		       v.price, v.computed_price,
		       COALESCE(v.barcode,''), COALESCE(v.weight,0),
		       COALESCE(c.descripcion, COALESCE(p.description_sale,'')),
		       (p.excluded_reason IS NOT NULL),
		       COALESCE((SELECT sum(st.qty_on_hand) FROM variant_stock st WHERE st.variant_id = v.id), 0),
		       COALESCE(ARRAY(SELECT a.reason FROM attention_queue a
		                      WHERE a.variant_id = v.id AND a.resolved_at IS NULL
		                      ORDER BY a.reason), '{}'),
		       -- El detalle explica el aviso: sin él «portada dudosa» no dice
		       -- quién lo decidió ni por qué, y no hay forma de juzgarlo.
		       COALESCE(ARRAY(SELECT a.reason || '	' || COALESCE(a.detail, '')
		                      FROM attention_queue a
		                      WHERE a.variant_id = v.id AND a.resolved_at IS NULL
		                      ORDER BY a.reason), '{}'),
		       COALESCE(c.titulos, '{}'::jsonb),
		       COALESCE(v.largo_cm,0), COALESCE(v.ancho_cm,0), COALESCE(v.alto_cm,0),
		       p.condicion, p.garantia_meses, COALESCE(p.garantia_tipo,''),
		       COALESCE(p.video_url,''), COALESCE(p.nota_interna,''),
		       -- La portada: la marcada como principal y, si no hay ninguna, la
		       -- primera por posición, que es lo que verá quien abra la ficha.
		       COALESCE((SELECT i.sha256 FROM producto_imagenes pi
		                 JOIN imagenes i ON i.id = pi.imagen_id
		                 WHERE pi.product_id = p.id
		                 ORDER BY pi.principal DESC, pi.posicion, pi.imagen_id
		                 LIMIT 1), ''),
		       COALESCE(ARRAY(
		           SELECT ch.code || ':' || vcl.status::text
		           FROM variant_channel_listings vcl
		           JOIN channel_accounts ca ON ca.id = vcl.channel_account_id AND ca.active
		           JOIN channels ch ON ch.id = ca.channel_id
		           WHERE vcl.variant_id = v.id
		           ORDER BY ch.code), '{}')
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN product_content c ON c.product_id = p.id ` + where + `
		ORDER BY ` + ordenar(f) + `
		LIMIT $` + fmt.Sprint(len(args)-1) + ` OFFSET $` + fmt.Sprint(len(args))

	filas, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("listando productos: %w", err)
	}
	defer filas.Close()

	var out []FilaProducto
	for filas.Next() {
		var r FilaProducto
		var titulos []byte
		var publicado, detalles []string
		if err := filas.Scan(&r.ID, &r.SKU, &r.Nombre, &r.Marca, &r.Categoria,
			&r.Precio, &r.PrecioSugerido, &r.Barcode, &r.Peso, &r.Descripcion,
			&r.Excluido, &r.Stock, &r.Problemas, &detalles,
			&titulos, &r.LargoCm, &r.AnchoCm, &r.AltoCm,
			&r.Condicion, &r.GarantiaMeses, &r.GarantiaTipo,
			&r.VideoURL, &r.NotaInterna, &r.PortadaSHA, &publicado); err != nil {
			return nil, 0, err
		}
		if err := json.Unmarshal(titulos, &r.Titulos); err != nil {
			return nil, 0, err
		}
		// Llegan como «canal:estado» para traerlos en un solo array; el canal
		// nunca lleva dos puntos, así que partir por el primero es seguro.
		r.Detalles = map[string]string{}
		for _, d := range detalles {
			if i := strings.Index(d, "	"); i >= 0 {
				r.Detalles[d[:i]] = d[i+1:]
			}
		}
		r.Publicado = []PublicadoEn{}
		for _, v := range publicado {
			if i := strings.Index(v, ":"); i > 0 {
				r.Publicado = append(r.Publicado, PublicadoEn{Canal: v[:i], Estado: v[i+1:]})
			}
		}
		out = append(out, r)
	}
	return out, total, filas.Err()
}

// FilaPrioridad es un producto en la cola de "qué publicar primero".
type FilaPrioridad struct {
	VarianteID int64    `json:"variante_id"`
	SKU        string   `json:"sku"`
	Nombre     string   `json:"nombre"`
	Marca      string   `json:"marca"`
	Precio     *float64 `json:"precio"`
	Stock      float64  `json:"stock"`
	ValorStock float64  `json:"valor_stock"`
	Listo      bool     `json:"listo"`
	Bloqueos   int      `json:"bloqueos"`
}

// PrioridadPublicacion ordena el catálogo por dónde conviene empezar a
// publicar: primero lo que ya cumple todos los requisitos, y dentro de eso lo
// que más valor de inventario tiene esperando venderse. Cuando existan las
// órdenes (Fase 5) el criterio se enriquecerá con la rotación real.
func (s *Store) PrioridadPublicacion(ctx context.Context, limite int) ([]FilaPrioridad, error) {
	if limite <= 0 || limite > 100 {
		limite = 10
	}
	filas, err := s.pool.Query(ctx, `
		SELECT v.id, COALESCE(v.sku,''), p.name, COALESCE(b.name,''),
		       v.price,
		       COALESCE(st.total, 0),
		       COALESCE(st.total, 0) * COALESCE(v.price, 0),
		       (bl.n = 0 AND COALESCE(st.total,0) > 0 AND v.price IS NOT NULL),
		       bl.n::int
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN LATERAL (
		    SELECT sum(qty_on_hand) AS total FROM variant_stock WHERE variant_id = v.id
		) st ON TRUE
		LEFT JOIN LATERAL (
		    SELECT count(*) AS n FROM attention_queue a
		    WHERE a.variant_id = v.id AND a.resolved_at IS NULL AND a.severity = 'blocking'
		) bl ON TRUE
		WHERE v.active AND p.active AND p.excluded_reason IS NULL
		ORDER BY (bl.n = 0 AND COALESCE(st.total,0) > 0 AND v.price IS NOT NULL) DESC,
		         COALESCE(st.total, 0) * COALESCE(v.price, 0) DESC,
		         p.name
		LIMIT $1`, limite)
	if err != nil {
		return nil, fmt.Errorf("calculando la prioridad de publicación: %w", err)
	}
	defer filas.Close()

	var out []FilaPrioridad
	for filas.Next() {
		var f FilaPrioridad
		if err := filas.Scan(&f.VarianteID, &f.SKU, &f.Nombre, &f.Marca,
			&f.Precio, &f.Stock, &f.ValorStock, &f.Listo, &f.Bloqueos); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, filas.Err()
}

// ConteoMarca alimenta el filtro por marca y el gráfico del panel.
type ConteoMarca struct {
	Codigo   string `json:"codigo"`
	Nombre   string `json:"nombre"`
	Cantidad int    `json:"cantidad"`
	Alias    int    `json:"alias"`
}

func (s *Store) Marcas(ctx context.Context) ([]ConteoMarca, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT b.code, b.name, count(p.id)::int,
		       (SELECT count(*)::int FROM brand_aliases a WHERE a.brand_id = b.id)
		FROM brands b
		LEFT JOIN products p ON p.brand_id = b.id AND p.active AND p.excluded_reason IS NULL
		GROUP BY b.id, b.code, b.name
		HAVING count(p.id) > 0
		ORDER BY count(p.id) DESC, b.name`)
	if err != nil {
		return nil, fmt.Errorf("listando marcas: %w", err)
	}
	defer filas.Close()

	var out []ConteoMarca
	for filas.Next() {
		var m ConteoMarca
		if err := filas.Scan(&m.Codigo, &m.Nombre, &m.Cantidad, &m.Alias); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, filas.Err()
}

// ConteoCategoria alimenta el filtro por categoría.
type ConteoCategoria struct {
	Nombre   string `json:"nombre"`
	Cantidad int    `json:"cantidad"`
}

func (s *Store) Categorias(ctx context.Context) ([]ConteoCategoria, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT p.categ_path, count(p.id)::int
		FROM products p
		WHERE p.active AND p.excluded_reason IS NULL AND p.categ_path IS NOT NULL AND p.categ_path != ''
		GROUP BY p.categ_path
		ORDER BY p.categ_path ASC`)
	if err != nil {
		return nil, fmt.Errorf("listando categorías: %w", err)
	}
	defer filas.Close()

	var out []ConteoCategoria
	for filas.Next() {
		var c ConteoCategoria
		if err := filas.Scan(&c.Nombre, &c.Cantidad); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, filas.Err()
}

// ConteoAtencion agrupa la cola de atención por motivo.
type ConteoAtencion struct {
	Motivo    string `json:"motivo"`
	Severidad string `json:"severidad"`
	Cantidad  int    `json:"cantidad"`
}

func (s *Store) Atencion(ctx context.Context) ([]ConteoAtencion, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT reason, severity, count(*)::int
		FROM attention_queue WHERE resolved_at IS NULL
		GROUP BY reason, severity ORDER BY count(*) DESC`)
	if err != nil {
		return nil, fmt.Errorf("leyendo la cola de atención: %w", err)
	}
	defer filas.Close()

	var out []ConteoAtencion
	for filas.Next() {
		var a ConteoAtencion
		if err := filas.Scan(&a.Motivo, &a.Severidad, &a.Cantidad); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, filas.Err()
}

// StockAlmacen alimenta el desglose por bodega.
type StockAlmacen struct {
	Codigo    string  `json:"codigo"`
	Nombre    string  `json:"nombre"`
	Unidades  float64 `json:"unidades"`
	Variantes int     `json:"variantes"`
}

func (s *Store) StockPorAlmacen(ctx context.Context) ([]StockAlmacen, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT w.code, w.name,
		       COALESCE(sum(vs.qty_on_hand), 0),
		       count(DISTINCT vs.variant_id)::int
		FROM odoo_warehouses w
		LEFT JOIN variant_stock vs ON vs.odoo_warehouse_id = w.id
		GROUP BY w.id, w.code, w.name
		ORDER BY sum(vs.qty_on_hand) DESC NULLS LAST`)
	if err != nil {
		return nil, fmt.Errorf("leyendo el stock por almacén: %w", err)
	}
	defer filas.Close()

	var out []StockAlmacen
	for filas.Next() {
		var a StockAlmacen
		if err := filas.Scan(&a.Codigo, &a.Nombre, &a.Unidades, &a.Variantes); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, filas.Err()
}

// ordenar construye el ORDER BY a partir de lo que pidió la pantalla.
//
// El nombre va siempre de segundo criterio: sin él, ordenar por marca o por
// precio deja las filas empatadas en un orden que cambia entre páginas, y el
// mismo producto aparece dos veces al pasar de página o no aparece nunca.
func ordenar(f FiltroProductos) string {
	col, ok := ordenes[f.Orden]
	if !ok {
		return "p.name"
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	// NULLS LAST en los dos sentidos: un producto sin precio no es «el más
	// barato», es uno al que le falta el dato, y encabezar la lista con ellos
	// esconde justo lo que se buscaba al ordenar.
	if col == "p.name" {
		return "p.name " + dir
	}
	return col + " " + dir + " NULLS LAST, p.name ASC"
}
