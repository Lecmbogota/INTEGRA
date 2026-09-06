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

// BuscadorCompetencia consulta la búsqueda pública de MercadoLibre.
//
// Es el mismo endpoint que usa su web. En algunos sitios MercadoLibre lo ha
// ido cerrando a clientes sin token: si responde 401/403, el error lo dice
// claro — con la cuenta conectada (Fase 3) la consulta pasa a ir autenticada
// y esta limitación desaparece.
type BuscadorCompetencia struct {
	cli   *http.Client
	sitio string
}

func NuevoBuscadorCompetencia(sitio string) *BuscadorCompetencia {
	if sitio == "" {
		sitio = SitioColombia
	}
	return &BuscadorCompetencia{
		cli:   &http.Client{Timeout: 15 * time.Second},
		sitio: sitio,
	}
}

// Buscar devuelve hasta max publicaciones que coinciden con la consulta
// (idealmente el EAN o el SKU, que es como se encuentra el producto exacto).
func (b *BuscadorCompetencia) Buscar(ctx context.Context, consulta string, max int) ([]Competidor, error) {
	if max <= 0 || max > 20 {
		max = 8
	}
	q := url.Values{"q": {consulta}, "limit": {fmt.Sprint(max)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.mercadolibre.com/sites/"+b.sitio+"/search?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := b.cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando MercadoLibre: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("MercadoLibre exige token para la búsqueda (HTTP %d): estará disponible al conectar la cuenta", resp.StatusCode)
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
