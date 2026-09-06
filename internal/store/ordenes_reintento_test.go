package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// El pedido que falla cinco veces y se queda fuera de Odoo para siempre.
//
// Cada fallo del montaje suma un intento y las dos consultas de pendientes
// cortan en cinco: un pedido cobrado que fallara por una caída pasajera de
// Odoo o por una tarifa que faltaba salía de la cola y no volvía. El único
// reinicio del contador vivía en ReemparejarLineasHuerfanas, que solo corre
// cuando un horario dispara. Estas pruebas montan ese escenario contra la
// base real y comprueban que el reintento a mano lo devuelve a la cola.

// escenarioReintento es lo mínimo: una cuenta de MercadoLibre y una conexión
// de Odoo de la que colgar el catálogo que haga falta.
type escenarioReintento struct {
	st         *Store
	cuentaID   int64
	conexionID int64
	// plantilla numera los productos que se creen, para no chocar con la
	// clave (conexión, plantilla de Odoo).
	plantilla int64
}

func montarEscenarioReintento(t *testing.T, etiqueta string) *escenarioReintento {
	t.Helper()
	ctx := context.Background()
	e := &escenarioReintento{st: abrirStore(t), plantilla: 997000}

	if err := e.st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, $1, '\x00'::bytea, false FROM channels WHERE code = 'mercadolibre'
		RETURNING id`, "cuenta-reintento-"+etiqueta).Scan(&e.cuentaID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ($1,'http://x',$1,'x','\x00'::bytea,false) RETURNING id`,
		"conexion-reintento-"+etiqueta).Scan(&e.conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = e.st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id = $1`, e.cuentaID)
		_, _ = e.st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, e.cuentaID)
		_, _ = e.st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, e.conexionID)
		_, _ = e.st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, e.conexionID)
	})
	return e
}

// variante da de alta en el catálogo el SKU, como haría un `integra sync`.
func (e *escenarioReintento) variante(t *testing.T, sku string) int64 {
	t.Helper()
	ctx := context.Background()
	e.plantilla++
	var prodID, varID int64
	if err := e.st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, $2, 'Producto de prueba de reintento') RETURNING id`,
		e.conexionID, e.plantilla).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, $2, $3) RETURNING id`, prodID, e.plantilla, sku).Scan(&varID); err != nil {
		t.Fatal(err)
	}
	return varID
}

// pedido guarda una venta de una unidad del SKU indicado.
func (e *escenarioReintento) pedido(t *testing.T, externo, sku string) int64 {
	t.Helper()
	id, nuevo, err := e.st.GuardarOrden(context.Background(), DatosOrden{
		CuentaID: e.cuentaID, ExternalID: externo, Numero: externo,
		EstadoCanal: "paid", FechaPedido: time.Now(), Moneda: "COP", Total: 100,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{{SKU: sku, Titulo: "Artículo", Cantidad: 1,
			PrecioUnit: 100, Total: 100}},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido %s: %v (nuevo=%v)", externo, err, nuevo)
	}
	return id
}

// agotar hace fallar el montaje cinco veces y comprueba la consecuencia que
// se quiere cerrar: el pedido ya no está pendiente para nadie.
func (e *escenarioReintento) agotar(t *testing.T, ordenID int64, causa string) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := e.st.MarcarOrdenFallida(ctx, ordenID, causa); err != nil {
			t.Fatal(err)
		}
	}
	if pend, err := e.st.OrdenPendientePorID(ctx, ordenID); err != nil {
		t.Fatal(err)
	} else if pend != nil {
		t.Fatal("tras cinco fallos el pedido no debería seguir pendiente")
	}
}

func (e *escenarioReintento) estado(t *testing.T, ordenID int64) (estado string, intentos int, causa string) {
	t.Helper()
	if err := e.st.pool.QueryRow(context.Background(),
		`SELECT status::text, sync_attempts, COALESCE(sync_error,'')
		 FROM channel_orders WHERE id = $1`, ordenID).Scan(&estado, &intentos, &causa); err != nil {
		t.Fatal(err)
	}
	return
}

// Odoo estuvo caído el cuarto de hora que duran los cinco reintentos con
// backoff. El pedido salió de la cola; el reintento a mano lo devuelve.
func TestUnPedidoQueAgotoLosIntentosVuelveALaColaAlReintentarlo(t *testing.T) {
	ctx := context.Background()
	e := montarEscenarioReintento(t, "agotado")
	e.variante(t, "SKU-REINTENTO-OK")
	ordenID := e.pedido(t, "PEDIDO-REINTENTO-1", "SKU-REINTENTO-OK")
	e.agotar(t, ordenID, "Odoo no responde")

	r, err := e.st.ReintentarOrden(ctx, ordenID)
	if err != nil {
		t.Fatalf("reintentando: %v", err)
	}
	if len(r.SinMapear) != 0 {
		t.Fatalf("el SKU existe y aun así se reportó sin mapear: %v", r.SinMapear)
	}
	if r.CuentaID != e.cuentaID || r.Canal != "mercadolibre" || r.Numero != "PEDIDO-REINTENTO-1" {
		t.Errorf("el reintento tiene que decir de qué pedido y cuenta se trata: %+v", r)
	}
	if r.IntentosPrevios != 5 || r.ErrorPrevio != "Odoo no responde" {
		t.Errorf("lo que se deshizo no se reportó bien: intentos=%d causa=%q",
			r.IntentosPrevios, r.ErrorPrevio)
	}

	pend, err := e.st.OrdenPendientePorID(ctx, ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if pend == nil {
		t.Fatal("el pedido tenía que volver a estar pendiente de montarse en Odoo")
	}
	if pend.Intentos != 0 || pend.Estado != "received" || pend.Error != "" {
		t.Errorf("el pedido no volvió limpio a la cola: intentos=%d estado=%s error=%q",
			pend.Intentos, pend.Estado, pend.Error)
	}
}

// El SKU no existía cuando llegó el pedido y el operador acaba de sincronizar
// el catálogo. Sin emparejar la línea aquí mismo, el reintento fallaría con el
// mismo «el SKU no existe» hasta que un horario disparara el rescate.
func TestElReintentoEmparejaElSKUQueYaLlegoAlCatalogo(t *testing.T) {
	ctx := context.Background()
	e := montarEscenarioReintento(t, "sku-tardio")
	ordenID := e.pedido(t, "PEDIDO-REINTENTO-2", "SKU-REINTENTO-TARDIO")
	e.agotar(t, ordenID, "el SKU no existe en el catálogo")

	varID := e.variante(t, "sku-reintento-tardio") // en otra caja: el emparejamiento no distingue

	r, err := e.st.ReintentarOrden(ctx, ordenID)
	if err != nil {
		t.Fatalf("reintentando: %v", err)
	}
	if len(r.SinMapear) != 0 {
		t.Fatalf("el SKU ya está en el catálogo y se reportó sin mapear: %v", r.SinMapear)
	}

	var variante *int64
	if err := e.st.pool.QueryRow(ctx,
		`SELECT variant_id FROM channel_order_lines WHERE channel_order_id = $1`,
		ordenID).Scan(&variante); err != nil {
		t.Fatal(err)
	}
	if variante == nil || *variante != varID {
		t.Errorf("la línea tenía que quedar emparejada con la variante %d: %v", varID, variante)
	}
	if pend, err := e.st.OrdenPendientePorID(ctx, ordenID); err != nil || pend == nil {
		t.Fatalf("el pedido tenía que volver a la cola (err=%v, pendiente=%v)", err, pend != nil)
	}
}

// Con un SKU que sigue sin existir, reintentar solo gastaría intentos: no se
// reactiva y se dice qué falta.
func TestElReintentoNoReactivaUnPedidoQueSigueSinSKU(t *testing.T) {
	ctx := context.Background()
	e := montarEscenarioReintento(t, "sin-sku")
	ordenID := e.pedido(t, "PEDIDO-REINTENTO-3", "SKU-QUE-NUNCA-EXISTIRA")
	e.agotar(t, ordenID, "el SKU no existe en el catálogo")

	r, err := e.st.ReintentarOrden(ctx, ordenID)
	if err != nil {
		t.Fatalf("no es un fallo, es una respuesta: %v", err)
	}
	if len(r.SinMapear) != 1 || r.SinMapear[0] != "SKU-QUE-NUNCA-EXISTIRA" {
		t.Fatalf("tenía que nombrar el SKU que falta: %v", r.SinMapear)
	}
	if estado, intentos, _ := e.estado(t, ordenID); estado != "failed" || intentos != 5 {
		t.Errorf("no debía reactivarse: estado=%s intentos=%d", estado, intentos)
	}
}

// Lo que ya está en Odoo y lo que el canal canceló no vuelve a la cola: sería
// un segundo sale.order, o uno de una venta que no existe.
func TestNoSeReintentaLoQueYaEstaEnOdooNiLoCancelado(t *testing.T) {
	ctx := context.Background()
	e := montarEscenarioReintento(t, "no-procede")
	e.variante(t, "SKU-REINTENTO-NO-PROCEDE")

	enOdoo := e.pedido(t, "PEDIDO-REINTENTO-4", "SKU-REINTENTO-NO-PROCEDE")
	if err := e.st.MarcarOrdenCreada(ctx, enOdoo, 777, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ReintentarOrden(ctx, enOdoo); !errors.Is(err, ErrOrdenYaEnOdoo) {
		t.Errorf("un pedido ya creado en Odoo tenía que rechazarse; err=%v", err)
	}
	if estado, _, _ := e.estado(t, enOdoo); estado != "created_in_odoo" {
		t.Errorf("el pedido en Odoo cambió de estado: %s", estado)
	}

	cancelado := e.pedido(t, "PEDIDO-REINTENTO-5", "SKU-REINTENTO-NO-PROCEDE")
	e.agotar(t, cancelado, "Odoo no responde")
	if _, err := e.st.MarcarOrdenCancelada(ctx, cancelado, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ReintentarOrden(ctx, cancelado); !errors.Is(err, ErrOrdenCancelada) {
		t.Errorf("un pedido cancelado por el canal tenía que rechazarse; err=%v", err)
	}
	if estado, _, _ := e.estado(t, cancelado); estado != "ignored" {
		t.Errorf("el pedido cancelado cambió de estado: %s", estado)
	}

	if _, err := e.st.ReintentarOrden(ctx, -1); !errors.Is(err, ErrOrdenNoExiste) {
		t.Errorf("un id que no existe tenía que decirlo; err=%v", err)
	}
}
