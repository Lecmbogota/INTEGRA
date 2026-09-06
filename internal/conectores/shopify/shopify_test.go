package shopify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// tienda imita lo justo de la Admin API de Shopify para probar el adaptador
// sin red ni credenciales. Cada prueba siembra las páginas que le importan.
//
// Se registran todas las llamadas porque la mitad de los defectos de este
// adaptador no se ven en el valor devuelto sino en la petición que NO se hizo
// (el PUT de contenido, el inventory_levels/set, la segunda página).
type tienda struct {
	t *testing.T
	*httptest.Server

	mu       sync.Mutex
	llamadas []llamada

	paginasProductos [][]map[string]any
	paginasPedidos   [][]map[string]any
	ubicaciones      []map[string]any
	// respuestaPost es lo que devuelve POST /products.json.
	respuestaPost map[string]any
	// inventarioVariante es el inventory_item_id que devuelven las variantes.
	inventarioVariante int64
	// estadoProducto responde a GET /products/{id}.json: código y cuerpo.
	estadoProducto func(id string) (int, map[string]any)
	// ordenesDespacho son los fulfillment orders por pedido.
	ordenesDespacho map[string][]map[string]any
	// stock es el disponible por ubicación y por inventory_item_id, que es
	// como lo guarda Shopify de verdad: inventory_quantity de la variante es
	// solo la suma de todas las ubicaciones.
	stock map[int64]map[int64]int
	// alcances son los permisos del token que devuelve access_scopes.json.
	alcances []string
}

type llamada struct {
	Metodo   string
	Ruta     string // sin el prefijo /admin/api/<version>
	Completa string
	Query    url.Values
	Cuerpo   map[string]any
}

func nuevaTienda(t *testing.T) *tienda {
	t.Helper()
	s := &tienda{
		t:                  t,
		ubicaciones:        []map[string]any{{"id": 111, "active": true}},
		inventarioVariante: 888,
		ordenesDespacho:    map[string][]map[string]any{},
		stock:              map[int64]map[int64]int{},
		alcances:           []string{"read_products", "write_products", "read_orders", "write_orders"},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.manejar))
	t.Cleanup(s.Close)
	return s
}

func (s *tienda) prefijo() string { return "/admin/api/" + VersionAPI }

func (s *tienda) manejar(w http.ResponseWriter, r *http.Request) {
	cuerpo := map[string]any{}
	if r.Body != nil {
		datos, _ := io.ReadAll(r.Body)
		if len(datos) > 0 {
			_ = json.Unmarshal(datos, &cuerpo)
		}
	}
	ruta := strings.TrimPrefix(r.URL.Path, s.prefijo())
	s.mu.Lock()
	s.llamadas = append(s.llamadas, llamada{
		Metodo: r.Method, Ruta: ruta, Completa: r.URL.Path,
		Query: r.URL.Query(), Cuerpo: cuerpo,
	})
	s.mu.Unlock()

	// access_scopes.json no cuelga de /admin/api/{version}: va antes de la
	// comprobación del prefijo, igual que en la tienda real.
	if r.URL.Path == "/admin/oauth/access_scopes.json" {
		alcances := make([]map[string]any, 0, len(s.alcances))
		for _, a := range s.alcances {
			alcances = append(alcances, map[string]any{"handle": a})
		}
		responder(w, map[string]any{"access_scopes": alcances})
		return
	}
	if !strings.HasPrefix(r.URL.Path, s.prefijo()+"/") {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"errors":"esta prueba solo sirve la version `+VersionAPI+`"}`)
		return
	}
	if r.Header.Get("X-Shopify-Access-Token") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"errors":"[API] Invalid API key or access token"}`)
		return
	}

	switch {
	case r.Method == http.MethodGet && ruta == "/products.json":
		s.pagina(w, r, "products", s.paginasProductos)
	case r.Method == http.MethodPost && ruta == "/products.json":
		resp := s.respuestaPost
		if resp == nil {
			resp = map[string]any{"product": map[string]any{
				"id": 55, "handle": "producto-nuevo",
				"variants": []map[string]any{{"id": 66, "sku": "SKU-001", "inventory_item_id": 99}},
			}}
		}
		responder(w, resp)
	case r.Method == http.MethodGet && ruta == "/orders.json":
		s.pagina(w, r, "orders", s.paginasPedidos)
	case r.Method == http.MethodGet && strings.HasPrefix(ruta, "/orders/") &&
		strings.HasSuffix(ruta, "/fulfillment_orders.json"):
		id := strings.TrimSuffix(strings.TrimPrefix(ruta, "/orders/"), "/fulfillment_orders.json")
		fos, hay := s.ordenesDespacho[id]
		if !hay {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"errors":"Not Found"}`)
			return
		}
		responder(w, map[string]any{"fulfillment_orders": fos})
	case r.Method == http.MethodPost && ruta == "/fulfillments.json":
		responder(w, map[string]any{"fulfillment": map[string]any{"id": 5001, "status": "success"}})
	case r.Method == http.MethodGet && ruta == "/locations.json":
		responder(w, map[string]any{"locations": s.ubicaciones})
	case r.Method == http.MethodGet && ruta == "/inventory_levels.json":
		q := r.URL.Query()
		var locs []int64
		for _, t := range strings.Split(q.Get("location_ids"), ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
				locs = append(locs, n)
			}
		}
		if len(locs) == 0 {
			// El endpoint exige al menos inventory_item_ids o location_ids.
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = io.WriteString(w, `{"errors":"either inventory_item_ids or location_ids is required"}`)
			return
		}
		pedidos := map[int64]bool{}
		for _, t := range strings.Split(q.Get("inventory_item_ids"), ",") {
			if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
				pedidos[n] = true
			}
		}
		niveles := []map[string]any{}
		for _, loc := range locs {
			for item, disp := range s.stock[loc] {
				if len(pedidos) > 0 && !pedidos[item] {
					continue
				}
				niveles = append(niveles, map[string]any{
					"inventory_item_id": item, "location_id": loc, "available": disp,
				})
			}
		}
		responder(w, map[string]any{"inventory_levels": niveles})
	case r.Method == http.MethodPost && ruta == "/inventory_levels/set.json":
		responder(w, map[string]any{"inventory_level": map[string]any{
			"inventory_item_id": cuerpo["inventory_item_id"],
			"location_id":       cuerpo["location_id"],
			"available":         cuerpo["available"],
		}})
	case r.Method == http.MethodPut && strings.HasPrefix(ruta, "/products/"):
		responder(w, map[string]any{"product": map[string]any{"id": idDe(ruta, "/products/")}})
	case r.Method == http.MethodGet && strings.HasPrefix(ruta, "/products/"):
		if s.estadoProducto == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"errors":"Not Found"}`)
			return
		}
		codigo, cuerpo := s.estadoProducto(idDe(ruta, "/products/"))
		if codigo != http.StatusOK {
			w.WriteHeader(codigo)
			_, _ = io.WriteString(w, `{"errors":"fallo simulado"}`)
			return
		}
		responder(w, cuerpo)
	case strings.HasPrefix(ruta, "/variants/"):
		responder(w, map[string]any{"variant": map[string]any{
			"id": idDe(ruta, "/variants/"), "inventory_item_id": s.inventarioVariante,
		}})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"errors":"ruta no simulada: `+ruta+`"}`)
	}
}

// pagina sirve la página que pida page_info y anuncia la siguiente en el Link
// header, que es como pagina de verdad la REST de Shopify.
func (s *tienda) pagina(w http.ResponseWriter, r *http.Request, clave string, paginas [][]map[string]any) {
	i := 0
	if t := r.URL.Query().Get("page_info"); t != "" {
		if _, err := fmt.Sscanf(t, "pag%d", &i); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"errors":"page_info invalido"}`)
			return
		}
	}
	var items []map[string]any
	if i < len(paginas) {
		items = paginas[i]
	}
	if i+1 < len(paginas) {
		w.Header().Set("Link", "<"+s.URL+s.prefijo()+"/"+clave+".json?limit=250&page_info=pag"+
			fmt.Sprint(i+1)+">; rel=\"next\"")
	}
	responder(w, map[string]any{clave: items})
}

