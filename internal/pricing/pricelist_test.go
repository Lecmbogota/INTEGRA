package pricing

import (
	"math"
	"testing"
	"time"
)

func casi(t *testing.T, got, want float64, msg string) {
	t.Helper()
	if math.Abs(got-want) > 0.005 {
		t.Errorf("%s: se obtuvo %.4f y se esperaba %.4f", msg, got, want)
	}
}

// tarifasDeMDV reproduce la configuración real encontrada en el Odoo de MDV,
// leída con odoo-explorer. Es el caso que de verdad hay que acertar.
func tarifasDeMDV() []Pricelist {
	const (
		catComputo        = 10
		catAlmacenamiento = 20
	)
	return []Pricelist{
		{
			ID: 1, Name: "Predeterminado", Currency: "COP",
			Rules: []Rule{
				// -25% de descuento sobre el coste = coste × 1,25
				{ID: 101, AppliedOn: OnCategory, ComputePrice: ComputeFormula,
					Base: BaseStandardPrice, CategoryID: catComputo, PriceDiscount: -25},
				{ID: 102, AppliedOn: OnCategory, ComputePrice: ComputeFormula,
					Base: BaseStandardPrice, CategoryID: catAlmacenamiento, PriceDiscount: -25},
			},
		},
		{
			ID: 2, Name: "Dynabook - Nacionalizado", Currency: "COP",
			Rules: []Rule{
				{ID: 201, AppliedOn: OnCategory, ComputePrice: ComputeFormula,
					Base: BaseStandardPrice, CategoryID: catComputo, PriceDiscount: -17.65},
			},
		},
		{
			ID: 5, Name: "FALABELLA", Currency: "COP",
			Rules: []Rule{
				{ID: 501, AppliedOn: OnProduct, ComputePrice: ComputeFixed,
					Base: BaseListPrice, TemplateID: 7001, FixedPrice: 83949.58},
				{ID: 502, AppliedOn: OnProduct, ComputePrice: ComputeFixed,
					Base: BaseListPrice, TemplateID: 7002, FixedPrice: 142773.11},
			},
		},
	}
}

func TestMargenSobreCosteComoEnMDV(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	ahora := time.Now()

	// Un AIO AllDroid-100: coste 941.536,18 COP, list_price 283,50 (en USD,
	// sin convertir). El precio correcto sale del coste, no del list_price.
	p := Product{
		VariantID: 5001, TemplateID: 7001, CategoryID: 10,
		CategoryAncestors: []int64{10},
		ListPrice:         283.50,
		Cost:              941536.18,
	}

	res, err := e.Price(1, p, 1, ahora)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Fatal("debería haber aplicado la regla de categoría")
	}
	// 941.536,18 × 1,25
	casi(t, res.Price, 1176920.225, "precio con margen del 25%")

	// La confirmación de que el diseño resuelve el problema de fondo: el
	// precio calculado no se parece en nada al list_price roto.
	if res.Price < p.ListPrice*100 {
		t.Fatalf("el precio calculado (%.2f) está sospechosamente cerca del list_price roto (%.2f)",
			res.Price, p.ListPrice)
	}
}

func TestTarifaDynabookUsaSuPropioMargen(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	p := Product{
		VariantID: 5002, TemplateID: 7003, CategoryID: 10,
		CategoryAncestors: []int64{10},
		Cost:              1000000,
	}

	pred, _ := e.Price(1, p, 1, time.Now())
	dyna, _ := e.Price(2, p, 1, time.Now())

	casi(t, pred.Price, 1250000, "tarifa Predeterminado")
	casi(t, dyna.Price, 1176500, "tarifa Dynabook")

	if dyna.Price >= pred.Price {
		t.Error("Dynabook tiene menos margen y debería salir más barata")
	}
}

