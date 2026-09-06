package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/notificaciones"
)

// Destinos de aviso: a dónde sale lo que Integra tiene que contar.
//
// Sin ninguno configurado, todo el sistema de alertas es una tabla que nadie
// lee: el planificador levanta el aviso, lo guarda y ahí se queda. Estas rutas
// son lo único que separa «Integra avisa» de «Integra apunta».
//
// Solo administradores: la configuración SMTP lleva credenciales de correo de
// la empresa, y cambiar el destinatario de las alertas es una forma silenciosa
// de dejar a todo el mundo a oscuras.
func (s *Server) registrarAvisos(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/avisos/destinos", s.listarDestinosAviso)
	mux.HandleFunc("PUT /api/avisos/destinos", s.guardarDestinoAviso)
	mux.HandleFunc("DELETE /api/avisos/destinos/{id}", s.borrarDestinoAviso)
	mux.HandleFunc("POST /api/avisos/destinos/{id}/probar", s.probarDestinoAviso)
}

func (s *Server) listarDestinosAviso(w http.ResponseWriter, r *http.Request) {
	if !s.exigirRol(w, r, auth.RolAdmin) {
		return
	}
	lista, err := s.st.ListarDestinos(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}

	// Los destinatarios se sacan de la configuración cifrada porque son lo
	// único de ahí dentro que hay que poder ver: «a quién le llega esto» es la
	// pregunta que se hace cualquiera al abrir la pantalla. El servidor, el
	// usuario y la contraseña no salen.
	for i := range lista {
		d, err := s.st.DestinoPorID(r.Context(), lista[i].ID)
		if err != nil {
			continue
		}
		if cfg, err := s.configDe(d.ConfigEnc); err == nil {
			lista[i].Destinatarios = cfg.Destinatarios
		}
	}
	escribir(w, http.StatusOK, lista)
}

// configDe descifra y decodifica lo que se guardó del destino.
func (s *Server) configDe(enc []byte) (notificaciones.ConfigSMTP, error) {
	var cfg notificaciones.ConfigSMTP
	if s.cif == nil {
		return cfg, fmt.Errorf("no hay clave maestra configurada")
	}
	claro, err := s.cif.DescifrarTexto(enc)
	if err != nil {
		return cfg, fmt.Errorf("no se pudo descifrar la configuración: %w", err)
	}
	if err := json.Unmarshal([]byte(claro), &cfg); err != nil {
		return cfg, fmt.Errorf("configuración ilegible: %w", err)
	}
	return cfg, nil
}

type peticionDestino struct {
	ID           int64                     `json:"id"`
	Nombre       string                    `json:"nombre"`
	MinSeveridad string                    `json:"min_severidad"`
	Activo       bool                      `json:"activo"`
	Config       notificaciones.ConfigSMTP `json:"config"`
}

var severidadesValidas = map[string]bool{
	"info": true, "warning": true, "error": true, "critical": true,
}

