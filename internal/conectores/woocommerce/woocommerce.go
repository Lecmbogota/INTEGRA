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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
)

// formatoFecha es el ISO 8601 sin zona que espera la API: tanto los filtros de
// fecha como los campos *_gmt se envían siempre en UTC.
const formatoFecha = "2006-01-02T15:04:05"

func init() {
	channel.Register(channel.WooCommerce, func(cfg channel.Config) (channel.Adapter, error) {
		base, err := ValidarURLTienda(cfg.Credentials["url"])
		if err != nil {
			return nil, err
		}
		ck := cfg.Credentials["consumer_key"]
		cs := cfg.Credentials["consumer_secret"]
		if ck == "" || cs == "" {
			return nil, fmt.Errorf("faltan la URL de la tienda y las claves consumer key/secret")
		}
		return &Adaptador{
			base: base, ck: ck, cs: cs,
			cli: &http.Client{Timeout: 30 * time.Second},
		}, nil
	})
}

// ValidarURLTienda normaliza la URL de la tienda y exige HTTPS.
//
// WooCommerce solo admite la clave y el secreto (por cabecera Basic o por
// query) cuando is_ssl() es cierto; sobre HTTP exige OAuth 1.0a de una pata,
// que este adaptador no implementa. Sin esta comprobación, una cuenta dada de
// alta con http:// falla con un 401 opaco en cada llamada y, de paso, el
// consumer secret viaja en claro por el cable y queda escrito en el log de
// acceso del servidor y de cualquier proxy intermedio. Es preferible rechazar
// la cuenta al darla de alta. Se exporta para que la prueba de conexión
// (conectores.probarWoo) pueda aplicar el mismo criterio.
func ValidarURLTienda(bruta string) (string, error) {
	base := strings.TrimSuffix(strings.TrimSpace(bruta), "/")
	if base == "" {
		return "", fmt.Errorf("falta la URL de la tienda")
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("la URL de la tienda no es válida: %w", err)
	}
	if u.Host == "" {
		return "", fmt.Errorf("la URL de la tienda no tiene servidor: %q", base)
	}
	// La excepción local es lo que hace utilizable la tienda de pruebas de
	// docker-compose.woocommerce.yml, que vive en http://localhost:8090 y a la
	// que ponerle un certificado sería más problema que solución. Fuera de la
	// propia máquina se sigue exigiendo https: sobre HTTP la API solo admite
	// OAuth 1.0a y el consumer secret viajaría en claro.
	if !strings.EqualFold(u.Scheme, "https") && !conectores.EsLocal(base) {
		return "", fmt.Errorf("la URL de la tienda debe empezar por https:// (llegó %q): "+
			"sobre HTTP WooCommerce solo admite OAuth 1.0a y el consumer secret viajaría en claro", base)
	}
	return base, nil
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
		res := channel.PublishResult{
			Ref: *ref, Adopted: true,
			Warnings:    []string{"ya existía en la tienda con el mismo SKU: se adoptó la publicación"},
			VariantRefs: map[string]channel.ExternalRef{v.SKU: *ref},
		}
		if req.DryRun {
			return res, nil
		}
		// La adopción tiene que escribir el contenido: el motor guarda el hash
		// de contenido como enviado en cuanto Publish responde bien, así que si
		// aquí no se hace el PUT, un título, una descripción o una foto nuevos
		// se pierden para siempre y nadie vuelve a encolar el trabajo.
		if err := a.llamar(ctx, http.MethodPut, "/products/"+ref.ListingID, nil,
			cuerpoContenido(p), nil); err != nil {
			return channel.PublishResult{}, err
		}
		return res, nil
	}
	if req.DryRun {
		return channel.PublishResult{Ref: channel.ExternalRef{SKU: v.SKU}}, nil
	}

	cuerpo := cuerpoContenido(p)
	cuerpo["type"] = "simple"
	cuerpo["status"] = "draft" // igual que en Shopify: activar es decisión humana
	cuerpo["short_description"] = ""
	cuerpo["sku"] = v.SKU
	// Woo espera el precio como cadena; un número se acepta pero devuelve
	// avisos y redondeos inesperados.
	cuerpo["regular_price"] = strconv.FormatFloat(v.RegularPrice, 'f', 2, 64)
	cuerpo["manage_stock"] = true
	cuerpo["stock_quantity"] = v.Quantity

	var resp struct {
		ID        int64  `json:"id"`
		Permalink string `json:"permalink"`
		SKU       string `json:"sku"`
	}
	if err := a.llamar(ctx, http.MethodPost, "/products", nil, cuerpo, &resp); err != nil {
		return channel.PublishResult{}, err
	}
	ref := channel.ExternalRef{ListingID: fmt.Sprint(resp.ID), VariantID: fmt.Sprint(resp.ID), SKU: v.SKU}
	return channel.PublishResult{
		Ref: ref, Permalink: resp.Permalink,
		VariantRefs: map[string]channel.ExternalRef{v.SKU: ref},
	}, nil
}

