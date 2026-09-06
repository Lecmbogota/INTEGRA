package publicar

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"
)

// ---------------------------------------------------------------- dobles

// almacenFalso sustituye al store para poder mirar exactamente qué hash se
// guarda tras cada envío, que es donde estaba el defecto: dar por publicado
// lo que nunca salió congela el producto en el canal para siempre.
type almacenFalso struct {
	candidato store.CandidatoPublicacion
	ref       channel.ExternalRef

	guardado *publicacionGuardada
	// soloContenido distingue por qué camino se guardó: actualizar una ficha
	// viva no puede tocar el precio ni el stock publicados.
	soloContenido bool
	precioHash    string
	stockHash     string
	errAnotados   []string

	porConciliar []store.PublicacionViva
	marcas       map[int64]marca
}

// marca es el veredicto que la conciliación escribió sobre una variante.
type marca struct{ veredicto, detalle string }

type publicacionGuardada struct {
	externalID, varianteExterna       string
	contentHash, priceHash, stockHash string
	precio                            float64
	cantidad                          int
}

func (a *almacenFalso) UnCandidato(ctx context.Context, cuentaID, varianteID int64) (*store.CandidatoPublicacion, error) {
	c := a.candidato
	return &c, nil
}

func (a *almacenFalso) AtributosParaPublicar(ctx context.Context, productoID int64, canalCodigo string) (map[string]string, []string, error) {
	return map[string]string{}, nil, nil
}

func (a *almacenFalso) RefDePublicacion(ctx context.Context, cuentaID, varianteID int64) (channel.ExternalRef, error) {
	return a.ref, nil
}

func (a *almacenFalso) GuardarPublicacion(ctx context.Context, cuentaID, productoID, varianteID int64,
	externalID, externalURL, varianteExterna, contentHash, priceHash, stockHash string,
	precio float64, cantidad int) error {

	a.guardado = &publicacionGuardada{
		externalID: externalID, varianteExterna: varianteExterna,
		contentHash: contentHash, priceHash: priceHash, stockHash: stockHash,
		precio: precio, cantidad: cantidad,
	}
	return nil
}

func (a *almacenFalso) GuardarContenidoPublicado(ctx context.Context, cuentaID, productoID, varianteID int64,
	externalID, externalURL, varianteExterna, contentHash string) error {

	// Se anota igual que una publicación, pero con precio y stock a cero y sus
	// hashes vacíos: es lo que distingue "solo se mandó la ficha" de "se
	// publicó entero", y es justo lo que comprueban las pruebas.
	a.guardado = &publicacionGuardada{
		externalID: externalID, varianteExterna: varianteExterna,
		contentHash: contentHash,
	}
	a.soloContenido = true
	return nil
}

func (a *almacenFalso) GuardarPrecioPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, precio float64) error {
	a.precioHash = hash
	return nil
}

func (a *almacenFalso) GuardarStockPublicado(ctx context.Context, cuentaID, varianteID int64, hash string, cantidad int) error {
	a.stockHash = hash
	return nil
}

func (a *almacenFalso) AnotarErrorPublicacion(ctx context.Context, cuentaID, productoID int64, causa string) error {
	a.errAnotados = append(a.errAnotados, causa)
	return nil
}

// Lo que necesita la conciliación. porConciliar es lo que el doble entrega
// como "publicado según Integra", y marcas anota qué veredicto se escribió
// sobre cada variante: es lo único que distingue recrear una ficha muerta de
// dejar cerrada la que bajó el canal.
func (a *almacenFalso) PublicacionesPorConciliar(ctx context.Context, cuentaID int64, limite int, periodo time.Duration) ([]store.PublicacionViva, error) {
	return a.porConciliar, nil
}

func (a *almacenFalso) MarcarPublicacionViva(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	a.anotarMarca(varianteID, "viva", estadoCanal)
	return nil
}

func (a *almacenFalso) MarcarPublicacionCaida(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	a.anotarMarca(varianteID, "caida", estadoCanal)
	return nil
}

