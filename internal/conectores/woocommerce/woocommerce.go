// Package woocommerce implementa el canal WooCommerce sobre su API REST v3.
//
// Es una tienda propia, no un marketplace: no hay categorías obligatorias ni
// atributos por categoría, y las claves no caducan. Lo único peculiar es que
// los precios viajan como cadena, no como número.
package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
)

func init() {
	channel.Register(channel.WooCommerce, func(cfg channel.Config) (channel.Adapter, error) {
		base := strings.TrimSuffix(cfg.Credentials["url"], "/")
		ck := cfg.Credentials["consumer_key"]
		cs := cfg.Credentials["consumer_secret"]
		if base == "" || ck == "" || cs == "" {
			return nil, fmt.Errorf("faltan la URL de la tienda y las claves consumer key/secret")
		}
		return &Adaptador{
			base: base, ck: ck, cs: cs,
			cli: &http.Client{Timeout: 30 * time.Second},
		}, nil
	})
}

type Adaptador struct {
	base string
	ck   string
	cs   string
	cli  *http.Client
}

func (a *Adaptador) Kind() channel.Kind { return channel.WooCommerce }

func (a *Adaptador) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		NativeCompareAtPrice: true, // regular_price + sale_price
		ScheduledOffers:      true, // date_on_sale_from / _to
		BulkPriceUpdate:      true, // products/batch
		BulkStockUpdate:      true,
		MaxBatchSize:         100,
		AsyncFeeds:           false,
		Variants:             channel.VariantesEnPublicacion,
		RequiresOAuthRefresh: false,
		MaxTitleLength:       255,
		RequiresDescription:  false,
		RequiresCategoryMapping: false,
	}
}

func (a *Adaptador) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	p := req.Product
	if len(p.Variants) == 0 {
		return channel.PublishResult{}, fmt.Errorf("el producto %s no trae variantes", p.SKU)
	}
	v := p.Variants[0]

	if ref, err := a.buscarPorSKU(ctx, v.SKU); err != nil {
		return channel.PublishResult{}, err
	} else if ref != nil {
		return channel.PublishResult{
			Ref: *ref, Adopted: true,
			Warnings:    []string{"ya existía en la tienda con el mismo SKU: se adoptó la publicación"},
			VariantRefs: map[string]channel.ExternalRef{v.SKU: *ref},
		}, nil
	}
	if req.DryRun {
		return channel.PublishResult{Ref: channel.ExternalRef{SKU: v.SKU}}, nil
	}

	cuerpo := map[string]any{
		"name":              p.Title,
		"type":              "simple",
		"status":            "draft", // igual que en Shopify: activar es decisión humana
		"description":       p.Description,
		"short_description": "",
		"sku":               v.SKU,
		// Woo espera el precio como cadena; un número se acepta pero devuelve
		// avisos y redondeos inesperados.
		"regular_price":  strconv.FormatFloat(v.RegularPrice, 'f', 2, 64),
		"manage_stock":   true,
		"stock_quantity": v.Quantity,
		"weight":         strconv.FormatFloat(p.Weight, 'f', 3, 64),
		"images":         imagenesDe(p.Images),
	}
	if p.Brand != "" {
		// Woo no tiene campo de marca en el núcleo: va como atributo visible.
		cuerpo["attributes"] = []map[string]any{{
			"name": "Marca", "visible": true, "options": []string{p.Brand},
		}}
	}

	var resp struct {
		ID          int64  `json:"id"`
		Permalink   string `json:"permalink"`
		SKU         string `json:"sku"`
	}
	if err := a.llamar(ctx, http.MethodPost, "/products", nil, cuerpo, &resp); err != nil {
		return channel.PublishResult{}, err
	}
	ref := channel.ExternalRef{ListingID: fmt.Sprint(resp.ID), VariantID: fmt.Sprint(resp.ID), SKU: v.SKU}
	return channel.PublishResult{
		Ref: ref, VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if req.Ref.ListingID == "" {
		return channel.UpdateResult{}, fmt.Errorf("falta el identificador de la publicación")
	}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	cuerpo := map[string]any{
		"name": req.Product.Title, "description": req.Product.Description,
	}
	err := a.llamar(ctx, http.MethodPut, "/products/"+req.Ref.ListingID, nil, cuerpo, nil)
	return channel.UpdateResult{Ref: req.Ref}, err
}

