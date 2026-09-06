// Package mercadolibre implementa el canal MercadoLibre.
//
// Es el más exigente de los cuatro por tres razones: el token caduca cada seis
// horas y hay que refrescarlo (y ML rota el refresh token en cada canje), la
// categoría es obligatoria, y cada categoría impone sus propios atributos
// obligatorios — que es lo que resuelve el paquete internal/atributos.
//
// Además conviven dos modelos de publicación. Un vendedor con el tag
// user_product_seller ya no manda título ni variaciones: manda family_name y
// ML agrupa los ítems en User Products. Publicar con el modelo viejo a un
// vendedor así devuelve 400, por eso se consulta el perfil antes de publicar.
package mercadolibre

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
)

func init() {
	channel.Register(channel.MercadoLibre, func(cfg channel.Config) (channel.Adapter, error) {
		a := &Adaptador{
			appID:     cfg.Credentials["app_id"],
			appSecret: cfg.Credentials["app_secret"],
			refresh:   cfg.Credentials["refresh_token"],
			token:     cfg.Credentials["access_token"],
			persistir: cfg.PersistCredentials,
			base:      conectores.URLBaseML,
			cli:       &http.Client{Timeout: 30 * time.Second},
		}
		if a.token == "" && (a.refresh == "" || a.appID == "" || a.appSecret == "") {
			return nil, fmt.Errorf("hace falta un access token, o app_id + app_secret + refresh_token")
		}
		return a, nil
	})
}

// tamPagina es lo que pide ML por página en búsquedas de ítems y pedidos.
const tamPagina = 50

// tamMultiget es el máximo de ids que acepta GET /items?ids= por llamada.
const tamMultiget = 20

type Adaptador struct {
	appID     string
	appSecret string
	cli       *http.Client
	base      string
	persistir func(context.Context, map[string]string) error

	mu      sync.Mutex
	token   string
	refresh string
	expira  time.Time

	// Perfil del vendedor, cacheado: no cambia durante la vida del adaptador.
	perfilCargado bool
	userID        int64
	upSeller      bool
}

func (a *Adaptador) Kind() channel.Kind { return channel.MercadoLibre }

func (a *Adaptador) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		NativeCompareAtPrice:    false, // no hay precio tachado propio
		ScheduledOffers:         false, // las promociones no llevan fechas por API
		BulkPriceUpdate:         false,
		BulkStockUpdate:         false,
		MaxBatchSize:            1,
		AsyncFeeds:              false,
		Variants:                channel.VariantesEnPublicacion,
		RequiresOAuthRefresh:    true,
		MaxTitleLength:          60, // el más estricto de los cuatro
		RequiresDescription:     true,
		RequiresCategoryMapping: true,
	}
}

// respuestaItem es lo que interesa de la respuesta de POST/PUT /items.
type respuestaItem struct {
	ID                string    `json:"id"`
	Permalink         string    `json:"permalink"`
	UserProductID     string    `json:"user_product_id"`
	Price             float64   `json:"price"`
	AvailableQuantity int       `json:"available_quantity"`
	Status            string    `json:"status"`
	Warnings          []avisoML `json:"warnings"`
}

type avisoML struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (r respuestaItem) avisos() []string {
	var out []string
	for _, w := range r.Warnings {
		out = append(out, strings.TrimSpace(w.Code+": "+w.Message))
	}
	return out
}

