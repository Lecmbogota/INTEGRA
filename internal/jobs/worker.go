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
		lease:     5 * time.Minute,
		manejores: map[string]Handler{},
	}
}

// Registrar asocia un tipo de trabajo con su manejador. Los conectores de
// canal se registran aquí desde sus paquetes.
func (w *Worker) Registrar(kind string, h Handler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.manejores[kind] = h
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

	var enVuelo sync.WaitGroup
	timer := time.NewTicker(w.tick)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker apagándose: esperando los trabajos en vuelo")
			enVuelo.Wait()
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
		trabajos, err := w.cola.Reclamar(ctx, libres, w.lease)
		if err != nil {
			w.log.Error("reclamando trabajos", "error", err)
			continue
		}

		for _, t := range trabajos {
			<-huecos
			enVuelo.Add(1)
			go func(t Trabajo) {
				defer func() { huecos <- struct{}{}; enVuelo.Done() }()
				w.procesar(ctx, t)
			}(t)
		}
	}
}

func (w *Worker) procesar(ctx context.Context, t Trabajo) {
	inicio := time.Now()

	h, ok := w.manejador(t.Kind)
	if !ok {
		// Sin manejador no tiene sentido reintentar: fallaría igual siempre.
		// Se anota claro para que se vea en el panel.
		_ = w.cola.Fallar(ctx, t.ID, fmt.Sprintf("no hay manejador registrado para %q", t.Kind))
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
		if fe := w.cola.Fallar(ctx, t.ID, err.Error()); fe != nil {
			w.log.Error("no se pudo registrar el fallo", "id", t.ID, "error", fe)
		}
		w.log.Warn("trabajo fallido", "kind", t.Kind, "id", t.ID,
			"intento", t.Intentos, "de", t.MaxIntentos, "error", err)
		return
	}

	if ce := w.cola.Completar(ctx, t.ID); ce != nil {
		w.log.Error("no se pudo completar el trabajo", "id", t.ID, "error", ce)
		return
	}
	w.log.Debug("trabajo hecho", "kind", t.Kind, "id", t.ID,
		"ms", time.Since(inicio).Milliseconds())
}
