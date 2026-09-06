// Package api expone el estado de Integra por HTTP.
//
// Se usa el enrutador de la biblioteca estándar: desde Go 1.22 ServeMux
// entiende patrones con método ("GET /api/productos"), que es todo lo que
// hace falta aquí. Una dependencia menos que auditar y actualizar.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdv/integra/internal/atributos"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/imagen"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/mercadolibre"
	"github.com/mdv/integra/internal/ordenes"
	"github.com/mdv/integra/internal/proyeccion"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
)

type Server struct {
	st   *store.Store
	log  *slog.Logger
	http *http.Server
	// sincronizar dispara una sincronización manual. Se inyecta para que este
	// paquete no dependa del motor de sincronización.
	sincronizar func(context.Context) (string, error)
	// almacen es el banco de imágenes. Nulo si no se configuró directorio.
	almacen *imagen.Almacen
	// cif cifra las credenciales de canal antes de que toquen la base.
	cif *crypto.Cifrador
	// cola encola los envíos a los canales; los ejecuta el worker.
	cola *jobs.Cola
	// sesiones recuerda por unos segundos si el usuario de cada token sigue
	// existiendo y activo, para no consultar la base en cada petición.
	sesiones *cacheSesiones

	// masivo es el estado del barrido de imágenes en curso, en memoria.
	masivoMu sync.Mutex
	masivo   BusquedaMasiva
}

func Nuevo(st *store.Store, log *slog.Logger, addr string, alm *imagen.Almacen, cif *crypto.Cifrador, cola *jobs.Cola, sincronizar func(context.Context) (string, error)) *Server {
	s := &Server{st: st, log: log, sincronizar: sincronizar, almacen: alm, cif: cif, cola: cola,
		sesiones: nuevaCacheSesiones(ttlSesion)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.salud)
	mux.HandleFunc("GET /api/resumen", s.resumen)
	mux.HandleFunc("GET /api/productos", s.productos)
	mux.HandleFunc("GET /api/marcas", s.marcas)
	mux.HandleFunc("GET /api/categorias", s.categorias)
	mux.HandleFunc("GET /api/atencion", s.atencion)
	mux.HandleFunc("GET /api/stock", s.stock)
	mux.HandleFunc("POST /api/sincronizar", s.lanzarSync)
	mux.HandleFunc("PATCH /api/productos/{id}", s.editarProducto)
	mux.HandleFunc("POST /api/productos/masivo", s.editarMasivo)
	mux.HandleFunc("GET /api/horarios", s.horarios)
	mux.HandleFunc("PUT /api/horarios", s.guardarHorario)
	mux.HandleFunc("DELETE /api/horarios/{id}", s.borrarHorario)
	mux.HandleFunc("GET /api/alertas", s.alertas)
	mux.HandleFunc("POST /api/alertas/{id}/vista", s.reconocerAlerta)
	mux.HandleFunc("GET /api/atributos/resumen", s.resumenAtributos)
	mux.HandleFunc("GET /api/productos/{id}/atributos", s.atributosProducto)
	mux.HandleFunc("PUT /api/productos/{id}/atributos/{attr}", s.guardarAtributo)
	mux.HandleFunc("GET /api/ordenes", s.ordenes)
	mux.HandleFunc("GET /api/ordenes/resumen", s.resumenOrdenes)
	mux.HandleFunc("POST /api/cuentas/{id}/ingerir-ordenes", s.ingerirOrdenes)
	mux.HandleFunc("GET /api/publicaciones", s.publicaciones)
	mux.HandleFunc("POST /api/cuentas/{id}/planificar", s.planificar)
	mux.HandleFunc("GET /api/cuentas", s.cuentas)
	mux.HandleFunc("PUT /api/cuentas/{canal}", s.guardarCuenta)
	mux.HandleFunc("POST /api/cuentas/{id}/probar", s.probarCuenta)
	mux.HandleFunc("GET /api/canales", s.canales)
	mux.HandleFunc("PATCH /api/canales/{codigo}", s.editarCanal)
	mux.HandleFunc("GET /api/prioridad", s.prioridad)
	mux.HandleFunc("GET /api/productos/{id}/competencia", s.competencia)
	mux.HandleFunc("GET /api/preview/{id}", s.preview)
	mux.HandleFunc("GET /api/mapeos", s.mapeos)
	mux.HandleFunc("POST /api/mapeos/{id}/confirmar", s.confirmarMapeo)
	s.registrarImagenes(mux)
	s.registrarBusquedaMasiva(mux)
	s.registrarPrecios(mux)
	s.registrarAuth(mux)
	s.registrarAuditoria(mux)
	s.registrarObservabilidad(mux)
	s.registrarIntegraciones(mux)

	s.http = &http.Server{
		Addr: addr,
		// El orden importa: primero el registro, luego CORS (para que el
		// preflight se responda aunque no haya sesión) y por último la
		// exigencia de sesión, que es lo más cercano a los manejadores.
		Handler: registrar(log, cors(s.exigirSesion(mux))),
		// Sin estos plazos, una conexión lenta puede retener un descriptor
		// indefinidamente. El de escritura es holgado porque el listado del
		// catálogo completo tarda.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s
}

// Escuchar arranca el servidor y lo apaga con orden al cancelarse el contexto.
func (s *Server) Escuchar(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		s.log.Info("servidor escuchando", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		s.log.Info("apagando el servidor")
		// Margen para que terminen las peticiones en curso en vez de cortarlas.
		apagado, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return s.http.Shutdown(apagado)
	}
}

// ------------------------------------------------------------ manejadores

func (s *Server) salud(w http.ResponseWriter, r *http.Request) {
	escribir(w, http.StatusOK, map[string]string{"estado": "ok"})
}

func (s *Server) resumen(w http.ResponseWriter, r *http.Request) {
	res, err := s.st.Resumen(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, res)
}

func (s *Server) productos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.FiltroProductos{
		Busqueda:      q.Get("q"),
		Marca:         q.Get("marca"),
		Categoria:     q.Get("categoria"),
		SoloProblemas: q.Get("problemas") == "1",
		VerExcluidos:  q.Get("excluidos") == "1",
		SoloSinPrecio: q.Get("sin_precio") == "1",
		Limite:        entero(q.Get("limite"), 100),
		Offset:        entero(q.Get("offset"), 0),
	}
	filas, total, err := s.st.ListarProductos(r.Context(), f)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if filas == nil {
		filas = []store.FilaProducto{}
	}
	escribir(w, http.StatusOK, map[string]any{
		"total": total, "limite": f.Limite, "offset": f.Offset, "items": filas,
	})
}

