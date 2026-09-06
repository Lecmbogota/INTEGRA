package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/store"
)

// El worker que lleva dos días muerto.
//
// El escenario que esto cubre es el del fin de semana largo: el contenedor del
// worker se cae el viernes y con él se va el planificador, que es lo único que
// levanta alertas y manda correos. Nadie se entera hasta el lunes, y para
// entonces hay pedidos de cuatro canales sin sale.order y stock que se siguió
// vendiendo congelado. La prueba comprueba que el aviso sale de la API, que es
// el proceso que sigue vivo.

type latidosFalsos struct {
	atrasados []store.Latido
	alertas   []string
	severidad []string
	avisado   int
}

func (l *latidosFalsos) LatidosAtrasados(context.Context) ([]store.Latido, error) {
	return l.atrasados, nil
}

func (l *latidosFalsos) CrearAlerta(_ context.Context, tipo, severidad string, _ *int64, mensaje string, _ any) error {
	if tipo != AlertaProcesoMudo {
		return errors.New("tipo de alerta inesperado: " + tipo)
	}
	l.alertas = append(l.alertas, mensaje)
	l.severidad = append(l.severidad, severidad)
	return nil
}

func mudo(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestElWorkerCaidoLevantaAlertaYSacaElCorreoDesdeLaAPI(t *testing.T) {
	falso := &latidosFalsos{atrasados: []store.Latido{{
		Componente: store.ComponenteWorker,
		Visto:      time.Now().Add(-48 * time.Hour),
		Periodo:    time.Minute,
	}}}
	notificar := func(context.Context) error { falso.avisado++; return nil }

	if err := vigilarProcesos(context.Background(), falso, mudo(t), notificar); err != nil {
		t.Fatal(err)
	}

	if len(falso.alertas) != 1 {
		t.Fatalf("un worker de dos días muerto tiene que levantar una alerta, se levantaron %d", len(falso.alertas))
	}
	if falso.severidad[0] != "critical" {
		t.Errorf("que no entre ni un pedido no es un aviso menor: %q", falso.severidad[0])
	}
	// El mensaje tiene que decir qué se está perdiendo, no solo que un proceso
	// técnico no responde: lo lee el dueño del negocio.
	if !strings.Contains(falso.alertas[0], "no entra ningún pedido") {
		t.Errorf("el mensaje no dice qué deja de pasar: %q", falso.alertas[0])
	}
	if falso.avisado != 1 {
		t.Errorf("el correo tiene que salir de la API, que es la que sigue viva; salidas: %d", falso.avisado)
	}
}

// La alerta se deduplica por texto mientras nadie la reconozca: si el mensaje
// llevara el tiempo transcurrido, dos días caído serían dos mil alertas y dos
// mil correos, que es otra forma de que nadie se entere.
func TestElMensajeDelProcesoMudoNoCambiaEntrePasadas(t *testing.T) {
	latido := store.Latido{Componente: store.ComponenteWorker, Periodo: time.Minute}
	falso := &latidosFalsos{}

	latido.Visto = time.Now().Add(-10 * time.Minute)
	falso.atrasados = []store.Latido{latido}
	if err := vigilarProcesos(context.Background(), falso, mudo(t), nil); err != nil {
		t.Fatal(err)
	}
	latido.Visto = time.Now().Add(-9 * time.Hour)
	falso.atrasados = []store.Latido{latido}
	if err := vigilarProcesos(context.Background(), falso, mudo(t), nil); err != nil {
		t.Fatal(err)
	}

	if falso.alertas[0] != falso.alertas[1] {
		t.Errorf("el mensaje cambió con el tiempo transcurrido:\n  %q\n  %q", falso.alertas[0], falso.alertas[1])
	}
}

// Con todos los procesos latiendo no se molesta a nadie.
func TestSinProcesosAtrasadosNoSaleNingunAviso(t *testing.T) {
	falso := &latidosFalsos{}
	notificar := func(context.Context) error { falso.avisado++; return nil }

	if err := vigilarProcesos(context.Background(), falso, mudo(t), notificar); err != nil {
		t.Fatal(err)
	}
	if len(falso.alertas) != 0 || falso.avisado != 0 {
		t.Errorf("un sistema sano no genera avisos: %d alertas, %d envíos", len(falso.alertas), falso.avisado)
	}
}

// El plazo va en el correo: "72h0m0s" no le dice nada a quien lo lee.
func TestElPlazoDelAvisoSeLeeEnCastellano(t *testing.T) {
	casos := map[time.Duration]string{
		3 * time.Minute:    "3 minutos",
		90 * time.Minute:   "90 minutos",
		6 * time.Hour:      "6 horas",
		72 * time.Hour:     "3 días",
		7 * 24 * time.Hour: "7 días",
	}
	for d, quiero := range casos {
		if got := plazo(d); got != quiero {
			t.Errorf("plazo(%s) = %q, se esperaba %q", d, got, quiero)
		}
	}
}

// ------------------------------------------------ la ruta que mira el uptime

// La sonda externa es la mitad que no depende de Integra: si el aviso por
// correo lo tuviera que mandar el proceso muerto, no saldría nunca. Un
// servicio de uptime pide esta ruta y le basta el código HTTP.
//
// Es de integración porque lo que se prueba es justo la frontera: que la ruta
// esté abierta sin sesión y que el 503 salga del estado real de la base.
func TestLaRutaDeEstadoDevuelve503CuandoElWorkerDejoDeLatir(t *testing.T) {
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("abriendo el store: %v", err)
	}
	t.Cleanup(st.Close)

	clave, err := crypto.GenerarClave()
	if err != nil {
		t.Fatal(err)
	}
	cif, err := crypto.DesdeBase64(clave)
	if err != nil {
		t.Fatal(err)
	}
	srv := Nuevo(st, mudo(t), ":0", nil, cif, jobs.NuevaCola(st.Pool()), nil)

	// La sonda no tiene sesión: se pide por el mismo camino que la usaría el
	// servicio de uptime, con el middleware puesto.
	pedir := func() int {
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, httptest.NewRequest("GET", "/estado", nil))
		return rec.Code
	}

	// La tabla de latidos es estado del despliegue, no del test: se pone al
	// día para partir de un sistema sano y se devuelve como estaba al acabar.
	restaurar := congelarLatidos(t, st)
	t.Cleanup(restaurar)

	if err := st.RegistrarLatido(ctx, store.ComponenteWorker, time.Minute, "prueba"); err != nil {
		t.Fatal(err)
	}
	if c := pedir(); c != http.StatusOK {
		t.Fatalf("con todo latiendo la sonda tiene que dar 200, dio %d", c)
	}

	envejecerLatido(t, st, store.ComponenteWorker, 2*time.Hour)
	if c := pedir(); c != http.StatusServiceUnavailable {
		t.Errorf("con el worker muerto la sonda tiene que dar 503, dio %d", c)
	}
}