func TestPrecioFijoDeFalabella(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	p := Product{
		VariantID: 5001, TemplateID: 7001, CategoryID: 10,
		CategoryAncestors: []int64{10},
		Cost:              941536.18, ListPrice: 283.50,
	}

	res, err := e.Price(5, p, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// El precio fijo ignora coste y list_price por completo.
	casi(t, res.Price, 83949.58, "precio fijo de Falabella")
	if res.RuleID != 501 {
		t.Errorf("regla aplicada = %d, se esperaba 501", res.RuleID)
	}
}

// Un producto fuera de las 4 categorías con regla —los 92 de RingConn, por
// ejemplo— no obtiene precio. Debe notarse, no colarse con el list_price roto.
func TestSinReglaAplicableNoSeMarcaComoAplicado(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	p := Product{
		VariantID: 5900, TemplateID: 7900, CategoryID: 99, // Wearables
		CategoryAncestors: []int64{99, 98},
		ListPrice:         1.00,
		Cost:              350000,
	}

	res, err := e.Price(1, p, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied {
		t.Fatal("no debería haber aplicado ninguna regla")
	}
	if res.Price != 1.00 {
		t.Errorf("sin regla, Odoo devuelve list_price: %.2f", res.Price)
	}
}

func TestEspecificidadDeReglas(t *testing.T) {
	// Todas las reglas encajan; debe ganar la de variante.
	pl := Pricelist{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFixed, FixedPrice: 4000},
		{ID: 2, AppliedOn: OnCategory, ComputePrice: ComputeFixed, CategoryID: 10, FixedPrice: 3000},
		{ID: 3, AppliedOn: OnProduct, ComputePrice: ComputeFixed, TemplateID: 700, FixedPrice: 2000},
		{ID: 4, AppliedOn: OnVariant, ComputePrice: ComputeFixed, VariantID: 500, FixedPrice: 1000},
	}}
	e := NewEngine([]Pricelist{pl})
	p := Product{VariantID: 500, TemplateID: 700, CategoryID: 10, CategoryAncestors: []int64{10}}

	res, _ := e.Price(1, p, 1, time.Now())
	casi(t, res.Price, 1000, "debe ganar la regla de variante")

	// Sin coincidencia de variante, gana la de plantilla.
	p2 := Product{VariantID: 999, TemplateID: 700, CategoryID: 10, CategoryAncestors: []int64{10}}
	res2, _ := e.Price(1, p2, 1, time.Now())
	casi(t, res2.Price, 2000, "debe ganar la regla de plantilla")

	// Sin plantilla, gana la de categoría.
	p3 := Product{VariantID: 999, TemplateID: 888, CategoryID: 10, CategoryAncestors: []int64{10}}
	res3, _ := e.Price(1, p3, 1, time.Now())
	casi(t, res3.Price, 3000, "debe ganar la regla de categoría")

	// Sin nada, la global.
	p4 := Product{VariantID: 999, TemplateID: 888, CategoryID: 77, CategoryAncestors: []int64{77}}
	res4, _ := e.Price(1, p4, 1, time.Now())
	casi(t, res4.Price, 4000, "debe ganar la regla global")
}

// Una regla sobre "Almacenamiento" debe alcanzar a "Almacenamiento / Disco /
// SSD / Interno / PCI M.2", que es como está montado el árbol de MDV.
func TestReglaDeCategoriaAlcanzaDescendientes(t *testing.T) {
	e := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnCategory, ComputePrice: ComputeFixed, CategoryID: 20, FixedPrice: 5000},
	}}})

	hijo := Product{VariantID: 1, TemplateID: 1, CategoryID: 24,
		CategoryAncestors: []int64{24, 23, 22, 20}}
	res, _ := e.Price(1, hijo, 1, time.Now())
	if !res.Applied {
		t.Fatal("la regla de la categoría padre debería alcanzar a la hija")
	}
	casi(t, res.Price, 5000, "precio por categoría heredada")

	ajeno := Product{VariantID: 2, TemplateID: 2, CategoryID: 30,
		CategoryAncestors: []int64{30}}
	if res2, _ := e.Price(1, ajeno, 1, time.Now()); res2.Applied {
		t.Fatal("una categoría de otra rama no debería coincidir")
	}
}

func TestVigenciaPorFechas(t *testing.T) {
	inicio := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fin := time.Date(2026, 9, 30, 23, 59, 59, 0, time.UTC)

	// Las fechas filtran, no ordenan: dentro de la ventana las dos reglas son
	// candidatas y gana la de mayor id, igual que en Odoo. Por eso la promoción
	// lleva el id más alto, que es como queda al crearla después.
	e := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFixed, FixedPrice: 1500},
		{ID: 2, AppliedOn: OnGlobal, ComputePrice: ComputeFixed, FixedPrice: 900,
			DateStart: &inicio, DateEnd: &fin},
	}}})
	p := Product{VariantID: 1, TemplateID: 1}

	dentro, _ := e.Price(1, p, 1, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	casi(t, dentro.Price, 900, "dentro de la promoción")

	antes, _ := e.Price(1, p, 1, time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC))
	casi(t, antes.Price, 1500, "antes de la promoción")

	despues, _ := e.Price(1, p, 1, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	casi(t, despues.Price, 1500, "después de la promoción")
}

