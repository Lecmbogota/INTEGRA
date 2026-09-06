// Package shopify implementa el canal Shopify sobre su Admin API REST.
//
// Es el primer adaptador a propósito: su API es la más simple de los cuatro
// canales (sin categorías obligatorias, sin atributos por categoría, sin
// feeds asíncronos), así que sirve para rodar el motor de publicación antes
// de pelear con MercadoLibre y Falabella.
package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// VersionAPI fija la versión de la Admin API, pero solo sirve de algo si es
// una versión viva: cuando se apunta a una retirada, Shopify no responde con
// error sino que aplica «fall forward» y sirve la petición con el
// comportamiento de la versión soportada más antigua, que cambia cada
// trimestre. Fijar una versión muerta consigue justo lo contrario de lo que
// se busca con el pin. Shopify soporta cada versión doce meses: 2026-07 vence
// el 16/07/2027, y esa es la fecha antes de la cual hay que volver a subirla.
//
// Es pública para que la prueba de conexión (conectores.probarShopify) use
// exactamente la misma y no convivan dos versiones distintas.
const VersionAPI = "2026-07"

// esquemaAPI es variable solo para que las pruebas puedan apuntar el
// adaptador a un httptest.Server local, que únicamente habla HTTP. Ningún
// camino de producción lo cambia.
var esquemaAPI = "https"

// maxPaginasCatalogo acota el recorrido del catálogo al buscar un SKU: son
// 250 productos por página, así que cubre 50.000 productos. El tope está para
// que una tienda que nunca deje de paginar no cuelgue un trabajo.
const maxPaginasCatalogo = 200

// maxItemsInventario es el máximo de inventory_item_ids que admite
// GET /inventory_levels.json en una llamada (también son 50 los location_ids).
// https://shopify.dev/docs/api/admin-rest/latest/resources/inventorylevel
const maxItemsInventario = 50

// ventanaPedidos es lo que el recurso Order deja leer hacia atrás sin el
// permiso read_all_orders: «only the last 60 days' worth of orders from a
// store are accessible from the Order resource by default».
// https://shopify.dev/docs/api/admin-rest/latest/resources/order
const ventanaPedidos = 60 * 24 * time.Hour

func init() {
	channel.Register(channel.Shopify, func(cfg channel.Config) (channel.Adapter, error) {
		tienda := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(
			cfg.Credentials["tienda"], "https://"), "http://"), "/")
		token := cfg.Credentials["token"]
		if tienda == "" || token == "" {
			return nil, fmt.Errorf("faltan la tienda y el token de Admin API")
		}
		a := &Adaptador{
			tienda: tienda, token: token,
			cli: &http.Client{Timeout: 30 * time.Second},
		}
		// La ubicación de inventario se puede fijar en la credencial: una
		// tienda con bodega y punto de venta tiene varias activas y elegir
		// «la primera que llegue» reparte el stock en la equivocada.
		if s := cfg.Credentials["location_id"]; s != "" {
			id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("location_id inválido en las credenciales: %q", s)
			}
			a.ubicacion = id
		}
		return a, nil
	})
}

type Adaptador struct {
	tienda string
	token  string
	cli    *http.Client

	mu sync.Mutex
	// ubicacion cachea el location_id: el adaptador se construye por trabajo,
	// así que la caché no sobrevive a un cambio de configuración, pero evita
	// repetir GET /locations.json por cada variante del mismo lote.
	ubicacion int64
	// todosLosPedidos cachea si el token lleva read_all_orders: 0 sin
	// consultar, 1 sí, -1 no. Se consulta como mucho una vez por trabajo.
	todosLosPedidos int8
}

func (a *Adaptador) Kind() channel.Kind { return channel.Shopify }

func (a *Adaptador) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		NativeCompareAtPrice:    true,  // compare_at_price
		ScheduledOffers:         false, // no hay fechas de promoción nativas
		BulkPriceUpdate:         false, // la REST actualiza variante a variante
		BulkStockUpdate:         false,
		MaxBatchSize:            1,
		AsyncFeeds:              false,
		Variants:                channel.VariantesEnPublicacion,
		RequiresOAuthRefresh:    false, // el token de app personalizada no caduca
		MaxTitleLength:          255,
		RequiresDescription:     false, // lo acepta vacío, aunque no conviene
		RequiresCategoryMapping: false,
	}
}