func (a *Adaptador) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if req.Ref.ListingID == "" {
		return channel.UpdateResult{}, fmt.Errorf("falta el identificador de la publicación")
	}
	if req.DryRun {
		return channel.UpdateResult{Ref: req.Ref}, nil
	}
	err := a.llamar(ctx, http.MethodPut, "/products/"+req.Ref.ListingID, nil,
		cuerpoContenido(req.Product), nil)
	return channel.UpdateResult{Ref: req.Ref}, err
}

// cuerpoContenido reúne los campos de ficha que Integra posee.
//
// Es el mismo cuerpo al crear, al adoptar y al actualizar: todos entran en el
// hash de contenido del motor, de modo que si uno no viaja, el motor lo da por
// sincronizado y la tienda se queda con el valor viejo indefinidamente.
func cuerpoContenido(p channel.Product) map[string]any {
	c := map[string]any{
		"name":        p.Title,
		"description": p.Description,
		"weight":      strconv.FormatFloat(p.Weight, 'f', 3, 64),
	}
	// "images": [] borra la foto principal y toda la galería; si el producto
	// no trae imágenes se omite la clave para no vaciar las de la tienda.
	if len(p.Images) > 0 {
		c["images"] = imagenesDe(p.Images)
	}
	if p.Brand != "" {
		// Woo no tiene campo de marca en el núcleo: va como atributo visible.
		c["attributes"] = []map[string]any{{
			"name": "Marca", "visible": true, "options": []string{p.Brand},
		}}
	}
	return c
}

func (a *Adaptador) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	out := make([]channel.OpResult, 0, len(ups))
	for _, u := range ups {
		cuerpo := map[string]any{
			"regular_price": strconv.FormatFloat(u.RegularPrice, 'f', 2, 64),
		}
		// El precio de oferta con fechas es nativo: no hacen falta los dos
		// trabajos (aplicar y revertir) que necesitan Shopify y MercadoLibre.
		//
		// La ventana viaja por los campos *_gmt: date_on_sale_from se
		// interpreta en la hora del sitio, así que una promoción de Colombia
		// entraría y saldría cinco horas corrida.
		if u.SalePrice > 0 {
			cuerpo["sale_price"] = strconv.FormatFloat(u.SalePrice, 'f', 2, 64)
			cuerpo["date_on_sale_from_gmt"] = fechaGMT(u.StartsAt)
			cuerpo["date_on_sale_to_gmt"] = fechaGMT(u.EndsAt)
		} else {
			// Se limpia también la ventana: una fecha heredada de la promoción
			// anterior deja is_on_sale() en falso y la tienda seguiría vendiendo
			// al precio normal aunque el precio de oferta sí se hubiera escrito.
			cuerpo["sale_price"] = ""
			cuerpo["date_on_sale_from_gmt"] = ""
			cuerpo["date_on_sale_to_gmt"] = ""
		}
		err := a.llamar(ctx, http.MethodPut, "/products/"+u.Ref.ListingID, nil, cuerpo, nil)
		out = append(out, channel.OpResult{Ref: u.Ref, OK: err == nil, Error: err})
	}
	return out, nil
}

// fechaGMT devuelve la fecha en UTC, o cadena vacía para que Woo la borre.
func fechaGMT(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(formatoFecha)
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
			// Que el producto ya no exista en la tienda es justo lo que se
			// venía a averiguar: se reporta como eliminado. Cualquier otro
			// error se devuelve, porque tragárselo hace pasar una tienda caída
			// por "todo en orden" y el informe sale incompleto sin decirlo.
			if errors.Is(err, channel.ErrNoEncontrado) {
				out = append(out, channel.ListingStatus{Ref: ref, Status: "eliminado"})
				continue
			}
			return out, err
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
	cab, err := a.llamarCab(ctx, http.MethodGet, "/products", q, nil, &resp)
	if err != nil {
		return channel.RemotePage{}, err
	}

	var items []channel.RemoteListing
	for _, p := range resp {
		precio, _ := strconv.ParseFloat(p.Price, 64)
		l := channel.RemoteListing{
			Ref:   channel.ExternalRef{ListingID: fmt.Sprint(p.ID), VariantID: fmt.Sprint(p.ID), SKU: p.SKU},
			Title: p.Name, Status: p.Status, Price: precio,
		}
		if p.StockQuantity != nil {
			l.Quantity = *p.StockQuantity
		}
		items = append(items, l)
	}
	return channel.RemotePage{
		Items: items,
		Next:  channel.Cursor{Page: pagina + 1},
		Done:  esUltimaPagina(cab, pagina, len(resp)),
	}, nil
}

