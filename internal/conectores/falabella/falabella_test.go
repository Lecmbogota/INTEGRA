package falabella

import (
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// servidorFalso imita lo justo del Seller Center para probar el adaptador sin
// red y sin credenciales. Toda la API vive en una sola ruta y se despacha por
// el parámetro Action, así que el servidor hace lo mismo: cada prueba registra
// la respuesta de las acciones que le importan.
type servidorFalso struct {
	t *testing.T
	*httptest.Server

	mu        sync.Mutex
	acciones  map[string]func(*http.Request) string
	llamadas  []llamada
	feedFalso string // RequestId que devuelven las escrituras
}

type llamada struct {
	Accion string
	Query  url.Values
	Cuerpo string
}

func nuevoServidor(t *testing.T) *servidorFalso {
	t.Helper()
	s := &servidorFalso{t: t, acciones: map[string]func(*http.Request) string{}, feedFalso: "FEED-1"}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		accion := r.URL.Query().Get("Action")

		s.mu.Lock()
		s.llamadas = append(s.llamadas, llamada{Accion: accion, Query: r.URL.Query(), Cuerpo: string(cuerpo)})
		h := s.acciones[accion]
		s.mu.Unlock()

		if h == nil {
			t.Errorf("el adaptador llamó a una acción sin respuesta preparada: %q", accion)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, h(r))
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *servidorFalso) responde(accion, cuerpo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acciones[accion] = func(*http.Request) string { return cuerpo }
}

func (s *servidorFalso) respondeCon(accion string, f func(*http.Request) string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acciones[accion] = f
}

// escrituraOK prepara ProductCreate/ProductUpdate para que devuelvan el feed en
// Head.RequestId (donde lo pone Falabella) y FeedStatus para que lo confirme.
func (s *servidorFalso) escrituraOK() {
	respuesta := `{"SuccessResponse":{"Head":{"RequestId":"` + s.feedFalso +
		`","RequestAction":"ProductCreate"},"Body":{}}}`
	s.responde("ProductCreate", respuesta)
	s.responde("ProductUpdate", respuesta)
	s.feedStatus(`{"Feed":"` + s.feedFalso + `","Status":"Finished","TotalRecords":"1",` +
		`"ProcessedRecords":"1","FailedRecords":"0","FeedErrors":""}`)
}

// feedStatus responde FeedStatus solo si le piden el feed que se emitió: así la
// prueba detecta que el identificador se sacó de Head.RequestId y no de un
// Body.FeedId que llega vacío.
func (s *servidorFalso) feedStatus(detalle string) {
	s.respondeCon("FeedStatus", func(r *http.Request) string {
		if r.URL.Query().Get("FeedID") != s.feedFalso {
			return `{"ErrorResponse":{"Head":{"ErrorCode":"12","ErrorMessage":"E012: Feed not found"}}}`
		}
		return `{"SuccessResponse":{"Body":{"FeedDetail":` + detalle + `}}}`
	})
}

func (s *servidorFalso) llamadasDe(accion string) []llamada {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []llamada
	for _, l := range s.llamadas {
		if l.Accion == accion {
			out = append(out, l)
		}
	}
	return out
}

func (s *servidorFalso) adaptador() *Adaptador {
	return &Adaptador{
		userID: "vendedor@mdv.co", apiKey: "clave", operador: "faco",
		base: s.URL + "/", cli: s.Client(),
		// Sin esperas: la confirmación del feed no debe dormir en las pruebas.
		esperas: []time.Duration{0, 0},
	}
}

// productoLeido es el XML que se recibió, decodificado para poder afirmar
// dónde quedó cada campo. Los atributos de la categoría se recogen con ",any"
// porque su nombre de elemento es el nombre del atributo.
type productoLeido struct {
	SellerSku       string `xml:"SellerSku"`
	Name            string `xml:"Name"`
	Description     string `xml:"Description"`
	PrimaryCategory string `xml:"PrimaryCategory"`
	ProductId       string `xml:"ProductId"`
	PrecioPlano     string `xml:"Price"`
	StockPlano      string `xml:"Quantity"`
	EstadoPlano     string `xml:"Status"`
	PesoPlano       string `xml:"PackageWeight"`
	Unidades        struct {
		Unidad []struct {
			OperatorCode    string `xml:"OperatorCode"`
			Price           string `xml:"Price"`
			SpecialPrice    string `xml:"SpecialPrice"`
			SpecialFromDate string `xml:"SpecialFromDate"`
			SpecialToDate   string `xml:"SpecialToDate"`
			Stock           string `xml:"Stock"`
			Status          string `xml:"Status"`
		} `xml:"BusinessUnit"`
	} `xml:"BusinessUnits"`
	Datos struct {
		ConditionType string `xml:"ConditionType"`
		PackageWeight string `xml:"PackageWeight"`
		Atributos     []struct {
			XMLName xml.Name
			Valor   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"ProductData"`
	Imagenes []string `xml:"Images>Image"`
}

func leerFeed(t *testing.T, cuerpo string) []productoLeido {
	t.Helper()
	var req struct {
		Producto []productoLeido `xml:"Product"`
	}
	if err := xml.Unmarshal([]byte(cuerpo), &req); err != nil {
		t.Fatalf("el feed enviado no es XML válido: %v\n%s", err, cuerpo)
	}
	return req.Producto
}

func (p productoLeido) atributo(nombre string) string {
	for _, a := range p.Datos.Atributos {
		if a.XMLName.Local == nombre {
			return a.Valor
		}
	}
	return ""
}

func productoDePrueba() channel.Product {
	return channel.Product{
		SKU: "AO-NU-1001", Title: "Anillo inteligente", Description: "Mide el sueño",
		Brand: "RingConn", CategoryID: "811", Weight: 0.85,
		Attributes: map[string]string{"Talla": "M", "Color": "Negro", "vacio": ""},
		Images:     []channel.Image{{URL: "https://mdv.co/1.jpg"}},
		Variants: []channel.Variant{{
			SKU: "AO-NU-1001", Barcode: "4066749430450",
			RegularPrice: 129900, Quantity: 7, Currency: "COP",
		}},
	}
}

// TestPublishLeeGetProductsAnidado: la respuesta real anida Body.Products.Product
// y colapsa el array a objeto con un solo resultado. Leyéndola como array,
// existeSKU fallaba con "cannot unmarshal object into Go struct field" y no se
// publicaba ni un producto.
func TestPublishLeeGetProductsAnidado(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Head":{"RequestAction":"GetProducts"},
		"Body":{"Products":{"Product":{"SellerSku":"OTRO-9","ShopSku":"9",
		"BusinessUnits":{"BusinessUnit":{"OperatorCode":"faco","Price":"1000.00","Stock":"1","Status":"active"}}}}}}}`)
	s.escrituraOK()

	res, err := s.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba()})
	if err != nil {
		t.Fatalf("Publish devolvió error: %v", err)
	}
	if res.Ref.SKU != "AO-NU-1001" || res.Adopted {
		t.Fatalf("se esperaba una publicación nueva del SKU, y llegó %+v", res)
	}
	if n := len(s.llamadasDe("ProductCreate")); n != 1 {
		t.Fatalf("se esperaba un ProductCreate y hubo %d", n)
	}
}