// Publish crea el producto. Si el SKU ya existe en la tienda, lo adopta en
// vez de duplicarlo: publicar dos veces el mismo SKU es el error que más
// cuesta deshacer en una tienda viva.
//
// Adoptar no es no hacer nada: el motor guarda los hashes de contenido,
// precio y stock en cuanto Publish devuelve bien (publicar/manejadores.go),
// de modo que devolver Adopted sin escribir dejaría el cambio marcado como
// sincronizado para siempre sin que hubiera llegado a la tienda.
func (a *Adaptador) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	p := req.Product
	if len(p.Variants) == 0 {
		return channel.PublishResult{}, fmt.Errorf("el producto %s no trae variantes", p.SKU)
	}
	v := p.Variants[0]

	existente, err := a.buscarPorSKU(ctx, v.SKU)
	if err != nil {
		return channel.PublishResult{}, err
	}

	if req.DryRun {
		ref := channel.ExternalRef{SKU: v.SKU}
		if existente != nil {
			ref = existente.ref
		}
		return channel.PublishResult{Ref: ref, Adopted: existente != nil}, nil
	}

	if existente != nil {
		return a.adoptar(ctx, p, v, *existente)
	}

	cuerpo := map[string]any{
		"product": map[string]any{
			"title":     p.Title,
			"body_html": p.Description,
			"vendor":    p.Brand,
			"status":    "draft", // se publica en borrador: activarlo es decisión humana
			"variants": []map[string]any{{
				"sku":                  v.SKU,
				"price":                strconv.FormatFloat(precioVenta(v.RegularPrice, v.SalePrice), 'f', 2, 64),
				"compare_at_price":     precioTachado(v.RegularPrice, v.SalePrice),
				"barcode":              v.Barcode,
				"inventory_management": "shopify",
				// inventory_quantity es de solo lectura en la Admin API: el
				// stock se fija abajo con inventory_levels/set. Mandarlo aquí
				// era lo que hacía nacer todos los productos con 0 unidades.
				"weight":      p.Weight,
				"weight_unit": "kg",
			}},
			"images": imagenesDe(p.Images),
		},
	}

	var resp struct {
		Product struct {
			ID       int64  `json:"id"`
			Handle   string `json:"handle"`
			Variants []struct {
				ID              int64  `json:"id"`
				SKU             string `json:"sku"`
				InventoryItemID int64  `json:"inventory_item_id"`
			} `json:"variants"`
		} `json:"product"`
	}
	if err := a.llamar(ctx, http.MethodPost, "/products.json", cuerpo, &resp); err != nil {
		return channel.PublishResult{}, err
	}

	ref := channel.ExternalRef{ListingID: fmt.Sprint(resp.Product.ID), SKU: v.SKU}
	refs := map[string]channel.ExternalRef{}
	var invID int64
	if len(resp.Product.Variants) > 0 {
		ref.VariantID = fmt.Sprint(resp.Product.Variants[0].ID)
		refs[v.SKU] = ref
		invID = resp.Product.Variants[0].InventoryItemID
	}
	// Si el stock falla se devuelve error a propósito: si no, el motor
	// guardaría el stock_hash como enviado y no volvería a encolar el
	// trabajo. El reintento no duplica nada porque encuentra el SKU y adopta.
	if err := a.fijarInventario(ctx, invID, v.Quantity); err != nil {
		return channel.PublishResult{}, err
	}
	return channel.PublishResult{Ref: ref, VariantRefs: refs}, nil
}

// adoptar manda a la publicación existente el contenido, el precio y el stock
// que el motor da por enviados en cuanto Publish devuelve bien.
func (a *Adaptador) adoptar(ctx context.Context, p channel.Product, v channel.Variant, e encontrado) (channel.PublishResult, error) {
	if err := a.llamar(ctx, http.MethodPut,
		"/products/"+e.ref.ListingID+".json", cuerpoContenido(e.ref.ListingID, p), nil); err != nil {
		return channel.PublishResult{}, err
	}

	invID := e.inventarioID
	if e.ref.VariantID != "" {
		var vr struct {
			Variant struct {
				InventoryItemID int64 `json:"inventory_item_id"`
			} `json:"variant"`
		}
		if err := a.llamar(ctx, http.MethodPut, "/variants/"+e.ref.VariantID+".json",
			cuerpoPrecio(e.ref.VariantID, v.RegularPrice, v.SalePrice), &vr); err != nil {
			return channel.PublishResult{}, err
		}
		if vr.Variant.InventoryItemID != 0 {
			invID = vr.Variant.InventoryItemID
		}
	}
	if err := a.fijarInventario(ctx, invID, v.Quantity); err != nil {
		return channel.PublishResult{}, err
	}
	return channel.PublishResult{
		Ref:         e.ref,
		Adopted:     true,
		Warnings:    []string{"ya existía en la tienda con el mismo SKU: se adoptó la publicación y se le envió el contenido actual"},
		VariantRefs: map[string]channel.ExternalRef{v.SKU: e.ref},
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if req.Ref.ListingID == "" {
		return channel.UpdateResult{}, fmt.Errorf("falta el identificador de la publicación")
	}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	if err := a.llamar(ctx, http.MethodPut, "/products/"+req.Ref.ListingID+".json",
		cuerpoContenido(req.Ref.ListingID, req.Product), nil); err != nil {
		return channel.UpdateResult{}, err
	}
	return channel.UpdateResult{Ref: req.Ref}, nil
}

func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		if u.Ref.VariantID == "" {
			out = append(out, channel.OpResult{Ref: u.Ref, Error: fmt.Errorf("falta la variante")})
			continue
		}
		err := a.llamar(ctx, http.MethodPut, "/variants/"+u.Ref.VariantID+".json",
			cuerpoPrecio(u.Ref.VariantID, u.RegularPrice, u.SalePrice), nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

// UpdateStock usa inventory_levels/set, que es como Shopify espera que se
// fije stock absoluto (poner inventory_quantity en la variante está obsoleto).
func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		invID, err := a.inventarioDeVariante(ctx, u.Ref.VariantID)
		if err != nil {
			out = append(out, channel.OpResult{Ref: u.Ref, Error: err})
			continue
		}
		err = a.fijarInventario(ctx, invID, u.Quantity)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

func (a *Adaptador) Pause(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/products/"+ref.ListingID+".json",
		map[string]any{"product": map[string]any{"id": ref.ListingID, "status": "draft"}}, nil)
}

func (a *Adaptador) Resume(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/products/"+ref.ListingID+".json",
		map[string]any{"product": map[string]any{"id": ref.ListingID, "status": "active"}}, nil)
}

