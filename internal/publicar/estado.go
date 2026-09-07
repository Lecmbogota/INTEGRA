package publicar

import (
	"context"
	"sort"
	"strings"

	"github.com/mdv/integra/internal/store"
)

// Estado de publicación producto a producto.
//
// La pantalla de Publicación contaba cuántas fichas hay vivas en cada canal, y
// la de Productos listaba el catálogo de Odoo: entre las dos no había forma de
// responder a la pregunta que se hace todos los días quien vende, que es cuál
// de estos tres es cada producto —uno que nunca se ha publicado, uno publicado
// y al día, o uno publicado al que le falta enviar un cambio— ni por qué uno
// concreto no puede ir a un canal.

// Situación de una variante en una cuenta.
const (
	// SitNuevo: cumple los requisitos y nunca se publicó ahí.
	SitNuevo = "nuevo"
	// SitAlDia: publicado y sin nada pendiente de enviar.
	SitAlDia = "al_dia"
	// SitPendiente: publicado, pero la ficha, el precio o el stock cambiaron
	// desde el último envío.
	SitPendiente = "pendiente"
	// SitPausado: retirado de la venta, por Integra o por una persona.
	SitPausado = "pausado"
	// SitRetirado: lo retiró el propio canal. No se reabre solo.
	SitRetirado = "retirado"
	// SitNoPublicable: le falta algo para poder salir a ese canal.
	SitNoPublicable = "no_publicable"
)

// EstadoEnCanal es lo que le pasa a una variante en una cuenta concreta.
type EstadoEnCanal struct {
	CuentaID  int64  `json:"cuenta_id"`
	Canal     string `json:"canal"`
	Situacion string `json:"situacion"`
	// Falta enumera lo que impide publicar, en lenguaje de quien lo va a
	// arreglar. Vacío salvo en no_publicable.
	Falta []string `json:"falta"`
	// Cambios dice qué hay pendiente de enviar: ficha, precio, stock.
	Cambios    []string `json:"cambios"`
	ExternalID string   `json:"external_id"`
}

// EstadoDeProducto agrupa por variante lo que pasa en cada canal.
type EstadoDeProducto struct {
	VarianteID int64           `json:"variante_id"`
	SKU        string          `json:"sku"`
	Titulo     string          `json:"titulo"`
	Canales    []EstadoEnCanal `json:"canales"`
}

// requisitosDeCanal describe lo que cada canal exige además del mínimo común.
//
// Vive aquí y no en cada adaptador porque el adaptador solo puede rechazarlo
// cuando ya se está enviando: para entonces el trabajo ya ocupó su sitio en la
// cola y el operador ve un error en rojo en vez de un producto que nunca
// estuvo listo. Los textos son los mismos que devuelven los adaptadores, para
// que el aviso previo y el fallo real digan lo mismo.
var requisitosDeCanal = map[string][]struct {
	falta func(store.CandidatoPublicacion) bool
	texto string
}{
	"mercadolibre": {
		{func(c store.CandidatoPublicacion) bool { return c.CategoriaCanal == "" },
			"sin categoría mapeada de MercadoLibre"},
	},
	"falabella": {
		{func(c store.CandidatoPublicacion) bool { return c.Barcode == "" }, "sin EAN"},
		{func(c store.CandidatoPublicacion) bool { return c.Peso <= 0 }, "sin peso"},
		{func(c store.CandidatoPublicacion) bool {
			return c.LargoCm <= 0 || c.AnchoCm <= 0 || c.AltoCm <= 0
		}, "sin las medidas del paquete"},
		{func(c store.CandidatoPublicacion) bool { return c.CategoriaCanal == "" },
			"sin categoría mapeada de Falabella"},
	},
}

