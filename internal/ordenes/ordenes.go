// Package ordenes trae los pedidos de los canales y los monta como pedidos
// de venta en Odoo.
//
// Es la mitad del ciclo que faltaba: hasta ahora Integra publicaba productos
// pero nunca se enteraba de que se vendían. El flujo es
// canal → channel_orders → sale.order de Odoo, y cada paso es idempotente
// porque los canales reenvían y los reintentos existen.
package ordenes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
)

// Tipos de trabajo.
const (
	TrabajoIngerir = "ingerir_ordenes"
	TrabajoAOdoo   = "orden_a_odoo"
)

// AlertaPedidoCancelado avisa de un pedido que el canal canceló después de
// haberlo ingerido. Integra no cancela el sale.order de Odoo: la decisión
// contable es de una persona, así que lo único automático es enterarse.
const AlertaPedidoCancelado = "pedido_cancelado"

// AlertaDescuadrePedido avisa de que el sale.order recién creado no cuadra con
// lo que cobró el canal: el total o la moneda que devuelve Odoo no son los que
// Integra mandó. Integra no lo corrige —tocar importes o moneda de un
// documento contable es decisión de una persona—, solo deja constancia antes
// de que alguien confirme el borrador.
const AlertaDescuadrePedido = "pedido_descuadrado"

type payloadIngerir struct {
	CuentaID int64 `json:"cuenta_id"`
}

type payloadOdoo struct {
	OrdenID int64 `json:"orden_id"`
}

// almacen es lo que este paquete necesita de la capa de persistencia.
//
// Se declara del lado que la consume —igual que en internal/sync— para poder
// probar el montaje del pedido con un doble en memoria. Sin esto, comprobar
// qué se escribe en Odoo exigiría un PostgreSQL con catálogo cargado, y por
// eso los campos que se perdían por el camino no los vigilaba ninguna prueba.
type almacen interface {
	WatermarkOrdenes(ctx context.Context, cuentaID int64) (time.Time, error)
	ActualizarWatermarkOrdenes(ctx context.Context, cuentaID int64, hasta time.Time, errMsg string) error
	GuardarOrden(ctx context.Context, d store.DatosOrden) (int64, bool, error)
	DescontarStockPublicado(ctx context.Context, ordenID int64) (float64, error)
	DevolverStockReservado(ctx context.Context, ordenID int64, motivo string) (float64, error)
	DestinosDeStockDeOrden(ctx context.Context, ordenID int64) ([]store.DestinoStock, error)
	MarcarOrdenCancelada(ctx context.Context, ordenID int64, estadoCanal string) (store.Cancelacion, error)
	CrearAlerta(ctx context.Context, tipo, severidad string, cuentaID *int64, mensaje string, detalle any) error
	OrdenPendientePorID(ctx context.Context, id int64) (*store.Orden, error)
	MarcarOrdenCreada(ctx context.Context, ordenID, odooPedidoID, odooPartnerID int64) error
	MarcarOrdenFallida(ctx context.Context, ordenID int64, causa string) error
	BodegaDeOrden(ctx context.Context, ordenID int64) (*store.BodegaCuenta, error)
	OdooProductIDDeVariante(ctx context.Context, varianteID int64) (int64, error)
	DatosCompradorDeOrden(ctx context.Context, ordenID int64) (*store.DatosComprador, error)
	DireccionEnvioDeOrden(ctx context.Context, ordenID int64) (DireccionEnvio, error)
}

// encolador es lo que este paquete necesita de la cola. Es una interfaz, como
// en internal/publicar, para poder comprobar en las pruebas qué trabajos deja
// pedidos una ingesta sin un PostgreSQL detrás; *jobs.Cola la cumple.
type encolador interface {
	Encolar(ctx context.Context, kind string, payload any, op jobs.Opciones) (int64, error)
}

// Servicio ingiere pedidos y los monta en Odoo.
type Servicio struct {
	st  almacen
	log *slog.Logger
	// abrirOdoo se inyecta para no duplicar aquí el descifrado de la conexión.
	abrirOdoo func(context.Context) (*odoo.Client, error)
	// adaptador resuelve el canal de una cuenta. Es un campo, y no una
	// llamada directa, para que las pruebas de la ingesta puedan inyectar un
	// canal simulado; queda nulo en las del montaje, que no hablan con nadie.
	adaptador func(ctx context.Context, cuentaID int64) (channel.Adapter, error)
	// cola encola el montaje en Odoo de cada pedido ingerido y el envío de
	// stock a las demás cuentas. La rellena Registrar con la cola del worker;
	// en la ruta de línea de comandos (`integra ordenes`) queda nula y el
	// montaje se hace en el acto.
	cola encolador
}

func NuevoServicio(st *store.Store, cif *crypto.Cifrador, log *slog.Logger,
	abrirOdoo func(context.Context) (*odoo.Client, error)) *Servicio {
	return &Servicio{
		st: tienda{st}, log: log, abrirOdoo: abrirOdoo,
		// El adaptador se construye por trabajo y no se cachea: así una
		// credencial reemplazada surte efecto en la siguiente ingesta sin
		// reiniciar el worker.
		adaptador: func(ctx context.Context, cuentaID int64) (channel.Adapter, error) {
			return conectores.AdaptadorDeCuenta(ctx, st, cif, cuentaID)
		},
	}
}

