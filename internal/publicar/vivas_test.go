package publicar

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"
)

// ------------------------------------------------------------- utilidades

func trabajoDeCuenta(t *testing.T, cuentaID int64) jobs.Trabajo {
	t.Helper()
	cuerpo, err := json.Marshal(PayloadCuenta{CuentaID: cuentaID})
	if err != nil {
		t.Fatal(err)
	}
	return jobs.Trabajo{ID: 1, Kind: TrabajoConciliar, Payload: cuerpo}
}

func publicadaEn(varianteID int64, listingID, sku string) store.PublicacionViva {
	return store.PublicacionViva{
		VarianteID: varianteID, ProductoID: 10, SKU: sku,
		Ref: channel.ExternalRef{ListingID: listingID, VariantID: listingID, SKU: sku},
	}
}

// ------------------------------------------------- 14: la ficha muerta

// El caso del informe: MercadoLibre da de baja la publicación, o alguien la
// borra a mano en la tienda, y nadie vuelve a preguntar. Integra la sigue
// contando como publicada, cada actualización se estrella contra un 404 hasta
// agotar reintentos y el producto no vuelve al canal jamás.
func TestLaPublicacionQueYaNoExisteEnElCanalSeVuelveAPublicar(t *testing.T) {
	st := &almacenFalso{porConciliar: []store.PublicacionViva{
		publicadaEn(1, "MCO1", "ABC-123"),
		publicadaEn(2, "MCO2", "ABC-124"),
	}}
	// MCO2 no está en el mapa: el canal no devuelve fila para lo que ya no
	// tiene, que es exactamente la señal de baja de los cuatro adaptadores.
	ad := &canalFalso{estados: map[string]string{"MCO1": "active"}}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.conciliar(context.Background(), trabajoDeCuenta(t, 1)); err != nil {
		t.Fatal(err)
	}

	if got := st.marcas[2].veredicto; got != "caida" {
		t.Fatalf("la publicación desaparecida tiene que quedar marcada como caída, no como %q", got)
	}
	if got := st.marcas[1].veredicto; got != "viva" {
		t.Fatalf("la que el canal sí tiene no se puede tocar: quedó %q", got)
	}
	if !cola.tiene(TrabajoPublicar) {
		t.Fatalf("limpiar la referencia no basta: hay que pedir la publicación de nuevo; encolado: %v", cola.encolados)
	}
}

// Que el canal la retirara él mismo no es lo mismo que borrarla: detrás suele
// haber una infracción, y volver a subirla sola convierte un aviso en una
// sanción. Se deja de contar como publicada —para que el aviso salga— pero no
// se recrea.
func TestLaPublicacionQueRetiroElCanalNoSeVuelveASubirSola(t *testing.T) {
	st := &almacenFalso{porConciliar: []store.PublicacionViva{publicadaEn(1, "MCO1", "ABC-123")}}
	ad := &canalFalso{estados: map[string]string{"MCO1": "closed/under_review"}}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.conciliar(context.Background(), trabajoDeCuenta(t, 1)); err != nil {
		t.Fatal(err)
	}

	if got := st.marcas[1].veredicto; got != "retirada" {
		t.Fatalf("una publicación que el canal cerró queda retirada, no %q", got)
	}
	if cola.tiene(TrabajoPublicar) {
		t.Fatalf("no se puede volver a subir lo que el canal retiró: %v", cola.encolados)
	}
}

// Un token caducado o la cuenta equivocada hacen que el canal no reconozca
// nada. Creerlo y recrear el catálogo entero contra el marketplace es mucho
// peor que no hacer nada: es el mismo criterio con el que el sync se niega a
// dar de baja todo el catálogo cuando Odoo no da por vivo ni un producto.
func TestNoSeRecreaElCatalogoEnteroCuandoElCanalNoReconoceNada(t *testing.T) {
	var vivas []store.PublicacionViva
	for i := int64(1); i <= 6; i++ {
		vivas = append(vivas, publicadaEn(i, fmt.Sprintf("MCO%d", i), fmt.Sprintf("SKU-%d", i)))
	}
	st := &almacenFalso{porConciliar: vivas}
	ad := &canalFalso{estados: map[string]string{}} // no reconoce ninguna
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.conciliar(context.Background(), trabajoDeCuenta(t, 1)); err == nil {
		t.Fatal("que el canal no reconozca ni una es una lectura rota: hay que abortar, no dar todo por muerto")
	}
	if len(st.marcas) != 0 {
		t.Fatalf("no se puede marcar nada al abortar: %v", st.marcas)
	}
	if len(cola.encolados) != 0 {
		t.Fatalf("no se puede encolar la republicación del catálogo entero: %v", cola.encolados)
	}
}

