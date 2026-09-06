package ordenes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// TrabajoDespachar confirma al canal que un pedido salió de bodega.
const TrabajoDespachar = "confirmar_despacho"

// AlertaDespachoFallido avisa de que el canal no se enteró del despacho.
const AlertaDespachoFallido = "despacho_no_confirmado"

// El despacho cierra el ciclo. Integra ya sabía vender y montar el pedido en
// Odoo, pero el marketplace se quedaba esperando: MercadoLibre y Falabella
// miden el tiempo hasta el despacho y, pasado el plazo, cancelan, reembolsan
// al comprador y bajan la reputación del vendedor, lo que además reduce la
// exposición de todas las publicaciones y retiene el pago.
//
// La señal de que salió es el albarán de salida validado en Odoo. La guía, en
// cambio, no sale de Odoo: el módulo de transporte (delivery) no está
// instalado en la instancia de MDV, así que ni carrier_tracking_ref ni
// carrier_id existen. Se lee si algún día existen, y mientras tanto la escribe
// el operador en la pantalla de Pedidos. Que falte no impide confirmar: en
// Mercado Envíos y en Falabella la logística la pone el canal y no hay guía
// que mandar, así que exigirla bloquearía justo los pedidos con menos margen.

type payloadDespacho struct {
	OrdenID int64 `json:"orden_id"`
}

// SalidaDeBodega es lo que Odoo sabe del albarán de un pedido.
type SalidaDeBodega struct {
	Validado bool
	Cuando   time.Time
	// Guia y Transportadora vienen vacías salvo que el módulo de transporte
	// esté instalado en Odoo.
	Guia           string
	Transportadora string
}

// EncolarDespacho pide confirmar al canal el despacho de un pedido.
//
// La clave única es por pedido, así que dos pasadas seguidas del planificador
// no producen dos avisos al canal.
func EncolarDespacho(ctx context.Context, cola encolador, cuentaID, ordenID int64) error {
	_, err := cola.Encolar(ctx, TrabajoDespachar, payloadDespacho{OrdenID: ordenID},
		jobs.Opciones{
			UniqueKey: fmt.Sprintf("%s:%d", TrabajoDespachar, ordenID),
			// Por delante de publicar: el reloj del plazo de envío del canal ya
			// está corriendo, y una ficha que tarda una hora más no cuesta nada.
			Priority: 40,
			CuentaID: cuentaID,
		})
	return err
}

