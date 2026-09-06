package imagen

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// pngDe genera un PNG sólido del tamaño pedido.
func pngDe(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAnalizar(t *testing.T) {
	datos := pngDe(t, 800, 600, color.RGBA{200, 30, 30, 255})

	inf, img, err := Analizar(datos)
	if err != nil {
		t.Fatal(err)
	}
	if inf.Ancho != 800 || inf.Alto != 600 {
		t.Errorf("dimensiones = %dx%d", inf.Ancho, inf.Alto)
	}
	if inf.Formato != FormatoPNG {
		t.Errorf("formato = %q", inf.Formato)
	}
	if inf.Bytes != len(datos) {
		t.Errorf("bytes = %d, se esperaba %d", inf.Bytes, len(datos))
	}
	if len(inf.SHA256) != 64 {
		t.Errorf("el hash debería tener 64 caracteres: %q", inf.SHA256)
	}
	if img == nil {
		t.Error("debería devolver la imagen decodificada")
	}

	// El hash es del contenido: el mismo fichero da el mismo hash.
	otra, _, _ := Analizar(datos)
	if otra.SHA256 != inf.SHA256 {
		t.Error("el mismo contenido debería dar el mismo hash")
	}
}

func TestAnalizarEntradasInvalidas(t *testing.T) {
	if _, _, err := Analizar(nil); !errors.Is(err, ErrVacia) {
		t.Errorf("nil debería dar ErrVacia, dio %v", err)
	}
	if _, _, err := Analizar([]byte("esto no es una imagen")); !errors.Is(err, ErrFormatoNoSoportado) {
		t.Errorf("basura debería dar ErrFormatoNoSoportado, dio %v", err)
	}
	grande := make([]byte, MaxBytesEntrada+1)
	if _, _, err := Analizar(grande); !errors.Is(err, ErrDemasiadoGrande) {
		t.Errorf("un fichero enorme debería rechazarse, dio %v", err)
	}
}

func TestProcesarReduceYCuadra(t *testing.T) {
	_, img, err := Analizar(pngDe(t, 2000, 1000, color.RGBA{10, 120, 200, 255}))
	if err != nil {
		t.Fatal(err)
	}

	datos, inf, err := Procesar(img, Variante{Nombre: "cuadrada_1200", Lado: 1200, Cuadrada: true, Calidad: 88})
	if err != nil {
		t.Fatal(err)
	}
	if inf.Ancho != 1200 || inf.Alto != 1200 {
		t.Errorf("la variante cuadrada debería ser 1200x1200, es %dx%d", inf.Ancho, inf.Alto)
	}
	if inf.Formato != FormatoJPEG {
		t.Errorf("las derivadas deben ser JPEG, son %q", inf.Formato)
	}
	// Debe poder decodificarse: si no, el canal recibiría basura.
	if _, err := jpeg.Decode(bytes.NewReader(datos)); err != nil {
		t.Fatalf("la derivada no es un JPEG válido: %v", err)
	}
}

// Ampliar una foto de 150×150 a 1200×1200 produce un borrón que se ve peor
// que la original y además engañaría a la validación.
func TestProcesarNuncaAmplia(t *testing.T) {
	_, img, err := Analizar(pngDe(t, 150, 150, color.RGBA{90, 90, 90, 255}))
	if err != nil {
		t.Fatal(err)
	}

	_, inf, err := Procesar(img, Variante{Nombre: "cuadrada_1200", Lado: 1200, Cuadrada: true})
	if err != nil {
		t.Fatal(err)
	}
	if inf.Ancho > 150 || inf.Alto > 150 {
		t.Fatalf("no debería ampliar: %dx%d a partir de 150x150", inf.Ancho, inf.Alto)
	}
}

func TestProcesarConservaLaProporcion(t *testing.T) {
	_, img, _ := Analizar(pngDe(t, 1600, 400, color.RGBA{0, 0, 0, 255}))

	_, inf, err := Procesar(img, Variante{Nombre: "web_800", Lado: 800})
	if err != nil {
		t.Fatal(err)
	}
	// 1600x400 escalado a lado 800 → 800x200.
	if inf.Ancho != 800 || inf.Alto != 200 {
		t.Errorf("la proporción debería mantenerse: %dx%d", inf.Ancho, inf.Alto)
	}
}

// Las imágenes reales de MDV: 150×150 WebP de 2,5 KB.
func TestValidarConLasImagenesRealesDeOdoo(t *testing.T) {
	odoo := Info{Formato: FormatoWebP, Ancho: 150, Alto: 150, Bytes: 2551}

	pub := Publicable(odoo)
	for canal, ok := range pub {
		if ok {
			t.Errorf("%s no debería aceptar una imagen de 150x150 WebP", canal)
		}
	}

	// Y el motivo tiene que ser explicable, no un "no válida" a secas.
	ps := Validar(odoo, "mercadolibre")
	if len(ps) == 0 {
		t.Fatal("debería explicar por qué no vale")
	}
	var mencionaTamano bool
	for _, p := range ps {
		if p.Bloquea && bytes.Contains([]byte(p.Motivo), []byte("150×150")) {
			mencionaTamano = true
		}
	}
	if !mencionaTamano {
		t.Errorf("el motivo debería citar la resolución real: %+v", ps)
	}
}

func TestValidarPorCanal(t *testing.T) {
	// 450×450 pasa en WooCommerce (mínimo 300) y en Shopify (400), pero no en
	// MercadoLibre (500) ni Falabella (600).
	media := Info{Formato: FormatoJPEG, Ancho: 450, Alto: 450, Bytes: 90_000}
	pub := Publicable(media)

	esperado := map[string]bool{
		"woocommerce": true, "shopify": true,
		"mercadolibre": false, "falabella": false,
	}
	for canal, quiero := range esperado {
		if pub[canal] != quiero {
			t.Errorf("%s: publicable=%v, se esperaba %v", canal, pub[canal], quiero)
		}
	}
}

func TestValidarBuenaImagenPasaEnTodos(t *testing.T) {
	buena := Info{Formato: FormatoJPEG, Ancho: 1200, Alto: 1200, Bytes: 400_000}
	for canal, ok := range Publicable(buena) {
		if !ok {
			t.Errorf("%s debería aceptar 1200x1200 JPEG: %+v", canal, Validar(buena, canal))
		}
	}
	// Y sin avisos, porque llega al tamaño recomendado.
	if ps := Validar(buena, "mercadolibre"); len(ps) != 0 {
		t.Errorf("1200x1200 no debería generar avisos en MercadoLibre: %+v", ps)
	}
}

func TestValidarAvisaSinBloquear(t *testing.T) {
	// 600×600 pasa el mínimo de MercadoLibre pero no llega al óptimo.
	justa := Info{Formato: FormatoJPEG, Ancho: 600, Alto: 600, Bytes: 120_000}
	ps := Validar(justa, "mercadolibre")
	if len(ps) != 1 || ps[0].Bloquea {
		t.Fatalf("debería avisar sin bloquear: %+v", ps)
	}
}

func TestValidarFormatoRechazado(t *testing.T) {
	// WebP con buena resolución: Shopify lo acepta, MercadoLibre no.
	webp := Info{Formato: FormatoWebP, Ancho: 1200, Alto: 1200, Bytes: 300_000}
	if Publicable(webp)["mercadolibre"] {
		t.Error("MercadoLibre no debería aceptar WebP")
	}
	if !Publicable(webp)["shopify"] {
		t.Error("Shopify sí debería aceptar WebP")
	}
}

func TestValidarCanalDesconocido(t *testing.T) {
	ps := Validar(Info{Formato: FormatoJPEG, Ancho: 1200, Alto: 1200}, "tiendanube")
	if len(ps) != 1 || !ps[0].Bloquea {
		t.Fatalf("un canal desconocido debería bloquear: %+v", ps)
	}
}
