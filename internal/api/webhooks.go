package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/ordenes"
)

// Webhooks de los canales: un DISPARADOR, no un camino de datos.
//
// El manejador no consulta la API del canal ni escribe pedidos. Identifica la
// cuenta, comprueba que la llamada venga de verdad del canal, responde 200 y
// encola la ingesta que ya existe (ordenes.EncolarIngesta). El worker hace el
// resto por el camino normal, que ya es idempotente y ya sabe paginar,
// deduplicar por (cuenta, id externo) y montar el sale.order.
//
// El motivo de no traer aquí el pedido es doble. Uno, MercadoLibre exige
// responder 200 en menos de 500 ms o desactiva el tópico (MERCADOLIBRE.md §5):
// cualquier llamada a la API del canal dentro del manejador se sale del
// presupuesto en la primera red lenta. Y dos, un segundo camino de datos sería
// una segunda implementación de la deduplicación y del mapeo de líneas que
// habría que mantener sincronizada con la del sondeo; el webhook solo adelanta
// la llegada de minutos a segundos.

// maxCuerpoWebhook acota lo que se lee de un endpoint público.
//
// El cuerpo solo se usa para calcular el HMAC y para leer el tópico, así que
// no hay motivo para aceptar megabytes de nadie. 512 KiB deja holgura al
// pedido de Shopify con muchas líneas —el mayor de los cuatro— y un cuerpo
// más grande que eso se rechaza con 413: si alguna vez fuera legítimo, el
// pedido llega igual por el sondeo, que es el camino que el webhook adelanta.
const maxCuerpoWebhook = 512 << 10

// plazoEncolado acota el trabajo posterior a la respuesta. No puede heredar
// el contexto de la petición: ese se cancela en cuanto el manejador retorna,
// que es justo lo que hacemos antes de encolar.
const plazoEncolado = 30 * time.Second

// errSinCuenta distingue "ese canal no tiene cuenta conectada" de un fallo de
// la base. Lo primero es 401 (no hay contra qué verificar la firma, y
// reintentar no lo va a arreglar); lo segundo es 503.
var errSinCuenta = errors.New("el canal no tiene ninguna cuenta conectada")

// manejadorWebhooks es el endpoint público, con sus dependencias inyectadas.
//
// Se separa del Server para poder probar la verificación y el disparo sin
// base de datos: los cuatro mecanismos de autenticidad son lo único que
// impide que cualquiera encole trabajo, y merecen prueba propia.
type manejadorWebhooks struct {
	log *slog.Logger
	// cuenta resuelve el canal de la ruta a la cuenta conectada y a sus
	// credenciales en claro (el secreto de firma vive ahí dentro).
	cuenta func(ctx context.Context, canal string) (cuentaID int64, cred map[string]string, err error)
	// encolar dispara la ingesta normal de pedidos de esa cuenta.
	encolar func(ctx context.Context, cuentaID int64) error
	// fondo ejecuta lo que va DESPUÉS de responder. En producción es `go f()`;
	// las pruebas lo sustituyen por una ejecución en el acto para no depender
	// de tiempos.
	fondo func(func())
}

func (s *Server) registrarWebhooks(mux *http.ServeMux) {
	m := &manejadorWebhooks{
		log:    s.log,
		cuenta: s.cuentaDeCanal,
		encolar: func(ctx context.Context, cuentaID int64) error {
			if s.cola == nil {
				return errors.New("no hay cola configurada en este proceso")
			}
			return ordenes.EncolarIngesta(ctx, s.cola, cuentaID)
		},
		fondo: func(f func()) { go f() },
	}
	mux.HandleFunc("POST /api/webhooks/{canal}", m.recibir)
}