// TestPublishMandaPrecioStockYPesoEnSuSitio: precio, stock y estado van dentro
// de BusinessUnits y el peso dentro de ProductData. Como hijos directos de
// Product, Falabella acepta el feed y no aplica nada.
func TestPublishMandaPrecioStockYPesoEnSuSitio(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":{"Product":[]}}}}`)
	s.escrituraOK()

	p := productoDePrueba()
	inicia := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	p.Variants[0].SalePrice = 99900
	p.Variants[0].SaleStartsAt = &inicia

	if _, err := s.adaptador().Publish(context.Background(), channel.PublishRequest{Product: p}); err != nil {
		t.Fatalf("Publish devolvió error: %v", err)
	}

	envios := s.llamadasDe("ProductCreate")
	if len(envios) != 1 {
		t.Fatalf("se esperaba un ProductCreate y hubo %d", len(envios))
	}
	prods := leerFeed(t, envios[0].Cuerpo)
	if len(prods) != 1 {
		t.Fatalf("se esperaba un producto en el feed y hay %d", len(prods))
	}
	pr := prods[0]

	if pr.PrecioPlano != "" || pr.StockPlano != "" || pr.EstadoPlano != "" || pr.PesoPlano != "" {
		t.Errorf("el feed sigue llevando campos planos: precio=%q stock=%q estado=%q peso=%q",
			pr.PrecioPlano, pr.StockPlano, pr.EstadoPlano, pr.PesoPlano)
	}
	if len(pr.Unidades.Unidad) != 1 {
		t.Fatalf("se esperaba una unidad de negocio y hay %d", len(pr.Unidades.Unidad))
	}
	u := pr.Unidades.Unidad[0]
	if u.OperatorCode != "faco" || u.Price != "129900.00" || u.Stock != "7" || u.Status != "inactive" {
		t.Errorf("la unidad de negocio llegó mal: %+v", u)
	}
	if u.SpecialPrice != "99900.00" || u.SpecialFromDate != "2026-09-01 00:00:00" {
		t.Errorf("la oferta llegó mal: %+v", u)
	}
	if pr.Datos.PackageWeight != "0.850" {
		t.Errorf("el peso tiene que ir en ProductData y llegó %q", pr.Datos.PackageWeight)
	}
	if pr.Datos.ConditionType != "Nuevo" {
		t.Errorf("ConditionType es obligatorio y llegó %q", pr.Datos.ConditionType)
	}
}

