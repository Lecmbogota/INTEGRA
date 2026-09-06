package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

// «Se vendieron 40 unidades a mitad de precio, ¿quién lo tocó?». Hasta ahora
// la respuesta era que no constaba: se auditaban el login y los usuarios, y
// nada más. Estas pruebas van contra el manejador HTTP entero, que es donde
// está el usuario de la sesión: auditar dentro del store perdería justo eso.

func servidorConBase(t *testing.T) (*store.Store, context.Context, *http.ServeMux) {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("abriendo la base: %v", err)
	}
	t.Cleanup(st.Close)

	s := &Server{
		st:       st,
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		sesiones: nuevaCacheSesiones(ttlSesion),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/productos/{id}", s.editarProducto)
	return st, ctx, mux
}

// comoOperador arma la petición tal y como llega tras el middleware: con los
// claims vigentes en el contexto y con RemoteAddr con puerto, que es lo que
// pone net/http de verdad.
func comoOperador(t *testing.T, usuarioID int64, ruta, cuerpo string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPatch, ruta, strings.NewReader(cuerpo))
	r.RemoteAddr = "192.168.10.20:41234"
	return r.WithContext(conClaims(r.Context(),
		&auth.Claims{UserID: usuarioID, Role: auth.RolOperator}))
}

func TestCambiarElPrecioDejaRastroDeQuienYDeCuantoACuanto(t *testing.T) {
	st, ctx, mux := servidorConBase(t)
	usuarioID := usuarioAuditor(t, st, ctx)
	varianteID := cobayaDeAuditoria(t, st, ctx)

	filtro := store.FiltroAuditoria{
		Entity: "product_variants", EntityID: strconv.FormatInt(varianteID, 10)}
	_, previos := auditoriaDe(t, st, ctx, filtro)

	poner := func(precio float64) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, comoOperador(t, usuarioID,
			"/api/productos/"+strconv.FormatInt(varianteID, 10),
			fmt.Sprintf(`{"precio":%.0f}`, precio)))
		if w.Code != http.StatusOK {
			t.Fatalf("guardando el precio %.0f: %d %s", precio, w.Code, w.Body.String())
		}
	}
	// Un precio sano y encima el dedazo: la fila del segundo tiene que traer
	// el primero como valor anterior.
	poner(400000)
	poner(40000)

	logs, total := auditoriaDe(t, st, ctx, filtro)
	if total-previos != 2 {
		t.Fatalf("los dos cambios de precio tenían que constar: %d filas nuevas", total-previos)
	}

	ultimo := logs[0] // ListarAuditoria devuelve del más reciente al más antiguo
	if ultimo.UserID == nil || *ultimo.UserID != usuarioID {
		t.Errorf("no consta quién lo tocó: %v", ultimo.UserID)
	}
	if ultimo.IP == nil || *ultimo.IP != "192.168.10.20" {
		t.Errorf("no consta desde dónde: %v", ultimo.IP)
	}
	if de, a := precioDe(t, ultimo.Before), precioDe(t, ultimo.After); de != 400000 || a != 40000 {
		t.Errorf("el cambio no queda explicado: %.0f y %.0f, se esperaba 400000 y 40000", de, a)
	}
}

// Sacar un producto del catálogo publicable lo retira de los cuatro canales.
// Si no constara, un producto desaparecido de la venta no tendría explicación.
func TestExcluirUnProductoDelCatalogoTambienConsta(t *testing.T) {
	st, ctx, mux := servidorConBase(t)
	usuarioID := usuarioAuditor(t, st, ctx)
	varianteID := cobayaDeAuditoria(t, st, ctx)

	filtro := store.FiltroAuditoria{
		Entity: "product_variants", EntityID: strconv.FormatInt(varianteID, 10)}
	_, previos := auditoriaDe(t, st, ctx, filtro)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, comoOperador(t, usuarioID,
		"/api/productos/"+strconv.FormatInt(varianteID, 10), `{"excluido":true}`))
	if w.Code != http.StatusOK {
		t.Fatalf("excluyendo el producto: %d %s", w.Code, w.Body.String())
	}

	logs, total := auditoriaDe(t, st, ctx, filtro)
	if total-previos != 1 {
		t.Fatalf("la exclusión no consta: %d filas nuevas", total-previos)
	}
	var antes, despues map[string]any
	if err := json.Unmarshal(logs[0].Before, &antes); err != nil {
		t.Fatalf("el valor anterior no es legible: %v", err)
	}
	if err := json.Unmarshal(logs[0].After, &despues); err != nil {
		t.Fatalf("el valor nuevo no es legible: %v", err)
	}
	if antes["excluido"] != false || despues["excluido"] != true {
		t.Errorf("la exclusión no quedó registrada: %v a %v", antes, despues)
	}
}

