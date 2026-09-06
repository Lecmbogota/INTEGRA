package api

import (
	"net/http"
	"strconv"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

func (s *Server) registrarObservabilidad(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/canales/llamadas", s.consultarLlamadasAPI)
}

func (s *Server) consultarLlamadasAPI(w http.ResponseWriter, r *http.Request) {
	if !s.exigirRol(w, r, auth.RolViewer) {
		return
	}

	q := r.URL.Query()
	var cuentaID *int64
	if cStr := q.Get("cuenta_id"); cStr != "" {
		if id, err := strconv.ParseInt(cStr, 10, 64); err == nil {
			cuentaID = &id
		}
	}

	soloErrores := q.Get("errores") == "true" || q.Get("errores") == "1"
	limite := entero(q.Get("limite"), 50)

	llamadas, err := s.st.LlamadasRecientesAPI(r.Context(), cuentaID, soloErrores, limite)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if llamadas == nil {
		llamadas = []store.RegistroLlamadaAPI{}
	}

	escribir(w, http.StatusOK, map[string]any{
		"items": llamadas,
	})
}