// TestPublishMandaLosAtributosObligatorios: el núcleo valida los atributos de
// la categoría y los deja en Product.Attributes; sin sitio donde ponerlos, el
// XML salía sin ProductData y Falabella rechazaba el feed después.
func TestPublishMandaLosAtributosObligatorios(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":""}}}`)
	s.escrituraOK()

	if _, err := s.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba()}); err != nil {
		t.Fatalf("Publish devolvió error: %v", err)
	}
	pr := leerFeed(t, s.llamadasDe("ProductCreate")[0].Cuerpo)[0]
	if pr.atributo("Talla") != "M" || pr.atributo("Color") != "Negro" {
		t.Fatalf("los atributos de la categoría no viajaron en ProductData: %+v", pr.Datos.Atributos)
	}
	// Un atributo sin valor no se manda: Seller Center rechaza el elemento vacío.
	if pr.atributo("vacio") != "" {
		t.Errorf("se mandó un atributo sin valor")
	}
}

// TestPublishActualizaLaFichaCuandoElSKUYaExiste: adoptar sin enviar nada dejaba
// el cambio de contenido sin viajar jamás, porque el motor guarda el hash nuevo
// en cuanto Publish responde.
func TestPublishActualizaLaFichaCuandoElSKUYaExiste(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":{"Product":[
		{"SellerSku":"AO-NU-1001","Name":"Anillo viejo"}]}}}}`)
	s.escrituraOK()

	res, err := s.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba()})
	if err != nil {
		t.Fatalf("Publish devolvió error: %v", err)
	}
	if !res.Adopted {
		t.Errorf("la publicación existente tenía que adoptarse")
	}
	envios := s.llamadasDe("ProductUpdate")
	if len(envios) != 1 {
		t.Fatalf("la adopción tenía que mandar la ficha con ProductUpdate; hubo %d envíos", len(envios))
	}
	pr := leerFeed(t, envios[0].Cuerpo)[0]
	if pr.Name != "Anillo inteligente" || pr.Description != "Mide el sueño" {
		t.Errorf("la ficha no viajó completa: %+v", pr)
	}
	// El precio y el stock tienen sus propios trabajos: la adopción no debe
	// pisar los valores vivos del Seller Center.
	if len(pr.Unidades.Unidad) != 0 {
		t.Errorf("la adopción no debe mandar precio ni stock: %+v", pr.Unidades.Unidad)
	}
}

// TestPublishFallaSiElFeedRechazaElProducto: la escritura es asíncrona, así que
// un 200 no significa que el producto exista. Sin consultar FeedStatus con el
// RequestId, un rechazo quedaba guardado como publicación correcta y el motor
// de diff no volvía a encolar el producto nunca.
func TestPublishFallaSiElFeedRechazaElProducto(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":{"Product":[]}}}}`)
	s.escrituraOK()
	s.feedStatus(`{"Feed":"FEED-1","Status":"Finished","TotalRecords":"1","ProcessedRecords":"0",
		"FailedRecords":"1","FeedErrors":{"Error":{"Message":"mandatory attribute missing: Genero",
		"SellerSku":"AO-NU-1001","ErrorCode":"6"}}}`)

	_, err := s.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba()})
	if err == nil {
		t.Fatal("un feed rechazado tiene que devolver error, no una publicación correcta")
	}
	if !strings.Contains(err.Error(), "mandatory attribute missing") {
		t.Errorf("el error no dice qué rechazó Falabella: %v", err)
	}
	if len(s.llamadasDe("FeedStatus")) == 0 {
		t.Error("no se consultó FeedStatus")
	}
}

