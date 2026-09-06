// Package falabella implementa el canal Falabella Seller Center.
//
// Es el más estricto de los cuatro y por eso va el último: firma HMAC en cada
// llamada, exige EAN y peso para calcular el envío, y sus escrituras son
// ASÍNCRONAS — devuelven un FeedID y el resultado se consulta después. Es el
// único canal con AsyncFeeds, así que estrena esa rama del contrato.
//
// Dos formas de la API condicionan todo el fichero. La primera: el precio, el
// stock y el estado NO son campos del producto, viven dentro de
// <BusinessUnits><BusinessUnit> junto al OperatorCode de la unidad de negocio
// (faco en Colombia); fuera de ahí el feed se acepta y no cambia nada. La
// segunda: las respuestas JSON anidan cada colección en un elemento con nombre
// propio (Body.Products.Product) y colapsan el array a objeto cuando solo hay
// un elemento, así que las listas se decodifican con listaSC y no con []T.
package falabella

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
)

const endpointPorDefecto = "https://sellercenter-api.falabella.com/"

// operadorPorDefecto es la unidad de negocio de Falabella Colombia. Los
// códigos son facl (Chile), faco (Colombia) y fape (Perú); se puede fijar otro
// con la credencial operator_code sin tocar código.
const operadorPorDefecto = "faco"

// fechaSC es el formato de fecha que acepta Seller Center en las promociones.
const fechaSC = "2006-01-02 15:04:05"

// esperasFeed son las pausas entre consultas de FeedStatus después de cada
// escritura. La escritura es asíncrona: un 200 solo dice que el feed se
// aceptó, así que sin confirmarlo un rechazo del Seller Center (atributo
// obligatorio ausente, EAN duplicado) pasaría por publicación correcta y el
// motor de diff no volvería a encolar el producto nunca.
var esperasFeed = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

func init() {
	channel.Register(channel.Falabella, func(cfg channel.Config) (channel.Adapter, error) {
		userID := cfg.Credentials["user_id"]
		apiKey := cfg.Credentials["api_key"]
		if userID == "" || apiKey == "" {
			return nil, fmt.Errorf("faltan el UserID y la API Key del Seller Center")
		}
		operador := cfg.Credentials["operator_code"]
		if operador == "" {
			operador = operadorPorDefecto
		}
		return &Adaptador{
			userID: userID, apiKey: apiKey, operador: operador,
			base:    endpointPorDefecto,
			cli:     &http.Client{Timeout: 40 * time.Second},
			esperas: esperasFeed,
		}, nil
	})
}

type Adaptador struct {
	userID   string
	apiKey   string
	operador string
	// base es variable para que las pruebas apunten a un httptest.Server.
	base    string
	cli     *http.Client
	esperas []time.Duration
}

func (a *Adaptador) Kind() channel.Kind { return channel.Falabella }

func (a *Adaptador) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		NativeCompareAtPrice: true, // SalePrice sobre Price
		ScheduledOffers:      true, // SaleStartDate / SaleEndDate
		BulkPriceUpdate:      true,
		BulkStockUpdate:      true,
		MaxBatchSize:         50,
		AsyncFeeds:           true, // lo distingue de los otros tres
		Variants:             channel.SinVariantes,
		RequiresOAuthRefresh: false,
		MaxTitleLength:       150,
		RequiresDescription:  true,

		RequiresCategoryMapping: true,
	}
}

// producto es la forma XML que espera Seller Center. La API acepta JSON en la
// respuesta pero el cuerpo de las escrituras va siempre en XML.
type sobreProductos struct {
	XMLName  xml.Name      `xml:"Request"`
	Producto []productoXML `xml:"Product"`
}

type productoXML struct {
	SellerSku       string       `xml:"SellerSku"`
	Name            string       `xml:"Name,omitempty"`
	Description     string       `xml:"Description,omitempty"`
	Brand           string       `xml:"Brand,omitempty"`
	PrimaryCategory string       `xml:"PrimaryCategory,omitempty"`
	ProductId       string       `xml:"ProductId,omitempty"` // el EAN
	Unidades        *unidadesXML `xml:"BusinessUnits,omitempty"`
	Datos           *datosXML    `xml:"ProductData,omitempty"`
	Images          *imgs        `xml:"Images,omitempty"`
}

