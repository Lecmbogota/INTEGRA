package conectores

import (
	"context"
	"sync"
	"testing"
	"time"
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
