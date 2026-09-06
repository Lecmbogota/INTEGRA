package atributos

import (
	"context"
	"fmt"
	"time"

	"github.com/mdv/integra/internal/store"
)

// FuenteRequisitos es lo que un canal sabe decir sobre sus categorías.
//
// Cada canal la implementa a su manera: MercadoLibre por API pública (no hace
// falta credencial), Falabella con su firma HMAC sobre la cuenta conectada.
// Las tiendas propias no implementan nada, y eso es correcto — ver más abajo.
type FuenteRequisitos interface {
	AtributosDeCategoria(ctx context.Context, categoriaID string) ([]store.AtributoCanal, error)
}

// CanalesConAtributos son los que imponen atributos por categoría.
//
// Shopify y WooCommerce NO están aquí a propósito: son tiendas propias, no
// marketplaces, y no exigen ningún atributo para publicar. Lo que allí se
// muestra como «especificaciones» es texto libre que sale de las specs del
// producto, así que no hay nada que traer ni que mapear.
var CanalesConAtributos = map[string]bool{
	"mercadolibre": true,
	"falabella":    true,
}

// ResultadoRefresco resume lo traído de un canal.
type ResultadoRefresco struct {
	Canal        string `json:"canal"`
	Categorias   int    `json:"categorias"`
	Atributos    int    `json:"atributos"`
	Obligatorios int    `json:"obligatorios"`
	Fallos       int    `json:"fallos"`
}

// Refrescar trae de un canal los atributos de cada categoría mapeada.
func Refrescar(ctx context.Context, st *store.Store, canal string, fuente FuenteRequisitos) (*ResultadoRefresco, error) {
	if !CanalesConAtributos[canal] {
		return nil, fmt.Errorf("%s no impone atributos por categoría: es una tienda propia", canal)
	}
	cats, err := st.CategoriasMapeadas(ctx, canal)
	if err != nil {
		return nil, err
	}
	if len(cats) == 0 {
		return nil, fmt.Errorf("no hay categorías confirmadas para %s", canal)
	}

	res := &ResultadoRefresco{Canal: canal}
	for _, cat := range cats {
		attrs, err := fuente.AtributosDeCategoria(ctx, cat)
		if err != nil {
			res.Fallos++
			continue
		}
		if err := st.GuardarAtributosCategoria(ctx, canal, cat, attrs); err != nil {
			return nil, err
		}
		res.Categorias++
		res.Atributos += len(attrs)
		for _, a := range attrs {
			if a.Obligatorio {
				res.Obligatorios++
			}
		}

		// Ritmo suave: son APIs ajenas con cupo.
		select {
		case <-ctx.Done():
			return res, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return res, nil
}
