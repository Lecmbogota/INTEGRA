package proyeccion

import (
	"fmt"
	"strconv"
)

// ------------------------------------------------------------ WooCommerce
//
// Estructura verificada contra la documentación de la REST API v3:
// https://woocommerce.github.io/woocommerce-rest-api-docs/#product-properties
//
// Es el canal más permisivo: solo `name` es obligatorio. Por eso se eligió
// como banco de pruebas — un mapeo mal hecho aquí se corrige borrando, no
// arrastra historial como en MercadoLibre.

func proyectarWoo(e Entrada) Proyeccion {
	p := Proyeccion{
		Canal: WooCommerce, Metodo: "POST", Endpoint: "/wp-json/wc/v3/products",
		TituloLimite: 255,
		Faltantes:    comunes(e),
	}
	p.Titulo = titulo(e, WooCommerce)
	p.Precio = e.Precio
	p.Stock = e.Stock

	// Los precios viajan como cadena en esta API, no como número.
	payload := map[string]any{
		"name":           p.Titulo,
		"type":           "simple",
		"sku":            e.SKU,
		"description":    e.Descripcion,
		"regular_price":  formatoImporte(e.Precio, moneda(e)),
		"manage_stock":   true,
		"stock_quantity": e.Stock,
		"stock_status":   estadoStockWoo(e.Stock),
	}

	if hayOferta(e) {
		payload["sale_price"] = formatoImporte(e.PrecioOferta, moneda(e))
		// WooCommerce sí admite vigencia nativa: no hace falta programar
		// trabajos para aplicar y revertir la promoción.
		payload["date_on_sale_from"] = fecha(e.OfertaDesde)
		payload["date_on_sale_to"] = fecha(e.OfertaHasta)
		p.PrecioTachado = e.Precio
		p.Precio = e.PrecioOferta
	}

	if e.Peso > 0 {
		payload["weight"] = strconv.FormatFloat(e.Peso, 'f', 3, 64)
	}
	if e.CategoriaCanal != "" {
		payload["categories"] = []map[string]any{{"id": e.CategoriaCanal}}
	} else {
		p.Faltantes = append(p.Faltantes, Faltante{
			"categories", "sin categoría mapeada el producto cae en 'Sin categorizar'", Advierte})
	}

	var imgs []map[string]any
	for i, u := range e.Imagenes {
		imgs = append(imgs, map[string]any{"src": u, "position": i})
	}
	if imgs != nil {
		payload["images"] = imgs
	}

	var attrs []map[string]any
	for i, s := range specsOrdenadas(e) {
		attrs = append(attrs, map[string]any{
			"name": s.Clave, "position": i, "visible": true, "options": []string{s.Valor},
		})
	}
	if attrs != nil {
		payload["attributes"] = attrs
	}

	p.Payload = payload
	p.Notas = append(p.Notas, "los precios viajan como cadena, no como número")
	return p
}

func estadoStockWoo(n int) string {
	if n > 0 {
		return "instock"
	}
	return "outofstock"
}

// ---------------------------------------------------------------- Shopify
//
// Shopify usa GraphQL Admin API. La estructura de abajo corresponde a la
// mutación productCreate con su ProductInput.
//
// PENDIENTE DE VERIFICAR contra la versión vigente de la API antes de
// implementar el adaptador: Shopify versiona por trimestre y ha movido precio
// y stock de Product a ProductVariant y de ahí a mutaciones específicas.