// nuevoCon inyecta la persistencia. Lo usan las pruebas, que sustituyen la
// base por un doble en memoria y Odoo por un servidor XML-RPC simulado.
func nuevoCon(st almacen, log *slog.Logger,
	abrirOdoo func(context.Context) (*odoo.Client, error)) *Servicio {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Servicio{st: st, log: log, abrirOdoo: abrirOdoo}
}

func (s *Servicio) Registrar(w *jobs.Worker) {
	s.cola = w.Cola()
	w.Registrar(TrabajoIngerir, s.ingerir)
	w.Registrar(TrabajoAOdoo, s.montarEnOdoo)
}

// EncolarMontaje pide montar en Odoo un pedido ya ingerido.
//
// Sin esto la ingesta dejaba los pedidos en 'received' para siempre: el
// manejador de orden_a_odoo estaba registrado pero nadie lo encolaba, así que
// el pedido solo llegaba a Odoo si alguien ejecutaba `integra ordenes` a mano.
// La clave única evita que dos pasadas encolen dos veces el mismo pedido.
func EncolarMontaje(ctx context.Context, cola encolador, cuentaID, ordenID int64) error {
	_, err := cola.Encolar(ctx, TrabajoAOdoo, payloadOdoo{OrdenID: ordenID},
		jobs.Opciones{
			UniqueKey: fmt.Sprintf("%s:%d", TrabajoAOdoo, ordenID),
			Priority:  5, // igual que la ingesta: un pedido sin montar cuesta dinero
			CuentaID:  cuentaID,
		})
	return err
}

// EncolarIngesta pide traer los pedidos nuevos de una cuenta.
func EncolarIngesta(ctx context.Context, cola encolador, cuentaID int64) error {
	_, err := cola.Encolar(ctx, TrabajoIngerir, payloadIngerir{CuentaID: cuentaID},
		jobs.Opciones{
			UniqueKey: fmt.Sprintf("%s:%d", TrabajoIngerir, cuentaID),
			Priority:  5, // por delante de todo: un pedido sin atender cuesta dinero
			CuentaID:  cuentaID,
		})
	return err
}

