package ordenes

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/odoo"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
)

// El fallo que estas pruebas vigilan: durante meses el manejador de
// orden_a_odoo estuvo registrado en el worker pero nadie encolaba ese
// trabajo, así que los pedidos ingeridos se quedaban en 'received' y solo
// llegaban a Odoo si alguien ejecutaba `integra ordenes` a mano. Compilaba,
// pasaba las pruebas y el tercer pilar de la plataforma estaba desconectado.

func servicioDePrueba() *Servicio {
	return NuevoServicio(nil, nil, slog.New(slog.NewTextHandler(os.Stderr, nil)),
		func(context.Context) (*odoo.Client, error) { return nil, nil })
}

// TestRegistrarDejaElServicioCapazDeEncolar comprueba el cableado que faltaba:
// sin la cola, ingerir guarda el pedido y nadie lo monta nunca en Odoo.
func TestRegistrarDejaElServicioCapazDeEncolar(t *testing.T) {
	s := servicioDePrueba()
	if s.cola != nil {
		t.Fatal("un servicio recién creado no debería tener cola")
	}

	w := jobs.NuevoWorker(jobs.NuevaCola(nil), slog.Default(), 1, time.Second)
	s.Registrar(w)

	if s.cola == nil {
		t.Error("Registrar debe dejar la cola del worker en el servicio: " +
			"sin ella la ingesta no encola el montaje y los pedidos no llegan a Odoo")
	}
}

// TestRegistrarAtiendeLosDosTrabajos evita el otro lado del mismo fallo:
// encolar un trabajo que nadie maneja.
func TestRegistrarAtiendeLosDosTrabajos(t *testing.T) {
	w := jobs.NuevoWorker(jobs.NuevaCola(nil), slog.Default(), 1, time.Second)
	servicioDePrueba().Registrar(w)

	for _, kind := range []string{TrabajoIngerir, TrabajoAOdoo} {
		if !w.Maneja(kind) {
			t.Errorf("el worker no atiende %q", kind)
		}
	}
}

func abrirPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("abriendo el pool: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM jobs WHERE kind = $1 AND payload->>'orden_id' = '999000111'`, TrabajoAOdoo)
		pool.Close()
	})
	return pool
}

