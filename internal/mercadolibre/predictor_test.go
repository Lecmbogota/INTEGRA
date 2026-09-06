package mercadolibre

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// respuestaReal es la que devolvió el servicio para un producto de MDV.
const respuestaReal = `[
 {"domain_id":"MCO-RAM_MEMORY_MODULES","domain_name":"Memorias RAM",
  "category_id":"MCO441773","category_name":"Memorias RAM de Portátiles",
  "attributes":[{"id":"RAM_MEMORY_MODULE_TOTAL_CAPACITY","name":"Capacidad total",
                 "value_id":"18614","value_name":"16 GB"}]},
 {"domain_id":"MCO-RAM_MEMORY_MODULES","domain_name":"Memorias RAM",
  "category_id":"MCO1694","category_name":"Memorias RAM","attributes":[]},
 {"domain_id":"MCO-MEMORY_CARDS","domain_name":"Tarjetas de memoria",
  "category_id":"MCO7908","category_name":"Memorias",
  "attributes":[{"id":"BRAND","name":"Marca","value_id":"16360","value_name":"Kingston"}]}
]`

func servidorFalso(t *testing.T, estado int, cuerpo string) *Predictor {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Se comprueba que la consulta llega bien formada.
		if q := r.URL.Query().Get("q"); q == "" {
			t.Errorf("falta el parámetro q en %s", r.URL)
		}
		if !strings.Contains(r.URL.Path, "/domain_discovery/search") {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(estado)
		_, _ = w.Write([]byte(cuerpo))
	}))
	t.Cleanup(srv.Close)

	anterior := baseAPI
	baseAPI = srv.URL
	t.Cleanup(func() { baseAPI = anterior })

	return NuevoPredictor(SitioColombia)
}

func TestPredecir(t *testing.T) {
	p := servidorFalso(t, http.StatusOK, respuestaReal)

	sug, err := p.Predecir(context.Background(), "Memoria RAM Kingston 16GB DDR5-5600 UDIMM", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(sug) != 3 {
		t.Fatalf("se esperaban 3 sugerencias, hay %d", len(sug))
	}

	// El orden importa: MercadoLibre las devuelve de más a menos probable.
	if sug[0].CategoriaID != "MCO441773" {
		t.Errorf("la primera debería ser MCO441773, fue %s", sug[0].CategoriaID)
	}
	if sug[0].Categoria != "Memorias RAM de Portátiles" {
		t.Errorf("nombre = %q", sug[0].Categoria)
	}

	// Los atributos deducidos son la mitad del valor: llegan con el id que
	// espera la API al publicar.
	if len(sug[0].Atributos) != 1 {
		t.Fatalf("se esperaba 1 atributo, hay %d", len(sug[0].Atributos))
	}
	a := sug[0].Atributos[0]
	if a.ID != "RAM_MEMORY_MODULE_TOTAL_CAPACITY" || a.ValorNombre != "16 GB" {
		t.Errorf("atributo = %+v", a)
	}
}

func TestPredecirTituloVacio(t *testing.T) {
	p := servidorFalso(t, http.StatusOK, respuestaReal)
	if _, err := p.Predecir(context.Background(), "   ", 3); err == nil {
		t.Fatal("un título vacío debería rechazarse antes de llamar al servicio")
	}
}

func TestPredecirSinResultados(t *testing.T) {
	p := servidorFalso(t, http.StatusOK, `[]`)
	_, err := p.Predecir(context.Background(), "xyzzy qwerty", 3)
	if !errors.Is(err, ErrSinSugerencia) {
		t.Fatalf("se esperaba ErrSinSugerencia, se obtuvo %v", err)
	}
}

func TestPredecirErrorDelServicio(t *testing.T) {
	p := servidorFalso(t, http.StatusForbidden,
		`{"status":403,"message":"At least one policy returned UNAUTHORIZED."}`)

	_, err := p.Predecir(context.Background(), "Memoria RAM", 3)
	if err == nil {
		t.Fatal("un 403 debería dar error")
	}
	// El mensaje debe llevar el cuerpo: es lo que permite distinguir un
	// bloqueo por política de una caída del servicio.
	if !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "UNAUTHORIZED") {
		t.Errorf("el error debería incluir estado y cuerpo: %v", err)
	}
}

func TestPredecirRespuestaIlegible(t *testing.T) {
	p := servidorFalso(t, http.StatusOK, `esto no es json`)
	if _, err := p.Predecir(context.Background(), "algo", 3); err == nil {
		t.Fatal("una respuesta no-JSON debería dar error")
	}
}

func TestPredecirRespetaCancelacion(t *testing.T) {
	p := servidorFalso(t, http.StatusOK, respuestaReal)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.Predecir(ctx, "Memoria RAM", 3); err == nil {
		t.Fatal("con el contexto cancelado no debería completarse")
	}
}

func TestConfianza(t *testing.T) {
	conAtributos := Sugerencia{Atributos: []AtributoSugerido{{}, {}}}
	sinAtributos := Sugerencia{}

	// La primera posición vale más que la segunda.
	if Confianza(sinAtributos, 0) <= Confianza(sinAtributos, 1) {
		t.Error("la primera sugerencia debería tener más confianza que la segunda")
	}
	// Deducir atributos es señal de que entendió el producto.
	if Confianza(conAtributos, 0) <= Confianza(sinAtributos, 0) {
		t.Error("los atributos deducidos deberían subir la confianza")
	}
	// Ninguna sugerencia automática merece certeza absoluta.
	muchos := Sugerencia{Atributos: make([]AtributoSugerido, 50)}
	if c := Confianza(muchos, 0); c >= 1.0 {
		t.Errorf("la confianza no debería llegar a 1,0: %v", c)
	}
}