func idDe(ruta, prefijo string) string {
	return strings.TrimSuffix(strings.TrimPrefix(ruta, prefijo), ".json")
}

func responder(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *tienda) adaptador(t *testing.T, cred map[string]string) *Adaptador {
	t.Helper()
	viejo := esquemaAPI
	esquemaAPI = "http"
	t.Cleanup(func() { esquemaAPI = viejo })

	if cred == nil {
		cred = map[string]string{}
	}
	cred["tienda"] = strings.TrimPrefix(s.URL, "http://")
	if cred["token"] == "" {
		cred["token"] = "shpat_prueba"
	}
	ad, err := channel.New(channel.Shopify, channel.Config{Credentials: cred})
	if err != nil {
		t.Fatal(err)
	}
	return ad.(*Adaptador)
}

func (s *tienda) buscarLlamada(metodo, ruta string) *llamada {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.llamadas {
		if s.llamadas[i].Metodo == metodo && s.llamadas[i].Ruta == ruta {
			return &s.llamadas[i]
		}
	}
	return nil
}

// llamadasDe devuelve todas las peticiones a una ruta, en orden: un pedido
// repartido entre ubicaciones produce varios POST y hay que verlos todos.
func (s *tienda) llamadasDe(metodo, ruta string) []llamada {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []llamada
	for _, l := range s.llamadas {
		if l.Metodo == metodo && l.Ruta == ruta {
			out = append(out, l)
		}
	}
	return out
}

func (s *tienda) contar(metodo, ruta string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.llamadas {
		if l.Metodo == metodo && l.Ruta == ruta {
			n++
		}
	}
	return n
}

// producto es el producto normalizado que manda el motor de publicación.
func producto() channel.Product {
	return channel.Product{
		SKU: "SKU-300", Title: "Impresora térmica 80mm", Description: "<p>Ficha nueva</p>",
		Brand: "Xprinter", Weight: 1.2,
		Images: []channel.Image{{URL: "https://integra.example/imagenes/abc/cuadrada_1200"}},
		Variants: []channel.Variant{{
			SKU: "SKU-300", Barcode: "7898095297749",
			RegularPrice: 150000, Currency: "COP", Quantity: 40,
		}},
	}
}

// catalogo arma una página de productos con SKUs correlativos.
func catalogo(desde, n int) []map[string]any {
	var out []map[string]any
	for i := desde; i < desde+n; i++ {
		out = append(out, map[string]any{
			"id": 1000 + i,
			"variants": []map[string]any{{
				"id": 2000 + i, "sku": fmt.Sprintf("SKU-%03d", i), "inventory_item_id": 3000 + i,
			}},
		})
	}
	return out
}

// ---------------------------------------------------------------- pruebas

// El defecto crítico: con una sola página, el SKU 300 de un catálogo de 452
// no se encuentra y Publish crea un duplicado en una tienda viva.
func TestAdoptaUnSKUQueEstaMasAllaDeLaPrimeraPagina(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasProductos = [][]map[string]any{catalogo(0, 250), catalogo(250, 202)}

	ad := s.adaptador(t, nil)
	res, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Adopted {
		t.Fatal("el SKU existe en la segunda página: había que adoptarlo, no crear otro")
	}
	if res.Ref.ListingID != "1300" || res.Ref.VariantID != "2300" {
		t.Errorf("ref = %+v, quería el producto 1300 / variante 2300", res.Ref)
	}
	if n := s.contar(http.MethodPost, "/products.json"); n != 0 {
		t.Errorf("se crearon %d productos: eso es un SKU duplicado en la tienda", n)
	}
	if n := s.contar(http.MethodGet, "/products.json"); n != 2 {
		t.Errorf("se pidieron %d páginas del catálogo, quería 2 (hay que agotarlo)", n)
	}
}