// TestUpdatePriceMandaElPrecioEnLaUnidadYSeparaLosFallos: el precio vive por
// unidad de negocio, y un SKU rechazado dentro del lote no puede invalidar los
// otros 49.
func TestUpdatePriceMandaElPrecioEnLaUnidadYSeparaLosFallos(t *testing.T) {
	s := nuevoServidor(t)
	s.escrituraOK()
	s.feedStatus(`{"Feed":"FEED-1","Status":"Finished","FeedErrors":{"Error":[
		{"Message":"price below minimum","SellerSku":"B-2","ErrorCode":"9"}]}}`)

	res, err := s.adaptador().UpdatePrice(context.Background(), []channel.PriceUpdate{
		{Ref: channel.ExternalRef{SKU: "A-1"}, RegularPrice: 1500},
		{Ref: channel.ExternalRef{SKU: "B-2"}, RegularPrice: 2500},
	})
	if err != nil {
		t.Fatalf("UpdatePrice devolvió error: %v", err)
	}
	if len(res) != 2 || !res[0].OK || res[1].OK {
		t.Fatalf("se esperaba A-1 correcto y B-2 rechazado: %+v", res)
	}
	if res[0].FeedID != "FEED-1" {
		t.Errorf("el FeedID sale de Head.RequestId y llegó %q", res[0].FeedID)
	}
	prods := leerFeed(t, s.llamadasDe("ProductUpdate")[0].Cuerpo)
	if len(prods) != 2 {
		t.Fatalf("el lote tenía que llevar los dos productos: %d", len(prods))
	}
	if prods[0].PrecioPlano != "" {
		t.Errorf("el precio no puede ir como campo plano del producto")
	}
	if len(prods[0].Unidades.Unidad) != 1 || prods[0].Unidades.Unidad[0].Price != "1500.00" {
		t.Errorf("el precio no llegó en la unidad de negocio: %+v", prods[0].Unidades)
	}
}

