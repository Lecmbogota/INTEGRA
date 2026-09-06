package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdv/integra/internal/store"
)

// El alcance «todos los del filtro actual» y el refresco de precios no se
// pueden probar contra la base sin una, así que lo que se prueba aquí es lo que
// decide la capa HTTP: qué filtro sale del cuerpo que manda la pantalla, qué
// consulta se usa para resolverlo y a qué cuentas se les rehacen los precios.

// ------------------------------------------------ el filtro de la pantalla

// La pantalla manda la categoría (web/src/Catalogo.tsx arma filtroActual con
// q, marca, categoria, problemas, excluidos y sin_precio). Si el struct que
// decodifica no la declara, Go la descarta sin decir nada y la operación se
// aplica a todo el catálogo: 450 productos reprecificados en vez de 12, sin
// deshacer.
func TestElCuerpoDeLaEdicionMasivaConservaLaCategoria(t *testing.T) {
	crudo := `{"filtro":{"q":"","marca":"","categoria":"Cómputo / Tabletas",
		"problemas":false,"excluidos":false,"sin_precio":false},
		"operacion":{"tipo":"precio_desde_coste","factor":1.35,"simular":true}}`

	var cuerpo cuerpoMasivo
	if err := json.Unmarshal([]byte(crudo), &cuerpo); err != nil {
		t.Fatalf("el cuerpo que manda la pantalla no se pudo leer: %v", err)
	}
	if cuerpo.Filtro == nil {
		t.Fatal("el filtro no llegó")
	}

	f := cuerpo.Filtro.aFiltro()
	if f.Categoria != "Cómputo / Tabletas" {
		t.Errorf("la categoría se perdió por el camino: %q", f.Categoria)
	}
	if cuerpo.Operacion.Tipo != store.MasivoPrecioDesdeCoste || cuerpo.Operacion.Factor != 1.35 {
		t.Errorf("la operación no se leyó bien: %+v", cuerpo.Operacion)
	}
}

// ------------------------------------------------ resolución del alcance

type catalogoFalso struct {
	// idsDeFiltro es lo que devuelve la consulta que NO filtra por categoría:
	// el catálogo entero.
	idsDeFiltro []int64
	// paginas son las respuestas sucesivas de ListarProductos.
	paginas [][]int64
	total   int

	llamadasIDs   int
	filtrosVistos []store.FiltroProductos
}

func (c *catalogoFalso) IDsDeFiltro(_ context.Context, f store.FiltroProductos) ([]int64, error) {
	c.llamadasIDs++
	c.filtrosVistos = append(c.filtrosVistos, f)
	return c.idsDeFiltro, nil
}

func (c *catalogoFalso) ListarProductos(_ context.Context, f store.FiltroProductos) ([]store.FilaProducto, int, error) {
	c.filtrosVistos = append(c.filtrosVistos, f)
	i := f.Offset / 500
	if i >= len(c.paginas) {
		return nil, c.total, nil
	}
	var out []store.FilaProducto
	for _, id := range c.paginas[i] {
		out = append(out, store.FilaProducto{ID: id})
	}
	return out, c.total, nil
}

// Con categoría, el alcance tiene que salir de la consulta que sí la aplica.
// Resolverlo con IDsDeFiltro devolvía el catálogo completo.
func TestElAlcanceConCategoriaNoSeSaleDeLaCategoria(t *testing.T) {
	cat := &catalogoFalso{
		idsDeFiltro: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, // todo el catálogo
		paginas:     [][]int64{{101, 102, 103}},             // los de la categoría
		total:       3,
	}

	ids, err := idsDelFiltro(context.Background(), cat, store.FiltroProductos{
		Categoria: "Cómputo / Tabletas",
	})
	if err != nil {
		t.Fatalf("resolviendo el alcance: %v", err)
	}

	if len(ids) != 3 {
		t.Fatalf("se esperaban los 3 de la categoría y llegaron %d: %v", len(ids), ids)
	}
	if cat.llamadasIDs != 0 {
		t.Error("con categoría no se puede resolver por la consulta que la ignora")
	}
	if cat.filtrosVistos[0].Categoria != "Cómputo / Tabletas" {
		t.Errorf("la categoría no llegó a la consulta: %+v", cat.filtrosVistos[0])
	}
}

// Sin categoría no cambia nada: se sigue resolviendo con la consulta ligera.
func TestElAlcanceSinCategoriaSigueUsandoLaConsultaDeSiempre(t *testing.T) {
	cat := &catalogoFalso{idsDeFiltro: []int64{1, 2, 3}}

	ids, err := idsDelFiltro(context.Background(), cat, store.FiltroProductos{Marca: "acme"})
	if err != nil {
		t.Fatalf("resolviendo el alcance: %v", err)
	}
	if len(ids) != 3 || cat.llamadasIDs != 1 {
		t.Fatalf("ids=%v llamadas=%d", ids, cat.llamadasIDs)
	}
}

