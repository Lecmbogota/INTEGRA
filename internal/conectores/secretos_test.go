package conectores

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// El mensaje de error de una prueba de conexión no se queda en la pantalla:
// server.go lo guarda en channel_accounts.config.probada_msg y ListarCuentas
// lo devuelve a cualquier sesión. Si lleva la credencial dentro, el secreto
// queda en claro en la base, al lado de la columna cifrada, y visible para un
// rol de solo lectura.

func TestUnFalloDeRedNoFiltraLasClavesDeWoo(t *testing.T) {
	// Un host que no resuelve: net/http devuelve *url.Error, cuyo Error()
	// lleva la URL entera. Es el caso corriente de una URL mal escrita.
	_, _, err := Probar(context.Background(), "woocommerce", Credenciales{
		URL:            "https://tienda-que-no-existe.invalid",
		ConsumerKey:    "ck_clave_publica",
		ConsumerSecret: "cs_SECRETO_QUE_NO_DEBE_SALIR",
	})
	if err == nil {
		t.Fatal("se esperaba un fallo de red")
	}
	for _, secreto := range []string{"cs_SECRETO_QUE_NO_DEBE_SALIR", "ck_clave_publica"} {
		if strings.Contains(err.Error(), secreto) {
			t.Errorf("el error filtra %q; mensaje: %s", secreto, err)
		}
	}
}

// sinURL se prueba aparte: con WooCommerce ya autenticando por cabecera, su
// URL no lleva secretos, así que la prueba de arriba pasaría igual sin este
// filtro. Sigue haciendo falta porque cualquier canal que ponga algo
// sensible en la dirección acabaría con ello en la base y en el panel.
func TestSinURLConservaLaCausaYTiraLaDireccion(t *testing.T) {
	original := &url.Error{
		Op:  "Get",
		URL: "https://tienda.example/wp-json/wc/v3/products?consumer_secret=cs_SECRETO",
		Err: errors.New("dial tcp: lookup tienda.example: no such host"),
	}
	limpio := sinURL(original)

	if strings.Contains(limpio.Error(), "cs_SECRETO") {
		t.Errorf("sigue filtrando el secreto: %s", limpio)
	}
	if !strings.Contains(limpio.Error(), "no such host") {
		t.Errorf("se perdió la causa, que es lo único útil para diagnosticar: %s", limpio)
	}
	if !errors.Is(limpio, original.Err) {
		t.Error("la causa debe seguir siendo desenvolvible con errors.Is")
	}
}

func TestWooExigeHTTPS(t *testing.T) {
	_, _, err := Probar(context.Background(), "woocommerce", Credenciales{
		URL:            "http://tienda.example",
		ConsumerKey:    "ck",
		ConsumerSecret: "cs",
	})
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("sobre http hay que rechazar la conexión, no mandar el secreto: %v", err)
	}
}

func TestWooMandaLasClavesEnLaCabeceraNoEnLaURL(t *testing.T) {
	var consulta, usuario, clave string
	var teniaBasic bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		consulta = r.URL.RawQuery
		usuario, clave, teniaBasic = r.BasicAuth()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	viejo := cliente
	cliente = srv.Client()
	defer func() { cliente = viejo }()

	if _, _, err := Probar(context.Background(), "woocommerce", Credenciales{
		URL: srv.URL, ConsumerKey: "ck_x", ConsumerSecret: "cs_x",
	}); err != nil {
		t.Fatal(err)
	}
	if !teniaBasic || usuario != "ck_x" || clave != "cs_x" {
		t.Errorf("las claves deben ir por autenticación básica: basic=%v u=%q", teniaBasic, usuario)
	}
	if strings.Contains(consulta, "consumer_secret") || strings.Contains(consulta, "cs_x") {
		t.Errorf("el secreto no puede viajar en la URL: %q", consulta)
	}
}

// La firma del Seller Center se recalcula en el servidor: si Integra codifica
// distinto, la respuesta es "firma inválida" y no dice por qué.
func TestLaFirmaCodificaElEspacioComoRFC3986(t *testing.T) {
	firmada := FirmarFalabella(map[string]string{
		"Action": "GetProducts",
		"Filter": "marca con espacios",
	}, "clave")

	if strings.Contains(firmada, "+") {
		t.Errorf("el espacio debe ir como %%20, no como '+': %s", firmada)
	}
	if !strings.Contains(firmada, "marca%20con%20espacios") {
		t.Errorf("falta la codificación RFC 3986 del espacio: %s", firmada)
	}
}