// TestEncolarMontajeNoDuplica: dos pasadas del planificador sobre el mismo
// pedido pendiente no pueden crear dos trabajos, o el pedido se montaría dos
// veces en Odoo si la idempotencia de client_order_ref fallara.
func TestEncolarMontajeNoDuplica(t *testing.T) {
	ctx := context.Background()
	cola := jobs.NuevaCola(abrirPool(t))
	const ordenID = 999000111

	if err := EncolarMontaje(ctx, cola, 0, ordenID); err != nil {
		t.Fatal(err)
	}
	if err := EncolarMontaje(ctx, cola, 0, ordenID); err != nil {
		t.Fatal(err)
	}

	var n int
	err := cola.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE kind = $1 AND payload->>'orden_id' = $2`,
		TrabajoAOdoo, "999000111").Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("se encolaron %d trabajos para el mismo pedido; la clave única debe dejar 1", n)
	}
}

// El sale.order se creaba sin warehouse_id, así que Odoo lo despachaba
// siempre de la bodega por defecto: una venta de Falabella descontaba de la
// bodega principal mientras el stock consignado en las bodegas FB seguía
// intacto. channel_account_warehouses existía desde el primer esquema y no la
// leía nadie en el camino del pedido.
func TestValoresPedidoLlevaLaBodegaDeLaCuenta(t *testing.T) {
	o := store.Orden{Canal: "falabella", Numero: "FB-1",
		FechaPedido: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}

	valores := valoresPedido(o, 42, "FALABELLA-FB-1", nil, 7, 1)

	if valores["warehouse_id"] != int64(7) {
		t.Errorf("el pedido debe salir de la bodega de la cuenta, warehouse_id = %v",
			valores["warehouse_id"])
	}
	if valores["partner_id"] != int64(42) {
		t.Errorf("partner_id = %v", valores["partner_id"])
	}
	if valores["client_order_ref"] != "FALABELLA-FB-1" {
		t.Errorf("client_order_ref = %v", valores["client_order_ref"])
	}
}

// Una cuenta sin bodegas asignadas tiene que seguir dejando que decida Odoo:
// mandar warehouse_id 0 crearía el pedido contra una bodega inexistente, que
// es peor que el comportamiento de hoy.
func TestValoresPedidoSinBodegaNoMandaLaClave(t *testing.T) {
	valores := valoresPedido(store.Orden{Canal: "shopify"}, 1, "SHOPIFY-1", nil, 0, 1)

	if _, hay := valores["warehouse_id"]; hay {
		t.Error("sin bodegas asignadas no se debe mandar warehouse_id: decide Odoo")
	}
}

// Ningún canal normaliza el estado del pedido: cada uno manda su vocabulario
// tal cual. Sin este mapeo, una cancelación pasaba por venta viva, el stock
// apartado no volvía nunca y el pedido se montaba igual en Odoo.
func TestEsCancelado(t *testing.T) {
	casos := []struct {
		estado    string
		cancelado bool
		porque    string
	}{
		{"cancelled", true, "MercadoLibre, WooCommerce y Shopify"},
		{"canceled", true, "la variante con una sola l"},
		{"CANCELLED", true, "el canal puede mandarlo en mayúsculas"},
		{" cancelled ", true, "con espacios alrededor"},
		{"invalid", true, "MercadoLibre marca así el pedido fraudulento"},
		{"refunded", true, "WooCommerce y el financial_status de Shopify"},
		{"voided", true, "Shopify: se anuló sin cobrar"},
		{"paid", false, "una venta viva"},
		{"delivered", false, "entregado no es cancelado"},
		{"failed", false, "pago rechazado que el comprador reintenta"},
		{"partially_refunded", false, "devolvió dinero, no la mercancía entera"},
		{"pending,canceled", false, "Falabella con una unidad anulada y otra viva: sigue habiendo qué despachar"},
		{"", false, "un canal que no reporte estado: no se puede suponer"},
	}
	for _, c := range casos {
		if got := esCancelado(c.estado); got != c.cancelado {
			t.Errorf("esCancelado(%q) = %v, se esperaba %v: %s",
				c.estado, got, c.cancelado, c.porque)
		}
	}
}

// ---------------------------------------------------------------------------
// Montaje del pedido en Odoo, con Odoo simulado.
//
// Las tres cosas que estas pruebas vigilan salían mal en TODOS los pedidos, no
// en casos raros, y ninguna daba error:
//
//  1. la línea viajaba sin tax_id, así que Odoo le sumaba su 15 % encima de un
//     precio que ya lo llevaba dentro y el total del pedido no cuadraba con lo
//     que cobró el canal;
//  2. el documento de identidad del comprador y media dirección se ingerían y
//     se tiraban al escribir el res.partner;
//  3. el pedido salía sin pricelist_id, así que la moneda la decidía la tarifa
//     del cliente y no el canal —y currency_id no se puede corregir después—.
// ---------------------------------------------------------------------------

// El comando x2many (6, 0, []) tal como lo serializa el codificador XML-RPC del
// proyecto: «la línea no lleva ningún impuesto». Se compara la cadena entera y
// no solo el nombre del campo porque mandar tax_id con cualquier otro
// contenido volvería a inflar el total.
const taxIDVacio = `<member><name>tax_id</name><value><array><data>` +
	`<value><array><data><value><int>6</int></value><value><int>0</int></value>` +
	`<value><array><data></data></array></value></data></array></value>` +
	`</data></array></value></member>`

// -------------------------------------------------------------- Odoo simulado

type odooFalso struct {
	*httptest.Server

	// tarifas son las product.pricelist que dice tener la instancia, por
	// moneda. La base real de MDV tiene tres en COP y una en USD.
	tarifas map[string]int64
	// totalDevuelto es el amount_total con el que Odoo responde al releer el
	// pedido. Cero significa «el que mandó Integra».
	totalDevuelto float64
	monedaOdoo    string

	mu      sync.Mutex
	cuerpos map[string]string // modelo → cuerpo del último create
	creados []string          // modelos creados, en orden
}

func nuevoOdoo(t *testing.T) *odooFalso {
	t.Helper()
	o := &odooFalso{
		tarifas:    map[string]int64{"COP": 4},
		monedaOdoo: "COP",
		cuerpos:    map[string]string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/xmlrpc/2/common", func(w http.ResponseWriter, r *http.Request) {
		responder(w, int64(2))
	})
	mux.HandleFunc("/xmlrpc/2/object", func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		o.despachar(t, w, string(cuerpo))
	})
	o.Server = httptest.NewServer(mux)
	t.Cleanup(o.Close)
	return o
}

// abrir devuelve el inyector que usa el Servicio para hablar con este Odoo.
func (o *odooFalso) abrir(t *testing.T) func(context.Context) (*odoo.Client, error) {
	t.Helper()
	return func(context.Context) (*odoo.Client, error) {
		return odoo.Connect(odoo.Config{
			URL: o.URL, Database: "mdv_replica", Username: "admin", APIKey: "admin",
		})
	}
}

// creado devuelve el cuerpo XML-RPC del create de un modelo, tal cual salió.
func (o *odooFalso) creado(modelo string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cuerpos[modelo]
}

func (o *odooFalso) seCreo(modelo string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, m := range o.creados {
		if m == modelo {
			return true
		}
	}
	return false
}

func (o *odooFalso) anotarCreate(modelo, cuerpo string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cuerpos[modelo] = cuerpo
	o.creados = append(o.creados, modelo)
}

func (o *odooFalso) despachar(t *testing.T, w http.ResponseWriter, cuerpo string) {
	pide := func(s string) bool { return strings.Contains(cuerpo, "<string>"+s+"</string>") }

	switch {
	// Idempotencia: no existe ningún pedido con esa referencia.
	case pide("sale.order") && pide("search_read") && strings.Contains(cuerpo, "client_order_ref"):
		responder(w, []interface{}{})

	// Relectura del pedido recién creado para contrastarlo con el canal.
	case pide("sale.order") && pide("search_read"):
		responder(w, []interface{}{map[string]interface{}{
			"id":           int64(900),
			"amount_total": o.totalDevuelto,
			"currency_id":  []interface{}{int64(8), o.monedaOdoo},
		}})

	case pide("sale.order") && pide("create"):
		o.anotarCreate("sale.order", cuerpo)
		responder(w, int64(900))

	// El comprador no existe todavía: se crea.
	case pide("res.partner") && pide("search_read"):
		responder(w, []interface{}{})

	case pide("res.partner") && pide("create"):
		o.anotarCreate("res.partner", cuerpo)
		responder(w, int64(500))

	case pide("res.country.state"):
		// Odoo guarda el código («ANT») y el nombre («Antioquia»).
		if pide("ANT") || pide("Antioquia") {
			responder(w, []interface{}{map[string]interface{}{"id": int64(651)}})
			return
		}
		responder(w, []interface{}{})

	case pide("res.country"):
		if pide("CO") || pide("Colombia") {
			responder(w, []interface{}{map[string]interface{}{"id": int64(49)}})
			return
		}
		responder(w, []interface{}{})

	case pide("product.pricelist"):
		for moneda, id := range o.tarifas {
			if pide(moneda) {
				responder(w, []interface{}{map[string]interface{}{
					"id": id, "name": "Tarifa " + moneda,
				}})
				return
			}
		}
		responder(w, []interface{}{})

	default:
		t.Errorf("el Odoo simulado no esperaba esta llamada: %s", cuerpo)
		responder(w, []interface{}{})
	}
}

func responder(w http.ResponseWriter, v interface{}) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><methodResponse><params><param>`)
	escribirValor(&b, v)
	b.WriteString(`</param></params></methodResponse>`)
	w.Header().Set("Content-Type", "text/xml")
	_, _ = io.WriteString(w, b.String())
}

