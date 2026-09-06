package store

import (
	"context"
	"testing"
	"time"
)

// El worker que se murió el viernes y nadie lo notó hasta el lunes.
//
// La cuenta de si un proceso dejó de latir la hace PostgreSQL con su propio
// reloj, así que se prueba contra la base: un cálculo en Go solo demostraría
// que el Go está bien y dejaría sin cubrir lo único que puede fallar aquí,
// que es la aritmética de intervalos de la consulta.
func TestUnWorkerQueDejaDeLatirSeDaPorMuerto(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	const componente = "worker-latido-test"
	t.Cleanup(func() {
		_, _ = st.pool.Exec(context.Background(),
			`DELETE FROM process_heartbeats WHERE component = $1`, componente)
	})

	if err := st.RegistrarLatido(ctx, componente, time.Minute, "recién arrancado"); err != nil {
		t.Fatal(err)
	}

	// Recién latido: nadie tiene que alarmarse.
	l := buscarLatido(t, st, componente)
	if l.Atrasado {
		t.Fatal("un proceso que acaba de latir no puede darse por muerto")
	}
	if l.Detalle != "recién arrancado" || l.Periodo != time.Minute {
		t.Fatalf("el latido no se guardó entero: %+v", l)
	}

	// Dos periodos: sigue dentro de la tolerancia, porque perder un latido en
	// un reinicio es normal y avisar por eso entrena a ignorar los avisos.
	envejecer(t, st, componente, 2*time.Minute)
	if buscarLatido(t, st, componente).Atrasado {
		t.Error("dos minutos sin latir todavía no es un worker caído")
	}

	// Cuatro periodos: ya no hay excusa.
	envejecer(t, st, componente, 4*time.Minute)
	if !buscarLatido(t, st, componente).Atrasado {
		t.Error("cuatro minutos sin latir tienen que salir como atrasado")
	}

	atrasados, err := st.LatidosAtrasados(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var visto bool
	for _, a := range atrasados {
		if a.Componente == componente {
			visto = true
		}
	}
	if !visto {
		t.Error("el proceso muerto no aparece en la lista que mira la vigilancia")
	}
}

// envejecer retrasa el último latido en la base, que es la única forma de
// probar el paso del tiempo sin dormir en el test.
func envejecer(t *testing.T, st *Store, componente string, edad time.Duration) {
	t.Helper()
	_, err := st.pool.Exec(context.Background(), `
		UPDATE process_heartbeats
		SET beat_at = now() - make_interval(secs => $2)
		WHERE component = $1`, componente, int(edad.Seconds()))
	if err != nil {
		t.Fatal(err)
	}
}

func buscarLatido(t *testing.T, st *Store, componente string) Latido {
	t.Helper()
	todos, err := st.Latidos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range todos {
		if l.Componente == componente {
			return l
		}
	}
	t.Fatalf("no se guardó el latido de %s", componente)
	return Latido{}
}