func (a *almacenFalso) MarcarPublicacionRetirada(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	a.anotarMarca(varianteID, "retirada", estadoCanal)
	return nil
}

func (a *almacenFalso) MarcarPublicacionPausada(ctx context.Context, cuentaID, varianteID int64, motivo string) error {
	a.anotarMarca(varianteID, "pausada", motivo)
	return nil
}

func (a *almacenFalso) MarcarPublicacionReanudada(ctx context.Context, cuentaID, varianteID int64) error {
	a.anotarMarca(varianteID, "reanudada", "")
	return nil
}

func (a *almacenFalso) anotarMarca(varianteID int64, veredicto, detalle string) {
	if a.marcas == nil {
		a.marcas = map[int64]marca{}
	}
	a.marcas[varianteID] = marca{veredicto: veredicto, detalle: detalle}
}

// colaFalsa anota qué trabajos se pidieron, en orden.
type colaFalsa struct{ encolados []string }

func (c *colaFalsa) Encolar(ctx context.Context, kind string, payload any, op jobs.Opciones) (int64, error) {
	c.encolados = append(c.encolados, kind)
	return int64(len(c.encolados)), nil
}

func (c *colaFalsa) tiene(kind string) bool {
	for _, k := range c.encolados {
		if k == kind {
			return true
		}
	}
	return false
}

// canalFalso es un adaptador que no habla con nadie y apunta qué le pidieron.
// Sirve para cualquiera de los cuatro canales: el núcleo solo ve el contrato.
type canalFalso struct {
	adopta bool

	publicaciones   []channel.Product
	actualizadas    []channel.UpdateRequest
	preciosEnviados []channel.PriceUpdate
	stocksEnviados  []channel.StockUpdate

	// estados es lo que el canal contesta a FetchStatus, por identificador de
	// publicación. Una referencia que no esté en el mapa no sale en la
	// respuesta, que es como los cuatro canales dicen "esa ya no la tengo".
	estados   map[string]string
	errEstado error

	pausadas   []channel.ExternalRef
	reanudadas []channel.ExternalRef
	errPausa   error
}

func (c *canalFalso) Kind() channel.Kind { return channel.Shopify }
func (c *canalFalso) Capabilities() channel.Capabilities {
	return channel.Capabilities{MaxTitleLength: 255}
}

func (c *canalFalso) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	c.publicaciones = append(c.publicaciones, req.Product)
	if c.adopta {
		// Igual que los cuatro adaptadores reales: se encuentra el SKU y se
		// devuelve la referencia sin haber escrito nada en el canal.
		ref := channel.ExternalRef{ListingID: "ADOPTADA-1", VariantID: "ADOPTADA-V1", SKU: req.Product.SKU}
		return channel.PublishResult{Ref: ref, Adopted: true}, nil
	}
	return channel.PublishResult{Ref: channel.ExternalRef{
		ListingID: "NUEVA-1", VariantID: "NUEVA-V1", SKU: req.Product.SKU,
	}}, nil
}

func (c *canalFalso) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	c.actualizadas = append(c.actualizadas, req)
	return channel.UpdateResult{Ref: req.Ref}, nil
}

func (c *canalFalso) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	c.stocksEnviados = append(c.stocksEnviados, ups...)
	return []channel.OpResult{{Ref: ups[0].Ref, OK: true}}, nil
}

func (c *canalFalso) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	c.preciosEnviados = append(c.preciosEnviados, ups...)
	return []channel.OpResult{{Ref: ups[0].Ref, OK: true}}, nil
}

func (c *canalFalso) Pause(ctx context.Context, ref channel.ExternalRef) error {
	if c.errPausa != nil {
		return c.errPausa
	}
	c.pausadas = append(c.pausadas, ref)
	return nil
}

func (c *canalFalso) Resume(ctx context.Context, ref channel.ExternalRef) error {
	if c.errPausa != nil {
		return c.errPausa
	}
	c.reanudadas = append(c.reanudadas, ref)
	return nil
}