// congelarLatidos pone todos los latidos al día y devuelve la función que
// restituye las fechas que había.
func congelarLatidos(t *testing.T, st *store.Store) func() {
	t.Helper()
	ctx := context.Background()
	filas, err := st.Pool().Query(ctx, `SELECT component, beat_at FROM process_heartbeats`)
	if err != nil {
		t.Fatal(err)
	}
	previos := map[string]time.Time{}
	for filas.Next() {
		var c string
		var v time.Time
		if err := filas.Scan(&c, &v); err != nil {
			filas.Close()
			t.Fatal(err)
		}
		previos[c] = v
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		t.Fatal(err)
	}

	if _, err := st.Pool().Exec(ctx, `UPDATE process_heartbeats SET beat_at = now()`); err != nil {
		t.Fatal(err)
	}
	return func() {
		for c, v := range previos {
			_, _ = st.Pool().Exec(context.Background(),
				`UPDATE process_heartbeats SET beat_at = $2 WHERE component = $1`, c, v)
		}
	}
}

func envejecerLatido(t *testing.T, st *store.Store, componente string, edad time.Duration) {
	t.Helper()
	_, err := st.Pool().Exec(context.Background(), `
		UPDATE process_heartbeats SET beat_at = now() - make_interval(secs => $2)
		WHERE component = $1`, componente, int(edad.Seconds()))
	if err != nil {
		t.Fatal(err)
	}
}
