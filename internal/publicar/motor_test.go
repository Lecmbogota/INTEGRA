package publicar

import (
	"testing"

	"github.com/mdv/integra/internal/store"
)

func base() store.CandidatoPublicacion {
	return store.CandidatoPublicacion{
		VarianteID: 1, SKU: "ABC-123", Titulo: "Disco 1TB", Descripcion: "Un disco.",
		Marca: "Toshiba", Barcode: "7701234567890", Peso: 0.5,
		Imagenes: []string{"aaa", "bbb"}, CategoriaCanal: "MCO1672",
		// Sin comisión, precio de canal == precio base.
		PrecioBase: 100000, PrecioCanal: 100000, Moneda: "COP", Stock: 7,
	}
}

// El punto entero de los tres hashes: mover el stock no puede invalidar el
// contenido, porque reenviar la ficha completa es el envío más caro.
func TestStockNoInvalidaContenidoNiPrecio(t *testing.T) {
	a, b := base(), base()
	b.Stock = 3

	if HashStock(a) == HashStock(b) {
		t.Fatal("cambiar el stock debe cambiar su hash")
	}
	if HashContenido(a) != HashContenido(b) {
		t.Fatal("el stock no debe invalidar el hash de contenido")
	}
	if HashPrecio(a) != HashPrecio(b) {
		t.Fatal("el stock no debe invalidar el hash de precio")
	}
}

func TestPrecioNoInvalidaContenido(t *testing.T) {
	a, b := base(), base()
	b.PrecioCanal = 120000

	if HashPrecio(a) == HashPrecio(b) {
		t.Fatal("cambiar el precio debe cambiar su hash")
	}
	if HashContenido(a) != HashContenido(b) {
		t.Fatal("el precio no debe invalidar el hash de contenido")
	}
}

// Un cambio de comisión no toca el catálogo pero sí el precio publicado: si
// no se reflejara, el canal seguiría vendiendo al precio viejo.
func TestComisionCambiaElHashDePrecio(t *testing.T) {
	a := base()
	b := base()
	b.PrecioCanal = store.PrecioParaCanal(b.PrecioBase, store.Canal{ComisionPct: 14})

	if b.PrecioCanal <= a.PrecioCanal {
		t.Fatalf("la comisión debe subir el precio publicado: %.2f → %.2f", a.PrecioCanal, b.PrecioCanal)
	}
	if HashPrecio(a) == HashPrecio(b) {
		t.Fatal("un precio de canal distinto debe dar un hash distinto")
	}
}

func TestContenidoCubreLosCamposQueSePublican(t *testing.T) {
	casos := []struct {
		nombre string
		mutar  func(*store.CandidatoPublicacion)
	}{
		{"título", func(c *store.CandidatoPublicacion) { c.Titulo = "Otro" }},
		{"descripción", func(c *store.CandidatoPublicacion) { c.Descripcion = "Otra" }},
		{"marca", func(c *store.CandidatoPublicacion) { c.Marca = "Kingston" }},
		{"categoría", func(c *store.CandidatoPublicacion) { c.CategoriaCanal = "MCO9999" }},
		{"barcode", func(c *store.CandidatoPublicacion) { c.Barcode = "7709999999999" }},
		{"peso", func(c *store.CandidatoPublicacion) { c.Peso = 1.25 }},
		{"imágenes", func(c *store.CandidatoPublicacion) { c.Imagenes = []string{"aaa"} }},
		{"orden de imágenes", func(c *store.CandidatoPublicacion) { c.Imagenes = []string{"bbb", "aaa"} }},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			a, b := base(), base()
			caso.mutar(&b)
			if HashContenido(a) == HashContenido(b) {
				t.Fatalf("cambiar %s debe invalidar el hash de contenido", caso.nombre)
			}
		})
	}
}

func TestHashEstableEntreLlamadas(t *testing.T) {
	a, b := base(), base()
	if HashContenido(a) != HashContenido(b) || HashPrecio(a) != HashPrecio(b) || HashStock(a) != HashStock(b) {
		t.Fatal("dos candidatos idénticos deben dar los mismos hashes")
	}
}