func (c *canalFalso) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	if c.errEstado != nil {
		return nil, c.errEstado
	}
	var out []channel.ListingStatus
	for _, r := range refs {
		est, ok := c.estados[r.ListingID]
		if !ok {
			continue // desaparecida: no hay fila que devolver
		}
		out = append(out, channel.ListingStatus{Ref: r, Status: est})
	}
	return out, nil
}
func (c *canalFalso) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	return channel.RemotePage{Done: true}, nil
}
func (c *canalFalso) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	return channel.OrderPage{Done: true}, nil
}
func (c *canalFalso) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	return nil
}

// ------------------------------------------------------------- utilidades

// registroMudo evita ensuciar la salida de las pruebas con los avisos que
// devuelven los canales.
func registroMudo() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func servicioDePrueba(st almacen, cola encolador, ad channel.Adapter) *Servicio {
	return &Servicio{
		st: st, cola: cola, log: registroMudo(),
		baseURL: "https://integra.example",
		// Igual que en producción (NuevoServicio): una publicación que
		// desapareció del canal se vuelve a crear.
		recrearCaidas: true,
		adaptador: func(ctx context.Context, cuentaID int64) (channel.Adapter, error) {
			return ad, nil
		},
	}
}

func trabajoDe(t *testing.T, cuentaID, varianteID int64) jobs.Trabajo {
	t.Helper()
	cuerpo, err := json.Marshal(PayloadPublicar{CuentaID: cuentaID, VarianteID: varianteID})
	if err != nil {
		t.Fatal(err)
	}
	return jobs.Trabajo{ID: 1, Kind: TrabajoPublicar, Payload: cuerpo}
}

// candidatoListo es el de motor_test.go, ya publicable.
func candidatoListo() store.CandidatoPublicacion {
	c := base()
	c.ProductoID = 10
	c.Listo = true
	return c
}

// ------------------------------------------------------------- pruebas

// El defecto crítico: con la publicación ya existente, publicar_producto solo
// llamaba a Publish —que adopta y no escribe— y guardaba los tres hashes. El
// cambio de contenido no llegaba nunca y el de precio se daba por hecho.
func TestContenidoDeUnaPublicacionExistenteSeMandaConUpdate(t *testing.T) {
	st := &almacenFalso{
		candidato: candidatoListo(),
		ref:       channel.ExternalRef{ListingID: "77", VariantID: "77V", SKU: "ABC-123"},
	}
	st.candidato.ExternalID = "77"
	st.candidato.Titulo = "Disco 1TB (corregido)"

	ad := &canalFalso{}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.publicar(context.Background(), trabajoDe(t, 1, 1)); err != nil {
		t.Fatal(err)
	}

	if len(ad.publicaciones) != 0 {
		t.Fatalf("una publicación que ya existe no se vuelve a crear: %d llamadas a Publish", len(ad.publicaciones))
	}
	if len(ad.actualizadas) != 1 {
		t.Fatalf("el contenido nuevo tiene que viajar en Update: %d llamadas", len(ad.actualizadas))
	}
	if got := ad.actualizadas[0].Product.Title; got != "Disco 1TB (corregido)" {
		t.Fatalf("Update recibió el título viejo: %q", got)
	}
	if got := ad.actualizadas[0].Ref.ListingID; got != "77" {
		t.Fatalf("Update tiene que apuntar a la publicación registrada, no a %q", got)
	}

	if st.guardado == nil {
		t.Fatal("no se guardó nada")
	}
	if st.guardado.contentHash != HashContenido(st.candidato) {
		t.Fatal("el contenido sí se envió: su hash tiene que quedar guardado")
	}
	if st.guardado.priceHash != "" || st.guardado.stockHash != "" {
		t.Fatalf("Update no lleva precio ni stock: no pueden darse por publicados (precio=%q stock=%q)",
			st.guardado.priceHash, st.guardado.stockHash)
	}
	if !cola.tiene(TrabajoPrecio) || !cola.tiene(TrabajoStock) {
		t.Fatalf("hay que pedir precio y stock aparte; encolado: %v", cola.encolados)
	}
}

