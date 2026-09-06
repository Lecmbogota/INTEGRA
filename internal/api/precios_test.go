package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mdv/integra/internal/store"
)

// Un override o una regla se guardaban, respondían ok:true y no llegaban nunca
// al canal: lo único que los aplica es RecalcularPreciosCuenta y los
// manejadores no lo llamaban. Estas pruebas atraviesan el manejador de verdad
// contra el esquema real y miran lo que el motor va a publicar después:
// effective_prices, que es lo que CandidatosPublicacion prefiere para el precio
// y para el hash. Se saltan sin INTEGRA_DATABASE_URL.
//
// El recálculo recorre todas las variantes del catálogo, también las que otro
// paquete de pruebas está creando y borrando en ese instante sobre la misma
// base: si una desaparece entre el SELECT y el UPSERT, falla la clave foránea.
// Con base, la suite se corre en serie (go test -p 1).

// cobayaDePrecios es una cuenta y una variante desechables con su precio
// efectivo ya calculado, como un catálogo que lleva tiempo publicado.
type cobayaDePrecios struct {
	cuentaID, varianteID, marcaID int64
	// calculado es el precio efectivo de partida, sin override ni reglas.
	calculado float64
}

func nuevaCobayaDePrecios(t *testing.T, st *store.Store, ctx context.Context) cobayaDePrecios {
	t.Helper()
	pool := st.Pool()
	var c cobayaDePrecios

	// La cuenta lleva marca para no chocar con la cuenta global del canal:
	// RecalcularPreciosCuenta solo exige que esté activa.
	if err := pool.QueryRow(ctx, `
		INSERT INTO brands (code, name) VALUES ('zz-test-precios-api', 'ZZ Test Precios API')
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&c.marcaID); err != nil {
		t.Fatalf("creando la marca de prueba: %v", err)
	}
	// Restos de una ejecución anterior que murió sin limpiar.
	_, _ = pool.Exec(ctx, `DELETE FROM channel_accounts WHERE brand_id = $1`, c.marcaID)
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc)
		SELECT $1, id, 'cuenta de prueba de precios', '\x00'::bytea FROM channels ORDER BY id LIMIT 1
		RETURNING id`, c.marcaID).Scan(&c.cuentaID); err != nil {
		t.Fatalf("creando la cuenta de prueba: %v", err)
	}
	// La URL es propia de esta prueba: (base_url, database) es único y los
	// paquetes de pruebas corren en paralelo contra la misma base.
	var conexionID, productoID int64
	_, _ = pool.Exec(ctx, `DELETE FROM odoo_connections WHERE base_url = 'http://precios-api-test'`)
	if err := pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-precios-api-test', 'http://precios-api-test', 'x', 'x', '\x00'::bytea, false)
		RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatalf("creando la conexión de prueba: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name, brand_id)
		VALUES ($1, 777101, 'Producto con precio pactado', $2) RETURNING id`,
		conexionID, c.marcaID).Scan(&productoID); err != nil {
		t.Fatalf("creando el producto de prueba: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku, price)
		VALUES ($1, 777101, 'PRECIO-API-TEST', 100000) RETURNING id`, productoID).Scan(&c.varianteID); err != nil {
		t.Fatalf("creando la variante de prueba: %v", err)
	}
	t.Cleanup(func() {
		// La cuenta arrastra en cascada overrides, reglas y precios efectivos;
		// la conexión, el producto y la variante.
		_, _ = pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, c.cuentaID)
		_, _ = pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
		_, _ = pool.Exec(ctx, `DELETE FROM brands WHERE id = $1`, c.marcaID)
	})

	if _, err := st.RecalcularPreciosCuenta(ctx, c.cuentaID); err != nil {
		t.Fatalf("calculando el precio de partida: %v", err)
	}
	c.calculado, _ = precioEfectivo(t, st, ctx, c.cuentaID, c.varianteID)
	return c
}

func precioEfectivo(t *testing.T, st *store.Store, ctx context.Context, cuentaID, varianteID int64) (float64, string) {
	t.Helper()
	var precio float64
	var fuente string
	if err := st.Pool().QueryRow(ctx, `
		SELECT regular_price, source FROM effective_prices
		WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID).Scan(&precio, &fuente); err != nil {
		t.Fatalf("leyendo el precio efectivo: %v", err)
	}
	return precio, fuente
}

// atender llama al manejador tal como lo haría el enrutador, con el {id} de la
// ruta ya resuelto.
func atender(t *testing.T, manejador http.HandlerFunc, metodo, ruta, id, cuerpo string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo))
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	manejador(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s %s respondió %d: %s", metodo, ruta, rec.Code, rec.Body.String())
	}
	return rec
}

func TestGuardarUnOverrideRehaceElPrecioEfectivoDeLaCuenta(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	c := nuevaCobayaDePrecios(t, st, ctx)

	atender(t, s.guardarOverridePrecio, http.MethodPut,
		fmt.Sprintf("/api/variantes/%d/override-precio", c.varianteID), strconv.FormatInt(c.varianteID, 10),
		fmt.Sprintf(`{"channel_account_id":%d,"price":87000,"reason":"pactado con el proveedor"}`, c.cuentaID))

	precio, fuente := precioEfectivo(t, st, ctx, c.cuentaID, c.varianteID)
	if precio != 87000 || fuente != "override" {
		t.Fatalf("el precio efectivo sigue en %.0f (%s): el override se guardó con ok:true y el canal seguirá a %.0f",
			precio, fuente, c.calculado)
	}
}

func TestQuitarElOverrideDevuelveElPrecioCalculado(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	c := nuevaCobayaDePrecios(t, st, ctx)

	// El canal ya vende al precio pactado.
	if err := st.GuardarOverridePrecio(ctx, c.varianteID, c.cuentaID, 87000, "pactado", nil); err != nil {
		t.Fatalf("fijando el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, c.cuentaID); err != nil {
		t.Fatalf("aplicando el override: %v", err)
	}

	atender(t, s.eliminarOverridePrecio, http.MethodDelete,
		fmt.Sprintf("/api/variantes/%d/override-precio?channel_account_id=%d", c.varianteID, c.cuentaID),
		strconv.FormatInt(c.varianteID, 10), "")

	precio, fuente := precioEfectivo(t, st, ctx, c.cuentaID, c.varianteID)
	if precio != c.calculado || fuente == "override" {
		t.Fatalf("tras quitar el override el precio efectivo es %.0f (%s); el canal seguiría al precio pactado en vez de volver a %.0f",
			precio, fuente, c.calculado)
	}
}

// La regla del informe: «+X en la marca». El ajuste es fijo y múltiplo de 100 a
// propósito: el redondeo al centenar lo deja intacto, así que el precio
// esperado no depende de la comisión que tenga el canal en esta base.
func TestGuardarUnaReglaRehaceLosPreciosDeLaCuenta(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	c := nuevaCobayaDePrecios(t, st, ctx)

	atender(t, s.guardarReglaPrecio, http.MethodPost,
		fmt.Sprintf("/api/cuentas/%d/reglas-precio", c.cuentaID), strconv.FormatInt(c.cuentaID, 10),
		fmt.Sprintf(`{"brand_id":%d,"adjustment_type":"fixed","adjustment_value":5000,"active":true}`, c.marcaID))

	precio, _ := precioEfectivo(t, st, ctx, c.cuentaID, c.varianteID)
	if precio != c.calculado+5000 {
		t.Fatalf("el precio efectivo es %.0f, se esperaba %.0f: la regla se guardó con ok:true y no llegará al canal",
			precio, c.calculado+5000)
	}
}
