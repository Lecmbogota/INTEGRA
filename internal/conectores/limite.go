package conectores

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdv/integra/internal/channel"
)

// Cupo de llamadas por cuenta de canal.
//
// El worker reclama tantos trabajos por tick como concurrencia tenga y
// dispara todas las peticiones a la vez. Publicar el catálogo entero —452
// productos por cuatro canales— contra una cuenta cuyo límite real son un
// par de peticiones por segundo produce 429 en cadena; como esos 429 vuelven
// a la cola con backoff, la tormenta se repite sola. MercadoLibre además
// bloquea la aplicación temporalmente por exceso.
//
// La columna rate_limit_rps de channel_accounts existía desde el primer
// esquema y no la leía nadie.

// limitador es un cubo de fichas: se rellena a `rps` fichas por segundo hasta
// un tope de `burst`, y cada llamada consume una.
//
// Se escribe aquí en vez de traer una dependencia porque son treinta líneas y
// el proyecto ya tiene ese criterio en otras piezas de bajo nivel.
type limitador struct {
	mu     sync.Mutex
	fichas float64
	rps    float64
	burst  float64
	ultimo time.Time
	// pausa es hasta cuándo el canal pidió que paráramos (un 429 con
	// Retry-After). El cubo de fichas no sirve para esto: mide nuestro ritmo,
	// no el castigo que ya nos impusieron.
	pausa time.Time
	// ahora se sustituye en las pruebas para no depender del reloj real.
	ahora func() time.Time
}

func nuevoLimitador(rps float64, burst int) *limitador {
	if rps <= 0 {
		rps = 2 // el mismo valor por defecto que declara el esquema
	}
	if burst < 1 {
		burst = 1
	}
	return &limitador{
		fichas: float64(burst), rps: rps, burst: float64(burst),
		ahora: time.Now,
	}
}

// TopePausa acota lo que un canal puede parar una cuenta. Los bloqueos reales
// de MercadoLibre son de minutos a una hora; más allá de eso es más probable
// una cabecera mal leída que un castigo de verdad, y no puede dejar la cuenta
// muda media jornada.
const TopePausa = time.Hour

// Frenar apunta que el canal pidió parar durante `d`. Se queda la pausa más
// larga de las vigentes: si dos trabajos reciben 429 a la vez, manda el que
// pide esperar más.
func (l *limitador) Frenar(d time.Duration) {
	if d <= 0 {
		return
	}
	if d > TopePausa {
		d = TopePausa
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if hasta := l.ahora().Add(d); hasta.After(l.pausa) {
		l.pausa = hasta
	}
}

// pausaRestante dice cuánto falta para que la cuenta vuelva a hablar con el
// canal, o cero si no está frenada.
func (l *limitador) pausaRestante() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pausa.IsZero() {
		return 0
	}
	if falta := l.pausa.Sub(l.ahora()); falta > 0 {
		return falta
	}
	return 0
}

