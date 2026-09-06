package store

import (
	"context"
	"testing"
	"time"
)

// Lo que Integra da por publicado y lo que el canal tiene de verdad son dos
// cosas distintas, y hasta la migración 023 la fila solo contaba la primera.
// De ahí salían las dos consecuencias caras: una ficha que el canal borró
// seguía prestando su external_id —así que el motor la actualizaba para
// siempre contra algo que ya no existe y no la recreaba jamás— y una variante
// archivada en Odoo desaparecía de la planificación dejando su ficha abierta.

// fixtureViva monta una cuenta ACTIVA con un producto publicable: sin cuenta
// activa, CandidatosPublicacion no devuelve nada y no habría nada que probar.
func fixtureViva(t *testing.T, st *Store) (cuentaID, prodID, varID int64) {
	t.Helper()
	ctx := context.Background()

	if err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-vivas-test', '\x00'::bytea, true FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID); err != nil {
		t.Fatal(err)
	}
	asignarBodegaDePrueba(t, st, ctx, cuentaID)

	var conexionID int64
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, active)
		VALUES ('conexion-vivas-test','http://x','x','x','\x00'::bytea,false) RETURNING id`).Scan(&conexionID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name, description_sale)
		VALUES ($1, 778001, 'Disco vivo', 'Un disco.') RETURNING id`, conexionID).Scan(&prodID); err != nil {
		t.Fatal(err)
	}
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku, price)
		VALUES ($1, 778001, 'VIVO-1', 100000) RETURNING id`, prodID).Scan(&varID); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM products WHERE odoo_connection_id = $1`, conexionID)
		_, _ = st.pool.Exec(ctx, `DELETE FROM odoo_connections WHERE id = $1`, conexionID)
	})
	return cuentaID, prodID, varID
}

func candidatoDe(t *testing.T, st *Store, cuentaID, varianteID int64) CandidatoPublicacion {
	t.Helper()
	todos, err := st.CandidatosPublicacion(context.Background(), cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range todos {
		if c.VarianteID == varianteID {
			return c
		}
	}
	t.Fatalf("la variante %d no salió entre los candidatos de la cuenta %d", varianteID, cuentaID)
	return CandidatoPublicacion{}
}

// El external_id vivía en la fila de producto y el motor lo leía sin mirar el
// estado: daba igual que la publicación estuviera muerta, seguía tratándola
// como existente y nunca volvía a crearla.
func TestUnaPublicacionCaidaDejaDePrestarSuReferencia(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "https://x/1", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}
	if got := candidatoDe(t, st, cuentaID, varID).ExternalID; got != "MCO1" {
		t.Fatalf("recién publicada, el candidato tiene que traer su referencia: %q", got)
	}

	if err := st.MarcarPublicacionCaida(ctx, cuentaID, varID, "eliminado"); err != nil {
		t.Fatal(err)
	}

	c := candidatoDe(t, st, cuentaID, varID)
	if c.ExternalID != "" {
		t.Errorf("la publicación ya no existe en el canal: el candidato no puede seguir apuntando a %q", c.ExternalID)
	}
	if c.ContentHash != "" || c.PriceHash != "" || c.StockHash != "" {
		t.Errorf("nada de lo enviado sigue en el canal: los tres hashes tienen que quedar en blanco (%q/%q/%q)",
			c.ContentHash, c.PriceHash, c.StockHash)
	}
}

