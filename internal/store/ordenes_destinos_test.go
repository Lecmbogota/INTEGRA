package store

import (
	"context"
	"testing"
	"time"
)

// Una venta bajaba el stock en la base al instante, pero el envío a los demás
// canales solo lo encolaba el planificador, una vez al día. Esta consulta es
// la mitad de persistencia del arreglo: dice a qué cuentas hay que mandar el
// stock de las variantes de un pedido y —tan importante— a cuáles no.

// entornoDestinos: una marca con cuenta en cada canal, una variante y su
// publicación en cada cuenta, en distintos estados. Las cuentas cuelgan de una
// marca en vez de ser globales para no chocar con la cuenta activa por canal
// que pueda haber en la base.
type entornoDestinos struct {
	st         *Store
	varianteID int64
	// vende hizo la venta; publicada es la única que debe recibir el stock.
	// sinIdentificador tiene la fila pero el canal nunca devolvió un id, y
	// apagada está desactivada.
	vende, publicada, sinIdentificador, apagada int64
}

func montarEntornoDestinos(t *testing.T) *entornoDestinos {
	t.Helper()
	ctx := context.Background()
	st := abrirStore(t)
	e := &entornoDestinos{st: st}

	var marcaID, conexionID, productoID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO brands (code, name) VALUES ('marca-destinos-test', 'Marca de prueba')
		RETURNING id`).Scan(&marcaID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-destinos-test', 'http://destinos.invalid', 'destinos', 'x', '\x00'::bytea, false)
		RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_orders WHERE channel_account_id IN
			(SELECT id FROM channel_accounts WHERE brand_id = $1)`, marcaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE brand_id = $1`, marcaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM brands WHERE id = $1`, marcaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})

	cuenta := func(canal string, activa bool) int64 {
		var id int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
			SELECT $1, id, $2, '\x00'::bytea, $3 FROM channels WHERE code = $2
			RETURNING id`, marcaID, canal, activa).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.vende = cuenta("woocommerce", true)
	e.publicada = cuenta("mercadolibre", true)
	e.sinIdentificador = cuenta("shopify", true)
	e.apagada = cuenta("falabella", false)

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 997001, 'Producto de prueba de destinos') RETURNING id`,
		conexionID).Scan(&productoID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 997001, 'SKU-DESTINOS-TEST') RETURNING id`,
		productoID).Scan(&e.varianteID); err != nil {
		t.Fatal(err)
	}

	// La publicación de la variante en cada cuenta, con o sin el
	// identificador que devuelve el canal.
	publicarEn := func(cuentaID int64, externalID any) {
		var listingID int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO product_channel_listings (product_id, channel_account_id, external_id, status)
			VALUES ($1, $2, $3, 'published') RETURNING id`,
			productoID, cuentaID, externalID).Scan(&listingID); err != nil {
			t.Fatal(err)
		}
		if _, err := st.pool.Exec(ctx, `
			INSERT INTO variant_channel_listings (listing_id, variant_id, channel_account_id, status)
			VALUES ($1, $2, $3, 'published')`, listingID, e.varianteID, cuentaID); err != nil {
			t.Fatal(err)
		}
	}
	publicarEn(e.vende, "WC-1")
	publicarEn(e.publicada, "MCO-1")
	publicarEn(e.sinIdentificador, nil)
	publicarEn(e.apagada, "FB-1")
	return e
}

// pedido guarda una venta de la variante en la cuenta indicada.
func (e *entornoDestinos) pedido(t *testing.T, cuentaID int64, externo, sku string) int64 {
	t.Helper()
	id, nuevo, err := e.st.GuardarOrden(context.Background(), DatosOrden{
		CuentaID: cuentaID, ExternalID: externo, Numero: externo,
		EstadoCanal: "processing", FechaPedido: time.Now(), Moneda: "COP", Total: 100,
		Comprador: "Comprador de prueba",
		Lineas: []DatosLinea{{
			SKU: sku, Titulo: "Producto de prueba de destinos",
			Cantidad: 1, PrecioUnit: 100, Total: 100,
		}},
	})
	if err != nil || !nuevo {
		t.Fatalf("guardando el pedido: %v (nuevo=%v)", err, nuevo)
	}
	return id
}

// Solo recibe el stock quien lo puede publicar: la otra cuenta activa con
// identificador en el canal. Ni la que vendió —su canal ya descontó la unidad,
// y reescribirle el stock en ese instante pisaría una segunda venta todavía
// sin ingerir—, ni la que nunca recibió un id del canal, ni la apagada.
func TestDestinosDeStockSonLasOtrasCuentasConLaVariantePublicada(t *testing.T) {
	e := montarEntornoDestinos(t)
	ordenID := e.pedido(t, e.vende, "PEDIDO-DESTINOS-1", "SKU-DESTINOS-TEST")

	destinos, err := e.st.DestinosDeStockDeOrden(context.Background(), ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinos) != 1 {
		t.Fatalf("se esperaba un solo destino (la cuenta %d) y salieron %+v", e.publicada, destinos)
	}
	if d := destinos[0]; d.CuentaID != e.publicada || d.VarianteID != e.varianteID {
		t.Errorf("destino %+v; se esperaba la cuenta %d con la variante %d", d, e.publicada, e.varianteID)
	}
}

// Una línea que no se pudo emparejar con el catálogo no mueve stock, así que
// tampoco hay a quién avisar.
func TestUnPedidoSinVarianteReconocidaNoTieneDestinos(t *testing.T) {
	e := montarEntornoDestinos(t)
	ordenID := e.pedido(t, e.vende, "PEDIDO-DESTINOS-2", "SKU-QUE-NO-EXISTE")

	destinos, err := e.st.DestinosDeStockDeOrden(context.Background(), ordenID)
	if err != nil {
		t.Fatal(err)
	}
	if len(destinos) != 0 {
		t.Errorf("sin variante no hay stock que mover, y salieron destinos: %+v", destinos)
	}
}