// Un SKU que de verdad no existe se sigue creando, y solo entonces.
func TestPublicaCuandoElSKUNoEstaEnNingunaPagina(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasProductos = [][]map[string]any{catalogo(0, 250), catalogo(250, 20)}

	ad := s.adaptador(t, nil)
	res, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Adopted || res.Ref.ListingID != "55" {
		t.Fatalf("res = %+v, quería una publicación nueva", res)
	}
}

// Adoptar y devolver éxito sin escribir dejaba el cambio de ficha marcado
// como sincronizado para siempre: el motor guarda los tres hashes en cuanto
// Publish devuelve bien.
func TestLaAdopcionEnviaContenidoPrecioYStock(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasProductos = [][]map[string]any{{{
		"id": 1300,
		"variants": []map[string]any{{
			"id": 2300, "sku": "SKU-300", "inventory_item_id": 3300,
		}},
	}}}

	ad := s.adaptador(t, nil)
	if _, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()}); err != nil {
		t.Fatal(err)
	}

	put := s.buscarLlamada(http.MethodPut, "/products/1300.json")
	if put == nil {
		t.Fatal("la adopción no mandó el contenido: la tienda se queda con la ficha vieja")
	}
	prod, _ := put.Cuerpo["product"].(map[string]any)
	if prod["title"] != "Impresora térmica 80mm" {
		t.Errorf("title = %v", prod["title"])
	}
	if prod["body_html"] != "<p>Ficha nueva</p>" {
		t.Errorf("body_html = %v", prod["body_html"])
	}
	if _, hay := prod["images"]; !hay {
		t.Error("las imágenes entran en el hash de contenido: tienen que viajar")
	}

	precio := s.buscarLlamada(http.MethodPut, "/variants/2300.json")
	if precio == nil {
		t.Fatal("la adopción no mandó el precio")
	}
	if v, _ := precio.Cuerpo["variant"].(map[string]any); v["price"] != "150000.00" {
		t.Errorf("price = %v", v["price"])
	}

	stock := s.buscarLlamada(http.MethodPost, "/inventory_levels/set.json")
	if stock == nil {
		t.Fatal("la adopción no fijó el stock")
	}
	if stock.Cuerpo["available"] != float64(40) {
		t.Errorf("available = %v, quería 40", stock.Cuerpo["available"])
	}
}

// inventory_quantity es de solo lectura: el producto nacía con 0 disponibles
// mientras Integra guardaba el stock_hash como enviado.
func TestLaPublicacionNuevaFijaElStockConNivelesDeInventario(t *testing.T) {
	s := nuevaTienda(t)

	ad := s.adaptador(t, nil)
	res, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.VariantID != "66" {
		t.Fatalf("ref = %+v", res.Ref)
	}

	post := s.buscarLlamada(http.MethodPost, "/products.json")
	prod, _ := post.Cuerpo["product"].(map[string]any)
	vars, _ := prod["variants"].([]any)
	v, _ := vars[0].(map[string]any)
	if _, hay := v["inventory_quantity"]; hay {
		t.Error("inventory_quantity es de solo lectura: mandarlo hace creer que el stock viajó")
	}

	stock := s.buscarLlamada(http.MethodPost, "/inventory_levels/set.json")
	if stock == nil {
		t.Fatal("no se fijó el stock tras crear el producto: nace con 0 disponibles")
	}
	if stock.Cuerpo["inventory_item_id"] != float64(99) {
		t.Errorf("inventory_item_id = %v, quería el de la variante recién creada", stock.Cuerpo["inventory_item_id"])
	}
	if stock.Cuerpo["available"] != float64(40) {
		t.Errorf("available = %v, quería 40", stock.Cuerpo["available"])
	}
	if stock.Cuerpo["location_id"] != float64(111) {
		t.Errorf("location_id = %v", stock.Cuerpo["location_id"])
	}
}

// Si el stock no se puede fijar, Publish tiene que fallar: si devolviera bien,
// el motor guardaría el stock_hash y no volvería a intentarlo.
func TestPublicarFallaSiNoSePuedeFijarElStock(t *testing.T) {
	s := nuevaTienda(t)
	s.ubicaciones = []map[string]any{{"id": 111, "active": false}}

	ad := s.adaptador(t, nil)
	if _, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()}); err == nil {
		t.Fatal("sin ubicación activa el stock no llega: Publish no puede devolver éxito")
	}
}

// El precio de línea de Shopify es antes de descuentos: el sale.order de Odoo
// quedaba por encima de lo cobrado.
func TestElPrecioDeLineaDescuentaElCupon(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{{
		"id": 5001, "name": "#1001", "financial_status": "paid",
		"created_at": "2026-09-01T10:01:50-05:00", "updated_at": "2026-09-01T10:05:00-05:00",
		"currency": "COP", "total_price": "103920.00", "total_tax": "0.00",
		"line_items": []map[string]any{{
			"id": 7001, "sku": "SKU-300", "title": "Impresora", "quantity": 1,
			"price": "129900.00", "total_discount": "25980.00",
			"discount_allocations": []map[string]any{{"amount": "25980.00"}},
		}},
	}}}

	ad := s.adaptador(t, nil)
	pag, err := ad.FetchOrders(context.Background(), time.Time{}, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	l := pag.Orders[0].Lines[0]
	if l.UnitPrice != 103920 {
		t.Errorf("UnitPrice = %v, quería 103920 (lo cobrado, no el precio de lista)", l.UnitPrice)
	}
	if l.TotalPrice != 103920 {
		t.Errorf("TotalPrice = %v, quería 103920", l.TotalPrice)
	}
	if suma := l.TotalPrice; suma != pag.Orders[0].Total {
		t.Errorf("las líneas suman %v y el pedido dice %v: el sale.order no cuadraría",
			suma, pag.Orders[0].Total)
	}
}

