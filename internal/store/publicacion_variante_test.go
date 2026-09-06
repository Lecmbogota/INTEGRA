package store

import (
	"context"
	"testing"
)

// Dos variantes del mismo producto compartían el hash de contenido, porque
// vivía en product_channel_listings, que tiene una fila por producto y cuenta.
// Cada planificación las hacía pisarse el hash: ninguna coincidía nunca con su
// catálogo y las dos se reenviaban sin fin, gastando cupo en el endpoint más
// caro del canal. La migración 019 lo baja a la variante.

func fixturePublicacion(t *testing.T, st *Store) (cuentaID, prodID, varA, varB int64) {
	t.Helper()
	ctx := context.Background()

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-hash-test', '\x00'::bytea, false FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID); err != nil {
		t.Fatal(err)
	}
	var conexionID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-hash-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 777001, 'Camiseta con dos tallas') RETURNING id`, conexionID).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 777001, 'CAM-M-TEST') RETURNING id`, prodID).Scan(&varA); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 777002, 'CAM-L-TEST') RETURNING id`, prodID).Scan(&varB); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})
	return cuentaID, prodID, varA, varB
}

func hashDeVariante(t *testing.T, st *Store, cuentaID, varianteID int64) string {
	t.Helper()
	var h *string
	if err := st.pool.QueryRow(context.Background(),
		`SELECT content_hash FROM variant_channel_listings
		 WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID).Scan(&h); err != nil {
		t.Fatal(err)
	}
	if h == nil {
		return ""
	}
	return *h
}

func TestDosVariantesNoSePisanElHashDeContenido(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varA, varB := fixturePublicacion(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "hash-de-la-M", "ph-a", "sh-a", 1000, 5); err != nil {
		t.Fatal(err)
	}
	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varB,
		"EXT-1", "", "VAR-B", "hash-de-la-L", "ph-b", "sh-b", 1000, 7); err != nil {
		t.Fatal(err)
	}

	if got := hashDeVariante(t, st, cuentaID, varA); got != "hash-de-la-M" {
		t.Errorf("la variante M perdió su hash: %q; publicar la L se lo pisaba", got)
	}
	if got := hashDeVariante(t, st, cuentaID, varB); got != "hash-de-la-L" {
		t.Errorf("la variante L guardó %q", got)
	}
}

func TestActualizarLaFichaNoBorraElPrecioNiElStockPublicados(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varA, _ := fixturePublicacion(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "hash-viejo", "ph-1", "sh-1", 189900, 6); err != nil {
		t.Fatal(err)
	}
	// Se manda solo la ficha, como hace el motor sobre una publicación viva.
	if err := st.GuardarContenidoPublicado(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "hash-nuevo"); err != nil {
		t.Fatal(err)
	}

	var precio float64
	var cantidad int
	var ph, sh string
	if err := st.pool.QueryRow(ctx, `
		SELECT published_price, published_qty, COALESCE(price_hash,''), COALESCE(stock_hash,'')
		FROM variant_channel_listings WHERE variant_id = $1 AND channel_account_id = $2`,
		varA, cuentaID).Scan(&precio, &cantidad, &ph, &sh); err != nil {
		t.Fatal(err)
	}
	if precio != 189900 || cantidad != 6 {
		t.Errorf("el precio y el stock publicados se perdieron: %v / %v; el canal sigue teniéndolos", precio, cantidad)
	}
	if ph != "ph-1" || sh != "sh-1" {
		t.Errorf("los hashes de precio y stock no son de este envío: %q / %q", ph, sh)
	}
	if got := hashDeVariante(t, st, cuentaID, varA); got != "hash-nuevo" {
		t.Errorf("el contenido sí debía anotarse: %q", got)
	}
}

func TestLaReferenciaUsaElSKUConElQueSePublico(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varA, _ := fixturePublicacion(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "h", "ph", "sh", 1000, 1); err != nil {
		t.Fatal(err)
	}
	// Alguien renombra el SKU en Odoo. El canal sigue conociendo el viejo, y
	// en Falabella el SKU es la referencia de la publicación.
	if _, err := st.pool.Exec(ctx,
		`UPDATE product_variants SET sku = 'CAM-M-RENOMBRADO' WHERE id = $1`, varA); err != nil {
		t.Fatal(err)
	}

	ref, err := st.RefDePublicacion(ctx, cuentaID, varA)
	if err != nil {
		t.Fatal(err)
	}
	if ref.SKU != "CAM-M-TEST" {
		t.Errorf("la referencia devolvió %q; tiene que ser el SKU publicado, no el actual", ref.SKU)
	}
}

// La ventana que cubre la prueba anterior se cerraba sola: el SKU entra en el
// hash de contenido, así que el renombrado dispara una republicación, y al
// anotarla se volvía a tomar el SKU de la variante. Desde ahí la referencia
// era el nuevo, que el canal no conoce, y en Falabella ningún envío de precio
// ni de stock volvía a llegar.
func TestRepublicarLaFichaNoPisaElSKUConElQueSePublico(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varA, _ := fixturePublicacion(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "h", "ph", "sh", 1000, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx,
		`UPDATE product_variants SET sku = 'CAM-M-RENOMBRADO' WHERE id = $1`, varA); err != nil {
		t.Fatal(err)
	}
	// El motor republica la ficha con el hash nuevo. Update no manda el SKU
	// en ningún canal, así que el canal sigue con el viejo.
	if err := st.GuardarContenidoPublicado(ctx, cuentaID, prodID, varA,
		"EXT-1", "", "VAR-A", "hash-con-el-sku-nuevo"); err != nil {
		t.Fatal(err)
	}

	ref, err := st.RefDePublicacion(ctx, cuentaID, varA)
	if err != nil {
		t.Fatal(err)
	}
	if ref.SKU != "CAM-M-TEST" {
		t.Errorf("tras republicar, la referencia devolvió %q; el canal sigue conociendo el publicado", ref.SKU)
	}
}
