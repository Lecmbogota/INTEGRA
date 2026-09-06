package publicar

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/jobs"
)

// Verificación diferida de las escrituras asíncronas.
//
// Falabella —el único canal con AsyncFeeds— responde 200 a una escritura
// diciendo únicamente «recibí el feed». El veredicto real llega minutos
// después, y cuando el Seller Center tiene cola son muchos minutos. El
// adaptador sondea unos segundos por si acaso, pero esperar dentro de la
// llamada no es una opción: bloquearía un hueco del worker durante más de lo
// que dura su lease.
//
// Sin este trabajo, todo feed lento se daba por bueno: el motor sellaba el
// hash de contenido, de precio o de stock, y un rechazo posterior (atributo
// obligatorio ausente, EAN duplicado) dejaba el producto congelado para
// siempre —nunca volvía a encolarse porque su hash ya coincidía— y sin una
// sola alerta. Aquí se sella el hash solo cuando el canal confirma, y si
// rechaza se anota en la publicación, que es de donde salen las alertas.
const TrabajoVerificarFeed = "verificar_feed"

// Qué se estaba enviando cuando el canal dejó el feed pendiente. Decide qué
// hay que sellar si el feed termina bien.
const (
	QuePublicacion = "publicacion" // alta: ficha, precio y stock en un envío
	QueContenido   = "contenido"   // solo la ficha (Update)
	QuePrecio      = "precio"
	QueStock       = "stock"
)

// estadoFeedPendiente es lo que se anota en la publicación mientras el canal
// no dice nada. No es un estado del canal: es la ausencia de estado.
const estadoFeedPendiente = "pendiente"

const (
	// esperaPrimeraConsulta retrasa la primera pregunta: recién enviado, el
	// feed casi seguro sigue en cola y consultarlo solo gasta cupo.
	esperaPrimeraConsulta = 30 * time.Second
	// intentosVerificacion, con el backoff de la cola (30 s, 1 m, 2 m, 4 m…),
	// da algo más de una hora de margen antes de dejar de esperar. Un feed que
	// no ha resuelto en una hora es un feed atascado, y seguir preguntando no
	// lo desatasca: lo que sirve es que el envío se vuelva a encolar.
	intentosVerificacion = 8
)

// PayloadVerificarFeed lleva lo que hay que sellar si el canal confirma.
//
// Los hashes viajan dentro del trabajo, calculados en el momento del envío, y
// no se recalculan al verificar: entre el envío y el veredicto pueden pasar
// veinte minutos, y en ese rato alguien puede haber cambiado el precio en
// Odoo. Sellar el hash de ahora daría por publicado algo que nunca salió.
type PayloadVerificarFeed struct {
	CuentaID   int64 `json:"cuenta_id"`
	VarianteID int64 `json:"variante_id"`
	ProductoID int64 `json:"producto_id"`
	// SKU es con el que se envió: es como el canal nombra los rechazos.
	SKU string `json:"sku"`
	// Feeds son las escrituras que quedaron sin veredicto. Publicar manda dos
	// (la ficha y las imágenes) y las dos tienen que terminar bien.
	Feeds []string `json:"feeds"`
	Que   string   `json:"que"`

	ExternalID      string  `json:"external_id,omitempty"`
	ExternalURL     string  `json:"external_url,omitempty"`
	VarianteExterna string  `json:"variante_externa,omitempty"`
	ContentHash     string  `json:"content_hash,omitempty"`
	PriceHash       string  `json:"price_hash,omitempty"`
	StockHash       string  `json:"stock_hash,omitempty"`
	Precio          float64 `json:"precio,omitempty"`
	Cantidad        int     `json:"cantidad,omitempty"`
}

// encolarVerificacion deja pedido el veredicto y anota el feed en la
// publicación, que es lo único que puede explicar después por qué un producto
// no aparece en el canal.
func (s *Servicio) encolarVerificacion(ctx context.Context, p PayloadVerificarFeed) error {
	if len(p.Feeds) == 0 {
		return nil
	}
	if s.cola == nil {
		// Igual que en encolarPrecioYStock: sin cola, el envío se quedaría sin
		// confirmar en silencio, que es justo el defecto que se corrige.
		return fmt.Errorf("el servicio no tiene cola: el feed %s de la variante %d se quedaría sin verificar",
			p.Feeds[0], p.VarianteID)
	}
	if err := s.st.AnotarFeed(ctx, p.CuentaID, p.ProductoID, p.Feeds[0], estadoFeedPendiente); err != nil {
		return err
	}
	_, err := s.cola.Encolar(ctx, TrabajoVerificarFeed, p, jobs.Opciones{
		// El feed forma parte de la clave: dos envíos distintos de la misma
		// variante son dos veredictos distintos y hay que esperar los dos.
		UniqueKey:   fmt.Sprintf("%s:%d:%d:%s:%s", TrabajoVerificarFeed, p.CuentaID, p.VarianteID, p.Que, p.Feeds[0]),
		RunAt:       time.Now().Add(esperaPrimeraConsulta),
		Priority:    40,
		MaxAttempts: intentosVerificacion,
		CuentaID:    p.CuentaID,
	})
	return err
}