// El negativo exacto del filtro de candidatos: si la variante deja de serlo,
// su ficha se queda abierta en el canal y nadie la ve. Es lo que hacía que un
// producto archivado en Odoo siguiera a la venta con el stock congelado.
func TestLaVarianteArchivadaSaleDeLosCandidatosYEntraEnLasHuerfanas(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}
	if h, err := st.PublicacionesHuerfanas(ctx, cuentaID); err != nil {
		t.Fatal(err)
	} else if len(h) != 0 {
		t.Fatalf("mientras es candidata no es huérfana: %+v", h)
	}

	// Lo que hace el sync cuando alguien archiva el producto en Odoo.
	if _, err := st.pool.Exec(ctx, `UPDATE product_variants SET active = false WHERE id = $1`, varID); err != nil {
		t.Fatal(err)
	}

	todos, err := st.CandidatosPublicacion(ctx, cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range todos {
		if c.VarianteID == varID {
			t.Fatal("una variante archivada no puede seguir siendo candidata")
		}
	}

	huerfanas, err := st.PublicacionesHuerfanas(ctx, cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(huerfanas) != 1 || huerfanas[0].VarianteID != varID {
		t.Fatalf("la ficha se quedó abierta en el canal y nadie la ve: %+v", huerfanas)
	}
	if huerfanas[0].Ref.ListingID != "MCO1" {
		t.Fatalf("sin la referencia no se puede pausar: %+v", huerfanas[0].Ref)
	}
}

// Borrarle el SKU es la otra puerta al mismo problema: el producto sigue vivo
// en Odoo pero deja de ser candidato, y su ficha se queda igual de abierta.
func TestLaVarianteSinSKUTambienDejaSuFichaHuerfana(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := st.pool.Exec(ctx, `UPDATE product_variants SET sku = NULL WHERE id = $1`, varID); err != nil {
		t.Fatal(err)
	}

	huerfanas, err := st.PublicacionesHuerfanas(ctx, cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	if len(huerfanas) != 1 {
		t.Fatalf("sin SKU deja de publicarse pero la ficha sigue viva: %+v", huerfanas)
	}
	// El SKU con el que se publicó sigue siendo la referencia en Falabella,
	// así que tiene que sobrevivir al borrado del actual.
	if huerfanas[0].Ref.SKU != "VIVO-1" {
		t.Fatalf("la pausa necesita el SKU con el que se publicó: %+v", huerfanas[0].Ref)
	}
}

// La pausa que decide Integra y la que decide el canal no son la misma cosa:
// la primera se levanta sola al volver el producto, la segunda no se toca.
func TestLaPausaDelCanalSobreviveALaConciliacionYLaDeIntegraSeReanuda(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}

	// La pausó Integra al salir del catálogo: el candidato tiene que decirlo
	// para que la planificación pida reabrirla.
	if err := st.MarcarPublicacionPausada(ctx, cuentaID, varID, PausaCatalogo); err != nil {
		t.Fatal(err)
	}
	c := candidatoDe(t, st, cuentaID, varID)
	if c.EstadoPublicacion != "paused" || c.PausaMotivo != PausaCatalogo {
		t.Fatalf("el candidato no sabe que está pausada: %q/%q", c.EstadoPublicacion, c.PausaMotivo)
	}
	if err := st.MarcarPublicacionReanudada(ctx, cuentaID, varID); err != nil {
		t.Fatal(err)
	}
	if c := candidatoDe(t, st, cuentaID, varID); c.EstadoPublicacion != "published" || c.PausaMotivo != "" {
		t.Fatalf("tras reabrirla queda publicada y sin motivo: %q/%q", c.EstadoPublicacion, c.PausaMotivo)
	}

	// La que retira el canal se anota con otro motivo, y solo se levanta
	// cuando el propio canal la vuelve a dar por activa.
	if err := st.MarcarPublicacionRetirada(ctx, cuentaID, varID, "under_review"); err != nil {
		t.Fatal(err)
	}
	if c := candidatoDe(t, st, cuentaID, varID); c.PausaMotivo != PausaCanal {
		t.Fatalf("la retirada del canal no se puede confundir con la de Integra: %q", c.PausaMotivo)
	}
	if retiradas, _, err := st.DesajustesDePublicacion(ctx); err != nil {
		t.Fatal(err)
	} else if retiradas == 0 {
		t.Fatal("sin contarla, el aviso no sale y nadie se entera de que el canal la bajó")
	}
	if err := st.MarcarPublicacionViva(ctx, cuentaID, varID, "active"); err != nil {
		t.Fatal(err)
	}
	if c := candidatoDe(t, st, cuentaID, varID); c.EstadoPublicacion != "published" || c.PausaMotivo != "" {
		t.Fatalf("si el canal la reabrió, Integra tiene que enterarse: %q/%q", c.EstadoPublicacion, c.PausaMotivo)
	}
}

// Una ficha que pausamos nosotros también le sale al canal como retirada. Si
// la conciliación la reetiquetara como decisión del canal, dejaría de
// reabrirse cuando el producto volviera al catálogo: quedaría cerrada para
// siempre por el propio arreglo del hueco 14.
func TestLaConciliacionNoSeApropiaDeLaPausaQueDecidioIntegra(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}
	if err := st.MarcarPublicacionPausada(ctx, cuentaID, varID, PausaCatalogo); err != nil {
		t.Fatal(err)
	}
	// El canal contesta lo esperable de algo que acabamos de pausar.
	if err := st.MarcarPublicacionRetirada(ctx, cuentaID, varID, "paused"); err != nil {
		t.Fatal(err)
	}

	if c := candidatoDe(t, st, cuentaID, varID); c.PausaMotivo != PausaCatalogo {
		t.Fatalf("la pausa la decidió Integra y sigue siendo suya: quedó como %q", c.PausaMotivo)
	}
	if retiradas, _, err := st.DesajustesDePublicacion(ctx); err != nil {
		t.Fatal(err)
	} else if retiradas != 0 {
		t.Fatalf("nuestra propia pausa no puede salir en el aviso de bajas del canal: %d", retiradas)
	}
}

// El periodo es lo que impide que una planificación horaria vuelva a consultar
// el catálogo entero cada hora y se lleve por delante el cupo de API que
// necesitan los envíos de stock.
func TestLaConciliacionNoRepitePublicacionesRecienConsultadas(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, prodID, varID := fixtureViva(t, st)

	if err := st.GuardarPublicacion(ctx, cuentaID, prodID, varID,
		"MCO1", "", "MCO1", "ch", "ph", "sh", 100000, 4); err != nil {
		t.Fatal(err)
	}
	pendientes, err := st.PublicacionesPorConciliar(ctx, cuentaID, 100, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendientes) != 1 {
		t.Fatalf("nunca consultada, tiene que salir: %+v", pendientes)
	}

	if err := st.MarcarPublicacionViva(ctx, cuentaID, varID, "active"); err != nil {
		t.Fatal(err)
	}
	if p, err := st.PublicacionesPorConciliar(ctx, cuentaID, 100, 24*time.Hour); err != nil {
		t.Fatal(err)
	} else if len(p) != 0 {
		t.Fatalf("recién consultada, no se vuelve a preguntar por ella: %+v", p)
	}
	// Pasado el periodo sí vuelve a tocarle.
	if p, err := st.PublicacionesPorConciliar(ctx, cuentaID, 100, time.Nanosecond); err != nil {
		t.Fatal(err)
	} else if len(p) != 1 {
		t.Fatalf("pasado el periodo hay que volver a preguntar: %+v", p)
	}
}
