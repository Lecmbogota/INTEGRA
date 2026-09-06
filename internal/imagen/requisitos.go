package imagen

import "fmt"

// Requisitos son las exigencias de imagen de un canal.
//
// PENDIENTE DE VERIFICAR contra la documentación vigente de cada canal antes
// de implementar su adaptador. Los valores de abajo son conservadores a
// propósito: es preferible marcar como dudosa una imagen que pasaría, a
// publicar una que el canal rechace y dejar la publicación a medias.
type Requisitos struct {
	Canal       string
	LadoMinimo  int
	LadoOptimo  int
	MaxBytes    int
	MaxImagenes int
	// ExigeCuadrada indica que la ficha principal debe ser cuadrada.
	ExigeCuadrada bool
	Formatos      []string
}

// PorCanal recoge los requisitos conocidos de cada canal.
var PorCanal = map[string]Requisitos{
	"mercadolibre": {
		Canal: "mercadolibre",
		// MercadoLibre penaliza en calidad de publicación por debajo de 500 px
		// y recomienda 1200 para el zoom.
		LadoMinimo: 500, LadoOptimo: 1200,
		MaxBytes: 10 << 20, MaxImagenes: 10,
		ExigeCuadrada: true,
		Formatos:      []string{FormatoJPEG, FormatoPNG},
	},
	"falabella": {
		Canal:      "falabella",
		LadoMinimo: 600, LadoOptimo: 1200,
		MaxBytes: 5 << 20, MaxImagenes: 8,
		ExigeCuadrada: true,
		Formatos:      []string{FormatoJPEG, FormatoPNG},
	},
	"shopify": {
		Canal:      "shopify",
		LadoMinimo: 400, LadoOptimo: 2048,
		MaxBytes: 20 << 20, MaxImagenes: 250,
		Formatos: []string{FormatoJPEG, FormatoPNG, FormatoWebP},
	},
	"woocommerce": {
		Canal:      "woocommerce",
		LadoMinimo: 300, LadoOptimo: 1000,
		MaxBytes: 8 << 20, MaxImagenes: 20,
		Formatos: []string{FormatoJPEG, FormatoPNG, FormatoWebP},
	},
}

// Problema es un incumplimiento detectado.
type Problema struct {
	Canal   string `json:"canal"`
	Motivo  string `json:"motivo"`
	Bloquea bool   `json:"bloquea"`
}

// Validar comprueba una imagen contra los requisitos de un canal.
//
// Se evalúa la imagen ORIGINAL, no la derivada: como el redimensionado nunca
// amplía, una fuente de 150×150 sigue siendo de 150×150 por mucho que la
// variante se llame "cuadrada_1200". Validar la derivada daría un falso
// aprobado, que es exactamente el error que hay que evitar.
func Validar(inf Info, canal string) []Problema {
	req, ok := PorCanal[canal]
	if !ok {
		return []Problema{{Canal: canal, Motivo: "canal desconocido", Bloquea: true}}
	}

	var out []Problema
	menor := inf.Ancho
	if inf.Alto < menor {
		menor = inf.Alto
	}

	if menor < req.LadoMinimo {
		out = append(out, Problema{
			Canal: canal,
			Motivo: fmt.Sprintf("%d×%d: el lado menor no llega a los %d px que exige el canal",
				inf.Ancho, inf.Alto, req.LadoMinimo),
			Bloquea: true,
		})
	} else if menor < req.LadoOptimo {
		out = append(out, Problema{
			Canal: canal,
			Motivo: fmt.Sprintf("%d×%d: por debajo de los %d px recomendados, se pierde el zoom",
				inf.Ancho, inf.Alto, req.LadoOptimo),
			Bloquea: false,
		})
	}

	if inf.Bytes > req.MaxBytes {
		out = append(out, Problema{
			Canal:   canal,
			Motivo:  fmt.Sprintf("%d bytes supera el máximo de %d", inf.Bytes, req.MaxBytes),
			Bloquea: true,
		})
	}

	admitido := false
	for _, f := range req.Formatos {
		if f == inf.Formato {
			admitido = true
			break
		}
	}
	if !admitido {
		out = append(out, Problema{
			Canal:   canal,
			Motivo:  fmt.Sprintf("el formato %s no lo acepta este canal", inf.Formato),
			Bloquea: true,
		})
	}

	return out
}

// ValidarTodos evalúa una imagen contra los cuatro canales.
func ValidarTodos(inf Info) map[string][]Problema {
	out := map[string][]Problema{}
	for canal := range PorCanal {
		if p := Validar(inf, canal); len(p) > 0 {
			out[canal] = p
		}
	}
	return out
}

// Publicable indica en qué canales sirve una imagen.
func Publicable(inf Info) map[string]bool {
	out := map[string]bool{}
	for canal := range PorCanal {
		ok := true
		for _, p := range Validar(inf, canal) {
			if p.Bloquea {
				ok = false
				break
			}
		}
		out[canal] = ok
	}
	return out
}