func TestCantidadMinima(t *testing.T) {
	e := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFixed, FixedPrice: 800, MinQuantity: 10},
		{ID: 2, AppliedOn: OnGlobal, ComputePrice: ComputeFixed, FixedPrice: 1000},
	}}})
	p := Product{VariantID: 1, TemplateID: 1}

	unidad, _ := e.Price(1, p, 1, time.Now())
	casi(t, unidad.Price, 1000, "por unidad")

	mayoreo, _ := e.Price(1, p, 10, time.Now())
	casi(t, mayoreo.Price, 800, "a partir de 10 unidades")
}

func TestRedondeoYMargenes(t *testing.T) {
	e := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
			Base: BaseStandardPrice, PriceDiscount: -25, PriceRound: 100},
	}}})
	// 137.777 × 1,25 = 172.221,25 → redondeado a centenas = 172.200
	p := Product{VariantID: 1, TemplateID: 1, Cost: 137777}
	res, _ := e.Price(1, p, 1, time.Now())
	casi(t, res.Price, 172200, "redondeo a centenas")

	// Margen mínimo: eleva el precio cuando el cálculo se queda corto.
	e2 := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
			Base: BaseStandardPrice, PriceDiscount: 0, PriceMinMargin: 50000},
	}}})
	res2, _ := e2.Price(1, Product{VariantID: 1, TemplateID: 1, Cost: 100000}, 1, time.Now())
	casi(t, res2.Price, 150000, "margen mínimo aplicado")

	// Margen máximo: limita el precio cuando el cálculo se pasa.
	e3 := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
			Base: BaseStandardPrice, PriceDiscount: -100, PriceMaxMargin: 30000},
	}}})
	res3, _ := e3.Price(1, Product{VariantID: 1, TemplateID: 1, Cost: 100000}, 1, time.Now())
	casi(t, res3.Price, 130000, "margen máximo aplicado")
}

func TestPorcentajeYRecargo(t *testing.T) {
	e := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputePercentage,
			Base: BaseListPrice, PercentPrice: 10},
	}}})
	res, _ := e.Price(1, Product{VariantID: 1, TemplateID: 1, ListPrice: 200000}, 1, time.Now())
	casi(t, res.Price, 180000, "10% de descuento")

	e2 := NewEngine([]Pricelist{{ID: 1, Rules: []Rule{
		{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
			Base: BaseStandardPrice, PriceDiscount: -20, PriceSurcharge: 5000},
	}}})
	res2, _ := e2.Price(1, Product{VariantID: 1, TemplateID: 1, Cost: 100000}, 1, time.Now())
	casi(t, res2.Price, 125000, "margen del 20% más recargo fijo")
}

func TestTarifaEncadenada(t *testing.T) {
	e := NewEngine([]Pricelist{
		{ID: 1, Rules: []Rule{
			{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
				Base: BaseStandardPrice, PriceDiscount: -25},
		}},
		{ID: 2, Rules: []Rule{
			// Parte del resultado de la tarifa 1 y aplica un 10% extra.
			{ID: 2, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
				Base: BasePricelist, BasePricelistID: 1, PriceDiscount: 10},
		}},
	})
	res, err := e.Price(2, Product{VariantID: 1, TemplateID: 1, Cost: 100000}, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// 100.000 × 1,25 = 125.000; 125.000 × 0,9 = 112.500
	casi(t, res.Price, 112500, "tarifa encadenada")
}

func TestCicloDeTarifasCorta(t *testing.T) {
	e := NewEngine([]Pricelist{
		{ID: 1, Rules: []Rule{
			{ID: 1, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
				Base: BasePricelist, BasePricelistID: 2},
		}},
		{ID: 2, Rules: []Rule{
			{ID: 2, AppliedOn: OnGlobal, ComputePrice: ComputeFormula,
				Base: BasePricelist, BasePricelistID: 1},
		}},
	})
	if _, err := e.Price(1, Product{VariantID: 1, TemplateID: 1}, 1, time.Now()); err == nil {
		t.Fatal("un ciclo de tarifas debería dar error, no colgarse")
	}
}

func TestTarifaDesconocida(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	if _, err := e.Price(999, Product{}, 1, time.Now()); err == nil {
		t.Fatal("una tarifa inexistente debería dar error")
	}
}

func TestExplicacionNoEstaVacia(t *testing.T) {
	e := NewEngine(tarifasDeMDV())
	p := Product{VariantID: 1, TemplateID: 1, CategoryID: 10,
		CategoryAncestors: []int64{10}, Cost: 941536.18}
	res, _ := e.Price(1, p, 1, time.Now())

	// La interfaz tiene que poder explicar de dónde sale cada precio.
	if res.Explanation == "" {
		t.Fatal("el resultado debería explicar el cálculo")
	}
	t.Logf("explicación: %s", res.Explanation)
}