func (a *Adaptador) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	p := req.Product
	if len(p.Variants) == 0 {
		return channel.PublishResult{}, fmt.Errorf("el producto %s no trae variantes", p.SKU)
	}
	if p.CategoryID == "" {
		return channel.PublishResult{}, fmt.Errorf("MercadoLibre exige categoría y %s no la tiene mapeada", p.SKU)
	}
	v := p.Variants[0]

	if ref, err := a.buscarPorSKU(ctx, v.SKU); err != nil {
		return channel.PublishResult{}, err
	} else if ref != nil {
		return channel.PublishResult{
			Ref: *ref, Adopted: true,
			Warnings:    []string{"ya existía en la cuenta con el mismo SKU: se adoptó la publicación"},
			VariantRefs: map[string]channel.ExternalRef{v.SKU: *ref},
		}, nil
	}
	if req.DryRun {
		return channel.PublishResult{Ref: channel.ExternalRef{SKU: v.SKU}}, nil
	}
	if err := a.perfil(ctx); err != nil {
		return channel.PublishResult{}, err
	}

	// Los atributos deducidos viajan como los espera ML: con id de valor
	// cuando existe, y con value_name en texto libre cuando no.
	var attrs []map[string]any
	tieneGTIN := false
	for id, valor := range p.Attributes {
		if id == "GTIN" || id == "EAN" {
			tieneGTIN = true
		}
		attrs = append(attrs, map[string]any{"id": id, "value_name": valor})
	}
	// El SKU va también como atributo: es como ML lo indexa, lo que devuelve
	// en las órdenes como seller_sku, y lo que permite adoptar la publicación
	// después sin duplicarla.
	attrs = append(attrs, map[string]any{"id": "SELLER_SKU", "value_name": v.SKU})
	if v.Barcode != "" && !tieneGTIN {
		attrs = append(attrs, map[string]any{"id": "GTIN", "value_name": v.Barcode})
	}

	var imgs []map[string]any
	for _, i := range p.Images {
		imgs = append(imgs, map[string]any{"source": i.URL})
	}

	cuerpo := map[string]any{
		"category_id":        p.CategoryID,
		"price":              v.RegularPrice,
		"currency_id":        monedaDe(v.Currency),
		"available_quantity": v.Quantity,
		"buying_mode":        "buy_it_now",
		"listing_type_id":    "gold_special",
		"condition":          "new",
		// Pausada al crear: activar una venta es decisión humana, igual que
		// en los otros canales.
		"status":     "paused",
		"pictures":   imgs,
		"attributes": attrs,
	}
	// Modelo User Products: el título lo compone ML a partir del family_name
	// y los atributos; mandarlo es un 400. Modelo legacy: al revés.
	if a.esUserProductSeller() {
		cuerpo["family_name"] = recortarRunes(p.Title, 60)
	} else {
		cuerpo["title"] = recortarRunes(p.Title, 60)
	}

	var resp respuestaItem
	if err := a.llamar(ctx, http.MethodPost, "/items", nil, cuerpo, &resp); err != nil {
		return channel.PublishResult{}, err
	}
	avisos := resp.avisos()

	// La descripción va en su propio recurso, no en el alta del ítem. Que
	// falle no deshace la publicación, pero tampoco puede pasar en silencio:
	// una ficha sin descripción pierde exposición.
	if p.Description != "" {
		if err := a.llamar(ctx, http.MethodPost, "/items/"+resp.ID+"/description", nil,
			map[string]any{"plain_text": p.Description}, nil); err != nil {
			avisos = append(avisos, "la publicación se creó pero la descripción no: "+err.Error())
		}
	}

	ref := channel.ExternalRef{ListingID: resp.ID, VariantID: resp.ID, SKU: v.SKU}
	return channel.PublishResult{
		Ref: ref, VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
		Warnings: avisos,
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if req.Ref.ListingID == "" {
		return channel.UpdateResult{}, fmt.Errorf("falta el identificador de la publicación")
	}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	if err := a.perfil(ctx); err != nil {
		return channel.UpdateResult{}, err
	}
	quiere := func(campo string) bool {
		if len(req.Fields) == 0 {
			return true
		}
		for _, f := range req.Fields {
			if f == campo {
				return true
			}
		}
		return false
	}

	cuerpo := map[string]any{}
	if quiere("title") && req.Product.Title != "" {
		// En User Products el título es de ML; lo editable es el family_name,
		// y solo mientras el ítem no tenga ventas.
		if a.esUserProductSeller() {
			cuerpo["family_name"] = recortarRunes(req.Product.Title, 60)
		} else {
			cuerpo["title"] = recortarRunes(req.Product.Title, 60)
		}
	}
	if quiere("attributes") && len(req.Product.Attributes) > 0 {
		var attrs []map[string]any
		for id, valor := range req.Product.Attributes {
			attrs = append(attrs, map[string]any{"id": id, "value_name": valor})
		}
		cuerpo["attributes"] = attrs
	}
	if quiere("pictures") && len(req.Product.Images) > 0 {
		var imgs []map[string]any
		for _, i := range req.Product.Images {
			imgs = append(imgs, map[string]any{"source": i.URL})
		}
		cuerpo["pictures"] = imgs
	}

	var avisos []string
	if len(cuerpo) > 0 {
		var resp respuestaItem
		if err := a.llamar(ctx, http.MethodPut, "/items/"+req.Ref.ListingID, nil, cuerpo, &resp); err != nil {
			return channel.UpdateResult{}, err
		}
		avisos = resp.avisos()
	}
	if quiere("description") && req.Product.Description != "" {
		if err := a.llamar(ctx, http.MethodPut, "/items/"+req.Ref.ListingID+"/description",
			url.Values{"api_version": {"2"}},
			map[string]any{"plain_text": req.Product.Description}, nil); err != nil {
			avisos = append(avisos, "la descripción no se actualizó: "+err.Error())
		}
	}
	return channel.UpdateResult{Ref: req.Ref, Warnings: avisos}, nil
}

