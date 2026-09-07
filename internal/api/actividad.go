package api

import (
	"net/http"

	"github.com/mdv/integra/internal/auth"
)

// Actividad: qué está haciendo Integra ahora y qué hizo antes.
//
// Sin esto, el trabajo en segundo plano era invisible: quien pulsaba un botón
// no sabía si su envío estaba corriendo, esperando detrás de otros
// trescientos, o fallado desde hacía una hora. Y la auditoría se escribía sin
// que ninguna pantalla la mostrara.
//
// No exige rol de administrador: ver qué está pasando es justo lo que necesita
// quien opera. La auditoría detallada, con el antes y el después de cada
// cambio, sigue siendo solo de administradores en /api/auditoria.
func (s *Server) registrarActividad(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/actividad", s.actividad)
	// Cancelar y reintentar sí piden permiso de escritura: vacían o rellenan
	// la cola de todo el mundo.
	mux.HandleFunc("POST /api/actividad/{accion}", s.gobernarCola)
}

func (s *Server) actividad(w http.ResponseWriter, r *http.Request) {
	cola, err := s.st.ColaPorTipo(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	historia, err := s.st.Historia(r.Context(), entero(r.URL.Query().Get("limite"), 60))
	if err != nil {
		s.fallo(w, err)
		return
	}

	// El número de la campana: lo que está en marcha ahora mismo. Los fallidos
	// no cuentan aquí —tienen su propio aviso— porque un número que sube
	// cuando algo se rompe y no baja al arreglarlo deja de mirarse.
	activas := 0
	for _, t := range cola {
		activas += t.Corriendo + t.Pendientes
	}
	escribir(w, http.StatusOK, map[string]any{
		"activas":  activas,
		"cola":     cola,
		"historia": historia,
	})
}

// gobernarCola cancela lo pendiente o devuelve a la cola lo fallido.
//
// Solo se cancela lo que aún no ha empezado. Un trabajo en curso ya está
// hablando con el canal, y marcarlo cancelado aquí no desharía la llamada:
// dejaría la base diciendo que no se envió algo que sí salió, que es peor que
// esperar los segundos que tarda.
func (s *Server) gobernarCola(w http.ResponseWriter, r *http.Request) {
	claims := s.exigirRolRetorno(w, r, auth.RolOperator)
	if claims == nil {
		return
	}
	tipo := r.URL.Query().Get("tipo")

	var n int64
	var err error
	accion := r.PathValue("accion")
	switch accion {
	case "cancelar":
		n, err = s.st.CancelarPendientes(r.Context(), tipo)
	case "reintentar":
		n, err = s.st.ReintentarFallidos(r.Context(), tipo)
	default:
		http.Error(w, "acción desconocida: solo cancelar o reintentar", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}

	// Vaciar la cola de un canal entero es la clase de cosa que después nadie
	// recuerda haber hecho.
	_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, accion, "jobs",
		tipo, nil, map[string]any{"trabajos": n}, r.RemoteAddr)

	escribir(w, http.StatusOK, map[string]any{"afectados": n})
}
