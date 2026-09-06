package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

// El agujero que estas pruebas cierran se comprobó a mano una vez: se borró un
// usuario de la base y su pestaña abierta siguió funcionando como si nada,
// porque el middleware solo miraba la firma y la fecha del token. Aquí se
// reproduce el escenario entero —token válido, usuario dado de baja, siguiente
// petición— contra el esquema real.
//
// Como el resto de pruebas de integración, se saltan sin INTEGRA_DATABASE_URL.

func servidorDePrueba(t *testing.T) (*Server, *store.Store, context.Context) {
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

	// Sin banco de imágenes, cifrador ni cola: nada de eso interviene en la
	// autenticación, y el servidor los admite nulos.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Nuevo(st, log, ":0", nil, nil, nil, nil), st, ctx
}

// cobayaConSesion crea un usuario desechable y su token, tal y como se lo
// habría llevado el navegador al iniciar sesión.
func cobayaConSesion(t *testing.T, s *Server, st *store.Store, ctx context.Context, rol string) (*auth.Usuario, string) {
	t.Helper()

	email := fmt.Sprintf("zz-test-api-%d@integra.local", time.Now().UnixNano())
	hash, err := auth.HashPassword("contraseña-de-prueba")
	if err != nil {
		t.Fatalf("hasheando: %v", err)
	}
	id, err := st.CrearUsuario(ctx, email, "ZZ Cobaya API", hash, rol)
	if err != nil {
		t.Fatalf("creando el usuario de prueba: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
		s.sesiones.olvidar(id)
	})

	token, err := auth.GenerarToken(id, email, rol, s.claveFirma(), time.Hour)
	if err != nil {
		t.Fatalf("firmando el token: %v", err)
	}

	u, err := st.UsuarioPorID(ctx, id)
	if err != nil {
		t.Fatalf("releyendo el usuario: %v", err)
	}
	return u, token
}

func pedirComo(t *testing.T, s *Server, metodo, ruta, token string) int {
	t.Helper()

	r := httptest.NewRequest(metodo, ruta, nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(w, r)
	return w.Code
}

func TestLaSesionDeUnUsuarioBorradoDejaDeValer(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	u, token := cobayaConSesion(t, s, st, ctx, auth.RolOperator)

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusOK {
		t.Fatalf("el usuario recién creado debía entrar, dio HTTP %d", got)
	}

	if _, err := st.BorrarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("borrando al usuario: %v", err)
	}
	// La CLI y el SQL a mano no pueden avisar al servidor; aquí se adelanta el
	// vencimiento de la caché para no tener que esperar los 30 s de rigor.
	s.sesiones.olvidar(u.ID)

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusUnauthorized {
		t.Fatalf("el token de un usuario borrado dio HTTP %d, se esperaba 401", got)
	}
}

func TestLaSesionDeUnUsuarioDesactivadoDejaDeValer(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	u, token := cobayaConSesion(t, s, st, ctx, auth.RolOperator)

	if _, err := st.DesactivarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("desactivando al usuario: %v", err)
	}
	s.sesiones.olvidar(u.ID)

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusUnauthorized {
		t.Fatalf("el token de un usuario desactivado dio HTTP %d, se esperaba 401", got)
	}

	// Y al devolverle el acceso vuelve a entrar con el mismo token: la baja
	// revoca la sesión, no destruye la cuenta.
	if _, err := st.ActivarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("reactivando al usuario: %v", err)
	}
	s.sesiones.olvidar(u.ID)

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusOK {
		t.Fatalf("tras reactivarlo dio HTTP %d, se esperaba 200", got)
	}
}

// El rol también viaja firmado dentro del token. Degradar a alguien a viewer
// tiene que quitarle la escritura sin esperar a que caduque su sesión.
func TestDegradarElRolQuitaLaEscrituraEnCaliente(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	u, token := cobayaConSesion(t, s, st, ctx, auth.RolOperator)

	// Una ruta que no existe: al operador le pasa el control de rol y muere en
	// el enrutador (404); al viewer lo para antes el middleware (403). Así se
	// distingue quién rechazó sin ejecutar ninguna operación de verdad.
	if got := pedirComo(t, s, http.MethodPost, "/api/zz-ruta-inexistente", token); got != http.StatusNotFound {
		t.Fatalf("un operator debía superar el control de escritura, dio HTTP %d", got)
	}

	if err := st.ActualizarUsuario(ctx, u.ID, u.Name, auth.RolViewer, true); err != nil {
		t.Fatalf("degradando a viewer: %v", err)
	}
	s.sesiones.olvidar(u.ID)

	if got := pedirComo(t, s, http.MethodPost, "/api/zz-ruta-inexistente", token); got != http.StatusForbidden {
		t.Fatalf("un viewer dio HTTP %d al escribir, se esperaba 403 con el token de operator", got)
	}
}

// Un token firmado para un identificador que nunca existió no puede colarse
// por el hecho de estar bien firmado.
func TestUnTokenDeUsuarioInexistenteNoEntra(t *testing.T) {
	s, _, _ := servidorDePrueba(t)

	token, err := auth.GenerarToken(-1, "zz-fantasma@integra.local", auth.RolAdmin, s.claveFirma(), time.Hour)
	if err != nil {
		t.Fatalf("firmando: %v", err)
	}
	t.Cleanup(func() { s.sesiones.olvidar(-1) })

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusUnauthorized {
		t.Fatalf("un token de usuario inexistente dio HTTP %d, se esperaba 401", got)
	}
}

// Las rutas públicas siguen sirviéndose sin sesión: la comprobación nueva no
// puede haber cerrado la sonda de salud ni el propio login.
func TestLasRutasPublicasSiguenAbiertas(t *testing.T) {
	s, _, _ := servidorDePrueba(t)

	if got := pedirComo(t, s, http.MethodGet, "/healthz", ""); got != http.StatusOK {
		t.Errorf("/healthz dio HTTP %d sin sesión", got)
	}
	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", ""); got != http.StatusUnauthorized {
		t.Errorf("sin token debía dar 401, dio HTTP %d", got)
	}
}

// La contrapartida documentada de la caché: mientras la entrada siga viva, una
// baja hecha fuera del servidor todavía no se nota. Es una ventana de segundos
// —antes eran hasta 24 horas— y conviene que quede fijada por una prueba, para
// que nadie la confunda con el agujero que se acaba de cerrar.
func TestLaBajaExternaTardaLoQueDureLaCache(t *testing.T) {
	s, st, ctx := servidorDePrueba(t)
	u, token := cobayaConSesion(t, s, st, ctx, auth.RolOperator)

	// Esta petición deja el usuario cacheado como válido.
	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusOK {
		t.Fatalf("primera petición dio HTTP %d", got)
	}

	if _, err := st.DesactivarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("desactivando: %v", err)
	}

	// Sin olvidar() ni esperar al vencimiento, todavía pasa.
	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusOK {
		t.Fatalf("dentro de la ventana de caché seguía valiendo; dio HTTP %d", got)
	}

	// Y en cuanto vence, no.
	e, _ := s.sesiones.leer(u.ID)
	e.hasta = time.Now().Add(-time.Second)
	s.sesiones.mu.Lock()
	s.sesiones.m[u.ID] = e
	s.sesiones.mu.Unlock()

	if got := pedirComo(t, s, http.MethodGet, "/api/auth/perfil", token); got != http.StatusUnauthorized {
		t.Fatalf("vencida la caché debía dar 401, dio HTTP %d", got)
	}
}
