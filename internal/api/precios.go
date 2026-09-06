package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/mdv/integra/internal/store"
)

func (s *Server) registrarPrecios(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cuentas/{id}/reglas-precio", s.reglasPrecioCuenta)
	mux.HandleFunc("POST /api/cuentas/{id}/reglas-precio", s.guardarReglaPrecio)
	mux.HandleFunc("POST /api/cuentas/{id}/recalcular-precios", s.recalcularPreciosCuenta)
	mux.HandleFunc("PATCH /api/cuentas/{id}/suelo-costo", s.guardarSueloCosto)
	mux.HandleFunc("PUT /api/variantes/{id}/override-precio", s.guardarOverridePrecio)
	mux.HandleFunc("DELETE /api/variantes/{id}/override-precio", s.eliminarOverridePrecio)
	mux.HandleFunc("POST /api/variantes/{id}/ofertas", s.guardarOferta)
	mux.HandleFunc("GET /api/variantes/{id}/ofertas", s.ofertasDeVariante)
	mux.HandleFunc("DELETE /api/ofertas/{id}", s.cancelarOferta)
	mux.HandleFunc("GET /api/ofertas", s.ofertasVigentes)

	// Actualización masiva por hoja de cálculo: se descarga el catálogo, se
	// edita en Excel y se sube. La carga sin `aplicar=true` es un simulacro.
	mux.HandleFunc("GET /api/plantillas/precios", s.descargarPlantilla)
	mux.HandleFunc("POST /api/plantillas/precios", s.cargarPlantilla)
}

// ofertasDeVariante lista las promociones de un producto con su estado ya
// resuelto: programada, vigente, terminada o cancelada.
func (s *Server) ofertasDeVariante(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	ofertas, err := s.st.OfertasDeVariante(r.Context(), varianteID)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if ofertas == nil {
		ofertas = []store.OfertaVista{}
	}
	escribir(w, http.StatusOK, ofertas)
}

// cancelarOferta la desactiva. No la borra: una promoción que existió explica
// por qué algo se vendió a ese precio.
func (s *Server) cancelarOferta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	antes, _ := s.st.OfertaPorID(r.Context(), id)
	if err := s.st.CancelarOferta(r.Context(), id); err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.auditar(r, "cancel", "offers", strconv.FormatInt(id, 10), antes,
		map[string]any{"active": false})
	escribir(w, http.StatusOK, map[string]string{"estado": "cancelada"})
}

func (s *Server) ofertasVigentes(w http.ResponseWriter, r *http.Request) {
	ofertas, err := s.st.OfertasVigentes(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if ofertas == nil {
		ofertas = []store.OfertaVista{}
	}
	escribir(w, http.StatusOK, ofertas)
}

func (s *Server) reglasPrecioCuenta(w http.ResponseWriter, r *http.Request) {
	cuentaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de cuenta inválido", http.StatusBadRequest)
		return
	}

	reglas, err := s.st.ReglasPrecioDeCuenta(r.Context(), cuentaID)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{
		"cuenta_id": cuentaID,
		"reglas":    reglas,
	})
}

func (s *Server) guardarReglaPrecio(w http.ResponseWriter, r *http.Request) {
	cuentaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de cuenta inválido", http.StatusBadRequest)
		return
	}

	var req store.ReglaPrecioCanal
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}
	req.ChannelAccountID = cuentaID

	var antes any
	accion := "create"
	if req.ID != 0 {
		accion = "update"
		if previa, err := s.st.ReglaPrecioPorID(r.Context(), req.ID); err == nil && previa != nil {
			antes = previa
		}
	}

	id, err := s.st.GuardarReglaPrecioCanal(r.Context(), req)
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, accion, "channel_price_rules", strconv.FormatInt(id, 10), antes, req)

	escribir(w, http.StatusOK, map[string]any{
		"ok": true,
		"id": id,
	})
}

