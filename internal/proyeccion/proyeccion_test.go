package proyeccion

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// entradaCompleta es un producto real de MDV con todo resuelto.
func entradaCompleta() Entrada {
	return Entrada{
		SKU:     "KVR56U46BS8-16",
		Barcode: "740617331240",
		Titulos: map[string]string{
			"mercadolibre": "Memoria RAM Kingston 16GB DDR5-5600 UDIMM",
			"falabella":    "Memoria RAM Kingston 16GB DDR5-5600 UDIMM Escritorio",
			"shopify":      "Memoria RAM Kingston 16GB DDR5-5600 UDIMM Non-ECC",
			"woocommerce":  "Memoria RAM Kingston 16GB DDR5-5600 UDIMM Non-ECC",
		},
		Descripcion:    "Memoria RAM Kingston 16GB DDR5-5600 UDIMM.\n\nCaracterísticas:\n• Capacidad: 16 GB",
		Marca:          "Kingston",
		CategPath:      "Almacenamiento / Memorias / RAM / DDR5 / UDIMM",
		CategoriaCanal: "MCO1704",
		Specs: []Spec{
			{Clave: "Capacidad", Valor: "16 GB"},
			{Clave: "Tecnología", Valor: "DDR5-5600"},
		},
		Precio:   705000,
		Moneda:   "COP",
		Stock:    199,
		Peso:     0.05,
		Imagenes: []string{"https://cdn.mdv.co/kvr56.jpg"},
	}
}

func TestProyeccionCompletaEsPublicableEnLosCuatro(t *testing.T) {
	e := entradaCompleta()
	for _, p := range ProyectarTodos(e) {
		if !p.Publicable() {
			t.Errorf("%s no es publicable con datos completos: %+v", p.Canal, p.Faltantes)
		}
		if p.Payload == nil {
			t.Errorf("%s no generó payload", p.Canal)
		}
		// El payload tiene que ser serializable: es lo que se manda por HTTP
		// y lo que se pinta en la vista previa.
		if _, err := json.Marshal(p.Payload); err != nil {
			t.Errorf("%s: el payload no serializa: %v", p.Canal, err)
		}
	}
}

// Sin categoría mapeada —el estado actual de todo el catálogo de MDV—
// MercadoLibre y Falabella bloquean, pero WooCommerce y Shopify no.
func TestSinCategoriaSoloBloqueanAlgunos(t *testing.T) {
	e := entradaCompleta()
	e.CategoriaCanal = ""

	esperado := map[Canal]bool{
		WooCommerce: true, Shopify: true,
		MercadoLibre: false, Falabella: false,
	}
	for _, p := range ProyectarTodos(e) {
		if got := p.Publicable(); got != esperado[p.Canal] {
			t.Errorf("%s publicable=%v, se esperaba %v. Faltantes: %+v",
				p.Canal, got, esperado[p.Canal], p.Faltantes)
		}
	}
}

func TestTituloLargoBloqueaSoloMercadoLibre(t *testing.T) {
	e := entradaCompleta()
	largo := strings.Repeat("A", 80)
	for k := range e.Titulos {
		e.Titulos[k] = largo
	}

	for _, p := range ProyectarTodos(e) {
		tieneFalloTitulo := false
		for _, f := range p.Faltantes {
			if strings.Contains(strings.ToLower(f.Campo), "title") ||
				strings.Contains(strings.ToLower(f.Campo), "name") {
				tieneFalloTitulo = true
			}
		}
		if p.Canal == MercadoLibre && !tieneFalloTitulo {
			t.Error("MercadoLibre debería rechazar un título de 80 caracteres")
		}
		if p.Canal == Shopify && tieneFalloTitulo {
			t.Error("Shopify admite 255 caracteres y no debería quejarse")
		}
	}
}

// Cada canal maneja la oferta a su manera: es la razón de ser de Capabilities.
func TestLaOfertaSeExpresaDistintoEnCadaCanal(t *testing.T) {
	e := entradaCompleta()
	desde := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	hasta := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	e.PrecioOferta = 599000
	e.OfertaDesde = &desde
	e.OfertaHasta = &hasta

	woo := Proyectar(WooCommerce, e)
	// En pesos colombianos, sin decimales.
	if woo.Payload["sale_price"] != "599000" {
		t.Errorf("Woo sale_price = %v", woo.Payload["sale_price"])
	}
	// Woo admite vigencia nativa.
	if woo.Payload["date_on_sale_from"] == nil {
		t.Error("Woo debería enviar la fecha de inicio de la promoción")
	}
	if woo.PrecioTachado != 705000 {
		t.Errorf("Woo debería tachar el precio regular, tachado=%v", woo.PrecioTachado)
	}

	shop := Proyectar(Shopify, e)
	v := shop.Payload["variants"].([]map[string]any)[0]
	if v["compareAtPrice"] != "705000" {
		t.Errorf("Shopify debería usar compareAtPrice, dio %v", v["compareAtPrice"])
	}
	if !avisaSobre(shop.Notas, "fechas") {
		t.Error("Shopify debería advertir que no admite fechas de promoción")
	}

	ml := Proyectar(MercadoLibre, e)
	if ml.Payload["price"] != 599000.0 {
		t.Errorf("MercadoLibre debería bajar el precio, dio %v", ml.Payload["price"])
	}
	if ml.PrecioTachado != 0 {
		t.Error("MercadoLibre no tiene precio tachado: debería quedar en cero")
	}
	if !avisaSobre(ml.Notas, "antes/después") {
		t.Errorf("MercadoLibre debería documentar la limitación: %v", ml.Notas)
	}

	fal := Proyectar(Falabella, e)
	if fal.Payload["SalePrice"] != 599000.0 {
		t.Errorf("Falabella SalePrice = %v", fal.Payload["SalePrice"])
	}
}

