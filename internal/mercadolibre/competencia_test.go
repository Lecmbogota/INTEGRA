package mercadolibre

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servidorBusqueda imita /sites/{site}/search tal como responde ML: sin la
// cabecera Authorization devuelve 403 forbidden, que es lo que se comprobó
// contra la API real.
func servidorBusqueda(t *testing.T, visto *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sites/MCO/search", func(w http.ResponseWriter, r *http.Request) {
		*visto = r.Header.Get("Authorization")
		if !strings.HasPrefix(*visto, "Bearer ") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"forbidden","error":"forbidden","status":403,"cause":[]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{
			"title":"Impresora térmica 80mm","price":189900,"currency_id":"COP",
			"permalink":"https://articulo.mercadolibre.com.co/MCO-1",
			"condition":"new","seller":{"nickname":"OTRA TIENDA"},
			"shipping":{"free_shipping":true}}]}`)
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	viejo := baseAPI
	baseAPI = s.URL
	t.Cleanup(func() { baseAPI = viejo })
	return s
}

// TestBuscarCompetenciaMandaElTokenDeLaCuenta es la prueba de que la búsqueda
// llega autenticada.
//
// «Ítems y Búsquedas» documenta /sites/$SITE_ID/search siempre con
// «-H 'Authorization: Bearer $ACCESS_TOKEN'»; sin esa cabecera ML responde 403
// y el módulo de competencia no devolvía un solo resultado nunca.
func TestBuscarCompetenciaMandaElTokenDeLaCuenta(t *testing.T) {
	var visto string
	servidorBusqueda(t, &visto)

	b := NuevoBuscadorCompetencia(SitioColombia, ConToken(
		func(context.Context) (string, error) { return "APP_USR-1", nil }))

	items, err := b.Buscar(context.Background(), "7898095297749", 8)
	if err != nil {
		t.Fatalf("con token la búsqueda debía funcionar: %v", err)
	}
	if visto != "Bearer APP_USR-1" {
		t.Errorf("Authorization = %q; ML exige el token en la cabecera", visto)
	}
	if len(items) != 1 {
		t.Fatalf("resultados = %d", len(items))
	}
	c := items[0]
	if c.Titulo != "Impresora térmica 80mm" || c.Precio != 189900 || c.Moneda != "COP" ||
		c.Vendedor != "OTRA TIENDA" || c.Condicion != "new" || !c.EnvioFree {
		t.Errorf("competidor = %+v", c)
	}
}

// TestBuscarCompetenciaSinCuentaNoGastaLaLlamada: sin token la respuesta de ML
// es un 403 seguro, así que no vale la pena hacer la petición.
func TestBuscarCompetenciaSinCuentaNoGastaLaLlamada(t *testing.T) {
	visto := "sin tocar"
	servidorBusqueda(t, &visto)

	_, err := NuevoBuscadorCompetencia(SitioColombia).
		Buscar(context.Background(), "7898095297749", 8)
	if err == nil || !strings.Contains(err.Error(), "conectar la cuenta") {
		t.Fatalf("error = %v; debía decir que hace falta conectar la cuenta", err)
	}
	if visto != "sin tocar" {
		t.Error("no se debe llamar a ML sabiendo que va a responder 403")
	}
}

// TestBuscarCompetenciaConTokenRechazadoLoDistingue: si el token sí viajó y
// ML lo rechaza, el problema es la cuenta, no la falta de credenciales.
func TestBuscarCompetenciaConTokenRechazadoLoDistingue(t *testing.T) {
	var visto string
	servidorBusqueda(t, &visto)

	_, err := NuevoBuscadorCompetencia(SitioColombia, ConToken(
		func(context.Context) (string, error) { return "", nil })).
		Buscar(context.Background(), "algo", 8)
	if err == nil || !strings.Contains(err.Error(), "conectar la cuenta") {
		t.Fatalf("un token vacío es una cuenta sin conectar: %v", err)
	}
}