func escribirValor(b *strings.Builder, v interface{}) {
	b.WriteString("<value>")
	defer b.WriteString("</value>")

	switch t := v.(type) {
	case nil:
		b.WriteString("<nil/>")
	case bool:
		if t {
			b.WriteString("<boolean>1</boolean>")
		} else {
			b.WriteString("<boolean>0</boolean>")
		}
	case int64:
		fmt.Fprintf(b, "<int>%d</int>", t)
	case float64:
		fmt.Fprintf(b, "<double>%g</double>", t)
	case string:
		b.WriteString("<string>")
		_ = xml.EscapeText(b, []byte(t))
		b.WriteString("</string>")
	case []interface{}:
		b.WriteString("<array><data>")
		for _, item := range t {
			escribirValor(b, item)
		}
		b.WriteString("</data></array>")
	case map[string]interface{}:
		b.WriteString("<struct>")
		for k, val := range t {
			b.WriteString("<member><name>" + k + "</name>")
			escribirValor(b, val)
			b.WriteString("</member>")
		}
		b.WriteString("</struct>")
	default:
		panic(fmt.Sprintf("el codificador de la prueba no sabe escribir %T", v))
	}
}

// ------------------------------------------------------------- base simulada

type alertaFalsa struct {
	tipo    string
	mensaje string
}