func proyectarShopify(e Entrada) Proyeccion {
	p := Proyeccion{
		Canal: Shopify, Metodo: "POST", Endpoint: "/admin/api/graphql.json (productCreate)",
		TituloLimite: 255,
		Faltantes:    comunes(e),
	}
	p.Titulo = titulo(e, Shopify)
	p.Precio = e.Precio
	p.Stock = e.Stock

	variante := map[string]any{
		"sku":                 e.SKU,
		"price":               formatoImporte(e.Precio, moneda(e)),
		"inventoryQuantities": map[string]any{"availableQuantity": e.Stock},
		"inventoryManagement": "SHOPIFY",
	}
	if e.Barcode != "" {
		variante["barcode"] = e.Barcode
	}
	if e.Peso > 0 {
		variante["weight"] = e.Peso
		variante["weightUnit"] = "KILOGRAMS"
	}

	if hayOferta(e) {
		// En Shopify el precio vigente es `price` y el tachado `compareAtPrice`.
		variante["price"] = formatoImporte(e.PrecioOferta, moneda(e))
		variante["compareAtPrice"] = formatoImporte(e.Precio, moneda(e))
		p.PrecioTachado = e.Precio
		p.Precio = e.PrecioOferta
		// No hay vigencia nativa: la promoción hay que revertirla desde fuera.
		p.Notas = append(p.Notas,
			"Shopify no admite fechas de promoción: Integra programará el cambio y su reversión")
	}

	var tags []string
	for _, s := range specsOrdenadas(e) {
		tags = append(tags, fmt.Sprintf("%s: %s", s.Clave, s.Valor))
	}

	payload := map[string]any{
		"title":           p.Titulo,
		"descriptionHtml": e.Descripcion,
		"vendor":          e.Marca,
		"productType":     e.CategoriaCanal,
		"status":          "ACTIVE",
		"tags":            tags,
		"variants":        []map[string]any{variante},
	}
	if len(e.Imagenes) > 0 {
		var media []map[string]any
		for _, u := range e.Imagenes {
			media = append(media, map[string]any{"originalSource": u, "mediaContentType": "IMAGE"})
		}
		payload["media"] = media
	}
	if e.Marca == "" {
		p.Faltantes = append(p.Faltantes, Faltante{
			"vendor", "Shopify usa el fabricante para filtrar en la tienda", Advierte})
	}

	p.Payload = payload
	p.Notas = append(p.Notas, "estructura pendiente de verificar contra la versión vigente de la Admin API")
	return p
}

// ----------------------------------------------------------- MercadoLibre
//
// Campos confirmados en la documentación de MercadoLibre: title, category_id,
// price, currency_id, available_quantity, buying_mode, condition,
// listing_type_id, pictures, attributes. `condition` es obligatorio.
//
// PENDIENTE DE VERIFICAR: la lista exhaustiva de campos obligatorios y los
// atributos que exige cada categoría, que solo se conocen consultando
// /categories/{id}/attributes con credenciales.

func proyectarML(e Entrada) Proyeccion {
	const limiteTitulo = 60

	p := Proyeccion{
		Canal: MercadoLibre, Metodo: "POST", Endpoint: "/items",
		TituloLimite: limiteTitulo,
		Faltantes:    comunes(e),
	}
	p.Titulo = titulo(e, MercadoLibre)
	p.Precio = e.Precio
	p.Stock = e.Stock

	if n := len([]rune(p.Titulo)); n > limiteTitulo {
		p.Faltantes = append(p.Faltantes, Faltante{
			"title", fmt.Sprintf("%d caracteres; el máximo es %d", n, limiteTitulo), Bloquea})
	}

	// La categoría no es opcional: sin ella no hay publicación, y de ella
	// dependen además los atributos obligatorios.
	if e.CategoriaCanal == "" {
		p.Faltantes = append(p.Faltantes, Faltante{
			"category_id", "hace falta mapear la categoría de Odoo a una de MercadoLibre", Bloquea})
	}

	var attrs []map[string]any
	if e.Marca != "" {
		attrs = append(attrs, map[string]any{"id": "BRAND", "value_name": e.Marca})
	} else {
		p.Faltantes = append(p.Faltantes, Faltante{
			"attributes.BRAND", "MercadoLibre exige la marca en casi todas las categorías", Bloquea})
	}
	if e.SKU != "" {
		attrs = append(attrs, map[string]any{"id": "SELLER_SKU", "value_name": e.SKU})
	}
	for clave, valor := range e.AtributosCanal {
		attrs = append(attrs, map[string]any{"id": clave, "value_name": valor})
	}

	var pics []map[string]any
	for _, u := range e.Imagenes {
		pics = append(pics, map[string]any{"source": u})
	}

	payload := map[string]any{
		"title":              p.Titulo,
		"category_id":        e.CategoriaCanal,
		"price":              e.Precio,
		"currency_id":        moneda(e),
		"available_quantity": e.Stock,
		"buying_mode":        "buy_it_now",
		"condition":          "new",
		"listing_type_id":    "gold_special",
		"description":        map[string]any{"plain_text": e.Descripcion},
		"attributes":         attrs,
	}
	if pics != nil {
		payload["pictures"] = pics
	}

	if hayOferta(e) {
		// No existe campo de precio tachado: la promoción es bajar el precio.
		payload["price"] = e.PrecioOferta
		p.PrecioTachado = 0
		p.Precio = e.PrecioOferta
		p.Notas = append(p.Notas,
			"MercadoLibre no tiene precio 'antes/después': la oferta baja el precio del ítem "+
				"e Integra programa su reversión al vencer")
	}

	p.Payload = payload
	p.Notas = append(p.Notas,
		"los atributos obligatorios dependen de la categoría y solo se conocen consultando la API")
	return p
}