// ingerir trae del canal los pedidos posteriores a la marca de agua.
func (s *Servicio) ingerir(ctx context.Context, t jobs.Trabajo) error {
	var p payloadIngerir
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payload ilegible: %w", err)
	}

	ad, err := s.adaptador(ctx, p.CuentaID)
	if err != nil {
		return err
	}
	desde, err := s.st.WatermarkOrdenes(ctx, p.CuentaID)
	if err != nil {
		return err
	}
	// Sin marca previa se pide una ventana corta: importar el histórico
	// completo de una tienda viva crearía cientos de pedidos viejos en Odoo.
	if desde.IsZero() {
		desde = time.Now().AddDate(0, 0, -7)
	}

	// Se recorren todas las páginas: un canal con más pedidos pendientes que
	// una página (50 en MercadoLibre) dejaría los demás sin traer hasta la
	// siguiente ronda, y la marca de agua ya habría avanzado por encima de
	// ellos. El tope evita que un canal que nunca diga "fin" nos cuelgue.
	const maxPaginas = 40
	var pedidos []channel.Order
	cur := channel.Cursor{}
	for i := 0; i < maxPaginas; i++ {
		pagina, err := ad.FetchOrders(ctx, desde, cur)
		if err != nil {
			_ = s.st.ActualizarWatermarkOrdenes(ctx, p.CuentaID, desde, err.Error())
			return err
		}
		pedidos = append(pedidos, pagina.Orders...)
		if pagina.Done || len(pagina.Orders) == 0 {
			break
		}
		cur = pagina.Next
	}

	nuevos := 0
	maxFecha := desde
	// Las cuentas a las que hay que mandar el stock que esta ingesta movió.
	// Se acumulan y se encolan al final, no pedido a pedido: si el worker
	// atendiera el envío entre dos pedidos de la misma variante, el segundo
	// descuento se quedaría sin publicar hasta el siguiente horario, que es
	// justo la ventana que se está cerrando.
	avisos := map[store.DestinoStock]struct{}{}
	for _, o := range pedidos {
		d := store.DatosOrden{
			CuentaID: p.CuentaID, ExternalID: o.ExternalID, Numero: o.Number,
			EstadoCanal: o.Status, FechaPedido: o.OrderedAt, Moneda: monedaDe(o.Currency),
			Total: o.Total, Envio: o.Shipping, Impuesto: o.Tax,
			Comprador: o.Buyer.Name, Documento: o.Buyer.Document,
			Email: o.Buyer.Email, Telefono: o.Buyer.Phone,
			Direccion: map[string]string{
				"linea1": o.Buyer.Address.Line1, "linea2": o.Buyer.Address.Line2,
				"ciudad": o.Buyer.Address.City, "departamento": o.Buyer.Address.State,
				"codigo_postal": o.Buyer.Address.PostalCode, "pais": o.Buyer.Address.Country,
			},
			Crudo: o.Raw,
		}
		for _, l := range o.Lines {
			d.Lineas = append(d.Lineas, store.DatosLinea{
				ExternalID: l.ExternalID, VarianteExt: l.VariantRef, SKU: l.SKU,
				Titulo: l.Title, Cantidad: l.Quantity,
				PrecioUnit: l.UnitPrice, Total: l.TotalPrice,
			})
		}

		ordenID, nuevo, err := s.st.GuardarOrden(ctx, d)
		if err != nil {
			return err
		}
		// La marca avanza con la última modificación cuando el canal la da:
		// así un pedido viejo que cambió de estado no se vuelve a pedir en
		// cada ronda, y uno nuevo tampoco se queda atrás.
		if o.OrderedAt.After(maxFecha) {
			maxFecha = o.OrderedAt
		}
		if o.UpdatedAt.After(maxFecha) {
			maxFecha = o.UpdatedAt
		}

		// Un pedido que el canal canceló se atiende llegue como llegue: si es
		// nuevo, para no montarlo en Odoo; si ya estaba, porque GuardarOrden
		// acaba de refrescarle el estado del canal y esta es la única pasada
		// en la que ese cambio se puede notar. Va antes del corte por `nuevo`.
		if esCancelado(o.Status) {
			if s.cancelar(ctx, ordenID, o) {
				s.anotarDestinosDeStock(ctx, avisos, ordenID, o.Number)
			}
			continue
		}

		if !nuevo {
			continue
		}
		nuevos++
		s.log.Info("pedido nuevo", "canal", ad.Kind(), "numero", o.Number, "total", o.Total)

		// El stock baja aquí mismo, no en el siguiente sync: mientras tanto
		// los otros tres canales seguirían ofreciendo unidades ya vendidas.
		// Un fallo descontando no puede tumbar la ingesta: el pedido ya está
		// guardado y perderlo sería mucho peor que publicar stock de más.
		if n, err := s.st.DescontarStockPublicado(ctx, ordenID); err != nil {
			s.log.Error("no se pudo descontar el stock vendido",
				"pedido", o.Number, "orden_id", ordenID, "error", err)
		} else if n > 0 {
			s.log.Info("stock descontado por venta", "pedido", o.Number, "unidades", n)
			s.anotarDestinosDeStock(ctx, avisos, ordenID, o.Number)
		}
		// El montaje va en su propio trabajo: si Odoo está caído, se
		// reintenta con backoff sin arrastrar a la ingesta, que ya hizo su
		// parte y no debe repetir la llamada al canal.
		if s.cola != nil {
			if err := EncolarMontaje(ctx, s.cola, p.CuentaID, ordenID); err != nil {
				s.log.Error("no se pudo encolar el montaje en Odoo",
					"pedido", o.Number, "orden_id", ordenID, "error", err)
			}
		}
	}
	// Antes de avanzar la marca de agua: si avanzarla fallara, el reintento
	// de la ingesta volvería a ver estos pedidos como ya guardados y no
	// descontaría —ni avisaría— nada por segunda vez.
	s.encolarStock(ctx, avisos)

	if err := s.st.ActualizarWatermarkOrdenes(ctx, p.CuentaID, maxFecha, ""); err != nil {
		return err
	}
	if nuevos > 0 {
		s.log.Info("ingesta de pedidos", "cuenta", p.CuentaID, "nuevos", nuevos)
	}
	return nil
}

// cancelar refleja una cancelación del canal sobre un pedido ya ingerido.
//
// Son tres cosas: devolver el stock que se apartó al ingerirlo (o esas
// unidades quedarían sin vender para siempre), sacarlo de la cola de montaje
// (para que no acabe en Odoo un pedido que ya no existe) y dejar constancia.
// El sale.order que ya esté creado NO se toca: cancelarlo mueve reservas y
// contabilidad, y eso lo decide una persona.
//
// Devuelve si volvió stock a la base, para que la ingesta lo mande a las
// demás cuentas: una unidad que vuelve a estar disponible y no se publica es
// una venta que no se hace.
func (s *Servicio) cancelar(ctx context.Context, ordenID int64, o channel.Order) (hayStock bool) {
	// La devolución va antes de marcar, y no al revés: si se marcara primero
	// y la devolución fallara, la marca impediría reintentarla en la
	// siguiente pasada y esas unidades quedarían apartadas para siempre.
	// Devolver dos veces no puede pasar: los asientos ya liberados no
	// vuelven a entrar.
	devueltas, err := s.st.DevolverStockReservado(ctx, ordenID, "cancelado en el canal")
	if err != nil {
		s.log.Error("no se pudo devolver el stock del pedido cancelado",
			"pedido", o.Number, "orden_id", ordenID, "error", err)
	}
	hayStock = devueltas > 0

	c, err := s.st.MarcarOrdenCancelada(ctx, ordenID, o.Status)
	if err != nil {
		s.log.Error("no se pudo marcar el pedido como cancelado",
			"pedido", o.Number, "orden_id", ordenID, "error", err)
		return hayStock
	}
	if !c.Cambio {
		return hayStock // ya estaba cancelado: no se repite el aviso
	}
	s.log.Warn("pedido cancelado en el canal", "canal", c.Canal, "numero", c.Numero,
		"unidades_devueltas", devueltas, "odoo_pedido", c.OdooPedidoID)

	// La alerta solo tiene sentido si hay algo que decidir. Si el pedido
	// nunca llegó a Odoo, cancelarlo no deja nada pendiente para nadie.
	if c.OdooPedidoID == nil {
		return hayStock
	}
	cuenta := c.CuentaID
	mensaje := fmt.Sprintf(
		"El canal canceló el pedido %s (%s) y ya estaba montado en Odoo como sale.order %d: "+
			"hay que decidir a mano qué se hace con él", c.Numero, c.Canal, *c.OdooPedidoID)
	if err := s.st.CrearAlerta(ctx, AlertaPedidoCancelado, "warning", &cuenta, mensaje,
		map[string]any{
			"orden_id": ordenID, "numero": c.Numero, "canal": c.Canal,
			"odoo_sale_order_id": *c.OdooPedidoID, "estado_canal": o.Status,
		}); err != nil {
		s.log.Error("no se pudo crear la alerta de cancelación", "orden_id", ordenID, "error", err)
	}
	return hayStock
}

