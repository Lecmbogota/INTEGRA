package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mdv/integra/internal/auth"
)

// Las bajas de usuario son la única operación del sistema que quita acceso, y
// hasta que existieron estos métodos se hacían con un DELETE a mano. Las
// pruebas van contra el esquema real —se saltan sin INTEGRA_DATABASE_URL— y
// crean y borran sus propios usuarios: no pueden apoyarse en filas ajenas,
// entre otras cosas porque una de ellas cuenta administradores.

// usuarioDePrueba da de alta un usuario desechable y programa su limpieza.
// El correo lleva la hora en nanosegundos para que dos ejecuciones seguidas,
// o dos casos del mismo test, no choquen contra el índice único de email.
func usuarioDePrueba(t *testing.T, st *Store, ctx context.Context, rol string) *auth.Usuario {
	t.Helper()

	email := fmt.Sprintf("zz-test-%d-%s@integra.local", time.Now().UnixNano(), rol)
	id, err := st.CrearUsuario(ctx, email, "ZZ Usuario De Prueba", "$2a$10$hashfalsoquenadievaausar", rol)
	if err != nil {
		t.Fatalf("creando el usuario de prueba: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})

	u, err := st.UsuarioPorID(ctx, id)
	if err != nil {
		t.Fatalf("releyendo el usuario de prueba: %v", err)
	}
	return u
}

// Lo que arregla el agujero: el middleware pregunta por aquí, así que esto
// tiene que distinguir «activo», «desactivado» y «ya no existe».
func TestEstadoUsuarioSigueLaActivacion(t *testing.T) {
	st, ctx := abrir(t)
	u := usuarioDePrueba(t, st, ctx, auth.RolOperator)

	rol, activo, err := st.EstadoUsuario(ctx, u.ID)
	if err != nil {
		t.Fatalf("EstadoUsuario de un usuario recién creado: %v", err)
	}
	if !activo || rol != auth.RolOperator {
		t.Fatalf("recién creado debería estar activo y ser operator, dio rol=%q activo=%v", rol, activo)
	}

	if _, err := st.DesactivarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("desactivando: %v", err)
	}
	if _, activo, err = st.EstadoUsuario(ctx, u.ID); err != nil {
		t.Fatalf("EstadoUsuario tras desactivar: %v", err)
	}
	if activo {
		t.Error("el usuario sigue figurando como activo después de desactivarlo")
	}

	if _, err := st.ActivarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("activando: %v", err)
	}
	if _, activo, err = st.EstadoUsuario(ctx, u.ID); err != nil {
		t.Fatalf("EstadoUsuario tras activar: %v", err)
	}
	if !activo {
		t.Error("el usuario no volvió a quedar activo")
	}
}

// Un cambio de rol tiene que verse aquí: el token lleva firmado el rol viejo y
// es esta consulta la que lo desmiente.
func TestEstadoUsuarioReflejaElRolNuevo(t *testing.T) {
	st, ctx := abrir(t)
	u := usuarioDePrueba(t, st, ctx, auth.RolAdmin)

	if err := st.ActualizarUsuario(ctx, u.ID, u.Name, auth.RolViewer, true); err != nil {
		t.Fatalf("degradando el usuario: %v", err)
	}

	rol, activo, err := st.EstadoUsuario(ctx, u.ID)
	if err != nil {
		t.Fatalf("EstadoUsuario: %v", err)
	}
	if rol != auth.RolViewer || !activo {
		t.Fatalf("rol=%q activo=%v, se esperaba viewer y activo", rol, activo)
	}
}

// Sin esto el middleware no puede separar «te han borrado» de «la base está
// caída», y confundirlas significa o dejar entrar a un borrado o echar a todo
// el mundo cuando PostgreSQL tose.
func TestEstadoUsuarioBorradoDevuelveErrUsuarioNoExiste(t *testing.T) {
	st, ctx := abrir(t)
	u := usuarioDePrueba(t, st, ctx, auth.RolOperator)

	if _, err := st.BorrarUsuario(ctx, u.Email); err != nil {
		t.Fatalf("borrando: %v", err)
	}

	_, _, err := st.EstadoUsuario(ctx, u.ID)
	if !errors.Is(err, ErrUsuarioNoExiste) {
		t.Fatalf("se esperaba ErrUsuarioNoExiste, dio %v", err)
	}

	if _, _, err := st.UsuarioPorEmail(ctx, u.Email); err == nil {
		t.Error("el usuario borrado sigue apareciendo por correo")
	}
}