func (s *Server) marcas(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.Marcas(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if m == nil {
		m = []store.ConteoMarca{}
	}
	escribir(w, http.StatusOK, m)
}

func (s *Server) categorias(w http.ResponseWriter, r *http.Request) {
	cats, err := s.st.Categorias(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if cats == nil {
		cats = []store.ConteoCategoria{}
	}
	escribir(w, http.StatusOK, cats)
}

func (s *Server) atencion(w http.ResponseWriter, r *http.Request) {
	a, err := s.st.Atencion(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if a == nil {
		a = []store.ConteoAtencion{}
	}
	escribir(w, http.StatusOK, a)
}

func (s *Server) stock(w http.ResponseWriter, r *http.Request) {
	st, err := s.st.StockPorAlmacen(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if st == nil {
		st = []store.StockAlmacen{}
	}
	escribir(w, http.StatusOK, st)
}

// preview proyecta un producto hacia los cuatro canales.
//
// No abre ninguna conexión con las tiendas: la proyección es una función pura,
// así que se puede ver el payload exacto sin haber configurado una credencial.
func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}

	d, err := s.st.DatosParaPreview(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}

	e := proyeccion.Entrada{
		SKU: d.SKU, Barcode: d.Barcode,
		Titulos: d.Titulos, Descripcion: d.Descripcion,
		Marca: d.Marca, CategPath: d.CategPath,
		Precio: d.Precio, Moneda: "COP",
		Stock: d.Stock, Peso: d.Peso,
		Imagenes: d.Imagenes,
	}
	for _, sp := range d.Specs {
		e.Specs = append(e.Specs, proyeccion.Spec{Clave: sp.Clave, Valor: sp.Valor})
	}

	// La comisión de cada canal se compensa en el precio publicado: lo que
	// queda tras descontarla es el precio base definido en Integra.
	canales, err := s.st.Canales(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	porCodigo := map[string]store.Canal{}
	for _, c := range canales {
		porCodigo[c.Codigo] = c
	}

	// La categoría mapeada y el precio cambian por canal, así que cada
	// proyección se hace con los suyos.
	var proys []proyeccion.Proyeccion
	for _, c := range proyeccion.Todos() {
		ec := e
		ec.CategoriaCanal = d.CategoriasCanal[string(c)]
		if cfg, ok := porCodigo[string(c)]; ok {
			ec.Precio = store.PrecioParaCanal(d.Precio, cfg)
		}
		proys = append(proys, proyeccion.Proyectar(c, ec))
	}

	escribir(w, http.StatusOK, map[string]any{
		"producto":     d,
		"proyecciones": proys,
	})
}

func (s *Server) mapeos(w http.ResponseWriter, r *http.Request) {
	canal := r.URL.Query().Get("canal")
	if canal == "" {
		canal = "mercadolibre"
	}
	m, err := s.st.ListarMapeos(r.Context(), canal)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if m == nil {
		m = []store.MapeoCategoria{}
	}
	escribir(w, http.StatusOK, m)
}

// confirmarMapeo valida una sugerencia para que pueda usarse al publicar.
//
// La confirmación es explícita a propósito: una categoría equivocada en
// MercadoLibre arrastra historial y no se arregla borrando la publicación.
func (s *Server) confirmarMapeo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := s.st.ConfirmarMapeo(r.Context(), id, nil); err != nil {
		// Este error sí es útil para quien usa la interfaz: dice si el mapeo
		// no existe o si le falta la categoría.
		escribir(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "confirmado"})
}

// editarMasivo aplica una operación a muchos productos de una vez.
//
// Acepta identificadores explícitos o un filtro ("todo lo que cumple esta
// búsqueda"), que es donde estaba el trabajo tedioso: poner precio a 157
// productos sin coste conocido, uno por uno.
func (s *Server) editarMasivo(w http.ResponseWriter, r *http.Request) {
	var cuerpo struct {
		IDs    []int64 `json:"ids"`
		Filtro *struct {
			Busqueda      string `json:"q"`
			Marca         string `json:"marca"`
			SoloProblemas bool   `json:"problemas"`
			VerExcluidos  bool   `json:"excluidos"`
			SoloSinPrecio bool   `json:"sin_precio"`
		} `json:"filtro"`
		Operacion store.OperacionMasiva `json:"operacion"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}

	ids := cuerpo.IDs
	if len(ids) == 0 && cuerpo.Filtro != nil {
		var err error
		ids, err = s.st.IDsDeFiltro(r.Context(), store.FiltroProductos{
			Busqueda:      cuerpo.Filtro.Busqueda,
			Marca:         cuerpo.Filtro.Marca,
			SoloProblemas: cuerpo.Filtro.SoloProblemas,
			VerExcluidos:  cuerpo.Filtro.VerExcluidos,
			SoloSinPrecio: cuerpo.Filtro.SoloSinPrecio,
		})
		if err != nil {
			s.fallo(w, err)
			return
		}
	}

	res, err := s.st.EditarMasivo(r.Context(), ids, cuerpo.Operacion)
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, res)
}

func (s *Server) horarios(w http.ResponseWriter, r *http.Request) {
	h, err := s.st.ListarHorarios(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if h == nil {
		h = []store.Horario{}
	}
	escribir(w, http.StatusOK, h)
}

func (s *Server) guardarHorario(w http.ResponseWriter, r *http.Request) {
	var h store.Horario
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	if h.Zona == "" {
		h.Zona = "America/Bogota"
	}
	if h.Nombre == "" {
		h.Nombre = "Tarea programada"
	}
	id, err := s.st.GuardarHorario(r.Context(), h)
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]any{"estado": "guardado", "id": id})
}

func (s *Server) borrarHorario(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := s.st.BorrarHorario(r.Context(), id); err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "borrado"})
}

func (s *Server) alertas(w http.ResponseWriter, r *http.Request) {
	a, err := s.st.ListarAlertas(r.Context(), r.URL.Query().Get("todas") == "1")
	if err != nil {
		s.fallo(w, err)
		return
	}
	if a == nil {
		a = []store.Alerta{}
	}
	escribir(w, http.StatusOK, a)
}

func (s *Server) reconocerAlerta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := s.st.ReconocerAlerta(r.Context(), id); err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "vista"})
}

func (s *Server) resumenAtributos(w http.ResponseWriter, r *http.Request) {
	canal := r.URL.Query().Get("canal")
	if canal == "" {
		canal = "mercadolibre"
	}
	res, err := s.st.ResumenAtributos(r.Context(), canal)
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, res)
}

// atributosProducto lista lo que pide la categoría del producto y con qué
// valor va cada atributo. El id de la ruta es el de variante, como en el
// resto de la interfaz.
func (s *Server) atributosProducto(w http.ResponseWriter, r *http.Request) {
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	canal := r.URL.Query().Get("canal")
	if canal == "" {
		canal = "mercadolibre"
	}
	vals, err := s.st.AtributosDeProducto(r.Context(), prodID, canal)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if vals == nil {
		vals = []store.ValorAtributoProducto{}
	}
	escribir(w, http.StatusOK, map[string]any{
		"canal": canal, "atributos": vals, "faltantes": atributos.FaltantesDe(vals),
	})
}

func (s *Server) guardarAtributo(w http.ResponseWriter, r *http.Request) {
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var cuerpo struct {
		Canal     string `json:"canal"`
		ValueID   string `json:"value_id"`
		ValueName string `json:"value_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	if cuerpo.Canal == "" {
		cuerpo.Canal = "mercadolibre"
	}
	// Lo que se escribe aquí es manual y la deducción automática no lo pisa.
	if err := s.st.GuardarAtributoProducto(r.Context(), prodID, cuerpo.Canal,
		r.PathValue("attr"), cuerpo.ValueID, cuerpo.ValueName, "manual", false); err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "guardado"})
}