// tiendaFalsa sustituye a PostgreSQL: devuelve el pedido de la prueba y anota
// lo que el montaje le pide.
type tiendaFalsa struct {
	comprador store.DatosComprador
	direccion DireccionEnvio
	productos map[int64]int64 // variante de Integra → product.product de Odoo

	creada  bool
	odooID  int64
	partner int64
	fallos  []string
	alertas []alertaFalsa

	// Lo que responde a la ingesta. Con los ceros la base está "vacía": el
	// pedido es nuevo, no hay stock que mover y ninguna otra cuenta tiene la
	// variante publicada.
	yaEstaba    bool    // GuardarOrden dice que el pedido ya existía
	descontadas float64 // unidades que DescontarStockPublicado dice haber bajado
	devueltas   float64 // unidades que DevolverStockReservado dice haber devuelto
	destinos    []store.DestinoStock
	errDestinos error
	guardados   int64 // pedidos guardados; numera los ids que devuelve
}

func nuevaTienda() *tiendaFalsa {
	return &tiendaFalsa{
		comprador: store.DatosComprador{
			Nombre: "Ana Pérez", Email: "ana@example.com", Telefono: "3001234567",
			Documento: "CC-1020304050", Ciudad: "Medellín", Direccion: "Calle 10 # 20-30",
		},
		direccion: DireccionEnvio{
			Linea2: "Apto 302", Departamento: "Antioquia",
			CodigoPostal: "050021", Pais: "CO",
		},
		productos: map[int64]int64{55: 188},
	}
}

func (t *tiendaFalsa) DatosCompradorDeOrden(context.Context, int64) (*store.DatosComprador, error) {
	d := t.comprador
	return &d, nil
}

func (t *tiendaFalsa) DireccionEnvioDeOrden(context.Context, int64) (DireccionEnvio, error) {
	return t.direccion, nil
}

func (t *tiendaFalsa) OdooProductIDDeVariante(_ context.Context, varianteID int64) (int64, error) {
	return t.productos[varianteID], nil
}

func (t *tiendaFalsa) BodegaDeOrden(context.Context, int64) (*store.BodegaCuenta, error) {
	return nil, nil // sin bodegas asignadas: decide Odoo
}

func (t *tiendaFalsa) MarcarOrdenCreada(_ context.Context, _, odooPedidoID, odooPartnerID int64) error {
	t.creada, t.odooID, t.partner = true, odooPedidoID, odooPartnerID
	return nil
}

func (t *tiendaFalsa) MarcarOrdenFallida(_ context.Context, _ int64, causa string) error {
	t.fallos = append(t.fallos, causa)
	return nil
}

func (t *tiendaFalsa) CrearAlerta(_ context.Context, tipo, _ string, _ *int64, mensaje string, _ any) error {
	t.alertas = append(t.alertas, alertaFalsa{tipo: tipo, mensaje: mensaje})
	return nil
}

// La parte de la interfaz que usa la ingesta: responde lo que se le programó.
func (t *tiendaFalsa) GuardarOrden(context.Context, store.DatosOrden) (int64, bool, error) {
	t.guardados++
	return t.guardados, !t.yaEstaba, nil
}
func (t *tiendaFalsa) DescontarStockPublicado(context.Context, int64) (float64, error) {
	return t.descontadas, nil
}
func (t *tiendaFalsa) DevolverStockReservado(context.Context, int64, string) (float64, error) {
	return t.devueltas, nil
}
func (t *tiendaFalsa) DestinosDeStockDeOrden(context.Context, int64) ([]store.DestinoStock, error) {
	return t.destinos, t.errDestinos
}

// El resto no interviene ni en el montaje ni en lo que se prueba de la ingesta.
func (t *tiendaFalsa) WatermarkOrdenes(context.Context, int64) (time.Time, error) {
	return time.Time{}, nil
}
func (t *tiendaFalsa) ActualizarWatermarkOrdenes(context.Context, int64, time.Time, string) error {
	return nil
}
func (t *tiendaFalsa) MarcarOrdenCancelada(context.Context, int64, string) (store.Cancelacion, error) {
	return store.Cancelacion{}, nil
}
func (t *tiendaFalsa) OrdenPendientePorID(context.Context, int64) (*store.Orden, error) {
	return nil, nil
}

