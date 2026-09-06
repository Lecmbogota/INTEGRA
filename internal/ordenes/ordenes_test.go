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
	"github.com/mdv/integra/internal/store"
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

// El sale.order se creaba sin warehouse_id, así que Odoo lo despachaba
// siempre de la bodega por defecto: una venta de Falabella descontaba de la
// bodega principal mientras el stock consignado en las bodegas FB seguía
// intacto. channel_account_warehouses existía desde el primer esquema y no la
// leía nadie en el camino del pedido.
func TestValoresPedidoLlevaLaBodegaDeLaCuenta(t *testing.T) {
	o := store.Orden{Canal: "falabella", Numero: "FB-1",
		FechaPedido: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)}

	valores := valoresPedido(o, 42, "FALABELLA-FB-1", nil, 7)

	if valores["warehouse_id"] != int64(7) {
		t.Errorf("el pedido debe salir de la bodega de la cuenta, warehouse_id = %v",
			valores["warehouse_id"])
	}
	if valores["partner_id"] != int64(42) {
		t.Errorf("partner_id = %v", valores["partner_id"])
	}
	if valores["client_order_ref"] != "FALABELLA-FB-1" {
		t.Errorf("client_order_ref = %v", valores["client_order_ref"])
	}
}

// Una cuenta sin bodegas asignadas tiene que seguir dejando que decida Odoo:
// mandar warehouse_id 0 crearía el pedido contra una bodega inexistente, que
// es peor que el comportamiento de hoy.
func TestValoresPedidoSinBodegaNoMandaLaClave(t *testing.T) {
	valores := valoresPedido(store.Orden{Canal: "shopify"}, 1, "SHOPIFY-1", nil, 0)

	if _, hay := valores["warehouse_id"]; hay {
		t.Error("sin bodegas asignadas no se debe mandar warehouse_id: decide Odoo")
	}
}

// Ningún canal normaliza el estado del pedido: cada uno manda su vocabulario
// tal cual. Sin este mapeo, una cancelación pasaba por venta viva, el stock
// apartado no volvía nunca y el pedido se montaba igual en Odoo.
func TestEsCancelado(t *testing.T) {
	casos := []struct {
		estado    string
		cancelado bool
		porque    string
	}{
		{"cancelled", true, "MercadoLibre, WooCommerce y Shopify"},
		{"canceled", true, "la variante con una sola l"},
		{"CANCELLED", true, "el canal puede mandarlo en mayúsculas"},
		{" cancelled ", true, "con espacios alrededor"},
		{"invalid", true, "MercadoLibre marca así el pedido fraudulento"},
		{"refunded", true, "WooCommerce y el financial_status de Shopify"},
		{"voided", true, "Shopify: se anuló sin cobrar"},
		{"paid", false, "una venta viva"},
		{"delivered", false, "entregado no es cancelado"},
		{"failed", false, "pago rechazado que el comprador reintenta"},
		{"partially_refunded", false, "devolvió dinero, no la mercancía entera"},
		{"", false, "Falabella no reporta estado: no se puede suponer"},
	}
	for _, c := range casos {
		if got := esCancelado(c.estado); got != c.cancelado {
			t.Errorf("esCancelado(%q) = %v, se esperaba %v: %s",
				c.estado, got, c.cancelado, c.porque)
		}
	}
}
