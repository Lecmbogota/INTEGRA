package store

import (
	"context"
	"fmt"
)

// Este fichero reúne las escrituras que hace una persona desde la interfaz.
// Son los campos propiedad de Integra: el sync con Odoo jamás los toca, así
// que la única vía de cambio es esta.

// ActualizarPrecio fija el PVP de una variante. Nulo lo borra.
func (s *Store) ActualizarPrecio(ctx context.Context, varianteID int64, precio *float64) error {
	if precio != nil && *precio < 0 {
		return fmt.Errorf("el precio no puede ser negativo")
	}
	et, err := s.pool.Exec(ctx, `
		UPDATE product_variants
		SET price = $2, price_updated_at = now(), updated_at = now()
		WHERE id = $1`, varianteID, precio)
	if err != nil {
		return fmt.Errorf("guardando el precio: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe la variante %d", varianteID)
	}
	return nil
}

// ActualizarVariante cambia los datos físicos que ya no vienen de Odoo.
// Los punteros nulos significan "no tocar este campo".
func (s *Store) ActualizarVariante(ctx context.Context, varianteID int64, barcode *string, peso *float64) error {
	et, err := s.pool.Exec(ctx, `
		UPDATE product_variants
		SET barcode = CASE WHEN $2::text IS NULL THEN barcode ELSE NULLIF(TRIM($2), '') END,
		    weight  = COALESCE($3, weight),
		    updated_at = now()
		WHERE id = $1`, varianteID, barcode, peso)
	if err != nil {
		return fmt.Errorf("guardando la variante: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe la variante %d", varianteID)
	}
	return nil
}

// ActualizarDimensiones guarda el tamaño del paquete.
//
// Es el dato con el que los canales calculan el peso volumétrico
// (largo × ancho × alto / 5000) y cobran el envío por el mayor entre ese y el
// peso real. Sin él, el envío sale mal cobrado o el canal bloquea la venta.
func (s *Store) ActualizarDimensiones(ctx context.Context, varianteID int64, largo, ancho, alto *float64) error {
	for _, v := range []*float64{largo, ancho, alto} {
		if v != nil && *v < 0 {
			return fmt.Errorf("las dimensiones no pueden ser negativas")
		}
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE product_variants
		SET largo_cm = COALESCE($2, largo_cm), ancho_cm = COALESCE($3, ancho_cm),
		    alto_cm = COALESCE($4, alto_cm), updated_at = now()
		WHERE id = $1`, varianteID, largo, ancho, alto)
	return err
}

// ActualizarFichaComercial guarda condición, garantía, vídeo y nota interna.
func (s *Store) ActualizarFichaComercial(ctx context.Context, productoID int64,
	condicion string, garantiaMeses *int, garantiaTipo, videoURL, notaInterna *string) error {

	if condicion != "" {
		switch condicion {
		case "nuevo", "usado", "reacondicionado":
		default:
			return fmt.Errorf("condición inválida: %q", condicion)
		}
	}
	if garantiaTipo != nil && *garantiaTipo != "" {
		switch *garantiaTipo {
		case "fabricante", "vendedor", "sin_garantia":
		default:
			return fmt.Errorf("tipo de garantía inválido: %q", *garantiaTipo)
		}
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE products SET
		    condicion = COALESCE(NULLIF($2,''), condicion),
		    garantia_meses = COALESCE($3, garantia_meses),
		    garantia_tipo = CASE WHEN $4::text IS NULL THEN garantia_tipo
		                         ELSE NULLIF($4,'') END,
		    video_url = CASE WHEN $5::text IS NULL THEN video_url ELSE NULLIF(TRIM($5),'') END,
		    nota_interna = CASE WHEN $6::text IS NULL THEN nota_interna ELSE NULLIF(TRIM($6),'') END,
		    updated_at = now()
		WHERE id = $1`,
		productoID, condicion, garantiaMeses, garantiaTipo, videoURL, notaInterna)
	if err != nil {
		return fmt.Errorf("guardando la ficha comercial: %w", err)
	}
	return nil
}

// ActualizarTitulo fija el título de un canal concreto.
//
// El título es lo más importante de una publicación y cada canal tiene su
// límite —MercadoLibre solo admite 60 caracteres—, así que se guarda uno por
// canal. Marcar el contenido como editado protege el texto del generador.
func (s *Store) ActualizarTitulo(ctx context.Context, productoID int64, canal, titulo string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO product_content (product_id, titulos, editado, generado_at, updated_at)
		VALUES ($1, jsonb_build_object($2::text, $3::text), TRUE, now(), now())
		ON CONFLICT (product_id) DO UPDATE
		SET titulos = product_content.titulos || jsonb_build_object($2::text, $3::text),
		    editado = TRUE, updated_at = now()`,
		productoID, canal, titulo)
	if err != nil {
		return fmt.Errorf("guardando el título de %s: %w", canal, err)
	}
	return nil
}

// ActualizarMarca asigna la marca de un producto a partir de su literal,
// creándola si no existe. El literal vacío quita la marca.
func (s *Store) ActualizarMarca(ctx context.Context, productoID int64, literal string) error {
	marcaID, err := s.ResolverMarca(ctx, literal)
	if err != nil {
		return err
	}
	var marca any
	if marcaID != 0 {
		marca = marcaID
	}
	et, err := s.pool.Exec(ctx, `
		UPDATE products SET brand_id = $2, brand_raw = $3, updated_at = now()
		WHERE id = $1`, productoID, marca, nulo(literal))
	if err != nil {
		return fmt.Errorf("guardando la marca: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe el producto %d", productoID)
	}
	return nil
}

// ActualizarDescripcion guarda la descripción escrita a mano.
//
// Va a product_content con editado=true: así el generador automático nunca la
// sobrescribe, que es la garantía central del contenido editado.
func (s *Store) ActualizarDescripcion(ctx context.Context, productoID int64, texto string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO product_content (product_id, descripcion, editado, generado_at, updated_at)
		VALUES ($1, $2, TRUE, now(), now())
		ON CONFLICT (product_id) DO UPDATE
		SET descripcion = EXCLUDED.descripcion, editado = TRUE, updated_at = now()`,
		productoID, nulo(texto))
	if err != nil {
		return fmt.Errorf("guardando la descripción: %w", err)
	}
	return nil
}

