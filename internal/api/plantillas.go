package api

import (
	"errors"
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

// Lo que se admite tener en memoria mientras se parsea el formulario. El resto
// del cuerpo va a un temporal que el servidor borra al terminar la petición,
// así que subirlo a 25 MB solo servía para que unas pocas subidas simultáneas
// se comieran la memoria del proceso.
const memoriaPlantilla = 8 << 20

// Tope real del cuerpo de la subida.
//
// ParseMultipartForm no limita el tamaño: su argumento es cuánto guarda en
// memoria, y todo lo que se pasa de ahí lo escribe en disco. Sin este tope,
// una subida de gigabytes llena el disco del servidor antes de que nadie
// llegue a mirar cab.Size. El margen sobre maxPlantilla cubre el sobre
// multipart (delimitadores y cabeceras de parte), que no es el archivo.
//
// Es variable y no constante para poder bajarlo en las pruebas: comprobar el
// corte con un cuerpo de 25 MB de verdad no aporta nada y tarda.
var limiteCuerpoPlantilla int64 = maxPlantilla + (1 << 20)

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
	r.Body = http.MaxBytesReader(w, r.Body, limiteCuerpoPlantilla)
	if err := r.ParseMultipartForm(memoriaPlantilla); err != nil {
		// El cuerpo se corta mientras se lee, así que este es el único punto
		// donde se puede distinguir «subida gigante» de «formulario mal
		// formado»: cab.Size llega demasiado tarde y solo mira una parte.
		var pasado *http.MaxBytesError
		if errors.As(err, &pasado) {
			http.Error(w, "el archivo es demasiado grande", http.StatusRequestEntityTooLarge)
			return
		}
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
	listos, malas := planificarPlantilla(filas, variantes, cuentas, time.Now())
	res.Problemas = append(res.Problemas, malas...)

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
				// La fecha de inicio es la que ya se resolvió al validar, no
				// una recién calculada: si se volviera a calcular aquí, la
				// vigencia que se comprobó no sería la que acaba en la base.
				if _, err := s.st.GuardarOferta(ctx, p.id, p.cuentaID,
					*p.cambio.PromoPrecio, p.inicia, p.cambio.PromoTermina, usuarioID); err != nil {
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

// planFila es una fila ya resuelta contra el catálogo y validada entera.
type planFila struct {
	cambio   CambioPlantilla
	id       int64
	cuentaID int64
	// inicia es la vigencia con la que se va a guardar la promoción, decidida
	// aquí para que la validación y la escritura miren la misma fecha.
	inicia time.Time
}

// planificarPlantilla resuelve las filas contra el catálogo y descarta las que
// no se pueden aplicar.
//
// Valida de más a propósito: repite aquí las reglas que ActualizarPrecio y
// GuardarOferta comprueban en la base. La carga no es una transacción —cada
// escritura se confirma por su cuenta—, así que una fila que solo revienta al
// escribirse deja aplicadas todas las anteriores mientras la API responde un
// error, y el operador cree que no se aplicó nada. Ese era el caso de la
// plantilla reutilizada del mes pasado: «termina» con fecha ya pasada y
// «empieza» vacío pasaba el simulacro y fallaba a mitad de la escritura.
func planificarPlantilla(filas []plantilla.FilaLeida, variantes map[string]store.VarianteResuelta,
	cuentas map[string]int64, ahora time.Time) (listos []planFila, problemas []plantilla.Problema) {

	for _, f := range filas {
		v, ok := variantes[store.ClaveSKU(f.SKU)]
		if !ok {
			problemas = append(problemas, plantilla.Problema{
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

		p := planFila{cambio: c, id: v.ID}
		malo := false

		if f.Precio != nil && *f.Precio < 0 {
			problemas = append(problemas, plantilla.Problema{
				Fila: f.Numero, SKU: f.SKU,
				Mensaje: "el precio no puede ser negativo"})
			malo = true
		}

		if f.PromoCanal != "" {
			cuentaID, ok := cuentas[f.PromoCanal]
			if !ok {
				problemas = append(problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: fmt.Sprintf("no hay ninguna cuenta conectada del canal %q (conectados: %s)",
						f.PromoCanal, listaCanales(cuentas))})
				malo = true
			} else {
				p.cuentaID = cuentaID
			}
			if f.PromoPrecio != nil && precioFinal != nil && *f.PromoPrecio >= *precioFinal {
				problemas = append(problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: fmt.Sprintf("el precio de promoción (%.0f) no es menor que el de venta (%.0f)",
						*f.PromoPrecio, *precioFinal)})
				malo = true
			}
			if f.PromoPrecio != nil && precioFinal == nil {
				problemas = append(problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: "no se puede rebajar un producto que todavía no tiene precio de venta"})
				malo = true
			}
			if f.PromoPrecio != nil && *f.PromoPrecio <= 0 {
				problemas = append(problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU,
					Mensaje: "el precio de promoción tiene que ser mayor que cero"})
				malo = true
			}

			// Sin fecha de inicio, la promoción arranca ya: es lo que espera
			// quien rellena solo el precio y le da a cargar. Con esa fecha ya
			// fijada se puede comprobar la vigencia completa, incluido el caso
			// que la lectura del archivo no ve —«termina» sin «empieza»—,
			// porque allí solo se comparan las dos fechas cuando están ambas.
			p.inicia = ahora
			if f.PromoInicia != nil {
				p.inicia = *f.PromoInicia
			}
			if f.PromoTermina != nil && f.PromoTermina.Before(p.inicia) {
				mensaje := "la promoción termina antes de empezar"
				if f.PromoInicia == nil {
					mensaje = "«promoción termina» ya pasó y no hay «promoción empieza»: " +
						"la promoción empezaría hoy y terminaría antes"
				}
				problemas = append(problemas, plantilla.Problema{
					Fila: f.Numero, SKU: f.SKU, Mensaje: mensaje})
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
	return listos, problemas
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
