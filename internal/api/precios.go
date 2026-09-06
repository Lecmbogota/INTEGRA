package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/mdv/integra/internal/store"
)

func (s *Server) registrarPrecios(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/cuentas/{id}/reglas-precio", s.reglasPrecioCuenta)
	mux.HandleFunc("POST /api/cuentas/{id}/reglas-precio", s.guardarReglaPrecio)
	mux.HandleFunc("POST /api/cuentas/{id}/recalcular-precios", s.recalcularPreciosCuenta)
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
	if err := s.st.CancelarOferta(r.Context(), id); err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
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

	id, err := s.st.GuardarReglaPrecioCanal(r.Context(), req)
	if err != nil {
		s.fallo(w, err)
		return
	}

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

	if err := s.st.GuardarOverridePrecio(r.Context(), varianteID, req.ChannelAccountID, req.Price, req.Reason, nil); err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{"ok": true})
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

	if err := s.st.EliminarOverridePrecio(r.Context(), varianteID, cuentaID); err != nil {
		s.fallo(w, err)
		return
	}

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

	id, err := s.st.GuardarOferta(r.Context(), varianteID, req.ChannelAccountID, req.OfferPrice, req.StartsAt, req.EndsAt, nil)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{
		"ok": true,
		"id": id,
	})
}
