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
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/store"
)

// Tipos de trabajo.
const (
	TrabajoIngerir = "ingerir_ordenes"
	TrabajoAOdoo   = "orden_a_odoo"
)

type payloadIngerir struct {
	CuentaID int64 `json:"cuenta_id"`
}

type payloadOdoo struct {
	OrdenID int64 `json:"orden_id"`
}

// Servicio ingiere pedidos y los monta en Odoo.
type Servicio struct {
	st  *store.Store
	cif *crypto.Cifrador
	log *slog.Logger
	// abrirOdoo se inyecta para no duplicar aquí el descifrado de la conexión.
	abrirOdoo func(context.Context) (*odoo.Client, error)
}

func NuevoServicio(st *store.Store, cif *crypto.Cifrador, log *slog.Logger,
	abrirOdoo func(context.Context) (*odoo.Client, error)) *Servicio {
	return &Servicio{st: st, cif: cif, log: log, abrirOdoo: abrirOdoo}
}

func (s *Servicio) Registrar(w *jobs.Worker) {
	w.Registrar(TrabajoIngerir, s.ingerir)
	w.Registrar(TrabajoAOdoo, s.montarEnOdoo)
}

// EncolarIngesta pide traer los pedidos nuevos de una cuenta.
func EncolarIngesta(ctx context.Context, cola *jobs.Cola, cuentaID int64) error {
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

		_, nuevo, err := s.st.GuardarOrden(ctx, d)
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
		if !nuevo {
			continue
		}
		nuevos++
		s.log.Info("pedido nuevo", "canal", ad.Kind(), "numero", o.Number, "total", o.Total)
	}

	if err := s.st.ActualizarWatermarkOrdenes(ctx, p.CuentaID, maxFecha, ""); err != nil {
		return err
	}
	if nuevos > 0 {
		s.log.Info("ingesta de pedidos", "cuenta", p.CuentaID, "nuevos", nuevos)
	}
	return nil
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
	pendientes, err := s.st.OrdenesPendientesOdoo(ctx, 200)
	if err != nil {
		return err
	}
	var orden *store.Orden
	for i := range pendientes {
		if pendientes[i].ID == p.OrdenID {
			orden = &pendientes[i]
			break
		}
	}
	if orden == nil {
		return nil // ya está creado o descartado: nada que hacer
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
			"product_id":       odooProductID,
			"product_uom_qty":  l.Cantidad,
			"price_unit":       l.PrecioUnit,
			"name":             l.Titulo,
		}})
	}

	valores := map[string]interface{}{
		"partner_id":       partnerID,
		"client_order_ref": ref,
		"origin":           strings.ToUpper(o.Canal),
		"date_order":       o.FechaPedido.UTC().Format("2006-01-02 15:04:05"),
		"order_line":       lineas,
	}

	// Se crea en borrador a propósito: confirmar reserva stock y dispara
	// contabilidad, y esa decisión es de quien lleva las cuentas de MDV.
	pedidoID, err := cli.Create("sale.order", valores)
	if err != nil {
		_ = s.st.MarcarOrdenFallida(ctx, o.ID, err.Error())
		return err
	}

	s.log.Info("pedido creado en Odoo", "canal", o.Canal, "numero", o.Numero,
		"odoo_id", pedidoID, "total", o.Total)
	return s.st.MarcarOrdenCreada(ctx, o.ID, pedidoID, partnerID)
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
		"name":     nombre,
		"comment":  "Creado por Integra desde " + o.Canal + " (pedido " + o.Numero + ")",
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
	return cli.Create("res.partner", valores)
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

func (s *Servicio) adaptador(ctx context.Context, cuentaID int64) (channel.Adapter, error) {
	return conectores.AdaptadorDeCuenta(ctx, s.st, s.cif, cuentaID)
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

func monedaDe(s string) string {
	if strings.TrimSpace(s) == "" {
		return "COP"
	}
	return strings.ToUpper(s)
}
