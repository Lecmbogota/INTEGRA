package atributos

import (
	"testing"

	"github.com/mdv/integra/internal/store"
)

func fuente() store.FuenteAtributos {
	f := store.FuenteAtributos{
		ProductoID: 1,
		Nombre:     "Memoria Kingston 16GB DDR5-5600 UDIMM",
		Marca:      "KINGSTON",
		SKU:        "KVR56U46BS8-16",
	}
	f.Specs = []struct {
		Clave string `json:"clave"`
		Valor string `json:"valor"`
	}{
		{Clave: "Capacidad", Valor: "16 GB"},
		{Clave: "Tecnología", Valor: "DDR5-5600"},
	}
	return f
}

func deductor() *Deductor { return &Deductor{} }

// El fallo que este test fija: MercadoLibre devuelve cientos de marcas
// sugeridas en un atributo de tipo string, pero acepta texto libre. Tratarlo
// como lista cerrada dejaba sin marca a todo producto cuya marca no estuviera
// en su catálogo — 99 productos reales del catálogo de MDV.
func TestMarcaFueraDelCatalogoSeMandaComoTextoLibre(t *testing.T) {
	a := store.AtributoCanal{
		AttributeID: "BRAND", Nombre: "Marca", TipoDato: "string", Obligatorio: true,
		ValoresValidos: []store.ValorAtributo{{ID: "1", Nombre: "Otra marca"}},
	}
	f := fuente()
	f.Marca = "CKECKPOINT" // no está en el catálogo de MercadoLibre

	id, valor := deductor().deducirValor(a, f)
	if valor != "CKECKPOINT" {
		t.Fatalf("una marca fuera del catálogo debe mandarse como texto libre, se obtuvo %q", valor)
	}
	if id != "" {
		t.Fatalf("sin equivalencia no puede haber id de valor, se obtuvo %q", id)
	}
}

func TestPrefiereElIdDelCatalogoCuandoCoincide(t *testing.T) {
	a := store.AtributoCanal{
		AttributeID: "BRAND", Nombre: "Marca", TipoDato: "string",
		ValoresValidos: []store.ValorAtributo{
			{ID: "20262", Nombre: "AOC"}, {ID: "9344", Nombre: "Kingston"},
		},
	}
	id, valor := deductor().deducirValor(a, fuente())
	if id != "9344" || valor != "Kingston" {
		t.Fatalf("debía resolver al id del catálogo: id=%q valor=%q", id, valor)
	}
}

// Una lista cerrada sí exige equivalencia: inventar "Sí" donde el canal
// espera uno de sus identificadores es un rechazo seguro.
func TestListaCerradaSinEquivalenciaSeDejaVacia(t *testing.T) {
	a := store.AtributoCanal{
		AttributeID: "IS_CURVED", Nombre: "Es curvo", TipoDato: "boolean", Obligatorio: true,
		ValoresValidos: []store.ValorAtributo{{ID: "242085", Nombre: "Sí"}, {ID: "242084", Nombre: "No"}},
	}
	f := fuente()
	f.Specs = append(f.Specs, struct {
		Clave string `json:"clave"`
		Valor string `json:"valor"`
	}{Clave: "Es curvo", Valor: "Quizá"})

	id, valor := deductor().deducirValor(a, f)
	if id != "" || valor != "" {
		t.Fatalf("una lista cerrada sin equivalencia debe quedar vacía: id=%q valor=%q", id, valor)
	}
}

func TestEquivalenciaLaxaPorEspacios(t *testing.T) {
	a := store.AtributoCanal{
		AttributeID: "CAPACITY", Nombre: "Capacidad", TipoDato: "list",
		ValoresValidos: []store.ValorAtributo{{ID: "77", Nombre: "16GB"}},
	}
	// La spec dice "16 GB" y el catálogo "16GB": deben cruzarse.
	id, valor := deductor().deducirValor(a, fuente())
	if id != "77" || valor != "16GB" {
		t.Fatalf("debía cruzar «16 GB» con «16GB»: id=%q valor=%q", id, valor)
	}
}

func TestModeloSaleDelSKU(t *testing.T) {
	a := store.AtributoCanal{AttributeID: "MODEL", Nombre: "Modelo", TipoDato: "string"}
	_, valor := deductor().deducirValor(a, fuente())
	if valor != "KVR56U46BS8-16" {
		t.Fatalf("el modelo debe salir del SKU, se obtuvo %q", valor)
	}
}

// Nunca se inventa un valor: sin dato, el hueco se queda para que lo llene
// una persona.
func TestAtributoDesconocidoQuedaVacio(t *testing.T) {
	a := store.AtributoCanal{AttributeID: "WEIGHT", Nombre: "Peso total", TipoDato: "number_unit"}
	id, valor := deductor().deducirValor(a, fuente())
	if id != "" || valor != "" {
		t.Fatalf("un atributo sin fuente debe quedar vacío: id=%q valor=%q", id, valor)
	}
}