// cuentaDeCanal descifra las credenciales de la cuenta conectada de un canal.
//
// Devuelve el mapa en claro y no el adaptador: el webhook no habla con el
// canal, solo necesita el secreto con el que comprobar la firma.
func (s *Server) cuentaDeCanal(ctx context.Context, canal string) (int64, map[string]string, error) {
	porCanal, err := s.st.CuentasPorCanal(ctx)
	if err != nil {
		return 0, nil, err
	}
	id, hay := porCanal[canal]
	if !hay {
		return 0, nil, errSinCuenta
	}
	_, cifrada, err := s.st.CredencialesDeCuenta(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	if s.cif == nil {
		return 0, nil, errors.New("no hay cifrador configurado en este proceso")
	}
	claro, err := s.cif.DescifrarTexto(cifrada)
	if err != nil {
		return 0, nil, fmt.Errorf("no se pudo descifrar la credencial de la cuenta %d: %w", id, err)
	}
	var campos map[string]string
	if err := json.Unmarshal([]byte(claro), &campos); err != nil {
		return 0, nil, fmt.Errorf("credenciales ilegibles de la cuenta %d: %w", id, err)
	}
	return id, campos, nil
}

// veredicto es lo que decide la verificación de un canal.
type veredicto struct {
	// autentico dice si la llamada viene de verdad del canal. Si es falso se
	// responde 401 y no se encola nada.
	autentico bool
	// dispara distingue "es del canal y habla de un pedido" de "es del canal
	// pero habla de otra cosa". Un cambio de precio de un ítem de
	// MercadoLibre es auténtico y no tiene por qué provocar una ingesta.
	dispara bool
	// motivo va al log para poder diagnosticar sin volcar el cuerpo.
	motivo string
	// topico es lo que dijo el canal, para el log.
	topico string
}

func (m *manejadorWebhooks) recibir(w http.ResponseWriter, r *http.Request) {
	canal := r.PathValue("canal")
	if !canalConWebhook(canal) {
		// No es 401 a propósito: un canal que no existe no es un problema de
		// autenticidad sino una URL mal configurada en el portal del canal.
		m.log.Warn("webhook de un canal desconocido", "canal", canal)
		escribir(w, http.StatusNotFound, map[string]string{"error": "canal desconocido"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCuerpoWebhook)
	cuerpo, err := io.ReadAll(r.Body)
	if err != nil {
		m.log.Warn("webhook rechazado", "canal", canal, "resultado", "cuerpo demasiado grande")
		escribir(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "cuerpo demasiado grande"})
		return
	}

	cuentaID, cred, err := m.cuenta(r.Context(), canal)
	switch {
	case errors.Is(err, errSinCuenta):
		m.log.Warn("webhook rechazado", "canal", canal, "resultado", "sin cuenta conectada")
		escribir(w, http.StatusUnauthorized, map[string]string{"error": "no verificable"})
		return
	case err != nil:
		// La base no contesta o la credencial no se puede descifrar. No es
		// culpa de quien llama y reintentar sí ayuda, así que se pide
		// reintento en vez de mentir con un 200 que perdería el aviso.
		m.log.Error("webhook: no se pudo resolver la cuenta", "canal", canal, "error", err)
		escribir(w, http.StatusServiceUnavailable, map[string]string{"error": "no se pudo verificar"})
		return
	}

	v := verificar(canal, cred, cuerpo, r)
	if !v.autentico {
		m.log.Warn("webhook rechazado", "canal", canal, "cuenta", cuentaID,
			"resultado", "no autentico", "motivo", v.motivo, "bytes", len(cuerpo))
		escribir(w, http.StatusUnauthorized, map[string]string{"error": "firma inválida"})
		return
	}

	// A partir de aquí ya se puede responder, y hay que hacerlo: MercadoLibre
	// da 500 ms contados desde su lado. Lo único que queda por hacer es un
	// INSERT en la cola, y se hace después de contestar.
	escribir(w, http.StatusOK, map[string]string{"estado": "recibido"})

	if !v.dispara {
		m.log.Info("webhook recibido", "canal", canal, "cuenta", cuentaID,
			"topico", v.topico, "resultado", "sin ingesta", "motivo", v.motivo,
			"bytes", len(cuerpo))
		return
	}

	m.fondo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), plazoEncolado)
		defer cancel()
		if err := m.encolar(ctx, cuentaID); err != nil {
			// El pedido no se pierde: el sondeo periódico lo traerá en su
			// siguiente pasada. Lo que se pierde es la inmediatez.
			m.log.Error("webhook: no se pudo encolar la ingesta",
				"canal", canal, "cuenta", cuentaID, "topico", v.topico, "error", err)
			return
		}
		m.log.Info("webhook recibido", "canal", canal, "cuenta", cuentaID,
			"topico", v.topico, "resultado", "ingesta encolada", "bytes", len(cuerpo))
	})
}

