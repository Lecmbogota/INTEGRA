package publicar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"
)

// Cuánto se contrasta con el canal y cada cuánto.
//
// El coste real es una petición por publicación en WooCommerce y Shopify —los
// otros dos consultan por lotes—, así que la conciliación se reparte en tandas
// y no vuelve a preguntar por la misma ficha hasta pasado el periodo. Con el
// catálogo de MDV, un día basta para recorrerlo entero sin morder el cupo que
// necesitan los envíos de stock, que son los urgentes.
const (
	tamConciliacion   = 200
	loteEstado        = 20
	periodoPorDefecto = 24 * time.Hour

	// A partir de este número, que TODO lo consultado salga muerto deja de ser
	// creíble como catálogo borrado y pasa a ser una lectura rota: un token
	// caducado, la cuenta equivocada, el canal devolviendo vacío. Recrear el
	// catálogo entero contra el canal por un fallo de lectura es mucho peor
	// que no recrear nada, así que por debajo de este umbral se actúa y por
	// encima se aborta. Es el mismo criterio que usa el sync con Odoo antes de
	// dar de baja el catálogo (internal/sync/catalogo.go).
	minimoParaSospechar = 5
)

// veredicto es lo que el canal dice de una publicación, ya traducido: los
// cuatro canales usan palabras distintas para las mismas tres situaciones.
type veredicto int

const (
	publicacionViva veredicto = iota
	// publicacionCaida: ya no existe. Se limpia la referencia para que el
	// motor la vuelva a crear.
	publicacionCaida
	// publicacionRetirada: existe pero no está a la venta, y lo decidió el
	// canal. No se toca sola.
	publicacionRetirada
)

// clasificar traduce el estado que devuelve FetchStatus.
//
// La cadena vacía es la ausencia: MercadoLibre y Falabella no devuelven fila
// para lo que ya no tienen, y WooCommerce y Shopify traducen su 404 a
// «eliminado» y «no_encontrado». Lo demás sale del vocabulario de cada canal:
// WooCommerce hereda los estados de WordPress (publish, draft, pending,
// private, trash), Shopify usa active/draft/archived, MercadoLibre
// active/paused/closed más subestados separados por «/», y Falabella
// active/inactive/deleted.
//
// Lo desconocido cae en retirada a propósito: una palabra que no reconocemos
// no puede disparar una escritura contra el canal, pero sí tiene que dejar de
// contar como publicada y salir en el aviso.
func clasificar(estado string) veredicto {
	estado = strings.ToLower(strings.TrimSpace(estado))
	base, sub, _ := strings.Cut(estado, "/")
	// El subestado de MercadoLibre es lo que distingue una publicación cerrada
	// que se puede reabrir de una que el vendedor borró.
	for _, s := range strings.Split(sub, ",") {
		if strings.TrimSpace(s) == "deleted" {
			return publicacionCaida
		}
	}
	switch base {
	case "", "eliminado", "no_encontrado", "deleted", "trash":
		return publicacionCaida
	case "active", "publish", "published":
		return publicacionViva
	default:
		return publicacionRetirada
	}
}

// pausar retira del canal una publicación cuyo producto dejó de ser mercancía.
//
// No pasa por el candidato porque justo lo que la trae aquí es haber dejado de
// serlo: se archivó en Odoo, se excluyó a mano o alguien le borró el SKU.
func (s *Servicio) pausar(ctx context.Context, t jobs.Trabajo) error {
	p, ref, ad, err := s.publicacion(ctx, t)
	if err != nil {
		return err
	}
	if err := ad.Pause(ctx, ref); err != nil {
		// Que ya no exista es un final aceptable para una pausa: lo que se
		// quería es que dejara de venderse, y eso ya se cumplió.
		if errors.Is(err, channel.ErrNoEncontrado) {
			return s.st.MarcarPublicacionCaida(ctx, p.CuentaID, p.VarianteID, "eliminado")
		}
		return err
	}
	s.log.Info("publicación pausada por salir del catálogo",
		"cuenta", p.CuentaID, "variante", p.VarianteID, "ref", ref.ListingID)
	// Vacío es «catálogo»: así se encolaba antes de que existiera la pausa a
	// mano, y una carga vieja que siga en la cola tiene que seguir valiendo.
	motivo := p.Motivo
	if motivo == "" {
		motivo = store.PausaCatalogo
	}
	return s.st.MarcarPublicacionPausada(ctx, p.CuentaID, p.VarianteID, motivo)
}

// reanudar vuelve a abrir lo que Integra había pausado.
//
// Solo llega aquí lo que pausó Integra: Planificar no encola la reanudación de
// lo que retiró el canal, porque reabrir una baja por infracción es lo que
// convierte un aviso en una sanción.
func (s *Servicio) reanudar(ctx context.Context, t jobs.Trabajo) error {
	p, ref, ad, err := s.publicacion(ctx, t)
	if err != nil {
		return err
	}
	if err := ad.Resume(ctx, ref); err != nil {
		// Si desapareció mientras estaba pausada, no hay nada que reabrir: se
		// marca caída y el motor la crea de nuevo en la siguiente pasada.
		if errors.Is(err, channel.ErrNoEncontrado) {
			return s.st.MarcarPublicacionCaida(ctx, p.CuentaID, p.VarianteID, "eliminado")
		}
		return err
	}
	return s.st.MarcarPublicacionReanudada(ctx, p.CuentaID, p.VarianteID)
}