// Editar un campo que no es ni el precio ni la exclusión no puede inventar un
// cambio de precio: comparar los punteros por dirección llenaría el registro
// de ruido que esconde lo que sí importa.
func TestEditarLaDescripcionNoInventaUnCambioDePrecio(t *testing.T) {
	st, ctx, mux := servidorConBase(t)
	usuarioID := usuarioAuditor(t, st, ctx)
	varianteID := cobayaDeAuditoria(t, st, ctx)

	filtro := store.FiltroAuditoria{
		Entity: "product_variants", EntityID: strconv.FormatInt(varianteID, 10)}
	_, previos := auditoriaDe(t, st, ctx, filtro)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, comoOperador(t, usuarioID,
		"/api/productos/"+strconv.FormatInt(varianteID, 10),
		`{"descripcion":"Una descripción de prueba."}`))
	if w.Code != http.StatusOK {
		t.Fatalf("editando la descripción: %d %s", w.Code, w.Body.String())
	}

	if _, total := auditoriaDe(t, st, ctx, filtro); total != previos {
		t.Errorf("una edición de descripción dejó %d filas de auditoría de precio", total-previos)
	}
}

// ------------------------------------------------------------- ayudas

func auditoriaDe(t *testing.T, st *store.Store, ctx context.Context,
	f store.FiltroAuditoria) ([]store.RegistroAuditoria, int) {
	t.Helper()
	logs, total, err := st.ListarAuditoria(ctx, f)
	if err != nil {
		t.Fatalf("consultando la auditoría: %v", err)
	}
	return logs, total
}

func precioDe(t *testing.T, crudo json.RawMessage) float64 {
	t.Helper()
	if len(crudo) == 0 {
		return 0
	}
	var m map[string]any
	if err := json.Unmarshal(crudo, &m); err != nil {
		t.Fatalf("el valor guardado no es legible: %v", err)
	}
	if v, ok := m["precio"].(float64); ok {
		return v
	}
	return 0
}

func usuarioAuditor(t *testing.T, st *store.Store, ctx context.Context) int64 {
	t.Helper()
	email := fmt.Sprintf("zz-auditoria-%d@integra.local", time.Now().UnixNano())
	id, err := st.CrearUsuario(ctx, email, "ZZ Auditoría", "$2a$10$hashfalsoquenadievaausar", auth.RolOperator)
	if err != nil {
		t.Fatalf("creando el usuario de prueba: %v", err)
	}
	// Se desactiva en vez de borrarse: la fila de auditoría apunta a su
	// usuario, y el registro tiene que seguir diciendo quién fue.
	t.Cleanup(func() {
		_ = st.ActualizarUsuario(context.Background(), id, "ZZ Auditoría", auth.RolOperator, false)
	})
	return id
}

// cobayaDeAuditoria toma una variante del catálogo y programa devolverla a
// como estaba: estas pruebas escriben de verdad sobre ella.
func cobayaDeAuditoria(t *testing.T, st *store.Store, ctx context.Context) int64 {
	t.Helper()
	filas, _, err := st.ListarProductos(ctx, store.FiltroProductos{Limite: 1})
	if err != nil || len(filas) == 0 {
		t.Skipf("la base no tiene catálogo con el que probar: %v", err)
	}
	f := filas[0]
	productoID, err := st.ProductoDeVariante(ctx, f.ID)
	if err != nil {
		t.Fatalf("buscando el producto de la variante: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = st.ActualizarPrecio(bg, f.ID, f.Precio)
		_ = st.ActualizarExclusion(bg, productoID, f.Excluido)
	})
	return f.ID
}
