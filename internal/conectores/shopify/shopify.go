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
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// versionAPI se fija a propósito: Shopify retira versiones cada trimestre y
// una versión flotante rompería la publicación sin avisar.
const versionAPI = "2024-10"

func init() {
	channel.Register(channel.Shopify, func(cfg channel.Config) (channel.Adapter, error) {
		tienda := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(
			cfg.Credentials["tienda"], "https://"), "http://"), "/")
		token := cfg.Credentials["token"]
		if tienda == "" || token == "" {
			return nil, fmt.Errorf("faltan la tienda y el token de Admin API")
		}
		return &Adaptador{
			tienda: tienda, token: token,
			cli: &http.Client{Timeout: 30 * time.Second},
		}, nil
	})
}

type Adaptador struct {
	tienda string
	token  string
	cli    *http.Client
}

func (a *Adaptador) Kind() channel.Kind { return channel.Shopify }

func (a *Adaptador) Capabilities() channel.Capabilities {
	return channel.Capabilities{
		NativeCompareAtPrice: true,  // compare_at_price
		ScheduledOffers:      false, // no hay fechas de promoción nativas
		BulkPriceUpdate:      false, // la REST actualiza variante a variante
		BulkStockUpdate:      false,
		MaxBatchSize:         1,
		AsyncFeeds:           false,
		Variants:             channel.VariantesEnPublicacion,
		RequiresOAuthRefresh: false, // el token de app personalizada no caduca
		MaxTitleLength:       255,
		RequiresDescription:  false, // lo acepta vacío, aunque no conviene
		RequiresCategoryMapping: false,
	}
}