// Sin leer el Link header la ingesta repetía la misma primera página 40 veces
// por ronda y solo entraban 100 pedidos.
func TestLosPedidosPasanDePaginaConElLinkHeader(t *testing.T) {
	s := nuevaTienda(t)
	pedido := func(id int) map[string]any {
		return map[string]any{
			"id": id, "name": fmt.Sprintf("#%d", id), "financial_status": "paid",
			"created_at": "2026-09-01T10:00:00-05:00", "updated_at": "2026-09-01T10:00:00-05:00",
			"currency": "COP", "total_price": "1000.00",
		}
	}
	s.paginasPedidos = [][]map[string]any{{pedido(1), pedido(2)}, {pedido(3)}}

	ad := s.adaptador(t, nil)
	desde := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	pag, err := ad.FetchOrders(context.Background(), desde, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if pag.Done || pag.Next.Token == "" {
		t.Fatalf("había una página más: Done=%v Next=%q", pag.Done, pag.Next.Token)
	}

	pag2, err := ad.FetchOrders(context.Background(), desde, pag.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(pag2.Orders) != 1 || pag2.Orders[0].ExternalID != "3" {
		t.Fatalf("la segunda página trajo %+v", pag2.Orders)
	}
	if !pag2.Done {
		t.Error("la segunda página es la última")
	}

	s.mu.Lock()
	ultima := s.llamadas[len(s.llamadas)-1]
	s.mu.Unlock()
	if ultima.Query.Get("page_info") == "" {
		t.Error("la segunda petición no llevó el cursor: se repetiría la página 1")
	}
	// Shopify rechaza con 400 cualquier parámetro que no sea limit o fields
	// junto a page_info.
	if ultima.Query.Get("status") != "" || ultima.Query.Get("updated_at_min") != "" {
		t.Errorf("con page_info solo viajan limit y fields, y viajó %v", ultima.Query)
	}
}

// La marca de agua del núcleo avanza con UpdatedAt: si el filtro fuera
// created_at_min, un pedido viejo modificado la empujaría por encima de
// pedidos nuevos que aún no se han traído.
func TestElFiltroDePedidosVaPorFechaDeModificacion(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{}}

	ad := s.adaptador(t, nil)
	desde := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	if _, err := ad.FetchOrders(context.Background(), desde, channel.Cursor{}); err != nil {
		t.Fatal(err)
	}
	l := s.buscarLlamada(http.MethodGet, "/orders.json")
	if l.Query.Get("updated_at_min") != "2026-09-01T15:00:00Z" {
		t.Errorf("updated_at_min = %q", l.Query.Get("updated_at_min"))
	}
	if l.Query.Get("created_at_min") != "" {
		t.Error("con created_at_min un cambio de estado deja pedidos nuevos bajo la marca de agua")
	}
}

// El comprador de una compra como invitado venía en la carga y se tiraba:
// Odoo acababa con un res.partner «Comprador SHOPIFY» por pedido.
func TestElPedidoSinCustomerUsaElCorreoYLaDireccionDeEnvio(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{{
		"id": 5002, "name": "#1002", "financial_status": "paid",
		"created_at": "2026-09-02T09:00:00-05:00", "updated_at": "2026-09-02T09:00:00-05:00",
		"currency": "COP", "total_price": "50000.00",
		"email": "ana@example.com", "customer": nil,
		"shipping_address": map[string]any{
			"name": "Ana Pérez", "phone": "3001234567", "address1": "Calle 10 # 5-20",
			"city": "Bogotá", "province": "Bogotá D.C.", "zip": "110111", "country": "Colombia",
		},
	}}}

	ad := s.adaptador(t, nil)
	pag, err := ad.FetchOrders(context.Background(), time.Time{}, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	c := pag.Orders[0].Buyer
	if c.Email != "ana@example.com" {
		t.Errorf("Email = %q: sin correo, resolverCliente crea un partner nuevo por pedido", c.Email)
	}
	if c.Name != "Ana Pérez" {
		t.Errorf("Name = %q", c.Name)
	}
	if c.Phone != "3001234567" {
		t.Errorf("Phone = %q", c.Phone)
	}
}

// Un pedido cancelado o de la pasarela de pruebas acababa como sale.order:
// el núcleo encola el montaje de todo lo que se ingiere.
func TestDescartaLosPedidosCanceladosYDePrueba(t *testing.T) {
	s := nuevaTienda(t)
	base := func(id int) map[string]any {
		return map[string]any{
			"id": id, "name": fmt.Sprintf("#%d", id), "financial_status": "paid",
			"created_at": "2026-09-02T09:00:00-05:00", "updated_at": "2026-09-02T09:00:00-05:00",
			"currency": "COP", "total_price": "1000.00",
		}
	}
	cancelado := base(1)
	cancelado["cancelled_at"] = "2026-09-02T09:05:00-05:00"
	prueba := base(2)
	prueba["test"] = true
	s.paginasPedidos = [][]map[string]any{{cancelado, prueba, base(3)}}

	ad := s.adaptador(t, nil)
	pag, err := ad.FetchOrders(context.Background(), time.Time{}, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pag.Orders) != 1 || pag.Orders[0].ExternalID != "3" {
		t.Fatalf("entraron %d pedidos, quería solo el bueno: %+v", len(pag.Orders), pag.Orders)
	}
}

// El envío va dentro de total_price pero en ninguna línea: sin leerlo, la
// cabecera del pedido no cuadra con sus líneas ni con el pedido de Odoo.
func TestLeeElEnvioYConservaLaCargaOriginal(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{{
		"id": 5003, "name": "#1003", "financial_status": "paid",
		"created_at": "2026-09-02T09:00:00-05:00", "updated_at": "2026-09-02T10:00:00-05:00",
		"currency": "COP", "total_price": "115000.00", "total_tax": "0.00",
		"total_shipping_price_set": map[string]any{
			"shop_money": map[string]any{"amount": "15000.00", "currency_code": "COP"},
		},
		"line_items": []map[string]any{{
			"id": 7003, "sku": "SKU-300", "title": "Impresora", "quantity": 1, "price": "100000.00",
		}},
	}}}

	ad := s.adaptador(t, nil)
	pag, err := ad.FetchOrders(context.Background(), time.Time{}, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	o := pag.Orders[0]
	if o.Shipping != 15000 {
		t.Errorf("Shipping = %v, quería 15000", o.Shipping)
	}
	if o.Lines[0].TotalPrice+o.Shipping != o.Total {
		t.Errorf("líneas (%v) + envío (%v) != total (%v)", o.Lines[0].TotalPrice, o.Shipping, o.Total)
	}
	if len(o.Raw) == 0 {
		t.Error("Raw vacío: el pedido quedaría en raw_payload como '{}' e imposible de reprocesar")
	}
	if o.UpdatedAt.IsZero() {
		t.Error("UpdatedAt vacío: un cambio de estado posterior no volvería a entrar")
	}
}

// Fijar una versión retirada no congela nada: Shopify hace fall-forward y
// sirve la petición con otra versión, que cambia cada trimestre.
func TestLaVersionDeAPIEstaSoportada(t *testing.T) {
	// Versiones vivas a la fecha de este arreglo (septiembre de 2026); cada
	// una se soporta doce meses desde su salida.
	soportadas := map[string]string{
		"2025-10": "vence el 16/10/2026",
		"2026-01": "vence el 16/01/2027",
		"2026-04": "vence el 16/04/2027",
		"2026-07": "vence el 16/07/2027",
	}
	if _, ok := soportadas[VersionAPI]; !ok {
		t.Fatalf("la versión %s no está soportada: Shopify serviría otra sin avisar", VersionAPI)
	}

	s := nuevaTienda(t)
	ad := s.adaptador(t, nil)
	if _, err := ad.ListRemote(context.Background(), channel.Cursor{}); err != nil {
		t.Fatal(err)
	}
	l := s.buscarLlamada(http.MethodGet, "/products.json")
	if !strings.HasPrefix(l.Completa, "/admin/api/"+VersionAPI+"/") {
		t.Errorf("la petición fue a %q: hay otra versión pegada en el código", l.Completa)
	}
}

func TestListRemoteDevuelveElCursorDeLaSiguientePagina(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasProductos = [][]map[string]any{
		{{"id": 1, "title": "Uno", "status": "active", "variants": []map[string]any{
			{"id": 11, "sku": "SKU-001", "price": "1000.00", "inventory_quantity": 3}}}},
		{{"id": 2, "title": "Dos", "status": "active"}},
	}

	ad := s.adaptador(t, nil)
	p1, err := ad.ListRemote(context.Background(), channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if p1.Done || p1.Next.Token == "" {
		t.Fatalf("hay una segunda página: Done=%v Next=%q", p1.Done, p1.Next.Token)
	}
	p2, err := ad.ListRemote(context.Background(), p1.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(p2.Items) != 1 || p2.Items[0].Ref.ListingID != "2" {
		t.Fatalf("la segunda página trajo %+v", p2.Items)
	}
	if !p2.Done {
		t.Error("la segunda página es la última")
	}
}

// La capacidad NativeCompareAtPrice se declaraba y no se escribía nunca.
func TestElPrecioDeOfertaViajaEnCompareAtPrice(t *testing.T) {
	s := nuevaTienda(t)
	ad := s.adaptador(t, nil)
	ref := channel.ExternalRef{ListingID: "1", VariantID: "77", SKU: "SKU-300"}

	res, err := ad.UpdatePrice(context.Background(), []channel.PriceUpdate{
		{Ref: ref, RegularPrice: 150000, SalePrice: 120000, Currency: "COP"},
	})
	if err != nil || !res[0].OK {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	v, _ := s.buscarLlamada(http.MethodPut, "/variants/77.json").Cuerpo["variant"].(map[string]any)
	if v["price"] != "120000.00" {
		t.Errorf("price = %v, quería la oferta", v["price"])
	}
	if v["compare_at_price"] != "150000.00" {
		t.Errorf("compare_at_price = %v, quería el precio de lista tachado", v["compare_at_price"])
	}

	// Al terminar la oferta hay que vaciar el tachado, o la tienda seguiría
	// mostrando un descuento que ya no existe.
	s.mu.Lock()
	s.llamadas = nil
	s.mu.Unlock()
	if _, err := ad.UpdatePrice(context.Background(), []channel.PriceUpdate{
		{Ref: ref, RegularPrice: 150000, Currency: "COP"},
	}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.buscarLlamada(http.MethodPut, "/variants/77.json").Cuerpo["variant"].(map[string]any)
	if v["price"] != "150000.00" {
		t.Errorf("price = %v", v["price"])
	}
	if valor, hay := v["compare_at_price"]; !hay || valor != nil {
		t.Errorf("compare_at_price = %v, quería null para borrar el tachado", valor)
	}
}

// Con dos ubicaciones activas, «la primera que llegue» reparte el stock en la
// equivocada; la cuenta puede fijarla.
func TestLaUbicacionSeTomaDeLasCredencialesYSeCachea(t *testing.T) {
	s := nuevaTienda(t)
	s.ubicaciones = []map[string]any{
		{"id": 222, "active": true}, // el punto de venta, que Shopify puede listar primero
		{"id": 111, "active": true},
	}
	ad := s.adaptador(t, map[string]string{"location_id": "111"})

	ups := []channel.StockUpdate{
		{Ref: channel.ExternalRef{VariantID: "77"}, Quantity: 5},
		{Ref: channel.ExternalRef{VariantID: "78"}, Quantity: 6},
	}
	res, err := ad.UpdateStock(context.Background(), ups)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range res {
		if !r.OK {
			t.Fatalf("res[%d] = %+v", i, r)
		}
	}
	if n := s.contar(http.MethodGet, "/locations.json"); n != 0 {
		t.Errorf("se consultaron las ubicaciones %d veces teniendo location_id en la cuenta", n)
	}
	l := s.buscarLlamada(http.MethodPost, "/inventory_levels/set.json")
	if l.Cuerpo["location_id"] != float64(111) {
		t.Errorf("location_id = %v, quería la de la cuenta", l.Cuerpo["location_id"])
	}
}

func TestSinCredencialLaUbicacionSeDescubreUnaSolaVez(t *testing.T) {
	s := nuevaTienda(t)
	s.ubicaciones = []map[string]any{{"id": 222, "active": true}, {"id": 111, "active": true}}
	ad := s.adaptador(t, nil)

	if _, err := ad.UpdateStock(context.Background(), []channel.StockUpdate{
		{Ref: channel.ExternalRef{VariantID: "77"}, Quantity: 5},
		{Ref: channel.ExternalRef{VariantID: "78"}, Quantity: 6},
	}); err != nil {
		t.Fatal(err)
	}
	if n := s.contar(http.MethodGet, "/locations.json"); n != 1 {
		t.Errorf("se consultaron las ubicaciones %d veces por lote, quería 1", n)
	}
}

// FetchStatus se tragaba los errores: una publicación borrada desaparecía del
// informe sin ruido.
func TestFetchStatusReportaLaPublicacionBorradaYElFalloDelCanal(t *testing.T) {
	s := nuevaTienda(t)
	s.estadoProducto = func(id string) (int, map[string]any) {
		switch id {
		case "1":
			return http.StatusOK, map[string]any{"product": map[string]any{
				"status": "active", "handle": "uno",
				"variants": []map[string]any{{"price": "1000.00", "inventory_quantity": 4}},
			}}
		case "2":
			return http.StatusNotFound, nil
		default:
			return http.StatusInternalServerError, nil
		}
	}

	ad := s.adaptador(t, nil)
	out, err := ad.FetchStatus(context.Background(), []channel.ExternalRef{
		{ListingID: "1"}, {ListingID: "2"}, {ListingID: "3"},
	})
	if err == nil {
		t.Error("el 500 del canal tiene que salir del informe, no perderse")
	}
	if len(out) != 2 {
		t.Fatalf("out = %+v", out)
	}
	if out[1].Status != "no_encontrado" {
		t.Errorf("la publicación borrada quedó como %q", out[1].Status)
	}
}

// ------------------------------------------------------ despacho (AckOrder)

func guia() channel.Fulfillment {
	return channel.Fulfillment{
		TrackingNumber: "SERV-99887", Carrier: "Servientrega",
		ShippedAt: time.Date(2026, 9, 4, 15, 30, 0, 0, time.UTC),
	}
}

// despacho saca el objeto fulfillment del cuerpo de un POST /fulfillments.json.
func despacho(t *testing.T, l llamada) map[string]any {
	t.Helper()
	f, ok := l.Cuerpo["fulfillment"].(map[string]any)
	if !ok {
		t.Fatalf("el cuerpo no lleva fulfillment: %v", l.Cuerpo)
	}
	return f
}

// ordenesDeUn saca los fulfillment_order_id que viajaron en un despacho.
func ordenesDeUn(t *testing.T, f map[string]any) []float64 {
	t.Helper()
	lista, ok := f["line_items_by_fulfillment_order"].([]any)
	if !ok {
		t.Fatalf("falta line_items_by_fulfillment_order: %v", f)
	}
	var out []float64
	for _, e := range lista {
		m, _ := e.(map[string]any)
		id, _ := m["fulfillment_order_id"].(float64)
		out = append(out, id)
	}
	return out
}

func TestAckOrderCreaElFulfillmentConLaGuiaYLaTransportadora(t *testing.T) {
	s := nuevaTienda(t)
	s.ordenesDespacho["1001"] = []map[string]any{
		{"id": 10, "status": "open", "assigned_location_id": 111,
			"supported_actions": []string{"create_fulfillment"}},
	}

	ad := s.adaptador(t, nil)
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1001"}, guia()); err != nil {
		t.Fatal(err)
	}
	// El alta cuelga del fulfillment order, no del pedido: el endpoint viejo
	// (POST /orders/{id}/fulfillments.json) ya no existe.
	if s.buscarLlamada(http.MethodGet, "/orders/1001/fulfillment_orders.json") == nil {
		t.Fatal("hay que leer los fulfillment orders antes de despachar")
	}
	posts := s.llamadasDe(http.MethodPost, "/fulfillments.json")
	if len(posts) != 1 {
		t.Fatalf("se crearon %d fulfillments, quería 1", len(posts))
	}
	f := despacho(t, posts[0])
	if ids := ordenesDeUn(t, f); len(ids) != 1 || ids[0] != 10 {
		t.Errorf("fulfillment orders = %v, quería [10]", ids)
	}
	info, _ := f["tracking_info"].(map[string]any)
	if info["number"] != "SERV-99887" || info["company"] != "Servientrega" {
		t.Errorf("tracking_info = %v; sin guía ni transportadora el comprador no ve nada", info)
	}
	// Sin notify_customer no sale el correo de «pedido enviado», que es el
	// único sitio donde el comprador lee la guía.
	if f["notify_customer"] != true {
		t.Errorf("notify_customer = %v", f["notify_customer"])
	}
}

func TestAckOrderCreaUnFulfillmentPorUbicacion(t *testing.T) {
	s := nuevaTienda(t)
	// Shopify solo deja agrupar fulfillment orders de la misma ubicación:
	// mandar bodega y punto de venta en el mismo POST es un 422.
	s.ordenesDespacho["1002"] = []map[string]any{
		{"id": 10, "status": "open", "assigned_location_id": 111,
			"supported_actions": []string{"create_fulfillment"}},
		{"id": 11, "status": "open", "assigned_location_id": 222,
			"supported_actions": []string{"create_fulfillment"}},
		{"id": 12, "status": "open", "assigned_location_id": 111,
			"supported_actions": []string{"create_fulfillment"}},
	}

	ad := s.adaptador(t, nil)
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1002"}, guia()); err != nil {
		t.Fatal(err)
	}
	posts := s.llamadasDe(http.MethodPost, "/fulfillments.json")
	if len(posts) != 2 {
		t.Fatalf("se crearon %d fulfillments, quería uno por ubicación (2)", len(posts))
	}
	if ids := ordenesDeUn(t, despacho(t, posts[0])); len(ids) != 2 || ids[0] != 10 || ids[1] != 12 {
		t.Errorf("primera ubicación = %v, quería [10 12]", ids)
	}
	if ids := ordenesDeUn(t, despacho(t, posts[1])); len(ids) != 1 || ids[0] != 11 {
		t.Errorf("segunda ubicación = %v, quería [11]", ids)
	}
}

func TestAckOrderIgnoraLasPartesQueShopifyNoDejaDespachar(t *testing.T) {
	s := nuevaTienda(t)
	s.ordenesDespacho["1003"] = []map[string]any{
		{"id": 10, "status": "open", "assigned_location_id": 111,
			"supported_actions": []string{"create_fulfillment"}},
		// Asignada a un servicio de fulfillment externo: no lleva
		// create_fulfillment y mandarla igual es un 422 que tumba el despacho
		// entero, incluida la parte que sí se podía enviar.
		{"id": 11, "status": "open", "assigned_location_id": 111,
			"supported_actions": []string{"request_fulfillment", "hold"}},
	}

	ad := s.adaptador(t, nil)
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1003"}, guia()); err != nil {
		t.Fatal(err)
	}
	posts := s.llamadasDe(http.MethodPost, "/fulfillments.json")
	if len(posts) != 1 {
		t.Fatalf("posts = %d", len(posts))
	}
	if ids := ordenesDeUn(t, despacho(t, posts[0])); len(ids) != 1 || ids[0] != 10 {
		t.Errorf("fulfillment orders = %v, quería solo la despachable [10]", ids)
	}
}

func TestAckOrderDeUnPedidoYaDespachadoNoDuplicaElFulfillment(t *testing.T) {
	s := nuevaTienda(t)
	s.ordenesDespacho["1004"] = []map[string]any{
		{"id": 10, "status": "closed", "assigned_location_id": 111, "supported_actions": []string{}},
	}

	ad := s.adaptador(t, nil)
	// El trabajo se reintenta: fallar por llegar dos veces dejaría el pedido
	// en error para siempre aunque el despacho sí se hubiera informado.
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1004"}, guia()); err != nil {
		t.Fatalf("el pedido ya estaba despachado, no es un fallo: %v", err)
	}
	if n := s.contar(http.MethodPost, "/fulfillments.json"); n != 0 {
		t.Errorf("se crearon %d fulfillments sobre un pedido ya despachado", n)
	}
}

func TestAckOrderConTodoRetenidoDaUnErrorAccionableYNoReintentable(t *testing.T) {
	s := nuevaTienda(t)
	s.ordenesDespacho["1005"] = []map[string]any{
		{"id": 10, "status": "on_hold", "assigned_location_id": 111,
			"supported_actions": []string{"release_hold"}},
	}

	ad := s.adaptador(t, nil)
	err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1005"}, guia())
	if err == nil {
		t.Fatal("no se despachó nada: darlo por bueno sería marcar el pedido como enviado sin estarlo")
	}
	if !strings.Contains(err.Error(), "retenidas") {
		t.Errorf("error = %v; debía decir por qué no se pudo", err)
	}
	if channel.EsReintentable(err) {
		t.Error("reintentarlo no libera la retención: no debe ser reintentable")
	}
}

func TestAckOrderSinFulfillmentOrdersLoDice(t *testing.T) {
	s := nuevaTienda(t)
	s.ordenesDespacho["1006"] = []map[string]any{}

	ad := s.adaptador(t, nil)
	err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "1006"}, guia())
	if err == nil || !strings.Contains(err.Error(), "no tiene fulfillment orders") {
		t.Fatalf("error = %v", err)
	}
	if n := s.contar(http.MethodPost, "/fulfillments.json"); n != 0 {
		t.Errorf("no había nada que despachar y se hicieron %d POST", n)
	}
}

