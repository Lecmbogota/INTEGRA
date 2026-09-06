package jobs

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Los tests de la cola son de integración: la semántica que importa
// (SKIP LOCKED, índices parciales, backoff en SQL) vive en PostgreSQL y un
// mock solo probaría que el mock funciona. Se saltan sin base configurada.
// prioridadDePrueba pone los trabajos de las pruebas por delante de todo.
//
// Reclamar sirve por prioridad, así que en una base con trabajo real
// pendiente —132 publicaciones encoladas bastaron— el trabajo de la prueba no
// entraba en la tanda reclamada y la prueba fallaba sin que hubiera nada roto.
// Con la prioridad más alta la prueba deja de depender de que la cola esté
// vacía, que es lo único que la hace determinista.
const prioridadDePrueba = 1

func abrirPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("abriendo el pool: %v", err)
	}
	t.Cleanup(func() {
		// Cada test usa tipos con prefijo test_ para no rozar trabajos reales.
		_, _ = pool.Exec(context.Background(), `DELETE FROM jobs WHERE kind LIKE 'test_%'`)
		pool.Close()
	})
	return pool
}

func TestEncolarYReclamar(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	id, err := cola.Encolar(ctx, "test_publicar", map[string]any{"variante": 42}, Opciones{Priority: prioridadDePrueba})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Fatal("el primer encolado no puede devolver 0")
	}

	trabajos, err := cola.Reclamar(ctx, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var mio *Trabajo
	for i := range trabajos {
		if trabajos[i].ID == id {
			mio = &trabajos[i]
		}
	}
	if mio == nil {
		t.Fatalf("el trabajo %d no salió al reclamar", id)
	}
	if mio.Intentos != 1 {
		t.Fatalf("reclamar debe consumir el intento: intentos = %d", mio.Intentos)
	}
	if err := cola.Completar(ctx, id); err != nil {
		t.Fatal(err)
	}

	// Completado no vuelve a salir.
	otra, err := cola.Reclamar(ctx, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range otra {
		if tr.ID == id {
			t.Fatal("un trabajo hecho volvió a salir de la cola")
		}
	}
}

func TestClaveUnicaDeduplica(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	op := Opciones{UniqueKey: fmt.Sprintf("test_dedup_%d", time.Now().UnixNano()), Priority: prioridadDePrueba}
	id1, err := cola.Encolar(ctx, "test_dedup", nil, op)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := cola.Encolar(ctx, "test_dedup", nil, op)
	if err != nil {
		t.Fatal(err)
	}
	if id1 == 0 || id2 != 0 {
		t.Fatalf("la clave única debe deduplicar: id1=%d id2=%d", id1, id2)
	}
}

func TestFallarReintentaConBackoffYAgota(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	id, err := cola.Encolar(ctx, "test_fragil", nil, Opciones{MaxAttempts: 2, Priority: prioridadDePrueba})
	if err != nil {
		t.Fatal(err)
	}

	// Primer intento: falla y debe volver a pending con run_at en el futuro.
	if _, err := cola.Reclamar(ctx, 50, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cola.Fallar(ctx, id, "primer tropiezo", 0); err != nil {
		t.Fatal(err)
	}
	var estado string
	var runAt time.Time
	if err := cola.pool.QueryRow(ctx,
		`SELECT status, run_at FROM jobs WHERE id = $1`, id).Scan(&estado, &runAt); err != nil {
		t.Fatal(err)
	}
	if estado != "pending" {
		t.Fatalf("tras el primer fallo debe quedar pending, quedó %s", estado)
	}
	if !runAt.After(time.Now()) {
		t.Fatal("el reintento debe programarse en el futuro (backoff)")
	}

	// Segundo intento: se agota y queda failed.
	if _, err := cola.pool.Exec(ctx,
		`UPDATE jobs SET run_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := cola.Reclamar(ctx, 50, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cola.Fallar(ctx, id, "segundo tropiezo", 0); err != nil {
		t.Fatal(err)
	}
	if err := cola.pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, id).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "failed" {
		t.Fatalf("con los intentos agotados debe quedar failed, quedó %s", estado)
	}
}

func TestRecuperarHuerfanos(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	id, err := cola.Encolar(ctx, "test_huerfano", nil, Opciones{Priority: prioridadDePrueba})
	if err != nil {
		t.Fatal(err)
	}
	// Lease cortísimo: simula un worker que reclamó y murió.
	if _, err := cola.Reclamar(ctx, 50, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	if _, err := cola.RecuperarHuerfanos(ctx); err != nil {
		t.Fatal(err)
	}
	var estado string
	if err := cola.pool.QueryRow(ctx,
		`SELECT status FROM jobs WHERE id = $1`, id).Scan(&estado); err != nil {
		t.Fatal(err)
	}
	if estado != "pending" {
		t.Fatalf("el huérfano debía volver a pending, quedó %s", estado)
	}
}

// El bloqueo de un canal dura lo que dice el canal: reintentar a los 30 s
// mientras MercadoLibre nos tiene parados una hora no publica nada, alarga el
// bloqueo y gasta los cinco intentos en ocho minutos, de modo que el catálogo
// entero acaba en failed justo cuando la API vuelve a aceptar escrituras.
func TestElTrabajoFrenadoPorElCanalNoVuelveAntesDeLaHoraQuePidio(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	id, err := cola.Encolar(ctx, "test_cupo", nil, Opciones{Priority: prioridadDePrueba})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cola.Reclamar(ctx, 50, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cola.Fallar(ctx, id, "429: llamadas por segundo excedidas", time.Hour); err != nil {
		t.Fatal(err)
	}

	var runAt time.Time
	if err := cola.pool.QueryRow(ctx,
		`SELECT run_at FROM jobs WHERE id = $1`, id).Scan(&runAt); err != nil {
		t.Fatal(err)
	}
	// Con el backoff solo, el primer reintento caería a los 30 s.
	if falta := time.Until(runAt); falta < 55*time.Minute {
		t.Fatalf("el canal pidió una hora y el reintento se programó dentro de %v", falta.Round(time.Second))
	}
}

// La espera del canal es un mínimo, no un sustituto: si el backoff ya es más
// largo (intentos avanzados), manda el backoff.
func TestLaEsperaDelCanalNoAcortaElBackoff(t *testing.T) {
	ctx := context.Background()
	cola := NuevaCola(abrirPool(t))

	id, err := cola.Encolar(ctx, "test_cupo_corto", nil,
		Opciones{Priority: prioridadDePrueba, MaxAttempts: 10})
	if err != nil {
		t.Fatal(err)
	}
	// Se simulan cuatro intentos gastados: el backoff toca a ocho minutos.
	if _, err := cola.pool.Exec(ctx, `UPDATE jobs SET attempts = 4 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := cola.Reclamar(ctx, 50, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cola.Fallar(ctx, id, "429 sin plazo", 2*time.Second); err != nil {
		t.Fatal(err)
	}

	var runAt time.Time
	if err := cola.pool.QueryRow(ctx,
		`SELECT run_at FROM jobs WHERE id = $1`, id).Scan(&runAt); err != nil {
		t.Fatal(err)
	}
	if falta := time.Until(runAt); falta < 4*time.Minute {
		t.Fatalf("el backoff del quinto intento son ocho minutos y quedó en %v", falta.Round(time.Second))
	}
}

// El plazo que pide el canal se lee del error sin que la cola sepa qué es un
// canal: cualquiera que sepa decir cuánto esperar sirve.
func TestLaEsperaSeLeeDelErrorDelCanal(t *testing.T) {
	if d := EsperaPedida(errorConEspera{30 * time.Minute}); d != 30*time.Minute {
		t.Fatalf("no se leyó la espera del error: %v", d)
	}
	if d := EsperaPedida(fmt.Errorf("un fallo cualquiera")); d != 0 {
		t.Fatalf("un error sin plazo no pide esperar nada: %v", d)
	}
	// Envuelto también: los manejadores devuelven el error del canal con
	// contexto añadido.
	envuelto := fmt.Errorf("publicando la variante 42: %w", errorConEspera{time.Minute})
	if d := EsperaPedida(envuelto); d != time.Minute {
		t.Fatalf("la espera tiene que sobrevivir al envoltorio: %v", d)
	}
}

type errorConEspera struct{ d time.Duration }

func (e errorConEspera) Error() string                          { return "el canal pidió parar" }
func (e errorConEspera) EsperaAntesDeReintentar() time.Duration { return e.d }
