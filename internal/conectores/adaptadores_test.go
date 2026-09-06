package conectores_test

import (
	"testing"

	"github.com/mdv/integra/internal/channel"

	_ "github.com/mdv/integra/internal/conectores/falabella"
	_ "github.com/mdv/integra/internal/conectores/mercadolibre"
	_ "github.com/mdv/integra/internal/conectores/shopify"
	_ "github.com/mdv/integra/internal/conectores/woocommerce"
)

// Estas pruebas no tocan la red: comprueban el registro y que las capacidades
// declaradas sean coherentes. El núcleo se fía de Capabilities para decidir
// cómo tratar ofertas, lotes y títulos, así que una capacidad mal declarada
// produce fallos que solo se verían en producción.

func TestLosAdaptadoresSeRegistran(t *testing.T) {
	for _, k := range []channel.Kind{
		channel.Shopify, channel.WooCommerce, channel.MercadoLibre, channel.Falabella} {
		if !channel.EstaRegistrado(k) {
			t.Errorf("el canal %s no está registrado", k)
		}
	}
}

func TestCredencialesIncompletasFallanAlConstruir(t *testing.T) {
	casos := []struct {
		canal channel.Kind
		cred  map[string]string
	}{
		{channel.Shopify, map[string]string{"tienda": "x.myshopify.com"}}, // sin token
		{channel.WooCommerce, map[string]string{"url": "https://x.com"}},  // sin claves
		{channel.MercadoLibre, map[string]string{"app_id": "123"}},        // sin secret ni tokens
		{channel.Falabella, map[string]string{"user_id": "a@b.com"}},      // sin API key
	}
	for _, c := range casos {
		if _, err := channel.New(c.canal, channel.Config{Credentials: c.cred}); err == nil {
			t.Errorf("%s: construir con credenciales incompletas debía fallar", c.canal)
		}
	}
}

func TestCapacidadesCoherentes(t *testing.T) {
	casos := []struct {
		canal channel.Kind
		cred  map[string]string
	}{
		{channel.Shopify, map[string]string{"tienda": "x.myshopify.com", "token": "shpat_x"}},
		{channel.WooCommerce, map[string]string{
			"url": "https://x.com", "consumer_key": "ck", "consumer_secret": "cs"}},
		{channel.MercadoLibre, map[string]string{"access_token": "APP_USR-x"}},
		{channel.Falabella, map[string]string{"user_id": "a@b.com", "api_key": "k"}},
	}
	for _, c := range casos {
		ad, err := channel.New(c.canal, channel.Config{Credentials: c.cred})
		if err != nil {
			t.Fatalf("%s: %v", c.canal, err)
		}
		if ad.Kind() != c.canal {
			t.Errorf("%s: Kind() devolvió %s", c.canal, ad.Kind())
		}
		cap := ad.Capabilities()
		if cap.MaxTitleLength <= 0 {
			t.Errorf("%s: MaxTitleLength debe ser positivo para poder recortar títulos", c.canal)
		}
		if cap.MaxBatchSize <= 0 {
			t.Errorf("%s: MaxBatchSize debe ser al menos 1", c.canal)
		}
		// Declarar lotes sin tamaño de lote sería una contradicción que el
		// planificador no sabría resolver.
		if (cap.BulkPriceUpdate || cap.BulkStockUpdate) && cap.MaxBatchSize < 2 {
			t.Errorf("%s: declara envíos por lote pero MaxBatchSize es %d", c.canal, cap.MaxBatchSize)
		}
	}
}

// MercadoLibre es el más restrictivo y de él dependen decisiones del núcleo:
// si estos límites cambiaran sin querer, se publicarían fichas que rechaza.
func TestMercadoLibreDeclaraSusRestricciones(t *testing.T) {
	ad, err := channel.New(channel.MercadoLibre,
		channel.Config{Credentials: map[string]string{"access_token": "APP_USR-x"}})
	if err != nil {
		t.Fatal(err)
	}
	cap := ad.Capabilities()
	if cap.MaxTitleLength != 60 {
		t.Errorf("MercadoLibre limita el título a 60 caracteres, declara %d", cap.MaxTitleLength)
	}
	if !cap.RequiresCategoryMapping {
		t.Error("MercadoLibre exige categoría: sin ella rechaza la publicación")
	}
	if !cap.RequiresDescription {
		t.Error("MercadoLibre exige descripción")
	}
	if !cap.RequiresOAuthRefresh {
		t.Error("el token de MercadoLibre caduca: debe declarar que necesita refresco")
	}
}

// Falabella es el único canal asíncrono: sus escrituras devuelven un FeedID
// y el resultado se consulta después. El núcleo distingue ese caso por
// AsyncFeeds, así que la declaración tiene que ser correcta.
func TestSoloFalabellaEsAsincrono(t *testing.T) {
	casos := []struct {
		canal   channel.Kind
		cred    map[string]string
		asincro bool
	}{
		{channel.Shopify, map[string]string{"tienda": "x.myshopify.com", "token": "t"}, false},
		{channel.WooCommerce, map[string]string{
			"url": "https://x.com", "consumer_key": "ck", "consumer_secret": "cs"}, false},
		{channel.MercadoLibre, map[string]string{"access_token": "t"}, false},
		{channel.Falabella, map[string]string{"user_id": "a@b.com", "api_key": "k"}, true},
	}
	for _, c := range casos {
		ad, err := channel.New(c.canal, channel.Config{Credentials: c.cred})
		if err != nil {
			t.Fatalf("%s: %v", c.canal, err)
		}
		if got := ad.Capabilities().AsyncFeeds; got != c.asincro {
			t.Errorf("%s: AsyncFeeds = %v, se esperaba %v", c.canal, got, c.asincro)
		}
	}
}

func TestTiendasPropiasNoExigenCategoria(t *testing.T) {
	casos := []struct {
		canal channel.Kind
		cred  map[string]string
	}{
		{channel.Shopify, map[string]string{"tienda": "x.myshopify.com", "token": "shpat_x"}},
		{channel.WooCommerce, map[string]string{
			"url": "https://x.com", "consumer_key": "ck", "consumer_secret": "cs"}},
	}
	for _, c := range casos {
		ad, _ := channel.New(c.canal, channel.Config{Credentials: c.cred})
		if ad.Capabilities().RequiresCategoryMapping {
			t.Errorf("%s es tienda propia: no debería exigir mapeo de categoría", c.canal)
		}
	}
}
