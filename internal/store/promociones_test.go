package store

import (
	"context"
	"testing"
	"time"
)

// El ciclo de vida de una promoción es lo único de precios que depende del
// reloj, y por eso es donde más fácil se cuela un error que nadie ve hasta que
// un producto se vende con descuento el mes equivocado. Estas pruebas mueven
// las fechas a mano y comprueban en qué estado cae cada una.
func TestPromocionProgramadaNoSeAplicaAntesDeTiempo(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)

	manana := time.Now().Add(24 * time.Hour)
	pasado := manana.Add(24 * time.Hour)
	id := crearOferta(t, st, ctx, varianteID, cuentaID, 1000, manana, &pasado)

	// Vigencia: no debe aparecer en el precio efectivo de hoy.
	activas, err := st.OfertasActivasDeCuenta(ctx, cuentaID)
	if err != nil {
		t.Fatalf("consultando ofertas activas: %v", err)
	}
	if _, hay := activas[varianteID]; hay {
		t.Error("una promoción que empieza mañana se está aplicando hoy")
	}

	// Y el planificador no debe tocarla todavía.
	if enCambios(t, st, ctx, id) != "" {
		t.Error("el planificador quiere mover una promoción que aún no empieza")
	}

	if e := estadoDe(t, st, ctx, varianteID, id); e != "programada" {
		t.Errorf("estado = %q, se esperaba \"programada\"", e)
	}
}

func TestPromocionVigenteSeAplicaYLuegoSeRevierte(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)

	inicio := time.Now().Add(-time.Hour)
	fin := time.Now().Add(time.Hour)
	id := crearOferta(t, st, ctx, varianteID, cuentaID, 1000, inicio, &fin)

	activas, err := st.OfertasActivasDeCuenta(ctx, cuentaID)
	if err != nil {
		t.Fatalf("consultando ofertas activas: %v", err)
	}
	if _, hay := activas[varianteID]; !hay {
		t.Fatal("una promoción vigente no se está aplicando")
	}
	if enCambios(t, st, ctx, id) != "aplicar" {
		t.Fatal("el planificador no la ve como pendiente de aplicar")
	}

	if err := st.MarcarPromocionesProcesadas(ctx, []int64{id}, nil); err != nil {
		t.Fatalf("marcando aplicada: %v", err)
	}
	// Ya sellada, no debe volver a encolarse en la siguiente pasada.
	if enCambios(t, st, ctx, id) != "" {
		t.Error("una promoción ya aplicada se vuelve a encolar en cada pasada")
	}

	// Se adelanta el fin: ahora toca deshacerla.
	if _, err := st.pool.Exec(ctx,
		`UPDATE offers SET ends_at = now() - interval '1 minute' WHERE id = $1`, id); err != nil {
		t.Fatalf("adelantando el fin: %v", err)
	}
	if enCambios(t, st, ctx, id) != "revertir" {
		t.Fatal("una promoción terminada no se encola para revertir; el canal se queda con el precio rebajado")
	}
	activas, err = st.OfertasActivasDeCuenta(ctx, cuentaID)
	if err != nil {
		t.Fatalf("consultando ofertas activas: %v", err)
	}
	if _, hay := activas[varianteID]; hay {
		t.Error("una promoción terminada sigue afectando al precio efectivo")
	}

	if err := st.MarcarPromocionesProcesadas(ctx, nil, []int64{id}); err != nil {
		t.Fatalf("marcando revertida: %v", err)
	}
	if enCambios(t, st, ctx, id) != "" {
		t.Error("una promoción ya revertida se vuelve a encolar")
	}
	if e := estadoDe(t, st, ctx, varianteID, id); e != "terminada" {
		t.Errorf("estado = %q, se esperaba \"terminada\"", e)
	}
}