// Con la lectura fallando a medias, las referencias que faltan en la respuesta
// no son bajas: son las que no se llegaron a leer. Interpretarlas borraría la
// referencia de publicaciones que están perfectamente vivas.
func TestUnFalloDelCanalNoSeInterpretaComoBaja(t *testing.T) {
	st := &almacenFalso{porConciliar: []store.PublicacionViva{publicadaEn(1, "MCO1", "ABC-123")}}
	ad := &canalFalso{errEstado: fmt.Errorf("502 desde el canal")}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.conciliar(context.Background(), trabajoDeCuenta(t, 1)); err == nil {
		t.Fatal("el fallo del canal tiene que propagarse para que el trabajo reintente")
	}
	if len(st.marcas) != 0 {
		t.Fatalf("con la lectura rota no se marca nada: %v", st.marcas)
	}
}

func TestElVocabularioDeLosCuatroCanalesSeTraduceIgual(t *testing.T) {
	casos := []struct {
		estado string
		quiere veredicto
	}{
		{"", publicacionCaida},                       // ausente en MercadoLibre y Falabella
		{"eliminado", publicacionCaida},              // WooCommerce traduce así su 404
		{"no_encontrado", publicacionCaida},          // Shopify traduce así el suyo
		{"deleted", publicacionCaida},                // Falabella
		{"trash", publicacionCaida},                  // papelera de WordPress
		{"closed/deleted", publicacionCaida},         // subestado de MercadoLibre
		{"active", publicacionViva},                  // MercadoLibre, Shopify, Falabella
		{"publish", publicacionViva},                 // WooCommerce
		{"draft", publicacionRetirada},               // WooCommerce y Shopify
		{"archived", publicacionRetirada},            // Shopify
		{"paused/out_of_stock", publicacionRetirada}, // MercadoLibre
		{"inactive", publicacionRetirada},            // Falabella
		{"palabra_que_no_conocemos", publicacionRetirada},
	}
	for _, c := range casos {
		if got := clasificar(c.estado); got != c.quiere {
			t.Errorf("%q se clasificó como %v y debía ser %v", c.estado, got, c.quiere)
		}
	}
}

// ------------------------------------------ 15: el producto que se archiva

// El escenario del informe: alguien archiva el producto en Odoo. El sync lo
// marca inactivo y deja de ser candidato, así que desaparece de la
// planificación… y su ficha se queda abierta en los cuatro canales vendiendo
// con la última cantidad conocida, congelada para siempre.
func TestElProductoQueDejaDeSerCandidatoSePausaEnElCanal(t *testing.T) {
	cat := &catalogoFalso{
		items:     []store.CandidatoPublicacion{}, // ya no es candidato
		huerfanas: []store.PublicacionViva{publicadaEn(1, "MCO1", "ABC-123")},
	}
	cola := &colaFalsa{}
	plan, err := Planificar(context.Background(), cat, cola, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Pausar != 1 || !cola.tiene(TrabajoPausar) {
		t.Fatalf("la ficha huérfana tiene que pedir su pausa; encolado: %v", cola.encolados)
	}
}

func TestLaPausaLlegaAlCanalYQuedaAnotadaComoDeIntegra(t *testing.T) {
	st := &almacenFalso{ref: channel.ExternalRef{ListingID: "MCO1", VariantID: "MCO1", SKU: "ABC-123"}}
	ad := &canalFalso{}
	s := servicioDePrueba(st, &colaFalsa{}, ad)

	t2 := trabajoDe(t, 1, 1)
	t2.Kind = TrabajoPausar
	if err := s.pausar(context.Background(), t2); err != nil {
		t.Fatal(err)
	}

	if len(ad.pausadas) != 1 || ad.pausadas[0].ListingID != "MCO1" {
		t.Fatalf("la pausa tiene que salir al canal: %v", ad.pausadas)
	}
	if m := st.marcas[1]; m.veredicto != "pausada" || m.detalle != store.PausaCatalogo {
		t.Fatalf("la pausa la decidió Integra y así hay que anotarla, no como %+v", m)
	}
}

// El producto vuelve a Odoo y vuelve a ser candidato. Nadie reabriría la ficha
// por su cuenta: los tres hashes siguen coincidiendo —el canal no cambió
// mientras estaba pausada— así que sin esto se quedaría cerrada para siempre.
func TestLaPublicacionQuePausoIntegraSeReabreCuandoElProductoVuelve(t *testing.T) {
	c := candidatoListo()
	c.ExternalID = "MCO1"
	c.ContentHash, c.PriceHash, c.StockHash = HashContenido(c), HashPrecio(c), HashStock(c)
	c.EstadoPublicacion = "paused"
	c.PausaMotivo = store.PausaCatalogo

	cola := &colaFalsa{}
	plan, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Reanudar != 1 || !cola.tiene(TrabajoReanudar) {
		t.Fatalf("sin cambios de hash nadie la reabriría: %v", cola.encolados)
	}
}

// Y la simétrica: lo que retiró el canal no se reabre solo ni aunque el
// producto siga siendo perfecto candidato.
func TestLaPublicacionQueRetiroElCanalNoSeReabreAlPlanificar(t *testing.T) {
	c := candidatoListo()
	c.ExternalID = "MCO1"
	c.ContentHash, c.PriceHash, c.StockHash = HashContenido(c), HashPrecio(c), HashStock(c)
	c.EstadoPublicacion = "paused"
	c.PausaMotivo = store.PausaCanal

	cola := &colaFalsa{}
	if _, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1); err != nil {
		t.Fatal(err)
	}
	if cola.tiene(TrabajoReanudar) {
		t.Fatalf("reabrir una baja del canal es lo que convierte un aviso en una sanción: %v", cola.encolados)
	}
}

