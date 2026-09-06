package store

import (
	"context"
	"testing"
	"time"
)

// La cola de atención tenía siete motivos y ninguno miraba el coste, así que
// un producto que se vendía a pérdida era indistinguible de uno sano. Estas
// pruebas van contra el recálculo completo, que es el único sitio donde se
// conoce a la vez el precio que se va a publicar y el coste.

func TestElRecalculoMarcaLaVarianteQueNoCubreCosteYLaDesmarcaAlArreglarla(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)
	costoDeCobaya(t, st, ctx, varianteID, 50000)

	// Un precio manual tecleado con un dígito de menos: 4.000 en vez de
	// 40.000. Es el override el que hay que probar, porque es el camino que
	// salta el suelo del cálculo a propósito.
	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 4000, "dedazo", nil); err != nil {
		t.Fatalf("guardando el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if !enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("un precio de 4.000 con un coste de 50.000 no levantó ni un aviso")
	}

	// Corregido el dedazo, el aviso tiene que irse solo en la misma pasada:
	// si hubiera que resolverlo a mano, la cola dejaría de decir la verdad.
	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 400000, "corregido", nil); err != nil {
		t.Fatalf("corrigiendo el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("el aviso sigue puesto sobre un precio ya corregido")
	}
}

// Una promoción por debajo de coste es el caso caro: el precio rebajado se
// aplica sobre el regular sin pasar por ningún suelo, y la venta a pérdida
// dura lo que dure la promoción.
func TestUnaPromocionPorDebajoDelCosteTambienLevantaElAviso(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)
	costoDeCobaya(t, st, ctx, varianteID, 50000)

	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 400000, "precio sano", nil); err != nil {
		t.Fatalf("guardando el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("el precio sano no debería avisar de nada")
	}

	inicio, fin := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	if _, err := st.GuardarOferta(ctx, varianteID, cuentaID, 9000, inicio, &fin, nil); err != nil {
		t.Fatalf("creando la promoción: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if !enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("una promoción a 9.000 sobre un coste de 50.000 no levantó ni un aviso")
	}
	if n, err := st.VariantesBajoCosto(ctx); err != nil || n == 0 {
		t.Fatalf("la vigilancia no ve nada que avisar: n=%d err=%v", n, err)
	}
}

// El margen mínimo de la cuenta rige donde antes no había nada: un precio que
// cubre el coste justo deja de bastar en cuanto se exige margen.
func TestElMargenMinimoDeLaCuentaCambiaLoQueSeConsideraBajoCoste(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)
	costoDeCobaya(t, st, ctx, varianteID, 50000)

	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 51000, "justito", nil); err != nil {
		t.Fatalf("guardando el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("51.000 sobre 50.000 cubre el coste con el suelo por defecto")
	}

	if err := st.ActualizarSueloCosto(ctx, cuentaID, SueloCosto{MinMargenPct: 30, Bloquear: true}); err != nil {
		t.Fatalf("subiendo el margen mínimo: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}
	if !enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("con un 30% exigido, 51.000 sobre 50.000 tiene que avisar")
	}
}

// ------------------------------------------------------------- ayudas

func costoDeCobaya(t *testing.T, st *Store, ctx context.Context, varianteID int64, costo float64) {
	t.Helper()
	var previo *float64
	if err := st.pool.QueryRow(ctx,
		`SELECT cost FROM product_variants WHERE id = $1`, varianteID).Scan(&previo); err != nil {
		t.Fatalf("leyendo el coste: %v", err)
	}
	if _, err := st.pool.Exec(ctx,
		`UPDATE product_variants SET cost = $2 WHERE id = $1`, varianteID, costo); err != nil {
		t.Fatalf("fijando el coste: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `UPDATE product_variants SET cost = $2 WHERE id = $1`, varianteID, previo)
	})
}

func enLaCola(t *testing.T, st *Store, ctx context.Context, varianteID, cuentaID int64) bool {
	t.Helper()
	var n int
	if err := st.pool.QueryRow(ctx, `
		SELECT count(*) FROM attention_queue
		WHERE variant_id = $1 AND channel_account_id = $2 AND reason = $3 AND resolved_at IS NULL`,
		varianteID, cuentaID, MotivoBajoCosto).Scan(&n); err != nil {
		t.Fatalf("consultando la cola de atención: %v", err)
	}
	return n > 0
}

// El motor solo mira el candidato, así que el bloqueo tiene que llegar hasta
// ahí: si no, el aviso de la cola sería un cartel y el precio a pérdida
// saldría igual al canal.
func TestElCandidatoDePublicacionLlegaMarcadoCuandoNoCubreCoste(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)
	costoDeCobaya(t, st, ctx, varianteID, 50000)

	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 4000, "dedazo", nil); err != nil {
		t.Fatalf("guardando el override: %v", err)
	}
	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("recalculando: %v", err)
	}

	c, err := st.UnCandidato(ctx, cuentaID, varianteID)
	if err != nil {
		t.Fatalf("buscando el candidato: %v", err)
	}
	if !c.BloqueadoPorCosto {
		t.Fatalf("el candidato sale sin marcar a %.0f con un coste de 50.000", c.PrecioCanal)
	}

	// Con el interruptor de la cuenta en falso el aviso sigue, pero el envío
	// no se detiene: es la salida para un coste mal cargado en Odoo.
	if err := st.ActualizarSueloCosto(ctx, cuentaID, SueloCosto{Bloquear: false}); err != nil {
		t.Fatalf("quitando el bloqueo: %v", err)
	}
	c, err = st.UnCandidato(ctx, cuentaID, varianteID)
	if err != nil {
		t.Fatalf("buscando el candidato: %v", err)
	}
	if c.BloqueadoPorCosto {
		t.Fatal("la cuenta pidió no frenar el envío y se frenó igual")
	}
	if !enLaCola(t, st, ctx, varianteID, cuentaID) {
		t.Fatal("sin bloqueo el aviso tiene que seguir puesto")
	}
}