// Esperar bloquea hasta que haya una ficha libre, o hasta que el contexto se
// cancele. Devolver el error del contexto es lo que permite que apagar el
// worker no deje llamadas colgadas.
func (l *limitador) Esperar(ctx context.Context) error {
	for {
		espera := l.tomar()
		if espera == 0 {
			return nil
		}
		t := time.NewTimer(espera)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// tomar consume una ficha si la hay, o devuelve cuánto falta para la próxima.
func (l *limitador) tomar() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	ahora := l.ahora()
	if l.ultimo.IsZero() {
		l.ultimo = ahora
	}
	if t := ahora.Sub(l.ultimo); t > 0 {
		l.fichas += t.Seconds() * l.rps
		if l.fichas > l.burst {
			l.fichas = l.burst
		}
		l.ultimo = ahora
	}
	if l.fichas >= 1 {
		l.fichas--
		return 0
	}
	// Lo que falta para completar una ficha, con un mínimo para no girar en
	// vacío si el reloj no avanza entre dos llamadas.
	falta := time.Duration((1 - l.fichas) / l.rps * float64(time.Second))
	if falta < time.Millisecond {
		falta = time.Millisecond
	}
	return falta
}

// EsperaMinimaTrasCupo es lo que se espera cuando el canal contesta 429 sin
// decir hasta cuándo. Devolver cero dejaría el reintento en manos del backoff
// de 30 s, que es volver a golpear a quien acaba de pedir que paremos.
const EsperaMinimaTrasCupo = time.Minute

// EsperaTrasCupo traduce la cabecera Retry-After de una respuesta 429.
//
// El RFC admite dos formas —segundos o fecha HTTP— y los canales usan las dos,
// así que se aceptan ambas: quedarse solo con la primera deja sin plazo la
// mitad de los bloqueos.
func EsperaTrasCupo(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return EsperaMinimaTrasCupo
	}
	if segundos, err := strconv.ParseFloat(v, 64); err == nil {
		if segundos <= 0 {
			return EsperaMinimaTrasCupo
		}
		return time.Duration(segundos * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		if falta := time.Until(t); falta > 0 {
			return falta
		}
	}
	return EsperaMinimaTrasCupo
}

var (
	limitadoresMu sync.Mutex
	limitadores   = map[int64]*limitador{}
)

// limitadorDe devuelve el cupo de una cuenta, compartido entre todos los
// adaptadores que se construyan para ella.
//
// Tiene que ser de paquete y no del adaptador: AdaptadorDeCuenta construye
// uno nuevo en cada trabajo a propósito, para que una credencial reemplazada
// surta efecto sin reiniciar. Un limitador por adaptador no limitaría nada.
func limitadorDe(cuentaID int64, rps float64, burst int) *limitador {
	limitadoresMu.Lock()
	defer limitadoresMu.Unlock()

	if l, ok := limitadores[cuentaID]; ok {
		return l
	}
	l := nuevoLimitador(rps, burst)
	limitadores[cuentaID] = l
	return l
}

// OlvidarCupos descarta los limitadores en memoria. Solo lo usan las pruebas
// y el cambio de configuración de una cuenta.
func OlvidarCupos() {
	limitadoresMu.Lock()
	defer limitadoresMu.Unlock()
	limitadores = map[int64]*limitador{}
}

// conCupo envuelve un adaptador para que respete el cupo de su cuenta.
//
// Kind y Capabilities no pasan por el cupo: no hablan con el canal.
type conCupo struct {
	channel.Adapter
	lim *limitador
}

// esperar pide ficha, salvo que el canal haya pedido parar.
//
// Cuando hay pausa NO se bloquea esperándola: un bloqueo de una hora ocuparía
// uno de los ocho huecos del worker, y el lease del trabajo dura cinco
// minutos, así que el trabajo acabaría recuperado como huérfano sin haber
// llamado a nadie. Se devuelve el 429 con la espera dentro y la cola aplaza el
// trabajo hasta esa hora.
func (a conCupo) esperar(ctx context.Context) error {
	if falta := a.lim.pausaRestante(); falta > 0 {
		return &channel.Error{
			Kind: a.Adapter.Kind(), StatusCode: http.StatusTooManyRequests,
			Code:       "cuenta_en_pausa",
			Message:    "el canal pidió parar; la cuenta no vuelve a escribir hasta dentro de " + falta.Round(time.Second).String(),
			RetryAfter: falta,
		}
	}
	return a.lim.Esperar(ctx)
}

// frenar apunta la pausa que pidió el canal y devuelve el error tal cual.
//
// Es lo que convierte un 429 de un trabajo en un freno de toda la cuenta: sin
// esto, los otros siete trabajos en vuelo siguen golpeando la misma API que
// acaba de decir «para», que es justo lo que alarga el bloqueo.
func (a conCupo) frenar(err error) error {
	var e *channel.Error
	if errors.As(err, &e) && e.RetryAfter > 0 {
		a.lim.Frenar(e.RetryAfter)
	}
	return err
}

// frenarLote hace lo mismo con los errores que viajan dentro de un lote: los
// canales que responden por elemento meten ahí su 429.
func (a conCupo) frenarLote(res []channel.OpResult, err error) error {
	for _, r := range res {
		_ = a.frenar(r.Error)
	}
	return a.frenar(err)
}

// VeredictoDeFeed reenvía la consulta del feed al canal asíncrono. La
// envoltura no puede tragársela: sin ella el núcleo no tendría a quién
// preguntar por una escritura que quedó sin confirmar.
func (a conCupo) VeredictoDeFeed(ctx context.Context, feedID, sku string) (channel.Veredicto, error) {
	cf, ok := a.Adapter.(channel.ConFeeds)
	if !ok {
		return channel.Veredicto{}, fmt.Errorf("el canal %s no escribe por feeds", a.Adapter.Kind())
	}
	if err := a.esperar(ctx); err != nil {
		return channel.Veredicto{}, err
	}
	v, err := cf.VeredictoDeFeed(ctx, feedID, sku)
	return v, a.frenar(err)
}

func (a conCupo) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.PublishResult{}, err
	}
	res, err := a.Adapter.Publish(ctx, req)
	return res, a.frenar(err)
}

func (a conCupo) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.UpdateResult{}, err
	}
	res, err := a.Adapter.Update(ctx, req)
	return res, a.frenar(err)
}

func (a conCupo) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	res, err := a.Adapter.UpdateStock(ctx, ups)
	return res, a.frenarLote(res, err)
}

func (a conCupo) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	res, err := a.Adapter.UpdatePrice(ctx, ups)
	return res, a.frenarLote(res, err)
}

func (a conCupo) Pause(ctx context.Context, ref channel.ExternalRef) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.frenar(a.Adapter.Pause(ctx, ref))
}

func (a conCupo) Resume(ctx context.Context, ref channel.ExternalRef) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.frenar(a.Adapter.Resume(ctx, ref))
}

func (a conCupo) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	res, err := a.Adapter.FetchStatus(ctx, refs)
	return res, a.frenar(err)
}

func (a conCupo) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.RemotePage{}, err
	}
	res, err := a.Adapter.ListRemote(ctx, cur)
	return res, a.frenar(err)
}

func (a conCupo) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.OrderPage{}, err
	}
	res, err := a.Adapter.FetchOrders(ctx, desde, cur)
	return res, a.frenar(err)
}

func (a conCupo) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.frenar(a.Adapter.AckOrder(ctx, ref, f))
}
