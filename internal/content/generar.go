package content

import (
	"fmt"
	"sort"
	"strings"
)

// Confianza indica cuánto se puede fiar uno del borrador generado.
type Confianza string

const (
	// ConfianzaAlta: hay tipo de producto, marca y al menos una spec.
	ConfianzaAlta Confianza = "alta"
	// ConfianzaMedia: falta alguna pieza pero el resultado es publicable
	// tras una revisión rápida.
	ConfianzaMedia Confianza = "media"
	// ConfianzaBaja: el origen no da material suficiente. Publicar esto sin
	// que lo mire una persona sería peor que no publicarlo.
	ConfianzaBaja Confianza = "baja"
)

// Fuente es lo que Integra ya sabe del producto.
type Fuente struct {
	Nombre    string
	Marca     string
	CategPath string
	SKU       string
	Peso      float64
}

// Borrador es la propuesta generada, siempre sujeta a revisión humana.
type Borrador struct {
	TituloBase  string            `json:"titulo_base"`
	Titulos     map[string]string `json:"titulos_por_canal"`
	Descripcion string            `json:"descripcion"`
	Specs       []Spec            `json:"specs"`
	Confianza   Confianza         `json:"confianza"`
	Avisos      []string          `json:"avisos"`
}

// LimitesTitulo son los máximos de caracteres de cada canal.
//
// El de MercadoLibre es el que aprieta y por eso condiciona el diseño: hay que
// poder descartar tokens por prioridad en vez de cortar la cadena.
var LimitesTitulo = map[string]int{
	"mercadolibre": 60,
	"falabella":    150,
	"shopify":      255,
	"woocommerce":  255,
}

// Generar produce el borrador de contenido de un producto.
func Generar(f Fuente) Borrador {
	b := Borrador{Titulos: map[string]string{}}

	nombre := NormalizarMayusculas(Limpiar(f.Nombre))
	marca := strings.TrimSpace(f.Marca)
	familia := FamiliaDeCategoria(f.CategPath)
	b.Specs = ExtraerSpecs(nombre, f.CategPath)

	// Un nombre que no llega a tres palabras útiles no describe nada. Es el
	// caso de "Disco de" o "Disco de Estado Solido": sin modelo ni capacidad
	// no hay publicación posible, por mucho que la categoría ayude.
	utiles := palabrasUtiles(nombre)
	if utiles < 3 && len(b.Specs) < 2 {
		b.Confianza = ConfianzaBaja
		b.Avisos = append(b.Avisos, fmt.Sprintf(
			"el nombre en Odoo (%q) no aporta información suficiente; hace falta escribirlo a mano", f.Nombre))
	}

	b.TituloBase = componerBase(familia, marca, nombre, b.Specs)
	for canal, limite := range LimitesTitulo {
		b.Titulos[canal] = ajustar(familia, marca, nombre, b.Specs, limite)
	}
	b.Descripcion = componerDescripcion(f, familia, marca, nombre, b.Specs)

	if b.Confianza == "" {
		b.Confianza = evaluar(familia, marca, b.Specs, &b)
	}
	return b
}

// componerBase arma el título largo, sin recortes.
func componerBase(familia, marca, nombre string, specs []Spec) string {
	tokens := construirTokens(familia, marca, nombre, specs)
	var partes []string
	for _, t := range tokens {
		partes = append(partes, t.texto)
	}
	return strings.Join(partes, " ")
}

type token struct {
	texto     string
	prioridad int
	// obligatorio marca lo que nunca se descarta aunque no quepa.
	obligatorio bool
}

// construirTokens ordena las piezas del título por lo que un comprador busca
// primero: qué es, de qué marca, qué modelo y con qué características.
func construirTokens(familia, marca, nombre string, specs []Spec) []token {
	var out []token
	usados := map[string]bool{}

	añadir := func(txt string, prio int, oblig bool) {
		txt = strings.TrimSpace(txt)
		clave := strings.ToLower(txt)
		if txt == "" || usados[clave] {
			return
		}
		usados[clave] = true
		out = append(out, token{texto: txt, prioridad: prio, obligatorio: oblig})
	}

	// 1. Qué es. Sale de la categoría, que es el dato más fiable.
	if familia != "" && !familiaYaCubierta(nombre, familia) {
		añadir(familia, 1, true)
	}
	// 2. La marca, si el nombre no la lleva ya.
	if marca != "" && !contieneToken(nombre, marca) {
		añadir(presentarMarca(marca), 2, true)
	}
	// 3. El nombre limpio, que suele traer el modelo.
	añadir(nombre, 3, true)

	// 4. Las specs que el nombre no incluya literalmente.
	ordenadas := append([]Spec(nil), specs...)
	sort.SliceStable(ordenadas, func(i, j int) bool { return ordenadas[i].Prioridad < ordenadas[j].Prioridad })
	for _, s := range ordenadas {
		if s.Clave == "Tipo" || specYaEnNombre(nombre, s.Valor) {
			continue
		}
		añadir(s.Valor, 10+s.Prioridad, false)
	}
	return out
}

// presentarMarca arregla las marcas gritadas del campo l10n_co_edi_brand.
func presentarMarca(m string) string {
	if pareceGritado(m + "xxxxxxxx") { // se alarga para superar el umbral mínimo
		return capitalizar(strings.ToLower(m))
	}
	if m == strings.ToUpper(m) && len(m) > 3 {
		return capitalizar(strings.ToLower(m))
	}
	return m
}

