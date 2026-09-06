package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

// Autenticación por middleware, no handler a handler.
//
// El módulo de auth ya traía extraerClaims y exigirRol, pero se aplicaban
// dentro de cada manejador que se acordara de llamarlos. El resultado era que
// de más de treinta rutas solo /api/auditoria estaba protegida: el catálogo,
// los precios, las credenciales de canal y las conexiones a Odoo respondían a
// cualquiera que alcanzara el puerto.
//
// Con un middleware que cierra por defecto, una ruta nueva nace protegida y hay
// que abrirla a propósito. Es el sentido correcto: olvidarse protege de más,
// no de menos.

// rutasPublicas son las únicas que se sirven sin sesión, y cada una por un
// motivo concreto.
var rutasPublicas = map[string]bool{
	// Sin esto no se podría iniciar sesión nunca.
	"/api/auth/login": true,
	// Sonda de salud: la usa el orquestador antes de que exista sesión.
	"/healthz": true,
}

// prefijosPublicos cubre las rutas con parte variable.
var prefijosPublicos = []string{
	// LAS IMÁGENES TIENEN QUE SER PÚBLICAS. Los canales de venta descargan
	// las fotos desde esta URL con un cliente anónimo: protegerlas
	// publicaría fichas sin imagen en las cuatro tiendas.
	//
	// No filtra nada sensible: la ruta es /imagenes/<sha256>/<variante>, así
	// que solo se puede pedir una foto cuyo hash ya se conoce.
	"/imagenes/",
	// Webhooks de los canales: llegan de servidores ajenos que no tienen
	// sesión. Su autenticidad se comprueba con la firma del propio canal.
	"/api/webhooks/",
}

func esPublica(ruta string) bool {
	if rutasPublicas[ruta] {
		return true
	}
	for _, p := range prefijosPublicos {
		if strings.HasPrefix(ruta, p) {
			return true
		}
	}
	return false
}

// exigirSesion envuelve el enrutador y rechaza lo que llegue sin token válido
// o cuyo usuario ya no tenga derecho a entrar.
//
// Comprobar la firma no basta. El token es un papel firmado hace hasta
// veinticuatro horas: dice quién era el portador y qué rol tenía entonces.
// Borrar al usuario de la tabla, desactivarlo o bajarle el rol no invalidaba
// nada, así que su sesión abierta seguía funcionando con normalidad hasta que
// el token caducaba por tiempo. Aquí se contrasta con la base —a través de una
// caché corta, ver sesiones.go— y el rol que rige el resto de la petición pasa
// a ser el que hay ahora en users, no el que se firmó.
func (s *Server) exigirSesion(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// El preflight de CORS no lleva cabecera de autorización: rechazarlo
		// rompería el navegador antes de que llegue a mandar el token.
		if r.Method == http.MethodOptions || esPublica(r.URL.Path) {
			siguiente.ServeHTTP(w, r)
			return
		}

		claims := s.extraerClaims(r)
		if claims == nil {
			escribir(w, http.StatusUnauthorized,
				map[string]string{"error": "sesión requerida"})
			return
		}

		// Un token de una sesión cerrada no vale, aunque su firma y su fecha
		// sigan siendo buenas.
		if tokensRevocados.EstaRevocado(tokenDe(r)) {
			escribir(w, http.StatusUnauthorized,
				map[string]string{"error": "la sesión se cerró"})
			return
		}

		estado, err := s.vigencia(r.Context(), claims.UserID)
		if err != nil {
			// La base no contesta. No es culpa de quien pide, y devolver 401
			// cerraría la sesión de todo el mundo en el navegador por una
			// caída pasajera de PostgreSQL: se responde «no disponible».
			s.log.Error("no se pudo comprobar la vigencia de la sesión",
				"usuario", claims.UserID, "error", err)
			escribir(w, http.StatusServiceUnavailable,
				map[string]string{"error": "no se pudo verificar la sesión"})
			return
		}
		if !estado.valida() {
			escribir(w, http.StatusUnauthorized,
				map[string]string{"error": "tu sesión ya no es válida"})
			return
		}

		// El rol vigente manda sobre el firmado, y viaja en el contexto para
		// que los manejadores que exigen admin vean la degradación igual de
		// pronto que la desactivación.
		vigentes := *claims
		vigentes.Role = estado.rol
		r = r.WithContext(conClaims(r.Context(), &vigentes))

		// Las escrituras exigen operador: un rol de solo lectura no debe
		// poder cambiar precios ni publicar, aunque tenga sesión válida.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !tienePermisoEscritura(vigentes.Role) {
				escribir(w, http.StatusForbidden,
					map[string]string{"error": "tu rol es de solo lectura"})
				return
			}
		}
		siguiente.ServeHTTP(w, r)
	})
}

// vigencia responde si el usuario del token sigue siendo alguien, tirando de
// la caché y bajando a la base solo cuando lo guardado ha caducado.
func (s *Server) vigencia(ctx context.Context, userID int64) (estadoSesion, error) {
	if e, hay := s.sesiones.leer(userID); hay {
		return e, nil
	}

	rol, activo, err := s.st.EstadoUsuario(ctx, userID)
	switch {
	case err == nil:
		e := estadoSesion{existe: true, activo: activo, rol: rol}
		s.sesiones.guardar(userID, e)
		return e, nil
	case errors.Is(err, store.ErrUsuarioNoExiste):
		// Que no exista es una respuesta, no un fallo: se cachea para no
		// repetir la consulta en cada petición del token huérfano.
		e := estadoSesion{}
		s.sesiones.guardar(userID, e)
		return e, nil
	default:
		return estadoSesion{}, err
	}
}

// conClaims deja los claims vigentes en el contexto de la petición.
func conClaims(ctx context.Context, c *auth.Claims) context.Context {
	return context.WithValue(ctx, usuarioContextKey, c)
}

// tienePermisoEscritura: viewer solo mira; operator y admin escriben.
func tienePermisoEscritura(rol string) bool {
	return rol == "admin" || rol == "operator"
}
