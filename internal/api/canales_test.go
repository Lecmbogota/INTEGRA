package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

// Subir la comisión de un canal del 12 % al 16 % decía «guardado» y no movía
// ni un precio publicado: CandidatosPublicacion prefiere el precio ya resuelto
// en effective_prices, calculado con la comisión vieja, y nadie lo rehacía
// hasta que alguien pulsara «recalcular» cuenta por cuenta. Mientras tanto
// cada venta dejaba cuatro puntos de margen en el canal.
//
// Se recorre el camino entero contra el esquema real: una cuenta con precios
// ya resueltos, el PATCH que manda la pantalla de canales y lo que el motor
// de publicación leería en la siguiente planificación.
//
// Como el resto de pruebas de integración, se salta sin INTEGRA_DATABASE_URL.

// canalConCuentaYVariante deja un canal con una cuenta global activa y una
// variante con precio base, y devuelve el canal a su comisión de antes al
// terminar: la fila del canal es compartida, no de la prueba.
func canalConCuentaYVariante(t *testing.T, st *store.Store, ctx context.Context, precioBase float64) (codigo string, cuentaID, varianteID int64) {
	t.Helper()
	pool := st.Pool()

	// La migración 015 solo admite una cuenta global activa por canal, así
	// que se toma un canal que no tenga ninguna.
	var comision, costoFijo float64
	err := pool.QueryRow(ctx, `
		SELECT ch.code, ch.comision_pct, ch.costo_fijo FROM channels ch
		WHERE NOT EXISTS (SELECT 1 FROM channel_accounts a
		                  WHERE a.channel_id = ch.id AND a.brand_id IS NULL AND a.active)
		ORDER BY ch.id LIMIT 1`).Scan(&codigo, &comision, &costoFijo)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("los cuatro canales ya tienen cuenta global activa en esta base")
	}
	if err != nil {
		t.Fatalf("buscando un canal sin cuenta: %v", err)
	}
	t.Cleanup(func() {
		_ = st.ActualizarCanal(context.Background(), codigo, comision, costoFijo)
	})

	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-comision-test', '\x00'::bytea, true FROM channels WHERE code = $1
		RETURNING id`, codigo).Scan(&cuentaID); err != nil {
		t.Fatalf("creando la cuenta de prueba: %v", err)
	}
	var conexionID, prodID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-comision-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatalf("creando la conexión de prueba: %v", err)
	}

	// La cuenta necesita al menos una bodega asignada: desde que se exige la
	// asignación, una cuenta sin bodegas no publica nada y esta prueba dejaría
	// de medir lo suyo para medir eso otro.
	var bodegaID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO odoo_warehouses (odoo_connection_id, odoo_id, code, name)
		VALUES ($1, 778901, 'COMIS', 'Bodega de la prueba de comisión') RETURNING id`,
		conexionID).Scan(&bodegaID); err != nil {
		t.Fatalf("creando la bodega de prueba: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_account_warehouses (channel_account_id, odoo_warehouse_id)
		VALUES ($1, $2)`, cuentaID, bodegaID); err != nil {
		t.Fatalf("asignando la bodega a la cuenta: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name)
		VALUES ($1, 778001, 'Audífonos de la prueba de comisión') RETURNING id`, conexionID).Scan(&prodID); err != nil {
		t.Fatalf("creando el producto de prueba: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku, price)
		VALUES ($1, 778001, 'AUD-COMISION-TEST', $2) RETURNING id`, prodID, precioBase).Scan(&varianteID); err != nil {
		t.Fatalf("creando la variante de prueba: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})
	return codigo, cuentaID, varianteID
}

// precioCanalDe es el precio que el motor mandaría al canal para la variante:
// lo que sale de CandidatosPublicacion, no lo que dice la tabla de canales.
func precioCanalDe(t *testing.T, st *store.Store, ctx context.Context, cuentaID, varianteID int64) float64 {
	t.Helper()
	candidatos, err := st.CandidatosPublicacion(ctx, cuentaID)
	if err != nil {
		t.Fatalf("leyendo los candidatos de publicación: %v", err)
	}
	for _, c := range candidatos {
		if c.VarianteID == varianteID {
			return c.PrecioCanal
		}
	}
	t.Fatalf("la variante %d no salió entre los candidatos de la cuenta %d", varianteID, cuentaID)
	return 0
}

func TestCambiarLaComisionDeUnCanalRehaceLosPreciosQueSePublican(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	_, token := cobayaConSesion(t, s, st, ctx, auth.RolAdmin)
	codigo, cuentaID, varianteID := canalConCuentaYVariante(t, st, ctx, 100000)

	// Con el contrato viejo los precios ya están resueltos: es el estado de
	// cualquier cuenta que lleve tiempo publicando. Base 100.000 al 12 % son
	// 113.636,36, redondeados al centenar: 113.700.
	if err := st.ActualizarCanal(ctx, codigo, 12, 0); err != nil {
		t.Fatalf("fijando la comisión de partida: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("resolviendo los precios de partida: %v", err)
	}
	if got := precioCanalDe(t, st, ctx, cuentaID, varianteID); got != 113700 {
		t.Fatalf("con el 12 %% el precio de canal tenía que ser 113.700 y es %v", got)
	}

	// Cambia el contrato y alguien lo guarda desde la pantalla de canales.
	r := httptest.NewRequest(http.MethodPatch, "/api/canales/"+codigo,
		strings.NewReader(`{"comision_pct":16,"costo_fijo":0}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("guardar el canal dio HTTP %d: %s", w.Code, w.Body.String())
	}

	// Lo que el motor leería en la siguiente planificación tiene que llevar
	// ya el 16 %: 100.000 / 0,84 = 119.047,62, o sea 119.100. Con el precio
	// efectivo viejo seguiría mandando 113.700.
	if got := precioCanalDe(t, st, ctx, cuentaID, varianteID); got != 119100 {
		t.Fatalf("tras subir la comisión al 16 %% el precio de canal es %v; se esperaba 119.100", got)
	}
}
