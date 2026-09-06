package api

import (
	"bytes"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdv/integra/internal/plantilla"
	"github.com/mdv/integra/internal/store"
)

// La carga por plantilla promete «todo o nada»: si una fila no se puede
// aplicar, no se aplica ninguna. Como cada escritura se confirma por su cuenta,
// esa promesa solo se sostiene si la validación caza TODO lo que la base puede
// rechazar antes de empezar a escribir. Estas pruebas cubren los rechazos que
// se colaban hasta la mitad del bucle.

func precio(v float64) *float64 { return &v }

func fecha(t *testing.T, s string) *time.Time {
	t.Helper()
	f, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		t.Fatalf("fecha de prueba mal escrita: %v", err)
	}
	return &f
}

func catalogoDePrueba() (map[string]store.VarianteResuelta, map[string]int64) {
	return map[string]store.VarianteResuelta{
			"SKU-1": {ID: 11, Nombre: "Tableta 10", Precio: precio(100000)},
			"SKU-2": {ID: 22, Nombre: "Tableta 12", Precio: precio(200000)},
		}, map[string]int64{
			"mercadolibre": 7,
		}
}

// El caso del informe: se reutiliza la plantilla del mes pasado, con «empieza»
// vacío y «termina» ya pasado. La lectura del archivo no lo ve porque solo
// compara las dos fechas cuando están las dos, así que el simulacro salía
// limpio y GuardarOferta reventaba en la fila N con medio catálogo ya
// reprecificado.
func TestUnaPromocionCaducadaSeRechazaAntesDeEscribirNada(t *testing.T) {
	variantes, cuentas := catalogoDePrueba()
	ahora := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	filas := []plantilla.FilaLeida{
		{Numero: 2, SKU: "SKU-1", Precio: precio(150000)},
		{Numero: 3, SKU: "SKU-2", Precio: precio(250000),
			PromoCanal: "mercadolibre", PromoPrecio: precio(180000),
			PromoTermina: fecha(t, "2026-08-31 23:59")},
	}

	listos, problemas := planificarPlantilla(filas, variantes, cuentas, ahora)

	if len(problemas) != 1 || problemas[0].Fila != 3 {
		t.Fatalf("la fila con la promoción ya terminada tenía que salir como problema: %+v", problemas)
	}
	if !strings.Contains(problemas[0].Mensaje, "termina") {
		t.Errorf("el mensaje no explica qué pasa con las fechas: %q", problemas[0].Mensaje)
	}
	// La fila mala no puede llegar a la lista de escritura; y con un problema
	// pendiente, resolverPlantilla tampoco escribe la buena.
	for _, p := range listos {
		if p.cambio.Fila == 3 {
			t.Fatal("la fila que la base va a rechazar no puede quedar lista para escribirse")
		}
	}
}

// El precio negativo lo rechaza ActualizarPrecio en la base. Si llega hasta
// allí, las filas anteriores ya están confirmadas.
func TestElPrecioNegativoSeCazaEnLaValidacion(t *testing.T) {
	variantes, cuentas := catalogoDePrueba()

	listos, problemas := planificarPlantilla([]plantilla.FilaLeida{
		{Numero: 2, SKU: "SKU-1", Precio: precio(-1)},
	}, variantes, cuentas, time.Now())

	if len(listos) != 0 {
		t.Fatalf("un precio negativo no puede quedar listo para escribirse: %+v", listos)
	}
	if len(problemas) != 1 || !strings.Contains(problemas[0].Mensaje, "negativo") {
		t.Fatalf("faltó el problema del precio negativo: %+v", problemas)
	}
}