func canalConWebhook(canal string) bool {
	switch channel.Kind(canal) {
	case channel.Shopify, channel.WooCommerce, channel.MercadoLibre, channel.Falabella:
		return true
	}
	return false
}

// verificar comprueba la autenticidad con el mecanismo de cada canal.
//
// Ninguno cae en "si no hay secreto, pasa": sin secreto configurado no hay
// nada que comprobar, así que se rechaza. Un endpoint público que falle
// abierto es un botón de "encólame trabajo" para cualquiera.
func verificar(canal string, cred map[string]string, cuerpo []byte, r *http.Request) veredicto {
	switch channel.Kind(canal) {
	case channel.Shopify:
		return verificarShopify(cred, cuerpo, r)
	case channel.WooCommerce:
		return verificarWooCommerce(cred, cuerpo, r)
	case channel.MercadoLibre:
		return verificarMercadoLibre(cred, cuerpo)
	case channel.Falabella:
		return verificarFalabella(cred, cuerpo, r)
	}
	return veredicto{motivo: "canal desconocido"}
}

// ------------------------------------------------------------------ Shopify

// Shopify firma el cuerpo crudo con HMAC-SHA256 usando el client secret de la
// app y manda el digest en base64 en X-Shopify-Hmac-Sha256.
//
// La clave NO es el token de administración (`token`, shpat_…) que Integra ya
// guarda: es el secreto de la app, que hay que copiar aparte. Mientras no esté
// en las credenciales de la cuenta, este endpoint responde 401 y el pedido
// sigue llegando por sondeo.
func verificarShopify(cred map[string]string, cuerpo []byte, r *http.Request) veredicto {
	secreto := primerNoVacio(cred, "webhook_secret", "api_secret", "client_secret")
	if secreto == "" {
		return veredicto{motivo: "la cuenta no tiene webhook_secret"}
	}
	topico := r.Header.Get("X-Shopify-Topic")
	if !firmaBase64Valida(secreto, cuerpo, r.Header.Get("X-Shopify-Hmac-Sha256")) {
		return veredicto{motivo: "HMAC no coincide", topico: topico}
	}
	return veredicto{autentico: true, dispara: esTopicoDePedido(topico, "orders/"),
		topico: topico, motivo: "tópico ajeno a pedidos"}
}

// -------------------------------------------------------------- WooCommerce

// WooCommerce firma igual que Shopify —HMAC-SHA256 del cuerpo en base64— pero
// en X-WC-Webhook-Signature.
//
// Si al crear el webhook no se le pone secreto, WooCommerce usa el consumer
// secret de la clave de API con la que se creó, que Integra ya tiene guardado.
// Por eso este canal funciona sin añadir nada; aun así conviene un secreto
// propio, para que rotarlo no obligue a rehacer las credenciales de la API.
func verificarWooCommerce(cred map[string]string, cuerpo []byte, r *http.Request) veredicto {
	secreto := primerNoVacio(cred, "webhook_secret", "consumer_secret")
	if secreto == "" {
		return veredicto{motivo: "la cuenta no tiene webhook_secret ni consumer_secret"}
	}
	topico := r.Header.Get("X-WC-Webhook-Topic")
	firma := r.Header.Get("X-WC-Webhook-Signature")

	// Al activar un webhook, WooCommerce manda un ping: un POST con el cuerpo
	// "webhook_id=<n>", sin firma y sin tópico, y exige exactamente 200 para
	// dar el alta por buena. Rechazarlo por falta de firma —que es lo que
	// pasaba— dejaba el webhook en "pending delivery" y WooCommerce no
	// entregaba nunca nada: el endpoint no se podía activar.
	//
	// Aceptarlo es seguro porque no dispara ningún trabajo. Lo que no se
	// puede es aceptar un cuerpo cualquiera sin firma, así que se exige que
	// case exactamente con la forma del ping.
	if firma == "" && topico == "" && esPingWoo(cuerpo) {
		return veredicto{autentico: true, dispara: false, topico: "ping",
			motivo: "ping de activación de WooCommerce"}
	}

	if !firmaBase64Valida(secreto, cuerpo, firma) {
		return veredicto{motivo: "HMAC no coincide", topico: topico}
	}
	return veredicto{autentico: true, dispara: esTopicoDePedido(topico, "order"),
		topico: topico, motivo: "tópico ajeno a pedidos"}
}