// anotarDestinosDeStock apunta las cuentas a las que hay que mandar el stock
// de las variantes de un pedido que acaba de moverlo. Un fallo aquí no tumba
// la ingesta: el pedido ya está guardado, y el horario reconcilia el stock
// por hash como hasta ahora; solo se pierde la inmediatez.
func (s *Servicio) anotarDestinosDeStock(ctx context.Context, avisos map[store.DestinoStock]struct{},
	ordenID int64, numero string) {

	destinos, err := s.st.DestinosDeStockDeOrden(ctx, ordenID)
	if err != nil {
		s.log.Error("no se pudo saber a qué cuentas mandar el stock del pedido",
			"pedido", numero, "orden_id", ordenID, "error", err)
		return
	}
	for _, d := range destinos {
		avisos[d] = struct{}{}
	}
}

// encolarStock pide el envío de stock a cada cuenta anotada durante la
// ingesta. Es lo que faltaba para que una venta en un canal bajara el stock
// en los otros tres en el acto y no en la corrida del día siguiente: el
// descuento sobre la base ya se hacía, pero nadie lo mandaba.
//
// Comparte clave única con lo que encola Planificar, así que un horario que
// caiga a la vez no manda el mismo stock dos veces. Queda una ventana que la
// clave no cubre: si el trabajo de una cuenta ya está en ejecución cuando otra
// ingesta vuelve a moverla, ese segundo aviso se descarta y lo recoge el
// siguiente horario. Son segundos y dos ventas de la misma variante a la vez.
func (s *Servicio) encolarStock(ctx context.Context, avisos map[store.DestinoStock]struct{}) {
	if s.cola == nil || len(avisos) == 0 {
		return
	}
	encolados := 0
	for d := range avisos {
		if err := publicar.EncolarStock(ctx, s.cola, d.CuentaID, d.VarianteID); err != nil {
			s.log.Error("no se pudo encolar el envío de stock a otra cuenta",
				"cuenta", d.CuentaID, "variante", d.VarianteID, "error", err)
			continue
		}
		encolados++
	}
	if encolados > 0 {
		s.log.Info("stock movido por ventas: envío encolado a las demás cuentas", "envios", encolados)
	}
}

// esCancelado dice si el estado que reporta el canal significa que la venta
// murió. Cada canal lo nombra a su manera y ninguno normaliza:
// MercadoLibre usa cancelled e invalid (fraude), WooCommerce cancelled y
// refunded, y Shopify manda su financial_status, donde una cancelación
// aparece como voided (nunca se cobró) o refunded (se devolvió el dinero).
//
// Deliberadamente fuera: WooCommerce 'failed' (pago rechazado que el
// comprador suele reintentar, no una cancelación) y los reembolsos parciales,
// que devuelven dinero pero no necesariamente la mercancía entera.
func esCancelado(estado string) bool {
	switch strings.ToLower(strings.TrimSpace(estado)) {
	case "cancelled", "canceled", "invalid", "refunded", "voided":
		return true
	}
	return false
}

// montarEnOdoo crea el pedido de venta. Es idempotente por partida doble:
// no se reintenta si ya hay un sale.order anotado, y antes de crear se busca
// en Odoo por la referencia del canal (client_order_ref) por si un intento
// anterior alcanzó a crearlo y se cayó antes de anotarlo.
func (s *Servicio) montarEnOdoo(ctx context.Context, t jobs.Trabajo) error {
	var p payloadOdoo
	if err := json.Unmarshal(t.Payload, &p); err != nil {
		return fmt.Errorf("payload ilegible: %w", err)
	}
	orden, err := s.st.OrdenPendientePorID(ctx, p.OrdenID)
	if err != nil {
		return err
	}
	if orden == nil {
		return nil // ya está creado, descartado o agotó los intentos
	}
	return s.CrearPedido(ctx, *orden)
}