func (a *Adaptador) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	pagina := cur.Page
	if pagina <= 0 {
		pagina = 1
	}
	q := url.Values{
		"per_page": {"100"},
		// Sin la página, el bucle de ingesta pedía cuarenta veces la primera y
		// una tienda con más de cien pedidos en la ventana no pasaba de ahí.
		"page":    {strconv.Itoa(pagina)},
		"orderby": {"date"},
		"order":   {"asc"},
		// Sin dates_are_gmt, WooCommerce compara el filtro contra post_modified,
		// que está en la hora local del sitio: en una tienda en UTC-5 los pedidos
		// de las cinco horas de desfase quedan por debajo de la marca de agua y
		// no se ingieren jamás.
		"dates_are_gmt": {"true"},
	}
	if !desde.IsZero() {
		// modified_after y no after: la marca de agua avanza con la última
		// modificación (ordenes.go usa UpdatedAt cuando el canal la da), así
		// que filtrar por la fecha de creación dejaría fuera pedidos creados
		// antes de esa marca. De paso vuelven los cambios de estado.
		q.Set("modified_after", desde.UTC().Format(formatoFecha))
	}

	var resp []struct {
		ID           int64           `json:"id"`
		Number       string          `json:"number"`
		Status       string          `json:"status"`
		DateCreated  string          `json:"date_created_gmt"`
		DateModified string          `json:"date_modified_gmt"`
		Currency     string          `json:"currency"`
		Total        string          `json:"total"`
		TotalTax     string          `json:"total_tax"`
		ShippingTot  string          `json:"shipping_total"`
		Billing      direccionPedido `json:"billing"`
		// El recurso Order trae DOS objetos de dirección independientes:
		// «billing – Billing address» y «shipping – Shipping address», cada
		// uno con su propia tabla de propiedades
		// (https://woocommerce.github.io/woocommerce-rest-api-docs/#order-properties).
		// Cuando el comprador marca «enviar a una dirección diferente» en el
		// checkout, shipping.* difiere de billing.*: leer solo billing
		// despachaba la guía a la dirección de facturación.
		Shipping  direccionPedido `json:"shipping"`
		LineItems []struct {
			ID       int64   `json:"id"`
			Name     string  `json:"name"`
			SKU      string  `json:"sku"`
			Quantity int     `json:"quantity"`
			Price    float64 `json:"price"`
			Total    string  `json:"total"`
		} `json:"line_items"`
	}
	cab, err := a.llamarCab(ctx, http.MethodGet, "/orders", q, nil, &resp)
	if err != nil {
		return channel.OrderPage{}, err
	}

	var out []channel.Order
	for _, o := range resp {
		total, _ := strconv.ParseFloat(o.Total, 64)
		imp, _ := strconv.ParseFloat(o.TotalTax, 64)
		envio, _ := strconv.ParseFloat(o.ShippingTot, 64)
		// Woo entrega la fecha GMT sin zona; se interpreta como UTC.
		fecha, _ := time.Parse(formatoFecha, o.DateCreated)
		modificado, _ := time.Parse(formatoFecha, o.DateModified)

		// La dirección que viaja a Odoo es la de ENTREGA: es con la que se
		// monta la guía. Se cae a billing solo cuando shipping viene en
		// blanco, que es lo normal en un pedido que no requiere envío
		// (producto digital) o cuando el comprador no marcó dirección
		// distinta; la documentación advierte que los campos vacíos llegan
		// como null o cadena vacía, no omitidos, así que hay que mirar el
		// contenido y no la presencia del objeto.
		entrega := o.Shipping
		if !entrega.tieneDireccion() {
			entrega = o.Billing
		}
		// El nombre del destinatario sale del bloque de entrega; el correo y
		// el teléfono siguen saliendo de billing porque la tabla «Order -
		// Shipping properties» no los incluye.
		nombre := entrega.nombre()
		if nombre == "" {
			nombre = o.Billing.nombre()
		}

		ord := channel.Order{
			ExternalID: fmt.Sprint(o.ID), Number: o.Number, Status: o.Status,
			OrderedAt: fecha, UpdatedAt: modificado, Currency: o.Currency,
			Total: total, Tax: imp, Shipping: envio,
			Buyer: channel.Buyer{
				Name:    nombre,
				Email:   o.Billing.Email,
				Phone:   o.Billing.Phone,
				Address: entrega.direccion(),
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
	return channel.OrderPage{
		Orders: out,
		Next:   channel.Cursor{Page: pagina + 1},
		Done:   esUltimaPagina(cab, pagina, len(resp)),
	}, nil
}

// direccionPedido son los campos comunes a las tablas «Order - Billing
// properties» y «Order - Shipping properties» del recurso Order.
//
// Se usa el mismo tipo para los dos bloques porque comparten los nueve campos
// de dirección; email y phone solo aparecen en la tabla de billing, así que en
// shipping llegan siempre vacíos y no se usan.
// https://woocommerce.github.io/woocommerce-rest-api-docs/#order-properties
type direccionPedido struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Company   string `json:"company"`
	Address1  string `json:"address_1"`
	Address2  string `json:"address_2"`
	City      string `json:"city"`
	State     string `json:"state"`
	Postcode  string `json:"postcode"`
	Country   string `json:"country"`
	Email     string `json:"email"` // solo en billing
	Phone     string `json:"phone"` // solo en billing
}

// tieneDireccion dice si el bloque trae algo con lo que despachar. No basta
// con que el objeto exista: WooCommerce lo manda siempre, con los campos en
// blanco cuando el pedido no lleva envío.
func (d direccionPedido) tieneDireccion() bool {
	return strings.TrimSpace(d.Address1+d.Address2+d.City+d.Postcode) != ""
}

func (d direccionPedido) nombre() string {
	return strings.TrimSpace(d.FirstName + " " + d.LastName)
}

func (d direccionPedido) direccion() channel.Address {
	return channel.Address{
		Line1: d.Address1, Line2: d.Address2,
		City: d.City, State: d.State,
		PostalCode: d.Postcode, Country: d.Country,
	}
}

// esUltimaPagina decide si queda algo por pedir.
//
// La API anuncia el total de páginas en X-WP-TotalPages; pedir una página
// posterior a la última responde error, así que se prefiere la cabecera y solo
// se cae en el conteo de elementos cuando el servidor no la manda.
func esUltimaPagina(cab http.Header, pagina, recibidos int) bool {
	if total, err := strconv.Atoi(strings.TrimSpace(cab.Get("X-WP-TotalPages"))); err == nil && total > 0 {
		return pagina >= total
	}
	return recibidos < 100
}

// AckOrder confirma el despacho en la tienda.
//
// ref.ListingID es el ID DEL PEDIDO (channel_orders.external_order_id), no el
// de una publicación: es lo único que identifica un pedido en la API de Woo.
//
// La API base de WooCommerce no tiene guía ni fulfillment —eso lo añaden
// plugins como Shipment Tracking, que aquí no se pueden suponer instalados—,
// así que el ciclo se cierra con lo que sí existe: una nota del pedido con la
// guía y el paso a "completed", que es lo que la tienda entiende por despachado.
func (a *Adaptador) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	pedido := strings.TrimSpace(ref.ListingID)
	if pedido == "" {
		// Sin id, la ruta quedaría en /orders/ y el PUT caería sobre el
		// endpoint de listado: mejor un error que diga qué falta.
		return &channel.Error{
			Kind: channel.WooCommerce, Code: "sin_pedido",
			Message: "el despacho de WooCommerce necesita el id del pedido en ExternalRef.ListingID",
		}
	}

	// La nota va antes del cambio de estado: si fallara después, el pedido
	// quedaría completado sin rastro de la guía y el trabajo no se reintenta.
	if nota := notaDeGuia(f); nota != "" {
		puesta, err := a.guiaYaAnotada(ctx, pedido, f)
		if err != nil {
			return err
		}
		// La nota es visible para el comprador (customer_note), y WooCommerce
		// le manda un correo por cada una: repetirla en el reintento del
		// trabajo le avisaría dos veces del mismo despacho.
		if !puesta {
			if err := a.llamar(ctx, http.MethodPost, "/orders/"+pedido+"/notes", nil,
				map[string]any{"note": nota, "customer_note": true}, nil); err != nil {
				return err
			}
		}
	}
	return a.llamar(ctx, http.MethodPut, "/orders/"+pedido, nil,
		map[string]any{"status": "completed"}, nil)
}

