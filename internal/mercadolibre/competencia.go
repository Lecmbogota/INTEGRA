package mercadolibre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Competidor es una publicación ajena encontrada en MercadoLibre.
type Competidor struct {
	Titulo    string  `json:"titulo"`
	Precio    float64 `json:"precio"`
	Moneda    string  `json:"moneda"`
	Permalink string  `json:"permalink"`
	Vendedor  string  `json:"vendedor"`
	Condicion string  `json:"condicion"`
	EnvioFree bool    `json:"envio_gratis"`
}

// BuscadorCompetencia consulta la búsqueda de los listados de MercadoLibre.
//
// A diferencia del predictor de categorías y de los atributos, que sí son
// públicos, /sites/{site}/search exige token: todos los ejemplos de «Ítems y
// Búsquedas» lo llaman con la cabecera de autorización —«curl -X GET -H
// 'Authorization: Bearer $ACCESS_TOKEN'
// https://api.mercadolibre.com/sites/$SITE_ID/search?…»— y la propia página
// remite a los errores 401 y 403 al consumir el recurso
// (https://developers.mercadolibre.com.co/es_ar/items-y-busquedas).
// Comprobado contra la API real: sin token responde
// 403 {"message":"forbidden","error":"forbidden","status":403}.
//
// Por eso el buscador no se construye solo: necesita de quién sacar el access
// token de la cuenta conectada (ConToken). Sin esa fuente ni siquiera sale la
// petición, porque se sabe de antemano que MercadoLibre la va a rechazar.
type BuscadorCompetencia struct {
	cli   *http.Client
	sitio string
	token FuenteToken
}

// FuenteToken entrega un access token vigente de la cuenta de MercadoLibre.
// Es una función y no un string porque el token de ML dura seis horas: quien
// la implemente (el adaptador del canal o conectores.RefrescarTokenML) lo
// refresca cuando toca.
type FuenteToken func(context.Context) (string, error)

// OpcionCompetencia configura el buscador al construirlo.
type OpcionCompetencia func(*BuscadorCompetencia)

// ConToken conecta el buscador con el access token de la cuenta.
func ConToken(f FuenteToken) OpcionCompetencia {
	return func(b *BuscadorCompetencia) { b.token = f }
}

func NuevoBuscadorCompetencia(sitio string, opciones ...OpcionCompetencia) *BuscadorCompetencia {
	if sitio == "" {
		sitio = SitioColombia
	}
	b := &BuscadorCompetencia{
		cli:   &http.Client{Timeout: 15 * time.Second},
		sitio: sitio,
	}
	for _, o := range opciones {
		o(b)
	}
	return b
}

// ErrSinCuenta indica que no hay cuenta de MercadoLibre conectada de la que
// sacar el token, así que la búsqueda de competencia no se puede hacer.
var ErrSinCuenta = fmt.Errorf("MercadoLibre exige token para buscar en los listados: hay que conectar la cuenta")

// Buscar devuelve hasta max publicaciones que coinciden con la consulta
// (idealmente el EAN o el SKU, que es como se encuentra el producto exacto).
func (b *BuscadorCompetencia) Buscar(ctx context.Context, consulta string, max int) ([]Competidor, error) {
	if max <= 0 || max > 20 {
		max = 8
	}
	if b.token == nil {
		return nil, ErrSinCuenta
	}
	token, err := b.token(ctx)
	if err != nil {
		return nil, fmt.Errorf("obteniendo el token de MercadoLibre: %w", err)
	}
	if token == "" {
		return nil, ErrSinCuenta
	}

	q := url.Values{"q": {consulta}, "limit": {fmt.Sprint(max)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseAPI+"/sites/"+b.sitio+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// ML documenta el token siempre en la cabecera, nunca en la query.
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := b.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando MercadoLibre: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		// Ahora el token sí viajó: si aun así rechaza, es la cuenta o los
		// permisos, no la falta de credenciales.
		return nil, fmt.Errorf("MercadoLibre rechazó la búsqueda (HTTP %d): el token de la cuenta "+
			"caducó o no tiene permiso para leer los listados", resp.StatusCode)
	default:
		return nil, fmt.Errorf("MercadoLibre respondió HTTP %d", resp.StatusCode)
	}

	var cuerpo struct {
		Results []struct {
			Title     string  `json:"title"`
			Price     float64 `json:"price"`
			Currency  string  `json:"currency_id"`
			Permalink string  `json:"permalink"`
			Condition string  `json:"condition"`
			Seller    struct {
				Nickname string `json:"nickname"`
			} `json:"seller"`
			Shipping struct {
				FreeShipping bool `json:"free_shipping"`
			} `json:"shipping"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&cuerpo); err != nil {
		return nil, fmt.Errorf("respuesta de MercadoLibre ilegible: %w", err)
	}

	var out []Competidor
	for _, r := range cuerpo.Results {
		out = append(out, Competidor{
			Titulo: r.Title, Precio: r.Price, Moneda: r.Currency,
			Permalink: r.Permalink, Vendedor: r.Seller.Nickname,
			Condicion: r.Condition, EnvioFree: r.Shipping.FreeShipping,
		})
	}
	return out, nil
}