func TestAckOrderSinPedidoNoLlamaANadie(t *testing.T) {
	s := nuevaTienda(t)
	ad := s.adaptador(t, nil)
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{}, guia()); err == nil {
		t.Fatal("sin id de pedido no hay fulfillment orders que buscar")
	}
	if len(s.llamadas) != 0 {
		t.Errorf("se llamó a la tienda igual: %+v", s.llamadas)
	}
}

// ------------------------------------------------------ stock por ubicación

// FetchStatus y ListRemote leían variants[].inventory_quantity, que la
// documentación define como el agregado de TODAS las ubicaciones, mientras
// fijarInventario escribe en una sola: en una tienda con bodega y punto de
// venta el informe nunca coincidía con lo que Integra empujó.
func TestElStockLeidoEsElDeLaUbicacionEnLaQueSeEscribe(t *testing.T) {
	s := nuevaTienda(t)
	s.ubicaciones = []map[string]any{
		{"id": 111, "active": true}, // la principal: la de id más bajo
		{"id": 222, "active": true}, // punto de venta
	}
	// 4 unidades en la bodega y 8 en el punto de venta: el agregado es 12.
	s.stock = map[int64]map[int64]int{
		111: {99: 4},
		222: {99: 8},
	}
	variante := map[string]any{
		"id": 66, "sku": "SKU-300", "price": "150000.00",
		"inventory_item_id": 99, "inventory_quantity": 12,
	}
	s.estadoProducto = func(id string) (int, map[string]any) {
		return http.StatusOK, map[string]any{"product": map[string]any{
			"status": "active", "handle": "impresora",
			"variants": []map[string]any{variante},
		}}
	}
	s.paginasProductos = [][]map[string]any{{{
		"id": 55, "title": "Impresora", "status": "active",
		"variants": []map[string]any{variante},
	}}}

	ad := s.adaptador(t, nil)

	est, err := ad.FetchStatus(context.Background(), []channel.ExternalRef{{ListingID: "55"}})
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if len(est) != 1 || est[0].Quantity != 4 {
		t.Fatalf("Quantity = %+v, quería 4 (lo de la ubicación 111): se publicó el agregado de todas", est)
	}

	l := s.buscarLlamada(http.MethodGet, "/inventory_levels.json")
	if l == nil {
		t.Fatal("no se consultó inventory_levels: el stock salió del agregado de la variante")
	}
	if l.Query.Get("location_ids") != "111" {
		t.Errorf("location_ids = %q: hay que leer la misma ubicación en la que escribe fijarInventario",
			l.Query.Get("location_ids"))
	}
	if l.Query.Get("inventory_item_ids") != "99" {
		t.Errorf("inventory_item_ids = %q", l.Query.Get("inventory_item_ids"))
	}

	pag, err := ad.ListRemote(context.Background(), channel.Cursor{})
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}
	if len(pag.Items) != 1 || len(pag.Items[0].Variants) != 1 {
		t.Fatalf("el listado remoto trajo %+v", pag.Items)
	}
	if q := pag.Items[0].Variants[0].Quantity; q != 4 {
		t.Errorf("Quantity = %d, quería 4: el listado remoto sigue publicando el agregado", q)
	}
}

