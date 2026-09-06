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
	"math"
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
	// NO hay campo Images: el esquema y el ejemplo de ProductCreate no
	// declaran ningún <Images> dentro de <Product> —sus hijos son SellerSku,
	// ParentSku, Name, PrimaryCategory, Description, Brand, ProductId, los
	// atributos de variación, BusinessUnits y ProductData—, así que un bloque
	// colgado ahí se acepta y se ignora y la publicación queda sin fotos para
	// siempre. Las imágenes tienen endpoint propio (Action=Image), ver
	// enviarImagenes. https://developers.falabella.com/v600.0.0/reference/productcreate
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
//
// Los cinco primeros campos son OBLIGATORIOS: la tabla de parámetros de
// ProductCreate marca con "Sí" ConditionType, PackageHeight (entero, cm),
// PackageWidth (entero, cm), PackageLength (entero, cm) y PackageWeight
// (decimal, kg), y el ejemplo oficial los muestra los cinco juntos. Faltando
// cualquiera de las tres aristas, el feed termina con FailedRecords>0 y
// "atributo obligatorio ausente" por cada SKU, así que ninguna alta pasa. El
// orden de los campos es el del ejemplo oficial.
// https://developers.falabella.com/v600.0.0/reference/productcreate
type datosXML struct {
	ConditionType string        `xml:"ConditionType,omitempty"`
	PackageHeight string        `xml:"PackageHeight,omitempty"`
	PackageWidth  string        `xml:"PackageWidth,omitempty"`
	PackageLength string        `xml:"PackageLength,omitempty"`
	PackageWeight string        `xml:"PackageWeight,omitempty"`
	Atributos     []atributoXML `xml:",omitempty"`
}

// atributoXML serializa un par nombre/valor como <talla>M</talla>: el nombre
// del elemento sale del dato, por eso lleva XMLName en vez de una etiqueta.
type atributoXML struct {
	XMLName xml.Name
	Valor   string `xml:",chardata"`
}

// sobreImagenes es el cuerpo de Action=Image, que es donde de verdad se cargan
// las fotos: <Request><ProductImage><SellerSku/><Images><Image/></Images>.
// https://developers.falabella.com/v500/reference/image
type sobreImagenes struct {
	XMLName xml.Name         `xml:"Request"`
	Imagen  []imagenProducto `xml:"ProductImage"`
}

