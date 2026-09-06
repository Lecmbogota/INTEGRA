package woocommerce

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

// Las pruebas levantan una tienda simulada sobre TLS (el adaptador exige
// https) y comprueban lo que sale por el cable: la query, las cabeceras y el
// cuerpo JSON. No hay credenciales de ninguna tienda real.

const (
	claveDePrueba   = "ck_1234567890abcdef"
	secretoDePrueba = "cs_secreto_que_no_debe_salir"
)

// desfaseBogota es la diferencia de la tienda de MDV respecto a UTC. La tienda
// simulada la usa para reproducir el comportamiento de WooCommerce: los
// filtros de fecha se comparan contra la columna en hora local del sitio salvo
// que se pida dates_are_gmt.
const desfaseBogota = -5 * time.Hour

type peticion struct {
	Metodo string
	Ruta   string
	Query  url.Values
	Auth   string
	Cuerpo map[string]any
}

type tienda struct {
	t *testing.T
	*httptest.Server

	mu         sync.Mutex
	peticiones []peticion
}

func nuevaTienda(t *testing.T, manejar http.HandlerFunc) *tienda {
	t.Helper()
	td := &tienda{t: t}
	td.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		datos, _ := io.ReadAll(r.Body)
		p := peticion{
			Metodo: r.Method, Ruta: r.URL.Path,
			Query: r.URL.Query(), Auth: r.Header.Get("Authorization"),
		}
		if len(datos) > 0 {
			_ = json.Unmarshal(datos, &p.Cuerpo)
		}
		td.mu.Lock()
		td.peticiones = append(td.peticiones, p)
		td.mu.Unlock()

		r.Body = io.NopCloser(bytes.NewReader(datos))
		manejar(w, r)
	}))
	t.Cleanup(td.Close)
	return td
}

// adaptador construye el adaptador por la factoría registrada, para que la
// prueba pase también por la validación de credenciales.
func (td *tienda) adaptador() *Adaptador {
	td.t.Helper()
	ad, err := channel.New(channel.WooCommerce, channel.Config{Credentials: map[string]string{
		"url": td.URL, "consumer_key": claveDePrueba, "consumer_secret": secretoDePrueba,
	}})
	if err != nil {
		td.t.Fatalf("construyendo el adaptador: %v", err)
	}
	a := ad.(*Adaptador)
	a.cli = td.Client() // el certificado del servidor de pruebas es autofirmado
	return a
}

func (td *tienda) buscar(metodo, ruta string) *peticion {
	td.mu.Lock()
	defer td.mu.Unlock()
	for i := range td.peticiones {
		if td.peticiones[i].Metodo == metodo && td.peticiones[i].Ruta == ruta {
			return &td.peticiones[i]
		}
	}
	return nil
}

func responder(w http.ResponseWriter, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cuerpo)
}

func fecha(t *testing.T, s string) time.Time {
	t.Helper()
	f, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("fecha de prueba inválida %q: %v", s, err)
	}
	return f
}

// ------------------------------------------------------- autenticación

func TestLaFactoriaRechazaLasTiendasSinHTTPS(t *testing.T) {
	casos := []struct {
		url   string
		falla bool
	}{
		{"http://localhost:8080", true},
		{"HTTP://tienda.com", true},
		{"tienda.com", true},
		{"", true},
		{"https://tienda.com/", false},
	}
	for _, c := range casos {
		_, err := channel.New(channel.WooCommerce, channel.Config{Credentials: map[string]string{
			"url": c.url, "consumer_key": claveDePrueba, "consumer_secret": secretoDePrueba,
		}})
		if c.falla && err == nil {
			t.Errorf("%q: debía rechazarse porque sobre HTTP Woo exige OAuth 1.0a", c.url)
		}
		if !c.falla && err != nil {
			t.Errorf("%q: debía aceptarse: %v", c.url, err)
		}
	}
}