// Con una sola ubicación el número coincide con el agregado, pero la lectura
// tiene que seguir siendo la de la ubicación: si la variante no está
// abastecida ahí, el disponible es cero aunque otra ubicación tenga
// existencias.
func TestSinExistenciasEnLaUbicacionElEstadoNoHeredaLasDeOtra(t *testing.T) {
	s := nuevaTienda(t)
	s.ubicaciones = []map[string]any{{"id": 111, "active": true}, {"id": 222, "active": true}}
	s.stock = map[int64]map[int64]int{222: {99: 30}} // todo en la otra ubicación
	s.estadoProducto = func(id string) (int, map[string]any) {
		return http.StatusOK, map[string]any{"product": map[string]any{
			"status": "active", "handle": "impresora",
			"variants": []map[string]any{{
				"id": 66, "price": "150000.00",
				"inventory_item_id": 99, "inventory_quantity": 30,
			}},
		}}
	}

	est, err := s.adaptador(t, nil).FetchStatus(context.Background(),
		[]channel.ExternalRef{{ListingID: "55"}})
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if est[0].Quantity != 0 {
		t.Errorf("Quantity = %d, quería 0: el informe esconde que la ubicación publicada está vacía",
			est[0].Quantity)
	}
}

// ------------------------------------------------------ ventana de 60 días