// CrearPedido monta un pedido concreto en Odoo.
func (s *Servicio) CrearPedido(ctx context.Context, o store.Orden) error {
	// Una línea sin variante reconocida no puede convertirse en línea de
	// pedido: se marca el fallo y se deja para que una persona lo mapee.
	for _, l := range o.Lineas {
		if l.VarianteID == nil {
			causa := fmt.Sprintf("el SKU %q del pedido %s no existe en el catálogo", l.SKU, o.Numero)
			_ = s.st.MarcarOrdenFallida(ctx, o.ID, causa)
			return fmt.Errorf("%s", causa)
		}
	}

	cli, err := s.abrirOdoo(ctx)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}

	ref := referencia(o)

	// Idempotencia contra Odoo: si ya existe un pedido con esta referencia,
	// se adopta en vez de duplicarlo.
	if existente, err := cli.BuscarUno("sale.order",
		[]interface{}{[]interface{}{"client_order_ref", "=", ref}}); err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	} else if existente != 0 {
		s.log.Warn("el pedido ya existía en Odoo: se adopta", "ref", ref, "odoo_id", existente)
		return s.st.MarcarOrdenCreada(ctx, o.ID, existente, 0)
	}

	partnerID, err := s.resolverCliente(ctx, cli, o)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}

	// La moneda se fija antes de crear nada: si no hay tarifa en la moneda del
	// canal, el pedido no se monta. Ver tarifaEnMoneda.
	tarifaID, tarifaNombre, err := tarifaEnMoneda(cli, o.Moneda)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}
	if tarifaID == 0 {
		causa := fmt.Sprintf(
			"no hay ninguna tarifa (product.pricelist) de Odoo en %s, que es la moneda del "+
				"pedido %s: crea una tarifa en esa moneda antes de reintentar, o el pedido "+
				"quedaría valorado en la moneda de la tarifa del cliente",
			o.Moneda, o.Numero)
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, causa)
		return fmt.Errorf("%s", causa)
	}

	// Las líneas van en el formato de comandos x2many de Odoo: (0, 0, valores)
	// crea una línea nueva dentro del pedido.
	var lineas []interface{}
	for _, l := range o.Lineas {
		odooProductID, err := s.productoOdoo(ctx, *l.VarianteID)
		if err != nil {
			_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
			return err
		}
		lineas = append(lineas, []interface{}{0, 0, map[string]interface{}{
			"product_id":      odooProductID,
			"product_uom_qty": l.Cantidad,
			"price_unit":      l.PrecioUnit,
			"name":            l.Titulo,
			// tax_id explícito y vacío, en el comando x2many (6, 0, []).
			//
			// price_unit es el precio final que pagó el comprador en el canal,
			// con el impuesto ya dentro. Los impuestos de venta de esta base
			// son «Tax Excluded» (account.tax.price_include_override = False),
			// y la documentación de Odoo 18 dice que en ese modo «the tax
			// amount is not included in the sales price. The tax computation
			// will therefore compute a tax amount on top of the sales price»
			// (https://www.odoo.com/documentation/18.0/applications/finance/accounting/taxes/tax_computation.html).
			//
			// Omitir la clave no significaba «sin impuestos»:
			// sale.order.line.tax_id se declara compute='_compute_tax_id',
			// store=True, readonly=False, precompute=True, y el ORM solo
			// precalcula los campos que NO vienen en los valores de create
			// (odoo/models.py, _add_precomputed_values: `if fname not in
			// vals`). Así que Odoo lo rellenaba desde product.taxes_id y
			// sumaba un 15 % encima de un precio que ya lo llevaba dentro.
			//
			// Mandándolo vacío el pedido refleja exactamente lo que cobró el
			// canal, que es la decisión tomada: borrador, comprador real y sin
			// tocar la configuración de impuestos de Odoo. El desglose fiscal
			// de la venta lo decide quien revise el borrador antes de
			// confirmarlo; ver AlertaDescuadrePedido.
			"tax_id": []interface{}{[]interface{}{6, 0, []interface{}{}}},
		}})
	}

	// La bodega sale de las que tenga asignadas la cuenta: una venta de
	// Falabella tiene que descontar de las bodegas FB, no de la principal.
	bodega, err := s.st.BodegaDeOrden(ctx, o.ID)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}
	var bodegaOdooID int64
	if bodega != nil {
		bodegaOdooID = bodega.OdooID
	}

	valores := valoresPedido(o, partnerID, ref, lineas, bodegaOdooID, tarifaID)

	// Se crea en borrador a propósito: confirmar reserva stock y dispara
	// contabilidad, y esa decisión es de quien lleva las cuentas de MDV.
	pedidoID, err := cli.Create("sale.order", valores)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}

	nombreBodega := "(por defecto de Odoo)"
	if bodega != nil {
		nombreBodega = bodega.Codigo
	}
	s.log.Info("pedido creado en Odoo", "canal", o.Canal, "numero", o.Numero,
		"odoo_id", pedidoID, "total", o.Total, "bodega", nombreBodega,
		"tarifa", tarifaNombre)

	// Lo que Odoo acabe calculando no depende solo de lo que se mandó: una
	// posición fiscal, una tarifa o una conversión de moneda pueden alterarlo
	// sin dar un solo error. Se relee y se contrasta antes de que nadie
	// confirme el borrador.
	s.verificarPedido(ctx, cli, o, pedidoID)
	return s.st.MarcarOrdenCreada(ctx, o.ID, pedidoID, partnerID)
}