// El correo se normaliza al crear (minúsculas, sin espacios): las bajas tienen
// que aceptar lo que el operador teclee, no solo la forma canónica.
func TestBajaAceptaElCorreoComoSeTeclea(t *testing.T) {
	st, ctx := abrir(t)
	u := usuarioDePrueba(t, st, ctx, auth.RolOperator)

	tecleado := "  " + upperASCII(u.Email) + "  "
	if _, err := st.DesactivarUsuario(ctx, tecleado); err != nil {
		t.Fatalf("desactivar con el correo en mayúsculas y con espacios: %v", err)
	}
	if _, activo, _ := st.EstadoUsuario(ctx, u.ID); activo {
		t.Error("no se desactivó al escribir el correo con otra caja")
	}
}

func TestBajaDeUsuarioInexistente(t *testing.T) {
	st, ctx := abrir(t)

	_, err := st.DesactivarUsuario(ctx, fmt.Sprintf("zz-no-existe-%d@integra.local", time.Now().UnixNano()))
	if !errors.Is(err, ErrUsuarioNoExiste) {
		t.Fatalf("se esperaba ErrUsuarioNoExiste, dio %v", err)
	}
}

// Quedarse sin ningún administrador activo no tiene arreglo desde la interfaz:
// no habría quien pudiera volver a crear usuarios. La comprobación se prueba
// dentro de una transacción que se revierte, porque para montar el escenario
// hay que dejar un solo administrador activo en toda la tabla y eso no puede
// tocar la base de verdad.
func TestNoSePuedeQuitarAlUltimoAdmin(t *testing.T) {
	st, ctx := abrir(t)
	admin := usuarioDePrueba(t, st, ctx, auth.RolAdmin)
	otro := usuarioDePrueba(t, st, ctx, auth.RolAdmin)

	tx, err := st.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abriendo transacción: %v", err)
	}
	defer tx.Rollback(ctx)

	// Todo el mundo fuera salvo nuestro admin: dentro de la transacción es el
	// único que queda.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET active = FALSE WHERE id <> $1`, admin.ID); err != nil {
		t.Fatalf("dejando un solo admin activo: %v", err)
	}

	if _, err := usuarioParaBaja(ctx, tx, admin.Email); !errors.Is(err, ErrUltimoAdmin) {
		t.Fatalf("se esperaba ErrUltimoAdmin al quitar al único admin, dio %v", err)
	}

	// Con otro administrador activo la baja sí debe permitirse.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET active = TRUE WHERE id = $1`, otro.ID); err != nil {
		t.Fatalf("reactivando al segundo admin: %v", err)
	}
	if _, err := usuarioParaBaja(ctx, tx, admin.Email); err != nil {
		t.Fatalf("con otro admin activo la baja debía permitirse, dio %v", err)
	}

	// Un usuario que no es admin nunca topa con el límite.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET role = 'viewer' WHERE id = $1`, otro.ID); err != nil {
		t.Fatalf("degradando al segundo usuario: %v", err)
	}
	if _, err := usuarioParaBaja(ctx, tx, otro.Email); err != nil {
		t.Fatalf("quitar a un viewer no debía chocar con nada, dio %v", err)
	}
}

// Un admin desactivado no cuenta como el último: ya no puede entrar, así que
// borrarlo no quita nada que no estuviera quitado.
func TestElAdminYaDesactivadoSePuedeBorrar(t *testing.T) {
	st, ctx := abrir(t)
	admin := usuarioDePrueba(t, st, ctx, auth.RolAdmin)

	tx, err := st.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("abriendo transacción: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE users SET active = FALSE`); err != nil {
		t.Fatalf("desactivando a todo el mundo: %v", err)
	}
	if _, err := usuarioParaBaja(ctx, tx, admin.Email); err != nil {
		t.Fatalf("un admin ya inactivo debía poder borrarse, dio %v", err)
	}
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
