// Package proyeccion convierte el producto maestro de Integra en el payload
// concreto que espera cada canal.
//
// La proyección es una función pura: no abre conexiones, no necesita
// credenciales y no tiene efectos. Eso permite dos cosas importantes:
//
//   - enseñar en la interfaz el JSON exacto que se enviaría, antes de conectar
//     una sola tienda
//   - probar el formato de cada canal sin mocks de red
//
// Los adaptadores reales usarán estas mismas funciones y se ocuparán solo del
// transporte. Si la proyección y el envío divergieran, la vista previa
// mentiría, que es justo lo que hay que evitar.
package proyeccion

import (
	"math"
	"sort"
	"strconv"
	"time"
)

// Canal identifica el destino.
type Canal string

const (
	MercadoLibre Canal = "mercadolibre"
	Falabella    Canal = "falabella"
	WooCommerce  Canal = "woocommerce"
	Shopify      Canal = "shopify"
)

// Todos devuelve los canales en orden estable.
func Todos() []Canal {
	return []Canal{WooCommerce, Shopify, MercadoLibre, Falabella}
}

// Spec es una especificación técnica del producto.
type Spec struct {
	Clave string `json:"clave"`
	Valor string `json:"valor"`
}

// Entrada es el producto maestro, ya normalizado y tarificado.
type Entrada struct {
	SKU         string
	Barcode     string
	Titulos     map[string]string // título por canal, ya ajustado a su límite
	Descripcion string
	Marca       string
	CategPath   string
	Specs       []Spec

	// CategoriaCanal es la categoría del canal destino. Vacía mientras no
	// exista el mapeo, que es el caso hoy en todo el catálogo de MDV.
	CategoriaCanal string
	// AtributosCanal son los atributos obligatorios ya resueltos.
	AtributosCanal map[string]string

	Precio       float64
	PrecioOferta float64
	OfertaDesde  *time.Time
	OfertaHasta  *time.Time
	Moneda       string

	Stock    int
	Peso     float64
	Imagenes []string
}

// Severidad clasifica lo que falta.
type Severidad string

const (
	// Bloquea: el canal rechazaría la publicación.
	Bloquea Severidad = "bloquea"
	// Advierte: se publicaría, pero con peor resultado comercial.
	Advierte Severidad = "advierte"
)

// Faltante es un requisito del canal que el producto no cumple.
type Faltante struct {
	Campo     string    `json:"campo"`
	Motivo    string    `json:"motivo"`
	Severidad Severidad `json:"severidad"`
}

// Proyeccion es el resultado: qué se enviaría y qué impide enviarlo.
type Proyeccion struct {
	Canal    Canal  `json:"canal"`
	Metodo   string `json:"metodo"`
	Endpoint string `json:"endpoint"`

	// Payload es el cuerpo exacto de la petición. Es lo que se muestra en la
	// vista previa: si aquí falta la marca, la vista enseña el hueco.
	Payload map[string]any `json:"payload"`

	// Campos derivados para pintar la maqueta sin volver a leer el payload.
	Titulo        string  `json:"titulo"`
	TituloLimite  int     `json:"titulo_limite"`
	Precio        float64 `json:"precio"`
	PrecioTachado float64 `json:"precio_tachado"`
	Stock         int     `json:"stock"`

	Faltantes []Faltante `json:"faltantes"`
	Notas     []string   `json:"notas"`

	// Imagen es la portada, para pintar la maqueta sin recorrer el payload:
	// cada canal la coloca en un sitio distinto del JSON.
	Imagen string `json:"imagen,omitempty"`
}

// Publicable indica si el canal aceptaría la publicación.
func (p Proyeccion) Publicable() bool {
	for _, f := range p.Faltantes {
		if f.Severidad == Bloquea {
			return false
		}
	}
	return true
}