func (a *Adaptador) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	out := make([]channel.ListingStatus, 0, len(refs))
	var fallo error
	// El stock NO sale del producto: se apunta el inventory_item_id de cada
	// publicación y se resuelve todo de un tirón contra inventory_levels, que
	// es la única lectura comparable con lo que escribe fijarInventario.
	pendientes := map[int]int64{}
	for _, ref := range refs {
		var resp struct {
			Product struct {
				Status   string `json:"status"`
				Handle   string `json:"handle"`
				Variants []struct {
					Price           string `json:"price"`
					InventoryItemID int64  `json:"inventory_item_id"`
				} `json:"variants"`
			} `json:"product"`
		}
		if err := a.llamar(ctx, http.MethodGet, "/products/"+ref.ListingID+".json", nil, &resp); err != nil {
			// Que la publicación ya no exista en la tienda es justo lo que
			// este informe tiene que contar; tragárselo la hacía desaparecer
			// del resultado sin ruido.
			if errors.Is(err, channel.ErrNoEncontrado) {
				out = append(out, channel.ListingStatus{Ref: ref, Status: "no_encontrado"})
				continue
			}
			if fallo == nil {
				fallo = err
			}
			continue
		}
		st := channel.ListingStatus{
			Ref: ref, Status: resp.Product.Status,
			Permalink: "https://" + a.tienda + "/products/" + resp.Product.Handle,
		}
		if len(resp.Product.Variants) > 0 {
			st.Price, _ = strconv.ParseFloat(resp.Product.Variants[0].Price, 64)
			if inv := resp.Product.Variants[0].InventoryItemID; inv != 0 {
				pendientes[len(out)] = inv
			}
		}
		out = append(out, st)
	}
	if len(pendientes) > 0 {
		ids := make([]int64, 0, len(pendientes))
		for _, inv := range pendientes {
			ids = append(ids, inv)
		}
		niveles, err := a.nivelesDeInventario(ctx, ids)
		if err != nil {
			// No se cae al agregado: publicar una cantidad que no es la de la
			// ubicación en la que se escribe es justo lo que se quiere evitar.
			if fallo == nil {
				fallo = err
			}
		} else {
			for i, inv := range pendientes {
				out[i].Quantity = niveles[inv]
			}
		}
	}
	return out, fallo
}

func (a *Adaptador) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	limite := cur.Size
	if limite <= 0 || limite > 250 {
		limite = 250
	}
	ruta := "/products.json?limit=" + strconv.Itoa(limite)
	if cur.Token != "" {
		ruta += "&page_info=" + url.QueryEscape(cur.Token)
	}
	var resp struct {
		Products []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Variants []struct {
				ID              int64  `json:"id"`
				SKU             string `json:"sku"`
				Price           string `json:"price"`
				InventoryItemID int64  `json:"inventory_item_id"`
			} `json:"variants"`
		} `json:"products"`
	}
	datos, cab, err := a.llamarCrudo(ctx, http.MethodGet, ruta, nil)
	if err != nil {
		return channel.RemotePage{}, err
	}
	if err := json.Unmarshal(datos, &resp); err != nil {
		return channel.RemotePage{}, fmt.Errorf("catálogo ilegible: %w", err)
	}
	var items []channel.RemoteListing
	// posicion apunta a la variante que espera su stock: igual que en
	// FetchStatus, las cantidades se resuelven al final contra
	// inventory_levels, no con el agregado que trae el producto.
	type posicion struct{ producto, variante int }
	pendientes := map[posicion]int64{}
	for _, p := range resp.Products {
		l := channel.RemoteListing{
			Ref:    channel.ExternalRef{ListingID: fmt.Sprint(p.ID)},
			Title:  p.Title,
			Status: p.Status,
		}
		for _, v := range p.Variants {
			precio, _ := strconv.ParseFloat(v.Price, 64)
			if v.InventoryItemID != 0 {
				pendientes[posicion{len(items), len(l.Variants)}] = v.InventoryItemID
			}
			l.Variants = append(l.Variants, channel.RemoteListing{
				Ref:   channel.ExternalRef{ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(v.ID), SKU: v.SKU},
				Price: precio,
			})
		}
		items = append(items, l)
	}
	if len(pendientes) > 0 {
		ids := make([]int64, 0, len(pendientes))
		for _, inv := range pendientes {
			ids = append(ids, inv)
		}
		niveles, err := a.nivelesDeInventario(ctx, ids)
		if err != nil {
			return channel.RemotePage{}, err
		}
		for pos, inv := range pendientes {
			items[pos.producto].Variants[pos.variante].Quantity = niveles[inv]
		}
	}
	// Quien dice si hay más páginas es el Link header, no el número de
	// elementos: una página llena puede ser la última.
	sig := siguientePagina(cab)
	return channel.RemotePage{
		Items: items,
		Next:  channel.Cursor{Token: sig, Size: limite},
		Done:  sig == "",
	}, nil
}