func (s *Server) recalcularPreciosCuenta(w http.ResponseWriter, r *http.Request) {
	cuentaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de cuenta inválido", http.StatusBadRequest)
		return
	}

	total, err := s.st.RecalcularPreciosCuenta(r.Context(), cuentaID)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{
		"ok":        true,
		"cuenta_id": cuentaID,
		"total":     total,
	})
}

// guardarSueloCosto fija el margen mínimo de la cuenta y si publicar por
// debajo de él frena el envío.
//
// Al guardar se recalculan los precios de la cuenta en el acto, porque es el
// recálculo el que aplica el suelo y el que rehace la lista de lo que está por
// debajo de coste: sin él, subir el margen sería un número en una pantalla que
// no cambia lo que sale a los canales hasta la siguiente pasada.
func (s *Server) guardarSueloCosto(w http.ResponseWriter, r *http.Request) {
	cuentaID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de cuenta inválido", http.StatusBadRequest)
		return
	}

	var req store.SueloCosto
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	antes, err := s.st.SueloCostoDeCuenta(r.Context(), cuentaID)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if err := s.st.ActualizarSueloCosto(r.Context(), cuentaID, req); err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	s.auditar(r, "update", "channel_accounts", strconv.FormatInt(cuentaID, 10), antes, req)

	total, err := s.st.RecalcularPreciosCuenta(r.Context(), cuentaID)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{"ok": true, "total": total})
}

type reqOverridePrecio struct {
	ChannelAccountID int64   `json:"channel_account_id"`
	Price            float64 `json:"price"`
	Reason           string  `json:"reason"`
}

func (s *Server) guardarOverridePrecio(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de variante inválido", http.StatusBadRequest)
		return
	}

	var req reqOverridePrecio
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	antes, _ := s.st.OverridePrecioDe(r.Context(), varianteID, req.ChannelAccountID)
	if err := s.st.GuardarOverridePrecio(r.Context(), varianteID, req.ChannelAccountID, req.Price, req.Reason, usuarioDe(r)); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "update", "price_overrides", refOverride(varianteID, req.ChannelAccountID), antes, req)

	escribir(w, http.StatusOK, map[string]any{"ok": true})
}

// refOverride identifica el override en la auditoría: un precio manual es de
// una variante EN una cuenta, así que la clave tiene que llevar las dos o el
// registro no dice en qué canal se cambió el precio.
func refOverride(varianteID, cuentaID int64) string {
	return fmt.Sprintf("%d:%d", varianteID, cuentaID)
}

func (s *Server) eliminarOverridePrecio(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de variante inválido", http.StatusBadRequest)
		return
	}

	cuentaIDStr := r.URL.Query().Get("channel_account_id")
	cuentaID, err := strconv.ParseInt(cuentaIDStr, 10, 64)
	if err != nil {
		http.Error(w, "parámetro channel_account_id inválido", http.StatusBadRequest)
		return
	}

	antes, _ := s.st.OverridePrecioDe(r.Context(), varianteID, cuentaID)
	if err := s.st.EliminarOverridePrecio(r.Context(), varianteID, cuentaID); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "delete", "price_overrides", refOverride(varianteID, cuentaID), antes, nil)

	escribir(w, http.StatusOK, map[string]any{"ok": true})
}

type reqOferta struct {
	ChannelAccountID int64      `json:"channel_account_id"`
	OfferPrice       float64    `json:"offer_price"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
}

func (s *Server) guardarOferta(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id de variante inválido", http.StatusBadRequest)
		return
	}

	var req reqOferta
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "cuerpo json inválido", http.StatusBadRequest)
		return
	}

	id, err := s.st.GuardarOferta(r.Context(), varianteID, req.ChannelAccountID, req.OfferPrice, req.StartsAt, req.EndsAt, usuarioDe(r))
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "create", "offers", strconv.FormatInt(id, 10), nil, req)

	escribir(w, http.StatusOK, map[string]any{
		"ok": true,
		"id": id,
	})
}