// UpdatePrice cambia el precio standard de cada publicación.
//
// Desde el 18/03/2026 ML rechaza el cambio de precio por API en los ítems
// que tienen automatización de precios activa: con solo price responde 400
// (item.price.not_modifiable) y si viene junto a otros campos responde 200,
// ignora el precio y lo cuenta en warnings. Aquí se manda solo price y se
// comprueba en la respuesta que el precio aplicado sea el pedido, para que
// un "200 sin efecto" no se anote como precio publicado.
func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		var resp respuestaItem
		err := a.llamar(ctx, http.MethodPut, "/items/"+u.Ref.ListingID, nil,
			map[string]any{"price": u.RegularPrice}, &resp)
		if err != nil {
			if precioBloqueado(err) {
				err = fmt.Errorf("MercadoLibre no deja cambiar el precio de %s por API porque tiene "+
					"automatización de precios activa; hay que desactivarla en Seller Central o "+
					"dejar que la gestione ML: %w", u.Ref.ListingID, err)
			}
			out = append(out, channel.OpResult{Ref: u.Ref, OK: false, Error: err})
			continue
		}
		for _, w := range resp.Warnings {
			if w.Code == "item.price.not_modifiable" {
				err = fmt.Errorf("MercadoLibre ignoró el precio de %s: %s", u.Ref.ListingID, w.Message)
			}
		}
		if err == nil && resp.Price != 0 && math.Abs(resp.Price-u.RegularPrice) > 0.005 {
			err = fmt.Errorf("MercadoLibre respondió con precio %.2f cuando se pidió %.2f en %s",
				resp.Price, u.RegularPrice, u.Ref.ListingID)
		}
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

func precioBloqueado(err error) bool {
	var e *channel.Error
	if !errors.As(err, &e) {
		return false
	}
	return e.Code == "item.price.not_modifiable" ||
		strings.Contains(e.Message, "item.price.not_modifiable")
}

func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		err := a.llamar(ctx, http.MethodPut, "/items/"+u.Ref.ListingID, nil,
			map[string]any{"available_quantity": u.Quantity}, nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

func (a *Adaptador) Pause(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/items/"+ref.ListingID, nil,
		map[string]any{"status": "paused"}, nil)
}

func (a *Adaptador) Resume(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/items/"+ref.ListingID, nil,
		map[string]any{"status": "active"}, nil)
}

// itemML es la vista de un ítem que se usa en consultas.
type itemML struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Status            string   `json:"status"`
	SubStatus         []string `json:"sub_status"`
	Permalink         string   `json:"permalink"`
	Price             float64  `json:"price"`
	AvailableQuantity int      `json:"available_quantity"`
	Attributes        []struct {
		ID        string `json:"id"`
		ValueName string `json:"value_name"`
	} `json:"attributes"`
}

// multiget trae varios ítems en una llamada (GET /items?ids=), en tandas de
// tamMultiget. Devuelve solo los que ML encontró, indexados por id.
func (a *Adaptador) multiget(ctx context.Context, ids []string, atributos string) (map[string]itemML, error) {
	out := make(map[string]itemML, len(ids))
	for i := 0; i < len(ids); i += tamMultiget {
		fin := i + tamMultiget
		if fin > len(ids) {
			fin = len(ids)
		}
		q := url.Values{"ids": {strings.Join(ids[i:fin], ",")}}
		if atributos != "" {
			q.Set("attributes", atributos)
		}
		var resp []struct {
			Code int    `json:"code"`
			Body itemML `json:"body"`
		}
		if err := a.llamar(ctx, http.MethodGet, "/items", q, nil, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp {
			if r.Code == http.StatusOK && r.Body.ID != "" {
				out[r.Body.ID] = r.Body
			}
		}
	}
	return out, nil
}

func (a *Adaptador) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	ids := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.ListingID != "" {
			ids = append(ids, r.ListingID)
		}
	}
	items, err := a.multiget(ctx, ids, "id,status,sub_status,permalink,price,available_quantity")
	if err != nil {
		return nil, err
	}
	out := make([]channel.ListingStatus, 0, len(refs))
	for _, ref := range refs {
		it, ok := items[ref.ListingID]
		if !ok {
			continue
		}
		out = append(out, channel.ListingStatus{
			Ref: ref, Status: estadoCon(it), Permalink: it.Permalink,
			Price: it.Price, Quantity: it.AvailableQuantity,
		})
	}
	return out, nil
}

