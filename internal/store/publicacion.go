package store

import (
	"context"
	"fmt"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// CandidatoPublicacion es una variante vista desde la óptica de "¿qué habría
// que mandar a este canal?": el contenido actual y lo último publicado.
type CandidatoPublicacion struct {
	VarianteID  int64
	ProductoID  int64
	SKU         string
	Titulo      string
	Descripcion string
	Marca       string
	Barcode     string
	Peso        float64
	// Medidas del paquete en centímetros. Falabella las exige por separado
	// dentro de ProductData y rechaza el alta entera si faltan: el peso solo
	// no basta.
	LargoCm  float64
	AnchoCm  float64
	AltoCm   float64
	Imagenes []string

	CategoriaCanal string
	PrecioBase     float64
	PrecioCanal    float64
	Moneda         string
	Stock          int

	// Estado de la publicación en el canal (vacío si nunca se publicó).
	ExternalID  string
	ContentHash string
	PriceHash   string
	StockHash   string

	// Listo indica que cumple todo lo exigible antes de intentar publicar.
	Listo bool
}

// CandidatosPublicacion arma la foto del catálogo publicable para una cuenta,
// con el precio ya ajustado por la comisión del canal y el stock limitado a
// las bodegas asignadas a esa cuenta.
//
// Sin bodegas asignadas se niega (ErrCuentaSinBodegas) en vez de sumar todas.
// «Todas» fue el valor por defecto desde el primer esquema, y como nada las
// asignaba, cada canal publicaba también Muestras, Garantías y el stock
// consignado en las bodegas de Falabella: WooCommerce ofrecía unidades que
// estaban en el centro de distribución de Falabella y la venta había que
// cancelarla. El desglose por bodega existe justo para no contar eso.
func (s *Store) CandidatosPublicacion(ctx context.Context, cuentaID int64) ([]CandidatoPublicacion, error) {
	var canalCodigo, nombreCuenta string
	var comision, costoFijo float64
	var conBodegas bool
	err := s.pool.QueryRow(ctx, `
		SELECT ch.code, ch.comision_pct, ch.costo_fijo, a.name,
		       EXISTS (SELECT 1 FROM channel_account_warehouses w WHERE w.channel_account_id = a.id)
		FROM channel_accounts a JOIN channels ch ON ch.id = a.channel_id
		WHERE a.id = $1 AND a.active`, cuentaID).Scan(&canalCodigo, &comision, &costoFijo, &nombreCuenta, &conBodegas)
	if err != nil {
		return nil, fmt.Errorf("no existe la cuenta %d: %w", cuentaID, err)
	}
	if !conBodegas {
		return nil, fmt.Errorf("la cuenta %q %w", nombreCuenta, ErrCuentaSinBodegas)
	}
	canal := Canal{Codigo: canalCodigo, ComisionPct: comision, CostoFijo: costoFijo}

	filas, err := s.pool.Query(ctx, `
		SELECT v.id, p.id, COALESCE(v.sku,''),
		       COALESCE(c.titulos->>$2, p.name),
		       COALESCE(c.descripcion, COALESCE(p.description_sale,'')),
		       COALESCE(b.name,''), COALESCE(v.barcode,''), COALESCE(v.weight,0),
		       COALESCE(v.largo_cm,0), COALESCE(v.ancho_cm,0), COALESCE(v.alto_cm,0),
		       COALESCE(cm.channel_category_id, ''),
		       COALESCE(v.price, 0),
		       COALESCE(ep.sale_price, ep.regular_price, 0),
		       COALESCE(st.total, 0)::int,
		       COALESCE(pcl.external_id, ''),
		       -- El hash de contenido se lee de la variante: en
		       -- product_channel_listings hay una sola fila por producto, así
		       -- que dos variantes se pisaban el hash entre sí y ninguna
		       -- coincidía nunca con su catálogo. Se cae al del producto solo
		       -- para las filas anteriores a la migración 019.
		       COALESCE(vcl.content_hash, pcl.content_hash, ''),
		       COALESCE(vcl.price_hash, ''), COALESCE(vcl.stock_hash, ''),
		       COALESCE(ARRAY(
		           SELECT i.sha256 FROM producto_imagenes pi
		           JOIN imagenes i ON i.id = pi.imagen_id
		           WHERE pi.product_id = p.id
		             AND least(i.ancho, i.alto) >= 600 AND i.formato IN ('jpeg','png')
		           ORDER BY pi.principal DESC, pi.posicion), '{}')
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN product_content c ON c.product_id = p.id
		LEFT JOIN effective_prices ep ON ep.variant_id = v.id AND ep.channel_account_id = $1
		LEFT JOIN LATERAL (
		    SELECT sum(vs.qty_on_hand) AS total
		    FROM variant_stock vs
		    WHERE vs.variant_id = v.id
		      AND vs.odoo_warehouse_id IN (
		          SELECT w.odoo_warehouse_id FROM channel_account_warehouses w
		          WHERE w.channel_account_id = $1)
		) st ON TRUE
		LEFT JOIN category_mappings cm ON cm.odoo_categ_path = p.categ_path
		     AND cm.channel_account_id IS NULL AND cm.confirmado_at IS NOT NULL
		     AND cm.channel_id = (SELECT channel_id FROM channel_accounts WHERE id = $1)
		LEFT JOIN product_channel_listings pcl ON pcl.product_id = p.id AND pcl.channel_account_id = $1
		LEFT JOIN variant_channel_listings vcl ON vcl.variant_id = v.id AND vcl.channel_account_id = $1
		WHERE v.active AND p.active AND p.excluded_reason IS NULL AND v.sku IS NOT NULL
		  -- Una referencia repetida en Odoo no se publica por ninguno de los
		  -- productos que la comparten: el segundo adoptaría por SKU la ficha
		  -- del primero y la pisaría en cada pasada, y una venta de ese SKU
		  -- no sabría de cuál es. Es la misma comparación, sin distinguir
		  -- mayúsculas, con la que se emparejan los pedidos, acotada a la
		  -- conexión porque el mismo SKU en otra instancia es el mismo
		  -- producto, no otro.
		  AND NOT EXISTS (SELECT 1 FROM product_variants v2
		                  JOIN products p2 ON p2.id = v2.product_id
		                  WHERE v2.id <> v.id AND v2.active
		                    AND p2.odoo_connection_id = p.odoo_connection_id
		                    AND lower(v2.sku) = lower(v.sku))
		ORDER BY p.name`, cuentaID, canalCodigo)
	if err != nil {
		return nil, fmt.Errorf("listando candidatos de publicación: %w", err)
	}
	defer filas.Close()

	var out []CandidatoPublicacion
	for filas.Next() {
		var c CandidatoPublicacion
		var precioEfectivo float64
		if err := filas.Scan(&c.VarianteID, &c.ProductoID, &c.SKU, &c.Titulo, &c.Descripcion,
			&c.Marca, &c.Barcode, &c.Peso, &c.LargoCm, &c.AnchoCm, &c.AltoCm, &c.CategoriaCanal, &c.PrecioBase, &precioEfectivo, &c.Stock,
			&c.ExternalID, &c.ContentHash, &c.PriceHash, &c.StockHash, &c.Imagenes); err != nil {
			return nil, err
		}
		c.Moneda = "COP"
		if precioEfectivo > 0 {
			c.PrecioCanal = precioEfectivo
		} else {
			c.PrecioCanal = PrecioParaCanal(c.PrecioBase, canal)
		}
		// Listo = lo mínimo que exige cualquier canal. La categoría solo la
		// piden MercadoLibre y Falabella; se comprueba en el adaptador.
		c.Listo = c.SKU != "" && c.Titulo != "" && c.Descripcion != "" &&
			(c.PrecioCanal > 0 || c.PrecioBase > 0) && len(c.Imagenes) > 0
		out = append(out, c)
	}
	return out, filas.Err()
}

// UnCandidato devuelve un candidato concreto, para que el manejador del
// trabajo tenga los datos frescos en el momento de enviarlos.
func (s *Store) UnCandidato(ctx context.Context, cuentaID, varianteID int64) (*CandidatoPublicacion, error) {
	todos, err := s.CandidatosPublicacion(ctx, cuentaID)
	if err != nil {
		return nil, err
	}
	for i := range todos {
		if todos[i].VarianteID == varianteID {
			return &todos[i], nil
		}
	}
	return nil, fmt.Errorf("la variante %d ya no es candidata de la cuenta %d", varianteID, cuentaID)
}

// GuardarPublicacion registra el resultado de publicar un producto.
func (s *Store) GuardarPublicacion(ctx context.Context, cuentaID, productoID, varianteID int64,
	externalID, externalURL, varianteExterna, contentHash, priceHash, stockHash string,
	precio float64, cantidad int) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var listingID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO product_channel_listings
		    (product_id, channel_account_id, external_id, external_url, status,
		     content_hash, last_published_at, last_error, last_error_at)
		VALUES ($1,$2,$3,$4,'published',$5, now(), NULL, NULL)
		ON CONFLICT (product_id, channel_account_id) DO UPDATE
		SET external_id = EXCLUDED.external_id, external_url = EXCLUDED.external_url,
		    status = 'published', content_hash = EXCLUDED.content_hash,
		    last_published_at = now(), last_error = NULL, last_error_at = NULL,
		    error_count = 0, updated_at = now()
		RETURNING id`,
		productoID, cuentaID, nulo(externalID), nulo(externalURL), nulo(contentHash)).Scan(&listingID)
	if err != nil {
		return fmt.Errorf("guardando la publicación: %w", err)
	}

	// channel_sku guarda el SKU con el que se publicó, tomado de la variante
	// en este momento. Se escribía siempre NULL, y por eso RefDePublicacion
	// tenía que caer en el SKU actual: si alguien renombraba el SKU en Odoo,
	// los envíos apuntaban a una referencia que el canal no conoce —en
	// Falabella el SKU *es* la referencia— y la publicación quedaba
	// inalcanzable sin que nada lo dijera.
	_, err = tx.Exec(ctx, `
		INSERT INTO variant_channel_listings
		    (listing_id, variant_id, channel_account_id, external_variant_id, channel_sku,
		     status, content_hash, price_hash, stock_hash, published_price, published_qty,
		     last_price_push_at, last_stock_push_at)
		VALUES ($1,$2,$3,$4,
		        (SELECT sku FROM product_variants WHERE id = $2),
		        'published',$5,$6,$7,$8,$9, now(), now())
		ON CONFLICT (variant_id, channel_account_id) DO UPDATE
		SET listing_id = EXCLUDED.listing_id,
		    external_variant_id = EXCLUDED.external_variant_id,
		    channel_sku = COALESCE(EXCLUDED.channel_sku, variant_channel_listings.channel_sku),
		    status = 'published',
		    content_hash = EXCLUDED.content_hash,
		    price_hash = EXCLUDED.price_hash, stock_hash = EXCLUDED.stock_hash,
		    published_price = EXCLUDED.published_price, published_qty = EXCLUDED.published_qty,
		    last_price_push_at = now(), last_stock_push_at = now(), updated_at = now()`,
		listingID, varianteID, cuentaID, nulo(varianteExterna),
		nulo(contentHash), nulo(priceHash), nulo(stockHash), precio, cantidad)
	if err != nil {
		return fmt.Errorf("guardando la variante publicada: %w", err)
	}
	return tx.Commit(ctx)
}

// GuardarContenidoPublicado anota que la ficha se envió, sin tocar el precio
// ni el stock.
//
// Existe aparte de GuardarPublicacion porque actualizar una publicación viva
// solo manda la ficha: en los cuatro canales el precio y el stock tienen su
// propio endpoint. Usar GuardarPublicacion en ese camino ponía a cero
// published_price y published_qty, que es mentira sobre lo que tiene el
// canal. Los hashes de precio y stock se dejan intactos a propósito: sus
// trabajos van aparte y son ellos los que deben anotarlos al enviarlos.
func (s *Store) GuardarContenidoPublicado(ctx context.Context, cuentaID, productoID, varianteID int64,
	externalID, externalURL, varianteExterna, contentHash string) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var listingID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO product_channel_listings
		    (product_id, channel_account_id, external_id, external_url, status,
		     content_hash, last_published_at, last_error, last_error_at)
		VALUES ($1,$2,$3,$4,'published',$5, now(), NULL, NULL)
		ON CONFLICT (product_id, channel_account_id) DO UPDATE
		SET external_id = COALESCE(EXCLUDED.external_id, product_channel_listings.external_id),
		    external_url = COALESCE(EXCLUDED.external_url, product_channel_listings.external_url),
		    status = 'published', content_hash = EXCLUDED.content_hash,
		    last_published_at = now(), last_error = NULL, last_error_at = NULL,
		    error_count = 0, updated_at = now()
		RETURNING id`,
		productoID, cuentaID, nulo(externalID), nulo(externalURL), nulo(contentHash)).Scan(&listingID)
	if err != nil {
		return fmt.Errorf("guardando el contenido publicado: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO variant_channel_listings
		    (listing_id, variant_id, channel_account_id, external_variant_id, channel_sku,
		     status, content_hash)
		VALUES ($1,$2,$3,$4,
		        (SELECT sku FROM product_variants WHERE id = $2),
		        'published',$5)
		ON CONFLICT (variant_id, channel_account_id) DO UPDATE
		SET listing_id = EXCLUDED.listing_id,
		    external_variant_id = COALESCE(EXCLUDED.external_variant_id,
		                                   variant_channel_listings.external_variant_id),
		    -- Actualizar la ficha no cambia el SKU con el que se publicó:
		    -- ninguno de los cuatro Update lo manda al canal, y Falabella
		    -- fuerza el viejo porque allí es la referencia. Preferir aquí el
		    -- de la variante hacía que la primera republicación tras un
		    -- renombrado en Odoo pisara el publicado, y desde ese momento los
		    -- envíos de precio y stock y el emparejamiento de pedidos
		    -- apuntaban a un SKU que el canal no conoce.
		    channel_sku = COALESCE(variant_channel_listings.channel_sku, EXCLUDED.channel_sku),
		    status = 'published', content_hash = EXCLUDED.content_hash, updated_at = now()`,
		listingID, varianteID, cuentaID, nulo(varianteExterna), nulo(contentHash))
	if err != nil {
		return fmt.Errorf("guardando el contenido de la variante: %w", err)
	}
	return tx.Commit(ctx)
}

// GuardarInventarioExterno anota el identificador de inventario que el canal
// usa para ajustar el stock. Shopify no lo hace sobre la variante sino sobre
// su inventory_item, que hay que resolver con una llamada aparte: guardarlo
// ahorra una petición en cada envío de stock, que son los más frecuentes.
func (s *Store) GuardarInventarioExterno(ctx context.Context, cuentaID, varianteID int64, inventarioID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings SET inventory_item_id = $3, updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID, nulo(inventarioID))
	return err
}

// RefDePublicacion devuelve la referencia externa de una variante ya
// publicada, que es lo que necesitan los envíos de precio y stock.
func (s *Store) RefDePublicacion(ctx context.Context, cuentaID, varianteID int64) (channel.ExternalRef, error) {
	var ref channel.ExternalRef
	err := s.pool.QueryRow(ctx, `
		-- Se prefiere el SKU con el que se publicó, no el actual de la
		-- variante: si alguien lo renombra en Odoo, el canal sigue conociendo
		-- el viejo, y en Falabella el SKU es la referencia de la publicación.
		SELECT COALESCE(l.external_id,''), COALESCE(v.external_variant_id,''),
		       COALESCE(v.channel_sku, pv.sku, '')
		FROM variant_channel_listings v
		JOIN product_channel_listings l ON l.id = v.listing_id
		JOIN product_variants pv ON pv.id = v.variant_id
		WHERE v.variant_id = $1 AND v.channel_account_id = $2`,
		varianteID, cuentaID).Scan(&ref.ListingID, &ref.VariantID, &ref.SKU)
	if err != nil {
		return ref, fmt.Errorf("la variante %d no está publicada en la cuenta %d", varianteID, cuentaID)
	}
	return ref, nil
}

// GuardarPrecioPublicado y GuardarStockPublicado anotan los envíos baratos.
func (s *Store) GuardarPrecioPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, precio float64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET price_hash = $3, published_price = $4, last_price_push_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID, hash, precio)
	return err
}

func (s *Store) GuardarStockPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, cantidad int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET stock_hash = $3, published_qty = $4, last_stock_push_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID, hash, cantidad)
	return err
}

// AnotarErrorPublicacion deja constancia del fallo en la publicación.
func (s *Store) AnotarErrorPublicacion(ctx context.Context, cuentaID, productoID int64, causa string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO product_channel_listings
		    (product_id, channel_account_id, status, last_error, last_error_at, error_count)
		VALUES ($1,$2,'error',$3, now(), 1)
		ON CONFLICT (product_id, channel_account_id) DO UPDATE
		SET status = 'error', last_error = EXCLUDED.last_error,
		    last_error_at = now(), error_count = product_channel_listings.error_count + 1,
		    updated_at = now()`, productoID, cuentaID, causa)
	return err
}

