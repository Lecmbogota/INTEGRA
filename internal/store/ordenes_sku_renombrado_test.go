package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Renombrar un SKU en Odoo no puede costar pedidos.
//
// El canal se queda con el SKU con el que se publicó: ningún Update se lo
// cambia y en Falabella ni siquiera se puede. Cada pedido de ese producto
// llega entonces con una referencia que el catálogo ya no tiene, y emparejando
// solo por product_variants.sku la línea quedaba sin variante, el montaje en
// Odoo fallaba cinco veces y el pedido salía de la cola: una venta cobrada al
// comprador que nunca llegaba. Estas pruebas recorren la secuencia real
// —publicar, renombrar en el sync, republicar, vender— y lo que queda para
// las personas: la cola de atención y la vigilancia.

const (
	skuViejo = "REN-VIEJO-TEST"
	skuNuevo = "REN-NUEVO-TEST"
)

// fixtureRenombrado deja un producto sincronizado y publicado con el SKU viejo
// en una cuenta de Falabella, donde el SKU es la referencia. El identificador
// de Odoo separa los productos de cada prueba.
func fixtureRenombrado(t *testing.T, st *Store, odooID int64) (cuentaID, conexionID, prodID, varID int64) {
	t.Helper()
	ctx := context.Background()

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-renombrado-test', '\x00'::bytea, false
		FROM channels WHERE code = 'falabella'
		RETURNING id`).Scan(&cuentaID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-renombrado-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})

	// El producto entra como lo trae el sync y se publica con ese SKU.
	prodID, varID, err := st.UpsertIdentidad(ctx, conexionID, Identidad{
		OdooTemplateID: odooID, OdooProductID: odooID, Nombre: "Producto que se renombra", SKU: skuViejo,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		skuViejo, "", skuViejo, "hash-inicial", "ph", "sh", 1000, 3); err != nil {
		t.Fatal(err)
	}
	return cuentaID, conexionID, prodID, varID
}

// renombrarEnOdoo hace lo que hace el sync cuando alguien cambia el SKU en
// Odoo: pisarlo.
func renombrarEnOdoo(t *testing.T, st *Store, conexionID, odooID int64) {
	t.Helper()
	if _, _, err := st.UpsertIdentidad(context.Background(), conexionID, Identidad{
		OdooTemplateID: odooID, OdooProductID: odooID, Nombre: "Producto que se renombra", SKU: skuNuevo,
	}); err != nil {
		t.Fatal(err)
	}
}

func pedidoCon(t *testing.T, st *Store, cuentaID int64, externo, sku string) int64 {
	t.Helper()
	ordenID, nuevo, err := st.GuardarOrden(context.Background(), DatosOrden{
		CuentaID: cuentaID, ExternalID: externo, Numero: externo,
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 1000,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{{
			SKU: sku, Titulo: "Producto que se renombra", Cantidad: 1, PrecioUnit: 1000, Total: 1000,
		}},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido %s: %v (nuevo=%v)", externo, err, nuevo)
	}
	return ordenID
}

func varianteDeLaLinea(t *testing.T, st *Store, ordenID int64) *int64 {
	t.Helper()
	var v *int64
	if err := st.pool.QueryRow(context.Background(),
		`SELECT variant_id FROM channel_order_lines WHERE channel_order_id = $1`, ordenID).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestUnPedidoConElSKUViejoEncuentraSuVarianteRenombrada(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	const odooID = 555001
	cuentaID, conexionID, prodID, varID := fixtureRenombrado(t, st, odooID)

	renombrarEnOdoo(t, st, conexionID, odooID)
	// El SKU entra en el hash de contenido, así que la siguiente
	// planificación republica la ficha. El canal no se entera del nombre
	// nuevo: ningún Update lo manda.
	if err := st.GuardarContenidoPublicado(ctx, cuentaID, prodID, varID,
		skuViejo, "", skuViejo, "hash-tras-renombrar"); err != nil {
		t.Fatal(err)
	}

	// Llega una venta. El canal manda el SKU con el que se publicó.
	got := varianteDeLaLinea(t, st, pedidoCon(t, st, cuentaID, "PEDIDO-REN-1", skuViejo))
	if got == nil || *got != varID {
		t.Fatalf("la línea con el SKU viejo quedó sin su variante (%v): el montaje en Odoo la agotaría en cinco intentos", got)
	}
	// El nuevo, que es el que tiene el catálogo, se sigue emparejando.
	got = varianteDeLaLinea(t, st, pedidoCon(t, st, cuentaID, "PEDIDO-REN-2", skuNuevo))
	if got == nil || *got != varID {
		t.Errorf("la línea con el SKU actual quedó sin variante: %v", got)
	}

	// Una cuenta que nunca publicó el producto no tiene por qué conocer el
	// SKU viejo: ahí no se adivina, se deja ver.
	var otraCuenta int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'otra-cuenta-renombrado-test', '\x00'::bytea, false
		FROM channels WHERE code = 'shopify' RETURNING id`).Scan(&otraCuenta); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, otraCuenta)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, otraCuenta)
	})
	if got := varianteDeLaLinea(t, st, pedidoCon(t, st, otraCuenta, "PEDIDO-REN-3", skuViejo)); got != nil {
		t.Errorf("una cuenta que nunca publicó el producto emparejó el SKU viejo con la variante %d", *got)
	}
}