// unidadXML es la unidad de negocio: el sitio donde Falabella guarda de verdad
// el precio, el stock y el estado de la publicación.
type unidadesXML struct {
	Unidad []unidadXML `xml:"BusinessUnit"`
}

type unidadXML struct {
	OperatorCode    string `xml:"OperatorCode"`
	Price           string `xml:"Price,omitempty"`
	SpecialPrice    string `xml:"SpecialPrice,omitempty"`
	SpecialFromDate string `xml:"SpecialFromDate,omitempty"`
	SpecialToDate   string `xml:"SpecialToDate,omitempty"`
	Stock           string `xml:"Stock,omitempty"`
	Status          string `xml:"Status,omitempty"`
}

// datosXML es <ProductData>: la condición, las medidas del paquete y los
// atributos obligatorios de la categoría, cada uno como elemento propio con su
// nombre. Falabella identifica los atributos por NOMBRE, no por un id opaco.
type datosXML struct {
	ConditionType string        `xml:"ConditionType,omitempty"`
	PackageWeight string        `xml:"PackageWeight,omitempty"`
	Atributos     []atributoXML `xml:",omitempty"`
}

// atributoXML serializa un par nombre/valor como <talla>M</talla>: el nombre
// del elemento sale del dato, por eso lleva XMLName en vez de una etiqueta.
type atributoXML struct {
	XMLName xml.Name
	Valor   string `xml:",chardata"`
}

type imgs struct {
	Image []string `xml:"Image"`
}

func (a *Adaptador) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	p := req.Product
	if len(p.Variants) == 0 {
		return channel.PublishResult{}, fmt.Errorf("el producto %s no trae variantes", p.SKU)
	}
	v := p.Variants[0]

	// Falabella no calcula el envío sin peso ni identifica el producto sin
	// EAN: comprobarlo aquí evita un feed que fallará de todas formas.
	if v.Barcode == "" {
		return channel.PublishResult{}, fmt.Errorf("Falabella exige EAN y %s no lo tiene", v.SKU)
	}
	if p.Weight <= 0 {
		return channel.PublishResult{}, fmt.Errorf("Falabella necesita el peso para calcular el envío y %s no lo tiene", v.SKU)
	}
	if p.CategoryID == "" {
		return channel.PublishResult{}, fmt.Errorf("Falabella exige categoría y %s no la tiene mapeada", v.SKU)
	}

	existe, err := a.existeSKU(ctx, v.SKU)
	if err != nil {
		return channel.PublishResult{}, err
	}

	// El SKU ES la referencia en Falabella: no hay un id de publicación
	// separado, y eso simplifica todo lo demás.
	ref := channel.ExternalRef{ListingID: v.SKU, VariantID: v.SKU, SKU: v.SKU}
	if req.DryRun {
		return channel.PublishResult{Ref: ref, Adopted: existe}, nil
	}

	ficha := a.ficha(p, v)
	accion := "ProductCreate"
	var avisos []string
	if existe {
		// Adoptar sin enviar nada dejaba el cambio de contenido sin viajar
		// jamás: el motor guarda el hash nuevo en cuanto Publish responde, así
		// que la descripción o la foto corregidas se daban por publicadas. Se
		// manda la ficha con ProductUpdate y se adopta la publicación.
		//
		// El precio y el stock NO van aquí: tienen sus propios trabajos
		// (UpdatePrice/UpdateStock) y enviarlos en la adopción pisaría los
		// valores vivos del Seller Center en la primera sincronización.
		accion = "ProductUpdate"
		avisos = append(avisos, "ya existía en el Seller Center con el mismo SKU: se adoptó y se le mandó la ficha")
	} else {
		// Inactivo al crear, como en los otros tres canales. Precio, stock y
		// estado van dentro de la unidad de negocio: como campos planos del
		// producto, Falabella acepta el feed y no los aplica.
		u := unidadXML{
			OperatorCode: a.operador,
			Price:        importe(v.RegularPrice),
			Stock:        strconv.Itoa(v.Quantity),
			Status:       "inactive",
		}
		if v.SalePrice > 0 {
			u.SpecialPrice = importe(v.SalePrice)
			u.SpecialFromDate = fechaOferta(v.SaleStartsAt)
			u.SpecialToDate = fechaOferta(v.SaleEndsAt)
		}
		ficha.Unidades = &unidadesXML{Unidad: []unidadXML{u}}
	}

	feed, err := a.enviarProductos(ctx, accion, []productoXML{ficha})
	if err != nil {
		return channel.PublishResult{}, err
	}
	if motivo := feed.Rechazo(v.SKU); motivo != "" {
		return channel.PublishResult{}, &channel.Error{
			Kind: channel.Falabella, Code: "feed", Message: motivo,
		}
	}
	return channel.PublishResult{
		Ref: ref, Adopted: existe,
		VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
		Warnings:    append(avisos, feed.Aviso()),
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	v := channel.Variant{SKU: req.Ref.SKU}
	if len(req.Product.Variants) > 0 {
		v = req.Product.Variants[0]
	}
	// La ficha va entera: Seller Center no tiene un endpoint por campo y lo que
	// cambia en Integra (descripción, marca, imágenes, atributos) tiene que
	// viajar junto para que el feed deje el producto como está en el catálogo.
	ficha := a.ficha(req.Product, v)
	ficha.SellerSku = req.Ref.SKU
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	feed, err := a.enviarProductos(ctx, "ProductUpdate", []productoXML{ficha})
	if err != nil {
		return channel.UpdateResult{}, err
	}
	if motivo := feed.Rechazo(req.Ref.SKU); motivo != "" {
		return channel.UpdateResult{}, &channel.Error{
			Kind: channel.Falabella, Code: "feed", Message: motivo,
		}
	}
	return channel.UpdateResult{Ref: req.Ref, Warnings: []string{feed.Aviso()}}, nil
}

