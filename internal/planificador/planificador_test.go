package planificador

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mdv/integra/internal/jobs"
	"github.com/mdv/integra/internal/publicar"
	"github.com/mdv/integra/internal/store"
)

// El horario periódico llamaba a publicar.Planificar sin rehacer los precios
// efectivos, así que un override o una regla que se quedaron sin recálculo —el
// de la petición falló, o los guardó el código anterior— no salían nunca hacia
// el canal: el motor comparaba el hash contra el precio viejo y no encolaba
// nada. Esta prueba corre el horario de verdad contra el esquema real. Se salta
// sin INTEGRA_DATABASE_URL.
//
// El recálculo recorre todas las variantes del catálogo, también las que otro
// paquete de pruebas está creando y borrando en ese instante sobre la misma
// base: si una desaparece entre el SELECT y el UPSERT, falla la clave foránea.
// Con base, la suite se corre en serie (go test -p 1).

func abrir(t *testing.T) (*store.Store, *jobs.Cola, context.Context) {
	t.Helper()
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("abriendo la base: %v", err)
	}
	t.Cleanup(st.Close)
	return st, jobs.NuevaCola(st.Pool()), ctx
}

// catalogoPublicado deja una cuenta global con una variante ya publicada a su
// precio calculado y los tres hashes al día: lo único que puede mover algo en
// la siguiente planificación es el precio.
func catalogoPublicado(t *testing.T, st *store.Store, ctx context.Context) (cuentaID, varianteID int64) {
	t.Helper()
	pool := st.Pool()

	// ListarCuentas solo devuelve cuentas globales (sin marca) y activas, y el
	// índice parcial admite una sola por canal: se usa un canal que no tenga.
	var canalID int64
	if err := pool.QueryRow(ctx, `
		SELECT ch.id FROM channels ch
		WHERE NOT EXISTS (SELECT 1 FROM channel_accounts a
		                  WHERE a.channel_id = ch.id AND a.brand_id IS NULL AND a.active)
		ORDER BY ch.id LIMIT 1`).Scan(&canalID); err != nil {
		t.Skipf("los cuatro canales ya tienen cuenta global activa; no hay sitio para una de prueba: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc)
		VALUES (NULL, $1, 'cuenta-horario-test', '\x00'::bytea) RETURNING id`, canalID).Scan(&cuentaID); err != nil {
		t.Fatalf("creando la cuenta de prueba: %v", err)
	}
	// La URL es propia de esta prueba: (base_url, database) es único y los
	// paquetes de pruebas corren en paralelo contra la misma base.
	var conexionID, productoID int64
	_, _ = pool.Exec(ctx, `DELETE FROM odoo_connections WHERE base_url = 'http://horario-test'`)
	if err := pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-horario-test', 'http://horario-test', 'x', 'x', '\x00'::bytea, false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatalf("creando la conexión de prueba: %v", err)
	}

	// La cuenta necesita al menos una bodega asignada: desde que se exige la
	// asignación, una cuenta sin bodegas no publica nada y esta prueba dejaría
	// de medir lo suyo para medir eso otro.
	var bodegaID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO odoo_warehouses (odoo_connection_id, odoo_id, code, name)
		VALUES ($1, 779901, 'HORAR', 'Bodega de la prueba de horario') RETURNING id`,
		conexionID).Scan(&bodegaID); err != nil {
		t.Fatalf("creando la bodega de prueba: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO channel_account_warehouses (channel_account_id, odoo_warehouse_id)
		VALUES ($1, $2)`, cuentaID, bodegaID); err != nil {
		t.Fatalf("asignando la bodega a la cuenta: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name, description_sale)
		VALUES ($1, 777201, 'Producto publicado', 'Una descripción.') RETURNING id`, conexionID).Scan(&productoID); err != nil {
		t.Fatalf("creando el producto de prueba: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku, price)
		VALUES ($1, 777201, 'HORARIO-TEST', 100000) RETURNING id`, productoID).Scan(&varianteID); err != nil {
		t.Fatalf("creando la variante de prueba: %v", err)
	}
	// Sin una foto publicable la variante no está lista y el motor la salta.
	const sha = "sha-horario-test"
	if _, err := pool.Exec(ctx, `
		INSERT INTO imagenes (sha256, ruta, formato, ancho, alto, bytes)
		VALUES ($1, 'x', 'jpeg', 1000, 1000, 1)
		ON CONFLICT (sha256) DO NOTHING`, sha); err != nil {
		t.Fatalf("creando la imagen de prueba: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO producto_imagenes (product_id, imagen_id, principal)
		SELECT $1, id, TRUE FROM imagenes WHERE sha256 = $2`, productoID, sha); err != nil {
		t.Fatalf("enlazando la imagen: %v", err)
	}
	t.Cleanup(func() {
		// La cuenta arrastra en cascada precios efectivos, overrides,
		// publicaciones y trabajos; la conexión, el producto y la variante.
		_, _ = pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
		_, _ = pool.Exec(ctx, `DELETE FROM imagenes WHERE sha256 = $1`, sha)
	})

	if _, err := st.RecalcularPreciosCuenta(ctx, cuentaID); err != nil {
		t.Fatalf("calculando el precio de partida: %v", err)
	}
	c, err := st.UnCandidato(ctx, cuentaID, varianteID)
	if err != nil {
		t.Fatalf("leyendo el candidato: %v", err)
	}
	if !c.Listo {
		t.Fatalf("la variante de prueba no está lista para publicar: %+v", *c)
	}
	if err := st.GuardarPublicacion(ctx, cuentaID, productoID, varianteID, "EXT-HORARIO", "", "",
		publicar.HashContenido(*c), publicar.HashPrecio(*c), publicar.HashStock(*c), c.PrecioCanal, c.Stock); err != nil {
		t.Fatalf("registrando la publicación: %v", err)
	}
	return cuentaID, varianteID
}

func TestElHorarioRehaceLosPreciosAntesDePlanificar(t *testing.T) {
	st, cola, ctx := abrir(t)
	cuentaID, varianteID := catalogoPublicado(t, st, ctx)

	// El override entra directo en la base, sin recálculo: es lo que dejaba
	// una petición cuyo refresco falló, o el código anterior a este arreglo.
	if err := st.GuardarOverridePrecio(ctx, varianteID, cuentaID, 87000, "pactado con el proveedor", nil); err != nil {
		t.Fatalf("guardando el override: %v", err)
	}

	p := Nuevo(st, cola, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute, nil)
	if err := p.ejecutar(ctx, store.Horario{Nombre: "precios", Alcance: "price", CuentaID: &cuentaID}); err != nil {
		t.Fatalf("ejecutando el horario: %v", err)
	}

	var precio float64
	if err := st.Pool().QueryRow(ctx, `
		SELECT regular_price FROM effective_prices WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID).Scan(&precio); err != nil {
		t.Fatalf("leyendo el precio efectivo: %v", err)
	}
	if precio != 87000 {
		t.Fatalf("el horario planificó con el precio efectivo viejo (%.0f): el override no sale al canal", precio)
	}
	if !encolado(t, st, ctx, publicar.TrabajoPrecio, cuentaID, varianteID) {
		t.Fatal("no se encoló la actualización de precio: el canal sigue vendiendo al precio anterior")
	}
	// Solo cambió el precio: reenviar la ficha entera gastaría cupo del
	// endpoint más caro por nada.
	if encolado(t, st, ctx, publicar.TrabajoPublicar, cuentaID, varianteID) {
		t.Error("se encoló la ficha completa cuando solo cambió el precio")
	}
}

func encolado(t *testing.T, st *store.Store, ctx context.Context, kind string, cuentaID, varianteID int64) bool {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(ctx, `
		SELECT count(*) FROM jobs WHERE unique_key = $1 AND status IN ('pending', 'running')`,
		fmt.Sprintf("%s:%d:%d", kind, cuentaID, varianteID)).Scan(&n); err != nil {
		t.Fatalf("consultando la cola: %v", err)
	}
	return n > 0
}