// estadoCon compone "paused/out_of_stock": el subestado es lo que distingue
// una pausa que se levanta sola al reponer stock de una decidida a mano.
func estadoCon(it itemML) string {
	if len(it.SubStatus) == 0 {
		return it.Status
	}
	return it.Status + "/" + strings.Join(it.SubStatus, ",")
}

func (a *Adaptador) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	if err := a.perfil(ctx); err != nil {
		return channel.RemotePage{}, err
	}
	offset := cur.Page * tamPagina
	q := url.Values{"limit": {strconv.Itoa(tamPagina)}, "offset": {strconv.Itoa(offset)}}

	var resp struct {
		Results []string `json:"results"`
		Paging  struct {
			Total int `json:"total"`
		} `json:"paging"`
	}
	if err := a.llamar(ctx, http.MethodGet,
		"/users/"+strconv.FormatInt(a.userID, 10)+"/items/search", q, nil, &resp); err != nil {
		return channel.RemotePage{}, err
	}

	detalle, err := a.multiget(ctx, resp.Results, "id,title,status,sub_status,price,available_quantity,attributes")
	if err != nil {
		return channel.RemotePage{}, err
	}
	var items []channel.RemoteListing
	for _, id := range resp.Results {
		it, ok := detalle[id]
		if !ok {
			continue
		}
		items = append(items, channel.RemoteListing{
			Ref: channel.ExternalRef{
				ListingID: it.ID, VariantID: it.ID, SKU: atributo(it.Attributes, "SELLER_SKU"),
			},
			Title: it.Title, Status: estadoCon(it),
			Price: it.Price, Quantity: it.AvailableQuantity,
		})
	}
	return channel.RemotePage{
		Items: items, Next: channel.Cursor{Page: cur.Page + 1},
		Done: len(resp.Results) < tamPagina || offset+len(resp.Results) >= resp.Paging.Total,
	}, nil
}

// ordenML es lo que interesa de una orden tal como la devuelve
// /orders/search (que ya trae el detalle completo, sin GET por orden).
type ordenML struct {
	ID              int64   `json:"id"`
	Status          string  `json:"status"`
	DateCreated     string  `json:"date_created"`
	DateLastUpdated string  `json:"date_last_updated"`
	LastUpdated     string  `json:"last_updated"`
	TotalAmount     float64 `json:"total_amount"`
	ShippingCost    float64 `json:"shipping_cost"`
	CurrencyID      string  `json:"currency_id"`
	Taxes           struct {
		Amount *float64 `json:"amount"`
	} `json:"taxes"`
	Buyer struct {
		ID        int64  `json:"id"`
		Nickname  string `json:"nickname"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Phone     struct {
			AreaCode string `json:"area_code"`
			Number   string `json:"number"`
		} `json:"phone"`
		BillingInfo struct {
			DocType   string `json:"doc_type"`
			DocNumber string `json:"doc_number"`
		} `json:"billing_info"`
	} `json:"buyer"`
	Shipping struct {
		ID int64 `json:"id"`
	} `json:"shipping"`
	OrderItems []struct {
		Item struct {
			ID          string      `json:"id"`
			Title       string      `json:"title"`
			SellerSKU   string      `json:"seller_sku"`
			VariationID json.Number `json:"variation_id"`
		} `json:"item"`
		Quantity   float64 `json:"quantity"`
		UnitPrice  float64 `json:"unit_price"`
		CurrencyID string  `json:"currency_id"`
	} `json:"order_items"`
}

// envioML es lo que interesa de GET /shipments/{id}: la dirección de
// entrega, que no viaja en la orden, y el costo del envío.
type envioML struct {
	Status          string `json:"status"`
	ReceiverAddress struct {
		ReceiverName  string `json:"receiver_name"`
		ReceiverPhone string `json:"receiver_phone"`
		AddressLine   string `json:"address_line"`
		StreetName    string `json:"street_name"`
		StreetNumber  string `json:"street_number"`
		Comment       string `json:"comment"`
		ZipCode       string `json:"zip_code"`
		City          struct {
			Name string `json:"name"`
		} `json:"city"`
		State struct {
			Name string `json:"name"`
		} `json:"state"`
		Country struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"country"`
	} `json:"receiver_address"`
	ShippingOption struct {
		Cost float64 `json:"cost"`
	} `json:"shipping_option"`
	LeadTime struct {
		Cost float64 `json:"cost"`
	} `json:"lead_time"`
}