func TestUpdateStockYPausaVanEnLaUnidadDeNegocio(t *testing.T) {
	s := nuevoServidor(t)
	s.escrituraOK()

	ad := s.adaptador()
	if _, err := ad.UpdateStock(context.Background(), []channel.StockUpdate{
		{Ref: channel.ExternalRef{SKU: "A-1"}, Quantity: 0},
	}); err != nil {
		t.Fatalf("UpdateStock devolvió error: %v", err)
	}
	if err := ad.Pause(context.Background(), channel.ExternalRef{SKU: "A-1"}); err != nil {
		t.Fatalf("Pause devolvió error: %v", err)
	}

	envios := s.llamadasDe("ProductUpdate")
	if len(envios) != 2 {
		t.Fatalf("se esperaban dos feeds y hubo %d", len(envios))
	}
	stock := leerFeed(t, envios[0].Cuerpo)[0]
	if len(stock.Unidades.Unidad) != 1 || stock.Unidades.Unidad[0].Stock != "0" {
		t.Errorf("el stock no llegó en la unidad de negocio: %s", envios[0].Cuerpo)
	}
	pausa := leerFeed(t, envios[1].Cuerpo)[0]
	if pausa.EstadoPlano != "" {
		t.Errorf("el estado no puede ir como campo plano: %s", envios[1].Cuerpo)
	}
	if len(pausa.Unidades.Unidad) != 1 || pausa.Unidades.Unidad[0].Status != "inactive" {
		t.Errorf("la pausa no llegó en la unidad de negocio: %s", envios[1].Cuerpo)
	}
}

