package conectores

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// Reloj falso: el cupo se mide en llamadas por segundo, y una prueba que
// esperara de verdad tardaría segundos y sería frágil en una máquina cargada.
type reloj struct {
	mu sync.Mutex
	t  time.Time
}

func (r *reloj) ahora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *reloj) avanzar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

func conReloj(rps float64, burst int) (*limitador, *reloj) {
	r := &reloj{t: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)}
	l := nuevoLimitador(rps, burst)
	l.ahora = r.ahora
	l.ultimo = r.t
	return l, r
}

func TestElCupoDejaPasarLaRafagaYLuegoFrena(t *testing.T) {
	l, _ := conReloj(2, 3)

	// La ráfaga inicial pasa entera: es para lo que existe el burst.
	for i := 0; i < 3; i++ {
		if espera := l.tomar(); espera != 0 {
			t.Fatalf("la llamada %d de la ráfaga debía pasar, esperó %v", i+1, espera)
		}
	}
	// La cuarta ya no: sin fichas, hay que esperar.
	if espera := l.tomar(); espera <= 0 {
		t.Error("pasada la ráfaga, la siguiente llamada tiene que esperar")
	}
}

func TestElCupoSeRellenaConElTiempo(t *testing.T) {
	l, r := conReloj(2, 1) // dos por segundo: una ficha cada 500 ms

	if espera := l.tomar(); espera != 0 {
		t.Fatal("la primera llamada debía pasar")
	}
	if espera := l.tomar(); espera == 0 {
		t.Fatal("la segunda, sin esperar nada, no debía pasar")
	}

	r.avanzar(500 * time.Millisecond)
	if espera := l.tomar(); espera != 0 {
		t.Errorf("tras medio segundo debía haber una ficha; esperó %v", espera)
	}
}

func TestElCupoNoAcumulaMasAllaDeLaRafaga(t *testing.T) {
	l, r := conReloj(2, 2)

	// Una hora parado no da derecho a una avalancha: el tope es el burst.
	r.avanzar(time.Hour)
	pasadas := 0
	for i := 0; i < 10; i++ {
		if l.tomar() == 0 {
			pasadas++
		}
	}
	if pasadas != 2 {
		t.Errorf("pasaron %d llamadas de golpe; el tope es la ráfaga (2)", pasadas)
	}
}

func TestEsperarRespetaLaCancelacion(t *testing.T) {
	l, _ := conReloj(1, 1)
	_ = l.tomar() // agota la única ficha

	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()

	// Apagar el worker no puede dejar llamadas colgadas esperando su turno.
	if err := l.Esperar(ctx); err == nil {
		t.Error("con el contexto cancelado, Esperar debe devolver su error")
	}
}

func TestElCupoSeCompartePorCuenta(t *testing.T) {
	OlvidarCupos()
	defer OlvidarCupos()

	// AdaptadorDeCuenta construye un adaptador nuevo en cada trabajo: si el
	// limitador no se compartiera, cada trabajo empezaría con la ráfaga llena
	// y el cupo no limitaría nada.
	a := limitadorDe(7, 2, 3)
	b := limitadorDe(7, 2, 3)
	if a != b {
		t.Error("dos adaptadores de la misma cuenta deben compartir limitador")
	}
	if c := limitadorDe(8, 2, 3); c == a {
		t.Error("cuentas distintas no pueden compartir cupo: agotar una frenaría a la otra")
	}
}

func TestUnCupoInvalidoNoDesactivaElFreno(t *testing.T) {
	// Una fila con 0 en la columna no puede significar "sin límite": sería
	// justo la cuenta que tumba la integración.
	l := nuevoLimitador(0, 0)
	if l.rps <= 0 || l.burst < 1 {
		t.Errorf("rps=%v burst=%v: un cupo inválido debe caer en el valor por defecto", l.rps, l.burst)
	}
}

// ------------------------------------------------- la pausa que pide el canal

// canalQuePideParar contesta 429 con su plazo y cuenta cuántas llamadas le
// llegaron de verdad. Es el doble mínimo del contrato: solo se ejercitan las
// escrituras.
type canalQuePideParar struct {
	llamadas int
	espera   time.Duration
}

func (c *canalQuePideParar) error429() error {
	c.llamadas++
	return &channel.Error{
		Kind: channel.MercadoLibre, StatusCode: 429,
		Message: "too many requests", RetryAfter: c.espera,
	}
}

