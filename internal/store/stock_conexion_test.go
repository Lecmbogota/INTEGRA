package store

import (
	"context"
	"testing"
)

// Sincronizar una conexión de Odoo dejaba a cero el stock de todas las demás:
// el DELETE de variant_stock no llevaba condición y solo se reinsertaban las
// filas de la conexión leída. Pasa justo durante el traslado entre instancias
// (`integra conexiones migrar`), que es cuando conviven dos con el mismo
// catálogo, y como las consultas de publicación no filtran por conexión, esos
// productos seguían siendo candidatos: el motor creaba una segunda publicación
// del mismo SKU con stock cero mientras la buena seguía viva.

func conexionConVariante(t *testing.T, st *Store, nombre, sku string, odooID int64) (conexionID, varianteID, almacenID int64) {
	t.Helper()
	ctx := context.Background()

	// La base exige (base_url, database) única, así que cada conexión de
	// prueba necesita las suyas.
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ($1, $2, $3, 'x', '\x00'::bytea, false) RETURNING id`,
		nombre, "http://"+nombre+".invalid", nombre).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	var prodID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, $2, 'Producto de prueba') RETURNING id`, conexionID, odooID).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, $2, $3) RETURNING id`, prodID, odooID, sku).Scan(&varianteID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_warehouses (odoo_connection_id, odoo_id, code, name)
		VALUES ($1, $2, $3, 'Bodega de prueba') RETURNING id`,
		conexionID, odooID, "T"+sku).Scan(&almacenID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_warehouses WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})
	return conexionID, varianteID, almacenID
}

func stockDe(t *testing.T, st *Store, varianteID int64) float64 {
	t.Helper()
	var total float64
	if err := st.pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(qty_on_hand),0) FROM variant_stock WHERE variant_id = $1`,
		varianteID).Scan(&total); err != nil {
		t.Fatal(err)
	}
	return total
}

func TestSincronizarUnaConexionNoBorraElStockDeOtra(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	conA, varA, almA := conexionConVariante(t, st, "conexion-vieja-test", "SKU-CONEX-A", 881001)
	conB, varB, almB := conexionConVariante(t, st, "conexion-nueva-test", "SKU-CONEX-B", 881002)

	if err := st.ReemplazarStock(ctx, conA, []FilaStock{
		{VarianteID: varA, AlmacenID: almA, OnHand: 40, Free: 40},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReemplazarStock(ctx, conB, []FilaStock{
		{VarianteID: varB, AlmacenID: almB, OnHand: 25, Free: 25},
	}); err != nil {
		t.Fatal(err)
	}

	if got := stockDe(t, st, varA); got != 40 {
		t.Errorf("la conexión vieja quedó con %v unidades; sincronizar la otra se lo llevaba por delante", got)
	}
	if got := stockDe(t, st, varB); got != 25 {
		t.Errorf("la conexión nueva quedó con %v unidades", got)
	}
}

func TestResincronizarReemplazaElStockDeSuPropiaConexion(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	con, variante, almacen := conexionConVariante(t, st, "conexion-resync-test", "SKU-RESYNC", 881003)

	if err := st.ReemplazarStock(ctx, con, []FilaStock{
		{VarianteID: variante, AlmacenID: almacen, OnHand: 100, Free: 100},
	}); err != nil {
		t.Fatal(err)
	}
	// La segunda lectura es la foto buena: sustituye, no suma.
	if err := st.ReemplazarStock(ctx, con, []FilaStock{
		{VarianteID: variante, AlmacenID: almacen, OnHand: 7, Free: 7},
	}); err != nil {
		t.Fatal(err)
	}

	if got := stockDe(t, st, variante); got != 7 {
		t.Errorf("quedaron %v unidades; la foto nueva debe sustituir a la vieja", got)
	}
}