// publicacion resuelve lo común de pausar y reanudar: la referencia externa y
// el adaptador de la cuenta.
func (s *Servicio) publicacion(ctx context.Context, t jobs.Trabajo) (PayloadPublicar, channel.ExternalRef, channel.Adapter, error) {
	var p PayloadPublicar
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return p, channel.ExternalRef{}, nil, fmt.Errorf("payload ilegible: %w", err)
	}
	ref, err := s.st.RefDePublicacion(ctx, p.CuentaID, p.VarianteID)
	if err != nil {
		return p, ref, nil, err
	}
	ad, err := s.adaptador(ctx, p.CuentaID)
	return p, ref, ad, err
}

// conciliar contrasta lo que Integra da por publicado contra lo que el canal
// tiene de verdad.
//
// Es lo único que descubre una publicación que el marketplace bajó por su
// cuenta o que alguien borró a mano en la tienda. Sin esto, Integra la sigue
// contando como publicada, cada actualización falla con 404 hasta agotar
// reintentos y el producto no vuelve al canal jamás.
func (s *Servicio) conciliar(ctx context.Context, t jobs.Trabajo) error {
	var p PayloadCuenta
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payload ilegible: %w", err)
	}
	vivas, err := s.st.PublicacionesPorConciliar(ctx, p.CuentaID, tamConciliacion, s.periodoConciliacion())
	if err != nil {
		return err
	}
	if len(vivas) == 0 {
		return nil
	}
	ad, err := s.adaptador(ctx, p.CuentaID)
	if err != nil {
		return err
	}

	// Las caídas se acumulan y no se aplican sobre la marcha: hay que ver la
	// tanda entera antes de decidir si son bajas de verdad o una lectura rota.
	var caidas []store.PublicacionViva
	for i := 0; i < len(vivas); i += loteEstado {
		lote := vivas[i:min(i+loteEstado, len(vivas))]
		refs := make([]channel.ExternalRef, len(lote))
		for j, v := range lote {
			refs[j] = v.Ref
		}
		estados, err := ad.FetchStatus(ctx, refs)
		// Con error no se interpreta nada: la ausencia de una referencia en la
		// respuesta es la señal de baja, y con la lectura a medias todas las
		// que faltan parecerían borradas.
		if err != nil {
			return fmt.Errorf("consultando el estado en %s: %w", ad.Kind(), err)
		}
		porRef := make(map[channel.ExternalRef]string, len(estados))
		for _, e := range estados {
			porRef[e.Ref] = e.Status
		}
		for _, v := range lote {
			estado := porRef[v.Ref] // ausente = cadena vacía = caída
			switch clasificar(estado) {
			case publicacionCaida:
				caidas = append(caidas, v)
			case publicacionRetirada:
				s.log.Warn("el canal retiró una publicación de la venta",
					"cuenta", p.CuentaID, "sku", v.SKU, "estado", estado)
				if err := s.st.MarcarPublicacionRetirada(ctx, p.CuentaID, v.VarianteID, estado); err != nil {
					return err
				}
			default:
				if err := s.st.MarcarPublicacionViva(ctx, p.CuentaID, v.VarianteID, estado); err != nil {
					return err
				}
			}
		}
	}

	if len(caidas) == len(vivas) && len(vivas) >= minimoParaSospechar {
		return fmt.Errorf("el canal no reconoce ninguna de las %d publicaciones de la cuenta %d: "+
			"se aborta antes de recrear el catálogo entero", len(vivas), p.CuentaID)
	}

	for _, v := range caidas {
		s.log.Warn("publicación desaparecida del canal",
			"cuenta", p.CuentaID, "sku", v.SKU, "ref", v.Ref.ListingID)
		if err := s.st.MarcarPublicacionCaida(ctx, p.CuentaID, v.VarianteID, "eliminado"); err != nil {
			return err
		}
		if !s.recrearCaidas || s.cola == nil {
			continue
		}
		// Se pide la publicación aquí y no se espera al siguiente horario
		// porque mientras tanto el producto no está a la venta en ningún
		// sitio. Publish es idempotente en los cuatro adaptadores: si la ficha
		// resultara existir, la adopta en vez de duplicar el SKU.
		if err := encolar(ctx, s.cola, TrabajoPublicar, p.CuentaID, v.VarianteID, 100); err != nil {
			return err
		}
	}
	return nil
}

func (s *Servicio) periodoConciliacion() time.Duration {
	if s.periodo <= 0 {
		return periodoPorDefecto
	}
	return s.periodo
}

// EncolarActivacion abre o apaga en bloque lo que una cuenta tiene publicado.
//
// Publicar deja la ficha en borrador a propósito: en WooCommerce y en Shopify,
// que la vea el público es decisión de una persona. Esto es donde se toma esa
// decisión, y sin ello había que entrar al canal producto por producto.
//
// Devuelve cuántas se encolaron. La clave única es por variante y cuenta, así
// que pulsar dos veces no manda dos avisos al canal.
func EncolarActivacion(ctx context.Context, cola encolador, st interface {
	PublicacionesDeCuenta(ctx context.Context, cuentaID int64, paraActivar bool) ([]store.PublicacionViva, error)
}, cuentaID int64, activar bool) (int, error) {
	pubs, err := st.PublicacionesDeCuenta(ctx, cuentaID, activar)
	if err != nil {
		return 0, err
	}
	kind := TrabajoPausar
	if activar {
		kind = TrabajoReanudar
	}
	for _, p := range pubs {
		// Prioridad alta: es una acción que alguien está mirando, y esperar
		// detrás de un catálogo entero haría creer que el botón no hizo nada.
		if _, err := cola.Encolar(ctx, kind,
			PayloadPublicar{CuentaID: cuentaID, VarianteID: p.VarianteID, Motivo: store.PausaManual},
			jobs.Opciones{
				UniqueKey: fmt.Sprintf("%s:%d:%d", kind, cuentaID, p.VarianteID),
				Priority:  10,
				CuentaID:  cuentaID,
			}); err != nil {
			return 0, err
		}
	}
	return len(pubs), nil
}
