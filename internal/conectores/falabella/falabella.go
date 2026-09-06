// Package falabella implementa el canal Falabella Seller Center.
//
// Es el más estricto de los cuatro y por eso va el último: firma HMAC en cada
// llamada, exige EAN y peso para calcular el envío, y sus escrituras son
// ASÍNCRONAS — devuelven un FeedID y el resultado se consulta después. Es el
// único canal con AsyncFeeds, así que estrena esa rama del contrato.
package falabella

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
)

const endpoint = "https://sellercenter-api.falabella.com/"

func init() {
	channel.Register(channel.Falabella, func(cfg channel.Config) (channel.Adapter, error) {
		userID := cfg.Credentials["user_id"]
		apiKey := cfg.Credentials["api_key"]
		if userID == "" || apiKey == "" {
			return nil, fmt.Errorf("faltan el UserID y la API Key del Seller Center")
		}
		return &Adaptador{
			userID: userID, apiKey: apiKey,
			cli: &http.Client{Timeout: 40 * time.Second},
		}, nil
	})
}

type Adaptador struct {
	userID string
	apiKey string
	cli    *http.Client
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
	XMLName  xml.Name       `xml:"Request"`
	Producto []productoXML  `xml:"Product"`
}

type productoXML struct {
	SellerSku       string `xml:"SellerSku"`
	Name            string `xml:"Name,omitempty"`
	Description     string `xml:"Description,omitempty"`
	Brand           string `xml:"Brand,omitempty"`
	PrimaryCategory string `xml:"PrimaryCategory,omitempty"`
	Price           string `xml:"Price,omitempty"`
	SalePrice       string `xml:"SalePrice,omitempty"`
	Quantity        string `xml:"Quantity,omitempty"`
	ProductId       string `xml:"ProductId,omitempty"` // el EAN
	PackageWeight   string `xml:"PackageWeight,omitempty"`
	Status          string `xml:"Status,omitempty"`
	Images          *imgs  `xml:"Images,omitempty"`
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

	if existe, err := a.existeSKU(ctx, v.SKU); err != nil {
		return channel.PublishResult{}, err
	} else if existe {
		ref := channel.ExternalRef{ListingID: v.SKU, VariantID: v.SKU, SKU: v.SKU}
		return channel.PublishResult{
			Ref: ref, Adopted: true,
			Warnings:    []string{"ya existía en el Seller Center con el mismo SKU: se adoptó"},
			VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
		}, nil
	}
	if req.DryRun {
		return channel.PublishResult{Ref: channel.ExternalRef{SKU: v.SKU}}, nil
	}

	prod := productoXML{
		SellerSku:       v.SKU,
		Name:            recortarRunes(p.Title, 150),
		Description:     p.Description,
		Brand:           p.Brand,
		PrimaryCategory: p.CategoryID,
		Price:           strconv.FormatFloat(v.RegularPrice, 'f', 2, 64),
		Quantity:        strconv.Itoa(v.Quantity),
		ProductId:       v.Barcode,
		PackageWeight:   strconv.FormatFloat(p.Weight, 'f', 3, 64),
		// Inactivo al crear, como en los otros tres canales.
		Status: "inactive",
	}
	if len(p.Images) > 0 {
		var urls []string
		for _, i := range p.Images {
			urls = append(urls, i.URL)
		}
		prod.Images = &imgs{Image: urls}
	}

	feedID, err := a.enviarProductos(ctx, "ProductCreate", []productoXML{prod})
	if err != nil {
		return channel.PublishResult{}, err
	}

	// El SKU ES la referencia en Falabella: no hay un id de publicación
	// separado, y eso simplifica todo lo demás.
	ref := channel.ExternalRef{ListingID: v.SKU, VariantID: v.SKU, SKU: v.SKU}
	return channel.PublishResult{
		Ref: ref, VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
		Warnings: []string{"escritura asíncrona: el resultado se confirma en el feed " + feedID},
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	p := req.Product
	prod := productoXML{
		SellerSku:   req.Ref.SKU,
		Name:        recortarRunes(p.Title, 150),
		Description: p.Description,
	}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	feedID, err := a.enviarProductos(ctx, "ProductUpdate", []productoXML{prod})
	if err != nil {
		return channel.UpdateResult{}, err
	}
	return channel.UpdateResult{Ref: req.Ref, Warnings: []string{"feed " + feedID}}, nil
}

// UpdatePrice aprovecha el lote: Falabella acepta hasta 50 por feed, así que
// mandar uno a uno desperdiciaría llamadas de una API con cupo.
func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	var prods []productoXML
	for _, u := range ups {
		prod := productoXML{
			SellerSku: u.Ref.SKU,
			Price:     strconv.FormatFloat(u.RegularPrice, 'f', 2, 64),
		}
		if u.SalePrice > 0 {
			prod.SalePrice = strconv.FormatFloat(u.SalePrice, 'f', 2, 64)
		}
		prods = append(prods, prod)
	}
	feedID, err := a.enviarProductos(ctx, "ProductUpdate", prods)
	return resultados(ups, feedID, err), nil
}

