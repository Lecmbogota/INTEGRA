package content

import (
	"fmt"
	"regexp"
	"strings"
)

// Spec es una especificación extraída del nombre o de la categoría.
type Spec struct {
	Clave  string // "Capacidad", "Velocidad", "Formato"…
	Valor  string // "16 GB", "DDR5-5600", "SODIMM"
	Origen string // "nombre" o "categoria"
	// Prioridad ordena qué sobrevive cuando el título no cabe. Menor gana.
	Prioridad int
}

type patron struct {
	clave     string
	prioridad int
	re        *regexp.Regexp
	// formatea convierte los grupos capturados en el valor final.
	formatea func([]string) string
}

// Los patrones están ordenados por lo que de verdad aparece en el catálogo de
// MDV: memorias, discos, impresoras POS, monitores y tabletas.
var patrones = []patron{
	{
		clave: "Capacidad", prioridad: 10,
		re: regexp.MustCompile(`(?i)\b(\d{1,4})\s?(GB|TB|MB)\b`),
		formatea: func(g []string) string {
			return g[1] + " " + strings.ToUpper(g[2])
		},
	},
	{
		clave: "Tecnología", prioridad: 20,
		re: regexp.MustCompile(`(?i)\b(DDR[2-5])\b(?:[-\s]?(\d{4,5}))?`),
		formatea: func(g []string) string {
			t := strings.ToUpper(g[1])
			if g[2] != "" {
				return t + "-" + g[2]
			}
			return t
		},
	},
	{
		clave: "Formato", prioridad: 30,
		re:       regexp.MustCompile(`(?i)\b(SO-?DIMM|U-?DIMM|DIMM)\b`),
		formatea: func(g []string) string { return strings.ToUpper(strings.ReplaceAll(g[1], "-", "")) },
	},
	// La interfaz va en patrones separados y ordenados de más específica a más
	// genérica. Con una sola alternancia ganaría la coincidencia situada más a
	// la izquierda, y en "…M2 500 GB PCI Express 30 NVMe" eso devolvía "M.2"
	// cuando el dato que importa es NVMe.
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bNVMe\b`),
		formatea: func([]string) string { return "NVMe" },
	},
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bSATA\b`),
		formatea: func([]string) string { return "SATA" },
	},
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bUSB-?C\b`),
		formatea: func([]string) string { return "USB-C" },
	},
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bUSB\s?(\d(?:\.\d)?)\b`),
		formatea: func(g []string) string { return "USB " + g[1] },
	},
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bM\.?2\b`),
		formatea: func([]string) string { return "M.2" },
	},
	{
		clave: "Interfaz", prioridad: 25,
		re:       regexp.MustCompile(`(?i)\bPCIe?(?:\s?Express)?\b`),
		formatea: func([]string) string { return "PCIe" },
	},
	{
		clave: "Velocidad", prioridad: 40,
		re: regexp.MustCompile(`(?i)\b(\d{3,5})\s?(MHZ|MT/S)\b`),
		formatea: func(g []string) string {
			u := strings.ToUpper(g[2])
			if u == "MT/S" {
				u = "MT/s" // la unidad se escribe con ese-minúscula
			}
			return g[1] + " " + u
		},
	},
	{
		clave: "Velocidad", prioridad: 40,
		re:       regexp.MustCompile(`(?i)\b(\d{3,5})\s?MB/?s\b`),
		formatea: func(g []string) string { return g[1] + " MB/s" },
	},
	{
		clave: "Revoluciones", prioridad: 45,
		re:       regexp.MustCompile(`(?i)\b(\d{4})\s?RPM\b`),
		formatea: func(g []string) string { return g[1] + " RPM" },
	},
	{
		clave: "Tamaño", prioridad: 35,
		re:       regexp.MustCompile(`\b(\d{1,2}(?:[.,]\d)?)\s?["”]`),
		formatea: func(g []string) string { return strings.Replace(g[1], ",", ".", 1) + `"` },
	},
	{
		clave: "Color", prioridad: 60,
		re:       regexp.MustCompile(`(?i)\b(negro|blanco|gris|plata|azul|rojo|verde|black|white|silver)\b`),
		formatea: func(g []string) string { return capitalizar(strings.ToLower(g[1])) },
	},
}

