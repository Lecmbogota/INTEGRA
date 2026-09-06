package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// El pedido cobrado que nunca llega a Odoo.
//
// Un canal entrega un pedido de un SKU que todavía no se ha sincronizado: la
// línea se guarda sin variante, el montaje falla cinco veces y el pedido sale
// de la cola de pendientes. Sincronizar el catálogo después no lo rescataba,
// porque las líneas de un pedido ya guardado no se vuelven a emparejar y el
// contador de intentos solo sube. Esta prueba monta ese escenario entero.

func abrirStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	st, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("abriendo el store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestUnPedidoFallidoSeRescataCuandoApareceSuSKU(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	const sku = "SKU-RESCATE-TEST"
	var cuentaID, conexionID, prodID, varID, ordenID int64

	// Fixture: cuenta de canal, producto y pedido. Todo se deshace al final.
	err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-rescate-test', '\x00'::bytea, false FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	err = st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-rescate-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})

	// El pedido llega antes de que el producto exista en el catálogo.
	ordenID, nuevo, err := st.GuardarOrden(ctx, DatosOrden{
		CuentaID: cuentaID, ExternalID: "PEDIDO-RESCATE-1", Numero: "1",
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 100,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{{
			SKU: sku, Titulo: "Producto que aún no está", Cantidad: 1,
			PrecioUnit: 100, Total: 100,
		}},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido: %v (nuevo=%v)", err, nuevo)
	}

	var variante *int64
	if err := st.pool.QueryRow(ctx,
		`SELECT variant_id FROM channel_order_lines WHERE channel_order_id = $1`,
		ordenID).Scan(&variante); err != nil {
		t.Fatal(err)
	}
	if variante != nil {
		t.Fatal("la línea no debería tener variante: el SKU aún no existe")
	}

	// Cinco intentos fallidos: el pedido sale de la cola de pendientes.
	for i := 0; i < 5; i++ {
		if err := st.MarcarOrdenFallida(ctx, ordenID, "el SKU no existe en el catálogo"); err != nil {
			t.Fatal(err)
		}
	}
	pend, err := st.OrdenesPendientesOdoo(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range pend {
		if o.ID == ordenID {
			t.Fatal("tras cinco fallos el pedido ya no debería estar pendiente")
		}
	}

	// Ahora sí llega el producto, como haría un `integra sync`.
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 999111, 'Producto que ya está') RETURNING id`, conexionID).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 999111, $2) RETURNING id`, prodID, sku).Scan(&varID); err != nil {
		t.Fatal(err)
	}

	lineas, pedidos, err := st.ReemparejarLineasHuerfanas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lineas < 1 || pedidos < 1 {
		t.Fatalf("no se rescató nada: lineas=%d pedidos=%d", lineas, pedidos)
	}

	if err := st.pool.QueryRow(ctx,
		`SELECT variant_id FROM channel_order_lines WHERE channel_order_id = $1`,
		ordenID).Scan(&variante); err != nil {
		t.Fatal(err)
	}
	if variante == nil || *variante != varID {
		t.Errorf("la línea sigue sin emparejar: %v", variante)
	}

	pend, err = st.OrdenesPendientesOdoo(ctx, 200)
	if err != nil {
		t.Fatal(err)
	}
	vuelto := false
	for _, o := range pend {
		if o.ID == ordenID {
			vuelto = true
			if o.Intentos != 0 {
				t.Errorf("los intentos deben reiniciarse, quedaron en %d", o.Intentos)
			}
		}
	}
	if !vuelto {
		t.Error("el pedido debía volver a la cola de pendientes tras emparejar su SKU")
	}
}

// Un pedido al que todavía le falta alguna variante no se reactiva: giraría
// en la cola fallando siempre por lo mismo.
func TestNoSeReactivaLoQueSigueSinEmparejar(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	var cuentaID, ordenID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-rescate-test-2', '\x00'::bytea, false FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
	})

	ordenID, _, err := st.GuardarOrden(ctx, DatosOrden{
		CuentaID: cuentaID, ExternalID: "PEDIDO-RESCATE-2", Numero: "2",
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 100,
		Lineas: []DatosLinea{{SKU: "SKU-QUE-NUNCA-EXISTIRA-TEST", Cantidad: 1, PrecioUnit: 100, Total: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := st.MarcarOrdenFallida(ctx, ordenID, "sin mapear"); err != nil {
			t.Fatal(err)
		}
	}

	if _, _, err := st.ReemparejarLineasHuerfanas(ctx); err != nil {
		t.Fatal(err)
	}

	var estado string
	var intentos int
	if err := st.pool.QueryRow(ctx,
		`SELECT status::text, sync_attempts FROM channel_orders WHERE id = $1`,
		ordenID).Scan(&estado, &intentos); err != nil {
		t.Fatal(err)
	}
	if estado != "failed" || intentos != 5 {
		t.Errorf("no debía reactivarse: estado=%s intentos=%d", estado, intentos)
	}
}
