package api

import (
	"net/http"
	"strconv"

	"github.com/mdv/integra/internal/auth"
	"github.com/mdv/integra/internal/store"
)

func (s *Server) registrarAuditoria(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/auditoria", s.consultarAuditoria)
}

func (s *Server) consultarAuditoria(w http.ResponseWriter, r *http.Request) {
	// Solo administradores: el registro dice quién hizo qué, con qué IP y con
	// qué valores, y es justo lo que sirve para preparar un abuso. Un rol de
	// solo lectura no necesita verlo.
	if !s.exigirRol(w, r, auth.RolAdmin) {
		return
	}

	q := r.URL.Query()
	f := store.FiltroAuditoria{
		Entity:   q.Get("entity"),
		EntityID: q.Get("entity_id"),
		Limite:   entero(q.Get("limite"), 50),
		Offset:   entero(q.Get("offset"), 0),
	}

	if uStr := q.Get("user_id"); uStr != "" {
		if uid, err := strconv.ParseInt(uStr, 10, 64); err == nil {
			f.UserID = &uid
		}
	}

	logs, total, err := s.st.ListarAuditoria(r.Context(), f)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if logs == nil {
		logs = []store.RegistroAuditoria{}
	}

	escribir(w, http.StatusOK, map[string]any{
		"total":  total,
		"limite": f.Limite,
		"offset": f.Offset,
		"items":  logs,
	})
}