// FetchOrders trae los pedidos del vendedor modificados desde `desde`.
//
// Se filtra por fecha de última modificación y no de creación: un pedido
// viejo que pasa a pagado o cancelado también hay que verlo. ML guarda 12
// meses y, buscando como vendedor, excluye los cancelados.
func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	if err := a.perfil(ctx); err != nil {
		return channel.OrderPage{}, err
	}
	offset := cur.Page * tamPagina
	q := url.Values{
		"seller": {strconv.FormatInt(a.userID, 10)},
		"sort":   {"date_asc"},
		"limit":  {strconv.Itoa(tamPagina)},
		"offset": {strconv.Itoa(offset)},
	}
	if !desde.IsZero() {
		// ML documenta que el filtro se aplica "hasta la hora": se redondea
		// hacia abajo para no perder nada por los minutos.
		q.Set("order.date_last_updated.from",
			desde.UTC().Truncate(time.Hour).Format("2006-01-02T15:04:05.000-07:00"))
	}

	var resp struct {
		Results []json.RawMessage `json:"results"`
		Paging  struct {
			Total int `json:"total"`
		} `json:"paging"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/orders/search", q, nil, &resp); err != nil {
		return channel.OrderPage{}, err
	}

	var out []channel.Order
	for _, crudo := range resp.Results {
		var o ordenML
		if err := json.Unmarshal(crudo, &o); err != nil {
			return channel.OrderPage{}, fmt.Errorf("orden ilegible de MercadoLibre: %w", err)
		}
		ord := channel.Order{
			ExternalID: strconv.FormatInt(o.ID, 10), Number: strconv.FormatInt(o.ID, 10),
			Status:    o.Status,
			OrderedAt: fechaML(o.DateCreated),
			UpdatedAt: fechaML(o.DateLastUpdated, o.LastUpdated),
			Currency:  monedaDe(o.CurrencyID), Total: o.TotalAmount,
			Shipping: o.ShippingCost,
			Buyer: channel.Buyer{
				Name:     strings.TrimSpace(o.Buyer.FirstName + " " + o.Buyer.LastName),
				Email:    o.Buyer.Email,
				Phone:    strings.TrimSpace(o.Buyer.Phone.AreaCode + " " + o.Buyer.Phone.Number),
				Document: o.Buyer.BillingInfo.DocNumber,
			},
			Raw: append([]byte(nil), crudo...),
		}
		if o.Taxes.Amount != nil {
			ord.Tax = *o.Taxes.Amount
		}
		if ord.Buyer.Name == "" {
			ord.Buyer.Name = o.Buyer.Nickname
		}
		for _, li := range o.OrderItems {
			variante := li.Item.ID
			if li.Item.VariationID != "" && li.Item.VariationID != "null" {
				variante = li.Item.ID + ":" + li.Item.VariationID.String()
			}
			ord.Lines = append(ord.Lines, channel.OrderLine{
				ExternalID: li.Item.ID, SKU: li.Item.SellerSKU, Title: li.Item.Title,
				VariantRef: variante,
				Quantity:   li.Quantity, UnitPrice: li.UnitPrice,
				TotalPrice: li.UnitPrice * li.Quantity,
			})
		}
		// La dirección de entrega vive en el envío. Que no se pueda leer no
		// invalida el pedido: se guarda sin dirección y se ve en el panel.
		if o.Shipping.ID != 0 {
			if env, err := a.envio(ctx, o.Shipping.ID); err == nil {
				a.volcarEnvio(&ord, env)
			}
		}
		out = append(out, ord)
	}
	return channel.OrderPage{
		Orders: out, Next: channel.Cursor{Page: cur.Page + 1},
		Done: len(resp.Results) < tamPagina || offset+len(resp.Results) >= resp.Paging.Total,
	}, nil
}

func (a *Adaptador) envio(ctx context.Context, id int64) (*envioML, error) {
	var env envioML
	// X-Api-Version: 2 es lo que hace que vengan nombre y teléfono del
	// receptor; sin él ML los omite por ser datos personales.
	err := a.llamarCon(ctx, http.MethodGet, "/shipments/"+strconv.FormatInt(id, 10), nil,
		map[string]string{"X-Api-Version": "2"}, nil, &env)
	if err != nil {
		return nil, err
	}
	return &env, nil
}

func (a *Adaptador) volcarEnvio(ord *channel.Order, env *envioML) {
	r := env.ReceiverAddress
	linea1 := strings.TrimSpace(r.AddressLine)
	if linea1 == "" {
		linea1 = strings.TrimSpace(r.StreetName + " " + r.StreetNumber)
	}
	ord.Buyer.Address = channel.Address{
		Line1: linea1, Line2: strings.TrimSpace(r.Comment),
		City: r.City.Name, State: r.State.Name,
		PostalCode: r.ZipCode, Country: r.Country.ID,
	}
	if ord.Buyer.Phone == "" {
		ord.Buyer.Phone = strings.TrimSpace(r.ReceiverPhone)
	}
	if ord.Buyer.Name == "" {
		ord.Buyer.Name = strings.TrimSpace(r.ReceiverName)
	}
	if ord.Shipping == 0 {
		ord.Shipping = env.LeadTime.Cost
	}
	if ord.Shipping == 0 {
		ord.Shipping = env.ShippingOption.Cost
	}
}

func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	// El despacho de MercadoLibre va por su propio recurso de envíos y
	// depende de si es Mercado Envíos o logística propia. Se implementa junto
	// con el resto del despacho.
	return fmt.Errorf("confirmación de despacho aún no implementada para MercadoLibre")
}

// ------------------------------------------------------------- auxiliares

// perfil carga id y modelo de publicación del vendedor, una sola vez.
func (a *Adaptador) perfil(ctx context.Context) error {
	a.mu.Lock()
	cargado := a.perfilCargado
	a.mu.Unlock()
	if cargado {
		return nil
	}

	var resp struct {
		ID   int64    `json:"id"`
		Tags []string `json:"tags"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/users/me", nil, nil, &resp); err != nil {
		return err
	}
	up := false
	for _, t := range resp.Tags {
		if t == "user_product_seller" {
			up = true
		}
	}
	a.mu.Lock()
	a.userID, a.upSeller, a.perfilCargado = resp.ID, up, true
	a.mu.Unlock()
	return nil
}

