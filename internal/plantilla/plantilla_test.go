package plantilla

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

// Los números son la parte que más fácil se rompe: la misma hoja abierta en un
// Excel en español y en uno en inglés escribe el mismo precio de tres formas
// distintas, y equivocarse por un factor de mil es un producto regalado.
func TestNumeroAceptaLosFormatosQueSalenDeExcel(t *testing.T) {
	casos := []struct {
		entrada string
		quiero  float64
	}{
		{"129900", 129900},
		{"129.900", 129900},   // miles con punto (es-CO)
		{"129,900", 129900},   // miles con coma (en-US)
		{"$ 129.900", 129900}, // pegado desde una factura
		{"1.234.567", 1234567},
		{"129900,50", 129900.50}, // decimal con coma
		{"129900.50", 129900.50}, // decimal con punto
		{"1.234,56", 1234.56},    // es-CO completo
		{"1,234.56", 1234.56},    // en-US completo
		{"0,5", 0.5},
	}
	for _, c := range casos {
		got, err := numero_(c.entrada)
		if err != nil {
			t.Errorf("numero_(%q) devolvió error: %v", c.entrada, err)
			continue
		}
		if got != c.quiero {
			t.Errorf("numero_(%q) = %v, quiero %v", c.entrada, got, c.quiero)
		}
	}

	for _, malo := range []string{"", "abc", "12 pesos", "--3"} {
		if v, err := numero_(malo); err == nil {
			t.Errorf("numero_(%q) devolvió %v; debería fallar", malo, v)
		}
	}
}

func TestFechaAceptaDiaPrimeroYISO(t *testing.T) {
	casos := []struct {
		entrada        string
		dia, mes, anio int
		hora, min      int
	}{
		{"25/12/2026 09:30", 25, 12, 2026, 9, 30},
		{"25/12/2026", 25, 12, 2026, 0, 0},
		{"25-12-2026 09:30", 25, 12, 2026, 9, 30},
		{"2026-12-25 09:30", 25, 12, 2026, 9, 30},
		{"2026-12-25", 25, 12, 2026, 0, 0},
	}
	for _, c := range casos {
		got, err := fecha(c.entrada)
		if err != nil {
			t.Errorf("fecha(%q) devolvió error: %v", c.entrada, err)
			continue
		}
		if got.Day() != c.dia || int(got.Month()) != c.mes || got.Year() != c.anio ||
			got.Hour() != c.hora || got.Minute() != c.min {
			t.Errorf("fecha(%q) = %v, quiero %02d/%02d/%d %02d:%02d",
				c.entrada, got, c.dia, c.mes, c.anio, c.hora, c.min)
		}
	}
	if _, err := fecha("mañana"); err == nil {
		t.Error("fecha(\"mañana\") debería fallar")
	}
}

