package jobs

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// Al apagar el worker, los trabajos en vuelo recibían el mismo contexto que se
// acababa de cancelar: la llamada al canal que estuviera a medias moría en el
// acto y —peor— tampoco se podía anotar el resultado, porque esa escritura
// usaba el mismo contexto muerto. El trabajo quedaba en 'running' hasta que
// caducaba su lease, bloqueado sin que nadie lo procesara.

func TestElTrabajoEnVueloTerminaAunqueSeApagueElWorker(t *testing.T) {
	cola := NuevaCola(abrirPool(t))
	w := NuevoWorker(cola, slog.New(slog.DiscardHandler), 1, time.Second)

	empezado := make(chan struct{})
	var mu sync.Mutex
	var contextoVivo bool
	var termino bool

	w.Registrar("test_apagado", func(ctx context.Context, _ Trabajo) error {
		close(empezado)
		// Simula una llamada al canal en curso mientras llega el apagado.
		time.Sleep(300 * time.Millisecond)
		mu.Lock()
		contextoVivo = ctx.Err() == nil
		termino = true
		mu.Unlock()
		return nil
	})

	ctx, apagar := context.WithCancel(context.Background())
	id, err := cola.Encolar(ctx, "test_apagado", map[string]any{}, Opciones{Priority: prioridadDePrueba})
	if err != nil {
		t.Fatal(err)
	}

	fin := make(chan error, 1)
	go func() { fin <- w.Ejecutar(ctx) }()

	select {
	case <-empezado:
	case <-time.After(10 * time.Second):
		t.Fatal("el trabajo no llegó a empezar")
	}
	apagar() // el apagado llega con el trabajo a medias

	select {
	case err := <-fin:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("el worker no terminó de apagarse")
	}

	mu.Lock()
	defer mu.Unlock()
	if !termino {
		t.Error("el worker no esperó a que el trabajo terminara")
	}
	if !contextoVivo {
		t.Error("el trabajo recibió un contexto ya cancelado: su llamada al canal moriría a medias " +
			"y no podría anotar el resultado")
	}

	// Y como pudo anotarlo, el trabajo no queda bloqueado en 'running'.
	var estado string
	if err := cola.Pool().QueryRow(context.Background(),
		`SELECT status::text FROM jobs WHERE id = $1`, id).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado == "running" {
		t.Error("el trabajo quedó en 'running': nadie lo procesa y bloquea hasta que caduque su lease")
	}
}

func TestElMargenDeApagadoNoEsInfinito(t *testing.T) {
	cola := NuevaCola(abrirPool(t))
	w := NuevoWorker(cola, slog.New(slog.DiscardHandler), 1, time.Second)
	// Un canal colgado no puede impedir para siempre que el proceso termine.
	w.gracia = 200 * time.Millisecond

	empezado := make(chan struct{})
	w.Registrar("test_apagado_colgado", func(ctx context.Context, _ Trabajo) error {
		close(empezado)
		<-ctx.Done() // se queda esperando hasta que lo cancelen
		return ctx.Err()
	})

	ctx, apagar := context.WithCancel(context.Background())
	if _, err := cola.Encolar(ctx, "test_apagado_colgado", map[string]any{}, Opciones{Priority: prioridadDePrueba}); err != nil {
		t.Fatal(err)
	}

	fin := make(chan error, 1)
	go func() { fin <- w.Ejecutar(ctx) }()

	select {
	case <-empezado:
	case <-time.After(10 * time.Second):
		t.Fatal("el trabajo no llegó a empezar")
	}
	apagar()

	select {
	case <-fin:
	case <-time.After(5 * time.Second):
		t.Fatal("el worker se quedó colgado esperando un trabajo que no termina")
	}
}
