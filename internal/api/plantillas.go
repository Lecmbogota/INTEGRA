package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mdv/integra/internal/plantilla"
	"github.com/mdv/integra/internal/store"
)

// Tamaño máximo del archivo subido. Una plantilla del catálogo entero pesa
// unos cientos de kilobytes; 25 MB deja margen de sobra y corta en seco un
// archivo que no es lo que dice ser.
const maxPlantilla = 25 << 20

// descargarPlantilla entrega el catálogo como hoja de cálculo editable.
func (s *Server) descargarPlantilla(w http.ResponseWriter, r *http.Request) {
	// Se aceptan los mismos nombres de parámetro que /api/productos para que la
	// pantalla pueda reenviar su filtro tal cual.
	q := r.URL.Query()
	filtro := store.FiltroProductos{
		Busqueda:      q.Get("q"),
		Marca:         q.Get("marca"),
		Categoria:     q.Get("categoria"),
		SoloProblemas: verdadero(q.Get("problemas")),
		VerExcluidos:  verdadero(q.Get("excluidos")),
		SoloSinPrecio: verdadero(q.Get("sin_precio")),
	}

	filas, err := s.st.FilasParaPlantilla(r.Context(), filtro)
	if err != nil {
		s.fallo(w, err)
		return
	}

	conv := make([]plantilla.Fila, 0, len(filas))
	for _, f := range filas {
		conv = append(conv, plantilla.Fila{
			SKU: f.SKU, Nombre: f.Nombre, Marca: f.Marca, Stock: f.Stock,
			Precio: f.Precio, PromoCanal: f.PromoCanal, PromoPrecio: f.PromoPrecio,
			PromoInicia: f.PromoInicia, PromoTermina: f.PromoTermina,
		})
	}

	nombre := fmt.Sprintf("integra-precios-%s.xlsx", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nombre+`"`)
	// Sin esto el navegador no deja leer el nombre del archivo desde JavaScript
	// y la descarga sale con un nombre inventado.
	w.Header().Set("Access-Control-Expose-Headers", "Content-Disposition")

	if err := plantilla.Generar(w, conv); err != nil {
		// La cabecera ya salió, así que no se puede devolver un error HTTP:
		// lo único honesto que queda es dejar constancia en el registro.
		s.log.Error("generando la plantilla", "error", err)
	}
}

// CambioPlantilla es una línea del resumen que se le enseña al operador antes
// de aplicar nada.
type CambioPlantilla struct {
	Fila   int    `json:"fila"`
	SKU    string `json:"sku"`
	Nombre string `json:"nombre"`

	PrecioAntes   *float64 `json:"precio_antes"`
	PrecioDespues *float64 `json:"precio_despues"`

	PromoCanal   string     `json:"promo_canal"`
	PromoPrecio  *float64   `json:"promo_precio"`
	PromoInicia  *time.Time `json:"promo_inicia"`
	PromoTermina *time.Time `json:"promo_termina"`
}

type RespuestaPlantilla struct {
	Aplicado    bool                 `json:"aplicado"`
	Cambios     []CambioPlantilla    `json:"cambios"`
	Problemas   []plantilla.Problema `json:"problemas"`
	FilasLeidas int                  `json:"filas_leidas"`
	Precios     int                  `json:"precios"`
	Promociones int                  `json:"promociones"`
}

// cargarPlantilla lee el archivo subido y, según el parámetro `aplicar`, o
// devuelve un simulacro o escribe los cambios.
//
// El simulacro no es un lujo: una plantilla mal hecha puede cambiar el precio
// de cuatrocientos productos, y deshacer eso a mano no es viable. Por eso el
// camino por defecto —sin `aplicar=true`— no toca nada.
func (s *Server) cargarPlantilla(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxPlantilla); err != nil {
		http.Error(w, "no se pudo leer el archivo subido: "+err.Error(), http.StatusBadRequest)
		return
	}
	archivo, cab, err := r.FormFile("archivo")
	if err != nil {
		http.Error(w, "falta el archivo (campo «archivo»)", http.StatusBadRequest)
		return
	}
	defer archivo.Close()

	if cab.Size > maxPlantilla {
		http.Error(w, "el archivo es demasiado grande", http.StatusRequestEntityTooLarge)
		return
	}

	filas, problemas, err := plantilla.Leer(archivo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	aplicar := r.URL.Query().Get("aplicar") == "true"
	res, err := s.resolverPlantilla(r, filas, problemas, aplicar)
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, res)
}