func (s *Server) ordenes(w http.ResponseWriter, r *http.Request) {
	o, err := s.st.ListarOrdenes(r.Context(), entero(r.URL.Query().Get("limite"), 50))
	if err != nil {
		s.fallo(w, err)
		return
	}
	if o == nil {
		o = []store.Orden{}
	}
	escribir(w, http.StatusOK, o)
}

func (s *Server) resumenOrdenes(w http.ResponseWriter, r *http.Request) {
	res, err := s.st.ResumenOrdenes(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, res)
}

// ingerirOrdenes encola la traída de pedidos nuevos de una cuenta.
func (s *Server) ingerirOrdenes(w http.ResponseWriter, r *http.Request) {
	if s.cola == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "la cola de trabajos no está disponible"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := ordenes.EncolarIngesta(r.Context(), s.cola, id); err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusAccepted, map[string]string{"estado": "ingesta encolada"})
}

func (s *Server) publicaciones(w http.ResponseWriter, r *http.Request) {
	res, err := s.st.ResumenPublicaciones(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if res == nil {
		res = []store.ResumenPublicacion{}
	}
	escribir(w, http.StatusOK, res)
}

// planificar compara el catálogo con lo publicado y encola solo lo que
// cambió. No envía nada: de eso se encarga el worker.
func (s *Server) planificar(w http.ResponseWriter, r *http.Request) {
	if s.cola == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "la cola de trabajos no está disponible en este proceso"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	plan, err := publicar.Planificar(r.Context(), s.st, s.cola, id)
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, plan)
}