// Sin read_all_orders, Shopify entrega solo los últimos 60 días y no avisa.
// Como el núcleo avanza la marca de agua con lo que sí llegó, los pedidos del
// hueco no se vuelven a pedir nunca: la ingesta informa «al día».
func TestLaIngestaSeDetieneSiLaMarcaSalioDeLaVentanaDe60Dias(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{{
		"id": 5001, "name": "#1001", "financial_status": "paid",
		"created_at": "2026-01-01T10:00:00-05:00", "updated_at": "2026-01-01T10:00:00-05:00",
		"currency": "COP", "total_price": "1000.00",
	}}}
	ad := s.adaptador(t, nil)
	desde := time.Now().AddDate(0, 0, -90)

	_, err := ad.FetchOrders(context.Background(), desde, channel.Cursor{})
	if err == nil {
		t.Fatal("una marca de 90 días con un token sin read_all_orders tiene que parar la ingesta: " +
			"si no, el hueco se cierra solo y esos pedidos no llegan nunca a Odoo")
	}
	if !strings.Contains(err.Error(), "read_all_orders") {
		t.Errorf("el error no dice qué falta: %v", err)
	}
	if channel.EsReintentable(err) {
		t.Error("reintentar no lo arregla: falta un permiso en la app, no es un fallo pasajero")
	}
	if s.contar(http.MethodGet, "/orders.json") != 0 {
		t.Error("se llegó a pedir la página: con la respuesta recortada la marca avanzaría igual")
	}
}

