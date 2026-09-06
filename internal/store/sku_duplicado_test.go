package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Dos product.product de Odoo con el mismo default_code. Nada lo impedía ni
// lo detectaba: los dos salían como candidatos de publicación, el segundo
// adoptaba por SKU la ficha del primero en el canal y la sobreescribía en
// cada pasada, y una venta de ese SKU se emparejaba con uno de los dos al
// azar —LIMIT 1 sin ORDER BY—, así que se descontaba stock y se montaba el
// pedido en Odoo contra el producto equivocado: se despachaba otra mercancía.
//
// Estas pruebas montan la colisión con dos productos distintos, con la
// referencia escrita en distinta caja porque los pedidos se emparejan sin
// distinguirla, y un tercero con referencia propia que sirve de control.

func fixtureSKURepetido(t *testing.T, st *Store) (cuentaID, varA, varB, varControl int64) {
	t.Helper()
	ctx := context.Background()

	// La cuenta va activa porque CandidatosPublicacion ignora las inactivas.
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-sku-repetido-test', '\x00'::bytea, true FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID); err != nil {
		t.Fatal(err)
	}
	var conexionID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-sku-repetido-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})

	alta := func(plantilla int64, nombre, sku string) int64 {
		var prodID, varID int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO products (odoo_connection_id, odoo_template_id, name)
			VALUES ($1, $2, $3) RETURNING id`, conexionID, plantilla, nombre).Scan(&prodID); err != nil {
			t.Fatal(err)
		}
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO product_variants (product_id, odoo_product_id, sku)
			VALUES ($1, $2, $3) RETURNING id`, prodID, plantilla, sku).Scan(&varID); err != nil {
			t.Fatal(err)
		}
		return varID
	}
	varA = alta(880001, "Cargador 65W (ficha buena)", "DUP-TEST-001")
	varB = alta(880002, "Cargador 65W (duplicado)", "dup-test-001")
	varControl = alta(880003, "Cable USB-C (control)", "CTRL-TEST-001")
	return cuentaID, varA, varB, varControl
}

// separarSKU deshace la colisión como lo haría el sync tras corregirla en Odoo.
func separarSKU(t *testing.T, st *Store, varianteID int64) {
	t.Helper()
	if _, err := st.pool.Exec(context.Background(),
		`UPDATE product_variants SET sku = 'DUP-TEST-001-B' WHERE id = $1`, varianteID); err != nil {
		t.Fatal(err)
	}
}

// mismoSKUEnOtraConexion mete el producto de control, con su misma
// referencia, en una segunda conexión de Odoo: es lo que hay mientras dura
// `conexiones migrar`, y es el mismo producto visto desde dos instancias, no
// dos productos.
func mismoSKUEnOtraConexion(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	var conexionID, prodID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-sku-repetido-test-2','http://y','y','y','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 880003, 'Cable USB-C (control, otra instancia)') RETURNING id`, conexionID).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 880003, 'CTRL-TEST-001')`, prodID); err != nil {
		t.Fatal(err)
	}
}

func candidata(cands []CandidatoPublicacion, varianteID int64) bool {
	for _, c := range cands {
		if c.VarianteID == varianteID {
			return true
		}
	}
	return false
}