// valoresPedido arma el sale.order que se manda a Odoo.
//
// Está aparte y sin dependencias para poder comprobar sin Odoo delante lo que
// se envía, que es donde vivía el hueco: el pedido salía sin warehouse_id y
// Odoo lo despachaba de la bodega por defecto.
//
// Con bodegaOdooID en cero la clave no se manda: una cuenta sin bodegas
// asignadas debe seguir dejando que Odoo decida, no fallar.
//
// Los impuestos no se configuran aquí: cada línea viaja con tax_id vacío para
// que Odoo no sume nada sobre el precio que ya cobró el canal. Ver CrearPedido.
//
// Con tarifaID en cero la clave no se manda. No debería ocurrir —CrearPedido
// falla antes— pero valoresPedido no puede decidir por su cuenta la moneda de
// un documento contable.
func valoresPedido(o store.Orden, partnerID int64, ref string,
	lineas []interface{}, bodegaOdooID, tarifaID int64) map[string]interface{} {

	valores := map[string]interface{}{
		"partner_id":       partnerID,
		"client_order_ref": ref,
		"origin":           strings.ToUpper(o.Canal),
		"date_order":       o.FechaPedido.UTC().Format("2006-01-02 15:04:05"),
		"order_line":       lineas,
	}
	if bodegaOdooID > 0 {
		valores["warehouse_id"] = bodegaOdooID
	}
	if tarifaID > 0 {
		valores["pricelist_id"] = tarifaID
	}
	return valores
}

// tarifaEnMoneda busca la tarifa de Odoo que factura en la moneda del canal.
//
// La moneda del pedido NO se puede mandar: sale.order.currency_id es
// readonly=True (comprobado con fields_get contra la instancia real) y se
// calcula desde la tarifa —_compute_currency_id hace
// `order.currency_id = order.pricelist_id.currency_id or company_id.currency_id`
// en sale/models/sale_order.py—. Y la tarifa, si no se manda, sale del
// cliente: «When a customer is added to the database, the default pricelist is
// automatically applied to them»
// (https://www.odoo.com/documentation/18.0/applications/sales/sales/products_prices/prices/pricing.html).
//
// Es decir, sin pricelist_id la moneda del pedido la decidía el partner. En la
// base de MDV conviven tres tarifas en COP y una en USD: bastaba con que
// resolverCliente encontrara por correo un partner con la tarifa en USD para
// que un pedido de 350.000 COP quedara valorado en 350.000 USD, sin un solo
// error y sin arreglo posible después, porque currency_id no se puede escribir.
//
// Mandar la tarifa no reescribe los importes: price_unit también se declara
// compute/store/readonly=False/precompute y el ORM no precalcula lo que ya
// viene en los valores de create, igual que con tax_id. La tarifa aquí solo
// fija la moneda.
func tarifaEnMoneda(cli *odoo.Client, moneda string) (int64, string, error) {
	m := strings.ToUpper(strings.TrimSpace(moneda))
	if m == "" {
		return 0, "", nil
	}
	// Ordenado por id para que la elección sea reproducible cuando hay varias
	// tarifas en la misma moneda, que es el caso normal.
	filas, err := cli.SearchRead("product.pricelist",
		[]interface{}{[]interface{}{"currency_id.name", "=", m}},
		[]string{"id", "name"},
		map[string]interface{}{"limit": 1, "order": "id"})
	if err != nil {
		return 0, "", fmt.Errorf("buscando la tarifa de Odoo en %s: %w", m, err)
	}
	if len(filas) == 0 {
		return 0, "", nil
	}
	return filas[0].ID(), filas[0].Str("name"), nil
}

