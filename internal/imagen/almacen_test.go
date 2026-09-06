package imagen

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func almacenTemporal(t *testing.T) *Almacen {
	t.Helper()
	a, err := NuevoAlmacen(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestIngerirGuardaOriginalYDerivadas(t *testing.T) {
	a := almacenTemporal(t)
	datos := pngDe(t, 1600, 1200, color.RGBA{20, 140, 90, 255})

	res, err := a.Ingerir(datos, VariantesPorDefecto)
	if err != nil {
		t.Fatal(err)
	}

	if !a.Existe(res.RutaOrig) {
		t.Errorf("el original no se guardó en %s", res.RutaOrig)
	}
	if len(res.Derivadas) != len(VariantesPorDefecto) {
		t.Fatalf("se esperaban %d derivadas, hay %d", len(VariantesPorDefecto), len(res.Derivadas))
	}
	for _, d := range res.Derivadas {
		if !a.Existe(d.Ruta) {
			t.Errorf("falta la derivada %s en %s", d.Variante, d.Ruta)
		}
		if d.Info.Formato != FormatoJPEG {
			t.Errorf("%s debería ser JPEG, es %s", d.Variante, d.Info.Formato)
		}
	}

	// El original se puede recuperar tal cual se subió.
	vuelta, err := a.Leer(res.RutaOrig)
	if err != nil {
		t.Fatal(err)
	}
	if len(vuelta) != len(datos) {
		t.Errorf("el original cambió de tamaño: %d vs %d", len(vuelta), len(datos))
	}
}

// Subir dos veces la misma foto debe ocupar una sola vez: es lo que hace
// barato reutilizar imágenes de fabricante entre variantes de un modelo.
func TestIngerirDeduplica(t *testing.T) {
	a := almacenTemporal(t)
	datos := pngDe(t, 800, 800, color.RGBA{7, 7, 7, 255})

	r1, err := a.Ingerir(datos, VariantesPorDefecto)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := a.Ingerir(datos, VariantesPorDefecto)
	if err != nil {
		t.Fatal(err)
	}

	if r1.Original.SHA256 != r2.Original.SHA256 {
		t.Error("el mismo contenido debería dar el mismo hash")
	}
	if r1.RutaOrig != r2.RutaOrig {
		t.Errorf("debería reutilizar la ruta: %s vs %s", r1.RutaOrig, r2.RutaOrig)
	}
}

func TestRepartePorSubdirectorios(t *testing.T) {
	a := almacenTemporal(t)
	res, err := a.Ingerir(pngDe(t, 400, 400, color.RGBA{1, 2, 3, 255}), nil)
	if err != nil {
		t.Fatal(err)
	}
	// La ruta debe empezar por los dos primeros caracteres del hash.
	prefijo := res.Original.SHA256[:2]
	if !strings.HasPrefix(filepath.ToSlash(res.RutaOrig), prefijo+"/") {
		t.Errorf("ruta = %s, debería empezar por %s/", res.RutaOrig, prefijo)
	}
}

// El hash llega desde la API, así que no puede permitirse que contenga
// separadores y escape del directorio del almacén.
func TestRutaRechazaHashesPeligrosos(t *testing.T) {
	a := almacenTemporal(t)
	for _, malo := range []string{
		"../../etc/passwd", "ab/cd", "ABCDEF", "ghijkl", "..", "a",
	} {
		if _, err := a.Ruta(malo, "", "jpg"); err == nil {
			t.Errorf("debería rechazar el hash %q", malo)
		}
	}
	// Y uno válido debe pasar.
	if _, err := a.Ruta("abc123def456", "", "jpg"); err != nil {
		t.Errorf("un hash hexadecimal debería aceptarse: %v", err)
	}
}

func TestRutaRechazaVariantesPeligrosas(t *testing.T) {
	a := almacenTemporal(t)
	for _, mala := range []string{"../x", "a/b", "MAY", "con espacio"} {
		if _, err := a.Ruta("abc123", mala, "jpg"); err == nil {
			t.Errorf("debería rechazar la variante %q", mala)
		}
	}
	// La variante vacía no es peligrosa: identifica al original.
	if _, err := a.Ruta("abc123", "", "jpg"); err != nil {
		t.Errorf("la variante vacía debería aceptarse como original: %v", err)
	}
}

func TestLeerYBorrarNoEscapanDelAlmacen(t *testing.T) {
	a := almacenTemporal(t)
	for _, malo := range []string{"../secreto.txt", "../../etc/passwd"} {
		if _, err := a.Leer(malo); err == nil {
			t.Errorf("Leer(%q) debería fallar", malo)
		}
		if err := a.Borrar(malo); err == nil {
			t.Errorf("Borrar(%q) debería fallar", malo)
		}
	}
}

func TestBorrarEsIdempotente(t *testing.T) {
	a := almacenTemporal(t)
	res, _ := a.Ingerir(pngDe(t, 300, 300, color.RGBA{9, 9, 9, 255}), nil)

	if err := a.Borrar(res.RutaOrig); err != nil {
		t.Fatal(err)
	}
	// Borrar lo que ya no está no es un error.
	if err := a.Borrar(res.RutaOrig); err != nil {
		t.Errorf("borrar dos veces debería ser inofensivo: %v", err)
	}
}

func TestGuardarEsAtomico(t *testing.T) {
	a := almacenTemporal(t)
	res, err := a.Ingerir(pngDe(t, 500, 500, color.RGBA{4, 4, 4, 255}), nil)
	if err != nil {
		t.Fatal(err)
	}
	// No debe quedar ningún .tmp suelto tras una escritura correcta.
	err = filepath.Walk(a.raiz, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".tmp") {
			t.Errorf("quedó un fichero temporal: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
}

func TestNuevoAlmacenRechazaRutaVacia(t *testing.T) {
	if _, err := NuevoAlmacen("   "); err == nil {
		t.Error("una ruta vacía debería rechazarse")
	}
}

func TestTipoMIME(t *testing.T) {
	casos := map[string]string{
		"ab/abc.jpg": "image/jpeg", "ab/abc.jpeg": "image/jpeg",
		"ab/abc.png": "image/png", "ab/abc.webp": "image/webp",
		"ab/abc.bin": "application/octet-stream",
	}
	for ruta, esperado := range casos {
		if got := TipoMIME(ruta); got != esperado {
			t.Errorf("TipoMIME(%q) = %q, se esperaba %q", ruta, got, esperado)
		}
	}
}