func normalizarInterfaz(s string) string {
	u := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	switch {
	case strings.HasPrefix(u, "NVME"):
		return "NVMe"
	case strings.HasPrefix(u, "USB-C"), u == "USBC":
		return "USB-C"
	case strings.HasPrefix(u, "USB"):
		// USB3.0 → USB 3.0
		resto := strings.TrimPrefix(u, "USB")
		if resto == "" {
			return "USB"
		}
		return "USB " + resto
	case strings.HasPrefix(u, "SATA"):
		return "SATA"
	case strings.HasPrefix(u, "PCI"):
		return "PCIe"
	case strings.HasPrefix(u, "M2"), strings.HasPrefix(u, "M.2"):
		return "M.2"
	default:
		return s
	}
}

func capitalizar(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// ExtraerSpecs saca las especificaciones del nombre.
//
// No se deduce nada de la categoría por sí sola: que un producto cuelgue de
// "DDR5" sugiere la tecnología, pero si el nombre no la dice puede tratarse de
// un producto mal categorizado. Solo se confirma lo que ambos coinciden.
func ExtraerSpecs(nombre, categPath string) []Spec {
	vistos := map[string]bool{}
	var out []Spec

	for _, p := range patrones {
		m := p.re.FindStringSubmatch(nombre)
		if m == nil {
			continue
		}
		// Se rellenan los grupos que falten para que formatea no se salga.
		g := make([]string, 4)
		copy(g, m)
		valor := p.formatea(g)
		clave := p.clave
		if valor == "" || vistos[clave] {
			continue
		}
		vistos[clave] = true
		out = append(out, Spec{Clave: clave, Valor: valor, Origen: "nombre", Prioridad: p.prioridad})
	}

	// La categoría solo confirma o añade la familia, nunca specs numéricas.
	if fam := FamiliaDeCategoria(categPath); fam != "" && !vistos["Tipo"] {
		out = append([]Spec{{Clave: "Tipo", Valor: fam, Origen: "categoria", Prioridad: 5}}, out...)
	}

	return out
}

// FamiliaDeCategoria traduce la rama de Odoo al sustantivo con el que un
// comprador busca el producto.
//
// Es lo que arregla nombres como "Disco de Estado Solido" o "Memoria": la
// categoría ya dice de qué tipo de producto se trata.
func FamiliaDeCategoria(path string) string {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "memorias / ram"), strings.Contains(p, "memoria ram"):
		return "Memoria RAM"
	case strings.Contains(p, "memorias / microsd"), strings.Contains(p, "microsd"):
		return "Memoria MicroSD"
	case strings.Contains(p, "memorias / usb"):
		return "Memoria USB"
	case strings.Contains(p, "disco / ssd"), strings.Contains(p, "ssd"):
		return "Disco SSD"
	case strings.Contains(p, "disco / mecanico"), strings.Contains(p, "nas"):
		return "Disco Duro"
	case strings.Contains(p, "impresión térmica"), strings.Contains(p, "impresion termica"):
		return "Impresora Térmica"
	case strings.Contains(p, "lector de códigos"), strings.Contains(p, "lector de codigos"):
		return "Lector de Código de Barras"
	case strings.Contains(p, "monitor táctil"), strings.Contains(p, "monitor tactil"):
		return "Monitor Táctil"
	case strings.Contains(p, "monitor"):
		return "Monitor"
	case strings.Contains(p, "tabletas"), strings.Contains(p, "tablet"):
		return "Tablet"
	case strings.Contains(p, "mini cpu"), strings.Contains(p, "mini pc"):
		return "Mini PC"
	case strings.Contains(p, "audífonos"), strings.Contains(p, "audifonos"):
		return "Audífonos"
	case strings.Contains(p, "teclado"):
		return "Teclado"
	case strings.Contains(p, "mouse pad"):
		return "Mouse Pad"
	case strings.Contains(p, "chasis"):
		return "Chasis"
	case strings.Contains(p, "smart rings"), strings.Contains(p, "wearables"):
		return "Anillo Inteligente"
	case strings.Contains(p, "cámaras"), strings.Contains(p, "camaras"):
		return "Cámara"
	case strings.Contains(p, "televisores"):
		return "Televisor"
	default:
		return ""
	}
}

// Resumen devuelve las specs en una línea, para depurar y para la interfaz.
func Resumen(specs []Spec) string {
	var partes []string
	for _, s := range specs {
		partes = append(partes, fmt.Sprintf("%s: %s", s.Clave, s.Valor))
	}
	return strings.Join(partes, " · ")
}
