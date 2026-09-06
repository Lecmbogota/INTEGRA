// Package mercadolibre agrupa lo específico de ese canal.
//
// El predictor de categorías vive aquí y es una pieza aparte del adaptador
// porque no necesita credenciales: el endpoint domain_discovery es público.
// Eso permite mapear todo el catálogo antes de abrir una sola cuenta.
package mercadolibre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SitioColombia es el identificador de MercadoLibre Colombia.
const SitioColombia = "MCO"

// baseAPI se deja como variable para poder apuntarla a un servidor de pruebas.
var baseAPI = "https://api.mercadolibre.com"

// Sugerencia es una categoría propuesta por el predictor.
type Sugerencia struct {
	DominioID     string `json:"domain_id"`
	DominioNombre string `json:"domain_name"`
	CategoriaID   string `json:"category_id"`
	Categoria     string `json:"category_name"`
	// Atributos son los que el predictor logró deducir del título. Vienen ya
	// con el identificador que espera la API al publicar, así que ahorran
	// buena parte del mapeo de atributos obligatorios.
	Atributos []AtributoSugerido `json:"attributes"`
}

type AtributoSugerido struct {
	ID          string `json:"id"`
	Nombre      string `json:"name"`
	ValorID     string `json:"value_id"`
	ValorNombre string `json:"value_name"`
}

// Predictor consulta el servicio público de descubrimiento de dominios.
type Predictor struct {
	Sitio string
	HTTP  *http.Client
}

func NuevoPredictor(sitio string) *Predictor {
	if sitio == "" {
		sitio = SitioColombia
	}
	return &Predictor{
		Sitio: sitio,
		HTTP:  &http.Client{Timeout: 20 * time.Second},
	}
}

// ErrSinSugerencia indica que el predictor no supo qué categoría proponer.
var ErrSinSugerencia = fmt.Errorf("el predictor no devolvió ninguna categoría")

// Predecir propone categorías a partir del título de un producto.
//
// Devuelve las sugerencias en el orden que da MercadoLibre, que es de más a
// menos probable. Nunca se usa la primera sin que una persona la confirme:
// una categoría equivocada arrastra historial y no se arregla borrando la
// publicación.
func (p *Predictor) Predecir(ctx context.Context, titulo string, limite int) ([]Sugerencia, error) {
	titulo = strings.TrimSpace(titulo)
	if titulo == "" {
		return nil, fmt.Errorf("el título está vacío")
	}
	if limite <= 0 || limite > 10 {
		limite = 3
	}

	u := fmt.Sprintf("%s/sites/%s/domain_discovery/search?limit=%d&q=%s",
		baseAPI, url.PathEscape(p.Sitio), limite, url.QueryEscape(titulo))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := p.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando el predictor: %w", err)
	}
	defer resp.Body.Close()

	cuerpo, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		recorte := string(cuerpo)
		if len(recorte) > 300 {
			recorte = recorte[:300] + "…"
		}
		return nil, fmt.Errorf("el predictor respondió HTTP %d: %s", resp.StatusCode, recorte)
	}

	var out []Sugerencia
	if err := json.Unmarshal(cuerpo, &out); err != nil {
		return nil, fmt.Errorf("respuesta del predictor ilegible: %w", err)
	}
	if len(out) == 0 {
		return nil, ErrSinSugerencia
	}
	return out, nil
}

// Confianza estima cuánto fiarse de una sugerencia.
//
// MercadoLibre no devuelve una puntuación, así que se deduce de dos señales
// observables: la posición en la lista y cuántos atributos logró deducir el
// predictor. Si reconoció la capacidad y la marca, entendió el producto.
func Confianza(s Sugerencia, posicion int) float64 {
	base := 0.9
	switch posicion {
	case 0:
	case 1:
		base = 0.6
	default:
		base = 0.4
	}
	// Cada atributo deducido suma, hasta un tope: no conviene llegar a 1,0
	// porque ninguna sugerencia automática merece certeza absoluta.
	base += float64(len(s.Atributos)) * 0.02
	if base > 0.95 {
		base = 0.95
	}
	return base
}