func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	var prods []productoXML
	for _, u := range ups {
		prods = append(prods, productoXML{
			SellerSku: u.Ref.SKU, Quantity: strconv.Itoa(u.Quantity),
		})
	}
	feedID, err := a.enviarProductos(ctx, "ProductUpdate", prods)
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		out = append(out, channel.OpResult{
			Ref: u.Ref, OK: err == nil, Error: err, FeedID: feedID,
		})
	}
	return out, nil
}

func (a *Adaptador) Pause(ctx context.Context, ref channel.ExternalRef) error {
	_, err := a.enviarProductos(ctx, "ProductUpdate",
		[]productoXML{{SellerSku: ref.SKU, Status: "inactive"}})
	return err
}

func (a *Adaptador) Resume(ctx context.Context, ref channel.ExternalRef) error {
	_, err := a.enviarProductos(ctx, "ProductUpdate",
		[]productoXML{{SellerSku: ref.SKU, Status: "active"}})
	return err
}

func (a *Adaptador) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	var out []channel.ListingStatus
	for _, ref := range refs {
		p := conectores.ParamsFalabella("GetProducts", a.userID)
		p["SkuSellerList"] = `["` + ref.SKU + `"]`

		var resp struct {
			SuccessResponse struct {
				Body struct {
					Products []struct {
						SellerSku string `json:"SellerSku"`
						Status    string `json:"Status"`
						Price     string `json:"Price"`
						Quantity  string `json:"Quantity"`
					} `json:"Products"`
				} `json:"Body"`
			} `json:"SuccessResponse"`
		}
		if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
			continue
		}
		for _, pr := range resp.SuccessResponse.Body.Products {
			precio, _ := strconv.ParseFloat(pr.Price, 64)
			cant, _ := strconv.Atoi(pr.Quantity)
			out = append(out, channel.ListingStatus{
				Ref: ref, Status: pr.Status, Price: precio, Quantity: cant,
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

	var resp struct {
		SuccessResponse struct {
			Body struct {
				Products []struct {
					SellerSku string `json:"SellerSku"`
					Name      string `json:"Name"`
					Status    string `json:"Status"`
					Price     string `json:"Price"`
					Quantity  string `json:"Quantity"`
				} `json:"Products"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return channel.RemotePage{}, err
	}

	var items []channel.RemoteListing
	for _, pr := range resp.SuccessResponse.Body.Products {
		precio, _ := strconv.ParseFloat(pr.Price, 64)
		cant, _ := strconv.Atoi(pr.Quantity)
		items = append(items, channel.RemoteListing{
			Ref:   channel.ExternalRef{ListingID: pr.SellerSku, VariantID: pr.SellerSku, SKU: pr.SellerSku},
			Title: pr.Name, Status: pr.Status, Price: precio, Quantity: cant,
		})
	}
	return channel.RemotePage{
		Items: items, Next: channel.Cursor{Page: cur.Page + 1, Size: limite},
		Done:  len(items) < limite,
	}, nil
}

func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	p := conectores.ParamsFalabella("GetOrders", a.userID)
	p["Limit"] = "100"
	if !desde.IsZero() {
		p["CreatedAfter"] = desde.UTC().Format("2006-01-02T15:04:05-0700")
	}

	var resp struct {
		SuccessResponse struct {
			Body struct {
				Orders []struct {
					OrderId        json.Number `json:"OrderId"`
					OrderNumber    json.Number `json:"OrderNumber"`
					CreatedAt      string      `json:"CreatedAt"`
					Price          string      `json:"Price"`
					ShippingFee    string      `json:"ShippingFeeTotal"`
					CustomerFirst  string      `json:"CustomerFirstName"`
					CustomerLast   string      `json:"CustomerLastName"`
					AddressShipping struct {
						Address1 string `json:"Address1"`
						City     string `json:"City"`
						Ward     string `json:"Ward"`
						Region   string `json:"Region"`
						PostCode string `json:"PostCode"`
						Country  string `json:"Country"`
						Phone    string `json:"Phone"`
					} `json:"AddressShipping"`
				} `json:"Orders"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return channel.OrderPage{}, err
	}

	var out []channel.Order
	for _, o := range resp.SuccessResponse.Body.Orders {
		total, _ := strconv.ParseFloat(o.Price, 64)
		envio, _ := strconv.ParseFloat(o.ShippingFee, 64)
		fecha, _ := time.Parse("2006-01-02 15:04:05", o.CreatedAt)

		ord := channel.Order{
			ExternalID: o.OrderId.String(), Number: o.OrderNumber.String(),
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
		// Las líneas van en otra llamada: GetOrderItems por pedido.
		if lineas, err := a.lineasDe(ctx, o.OrderId.String()); err == nil {
			ord.Lines = lineas
		}
		out = append(out, ord)
	}
	return channel.OrderPage{Orders: out, Done: len(out) < 100}, nil
}

func (a *Adaptador) lineasDe(ctx context.Context, orderID string) ([]channel.OrderLine, error) {
	p := conectores.ParamsFalabella("GetOrderItems", a.userID)
	p["OrderId"] = orderID

	var resp struct {
		SuccessResponse struct {
			Body struct {
				OrderItems []struct {
					OrderItemId json.Number `json:"OrderItemId"`
					Sku         string      `json:"Sku"`
					Name        string      `json:"Name"`
					ItemPrice   string      `json:"ItemPrice"`
				} `json:"OrderItems"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return nil, err
	}

	var out []channel.OrderLine
	for _, li := range resp.SuccessResponse.Body.OrderItems {
		precio, _ := strconv.ParseFloat(li.ItemPrice, 64)
		// Falabella entrega una línea por unidad, no una línea con cantidad.
		out = append(out, channel.OrderLine{
			ExternalID: li.OrderItemId.String(), SKU: li.Sku, Title: li.Name,
			Quantity: 1, UnitPrice: precio, TotalPrice: precio,
		})
	}
	return out, nil
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

// enviarProductos manda un feed y devuelve su identificador. La escritura es
// asíncrona: que la llamada devuelva 200 solo significa que el feed se aceptó,
// no que los productos se hayan creado.
func (a *Adaptador) enviarProductos(ctx context.Context, accion string, prods []productoXML) (string, error) {
	if len(prods) == 0 {
		return "", nil
	}
	cuerpo, err := xml.Marshal(sobreProductos{Producto: prods})
	if err != nil {
		return "", err
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
		return "", err
	}
	return resp.SuccessResponse.Body.FeedID, nil
}

// EstadoFeed consulta cómo terminó una escritura asíncrona.
func (a *Adaptador) EstadoFeed(ctx context.Context, feedID string) (estado string, errores []string, err error) {
	p := conectores.ParamsFalabella("FeedStatus", a.userID)
	p["FeedID"] = feedID

	var resp struct {
		SuccessResponse struct {
			Body struct {
				FeedDetail struct {
					Status         string `json:"Status"`
					FeedErrors     struct {
						Error []struct {
							Message   string `json:"Message"`
							SellerSku string `json:"SellerSku"`
						} `json:"Error"`
					} `json:"FeedErrors"`
				} `json:"FeedDetail"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return "", nil, err
	}
	d := resp.SuccessResponse.Body.FeedDetail
	for _, e := range d.FeedErrors.Error {
		errores = append(errores, e.SellerSku+": "+e.Message)
	}
	return d.Status, errores, nil
}

func (a *Adaptador) existeSKU(ctx context.Context, sku string) (bool, error) {
	p := conectores.ParamsFalabella("GetProducts", a.userID)
	p["SkuSellerList"] = `["` + sku + `"]`

	var resp struct {
		SuccessResponse struct {
			Body struct {
				Products []struct {
					SellerSku string `json:"SellerSku"`
				} `json:"Products"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return false, err
	}
	for _, pr := range resp.SuccessResponse.Body.Products {
		if strings.EqualFold(strings.TrimSpace(pr.SellerSku), strings.TrimSpace(sku)) {
			return true, nil
		}
	}
	return false, nil
}

func (a *Adaptador) llamar(ctx context.Context, metodo string, params map[string]string, cuerpo []byte, out any) error {
	consulta := conectores.FirmarFalabella(params, a.apiKey)

	var body io.Reader
	if cuerpo != nil {
		body = strings.NewReader(string(cuerpo))
	}
	req, err := http.NewRequestWithContext(ctx, metodo, endpoint+"?"+consulta, body)
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
				ErrorCode    json.Number `json:"ErrorCode"`
				ErrorMessage string      `json:"ErrorMessage"`
			} `json:"Head"`
		} `json:"ErrorResponse"`
	}
	if err := json.Unmarshal(datos, &errResp); err == nil && errResp.ErrorResponse != nil {
		return &channel.Error{
			Kind:    channel.Falabella,
			Code:    errResp.ErrorResponse.Head.ErrorCode.String(),
			Message: errResp.ErrorResponse.Head.ErrorMessage,
		}
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(datos, out)
}

func resultados(ups []channel.PriceUpdate, feedID string, err error) []channel.OpResult {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		out = append(out, channel.OpResult{
			Ref: u.Ref, OK: err == nil, Error: err, FeedID: feedID,
		})
	}
	return out
}

func recortarRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n])
}