// FetchOrders trae los pedidos modificados desde una fecha. La Fase 5 los
// convierte en pedidos de venta de Odoo.
func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	const porPagina = 100
	ruta := "/orders.json?limit=" + strconv.Itoa(porPagina)
	if cur.Token != "" {
		// Junto a page_info Shopify solo admite limit y fields: mandar
		// status o la fecha en la segunda página es un 400.
		ruta += "&page_info=" + url.QueryEscape(cur.Token)
	} else {
		ruta += "&status=any"
		if !desde.IsZero() {
			// Antes de pedir nada: si la marca de agua se salió de la ventana
			// de 60 días, Shopify recortaría la respuesta en silencio.
			if err := a.comprobarVentanaDePedidos(ctx, desde); err != nil {
				return channel.OrderPage{}, err
			}
			// Se filtra por updated_at y no por created_at porque la marca de
			// agua del núcleo avanza con UpdatedAt (ordenes.go): con
			// created_at_min, un pedido viejo que cambia de estado empujaría
			// la marca por encima de pedidos nuevos que aún no se han traído,
			// y esos no se volverían a pedir nunca.
			ruta += "&updated_at_min=" + url.QueryEscape(desde.UTC().Format(time.RFC3339))
		}
	}

	// Se decodifica en dos pasos para conservar el JSON de cada pedido tal
	// como llegó: es lo que permite reprocesarlo sin volver a pedirlo.
	var sobre struct {
		Orders []json.RawMessage `json:"orders"`
	}
	crudo, cab, err := a.llamarCrudo(ctx, http.MethodGet, ruta, nil)
	if err != nil {
		return channel.OrderPage{}, err
	}
	if err := json.Unmarshal(crudo, &sobre); err != nil {
		return channel.OrderPage{}, fmt.Errorf("pedidos ilegibles: %w", err)
	}

	var out []channel.Order
	for _, bruto := range sobre.Orders {
		var o pedido
		if err := json.Unmarshal(bruto, &o); err != nil {
			return channel.OrderPage{}, fmt.Errorf("pedido ilegible: %w", err)
		}
		// Un pedido de la pasarela de pruebas o uno cancelado no puede acabar
		// en un sale.order: el núcleo encola el montaje de todo lo que se
		// ingiere y después nadie lo vuelve a mirar.
		if o.Test || o.CancelledAt != nil {
			continue
		}
		out = append(out, o.normalizar(bruto))
	}
	sig := siguientePagina(cab)
	return channel.OrderPage{
		Orders: out,
		Next:   channel.Cursor{Token: sig},
		Done:   sig == "",
	}, nil
}

// comprobarVentanaDePedidos frena la ingesta cuando la marca de agua se ha
// quedado fuera de lo que el token puede leer.
//
// El recurso Order advierte que «only the last 60 days' worth of orders from a
// store are accessible from the Order resource by default» y que para leer más
// atrás hace falta el permiso read_all_orders además de read_orders o
// write_orders (https://shopify.dev/docs/api/admin-rest/latest/resources/order).
// Shopify no devuelve error cuando falta: entrega la respuesta recortada.
//
// Eso es lo que convierte una parada larga (cuenta pausada, token caducado,
// cola atascada) en pérdida definitiva: al reanudar solo llegan los últimos 60
// días, el núcleo avanza la marca con lo recibido (ordenes.go) y los pedidos
// del hueco no se vuelven a pedir jamás, sin traza. Devolver error deja la
// marca donde estaba —ordenes.go solo la mueve si FetchOrders responde bien— y
// escribe el motivo en el trabajo. Va con 403 a propósito: channel.EsReintentable
// no reintenta los 4xx, y este no se arregla repitiendo la llamada.
func (a *Adaptador) comprobarVentanaDePedidos(ctx context.Context, desde time.Time) error {
	if time.Since(desde) <= ventanaPedidos {
		return nil
	}
	todos, err := a.leeTodosLosPedidos(ctx)
	if err != nil {
		return err
	}
	if todos {
		return nil
	}
	return &channel.Error{
		Kind: channel.Shopify, StatusCode: http.StatusForbidden, Code: "ventana_60_dias",
		Message: "la ingesta pide los pedidos modificados desde " + desde.UTC().Format(time.RFC3339) +
			", hace más de 60 días, y el token no tiene el permiso read_all_orders: Shopify " +
			"devolvería solo los últimos 60 días sin avisar y los anteriores no se volverían a " +
			"pedir nunca. Hay que añadir read_all_orders a la app personalizada, o adelantar a " +
			"mano la marca de agua asumiendo el hueco",
	}
}

// leeTodosLosPedidos dice si el token lleva read_all_orders.
//
// La lista de permisos del token se pide a GET /admin/oauth/access_scopes.json,
// que NO va bajo /admin/api/{version}
// (https://shopify.dev/docs/api/admin-rest/latest/resources/accessscope).
func (a *Adaptador) leeTodosLosPedidos(ctx context.Context) (bool, error) {
	a.mu.Lock()
	cache := a.todosLosPedidos
	a.mu.Unlock()
	if cache != 0 {
		return cache > 0, nil
	}

	var resp struct {
		AccessScopes []struct {
			Handle string `json:"handle"`
		} `json:"access_scopes"`
	}
	datos, _, err := a.llamarRuta(ctx, http.MethodGet, "/admin/oauth/access_scopes.json", nil)
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(datos, &resp); err != nil {
		return false, fmt.Errorf("lista de permisos ilegible: %w", err)
	}
	todos := false
	for _, s := range resp.AccessScopes {
		if strings.EqualFold(strings.TrimSpace(s.Handle), "read_all_orders") {
			todos = true
			break
		}
	}

	a.mu.Lock()
	if todos {
		a.todosLosPedidos = 1
	} else {
		a.todosLosPedidos = -1
	}
	a.mu.Unlock()
	return todos, nil
}