// La fecha de inicio se decide al validar y se guarda con la fila: si se
// recalculara al escribir, la vigencia comprobada no sería la que acaba en la
// base.
func TestLaPromocionSinFechaDeInicioArrancaConLaDeLaValidacion(t *testing.T) {
	variantes, cuentas := catalogoDePrueba()
	ahora := time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)

	listos, problemas := planificarPlantilla([]plantilla.FilaLeida{
		{Numero: 2, SKU: "SKU-1", PromoCanal: "mercadolibre", PromoPrecio: precio(80000),
			PromoTermina: fecha(t, "2026-09-30 23:59")},
	}, variantes, cuentas, ahora)

	if len(problemas) != 0 {
		t.Fatalf("esta fila es correcta y no debía dar problemas: %+v", problemas)
	}
	if len(listos) != 1 {
		t.Fatalf("la fila correcta tenía que quedar lista: %+v", listos)
	}
	if !listos[0].inicia.Equal(ahora) {
		t.Errorf("la promoción debía arrancar en %s y arranca en %s", ahora, listos[0].inicia)
	}
	if listos[0].cuentaID != 7 {
		t.Errorf("la cuenta del canal no se resolvió: %+v", listos[0])
	}
}

// Lo que ya funcionaba tiene que seguir funcionando: un canal sin cuenta
// conectada y una rebaja que no rebaja siguen siendo problemas de fila.
func TestSiguenRechazandoseElCanalDesconocidoYLaRebajaQueNoRebaja(t *testing.T) {
	variantes, cuentas := catalogoDePrueba()

	listos, problemas := planificarPlantilla([]plantilla.FilaLeida{
		{Numero: 2, SKU: "SKU-1", PromoCanal: "shopify", PromoPrecio: precio(80000)},
		{Numero: 3, SKU: "SKU-2", PromoCanal: "mercadolibre", PromoPrecio: precio(300000)},
		{Numero: 4, SKU: "NO-EXISTE", Precio: precio(1000)},
	}, variantes, cuentas, time.Now())

	if len(listos) != 0 {
		t.Fatalf("ninguna de las tres filas era aplicable: %+v", listos)
	}
	if len(problemas) != 3 {
		t.Fatalf("se esperaban tres problemas, uno por fila: %+v", problemas)
	}
}

// El tope de tamaño de la subida.
//
// ParseMultipartForm solo limitaba la memoria: el resto del cuerpo se escribía
// en disco, así que una subida de gigabytes llenaba el disco del servidor antes
// de que nadie mirara el tamaño declarado del archivo.
func TestUnaSubidaMasGrandeQueElTopeSeCortaConUn413(t *testing.T) {
	anterior := limiteCuerpoPlantilla
	limiteCuerpoPlantilla = 512
	t.Cleanup(func() { limiteCuerpoPlantilla = anterior })

	cuerpo, tipo := subidaDePrueba(t, "precios.xlsx", bytes.Repeat([]byte("x"), 4096))
	r := httptest.NewRequest(http.MethodPost, "/api/plantillas/precios?aplicar=true", cuerpo)
	r.Header.Set("Content-Type", tipo)
	w := httptest.NewRecorder()

	// Sin store: el corte ocurre al leer el cuerpo, mucho antes de tocar la
	// base. Si el servidor llegara a consultarla, esta prueba reventaría.
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.cargarPlantilla(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("se esperaba 413 y llegó %d: %s", w.Code, w.Body.String())
	}
}

// Por debajo del tope el archivo se lee y falla por lo que es: no es un Excel.
// Así se comprueba que el tope no se come las subidas legítimas.
func TestUnaSubidaDentroDelTopePasaDelLimiteYFallaPorElContenido(t *testing.T) {
	cuerpo, tipo := subidaDePrueba(t, "precios.xlsx", []byte("esto no es un xlsx"))
	r := httptest.NewRequest(http.MethodPost, "/api/plantillas/precios", cuerpo)
	r.Header.Set("Content-Type", tipo)
	w := httptest.NewRecorder()

	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.cargarPlantilla(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("se esperaba 400 por el contenido y llegó %d: %s", w.Code, w.Body.String())
	}
}

func subidaDePrueba(t *testing.T, nombre string, datos []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	m := multipart.NewWriter(&buf)
	parte, err := m.CreateFormFile("archivo", nombre)
	if err != nil {
		t.Fatalf("armando el formulario: %v", err)
	}
	if _, err := parte.Write(datos); err != nil {
		t.Fatalf("escribiendo el archivo de prueba: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("cerrando el formulario: %v", err)
	}
	return &buf, m.FormDataContentType()
}