func (s *Server) guardarDestinoAviso(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolAdmin)
	if claims == nil {
		return
	}

	var req peticionDestino
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	req.Nombre = strings.TrimSpace(req.Nombre)
	if req.Nombre == "" {
		// Un destino sin nombre es una fila en blanco entre varias: al leer
		// «no llegan los avisos» hay que poder decir cuál de ellos falla.
		http.Error(w, "ponle un nombre al destino para reconocerlo en la lista", http.StatusBadRequest)
		return
	}
	if !severidadesValidas[req.MinSeveridad] {
		req.MinSeveridad = "error"
	}

	req.Config.Remitente = strings.TrimSpace(req.Config.Remitente)
	req.Config.Host = strings.TrimSpace(req.Config.Host)
	req.Config.Destinatarios = limpiarCorreos(req.Config.Destinatarios)

	// La contraseña no vuelve nunca al navegador, así que al editar llega
	// vacía salvo que la estén cambiando: en ese caso se conserva la que ya
	// estaba. Sin esto, guardar un cambio de severidad borraría la contraseña
	// y el destino dejaría de enviar sin que nadie tocara el correo.
	if req.ID != 0 && req.Config.Password == "" {
		anterior, err := s.st.DestinoPorID(r.Context(), req.ID)
		if err != nil {
			s.fallo(w, err)
			return
		}
		if cfg, err := s.configDe(anterior.ConfigEnc); err == nil {
			req.Config.Password = cfg.Password
		}
	}

	if err := validarSMTP(req.Config); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.cif == nil {
		s.fallo(w, fmt.Errorf("no hay clave maestra configurada: la contraseña SMTP no se puede cifrar"))
		return
	}

	crudo, err := json.Marshal(req.Config)
	if err != nil {
		s.fallo(w, err)
		return
	}
	enc, err := s.cif.Cifrar(crudo)
	if err != nil {
		s.fallo(w, err)
		return
	}

	id, err := s.st.GuardarDestino(r.Context(), req.ID, "email", req.Nombre, req.MinSeveridad, req.Activo, enc)
	if err != nil {
		s.fallo(w, err)
		return
	}

	accion := "create"
	if req.ID != 0 {
		accion = "update"
	}
	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, accion, "notification_destinations",
		strconv.FormatInt(id, 10), nil, resumenDestino(req), r.RemoteAddr)

	escribir(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

// resumenDestino es lo que se guarda en la auditoría.
//
// La auditoría se consulta por API y se conserva indefinidamente: volcar el
// cuerpo tal cual dejaría la contraseña SMTP en claro en otra tabla, con lo
// que no habría servido de nada cifrarla en la suya.
func resumenDestino(req peticionDestino) map[string]any {
	m := map[string]any{
		"nombre":        req.Nombre,
		"min_severidad": req.MinSeveridad,
		"activo":        req.Activo,
		"host":          req.Config.Host,
		"remitente":     req.Config.Remitente,
		"destinatarios": req.Config.Destinatarios,
	}
	if req.Config.Password != "" {
		m["password"] = "(fijada, no se registra)"
	}
	return m
}

// limpiarCorreos quita espacios y vacíos de la lista de destinatarios.
//
// La pantalla los pide separados por comas, y una coma de más dejaba un
// destinatario vacío que el servidor SMTP rechaza: el correo entero no salía
// por una coma.
func limpiarCorreos(in []string) []string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		for _, parte := range strings.Split(c, ",") {
			if p := strings.TrimSpace(parte); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func validarSMTP(c notificaciones.ConfigSMTP) error {
	if c.Host == "" {
		return fmt.Errorf("falta el servidor de correo")
	}
	if c.Remitente == "" {
		return fmt.Errorf("falta la dirección desde la que se envía")
	}
	if len(c.Destinatarios) == 0 {
		return fmt.Errorf("falta al menos un destinatario: un destino sin nadie a quien avisar no avisa")
	}
	if c.Usuario != "" && c.Password == "" {
		return fmt.Errorf("hay usuario pero no contraseña")
	}
	return nil
}

func (s *Server) borrarDestinoAviso(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolAdmin)
	if claims == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "identificador inválido", http.StatusBadRequest)
		return
	}
	if err := s.st.BorrarDestino(r.Context(), id); err != nil {
		s.fallo(w, err)
		return
	}
	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, "delete", "notification_destinations",
		strconv.FormatInt(id, 10), nil, nil, r.RemoteAddr)
	escribir(w, http.StatusOK, map[string]any{"estado": "borrado"})
}

// probarDestinoAviso manda un correo de verdad.
//
// Es la única forma de saber que un destino funciona. Una configuración SMTP
// que nadie ha probado no se descubre rota el día que se guarda, sino el día
// que hay una alerta de verdad —que es justo el día en que no se puede
// descubrir nada—.
func (s *Server) probarDestinoAviso(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolAdmin)
	if claims == nil {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "identificador inválido", http.StatusBadRequest)
		return
	}

	d, err := s.st.DestinoPorID(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	cfg, err := s.configDe(d.ConfigEnc)
	if err != nil {
		escribir(w, http.StatusOK, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}

	enviar := s.enviarCorreo
	if enviar == nil {
		enviar = notificaciones.EnviarSMTP
	}
	err = enviar(cfg,
		"Integra: prueba de destino de avisos",
		"Este es un correo de prueba enviado desde Integra.\n\n"+
			"Si te ha llegado, el destino «"+d.Nombre+"» está bien configurado y por\n"+
			"aquí saldrán los avisos de catálogo, pedidos y canales.\n")

	// El resultado se anota igual que un envío real, para que la pantalla
	// enseñe el mismo estado que enseñaría tras un aviso de verdad.
	_ = s.st.AnotarEnvioNotificacion(r.Context(), id, err)
	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, "test", "notification_destinations",
		strconv.FormatInt(id, 10), nil, map[string]any{"ok": err == nil}, r.RemoteAddr)

	if err != nil {
		escribir(w, http.StatusOK, map[string]any{"ok": false, "mensaje": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]any{
		"ok":      true,
		"mensaje": "Correo de prueba enviado a " + strings.Join(cfg.Destinatarios, ", "),
	})
}
