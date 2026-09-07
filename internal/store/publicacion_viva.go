package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mdv/integra/internal/channel"
)

// Motivos por los que una publicación queda pausada. La diferencia decide si
// se puede reanudar sola: lo que pausó Integra sí, lo que retiró el canal no.
const (
	PausaCatalogo = "catalogo"
	PausaCanal    = "canal"
	// PausaManual la decidió una persona desde la pantalla de Publicación.
	// No se reanuda sola —Planificar solo reabre las de motivo «catalogo»—
	// porque deshacer con un horario lo que alguien apagó a mano es la forma
	// más rápida de que nadie vuelva a fiarse del botón.
	PausaManual = "manual"
)

// PublicacionViva es una fila de variant_channel_listings vista como "algo que
// existe ahí fuera": lo justo para consultar su estado o pausarla.
type PublicacionViva struct {
	VarianteID int64
	ProductoID int64
	SKU        string
	Ref        channel.ExternalRef
}

// PublicacionesPorConciliar devuelve las publicaciones de una cuenta que más
// tiempo llevan sin contrastarse contra el canal.
//
// Se limita el número por pasada a propósito: consultar el estado cuesta una
// petición por publicación en WooCommerce y Shopify, y quemar el cupo diario
// del canal en un informe dejaría sin enviar el stock, que es lo urgente. Con
// tandas, el catálogo entero se recorre en unas cuantas pasadas y ninguna
// publicación se queda sin revisar, porque siempre salen primero las más
// viejas.
func (s *Store) PublicacionesPorConciliar(ctx context.Context, cuentaID int64, limite int, periodo time.Duration) ([]PublicacionViva, error) {
	if limite <= 0 {
		limite = 200
	}
	filas, err := s.pool.Query(ctx, `
		SELECT v.variant_id, pv.product_id, COALESCE(pv.sku,''),
		       COALESCE(l.external_id,''), COALESCE(v.external_variant_id,''),
		       COALESCE(v.channel_sku, pv.sku, '')
		FROM variant_channel_listings v
		JOIN product_channel_listings l ON l.id = v.listing_id
		JOIN product_variants pv ON pv.id = v.variant_id
		WHERE v.channel_account_id = $1
		  AND v.status IN ('published','paused')
		  -- Sin identificador no hay nada que preguntar. Los cuatro canales lo
		  -- rellenan al publicar —en Falabella el propio SKU hace de
		  -- identificador—, así que una fila sin él nunca llegó al canal y
		  -- tratarla como desaparecida sería inventarse una baja.
		  AND l.external_id IS NOT NULL
		  -- Ya preguntada hace poco: no se vuelve a gastar una petición en
		  -- ella. Es lo que impide que una planificación horaria consulte el
		  -- catálogo entero cada hora.
		  AND (v.conciliada_at IS NULL OR v.conciliada_at < now() - $3::interval)
		ORDER BY v.conciliada_at NULLS FIRST, v.id
		LIMIT $2`, cuentaID, limite, periodo)
	if err != nil {
		return nil, fmt.Errorf("listando publicaciones por conciliar: %w", err)
	}
	defer filas.Close()
	return leerPublicacionesVivas(filas)
}