// Lo que Integra escribe tiene que poder volver a leerlo Integra: si el
// círculo no cierra, el operador descarga una plantilla que su propio sistema
// rechaza.
func TestGenerarYLeerCierranElCirculo(t *testing.T) {
	precio := 129900.0
	promo := 99900.0
	inicia := time.Date(2026, 12, 20, 8, 0, 0, 0, time.Local)
	termina := time.Date(2026, 12, 27, 23, 59, 0, 0, time.Local)

	var buf bytes.Buffer
	err := Generar(&buf, []Fila{
		{SKU: "ABC-123", Nombre: "Teclado mecánico", Marca: "AON", Stock: 12, Precio: &precio,
			PromoCanal: "mercadolibre", PromoPrecio: &promo,
			PromoInicia: &inicia, PromoTermina: &termina},
		{SKU: "SIN-PROMO", Nombre: "Ratón", Marca: "AON", Stock: 3, Precio: &precio},
	})
	if err != nil {
		t.Fatalf("generando: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("la plantilla salió vacía")
	}

	filas, problemas, err := Leer(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("leyendo lo que acabamos de generar: %v", err)
	}
	if len(problemas) != 0 {
		t.Fatalf("la plantilla recién generada tiene errores: %+v", problemas)
	}
	// Solo la fila con promoción trae datos editables; la otra tiene el precio
	// nuevo vacío a propósito y por tanto no se procesa.
	if len(filas) != 1 {
		t.Fatalf("filas con datos = %d, quiero 1: %+v", len(filas), filas)
	}
	f := filas[0]
	if f.SKU != "ABC-123" {
		t.Errorf("SKU = %q", f.SKU)
	}
	if f.PromoCanal != "mercadolibre" {
		t.Errorf("canal = %q", f.PromoCanal)
	}
	if f.PromoPrecio == nil || *f.PromoPrecio != promo {
		t.Errorf("precio promoción = %v, quiero %v", f.PromoPrecio, promo)
	}
	if f.PromoInicia == nil || !f.PromoInicia.Equal(inicia) {
		t.Errorf("inicia = %v, quiero %v", f.PromoInicia, inicia)
	}
	if f.PromoTermina == nil || !f.PromoTermina.Equal(termina) {
		t.Errorf("termina = %v, quiero %v", f.PromoTermina, termina)
	}
	// El precio nuevo va vacío en la plantilla generada: rellenarlo haría que
	// subirla sin tocar nada reescribiera el catálogo entero.
	if f.Precio != nil {
		t.Errorf("precio nuevo = %v; la plantilla generada debe traerlo vacío", *f.Precio)
	}
}

// Una promoción a la que le falta la mitad de los datos no se puede aplicar, y
// aplicar la mitad sería peor: hay que rechazar la fila y decir por qué.
func TestPromocionIncompletaSeRechaza(t *testing.T) {
	casos := []struct {
		nombre string
		fila   []any
		espera string
	}{
		{"sin canal", []any{"ABC-1", "n", "m", 1, 100.0, nil, "", 90.0, nil, nil}, "canal"},
		{"sin precio", []any{"ABC-1", "n", "m", 1, 100.0, nil, "mercadolibre", nil, nil, nil}, "precio de promoción"},
		{"termina antes", []any{"ABC-1", "n", "m", 1, 100.0, nil, "mercadolibre", 90.0,
			"25/12/2026 10:00", "24/12/2026 10:00"}, "antes de empezar"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			buf := hojaCruda(t, c.fila)
			filas, problemas, err := Leer(bytes.NewReader(buf))
			if err != nil {
				t.Fatalf("leyendo: %v", err)
			}
			if len(filas) != 0 {
				t.Errorf("la fila defectuosa se aceptó: %+v", filas)
			}
			if len(problemas) == 0 {
				t.Fatal("no se reportó ningún problema")
			}
			if !contiene(problemas, c.espera) {
				t.Errorf("problemas = %+v; esperaba que mencionaran %q", problemas, c.espera)
			}
		})
	}
}

// Una fila sin nada editado es la mayoría del archivo y no debe generar ni
// cambios ni errores.
func TestFilasSinEditarSeIgnoran(t *testing.T) {
	buf := hojaCruda(t, []any{"ABC-1", "nombre", "marca", 5, 100.0, nil, "", nil, nil, nil})
	filas, problemas, err := Leer(bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("leyendo: %v", err)
	}
	if len(filas) != 0 || len(problemas) != 0 {
		t.Errorf("filas=%+v problemas=%+v; ambas deberían estar vacías", filas, problemas)
	}
}

func TestArchivoQueNoEsLaPlantillaSeRechazaConClaridad(t *testing.T) {
	_, _, err := Leer(bytes.NewReader([]byte("esto no es un xlsx")))
	if err == nil {
		t.Fatal("un archivo que no es Excel debería fallar")
	}
}

func contiene(ps []Problema, txt string) bool {
	for _, p := range ps {
		if bytes.Contains([]byte(p.Mensaje), []byte(txt)) {
			return true
		}
	}
	return false
}

// hojaCruda arma un .xlsx con los encabezados reales y una única fila de
// datos, para poder probar entradas que la plantilla generada nunca produce.
func hojaCruda(t *testing.T, valores []any) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	idx, err := f.NewSheet(Hoja)
	if err != nil {
		t.Fatalf("creando hoja: %v", err)
	}
	f.SetActiveSheet(idx)

	for i, c := range columnas {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetCellStr(Hoja, col+"1", c.titulo); err != nil {
			t.Fatalf("encabezado: %v", err)
		}
	}
	for i, v := range valores {
		if v == nil {
			continue
		}
		col, _ := excelize.ColumnNumberToName(i + 1)
		if err := f.SetCellValue(Hoja, col+"2", v); err != nil {
			t.Fatalf("celda: %v", err)
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatalf("escribiendo: %v", err)
	}
	return buf.Bytes()
}
