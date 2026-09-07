package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

// Handler procesa un trabajo. Devolver error lo manda a reintento con
// backoff; devolver nil lo completa.
type Handler func(ctx context.Context, t Trabajo) error

// Worker reclama trabajos de la cola y los ejecuta con concurrencia acotada.
type Worker struct {
	cola         *Cola
	log          *slog.Logger
	concurrencia int
	tick         time.Duration
	lease        time.Duration
	// gracia es cuánto se espera al apagar a que terminen los trabajos que ya
	// están hablando con un canal.
	gracia time.Duration

	mu        sync.Mutex
	manejores map[string]Handler
}

func NuevoWorker(cola *Cola, log *slog.Logger, concurrencia int, tick time.Duration) *Worker {
	if concurrencia < 1 {
		concurrencia = 1
	}
	if tick < time.Second {
		tick = time.Second
	}
	return &Worker{
		cola: cola, log: log, concurrencia: concurrencia, tick: tick,
		// El lease debe superar al trabajo más largo esperable; cinco minutos
		// cubre de sobra una llamada a un canal con reintentos de red.
		lease: 5 * time.Minute,
		// Medio minuto: suficiente para que termine una llamada a un canal
		// con su reintento, y poco para no dejar el contenedor colgado en un
		// despliegue. Docker manda SIGKILL a los 10 s por defecto, así que el
		// compose necesita stop_grace_period acorde.
		gracia:    30 * time.Second,
		manejores: map[string]Handler{},
	}
}

// Registrar asocia un tipo de trabajo con su manejador. Los conectores de
// canal se registran aquí desde sus paquetes.
// Cola devuelve la cola sobre la que trabaja este worker, para que un
// servicio registrado pueda encolar trabajos derivados (ingerir un pedido
// encola su montaje en Odoo) sin que haya que pasarle la cola por separado.
func (w *Worker) Cola() *Cola { return w.cola }

// tipos son los que este worker tiene registrados.
func (w *Worker) tipos() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, 0, len(w.manejores))
	for k := range w.manejores {
		out = append(out, k)
	}
	return out
}

func (w *Worker) Registrar(kind string, h Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.manejores[kind] = h
}

// Maneja indica si hay un manejador registrado para ese tipo de trabajo.
// Encolar un tipo que nadie atiende deja el trabajo en la cola para siempre.
func (w *Worker) Maneja(kind string) bool {
	_, ok := w.manejador(kind)
	return ok
}

func (w *Worker) manejador(kind string) (Handler, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	h, ok := w.manejores[kind]
	return h, ok
}

// Ejecutar procesa la cola hasta que el contexto se cancele.
//
// El bucle es deliberadamente simple: cada tick recupera huérfanos y reclama
// tantos trabajos como huecos libres haya. La justicia entre tipos de trabajo
// la da la prioridad en la cola, no el worker.
func (w *Worker) Ejecutar(ctx context.Context) error {
	w.log.Info("worker en marcha", "concurrencia", w.concurrencia, "tick", w.tick.String())

	huecos := make(chan struct{}, w.concurrencia)
	for i := 0; i < w.concurrencia; i++ {
		huecos <- struct{}{}
	}

	// Los trabajos corren con un contexto propio, desligado del que apaga el
	// worker. Antes recibían el mismo: al pedir el apagado, la llamada al
	// canal que estuviera a medias moría en el acto y —peor— tampoco se podía
	// anotar el resultado en la base, porque esa escritura usaba el mismo
	// contexto muerto. El trabajo se quedaba en 'running' hasta que caducaba
	// su lease, bloqueado sin que nadie lo estuviera procesando.
	ctxTrabajos, cancelarTrabajos := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelarTrabajos()

	var enVuelo sync.WaitGroup
	timer := time.NewTicker(w.tick)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			w.esperarEnVuelo(&enVuelo, cancelarTrabajos)
			return nil
		case <-timer.C:
		}

		if n, err := w.cola.RecuperarHuerfanos(ctx); err != nil {
			w.log.Error("recuperando huérfanos", "error", err)
		} else if n > 0 {
			w.log.Warn("trabajos huérfanos devueltos a la cola", "cantidad", n)
		}

		// Solo se reclama lo que se puede atender ya: reclamar de más dejaría
		// trabajos bloqueados por su lease sin nadie procesándolos.
		libres := len(huecos)
		if libres == 0 {
			continue
		}
		// Solo los tipos que este worker sabe atender: reclamar los demás les
		// gastaría un intento y los daría por fallidos sin haberlos intentado.
		trabajos, err := w.cola.Reclamar(ctx, libres, w.lease, w.tipos()...)
		if err != nil {
			w.log.Error("reclamando trabajos", "error", err)
			continue
		}

		for _, t := range trabajos {
			<-huecos
			enVuelo.Add(1)
			go func(t Trabajo) {
				defer func() { huecos <- struct{}{}; enVuelo.Done() }()
				w.procesar(ctxTrabajos, t)
			}(t)
		}
	}
}

