// Package atributos rellena los atributos que cada categoría de canal exige.
//
// Es lo que desbloquea MercadoLibre y Falabella: rechazan la publicación si
// falta un atributo obligatorio de la categoría, así que sin este paso sus
// adaptadores fallarían en el primer envío.
//
// Los valores salen de lo que Integra ya sabe —marca, specs extraídas del
// nombre, el propio nombre— y nunca se inventan: un atributo que no se puede
// deducir se deja vacío para que una persona lo escriba. Poner un valor
// plausible pero falso en una ficha de producto es peor que dejar el hueco.
package atributos

import (
	"context"
	"log/slog"
	"strings"

	"github.com/mdv/integra/internal/store"
)

// sinonimos traduce el nombre de una spec de Integra a los nombres con los
// que los canales llaman al mismo atributo. Se compara en minúsculas.
var sinonimos = map[string][]string{
	"marca":       {"brand", "marca"},
	"modelo":      {"model", "modelo", "número de modelo", "modelo alfanumérico"},
	"capacidad":   {"capacity", "capacidad", "capacidad de almacenamiento", "capacidad total"},
	"color":       {"color", "colour"},
	"tecnología":  {"technology", "tecnología", "tipo de memoria"},
	"formato":     {"format", "formato", "factor de forma"},
	"interfaz":    {"interface", "interfaz", "tipo de conexión"},
	"velocidad":   {"speed", "velocidad", "frecuencia"},
	"tamaño":      {"size", "tamaño", "tamaño de pantalla"},
	"resolución":  {"resolution", "resolución"},
}

// Deductor rellena atributos de un canal.
type Deductor struct {
	st  *store.Store
	log *slog.Logger
}

func NuevoDeductor(st *store.Store, log *slog.Logger) *Deductor {
	return &Deductor{st: st, log: log}
}

// Resultado resume una pasada de deducción.
type Resultado struct {
	Productos   int `json:"productos"`
	Asignados   int `json:"atributos_asignados"`
	Faltantes   int `json:"obligatorios_sin_valor"`
	Completos   int `json:"productos_completos"`
}

// Deducir recorre los productos con categoría mapeada y rellena lo que pueda.
//
// Respeta siempre los valores escritos a mano: rehacer la deducción no puede
// destruir trabajo humano.
func (d *Deductor) Deducir(ctx context.Context, canalCodigo string) (*Resultado, error) {
	fuentes, err := d.st.ProductosParaAtributos(ctx, canalCodigo)
	if err != nil {
		return nil, err
	}

	res := &Resultado{Productos: len(fuentes)}
	// Los atributos de una categoría se consultan una vez y se reutilizan:
	// varios productos comparten categoría.
	cache := map[string][]store.AtributoCanal{}

	for _, f := range fuentes {
		attrs, ok := cache[f.CategCanal]
		if !ok {
			attrs, err = d.st.AtributosDeCategoria(ctx, canalCodigo, f.CategCanal, false)
			if err != nil {
				return nil, err
			}
			cache[f.CategCanal] = attrs
		}

		faltanAqui := 0
		for _, a := range attrs {
			valorID, valor := d.deducirValor(a, f)
			if valor == "" {
				if a.Obligatorio {
					faltanAqui++
					res.Faltantes++
				}
				continue
			}
			if err := d.st.GuardarAtributoProducto(ctx, f.ProductoID, canalCodigo,
				a.AttributeID, valorID, valor, "deducido", true); err != nil {
				return nil, err
			}
			res.Asignados++
		}
		if faltanAqui == 0 {
			res.Completos++
		}
	}
	return res, nil
}

// listaCerrada indica que el canal solo admite valores de su propio catálogo.
//
// Lo decide el tipo de dato, no la presencia de valores sugeridos: en
// MercadoLibre "Marca" y "Modelo" son de tipo string y traen cientos de
// valores, pero son SUGERENCIAS — acepta texto libre y así es como entran las
// marcas nuevas. Solo "list" y "boolean" son cerrados de verdad. Confundirlos
// dejaba sin marca a 99 productos cuya marca simplemente no estaba en la
// lista de MercadoLibre.
func listaCerrada(tipo string) bool {
	return tipo == "list" || tipo == "boolean"
}

// deducirValor busca el valor de un atributo entre lo que Integra sabe.
//
// Cuando encuentra la equivalencia en el catálogo del canal devuelve su id,
// que es siempre preferible. Si no la encuentra, manda texto libre salvo que
// el atributo sea de lista cerrada: ahí inventar un valor es un rechazo
// seguro, y es mejor dejar el hueco para que una persona lo resuelva.
func (d *Deductor) deducirValor(a store.AtributoCanal, f store.FuenteAtributos) (valorID, valor string) {
	crudo := d.valorCrudo(a, f)
	if crudo == "" {
		return "", ""
	}

	for _, v := range a.ValoresValidos {
		if strings.EqualFold(strings.TrimSpace(v.Nombre), strings.TrimSpace(crudo)) {
			return v.ID, v.Nombre
		}
	}
	// Segunda pasada, más laxa: "16 GB" contra "16GB".
	compacto := compactar(crudo)
	for _, v := range a.ValoresValidos {
		if compactar(v.Nombre) == compacto {
			return v.ID, v.Nombre
		}
	}

	if listaCerrada(a.TipoDato) {
		return "", ""
	}
	return "", crudo
}

// valorCrudo obtiene el texto del atributo desde las fuentes de Integra.
func (d *Deductor) valorCrudo(a store.AtributoCanal, f store.FuenteAtributos) string {
	nombreAttr := strings.ToLower(strings.TrimSpace(a.Nombre))
	idAttr := strings.ToLower(strings.TrimSpace(a.AttributeID))

	// La marca es el caso más frecuente y el que más rechazos causa.
	if coincide(nombreAttr, idAttr, sinonimos["marca"]) {
		return f.Marca
	}

	// El modelo se deduce del SKU, que es como los fabricantes lo publican.
	if coincide(nombreAttr, idAttr, sinonimos["modelo"]) {
		return f.SKU
	}

	// El resto sale de las specs extraídas del nombre.
	for _, sp := range f.Specs {
		clave := strings.ToLower(strings.TrimSpace(sp.Clave))
		if clave == nombreAttr || clave == idAttr {
			return sp.Valor
		}
		if lista, ok := sinonimos[clave]; ok && coincide(nombreAttr, idAttr, lista) {
			return sp.Valor
		}
	}
	return ""
}

func coincide(nombre, id string, alternativas []string) bool {
	for _, alt := range alternativas {
		if nombre == alt || id == alt {
			return true
		}
	}
	return false
}

func compactar(s string) string {
	return strings.ToLower(strings.NewReplacer(" ", "", "-", "", ".", "", "_", "").Replace(s))
}

// FaltantesDe describe qué le falta a un producto para poder publicarse.
func FaltantesDe(vals []store.ValorAtributoProducto) []string {
	var out []string
	for _, v := range vals {
		if v.Obligatorio && strings.TrimSpace(v.ValueName) == "" {
			out = append(out, v.Nombre)
		}
	}
	return out
}

// Etiqueta traduce el origen de un valor para mostrarlo.
func Etiqueta(origen string) string {
	switch origen {
	case "deducido":
		return "Deducido del nombre"
	case "predictor":
		return "Sugerido por el canal"
	case "manual":
		return "Escrito a mano"
	case "defecto":
		return "Valor por defecto"
	default:
		return "sin valor"
	}
}
