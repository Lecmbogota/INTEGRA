package store

import (
	"context"
	"os"
	"testing"
)

// Estas pruebas ejecutan cada consulta contra el esquema real y solo
// comprueban que no fallen. Suena poco, pero es exactamente lo que atrapa la
// clase de error que ningún test unitario ve: un nombre de columna mal
// escrito, un JOIN a una tabla renombrada o un valor que no existe en un enum.
//
// Se escribieron después de que el planificador reventara en su primera
// ejecución con `invalid input value for enum listing_status: "active"` —
// el enum usa 'published'. Compilaba, pasaba todos los tests y fallaba al
// primer contacto con PostgreSQL.
func abrir(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	ctx := context.Background()
	st, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("abriendo la base: %v", err)
	}
	t.Cleanup(st.Close)
	return st, ctx
}

func TestLasConsultasDelPanelEjecutan(t *testing.T) {
	st, ctx := abrir(t)

	casos := []struct {
		nombre string
		correr func() error
	}{
		{"Resumen", func() error { _, err := st.Resumen(ctx); return err }},
		{"Marcas", func() error { _, err := st.Marcas(ctx); return err }},
		{"Atencion", func() error { _, err := st.Atencion(ctx); return err }},
		{"StockPorAlmacen", func() error { _, err := st.StockPorAlmacen(ctx); return err }},
		{"PrioridadPublicacion", func() error { _, err := st.PrioridadPublicacion(ctx, 5); return err }},
		{"Canales", func() error { _, err := st.Canales(ctx); return err }},
		{"ListarCuentas", func() error { _, err := st.ListarCuentas(ctx); return err }},
		{"ListarProductos", func() error { _, _, err := st.ListarProductos(ctx, FiltroProductos{Limite: 5}); return err }},
		{"ResumenImagenes", func() error { _, err := st.ResumenImagenes(ctx); return err }},
		{"ResumenContenido", func() error { _, err := st.ResumenContenido(ctx); return err }},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if err := c.correr(); err != nil {
				t.Fatalf("%s falló contra el esquema real: %v", c.nombre, err)
			}
		})
	}
}

// Las que usa el planificador en cada tick: si una falla, Integra deja de
// vigilarse a sí misma y nadie se entera.
func TestLasConsultasDelPlanificadorEjecutan(t *testing.T) {
	st, ctx := abrir(t)

	casos := []struct {
		nombre string
		correr func() error
	}{
		{"ResumenOrdenes", func() error { _, err := st.ResumenOrdenes(ctx); return err }},
		{"ResumenPublicaciones", func() error { _, err := st.ResumenPublicaciones(ctx); return err }},
		{"TrabajosFallidos", func() error { _, err := st.TrabajosFallidos(ctx); return err }},
		{"PublicadosSinStock", func() error { _, err := st.PublicadosSinStock(ctx); return err }},
		{"HorariosVencidos", func() error { _, err := st.HorariosVencidos(ctx); return err }},
		{"ListarHorarios", func() error { _, err := st.ListarHorarios(ctx); return err }},
		{"ListarAlertas", func() error { _, err := st.ListarAlertas(ctx, false); return err }},
		{"ContarAlertas", func() error { _, err := st.ContarAlertas(ctx); return err }},
		{"ListarOrdenes", func() error { _, err := st.ListarOrdenes(ctx, 5); return err }},
		{"OrdenesPendientesOdoo", func() error { _, err := st.OrdenesPendientesOdoo(ctx, 5); return err }},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			if err := c.correr(); err != nil {
				t.Fatalf("%s falló contra el esquema real: %v", c.nombre, err)
			}
		})
	}
}

func TestLasConsultasDeAtributosEjecutan(t *testing.T) {
	st, ctx := abrir(t)

	if _, err := st.ResumenAtributos(ctx, "mercadolibre"); err != nil {
		t.Fatalf("ResumenAtributos falló: %v", err)
	}
	if _, err := st.CategoriasMapeadas(ctx, "mercadolibre"); err != nil {
		t.Fatalf("CategoriasMapeadas falló: %v", err)
	}
	if _, err := st.ProductosParaAtributos(ctx, "mercadolibre"); err != nil {
		t.Fatalf("ProductosParaAtributos falló: %v", err)
	}
}

// CandidatosPublicacion es la consulta más grande del sistema y la que decide
// qué sale a cada canal: merece su propia comprobación.
func TestCandidatosPublicacionEjecuta(t *testing.T) {
	st, ctx := abrir(t)

	cuentas, err := st.ListarCuentas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cuentas) == 0 {
		t.Skip("sin cuentas de canal configuradas")
	}
	if _, err := st.CandidatosPublicacion(ctx, cuentas[0].ID); err != nil {
		t.Fatalf("CandidatosPublicacion falló: %v", err)
	}
}
