// Package conectores habla con las APIs de los canales de venta.
//
// Por ahora contiene la prueba de conexión de cada canal: la llamada más
// pequeña que demuestra que la credencial funciona. Los adaptadores de
// publicación (Fase 4) crecerán sobre estas mismas credenciales.
package conectores

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var cliente = &http.Client{Timeout: 20 * time.Second}

// Credenciales es el JSON en claro que se cifra al guardar la cuenta. Los
// campos usados dependen del canal.
type Credenciales struct {
	// Shopify
	Tienda string `json:"tienda,omitempty"` // p.ej. mitienda.myshopify.com
	Token  string `json:"token,omitempty"`  // Admin API access token (shpat_…)
	// WooCommerce
	URL            string `json:"url,omitempty"`
	ConsumerKey    string `json:"consumer_key,omitempty"`
	ConsumerSecret string `json:"consumer_secret,omitempty"`
	// Falabella Seller Center
	UserID string `json:"user_id,omitempty"`
	APIKey string `json:"api_key,omitempty"`
	// MercadoLibre
	AppID        string `json:"app_id,omitempty"`
	AppSecret    string `json:"app_secret,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// Probar hace la llamada mínima que confirma que la credencial sirve.
// Devuelve un mensaje humano ("conectado como MDV Distribuidora") o error.
//
// Si la prueba obligó a rotar una credencial (MercadoLibre invalida el
// refresh token al canjearlo), devuelve además el juego nuevo, que quien
// llama debe guardar: si no, la prueba habrá dejado la cuenta con un token
// muerto y el siguiente trabajo fallará con invalid_grant.
func Probar(ctx context.Context, canal string, cred Credenciales) (msg string, rotadas *Credenciales, err error) {
	switch canal {
	case "shopify":
		msg, err = probarShopify(ctx, cred)
	case "woocommerce":
		msg, err = probarWoo(ctx, cred)
	case "falabella":
		msg, err = probarFalabella(ctx, cred)
	case "mercadolibre":
		return probarMercadoLibre(ctx, cred)
	default:
		err = fmt.Errorf("canal desconocido: %s", canal)
	}
	return msg, nil, err
}

func probarShopify(ctx context.Context, c Credenciales) (string, error) {
	if c.Tienda == "" || c.Token == "" {
		return "", fmt.Errorf("faltan la tienda (mitienda.myshopify.com) y el token de Admin API")
	}
	tienda := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.Tienda, "https://"), "http://"), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://"+tienda+"/admin/api/2024-10/shop.json", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Shopify-Access-Token", c.Token)

	var cuerpo struct {
		Shop struct {
			Name string `json:"name"`
		} `json:"shop"`
	}
	if err := hacer(req, &cuerpo); err != nil {
		return "", err
	}
	return "conectado a la tienda «" + cuerpo.Shop.Name + "»", nil
}

func probarWoo(ctx context.Context, c Credenciales) (string, error) {
	if c.URL == "" || c.ConsumerKey == "" || c.ConsumerSecret == "" {
		return "", fmt.Errorf("faltan la URL de la tienda y las claves consumer key/secret")
	}
	base := strings.TrimSuffix(c.URL, "/")
	if !strings.HasPrefix(base, "https://") {
		return "", fmt.Errorf("la tienda tiene que estar en https: sobre http WooCommerce " +
			"no acepta la clave y el secreto y viajarían en claro por la red")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/wp-json/wc/v3/products?per_page=1", nil)
	if err != nil {
		return "", err
	}
	// Las claves van en la cabecera y no en la URL: un fallo de transporte
	// devuelve la URL dentro del mensaje de error, y ese mensaje se guarda en
	// la base y se muestra en el panel. En la cadena de consulta el secreto
	// acabaría en texto plano en ambos sitios.
	req.SetBasicAuth(c.ConsumerKey, c.ConsumerSecret)

	var cuerpo []any
	if err := hacer(req, &cuerpo); err != nil {
		return "", err
	}
	return "conectado a " + base, nil
}

// sinURL quita la dirección del mensaje de los errores de transporte.
//
// net/http envuelve los fallos de red en *url.Error, cuyo Error() incluye la
// URL completa. Con credenciales en la cadena de consulta, ese texto acaba
// guardado en channel_accounts.config, en jobs.last_error y en el panel, en
// claro. Aquí se conserva la causa y se descarta la dirección.
func sinURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// FirmarFalabella construye la cadena de consulta firmada que exige Seller
// Center: HMAC-SHA256 en hex sobre los parámetros ordenados alfabéticamente y
// codificados RFC 3986.
//
// El orden y la codificación son parte de la firma: cualquier diferencia con
// lo que el servidor recalcula produce un rechazo por firma inválida, que es
// el error más difícil de diagnosticar de esta API.
func FirmarFalabella(params map[string]string, apiKey string) string {
	claves := make([]string, 0, len(params))
	for k := range params {
		claves = append(claves, k)
	}
	sort.Strings(claves)
	var partes []string
	for _, k := range claves {
		partes = append(partes, escaparRFC3986(k)+"="+escaparRFC3986(params[k]))
	}
	base := strings.Join(partes, "&")

	mac := hmac.New(sha256.New, []byte(apiKey))
	mac.Write([]byte(base))
	return base + "&Signature=" + hex.EncodeToString(mac.Sum(nil))
}

// escaparRFC3986 codifica como espera la firma del Seller Center.
//
// url.QueryEscape aplica la codificación de formularios, que convierte el
// espacio en '+'. Falabella recalcula la firma con codificación RFC 3986, que
// lo convierte en %20: cualquier parámetro con espacios —un nombre de
// producto, una marca de tiempo con formato distinto— producía una firma que
// el servidor rechazaba, y el error que devuelve es solo "firma inválida".
func escaparRFC3986(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// ParamsFalabella arma los parámetros comunes de toda llamada.
func ParamsFalabella(accion, userID string) map[string]string {
	return map[string]string{
		"Action":    accion,
		"Format":    "JSON",
		"Timestamp": time.Now().UTC().Format("2006-01-02T15:04:05-0700"),
		"UserID":    userID,
		"Version":   "1.0",
	}
}

func probarFalabella(ctx context.Context, c Credenciales) (string, error) {
	if c.UserID == "" || c.APIKey == "" {
		return "", fmt.Errorf("faltan el UserID y la API Key del Seller Center")
	}
	consulta := FirmarFalabella(ParamsFalabella("GetSeller", c.UserID), c.APIKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://sellercenter-api.falabella.com/?"+consulta, nil)
	if err != nil {
		return "", err
	}
	var cuerpo struct {
		SuccessResponse *struct {
			Body struct {
				Seller struct {
					Name string `json:"Name"`
				} `json:"Seller"`
			} `json:"Body"`
		} `json:"SuccessResponse"`
		ErrorResponse *struct {
			Head struct {
				ErrorMessage string `json:"ErrorMessage"`
			} `json:"Head"`
		} `json:"ErrorResponse"`
	}
	if err := hacer(req, &cuerpo); err != nil {
		return "", err
	}
	if cuerpo.ErrorResponse != nil {
		return "", fmt.Errorf("Seller Center: %s", cuerpo.ErrorResponse.Head.ErrorMessage)
	}
	if cuerpo.SuccessResponse == nil {
		return "", fmt.Errorf("respuesta inesperada del Seller Center")
	}
	return "conectado como «" + cuerpo.SuccessResponse.Body.Seller.Name + "»", nil
}

func probarMercadoLibre(ctx context.Context, c Credenciales) (string, *Credenciales, error) {
	token := c.AccessToken
	var rotadas *Credenciales
	// El access token de ML dura 6 horas: lo normal es entrar con el refresh.
	if token == "" && c.RefreshToken != "" && c.AppID != "" && c.AppSecret != "" {
		nuevo, err := RefrescarTokenML(ctx, c.AppID, c.AppSecret, c.RefreshToken)
		if err != nil {
			return "", nil, err
		}
		token = nuevo.AccessToken
		// El canje acaba de invalidar el refresh token guardado: hay que
		// devolver el nuevo para que se persista, y de paso el access token
		// vigente, que ahorra un canje al primer trabajo.
		r := c
		r.AccessToken = nuevo.AccessToken
		if nuevo.RefreshToken != "" {
			r.RefreshToken = nuevo.RefreshToken
		}
		rotadas = &r
	}
	if token == "" {
		return "", nil, fmt.Errorf("hace falta un access token, o app_id + app_secret + refresh_token")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, URLBaseML+"/users/me", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	var cuerpo struct {
		Nickname string   `json:"nickname"`
		Tags     []string `json:"tags"`
	}
	if err := hacer(req, &cuerpo); err != nil {
		return "", rotadas, err
	}
	msg := "conectado como «" + cuerpo.Nickname + "»"
	for _, t := range cuerpo.Tags {
		if t == "user_product_seller" {
			msg += " (modelo User Products)"
		}
	}
	return msg, rotadas, nil
}

// URLBaseML es la raíz de la API de MercadoLibre. Es variable para que las
// pruebas la apunten a un servidor local.
var URLBaseML = "https://api.mercadolibre.com"

// TokenML es la respuesta del intercambio OAuth de MercadoLibre.
type TokenML struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// RefrescarTokenML canjea el refresh token por un access token nuevo.
// MercadoLibre rota el refresh token en cada canje: hay que guardar el nuevo.
func RefrescarTokenML(ctx context.Context, appID, appSecret, refresh string) (*TokenML, error) {
	datos := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {appID},
		"client_secret": {appSecret},
		"refresh_token": {refresh},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		URLBaseML+"/oauth/token", strings.NewReader(datos.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var t TokenML
	if err := hacer(req, &t); err != nil {
		// invalid_grant es el error que deja ML cuando el refresh token ya se
		// usó, caducó (6 meses) o el vendedor revocó la aplicación. No hay
		// reintento que lo arregle: hace falta volver a autorizar.
		if strings.Contains(err.Error(), "invalid_grant") {
			return nil, fmt.Errorf("MercadoLibre rechazó el refresh token (invalid_grant): "+
				"ya fue usado, caducó o el vendedor revocó la aplicación. "+
				"Hay que volver a autorizar la cuenta y pegar el refresh token nuevo: %w", err)
		}
		return nil, fmt.Errorf("refrescando el token de MercadoLibre: %w", err)
	}
	return &t, nil
}

func hacer(req *http.Request, out any) error {
	req.Header.Set("Accept", "application/json")
	resp, err := cliente.Do(req)
	if err != nil {
		return sinURL(err)
	}
	defer resp.Body.Close()
	cuerpo, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resumen := strings.TrimSpace(string(cuerpo))
		if len(resumen) > 200 {
			resumen = resumen[:200] + "…"
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resumen)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(cuerpo, out)
}