// ------------------------------------------------------------------ pruebas

func idDe(n int64) *int64 { return &n }

// Un pedido de MercadoLibre: una línea a 115.000 pesos, que es lo que pagó el
// comprador con el IVA ya dentro.
func ordenDePrueba() store.Orden {
	return store.Orden{
		ID: 7, CuentaID: 3, Canal: "mercadolibre", Numero: "ML-1",
		ExternalID: "2000001", Moneda: "COP", Total: 115000,
		FechaPedido: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
		Lineas: []store.LineaOrden{{
			ID: 1, SKU: "COL-101", Titulo: "Colchón Alfa", Cantidad: 1,
			PrecioUnit: 115000, Total: 115000, VarianteID: idDe(55),
		}},
	}
}

func montar(t *testing.T, srv *odooFalso, st *tiendaFalsa, o store.Orden) error {
	t.Helper()
	if srv.totalDevuelto == 0 {
		for _, l := range o.Lineas {
			srv.totalDevuelto += l.Cantidad * l.PrecioUnit
		}
	}
	return nuevoCon(st, slog.New(slog.DiscardHandler), srv.abrir(t)).
		CrearPedido(context.Background(), o)
}

func recorte(s string) string {
	if len(s) > 1200 {
		return s[:1200] + "…"
	}
	return s
}

// price_unit es el precio final del canal, con el impuesto dentro. Sin tax_id
// explícito, Odoo lo rellena solo desde product.taxes_id —el campo es
// compute/store/readonly=False/precompute, y el ORM solo precalcula lo que no
// viene en los valores de create— y suma un 15 % encima: el amount_total del
// pedido queda inflado respecto de lo que cobró el marketplace.
func TestLaLineaLlegaSinImpuestosParaQueOdooNoSumeEncima(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()

	if err := montar(t, srv, st, ordenDePrueba()); err != nil {
		t.Fatalf("montando el pedido: %v", err)
	}

	cuerpo := srv.creado("sale.order")
	if !strings.Contains(cuerpo, taxIDVacio) {
		t.Errorf("la línea del pedido no lleva tax_id vacío, así que Odoo le sumará "+
			"su impuesto sobre un precio que ya lo incluye:\n%s", recorte(cuerpo))
	}
	if !st.creada {
		t.Error("el pedido no quedó anotado como creado")
	}
}

// El documento y media dirección se ingerían y se perdían justo al escribir el
// contacto: los compradores nacían sin identificación fiscal —imposible
// facturarles electrónicamente en Colombia— y con la dirección a medias.
func TestElCompradorLlegaAOdooConDocumentoYDireccionCompleta(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()

	if err := montar(t, srv, st, ordenDePrueba()); err != nil {
		t.Fatalf("montando el pedido: %v", err)
	}

	cuerpo := srv.creado("res.partner")
	casos := []struct {
		campo     string
		fragmento string
		porque    string
	}{
		{"vat", `<member><name>vat</name><value><string>CC-1020304050</string></value></member>`,
			"sin identificación fiscal no se puede emitir la factura electrónica"},
		{"street2", `<member><name>street2</name><value><string>Apto 302</string></value></member>`,
			"el albarán sale con la dirección incompleta"},
		{"zip", `<member><name>zip</name><value><string>050021</string></value></member>`,
			"el albarán sale sin código postal"},
		{"country_id", `<member><name>country_id</name><value><int>49</int></value></member>`,
			"el país se resuelve por el código ISO que manda el canal"},
		{"state_id", `<member><name>state_id</name><value><int>651</int></value></member>`,
			"el departamento se resuelve dentro del país"},
	}
	for _, c := range casos {
		if !strings.Contains(cuerpo, c.fragmento) {
			t.Errorf("el res.partner no lleva %s: %s\ncuerpo: %s", c.campo, c.porque, recorte(cuerpo))
		}
	}
}

