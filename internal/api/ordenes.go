package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdv/integra/internal/ordenes"
	"github.com/mdv/integra/internal/store"
)

// Reintento a mano de un pedido que no llegó a Odoo.
//
// Cada fallo del montaje suma un intento y a los cinco el pedido desaparece
// de las consultas de pendientes. Hasta ahora eso era definitivo: la alerta
// seguía sonando y no había ningún sitio donde pulsar. Este es el sitio.

// reintentadorOrdenes es lo que necesita reintentarOrden del store. Interfaz
// mínima para poder probar sin base de datos qué se encola y qué se rechaza.
type reintentadorOrdenes interface {
	ReintentarOrden(context.Context, int64) (store.Reintento, error)
}

// respuestaReintento es lo que decide reintentarOrden: el código y el cuerpo
// de la respuesta y, si el pedido volvió a la cola, con qué contaba antes.
type respuestaReintento struct {
	codigo int
	cuerpo map[string]any
	antes  *store.Reintento // nil si no se reactivó
}

func (s *Server) reintentarOrden(w http.ResponseWriter, r *http.Request) {
	if s.cola == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "la cola de trabajos no está disponible"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}

	res, err := reintentarOrden(r.Context(), s.st,
		func(ctx context.Context, cuentaID, ordenID int64) error {
			return ordenes.EncolarMontaje(ctx, s.cola, cuentaID, ordenID)
		}, id)
	if err != nil {
		s.fallo(w, err)
		return
	}

	if res.antes != nil {
		// Queda quién devolvió el pedido a la cola y qué se deshizo: el mismo
		// pedido reintentado una y otra vez es la señal de que lo que falla no
		// es pasajero.
		var usuario *int64
		if c := ClaimsDeContext(r.Context()); c != nil {
			usuario = &c.UserID
		}
		_ = s.st.RegistrarAuditoria(r.Context(), usuario, "retry", "channel_orders",
			strconv.FormatInt(id, 10),
			map[string]any{"intentos": res.antes.IntentosPrevios, "error": res.antes.ErrorPrevio},
			map[string]any{"intentos": 0, "estado": "received"}, r.RemoteAddr)
		s.log.Info("pedido devuelto a mano a la cola de montaje", "orden_id", id,
			"canal", res.antes.Canal, "numero", res.antes.Numero,
			"intentos_previos", res.antes.IntentosPrevios)
	}
	escribir(w, res.codigo, res.cuerpo)
}

// reintentarOrden decide qué se responde y encola el montaje solo cuando el
// pedido volvió de verdad a la cola.
//
// El orden importa: primero se reinicia el contador y después se encola. Al
// revés, el worker podría reclamar el trabajo antes del reinicio, encontrar el
// pedido todavía agotado y completarlo sin hacer nada.
func reintentarOrden(ctx context.Context, st reintentadorOrdenes,
	encolar func(ctx context.Context, cuentaID, ordenID int64) error, id int64) (respuestaReintento, error) {

	r, err := st.ReintentarOrden(ctx, id)
	switch {
	case errors.Is(err, store.ErrOrdenNoExiste):
		return respuestaReintento{codigo: http.StatusNotFound,
			cuerpo: map[string]any{"error": err.Error()}}, nil
	case errors.Is(err, store.ErrOrdenYaEnOdoo), errors.Is(err, store.ErrOrdenCancelada):
		// Estos sí le sirven a quien pulsa: dicen por qué no hay nada que
		// reintentar.
		return respuestaReintento{codigo: http.StatusConflict,
			cuerpo: map[string]any{"error": err.Error()}}, nil
	case err != nil:
		return respuestaReintento{}, err
	}

	if len(r.SinMapear) > 0 {
		return respuestaReintento{codigo: http.StatusConflict, cuerpo: map[string]any{
			"error": fmt.Sprintf("el pedido %s sigue con SKU que no existen en el catálogo (%s): "+
				"sincroniza el catálogo o corrige el SKU antes de reintentar",
				r.Numero, strings.Join(r.SinMapear, ", ")),
			"sin_mapear": r.SinMapear,
		}}, nil
	}

	if err := encolar(ctx, r.CuentaID, id); err != nil {
		// El contador ya está a cero y el pedido en 'received': aunque esto
		// falle, la red de seguridad del planificador lo encola en su siguiente
		// pasada. Se devuelve el error igual, porque es lo que pasó.
		return respuestaReintento{}, fmt.Errorf("encolando el montaje del pedido %d: %w", id, err)
	}
	return respuestaReintento{
		codigo: http.StatusAccepted,
		cuerpo: map[string]any{"estado": "reintento encolado", "numero": r.Numero},
		antes:  &r,
	}, nil
}
