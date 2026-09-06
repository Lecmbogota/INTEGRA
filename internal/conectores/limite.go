package conectores

import (
	"context"
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

func (a conCupo) esperar(ctx context.Context) error { return a.lim.Esperar(ctx) }

func (a conCupo) Publish(ctx context.Context, req channel.PublishRequest) (channel.PublishResult, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.PublishResult{}, err
	}
	return a.Adapter.Publish(ctx, req)
}

func (a conCupo) Update(ctx context.Context, req channel.UpdateRequest) (channel.UpdateResult, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.UpdateResult{}, err
	}
	return a.Adapter.Update(ctx, req)
}

func (a conCupo) UpdateStock(ctx context.Context, ups []channel.StockUpdate) ([]channel.OpResult, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	return a.Adapter.UpdateStock(ctx, ups)
}

func (a conCupo) UpdatePrice(ctx context.Context, ups []channel.PriceUpdate) ([]channel.OpResult, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	return a.Adapter.UpdatePrice(ctx, ups)
}

func (a conCupo) Pause(ctx context.Context, ref channel.ExternalRef) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.Adapter.Pause(ctx, ref)
}

func (a conCupo) Resume(ctx context.Context, ref channel.ExternalRef) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.Adapter.Resume(ctx, ref)
}

func (a conCupo) FetchStatus(ctx context.Context, refs []channel.ExternalRef) ([]channel.ListingStatus, error) {
	if err := a.esperar(ctx); err != nil {
		return nil, err
	}
	return a.Adapter.FetchStatus(ctx, refs)
}

func (a conCupo) ListRemote(ctx context.Context, cur channel.Cursor) (channel.RemotePage, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.RemotePage{}, err
	}
	return a.Adapter.ListRemote(ctx, cur)
}

func (a conCupo) FetchOrders(ctx context.Context, desde time.Time, cur channel.Cursor) (channel.OrderPage, error) {
	if err := a.esperar(ctx); err != nil {
		return channel.OrderPage{}, err
	}
	return a.Adapter.FetchOrders(ctx, desde, cur)
}

func (a conCupo) AckOrder(ctx context.Context, ref channel.ExternalRef, f channel.Fulfillment) error {
	if err := a.esperar(ctx); err != nil {
		return err
	}
	return a.Adapter.AckOrder(ctx, ref, f)
}