func (s *Server) cuentas(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.ListarCuentas(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if c == nil {
		c = []store.CuentaCanal{}
	}
	escribir(w, http.StatusOK, c)
}

// guardarCuenta recibe las credenciales de un canal, las cifra y las guarda.
// Nunca se devuelven: la interfaz solo ve si la cuenta existe y si conecta.
func (s *Server) guardarCuenta(w http.ResponseWriter, r *http.Request) {
	canal := r.PathValue("canal")
	var cuerpo struct {
		Nombre       string                  `json:"nombre"`
		Credenciales conectores.Credenciales `json:"credenciales"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	if cuerpo.Nombre == "" {
		cuerpo.Nombre = "Cuenta " + canal
	}

	claro, err := json.Marshal(cuerpo.Credenciales)
	if err != nil {
		s.fallo(w, err)
		return
	}
	cifrada, err := s.cif.CifrarTexto(string(claro))
	if err != nil {
		s.fallo(w, err)
		return
	}
	id, err := s.st.GuardarCuenta(r.Context(), canal, cuerpo.Nombre, cifrada)
	if err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]any{"estado": "guardada", "id": id})
}

// probarCuenta hace la llamada mínima al canal que demuestra que la
// credencial funciona, y deja el resultado anotado para el panel.
func (s *Server) probarCuenta(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	canal, cifrada, err := s.st.CredencialesDeCuenta(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	claro, err := s.cif.DescifrarTexto(cifrada)
	if err != nil {
		s.fallo(w, err)
		return
	}
	var cred conectores.Credenciales
	if err := json.Unmarshal([]byte(claro), &cred); err != nil {
		s.fallo(w, err)
		return
	}

	msg, rotadas, err := conectores.Probar(r.Context(), canal, cred)
	ok := err == nil
	if err != nil {
		msg = err.Error()
	}
	// Si probar rotó una credencial (el refresh token de MercadoLibre se
	// invalida al canjearlo), se guarda la nueva aunque la prueba haya
	// fallado después: lo que no se puede es dejar guardado un token muerto.
	if rotadas != nil {
		claro, err := json.Marshal(rotadas)
		if err != nil {
			s.fallo(w, err)
			return
		}
		cifrada, err := s.cif.CifrarTexto(string(claro))
		if err != nil {
			s.fallo(w, err)
			return
		}
		if err := s.st.ActualizarCredenciales(r.Context(), id, cifrada); err != nil {
			s.fallo(w, err)
			return
		}
	}
	if err := s.st.AnotarPrueba(r.Context(), id, ok, msg); err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, map[string]any{"ok": ok, "mensaje": msg})
}

func (s *Server) canales(w http.ResponseWriter, r *http.Request) {
	c, err := s.st.Canales(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, c)
}

// editarCanal fija la comisión con la que se compensa el precio publicado.
func (s *Server) editarCanal(w http.ResponseWriter, r *http.Request) {
	var cuerpo struct {
		ComisionPct float64 `json:"comision_pct"`
		CostoFijo   float64 `json:"costo_fijo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&cuerpo); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	if err := s.st.ActualizarCanal(r.Context(), r.PathValue("codigo"),
		cuerpo.ComisionPct, cuerpo.CostoFijo); err != nil {
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "guardado"})
}

