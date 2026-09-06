package falabella

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mdv/integra/internal/conectores"
)

// AtributoCategoria es un atributo que Seller Center pide en una categoría.
type AtributoCategoria struct {
	Nombre      string          `json:"Name"`
	Etiqueta    string          `json:"Label"`
	TipoDato    string          `json:"AttributeType"`
	Obligatorio json.Number     `json:"isMandatory"`
	Opciones    []OpcionAtributo `json:"Options"`
}

type OpcionAtributo struct {
	Nombre string `json:"Name"`
}

// EsObligatorio: Seller Center devuelve 1/0 como número, no un booleano.
func (a AtributoCategoria) EsObligatorio() bool {
	n, err := a.Obligatorio.Int64()
	return err == nil && n == 1
}

// AtributosDeCategoria consulta qué pide una categoría de Falabella.
//
// Mismo patrón que MercadoLibre pero con su firma HMAC. La diferencia de
// fondo: Falabella identifica los atributos por NOMBRE, no por un id opaco,
// así que el nombre es a la vez la clave y la etiqueta.
func (a *Adaptador) AtributosDeCategoria(ctx context.Context, categoriaID string) ([]AtributoCategoria, error) {
	p := conectores.ParamsFalabella("GetCategoryAttributes", a.userID)
	p["PrimaryCategory"] = categoriaID

	var resp struct {
		SuccessResponse struct {
			Body []AtributoCategoria `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return nil, err
	}
	return resp.SuccessResponse.Body, nil
}