// sale.order.currency_id es de solo lectura: la moneda sale de la tarifa, y la
// tarifa —si no se manda— sale del cliente. Un partner con la tarifa en USD
// convertía un pedido de 115.000 COP en 115.000 USD sin dar un solo error.
func TestElPedidoLlevaLaTarifaDeLaMonedaDelCanal(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()

	if err := montar(t, srv, st, ordenDePrueba()); err != nil {
		t.Fatalf("montando el pedido: %v", err)
	}

	esperado := `<member><name>pricelist_id</name><value><int>4</int></value></member>`
	if !strings.Contains(srv.creado("sale.order"), esperado) {
		t.Errorf("el pedido no lleva pricelist_id: la moneda la decidiría la tarifa "+
			"del cliente, y currency_id no se puede corregir después:\n%s",
			recorte(srv.creado("sale.order")))
	}
}

// Sin tarifa en la moneda del canal no hay forma de fijar la moneda del
// pedido. Crearlo igual sería registrarlo en la moneda equivocada, que es
// justo el fallo silencioso que hay que evitar: mejor fallar y que se vea.
func TestSinTarifaEnLaMonedaDelCanalNoSeCreaElPedido(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()
	o := ordenDePrueba()
	o.Moneda = "USD" // la instancia solo tiene tarifa en COP

	err := montar(t, srv, st, o)
	if err == nil {
		t.Fatal("se creó el pedido sin tarifa en la moneda del canal")
	}
	if !strings.Contains(err.Error(), "USD") {
		t.Errorf("el error no dice qué moneda falta: %v", err)
	}
	if srv.seCreo("sale.order") {
		t.Error("se llegó a crear el sale.order pese a no poder fijar la moneda")
	}
	if len(st.fallos) == 0 {
		t.Error("el pedido no quedó marcado como fallido: nadie se enteraría")
	}
}

// La contrapartida de crear en borrador: Odoo puede alterar el total con una
// posición fiscal o un impuesto por defecto sin dar error. Integra no lo
// corrige —es un documento contable— pero deja constancia antes de que alguien
// confirme el borrador.
func TestSeAvisaSiElTotalDeOdooNoCuadraConElCanal(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()
	srv.totalDevuelto = 132250 // 115.000 + 15 %: Odoo sumó su impuesto igualmente

	if err := montar(t, srv, st, ordenDePrueba()); err != nil {
		t.Fatalf("montando el pedido: %v", err)
	}

	if len(st.alertas) == 0 {
		t.Fatal("Odoo devolvió un total distinto del cobrado y no se avisó a nadie")
	}
	a := st.alertas[0]
	if a.tipo != AlertaDescuadrePedido {
		t.Errorf("tipo de alerta = %q, se esperaba %q", a.tipo, AlertaDescuadrePedido)
	}
	if !strings.Contains(a.mensaje, "132250") || !strings.Contains(a.mensaje, "115000") {
		t.Errorf("la alerta no dice los dos importes: %s", a.mensaje)
	}
	// El pedido existe en Odoo: el descuadre avisa, no revierte.
	if !st.creada {
		t.Error("el pedido dejó de anotarse como creado por culpa de la comprobación")
	}
}

// Y si cuadra, silencio: una alerta por pedido sería ruido y acabaría
// ignorándose justo cuando importa.
func TestUnPedidoQueCuadraNoGeneraAlerta(t *testing.T) {
	srv, st := nuevoOdoo(t), nuevaTienda()

	if err := montar(t, srv, st, ordenDePrueba()); err != nil {
		t.Fatalf("montando el pedido: %v", err)
	}
	if len(st.alertas) != 0 {
		t.Errorf("se alertó de un pedido que cuadra: %+v", st.alertas)
	}
}

// ---------------------------------------------------------------------------
// Ingesta: lo que una venta deja pedido para los demás canales.
//
// El stock bajaba en la base al instante (DescontarStockPublicado), pero nadie
// encolaba su envío a las otras cuentas: el diff por hash solo corre desde el
// horario, una vez al día, así que la última unidad vendida en un canal se
// seguía ofreciendo en los otros tres hasta la corrida siguiente.
// ---------------------------------------------------------------------------

// canalFalso entrega los pedidos que se le programen y no habla con nadie.
type canalFalso struct{ pedidos []channel.Order }