func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		cuerpo := map[string]any{
			"regular_price": strconv.FormatFloat(u.RegularPrice, 'f', 2, 64),
		}
		// El precio de oferta con fechas es nativo: no hacen falta los dos
		// trabajos (aplicar y revertir) que necesitan Shopify y MercadoLibre.
		if u.SalePrice > 0 {
			cuerpo["sale_price"] = strconv.FormatFloat(u.SalePrice, 'f', 2, 64)
			if u.StartsAt != nil {
				cuerpo["date_on_sale_from"] = u.StartsAt.Format("2006-01-02T15:04:05")
			}
			if u.EndsAt != nil {
				cuerpo["date_on_sale_to"] = u.EndsAt.Format("2006-01-02T15:04:05")
			}
		} else {
			cuerpo["sale_price"] = ""
		}
		err := a.llamar(ctx, http.MethodPut, "/products/"+u.Ref.ListingID, nil, cuerpo, nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		cuerpo := map[string]any{"manage_stock": true, "stock_quantity": u.Quantity}
		err := a.llamar(ctx, http.MethodPut, "/products/"+u.Ref.ListingID, nil, cuerpo, nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

func (a *Adaptador) Pause(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/products/"+ref.ListingID, nil,
		map[string]any{"status": "draft"}, nil)
}

func (a *Adaptador) Resume(ctx context.Context, ref channel.ExternalRef) error {
	return a.llamar(ctx, http.MethodPut, "/products/"+ref.ListingID, nil,
		map[string]any{"status": "publish"}, nil)
}

func (a *Adaptador) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	out := make([]channel.ListingStatus, 0, len(refs))
	for _, ref := range refs {
		var resp struct {
			Status        string `json:"status"`
			Permalink     string `json:"permalink"`
			Price         string `json:"price"`
			StockQuantity *int   `json:"stock_quantity"`
		}
		if err := a.llamar(ctx, http.MethodGet, "/products/"+ref.ListingID, nil, nil, &resp); err != nil {
			continue
		}
		st := channel.ListingStatus{Ref: ref, Status: resp.Status, Permalink: resp.Permalink}
		st.Price, _ = strconv.ParseFloat(resp.Price, 64)
		if resp.StockQuantity != nil {
			st.Quantity = *resp.StockQuantity
		}
		out = append(out, st)
	}
	return out, nil
}

func (a *Adaptador) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	pagina := cur.Page
	if pagina <= 0 {
		pagina = 1
	}
	q := url.Values{"per_page": {"100"}, "page": {strconv.Itoa(pagina)}}

	var resp []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		SKU           string `json:"sku"`
		Status        string `json:"status"`
		Price         string `json:"price"`
		StockQuantity *int   `json:"stock_quantity"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/products", q, nil, &resp); err != nil {
		return channel.RemotePage{}, err
	}

	var items []channel.RemoteListing
	for _, p := range resp {
		precio, _ := strconv.ParseFloat(p.Price, 64)
		l := channel.RemoteListing{
			Ref:    channel.ExternalRef{ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(p.ID), SKU: p.SKU},
			Title:  p.Name, Status: p.Status, Price: precio,
		}
		if p.StockQuantity != nil {
			l.Quantity = *p.StockQuantity
		}
		items = append(items, l)
	}
	return channel.RemotePage{
		Items: items,
		Next:  channel.Cursor{Page: pagina + 1},
		Done:  len(resp) < 100,
	}, nil
}

func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	q := url.Values{"per_page": {"100"}, "orderby": {"date"}, "order": {"asc"}}
	if !desde.IsZero() {
		q.Set("after", desde.UTC().Format("2006-01-02T15:04:05"))
	}

	var resp []struct {
		ID          int64  `json:"id"`
		Number      string `json:"number"`
		Status      string `json:"status"`
		DateCreated string `json:"date_created_gmt"`
		Currency    string `json:"currency"`
		Total       string `json:"total"`
		TotalTax    string `json:"total_tax"`
		ShippingTot string `json:"shipping_total"`
		Billing     struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Email     string `json:"email"`
			Phone     string `json:"phone"`
			Address1  string `json:"address_1"`
			Address2  string `json:"address_2"`
			City      string `json:"city"`
			State     string `json:"state"`
			Postcode  string `json:"postcode"`
			Country   string `json:"country"`
		} `json:"billing"`
		LineItems []struct {
			ID       int64  `json:"id"`
			Name     string `json:"name"`
			SKU      string `json:"sku"`
			Quantity int    `json:"quantity"`
			Price    float64 `json:"price"`
			Total    string `json:"total"`
		} `json:"line_items"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/orders", q, nil, &resp); err != nil {
		return channel.OrderPage{}, err
	}

	var out []channel.Order
	for _, o := range resp {
		total, _ := strconv.ParseFloat(o.Total, 64)
		imp, _ := strconv.ParseFloat(o.TotalTax, 64)
		envio, _ := strconv.ParseFloat(o.ShippingTot, 64)
		// Woo entrega la fecha GMT sin zona; se interpreta como UTC.
		fecha, _ := time.Parse("2006-01-02T15:04:05", o.DateCreated)

		ord := channel.Order{
			ExternalID: fmt.Sprint(o.ID), Number: o.Number, Status: o.Status,
			OrderedAt: fecha, Currency: o.Currency,
			Total: total, Tax: imp, Shipping: envio,
			Buyer: channel.Buyer{
				Name:  strings.TrimSpace(o.Billing.FirstName + " " + o.Billing.LastName),
				Email: o.Billing.Email, Phone: o.Billing.Phone,
				Address: channel.Address{
					Line1: o.Billing.Address1, Line2: o.Billing.Address2,
					City: o.Billing.City, State: o.Billing.State,
					PostalCode: o.Billing.Postcode, Country: o.Billing.Country,
				},
			},
		}
		for _, li := range o.LineItems {
			lineaTotal, _ := strconv.ParseFloat(li.Total, 64)
			ord.Lines = append(ord.Lines, channel.OrderLine{
				ExternalID: fmt.Sprint(li.ID), SKU: li.SKU, Title: li.Name,
				Quantity: float64(li.Quantity), UnitPrice: li.Price, TotalPrice: lineaTotal,
			})
		}
		out = append(out, ord)
	}
	return channel.OrderPage{Orders: out, Done: len(resp) < 100}, nil
}