// La paginación no puede pasarse del tope que acepta EditarMasivo, ni quedarse
// corta cuando la categoría tiene más de una página.
func TestElAlcanceConCategoriaPaginaHastaElTope(t *testing.T) {
	var paginas [][]int64
	var id int64 = 1
	for p := 0; p < 6; p++ {
		var pag []int64
		for i := 0; i < 500; i++ {
			pag = append(pag, id)
			id++
		}
		paginas = append(paginas, pag)
	}
	cat := &catalogoFalso{paginas: paginas, total: 3000}

	ids, err := idsDelFiltro(context.Background(), cat, store.FiltroProductos{Categoria: "Audio"})
	if err != nil {
		t.Fatalf("resolviendo el alcance: %v", err)
	}
	if len(ids) != topeMasivo {
		t.Fatalf("se esperaban %d ids (el tope) y llegaron %d", topeMasivo, len(ids))
	}
	if ids[len(ids)-1] != int64(topeMasivo) {
		t.Errorf("la última página no se leyó entera: último id %d", ids[len(ids)-1])
	}
}

// ------------------------------------------- refresco de precios efectivos

type recalculadorFalso struct {
	cuentas []store.CuentaCanal
	fallan  map[int64]bool

	hechas []int64
}

func (r *recalculadorFalso) ListarCuentas(context.Context) ([]store.CuentaCanal, error) {
	return r.cuentas, nil
}

func (r *recalculadorFalso) RecalcularPreciosCuenta(_ context.Context, id int64) (int, error) {
	r.hechas = append(r.hechas, id)
	if r.fallan[id] {
		return 0, errors.New("la cuenta no responde")
	}
	return 10, nil
}

// Editar el precio en la ficha dejaba effective_prices con el valor viejo, que
// es el que CandidatosPublicacion prefiere para el precio y para el hash: el
// motor no veía el cambio y el canal seguía vendiendo al precio anterior.
func TestElRefrescoRehaceLosPreciosDeTodasLasCuentasActivas(t *testing.T) {
	rc := &recalculadorFalso{cuentas: []store.CuentaCanal{
		{ID: 1, CanalCodigo: "mercadolibre", Activa: true},
		{ID: 2, CanalCodigo: "shopify", Activa: true},
		{ID: 3, CanalCodigo: "falabella", Activa: false},
	}}

	if err := refrescarPreciosEfectivos(context.Background(), rc); err != nil {
		t.Fatalf("no debía fallar: %v", err)
	}
	if len(rc.hechas) != 2 || rc.hechas[0] != 1 || rc.hechas[1] != 2 {
		t.Fatalf("se recalcularon %v; se esperaban las dos cuentas activas", rc.hechas)
	}
}

// Una cuenta que falla no puede dejar a las demás con el precio viejo.
func TestUnaCuentaQueFallaNoImpideRefrescarLasDemas(t *testing.T) {
	rc := &recalculadorFalso{
		cuentas: []store.CuentaCanal{
			{ID: 1, CanalCodigo: "mercadolibre", Activa: true},
			{ID: 2, CanalCodigo: "shopify", Activa: true},
		},
		fallan: map[int64]bool{1: true},
	}

	err := refrescarPreciosEfectivos(context.Background(), rc)
	if err == nil {
		t.Fatal("el fallo de la cuenta 1 tenía que llegar al log")
	}
	if len(rc.hechas) != 2 {
		t.Fatalf("se recalcularon %v; la cuenta 2 también tenía que intentarse", rc.hechas)
	}
}

// Solo las operaciones que tocan el PVP obligan a rehacer los precios
// efectivos; cambiar la marca o excluir no mueve ningún precio.
func TestSoloLasOperacionesDePrecioObliganARefrescar(t *testing.T) {
	casos := map[string]bool{
		store.MasivoPrecioDesdeCoste: true,
		store.MasivoPrecioFijo:       true,
		store.MasivoPrecioAjustar:    true,
		store.MasivoBorrarPrecio:     true,
		store.MasivoMarca:            false,
		store.MasivoExcluir:          false,
		store.MasivoIncluir:          false,
	}
	for tipo, quiere := range casos {
		if got := tocaPrecio(store.OperacionMasiva{Tipo: tipo}); got != quiere {
			t.Errorf("%s: tocaPrecio = %v, se esperaba %v", tipo, got, quiere)
		}
	}
}