// -------------------------------------------------------------- Falabella
//
// Falabella Seller Center trabaja por feeds asíncronos: la petición devuelve
// un identificador y el resultado se consulta después.
//
// PENDIENTE DE VERIFICAR: nombres exactos de campo y firma HMAC de la
// petición. Su documentación no es pública, así que esta proyección es la
// hipótesis de trabajo y habrá que ajustarla con las credenciales delante.

func proyectarFalabella(e Entrada) Proyeccion {
	p := Proyeccion{
		Canal: Falabella, Metodo: "POST", Endpoint: "/products (feed asíncrono)",
		TituloLimite: 150,
		Faltantes:    comunes(e),
	}
	p.Titulo = titulo(e, Falabella)
	p.Precio = e.Precio
	p.Stock = e.Stock

	payload := map[string]any{
		"SellerSku":       e.SKU,
		"Name":            p.Titulo,
		"Description":     e.Descripcion,
		"Brand":           e.Marca,
		"PrimaryCategory": e.CategoriaCanal,
		"Price":           e.Precio,
		"Quantity":        e.Stock,
		"Status":          "active",
	}

	if hayOferta(e) {
		payload["SalePrice"] = e.PrecioOferta
		payload["SaleStartDate"] = fecha(e.OfertaDesde)
		payload["SaleEndDate"] = fecha(e.OfertaHasta)
		p.PrecioTachado = e.Precio
		p.Precio = e.PrecioOferta
	}

	// Falabella exige logística: sin peso ni dimensiones no calcula el envío.
	if e.Peso > 0 {
		payload["PackageWeight"] = e.Peso
	} else {
		p.Faltantes = append(p.Faltantes, Faltante{
			"PackageWeight", "Falabella necesita el peso para calcular el envío", Bloquea})
	}
	if e.Marca == "" {
		p.Faltantes = append(p.Faltantes, Faltante{
			"Brand", "la marca es obligatoria y debe estar registrada en Falabella", Bloquea})
	}
	if e.CategoriaCanal == "" {
		p.Faltantes = append(p.Faltantes, Faltante{
			"PrimaryCategory", "hace falta mapear la categoría al árbol de Falabella", Bloquea})
	}

	var imgs []string
	imgs = append(imgs, e.Imagenes...)
	if imgs != nil {
		payload["Images"] = imgs
	}

	p.Payload = payload
	p.Notas = append(p.Notas,
		"la escritura es asíncrona: devuelve un FeedID y el resultado se consulta después")
	p.Notas = append(p.Notas,
		"nombres de campo pendientes de verificar: la documentación no es pública")
	return p
}