// guiaYaAnotada dice si el despacho ya se le comunicó al comprador.
//
// El trabajo de despacho se reintenta con backoff, y el segundo intento vuelve
// a pasar por aquí: sin esta comprobación cada reintento crea otra nota y otro
// correo al comprador con la misma guía.
//
// Se compara por contenido y no por igualdad exacta porque la tienda devuelve
// la nota ya renderizada (entidades HTML, saltos de línea): lo que se busca es
// el número de guía, que es lo que no puede repetirse. Sin guía —solo
// transportadora— se compara el texto completo, que es todo lo que hay.
func (a *Adaptador) guiaYaAnotada(ctx context.Context, pedido string, f channel.Fulfillment) (bool, error) {
	marca := strings.TrimSpace(f.TrackingNumber)
	if marca == "" {
		marca = notaDeGuia(f)
	}
	var notas []struct {
		Note string `json:"note"`
	}
	// type=customer: las notas internas de la tienda no las escribe Integra y
	// no dicen nada sobre si el comprador ya fue avisado.
	if err := a.llamar(ctx, http.MethodGet, "/orders/"+pedido+"/notes",
		url.Values{"type": {"customer"}}, nil, &notas); err != nil {
		return false, err
	}
	for _, n := range notas {
		if strings.Contains(n.Note, marca) {
			return true, nil
		}
	}
	return false, nil
}