// esPingWoo reconoce el cuerpo exacto del ping de activación y nada más.
var formaPingWoo = regexp.MustCompile(`^webhook_id=\d+$`)

func esPingWoo(cuerpo []byte) bool {
	return formaPingWoo.Match(bytes.TrimSpace(cuerpo))
}

// ------------------------------------------------------------- MercadoLibre

// notificacionML es lo que manda MercadoLibre (MERCADOLIBRE.md §5). No trae
// datos del pedido, solo el recurso a consultar.
type notificacionML struct {
	Topic         string   `json:"topic"`
	Resource      string   `json:"resource"`
	UserID        numeroML `json:"user_id"`
	ApplicationID numeroML `json:"application_id"`
}

// numeroML acepta el mismo campo como número o como cadena. La documentación
// lo muestra como número, pero conserva el literal tal cual para no pasar por
// float64: application_id es un entero de 16 cifras y redondearlo convertiría
// la comprobación en un "casi igual".
type numeroML string

func (n *numeroML) UnmarshalJSON(b []byte) error {
	*n = numeroML(strings.Trim(string(b), `"`))
	return nil
}

// MercadoLibre no firma sus notificaciones: lo único comprobable es que la
// notificación diga ser de nuestra aplicación y de nuestro vendedor.
//
// Es una verificación débil —quien conozca los dos números puede provocar una
// ingesta— pero el daño posible es acotado a propósito: lo que se encola es
// una ingesta contra la API de MercadoLibre con nuestras credenciales, con
// clave única por cuenta (así que un aluvión colapsa en un solo trabajo) y
// sin dato alguno tomado del cuerpo. La alternativa que ofrece ML es filtrar
// por su lista de ~100 IPs, que es operación de red, no de este manejador.
func verificarMercadoLibre(cred map[string]string, cuerpo []byte) veredicto {
	appEsperada := strings.TrimSpace(cred["app_id"])
	if appEsperada == "" {
		return veredicto{motivo: "la cuenta no tiene app_id"}
	}
	var n notificacionML
	if err := json.Unmarshal(cuerpo, &n); err != nil {
		return veredicto{motivo: "cuerpo JSON ilegible"}
	}
	if !igualEnTiempoConstante(appEsperada, string(n.ApplicationID)) {
		return veredicto{motivo: "application_id ajeno", topico: n.Topic}
	}
	// El user_id es opcional porque la cuenta puede haberse dado de alta sin
	// él; cuando está, se exige que coincida.
	if vendedor := strings.TrimSpace(cred["user_id"]); vendedor != "" {
		if !igualEnTiempoConstante(vendedor, string(n.UserID)) {
			return veredicto{motivo: "user_id ajeno", topico: n.Topic}
		}
	}
	return veredicto{autentico: true, dispara: esTopicoDePedidoML(n.Topic),
		topico: n.Topic, motivo: "tópico ajeno a pedidos"}
}