func (s *Server) resolverPlantilla(r *http.Request, filas []plantilla.FilaLeida,
	problemas []plantilla.Problema, aplicar bool) (*RespuestaPlantilla, error) {
	ctx := r.Context()

	skus := make([]string, 0, len(filas))
	for _, f := range filas {
		skus = append(skus, f.SKU)
	}
	variantes, err := s.st.ResolverSKUs(ctx, skus)
	if err != nil {
		return nil, err
	}
	cuentas, err := s.st.CuentasPorCanal(ctx)
	if err != nil {
		return nil, err
	}

	// Las listas se inicializan vacías, no nil: un nil en Go sale como `null`
	// en JSON, y el cliente que hace `.length` sobre eso revienta en vez de
	// enseñar «sin errores».
	if problemas == nil {
		problemas = []plantilla.Problema{}
	}
	res := &RespuestaPlantilla{
		Aplicado:    aplicar,
		Cambios:     []CambioPlantilla{},
		Problemas:   problemas,
		FilasLeidas: len(filas) + len(problemas),
	}

	// Primera vuelta: resolver y validar TODAS las filas antes de escribir
	// ninguna. Aplicar a medias dejaría el catálogo en un estado que el
	// operador no pidió y no puede reproducir.
	type pendiente struct {
		cambio   CambioPlantilla
		id       int64
		cuentaID int64
	}
	var listos []pendiente

	for _, f := range filas {
		v, ok := variantes[store.ClaveSKU(f.SKU)]
		if !ok {
			res.Problemas = append(res.Problemas, plantilla.Problema{
				Fila: f.Numero, SKU: f.SKU,
				Mensaje: "no existe ningún producto activo con ese SKU"})
			continue
		}

		c := CambioPlantilla{
			Fila: f.Numero, SKU: f.SKU, Nombre: v.Nombre,
			PrecioAntes:   v.Precio,
			PrecioDespues: f.Precio,
		}

		// El precio contra el que se compara la promoción es el que va a
		// quedar tras esta misma carga, no el que había: si la fila sube el
		// precio y rebaja a la vez, lo que importa es el resultado final.
		precioFinal := v.Precio
		if f.Precio != nil {
			precioFinal = f.Precio
		}

		p := pendiente{cambio: c, id: v.ID}
		malo := false

		if f.PromoCanal != "" {
			cuentaID, ok := cuentas[f.PromoCanal]
			if !ok {
				res.Problemas = append(res.Problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: fmt.Sprintf("no hay ninguna cuenta conectada del canal %q (conectados: %s)",
						f.PromoCanal, listaCanales(cuentas))})
				malo = true
			} else {
				p.cuentaID = cuentaID
			}
			if f.PromoPrecio != nil && precioFinal != nil && *f.PromoPrecio >= *precioFinal {
				res.Problemas = append(res.Problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: fmt.Sprintf("el precio de promoción (%.0f) no es menor que el de venta (%.0f)",
						*f.PromoPrecio, *precioFinal)})
				malo = true
			}
			if f.PromoPrecio != nil && precioFinal == nil {
				res.Problemas = append(res.Problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: "no se puede rebajar un producto que todavía no tiene precio de venta"})
				malo = true
			}
			p.cambio.PromoCanal = f.PromoCanal
			p.cambio.PromoPrecio = f.PromoPrecio
			p.cambio.PromoInicia = f.PromoInicia
			p.cambio.PromoTermina = f.PromoTermina
		}

		if !malo {
			listos = append(listos, p)
		}
	}

	// Con errores por resolver no se escribe nada, ni siquiera las filas
	// buenas: el operador corrige el archivo y lo vuelve a subir entero.
	if aplicar && len(res.Problemas) > 0 {
		res.Aplicado = false
		aplicar = false
	}

	// Quién cargó la plantilla queda en cada promoción creada: con cambios de
	// cientos de filas, saber de quién salió es la mitad de la investigación.
	var usuarioID *int64
	if c := s.extraerClaims(r); c != nil {
		usuarioID = &c.UserID
	}
	for _, p := range listos {
		if p.cambio.PrecioDespues != nil {
			res.Precios++
			if aplicar {
				if err := s.st.ActualizarPrecio(ctx, p.id, p.cambio.PrecioDespues); err != nil {
					return nil, fmt.Errorf("fila %d (%s): %w", p.cambio.Fila, p.cambio.SKU, err)
				}
			}
		}
		if p.cambio.PromoPrecio != nil {
			res.Promociones++
			if aplicar {
				// Sin fecha de inicio, la promoción arranca ya: es lo que
				// espera quien rellena solo el precio y le da a cargar.
				inicia := time.Now()
				if p.cambio.PromoInicia != nil {
					inicia = *p.cambio.PromoInicia
				}
				if _, err := s.st.GuardarOferta(ctx, p.id, p.cuentaID,
					*p.cambio.PromoPrecio, inicia, p.cambio.PromoTermina, usuarioID); err != nil {
					return nil, fmt.Errorf("fila %d (%s): %w", p.cambio.Fila, p.cambio.SKU, err)
				}
			}
		}
		res.Cambios = append(res.Cambios, p.cambio)
	}

	if aplicar && (res.Precios > 0 || res.Promociones > 0) {
		if err := s.st.RecalcularAtencion(ctx); err != nil {
			s.log.Error("recalculando atención tras la plantilla", "error", err)
		}
	}

	sort.Slice(res.Problemas, func(i, j int) bool {
		return res.Problemas[i].Fila < res.Problemas[j].Fila
	})
	return res, nil
}

// verdadero acepta las dos convenciones que ya conviven en la API: "1" en
// /api/productos y "true" en las rutas nuevas. Elegir una sola rompería a
// alguien, y aceptar ambas no cuesta nada.
func verdadero(v string) bool { return v == "1" || v == "true" }

func listaCanales(cuentas map[string]int64) string {
	if len(cuentas) == 0 {
		return "ninguno"
	}
	nombres := make([]string, 0, len(cuentas))
	for c := range cuentas {
		nombres = append(nombres, c)
	}
	sort.Strings(nombres)
	return strings.Join(nombres, ", ")
}