// UpdatePrice aprovecha el lote: Falabella acepta hasta 50 por feed, así que
// mandar uno a uno desperdiciaría llamadas de una API con cupo.
func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	var prods []productoXML
	for _, u := range ups {
		unidad := unidadXML{OperatorCode: a.operador, Price: importe(u.RegularPrice)}
		if u.SalePrice > 0 {
			unidad.SpecialPrice = importe(u.SalePrice)
			unidad.SpecialFromDate = fechaOferta(u.StartsAt)
			unidad.SpecialToDate = fechaOferta(u.EndsAt)
		}
		prods = append(prods, productoXML{
			SellerSku: u.Ref.SKU,
			Unidades:  &unidadesXML{Unidad: []unidadXML{unidad}},
		})
	}
	feed, err := a.enviarProductos(ctx, "ProductUpdate", prods)
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		out = append(out, resultadoDe(u.Ref, feed, err))
	}
	return out, nil
}

func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	var prods []productoXML
	for _, u := range ups {
		prods = append(prods, productoXML{
			SellerSku: u.Ref.SKU,
			Unidades: &unidadesXML{Unidad: []unidadXML{{
				OperatorCode: a.operador, Stock: strconv.Itoa(u.Quantity),
			}}},
		})
	}
	feed, err := a.enviarProductos(ctx, "ProductUpdate", prods)
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		out = append(out, resultadoDe(u.Ref, feed, err))
	}
	return out, nil
}

func (a *Adaptador) Pause(ctx context.Context, ref channel.ExternalRef) error {
	return a.cambiarEstado(ctx, ref, "inactive")
}

func (a *Adaptador) Resume(ctx context.Context, ref channel.ExternalRef) error {
	return a.cambiarEstado(ctx, ref, "active")
}

// cambiarEstado manda el estado dentro de la unidad de negocio: un
// <Status>inactive</Status> suelto se acepta y el producto sigue vendiéndose.
func (a *Adaptador) cambiarEstado(ctx context.Context, ref channel.ExternalRef, estado string) error {
	feed, err := a.enviarProductos(ctx, "ProductUpdate", []productoXML{{
		SellerSku: ref.SKU,
		Unidades: &unidadesXML{Unidad: []unidadXML{{
			OperatorCode: a.operador, Status: estado,
		}}},
	}})
	if err != nil {
		return err
	}
	if motivo := feed.Rechazo(ref.SKU); motivo != "" {
		return &channel.Error{Kind: channel.Falabella, Code: "feed", Message: motivo}
	}
	return nil
}

