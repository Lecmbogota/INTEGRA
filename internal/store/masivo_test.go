package store

import "testing"

// El redondeo es puro y se prueba entero. En una operación masiva un error
// aquí no afecta a un producto: afecta a cientos, y hacia arriba significa
// vender más caro de lo que alguien decidió.

func TestRedondeoComercialTerminaEn900(t *testing.T) {
	casos := []struct {
		entrada float64
		quiero  float64
	}{
		{116300, 116900},  // el caso real: coste × comisión
		{119900, 119900},  // ya está en 900: no se toca
		{120000, 120900},  // sube al 900 del mismo millar
		{99500, 99900},
	}
	for _, c := range casos {
		if got := redondear(c.entrada, 900); got != c.quiero {
			t.Errorf("redondear(%.0f, 900) = %.0f, se esperaba %.0f", c.entrada, got, c.quiero)
		}
	}
}

// Por debajo de mil el patrón 900 no tiene sentido: un accesorio de $ 450 no
// puede convertirse en $ 900, que es el doble.
func TestRedondeo900NoDuplicaPreciosPequenos(t *testing.T) {
	got := redondear(450, 900)
	if got > 500 {
		t.Fatalf("un precio de 450 no puede subir a %.0f", got)
	}
}

func TestRedondeoAlCentenar(t *testing.T) {
	if got := redondear(116279.07, 100); got != 116300 {
		t.Errorf("redondear al centenar dio %.2f", got)
	}
}

func TestSinRedondeoConservaCentavos(t *testing.T) {
	if got := redondear(116279.07, 0); got != 116279.07 {
		t.Errorf("sin redondeo debía conservar el valor, dio %.2f", got)
	}
}

func TestRedondeoDeCeroEsCero(t *testing.T) {
	for _, modo := range []int{0, 100, 900} {
		if got := redondear(0, modo); got != 0 {
			t.Errorf("redondear(0, %d) = %.2f", modo, got)
		}
	}
}

// Los precios en Colombia se escriben «1.234.567,89»: el punto es separador
// de miles y la coma decimal. Interpretarlos al revés convertiría un millón
// en uno con veintitrés decimales.
func TestPrecioEnFormatoColombiano(t *testing.T) {
	casos := []struct {
		entrada string
		quiero  float64
	}{
		{"119900", 119900},
		{"119.900", 119900},
		{"1.234.567,89", 1234567.89},
		{"99900,50", 99900.50},
		{"  50000  ", 50000},
	}
	for _, c := range casos {
		got, err := aFloat(c.entrada)
		if err != nil {
			t.Errorf("aFloat(%q) devolvió error: %v", c.entrada, err)
			continue
		}
		if got != c.quiero {
			t.Errorf("aFloat(%q) = %.2f, se esperaba %.2f", c.entrada, got, c.quiero)
		}
	}
}

func TestPrecioNegativoSeRechaza(t *testing.T) {
	if _, err := aFloat("-1000"); err == nil {
		t.Fatal("un precio negativo debe rechazarse")
	}
}