// TestFetchStatusLeeLaUnidadDeNegocio: precio, stock y estado están dentro de
// BusinessUnits; como campos planos llegaban a cero para todo el catálogo.
func TestFetchStatusLeeLaUnidadDeNegocio(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":{"Product":{
		"SellerSku":"A-1","Name":"Anillo","BusinessUnits":{"BusinessUnit":[
		{"BusinessUnit":"Falabella","OperatorCode":"facl","Price":"99990.00","Stock":"3","Status":"inactive"},
		{"BusinessUnit":"Falabella","OperatorCode":"faco","Price":"129900.00","Stock":"7","Status":"active"}]}}}}}}`)

	est, err := s.adaptador().FetchStatus(context.Background(),
		[]channel.ExternalRef{{SKU: "A-1", ListingID: "A-1"}})
	if err != nil {
		t.Fatalf("FetchStatus devolvió error: %v", err)
	}
	if len(est) != 1 {
		t.Fatalf("se esperaba un estado y llegaron %d", len(est))
	}
	if est[0].Price != 129900 || est[0].Quantity != 7 || est[0].Status != "active" {
		t.Fatalf("no se leyó la unidad de negocio de la cuenta: %+v", est[0])
	}
}

// TestFetchStatusPropagaElError: tragarse el fallo devolvía una lista vacía,
// que el núcleo no distingue de "no hay nada publicado".
func TestFetchStatusPropagaElError(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts",
		`{"ErrorResponse":{"Head":{"ErrorCode":"7","ErrorMessage":"E007: Login failed"}}}`)

	if _, err := s.adaptador().FetchStatus(context.Background(),
		[]channel.ExternalRef{{SKU: "A-1"}}); err == nil {
		t.Fatal("el error del Seller Center tenía que propagarse")
	}
}

func TestListRemoteLeeLaFormaAnidada(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetProducts", `{"SuccessResponse":{"Body":{"Products":{"Product":[
		{"SellerSku":"A-1","Name":"Uno","BusinessUnits":{"BusinessUnit":{"OperatorCode":"faco",
		"Price":"1000.00","Stock":"2","Status":"active"}}}]}}}}`)

	pag, err := s.adaptador().ListRemote(context.Background(), channel.Cursor{Size: 50})
	if err != nil {
		t.Fatalf("ListRemote devolvió error: %v", err)
	}
	if len(pag.Items) != 1 || pag.Items[0].Ref.SKU != "A-1" || pag.Items[0].Price != 1000 {
		t.Fatalf("no se leyó el catálogo remoto: %+v", pag.Items)
	}
}

// TestFetchOrdersPaginaConOffset: sin Offset ni cursor, el núcleo pedía cuarenta
// veces la misma primera página y los pedidos a partir del 101 no entraban.
func TestFetchOrdersPaginaConOffset(t *testing.T) {
	s := nuevoServidor(t)
	s.respondeCon("GetOrders", func(r *http.Request) string {
		switch r.URL.Query().Get("Offset") {
		case "0":
			return `{"SuccessResponse":{"Body":{"Orders":{"Order":[
				{"OrderId":1001,"OrderNumber":200001,"CreatedAt":"2026-09-01 10:00:00","Price":"45990.00","ShippingFeeTotal":"5000.00","CustomerFirstName":"Ana","CustomerLastName":"Ruiz","AddressShipping":{"Address1":"Cra 1","City":"Bogotá","Country":"CO","Phone":"3000"}},
				{"OrderId":1002,"OrderNumber":200002,"CreatedAt":"2026-09-01 11:00:00","Price":"12000.00","AddressShipping":{"City":"Cali"}}]}}}}`
		case "2":
			return `{"SuccessResponse":{"Body":{"Orders":{"Order":
				{"OrderId":1003,"OrderNumber":200003,"CreatedAt":"2026-09-01 12:00:00","Price":"7000.00","AddressShipping":{"City":"Medellín"}}}}}}`
		default:
			return `{"SuccessResponse":{"Body":{"Orders":""}}}`
		}
	})
	s.respondeCon("GetOrderItems", func(r *http.Request) string {
		return `{"SuccessResponse":{"Body":{"OrderItems":{"OrderItem":{"OrderItemId":"101311982",
			"OrderId":"` + r.URL.Query().Get("OrderId") + `","Sku":"AO-NU-1001","Name":"Anillo",
			"ItemPrice":"45990.00","PaidPrice":"45990.00","Status":"pending"}}}}}`
	})

	ad := s.adaptador()
	desde := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	pag, err := ad.FetchOrders(context.Background(), desde, channel.Cursor{Size: 2})
	if err != nil {
		t.Fatalf("FetchOrders devolvió error: %v", err)
	}
	if len(pag.Orders) != 2 || pag.Done {
		t.Fatalf("se esperaban dos pedidos y otra página: %d pedidos, Done=%v", len(pag.Orders), pag.Done)
	}
	if pag.Orders[0].Total != 45990 || pag.Orders[0].Shipping != 5000 {
		t.Errorf("la cabecera del pedido llegó mal: %+v", pag.Orders[0])
	}
	// Las líneas van anidadas en OrderItems.OrderItem y colapsan a objeto con
	// una sola: leídas como array, el pedido se guardaba sin una sola línea.
	if len(pag.Orders[0].Lines) != 1 || pag.Orders[0].Lines[0].ExternalID != "101311982" {
		t.Fatalf("las líneas del pedido no se leyeron: %+v", pag.Orders[0].Lines)
	}
	if pag.Orders[0].Lines[0].SKU != "AO-NU-1001" || pag.Orders[0].Lines[0].UnitPrice != 45990 {
		t.Errorf("la línea llegó mal: %+v", pag.Orders[0].Lines[0])
	}

	pag2, err := ad.FetchOrders(context.Background(), desde, pag.Next)
	if err != nil {
		t.Fatalf("la segunda página devolvió error: %v", err)
	}
	if len(pag2.Orders) != 1 || !pag2.Done {
		t.Fatalf("la segunda página tenía que traer el pedido 1003 y cerrar: %+v", pag2)
	}
	if pag2.Orders[0].ExternalID != "1003" {
		t.Errorf("se repitió la primera página: %+v", pag2.Orders[0])
	}

	llamadas := s.llamadasDe("GetOrders")
	if len(llamadas) != 2 {
		t.Fatalf("se esperaban dos llamadas a GetOrders y hubo %d", len(llamadas))
	}
	if llamadas[0].Query.Get("Offset") != "0" || llamadas[1].Query.Get("Offset") != "2" {
		t.Errorf("el cursor no se tradujo a Offset: %q y %q",
			llamadas[0].Query.Get("Offset"), llamadas[1].Query.Get("Offset"))
	}
	if llamadas[0].Query.Get("SortBy") != "created_at" || llamadas[0].Query.Get("SortDirection") != "ASC" {
		t.Errorf("sin orden ascendente por fecha la marca de agua puede saltarse pedidos: %v", llamadas[0].Query)
	}
}

// TestFetchOrdersFallaSiNoPuedeLeerLasLineas: un pedido sin líneas monta en Odoo
// un sale.order con total cobrado y nada que despachar. El fallo se descartaba
// en silencio con "if err == nil".
func TestFetchOrdersFallaSiNoPuedeLeerLasLineas(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetOrders", `{"SuccessResponse":{"Body":{"Orders":{"Order":
		{"OrderId":1001,"OrderNumber":200001,"CreatedAt":"2026-09-01 10:00:00","Price":"45990.00"}}}}}`)
	s.responde("GetOrderItems",
		`{"ErrorResponse":{"Head":{"ErrorCode":"33","ErrorMessage":"E033: Order not found"}}}`)

	if _, err := s.adaptador().FetchOrders(context.Background(), time.Time{}, channel.Cursor{}); err == nil {
		t.Fatal("un pedido cuyas líneas no se pueden leer no puede ingerirse sin más")
	}
}

// TestAtributosDeCategoriaAceptaLaFormaAnidada: Seller Center entrega los
// atributos bajo Body.Attribute, las opciones bajo Options.Option y isMandatory
// unas veces como número y otras como cadena.
func TestAtributosDeCategoriaAceptaLaFormaAnidada(t *testing.T) {
	s := nuevoServidor(t)
	s.responde("GetCategoryAttributes", `{"SuccessResponse":{"Body":{"Attribute":[
		{"Name":"Genero","Label":"Género","AttributeType":"option","isMandatory":"1",
		 "Options":{"Option":[{"Name":"Mujer"},{"Name":"Hombre"}]}},
		{"Name":"Talla","Label":"Talla","AttributeType":"value","isMandatory":0,
		 "Options":{"Option":{"Name":"M"}}}]}}}`)

	attrs, err := s.adaptador().AtributosDeCategoria(context.Background(), "811")
	if err != nil {
		t.Fatalf("AtributosDeCategoria devolvió error: %v", err)
	}
	if len(attrs) != 2 {
		t.Fatalf("se esperaban dos atributos y llegaron %d", len(attrs))
	}
	if !attrs[0].EsObligatorio() || attrs[1].EsObligatorio() {
		t.Errorf("isMandatory no se interpretó: %+v", attrs)
	}
	if len(attrs[0].Opciones) != 2 || attrs[0].Opciones[0].Nombre != "Mujer" {
		t.Errorf("las opciones no se leyeron: %+v", attrs[0].Opciones)
	}
	if len(attrs[1].Opciones) != 1 || attrs[1].Opciones[0].Nombre != "M" {
		t.Errorf("la opción única colapsada a objeto no se leyó: %+v", attrs[1].Opciones)
	}
}

// TestListaSCAceptaLasTresFormas fija el contrato del decodificador: array
// plano, lista anidada y objeto único.
func TestListaSCAceptaLasTresFormas(t *testing.T) {
	casos := map[string]struct {
		json    string
		cuantos int
	}{
		"array plano":   {`[{"SellerSku":"A"},{"SellerSku":"B"}]`, 2},
		"lista anidada": {`{"Product":[{"SellerSku":"A"}]}`, 1},
		"objeto único":  {`{"Product":{"SellerSku":"A"}}`, 1},
		"suelto":        {`{"SellerSku":"A"}`, 1},
		"vacío":         {`""`, 0},
		"nulo":          {`null`, 0},
		"sin nada":      {`{}`, 0},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			out, err := listaSC[productoResp]([]byte(c.json), "Product")
			if err != nil {
				t.Fatalf("%s: %v", nombre, err)
			}
			if len(out) != c.cuantos {
				t.Fatalf("%s: se esperaban %d y llegaron %d", nombre, c.cuantos, len(out))
			}
		})
	}
}
