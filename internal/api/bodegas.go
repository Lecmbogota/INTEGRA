package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/mdv/integra/internal/store"
)

// Bodegas por cuenta: qué almacenes de Odoo alimentan lo que se publica en
// cada canal. Estas dos rutas son la única forma de rellenar
// channel_account_warehouses, y sin filas ahí una cuenta no publica (ver
// store.ErrCuentaSinBodegas).
func (s *Server) registrarBodegas(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cuentas/{id}/bodegas", s.bodegasCuenta)
	mux.HandleFunc("PUT /api/cuentas/{id}/bodegas", s.asignarBodegas)
}

func (s *Server) bodegasCuenta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	b, err := s.st.BodegasDeCuenta(r.Context(), id)
	if err != nil {
		s.falloBodegas(w, err)
		return
	}
	if b == nil {
		b = []store.BodegaAsignable{}
	}
	escribir(w, http.StatusOK, b)
}

// asignarBodegas reemplaza la asignación entera con las casillas marcadas.
func (s *Server) asignarBodegas(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	var cuerpo struct {
		Bodegas []int64 `json:"bodegas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}

	// Lo que había, para la auditoría: cambiar las bodegas cambia qué stock
	// ve un canal entero, y conviene poder saber quién lo movió y desde qué.
	antes, err := s.st.BodegasDeCuenta(r.Context(), id)
	if err != nil {
		s.falloBodegas(w, err)
		return
	}
	if err := s.st.AsignarBodegas(r.Context(), id, cuerpo.Bodegas); err != nil {
		s.falloBodegas(w, err)
		return
	}
	if claims := s.extraerClaims(r); claims != nil {
		_ = s.st.RegistrarAuditoria(r.Context(), &claims.UserID, "update", "channel_account_warehouses",
			strconv.FormatInt(id, 10), idsAsignadas(antes), cuerpo.Bodegas, r.RemoteAddr)
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "asignadas"})
}

// falloBodegas separa el rechazo por datos de quien pide (422, con el motivo)
// de la avería (500, sin detalle).
func (s *Server) falloBodegas(w http.ResponseWriter, err error) {
	var rechazo *store.RechazoBodegas
	if errors.As(err, &rechazo) {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": rechazo.Error()})
		return
	}
	s.fallo(w, err)
}

func idsAsignadas(bodegas []store.BodegaAsignable) []int64 {
	ids := []int64{}
	for _, b := range bodegas {
		if b.Asignada {
			ids = append(ids, b.ID)
		}
	}
	return ids
}