// ActualizarExclusion mete o saca un producto del catálogo publicable.
func (s *Store) ActualizarExclusion(ctx context.Context, productoID int64, excluido bool) error {
	var motivo any
	if excluido {
		motivo = "manual"
	}
	et, err := s.pool.Exec(ctx, `
		UPDATE products SET excluded_reason = $2, updated_at = now()
		WHERE id = $1`, productoID, motivo)
	if err != nil {
		return fmt.Errorf("guardando la exclusión: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe el producto %d", productoID)
	}
	return nil
}

// RecalcularAtencion reconstruye la cola de atención a partir del estado
// actual, mezclando lo que viene de Odoo (SKU, stock) con lo que es de
// Integra (precio, marca, descripción).
//
// Se recalcula entera en SQL en vez de fila a fila: con ~600 variantes es
// instantáneo y garantiza que un problema resuelto por una edición desaparece
// de la cola en la misma transacción.
func (s *Store) RecalcularAtencion(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM attention_queue WHERE channel_account_id IS NULL`); err != nil {
		return fmt.Errorf("limpiando la cola de atención: %w", err)
	}

	// La descripción cuenta si existe en product_content (propiedad de
	// Integra) o quedó heredada en description_sale de la época en que se leía
	// de Odoo: el mismo criterio que usa la vista previa.
	//
	// La referencia repetida bloquea a todos los productos que la comparten:
	// dos product.product con el mismo default_code se pisarían la ficha en el
	// canal y una venta de ese SKU no sabría contra cuál montarse en Odoo, así
	// que ninguno se publica hasta que se corrija en Odoo, que es donde está
	// el error. Se agrupa una sola vez sobre el catálogo, y sin distinguir
	// mayúsculas porque así emparejan los pedidos. Dentro de cada conexión:
	// el mismo SKU en dos conexiones es un producto visto desde dos instancias
	// (pasa mientras dura `conexiones migrar`), no dos productos.
	_, err = tx.Exec(ctx, `
		INSERT INTO attention_queue (variant_id, channel_account_id, reason, detail, severity, last_seen_at)
		SELECT v.id, NULL, m.reason, m.detail, m.severity, now()
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		LEFT JOIN product_content c ON c.product_id = p.id
		LEFT JOIN (
		    SELECT p2.odoo_connection_id AS conexion, lower(v2.sku) AS sku,
		           string_agg(p2.name, ', ' ORDER BY p2.name) AS productos
		    FROM product_variants v2 JOIN products p2 ON p2.id = v2.product_id
		    WHERE v2.active AND v2.sku IS NOT NULL
		    GROUP BY p2.odoo_connection_id, lower(v2.sku) HAVING count(*) > 1
		) rep ON rep.conexion = p.odoo_connection_id AND rep.sku = lower(v.sku)
		CROSS JOIN LATERAL (VALUES
		  ('missing_sku',
		   'los cuatro canales exigen referencia interna',
		   'blocking',
		   NULLIF(TRIM(COALESCE(v.sku,'')), '') IS NULL),
		  ('duplicate_sku',
		   'la referencia ' || v.sku || ' está en más de un producto de Odoo: ' || rep.productos,
		   'blocking',
		   rep.sku IS NOT NULL),
		  ('missing_description',
		   'sin descripción; se escribe en Integra',
		   'blocking',
		   NULLIF(TRIM(COALESCE(c.descripcion, p.description_sale, '')), '') IS NULL),
		  ('missing_price',
		   'sin precio asignado en Integra',
		   'blocking',
		   v.price IS NULL),
		  ('title_too_long',
		   length(p.name) || ' caracteres; MercadoLibre admite 60',
		   'warning',
		   length(p.name) > 60),
		  ('missing_brand',
		   'sin marca asignada en Integra',
		   'warning',
		   p.brand_id IS NULL),
		  ('no_stock',
		   'sin existencias en ningún almacén',
		   'warning',
		   NOT EXISTS (SELECT 1 FROM variant_stock st
		               WHERE st.variant_id = v.id AND st.qty_on_hand > 0)),
		  ('image_mismatch',
		   'la portada no parece mostrar este producto (verificación visual)',
		   'warning',
		   EXISTS (SELECT 1 FROM producto_imagenes pi
		           WHERE pi.product_id = p.id AND pi.principal
		             AND pi.verificacion = 'no_corresponde'))
		) AS m(reason, detail, severity, aplica)
		WHERE v.active AND p.active AND p.excluded_reason IS NULL AND m.aplica`)
	if err != nil {
		return fmt.Errorf("recalculando la cola de atención: %w", err)
	}
	return tx.Commit(ctx)
}