type imagenProducto struct {
	SellerSku string `xml:"SellerSku"`
	Images    imgs   `xml:"Images"`
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
	// PackageHeight, PackageWidth y PackageLength son obligatorios dentro de
	// <ProductData>, así que sin las tres medidas el feed se rechaza entero.
	// Se comprueba aquí, junto al EAN y al peso, para no gastar una escritura
	// asíncrona que se sabe que va a fallar y que además solo se descubriría
	// al consultar FeedStatus.
	// https://developers.falabella.com/v600.0.0/reference/productcreate
	if p.LengthCm <= 0 || p.WidthCm <= 0 || p.HeightCm <= 0 {
		return channel.PublishResult{}, fmt.Errorf(
			"Falabella exige las medidas del paquete (largo, ancho y alto en cm) y %s tiene %gx%gx%g",
			v.SKU, p.LengthCm, p.WidthCm, p.HeightCm)
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
	avisos = append(avisos, feed.Aviso())
	pendientes := sinVeredicto(nil, feed)

	// Segunda escritura: las fotos no viajan en el feed de producto.
	avisoImgs, feedImgs, err := a.imagenesDe(ctx, v.SKU, p.Images)
	if err != nil {
		return channel.PublishResult{}, err
	}
	if avisoImgs != "" {
		avisos = append(avisos, avisoImgs)
	}
	return channel.PublishResult{
		Ref: ref, Adopted: existe,
		VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
		Warnings:    avisos,
		// Lo que Seller Center todavía no ha resuelto no se puede dar por
		// publicado: quien llama tiene que preguntar por estos feeds antes de
		// sellar el hash.
		FeedsPendientes: sinVeredicto(pendientes, feedImgs),
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
	avisos := []string{feed.Aviso()}
	pendientes := sinVeredicto(nil, feed)

	// Las imágenes van en su propia escritura, igual que en Publish.
	avisoImgs, feedImgs, err := a.imagenesDe(ctx, req.Ref.SKU, req.Product.Images)
	if err != nil {
		return channel.UpdateResult{}, err
	}
	if avisoImgs != "" {
		avisos = append(avisos, avisoImgs)
	}
	return channel.UpdateResult{
		Ref: req.Ref, Warnings: avisos,
		FeedsPendientes: sinVeredicto(pendientes, feedImgs),
	}, nil
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
	items, err := a.itemsDe(ctx, orderID)
	if err != nil {
		return nil, err
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

// itemsDe trae las líneas del pedido tal como las entrega el canal.
//
// Está separada de lineasDe porque tiene dos consumidores con necesidades
// distintas: FetchOrders solo quiere SKU y precio, mientras que AckOrder
// necesita el OrderItemId, el PackageId y el estado. Esos tres no viajan en la
// cabecera del pedido ni los transporta el contrato de channel: GetOrderItems
// es el único sitio donde existen.
func (a *Adaptador) itemsDe(ctx context.Context, orderID string) ([]itemResp, error) {
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
	return items, nil
}

type itemResp struct {
	OrderItemId numOTexto `json:"OrderItemId"`
	Sku         string    `json:"Sku"`
	Name        string    `json:"Name"`
	ItemPrice   numOTexto `json:"ItemPrice"`
	// Los tres siguientes solo los usa el despacho. PackageId llega ya hecho
	// en el flujo normal, y ShippingType distingue lo que despacha el vendedor
	// de lo que despacha Falabella con su propio inventario.
	Status       string    `json:"Status"`
	ShippingType string    `json:"ShippingType"`
	PackageId    numOTexto `json:"PackageId"`
}

// AckOrder confirma al Seller Center que el pedido salió.
//
// Falabella NO tiene una acción "shipped": SetStatusToShipped no existe en esta
// API y respondía E008. El flujo vigente (v600) es GetOrderItems → GetDocument
// (la etiqueta) → SetStatusToReadyToShip, y lo que identifica el bulto es el
// PackageId, no la publicación. Por eso aquí ref.ListingID es el ID DEL PEDIDO
// en el canal y no el de un listing: los OrderItemId y el PackageId se releen
// con GetOrderItems, que es el único sitio donde están —el contrato de AckOrder
// no transporta las líneas del pedido—.
//
// La guía y la transportadora de Fulfillment se ignoran A PROPÓSITO: la guía la
// genera Falabella (el TrackingCode que devuelve GetOrderItems) y el vendedor no
// puede fijarla; mandarla se rechaza con E091, "you are not allowed to set the
// shipment provider and tracking number".
func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, _ channel.Fulfillment) error {
	pedido := strings.TrimSpace(ref.ListingID)
	if pedido == "" {
		return &channel.Error{
			Kind: channel.Falabella, Code: "sin_pedido",
			Message: "el despacho de Falabella necesita el id del pedido en ExternalRef.ListingID",
		}
	}

	items, err := a.itemsDe(ctx, pedido)
	if err != nil {
		return err
	}

	var ids []string
	var paquete string
	var yaDespachados, deFalabella int
	for _, it := range items {
		if despachaFalabella(it.ShippingType) {
			deFalabella++
			continue
		}
		switch normalizarEstado(it.Status) {
		case "pending", "ready_to_ship":
			ids = append(ids, string(it.OrderItemId))
			if paquete == "" {
				paquete = strings.TrimSpace(string(it.PackageId))
			}
		case "shipped", "delivered":
			yaDespachados++
		}
	}

	if len(ids) == 0 {
		// Que no quede nada que confirmar no es un fallo: o el pedido ya salió
		// —y el reintento del mismo trabajo tiene que terminar bien en vez de
		// repetir la acción— o es un pedido FBF que despacha Falabella con su
		// propio inventario, donde la acción está prohibida.
		if yaDespachados > 0 || deFalabella > 0 {
			return nil
		}
		// Cancelado, devuelto o en un estado que Seller Center no acepta. Se
		// reporta en vez de callarse: significa que se despachó algo que el
		// canal no da por vendido, y E073 lo rechazaría igual.
		return &channel.Error{
			Kind: channel.Falabella, Code: "sin_lineas_despachables",
			Message: "el pedido " + pedido + " no tiene líneas en estado pending o ready_to_ship",
		}
	}

	if paquete == "" {
		if paquete, err = a.empaquetar(ctx, ids); err != nil {
			return err
		}
	}

	p := conectores.ParamsFalabella("SetStatusToReadyToShip", a.userID)
	p["OrderItemIds"] = listaDeIDs(ids)
	p["PackageId"] = paquete
	return a.llamar(ctx, http.MethodPost, p, nil, nil)
}

// empaquetar consigue el PackageId cuando GetOrderItems todavía no lo trae.
//
// En el flujo vigente el paquete llega ya hecho, así que esto es la excepción y
// no el camino normal: sin PackageId, SetStatusToReadyToShip no se puede
// llamar, y la única acción documentada que lo crea es
// SetStatusToPackedByMarketplace —que Falabella rotula "endpoint deprecado, aún
// operativo" sin publicar sustituto—.
func (a *Adaptador) empaquetar(ctx context.Context, ids []string) (string, error) {
	p := conectores.ParamsFalabella("SetStatusToPackedByMarketplace", a.userID)
	p["OrderItemIds"] = listaDeIDs(ids)
	// dropship es "lo despacha el vendedor". Los otros dos modos que acepta la
	// API (pickup y sendtowarehouse) son cross-docking contra las bodegas de
	// Falabella y no se usan desde Integra.
	p["DeliveryType"] = "dropship"

	var resp struct {
		SuccessResponse struct {
			Body struct {
				OrderItems json.RawMessage `json:"OrderItems"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodPost, p, nil, &resp); err != nil {
		return "", err
	}
	items, err := listaSC[itemResp](resp.SuccessResponse.Body.OrderItems, "OrderItem")
	if err != nil {
		return "", &channel.Error{
			Kind:    channel.Falabella,
			Message: "SetStatusToPackedByMarketplace ilegible: " + err.Error(), Err: err,
		}
	}
	for _, it := range items {
		if id := strings.TrimSpace(string(it.PackageId)); id != "" {
			return id, nil
		}
	}
	return "", &channel.Error{
		Kind: channel.Falabella, Code: "sin_paquete",
		Message: "SetStatusToPackedByMarketplace no devolvió PackageId y sin él " +
			"no se puede marcar el pedido listo para envío",
	}
}

// listaDeIDs arma el formato que exige Seller Center para las listas de
// identificadores: [1,2,3], sin espacios ni comillas.
func listaDeIDs(ids []string) string { return "[" + strings.Join(ids, ",") + "]" }

// despachaFalabella distingue los pedidos que el marketplace despacha con su
// propio inventario (FBF), donde SetStatusToReadyToShip está PROHIBIDO. El
// nombre del modo cambia según el endpoint —unas respuestas lo llaman
// "Own Warehouse" y la restricción de la acción habla de un ShipmentType
// "Fulfillment"—, así que se aceptan los dos. "Dropshipping" manda sobre
// cualquier otra coincidencia: la propia documentación llama a ese modo
// "Fulfillment by Seller", y confundirlos dejaría sin confirmar todos los
// pedidos que despacha el vendedor, que son la mayoría.
func despachaFalabella(tipo string) bool {
	t := strings.ToLower(strings.TrimSpace(tipo))
	if t == "" || strings.Contains(t, "dropship") {
		return false
	}
	return strings.Contains(t, "own warehouse") || strings.Contains(t, "fulfillment")
}

// normalizarEstado unifica cómo nombra Seller Center los estados: la misma API
// devuelve "ready_to_ship" en unas respuestas y "Ready To Ship" en otras.
func normalizarEstado(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
}

// ------------------------------------------------------------- auxiliares

// ficha arma la parte de contenido del producto: lo que no depende de la
// unidad de negocio. Precio, stock y estado los añade quien llama. Las
// imágenes tampoco van aquí: viajan aparte con Action=Image (ver
// enviarImagenes).
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
	// Las tres aristas del paquete, en enteros de centímetros, como pide la
	// tabla de ProductData. Se redondea HACIA ARRIBA: Integra las guarda con
	// dos decimales y declarar 12 cm donde hay 12,4 es declarar un paquete más
	// pequeño que el real, lo que el canal cobra o rechaza en la bodega.
	prod.Datos.PackageHeight = centimetros(p.HeightCm)
	prod.Datos.PackageWidth = centimetros(p.WidthCm)
	prod.Datos.PackageLength = centimetros(p.LengthCm)
	return prod
}

// centimetros pasa una medida a la cadena entera de centímetros que espera
// Seller Center. Cero devuelve vacío para que omitempty no mande un elemento
// en blanco, que Falabella cuenta como atributo obligatorio ausente igual que
// si faltara.
func centimetros(cm float64) string {
	if cm <= 0 {
		return ""
	}
	return strconv.FormatFloat(math.Ceil(cm), 'f', 0, 64)
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
	return a.enviarFeed(ctx, accion, cuerpo)
}

// maxImagenes es el tope que admite Action=Image por producto. Recortar aquí
// es mejor que mandar nueve: el endpoint rechazaría la llamada entera y el
// producto se quedaría sin ninguna foto.
// https://developers.falabella.com/v500/reference/image
const maxImagenes = 8

// enviarImagenes carga las fotos con su endpoint propio, Action=Image, que es
// el único sitio donde Falabella las lee. Colgarlas como <Images> dentro de
// <Product> en ProductCreate/ProductUpdate no da error —el bloque no está en
// el esquema y se ignora— pero la publicación se queda sin fotos, no pasa el
// control de calidad del Seller Center y nunca se hace visible; y como el
// motor de diff guarda el hash en cuanto Publish responde, el producto no se
// vuelve a encolar.
//
// Ojo con dos comportamientos documentados del endpoint: la PRIMERA URL queda
// como imagen principal (por eso se respeta el orden que trae el producto) y
// cada llamada DESASOCIA las imágenes anteriores, así que hay que mandar
// siempre la lista completa y no solo las nuevas.
// https://developers.falabella.com/v500/reference/image
func (a *Adaptador) enviarImagenes(ctx context.Context, sku string, imagenes []channel.Image) (Feed, error) {
	urls := make([]string, 0, len(imagenes))
	for _, i := range imagenes {
		if u := strings.TrimSpace(i.URL); u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return Feed{}, nil
	}
	if len(urls) > maxImagenes {
		urls = urls[:maxImagenes]
	}
	cuerpo, err := xml.Marshal(sobreImagenes{Imagen: []imagenProducto{{
		SellerSku: sku, Images: imgs{Image: urls},
	}}})
	if err != nil {
		return Feed{}, err
	}
	return a.enviarFeed(ctx, "Image", cuerpo)
}

// imagenesDe manda las fotos y traduce el resultado a un aviso, o a un error si
// el feed las rechazó. Se devuelve error a propósito: dar la publicación por
// buena guardaría el hash y la dejaría sin fotos para siempre, mientras que
// reintentar es inocuo porque Action=Image reemplaza la lista entera.
func (a *Adaptador) imagenesDe(ctx context.Context, sku string, imagenes []channel.Image) (string, Feed, error) {
	feed, err := a.enviarImagenes(ctx, sku, imagenes)
	if err != nil {
		return "", Feed{}, err
	}
	if feed.ID == "" {
		return "", Feed{}, nil
	}
	if motivo := feed.Rechazo(sku); motivo != "" {
		return "", feed, &channel.Error{
			Kind: channel.Falabella, Code: "feed_imagenes",
			Message: "las imágenes de " + sku + " fueron rechazadas: " + motivo,
		}
	}
	return "imágenes: " + feed.Aviso(), feed, nil
}

// sinVeredicto acumula los feeds que se enviaron y todavía no terminaron.
//
// Es la lista que separa «Seller Center lo aceptó» de «Seller Center lo
// aplicó»: entre lo uno y lo otro pueden pasar minutos, y el sondeo corto de
// enviarFeed solo alcanza a ver los feeds rápidos.
func sinVeredicto(feeds []string, f Feed) []string {
	if f.ID == "" || f.Terminado() {
		return feeds
	}
	return append(feeds, f.ID)
}

// VeredictoDeFeed responde por una escritura asíncrona que quedó sin
// confirmar. Es lo que permite sellar el hash cuando el feed termina bien —o
// dejarlo sin sellar y con el motivo anotado cuando el Seller Center lo
// rechaza minutos después de haberlo aceptado.
func (a *Adaptador) VeredictoDeFeed(ctx context.Context, feedID, sku string) (channel.Veredicto, error) {
	f, err := a.EstadoFeed(ctx, feedID)
	if err != nil {
		return channel.Veredicto{}, err
	}
	// EstadoFeed devuelve el identificador que trae el cuerpo; si viniera
	// vacío, Rechazo() lo nombraría con una cadena en blanco.
	f.ID = feedID
	return channel.Veredicto{
		Terminado: f.Terminado(), Estado: f.Estado, Rechazo: f.Rechazo(sku),
	}, nil
}

// enviarFeed manda un cuerpo XML a una acción de escritura y espera a saber
// cómo terminó. Lo comparten los productos y las imágenes porque las dos
// escrituras son asíncronas y devuelven el feed en el mismo sitio.
func (a *Adaptador) enviarFeed(ctx context.Context, accion string, cuerpo []byte) (Feed, error) {
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
		e := &channel.Error{
			Kind: channel.Falabella, StatusCode: resp.StatusCode,
			Message: recortarRunes(string(datos), 300),
		}
		// Seller Center corta con 429 cuando se pasa el cupo. Sin leer el
		// plazo, la cola reintentaba a los 30 s contra una API que ya había
		// pedido parar, y cada reintento alarga el corte.
		if resp.StatusCode == http.StatusTooManyRequests {
			e.RetryAfter = conectores.EsperaTrasCupo(resp.Header)
		}
		return e
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
	// Un feed que sigue en cola no dice nada todavía: darlo por bueno es lo
	// que sellaba el hash de un precio o de una bajada de stock que el Seller
	// Center podía rechazar diez minutos después.
	r.FeedPendiente = feed.ID != "" && !feed.Terminado()
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
