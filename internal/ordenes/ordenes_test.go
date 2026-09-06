package ordenes

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/odoo"
)

// El fallo que estas pruebas vigilan: durante meses el manejador de
// orden_a_odoo estuvo registrado en el worker pero nadie encolaba ese
// trabajo, así que los pedidos ingeridos se quedaban en 'received' y solo
// llegaban a Odoo si alguien ejecutaba `integra ordenes` a mano. Compilaba,
// pasaba las pruebas y el tercer pilar de la plataforma estaba desconectado.

func servicioDePrueba() *Servicio {
	return NuevoServicio(nil, nil, slog.New(slog.NewTextHandler(os.Stderr, nil)),
		func(context.Context) (*odoo.Client, error) { return nil, nil })
}

// TestRegistrarDejaElServicioCapazDeEncolar comprueba el cableado que faltaba:
// sin la cola, ingerir guarda el pedido y nadie lo monta nunca en Odoo.
func TestRegistrarDejaElServicioCapazDeEncolar(t *testing.T) {
	s := servicioDePrueba()
	if s.cola != nil {
		t.Fatal("un servicio recién creado no debería tener cola")
	}

	w := jobs.NuevoWorker(jobs.NuevaCola(nil), slog.Default(), 1, time.Second)
	s.Registrar(w)

	if s.cola == nil {
		t.Error("Registrar debe dejar la cola del worker en el servicio: " +
			"sin ella la ingesta no encola el montaje y los pedidos no llegan a Odoo")
	}
}

// TestRegistrarAtiendeLosDosTrabajos evita el otro lado del mismo fallo:
// encolar un trabajo que nadie maneja.
func TestRegistrarAtiendeLosDosTrabajos(t *testing.T) {
	w := jobs.NuevoWorker(jobs.NuevaCola(nil), slog.Default(), 1, time.Second)
	servicioDePrueba().Registrar(w)

	for _, kind := range []string{TrabajoIngerir, TrabajoAOdoo} {
		if !w.Maneja(kind) {
			t.Errorf("el worker no atiende %q", kind)
		}
	}
}

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
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM jobs WHERE kind = $1 AND payload->>'orden_id' = '999000111'`, TrabajoAOdoo)
		pool.Close()
	})
	return pool
}

// TestEncolarMontajeNoDuplica: dos pasadas del planificador sobre el mismo
// pedido pendiente no pueden crear dos trabajos, o el pedido se montaría dos
// veces en Odoo si la idempotencia de client_order_ref fallara.
func TestEncolarMontajeNoDuplica(t *testing.T) {
	ctx := context.Background()
	cola := jobs.NuevaCola(abrirPool(t))
	const ordenID = 999000111

	if err := EncolarMontaje(ctx, cola, 0, ordenID); err != nil {
		t.Fatal(err)
	}
	if err := EncolarMontaje(ctx, cola, 0, ordenID); err != nil {
		t.Fatal(err)
	}

	var n int
	err := cola.Pool().QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE kind = $1 AND payload->>'orden_id' = $2`,
		TrabajoAOdoo, "999000111").Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("se encolaron %d trabajos para el mismo pedido; la clave única debe dejar 1", n)
	}
}