func TestUnaReferenciaRepetidaBloqueaLaPublicacionDeLosDosProductos(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, varA, varB, varControl := fixtureSKURepetido(t, st)
	mismoSKUEnOtraConexion(t, st)

	cands, err := st.CandidatosPublicacion(ctx, cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	if candidata(cands, varA) || candidata(cands, varB) {
		t.Error("con la referencia repetida ninguno de los dos debe ser candidato: el segundo pisaría la ficha del primero en el canal")
	}
	if !candidata(cands, varControl) {
		t.Error("el producto con referencia propia sí debe seguir siendo candidato, aunque otra conexión tenga su mismo SKU")
	}

	// Corregida la colisión en Odoo, los dos vuelven a poder publicarse.
	separarSKU(t, st, varB)
	if cands, err = st.CandidatosPublicacion(ctx, cuentaID); err != nil {
		t.Fatal(err)
	}
	if !candidata(cands, varA) || !candidata(cands, varB) {
		t.Error("con referencias distintas los dos deben volver a ser candidatos")
	}
}

func TestLaReferenciaRepetidaEntraEnLaColaDeAtencionComoBloqueante(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	_, varA, varB, varControl := fixtureSKURepetido(t, st)
	mismoSKUEnOtraConexion(t, st)

	motivoDe := func(varianteID int64) (severidad, detalle string, hay bool) {
		err := st.pool.QueryRow(ctx, `
			SELECT severity, COALESCE(detail,'') FROM attention_queue
			WHERE variant_id = $1 AND reason = 'duplicate_sku' AND resolved_at IS NULL`,
			varianteID).Scan(&severidad, &detalle)
		if err != nil {
			return "", "", false
		}
		return severidad, detalle, true
	}

	if err := st.RecalcularAtencion(ctx); err != nil {
		t.Fatal(err)
	}
	sev, det, hay := motivoDe(varA)
	if !hay || sev != "blocking" {
		t.Fatalf("la ficha buena debe tener el motivo bloqueante duplicate_sku: hay=%v severidad=%q", hay, sev)
	}
	// El detalle nombra al otro producto: es lo que hay que ir a corregir en
	// Odoo, y sin el nombre la cola solo diría que "algo" se repite.
	if !strings.Contains(det, "Cargador 65W (duplicado)") {
		t.Errorf("el detalle no dice con quién choca: %q", det)
	}
	if _, det, hay = motivoDe(varB); !hay || !strings.Contains(det, "Cargador 65W (ficha buena)") {
		t.Errorf("el duplicado también debe quedar bloqueado y nombrar al otro: hay=%v detalle=%q", hay, det)
	}
	if _, _, hay = motivoDe(varControl); hay {
		t.Error("el producto con referencia propia no debe aparecer como repetido: la otra conexión es la misma mercancía")
	}

	// Al separar las referencias el motivo desaparece en el siguiente recálculo.
	separarSKU(t, st, varB)
	if err := st.RecalcularAtencion(ctx); err != nil {
		t.Fatal(err)
	}
	for _, v := range []int64{varA, varB} {
		if _, _, hay = motivoDe(v); hay {
			t.Errorf("la variante %d sigue marcada como repetida después de separar las referencias", v)
		}
	}
}

func TestUnPedidoDeUnaReferenciaRepetidaNoSeEmparejaConNingunProducto(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, varA, varB, varControl := fixtureSKURepetido(t, st)

	ordenID, nuevo, err := st.GuardarOrden(ctx, DatosOrden{
		CuentaID: cuentaID, ExternalID: "PEDIDO-SKU-REPETIDO-1", Numero: "1",
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 300,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{
			{SKU: "DUP-TEST-001", Titulo: "Cargador", Cantidad: 1, PrecioUnit: 200, Total: 200},
			{SKU: "CTRL-TEST-001", Titulo: "Cable", Cantidad: 1, PrecioUnit: 100, Total: 100},
		},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido: %v (nuevo=%v)", err, nuevo)
	}

	varianteDe := func(sku string) *int64 {
		var v *int64
		if err := st.pool.QueryRow(ctx, `
			SELECT variant_id FROM channel_order_lines
			WHERE channel_order_id = $1 AND channel_sku = $2`, ordenID, sku).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	// La línea ambigua queda sin variante: elegir una al azar es despachar
	// otra mercancía la mitad de las veces. La de control se empareja igual
	// que siempre.
	if v := varianteDe("DUP-TEST-001"); v != nil {
		t.Errorf("la línea del SKU repetido se emparejó con la variante %d; no hay forma de saber cuál de las dos compró el cliente", *v)
	}
	if v := varianteDe("CTRL-TEST-001"); v == nil || *v != varControl {
		t.Errorf("la línea de control debía emparejarse con %d, quedó %v", varControl, v)
	}

	// El rescate tampoco puede adivinar mientras dure la colisión.
	if err := st.MarcarOrdenFallida(ctx, ordenID, "sin mapear"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReemparejarLineasHuerfanas(ctx); err != nil {
		t.Fatal(err)
	}
	if v := varianteDe("DUP-TEST-001"); v != nil {
		t.Errorf("el rescate emparejó la línea ambigua con la variante %d", *v)
	}

	// Separadas las referencias, el pedido se empareja con la que conserva
	// el SKU vendido y vuelve solo a la cola de montaje.
	separarSKU(t, st, varB)
	if _, _, err := st.ReemparejarLineasHuerfanas(ctx); err != nil {
		t.Fatal(err)
	}
	if v := varianteDe("DUP-TEST-001"); v == nil || *v != varA {
		t.Errorf("tras separar las referencias la línea debía ir a %d, quedó %v", varA, v)
	}
	var estado string
	if err := st.pool.QueryRow(ctx,
		`SELECT status::text FROM channel_orders WHERE id = $1`, ordenID).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "received" {
		t.Errorf("el pedido debía volver a la cola de montaje, está en %q", estado)
	}
}
