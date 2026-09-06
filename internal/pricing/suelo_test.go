package pricing

import (
	"testing"
	"time"
)

// El suelo de coste. Antes solo existía si alguien había creado una regla de
// canal Y le había puesto min_margin_percent: sin regla, el precio salía tal
// cual y un coste que subía en Odoo se vendía a pérdida sin un solo aviso.

func TestSinReglaElPrecioCalculadoSigueSinPoderBajarDelCoste(t *testing.T) {
	canal := ChannelInfo{Currency: "COP"} // sin comisión, para aislar el suelo
	input := VariantPricingInput{VariantID: 1, BasePrice: 8000, Cost: 10000}

	// Sin reglas: es el caso de casi todo el catálogo de MDV.
	ef := ResolverPrecio(input, canal, nil, nil, nil, time.Now())

	if ef.RegularPrice < input.Cost {
		t.Fatalf("se publicaría a %.2f con un coste de %.2f: %s",
			ef.RegularPrice, input.Cost, ef.Explanation)
	}
}

// La comisión se la lleva el canal del precio de escaparate, así que comparar
// el precio contra el coste da por bueno vender a pérdida: a 100.000 en un
// canal que cobra el 20% llegan 80.000, y un coste de 90.000 es una pérdida de
// 10.000 por unidad que la comprobación anterior no veía.
func TestElSueloSeMideSobreLoQueQuedaTrasLaComisionDelCanal(t *testing.T) {
	canal := ChannelInfo{CommissionPct: 20, Currency: "COP"}
	input := VariantPricingInput{VariantID: 1, BasePrice: 72000, Cost: 80000}

	diez := 10.0
	regla := ChannelPriceRule{ID: 3, AdjustmentType: AdjustmentPercent, MinMarginPercent: &diez, Active: true}

	// El precio de escaparate sale a 90.000, que contra un coste de 80.000 más
	// el 10% (88.000) parecía margen de sobra. Pero el canal se lleva el 20%:
	// entran 72.000 y se pierden 16.000 en cada unidad.
	ef := ResolverPrecio(input, canal, []ChannelPriceRule{regla}, nil, nil, time.Now())

	neto := NetoDelCanal(ef.RegularPrice, canal)
	if neto < input.Cost*1.1 {
		t.Fatalf("publicado a %.2f, el canal deja %.2f y hacían falta %.2f: %s",
			ef.RegularPrice, neto, input.Cost*1.1, ef.Explanation)
	}
}

// El margen de la cuenta rige cuando la regla no trae el suyo, y la regla
// manda cuando lo trae: si no, poner un suelo en la cuenta se llevaría por
// delante los márgenes finos ya configurados por marca o categoría.
func TestElMargenDeLaReglaMandaSobreElDeLaCuenta(t *testing.T) {
	canal := ChannelInfo{Currency: "COP", MinMargenPct: 50}
	input := VariantPricingInput{VariantID: 1, BasePrice: 10000, Cost: 10000}

	diez := 10.0
	regla := ChannelPriceRule{ID: 7, AdjustmentType: AdjustmentPercent, MinMarginPercent: &diez, Active: true}

	conRegla := ResolverPrecio(input, canal, []ChannelPriceRule{regla}, nil, nil, time.Now())
	if conRegla.RegularPrice > 12000 {
		t.Fatalf("la regla exige 10%%, no el 50%% de la cuenta: %.2f (%s)",
			conRegla.RegularPrice, conRegla.Explanation)
	}

	sinRegla := ResolverPrecio(input, canal, nil, nil, nil, time.Now())
	if sinRegla.RegularPrice < 15000 {
		t.Fatalf("sin regla manda el 50%% de la cuenta: %.2f (%s)",
			sinRegla.RegularPrice, sinRegla.Explanation)
	}
}

// Un coste desconocido —cero, lo que trae Odoo mientras el producto no se haya
// comprado nunca— no puede decir si se pierde dinero, y no debe frenar nada:
// bloquear medio catálogo por un dato que falta sería peor que el problema.
func TestSinCosteConocidoNoSeDaNadaPorPerdida(t *testing.T) {
	canal := ChannelInfo{CommissionPct: 16}
	if !CubreCosto(1, 0, 30, canal) {
		t.Fatal("un coste desconocido no es una venta a pérdida")
	}
}

// El redondeo comercial deja el precio justo en el suelo, y sin holgura la
// coma flotante lo delataba como pérdida: el aviso saldría para siempre sobre
// un producto que no tiene nada que arreglar.
func TestUnPrecioJustoEnElSueloCubreElCoste(t *testing.T) {
	canal := ChannelInfo{CommissionPct: 16, FixedCost: 950}
	costo, margen := 47300.0, 12.5

	suelo := SueloDeCosto(costo, margen, canal)
	if !CubreCosto(suelo, costo, margen, canal) {
		t.Fatalf("el suelo %.4f no se cubre a sí mismo (neto %.4f, exigido %.4f)",
			suelo, NetoDelCanal(suelo, canal), costo*(1+margen/100))
	}
}

// 436 de los 632 productos de MDV no tienen precio usable. El suelo levanta un
// precio demasiado bajo; no inventa uno donde no hay ninguno, porque hacerlo
// los volvería publicables de golpe a un precio que nadie decidió.
func TestUnProductoSinPrecioSigueSinPrecioAunqueTengaCoste(t *testing.T) {
	canal := ChannelInfo{Currency: "COP", MinMargenPct: 30}
	input := VariantPricingInput{VariantID: 1, BasePrice: 0, Cost: 50000}

	ef := ResolverPrecio(input, canal, nil, nil, nil, time.Now())

	if ef.RegularPrice != 0 {
		t.Fatalf("se le inventó un precio de %.2f a un producto sin precio: %s",
			ef.RegularPrice, ef.Explanation)
	}
}