func (c *canalFalso) Kind() channel.Kind                 { return channel.WooCommerce }
func (c *canalFalso) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (c *canalFalso) Publish(context.Context, channel.PublishRequest) (channel.PublishResult, error) {
	return channel.PublishResult{}, nil
}
func (c *canalFalso) Update(context.Context, channel.UpdateRequest) (channel.UpdateResult, error) {
	return channel.UpdateResult{}, nil
}
func (c *canalFalso) UpdateStock(context.Context, []channel.StockUpdate) ([]channel.OpResult, error) {
	return nil, nil
}
func (c *canalFalso) UpdatePrice(context.Context, []channel.PriceUpdate) ([]channel.OpResult, error) {
	return nil, nil
}
func (c *canalFalso) Pause(context.Context, channel.ExternalRef) error  { return nil }
func (c *canalFalso) Resume(context.Context, channel.ExternalRef) error { return nil }
func (c *canalFalso) FetchStatus(context.Context, []channel.ExternalRef) ([]channel.ListingStatus, error) {
	return nil, nil
}
func (c *canalFalso) ListRemote(context.Context, channel.Cursor) (channel.RemotePage, error) {
	return channel.RemotePage{Done: true}, nil
}
func (c *canalFalso) FetchOrders(context.Context, time.Time, channel.Cursor) (channel.OrderPage, error) {
	return channel.OrderPage{Orders: c.pedidos, Done: true}, nil
}
func (c *canalFalso) AckOrder(context.Context, channel.ExternalRef, channel.Fulfillment) error {
	return nil
}

// colaFalsa anota cada trabajo pedido con su carga y sus opciones: lo que se
// comprueba es exactamente qué queda en la cola, no solo que haya algo.
type colaFalsa struct{ trabajos []trabajoPedido }

type trabajoPedido struct {
	kind    string
	payload any
	op      jobs.Opciones
}

func (c *colaFalsa) Encolar(_ context.Context, kind string, payload any, op jobs.Opciones) (int64, error) {
	c.trabajos = append(c.trabajos, trabajoPedido{kind: kind, payload: payload, op: op})
	return int64(len(c.trabajos)), nil
}

// stocks devuelve los envíos de stock pedidos, por cuenta y variante.
func (c *colaFalsa) stocks() map[publicar.PayloadPublicar]jobs.Opciones {
	out := map[publicar.PayloadPublicar]jobs.Opciones{}
	for _, t := range c.trabajos {
		if t.kind == publicar.TrabajoStock {
			out[t.payload.(publicar.PayloadPublicar)] = t.op
		}
	}
	return out
}

func (c *colaFalsa) cuantos(kind string) int {
	n := 0
	for _, t := range c.trabajos {
		if t.kind == kind {
			n++
		}
	}
	return n
}

// venta es un pedido de WooCommerce de una unidad del SKU COL-101 (la
// variante 55 de la base simulada), tal como lo entregaría el adaptador.
func venta(numero, estado string) channel.Order {
	return channel.Order{
		ExternalID: "WC-" + numero, Number: numero, Status: estado,
		OrderedAt: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
		Currency:  "COP", Total: 115000,
		Buyer: channel.Buyer{Name: "Ana Pérez", Email: "ana@example.com"},
		Lines: []channel.OrderLine{{
			ExternalID: "L1", SKU: "COL-101", Title: "Colchón Alfa",
			Quantity: 1, UnitPrice: 115000, TotalPrice: 115000,
		}},
	}
}

// ingerirEn corre la ingesta de la cuenta 3 contra el canal simulado y
// devuelve la cola con lo que dejó pedido.
func ingerirEn(t *testing.T, st *tiendaFalsa, pedidos ...channel.Order) *colaFalsa {
	t.Helper()
	cola := &colaFalsa{}
	s := nuevoCon(st, slog.New(slog.DiscardHandler), nil)
	s.cola = cola
	s.adaptador = func(context.Context, int64) (channel.Adapter, error) {
		return &canalFalso{pedidos: pedidos}, nil
	}
	if err := s.ingerir(context.Background(), jobs.Trabajo{
		Kind: TrabajoIngerir, Payload: []byte(`{"cuenta_id":3}`),
	}); err != nil {
		t.Fatalf("ingiriendo: %v", err)
	}
	return cola
}