// Proyectar construye la proyección de una entrada para un canal.
func Proyectar(c Canal, e Entrada) Proyeccion {
	e = normalizar(e)

	var p Proyeccion
	switch c {
	case WooCommerce:
		p = proyectarWoo(e)
	case Shopify:
		p = proyectarShopify(e)
	case MercadoLibre:
		p = proyectarML(e)
	case Falabella:
		p = proyectarFalabella(e)
	default:
		return Proyeccion{Canal: c, Faltantes: []Faltante{
			{Campo: "canal", Motivo: "canal desconocido", Severidad: Bloquea},
		}}
	}

	if len(e.Imagenes) > 0 {
		p.Imagen = e.Imagenes[0]
	}
	return p
}

// ProyectarTodos devuelve la proyección de los cuatro canales.
func ProyectarTodos(e Entrada) []Proyeccion {
	var out []Proyeccion
	for _, c := range Todos() {
		out = append(out, Proyectar(c, e))
	}
	return out
}

// ------------------------------------------------------------- utilidades

// comunes acumula los requisitos que exigen los cuatro canales por igual.
func comunes(e Entrada) []Faltante {
	var f []Faltante
	if e.SKU == "" {
		f = append(f, Faltante{"sku", "los cuatro canales exigen una referencia única", Bloquea})
	}
	if e.Precio <= 0 {
		f = append(f, Faltante{"precio", "sin precio calculado no se puede publicar", Bloquea})
	}
	if e.Descripcion == "" {
		f = append(f, Faltante{"descripcion", "sin descripción de venta", Bloquea})
	}
	if len(e.Imagenes) == 0 {
		f = append(f, Faltante{"imagenes", "sin imágenes la publicación no convierte", Advierte})
	}
	if e.Stock <= 0 {
		f = append(f, Faltante{"stock", "sin existencias se publicaría agotado", Advierte})
	}
	return f
}

func titulo(e Entrada, c Canal) string {
	if t, ok := e.Titulos[string(c)]; ok && t != "" {
		return t
	}
	// Si no hay título específico, se usa cualquiera como último recurso; el
	// canal lo rechazará por longitud si no cabe, y eso se ve en Faltantes.
	for _, t := range e.Titulos {
		if t != "" {
			return t
		}
	}
	return ""
}

func moneda(e Entrada) string {
	if e.Moneda != "" {
		return e.Moneda
	}
	return "COP"
}

// decimalesDeMoneda devuelve cuántos decimales admite cada moneda.
//
// El peso colombiano no se fracciona: publicar 102.111,5953 sería incorrecto
// en las cuatro tiendas, y el cálculo de tarifa (coste × 1,25) produce
// decimales constantemente.
func decimalesDeMoneda(m string) int {
	switch m {
	case "COP", "CLP", "PYG", "JPY":
		return 0
	default:
		return 2
	}
}

// redondear ajusta un importe a los decimales de su moneda.
func redondear(v float64, m string) float64 {
	d := decimalesDeMoneda(m)
	if d == 0 {
		return math.Round(v)
	}
	f := math.Pow(10, float64(d))
	return math.Round(v*f) / f
}

// normalizar deja la entrada con los importes ya redondeados, de modo que
// todos los proyectores trabajen sobre el mismo valor y la vista previa
// coincida exactamente con lo que se enviaría.
func normalizar(e Entrada) Entrada {
	m := moneda(e)
	e.Moneda = m
	e.Precio = redondear(e.Precio, m)
	e.PrecioOferta = redondear(e.PrecioOferta, m)
	return e
}

// formatoImporte serializa un importe con los decimales de su moneda, para
// las APIs que esperan el precio como cadena.
func formatoImporte(v float64, m string) string {
	return strconv.FormatFloat(v, 'f', decimalesDeMoneda(m), 64)
}

// hayOferta indica si el precio de oferta está vigente y es menor que el base.
func hayOferta(e Entrada) bool {
	return e.PrecioOferta > 0 && e.PrecioOferta < e.Precio
}

// specsOrdenadas devuelve las especificaciones en orden estable, para que dos
// proyecciones seguidas del mismo producto produzcan el mismo payload y el
// hash de contenido no cambie sin motivo.
func specsOrdenadas(e Entrada) []Spec {
	out := append([]Spec(nil), e.Specs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Clave < out[j].Clave })
	return out
}

func fecha(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02T15:04:05")
}
