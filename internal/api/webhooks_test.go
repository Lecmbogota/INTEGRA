package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// Estas pruebas no tocan la base ni la red: el manejador de webhooks recibe
// sus dependencias inyectadas, así que se puede comprobar lo único que de
// verdad protege el endpoint —la verificación por canal— y lo único que hace
// —encolar la ingesta de esa cuenta.

// espia sustituye a la base y a la cola.
type espia struct {
	cuentaID int64
	cred     map[string]string
	errCta   error

	mu        sync.Mutex
	encolados []int64
	errEncole error
}

func (e *espia) anotar(id int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.encolados = append(e.encolados, id)
	return e.errEncole
}

func (e *espia) vistos() []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int64(nil), e.encolados...)
}

// manejadorDePrueba arma el endpoint con el espía detrás. `fondo` corre en el
// acto: lo que se prueba aquí es qué se encola, no cuándo (para el cuándo
// está TestLaRespuestaNoEsperaAlEncolado).
func manejadorDePrueba(e *espia, registro *bytes.Buffer) http.Handler {
	salida := slog.New(slog.NewTextHandler(registro, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := &manejadorWebhooks{
		log: salida,
		cuenta: func(context.Context, string) (int64, map[string]string, error) {
			if e.errCta != nil {
				return 0, nil, e.errCta
			}
			return e.cuentaID, e.cred, nil
		},
		encolar: func(_ context.Context, id int64) error { return e.anotar(id) },
		fondo:   func(f func()) { f() },
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/webhooks/{canal}", m.recibir)
	return mux
}

// llamar dispara un webhook y devuelve el código de respuesta.
func llamar(t *testing.T, h http.Handler, ruta string, cuerpo []byte, cabeceras map[string]string) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, ruta, bytes.NewReader(cuerpo))
	for k, v := range cabeceras {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// firmar reproduce lo que hacen Shopify y WooCommerce: HMAC-SHA256 del cuerpo
// crudo, en base64.
func firmar(secreto string, cuerpo []byte) string {
	mac := hmac.New(sha256.New, []byte(secreto))
	mac.Write(cuerpo)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// ------------------------------------------------------------------ Shopify

func TestShopifyConFirmaValidaEncolaLaIngesta(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s3cr3t0"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":450789469,"line_items":[]}`)

	code := llamar(t, h, "/api/webhooks/shopify", cuerpo, map[string]string{
		"X-Shopify-Topic":       "orders/create",
		"X-Shopify-Hmac-Sha256": firmar("s3cr3t0", cuerpo),
	})
	if code != http.StatusOK {
		t.Fatalf("una firma válida debía dar 200, dio %d", code)
	}
	if got := e.vistos(); len(got) != 1 || got[0] != 7 {
		t.Errorf("debía encolarse la ingesta de la cuenta 7, se encoló %v", got)
	}
}

// El caso que justifica todo el endpoint: sin verificación, cualquiera que
// conozca la URL pública encola trabajo contra la API del canal.
func TestShopifyConFirmaInvalidaNoEncolaNada(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s3cr3t0"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":450789469}`)

	casos := []struct {
		nombre string
		firma  string
	}{
		{"firmado con otro secreto", firmar("el-secreto-del-atacante", cuerpo)},
		{"firma de otro cuerpo", firmar("s3cr3t0", []byte(`{"id":1}`))},
		{"sin cabecera de firma", ""},
		{"basura que no es base64", "no-es-base64-@@@"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cab := map[string]string{"X-Shopify-Topic": "orders/create"}
			if c.firma != "" {
				cab["X-Shopify-Hmac-Sha256"] = c.firma
			}
			if code := llamar(t, h, "/api/webhooks/shopify", cuerpo, cab); code != http.StatusUnauthorized {
				t.Errorf("debía dar 401, dio %d", code)
			}
		})
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("no debía encolarse nada, se encoló %v", got)
	}
}

// Sin el secreto en la cuenta no hay nada con que comprobar la firma: se
// rechaza en vez de dejar pasar. Un endpoint público que falle abierto es un
// botón de "encólame trabajo" para cualquiera.
func TestShopifySinSecretoConfiguradoRechaza(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"token": "shpat_loquesea"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":1}`)

	code := llamar(t, h, "/api/webhooks/shopify", cuerpo, map[string]string{
		"X-Shopify-Hmac-Sha256": firmar("shpat_loquesea", cuerpo),
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("sin webhook_secret debía dar 401, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("no debía encolarse nada, se encoló %v", got)
	}
}

// Un webhook de producto es auténtico y no es un pedido: se contesta 200 (si
// no, el canal acaba desactivando el tópico) pero no se encola ingesta.
func TestShopifyConTopicoAjenoAPedidosNoEncola(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s3cr3t0"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":99}`)

	code := llamar(t, h, "/api/webhooks/shopify", cuerpo, map[string]string{
		"X-Shopify-Topic":       "products/update",
		"X-Shopify-Hmac-Sha256": firmar("s3cr3t0", cuerpo),
	})
	if code != http.StatusOK {
		t.Fatalf("debía contestar 200 igualmente, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("un cambio de producto no debe disparar ingesta de pedidos, encoló %v", got)
	}
}

// -------------------------------------------------------------- WooCommerce

func TestWooCommerceVerificaSuPropiaCabecera(t *testing.T) {
	e := &espia{cuentaID: 3, cred: map[string]string{"webhook_secret": "woo-secreto"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":727,"status":"processing"}`)

	// Con la firma en la cabecera de Shopify no vale: cada canal manda la
	// suya y confundirlas dejaría el endpoint abierto para el otro.
	code := llamar(t, h, "/api/webhooks/woocommerce", cuerpo, map[string]string{
		"X-WC-Webhook-Topic":    "order.created",
		"X-Shopify-Hmac-Sha256": firmar("woo-secreto", cuerpo),
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("sin X-WC-Webhook-Signature debía dar 401, dio %d", code)
	}

	code = llamar(t, h, "/api/webhooks/woocommerce", cuerpo, map[string]string{
		"X-WC-Webhook-Topic":     "order.created",
		"X-WC-Webhook-Signature": firmar("woo-secreto", cuerpo),
	})
	if code != http.StatusOK {
		t.Fatalf("una firma válida debía dar 200, dio %d", code)
	}
	if got := e.vistos(); len(got) != 1 || got[0] != 3 {
		t.Errorf("debía encolarse la cuenta 3, se encoló %v", got)
	}
}

// WooCommerce, cuando al webhook no se le pone secreto propio, firma con el
// consumer secret de la clave de API con la que se creó. Ese ya está guardado,
// así que este canal funciona sin añadir ninguna credencial nueva.
func TestWooCommerceCaeAlConsumerSecretSiNoHayWebhookSecret(t *testing.T) {
	e := &espia{cuentaID: 3, cred: map[string]string{
		"consumer_key": "ck_x", "consumer_secret": "cs_secreto"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"id":728}`)

	code := llamar(t, h, "/api/webhooks/woocommerce", cuerpo, map[string]string{
		"X-WC-Webhook-Topic":     "order.updated",
		"X-WC-Webhook-Signature": firmar("cs_secreto", cuerpo),
	})
	if code != http.StatusOK {
		t.Fatalf("debía aceptar la firma hecha con el consumer secret, dio %d", code)
	}
	if got := e.vistos(); len(got) != 1 {
		t.Errorf("debía encolarse una ingesta, se encoló %v", got)
	}
}

// ------------------------------------------------------------- MercadoLibre

func cuerpoML(topico, appID, userID string) []byte {
	return []byte(`{"_id":"f9f08571","resource":"/orders/2195160686","user_id":` + userID +
		`,"topic":"` + topico + `","application_id":` + appID + `,"attempts":1}`)
}

func TestMercadoLibreValidaApplicationIDyUserID(t *testing.T) {
	// application_id de 16 cifras: es el caso que rompe si se decodifica
	// como float64 en vez de conservar el literal.
	const app = "5503910054141466"
	const vendedor = "468424240"

	casos := []struct {
		nombre  string
		cred    map[string]string
		cuerpo  []byte
		code    int
		encolar bool
	}{
		{"de nuestra app y nuestro vendedor",
			map[string]string{"app_id": app, "user_id": vendedor},
			cuerpoML("orders_v2", app, vendedor), http.StatusOK, true},
		{"de otra aplicación",
			map[string]string{"app_id": app, "user_id": vendedor},
			cuerpoML("orders_v2", "9999999999999999", vendedor), http.StatusUnauthorized, false},
		{"de otro vendedor",
			map[string]string{"app_id": app, "user_id": vendedor},
			cuerpoML("orders_v2", app, "111111111"), http.StatusUnauthorized, false},
		{"sin app_id en la cuenta no hay nada que comprobar",
			map[string]string{"access_token": "APP_USR-x"},
			cuerpoML("orders_v2", app, vendedor), http.StatusUnauthorized, false},
		{"cuenta sin user_id: basta con la aplicación",
			map[string]string{"app_id": app},
			cuerpoML("orders_v2", app, vendedor), http.StatusOK, true},
		{"cuerpo que no es JSON",
			map[string]string{"app_id": app},
			[]byte("no soy json"), http.StatusUnauthorized, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			e := &espia{cuentaID: 11, cred: c.cred}
			h := manejadorDePrueba(e, &bytes.Buffer{})
			if code := llamar(t, h, "/api/webhooks/mercadolibre", c.cuerpo, nil); code != c.code {
				t.Fatalf("esperaba %d, dio %d", c.code, code)
			}
			if hubo := len(e.vistos()) > 0; hubo != c.encolar {
				t.Errorf("encolado=%v, esperaba %v", hubo, c.encolar)
			}
		})
	}
}

// El grueso de las notificaciones de MercadoLibre son de ítems y precios, y
// las provocamos nosotros al publicar: encolar una ingesta de pedidos por cada
// una sería ruido puro.
func TestMercadoLibreSoloDisparaConTopicosDeVenta(t *testing.T) {
	const app = "5503910054141466"
	for topico, dispara := range map[string]bool{
		"orders_v2": true, "created_orders": true,
		"items": false, "items_prices": false, "shipments": false, "questions": false,
	} {
		e := &espia{cuentaID: 11, cred: map[string]string{"app_id": app}}
		h := manejadorDePrueba(e, &bytes.Buffer{})
		code := llamar(t, h, "/api/webhooks/mercadolibre", cuerpoML(topico, app, "468424240"), nil)
		// Siempre 200: un 4xx repetido hace que MercadoLibre desactive el
		// tópico y se pierdan las notificaciones de ese período.
		if code != http.StatusOK {
			t.Errorf("%s: esperaba 200, dio %d", topico, code)
		}
		if hubo := len(e.vistos()) > 0; hubo != dispara {
			t.Errorf("%s: encolado=%v, esperaba %v", topico, hubo, dispara)
		}
	}
}

// ---------------------------------------------------------------- Falabella

// Falabella no documenta ninguna firma para su callback, así que lo que se
// comprueba es un token propio que viaja en la URL que nosotros registramos.
// Sin token configurado, el endpoint no dispara nada.
func TestFalabellaExigeElTokenDeLaCuenta(t *testing.T) {
	const evento = `{"event":"onOrderCreated","payload":{"OrderId":190}}`

	casos := []struct {
		nombre  string
		cred    map[string]string
		ruta    string
		cab     map[string]string
		code    int
		encolar bool
	}{
		{"token correcto en la URL", map[string]string{"webhook_secret": "tok-integra"},
			"/api/webhooks/falabella?token=tok-integra", nil, http.StatusOK, true},
		{"token correcto en la cabecera", map[string]string{"webhook_secret": "tok-integra"},
			"/api/webhooks/falabella", map[string]string{"X-Integra-Webhook-Token": "tok-integra"},
			http.StatusOK, true},
		{"token equivocado", map[string]string{"webhook_secret": "tok-integra"},
			"/api/webhooks/falabella?token=otro", nil, http.StatusUnauthorized, false},
		{"sin token en la petición", map[string]string{"webhook_secret": "tok-integra"},
			"/api/webhooks/falabella", nil, http.StatusUnauthorized, false},
		{"la cuenta no tiene token: nada que comprobar", map[string]string{"api_key": "k"},
			"/api/webhooks/falabella?token=loquesea", nil, http.StatusUnauthorized, false},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			e := &espia{cuentaID: 4, cred: c.cred}
			h := manejadorDePrueba(e, &bytes.Buffer{})
			if code := llamar(t, h, c.ruta, []byte(evento), c.cab); code != c.code {
				t.Fatalf("esperaba %d, dio %d", c.code, code)
			}
			if hubo := len(e.vistos()) > 0; hubo != c.encolar {
				t.Errorf("encolado=%v, esperaba %v", hubo, c.encolar)
			}
		})
	}
}

func TestFalabellaConEventoAjenoAPedidosNoEncola(t *testing.T) {
	e := &espia{cuentaID: 4, cred: map[string]string{"webhook_secret": "tok"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	cuerpo := []byte(`{"event":"onFeedCompleted","payload":{"FeedId":"abc"}}`)

	if code := llamar(t, h, "/api/webhooks/falabella?token=tok", cuerpo, nil); code != http.StatusOK {
		t.Fatalf("esperaba 200, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("un feed terminado no es un pedido, encoló %v", got)
	}
}

// ------------------------------------------------------------- transversales

func TestUnCanalDesconocidoNoLlegaAVerificarse(t *testing.T) {
	e := &espia{cuentaID: 1, cred: map[string]string{"webhook_secret": "x"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	if code := llamar(t, h, "/api/webhooks/amazon", []byte(`{}`), nil); code != http.StatusNotFound {
		t.Fatalf("esperaba 404, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("no debía encolarse nada, se encoló %v", got)
	}
}

// Sin cuenta conectada no hay secreto contra el que verificar: 401, no 500 ni
// un 200 mentiroso.
func TestSinCuentaConectadaSeRechaza(t *testing.T) {
	e := &espia{errCta: errSinCuenta}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	if code := llamar(t, h, "/api/webhooks/shopify", []byte(`{}`), nil); code != http.StatusUnauthorized {
		t.Fatalf("esperaba 401, dio %d", code)
	}
}

// Una base caída no es una firma inválida: se pide reintento (503) en vez de
// contestar 200 y perder el aviso para siempre.
func TestSiLaBaseNoContestaSePideReintento(t *testing.T) {
	e := &espia{errCta: errors.New("connection refused")}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	if code := llamar(t, h, "/api/webhooks/shopify", []byte(`{}`), nil); code != http.StatusServiceUnavailable {
		t.Fatalf("esperaba 503, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("no debía encolarse nada, se encoló %v", got)
	}
}

// El endpoint es público: un cuerpo enorme no puede quedarse en memoria ni
// obligar a calcular un HMAC sobre megabytes.
func TestUnCuerpoDesmesuradoSeCortaYNoEncola(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s"}}
	h := manejadorDePrueba(e, &bytes.Buffer{})
	enorme := bytes.Repeat([]byte("a"), maxCuerpoWebhook+1)

	code := llamar(t, h, "/api/webhooks/shopify", enorme, map[string]string{
		"X-Shopify-Hmac-Sha256": firmar("s", enorme),
	})
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("esperaba 413, dio %d", code)
	}
	if got := e.vistos(); len(got) != 0 {
		t.Errorf("no debía encolarse nada, se encoló %v", got)
	}
}

// La regla dura de MercadoLibre: 200 en menos de 500 ms o desactiva el tópico.
// Aquí el encolado tarda medio segundo largo a propósito; si el manejador lo
// esperara, la respuesta llegaría tarde y el tópico acabaría muerto.
func TestLaRespuestaNoEsperaAlEncolado(t *testing.T) {
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s3cr3t0"}}
	listo := make(chan struct{})
	salida := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	m := &manejadorWebhooks{
		log: salida,
		cuenta: func(context.Context, string) (int64, map[string]string, error) {
			return e.cuentaID, e.cred, nil
		},
		encolar: func(_ context.Context, id int64) error {
			time.Sleep(600 * time.Millisecond)
			err := e.anotar(id)
			close(listo)
			return err
		},
		// El de producción: en segundo plano.
		fondo: func(f func()) { go f() },
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/webhooks/{canal}", m.recibir)

	cuerpo := []byte(`{"id":1}`)
	inicio := time.Now()
	code := llamar(t, mux, "/api/webhooks/shopify", cuerpo, map[string]string{
		"X-Shopify-Topic":       "orders/create",
		"X-Shopify-Hmac-Sha256": firmar("s3cr3t0", cuerpo),
	})
	tardanza := time.Since(inicio)

	if code != http.StatusOK {
		t.Fatalf("esperaba 200, dio %d", code)
	}
	if tardanza > 200*time.Millisecond {
		t.Errorf("la respuesta tardó %v: MercadoLibre da 500 ms", tardanza)
	}
	select {
	case <-listo:
	case <-time.After(5 * time.Second):
		t.Fatal("la ingesta nunca se encoló")
	}
	if got := e.vistos(); len(got) != 1 {
		t.Errorf("debía encolarse una ingesta, se encoló %v", got)
	}
}

// El cuerpo de un webhook trae datos del comprador (nombre, dirección,
// correo). Se registra que llegó, no lo que traía.
func TestElLogNoVuelcaElCuerpo(t *testing.T) {
	var registro bytes.Buffer
	e := &espia{cuentaID: 7, cred: map[string]string{"webhook_secret": "s3cr3t0"}}
	h := manejadorDePrueba(e, &registro)
	cuerpo := []byte(`{"email":"comprador@ejemplo.com","shipping_address":{"address1":"Calle 93 #11-27"}}`)

	llamar(t, h, "/api/webhooks/shopify", cuerpo, map[string]string{
		"X-Shopify-Topic":       "orders/create",
		"X-Shopify-Hmac-Sha256": firmar("s3cr3t0", cuerpo),
	})

	texto := registro.String()
	for _, prohibido := range []string{"comprador@ejemplo.com", "Calle 93", "s3cr3t0"} {
		if strings.Contains(texto, prohibido) {
			t.Errorf("el log filtró %q:\n%s", prohibido, texto)
		}
	}
	if !strings.Contains(texto, "webhook recibido") || !strings.Contains(texto, "shopify") {
		t.Errorf("el log debía dejar constancia del webhook y su canal:\n%s", texto)
	}
}

// La exención de sesión del prefijo /api/webhooks/ es lo que hace que estas
// rutas sean alcanzables por los canales. Si alguien la quitara, el endpoint
// devolvería 401 a los cuatro canales sin que ninguna prueba de arriba se
// enterara: todas llaman al manejador sin pasar por el middleware.
func TestLasRutasDeWebhookEstanExentasDeSesion(t *testing.T) {
	for _, canal := range []string{"shopify", "woocommerce", "mercadolibre", "falabella"} {
		if !esPublica("/api/webhooks/" + canal) {
			t.Errorf("/api/webhooks/%s debería servirse sin sesión", canal)
		}
	}
	// Y solo ese prefijo: el resto de la API sigue exigiendo sesión.
	if esPublica("/api/ordenes") {
		t.Error("/api/ordenes no debe ser pública")
	}
}
