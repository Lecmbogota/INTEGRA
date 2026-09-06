package store

import (
	"context"
	"testing"
	"time"
)

// Dos agujeros del camino canal → Odoo, montados contra la base de verdad.
//
//  1. El pedido se creaba sin bodega, así que Odoo lo despachaba de la
//     principal aunque la venta fuera de Falabella y su mercancía estuviera
//     consignada en las bodegas FB. channel_account_warehouses llevaba desde
//     el primer esquema sin que la leyera nadie en este camino.
//  2. El stock no bajaba hasta el siguiente sync con Odoo. Entre la venta y
//     ese sync, los otros tres canales seguían ofreciendo unidades vendidas,
//     y como el pedido se crea en Odoo en borrador a propósito, la ventana no
//     se cierra ni cuando el sync corre: puede durar días.

// entorno es el mínimo con el que se puede probar todo esto: una conexión de
// Odoo, tres bodegas, una cuenta de canal con dos de ellas asignadas, una
// variante con existencias repartidas y un pedido de esa variante.
type entorno struct {
	st                     *Store
	cuentaID, cuentaSinBod int64
	varianteID             int64
	principal, fb1, fb2    int64 // ids locales de odoo_warehouses
	odooFB2                int64 // id en Odoo de la bodega FB2
}

func montarEntorno(t *testing.T, etiqueta string) *entorno {
	t.Helper()
	ctx := context.Background()
	st := abrirStore(t)
	e := &entorno{st: st}

	var conexionID, productoID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ($1,'http://x',$1,'x','\x00'::bytea,false) RETURNING id`,
		"conexion-bodega-"+etiqueta).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id IN ($1,$2)`,
			e.cuentaID, e.cuentaSinBod)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id IN ($1,$2)`,
			e.cuentaID, e.cuentaSinBod)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})

	bodega := func(codigo string, odooID int64) int64 {
		var id int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO odoo_warehouses (odoo_connection_id, odoo_id, code, name)
			VALUES ($1,$2,$3,$3) RETURNING id`, conexionID, odooID, codigo).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.principal = bodega("A-PRINCIPAL", 1)
	e.fb1 = bodega("B-FB1", 2)
	e.fb2 = bodega("C-FB2", 3)
	e.odooFB2 = 3

	cuenta := func(nombre string) int64 {
		var id int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
			SELECT NULL, id, $1, '\x00'::bytea, false FROM channels WHERE code = 'falabella'
			RETURNING id`, nombre).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.cuentaID = cuenta("cuenta-bodega-" + etiqueta)
	e.cuentaSinBod = cuenta("cuenta-sin-bodega-" + etiqueta)

	// La cuenta de Falabella solo ve sus dos bodegas FB.
	for _, b := range []int64{e.fb1, e.fb2} {
		if _, err := st.pool.Exec(ctx, `
			INSERT INTO channel_account_warehouses (channel_account_id, odoo_warehouse_id)
			VALUES ($1,$2)`, e.cuentaID, b); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 998001, 'Producto de prueba de bodegas') RETURNING id`,
		conexionID).Scan(&productoID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 998001, $2) RETURNING id`,
		productoID, "SKU-BODEGA-"+etiqueta).Scan(&e.varianteID); err != nil {
		t.Fatal(err)
	}
	e.sembrarStock(t, 10, 4, 6)
	return e
}

// sembrarStock deja las existencias como las dejaría un sync con Odoo.
func (e *entorno) sembrarStock(t *testing.T, principal, fb1, fb2 float64) {
	t.Helper()
	ctx := context.Background()
	for bodega, qty := range map[int64]float64{
		e.principal: principal, e.fb1: fb1, e.fb2: fb2,
	} {
		if _, err := e.st.pool.Exec(ctx, `
			INSERT INTO variant_stock (variant_id, odoo_warehouse_id, qty_on_hand)
			VALUES ($1,$2,$3)
			ON CONFLICT (variant_id, odoo_warehouse_id) DO UPDATE
			SET qty_on_hand = EXCLUDED.qty_on_hand`, e.varianteID, bodega, qty); err != nil {
			t.Fatal(err)
		}
	}
}

func (e *entorno) stock(t *testing.T, bodega int64) float64 {
	t.Helper()
	var q float64
	if err := e.st.pool.QueryRow(context.Background(),
		`SELECT COALESCE(qty_on_hand,0) FROM variant_stock
		 WHERE variant_id = $1 AND odoo_warehouse_id = $2`,
		e.varianteID, bodega).Scan(&q); err != nil {
		t.Fatal(err)
	}
	return q
}

// pedido guarda una venta de `cantidad` unidades en la cuenta indicada.
func (e *entorno) pedido(t *testing.T, cuentaID int64, externo string, cantidad float64) int64 {
	t.Helper()
	var sku string
	if err := e.st.pool.QueryRow(context.Background(),
		`SELECT sku FROM product_variants WHERE id = $1`, e.varianteID).Scan(&sku); err != nil {
		t.Fatal(err)
	}
	id, nuevo, err := e.st.GuardarOrden(context.Background(), DatosOrden{
		CuentaID: cuentaID, ExternalID: externo, Numero: externo,
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 100,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{{
			SKU: sku, Titulo: "Producto de prueba de bodegas",
			Cantidad: cantidad, PrecioUnit: 100, Total: 100 * cantidad,
		}},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido: %v (nuevo=%v)", err, nuevo)
	}
	return id
}

// requiereReservas salta la prueba mientras no exista la tabla del libro de
// reservas, que va en la migración descrita en el informe.
func requiereReservas(t *testing.T, st *Store) {
	t.Helper()
	var existe bool
	if err := st.pool.QueryRow(context.Background(),
		`SELECT to_regclass('public.order_stock_reservations') IS NOT NULL`).Scan(&existe); err != nil {
		t.Fatal(err)
	}
	if !existe {
		t.Skip("falta order_stock_reservations: la migración está descrita en el informe, sin escribir")
	}
}

// La bodega del pedido sale de las asignadas a la cuenta, y entre ellas gana
// la que más unidades del pedido puede cubrir: con 5 unidades vendidas, FB2
// (6 en existencia) las cubre todas y FB1 (4) no.
func TestBodegaDeOrdenEligeEntreLasDeLaCuenta(t *testing.T) {
	e := montarEntorno(t, "elige")
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-1", 5)

	b, err := e.st.BodegaDeOrden(context.Background(), ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if b == nil {
		t.Fatal("la cuenta tiene bodegas asignadas: el pedido no puede salir sin bodega")
	}
	if b.ID != e.fb2 {
		t.Errorf("se eligió la bodega %d (%s); debía ser FB2, la única que cubre las 5 unidades",
			b.ID, b.Codigo)
	}
	if b.OdooID != e.odooFB2 {
		t.Errorf("warehouse_id para Odoo = %d, se esperaba %d", b.OdooID, e.odooFB2)
	}
}

// Una cuenta sin bodegas asignadas se queda como está hoy: decide Odoo.
func TestBodegaDeOrdenSinAsignacionDejaDecidirAOdoo(t *testing.T) {
	e := montarEntorno(t, "sinasignar")
	ordenID := e.pedido(t, e.cuentaSinBod, "PEDIDO-BODEGA-2", 1)

	b, err := e.st.BodegaDeOrden(context.Background(), ordenID)
	if err != nil {
		t.Fatalf("una cuenta sin bodegas no debe fallar, solo no elegir: %v", err)
	}
	if b != nil {
		t.Errorf("no debía elegir bodega y eligió %s", b.Codigo)
	}
}

// El descuento inmediato: vender 5 unidades en Falabella baja el stock ya, y
// lo baja de las bodegas FB, no de la principal.
func TestDescontarStockPublicadoBajaSoloLasBodegasDeLaCuenta(t *testing.T) {
	e := montarEntorno(t, "descuento")
	requiereReservas(t, e.st)
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-3", 5)

	n, err := e.st.DescontarStockPublicado(ctx, ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Errorf("se descontaron %v unidades, se vendieron 5", n)
	}
	if q := e.stock(t, e.fb2); q != 1 {
		t.Errorf("FB2 tenía 6 y se vendieron 5: quedan %v", q)
	}
	if q := e.stock(t, e.principal); q != 10 {
		t.Errorf("la bodega principal no alimenta esta cuenta y quedó en %v", q)
	}

	// Idempotencia: el sondeo relee ventanas solapadas y el trabajo se
	// reintenta. Descontar dos veces la misma venta dejaría de publicarse
	// stock que sí existe.
	if n, err := e.st.DescontarStockPublicado(ctx, ordenID); err != nil || n != 0 {
		t.Errorf("el segundo descuento debía no hacer nada: n=%v err=%v", n, err)
	}
	if q := e.stock(t, e.fb2); q != 1 {
		t.Errorf("el descuento se aplicó dos veces: FB2 quedó en %v", q)
	}
}

// Lo que se descontó vuelve a aplicarse después de cada sync. Sin esto el
// sync, que reemplaza variant_stock entero, resucitaría cada pocas horas las
// unidades ya vendidas: el pedido se queda en borrador en Odoo a propósito,
// así que Odoo tampoco las ha descontado.
func TestLasReservasSobrevivenAlSync(t *testing.T) {
	e := montarEntorno(t, "sync")
	requiereReservas(t, e.st)
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-4", 5)

	if _, err := e.st.DescontarStockPublicado(ctx, ordenID); err != nil {
		t.Fatal(err)
	}
	e.sembrarStock(t, 10, 4, 6) // el sync vuelve a poner la foto de Odoo

	if _, err := e.st.ReaplicarReservasDeStock(ctx); err != nil {
		t.Fatal(err)
	}
	if q := e.stock(t, e.fb2); q != 1 {
		t.Errorf("tras el sync la reserva debía volver a descontarse: FB2 = %v", q)
	}
}

// Un pedido cancelado por el canal después de ingerido seguía en Integra como
// venta viva: se montaba igual en Odoo y, si ya estaba montado, nadie se
// enteraba. Marcarlo lo saca de la cola de montaje y devuelve lo que hace
// falta para avisar (canal, número e id del sale.order ya creado).
//
// El sale.order NO se cancela: esa decisión es de una persona.
func TestCancelarSacaElPedidoDeLaColaDeMontaje(t *testing.T) {
	e := montarEntorno(t, "cancela")
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-5", 5)

	// El pedido ya se había montado en Odoo antes de que el canal cancelara:
	// es el caso que nadie veía.
	if err := e.st.MarcarOrdenCreada(ctx, ordenID, 4321, 99); err != nil {
		t.Fatal(err)
	}

	c, err := e.st.MarcarOrdenCancelada(ctx, ordenID, "canceled")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Cambio {
		t.Fatal("la primera cancelación tiene que contar como cambio")
	}
	if c.Canal != "falabella" || c.Numero != "PEDIDO-BODEGA-5" {
		t.Errorf("la cancelación no identifica el pedido: canal=%q numero=%q", c.Canal, c.Numero)
	}
	if c.OdooPedidoID == nil || *c.OdooPedidoID != 4321 {
		t.Errorf("hay que devolver el sale.order ya creado para poder avisar: %v", c.OdooPedidoID)
	}

	var estado string
	var odooID *int64
	if err := e.st.pool.QueryRow(ctx,
		`SELECT status::text, odoo_sale_order_id FROM channel_orders WHERE id = $1`,
		ordenID).Scan(&estado, &odooID); err != nil {
		t.Fatal(err)
	}
	if estado != "ignored" {
		t.Errorf("el pedido cancelado quedó en %q", estado)
	}
	if odooID == nil {
		t.Error("no se puede perder el id del sale.order: es lo que una persona tiene que revisar")
	}

	// La segunda pasada del sondeo lo vuelve a ver cancelado: ni vuelve a
	// avisar ni volvería a devolver el stock.
	c2, err := e.st.MarcarOrdenCancelada(ctx, ordenID, "canceled")
	if err != nil {
		t.Fatal(err)
	}
	if c2.Cambio {
		t.Error("la segunda cancelación no es un cambio: duplicaría aviso y devolución de stock")
	}
}

// Un pedido cancelado no puede seguir esperando su sale.order.
func TestUnPedidoCanceladoNoSigueEsperandoAOdoo(t *testing.T) {
	e := montarEntorno(t, "colamontaje")
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-7", 2)

	if pend, err := e.st.OrdenPendientePorID(ctx, ordenID); err != nil || pend == nil {
		t.Fatalf("el pedido recién ingerido debe estar pendiente: %v (err=%v)", pend, err)
	}
	if _, err := e.st.MarcarOrdenCancelada(ctx, ordenID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	pend, err := e.st.OrdenPendientePorID(ctx, ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if pend != nil {
		t.Error("un pedido cancelado no puede seguir en la cola de montaje a Odoo")
	}
}

// La cancelación devuelve exactamente lo que se apartó, una sola vez.
func TestCancelarDevuelveElStockApartado(t *testing.T) {
	e := montarEntorno(t, "devuelve")
	requiereReservas(t, e.st)
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-8", 5)
	if _, err := e.st.DescontarStockPublicado(ctx, ordenID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.MarcarOrdenCancelada(ctx, ordenID, "canceled"); err != nil {
		t.Fatal(err)
	}

	devueltas, err := e.st.DevolverStockReservado(ctx, ordenID, "cancelado en el canal")
	if err != nil {
		t.Fatal(err)
	}
	if devueltas != 5 {
		t.Errorf("se devolvieron %v unidades de las 5 apartadas", devueltas)
	}
	if q := e.stock(t, e.fb2); q != 6 {
		t.Errorf("FB2 debía volver a 6 y quedó en %v", q)
	}
	if r, err := e.st.ReservasAbiertasDeOrden(ctx, ordenID); err != nil || len(r) != 0 {
		t.Errorf("no debía quedar reserva abierta: %v (err=%v)", r, err)
	}

	// Devolver dos veces publicaría stock que no existe.
	if devueltas, err := e.st.DevolverStockReservado(ctx, ordenID, "cancelado en el canal"); err != nil || devueltas != 0 {
		t.Errorf("no quedaba nada que devolver: %v (err=%v)", devueltas, err)
	}
	if q := e.stock(t, e.fb2); q != 6 {
		t.Errorf("el stock se devolvió dos veces: FB2 = %v", q)
	}
}

// Un pedido que llega ya cancelado no aparta stock: no hay venta viva.
func TestUnPedidoYaCanceladoNoApartaStock(t *testing.T) {
	e := montarEntorno(t, "nacecancelado")
	requiereReservas(t, e.st)
	ctx := context.Background()
	ordenID := e.pedido(t, e.cuentaID, "PEDIDO-BODEGA-6", 5)

	if _, err := e.st.MarcarOrdenCancelada(ctx, ordenID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	n, err := e.st.DescontarStockPublicado(ctx, ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("se apartaron %v unidades por un pedido cancelado", n)
	}
	if q := e.stock(t, e.fb2); q != 6 {
		t.Errorf("FB2 no debía moverse y quedó en %v", q)
	}
}