func (a *Adaptador) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	var out []channel.ListingStatus
	for _, ref := range refs {
		p := conectores.ParamsFalabella("GetProducts", a.userID)
		p["SkuSellerList"] = `["` + ref.SKU + `"]`

		// El error se propaga: tragárselo devolvía una lista vacía, que el
		// núcleo no distingue de "no hay nada publicado".
		prods, err := a.productos(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, pr := range prods {
			precio, cant, estado := a.venta(pr)
			out = append(out, channel.ListingStatus{
				Ref: ref, Status: estado, Price: precio, Quantity: cant,
			})
		}
	}
	return out, nil
}

func (a *Adaptador) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	limite := cur.Size
	if limite <= 0 || limite > 100 {
		limite = 100
	}
	p := conectores.ParamsFalabella("GetProducts", a.userID)
	p["Limit"] = strconv.Itoa(limite)
	p["Offset"] = strconv.Itoa(cur.Page * limite)

	prods, err := a.productos(ctx, p)
	if err != nil {
		return channel.RemotePage{}, err
	}

	var items []channel.RemoteListing
	for _, pr := range prods {
		precio, cant, estado := a.venta(pr)
		items = append(items, channel.RemoteListing{
			Ref:   channel.ExternalRef{ListingID: pr.SellerSku, VariantID: pr.SellerSku, SKU: pr.SellerSku},
			Title: pr.Name, Status: estado, Price: precio, Quantity: cant,
		})
	}
	return channel.RemotePage{
		Items: items, Next: channel.Cursor{Page: cur.Page + 1, Size: limite},
		Done: len(items) < limite,
	}, nil
}

func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	limite := cur.Size
	if limite <= 0 || limite > 100 {
		limite = 100
	}
	p := conectores.ParamsFalabella("GetOrders", a.userID)
	p["Limit"] = strconv.Itoa(limite)
	// Sin Offset el núcleo pedía cuarenta veces la misma primera página y los
	// pedidos a partir del 101 no entraban nunca. Y ordenado por fecha
	// ascendente, para que la marca de agua avance por el pedido más viejo y no
	// pueda saltarse los que quedaron fuera de la página.
	p["Offset"] = strconv.Itoa(cur.Page * limite)
	p["SortBy"] = "created_at"
	p["SortDirection"] = "ASC"
	if !desde.IsZero() {
		p["CreatedAfter"] = desde.UTC().Format("2006-01-02T15:04:05-0700")
	}

	var resp struct {
		SuccessResponse struct {
			Body struct {
				Orders json.RawMessage `json:"Orders"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return channel.OrderPage{}, err
	}
	pedidos, err := listaSC[ordenResp](resp.SuccessResponse.Body.Orders, "Order")
	if err != nil {
		return channel.OrderPage{}, &channel.Error{
			Kind: channel.Falabella, Message: "GetOrders ilegible: " + err.Error(), Err: err,
		}
	}

	var out []channel.Order
	for _, o := range pedidos {
		total, _ := strconv.ParseFloat(string(o.Price), 64)
		envio, _ := strconv.ParseFloat(string(o.ShippingFee), 64)
		fecha, _ := time.Parse("2006-01-02 15:04:05", o.CreatedAt)

		ord := channel.Order{
			ExternalID: string(o.OrderId), Number: string(o.OrderNumber),
			OrderedAt: fecha, Currency: "COP", Total: total, Shipping: envio,
			Buyer: channel.Buyer{
				Name:  strings.TrimSpace(o.CustomerFirst + " " + o.CustomerLast),
				Phone: o.AddressShipping.Phone,
				Address: channel.Address{
					Line1: o.AddressShipping.Address1,
					City:  o.AddressShipping.City, State: o.AddressShipping.Region,
					PostalCode: o.AddressShipping.PostCode, Country: o.AddressShipping.Country,
				},
			},
		}
		// Las líneas van en otra llamada: GetOrderItems por pedido. El fallo se
		// propaga: un pedido sin líneas montaría en Odoo un sale.order vacío,
		// con total cobrado y nada que despachar, sin dejar rastro del error.
		lineas, err := a.lineasDe(ctx, string(o.OrderId))
		if err != nil {
			return channel.OrderPage{}, err
		}
		ord.Lines = lineas
		out = append(out, ord)
	}
	return channel.OrderPage{
		Orders: out,
		Next:   channel.Cursor{Page: cur.Page + 1, Size: limite},
		Done:   len(out) < limite,
	}, nil
}

type ordenResp struct {
	OrderId         numOTexto `json:"OrderId"`
	OrderNumber     numOTexto `json:"OrderNumber"`
	CreatedAt       string    `json:"CreatedAt"`
	Price           numOTexto `json:"Price"`
	ShippingFee     numOTexto `json:"ShippingFeeTotal"`
	CustomerFirst   string    `json:"CustomerFirstName"`
	CustomerLast    string    `json:"CustomerLastName"`
	AddressShipping struct {
		Address1 string `json:"Address1"`
		City     string `json:"City"`
		Ward     string `json:"Ward"`
		Region   string `json:"Region"`
		PostCode string `json:"PostCode"`
		Country  string `json:"Country"`
		Phone    string `json:"Phone"`
	} `json:"AddressShipping"`
}

func (a *Adaptador) lineasDe(ctx context.Context, orderID string) ([]channel.OrderLine, error) {
	p := conectores.ParamsFalabella("GetOrderItems", a.userID)
	p["OrderId"] = orderID

	var resp struct {
		SuccessResponse struct {
			Body struct {
				OrderItems json.RawMessage `json:"OrderItems"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return nil, err
	}
	items, err := listaSC[itemResp](resp.SuccessResponse.Body.OrderItems, "OrderItem")
	if err != nil {
		return nil, &channel.Error{
			Kind:    channel.Falabella,
			Message: "GetOrderItems ilegible en el pedido " + orderID + ": " + err.Error(),
			Err:     err,
		}
	}

	var out []channel.OrderLine
	for _, li := range items {
		precio, _ := strconv.ParseFloat(string(li.ItemPrice), 64)
		// Falabella entrega una línea por unidad, no una línea con cantidad.
		out = append(out, channel.OrderLine{
			ExternalID: string(li.OrderItemId), SKU: li.Sku, Title: li.Name,
			Quantity: 1, UnitPrice: precio, TotalPrice: precio,
		})
	}
	return out, nil
}

type itemResp struct {
	OrderItemId numOTexto `json:"OrderItemId"`
	Sku         string    `json:"Sku"`
	Name        string    `json:"Name"`
	ItemPrice   numOTexto `json:"ItemPrice"`
}

func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	p := conectores.ParamsFalabella("SetStatusToShipped", a.userID)
	p["OrderItemIds"] = "[" + ref.ListingID + "]"
	p["DeliveryType"] = "dropship"
	if f.TrackingNumber != "" {
		p["TrackingNumber"] = f.TrackingNumber
	}
	if f.Carrier != "" {
		p["ShippingProvider"] = f.Carrier
	}
	return a.llamar(ctx, http.MethodPost, p, nil, nil)
}