// verificarPedido relee el pedido recién creado y comprueba que dice lo mismo
// que el canal. No corrige nada: avisa.
//
// Es la contrapartida de crear el pedido en borrador. Odoo puede haber
// aplicado una posición fiscal, un impuesto por defecto o una conversión de
// moneda que Integra no mandó, y ninguna de esas cosas da error: el pedido se
// crea igual, con otro total, y el descuadre aparece al facturar.
//
// Un fallo aquí no puede tumbar el montaje: el pedido ya existe en Odoo y
// perder ese hecho sería mucho peor que quedarse sin la comprobación.
func (s *Servicio) verificarPedido(ctx context.Context, cli *odoo.Client, o store.Orden, pedidoID int64) {
	filas, err := cli.SearchRead("sale.order",
		[]interface{}{[]interface{}{"id", "=", pedidoID}},
		[]string{"amount_total", "currency_id"},
		map[string]interface{}{"limit": 1})
	if err != nil || len(filas) == 0 {
		s.log.Warn("no se pudo releer el pedido para contrastarlo con el canal",
			"odoo_id", pedidoID, "pedido", o.Numero, "error", err)
		return
	}
	totalOdoo := filas[0].Float("amount_total")
	monedaOdoo := filas[0].RefName("currency_id")

	// Lo que Integra mandó es la suma de las líneas, no o.Total: el flete no
	// viaja a Odoo (ver más abajo).
	var esperado float64
	for _, l := range o.Lineas {
		esperado += l.Cantidad * l.PrecioUnit
	}
	// Odoo redondea cada línea a los decimales de la moneda —en pesos, cero—,
	// así que se tolera medio peso por línea.
	tolerancia := 0.01 + 0.5*float64(len(o.Lineas))

	var problemas []string
	if math.Abs(totalOdoo-esperado) > tolerancia {
		problemas = append(problemas, fmt.Sprintf(
			"Odoo dice %.2f y el canal cobró %.2f por las líneas", totalOdoo, esperado))
	}
	if monedaOdoo != "" && !strings.EqualFold(monedaOdoo, o.Moneda) {
		problemas = append(problemas, fmt.Sprintf(
			"Odoo lo valoró en %s y el canal cobró en %s", monedaOdoo, o.Moneda))
	}

	// El flete es harina de otro costal: llevarlo a Odoo exige un producto de
	// servicio y una cuenta contable que nadie ha decidido todavía, así que
	// hoy no viaja. No es un descuadre del pedido, pero sí la razón de que el
	// total de Odoo no cuadre con el abono del marketplace, y conviene que
	// quede dicho en el pedido donde ocurre.
	if o.Envio > 0 {
		s.log.Warn("el flete del pedido no se traslada a Odoo: falta decidir con qué producto de servicio se factura",
			"pedido", o.Numero, "canal", o.Canal, "flete", o.Envio, "odoo_id", pedidoID)
	}

	if len(problemas) == 0 {
		return
	}
	s.log.Warn("pedido descuadrado en Odoo", "pedido", o.Numero, "odoo_id", pedidoID,
		"problemas", strings.Join(problemas, "; "))
	mensaje := fmt.Sprintf(
		"El pedido %s (%s) se creó en Odoo como sale.order %d pero no cuadra con el canal: %s",
		o.Numero, o.Canal, pedidoID, strings.Join(problemas, "; "))
	cuenta := o.CuentaID
	if err := s.st.CrearAlerta(ctx, AlertaDescuadrePedido, "warning", &cuenta, mensaje,
		map[string]any{
			"orden_id": o.ID, "numero": o.Numero, "canal": o.Canal,
			"odoo_sale_order_id": pedidoID, "total_odoo": totalOdoo,
			"total_lineas_canal": esperado, "moneda_odoo": monedaOdoo,
			"moneda_canal": o.Moneda, "envio_no_trasladado": o.Envio,
		}); err != nil {
		s.log.Error("no se pudo crear la alerta de descuadre", "orden_id", o.ID, "error", err)
	}
}

// resolverCliente busca el comprador por correo y, si no está, lo crea.
//
// Los canales entregan datos de contacto incompletos y con restricciones de
// uso, así que se guarda lo mínimo para facturar y despachar.
func (s *Servicio) resolverCliente(ctx context.Context, cli *odoo.Client, o store.Orden) (int64, error) {
	datos, err := s.st.DatosCompradorDeOrden(ctx, o.ID)
	if err != nil {
		return 0, err
	}
	dir, err := s.st.DireccionEnvioDeOrden(ctx, o.ID)
	if err != nil {
		return 0, err
	}

	if datos.Email != "" {
		id, err := cli.BuscarUno("res.partner",
			[]interface{}{[]interface{}{"email", "=", datos.Email}})
		if err != nil {
			return 0, err
		}
		if id != 0 {
			return id, nil
		}
	}

	nombre := datos.Nombre
	if strings.TrimSpace(nombre) == "" {
		// Sin nombre, el pedido se atribuye a un cliente genérico del canal:
		// mejor eso que un partner vacío imposible de identificar después.
		nombre = "Comprador " + strings.ToUpper(o.Canal)
	}
	valores := map[string]interface{}{
		"name":          nombre,
		"comment":       "Creado por Integra desde " + o.Canal + " (pedido " + o.Numero + ")",
		"customer_rank": 1,
	}
	if datos.Email != "" {
		valores["email"] = datos.Email
	}
	if datos.Telefono != "" {
		valores["phone"] = datos.Telefono
	}
	if datos.Ciudad != "" {
		valores["city"] = datos.Ciudad
	}
	if datos.Direccion != "" {
		valores["street"] = datos.Direccion
	}

	// El documento de identidad va a vat, que la ficha de contacto de Odoo 18
	// documenta como «the identification number used for tax and accounting
	// purposes»
	// (https://www.odoo.com/documentation/18.0/applications/essentials/contacts.html).
	// Se ingería desde el canal y se tiraba justo aquí: todos los compradores
	// nacían sin identificación fiscal, y sin ella no se les puede emitir la
	// factura electrónica, que en Colombia es obligatoria.
	if datos.Documento != "" {
		valores["vat"] = datos.Documento
	} else {
		// Queda dicho en vez de descartarse en silencio: alguien tendrá que
		// completar el contacto a mano antes de facturar.
		s.log.Warn("el canal no entregó el documento del comprador: el contacto de Odoo nace sin identificación fiscal",
			"canal", o.Canal, "pedido", o.Numero)
	}

	// La otra mitad de la dirección. street y city ya viajaban; línea 2, código
	// postal, departamento y país se ingerían (ver ingerir) y se perdían justo
	// aquí, así que el albarán de despacho salía con la dirección incompleta.
	// Comprobado con fields_get contra la instancia real: street2, zip,
	// state_id y country_id existen en el res.partner de Odoo 18 y son
	// readonly=False, es decir escribibles por RPC.
	if dir.Linea2 != "" {
		valores["street2"] = dir.Linea2
	}
	if dir.CodigoPostal != "" {
		valores["zip"] = dir.CodigoPostal
	}
	paisID, err := buscarPais(cli, dir.Pais)
	if err != nil {
		return 0, err
	}
	switch {
	case paisID != 0:
		valores["country_id"] = paisID
		depID, err := buscarDepartamento(cli, paisID, dir.Departamento)
		if err != nil {
			return 0, err
		}
		if depID != 0 {
			valores["state_id"] = depID
		} else if strings.TrimSpace(dir.Departamento) != "" {
			s.log.Warn("el departamento del comprador no existe en Odoo: el contacto se crea sin él",
				"departamento", dir.Departamento, "pedido", o.Numero)
		}
	case strings.TrimSpace(dir.Pais) != "":
		s.log.Warn("el país del comprador no existe en Odoo: el contacto se crea sin país",
			"pais", dir.Pais, "pedido", o.Numero)
	}

	return cli.Create("res.partner", valores)
}