func (a *Adaptador) esUserProductSeller() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.upSeller
}

func (a *Adaptador) buscarPorSKU(ctx context.Context, sku string) (*channel.ExternalRef, error) {
	if sku == "" {
		return nil, nil
	}
	if err := a.perfil(ctx); err != nil {
		return nil, err
	}
	// ML indexa el SKU del vendedor y permite buscarlo directamente.
	q := url.Values{"seller_sku": {sku}, "limit": {"5"}}
	var resp struct {
		Results []string `json:"results"`
	}
	if err := a.llamar(ctx, http.MethodGet,
		"/users/"+strconv.FormatInt(a.userID, 10)+"/items/search", q, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, nil
	}
	id := resp.Results[0]
	return &channel.ExternalRef{ListingID: id, VariantID: id, SKU: sku}, nil
}

// accessToken devuelve un token vigente, refrescándolo si hace falta.
//
// ML rota el refresh token en cada canje e invalida el anterior, así que el
// nuevo se guarda en memoria y, si hay dónde, también en la base: si el
// proceso muere con el nuevo solo en memoria, el siguiente arranque canjea
// el viejo y ML responde invalid_grant.
func (a *Adaptador) accessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	// Se refresca un minuto antes de caducar para no perder una llamada por
	// el camino.
	if a.token != "" && (a.expira.IsZero() || time.Now().Before(a.expira.Add(-time.Minute))) {
		tok := a.token
		a.mu.Unlock()
		return tok, nil
	}
	if a.refresh == "" || a.appID == "" || a.appSecret == "" {
		tok := a.token // solo hay token fijo: se usa hasta que falle
		a.mu.Unlock()
		return tok, nil
	}

	// El canje se hace con el candado tomado: dos llamadas concurrentes que
	// refrescaran a la vez se invalidarían el refresh token entre sí.
	t, err := conectores.RefrescarTokenML(ctx, a.appID, a.appSecret, a.refresh)
	if err != nil {
		a.mu.Unlock()
		return "", err
	}
	a.token = t.AccessToken
	if t.RefreshToken != "" {
		a.refresh = t.RefreshToken
	}
	a.expira = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	tok := a.token
	cred := map[string]string{
		"app_id": a.appID, "app_secret": a.appSecret,
		"refresh_token": a.refresh, "access_token": a.token,
	}
	persistir := a.persistir
	a.mu.Unlock()

	if persistir != nil {
		if err := persistir(ctx, cred); err != nil {
			return "", fmt.Errorf("el token de MercadoLibre se renovó pero no se pudo guardar el refresh token nuevo: %w", err)
		}
	}
	return tok, nil
}