// verificarFeed pregunta por una escritura que el canal aceptó sin resolver.
//
// Tres desenlaces: confirmada (se sella lo que se envió), rechazada (se anota
// en la publicación y no se sella nada, así que la próxima planificación lo
// vuelve a encolar con el motivo a la vista) o todavía sin veredicto (se falla
// para que la cola lo reintente con su backoff).
func (s *Servicio) verificarFeed(ctx context.Context, t jobs.Trabajo) error {
	var p PayloadVerificarFeed
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payload ilegible: %w", err)
	}
	ad, err := s.adaptador(ctx, p.CuentaID)
	if err != nil {
		return err
	}
	// El núcleo no pregunta «¿eres Falabella?»: pregunta si el canal escribe
	// por feeds y si sabe responder por ellos.
	cf, ok := ad.(channel.ConFeeds)
	if !ok || !ad.Capabilities().AsyncFeeds {
		return fmt.Errorf("el canal %s de la cuenta %d no sabe responder por el feed %v",
			ad.Kind(), p.CuentaID, p.Feeds)
	}

	for _, feedID := range p.Feeds {
		v, err := cf.VeredictoDeFeed(ctx, feedID, p.SKU)
		if err != nil {
			return err
		}
		if err := s.st.AnotarFeed(ctx, p.CuentaID, p.ProductoID, feedID, v.Estado); err != nil {
			return err
		}

		if v.Rechazo != "" {
			// Rechazo es definitivo: reintentar el mismo feed no lo cambia. Se
			// anota en la publicación —de ahí salen las alertas— y el trabajo
			// termina sin sellar nada.
			causa := fmt.Sprintf("el feed %s rechazó %s: %s", feedID, p.SKU, v.Rechazo)
			if err := s.st.AnotarErrorPublicacion(ctx, p.CuentaID, p.ProductoID, causa); err != nil {
				return err
			}
			s.log.Warn("escritura asíncrona rechazada por el canal",
				"sku", p.SKU, "feed", feedID, "que", p.Que, "motivo", v.Rechazo)
			return nil
		}

		if !v.Terminado {
			causa := fmt.Sprintf("el feed %s de %s sigue sin veredicto (estado %q)", feedID, p.SKU, v.Estado)
			if t.Intentos >= t.MaxIntentos {
				// Último intento: se deja constancia en la publicación para
				// que salga en las alertas. Un trabajo agotado en la cola no
				// dice qué producto se quedó a medias.
				_ = s.st.AnotarErrorPublicacion(ctx, p.CuentaID, p.ProductoID,
					causa+"; se agotó la espera y el envío se repetirá en la próxima planificación")
			}
			return fmt.Errorf("%s", causa)
		}
	}

	return s.sellar(ctx, p)
}

// sellar anota como publicado exactamente lo que se envió, ahora que el canal
// ha confirmado que lo aplicó.
func (s *Servicio) sellar(ctx context.Context, p PayloadVerificarFeed) error {
	switch p.Que {
	case QuePublicacion:
		return s.st.GuardarPublicacion(ctx, p.CuentaID, p.ProductoID, p.VarianteID,
			p.ExternalID, p.ExternalURL, p.VarianteExterna,
			p.ContentHash, p.PriceHash, p.StockHash, p.Precio, p.Cantidad)
	case QueContenido:
		return s.st.GuardarContenidoPublicado(ctx, p.CuentaID, p.ProductoID, p.VarianteID,
			p.ExternalID, p.ExternalURL, p.VarianteExterna, p.ContentHash)
	case QuePrecio:
		return s.st.GuardarPrecioPublicado(ctx, p.CuentaID, p.VarianteID, p.PriceHash, p.Precio)
	case QueStock:
		return s.st.GuardarStockPublicado(ctx, p.CuentaID, p.VarianteID, p.StockHash, p.Cantidad)
	}
	return fmt.Errorf("no sé qué sellar del feed %v: %q", p.Feeds, p.Que)
}