// PublicacionesHuerfanas devuelve lo que sigue abierto en el canal aunque su
// variante ya no sea publicable.
//
// El filtro es el negativo exacto del WHERE de CandidatosPublicacion: si una
// variante dejó de cumplirlo —la archivaron en Odoo, la excluyeron a mano o
// alguien le borró el SKU— desaparece de la planificación y su ficha se queda
// viva en el canal con la última cantidad conocida, congelada. Los dos filtros
// tienen que moverse juntos.
func (s *Store) PublicacionesHuerfanas(ctx context.Context, cuentaID int64) ([]PublicacionViva, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT v.variant_id, pv.product_id, COALESCE(pv.sku,''),
		       COALESCE(l.external_id,''), COALESCE(v.external_variant_id,''),
		       COALESCE(v.channel_sku, pv.sku, '')
		FROM variant_channel_listings v
		JOIN product_channel_listings l ON l.id = v.listing_id
		JOIN product_variants pv ON pv.id = v.variant_id
		JOIN products p ON p.id = pv.product_id
		WHERE v.channel_account_id = $1
		  AND v.status = 'published'
		  AND (NOT pv.active OR NOT p.active OR p.excluded_reason IS NOT NULL
		       OR pv.sku IS NULL OR pv.sku = '')
		ORDER BY v.id`, cuentaID)
	if err != nil {
		return nil, fmt.Errorf("listando publicaciones huérfanas: %w", err)
	}
	defer filas.Close()
	return leerPublicacionesVivas(filas)
}

func leerPublicacionesVivas(filas pgx.Rows) ([]PublicacionViva, error) {
	var out []PublicacionViva
	for filas.Next() {
		var p PublicacionViva
		if err := filas.Scan(&p.VarianteID, &p.ProductoID, &p.SKU,
			&p.Ref.ListingID, &p.Ref.VariantID, &p.Ref.SKU); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, filas.Err()
}

// MarcarPublicacionCaida registra que el canal ya no tiene esa publicación.
//
// Borrar la referencia y los tres hashes es lo que devuelve la variante al
// camino de creación: mientras external_id siguiera ahí, el motor la trataría
// como viva y la actualizaría contra una ficha inexistente hasta agotar
// reintentos, sin recrearla jamás. El estado 'deleted' es además lo que hace
// que CandidatosPublicacion deje de heredar el external_id del producto.
func (s *Store) MarcarPublicacionCaida(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET status = 'deleted', external_variant_id = NULL,
		    content_hash = NULL, price_hash = NULL, stock_hash = NULL,
		    pausada_at = NULL, pausada_motivo = NULL,
		    estado_canal = $3, conciliada_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID, nulo(estadoCanal))
	return err
}

// MarcarPublicacionRetirada anota que la publicación existe pero no está a la
// venta porque lo decidió el canal.
//
// No se recrea ni se reanuda desde aquí: lo habitual detrás de esto es una
// baja por infracción, y volver a abrirla sola convierte un aviso en una
// sanción. Queda contada para que la vigilancia lo avise y lo decida alguien.
//
// El motivo solo se pone si no había ninguno: una ficha que pausó Integra al
// salir el producto del catálogo también le sale al canal como retirada, y
// reetiquetarla como decisión del canal la dejaría sin reabrirse nunca cuando
// el producto volviera.
func (s *Store) MarcarPublicacionRetirada(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET status = 'paused', pausada_motivo = COALESCE(pausada_motivo, 'canal'),
		    pausada_at = COALESCE(pausada_at, now()),
		    estado_canal = $3, conciliada_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID, nulo(estadoCanal))
	return err
}

// MarcarPublicacionViva confirma que el canal la tiene a la venta.
//
// Si estaba marcada como retirada por el canal y ha vuelto sola —una revisión
// que terminó bien, una pausa por falta de stock que se levantó al reponer—,
// la marca se retira aquí. La pausa que decidió Integra no se toca: esa la
// levanta el trabajo de reanudación cuando el producto vuelve al catálogo.
func (s *Store) MarcarPublicacionViva(ctx context.Context, cuentaID, varianteID int64, estadoCanal string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET status = CASE WHEN pausada_motivo = 'canal' THEN 'published'::listing_status ELSE status END,
		    pausada_motivo = CASE WHEN pausada_motivo = 'canal' THEN NULL ELSE pausada_motivo END,
		    pausada_at = CASE WHEN pausada_motivo = 'canal' THEN NULL ELSE pausada_at END,
		    estado_canal = $3, conciliada_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID, nulo(estadoCanal))
	return err
}

// MarcarPublicacionPausada anota la pausa que acaba de aceptar el canal.
func (s *Store) MarcarPublicacionPausada(ctx context.Context, cuentaID, varianteID int64, motivo string) error {
	if motivo != PausaCatalogo && motivo != PausaCanal && motivo != PausaManual {
		return fmt.Errorf("motivo de pausa desconocido: %q", motivo)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET status = 'paused', pausada_motivo = $3, pausada_at = now(), updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID, motivo)
	return err
}

// MarcarPublicacionReanudada deshace una pausa de Integra.
//
// Los hashes se dejan como estaban: la ficha, el precio y el stock que tiene
// el canal no cambiaron por estar pausada, y ponerlos a cero obligaría a
// reenviarlo todo por el endpoint más caro sin necesidad.
func (s *Store) MarcarPublicacionReanudada(ctx context.Context, cuentaID, varianteID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE variant_channel_listings
		SET status = 'published', pausada_motivo = NULL, pausada_at = NULL, updated_at = now()
		WHERE variant_id = $1 AND channel_account_id = $2`, varianteID, cuentaID)
	return err
}

