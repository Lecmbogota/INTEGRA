package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mdv/integra/internal/store"
	"github.com/mdv/integra/internal/webimagenes"
)

// BusquedaMasiva es el estado del barrido de imágenes, tal como lo consume la
// interfaz. Vive en memoria del proceso: si el servidor se reinicia a mitad,
// el barrido se corta y se puede relanzar (los productos ya cubiertos quedan
// fuera del siguiente porque ya tienen foto).
type BusquedaMasiva struct {
	EnCurso        bool   `json:"en_curso"`
	Total          int    `json:"total"`
	Procesados     int    `json:"procesados"`
	ConFotoNueva   int    `json:"productos_con_foto"`
	FotosAgregadas int    `json:"fotos_agregadas"`
	Fallos         int    `json:"fallos"`
	Ultimo         string `json:"ultimo"`
	Mensaje        string `json:"mensaje"`
}

func (s *Server) registrarBusquedaMasiva(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/imagenes/buscar-masivo", s.lanzarBusquedaMasiva)
	mux.HandleFunc("GET /api/imagenes/buscar-masivo", s.estadoBusquedaMasiva)
}

func (s *Server) estadoBusquedaMasiva(w http.ResponseWriter, r *http.Request) {
	s.masivoMu.Lock()
	estado := s.masivo
	s.masivoMu.Unlock()
	escribir(w, http.StatusOK, estado)
}

// lanzarBusquedaMasiva recorre todos los productos publicables sin foto y les
// busca imágenes por SKU, en segundo plano.
//
// Va en serie y con pausa entre productos a propósito: el buscador es un
// servicio ajeno y gratuito, y un barrido agresivo acabaría bloqueado. Si la
// búsqueda falla varias veces seguidas se aborta con mensaje claro en vez de
// insistir contra un bloqueo.
func (s *Server) lanzarBusquedaMasiva(w http.ResponseWriter, r *http.Request) {
	if s.almacen == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "el banco de imágenes no está configurado"})
		return
	}

	var cuerpo struct {
		MaxPorProducto int  `json:"max_por_producto"`
		Limite         int  `json:"limite"`        // 0 = todos
		SoloSinFoto    bool `json:"solo_sin_foto"` // false = sin imagen apta para los 4 canales
	}
	_ = json.NewDecoder(r.Body).Decode(&cuerpo)
	if cuerpo.MaxPorProducto <= 0 {
		cuerpo.MaxPorProducto = 3
	}
	if cuerpo.MaxPorProducto > 5 {
		cuerpo.MaxPorProducto = 5
	}

	objetivos, err := s.st.ObjetivosBusquedaImagenes(r.Context(), cuerpo.SoloSinFoto)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if cuerpo.Limite > 0 && len(objetivos) > cuerpo.Limite {
		objetivos = objetivos[:cuerpo.Limite]
	}
	if len(objetivos) == 0 {
		escribir(w, http.StatusOK, map[string]string{
			"estado": "nada que hacer: todos los productos publicables con SKU ya tienen foto"})
		return
	}

	s.masivoMu.Lock()
	if s.masivo.EnCurso {
		s.masivoMu.Unlock()
		escribir(w, http.StatusConflict,
			map[string]string{"error": "ya hay una búsqueda masiva en curso"})
		return
	}
	s.masivo = BusquedaMasiva{EnCurso: true, Total: len(objetivos)}
	s.masivoMu.Unlock()

	// Contexto propio: cerrar el navegador no debe abortar el barrido. El tope
	// de 4 horas es un cinturón de seguridad, no una expectativa.
	go s.correrBusquedaMasiva(objetivos, cuerpo.MaxPorProducto)

	escribir(w, http.StatusAccepted, map[string]any{
		"estado": "búsqueda masiva lanzada", "productos": len(objetivos),
	})
}

func (s *Server) correrBusquedaMasiva(objetivos []store.ObjetivoBusqueda, maxPorProducto int) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	buscador := webimagenes.NuevoBuscador()
	fallosSeguidos := 0

	fin := func(msg string) {
		s.masivoMu.Lock()
		s.masivo.EnCurso = false
		s.masivo.Mensaje = msg
		s.masivoMu.Unlock()
		s.log.Info("búsqueda masiva de imágenes terminada", "resultado", msg)
	}

	for i, o := range objetivos {
		if ctx.Err() != nil {
			fin(fmt.Sprintf("cortada por tiempo tras %d de %d productos", i, len(objetivos)))
			return
		}

		consulta := strings.TrimSpace(o.SKU + " " + o.Marca)
		candidatas, err := buscador.Buscar(ctx, consulta, 600, maxPorProducto*4)
		if err != nil {
			fallosSeguidos++
			s.masivoMu.Lock()
			s.masivo.Fallos++
			s.masivo.Procesados = i + 1
			s.masivo.Ultimo = o.SKU
			s.masivoMu.Unlock()
			if fallosSeguidos >= 5 {
				fin(fmt.Sprintf("abortada en %d/%d: la búsqueda falla repetidamente (posible bloqueo del buscador); reintenta en un rato",
					i+1, len(objetivos)))
				return
			}
			continue
		}
		fallosSeguidos = 0

		// Los productos objetivo pueden tener ya fotos (pequeñas o WebP): se
		// cargan sus hashes para no volver a asociar exactamente la misma.
		yaTenia := map[string]bool{}
		if _, _, _, shas, err := s.st.DatosBusquedaImagen(ctx, o.VarianteID); err == nil {
			for _, sha := range shas {
				yaTenia[sha] = true
			}
		}

		agregadas, _, err := s.ingerirCandidatas(ctx, buscador, o.ProductoID, candidatas, maxPorProducto, yaTenia)
		if err != nil {
			// Un error de base de datos sí aborta: seguir escribiría a ciegas.
			fin(fmt.Sprintf("abortada en %d/%d por error interno: %v", i+1, len(objetivos), err))
			return
		}

		s.masivoMu.Lock()
		s.masivo.Procesados = i + 1
		s.masivo.Ultimo = o.SKU
		if len(agregadas) > 0 {
			s.masivo.ConFotoNueva++
			s.masivo.FotosAgregadas += len(agregadas)
		}
		s.masivoMu.Unlock()

		// Ritmo suave entre productos.
		select {
		case <-ctx.Done():
		case <-time.After(400 * time.Millisecond):
		}
	}

	s.masivoMu.Lock()
	m := s.masivo
	s.masivoMu.Unlock()
	fin(fmt.Sprintf("lista: %d de %d productos consiguieron foto (%d fotos, %d búsquedas fallidas)",
		m.ConFotoNueva, m.Total, m.FotosAgregadas, m.Fallos))
}