// Los pedidos que ya se atascaron con el código anterior se rescatan en la
// siguiente pasada del planificador, sin tocar nada a mano.
func TestUnPedidoAtascadoPorElRenombradoSeRescataSolo(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	const odooID = 555002
	cuentaID, conexionID, _, varID := fixtureRenombrado(t, st, odooID)
	renombrarEnOdoo(t, st, conexionID, odooID)

	ordenID := pedidoCon(t, st, cuentaID, "PEDIDO-REN-4", skuViejo)
	// Así quedó guardado por el código que solo miraba el SKU actual: sin
	// variante, agotado en cinco intentos y fuera de la cola.
	if _, err := st.pool.Exec(ctx,
		`UPDATE channel_order_lines SET variant_id = NULL WHERE channel_order_id = $1`, ordenID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := st.MarcarOrdenFallida(ctx, ordenID, "el SKU no existe en el catálogo"); err != nil {
			t.Fatal(err)
		}
	}

	lineas, pedidos, err := st.ReemparejarLineasHuerfanas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if lineas < 1 || pedidos < 1 {
		t.Fatalf("no se rescató nada: lineas=%d pedidos=%d", lineas, pedidos)
	}
	if got := varianteDeLaLinea(t, st, ordenID); got == nil || *got != varID {
		t.Errorf("la línea sigue sin su variante: %v", got)
	}
	pend, err := st.OrdenPendientePorID(ctx, ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if pend == nil || pend.Intentos != 0 {
		t.Errorf("el pedido debía volver a la cola con los intentos a cero: %+v", pend)
	}
}

// Lo que queda para las personas: la ficha del marketplace muestra una
// referencia que Odoo ya no tiene, y eso alguien lo tiene que decidir.
func TestElSKURenombradoSeVeEnLaColaDeAtencionYEnLaVigilancia(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	const odooID = 555003
	_, conexionID, _, varID := fixtureRenombrado(t, st, odooID)

	motivo := func() (detalle string, hay bool) {
		t.Helper()
		var n int
		if err := st.pool.QueryRow(ctx, `
			SELECT count(*), COALESCE(max(detail),'') FROM attention_queue
			WHERE variant_id = $1 AND reason = 'sku_renombrado' AND severity = 'warning'`,
			varID).Scan(&n, &detalle); err != nil {
			t.Fatal(err)
		}
		return detalle, n > 0
	}

	// Mientras el SKU coincide con el publicado no hay nada que avisar.
	if err := st.RecalcularAtencion(ctx); err != nil {
		t.Fatal(err)
	}
	if _, hay := motivo(); hay {
		t.Fatal("sin renombrar no debe haber motivo sku_renombrado")
	}
	antes, err := st.PublicadosConSKURenombrado(ctx)
	if err != nil {
		t.Fatal(err)
	}

	renombrarEnOdoo(t, st, conexionID, odooID)
	if err := st.RecalcularAtencion(ctx); err != nil {
		t.Fatal(err)
	}
	detalle, hay := motivo()
	if !hay {
		t.Fatal("el renombrado no entró en la cola de atención")
	}
	if !strings.Contains(detalle, skuViejo) || !strings.Contains(detalle, "falabella") {
		t.Errorf("el detalle tiene que decir qué canal sigue con qué SKU: %q", detalle)
	}
	despues, err := st.PublicadosConSKURenombrado(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if despues != antes+1 {
		t.Errorf("la vigilancia cuenta %d publicaciones con el SKU renombrado; antes había %d", despues, antes)
	}
}