// ------------------------------------------------------------- auxiliares

// ficha arma la parte de contenido del producto: lo que no depende de la
// unidad de negocio. Precio, stock y estado los añade quien llama.
func (a *Adaptador) ficha(p channel.Product, v channel.Variant) productoXML {
	prod := productoXML{
		SellerSku:       v.SKU,
		Name:            recortarRunes(p.Title, 150),
		Description:     p.Description,
		Brand:           p.Brand,
		PrimaryCategory: p.CategoryID,
		ProductId:       v.Barcode,
		// ConditionType es obligatorio y el peso solo se lee dentro de
		// ProductData: como hijo directo de Product, Falabella lo ignora y el
		// producto queda sin peso de despacho.
		Datos: &datosXML{ConditionType: "Nuevo", Atributos: atributosXML(p.Attributes)},
	}
	if p.Weight > 0 {
		prod.Datos.PackageWeight = strconv.FormatFloat(p.Weight, 'f', 3, 64)
	}
	if len(p.Images) > 0 {
		var urls []string
		for _, i := range p.Images {
			urls = append(urls, i.URL)
		}
		prod.Images = &imgs{Image: urls}
	}
	return prod
}

// atributosXML convierte los atributos obligatorios de la categoría en
// elementos con su propio nombre (<talla>M</talla>), que es como Seller Center
// los identifica. Van ordenados para que el mismo producto produzca siempre el
// mismo XML.
func atributosXML(attrs map[string]string) []atributoXML {
	nombres := make([]string, 0, len(attrs))
	for k, v := range attrs {
		if strings.TrimSpace(v) == "" || !nombreXMLValido(k) {
			continue
		}
		nombres = append(nombres, k)
	}
	sort.Strings(nombres)
	out := make([]atributoXML, 0, len(nombres))
	for _, n := range nombres {
		out = append(out, atributoXML{XMLName: xml.Name{Local: n}, Valor: attrs[n]})
	}
	return out
}