func notaDeGuia(f channel.Fulfillment) string {
	var partes []string
	if c := strings.TrimSpace(f.Carrier); c != "" {
		partes = append(partes, "Transportadora: "+c)
	}
	if g := strings.TrimSpace(f.TrackingNumber); g != "" {
		partes = append(partes, "Guía: "+g)
	}
	if len(partes) == 0 {
		return ""
	}
	if !f.ShippedAt.IsZero() {
		partes = append(partes, "Despachado: "+f.ShippedAt.Format("2006-01-02"))
	}
	return strings.Join(partes, " · ")
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
	_, err := a.llamarCab(ctx, metodo, ruta, q, cuerpo, out)
	return err
}

// llamarCab hace la petición y devuelve además las cabeceras, que es donde
// viaja la paginación (X-WP-TotalPages).
func (a *Adaptador) llamarCab(ctx context.Context, metodo, ruta string, q url.Values, cuerpo any, out any) (http.Header, error) {
	if q == nil {
		q = url.Values{}
	}

	var body io.Reader
	if cuerpo != nil {
		j, err := json.Marshal(cuerpo)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(j)
	}

	req, err := http.NewRequestWithContext(ctx, metodo,
		a.base+"/wp-json/wc/v3"+ruta+"?"+q.Encode(), body)
	if err != nil {
		return nil, err
	}
	// Las claves van por cabecera, no por la cadena de consulta: en la query
	// acaban en el log de acceso del servidor, en el de cualquier proxy y —lo
	// que de verdad las expone— dentro del texto de los errores de red, que se
	// guardan en channel_accounts.probada_msg y en jobs.last_error y la API
	// devuelve a cualquier usuario.
	req.SetBasicAuth(a.ck, a.cs)
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.cli.Do(req)
	if err != nil {
		// El error de net/http incorpora la URL completa de la petición; se
		// conserva solo la causa para no arrastrarla a los mensajes guardados.
		return nil, &channel.Error{Kind: channel.WooCommerce, Message: causaDe(err), Err: err}
	}
	defer resp.Body.Close()
	datos, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return resp.Header, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		e := &channel.Error{
			Kind: channel.WooCommerce, StatusCode: resp.StatusCode,
			Message: recortar(string(datos), 300),
		}
		if resp.StatusCode == http.StatusNotFound {
			e.Err = channel.ErrNoEncontrado
		}
		// WooCommerce no limita por sí mismo, pero el hosting sí: Cloudflare,
		// Wordfence o el módulo de rate limit del servidor devuelven 429 con
		// Retry-After. Sin leerlo, la cola reintentaba a los 30 s contra una
		// tienda que ya estaba rechazando por exceso.
		if resp.StatusCode == http.StatusTooManyRequests {
			e.RetryAfter = conectores.EsperaTrasCupo(resp.Header)
		}
		return resp.Header, e
	}
	if out == nil {
		return resp.Header, nil
	}
	return resp.Header, json.Unmarshal(datos, out)
}

// causaDe desenvuelve el *url.Error de net/http para quedarse con el motivo
// («connection refused», «no such host») sin la URL de la petición.
func causaDe(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