// esperarEnVuelo da un margen a lo que ya está a medias antes de cortarlo.
//
// Un envío al canal que se corta a la mitad es lo peor que puede pasar aquí:
// no se sabe si llegó, y el reintento puede duplicar una publicación. Por eso
// se espera; pero el margen es finito, porque un canal colgado no puede
// impedir para siempre que el proceso termine. Lo que no acabe a tiempo se
// cancela y vuelve a la cola por el camino de los huérfanos.
func (w *Worker) esperarEnVuelo(enVuelo *sync.WaitGroup, cancelar context.CancelFunc) {
	hecho := make(chan struct{})
	go func() { enVuelo.Wait(); close(hecho) }()

	select {
	case <-hecho:
		w.log.Info("worker apagado: los trabajos en vuelo terminaron")
	case <-time.After(w.gracia):
		w.log.Warn("se agotó el margen de apagado: los trabajos en vuelo se cancelan y volverán a la cola",
			"margen", w.gracia.String())
		cancelar()
		<-hecho
	}
}

func (w *Worker) procesar(ctx context.Context, t Trabajo) {
	inicio := time.Now()

	h, ok := w.manejador(t.Kind)
	if !ok {
		// Sin manejador no tiene sentido reintentar: fallaría igual siempre.
		// Se anota claro para que se vea en el panel.
		_ = w.cola.Fallar(ctx, t.ID, fmt.Sprintf("no hay manejador registrado para %q", t.Kind), 0)
		w.log.Error("trabajo sin manejador", "kind", t.Kind, "id", t.ID)
		return
	}

	err := func() (err error) {
		// Un pánico en un manejador no puede tumbar el worker entero: se
		// convierte en fallo del trabajo, con su traza en el error.
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("pánico: %v\n%s", r, debug.Stack())
			}
		}()
		return h(ctx, t)
	}()

	if err != nil {
		// Si el canal dijo cuándo volver, manda su plazo sobre el backoff: es
		// el único que sabe cuánto dura su propio bloqueo.
		espera := EsperaPedida(err)
		if fe := w.cola.Fallar(ctx, t.ID, err.Error(), espera); fe != nil {
			w.log.Error("no se pudo registrar el fallo", "id", t.ID, "error", fe)
		}
		w.log.Warn("trabajo fallido", "kind", t.Kind, "id", t.ID,
			"intento", t.Intentos, "de", t.MaxIntentos, "espera", espera.String(), "error", err)
		return
	}

	if ce := w.cola.Completar(ctx, t.ID); ce != nil {
		w.log.Error("no se pudo completar el trabajo", "id", t.ID, "error", ce)
		return
	}
	w.log.Debug("trabajo hecho", "kind", t.Kind, "id", t.ID,
		"ms", time.Since(inicio).Milliseconds())
}