// Con read_all_orders el token sí lee más atrás de 60 días: la ingesta sigue.
func TestConReadAllOrdersLaIngestaAntiguaSigueAdelante(t *testing.T) {
	s := nuevaTienda(t)
	s.alcances = append(s.alcances, "read_all_orders")
	s.paginasPedidos = [][]map[string]any{{{
		"id": 5001, "name": "#1001", "financial_status": "paid",
		"created_at": "2026-01-01T10:00:00-05:00", "updated_at": "2026-01-01T10:00:00-05:00",
		"currency": "COP", "total_price": "1000.00",
	}}}

	pag, err := s.adaptador(t, nil).FetchOrders(context.Background(),
		time.Now().AddDate(0, 0, -90), channel.Cursor{})
	if err != nil {
		t.Fatalf("FetchOrders: %v", err)
	}
	if len(pag.Orders) != 1 {
		t.Fatalf("con read_all_orders el pedido antiguo tiene que llegar: %+v", pag.Orders)
	}
}

// La comprobación no puede costar una llamada de permisos en cada ronda
// normal: la marca de agua está casi siempre dentro de la ventana.
func TestLaMarcaRecienteNoConsultaLosPermisos(t *testing.T) {
	s := nuevaTienda(t)
	s.paginasPedidos = [][]map[string]any{{}}

	if _, err := s.adaptador(t, nil).FetchOrders(context.Background(),
		time.Now().AddDate(0, 0, -7), channel.Cursor{}); err != nil {
		t.Fatalf("FetchOrders: %v", err)
	}
	if n := s.contar(http.MethodGet, "/admin/oauth/access_scopes.json"); n != 0 {
		t.Errorf("se consultaron los permisos %d veces con la marca dentro de la ventana", n)
	}
}
