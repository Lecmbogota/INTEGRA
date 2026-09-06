package falabella

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mdv/integra/internal/conectores"
)

// AtributoCategoria es un atributo que Seller Center pide en una categoría.
type AtributoCategoria struct {
	Nombre      string    `json:"Name"`
	Etiqueta    string    `json:"Label"`
	TipoDato    string    `json:"AttributeType"`
	Obligatorio numOTexto `json:"isMandatory"`
	Opciones    opciones  `json:"Options"`
}

type OpcionAtributo struct {
	Nombre string `json:"Name"`
}

// opciones acepta las dos formas con las que llega la lista de valores: el
// array plano y el anidado en Options.Option, que además colapsa a objeto
// cuando la categoría solo ofrece un valor.
type opciones []OpcionAtributo

func (o *opciones) UnmarshalJSON(b []byte) error {
	lista, err := listaSC[OpcionAtributo](json.RawMessage(b), "Option")
	if err != nil {
		return err
	}
	*o = lista
	return nil
}

// EsObligatorio: Seller Center devuelve 1/0, unas veces como número y otras
// como cadena, así que se compara el texto en vez de exigir un tipo.
func (a AtributoCategoria) EsObligatorio() bool {
	v := strings.TrimSpace(string(a.Obligatorio))
	return v == "1" || strings.EqualFold(v, "true")
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
			Body json.RawMessage `json:"Body"`
		} `json:"SuccessResponse"`
	}
	if err := a.llamar(ctx, http.MethodGet, p, nil, &resp); err != nil {
		return nil, err
	}
	return listaSC[AtributoCategoria](resp.SuccessResponse.Body, "Attribute")
}