// AckOrder crea el fulfillment del pedido con la guía y la transportadora.
//
// Shopify ya no deja crear un fulfillment contra el pedido: desde 2022-07 el
// alta cuelga de los «fulfillment orders», que es como Shopify parte un pedido
// por ubicación de despacho, y el endpoint viejo
// (POST /orders/{id}/fulfillments.json) está retirado. El flujo vigente es
// leer GET /orders/{id}/fulfillment_orders.json y mandar POST
// /fulfillments.json con line_items_by_fulfillment_order.
//
// Un fulfillment solo puede agrupar fulfillment orders de la MISMA ubicación,
// así que un pedido repartido entre bodega y punto de venta produce uno por
// ubicación; mandarlas juntas es un 422.
func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	if ref.ListingID == "" {
		return fmt.Errorf("falta el identificador del pedido")
	}
	ordenes, err := a.ordenesDeDespacho(ctx, ref.ListingID)
	if err != nil {
		return err
	}
	if len(ordenes) == 0 {
		return &channel.Error{
			Kind: channel.Shopify, StatusCode: http.StatusConflict, Code: "sin_fulfillment_orders",
			Message: "el pedido " + ref.ListingID + " no tiene fulfillment orders en Shopify: no hay " +
				"nada que despachar (pedido solo digital, cancelado o ya archivado)",
		}
	}

	// Se agrupan por ubicación conservando el orden en que Shopify las
	// devolvió, para que el mismo pedido produzca siempre las mismas llamadas.
	var ubicaciones []int64
	porUbicacion := map[int64][]int64{}
	terminadas := 0
	for _, o := range ordenes {
		if o.despachable() {
			if _, hay := porUbicacion[o.AssignedLocationID]; !hay {
				ubicaciones = append(ubicaciones, o.AssignedLocationID)
			}
			porUbicacion[o.AssignedLocationID] = append(porUbicacion[o.AssignedLocationID], o.ID)
			continue
		}
		if o.terminada() {
			terminadas++
		}
	}
	if len(ubicaciones) == 0 {
		// Todo cerrado o cancelado: el pedido ya estaba despachado. AckOrder
		// se reintenta, así que volver a fallar aquí dejaría el trabajo en
		// error para siempre por haber llegado dos veces.
		if terminadas == len(ordenes) {
			return nil
		}
		return &channel.Error{
			Kind: channel.Shopify, StatusCode: http.StatusConflict, Code: "nada_despachable",
			Message: fmt.Sprintf("ninguna de las %d partes del pedido %s se puede despachar: están "+
				"retenidas, programadas o asignadas a un servicio de fulfillment externo, y Shopify "+
				"no acepta crear el fulfillment desde aquí; hay que liberarlas en el panel",
				len(ordenes), ref.ListingID),
		}
	}

	for _, loc := range ubicaciones {
		lineas := make([]map[string]any, 0, len(porUbicacion[loc]))
		for _, id := range porUbicacion[loc] {
			lineas = append(lineas, map[string]any{"fulfillment_order_id": id})
		}
		despacho := map[string]any{
			"line_items_by_fulfillment_order": lineas,
			// El correo de «pedido enviado» es lo único que le enseña la guía
			// al comprador: sin avisar, el fulfillment queda mudo.
			"notify_customer": true,
		}
		if info := seguimientoDe(f); info != nil {
			despacho["tracking_info"] = info
		}
		if err := a.llamar(ctx, http.MethodPost, "/fulfillments.json",
			map[string]any{"fulfillment": despacho}, nil); err != nil {
			return err
		}
	}
	return nil
}

// ordenDespacho es lo que interesa de un fulfillment order.
type ordenDespacho struct {
	ID                 int64    `json:"id"`
	Status             string   `json:"status"`
	AssignedLocationID int64    `json:"assigned_location_id"`
	SupportedActions   []string `json:"supported_actions"`
}

// despachable dice si el vendedor puede crear el fulfillment de esta parte del
// pedido. Shopify lo anuncia en supported_actions: una parte retenida,
// programada o asignada a un servicio de fulfillment externo no lleva
// create_fulfillment, y mandarla igual es un 422.
func (o ordenDespacho) despachable() bool {
	for _, acc := range o.SupportedActions {
		if acc == "create_fulfillment" {
			return true
		}
	}
	if len(o.SupportedActions) > 0 {
		return false
	}
	// Sin supported_actions (respuesta recortada) se cae al estado.
	e := strings.ToLower(o.Status)
	return e == "open" || e == "in_progress"
}

// terminada dice si esta parte ya no espera despacho: cerrada (o sea,
// despachada), cancelada o incompleta.
func (o ordenDespacho) terminada() bool {
	switch strings.ToLower(o.Status) {
	case "closed", "cancelled", "incomplete":
		return true
	}
	return false
}

func (a *Adaptador) ordenesDeDespacho(ctx context.Context, ordenID string) ([]ordenDespacho, error) {
	var resp struct {
		FulfillmentOrders []ordenDespacho `json:"fulfillment_orders"`
	}
	if err := a.llamar(ctx, http.MethodGet,
		"/orders/"+ordenID+"/fulfillment_orders.json", nil, &resp); err != nil {
		return nil, err
	}
	return resp.FulfillmentOrders, nil
}

// seguimientoDe arma el tracking_info del fulfillment. Shopify deduce la URL
// de rastreo a partir de company cuando reconoce la transportadora; si no la
// reconoce enseña el número sin enlace, que sigue siendo mejor que nada.
func seguimientoDe(f channel.Fulfillment) map[string]any {
	info := map[string]any{}
	if n := strings.TrimSpace(f.TrackingNumber); n != "" {
		info["number"] = n
	}
	if c := strings.TrimSpace(f.Carrier); c != "" {
		info["company"] = c
	}
	if len(info) == 0 {
		return nil
	}
	return info
}

// --------------------------------------------------------------- pedidos