// EncolarDespachos deja un trabajo por cada pedido montado en Odoo al que no
// se le ha confirmado el despacho. Lo usa la ruta de línea de comandos.
func (s *Servicio) EncolarDespachos(ctx context.Context) (int, error) {
	if s.cola == nil {
		return 0, nil
	}
	pendientes, err := s.st.OrdenesPorDespachar(ctx, 200)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range pendientes {
		if err := EncolarDespacho(ctx, s.cola, o.CuentaID, o.ID); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ConfirmarDespacho avisa al canal de que el pedido salió, si de verdad salió.
//
// Es idempotente: un pedido ya confirmado no vuelve a avisarse, y uno cuyo
// albarán todavía no está validado se deja para la siguiente pasada sin
// contarlo como fallo. Avisar de un despacho que no ocurrió es peor que
// tardar: el comprador recibe un aviso de envío que no existe.
func (s *Servicio) ConfirmarDespacho(ctx context.Context, ordenID int64) error {
	o, err := s.st.OrdenPorDespacharID(ctx, ordenID)
	if err != nil {
		return err
	}
	if o.OdooPedidoID == 0 {
		return fmt.Errorf("el pedido %d todavía no está montado en Odoo", ordenID)
	}

	salida, err := s.salidaDeBodega(ctx, o.OdooPedidoID)
	if err != nil {
		return err
	}
	if !salida.Validado {
		// Todavía no ha salido de bodega. No es un error: es el estado normal
		// de un pedido reciente, y marcarlo como fallo llenaría la pantalla de
		// rojo por pedidos que van bien.
		s.log.Debug("el pedido aún no tiene albarán validado en Odoo",
			"orden", ordenID, "sale_order", o.OdooPedidoID)
		return nil
	}
	if err := s.st.MarcarSalidaDeBodega(ctx, ordenID, salida.Cuando); err != nil {
		return err
	}

	// Lo que escribió el operador manda sobre lo que traiga Odoo: si alguien
	// se molestó en teclear la guía, es porque la de Odoo faltaba o estaba mal.
	guia := primeroNoVacio(o.Guia, salida.Guia)
	transportadora := primeroNoVacio(o.Transportadora, salida.Transportadora)

	ad, err := s.adaptador(ctx, o.CuentaID)
	if err != nil {
		return err
	}
	ref := channel.ExternalRef{ListingID: o.ExternalID}
	err = ad.AckOrder(ctx, ref, channel.Fulfillment{
		TrackingNumber: guia,
		Carrier:        transportadora,
		ShippedAt:      salida.Cuando,
	})
	if err != nil {
		// Un «no procede» no es un fallo: el canal dice que ese pedido no
		// admite esta confirmación (un envío gestionado por él, uno ya
		// despachado, uno digital). Se sella igual para no reintentarlo
		// eternamente ni llenar la pantalla de errores que nadie puede
		// arreglar.
		if !channel.EsReintentable(err) {
			s.log.Info("el canal no admite confirmar este despacho; se da por cerrado",
				"orden", ordenID, "canal", o.Canal, "motivo", err)
			return s.st.MarcarDespachoConfirmado(ctx, ordenID, guia, transportadora)
		}
		_ = s.st.AnotarFalloDespacho(ctx, ordenID, err.Error())
		// El aviso solo a partir del tercer intento: los dos primeros suelen
		// ser un corte de red, y avisar de todos entrena a ignorar el correo.
		if o.Intentos+1 >= 3 {
			cuenta := o.CuentaID
			_ = s.st.CrearAlerta(ctx, AlertaDespachoFallido, "error", &cuenta,
				fmt.Sprintf("No se pudo confirmar a %s el despacho del pedido %s: %v. "+
					"El canal sigue contando el tiempo de envío.", o.Canal, numeroODs(o), err),
				map[string]any{"orden_id": ordenID, "intentos": o.Intentos + 1})
		}
		return err
	}

	if err := s.st.MarcarDespachoConfirmado(ctx, ordenID, guia, transportadora); err != nil {
		return err
	}
	s.log.Info("despacho confirmado al canal",
		"orden", ordenID, "canal", o.Canal, "pedido", numeroODs(o), "guia", guia != "")
	return nil
}

// salidaDeBodega pregunta a Odoo si el albarán de salida del pedido está
// validado.
//
// Se mira el albarán de salida (picking_type_code = 'outgoing') y no el estado
// del sale.order: un pedido confirmado no es un pedido despachado, y avisar al
// canal en la confirmación haría que el comprador reciba un aviso de envío por
// mercancía que sigue en bodega.
func (s *Servicio) salidaDeBodega(ctx context.Context, saleOrderID int64) (SalidaDeBodega, error) {
	cli, err := s.abrirOdoo(ctx)
	if err != nil {
		return SalidaDeBodega{}, err
	}
	cli = cli.ConContexto(ctx)

	campos := []string{"state", "date_done"}
	// El módulo de transporte de Odoo (delivery) no está instalado en MDV, así
	// que estos dos campos no existen y pedirlos daría error. Se piden solo si
	// el modelo los declara, para que el día que se instale la guía empiece a
	// llegar sola sin tocar el código.
	if s.pickingTraeGuia(cli) {
		campos = append(campos, "carrier_tracking_ref", "carrier_id")
	}

	albaranes, err := cli.SearchRead("stock.picking",
		[]interface{}{
			[]interface{}{"sale_id", "=", saleOrderID},
			[]interface{}{"picking_type_code", "=", "outgoing"},
		},
		campos,
		map[string]interface{}{"limit": 20, "order": "date_done desc, id desc"})
	if err != nil {
		return SalidaDeBodega{}, fmt.Errorf("leyendo el albarán del pedido %d: %w", saleOrderID, err)
	}

	var out SalidaDeBodega
	for _, a := range albaranes {
		if a.Str("state") != "done" {
			continue
		}
		out.Validado = true
		if t, ok := a.Time("date_done"); ok {
			out.Cuando = t
		}
		if g := strings.TrimSpace(a.Str("carrier_tracking_ref")); g != "" {
			out.Guia = g
		}
		if c := strings.TrimSpace(a.RefName("carrier_id")); c != "" {
			out.Transportadora = c
		}
		break
	}
	if out.Validado && out.Cuando.IsZero() {
		out.Cuando = time.Now()
	}
	return out, nil
}

// pickingTraeGuia comprueba una sola vez si Odoo declara los campos de
// transporte. Sin el módulo delivery instalado no existen.
func (s *Servicio) pickingTraeGuia(cli *odoo.Client) bool {
	s.guiaMu.Lock()
	defer s.guiaMu.Unlock()
	if s.guiaSabida {
		return s.guiaHay
	}
	campos, err := cli.FieldsGet("stock.picking", []string{"type"})
	if err != nil {
		// Ante la duda, no se piden: un campo inexistente rompe la lectura
		// entera y con ella el despacho, que es lo que importa.
		return false
	}
	_, hay := campos["carrier_tracking_ref"]
	s.guiaSabida, s.guiaHay = true, hay
	return hay
}

func primeroNoVacio(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return strings.TrimSpace(a)
	}
	return strings.TrimSpace(b)
}

// numeroODs devuelve el número que el vendedor reconoce, con el interno como
// respaldo: en un correo de alerta, «12345678» no le dice nada a nadie.
func numeroODs(o *store.OrdenPorDespachar) string {
	if o.Numero != "" {
		return o.Numero
	}
	return o.ExternalID
}

// despachar es el manejador del trabajo.
func (s *Servicio) despachar(ctx context.Context, t jobs.Trabajo) error {
	var p payloadDespacho
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payload de despacho ilegible: %w", err)
	}
	return s.ConfirmarDespacho(ctx, p.OrdenID)
}