// Las otras dos cuentas tienen publicada la variante: la venta tiene que
// dejarles pedido el envío de stock, con la clave y la prioridad del
// planificador. Y dos pedidos de la misma variante en la misma tanda piden un
// solo envío, que al ejecutarse lee el stock ya descontado por los dos.
func TestUnaVentaEncolaElStockDeSuVarianteEnLasOtrasCuentas(t *testing.T) {
	st := nuevaTienda()
	st.descontadas = 1
	st.destinos = []store.DestinoStock{{CuentaID: 4, VarianteID: 55}, {CuentaID: 5, VarianteID: 55}}

	cola := ingerirEn(t, st, venta("1001", "processing"), venta("1002", "processing"))

	stocks := cola.stocks()
	if len(stocks) != 2 {
		t.Fatalf("se esperaban envíos de stock a las cuentas 4 y 5, y se pidió: %+v", cola.trabajos)
	}
	for _, cuenta := range []int64{4, 5} {
		op, hay := stocks[publicar.PayloadPublicar{CuentaID: cuenta, VarianteID: 55}]
		if !hay {
			t.Errorf("la cuenta %d tiene la variante publicada y no se le pidió el stock", cuenta)
			continue
		}
		clave := fmt.Sprintf("%s:%d:55", publicar.TrabajoStock, cuenta)
		if op.UniqueKey != clave {
			t.Errorf("clave única %q, se esperaba %q: sin la clave del planificador el mismo envío iría dos veces",
				op.UniqueKey, clave)
		}
		if op.Priority != 10 {
			t.Errorf("prioridad %d: el stock va por delante de todo, con 10", op.Priority)
		}
		if op.CuentaID != cuenta {
			t.Errorf("el trabajo no queda atado a la cuenta %d: %+v", cuenta, op)
		}
	}
	if n := cola.cuantos(TrabajoAOdoo); n != 2 {
		t.Errorf("cada pedido nuevo sigue pidiendo su montaje en Odoo, y se pidieron %d", n)
	}
}

// Lo contrario también cuenta: una cancelación devuelve la unidad a la base y,
// si no se manda, los demás canales siguen sin ofrecerla hasta el día
// siguiente. Es una venta que no se hace.
func TestUnaCancelacionEncolaElStockDevueltoEnLasOtrasCuentas(t *testing.T) {
	st := nuevaTienda()
	st.yaEstaba = true // el pedido se ingirió vivo en una pasada anterior
	st.devueltas = 1
	st.destinos = []store.DestinoStock{{CuentaID: 4, VarianteID: 55}}

	cola := ingerirEn(t, st, venta("1003", "cancelled"))

	if _, hay := cola.stocks()[publicar.PayloadPublicar{CuentaID: 4, VarianteID: 55}]; !hay {
		t.Errorf("el stock devuelto por la cancelación no se pidió a la cuenta 4: %+v", cola.trabajos)
	}
	if n := cola.cuantos(TrabajoAOdoo); n != 0 {
		t.Errorf("un pedido cancelado no se monta en Odoo, y se pidió %d veces", n)
	}
}

// Sin movimiento de stock no hay nada que mandar: un pedido repetido por el
// sondeo, una venta de algo que ya estaba en cero o una cancelación cuyas
// unidades ya habían vuelto. Y si la base no sabe decir a quién avisar, la
// ingesta sigue: el pedido ya está guardado y el horario reconcilia por hash.
func TestSinStockQueMoverNoSeEncolaNingunEnvio(t *testing.T) {
	casos := []struct {
		nombre string
		tienda func(*tiendaFalsa)
		pedido channel.Order
	}{
		{"pedido repetido", func(t *tiendaFalsa) { t.yaEstaba = true; t.descontadas = 1 },
			venta("2001", "processing")},
		{"venta sin stock que descontar", func(t *tiendaFalsa) { t.descontadas = 0 },
			venta("2002", "processing")},
		{"cancelación ya devuelta", func(t *tiendaFalsa) { t.yaEstaba = true; t.devueltas = 0 },
			venta("2003", "cancelled")},
		{"la base no sabe a quién avisar", func(t *tiendaFalsa) {
			t.descontadas = 1
			t.errDestinos = fmt.Errorf("conexión perdida")
		}, venta("2004", "processing")},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			st := nuevaTienda()
			st.destinos = []store.DestinoStock{{CuentaID: 4, VarianteID: 55}}
			c.tienda(st)

			cola := ingerirEn(t, st, c.pedido)

			if n := cola.cuantos(publicar.TrabajoStock); n != 0 {
				t.Errorf("se pidieron %d envíos de stock sin que el stock se moviera: %+v", n, cola.trabajos)
			}
		})
	}
}