// pedido es la carga de /orders.json con los campos que el núcleo necesita
// para montar el pedido en Odoo.
type pedido struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	FinancialStatus string     `json:"financial_status"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CancelledAt     *time.Time `json:"cancelled_at"`
	Test            bool       `json:"test"`
	Currency        string     `json:"currency"`
	TotalPrice      string     `json:"total_price"`
	TotalTax        string     `json:"total_tax"`
	// El pedido puede no traer objeto customer (compra como invitado, o
	// cliente redactado por privacidad), pero el correo de contacto sí viene
	// en la raíz: sin él, Odoo acaba con un partner genérico por cada pedido.
	Email        string `json:"email"`
	ContactEmail string `json:"contact_email"`
	Phone        string `json:"phone"`
	Customer     *struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Phone     string `json:"phone"`
	} `json:"customer"`
	ShippingAddress struct {
		Name      string `json:"name"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Phone     string `json:"phone"`
		Address1  string `json:"address1"`
		Address2  string `json:"address2"`
		City      string `json:"city"`
		Province  string `json:"province"`
		Zip       string `json:"zip"`
		Country   string `json:"country"`
	} `json:"shipping_address"`
	TotalShippingPriceSet struct {
		ShopMoney struct {
			Amount string `json:"amount"`
		} `json:"shop_money"`
	} `json:"total_shipping_price_set"`
	ShippingLines []struct {
		Price string `json:"price"`
	} `json:"shipping_lines"`
	LineItems []struct {
		ID                  int64            `json:"id"`
		SKU                 string           `json:"sku"`
		Title               string           `json:"title"`
		Quantity            int              `json:"quantity"`
		Price               string           `json:"price"`
		TotalDiscount       string           `json:"total_discount"`
		DiscountAllocations []asignacionDesc `json:"discount_allocations"`
	} `json:"line_items"`
}

type asignacionDesc struct {
	Amount string `json:"amount"`
}

func (o pedido) normalizar(bruto []byte) channel.Order {
	total, _ := strconv.ParseFloat(o.TotalPrice, 64)
	imp, _ := strconv.ParseFloat(o.TotalTax, 64)

	nombre, correoCliente, telCliente := "", "", ""
	if o.Customer != nil {
		nombre = strings.TrimSpace(o.Customer.FirstName + " " + o.Customer.LastName)
		correoCliente, telCliente = o.Customer.Email, o.Customer.Phone
	}
	if nombre == "" {
		nombre = strings.TrimSpace(o.ShippingAddress.Name)
	}
	if nombre == "" {
		nombre = strings.TrimSpace(o.ShippingAddress.FirstName + " " + o.ShippingAddress.LastName)
	}

	ord := channel.Order{
		ExternalID: fmt.Sprint(o.ID), Number: o.Name, Status: o.FinancialStatus,
		OrderedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
		Currency: o.Currency, Total: total, Tax: imp, Shipping: o.envio(),
		Raw: bruto,
		Buyer: channel.Buyer{
			Name:  nombre,
			Email: primero(correoCliente, o.Email, o.ContactEmail),
			Phone: primero(telCliente, o.Phone, o.ShippingAddress.Phone),
			Address: channel.Address{
				Line1: o.ShippingAddress.Address1, Line2: o.ShippingAddress.Address2,
				City: o.ShippingAddress.City, State: o.ShippingAddress.Province,
				PostalCode: o.ShippingAddress.Zip, Country: o.ShippingAddress.Country,
			},
		},
	}
	for _, li := range o.LineItems {
		precio, _ := strconv.ParseFloat(li.Price, 64)
		cant := float64(li.Quantity)
		// line_items[].price es el precio ANTES de descuentos: cobrar en
		// Shopify con un cupón y facturar en Odoo el precio de lista deja el
		// pedido por encima de lo cobrado.
		lista := precio * cant
		neto := lista - descuentoDeLinea(li.TotalDiscount, li.DiscountAllocations)
		if neto < 0 {
			neto = 0
		}
		unitario := precio
		if cant > 0 {
			unitario = neto / cant
		}
		ord.Lines = append(ord.Lines, channel.OrderLine{
			ExternalID: fmt.Sprint(li.ID), SKU: li.SKU, Title: li.Title,
			Quantity: cant, UnitPrice: unitario, TotalPrice: neto,
		})
	}
	return ord
}

// envio saca el envío cobrado, que va dentro de total_price pero no en
// ninguna línea: sin él la cabecera del pedido no cuadra con sus líneas.
func (o pedido) envio() float64 {
	if v, err := strconv.ParseFloat(o.TotalShippingPriceSet.ShopMoney.Amount, 64); err == nil {
		return v
	}
	var suma float64
	for _, l := range o.ShippingLines {
		v, _ := strconv.ParseFloat(l.Price, 64)
		suma += v
	}
	return suma
}

// descuentoDeLinea prefiere discount_allocations, que es lo que recomienda la
// documentación; total_discount queda de respaldo para los pedidos que no las
// traen.
func descuentoDeLinea(totalDescuento string, asignaciones []asignacionDesc) float64 {
	var suma float64
	for _, a := range asignaciones {
		v, _ := strconv.ParseFloat(a.Amount, 64)
		suma += v
	}
	if suma > 0 {
		return suma
	}
	v, _ := strconv.ParseFloat(totalDescuento, 64)
	return v
}