func TestLasClavesViajanPorCabeceraYNoPorLaQuery(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, []any{})
	})
	if _, err := td.adaptador().ListRemote(context.Background(), channel.Cursor{}); err != nil {
		t.Fatalf("ListRemote: %v", err)
	}

	p := td.buscar(http.MethodGet, "/wp-json/wc/v3/products")
	if p == nil {
		t.Fatal("no llegó la petición de productos")
	}
	if p.Query.Has("consumer_key") || p.Query.Has("consumer_secret") {
		t.Errorf("las claves siguen en la query: %v", p.Query)
	}
	esperado := "Basic " + base64.StdEncoding.EncodeToString([]byte(claveDePrueba+":"+secretoDePrueba))
	if p.Auth != esperado {
		t.Errorf("Authorization = %q, se esperaba autenticación básica con la clave y el secreto", p.Auth)
	}
}

func TestElErrorDeRedNoArrastraLaURLNiElSecreto(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {})
	a := td.adaptador()
	td.Close() // la tienda deja de responder: error de transporte

	_, err := a.ListRemote(context.Background(), channel.Cursor{})
	if err == nil {
		t.Fatal("se esperaba un error de transporte")
	}
	// Ese texto acaba en channel_accounts.probada_msg y en jobs.last_error, que
	// la API devuelve a cualquier usuario.
	if strings.Contains(err.Error(), secretoDePrueba) || strings.Contains(err.Error(), claveDePrueba) {
		t.Errorf("el error filtra las credenciales: %q", err.Error())
	}
	if strings.Contains(err.Error(), "wp-json") {
		t.Errorf("el error arrastra la URL de la petición: %q", err.Error())
	}
}

// ------------------------------------------------------------- pedidos

// pedidoFalso es un pedido de la tienda simulada, con sus fechas en UTC.
type pedidoFalso struct {
	id                 int64
	creado, modificado time.Time
}

