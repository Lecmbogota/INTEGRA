package mercadolibre

import (
	"strings"
	"unicode"
)

// Coherencia mide si la categoría sugerida tiene algo que ver con la rama de
// Odoo de la que salió.
//
// El predictor de MercadoLibre se engancha a palabras sueltas del título. Con
// el catálogo real de MDV produjo disparates como:
//
//	POS / Cajones Monederos / Billetes: 3   → "Dados"
//	POS / Cómputo / AIO / OS: Windows       → "Sistemas Operativos"
//	Cómputo / Corporativo / OS: Windows     → "Skins"
//
// En los tres casos el nombre sugerido no comparte ni una palabra con la rama
// de origen, mientras que los aciertos sí: "Memorias RAM" contra
// "Almacenamiento / Memorias / RAM / DDR5". Esa señal basta para separar unos
// de otros y no mezclarlos en la misma cola de revisión.
func Coherencia(nombreSugerido, rutaOdoo, familia string) float64 {
	sug := palabrasSignificativas(nombreSugerido)
	if len(sug) == 0 {
		return 0
	}
	origen := palabrasSignificativas(sinRaiz(rutaOdoo) + " " + familia)

	var coinciden int
	for p := range sug {
		if origen[p] {
			coinciden++
			continue
		}
		// Coincidencia por raíz: "impresora"/"impresión", "lector"/"lectores".
		for o := range origen {
			if raizComun(p, o) {
				coinciden++
				break
			}
		}
	}
	return float64(coinciden) / float64(len(sug))
}

// sinRaiz descarta el primer segmento de la ruta de Odoo.
//
// La rama raíz —"POS", "Almacenamiento", "Cómputo"— cubre decenas de
// categorías distintas, así que coincidir con ella no dice nada. Es lo que
// hacía pasar por buena la sugerencia "Terminales POS para tarjetas" para
// "POS / Soportes y Accesorios / Batería Recargable": lo único en común era
// justamente el "POS" del que cuelga medio catálogo.
func sinRaiz(ruta string) string {
	partes := strings.Split(ruta, "/")
	if len(partes) <= 1 {
		return ruta
	}
	return strings.Join(partes[1:], "/")
}

// vacias son palabras que aparecen en cualquier categoría y no aportan señal.
var vacias = map[string]bool{
	"de": true, "del": true, "la": true, "el": true, "los": true, "las": true,
	"y": true, "con": true, "para": true, "en": true, "por": true, "a": true,
	"otros": true, "otras": true, "accesorios": true, "tipo": true,
}

// palabrasSignificativas normaliza un texto a un conjunto de palabras
// comparables: sin acentos, sin signos y sin partículas.
func palabrasSignificativas(s string) map[string]bool {
	out := map[string]bool{}
	var actual strings.Builder

	vaciar := func() {
		p := actual.String()
		actual.Reset()
		if len([]rune(p)) < 3 || vacias[p] {
			return
		}
		out[p] = true
	}

	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			actual.WriteRune(sinAcento(r))
		default:
			vaciar()
		}
	}
	vaciar()
	return out
}

func sinAcento(r rune) rune {
	switch r {
	case 'á', 'à', 'ä', 'â':
		return 'a'
	case 'é', 'è', 'ë', 'ê':
		return 'e'
	case 'í', 'ì', 'ï', 'î':
		return 'i'
	case 'ó', 'ò', 'ö', 'ô':
		return 'o'
	case 'ú', 'ù', 'ü', 'û':
		return 'u'
	case 'ñ':
		return 'n'
	default:
		return r
	}
}

// raizComun compara dos palabras por sus primeras letras.
//
// Cubre las variaciones habituales del español —singular/plural, sustantivo/
// adjetivo— sin necesidad de un lematizador: "impresora"/"impresion" y
// "lector"/"lectores" comparten prefijo, "dados"/"cajones" no.
func raizComun(a, b string) bool {
	const minimo = 5
	ra, rb := []rune(a), []rune(b)
	if len(ra) < minimo || len(rb) < minimo {
		return false
	}
	n := minimo
	for i := 0; i < n; i++ {
		if ra[i] != rb[i] {
			return false
		}
	}
	return true
}

// ConfianzaCon combina la posición en la lista, los atributos deducidos y la
// coherencia con el origen.
//
// Es la que se usa en producción: Confianza a secas no ve la rama de Odoo y
// por tanto no puede detectar las sugerencias disparatadas.
func ConfianzaCon(s Sugerencia, posicion int, rutaOdoo, familia string) float64 {
	base := Confianza(s, posicion)
	coh := Coherencia(s.Categoria, rutaOdoo, familia)

	// La señal útil es binaria: o hay alguna palabra en común o no la hay.
	// Exigir una proporción alta penalizaría injustamente nombres correctos
	// pero más largos que su origen, como "Discos Duros y SSDs" para la rama
	// "Disco / Mecánico / Externo".
	if coh == 0 {
		return base * 0.25
	}
	return base
}

// RequiereRevision marca las sugerencias que no conviene confirmar en bloque.
func RequiereRevision(confianza float64) bool { return confianza < 0.5 }