func primero(valores ...string) string {
	for _, v := range valores {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

// ------------------------------------------------------------- auxiliares

type encontrado struct {
	ref          channel.ExternalRef
	inventarioID int64
}

// buscarPorSKU recorre el catálogo entero. La REST no filtra productos por
// SKU, así que hay que paginar con el Link header hasta agotarlo: quedarse en
// la primera página hacía que a partir del producto 251 la adopción fallara y
// Publish creara un duplicado del mismo SKU en una tienda viva.
func (a *Adaptador) buscarPorSKU(ctx context.Context, sku string) (*encontrado, error) {
	if sku == "" {
		return nil, nil
	}
	buscado := strings.TrimSpace(sku)
	const campos = "/products.json?limit=250&fields=id,variants"
	ruta := campos
	for i := 0; i < maxPaginasCatalogo; i++ {
		var resp struct {
			Products []struct {
				ID       int64 `json:"id"`
				Variants []struct {
					ID              int64  `json:"id"`
					SKU             string `json:"sku"`
					InventoryItemID int64  `json:"inventory_item_id"`
				} `json:"variants"`
			} `json:"products"`
		}
		datos, cab, err := a.llamarCrudo(ctx, http.MethodGet, ruta, nil)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(datos, &resp); err != nil {
			return nil, fmt.Errorf("catálogo ilegible: %w", err)
		}
		for _, p := range resp.Products {
			for _, v := range p.Variants {
				if strings.EqualFold(strings.TrimSpace(v.SKU), buscado) {
					return &encontrado{
						ref: channel.ExternalRef{
							ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(v.ID), SKU: sku,
						},
						inventarioID: v.InventoryItemID,
					}, nil
				}
			}
		}
		sig := siguientePagina(cab)
		if sig == "" {
			return nil, nil
		}
		ruta = campos + "&page_info=" + url.QueryEscape(sig)
	}
	// Agotar el tope sin encontrarlo no es «no existe»: seguir adelante y
	// crear el producto sería justo el duplicado que se quiere evitar.
	return nil, fmt.Errorf("no se pudo recorrer el catálogo completo buscando el SKU %s: más de %d páginas", sku, maxPaginasCatalogo)
}

// siguientePagina saca el page_info del Link rel="next". Es la única forma de
// pasar de página en la REST de Shopify: no existe número de página.
func siguientePagina(cab http.Header) string {
	for _, enlace := range cab.Values("Link") {
		for _, parte := range strings.Split(enlace, ",") {
			if !strings.Contains(parte, "rel=\"next\"") && !strings.Contains(parte, "rel=next") {
				continue
			}
			i := strings.Index(parte, "<")
			j := strings.Index(parte, ">")
			if i < 0 || j <= i {
				continue
			}
			u, err := url.Parse(parte[i+1 : j])
			if err != nil {
				continue
			}
			if pi := u.Query().Get("page_info"); pi != "" {
				return pi
			}
		}
	}
	return ""
}

func (a *Adaptador) ubicacionPrincipal(ctx context.Context) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ubicacion != 0 {
		return a.ubicacion, nil
	}
	var resp struct {
		Locations []struct {
			ID     int64 `json:"id"`
			Active bool  `json:"active"`
		} `json:"locations"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/locations.json", nil, &resp); err != nil {
		return 0, err
	}
	// Shopify no garantiza el orden de /locations.json. Se toma la activa de
	// id más bajo (la que nace con la tienda) para que al menos sea siempre
	// la misma entre trabajos; cuando la tienda tiene bodega y punto de
	// venta, hay que fijar location_id en las credenciales de la cuenta.
	activas := make([]int64, 0, len(resp.Locations))
	for _, l := range resp.Locations {
		if l.Active {
			activas = append(activas, l.ID)
		}
	}
	if len(activas) == 0 {
		return 0, fmt.Errorf("la tienda no tiene ninguna ubicación activa")
	}
	sort.Slice(activas, func(i, j int) bool { return activas[i] < activas[j] })
	a.ubicacion = activas[0]
	return a.ubicacion, nil
}

func (a *Adaptador) inventarioDeVariante(ctx context.Context, varianteID string) (int64, error) {
	if varianteID == "" {
		return 0, fmt.Errorf("falta el identificador de variante")
	}
	var resp struct {
		Variant struct {
			InventoryItemID int64 `json:"inventory_item_id"`
		} `json:"variant"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/variants/"+varianteID+".json", nil, &resp); err != nil {
		return 0, err
	}
	return resp.Variant.InventoryItemID, nil
}

// fijarInventario pone el stock absoluto de un inventory_item en la ubicación
// de la cuenta. Es el único camino que Shopify acepta: inventory_quantity de
// la variante es de solo lectura y se ignora en silencio.
func (a *Adaptador) fijarInventario(ctx context.Context, inventarioID int64, cantidad int) error {
	if inventarioID == 0 {
		return fmt.Errorf("la variante no trae inventory_item_id: no se puede fijar el stock")
	}
	loc, err := a.ubicacionPrincipal(ctx)
	if err != nil {
		return err
	}
	return a.llamar(ctx, http.MethodPost, "/inventory_levels/set.json", map[string]any{
		"location_id": loc, "inventory_item_id": inventarioID, "available": cantidad,
	}, nil)
}

// nivelesDeInventario lee el stock disponible de la MISMA ubicación en la que
// escribe fijarInventario, indexado por inventory_item_id.
//
// No se usa variants[].inventory_quantity: la documentación del recurso
// Product Variant lo marca de solo lectura y lo define como «an aggregate of
// inventory across all locations», y remite al recurso InventoryLevel para
// consultar o ajustar el inventario de una ubicación concreta
// (https://shopify.dev/docs/api/admin-rest/latest/resources/product-variant).
// Leer el agregado mientras se escribe en una sola ubicación hace que, en
// cuanto la tienda tenga bodega y punto de venta, el informe de estado y el
// listado remoto devuelvan un número que nunca coincide con lo que Integra
// empujó: desajuste permanente en todos los SKU, o un cero real escondido
// detrás de las existencias de otra ubicación.
//
// Se pide en tandas porque el endpoint admite como mucho 50
// inventory_item_ids por llamada.
func (a *Adaptador) nivelesDeInventario(ctx context.Context, ids []int64) (map[int64]int, error) {
	loc, err := a.ubicacionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	unicos := make([]int64, 0, len(ids))
	visto := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id == 0 || visto[id] {
			continue
		}
		visto[id] = true
		unicos = append(unicos, id)
	}

	out := make(map[int64]int, len(unicos))
	for i := 0; i < len(unicos); i += maxItemsInventario {
		fin := i + maxItemsInventario
		if fin > len(unicos) {
			fin = len(unicos)
		}
		partes := make([]string, 0, fin-i)
		for _, id := range unicos[i:fin] {
			partes = append(partes, strconv.FormatInt(id, 10))
		}
		q := url.Values{
			"inventory_item_ids": {strings.Join(partes, ",")},
			"location_ids":       {strconv.FormatInt(loc, 10)},
			// El límite por defecto son 50 registros: con la tanda llena se
			// perdería la mitad de las respuestas si el tope se dejara puesto.
			"limit": {"250"},
		}
		var resp struct {
			InventoryLevels []struct {
				InventoryItemID int64 `json:"inventory_item_id"`
				LocationID      int64 `json:"location_id"`
				Available       int   `json:"available"`
			} `json:"inventory_levels"`
		}
		if err := a.llamar(ctx, http.MethodGet, "/inventory_levels.json?"+q.Encode(), nil, &resp); err != nil {
			return nil, err
		}
		for _, n := range resp.InventoryLevels {
			// Un nivel de otra ubicación no cuenta: se compara contra el
			// stock que Integra escribe en la suya.
			if n.LocationID != loc {
				continue
			}
			out[n.InventoryItemID] = n.Available
		}
	}
	return out, nil
}