// Publish crea el producto. Si el SKU ya existe en la tienda, lo adopta en
// vez de duplicarlo: publicar dos veces el mismo SKU es el error que más
// cuesta deshacer en una tienda viva.
func (a *Adaptador) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	p := req.Product
	if len(p.Variants) == 0 {
		return channel.PublishResult{}, fmt.Errorf("el producto %s no trae variantes", p.SKU)
	}
	v := p.Variants[0]

	if existente, err := a.buscarPorSKU(ctx, v.SKU); err != nil {
		return channel.PublishResult{}, err
	} else if existente != nil {
		return channel.PublishResult{
			Ref:      existente.ref,
			Adopted:  true,
			Warnings: []string{"ya existía en la tienda con el mismo SKU: se adoptó la publicación"},
			VariantRefs: map[string]channel.ExternalRef{v.SKU: existente.ref},
		}, nil
	}

	if req.DryRun {
		return channel.PublishResult{Ref: channel.ExternalRef{SKU: v.SKU}}, nil
	}

	cuerpo := map[string]any{
		"product": map[string]any{
			"title":     p.Title,
			"body_html": p.Description,
			"vendor":    p.Brand,
			"status":    "draft", // se publica en borrador: activarlo es decisión humana
			"variants": []map[string]any{{
				"sku":                  v.SKU,
				"price":                strconv.FormatFloat(v.RegularPrice, 'f', 2, 64),
				"barcode":              v.Barcode,
				"inventory_management": "shopify",
				"inventory_quantity":   v.Quantity,
				"weight":               p.Weight,
				"weight_unit":          "kg",
			}},
			"images": imagenesDe(p.Images),
		},
	}

	var resp struct {
		Product struct {
			ID       int64  `json:"id"`
			Handle   string `json:"handle"`
			Variants []struct {
				ID  int64  `json:"id"`
				SKU string `json:"sku"`
			} `json:"variants"`
		} `json:"product"`
	}
	if err := a.llamar(ctx, http.MethodPost, "/products.json", cuerpo, &resp); err != nil {
		return channel.PublishResult{}, err
	}

	ref := channel.ExternalRef{ListingID: fmt.Sprint(resp.Product.ID), SKU: v.SKU}
	refs := map[string]channel.ExternalRef{}
	if len(resp.Product.Variants) > 0 {
		ref.VariantID = fmt.Sprint(resp.Product.Variants[0].ID)
		refs[v.SKU] = ref
	}
	return channel.PublishResult{Ref: ref, VariantRefs: refs}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if req.Ref.ListingID == "" {
		return channel.UpdateResult{}, fmt.Errorf("falta el identificador de la publicación")
	}
	p := req.Product
	cuerpo := map[string]any{"product": map[string]any{
		"id": req.Ref.ListingID, "title": p.Title,
		"body_html": p.Description, "vendor": p.Brand,
	}}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	if err := a.llamar(ctx, http.MethodPut, "/products/"+req.Ref.ListingID+".json", cuerpo, nil); err != nil {
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
		cuerpo := map[string]any{"variant": map[string]any{
			"id":    u.Ref.VariantID,
			"price": strconv.FormatFloat(u.RegularPrice, 'f', 2, 64),
		}}
		err := a.llamar(ctx, http.MethodPut, "/variants/"+u.Ref.VariantID+".json", cuerpo, nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

// UpdateStock usa inventory_levels/set, que es como Shopify espera que se
// fije stock absoluto (poner inventory_quantity en la variante está obsoleto).
func (a *Adaptador) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	loc, err := a.ubicacionPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		invID, err := a.inventarioDeVariante(ctx, u.Ref.VariantID)
		if err != nil {
			out = append(out, channel.OpResult{Ref: u.Ref, Error: err})
			continue
		}
		cuerpo := map[string]any{
			"location_id": loc, "inventory_item_id": invID, "available": u.Quantity,
		}
		err = a.llamar(ctx, http.MethodPost, "/inventory_levels/set.json", cuerpo, nil)
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
	for _, ref := range refs {
		var resp struct {
			Product struct {
				Status   string `json:"status"`
				Handle   string `json:"handle"`
				Variants []struct {
					Price             string `json:"price"`
					InventoryQuantity int    `json:"inventory_quantity"`
				} `json:"variants"`
			} `json:"product"`
		}
		if err := a.llamar(ctx, http.MethodGet, "/products/"+ref.ListingID+".json", nil, &resp); err != nil {
			continue
		}
		st := channel.ListingStatus{
			Ref: ref, Status: resp.Product.Status,
			Permalink: "https://" + a.tienda + "/products/" + resp.Product.Handle,
		}
		if len(resp.Product.Variants) > 0 {
			st.Price, _ = strconv.ParseFloat(resp.Product.Variants[0].Price, 64)
			st.Quantity = resp.Product.Variants[0].InventoryQuantity
		}
		out = append(out, st)
	}
	return out, nil
}

func (a *Adaptador) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	limite := cur.Size
	if limite <= 0 || limite > 250 {
		limite = 250
	}
	ruta := "/products.json?limit=" + strconv.Itoa(limite)
	if cur.Token != "" {
		ruta += "&page_info=" + cur.Token
	}
	var resp struct {
		Products []struct {
			ID       int64  `json:"id"`
			Title    string `json:"title"`
			Status   string `json:"status"`
			Variants []struct {
				ID                int64  `json:"id"`
				SKU               string `json:"sku"`
				Price             string `json:"price"`
				InventoryQuantity int    `json:"inventory_quantity"`
			} `json:"variants"`
		} `json:"products"`
	}
	if err := a.llamar(ctx, http.MethodGet, ruta, nil, &resp); err != nil {
		return channel.RemotePage{}, err
	}
	var items []channel.RemoteListing
	for _, p := range resp.Products {
		l := channel.RemoteListing{
			Ref:    channel.ExternalRef{ListingID: fmt.Sprint(p.ID)},
			Title:  p.Title,
			Status: p.Status,
		}
		for _, v := range p.Variants {
			precio, _ := strconv.ParseFloat(v.Price, 64)
			l.Variants = append(l.Variants, channel.RemoteListing{
				Ref:      channel.ExternalRef{ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(v.ID), SKU: v.SKU},
				Price:    precio,
				Quantity: v.InventoryQuantity,
			})
		}
		items = append(items, l)
	}
	return channel.RemotePage{Items: items, Done: len(resp.Products) < limite}, nil
}

