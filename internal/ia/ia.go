// Package ia redacta fichas de producto.
//
// Hay dos proveedores y el mismo contrato para ambos: un modelo local servido
// por Ollama, que es el que se usa por defecto, y la API de Claude para cuando
// haga falta más calidad en un lote concreto. Se elige con IA_PROVEEDOR.
//
// El motivo de que lo local mande es que el catálogo de MDV se redacta una vez
// y se retoca a mano después: pagar por token cada vez que se reescriben
// cuatrocientas fichas no compensa, y las descripciones pasan igualmente por
// revisión humana en la interfaz.
package ia

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Proveedor es lo que Integra necesita de un modelo, sea local o remoto.
type Proveedor interface {
	// GenerarFicha redacta títulos por canal, descripción y especificaciones.
	GenerarFicha(ctx context.Context, nombre, marca, categoria, sku, specsPrevias string) (*Ficha, error)
	// Comprobar valida la configuración ANTES de empezar un lote: que el
	// servicio responda, que el modelo esté descargado y que sepa devolver
	// JSON. Descubrir eso en el producto 300 de 450 es la peor forma de
	// enterarse.
	Comprobar(ctx context.Context) error
	// Descripcion es lo que se imprime en pantalla al arrancar un lote.
	Descripcion() string
}

// Ficha es el contenido generado para un producto.
type Ficha struct {
	Titulos     map[string]string `json:"titulos"`
	Descripcion string            `json:"descripcion"`
	Specs       []Spec            `json:"specs"`
}

type Spec struct {
	Clave string `json:"clave"`
	Valor string `json:"valor"`
}

