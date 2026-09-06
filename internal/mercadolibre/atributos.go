package mercadolibre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AtributoCategoria es un atributo que MercadoLibre pide en una categoría.
type AtributoCategoria struct {
	ID       string          `json:"id"`
	Nombre   string          `json:"name"`
	TipoDato string          `json:"value_type"`
	Unidad   string          `json:"default_unit"`
	Valores  []ValorAtributo `json:"values"`
	Tags     map[string]bool `json:"tags"`
}

type ValorAtributo struct {
	ID     string `json:"id"`
	Nombre string `json:"name"`
}

// Obligatorio indica si la publicación se rechaza sin este atributo.
//
// MercadoLibre marca la obligatoriedad con etiquetas, no con un booleano:
// "required" es obligatorio para publicar y "catalog_required" lo exige
// además el catálogo unificado.
func (a AtributoCategoria) Obligatorio() bool {
	return a.Tags["required"] || a.Tags["catalog_required"]
}

// AtributosDeCategoria consulta qué pide una categoría. Es un endpoint
// público: no hace falta token, así que se puede preparar el mapeo antes de
// conectar la cuenta.
func (p *Predictor) AtributosDeCategoria(ctx context.Context, categoriaID string) ([]AtributoCategoria, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.mercadolibre.com/categories/"+categoriaID+"/attributes", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("consultando atributos de %s: %w", categoriaID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MercadoLibre respondió HTTP %d para la categoría %s",
			resp.StatusCode, categoriaID)
	}

	var out []AtributoCategoria
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("atributos ilegibles de %s: %w", categoriaID, err)
	}
	return out, nil
}