// ajustar compone un título que quepa en el límite del canal.
//
// Se descartan tokens por prioridad inversa antes que cortar la cadena: un
// título recortado a mitad de palabra se ve peor y penaliza en buscadores.
func ajustar(familia, marca, nombre string, specs []Spec, limite int) string {
	tokens := construirTokens(familia, marca, nombre, specs)

	unir := func(ts []token) string {
		var p []string
		for _, t := range ts {
			p = append(p, t.texto)
		}
		return strings.Join(p, " ")
	}

	if s := unir(tokens); len([]rune(s)) <= limite {
		return s
	}

	// Se quitan opcionales del menos importante al más importante.
	activos := append([]token(nil), tokens...)
	for {
		idx := -1
		peor := -1
		for i, t := range activos {
			if !t.obligatorio && t.prioridad > peor {
				peor = t.prioridad
				idx = i
			}
		}
		if idx < 0 {
			break
		}
		activos = append(activos[:idx], activos[idx+1:]...)
		if len([]rune(unir(activos))) <= limite {
			return unir(activos)
		}
	}

	// Solo quedan obligatorios y siguen sin caber: se recorta por palabras.
	return recortarPorPalabras(unir(activos), limite)
}

// recortarPorPalabras corta en el último espacio que quepa, nunca a mitad.
func recortarPorPalabras(s string, limite int) string {
	r := []rune(s)
	if len(r) <= limite {
		return s
	}
	corte := string(r[:limite])
	if i := strings.LastIndex(corte, " "); i > limite/2 {
		corte = corte[:i]
	}
	return strings.TrimRight(strings.TrimSpace(corte), " -–—/,")
}

func contieneToken(texto, token string) bool {
	return strings.Contains(strings.ToLower(texto), strings.ToLower(token))
}

// familiaYaCubierta evita el tartamudeo del tipo "Monitor Táctil Monitor 15…"
// o "Disco SSD Sandisk 1TB SSD 2.5 SATA".
//
// Basta con que el nombre mencione cualquier palabra significativa de la
// familia: si ya dice "SSD" o "Monitor", el comprador sabe qué está mirando y
// anteponer la familia completa solo produce una repetición que se lee mal.
func familiaYaCubierta(nombre, familia string) bool {
	if contieneToken(nombre, familia) {
		return true
	}
	for _, palabra := range strings.Fields(familia) {
		// Se ignoran partículas como "de" o "el", que aparecen en cualquier sitio.
		if len([]rune(palabra)) >= 3 && contieneToken(nombre, palabra) {
			return true
		}
	}
	return false
}

// specYaEnNombre comprueba si una especificación ya está en el nombre,
// ignorando el espaciado.
//
// El extractor normaliza "2TB" a "2 TB" para que se lea bien, pero luego hay
// que reconocer que ambas son la misma cosa. Sin esto, "KINGSTON-2TB" acababa
// produciendo "…KINGSTON-2TB Negro 2 TB", con la capacidad repetida.
func specYaEnNombre(nombre, valor string) bool {
	quitar := func(s string) string {
		return strings.ToLower(strings.ReplaceAll(s, " ", ""))
	}
	return strings.Contains(quitar(nombre), quitar(valor))
}

// componerDescripcion arma una descripción a partir de lo conocido.
//
// Deliberadamente sobria: sin adjetivos ni promesas que nadie ha verificado.
// Solo se afirma lo que el origen dice.
func componerDescripcion(f Fuente, familia, marca, nombre string, specs []Spec) string {
	var sb strings.Builder

	intro := nombre
	if familia != "" && !familiaYaCubierta(nombre, familia) {
		intro = familia + " " + nombre
	}
	if marca != "" && !contieneToken(intro, marca) {
		intro += " de " + presentarMarca(marca)
	}
	sb.WriteString(intro + ".\n\n")

	// Ficha técnica solo con lo extraído, nunca con supuestos.
	var fichas []Spec
	for _, s := range specs {
		if s.Clave != "Tipo" {
			fichas = append(fichas, s)
		}
	}
	if len(fichas) > 0 {
		sb.WriteString("Características:\n")
		sort.SliceStable(fichas, func(i, j int) bool { return fichas[i].Prioridad < fichas[j].Prioridad })
		for _, s := range fichas {
			fmt.Fprintf(&sb, "• %s: %s\n", s.Clave, s.Valor)
		}
		sb.WriteString("\n")
	}

	if f.SKU != "" {
		fmt.Fprintf(&sb, "Referencia: %s\n", f.SKU)
	}
	if f.Peso > 0 {
		fmt.Fprintf(&sb, "Peso: %.2f kg\n", f.Peso)
	}
	return strings.TrimSpace(sb.String())
}

func evaluar(familia, marca string, specs []Spec, b *Borrador) Confianza {
	tecnicas := 0
	for _, s := range specs {
		if s.Clave != "Tipo" {
			tecnicas++
		}
	}

	if familia == "" {
		b.Avisos = append(b.Avisos,
			"la categoría de Odoo no corresponde a ninguna familia conocida: revisa el tipo de producto del título")
	}
	if marca == "" {
		b.Avisos = append(b.Avisos, "sin marca en Odoo; varios canales la exigen")
	}
	if tecnicas == 0 {
		b.Avisos = append(b.Avisos,
			"no se extrajo ninguna característica técnica del nombre: la descripción quedará muy pobre")
	}

	switch {
	case familia != "" && marca != "" && tecnicas >= 2:
		return ConfianzaAlta
	case tecnicas >= 1 || (familia != "" && marca != ""):
		return ConfianzaMedia
	default:
		return ConfianzaBaja
	}
}