// DesajustesDePublicacion cuenta lo que la conciliación encontró distinto de
// lo que Integra creía.
//
// Es lo que alimenta los avisos: nadie en MDV se entera de que MercadoLibre
// cerró una publicación si Integra no lo dice, y mientras tanto el producto no
// se vende y el panel lo sigue contando como activo.
func (s *Store) DesajustesDePublicacion(ctx context.Context) (retiradas, caidas int, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'paused' AND pausada_motivo = 'canal')::int,
		       count(*) FILTER (WHERE status = 'deleted')::int
		FROM variant_channel_listings`).Scan(&retiradas, &caidas)
	return retiradas, caidas, err
}

// PublicacionesDeCuenta devuelve lo que la cuenta tiene abierto en el canal,
// para poder activarlo o apagarlo en bloque desde la pantalla.
//
// Publicar deja la ficha en borrador a propósito —en WooCommerce y en Shopify
// activar de cara al público es decisión humana— y sin esto no había ningún
// sitio donde tomarla: había que entrar al canal producto por producto.
//
// paraActivar decide a cuáles se apunta: las pausadas y las que nunca se
// activaron si es cierto; las vivas si es falso. Nunca se toca lo que retiró
// el propio canal: reabrir una baja por infracción convierte un aviso en una
// sanción.
func (s *Store) PublicacionesDeCuenta(ctx context.Context, cuentaID int64, paraActivar bool) ([]PublicacionViva, error) {
	// Al activar se apunta a TODO lo que tiene ficha en el canal, no solo a lo
	// que Integra marcó como pausado. El estado de aquí dice «se lo mandé al
	// canal», no «está a la venta»: la ficha nace en borrador y esa diferencia
	// vive únicamente del lado del canal, así que filtrar por el estado local
	// no encolaba nada justo cuando todo estaba por activar. Resume es
	// idempotente en los cuatro adaptadores, de modo que pedirlo sobre una que
	// ya está viva no cuesta nada.
	//
	// Lo que retiró el propio canal se excluye siempre: reabrir una baja por
	// infracción convierte un aviso en una sanción, y hacerlo desde un botón
	// que dice «poner a la venta» es peor todavía, porque nadie sabría que
	// ocurrió.
	cond := `v.status = 'published'`
	args := []any{cuentaID}
	if paraActivar {
		cond = `COALESCE(v.pausada_motivo, '') <> $2`
		args = append(args, PausaCanal)
	}

	filas, err := s.pool.Query(ctx, `
		SELECT v.variant_id, pv.product_id, COALESCE(pv.sku,''),
		       COALESCE(l.external_id,''), COALESCE(v.external_variant_id,''),
		       COALESCE(v.channel_sku, pv.sku, '')
		FROM variant_channel_listings v
		JOIN product_channel_listings l ON l.id = v.listing_id
		JOIN product_variants pv ON pv.id = v.variant_id
		WHERE v.channel_account_id = $1
		  AND l.external_id IS NOT NULL
		  AND `+cond+`
		ORDER BY v.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("listando las publicaciones de la cuenta %d: %w", cuentaID, err)
	}
	defer filas.Close()
	return leerPublicacionesVivas(filas)
}

// OlvidarPublicacion borra el rastro local de una ficha eliminada del canal.
//
// Se quitan las dos filas y sus hashes: dejarlas haría que el motor tratara de
// actualizar para siempre una publicación que ya no existe, y que el producto
// no se pudiera volver a publicar nunca porque el external_id seguía ahí.
func (s *Store) OlvidarPublicacion(ctx context.Context, cuentaID, varianteID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM variant_channel_listings
		WHERE channel_account_id = $1 AND variant_id = $2`, cuentaID, varianteID); err != nil {
		return fmt.Errorf("olvidando la publicación de la variante %d: %w", varianteID, err)
	}
	// La fila de producto solo se borra si no le queda ninguna variante: un
	// producto con varias publicadas sigue vivo en el canal.
	if _, err := tx.Exec(ctx, `
		DELETE FROM product_channel_listings l
		WHERE l.channel_account_id = $1
		  AND l.product_id = (SELECT product_id FROM product_variants WHERE id = $2)
		  AND NOT EXISTS (SELECT 1 FROM variant_channel_listings v WHERE v.listing_id = l.id)`,
		cuentaID, varianteID); err != nil {
		return fmt.Errorf("olvidando la ficha de producto: %w", err)
	}
	return tx.Commit(ctx)
}
