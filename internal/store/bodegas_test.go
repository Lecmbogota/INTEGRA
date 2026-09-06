package store

import (
	"context"
	"errors"
	"testing"
)

// La asignación de bodegas por cuenta y su consecuencia sobre lo que se
// publica, contra la base de verdad.
//
// channel_account_warehouses existía desde el primer esquema sin que nada la
// escribiera, y «sin filas = todas» hacía que cada canal publicara también
// Muestras, Garantías y el stock consignado en las bodegas de Falabella: se
// vendía lo que no se podía despachar.

// entornoBodegas es lo mínimo para ver el stock que se publicaría: una
// conexión de Odoo con tres bodegas (principal y dos FB), dos cuentas activas
// —una con las FB asignadas y otra sin ninguna— y una variante con
// existencias repartidas 10 / 4 / 6.
//
// Las cuentas llevan marca y no son globales: CandidatosPublicacion exige que
// estén activas, y el índice parcial de la migración 015 solo admite una
// cuenta global activa por canal, que en una base de desarrollo puede ser la
// de verdad. Con marca no chocan con ella, y como ListarCuentas solo ve las
// globales, un worker corriendo contra la misma base tampoco las planifica.
type entornoBodegas struct {
	st                     *Store
	conBodegas, sinBodegas int64
	varianteID             int64
	principal, fb1, fb2    int64
}