// La adopción es el camino habitual de la primera sincronización sobre un
// catálogo vivo: el adaptador reconoce el SKU y no escribe nada.
func TestAdopcionNoDaPorPublicadosNiFichaNiPrecioNiStock(t *testing.T) {
	st := &almacenFalso{candidato: candidatoListo()}
	ad := &canalFalso{adopta: true}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.publicar(context.Background(), trabajoDe(t, 1, 1)); err != nil {
		t.Fatal(err)
	}

	if len(ad.actualizadas) != 1 {
		t.Fatalf("tras adoptar hay que mandar la ficha con Update: %d llamadas", len(ad.actualizadas))
	}
	if got := ad.actualizadas[0].Ref.ListingID; got != "ADOPTADA-1" {
		t.Fatalf("Update tiene que usar la referencia adoptada, no %q", got)
	}
	if st.guardado.externalID != "ADOPTADA-1" || st.guardado.varianteExterna != "ADOPTADA-V1" {
		t.Fatalf("hay que registrar la publicación adoptada: %+v", st.guardado)
	}
	if st.guardado.priceHash != "" || st.guardado.stockHash != "" {
		t.Fatalf("en la adopción no salió ni el precio ni el stock (precio=%q stock=%q)",
			st.guardado.priceHash, st.guardado.stockHash)
	}
	if !cola.tiene(TrabajoPrecio) || !cola.tiene(TrabajoStock) {
		t.Fatalf("la adopción tiene que pedir precio y stock; encolado: %v", cola.encolados)
	}
}

// La creación de verdad sí lleva precio y stock en el mismo cuerpo: ahí los
// tres hashes describen lo que tiene el canal y no hace falta pedir nada más.
func TestPublicacionNuevaGuardaLosTresHashesYNoEncolaNada(t *testing.T) {
	st := &almacenFalso{candidato: candidatoListo()}
	ad := &canalFalso{}
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.publicar(context.Background(), trabajoDe(t, 1, 1)); err != nil {
		t.Fatal(err)
	}

	if len(ad.publicaciones) != 1 || len(ad.actualizadas) != 0 {
		t.Fatalf("lo nuevo se crea con Publish: %d publish, %d update",
			len(ad.publicaciones), len(ad.actualizadas))
	}
	c := st.candidato
	if st.guardado.contentHash != HashContenido(c) ||
		st.guardado.priceHash != HashPrecio(c) ||
		st.guardado.stockHash != HashStock(c) {
		t.Fatalf("la creación publica los tres a la vez: %+v", st.guardado)
	}
	if len(cola.encolados) != 0 {
		t.Fatalf("no hace falta pedir nada más: %v", cola.encolados)
	}
}

// -------------------------------------------------- planificación (motor)

type catalogoFalso struct {
	items     []store.CandidatoPublicacion
	huerfanas []store.PublicacionViva
}

func (c *catalogoFalso) CandidatosPublicacion(ctx context.Context, cuentaID int64) ([]store.CandidatoPublicacion, error) {
	return c.items, nil
}

func (c *catalogoFalso) PublicacionesHuerfanas(ctx context.Context, cuentaID int64) ([]store.PublicacionViva, error) {
	return c.huerfanas, nil
}