// buscarPais traduce lo que manda el canal a un res.country.
//
// Unos canales entregan el código ISO de dos letras («CO») y otros el nombre
// («Colombia»), así que se prueban los dos: primero el código, que es exacto, y
// después el nombre sin distinguir mayúsculas. Devuelve 0 sin error cuando no
// hay con qué buscar o no se encuentra: un país desconocido deja el contacto
// sin país, no tumba el pedido.
func buscarPais(cli *odoo.Client, pais string) (int64, error) {
	p := strings.TrimSpace(pais)
	if p == "" {
		return 0, nil
	}
	if len(p) == 2 {
		id, err := cli.BuscarUno("res.country",
			[]interface{}{[]interface{}{"code", "=", strings.ToUpper(p)}})
		if err != nil || id != 0 {
			return id, err
		}
	}
	return cli.BuscarUno("res.country",
		[]interface{}{[]interface{}{"name", "=ilike", p}})
}

// buscarDepartamento resuelve el res.country.state DENTRO del país.
//
// Acotar por país no es un adorno: hay departamentos y estados homónimos entre
// países, y un state_id del país equivocado sería peor que ninguno, porque el
// albarán saldría con una dirección falsa en vez de incompleta. Se prueba el
// código de Odoo («ANT») y después el nombre («Antioquia»), que es lo que
// suelen mandar los canales.
func buscarDepartamento(cli *odoo.Client, paisID int64, departamento string) (int64, error) {
	d := strings.TrimSpace(departamento)
	if paisID == 0 || d == "" {
		return 0, nil
	}
	id, err := cli.BuscarUno("res.country.state", []interface{}{
		[]interface{}{"country_id", "=", paisID},
		[]interface{}{"code", "=", strings.ToUpper(d)},
	})
	if err != nil || id != 0 {
		return id, err
	}
	return cli.BuscarUno("res.country.state", []interface{}{
		[]interface{}{"country_id", "=", paisID},
		[]interface{}{"name", "=ilike", d},
	})
}

func (s *Servicio) productoOdoo(ctx context.Context, varianteID int64) (int64, error) {
	id, err := s.st.OdooProductIDDeVariante(ctx, varianteID)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, fmt.Errorf("la variante %d no tiene producto de Odoo asociado", varianteID)
	}
	return id, nil
}

// referencia es la clave que ata el pedido de Odoo con el del canal. Va en
// client_order_ref y es lo que hace idempotente la creación.
func referencia(o store.Orden) string {
	n := o.Numero
	if n == "" {
		n = o.ExternalID
	}
	return strings.ToUpper(o.Canal) + "-" + n
}

// DireccionEnvio es la mitad de la dirección del comprador que la ingesta
// guarda en shipping_address y que hasta ahora no leía nadie.
type DireccionEnvio struct {
	Linea2       string
	Departamento string
	CodigoPostal string
	Pais         string
}

// tienda añade a *store.Store la única lectura que la capa de persistencia no
// ofrecía. Vive aquí, y no junto a DatosCompradorDeOrden, por la misma razón
// que AjustarActividad vive en internal/sync: la dirección completa solo hace
// falta al escribir el contacto en Odoo, que es asunto exclusivo de este
// paquete.
type tienda struct{ *store.Store }

func (t tienda) DireccionEnvioDeOrden(ctx context.Context, ordenID int64) (DireccionEnvio, error) {
	var d DireccionEnvio
	err := t.Pool().QueryRow(ctx, `
		SELECT COALESCE(shipping_address->>'linea2',''),
		       COALESCE(shipping_address->>'departamento',''),
		       COALESCE(shipping_address->>'codigo_postal',''),
		       COALESCE(shipping_address->>'pais','')
		FROM channel_orders WHERE id = $1`, ordenID).
		Scan(&d.Linea2, &d.Departamento, &d.CodigoPostal, &d.Pais)
	if err != nil {
		return d, fmt.Errorf("leyendo la dirección de envío del pedido %d: %w", ordenID, err)
	}
	return d, nil
}

func monedaDe(s string) string {
	if strings.TrimSpace(s) == "" {
		return "COP"
	}
	return strings.ToUpper(s)
}