func montarEntornoBodegas(t *testing.T, etiqueta string) *entornoBodegas {
	t.Helper()
	ctx := context.Background()
	st := abrirStore(t)
	e := &entornoBodegas{st: st}

	var conexionID, marcaID, productoID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ($1,'http://x',$1,'x','\x00'::bytea,true) RETURNING id`,
		"conexion-asigna-"+etiqueta).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO brands (code, name) VALUES ($1,$1) RETURNING id`,
		"marca-asigna-"+etiqueta).Scan(&marcaID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE brand_id = $1`, marcaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM brands WHERE id = $1`, marcaID)
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

	// Dos canales distintos: channel_accounts es única por (marca, canal).
	cuenta := func(canal, nombre string) int64 {
		var id int64
		if err := st.pool.QueryRow(ctx, `
			INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
			SELECT $1, id, $2, '\x00'::bytea, true FROM channels WHERE code = $3
			RETURNING id`, marcaID, nombre, canal).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	e.conBodegas = cuenta("woocommerce", "cuenta-con-bodegas-"+etiqueta)
	e.sinBodegas = cuenta("shopify", "cuenta-sin-bodegas-"+etiqueta)

	for _, b := range []int64{e.fb1, e.fb2} {
		if _, err := st.pool.Exec(ctx, `
			INSERT INTO channel_account_warehouses (channel_account_id, odoo_warehouse_id)
			VALUES ($1,$2)`, e.conBodegas, b); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 997001, 'Producto de prueba de asignación') RETURNING id`,
		conexionID).Scan(&productoID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku)
		VALUES ($1, 997001, $2) RETURNING id`,
		productoID, "SKU-ASIGNA-"+etiqueta).Scan(&e.varianteID); err != nil {
		t.Fatal(err)
	}
	for bodega, qty := range map[int64]float64{e.principal: 10, e.fb1: 4, e.fb2: 6} {
		if _, err := st.pool.Exec(ctx, `
			INSERT INTO variant_stock (variant_id, odoo_warehouse_id, qty_on_hand)
			VALUES ($1,$2,$3)`, e.varianteID, bodega, qty); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// stockPublicable es lo que CandidatosPublicacion diría que hay para vender.
func (e *entornoBodegas) stockPublicable(t *testing.T, cuentaID int64) int {
	t.Helper()
	cands, err := e.st.CandidatosPublicacion(context.Background(), cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cands {
		if c.VarianteID == e.varianteID {
			return c.Stock
		}
	}
	t.Fatalf("la variante %d no salió entre los candidatos de la cuenta %d", e.varianteID, cuentaID)
	return 0
}

// asignadas devuelve los ids de las bodegas que alimentan la cuenta, en el
// orden en que la pantalla las lista (por código).
func (e *entornoBodegas) asignadas(t *testing.T, cuentaID int64) []int64 {
	t.Helper()
	bs, err := e.st.BodegasDeCuenta(context.Background(), cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, b := range bs {
		if b.Asignada {
			ids = append(ids, b.ID)
		}
	}
	return ids
}

// Una cuenta a la que nadie ha asignado bodegas no publica: antes sumaba las
// tres (20 unidades, principal y consignación incluidas) y las ofrecía.
func TestUnaCuentaSinBodegasAsignadasNoPublica(t *testing.T) {
	e := montarEntornoBodegas(t, "niega")
	ctx := context.Background()

	_, err := e.st.CandidatosPublicacion(ctx, e.sinBodegas)
	if !errors.Is(err, ErrCuentaSinBodegas) {
		t.Fatalf("sin bodegas asignadas hay que negarse a publicar, no sumar todas: err=%v", err)
	}
	// El manejador de cada trabajo pasa por UnCandidato: la negativa tiene
	// que llegarle igual, o un trabajo encolado antes de la asignación
	// publicaría el stock de todas.
	if _, err := e.st.UnCandidato(ctx, e.sinBodegas, e.varianteID); !errors.Is(err, ErrCuentaSinBodegas) {
		t.Errorf("UnCandidato debía negarse también: err=%v", err)
	}
}

// Con las bodegas FB asignadas el canal ve 4 + 6, no las 10 de la principal.
func TestElStockPublicableSoloSumaLasBodegasAsignadas(t *testing.T) {
	e := montarEntornoBodegas(t, "suma")
	if got := e.stockPublicable(t, e.conBodegas); got != 10 {
		t.Errorf("stock publicable = %d; las bodegas asignadas suman 10 y la principal no alimenta esta cuenta", got)
	}
}

// Asignar reemplaza la asignación entera —lo que llega son las casillas
// marcadas— y desde ese momento la cuenta publica con esas bodegas.
func TestAsignarBodegasReemplazaLaAsignacionEntera(t *testing.T) {
	e := montarEntornoBodegas(t, "reemplaza")
	ctx := context.Background()

	if err := e.st.AsignarBodegas(ctx, e.sinBodegas, []int64{e.principal}); err != nil {
		t.Fatal(err)
	}
	if got := e.asignadas(t, e.sinBodegas); len(got) != 1 || got[0] != e.principal {
		t.Errorf("asignadas = %v; debía ser solo la principal %d", got, e.principal)
	}
	if got := e.stockPublicable(t, e.sinBodegas); got != 10 {
		t.Errorf("con la principal asignada el stock publicable es 10, salió %d", got)
	}

	// Cambiar de opinión: solo las FB. La repetida no es un error de quien
	// asigna, es una casilla marcada dos veces.
	if err := e.st.AsignarBodegas(ctx, e.sinBodegas, []int64{e.fb1, e.fb2, e.fb2}); err != nil {
		t.Fatal(err)
	}
	if got := e.asignadas(t, e.sinBodegas); len(got) != 2 || got[0] != e.fb1 || got[1] != e.fb2 {
		t.Errorf("asignadas = %v; debía ser exactamente FB1 %d y FB2 %d", got, e.fb1, e.fb2)
	}
	if got := e.stockPublicable(t, e.sinBodegas); got != 10 {
		t.Errorf("con las FB asignadas el stock publicable es 4 + 6, salió %d", got)
	}
}

// Una asignación que no vale —vacía, o con una bodega que no existe— se
// rechaza con su motivo y deja intacta la que había: parar un canal
// desmarcando todas las casillas no puede ser un accidente.
func TestUnaAsignacionInvalidaNoTocaLaQueHabia(t *testing.T) {
	e := montarEntornoBodegas(t, "rechaza")
	ctx := context.Background()

	casos := []struct {
		nombre string
		ids    []int64
	}{
		{"vacía", nil},
		{"bodega inexistente", []int64{e.fb1, 999999999}},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			err := e.st.AsignarBodegas(ctx, e.conBodegas, caso.ids)
			var rechazo *RechazoBodegas
			if !errors.As(err, &rechazo) {
				t.Fatalf("debía rechazarse con motivo, no %v", err)
			}
			if got := e.asignadas(t, e.conBodegas); len(got) != 2 || got[0] != e.fb1 || got[1] != e.fb2 {
				t.Errorf("la asignación anterior se perdió: %v", got)
			}
		})
	}
}