// esTopicoDePedidoML: solo los tópicos de venta provocan ingesta. `items`,
// `items_prices` y `stock-location` llegan constantemente —los provocamos
// nosotros al publicar— y encolar una ingesta por cada uno sería ruido.
func esTopicoDePedidoML(topico string) bool {
	switch strings.ToLower(strings.TrimSpace(topico)) {
	case "orders_v2", "orders", "created_orders":
		return true
	case "":
		// Sin tópico no se puede decidir; se dispara, que es el error barato.
		return true
	}
	return false
}

// ---------------------------------------------------------------- Falabella

// Falabella sí tiene webhooks (eventos onOrderCreated y
// onOrderItemsStatusChanged, callback registrable por API o por el portal),
// pero su documentación NO describe ninguna firma, cabecera de autenticación
// ni secreto compartido para la llamada de vuelta: la firma HMAC que sí
// documenta es la de las peticiones que Integra le hace a ella, no al revés.
//
// Como la URL de callback la elegimos nosotros, lo que se comprueba es un
// token propio de Integra que viaja en esa URL (o en una cabecera, si algún
// día la admiten). No es el mecanismo del canal y no se presenta como tal:
// es lo único verificable que hay. Sin token configurado se responde 401,
// porque un endpoint que dispare trabajo sin comprobar nada es peor que no
// tener webhook.
func verificarFalabella(cred map[string]string, cuerpo []byte, r *http.Request) veredicto {
	secreto := primerNoVacio(cred, "webhook_secret")
	if secreto == "" {
		return veredicto{motivo: "la cuenta no tiene webhook_secret"}
	}
	recibido := r.Header.Get("X-Integra-Webhook-Token")
	if recibido == "" {
		recibido = r.URL.Query().Get("token")
	}
	if !igualEnTiempoConstante(secreto, recibido) {
		return veredicto{motivo: "token ajeno"}
	}
	// El evento va en el cuerpo, no en una cabecera:
	// {"event":"onOrderCreated","payload":{"OrderId":190}}.
	var n struct {
		Event string `json:"event"`
	}
	_ = json.Unmarshal(cuerpo, &n) // un cuerpo ilegible se trata como evento vacío
	return veredicto{autentico: true, dispara: esTopicoDePedido(n.Event, "onorder"),
		topico: n.Event, motivo: "evento ajeno a pedidos"}
}

// ------------------------------------------------------------- utilidades

// firmaBase64Valida compara el HMAC-SHA256 del cuerpo crudo con el que llegó
// en la cabecera, en tiempo constante.
//
// Se comparan los digests decodificados y no las cadenas: base64 admite
// variantes (relleno, alfabeto URL) que representan los mismos bytes, y
// compararlas como texto rechazaría una firma correcta.
func firmaBase64Valida(secreto string, cuerpo []byte, cabecera string) bool {
	cabecera = strings.TrimSpace(cabecera)
	if cabecera == "" {
		return false
	}
	recibido, err := decodificarBase64(cabecera)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secreto))
	mac.Write(cuerpo)
	return hmac.Equal(recibido, mac.Sum(nil))
}

func decodificarBase64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// igualEnTiempoConstante compara dos identificadores sin filtrar por tiempo
// cuántos caracteres coincidían.
func igualEnTiempoConstante(a, b string) bool {
	return hmac.Equal([]byte(a), []byte(b))
}

// esTopicoDePedido: un tópico vacío dispara. No saber de qué habla el canal
// es más barato de resolver con una ingesta de más (que es idempotente y se
// deduplica por clave única) que perdiendo un pedido.
func esTopicoDePedido(topico, prefijo string) bool {
	t := strings.ToLower(strings.TrimSpace(topico))
	return t == "" || strings.HasPrefix(t, prefijo)
}

// primerNoVacio devuelve el valor de la primera clave que traiga algo. Sirve
// para admitir el nombre nuevo de una credencial y el que ya estuviera.
func primerNoVacio(cred map[string]string, claves ...string) string {
	for _, k := range claves {
		if v := strings.TrimSpace(cred[k]); v != "" {
			return v
		}
	}
	return ""
}
