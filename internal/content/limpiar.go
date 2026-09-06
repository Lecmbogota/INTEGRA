// Package content genera títulos y descripciones a partir de lo que ya existe
// en Odoo.
//
// Es deterministo y basado en reglas, no generativo: extrae lo que el nombre y
// la categoría ya dicen y lo recompone. Nunca inventa una especificación que no
// esté en el origen, porque publicar "16GB" en un producto que no lo declara
// sería peor que no publicarlo.
//
// Cuando no hay material suficiente lo dice (Confianza baja) en vez de producir
// un texto plausible pero vacío.
package content

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// Marcas de duplicado que dejan los usuarios al copiar productos en Odoo.
	reCopia = regexp.MustCompile(`(?i)\s*\((copia|copy|copia \d+)\)\s*`)
	// Espacios repetidos: en el catálogo de MDV abundan los dobles y triples.
	reEspacios = regexp.MustCompile(`\s+`)
	// Guiones sueltos rodeados de espacios al final o principio.
	reGuionSuelto = regexp.MustCompile(`^\s*[-–—/]\s*|\s*[-–—/]\s*$`)
	// Separadores repetidos: "--", "//", "-/". RE2 no admite retro-referencias,
	// así que se captura el primero y se descartan los siguientes.
	reSeparadores = regexp.MustCompile(`([-–—/,])[-–—/,]+`)
)

// Limpiar normaliza un nombre de producto de Odoo.
func Limpiar(s string) string {
	s = reCopia.ReplaceAllString(s, " ")
	s = reSeparadores.ReplaceAllString(s, "$1")
	s = reEspacios.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	s = reGuionSuelto.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// NormalizarMayusculas convierte los gritos en texto legible.
//
// En el catálogo de MDV conviven "KINGSTON MEMORIA RAM UDIMM DDR4-25600 16GB
// 3200MHZ DESKTOP LP" con "Memoria Kingston 16GB DDR5-5600 UDIMM". Publicar el
// primero tal cual penaliza en MercadoLibre y se ve mal en cualquier tienda.
//
// Los tokens que son siglas o códigos técnicos se dejan intactos: convertir
// "NVMe" en "Nvme" o "SSD" en "Ssd" sería empeorarlo.
func NormalizarMayusculas(s string) string {
	if !pareceGritado(s) {
		return s
	}
	palabras := strings.Split(s, " ")
	for i, p := range palabras {
		if p == "" || esTokenTecnico(p) {
			continue
		}
		runas := []rune(strings.ToLower(p))
		// La primera letra alfabética de la palabra va en mayúscula.
		for j, r := range runas {
			if unicode.IsLetter(r) {
				runas[j] = unicode.ToUpper(r)
				break
			}
		}
		palabras[i] = string(runas)
	}
	return strings.Join(palabras, " ")
}

// pareceGritado detecta un texto mayoritariamente en mayúsculas.
func pareceGritado(s string) bool {
	var altas, letras int
	for _, r := range s {
		if unicode.IsLetter(r) {
			letras++
			if unicode.IsUpper(r) {
				altas++
			}
		}
	}
	// Con pocas letras la proporción no dice nada fiable.
	if letras < 8 {
		return false
	}
	return float64(altas)/float64(letras) > 0.7
}

// tokensTecnicos son siglas y unidades que deben conservar su forma.
var tokensTecnicos = map[string]bool{
	"SSD": true, "HDD": true, "NVME": true, "SATA": true, "USB": true,
	"RAM": true, "DDR": true, "DDR3": true, "DDR4": true, "DDR5": true,
	"SODIMM": true, "UDIMM": true, "DIMM": true, "ECC": true, "LP": true,
	"NAS": true, "PC": true, "PCI": true, "PCIE": true, "GB": true, "TB": true,
	"MB": true, "MHZ": true, "RPM": true, "POS": true, "LED": true, "LCD": true,
	"HDMI": true, "RGB": true, "CPU": true, "GPU": true, "OLED": true,
	"IPS": true, "FHD": true, "UHD": true, "HD": true, "QHD": true, "OS": true,
	"WIFI": true, "USBC": true, "M2": true, "AC": true, "DC": true, "TFT": true,
}

func esTokenTecnico(p string) bool {
	limpio := strings.Trim(strings.ToUpper(p), ".,;:()[]-–—/\"'")
	if limpio == "" {
		return false
	}
	if tokensTecnicos[limpio] {
		return true
	}
	// Códigos alfanuméricos como KVR56U46BS8-32, PC4-25600, SN350, DDR5-5600.
	var tieneLetra, tieneDigito bool
	for _, r := range limpio {
		if unicode.IsLetter(r) {
			tieneLetra = true
		}
		if unicode.IsDigit(r) {
			tieneDigito = true
		}
	}
	return tieneLetra && tieneDigito
}

// palabrasUtiles cuenta los tokens que aportan significado, ignorando
// preposiciones y artículos.
//
// Sirve para detectar nombres inservibles: "Disco de " tiene dos palabras pero
// una sola útil, y con eso no se puede publicar nada.
func palabrasUtiles(s string) int {
	vacias := map[string]bool{
		"de": true, "del": true, "la": true, "el": true, "los": true, "las": true,
		"y": true, "con": true, "para": true, "en": true, "a": true, "un": true,
		"una": true, "por": true,
	}
	n := 0
	for _, p := range strings.Fields(strings.ToLower(s)) {
		p = strings.Trim(p, ".,;:()[]-–—/")
		if p != "" && !vacias[p] {
			n++
		}
	}
	return n
}