// FetchOrders trae los pedidos creados desde una fecha. La Fase 5 los
// convierte en pedidos de venta de Odoo.
func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	ruta := "/orders.json?status=any&limit=100"
	if !desde.IsZero() {
		ruta += "&created_at_min=" + desde.UTC().Format(time.RFC3339)
	}
	var resp struct {
		Orders []struct {
			ID                int64  `json:"id"`
			Name              string `json:"name"`
			FinancialStatus   string `json:"financial_status"`
			CreatedAt         time.Time `json:"created_at"`
			Currency          string `json:"currency"`
			TotalPrice        string `json:"total_price"`
			TotalTax          string `json:"total_tax"`
			Customer          struct {
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
				Email     string `json:"email"`
				Phone     string `json:"phone"`
			} `json:"customer"`
			ShippingAddress struct {
				Address1 string `json:"address1"`
				Address2 string `json:"address2"`
				City     string `json:"city"`
				Province string `json:"province"`
				Zip      string `json:"zip"`
				Country  string `json:"country"`
			} `json:"shipping_address"`
			LineItems []struct {
				ID       int64  `json:"id"`
				SKU      string `json:"sku"`
				Title    string `json:"title"`
				Quantity int    `json:"quantity"`
				Price    string `json:"price"`
			} `json:"line_items"`
		} `json:"orders"`
	}
	crudo, err := a.llamarCrudo(ctx, http.MethodGet, ruta, nil)
	if err != nil {
		return channel.OrderPage{}, err
	}
	if err := json.Unmarshal(crudo, &resp); err != nil {
		return channel.OrderPage{}, fmt.Errorf("pedidos ilegibles: %w", err)
	}

	var out []channel.Order
	for _, o := range resp.Orders {
		total, _ := strconv.ParseFloat(o.TotalPrice, 64)
		imp, _ := strconv.ParseFloat(o.TotalTax, 64)
		ord := channel.Order{
			ExternalID: fmt.Sprint(o.ID), Number: o.Name, Status: o.FinancialStatus,
			OrderedAt: o.CreatedAt, Currency: o.Currency, Total: total, Tax: imp,
			Buyer: channel.Buyer{
				Name:  strings.TrimSpace(o.Customer.FirstName + " " + o.Customer.LastName),
				Email: o.Customer.Email, Phone: o.Customer.Phone,
				Address: channel.Address{
					Line1: o.ShippingAddress.Address1, Line2: o.ShippingAddress.Address2,
					City: o.ShippingAddress.City, State: o.ShippingAddress.Province,
					PostalCode: o.ShippingAddress.Zip, Country: o.ShippingAddress.Country,
				},
			},
		}
		for _, li := range o.LineItems {
			precio, _ := strconv.ParseFloat(li.Price, 64)
			ord.Lines = append(ord.Lines, channel.OrderLine{
				ExternalID: fmt.Sprint(li.ID), SKU: li.SKU, Title: li.Title,
				Quantity: float64(li.Quantity), UnitPrice: precio,
				TotalPrice: precio * float64(li.Quantity),
			})
		}
		out = append(out, ord)
	}
	return channel.OrderPage{Orders: out, Done: len(resp.Orders) < 100}, nil
}

func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	// El fulfillment de Shopify exige el fulfillment_order; se implementa con
	// el resto del despacho en la Fase 5.
	return fmt.Errorf("confirmación de despacho aún no implementada para Shopify")
}

// ------------------------------------------------------------- auxiliares

type encontrado struct{ ref channel.ExternalRef }

func (a *Adaptador) buscarPorSKU(ctx context.Context, sku string) (*encontrado, error) {
	if sku == "" {
		return nil, nil
	}
	// La REST no filtra productos por SKU: se busca sobre variantes con la
	// búsqueda de la tienda, que sí lo indexa.
	var resp struct {
		Products []struct {
			ID       int64 `json:"id"`
			Variants []struct {
				ID  int64  `json:"id"`
				SKU string `json:"sku"`
			} `json:"variants"`
		} `json:"products"`
	}
	ruta := "/products.json?limit=250&fields=id,variants"
	if err := a.llamar(ctx, http.MethodGet, ruta, nil, &resp); err != nil {
		return nil, err
	}
	for _, p := range resp.Products {
		for _, v := range p.Variants {
			if strings.EqualFold(strings.TrimSpace(v.SKU), strings.TrimSpace(sku)) {
				return &encontrado{ref: channel.ExternalRef{
					ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(v.ID), SKU: sku,
				}}, nil
			}
		}
	}
	return nil, nil
}

func (a *Adaptador) ubicacionPrincipal(ctx context.Context) (int64, error) {
	var resp struct {
		Locations []struct {
			ID     int64 `json:"id"`
			Active bool  `json:"active"`
		} `json:"locations"`
	}
	if err := a.llamar(ctx, http.MethodGet, "/locations.json", nil, &resp); err != nil {
		return 0, err
	}
	for _, l := range resp.Locations {
		if l.Active {
			return l.ID, nil
		}
	}
	return 0, fmt.Errorf("la tienda no tiene ninguna ubicación activa")
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

func imagenesDe(imgs []channel.Image) []map[string]any {
	out := make([]map[string]any, 0, len(imgs))
	for _, i := range imgs {
		out = append(out, map[string]any{"src": i.URL, "position": i.Position + 1})
	}
	return out
}

func (a *Adaptador) llamar(ctx context.Context, metodo, ruta string, cuerpo any, out any) error {
	datos, err := a.llamarCrudo(ctx, metodo, ruta, cuerpo)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(datos, out)
}

func (a *Adaptador) llamarCrudo(ctx context.Context, metodo, ruta string, cuerpo any) ([]byte, error) {
	var body io.Reader
	if cuerpo != nil {
		j, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(j)
	}
	url := "https://" + a.tienda + "/admin/api/" + versionAPI + ruta
	req, err := http.NewRequestWithContext(ctx, metodo, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shopify-Access-Token", a.token)
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		return nil, &channel.Error{Kind: channel.Shopify, Message: err.Error(), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
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
		return nil, e
	}
	return datos, nil
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
