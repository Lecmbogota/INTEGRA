package publicar

import (
	"strings"
	"testing"

	"github.com/mdv/integra/internal/store"
)

// Lo que decide si un producto puede salir a un canal estaba repartido entre
// la consulta de candidatos —el mínimo común— y cada adaptador, que solo lo
// comprueba cuando ya está enviando. Para entonces el trabajo ya ocupó su
// sitio en la cola y quien mira la pantalla ve un error en rojo en vez de un
// producto que nunca estuvo listo. Estas pruebas fijan que el aviso previo
// diga exactamente lo mismo que diría el adaptador.

// completo es un candidato al que no le falta nada para ningún canal.
func completo() store.CandidatoPublicacion {
	return store.CandidatoPublicacion{
		VarianteID: 1, SKU: "SKU-1", Titulo: "Un producto", Descripcion: "Una descripción",
		PrecioBase: 100000, PrecioCanal: 100000, Imagenes: []string{"abc"},
		Barcode: "7701234567890", Peso: 1.5, LargoCm: 20, AnchoCm: 15, AltoCm: 10,
		CategoriaCanal: "MCO1234",
	}
}

func TestLoQueLeFaltaSeDiceAntesDeEncolarNoDespuesDeFallar(t *testing.T) {
	casos := []struct {
		nombre string
		canal  string
		romper func(*store.CandidatoPublicacion)
		espera string
	}{
		{"Falabella sin EAN", "falabella",
			func(c *store.CandidatoPublicacion) { c.Barcode = "" }, "sin EAN"},
		{"Falabella sin peso", "falabella",
			func(c *store.CandidatoPublicacion) { c.Peso = 0 }, "sin peso"},
		{"Falabella sin medidas", "falabella",
			func(c *store.CandidatoPublicacion) { c.AltoCm = 0 }, "medidas del paquete"},
		{"Falabella sin categoría", "falabella",
			func(c *store.CandidatoPublicacion) { c.CategoriaCanal = "" }, "categoría mapeada de Falabella"},
		{"MercadoLibre sin categoría", "mercadolibre",
			func(c *store.CandidatoPublicacion) { c.CategoriaCanal = "" }, "categoría mapeada de MercadoLibre"},
		{"sin fotos, en cualquier canal", "woocommerce",
			func(c *store.CandidatoPublicacion) { c.Imagenes = nil }, "sin fotos"},
		{"sin descripción, en cualquier canal", "woocommerce",
			func(c *store.CandidatoPublicacion) { c.Descripcion = "" }, "sin descripción"},
	}

	for _, cs := range casos {
		t.Run(cs.nombre, func(t *testing.T) {
			c := completo()
			cs.romper(&c)
			e := situacionDe(c, cs.canal)

			if e.Situacion != SitNoPublicable {
				t.Fatalf("se dio por publicable: %q", e.Situacion)
			}
			if !strings.Contains(strings.Join(e.Falta, " | "), cs.espera) {
				t.Errorf("no se dice qué falta: %q, se esperaba algo con %q", e.Falta, cs.espera)
			}
		})
	}
}

func TestLoQueLeFaltaAUnCanalNoSeLeExigeAOtro(t *testing.T) {
	// Un producto sin EAN ni peso es perfectamente publicable en WooCommerce y
	// en Shopify. Exigírselo dejaría fuera de las tiendas propias medio
	// catálogo por un requisito que solo pide Falabella.
	c := completo()
	c.Barcode, c.Peso = "", 0
	c.LargoCm, c.AnchoCm, c.AltoCm = 0, 0, 0

	for _, canal := range []string{"woocommerce", "shopify"} {
		if e := situacionDe(c, canal); e.Situacion != SitNuevo {
			t.Errorf("%s: %q con falta %q; sin EAN ni peso se publica igual", canal, e.Situacion, e.Falta)
		}
	}
	if e := situacionDe(c, "falabella"); e.Situacion != SitNoPublicable {
		t.Errorf("falabella lo dio por publicable sin EAN ni peso: %q", e.Situacion)
	}
}

func TestUnaFichaVivaNoSeLlamaNoPublicableAunqueLeFalteAlgo(t *testing.T) {
	// Decir «no publicable» de una ficha que lleva meses vendiendo confunde
	// más de lo que ayuda: lo que le falta se enumera igual, pero su situación
	// es la que tiene en el canal.
	c := completo()
	c.Barcode = ""
	c.ExternalID = "777"
	c.ContentHash, c.PriceHash, c.StockHash = HashContenido(c), HashPrecio(c), HashStock(c)

	e := situacionDe(c, "falabella")
	if e.Situacion != SitAlDia {
		t.Errorf("una publicación viva y sin cambios salió como %q", e.Situacion)
	}
	if len(e.Falta) == 0 {
		t.Error("se calló lo que le falta: nadie sabría por qué el próximo envío va a fallar")
	}
}

func TestSeDistingueLoNuncaPublicadoDeLoPublicadoYDeLoQueTieneCambios(t *testing.T) {
	// Es la pregunta que ni Productos ni Publicación sabían contestar.
	nuevo := completo()
	if e := situacionDe(nuevo, "woocommerce"); e.Situacion != SitNuevo {
		t.Errorf("un producto que nunca se publicó salió como %q", e.Situacion)
	}

	alDia := completo()
	alDia.ExternalID = "777"
	alDia.ContentHash = HashContenido(alDia)
	alDia.PriceHash = HashPrecio(alDia)
	alDia.StockHash = HashStock(alDia)
	if e := situacionDe(alDia, "woocommerce"); e.Situacion != SitAlDia {
		t.Errorf("un producto publicado y sin cambios salió como %q", e.Situacion)
	}

	// Le cambian el precio en Integra: la ficha del canal se queda con el
	// viejo hasta el próximo envío, y eso tiene que verse.
	conCambios := alDia
	conCambios.PrecioCanal = 120000
	e := situacionDe(conCambios, "woocommerce")
	if e.Situacion != SitPendiente {
		t.Fatalf("un cambio de precio sin enviar salió como %q", e.Situacion)
	}
	if strings.Join(e.Cambios, ",") != "precio" {
		t.Errorf("no se dice qué cambió: %q", e.Cambios)
	}
}

func TestLoQueRetiroElCanalNoSeConfundeConLoQuePausamosNosotros(t *testing.T) {
	// La diferencia decide si se puede reabrir: detrás de una retirada del
	// canal suele haber una infracción.
	base := completo()
	base.ExternalID = "777"
	base.EstadoPublicacion = "paused"

	nuestra := base
	nuestra.PausaMotivo = store.PausaManual
	if e := situacionDe(nuestra, "woocommerce"); e.Situacion != SitPausado {
		t.Errorf("nuestra pausa salió como %q", e.Situacion)
	}

	suya := base
	suya.PausaMotivo = store.PausaCanal
	if e := situacionDe(suya, "woocommerce"); e.Situacion != SitRetirado {
		t.Errorf("la retirada del canal salió como %q", e.Situacion)
	}
}