// TrabajosFallidos cuenta los que agotaron sus reintentos.
func (s *Store) TrabajosFallidos(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE status = 'failed'`).Scan(&n)
	return n, err
}

// PublicadosSinStock cuenta lo que sigue publicado pero ya no tiene
// existencias: es lo que produce una venta que no se puede despachar.
func (s *Store) PublicadosSinStock(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM variant_channel_listings v
		WHERE v.status = 'published'
		  AND NOT EXISTS (SELECT 1 FROM variant_stock st
		                  WHERE st.variant_id = v.variant_id AND st.qty_on_hand > 0)`).Scan(&n)
	return n, err
}

// PublicadosConSKURenombrado cuenta las publicaciones cuyo SKU en Odoo ya no
// es el que conoce el canal. Los pedidos y los envíos no se pierden —los dos
// usan el SKU publicado—, pero la ficha del marketplace muestra una
// referencia que el catálogo ya no tiene, y eso alguien lo tiene que decidir.
func (s *Store) PublicadosConSKURenombrado(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM variant_channel_listings v
		JOIN product_variants pv ON pv.id = v.variant_id
		WHERE v.channel_sku IS NOT NULL AND pv.sku IS NOT NULL
		  AND lower(v.channel_sku) <> lower(pv.sku)`).Scan(&n)
	return n, err
}

// ResumenPublicaciones alimenta el panel de estado por canal.
type ResumenPublicacion struct {
	Canal     string     `json:"canal"`
	CuentaID  int64      `json:"cuenta_id"`
	Activas   int        `json:"activas"`
	ConError  int        `json:"con_error"`
	Pendiente int        `json:"pendientes_cola"`
	UltimaPub *time.Time `json:"ultima_publicacion"`
}

func (s *Store) ResumenPublicaciones(ctx context.Context) ([]ResumenPublicacion, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT ch.code, a.id,
		       count(*) FILTER (WHERE l.status = 'published')::int,
		       count(*) FILTER (WHERE l.status = 'error')::int,
		       (SELECT count(*) FROM jobs j
		        WHERE j.channel_account_id = a.id AND j.status IN ('pending','running'))::int,
		       max(l.last_published_at)
		FROM channel_accounts a
		JOIN channels ch ON ch.id = a.channel_id
		LEFT JOIN product_channel_listings l ON l.channel_account_id = a.id
		WHERE a.active AND a.brand_id IS NULL
		GROUP BY ch.code, a.id, ch.id ORDER BY ch.id`)
	if err != nil {
		return nil, fmt.Errorf("resumen de publicaciones: %w", err)
	}
	defer filas.Close()

	var out []ResumenPublicacion
	for filas.Next() {
		var r ResumenPublicacion
		if err := filas.Scan(&r.Canal, &r.CuentaID, &r.Activas, &r.ConError, &r.Pendiente, &r.UltimaPub); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, filas.Err()
}