// faltaComun es el mínimo que exige cualquier canal, dicho en la misma forma.
func faltaComun(c store.CandidatoPublicacion) []string {
	var f []string
	if c.SKU == "" {
		f = append(f, "sin SKU")
	}
	if strings.TrimSpace(c.Titulo) == "" {
		f = append(f, "sin título")
	}
	if strings.TrimSpace(c.Descripcion) == "" {
		f = append(f, "sin descripción")
	}
	if c.PrecioCanal <= 0 && c.PrecioBase <= 0 {
		f = append(f, "sin precio")
	}
	if len(c.Imagenes) == 0 {
		f = append(f, "sin fotos")
	}
	return f
}

// situacionDe resuelve en qué estado está una variante en una cuenta.
func situacionDe(c store.CandidatoPublicacion, canal string) EstadoEnCanal {
	e := EstadoEnCanal{Canal: canal, ExternalID: c.ExternalID}

	falta := faltaComun(c)
	for _, r := range requisitosDeCanal[canal] {
		if r.falta(c) {
			falta = append(falta, r.texto)
		}
	}
	if c.BloqueadoPorCosto {
		falta = append(falta, "su precio no cubre el coste")
	}

	// Lo que ya está publicado se informa por su estado aunque le falte algo:
	// decir «no publicable» de una ficha que lleva meses vendiendo confunde
	// más de lo que ayuda. Lo que le falta se sigue enumerando.
	if c.ExternalID == "" {
		if len(falta) > 0 {
			e.Situacion, e.Falta = SitNoPublicable, falta
			return e
		}
		e.Situacion = SitNuevo
		return e
	}
	e.Falta = falta

	switch {
	case c.EstadoPublicacion == "paused" && c.PausaMotivo == store.PausaCanal:
		e.Situacion = SitRetirado
		return e
	case c.EstadoPublicacion == "paused":
		e.Situacion = SitPausado
		return e
	}

	if c.ContentHash != HashContenido(c) {
		e.Cambios = append(e.Cambios, "ficha")
	}
	if c.PriceHash != HashPrecio(c) {
		e.Cambios = append(e.Cambios, "precio")
	}
	if c.StockHash != HashStock(c) {
		e.Cambios = append(e.Cambios, "stock")
	}
	if len(e.Cambios) > 0 {
		e.Situacion = SitPendiente
	} else {
		e.Situacion = SitAlDia
	}
	return e
}

// CuentaDeCanal es lo mínimo que hace falta saber de una cuenta.
type CuentaDeCanal struct {
	ID    int64
	Canal string
}

// EstadoPublicaciones devuelve, por variante, qué le pasa en cada cuenta.
//
// Se recorren las cuentas y no los productos porque CandidatosPublicacion ya
// resuelve por cuenta el precio efectivo, la categoría mapeada y los hashes
// publicados: pedirlo por producto obligaría a repetir esa consulta una vez
// por fila.
func EstadoPublicaciones(ctx context.Context, st catalogo, cuentas []CuentaDeCanal) ([]EstadoDeProducto, error) {
	porVariante := map[int64]*EstadoDeProducto{}

	for _, cu := range cuentas {
		candidatos, err := st.CandidatosPublicacion(ctx, cu.ID)
		if err != nil {
			// Una cuenta sin bodegas asignadas no publica nada y devuelve
			// error: eso no puede dejar sin respuesta a las demás.
			continue
		}
		for _, c := range candidatos {
			p, ok := porVariante[c.VarianteID]
			if !ok {
				p = &EstadoDeProducto{VarianteID: c.VarianteID, SKU: c.SKU, Titulo: c.Titulo}
				porVariante[c.VarianteID] = p
			}
			e := situacionDe(c, cu.Canal)
			e.CuentaID = cu.ID
			p.Canales = append(p.Canales, e)
		}
	}

	out := make([]EstadoDeProducto, 0, len(porVariante))
	for _, p := range porVariante {
		sort.Slice(p.Canales, func(i, j int) bool { return p.Canales[i].Canal < p.Canales[j].Canal })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKU < out[j].SKU })
	return out, nil
}