// La rama de contenido era excluyente: cambiar descripción y precio a la vez
// encolaba solo publicar_producto y el precio nuevo se perdía.
func TestPlanificarNoSeTragaElCambioDePrecioNiElDeStock(t *testing.T) {
	c := candidatoListo()
	c.ExternalID = "77"
	// Lo publicado corresponde al estado anterior en las tres dimensiones.
	viejo := c
	viejo.Titulo = "Disco 1TB"
	viejo.PrecioCanal = 100000
	viejo.Stock = 7
	c.ContentHash = HashContenido(viejo)
	c.PriceHash = HashPrecio(viejo)
	c.StockHash = HashStock(viejo)
	// El operador corrige la ficha y sube el precio en la misma tanda.
	c.Titulo = "Disco 1TB (corregido)"
	c.PrecioCanal = 150000

	cola := &colaFalsa{}
	plan, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !cola.tiene(TrabajoPublicar) {
		t.Fatalf("el contenido cambió: %v", cola.encolados)
	}
	if !cola.tiene(TrabajoPrecio) {
		t.Fatalf("el precio cambió y también hay que encolarlo: %v", cola.encolados)
	}
	if cola.tiene(TrabajoStock) {
		t.Fatalf("el stock no cambió: %v", cola.encolados)
	}
	if plan.Publicar != 1 || plan.Precio != 1 || plan.Stock != 0 {
		t.Fatalf("el resumen no cuadra: %+v", plan)
	}
}

// Lo que nunca se publicó se crea de una vez, con precio y stock dentro: no
// hay que encolar envíos que fallarían por no existir todavía la referencia.
func TestPlanificarPublicacionNuevaSoloEncolaLaCreacion(t *testing.T) {
	c := candidatoListo() // sin ExternalID ni hashes

	cola := &colaFalsa{}
	if _, err := Planificar(context.Background(), &catalogoFalso{items: []store.CandidatoPublicacion{c}}, cola, 1); err != nil {
		t.Fatal(err)
	}
	if len(cola.encolados) != 1 || cola.encolados[0] != TrabajoPublicar {
		t.Fatalf("solo la creación: %v", cola.encolados)
	}
}

// ------------------------------------------------- canal real por HTTP

// mlFalso imita lo justo de api.mercadolibre.com. Se usa un adaptador real, y
// no el doble, para comprobar que el arreglo llega hasta la petición HTTP:
// antes, un cambio de contenido sobre un SKU ya publicado no producía una
// sola escritura contra el canal.
type mlFalso struct {
	*httptest.Server
	mu       sync.Mutex
	escritos []escritura
}

type escritura struct {
	metodo string
	ruta   string
	cuerpo map[string]any
}

func nuevoML(t *testing.T) *mlFalso {
	t.Helper()
	s := &mlFalso{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /users/me", func(w http.ResponseWriter, r *http.Request) {
		responderJSON(w, map[string]any{"id": 123, "nickname": "TEST", "tags": []string{}})
	})
	// El SKU ya está publicado: es lo que dispara la adopción y lo que antes
	// hacía que no se enviara nada.
	mux.HandleFunc("GET /users/123/items/search", func(w http.ResponseWriter, r *http.Request) {
		responderJSON(w, map[string]any{"results": []string{"MCO1"}})
	})
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		s.anotar(r, nil)
		responderJSON(w, map[string]any{"id": "MCO-DUPLICADO"})
	})
	mux.HandleFunc("PUT /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		cuerpo := s.anotar(r, r.Body)
		resp := map[string]any{"id": r.PathValue("id")}
		if precio, ok := cuerpo["price"]; ok {
			resp["price"] = precio
		}
		responderJSON(w, resp)
	})
	mux.HandleFunc("PUT /items/{id}/description", func(w http.ResponseWriter, r *http.Request) {
		s.anotar(r, r.Body)
		responderJSON(w, map[string]any{"plain_text": "ok"})
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *mlFalso) anotar(r *http.Request, cuerpo io.Reader) map[string]any {
	var m map[string]any
	if cuerpo != nil {
		datos, _ := io.ReadAll(cuerpo)
		_ = json.Unmarshal(datos, &m)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.escritos = append(s.escritos, escritura{metodo: r.Method, ruta: r.URL.Path, cuerpo: m})
	return m
}

func (s *mlFalso) escrituras() []escritura {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]escritura(nil), s.escritos...)
}

func responderJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestSobreUnCanalRealElContenidoYElPrecioSalenPorLaRed(t *testing.T) {
	ml := nuevoML(t)
	viejo := conectores.URLBaseML
	conectores.URLBaseML = ml.URL
	t.Cleanup(func() { conectores.URLBaseML = viejo })

	ad, err := channel.New(channel.MercadoLibre, channel.Config{
		Credentials: map[string]string{"access_token": "APP_USR-vivo"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Producto ya publicado al que se le corrige la descripción y se le sube
	// el precio en la misma tanda: el escenario del informe.
	st := &almacenFalso{
		candidato: candidatoListo(),
		ref:       channel.ExternalRef{ListingID: "MCO1", VariantID: "MCO1", SKU: "ABC-123"},
	}
	st.candidato.ExternalID = "MCO1"
	st.candidato.Descripcion = "Descripción corregida."
	st.candidato.PrecioCanal = 150000
	cola := &colaFalsa{}
	s := servicioDePrueba(st, cola, ad)

	if err := s.publicar(context.Background(), trabajoDe(t, 1, 1)); err != nil {
		t.Fatal(err)
	}

	escritos := ml.escrituras()
	if len(escritos) == 0 {
		t.Fatal("el cambio de contenido no produjo una sola escritura contra el canal")
	}
	var descripcion string
	for _, e := range escritos {
		if e.metodo == http.MethodPost {
			t.Fatalf("el SKU ya estaba publicado: no se puede crear otra publicación (%s %s)", e.metodo, e.ruta)
		}
		if strings.HasSuffix(e.ruta, "/description") {
			descripcion, _ = e.cuerpo["plain_text"].(string)
		}
	}
	if descripcion != "Descripción corregida." {
		t.Fatalf("la descripción nueva no salió por la red: %+v", escritos)
	}

	// Y el precio, que el manejador dejó pedido, llega en su propio trabajo.
	if !cola.tiene(TrabajoPrecio) {
		t.Fatalf("el precio quedó sin pedir: %v", cola.encolados)
	}
	if err := s.actualizarPrecio(context.Background(), trabajoDe(t, 1, 1)); err != nil {
		t.Fatal(err)
	}
	ultimo := ml.escrituras()
	fin := ultimo[len(ultimo)-1]
	if fin.cuerpo["price"] != 150000.0 {
		t.Fatalf("el precio nuevo no llegó al canal: %+v", fin)
	}
	if st.precioHash != HashPrecio(st.candidato) {
		t.Fatal("tras enviarlo de verdad, el hash de precio sí se guarda")
	}
}

// La URL que se publica tiene que terminar en un nombre de fichero con
// extension. WordPress —y con el cualquier canal que use la biblioteca de
// medios de WordPress— mira el final de la URL para decidir el tipo del
// fichero, ignora el Content-Type, y sin extension responde «no tienes
// permiso para subir este tipo de fichero». La ficha entera se rechaza y el
// mensaje no menciona la URL, asi que el motivo real no se ve por ningun lado.
func TestLaURLDeLaImagenPublicadaLlevaExtension(t *testing.T) {
	s := &Servicio{baseURL: "https://integra.ejemplo.com"}
	p := s.producto(&store.CandidatoPublicacion{
		SKU:      "SKU-1",
		Titulo:   "Un producto",
		Imagenes: []string{"abc123"},
	}, channel.Capabilities{})

	if len(p.Images) != 1 {
		t.Fatalf("se esperaba una imagen, hay %d", len(p.Images))
	}
	quiero := "https://integra.ejemplo.com/imagenes/abc123/cuadrada_1200.jpg"
	if p.Images[0].URL != quiero {
		t.Errorf("URL de imagen sin extension: %q", p.Images[0].URL)
	}
	// El hash sigue siendo el identificador: la extension es solo del nombre.
	if p.Images[0].Hash != "abc123" {
		t.Errorf("el hash cambio: %q", p.Images[0].Hash)
	}
}