// nombreXMLValido descarta las claves que no pueden ser un nombre de elemento:
// un atributo con espacios rompería el XML entero del feed, y con él el resto
// de productos del lote.
func nombreXMLValido(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && (r >= '0' && r <= '9' || r == '-' || r == '.'):
		default:
			return false
		}
	}
	return true
}

func importe(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) }

func fechaOferta(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format(fechaSC)
}

// Feed es el resultado de una escritura asíncrona.
type Feed struct {
	ID         string
	Estado     string
	Total      int
	Procesados int
	Fallidos   int
	Errores    []ErrorFeed
}

type ErrorFeed struct {
	SellerSku string
	Codigo    string
	Mensaje   string
}

// Terminado dice si el feed ya no va a cambiar de estado.
func (f Feed) Terminado() bool {
	switch f.Estado {
	case "Finished", "Error", "Canceled":
		return true
	}
	return false
}

// Rechazo devuelve por qué el feed rechazó un SKU, o cadena vacía si no lo
// rechazó (o si todavía no se sabe). Los errores sin SellerSku afectan al lote
// entero, así que valen para cualquier SKU.
func (f Feed) Rechazo(sku string) string {
	var generales []string
	for _, e := range f.Errores {
		msg := strings.TrimSpace(e.Codigo + " " + e.Mensaje)
		if e.SellerSku == "" {
			generales = append(generales, msg)
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.SellerSku), strings.TrimSpace(sku)) {
			return msg
		}
	}
	if len(generales) > 0 {
		return strings.Join(generales, "; ")
	}
	if f.Estado == "Error" || f.Estado == "Canceled" {
		return "el feed " + f.ID + " terminó en estado " + f.Estado
	}
	return ""
}

// Aviso es lo que se le cuenta al operador de una escritura que no se puede
// confirmar en el momento.
func (f Feed) Aviso() string {
	switch {
	case f.ID == "":
		return "escritura asíncrona: el Seller Center no devolvió identificador de feed, no se pudo confirmar"
	case f.Estado == "Finished":
		return "feed " + f.ID + " procesado sin errores"
	default:
		return "escritura asíncrona: el resultado se confirma en el feed " + f.ID
	}
}

