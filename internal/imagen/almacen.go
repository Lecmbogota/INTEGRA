package imagen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Almacen guarda los ficheros en disco, direccionados por su contenido.
//
// El nombre del fichero es el hash de lo que contiene, así que subir dos veces
// la misma foto ocupa una sola vez. Con imágenes de fabricante compartidas
// entre variantes de un mismo modelo, eso ahorra bastante espacio.
//
// Se reparte en subdirectorios por los dos primeros caracteres del hash para
// no acabar con decenas de miles de ficheros en una sola carpeta, que degrada
// el rendimiento del sistema de archivos.
type Almacen struct{ raiz string }

func NuevoAlmacen(raiz string) (*Almacen, error) {
	if strings.TrimSpace(raiz) == "" {
		return nil, fmt.Errorf("la ruta del almacén de imágenes está vacía")
	}
	if err := os.MkdirAll(raiz, 0o755); err != nil {
		return nil, fmt.Errorf("creando el almacén en %s: %w", raiz, err)
	}
	return &Almacen{raiz: raiz}, nil
}

// Ruta devuelve la ruta relativa de una imagen dentro del almacén.
//
// La variante vacía corresponde al original.
func (a *Almacen) Ruta(sha, variante, extension string) (string, error) {
	if len(sha) < 4 {
		return "", fmt.Errorf("hash inválido: %q", sha)
	}
	// Se valida el hash para que no pueda contener separadores y escapar del
	// directorio: el valor llega desde la API.
	for _, r := range sha {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return "", fmt.Errorf("hash inválido: %q", sha)
		}
	}
	nombre := sha
	if variante != "" {
		if !nombreVarianteValido(variante) {
			return "", fmt.Errorf("nombre de variante inválido: %q", variante)
		}
		nombre = sha + "_" + variante
	}
	return filepath.Join(sha[:2], nombre+"."+extension), nil
}

func nombreVarianteValido(v string) bool {
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return v != ""
}

// Guardar escribe un fichero y devuelve su ruta relativa.
func (a *Almacen) Guardar(sha, variante, extension string, datos []byte) (string, error) {
	rel, err := a.Ruta(sha, variante, extension)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(a.raiz, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", fmt.Errorf("creando el directorio de %s: %w", rel, err)
	}

	// Escritura atómica: si el proceso muere a mitad, no queda un fichero
	// corrupto con un nombre que aparenta ser válido.
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, datos, 0o644); err != nil {
		return "", fmt.Errorf("escribiendo %s: %w", rel, err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("publicando %s: %w", rel, err)
	}
	return rel, nil
}

// Leer devuelve el contenido de una imagen del almacén.
func (a *Almacen) Leer(rel string) ([]byte, error) {
	limpio := filepath.Clean(rel)
	if filepath.IsAbs(limpio) || strings.HasPrefix(limpio, "..") {
		return nil, fmt.Errorf("ruta fuera del almacén: %q", rel)
	}
	return os.ReadFile(filepath.Join(a.raiz, limpio))
}

// Existe indica si un fichero ya está guardado.
func (a *Almacen) Existe(rel string) bool {
	_, err := os.Stat(filepath.Join(a.raiz, filepath.Clean(rel)))
	return err == nil
}

// Borrar elimina un fichero. No falla si ya no estaba.
func (a *Almacen) Borrar(rel string) error {
	limpio := filepath.Clean(rel)
	if filepath.IsAbs(limpio) || strings.HasPrefix(limpio, "..") {
		return fmt.Errorf("ruta fuera del almacén: %q", rel)
	}
	err := os.Remove(filepath.Join(a.raiz, limpio))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Resultado agrupa el original y sus derivadas ya guardados.
type Resultado struct {
	Original  Info
	RutaOrig  string
	Derivadas []Derivada
}

type Derivada struct {
	Variante string
	Ruta     string
	Info     Info
}

// Ingerir analiza, guarda y genera las derivadas de una imagen subida.
//
// El WebP se transcodifica a JPEG a tamaño completo antes de guardar:
// MercadoLibre y Falabella no lo aceptan, y medio internet (Shopee, CDNs)
// sirve WebP. Convertirlo en la puerta deja todo el banco publicable en los
// cuatro canales sin perder resolución.
func (a *Almacen) Ingerir(datos []byte, variantes []Variante) (*Resultado, error) {
	inf, img, err := Analizar(datos)
	if err != nil {
		return nil, err
	}
	if inf.Formato == FormatoWebP {
		datos, inf, err = TranscodificarJPEG(img)
		if err != nil {
			return nil, err
		}
	}

	ext := extensionDe(inf.Formato)
	ruta, err := a.Guardar(inf.SHA256, "", ext, datos)
	if err != nil {
		return nil, err
	}

	res := &Resultado{Original: inf, RutaOrig: ruta}
	for _, v := range variantes {
		d, di, err := Procesar(img, v)
		if err != nil {
			return nil, err
		}
		rd, err := a.Guardar(inf.SHA256, v.Nombre, "jpg", d)
		if err != nil {
			return nil, err
		}
		res.Derivadas = append(res.Derivadas, Derivada{Variante: v.Nombre, Ruta: rd, Info: di})
	}
	return res, nil
}

func extensionDe(formato string) string {
	switch formato {
	case FormatoJPEG:
		return "jpg"
	case FormatoPNG:
		return "png"
	case FormatoWebP:
		return "webp"
	default:
		return "bin"
	}
}

// TipoMIME devuelve el Content-Type a partir de la extensión del fichero.
func TipoMIME(ruta string) string {
	switch strings.ToLower(filepath.Ext(ruta)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}