func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	// Woo no tiene fulfillment nativo: marcar completado es lo que hace la
	// tienda. El número de guía va como nota del pedido.
	return a.llamar(ctx, http.MethodPut, "/orders/"+ref.ListingID, nil,
		map[string]any{"status": "completed"}, nil)
}

// ------------------------------------------------------------- auxiliares

func (a *Adaptador) buscarPorSKU(ctx context.Context, sku string) (*channel.ExternalRef, error) {
	if sku == "" {
		return nil, nil
	}
	var resp []struct {
		ID  int64  `json:"id"`
		SKU string `json:"sku"`
	}
	q := url.Values{"sku": {sku}}
	if err := a.llamar(ctx, http.MethodGet, "/products", q, nil, &resp); err != nil {
		return nil, err
	}
	for _, p := range resp {
		if strings.EqualFold(strings.TrimSpace(p.SKU), strings.TrimSpace(sku)) {
			return &channel.ExternalRef{
				ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(p.ID), SKU: sku,
			}, nil
		}
	}
	return nil, nil
}

func imagenesDe(imgs []channel.Image) []map[string]any {
	out := make([]map[string]any, 0, len(imgs))
	for _, i := range imgs {
		out = append(out, map[string]any{"src": i.URL, "position": i.Position})
	}
	return out
}

func (a *Adaptador) llamar(ctx context.Context, metodo, ruta string, q url.Values, cuerpo any, out any) error {
	if q == nil {
		q = url.Values{}
	}
	// Woo admite las claves por query sobre HTTPS. Es lo que documenta para
	// clientes que no implementan OAuth 1.0a.
	q.Set("consumer_key", a.ck)
	q.Set("consumer_secret", a.cs)

	var body io.Reader
	if cuerpo != nil {
		j, err := json.Marshal(cuerpo)
		if err != nil {
			return err
		}
		body = bytes.NewReader(j)
	}

	req, err := http.NewRequestWithContext(ctx, metodo,
		a.base+"/wp-json/wc/v3"+ruta+"?"+q.Encode(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		return &channel.Error{Kind: channel.WooCommerce, Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &channel.Error{
			Kind: channel.WooCommerce, StatusCode: resp.StatusCode,
			Message: recortar(string(datos), 300),
		}
		if resp.StatusCode == http.StatusNotFound {
			e.Err = channel.ErrNoEncontrado
		}
		return e
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(datos, out)
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