func (c *canalQuePideParar) Kind() channel.Kind                 { return channel.MercadoLibre }
func (c *canalQuePideParar) Capabilities() channel.Capabilities { return channel.Capabilities{} }
func (c *canalQuePideParar) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	return channel.PublishResult{}, c.error429()
}
func (c *canalQuePideParar) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	return channel.UpdateResult{}, c.error429()
}
func (c *canalQuePideParar) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	// Los canales que responden por elemento meten el 429 dentro del lote: si
	// solo se mirara el error de arriba, este freno no llegaría nunca.
	return []channel.OpResult{{Ref: ups[0].Ref, Error: c.error429()}}, nil
}
func (c *canalQuePideParar) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	return nil, c.error429()
}
func (c *canalQuePideParar) Pause(ctx context.Context, ref channel.ExternalRef) error {
	return c.error429()
}
func (c *canalQuePideParar) Resume(ctx context.Context, ref channel.ExternalRef) error {
	return c.error429()
}
func (c *canalQuePideParar) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	return nil, c.error429()
}
func (c *canalQuePideParar) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	return channel.RemotePage{}, c.error429()
}
func (c *canalQuePideParar) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	return channel.OrderPage{}, c.error429()
}
func (c *canalQuePideParar) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	return c.error429()
}

// El 429 que recibe un trabajo tiene que frenar a la cuenta entera: con ocho
// trabajos en vuelo, seguir golpeando la API que acaba de pedir parar es lo
// que alarga el bloqueo. Y el freno no puede ser una espera bloqueante: el
// hueco del worker y el lease del trabajo duran mucho menos que el castigo.
func TestUn429FrenaLaCuentaEnteraSinBloquearElWorker(t *testing.T) {
	ad := &canalQuePideParar{espera: 30 * time.Minute}
	lim, reloj := conReloj(100, 100) // cupo holgado: aquí frena la pausa, no el ritmo
	a := conCupo{Adapter: ad, lim: lim}
	ctx := context.Background()

	// El primero recibe el 429 y anota la pausa de la cuenta.
	if _, err := a.UpdateStock(ctx, []channel.StockUpdate{
		{Ref: channel.ExternalRef{SKU: "A-1"}, Quantity: 3}}); err != nil {
		t.Fatalf("UpdateStock reparte el error por elemento: %v", err)
	}
	if ad.llamadas != 1 {
		t.Fatalf("la primera llamada sí tiene que salir: %d", ad.llamadas)
	}

	// El segundo ni siquiera sale, y falla con el plazo que pidió el canal.
	_, err := a.Publish(ctx, channel.PublishRequest{})
	if err == nil {
		t.Fatal("con la cuenta en pausa la llamada tiene que fallar")
	}
	if ad.llamadas != 1 {
		t.Fatalf("con la cuenta en pausa no puede salir ninguna llamada más: %d", ad.llamadas)
	}
	var e *channel.Error
	if !errors.As(err, &e) || e.RetryAfter <= 0 {
		t.Fatalf("el error tiene que llevar cuánto falta para reanudar: %v", err)
	}
	if e.RetryAfter > 30*time.Minute {
		t.Fatalf("la espera no puede crecer sola: %v", e.RetryAfter)
	}

	// Pasado el castigo, la cuenta vuelve a hablar sin que nadie la reactive.
	reloj.avanzar(31 * time.Minute)
	if _, err := a.Publish(ctx, channel.PublishRequest{}); err == nil {
		t.Fatal("el doble siempre responde 429: se esperaba error")
	}
	if ad.llamadas != 2 {
		t.Fatalf("terminada la pausa la llamada tiene que salir: %d", ad.llamadas)
	}
}

// Un Retry-After absurdo —o mal leído— no puede dejar muda una cuenta media
// jornada: el castigo se acota.
func TestLaPausaDeLaCuentaTieneTope(t *testing.T) {
	lim, _ := conReloj(100, 100)
	lim.Frenar(48 * time.Hour)
	if falta := lim.pausaRestante(); falta > TopePausa {
		t.Fatalf("la pausa se acota en %v y quedó en %v", TopePausa, falta)
	}
}

// Retry-After viaja en segundos o en fecha HTTP, y hay canales que contestan
// 429 sin cabecera ninguna: ahí lo peligroso es no esperar nada.
func TestLaEsperaTrasCupoLeeLasDosFormasYTieneMinimo(t *testing.T) {
	con := func(v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set("Retry-After", v)
		}
		return h
	}
	if d := EsperaTrasCupo(con("120")); d != 2*time.Minute {
		t.Errorf("Retry-After en segundos: %v", d)
	}
	if d := EsperaTrasCupo(con(time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat))); d < 8*time.Minute {
		t.Errorf("Retry-After como fecha HTTP: %v", d)
	}
	if d := EsperaTrasCupo(con("")); d != EsperaMinimaTrasCupo {
		t.Errorf("sin cabecera hay que esperar el mínimo, no cero: %v", d)
	}
	if d := EsperaTrasCupo(con("mañana por la tarde")); d != EsperaMinimaTrasCupo {
		t.Errorf("una cabecera ilegible no puede valer cero: %v", d)
	}
}

func (c *canalQuePideParar) Delete(ctx context.Context, ref channel.ExternalRef) error {
	return c.error429()
}