func (s *Server) prioridad(w http.ResponseWriter, r *http.Request) {
	filas, err := s.st.PrioridadPublicacion(r.Context(), entero(r.URL.Query().Get("limite"), 10))
	if err != nil {
		s.fallo(w, err)
		return
	}
	if filas == nil {
		filas = []store.FilaPrioridad{}
	}
	escribir(w, http.StatusOK, filas)
}

// competencia consulta publicaciones ajenas en MercadoLibre para el producto.
//
// Se busca por EAN si existe (identifica el producto exacto) y si no por SKU.
// No se guarda nada: es una consulta en vivo bajo demanda.
func (s *Server) competencia(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	d, err := s.st.DatosParaPreview(r.Context(), varianteID)
	if err != nil {
		s.fallo(w, err)
		return
	}
	consulta := strings.TrimSpace(d.Barcode)
	if consulta == "" {
		consulta = strings.TrimSpace(d.SKU)
	}
	if consulta == "" {
		escribir(w, http.StatusUnprocessableEntity,
			map[string]string{"error": "sin EAN ni SKU no se puede buscar competencia"})
		return
	}

	items, err := mercadolibre.NuevoBuscadorCompetencia(mercadolibre.SitioColombia).
		Buscar(r.Context(), consulta, 8)
	if err != nil {
		escribir(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []mercadolibre.Competidor{}
	}
	escribir(w, http.StatusOK, map[string]any{
		"consulta": consulta, "precio_propio": d.Precio, "items": items,
	})
}

// editarProducto escribe los campos propiedad de Integra.
//
// El identificador de la ruta es el de variante, igual que en el resto de la
// interfaz. Solo se tocan las claves presentes en el cuerpo: mandar
// {"precio": null} borra el precio, no mandar "precio" lo deja como está.
func (s *Server) editarProducto(w http.ResponseWriter, r *http.Request) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}

	var campos map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&campos); err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "cuerpo JSON inválido"})
		return
	}
	if len(campos) == 0 {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "no hay ningún campo que editar"})
		return
	}

	prodID, err := s.st.ProductoDeVariante(r.Context(), varianteID)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	malCampo := func(clave string) {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "valor inválido para " + clave})
	}

	for clave, crudo := range campos {
		var err error
		switch clave {
		case "precio":
			var v *float64
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarPrecio(r.Context(), varianteID, v)
		case "marca":
			var v string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarMarca(r.Context(), prodID, v)
		case "descripcion":
			var v string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarDescripcion(r.Context(), prodID, v)
		case "barcode":
			var v string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarVariante(r.Context(), varianteID, &v, nil)
		case "peso":
			var v float64
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarVariante(r.Context(), varianteID, nil, &v)
		case "excluido":
			var v bool
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarExclusion(r.Context(), prodID, v)
		case "largo_cm", "ancho_cm", "alto_cm":
			var v float64
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			var largo, ancho, alto *float64
			switch clave {
			case "largo_cm":
				largo = &v
			case "ancho_cm":
				ancho = &v
			default:
				alto = &v
			}
			err = s.st.ActualizarDimensiones(r.Context(), varianteID, largo, ancho, alto)
		case "condicion":
			var v string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarFichaComercial(r.Context(), prodID, v, nil, nil, nil, nil)
		case "garantia_meses":
			var v int
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			err = s.st.ActualizarFichaComercial(r.Context(), prodID, "", &v, nil, nil, nil)
		case "garantia_tipo", "video_url", "nota_interna":
			var v string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			switch clave {
			case "garantia_tipo":
				err = s.st.ActualizarFichaComercial(r.Context(), prodID, "", nil, &v, nil, nil)
			case "video_url":
				err = s.st.ActualizarFichaComercial(r.Context(), prodID, "", nil, nil, &v, nil)
			default:
				err = s.st.ActualizarFichaComercial(r.Context(), prodID, "", nil, nil, nil, &v)
			}
		case "titulos":
			// {"titulos": {"mercadolibre": "…"}} — uno por canal, porque cada
			// uno tiene su propio límite de caracteres.
			var v map[string]string
			if json.Unmarshal(crudo, &v) != nil {
				malCampo(clave)
				return
			}
			for canal, titulo := range v {
				if err = s.st.ActualizarTitulo(r.Context(), prodID, canal, titulo); err != nil {
					break
				}
			}
		default:
			escribir(w, http.StatusBadRequest, map[string]string{"error": "campo desconocido: " + clave})
			return
		}
		if err != nil {
			// Los errores de edición sí son útiles para quien teclea: "el
			// precio no puede ser negativo" debe llegar a la interfaz.
			escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
	}

	// La cola de atención depende de lo recién editado: asignar un precio debe
	// hacer desaparecer el aviso "sin precio" al instante.
	if err := s.st.RecalcularAtencion(r.Context()); err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, map[string]string{"estado": "guardado"})
}