func (a *Adaptador) llamar(ctx context.Context, metodo, ruta string, q url.Values, cuerpo any, out any) error {
	return a.llamarCon(ctx, metodo, ruta, q, nil, cuerpo, out)
}

// llamarCon hace una llamada autenticada. Si ML responde 401 y hay con qué
// refrescar, canjea el token y repite una vez: es el caso normal tras un
// reinicio, cuando el access token guardado ya caducó pero el refresh no.
func (a *Adaptador) llamarCon(ctx context.Context, metodo, ruta string, q url.Values, cabeceras map[string]string, cuerpo any, out any) error {
	err := a.llamarUnaVez(ctx, metodo, ruta, q, cabeceras, cuerpo, out)
	var e *channel.Error
	if errors.As(err, &e) && e.StatusCode == http.StatusUnauthorized && a.puedeRefrescar() {
		return a.llamarUnaVez(ctx, metodo, ruta, q, cabeceras, cuerpo, out)
	}
	return err
}

func (a *Adaptador) puedeRefrescar() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.refresh != "" && a.appID != "" && a.appSecret != ""
}

func (a *Adaptador) llamarUnaVez(ctx context.Context, metodo, ruta string, q url.Values, cabeceras map[string]string, cuerpo any, out any) error {
	token, err := a.accessToken(ctx)
	if err != nil {
		return err
	}

	var body io.Reader
	if cuerpo != nil {
		j, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		body = bytes.NewReader(j)
	}
	destino := a.base + ruta
	if len(q) > 0 {
		destino += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, metodo, destino, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cabeceras {
		req.Header.Set(k, v)
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		return &channel.Error{Kind: channel.MercadoLibre, Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &channel.Error{
			Kind: channel.MercadoLibre, StatusCode: resp.StatusCode,
			Message: recortarRunes(string(datos), 300),
		}
		// ML devuelve {"error": "codigo", "message": "..."} en los fallos de
		// validación; el código es lo que permite decidir qué hacer.
		var detalle struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(datos, &detalle) == nil {
			e.Code = detalle.Error
			if detalle.Message != "" {
				e.Message = detalle.Error + ": " + recortarRunes(detalle.Message, 300)
			}
		}
		if resp.StatusCode == http.StatusNotFound {
			e.Err = channel.ErrNoEncontrado
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				e.RetryAfter = time.Duration(s) * time.Second
			}
		}
		// Un 401 suele ser el token caducado: se invalida para que el próximo
		// intento lo refresque en vez de repetir el fallo.
		if resp.StatusCode == http.StatusUnauthorized {
			a.mu.Lock()
			a.expira = time.Time{}
			a.token = ""
			a.mu.Unlock()
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(datos, out)
}

// fechaML interpreta las fechas de ML ("2019-05-22T03:51:05.000-04:00"),
// probando cada candidata hasta que una tenga valor legible.
func fechaML(candidatas ...string) time.Time {
	for _, s := range candidatas {
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func atributo(attrs []struct {
	ID        string `json:"id"`
	ValueName string `json:"value_name"`
}, id string) string {
	for _, a := range attrs {
		if a.ID == id {
			return a.ValueName
		}
	}
	return ""
}

func monedaDe(s string) string {
	if strings.TrimSpace(s) == "" {
		return "COP"
	}
	return strings.ToUpper(s)
}

func recortarRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
