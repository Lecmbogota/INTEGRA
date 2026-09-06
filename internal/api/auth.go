package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/auth"
)

type contextKey string

const usuarioContextKey contextKey = "integra_usuario_claims"

func (s *Server) registrarAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/auth/perfil", s.perfil)

	// Gestión de usuarios (requiere admin)
	mux.HandleFunc("GET /api/usuarios", s.listarUsuarios)
	mux.HandleFunc("POST /api/usuarios", s.crearUsuario)
	mux.HandleFunc("PATCH /api/usuarios/{id}", s.editarUsuario)
}

func (s *Server) claveFirma() []byte {
	if s.cif != nil {
		return []byte(s.cif.ClaveBase64())
	}
	return []byte("integra-clave-secreta-default-fallback")
}

// ExtraerClaims obtiene los claims del usuario desde el context.
func ClaimsDeContext(ctx context.Context) *auth.Claims {
	if val, ok := ctx.Value(usuarioContextKey).(*auth.Claims); ok {
		return val
	}
	return nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	u, hash, err := s.st.UsuarioPorEmail(r.Context(), req.Email)
	if err != nil {
		http.Error(w, "correo o contraseña incorrectos", http.StatusUnauthorized)
		return
	}

	if !u.Active {
		http.Error(w, "el usuario está inactivo", http.StatusForbidden)
		return
	}

	if !auth.VerificarPassword(hash, req.Password) {
		http.Error(w, "correo o contraseña incorrectos", http.StatusUnauthorized)
		return
	}

	token, err := auth.GenerarToken(u.ID, u.Email, u.Role, s.claveFirma(), 24*time.Hour)
	if err != nil {
		s.fallo(w, err)
		return
	}

	_ = s.st.RegistrarLogin(r.Context(), u.ID)
	_ = s.st.RegistrarAuditoria(r.Context(), &u.ID, "login", "users", strconv.FormatInt(u.ID, 10), nil, nil, r.RemoteAddr)

	http.SetCookie(w, &http.Cookie{
		Name:     "integra_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	escribir(w, http.StatusOK, map[string]any{
		"token":   token,
		"usuario": u,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "integra_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
	escribir(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) perfil(w http.ResponseWriter, r *http.Request) {
	claims := s.extraerClaims(r)
	if claims == nil {
		http.Error(w, "no autenticado", http.StatusUnauthorized)
		return
	}

	u, err := s.st.UsuarioPorID(r.Context(), claims.UserID)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, u)
}

func (s *Server) listarUsuarios(w http.ResponseWriter, r *http.Request) {
	if !s.exigirRol(w, r, auth.RolAdmin) {
		return
	}

	lista, err := s.st.ListarUsuarios(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if lista == nil {
		lista = []auth.Usuario{}
	}
	escribir(w, http.StatusOK, lista)
}

func (s *Server) crearUsuario(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolAdmin)
	if claims == nil {
		return
	}

	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	id, err := s.st.CrearUsuario(r.Context(), req.Email, req.Name, hash, req.Role)
	if err != nil {
		s.fallo(w, err)
		return
	}

	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, "create", "users",
		strconv.FormatInt(id, 10), nil, sinPassword(req.Email, req.Name, req.Role, req.Password), r.RemoteAddr)

	escribir(w, http.StatusCreated, map[string]any{
		"ok": true,
		"id": id,
	})
}

// sinPassword arma lo que se guarda en la auditoría de un alta o una edición
// de usuario.
//
// El registro de auditoría se consulta por API y se conserva indefinidamente:
// volcar ahí el cuerpo de la petición tal cual dejaba la contraseña en claro
// en una tabla aparte, con lo que el bcrypt de `users` no protegía nada.
// Queda constancia de que la contraseña se fijó, nunca de cuál era.
func sinPassword(email, nombre, rol, password string, activo ...bool) map[string]any {
	m := map[string]any{"name": nombre, "role": rol}
	if email != "" {
		m["email"] = email
	}
	if len(activo) > 0 {
		m["active"] = activo[0]
	}
	if password != "" {
		m["password"] = "(fijada, no se registra)"
	}
	return m
}

func (s *Server) editarUsuario(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolAdmin)
	if claims == nil {
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de usuario inválido", http.StatusBadRequest)
		return
	}

	var req struct {
		Name     string `json:"name"`
		Role     string `json:"role"`
		Active   bool   `json:"active"`
		Password string `json:"password,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	prev, err := s.st.UsuarioPorID(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}

	if err := s.st.ActualizarUsuario(r.Context(), id, req.Name, req.Role, req.Active); err != nil {
		s.fallo(w, err)
		return
	}

	// Desactivar o degradar a alguien desde la interfaz tiene que notarse ya,
	// no dentro de medio minuto: se tira lo que la caché tenga guardado suyo.
	s.sesiones.olvidar(id)

	if req.Password != "" {
		hash, err := auth.HashPassword(req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.st.ActualizarPassword(r.Context(), id, hash); err != nil {
			s.fallo(w, err)
			return
		}
	}

	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, "update", "users",
		strconv.FormatInt(id, 10), prev,
		sinPassword("", req.Name, req.Role, req.Password, req.Active), r.RemoteAddr)

	escribir(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) extraerClaims(r *http.Request) *auth.Claims {
	// El middleware ya validó el token y refrescó el rol contra la base: si
	// dejó los claims en el contexto, esos son los buenos. Volver a leer el
	// token aquí devolvería el rol firmado, que puede estar caducado como
	// dato aunque el token siga siendo válido como papel.
	if c := ClaimsDeContext(r.Context()); c != nil {
		return c
	}

	authHeader := r.Header.Get("Authorization")
	var tokenStr string
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	} else if cookie, err := r.Cookie("integra_token"); err == nil {
		tokenStr = cookie.Value
	}

	if tokenStr == "" {
		return nil
	}

	claims, err := auth.VerificarToken(tokenStr, s.claveFirma())
	if err != nil {
		return nil
	}
	return claims
}

func (s *Server) exigirRol(w http.ResponseWriter, r *http.Request, rolMinimo string) bool {
	return s.exigirRolRetorno(w, r, rolMinimo) != nil
}

func (s *Server) exigirRolRetorno(w http.ResponseWriter, r *http.Request, rolMinimo string) *auth.Claims {
	claims := s.extraerClaims(r)
	if claims == nil {
		http.Error(w, "autenticación requerida", http.StatusUnauthorized)
		return nil
	}
	if !auth.TienePermiso(claims.Role, rolMinimo) {
		http.Error(w, "permisos insuficientes", http.StatusForbidden)
		return nil
	}
	return claims
}