// Pausar algo que ya no está no es un fallo: lo que se quería era que dejara
// de venderse. Se anota como caída para que el motor la recree.
func TestPausarLoQueYaNoExisteLoDejaMarcadoComoCaido(t *testing.T) {
	st := &almacenFalso{ref: channel.ExternalRef{ListingID: "MCO1", SKU: "ABC-123"}}
	ad := &canalFalso{errPausa: channel.ErrNoEncontrado}
	s := servicioDePrueba(st, &colaFalsa{}, ad)

	t2 := trabajoDe(t, 1, 1)
	t2.Kind = TrabajoPausar
	if err := s.pausar(context.Background(), t2); err != nil {
		t.Fatalf("que ya no exista no puede ser un error de la pausa: %v", err)
	}
	if got := st.marcas[1].veredicto; got != "caida" {
		t.Fatalf("quedó como %q", got)
	}
}

// ------------------------------------------------------------------ borrado

// Borrar no es pausar. Pausar deja la ficha con su historial, sus preguntas,
// sus reseñas y su posición en el buscador del canal, listos para volver;
// borrar tira todo eso y la dirección deja de existir. Por eso el motor no lo
// encola jamás por su cuenta y por eso el orden de las dos operaciones —canal
// primero, olvido después— importa tanto.

func trabajoDeBorrado(t *testing.T, cuentaID, varianteID int64) jobs.Trabajo {
	t.Helper()
	cuerpo, err := json.Marshal(PayloadPublicar{CuentaID: cuentaID, VarianteID: varianteID})
	if err != nil {
		t.Fatal(err)
	}
	return jobs.Trabajo{ID: 1, Kind: TrabajoBorrar, Payload: cuerpo}
}

func TestBorrarQuitaLaFichaDelCanalYDespuesSuRastro(t *testing.T) {
	st := &almacenFalso{ref: channel.ExternalRef{ListingID: "777", SKU: "SKU-1"}}
	canal := &canalFalso{}
	s := servicioDePrueba(st, &colaFalsa{}, canal)

	if err := s.borrar(context.Background(), trabajoDeBorrado(t, 5, 42)); err != nil {
		t.Fatal(err)
	}
	if len(canal.borradas) != 1 || canal.borradas[0] != "777" {
		t.Errorf("no se pidió borrar la ficha correcta: %q", canal.borradas)
	}
	if len(st.olvidadas) != 1 || st.olvidadas[0] != 42 {
		t.Errorf("no se olvidó el rastro local: %v", st.olvidadas)
	}
}

func TestSiElCanalNoBorraNoSeOlvidaElRastro(t *testing.T) {
	// Al revés —olvidar primero— un fallo de red dejaría la ficha viva en el
	// canal y a Integra convencida de que no existe: un producto vendiéndose
	// sin que nadie lo vigile ni lo pueda volver a tocar.
	st := &almacenFalso{ref: channel.ExternalRef{ListingID: "777", SKU: "SKU-1"}}
	canal := &canalFalso{errBorrar: &channel.Error{
		Kind: channel.WooCommerce, StatusCode: 502, Message: "la tienda no responde",
	}}
	s := servicioDePrueba(st, &colaFalsa{}, canal)

	if err := s.borrar(context.Background(), trabajoDeBorrado(t, 5, 42)); err == nil {
		t.Fatal("un 502 tiene que devolver error para que el trabajo se reintente")
	}
	if len(st.olvidadas) != 0 {
		t.Error("se olvidó el rastro de una ficha que sigue viva en el canal")
	}
}

func TestUnaFichaQueYaNoExisteSeOlvidaIgual(t *testing.T) {
	// El objetivo era que dejara de existir y ya no existe. Dejar el rastro
	// haría que el motor intentara actualizarla para siempre.
	st := &almacenFalso{ref: channel.ExternalRef{ListingID: "777", SKU: "SKU-1"}}
	canal := &canalFalso{errBorrar: channel.ErrNoEncontrado}
	s := servicioDePrueba(st, &colaFalsa{}, canal)

	if err := s.borrar(context.Background(), trabajoDeBorrado(t, 5, 42)); err != nil {
		t.Fatalf("una ficha ya inexistente no es un fallo: %v", err)
	}
	if len(st.olvidadas) != 1 {
		t.Error("no se limpió el rastro de una ficha que ya no está")
	}
}