// Cancelar una promoción ya aplicada tiene que deshacerla en el canal: si solo
// se desactiva en la base, el precio rebajado sigue publicado para siempre.
func TestPromocionCanceladaSeRevierteEnElCanal(t *testing.T) {
	st, ctx := abrir(t)
	varianteID, cuentaID := cobayas(t, st, ctx)

	id := crearOferta(t, st, ctx, varianteID, cuentaID, 1000, time.Now().Add(-time.Hour), nil)
	if err := st.MarcarPromocionesProcesadas(ctx, []int64{id}, nil); err != nil {
		t.Fatalf("marcando aplicada: %v", err)
	}

	if err := st.CancelarOferta(ctx, id); err != nil {
		t.Fatalf("cancelando: %v", err)
	}
	if enCambios(t, st, ctx, id) != "revertir" {
		t.Error("cancelar no encola la reversión: el canal se queda con el descuento")
	}
	if e := estadoDe(t, st, ctx, varianteID, id); e != "cancelada" {
		t.Errorf("estado = %q, se esperaba \"cancelada\"", e)
	}
}

// enCambios devuelve "aplicar", "revertir" o "" según lo que el planificador
// quiera hacer ahora mismo con esa oferta.
func enCambios(t *testing.T, st *Store, ctx context.Context, id int64) string {
	t.Helper()
	cambios, err := st.PromocionesPendientes(ctx)
	if err != nil {
		t.Fatalf("consultando promociones pendientes: %v", err)
	}
	for _, c := range cambios {
		for _, a := range c.Aplicar {
			if a == id {
				return "aplicar"
			}
		}
		for _, r := range c.Revertir {
			if r == id {
				return "revertir"
			}
		}
	}
	return ""
}

func estadoDe(t *testing.T, st *Store, ctx context.Context, varianteID, id int64) string {
	t.Helper()
	vistas, err := st.OfertasDeVariante(ctx, varianteID)
	if err != nil {
		t.Fatalf("listando ofertas de la variante: %v", err)
	}
	for _, v := range vistas {
		if v.ID == id {
			return v.Estado
		}
	}
	t.Fatalf("la oferta %d no aparece en la lista de la variante", id)
	return ""
}

func crearOferta(t *testing.T, st *Store, ctx context.Context, varianteID, cuentaID int64,
	precio float64, inicia time.Time, termina *time.Time) int64 {
	t.Helper()
	id, err := st.GuardarOferta(ctx, varianteID, cuentaID, precio, inicia, termina, nil)
	if err != nil {
		t.Fatalf("creando la oferta: %v", err)
	}
	// La promoción es del test, no del catálogo: se borra al terminar para no
	// dejar precios raros en la base de desarrollo.
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM offers WHERE id = $1`, id)
	})
	return id
}

// cobayas devuelve una variante real y una cuenta de canal desechable.
//
// La cuenta se crea aquí en vez de reutilizar una existente porque en
// desarrollo todavía no hay ninguna conectada, y un test que se salta por
// falta de datos no comprueba nada — que es justo lo que pasó la primera vez
// que se ejecutó este archivo.
func cobayas(t *testing.T, st *Store, ctx context.Context) (int64, int64) {
	t.Helper()

	var varianteID int64
	if err := st.pool.QueryRow(ctx,
		`SELECT id FROM product_variants WHERE active LIMIT 1`).Scan(&varianteID); err != nil {
		t.Fatalf("la base no tiene ni una variante activa: %v", err)
	}

	var canalID int64
	if err := st.pool.QueryRow(ctx, `SELECT id FROM channels LIMIT 1`).Scan(&canalID); err != nil {
		t.Fatalf("la base no tiene canales: %v", err)
	}

	// brand_id + channel_id son únicos, así que se usa una marca desechable
	// para no chocar con una cuenta real ni impedir crearla después.
	var marcaTestID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO brands (code, name) VALUES ('zz-test-promociones', 'ZZ Test Promociones')
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
		RETURNING id`).Scan(&marcaTestID); err != nil {
		t.Fatalf("creando la marca de prueba: %v", err)
	}

	var cuentaID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc)
		VALUES ($1, $2, 'cuenta de prueba', '\x00'::bytea)
		RETURNING id`, marcaTestID, canalID).Scan(&cuentaID); err != nil {
		t.Fatalf("creando la cuenta de prueba: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM brands WHERE id = $1`, marcaTestID)
	})

	return varianteID, cuentaID
}