// cuerpoContenido arma la ficha del producto. Lleva las imágenes porque el
// hash de contenido del motor las incluye: si no viajaran, cambiar una foto
// quedaría marcado como sincronizado sin haber llegado a la tienda.
func cuerpoContenido(listingID string, p channel.Product) map[string]any {
	prod := map[string]any{
		"id": listingID, "title": p.Title,
		"body_html": p.Description, "vendor": p.Brand,
	}
	if len(p.Images) > 0 {
		prod["images"] = imagenesDe(p.Images)
	}
	return map[string]any{"product": prod}
}

// cuerpoPrecio arma el precio de la variante. Shopify no tiene fechas de
// promoción: la oferta va en price y el precio de lista en compare_at_price
// (el tachado). Cuando la oferta termina hay que vaciar compare_at_price
// explícitamente, o la tienda seguiría enseñando un descuento que ya no
// existe.
func cuerpoPrecio(varianteID string, regular, oferta float64) map[string]any {
	return map[string]any{"variant": map[string]any{
		"id":               varianteID,
		"price":            strconv.FormatFloat(precioVenta(regular, oferta), 'f', 2, 64),
		"compare_at_price": precioTachado(regular, oferta),
	}}
}

func precioVenta(regular, oferta float64) float64 {
	if oferta > 0 && oferta < regular {
		return oferta
	}
	return regular
}

// precioTachado devuelve nil (null en JSON) cuando no hay oferta, que es como
// Shopify borra el precio comparativo.
func precioTachado(regular, oferta float64) any {
	if oferta > 0 && oferta < regular {
		return strconv.FormatFloat(regular, 'f', 2, 64)
	}
	return nil
}

func imagenesDe(imgs []channel.Image) []map[string]any {
	out := make([]map[string]any, 0, len(imgs))
	for _, i := range imgs {
		out = append(out, map[string]any{"src": i.URL, "position": i.Position + 1})
	}
	return out
}

func (a *Adaptador) llamar(ctx context.Context, metodo, ruta string, cuerpo any, out any) error {
	datos, _, err := a.llamarCrudo(ctx, metodo, ruta, cuerpo)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(datos, out)
}

// llamarCrudo devuelve también las cabeceras porque la paginación de Shopify
// viaja en el Link header, no en el cuerpo de la respuesta.
func (a *Adaptador) llamarCrudo(ctx context.Context, metodo, ruta string, cuerpo any) ([]byte, http.Header, error) {
	return a.llamarRuta(ctx, metodo, "/admin/api/"+VersionAPI+ruta, cuerpo)
}

// llamarRuta habla con una ruta absoluta de la tienda. Existe porque no todo
// cuelga de /admin/api/{version}: la lista de permisos del token está en
// /admin/oauth/access_scopes.json, sin versión.
func (a *Adaptador) llamarRuta(ctx context.Context, metodo, ruta string, cuerpo any) ([]byte, http.Header, error) {
	var body io.Reader
	if cuerpo != nil {
		j, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, nil, err
		}
		body = bytes.NewReader(j)
	}
	destino := esquemaAPI + "://" + a.tienda + ruta
	req, err := http.NewRequestWithContext(ctx, metodo, destino, body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-Shopify-Access-Token", a.token)
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		return nil, nil, &channel.Error{Kind: channel.Shopify, Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &channel.Error{
			Kind: channel.Shopify, StatusCode: resp.StatusCode,
			Message: recortar(string(datos), 300),
		}
		// Shopify limita a 2 req/s y responde 429 con Retry-After en segundos.
		if resp.StatusCode == http.StatusTooManyRequests {
			if s := resp.Header.Get("Retry-After"); s != "" {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					e.RetryAfter = time.Duration(f * float64(time.Second))
				}
			}
		}
		if resp.StatusCode == http.StatusNotFound {
			e.Err = channel.ErrNoEncontrado
		}
		return nil, resp.Header, e
	}
	return datos, resp.Header, nil
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
