package mercadolibre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
)

// servidorFalso imita lo justo de api.mercadolibre.com para probar el
// adaptador sin red. Cada prueba ajusta los campos que le importan.
type servidorFalso struct {
	t *testing.T
	*httptest.Server

	mu            sync.Mutex
	tags          []string
	automatizados map[string]bool
	cuerpoItems   map[string]any // último POST /items
	canjes        int
	refreshVisto  string
	cabeceraEnvio string
	queryOrdenes  string

	// Despacho: envíos por pedido, detalle por envío y lo que se escribió.
	envios         map[string][]map[string]any
	detalleEnvio   map[string]map[string]any
	cabeceraNuevo  string
	putEnvio       map[string]any
	rutaPutEnvio   string
	notificacion   map[string]any
	rutaNotificada string
}

func nuevoServidor(t *testing.T) *servidorFalso {
	t.Helper()
	s := &servidorFalso{
		t: t, automatizados: map[string]bool{},
		envios: map[string][]map[string]any{}, detalleEnvio: map[string]map[string]any{},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		s.mu.Lock()
		s.canjes++
		s.refreshVisto = r.Form.Get("refresh_token")
		n := s.canjes
		s.mu.Unlock()
		if r.Form.Get("refresh_token") == "TG-muerto" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Error validating grant"}`)
			return
		}
		responder(w, map[string]any{
			"access_token": "APP_USR-nuevo-" + itoa(n), "token_type": "bearer",
			"expires_in": 10800, "refresh_token": "TG-nuevo-" + itoa(n), "user_id": 123,
		})
	})
	mux.HandleFunc("GET /users/me", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer APP_USR-") {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"message":"invalid token","error":"unauthorized"}`)
			return
		}
		s.mu.Lock()
		tags := s.tags
		s.mu.Unlock()
		responder(w, map[string]any{"id": 123, "nickname": "TEST0548", "tags": tags})
	})
	mux.HandleFunc("GET /users/123/items/search", func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{"results": []string{}, "paging": map[string]int{"total": 0}})
	})
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		s.mu.Lock()
		s.cuerpoItems = cuerpo
		s.mu.Unlock()
		responder(w, map[string]any{
			"id": "MCO1", "permalink": "https://articulo.mercadolibre.com.co/MCO-1",
			"user_product_id": "MCOU1", "status": "paused",
			"warnings": []map[string]string{{"code": "item.pictures.slow", "message": "imagen lenta"}},
		})
	})
	mux.HandleFunc("POST /items/MCO1/description", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
	})
	mux.HandleFunc("PUT /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		precio, conPrecio := cuerpo["price"]
		s.mu.Lock()
		auto := s.automatizados[id]
		s.mu.Unlock()
		if conPrecio && auto {
			if len(cuerpo) == 1 {
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"message":"Cannot modify price on items with dynamic pricing","error":"item.price.not_modifiable","status":400,"cause":[]}`)
				return
			}
			responder(w, map[string]any{"id": id, "price": 999.0, "warnings": []map[string]string{
				{"code": "item.price.not_modifiable", "message": "Cannot modify price on items with dynamic pricing"}}})
			return
		}
		resp := map[string]any{"id": id}
		if conPrecio {
			resp["price"] = precio
		}
		responder(w, resp)
	})
	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		var out []map[string]any
		for _, id := range strings.Split(r.URL.Query().Get("ids"), ",") {
			out = append(out, map[string]any{"code": 200, "body": map[string]any{
				"id": id, "status": "paused", "sub_status": []string{"out_of_stock"},
				"price": 100.0, "available_quantity": 0,
			}})
		}
		responder(w, out)
	})
	mux.HandleFunc("GET /orders/search", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.queryOrdenes = r.URL.RawQuery
		s.mu.Unlock()
		responder(w, map[string]any{
			"results": []map[string]any{{
				"id": 2000003508419013, "status": "paid",
				"date_created":      "2026-09-01T10:01:50.000-05:00",
				"date_last_updated": "2026-09-03T02:55:49.811Z",
				"total_amount":      129950.0, "shipping_cost": 0, "currency_id": "COP",
				"taxes":    map[string]any{"amount": nil, "currency_id": nil},
				"buyer":    map[string]any{"id": 89660613, "nickname": "COMPRADOR", "first_name": "Ana", "last_name": "Pérez"},
				"shipping": map[string]any{"id": 555},
				"order_items": []map[string]any{{
					"item":     map[string]any{"id": "MCO1", "title": "Impresora térmica", "seller_sku": "SKU-001", "variation_id": nil},
					"quantity": 2, "unit_price": 64975.0, "currency_id": "COP",
				}},
			}},
			"paging": map[string]int{"total": 1, "offset": 0, "limit": 50},
		})
	})
	mux.HandleFunc("GET /shipments/555", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.cabeceraEnvio = r.Header.Get("X-Api-Version")
		s.mu.Unlock()
		responder(w, map[string]any{
			"status": "ready_to_ship",
			"receiver_address": map[string]any{
				"receiver_name": "Ana Pérez", "receiver_phone": "3001234567",
				"address_line": "Calle 10 # 5-20", "comment": "Apto 301", "zip_code": "110111",
				"city": map[string]string{"name": "Bogotá"}, "state": map[string]string{"name": "Bogotá D.C."},
				"country": map[string]string{"id": "CO", "name": "Colombia"},
			},
			"shipping_option": map[string]any{"cost": 8900.0, "list_cost": 12000.0},
			"lead_time":       map[string]any{"cost": 8900.0},
		})
	})
	mux.HandleFunc("GET /orders/{id}/shipments", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.cabeceraNuevo = r.Header.Get("X-New-Domain")
		lista, hay := s.envios[r.PathValue("id")]
		s.mu.Unlock()
		if !hay {
			// Así responde ML mientras el envío todavía no se asoció.
			w.WriteHeader(204)
			return
		}
		responder(w, lista)
	})
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		responder(w, map[string]any{
			"id": r.PathValue("id"), "buyer": map[string]any{"id": 89660613},
		})
	})
	mux.HandleFunc("GET /shipments/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		det, hay := s.detalleEnvio[r.PathValue("id")]
		s.mu.Unlock()
		if !hay {
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"error":"not_found","message":"shipment not found"}`)
			return
		}
		responder(w, det)
	})
	mux.HandleFunc("PUT /shipments/{id}", func(w http.ResponseWriter, r *http.Request) {
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		s.mu.Lock()
		s.putEnvio, s.rutaPutEnvio = cuerpo, r.URL.Path
		s.mu.Unlock()
		responder(w, map[string]any{"id": r.PathValue("id"), "status": "shipped"})
	})
	mux.HandleFunc("POST /shipments/{id}/seller_notifications", func(w http.ResponseWriter, r *http.Request) {
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		s.mu.Lock()
		s.notificacion, s.rutaNotificada = cuerpo, r.URL.Path
		s.mu.Unlock()
		w.WriteHeader(201)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func responder(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func itoa(n int) string { return strconv.Itoa(n) }

func adaptadorDe(t *testing.T, s *servidorFalso, cred map[string]string, persistir func(context.Context, map[string]string) error) *Adaptador {
	t.Helper()
	viejo := conectores.URLBaseML
	conectores.URLBaseML = s.URL
	t.Cleanup(func() { conectores.URLBaseML = viejo })
	ad, err := channel.New(channel.MercadoLibre, channel.Config{
		Credentials: cred, PersistCredentials: persistir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ad.(*Adaptador)
}

func producto() channel.Product {
	return channel.Product{
		SKU: "SKU-001", Title: "Impresora térmica 80mm Xprinter XP-80C",
		Description: "Impresora de tickets.", CategoryID: "MCO12345",
		Attributes: map[string]string{"BRAND": "Xprinter"},
		Images:     []channel.Image{{URL: "https://integra.example/imagenes/abc/cuadrada_1200"}},
		Variants: []channel.Variant{{
			SKU: "SKU-001", Barcode: "7898095297749", RegularPrice: 189900, Currency: "COP", Quantity: 6,
		}},
	}
}

func TestPublicaConFamilyNameSiElVendedorEsUserProduct(t *testing.T) {
	s := nuevoServidor(t)
	s.tags = []string{"normal", "user_product_seller"}
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)

	res, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.ListingID != "MCO1" {
		t.Errorf("ListingID = %q, quería MCO1", res.Ref.ListingID)
	}
	if _, hay := s.cuerpoItems["title"]; hay {
		t.Error("en el modelo User Products no se debe mandar title")
	}
	if fn, _ := s.cuerpoItems["family_name"].(string); fn != "Impresora térmica 80mm Xprinter XP-80C" {
		t.Errorf("family_name = %q", fn)
	}
	attrs := s.cuerpoItems["attributes"].([]any)
	if !tieneAtributo(attrs, "SELLER_SKU", "SKU-001") {
		t.Error("falta SELLER_SKU: sin él las órdenes no traen el SKU")
	}
	if !tieneAtributo(attrs, "GTIN", "7898095297749") {
		t.Error("el código de barras debía viajar como GTIN")
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "item.pictures.slow") {
		t.Errorf("los warnings de ML deben llegar al núcleo: %v", res.Warnings)
	}
}

func TestPublicaConTituloEnElModeloLegacy(t *testing.T) {
	s := nuevoServidor(t)
	s.tags = []string{"normal"}
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)

	if _, err := ad.Publish(context.Background(), channel.PublishRequest{Product: producto()}); err != nil {
		t.Fatal(err)
	}
	if _, hay := s.cuerpoItems["family_name"]; hay {
		t.Error("en el modelo legacy no se manda family_name")
	}
	if tit, _ := s.cuerpoItems["title"].(string); tit == "" {
		t.Error("en el modelo legacy el title es obligatorio")
	}
}

func tieneAtributo(attrs []any, id, valor string) bool {
	for _, a := range attrs {
		m, _ := a.(map[string]any)
		if m["id"] == id && m["value_name"] == valor {
			return true
		}
	}
	return false
}

func TestRefrescaElTokenUnaVezYGuardaElRefreshNuevo(t *testing.T) {
	s := nuevoServidor(t)
	var guardadas []map[string]string
	ad := adaptadorDe(t, s, map[string]string{
		"app_id": "1", "app_secret": "s", "refresh_token": "TG-inicial",
	}, func(_ context.Context, cred map[string]string) error {
		guardadas = append(guardadas, cred)
		return nil
	})

	refs := []channel.ExternalRef{{ListingID: "MCO1"}, {ListingID: "MCO2"}}
	for i := 0; i < 2; i++ {
		if _, err := ad.FetchStatus(context.Background(), refs); err != nil {
			t.Fatal(err)
		}
	}
	if s.canjes != 1 {
		t.Errorf("se canjeó %d veces; un token vigente no se vuelve a canjear", s.canjes)
	}
	if s.refreshVisto != "TG-inicial" {
		t.Errorf("se canjeó con %q", s.refreshVisto)
	}
	if len(guardadas) != 1 {
		t.Fatalf("la credencial rotada se guardó %d veces, quería 1", len(guardadas))
	}
	g := guardadas[0]
	if g["refresh_token"] != "TG-nuevo-1" || g["access_token"] != "APP_USR-nuevo-1" {
		t.Errorf("se guardó %v: debía llevar el refresh y el access token nuevos", g)
	}
	if g["app_id"] != "1" || g["app_secret"] != "s" {
		t.Error("al guardar hay que conservar app_id y app_secret")
	}
}

func TestAccessTokenCaducadoSeRenuevaYRepiteLaLlamada(t *testing.T) {
	s := nuevoServidor(t)
	var guardadas int
	ad := adaptadorDe(t, s, map[string]string{
		"app_id": "1", "app_secret": "s", "refresh_token": "TG-inicial",
		"access_token": "caducado", // el servidor solo acepta APP_USR-*
	}, func(context.Context, map[string]string) error { guardadas++; return nil })

	if err := ad.perfil(context.Background()); err != nil {
		t.Fatalf("tras un 401 con refresh disponible la llamada debía repetirse: %v", err)
	}
	if s.canjes != 1 || guardadas != 1 {
		t.Errorf("canjes=%d guardadas=%d; quería 1 y 1", s.canjes, guardadas)
	}
}

func TestRefreshTokenMuertoExplicaQueHayQueReautorizar(t *testing.T) {
	s := nuevoServidor(t)
	ad := adaptadorDe(t, s, map[string]string{
		"app_id": "1", "app_secret": "s", "refresh_token": "TG-muerto",
	}, nil)
	_, err := ad.FetchStatus(context.Background(), []channel.ExternalRef{{ListingID: "MCO1"}})
	if err == nil || !strings.Contains(err.Error(), "volver a autorizar") {
		t.Errorf("error = %v; debía decir que hay que volver a autorizar", err)
	}
}

func TestUpdatePriceDetectaLaAutomatizacionDePrecios(t *testing.T) {
	s := nuevoServidor(t)
	s.automatizados["MCO-AUTO"] = true
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)

	res, err := ad.UpdatePrice(context.Background(), []channel.PriceUpdate{
		{Ref: channel.ExternalRef{ListingID: "MCO-AUTO"}, RegularPrice: 150000},
		{Ref: channel.ExternalRef{ListingID: "MCO-LIBRE"}, RegularPrice: 150000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].OK || res[0].Error == nil || !strings.Contains(res[0].Error.Error(), "automatización") {
		t.Errorf("el ítem automatizado debía fallar explicando la causa: ok=%v err=%v", res[0].OK, res[0].Error)
	}
	if !res[1].OK {
		t.Errorf("el ítem sin automatización debía actualizarse: %v", res[1].Error)
	}
}

func TestFetchOrdersTraeDireccionDelEnvioYFiltraPorModificacion(t *testing.T) {
	s := nuevoServidor(t)
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)

	desde := time.Date(2026, 9, 1, 13, 45, 0, 0, time.UTC)
	pag, err := ad.FetchOrders(context.Background(), desde, channel.Cursor{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.queryOrdenes, "seller=123") {
		t.Errorf("la búsqueda debe ir filtrada por vendedor: %s", s.queryOrdenes)
	}
	if !strings.Contains(s.queryOrdenes, "order.date_last_updated.from=2026-09-01T13%3A00%3A00.000%2B00%3A00") {
		t.Errorf("debía filtrar por última modificación redondeada a la hora: %s", s.queryOrdenes)
	}
	if !pag.Done || len(pag.Orders) != 1 {
		t.Fatalf("página: done=%v pedidos=%d", pag.Done, len(pag.Orders))
	}
	o := pag.Orders[0]
	if o.ExternalID != "2000003508419013" || o.Status != "paid" {
		t.Errorf("orden = %+v", o)
	}
	if o.UpdatedAt.IsZero() || !o.UpdatedAt.After(o.OrderedAt) {
		t.Errorf("UpdatedAt=%v OrderedAt=%v", o.UpdatedAt, o.OrderedAt)
	}
	if s.cabeceraEnvio != "2" {
		t.Error("el envío se pide con X-Api-Version: 2 para recibir nombre y teléfono")
	}
	if o.Buyer.Address.City != "Bogotá" || o.Buyer.Address.Line1 != "Calle 10 # 5-20" ||
		o.Buyer.Address.Line2 != "Apto 301" || o.Buyer.Address.Country != "CO" {
		t.Errorf("dirección = %+v", o.Buyer.Address)
	}
	if o.Buyer.Phone != "3001234567" || o.Buyer.Name != "Ana Pérez" {
		t.Errorf("comprador = %+v", o.Buyer)
	}
	if o.Shipping != 8900 {
		t.Errorf("envío = %v", o.Shipping)
	}
	if len(o.Lines) != 1 || o.Lines[0].SKU != "SKU-001" || o.Lines[0].Quantity != 2 || o.Lines[0].TotalPrice != 129950 {
		t.Errorf("líneas = %+v", o.Lines)
	}
	if len(o.Raw) == 0 || !json.Valid(o.Raw) {
		t.Error("Raw debe conservar la orden original")
	}
}

func TestFetchStatusUsaMultigetYComponeElSubestado(t *testing.T) {
	s := nuevoServidor(t)
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)
	st, err := ad.FetchStatus(context.Background(), []channel.ExternalRef{{ListingID: "MCO1"}, {ListingID: "MCO2"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 || st[0].Status != "paused/out_of_stock" {
		t.Errorf("estados = %+v", st)
	}
}

// ------------------------------------------------------ despacho (AckOrder)

// despachoDe siembra un pedido con su envío y devuelve el adaptador listo.
func despachoDe(t *testing.T, s *servidorFalso, orden string, envio map[string]any, cred map[string]string) *Adaptador {
	t.Helper()
	id := envio["id"]
	s.envios[orden] = []map[string]any{{"id": id, "logistic": map[string]any{"direction": "forward"}}}
	s.detalleEnvio[fmt.Sprint(id)] = envio
	if cred == nil {
		cred = map[string]string{}
	}
	cred["access_token"] = "APP_USR-fijo"
	return adaptadorDe(t, s, cred, nil)
}

func guia() channel.Fulfillment {
	return channel.Fulfillment{
		TrackingNumber: "SERV-99887", Carrier: "Servientrega",
		ShippedAt: time.Date(2026, 9, 4, 15, 30, 0, 0, time.UTC),
	}
}

func TestAckOrderEnvioPersonalizadoMarcaEnviadoConLaGuia(t *testing.T) {
	s := nuevoServidor(t)
	ad := despachoDe(t, s, "9001", map[string]any{
		"id": 777, "status": "pending", "mode": "custom", "receiver_id": 42, "speed": 72,
	}, nil)

	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9001"}, guia()); err != nil {
		t.Fatal(err)
	}
	if s.cabeceraNuevo != "true" {
		t.Error("los envíos del pedido se piden con X-New-Domain: true; la vista vieja se retira")
	}
	if s.rutaPutEnvio != "/shipments/777" {
		t.Fatalf("se escribió en %q, quería /shipments/777", s.rutaPutEnvio)
	}
	if s.putEnvio["status"] != "shipped" {
		t.Errorf("status = %v", s.putEnvio["status"])
	}
	if s.putEnvio["tracking_number"] != "SERV-99887" {
		t.Errorf("la guía no viajó: %v", s.putEnvio["tracking_number"])
	}
	// receiver_id es obligatorio para ML y sale del envío, no de la referencia.
	if s.putEnvio["receiver_id"] != 42.0 {
		t.Errorf("receiver_id = %v, quería 42", s.putEnvio["receiver_id"])
	}
	// La promesa de entrega se reenvía tal cual: inventarla le prometería al
	// comprador una fecha que nadie se comprometió a cumplir.
	if s.putEnvio["speed"] != 72.0 {
		t.Errorf("speed = %v, quería la que ya tenía el envío (72)", s.putEnvio["speed"])
	}
}

func TestAckOrderPersonalizadoSacaElCompradorDelPedidoSiElEnvioNoLoTrae(t *testing.T) {
	s := nuevoServidor(t)
	ad := despachoDe(t, s, "9001", map[string]any{
		"id": 777, "status": "pending", "mode": "custom",
	}, nil)

	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9001"}, guia()); err != nil {
		t.Fatal(err)
	}
	if s.putEnvio["receiver_id"] != 89660613.0 {
		t.Errorf("receiver_id = %v, quería el comprador del pedido", s.putEnvio["receiver_id"])
	}
}

func TestAckOrderME1NotificaConElServiceIdDelSitioYLaGuiaEnlazada(t *testing.T) {
	s := nuevoServidor(t)
	ad := despachoDe(t, s, "9002", map[string]any{
		"id": 778, "status": "ready_to_ship", "mode": "me1",
		"source": map[string]any{"site_id": "MCO"},
	}, map[string]string{"url_seguimiento": "https://rastreo.example/guia/{guia}"})

	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9002"}, guia()); err != nil {
		t.Fatal(err)
	}
	if s.rutaNotificada != "/shipments/778/seller_notifications" {
		t.Fatalf("se notificó en %q; la v1 está deprecada", s.rutaNotificada)
	}
	n := s.notificacion
	if n["status"] != "shipped" {
		t.Errorf("status = %v", n["status"])
	}
	// ML exige substatus presente aunque sea nulo: nulo es "en camino".
	if v, hay := n["substatus"]; !hay || v != nil {
		t.Errorf("substatus: presente=%v valor=%v; ML lo exige presente y nulo", hay, v)
	}
	p, _ := n["payload"].(map[string]any)
	if p["service_id"] != 282579.0 {
		t.Errorf("service_id = %v, quería 282579 (MCO)", p["service_id"])
	}
	if f, _ := p["date"].(string); !strings.HasPrefix(f, "2026-09-04T15:30:00") {
		t.Errorf("date = %v; debía ser la fecha de despacho", p["date"])
	}
	if n["tracking_number"] != "SERV-99887" || n["tracking_url"] != "https://rastreo.example/guia/SERV-99887" {
		t.Errorf("guía=%v url=%v; ML los exige juntos", n["tracking_number"], n["tracking_url"])
	}
}

func TestAckOrderME1SinPlantillaNoMandaGuiaSueltaYLaDejaEnElComentario(t *testing.T) {
	s := nuevoServidor(t)
	ad := despachoDe(t, s, "9002", map[string]any{
		"id": 778, "mode": "me1", "source": map[string]any{"site_id": "MCO"},
	}, nil)

	if err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9002"}, guia()); err != nil {
		t.Fatal(err)
	}
	n := s.notificacion
	// tracking_number y tracking_url van juntos o no va ninguno: mandar el
	// número solo hace que ML rechace la notificación entera.
	if _, hay := n["tracking_number"]; hay {
		t.Error("sin url de rastreo no se puede mandar tracking_number suelto")
	}
	if _, hay := n["tracking_url"]; hay {
		t.Error("no hay url que mandar")
	}
	p, _ := n["payload"].(map[string]any)
	if c, _ := p["comment"].(string); !strings.Contains(c, "SERV-99887") || !strings.Contains(c, "Servientrega") {
		t.Errorf("comment = %q; la guía y la transportadora tienen que quedar registradas", c)
	}
}

func TestAckOrderME1SinServiceIdDelSitioLoDice(t *testing.T) {
	s := nuevoServidor(t)
	// Un sitio fuera de la tabla de ML: no hay service_id que mandar.
	ad := despachoDe(t, s, "9002", map[string]any{
		"id": 778, "mode": "me1", "source": map[string]any{"site_id": "MLV"},
	}, nil)

	err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9002"}, guia())
	if err == nil || !strings.Contains(err.Error(), "service_id") {
		t.Fatalf("error = %v; debía explicar que falta el service_id del país", err)
	}
	if s.rutaNotificada != "" {
		t.Error("no se debe notificar sin service_id")
	}
}

func TestAckOrderMercadoEnviosExplicaQueLaGuiaEsDeMercadoLibre(t *testing.T) {
	for _, caso := range []struct{ nombre, modo, tipo string }{
		{"me2 drop off", "me2", "drop_off"},
		{"flex sin modo", "", "self_service"},
		{"colecta", "me2", "cross_docking"},
	} {
		t.Run(caso.nombre, func(t *testing.T) {
			s := nuevoServidor(t)
			ad := despachoDe(t, s, "9003", map[string]any{
				"id": 779, "status": "ready_to_ship", "mode": caso.modo, "logistic_type": caso.tipo,
			}, nil)

			err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9003"}, guia())
			if err == nil {
				t.Fatal("Mercado Envíos no acepta guía propia: darlo por despachado es mentir")
			}
			if !strings.Contains(err.Error(), "etiqueta") {
				t.Errorf("error = %v; debía decir qué hacer (imprimir la etiqueta)", err)
			}
			if channel.EsReintentable(err) {
				t.Error("reintentarlo no lo arregla nunca: no debe ser reintentable")
			}
			if s.rutaPutEnvio != "" || s.rutaNotificada != "" {
				t.Error("no se debe escribir nada en un envío de Mercado Envíos")
			}
		})
	}
}

func TestAckOrderFulfillmentExplicaQueDespachaMercadoLibre(t *testing.T) {
	s := nuevoServidor(t)
	ad := despachoDe(t, s, "9004", map[string]any{
		"id": 780, "logistic": map[string]any{"mode": "me2", "type": "fulfillment"},
	}, nil)

	err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9004"}, guia())
	if err == nil || !strings.Contains(err.Error(), "Full") {
		t.Fatalf("error = %v; debía decir que el paquete ya está en la bodega de ML", err)
	}
}

func TestAckOrderSinEnvioExplicaQueSeAcuerdaConElComprador(t *testing.T) {
	s := nuevoServidor(t)
	// Sin envío sembrado: es el modo "no especificado", donde ML ni siquiera
	// crea un shipment y el pedido responde 204.
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)

	err := ad.AckOrder(context.Background(), channel.ExternalRef{ListingID: "9005"}, guia())
	if err == nil || !strings.Contains(err.Error(), "comprador") {
		t.Fatalf("error = %v; debía explicar que no hay dónde registrar la guía", err)
	}
	if channel.EsReintentable(err) {
		t.Error("no es un fallo pasajero: no debe reintentarse")
	}
}

func TestAckOrderSinPedidoNoLlamaANadie(t *testing.T) {
	s := nuevoServidor(t)
	ad := adaptadorDe(t, s, map[string]string{"access_token": "APP_USR-fijo"}, nil)
	if err := ad.AckOrder(context.Background(), channel.ExternalRef{}, guia()); err == nil {
		t.Fatal("sin id de pedido no hay envío que buscar")
	}
}
