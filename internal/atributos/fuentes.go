package atributos

import (
	"context"

	"github.com/mdv/integra/internal/conectores/falabella"
	"github.com/mdv/integra/internal/mercadolibre"
	"github.com/mdv/integra/internal/store"
)

// Este fichero adapta cada canal a FuenteRequisitos. Vive aquí y no en cada
// paquete de canal para que los adaptadores no dependan del store: ellos
// hablan con su API y punto; la traducción al modelo de Integra es de aquí.

// FuenteML consulta la API pública de MercadoLibre. No necesita credenciales,
// así que el mapeo de atributos se puede preparar antes de conectar la cuenta.
type FuenteML struct{ p *mercadolibre.Predictor }

func NuevaFuenteML() *FuenteML {
	return &FuenteML{p: mercadolibre.NuevoPredictor(mercadolibre.SitioColombia)}
}

func (f *FuenteML) AtributosDeCategoria(ctx context.Context, categoriaID string) ([]store.AtributoCanal, error) {
	attrs, err := f.p.AtributosDeCategoria(ctx, categoriaID)
	if err != nil {
		return nil, err
	}
	out := make([]store.AtributoCanal, 0, len(attrs))
	for _, a := range attrs {
		var valores []store.ValorAtributo
		for _, v := range a.Valores {
			valores = append(valores, store.ValorAtributo{ID: v.ID, Nombre: v.Nombre})
		}
		out = append(out, store.AtributoCanal{
			AttributeID: a.ID, Nombre: a.Nombre, Obligatorio: a.Obligatorio(),
			TipoDato: a.TipoDato, Unidad: a.Unidad, ValoresValidos: valores,
		})
	}
	return out, nil
}

// FuenteFalabella consulta el Seller Center. A diferencia de MercadoLibre sí
// exige credenciales, así que solo se puede refrescar con la cuenta conectada.
type FuenteFalabella struct{ ad *falabella.Adaptador }

func NuevaFuenteFalabella(ad *falabella.Adaptador) *FuenteFalabella {
	return &FuenteFalabella{ad: ad}
}

func (f *FuenteFalabella) AtributosDeCategoria(ctx context.Context, categoriaID string) ([]store.AtributoCanal, error) {
	attrs, err := f.ad.AtributosDeCategoria(ctx, categoriaID)
	if err != nil {
		return nil, err
	}
	out := make([]store.AtributoCanal, 0, len(attrs))
	for _, a := range attrs {
		var valores []store.ValorAtributo
		for _, o := range a.Opciones {
			// Falabella identifica los valores por nombre, no por id.
			valores = append(valores, store.ValorAtributo{ID: o.Nombre, Nombre: o.Nombre})
		}
		etiqueta := a.Etiqueta
		if etiqueta == "" {
			etiqueta = a.Nombre
		}
		out = append(out, store.AtributoCanal{
			AttributeID: a.Nombre, Nombre: etiqueta, Obligatorio: a.EsObligatorio(),
			TipoDato: tipoFalabella(a.TipoDato), ValoresValidos: valores,
		})
	}
	return out, nil
}

// tipoFalabella traduce sus tipos a los mismos nombres que usa MercadoLibre,
// para que la deducción no tenga que distinguir de qué canal viene el dato.
func tipoFalabella(t string) string {
	switch t {
	case "dropdown", "enumInput":
		return "list"
	case "checkbox":
		return "boolean"
	case "numeric":
		return "number_unit"
	default:
		return "string"
	}
}