func TestFalabellaExigePesoYMarca(t *testing.T) {
	e := entradaCompleta()
	e.Peso = 0
	e.Marca = ""

	p := Proyectar(Falabella, e)
	if p.Publicable() {
		t.Fatal("sin peso ni marca Falabella no debería aceptar")
	}
	campos := map[string]bool{}
	for _, f := range p.Faltantes {
		campos[f.Campo] = true
	}
	for _, c := range []string{"PackageWeight", "Brand"} {
		if !campos[c] {
			t.Errorf("falta el aviso de %s: %+v", c, p.Faltantes)
		}
	}
}

func TestRequisitosComunes(t *testing.T) {
	vacia := Entrada{}
	p := Proyectar(WooCommerce, vacia)

	campos := map[string]bool{}
	for _, f := range p.Faltantes {
		campos[f.Campo] = true
	}
	for _, c := range []string{"sku", "precio", "descripcion"} {
		if !campos[c] {
			t.Errorf("un producto vacío debería avisar de %q: %+v", c, p.Faltantes)
		}
	}
	if p.Publicable() {
		t.Error("un producto vacío no debería ser publicable")
	}
}

// La proyección debe ser determinista: si cambiara entre dos llamadas, el
// hash de contenido variaría sin motivo y se reenviaría todo cada noche.
func TestProyeccionEsDeterminista(t *testing.T) {
	e := entradaCompleta()
	e.Specs = []Spec{
		{Clave: "Tecnología", Valor: "DDR5-5600"},
		{Clave: "Capacidad", Valor: "16 GB"},
		{Clave: "Formato", Valor: "UDIMM"},
	}

	primera, err := json.Marshal(Proyectar(WooCommerce, e).Payload)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 25; i++ {
		otra, err := json.Marshal(Proyectar(WooCommerce, e).Payload)
		if err != nil {
			t.Fatal(err)
		}
		if string(otra) != string(primera) {
			t.Fatalf("la proyección cambió entre llamadas:\n  %s\n  %s", primera, otra)
		}
	}
}

func TestStockCeroAdvierteYMarcaAgotado(t *testing.T) {
	e := entradaCompleta()
	e.Stock = 0

	woo := Proyectar(WooCommerce, e)
	if woo.Payload["stock_status"] != "outofstock" {
		t.Errorf("stock_status = %v", woo.Payload["stock_status"])
	}
	// Sin existencias se advierte, pero no se bloquea: puede interesar
	// publicar agotado para no perder el historial de la publicación.
	if !woo.Publicable() {
		t.Error("el stock cero debería advertir, no bloquear")
	}
}

// El cálculo de tarifa (coste × 1,25) produce decimales constantemente, y el
// peso colombiano no se fracciona. Publicar 102.111,5953 sería incorrecto.
func TestElPesoColombianoNoLlevaDecimales(t *testing.T) {
	e := entradaCompleta()
	e.Precio = 102111.5953
	e.PrecioOferta = 89999.4444

	for _, p := range ProyectarTodos(e) {
		if p.Precio != float64(int64(p.Precio)) {
			t.Errorf("%s publica un precio con decimales: %v", p.Canal, p.Precio)
		}
		if p.PrecioTachado != float64(int64(p.PrecioTachado)) {
			t.Errorf("%s tacha un precio con decimales: %v", p.Canal, p.PrecioTachado)
		}
	}

	// Y en el payload, no solo en el campo derivado.
	woo := Proyectar(WooCommerce, e)
	if woo.Payload["regular_price"] != "102112" {
		t.Errorf("Woo regular_price = %v, se esperaba \"102112\"", woo.Payload["regular_price"])
	}
	ml := Proyectar(MercadoLibre, e)
	if ml.Payload["price"] != 89999.0 {
		t.Errorf("ML price = %v, se esperaba 89999", ml.Payload["price"])
	}
}

// Una moneda con céntimos sí los conserva.
func TestOtrasMonedasConservanDecimales(t *testing.T) {
	e := entradaCompleta()
	e.Moneda = "USD"
	e.Precio = 24.99

	p := Proyectar(WooCommerce, e)
	if p.Payload["regular_price"] != "24.99" {
		t.Errorf("en USD debería conservar los céntimos: %v", p.Payload["regular_price"])
	}
}

func avisaSobre(notas []string, frag string) bool {
	for _, n := range notas {
		if strings.Contains(strings.ToLower(n), strings.ToLower(frag)) {
			return true
		}
	}
	return false
}