// enviarProductos manda un feed y espera a saber cómo terminó. La escritura es
// asíncrona: que la llamada devuelva 200 solo significa que el feed se aceptó,
// así que se consulta FeedStatus antes de dar la escritura por buena.
func (a *Adaptador) enviarProductos(ctx context.Context, accion string, prods []productoXML) (Feed, error) {
	if len(prods) == 0 {
		return Feed{}, nil
	}
	cuerpo, err := xml.Marshal(sobreProductos{Producto: prods})
	if err != nil {
		return Feed{}, err
	}

	var resp struct {
		SuccessResponse struct {
			Head struct {
				RequestId string `json:"RequestId"`
			} `json:"Head"`
			Body struct {
				FeedID string `json:"FeedId"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	p := conectores.ParamsFalabella(accion, a.userID)
	if err := a.llamar(ctx, http.MethodPost, p, cuerpo, &resp); err != nil {
		return Feed{}, err
	}
	// El identificador del feed viaja en Head.RequestId; ProductCreate y
	// ProductUpdate devuelven el Body vacío.
	id := resp.SuccessResponse.Head.RequestId
	if id == "" {
		id = resp.SuccessResponse.Body.FeedID
	}
	if id == "" {
		return Feed{}, nil
	}
	return a.confirmarFeed(ctx, id), nil
}

// confirmarFeed sondea FeedStatus hasta que el feed termina o se agotan las
// esperas. Un fallo de la consulta no invalida la escritura: se devuelve el
// feed sin estado y quien llama lo trata como "aún sin confirmar".
func (a *Adaptador) confirmarFeed(ctx context.Context, id string) Feed {
	feed := Feed{ID: id}
	for _, espera := range a.esperas {
		if espera > 0 {
			t := time.NewTimer(espera)
			select {
			case <-ctx.Done():
				t.Stop()
				return feed
			case <-t.C:
			}
		}
		f, err := a.EstadoFeed(ctx, id)
		if err != nil {
			return feed
		}
		f.ID = id
		feed = f
		if feed.Terminado() {
			break
		}
	}
	return feed
}

// EstadoFeed consulta cómo terminó una escritura asíncrona.
func (a *Adaptador) EstadoFeed(ctx context.Context, feedID string) (Feed, error) {
	p := conectores.ParamsFalabella("FeedStatus", a.userID)
	p["FeedID"] = feedID

	var resp struct {
		SuccessResponse struct {
			Body struct {
				FeedDetail struct {
					Feed             string          `json:"Feed"`
					Status           string          `json:"Status"`
					TotalRecords     numOTexto       `json:"TotalRecords"`
					ProcessedRecords numOTexto       `json:"ProcessedRecords"`
					FailedRecords    numOTexto       `json:"FailedRecords"`
					FeedErrors       json.RawMessage `json:"FeedErrors"`
				} `json:"FeedDetail"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return Feed{}, err
	}
	d := resp.SuccessResponse.Body.FeedDetail
	f := Feed{ID: d.Feed, Estado: d.Status}
	f.Total, _ = strconv.Atoi(string(d.TotalRecords))
	f.Procesados, _ = strconv.Atoi(string(d.ProcessedRecords))
	f.Fallidos, _ = strconv.Atoi(string(d.FailedRecords))

	errores, err := listaSC[errorFeedResp](d.FeedErrors, "Error")
	if err != nil {
		return f, nil // el feed se leyó; solo la lista de errores es ilegible
	}
	for _, e := range errores {
		f.Errores = append(f.Errores, ErrorFeed{
			SellerSku: e.SellerSku, Codigo: string(e.Code), Mensaje: e.Message,
		})
	}
	return f, nil
}

type errorFeedResp struct {
	Message   string    `json:"Message"`
	SellerSku string    `json:"SellerSku"`
	Code      numOTexto `json:"ErrorCode"`
}

func (a *Adaptador) existeSKU(ctx context.Context, sku string) (bool, error) {
	p := conectores.ParamsFalabella("GetProducts", a.userID)
	p["SkuSellerList"] = `["` + sku + `"]`

	prods, err := a.productos(ctx, p)
	if err != nil {
		return false, err
	}
	for _, pr := range prods {
		if strings.EqualFold(strings.TrimSpace(pr.SellerSku), strings.TrimSpace(sku)) {
			return true, nil
		}
	}
	return false, nil
}

// productoResp es un producto tal como lo devuelve GetProducts. Los campos
// planos (Price/Quantity/Status) se conservan porque la respuesta heredada los
// trae ahí; los vigentes están dentro de BusinessUnits.
type productoResp struct {
	SellerSku string          `json:"SellerSku"`
	Name      string          `json:"Name"`
	Status    string          `json:"Status"`
	Price     numOTexto       `json:"Price"`
	Quantity  numOTexto       `json:"Quantity"`
	Unidades  json.RawMessage `json:"BusinessUnits"`
}

type unidadResp struct {
	OperatorCode string    `json:"OperatorCode"`
	Price        numOTexto `json:"Price"`
	SpecialPrice numOTexto `json:"SpecialPrice"`
	Stock        numOTexto `json:"Stock"`
	Status       string    `json:"Status"`
}

// productos ejecuta un GetProducts y devuelve la lista, venga anidada en
// Body.Products.Product o como array plano.
func (a *Adaptador) productos(ctx context.Context, p map[string]string) ([]productoResp, error) {
	var resp struct {
		SuccessResponse struct {
			Body struct {
				Products json.RawMessage `json:"Products"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return nil, err
	}
	prods, err := listaSC[productoResp](resp.SuccessResponse.Body.Products, "Product")
	if err != nil {
		return nil, &channel.Error{
			Kind: channel.Falabella, Message: "GetProducts ilegible: " + err.Error(), Err: err,
		}
	}
	return prods, nil
}

// venta saca precio, stock y estado de la unidad de negocio de la cuenta. Como
// campos planos del producto llegaban vacíos para todo el catálogo.
func (a *Adaptador) venta(pr productoResp) (precio float64, stock int, estado string) {
	precio, _ = strconv.ParseFloat(string(pr.Price), 64)
	stock, _ = strconv.Atoi(string(pr.Quantity))
	estado = pr.Status

	unidades, err := listaSC[unidadResp](pr.Unidades, "BusinessUnit")
	if err != nil || len(unidades) == 0 {
		return precio, stock, estado
	}
	u := unidades[0]
	for _, c := range unidades {
		if strings.EqualFold(c.OperatorCode, a.operador) {
			u = c
			break
		}
	}
	if v, err := strconv.ParseFloat(string(u.Price), 64); err == nil {
		precio = v
	}
	if v, err := strconv.Atoi(string(u.Stock)); err == nil {
		stock = v
	}
	if u.Status != "" {
		estado = u.Status
	}
	return precio, stock, estado
}

// numOTexto acepta un valor que Seller Center puede entregar como cadena o como
// número: la misma respuesta mezcla las dos formas según el campo y la versión.
type numOTexto string

func (n *numOTexto) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*n = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*n = numOTexto(s)
		return nil
	}
	*n = numOTexto(b)
	return nil
}

// listaSC decodifica una colección de Seller Center. La API la anida en un
// elemento con nombre propio (Body.Products.Product, Body.OrderItems.OrderItem)
// y colapsa el array a objeto cuando solo hay un elemento; algunas respuestas
// la entregan como array plano. Se aceptan las tres formas porque el mismo
// endpoint cambia de una a otra según cuántos resultados haya.
func listaSC[T any](raw json.RawMessage, etiqueta string) ([]T, error) {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || string(b) == "null" || string(b) == `""` {
		return nil, nil
	}
	switch b[0] {
	case '[':
		var out []T
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return out, nil
	case '{':
		var envoltorio map[string]json.RawMessage
		if err := json.Unmarshal(b, &envoltorio); err != nil {
			return nil, err
		}
		if len(envoltorio) == 0 {
			return nil, nil
		}
		if hijo, ok := envoltorio[etiqueta]; ok {
			return listaSC[T](hijo, etiqueta)
		}
		var uno T
		if err := json.Unmarshal(b, &uno); err != nil {
			return nil, err
		}
		return []T{uno}, nil
	}
	return nil, fmt.Errorf("se esperaba una lista o un objeto y llegó %s", recortarRunes(string(b), 40))
}

func (a *Adaptador) llamar(ctx context.Context, metodo string, params map[string]string, cuerpo []byte, out any) error {
	consulta := conectores.FirmarFalabella(params, a.apiKey)

	var body io.Reader
	if cuerpo != nil {
		body = strings.NewReader(string(cuerpo))
	}
	req, err := http.NewRequestWithContext(ctx, metodo, a.base+"?"+consulta, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "text/xml")
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		return &channel.Error{Kind: channel.Falabella, Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &channel.Error{
			Kind: channel.Falabella, StatusCode: resp.StatusCode,
			Message: recortarRunes(string(datos), 300),
		}
	}

	// Seller Center devuelve 200 con ErrorResponse en el cuerpo: sin esta
	// comprobación, un fallo de negocio pasaría por éxito.
	var errResp struct {
		ErrorResponse *struct {
			Head struct {
				ErrorCode    numOTexto `json:"ErrorCode"`
				ErrorMessage string    `json:"ErrorMessage"`
			} `json:"Head"`
		} `json:"ErrorResponse"`
	}
	if err := json.Unmarshal(datos, &errResp); err == nil && errResp.ErrorResponse != nil {
		return &channel.Error{
			Kind:    channel.Falabella,
			Code:    string(errResp.ErrorResponse.Head.ErrorCode),
			Message: errResp.ErrorResponse.Head.ErrorMessage,
		}
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(datos, out)
}

// resultadoDe traduce el resultado del feed a la respuesta por referencia: un
// SKU rechazado dentro del lote no invalida los otros 49.
func resultadoDe(ref channel.ExternalRef, feed Feed, err error) channel.OpResult {
	r := channel.OpResult{Ref: ref, OK: err == nil, Error: err, FeedID: feed.ID}
	if err != nil {
		return r
	}
	if motivo := feed.Rechazo(ref.SKU); motivo != "" {
		r.OK = false
		r.Error = &channel.Error{Kind: channel.Falabella, Code: "feed", Message: motivo}
	}
	return r
}

func recortarRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