// Nuevo devuelve el proveedor configurado, ya comprobado.
//
// Devuelve error en vez de un cliente a medias: el coste de un diagnóstico
// tardío es un lote largo que se cae por la mitad, con la mitad de las fichas
// escritas y sin saber cuáles.
func Nuevo(ctx context.Context) (Proveedor, error) {
	var p Proveedor
	switch strings.ToLower(strings.TrimSpace(os.Getenv("IA_PROVEEDOR"))) {
	case "", "ollama", "local":
		p = NuevoOllama()
	case "anthropic", "claude":
		p = NuevoAnthropic()
	case "auto":
		// «auto» prefiere lo local y solo sale a la red si no hay nada
		// escuchando: útil en un portátil que a veces tiene Ollama abierto.
		o := NuevoOllama()
		if err := o.Comprobar(ctx); err == nil {
			return o, nil
		}
		p = NuevoAnthropic()
	default:
		return nil, fmt.Errorf("IA_PROVEEDOR=%q no se reconoce; usa «ollama», «anthropic» o «auto»",
			os.Getenv("IA_PROVEEDOR"))
	}

	if err := p.Comprobar(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// --------------------------------------------------------------- prompts

// Los prompts son los mismos para los dos proveedores: si divergen, comparar
// la calidad de uno contra otro deja de significar nada.

// promptFicha está escrito para el peor lector, no para el mejor.
//
// La primera versión pedía las cosas en prosa y Claude la seguía bien; un
// modelo de 3B devolvía el nombre del ERP copiado tal cual en los cuatro
// canales, empezando por el SKU, y una descripción de tres líneas con
// invenciones («velocidad de transferencia de N300», que es la línea de
// producto y no una velocidad). Las reglas numeradas, las prohibiciones
// explícitas y el ejemplo resuelven eso y no le quitan nada a un modelo bueno.
func promptFicha(nombre, marca, categoria, sku, specsPrevias string) string {
	if specsPrevias == "" {
		specsPrevias = "(ninguna)"
	}
	return fmt.Sprintf(`Eres redactor de fichas de producto para e-commerce colombiano.
Escribes en español neutro de Colombia.

DATOS DEL PRODUCTO (vienen de un ERP y son crípticos a propósito):
- Nombre: %s
- Marca: %s
- Categoría: %s
- SKU: %s
- Especificaciones ya extraídas: %s

REGLAS DE LOS TÍTULOS
R1. NUNCA empieces un título por el SKU ni lo incluyas. El SKU es un código
    interno; al comprador no le dice nada.
R2. Empieza siempre por el TIPO DE PRODUCTO en palabras normales
    ("Disco Duro Interno", "Audífonos Inalámbricos", "Impresora Térmica").
R3. Fórmula: Tipo de producto + Marca + Modelo + la spec que más importe.
R4. Los cuatro títulos deben ser DISTINTOS entre sí. El de MercadoLibre es el
    más corto y va al grano; los de WooCommerce y Shopify pueden ser más
    descriptivos.
R5. Límites de caracteres, estrictos:
    mercadolibre 60 | falabella 150 | woocommerce 255 | shopify 255
R6. Sin signos de exclamación, sin MAYÚSCULAS SOSTENIDAS, sin palabras como
    "oferta", "promoción", "envío gratis" ni "el mejor".

REGLAS DE LA DESCRIPCIÓN
R7. Entre 3 y 5 frases, mínimo 400 caracteres. Vendedora pero factual.
R8. Solo puedes afirmar lo que se deduzca del nombre, la categoría o las specs.
    Si no sabes la garantía, la compatibilidad o una cifra, NO la menciones.
R9. Prohibido inventar significados. Si una parte del nombre es un código de
    línea o modelo que no entiendes, trátalo como nombre de modelo y no le
    atribuyas un valor técnico.
R10. No repitas el nombre del ERP palabra por palabra.

EJEMPLO
Entrada: Nombre "HDWG480XZSTA 4TB 3.5\" N300 DESKTOP NAS 7200RPM", Marca "TOSHIBA",
Categoría "Almacenamiento / Discos duros", SKU "HDWG480XZSTA"
Salida:
{"titulos":{
  "mercadolibre":"Disco Duro Interno Toshiba N300 4TB NAS 7200rpm",
  "falabella":"Disco Duro Interno Toshiba N300 de 4TB, 3.5 pulgadas, 7200 rpm, para sistemas NAS",
  "woocommerce":"Disco Duro Interno Toshiba N300 4TB 3.5\" - Optimizado para NAS, 7200 rpm",
  "shopify":"Toshiba N300 4TB - Disco Duro Interno 3.5\" para NAS, 7200 rpm"},
 "descripcion":"El Toshiba N300 de 4TB está diseñado para sistemas de almacenamiento en red que trabajan sin descanso. Su formato de 3.5 pulgadas y su velocidad de 7200 rpm lo hacen apto para equipos NAS de escritorio que necesitan leer y escribir de forma constante. Al ser un disco pensado para funcionamiento continuo, resulta apropiado para respaldos, archivos compartidos y bibliotecas multimedia en casa u oficina. Se instala internamente en bahías estándar de 3.5 pulgadas.",
 "specs":[{"clave":"Capacidad","valor":"4TB"},{"clave":"Formato","valor":"3.5 pulgadas"},{"clave":"Velocidad","valor":"7200 rpm"},{"clave":"Línea","valor":"N300"},{"clave":"Uso recomendado","valor":"Sistemas NAS"}]}

Ahora redacta la ficha del producto de arriba.
Responde ÚNICAMENTE el objeto JSON, sin markdown ni explicación.`,
		nombre, marca, categoria, sku, specsPrevias)
}

// --------------------------------------------------------------- validación

// validarFicha rechaza lo que no sirve para publicar.
//
// Importa más con un modelo local: uno de 3B a veces devuelve el JSON correcto
// pero con la descripción vacía o los títulos en una sola cadena. Guardarlo
// sería peor que fallar, porque el aviso «sin descripción» ya se habría
// cerrado y nadie volvería a mirar ese producto.
// descripcionMinima son los caracteres por debajo de los cuales una
// descripción no es una descripción. Un 3B, si se le deja, devuelve dos frases
// que repiten el nombre del ERP; eso no vende nada y además cierra el aviso
// «sin descripción», dejando el producto peor que antes porque ya nadie lo
// revisa.
const descripcionMinima = 200

func validarFicha(f *Ficha, sku string) error {
	desc := strings.TrimSpace(f.Descripcion)
	if desc == "" {
		return fmt.Errorf("respuesta sin descripción para %q", sku)
	}
	if n := len([]rune(desc)); n < descripcionMinima {
		return fmt.Errorf("descripción de solo %d caracteres para %q; se pidieron al menos %d",
			n, sku, descripcionMinima)
	}
	if len(f.Titulos) == 0 {
		return fmt.Errorf("respuesta sin títulos para %q", sku)
	}
	// Un título que arranca por el SKU es el fallo más visible de un modelo
	// pequeño: copia el nombre del ERP tal cual. Al comprador el código no le
	// dice nada y en MercadoLibre hunde la búsqueda.
	if sku != "" {
		for canal, t := range f.Titulos {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(t)), strings.ToUpper(sku)) {
				return fmt.Errorf("el título de %s para %q empieza por el SKU en vez de por el producto",
					canal, sku)
			}
		}
	}
	// Los límites por canal son la razón de tener un título por canal; un
	// título de 80 caracteres en MercadoLibre lo rechaza el canal, no Integra.
	for canal, max := range map[string]int{
		"mercadolibre": 60, "falabella": 150, "woocommerce": 255, "shopify": 255,
	} {
		if t, ok := f.Titulos[canal]; ok && len([]rune(t)) > max {
			f.Titulos[canal] = recortarTitulo(t, max)
		}
	}
	return nil
}

// recortarTitulo corta por la última palabra entera que quepa: un título
// partido a mitad de palabra se ve peor que uno más corto.
func recortarTitulo(t string, max int) string {
	r := []rune(strings.TrimSpace(t))
	if len(r) <= max {
		return string(r)
	}
	corte := string(r[:max])
	if i := strings.LastIndexAny(corte, " -–—/"); i > max/2 {
		corte = corte[:i]
	}
	return strings.TrimRight(strings.TrimSpace(corte), " -–—/,;:")
}

// extraerJSON tolera que el modelo envuelva el JSON en cercas de markdown o lo
// preceda de un «Aquí tienes el JSON:». Los modelos locales lo hacen bastante
// más que Claude, incluso pidiéndoles que no lo hagan.
func extraerJSON(s string) string {
	s = strings.TrimSpace(s)
	// Algunos modelos con razonamiento escupen un bloque <think> delante.
	if i := strings.Index(s, "</think>"); i >= 0 {
		s = strings.TrimSpace(s[i+len("</think>"):])
	}
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}
