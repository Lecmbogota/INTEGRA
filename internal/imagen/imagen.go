// Package imagen procesa y valida las fotografías de producto.
//
// Existe porque las imágenes de Odoo no sirven para publicar: son miniaturas
// WebP de unos 2,5 KB con resoluciones de 150×150, muy por debajo del mínimo
// de cualquier marketplace. Integra guarda las suyas, las normaliza y avisa
// antes de publicar si alguna no cumple.
package imagen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	_ "image/png" // registra el decodificador PNG

	drawx "golang.org/x/image/draw" // escalado de calidad, que el estándar no trae
	_ "golang.org/x/image/webp"     // registra el decodificador WebP: es lo que guarda Odoo
)

// blanco es el fondo de las imágenes cuadradas. Las fichas de producto de los
// marketplaces lo esperan así, y evita que un PNG con transparencia acabe
// mostrándose sobre negro.
var blanco = color.RGBA{255, 255, 255, 255}

// Formatos aceptados a la entrada.
const (
	FormatoJPEG = "jpeg"
	FormatoPNG  = "png"
	FormatoWebP = "webp"
)

// Info describe una imagen decodificada.
type Info struct {
	SHA256  string
	Formato string
	Ancho   int
	Alto    int
	Bytes   int
}

// Errores de validación de entrada.
var (
	ErrFormatoNoSoportado = fmt.Errorf("formato no soportado: se admite JPEG, PNG o WebP")
	ErrDemasiadoGrande    = fmt.Errorf("la imagen supera el tamaño máximo")
	ErrVacia              = fmt.Errorf("el fichero está vacío")
)

// MaxBytesEntrada limita lo que se acepta subir. 25 MB cubre cualquier foto de
// producto razonable y evita que un fichero enorme agote la memoria.
const MaxBytesEntrada = 25 << 20

// Analizar decodifica la imagen y devuelve sus datos, sin guardarla.
func Analizar(datos []byte) (Info, image.Image, error) {
	if len(datos) == 0 {
		return Info{}, nil, ErrVacia
	}
	if len(datos) > MaxBytesEntrada {
		return Info{}, nil, fmt.Errorf("%w (%d bytes, máximo %d)", ErrDemasiadoGrande, len(datos), MaxBytesEntrada)
	}

	img, formato, err := image.Decode(bytes.NewReader(datos))
	if err != nil {
		return Info{}, nil, fmt.Errorf("%w: %v", ErrFormatoNoSoportado, err)
	}
	switch formato {
	case FormatoJPEG, FormatoPNG, FormatoWebP:
	default:
		return Info{}, nil, fmt.Errorf("%w (se detectó %q)", ErrFormatoNoSoportado, formato)
	}

	suma := sha256.Sum256(datos)
	b := img.Bounds()
	return Info{
		SHA256:  hex.EncodeToString(suma[:]),
		Formato: formato,
		Ancho:   b.Dx(),
		Alto:    b.Dy(),
		Bytes:   len(datos),
	}, img, nil
}

// Variante es una versión derivada que se genera al subir.
type Variante struct {
	Nombre string
	Lado   int
	// Cuadrada rellena con blanco hasta dejar la imagen cuadrada, que es lo
	// que piden los marketplaces para las fichas de producto.
	Cuadrada bool
	Calidad  int
}

// VariantesPorDefecto cubre lo que necesitan los cuatro canales.
var VariantesPorDefecto = []Variante{
	{Nombre: "cuadrada_1200", Lado: 1200, Cuadrada: true, Calidad: 88},
	{Nombre: "web_800", Lado: 800, Calidad: 85},
	{Nombre: "miniatura_300", Lado: 300, Calidad: 80},
}

// Procesar genera una derivada en JPEG.
//
// Siempre JPEG: es el único formato que aceptan los cuatro canales sin
// discusión, y el WebP que guarda Odoo lo rechazan varios.
func Procesar(src image.Image, v Variante) ([]byte, Info, error) {
	dst := redimensionar(src, v.Lado, v.Cuadrada)

	calidad := v.Calidad
	if calidad <= 0 {
		calidad = 85
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: calidad}); err != nil {
		return nil, Info{}, fmt.Errorf("codificando %s: %w", v.Nombre, err)
	}

	datos := buf.Bytes()
	suma := sha256.Sum256(datos)
	b := dst.Bounds()
	return datos, Info{
		SHA256:  hex.EncodeToString(suma[:]),
		Formato: FormatoJPEG,
		Ancho:   b.Dx(),
		Alto:    b.Dy(),
		Bytes:   len(datos),
	}, nil
}

// TranscodificarJPEG re-codifica una imagen ya decodificada a JPEG a tamaño
// completo, sin redimensionar. Calidad 90: en fotos de producto la diferencia
// con el original es imperceptible y el peso baja frente al PNG.
func TranscodificarJPEG(src image.Image) ([]byte, Info, error) {
	// Fondo blanco por si la fuente traía transparencia: JPEG no la soporta y
	// sin esto quedaría negra.
	b := src.Bounds()
	lienzo := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(lienzo, lienzo.Bounds(), image.NewUniform(blanco), image.Point{}, draw.Src)
	draw.Draw(lienzo, lienzo.Bounds(), src, b.Min, draw.Over)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, lienzo, &jpeg.Options{Quality: 90}); err != nil {
		return nil, Info{}, fmt.Errorf("transcodificando a JPEG: %w", err)
	}
	datos := buf.Bytes()
	suma := sha256.Sum256(datos)
	return datos, Info{
		SHA256:  hex.EncodeToString(suma[:]),
		Formato: FormatoJPEG,
		Ancho:   b.Dx(),
		Alto:    b.Dy(),
		Bytes:   len(datos),
	}, nil
}

// redimensionar escala la imagen para que quepa en un cuadrado de lado píxeles.
//
// Nunca amplía: estirar una foto de 150×150 a 1200×1200 produce un borrón que
// se ve peor que la original y no engaña al comprador. Si la fuente es
// pequeña, se conserva su tamaño y la validación lo marcará.
func redimensionar(src image.Image, lado int, cuadrada bool) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	escala := 1.0
	if mayor := max(w, h); mayor > lado {
		escala = float64(lado) / float64(mayor)
	}
	nw, nh := int(float64(w)*escala), int(float64(h)*escala)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}

	escalada := image.NewRGBA(image.Rect(0, 0, nw, nh))
	drawx.CatmullRom.Scale(escalada, escalada.Bounds(), src, b, drawx.Over, nil)

	if !cuadrada {
		return escalada
	}

	// Lienzo cuadrado con fondo blanco: es el fondo que piden las fichas de
	// producto y evita el negro que dejaría un PNG transparente.
	ladoFinal := max(nw, nh)
	lienzo := image.NewRGBA(image.Rect(0, 0, ladoFinal, ladoFinal))
	draw.Draw(lienzo, lienzo.Bounds(), image.NewUniform(blanco), image.Point{}, draw.Src)

	off := image.Pt((ladoFinal-nw)/2, (ladoFinal-nh)/2)
	draw.Draw(lienzo, image.Rectangle{Min: off, Max: off.Add(image.Pt(nw, nh))},
		escalada, image.Point{}, draw.Over)
	return lienzo
}