func (s *Server) lanzarSync(w http.ResponseWriter, r *http.Request) {
	if s.sincronizar == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "la sincronización manual no está disponible en este proceso"})
		return
	}
	// La sincronización completa tarda más de lo que aguanta una petición HTTP,
	// así que se lanza en segundo plano con su propio contexto: cerrar el
	// navegador no debe abortarla a mitad.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if msg, err := s.sincronizar(ctx); err != nil {
			s.log.Error("la sincronización falló", "error", err)
		} else {
			s.log.Info("sincronización terminada", "resultado", msg)
		}
	}()
	escribir(w, http.StatusAccepted, map[string]string{"estado": "sincronización lanzada"})
}

// ------------------------------------------------------------- utilidades

func escribir(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// La cabecera ya salió: solo queda dejar constancia.
		slog.Error("no se pudo serializar la respuesta", "error", err)
	}
}

func (s *Server) fallo(w http.ResponseWriter, err error) {
	s.log.Error("error atendiendo la petición", "error", err)
	// El detalle va al log, no al cliente: los errores de PostgreSQL revelan
	// estructura interna.
	escribir(w, http.StatusInternalServerError,
		map[string]string{"error": "error interno; revisa el log del servidor"})
}

func entero(s string, porDefecto int) int {
	if s == "" {
		return porDefecto
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return porDefecto
	}
	return n
}

// cors permite que el servidor de desarrollo de Vite hable con la API.
//
// Solo se abren orígenes locales: en producción el frontend se sirve desde el
// mismo origen y esto no llega a activarse.
func cors(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origen := r.Header.Get("Origin")
		if strings.HasPrefix(origen, "http://localhost:") || strings.HasPrefix(origen, "http://127.0.0.1:") {
			w.Header().Set("Access-Control-Allow-Origin", origen)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		siguiente.ServeHTTP(w, r)
	})
}

func registrar(log *slog.Logger, siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inicio := time.Now()
		capt := &capturaEstado{ResponseWriter: w, estado: http.StatusOK}
		siguiente.ServeHTTP(capt, r)
		log.Debug("petición",
			"metodo", r.Method, "ruta", r.URL.Path,
			"estado", capt.estado, "ms", time.Since(inicio).Milliseconds())
	})
}

type capturaEstado struct {
	http.ResponseWriter
	estado int
}

func (c *capturaEstado) WriteHeader(code int) {
	c.estado = code
	c.ResponseWriter.WriteHeader(code)
}

var _ = fmt.Sprintf