// tiendaDePedidos imita el filtro de fechas de WooCommerce: `modified_after`
// se compara contra post_modified (hora local del sitio) y solo con
// dates_are_gmt=true contra post_modified_gmt.
func tiendaDePedidos(t *testing.T, pedidos []pedidoFalso) *tienda {
	return nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		enGMT := q.Get("dates_are_gmt") == "true"
		corte, hayCorte := time.Time{}, q.Get("modified_after") != ""
		if hayCorte {
			var err error
			corte, err = time.Parse(formatoFecha, q.Get("modified_after"))
			if err != nil {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"code":"rest_invalid_param"}`)
				return
			}
		}
		salida := []map[string]any{}
		for _, p := range pedidos {
			columna := p.modificado
			if !enGMT {
				columna = columna.Add(desfaseBogota) // post_modified, hora del sitio
			}
			if hayCorte && !columna.After(corte) {
				continue
			}
			salida = append(salida, map[string]any{
				"id": p.id, "number": "W" + time.Duration(p.id).String(),
				"status":            "processing",
				"date_created_gmt":  p.creado.UTC().Format(formatoFecha),
				"date_modified_gmt": p.modificado.UTC().Format(formatoFecha),
				"currency":          "COP", "total": "115000.00",
				"total_tax": "0.00", "shipping_total": "15000.00",
				"billing": map[string]any{"first_name": "Ana", "last_name": "Ruiz"},
				"line_items": []map[string]any{{
					"id": 1, "name": "Silla", "sku": "MDV-1",
					"quantity": 1, "price": 100000.0, "total": "100000.00",
				}},
			})
		}
		responder(w, salida)
	})
}

func TestFetchOrdersFiltraEnGMTYNoSeDejaPedidosBajoLaMarca(t *testing.T) {
	// Escenario del informe: tienda en Bogotá (UTC-5). El pedido A ya se
	// ingirió y dejó la marca de agua en su date_modified_gmt; el pedido B
	// entra quince minutos después, dentro de la franja de desfase.
	marca := fecha(t, "2026-09-01T15:00:00Z")
	b := pedidoFalso{id: 2,
		creado:     fecha(t, "2026-09-01T15:15:00Z"),
		modificado: fecha(t, "2026-09-01T15:15:00Z")}
	td := tiendaDePedidos(t, []pedidoFalso{b})

	pag, err := td.adaptador().FetchOrders(context.Background(), marca, channel.Cursor{})
	if err != nil {
		t.Fatalf("FetchOrders: %v", err)
	}
	if len(pag.Orders) != 1 {
		t.Fatalf("se ingirieron %d pedidos; el pedido de la franja de desfase se perdió", len(pag.Orders))
	}
	if !pag.Orders[0].UpdatedAt.Equal(b.modificado) {
		t.Errorf("UpdatedAt = %v, se esperaba %v: sin él la marca de agua no avanza con las modificaciones",
			pag.Orders[0].UpdatedAt, b.modificado)
	}

	p := td.buscar(http.MethodGet, "/wp-json/wc/v3/orders")
	if p.Query.Get("dates_are_gmt") != "true" {
		t.Errorf("falta dates_are_gmt=true: %v", p.Query)
	}
	if p.Query.Get("modified_after") != "2026-09-01T15:00:00" {
		t.Errorf("modified_after = %q, se esperaba la marca en UTC", p.Query.Get("modified_after"))
	}
}

func TestFetchOrdersPaginaConLaCabeceraDeTotalDePaginas(t *testing.T) {
	var totalPaginas string
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-WP-Total", "101")
		w.Header().Set("X-WP-TotalPages", totalPaginas)
		responder(w, []map[string]any{{
			"id": 1, "number": "W1", "status": "processing",
			"date_created_gmt": "2026-09-01T10:00:00", "date_modified_gmt": "2026-09-01T10:00:00",
			"currency": "COP", "total": "1000.00",
		}})
	})
	a := td.adaptador()
	totalPaginas = "2"

	pag, err := a.FetchOrders(context.Background(), time.Time{}, channel.Cursor{})
	if err != nil {
		t.Fatalf("FetchOrders: %v", err)
	}
	if pag.Done {
		t.Error("la primera de dos páginas se dio por última: los pedidos de la segunda no se piden nunca")
	}
	if pag.Next.Page != 2 {
		t.Fatalf("Next.Page = %d, se esperaba 2", pag.Next.Page)
	}

	totalPaginas = "2"
	pag2, err := a.FetchOrders(context.Background(), time.Time{}, pag.Next)
	if err != nil {
		t.Fatalf("FetchOrders (página 2): %v", err)
	}
	if !pag2.Done {
		t.Error("la última página no se marcó como final")
	}
	td.mu.Lock()
	ultima := td.peticiones[len(td.peticiones)-1]
	td.mu.Unlock()
	if ultima.Query.Get("page") != "2" {
		t.Errorf("page = %q: el cursor no llega a la API y se repite la primera página",
			ultima.Query.Get("page"))
	}
}

func TestAckOrderDejaLaGuiaComoNotaAntesDeCompletar(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{"id": 77})
	})
	err := td.adaptador().AckOrder(context.Background(),
		channel.ExternalRef{ListingID: "77"},
		channel.Fulfillment{TrackingNumber: "ABC123", Carrier: "Servientrega",
			ShippedAt: fecha(t, "2026-09-02T13:00:00Z")})
	if err != nil {
		t.Fatalf("AckOrder: %v", err)
	}

	nota := td.buscar(http.MethodPost, "/wp-json/wc/v3/orders/77/notes")
	if nota == nil {
		t.Fatal("la guía no llegó a la tienda: no se creó la nota del pedido")
	}
	texto, _ := nota.Cuerpo["note"].(string)
	if !strings.Contains(texto, "ABC123") || !strings.Contains(texto, "Servientrega") {
		t.Errorf("la nota no lleva guía ni transportadora: %q", texto)
	}
	if nota.Cuerpo["customer_note"] != true {
		t.Error("la nota no es visible para el comprador, que es quien necesita la guía")
	}
	td.mu.Lock()
	defer td.mu.Unlock()
	if td.peticiones[0].Ruta != "/wp-json/wc/v3/orders/77/notes" {
		t.Error("el pedido se completó antes de dejar la guía")
	}
	if td.peticiones[len(td.peticiones)-1].Cuerpo["status"] != "completed" {
		t.Error("el pedido no se marcó como completado")
	}
}

// ------------------------------------------------------------- catálogo

func productoDePrueba() channel.Product {
	return channel.Product{
		SKU: "MDV-1", Title: "Silla Nórdica", Description: "<p>Descripción nueva</p>",
		Brand: "MDV", Weight: 1.5,
		Images:   []channel.Image{{URL: "https://cdn/1.jpg", Position: 0}},
		Variants: []channel.Variant{{SKU: "MDV-1", RegularPrice: 99900, Quantity: 7}},
	}
}

func TestPublishAdoptadoEnviaLaFichaNueva(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/wp-json/wc/v3/products" {
			responder(w, []map[string]any{{"id": 55, "sku": "MDV-1"}})
			return
		}
		responder(w, map[string]any{"id": 55})
	})

	res, err := td.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba()})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if !res.Adopted || res.Ref.ListingID != "55" {
		t.Fatalf("se esperaba adoptar la publicación 55: %+v", res)
	}

	// El motor guarda el hash de contenido en cuanto Publish responde bien: si
	// la adopción no escribe, el título nuevo no llega a la tienda jamás.
	put := td.buscar(http.MethodPut, "/wp-json/wc/v3/products/55")
	if put == nil {
		t.Fatal("la adopción no envió la ficha: el cambio de contenido se pierde")
	}
	if put.Cuerpo["name"] != "Silla Nórdica" || put.Cuerpo["description"] != "<p>Descripción nueva</p>" {
		t.Errorf("la ficha enviada no lleva el contenido nuevo: %v", put.Cuerpo)
	}
	if put.Cuerpo["images"] == nil || put.Cuerpo["attributes"] == nil || put.Cuerpo["weight"] != "1.500" {
		t.Errorf("faltan imágenes, marca o peso en la ficha enviada: %v", put.Cuerpo)
	}
	if td.buscar(http.MethodPost, "/wp-json/wc/v3/products") != nil {
		t.Error("se creó un producto nuevo con un SKU que ya existía")
	}
}

func TestPublishAdoptadoEnSimulacroNoEscribe(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, []map[string]any{{"id": 55, "sku": "MDV-1"}})
	})
	if _, err := td.adaptador().Publish(context.Background(),
		channel.PublishRequest{Product: productoDePrueba(), DryRun: true}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if td.buscar(http.MethodPut, "/wp-json/wc/v3/products/55") != nil {
		t.Error("el simulacro escribió en la tienda")
	}
}

func TestUpdateMandaLaFichaCompleta(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{"id": 55})
	})
	if _, err := td.adaptador().Update(context.Background(), channel.UpdateRequest{
		Ref: channel.ExternalRef{ListingID: "55"}, Product: productoDePrueba(),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	put := td.buscar(http.MethodPut, "/wp-json/wc/v3/products/55")
	if put == nil {
		t.Fatal("Update no escribió nada")
	}
	for _, campo := range []string{"name", "description", "weight", "images", "attributes"} {
		if put.Cuerpo[campo] == nil {
			t.Errorf("Update no envía %q: ese cambio entra en el hash de contenido y se daría por sincronizado", campo)
		}
	}
}

func TestPublishSinImagenesNoVaciaLaGaleriaDeLaTienda(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			responder(w, []any{})
			return
		}
		responder(w, map[string]any{"id": 90})
	})
	p := productoDePrueba()
	p.Images = nil
	if _, err := td.adaptador().Publish(context.Background(), channel.PublishRequest{Product: p}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	post := td.buscar(http.MethodPost, "/wp-json/wc/v3/products")
	if post == nil {
		t.Fatal("no se creó el producto")
	}
	if _, hay := post.Cuerpo["images"]; hay {
		t.Error("se envió images vacío, lo que borraría la foto principal y la galería")
	}
	// Lo que sí se crea en el alta y no en la actualización.
	if post.Cuerpo["sku"] != "MDV-1" || post.Cuerpo["regular_price"] != "99900.00" ||
		post.Cuerpo["status"] != "draft" {
		t.Errorf("el alta perdió sku, precio o estado: %v", post.Cuerpo)
	}
}

// ------------------------------------------------------------- precios

func TestUpdatePriceMandaLaVentanaEnGMT(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{"id": 55})
	})
	bogota := time.FixedZone("-05", -5*60*60)
	inicia := time.Date(2026, 9, 10, 0, 0, 0, 0, bogota)
	termina := time.Date(2026, 9, 12, 23, 59, 59, 0, bogota)

	res, err := td.adaptador().UpdatePrice(context.Background(), []channel.PriceUpdate{{
		Ref: channel.ExternalRef{ListingID: "55"}, RegularPrice: 99900,
		SalePrice: 79900, StartsAt: &inicia, EndsAt: &termina,
	}})
	if err != nil || !res[0].OK {
		t.Fatalf("UpdatePrice: %v / %+v", err, res)
	}
	put := td.buscar(http.MethodPut, "/wp-json/wc/v3/products/55")
	if put.Cuerpo["date_on_sale_from_gmt"] != "2026-09-10T05:00:00" {
		t.Errorf("date_on_sale_from_gmt = %v: la ventana llega corrida cinco horas",
			put.Cuerpo["date_on_sale_from_gmt"])
	}
	if put.Cuerpo["date_on_sale_to_gmt"] != "2026-09-13T04:59:59" {
		t.Errorf("date_on_sale_to_gmt = %v", put.Cuerpo["date_on_sale_to_gmt"])
	}
	if _, hay := put.Cuerpo["date_on_sale_from"]; hay {
		t.Error("se envió date_on_sale_from, que WooCommerce interpreta en la hora del sitio")
	}
}

func TestUpdatePriceSinOfertaLimpiaLaVentanaAnterior(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{"id": 55})
	})
	if _, err := td.adaptador().UpdatePrice(context.Background(), []channel.PriceUpdate{{
		Ref: channel.ExternalRef{ListingID: "55"}, RegularPrice: 120000,
	}}); err != nil {
		t.Fatalf("UpdatePrice: %v", err)
	}
	put := td.buscar(http.MethodPut, "/wp-json/wc/v3/products/55")
	for _, campo := range []string{"sale_price", "date_on_sale_from_gmt", "date_on_sale_to_gmt"} {
		if put.Cuerpo[campo] != "" {
			t.Errorf("%s = %v: una ventana heredada deja la oferta siguiente sin aplicar",
				campo, put.Cuerpo[campo])
		}
	}
}

// ------------------------------------------------------------- estado

func TestFetchStatusReportaLoBorradoYNoSeTragaLosFallos(t *testing.T) {
	td := nuevaTienda(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wp-json/wc/v3/products/9":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"woocommerce_rest_product_invalid_id"}`)
		case "/wp-json/wc/v3/products/7":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"code":"internal_server_error"}`)
		default:
			responder(w, map[string]any{"status": "publish", "price": "99900.00"})
		}
	})
	a := td.adaptador()

	est, err := a.FetchStatus(context.Background(), []channel.ExternalRef{{ListingID: "9"}})
	if err != nil {
		t.Fatalf("FetchStatus: %v", err)
	}
	if len(est) != 1 || est[0].Status != "eliminado" {
		t.Fatalf("una publicación borrada en la tienda desapareció del informe: %+v", est)
	}

	if _, err := a.FetchStatus(context.Background(), []channel.ExternalRef{{ListingID: "7"}}); err == nil {
		t.Error("una tienda caída se reportó como «todo en orden»")
	}
}
