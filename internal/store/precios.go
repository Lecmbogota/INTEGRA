package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mdv/integra/internal/pricing"
)

// ReglaPrecioCanal representa una regla en channel_price_rules.
type ReglaPrecioCanal struct {
	ID               int64     `json:"id"`
	ChannelAccountID int64     `json:"channel_account_id"`
	BrandID          *int64    `json:"brand_id,omitempty"`
	CategPathPrefix  *string   `json:"categ_path_prefix,omitempty"`
	AdjustmentType   string    `json:"adjustment_type"` // 'percent' | 'fixed'
	AdjustmentValue  float64   `json:"adjustment_value"`
	RoundTo          *float64  `json:"round_to,omitempty"`
	MinMarginPercent *float64  `json:"min_margin_percent,omitempty"`
	Priority         int       `json:"priority"`
	Active           bool      `json:"active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// OverridePrecio representa una excepción fijada a mano en price_overrides.
type OverridePrecio struct {
	ID               int64     `json:"id"`
	VariantID        int64     `json:"variant_id"`
	ChannelAccountID int64     `json:"channel_account_id"`
	Price            float64   `json:"price"`
	Reason           string    `json:"reason"`
	CreatedBy        *int64    `json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// OfertaCanal representa una oferta temporal en offers.
type OfertaCanal struct {
	ID               int64      `json:"id"`
	VariantID        int64      `json:"variant_id"`
	ChannelAccountID int64      `json:"channel_account_id"`
	OfferPrice       float64    `json:"offer_price"`
	StartsAt         time.Time  `json:"starts_at"`
	EndsAt           *time.Time `json:"ends_at,omitempty"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	RevertedAt       *time.Time `json:"reverted_at,omitempty"`
	Active           bool       `json:"active"`
	CreatedBy        *int64     `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// PrecioEfectivo representa una fila en effective_prices.
type PrecioEfectivo struct {
	VariantID        int64     `json:"variant_id"`
	ChannelAccountID int64     `json:"channel_account_id"`
	RegularPrice     float64   `json:"regular_price"`
	SalePrice        *float64  `json:"sale_price,omitempty"`
	Currency         string    `json:"currency"`
	Source           string    `json:"source"`
	ComputedAt       time.Time `json:"computed_at"`
}

// ReglasPrecioDeCuenta lista las reglas de precio para una cuenta de canal.
func (s *Store) ReglasPrecioDeCuenta(ctx context.Context, cuentaID int64) ([]pricing.ChannelPriceRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, channel_account_id, brand_id, categ_path_prefix,
		       adjustment_type, adjustment_value, round_to, min_margin_percent, priority, active
		FROM channel_price_rules
		WHERE channel_account_id = $1 AND active
		ORDER BY priority ASC, id ASC`, cuentaID)
	if err != nil {
		return nil, fmt.Errorf("consultando reglas de precio de cuenta %d: %w", cuentaID, err)
	}
	defer rows.Close()

	var out []pricing.ChannelPriceRule
	for rows.Next() {
		var r pricing.ChannelPriceRule
		if err := rows.Scan(&r.ID, &r.ChannelAccountID, &r.BrandID, &r.CategPathPrefix,
			&r.AdjustmentType, &r.AdjustmentValue, &r.RoundTo, &r.MinMarginPercent,
			&r.Priority, &r.Active); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GuardarReglaPrecioCanal crea o actualiza una regla de precio de canal.
func (s *Store) GuardarReglaPrecioCanal(ctx context.Context, r ReglaPrecioCanal) (int64, error) {
	if r.AdjustmentType != "percent" && r.AdjustmentType != "fixed" {
		return 0, fmt.Errorf("tipo de ajuste inválido: %q (debe ser percent o fixed)", r.AdjustmentType)
	}
	if r.ID == 0 {
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO channel_price_rules
			    (channel_account_id, brand_id, categ_path_prefix, adjustment_type,
			     adjustment_value, round_to, min_margin_percent, priority, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id`,
			r.ChannelAccountID, r.BrandID, r.CategPathPrefix, r.AdjustmentType,
			r.AdjustmentValue, r.RoundTo, r.MinMarginPercent, r.Priority, r.Active).Scan(&id)
		return id, err
	}

	// La regla tiene que ser de la cuenta indicada: si no, se editaría la de
	// otra cuenta y quien guarda recalcularía los precios de la equivocada.
	et, err := s.pool.Exec(ctx, `
		UPDATE channel_price_rules
		SET brand_id = $2, categ_path_prefix = $3, adjustment_type = $4,
		    adjustment_value = $5, round_to = $6, min_margin_percent = $7,
		    priority = $8, active = $9, updated_at = now()
		WHERE id = $1 AND channel_account_id = $10`,
		r.ID, r.BrandID, r.CategPathPrefix, r.AdjustmentType,
		r.AdjustmentValue, r.RoundTo, r.MinMarginPercent, r.Priority, r.Active,
		r.ChannelAccountID)
	if err != nil {
		return 0, err
	}
	if et.RowsAffected() == 0 {
		return 0, fmt.Errorf("no existe la regla %d en la cuenta %d", r.ID, r.ChannelAccountID)
	}
	return r.ID, nil
}

// OverridesDeCuenta devuelve los overrides manuales fijados para una cuenta.
func (s *Store) OverridesDeCuenta(ctx context.Context, cuentaID int64) (map[int64]pricing.PriceOverride, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT variant_id, channel_account_id, price, COALESCE(reason, '')
		FROM price_overrides
		WHERE channel_account_id = $1`, cuentaID)
	if err != nil {
		return nil, fmt.Errorf("consultando overrides de cuenta %d: %w", cuentaID, err)
	}
	defer rows.Close()

	out := make(map[int64]pricing.PriceOverride)
	for rows.Next() {
		var o pricing.PriceOverride
		if err := rows.Scan(&o.VariantID, &o.ChannelAccountID, &o.Price, &o.Reason); err != nil {
			return nil, err
		}
		out[o.VariantID] = o
	}
	return out, rows.Err()
}

// GuardarOverridePrecio fija un precio manual para una variante en un canal.
func (s *Store) GuardarOverridePrecio(ctx context.Context, varianteID, cuentaID int64, precio float64, razon string, userID *int64) error {
	if precio <= 0 {
		return fmt.Errorf("el precio de override debe ser mayor a 0")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO price_overrides (variant_id, channel_account_id, price, reason, created_by)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (variant_id, channel_account_id) DO UPDATE
		SET price = EXCLUDED.price, reason = EXCLUDED.reason,
		    created_by = EXCLUDED.created_by, updated_at = now()`,
		varianteID, cuentaID, precio, razon, userID)
	return err
}

// EliminarOverridePrecio quita el precio manual de una variante en una cuenta.
func (s *Store) EliminarOverridePrecio(ctx context.Context, varianteID, cuentaID int64) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM price_overrides WHERE variant_id = $1 AND channel_account_id = $2`,
		varianteID, cuentaID)
	return err
}

// OfertasActivasDeCuenta devuelve las ofertas VIGENTES de una cuenta,
// indexadas por variant_id.
//
// Vigente significa las dos cosas: ya empezó y aún no terminó. Comprobar solo
// el fin —como hacía esta consulta— aplicaba hoy el precio de una promoción
// programada para el mes que viene, que es exactamente lo contrario de
// programarla.
//
// Cuando hay varias ofertas solapadas para la misma variante gana la que
// empezó más tarde: es la decisión más reciente de quien la creó.
func (s *Store) OfertasActivasDeCuenta(ctx context.Context, cuentaID int64) (map[int64]pricing.Offer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (variant_id)
		       id, variant_id, channel_account_id, offer_price, starts_at, ends_at, applied_at, reverted_at, active
		FROM offers
		WHERE channel_account_id = $1 AND active
		  AND starts_at <= now()
		  AND (ends_at IS NULL OR ends_at > now())
		ORDER BY variant_id, starts_at DESC`, cuentaID)
	if err != nil {
		return nil, fmt.Errorf("consultando ofertas activas de cuenta %d: %w", cuentaID, err)
	}
	defer rows.Close()

	out := make(map[int64]pricing.Offer)
	for rows.Next() {
		var o pricing.Offer
		if err := rows.Scan(&o.ID, &o.VariantID, &o.ChannelAccountID, &o.OfferPrice,
			&o.StartsAt, &o.EndsAt, &o.AppliedAt, &o.RevertedAt, &o.Active); err != nil {
			return nil, err
		}
		out[o.VariantID] = o
	}
	return out, rows.Err()
}

// GuardarOferta registra o actualiza una oferta con vigencia.
func (s *Store) GuardarOferta(ctx context.Context, varianteID, cuentaID int64, precio float64, startsAt time.Time, endsAt *time.Time, userID *int64) (int64, error) {
	if precio <= 0 {
		return 0, fmt.Errorf("el precio de oferta debe ser mayor a 0")
	}
	if endsAt != nil && endsAt.Before(startsAt) {
		return 0, fmt.Errorf("la fecha de fin no puede ser anterior a la de inicio")
	}

	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO offers (variant_id, channel_account_id, offer_price, starts_at, ends_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`,
		varianteID, cuentaID, precio, startsAt, endsAt, userID).Scan(&id)
	return id, err
}

// OfertaVista es una promoción tal como se muestra en la interfaz.
type OfertaVista struct {
	ID         int64      `json:"id"`
	VarianteID int64      `json:"variante_id"`
	CuentaID   int64      `json:"cuenta_id"`
	Canal      string     `json:"canal"`
	Precio     float64    `json:"precio"`
	Inicia     time.Time  `json:"inicia"`
	Termina    *time.Time `json:"termina"`
	Activa     bool       `json:"activa"`
	// Estado resuelto contra el reloj: programada | vigente | terminada | cancelada.
	Estado     string     `json:"estado"`
	AplicadaAt *time.Time `json:"aplicada_at"`
}

// OfertasDeVariante lista todas las promociones de un producto, con su estado
// ya resuelto para no obligar a la interfaz a comparar fechas.
func (s *Store) OfertasDeVariante(ctx context.Context, varianteID int64) ([]OfertaVista, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT o.id, o.variant_id, o.channel_account_id, ch.code,
		       o.offer_price, o.starts_at, o.ends_at, o.active, o.applied_at,
		       CASE
		         WHEN NOT o.active                              THEN 'cancelada'
		         WHEN o.ends_at IS NOT NULL AND o.ends_at <= now() THEN 'terminada'
		         WHEN o.starts_at > now()                       THEN 'programada'
		         ELSE 'vigente'
		       END
		FROM offers o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.variant_id = $1
		ORDER BY o.starts_at DESC`, varianteID)
	if err != nil {
		return nil, fmt.Errorf("listando ofertas de la variante %d: %w", varianteID, err)
	}
	defer filas.Close()

	var out []OfertaVista
	for filas.Next() {
		var o OfertaVista
		if err := filas.Scan(&o.ID, &o.VarianteID, &o.CuentaID, &o.Canal,
			&o.Precio, &o.Inicia, &o.Termina, &o.Activa, &o.AplicadaAt, &o.Estado); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, filas.Err()
}

// CancelarOferta la desactiva sin borrarla.
//
// No se borra a propósito: una promoción que existió es un hecho comercial y
// puede hacer falta explicar por qué algo se vendió a ese precio.
func (s *Store) CancelarOferta(ctx context.Context, ofertaID int64) error {
	et, err := s.pool.Exec(ctx, `
		UPDATE offers SET active = FALSE, updated_at = now() WHERE id = $1`, ofertaID)
	if err != nil {
		return fmt.Errorf("cancelando la oferta: %w", err)
	}
	if et.RowsAffected() == 0 {
		return fmt.Errorf("no existe la oferta %d", ofertaID)
	}
	return nil
}

// CambioPromocion agrupa por cuenta las promociones que acaban de cruzar una
// frontera de vigencia y todavía no se han empujado al canal.
type CambioPromocion struct {
	CuentaID int64
	// Aplicar: ya empezaron pero el canal sigue mostrando el precio normal.
	Aplicar []int64
	// Revertir: ya terminaron pero el canal sigue mostrando el rebajado, que es
	// el caso caro — se está vendiendo con descuento fuera de la promoción.
	Revertir []int64
}

// PromocionesPendientes busca las promociones cuyo estado en la base ya no
// coincide con lo que está publicado.
//
// Es lo que convierte una fecha en un hecho: sin esta pasada, programar una
// promoción para el viernes no hace nada el viernes, porque el recálculo de
// precios solo ocurría cuando alguien pulsaba un botón.
func (s *Store) PromocionesPendientes(ctx context.Context) ([]CambioPromocion, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT channel_account_id, id, 'aplicar' AS que
		FROM offers
		WHERE active AND applied_at IS NULL AND starts_at <= now()
		  AND (ends_at IS NULL OR ends_at > now())
		UNION ALL
		-- Una promoción cancelada a mano también hay que revertirla: el precio
		-- rebajado sigue publicado hasta que alguien lo deshaga.
		SELECT channel_account_id, id, 'revertir'
		FROM offers
		WHERE applied_at IS NOT NULL AND reverted_at IS NULL
		  AND (NOT active OR (ends_at IS NOT NULL AND ends_at <= now()))
		ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("buscando promociones pendientes: %w", err)
	}
	defer filas.Close()

	porCuenta := map[int64]*CambioPromocion{}
	var orden []int64
	for filas.Next() {
		var cuenta, oferta int64
		var que string
		if err := filas.Scan(&cuenta, &oferta, &que); err != nil {
			return nil, err
		}
		c, ok := porCuenta[cuenta]
		if !ok {
			c = &CambioPromocion{CuentaID: cuenta}
			porCuenta[cuenta] = c
			orden = append(orden, cuenta)
		}
		if que == "aplicar" {
			c.Aplicar = append(c.Aplicar, oferta)
		} else {
			c.Revertir = append(c.Revertir, oferta)
		}
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}

	out := make([]CambioPromocion, 0, len(orden))
	for _, id := range orden {
		out = append(out, *porCuenta[id])
	}
	return out, nil
}

// MarcarPromocionesProcesadas sella las que ya se empujaron al canal.
//
// Se llama DESPUÉS de encolar el envío, no antes: si el proceso muere entre
// medias se vuelve a planificar en la siguiente pasada, y planificar de más es
// inofensivo —el diff por hash descarta lo que no cambió— mientras que sellar
// de más deja una promoción que nunca llega al canal.
func (s *Store) MarcarPromocionesProcesadas(ctx context.Context, aplicadas, revertidas []int64) error {
	if len(aplicadas) > 0 {
		if _, err := s.pool.Exec(ctx, `
			UPDATE offers SET applied_at = now(), updated_at = now()
			WHERE id = ANY($1) AND applied_at IS NULL`, aplicadas); err != nil {
			return fmt.Errorf("marcando promociones aplicadas: %w", err)
		}
	}
	if len(revertidas) > 0 {
		if _, err := s.pool.Exec(ctx, `
			UPDATE offers SET reverted_at = now(), updated_at = now()
			WHERE id = ANY($1) AND reverted_at IS NULL`, revertidas); err != nil {
			return fmt.Errorf("marcando promociones revertidas: %w", err)
		}
	}
	return nil
}

// OfertasVigentes alimenta el panel: lo que está rebajado ahora mismo.
func (s *Store) OfertasVigentes(ctx context.Context) ([]OfertaVista, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT o.id, o.variant_id, o.channel_account_id, ch.code,
		       o.offer_price, o.starts_at, o.ends_at, o.active, o.applied_at,
		       CASE WHEN o.starts_at > now() THEN 'programada' ELSE 'vigente' END
		FROM offers o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.active AND (o.ends_at IS NULL OR o.ends_at > now())
		ORDER BY o.starts_at DESC
		LIMIT 200`)
	if err != nil {
		return nil, fmt.Errorf("listando ofertas vigentes: %w", err)
	}
	defer filas.Close()

	var out []OfertaVista
	for filas.Next() {
		var o OfertaVista
		if err := filas.Scan(&o.ID, &o.VarianteID, &o.CuentaID, &o.Canal,
			&o.Precio, &o.Inicia, &o.Termina, &o.Activa, &o.AplicadaAt, &o.Estado); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, filas.Err()
}

// GuardarPreciosEfectivos persiste en bloque los precios resueltos en effective_prices.
func (s *Store) GuardarPreciosEfectivos(ctx context.Context, precios []pricing.EffectivePrice) error {
	if len(precios) == 0 {
		return nil
	}

	b := &pgx.Batch{}
	for _, p := range precios {
		// El SELECT ... WHERE EXISTS, en vez de VALUES, salta la variante que
		// ya no está en vez de romper la inserción entera. El recálculo de una
		// cuenta recorre cientos de variantes y puede solaparse con un borrado
		// —al eliminar una conexión de Odoo, por ejemplo—: sin esto, una sola
		// variante desaparecida a mitad tiraba el lote completo y la cuenta se
		// quedaba con los precios viejos sin que nadie supiera por qué.
		//
		// FOR KEY SHARE porque comprobar que existe no basta: la comprobación
		// de la clave foránea se hace con una lectura posterior, y entre una y
		// otra cabe un borrado que hacía saltar el mismo error que se quería
		// evitar. El bloqueo retiene la fila padre hasta que la inserción
		// termina, y no estorba a nadie: solo impide borrar esa variante.
		b.Queue(`
			INSERT INTO effective_prices
			    (variant_id, channel_account_id, regular_price, sale_price, currency, source, computed_at)
			SELECT $1, $2, $3, $4, $5, $6, now()
			WHERE EXISTS (SELECT 1 FROM product_variants WHERE id = $1 FOR KEY SHARE)
			ON CONFLICT (variant_id, channel_account_id) DO UPDATE
			SET regular_price = EXCLUDED.regular_price,
			    sale_price = EXCLUDED.sale_price,
			    currency = EXCLUDED.currency,
			    source = EXCLUDED.source,
			    computed_at = now()`,
			p.VariantID, p.ChannelAccountID, p.RegularPrice, p.SalePrice, p.Currency, p.Source)
	}

	br := s.pool.SendBatch(ctx, b)
	defer br.Close()

	for range precios {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("guardando precios efectivos: %w", err)
		}
	}
	return nil
}

// RecalcularPreciosCuenta resuelve y guarda los precios efectivos para todas las variantes de una cuenta.
func (s *Store) RecalcularPreciosCuenta(ctx context.Context, cuentaID int64) (int, error) {
	var canalCodigo string
	var comision, costoFijo, minMargen float64
	err := s.pool.QueryRow(ctx, `
		SELECT ch.code, ch.comision_pct, ch.costo_fijo, a.min_margen_pct
		FROM channel_accounts a JOIN channels ch ON ch.id = a.channel_id
		WHERE a.id = $1 AND a.active`, cuentaID).Scan(&canalCodigo, &comision, &costoFijo, &minMargen)
	if err != nil {
		return 0, fmt.Errorf("no existe o no está activa la cuenta %d: %w", cuentaID, err)
	}

	channelInfo := pricing.ChannelInfo{
		CommissionPct: comision,
		FixedCost:     costoFijo,
		Currency:      "COP",
		MinMargenPct:  minMargen,
	}

	reglas, err := s.ReglasPrecioDeCuenta(ctx, cuentaID)
	if err != nil {
		return 0, err
	}
	overrides, err := s.OverridesDeCuenta(ctx, cuentaID)
	if err != nil {
		return 0, err
	}
	ofertas, err := s.OfertasActivasDeCuenta(ctx, cuentaID)
	if err != nil {
		return 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT v.id, p.id, p.brand_id, COALESCE(p.categ_path, ''),
		       COALESCE(v.price, 0), COALESCE(v.cost, 0)
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		WHERE v.active AND p.active AND p.excluded_reason IS NULL`)
	if err != nil {
		return 0, fmt.Errorf("consultando variantes para precios: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	var efectivos []pricing.EffectivePrice
	var bajoCosto []VarianteBajoCosto

	for rows.Next() {
		var input pricing.VariantPricingInput
		if err := rows.Scan(&input.VariantID, &input.ProductID, &input.BrandID,
			&input.CategPath, &input.BasePrice, &input.Cost); err != nil {
			return 0, err
		}

		var ovPtr *pricing.PriceOverride
		if ov, ok := overrides[input.VariantID]; ok {
			ovPtr = &ov
		}
		var ofPtr *pricing.Offer
		if of, ok := ofertas[input.VariantID]; ok {
			ofPtr = &of
		}

		ef := pricing.ResolverPrecio(input, channelInfo, reglas, ovPtr, ofPtr, now)
		ef.ChannelAccountID = cuentaID
		efectivos = append(efectivos, ef)

		// El suelo de coste de ResolverPrecio solo alcanza al precio
		// calculado: un override tecleado con un dígito de menos y una oferta
		// por debajo del coste lo saltan a propósito, porque son decisiones de
		// una persona y subirlas a escondidas sería peor. Lo que no puede
		// pasar es que nadie se entere, así que se comprueba aquí el precio
		// que de verdad se va a publicar.
		precio := ef.RegularPrice
		if ef.SalePrice != nil {
			precio = *ef.SalePrice
		}
		// Sin precio no hay venta a pérdida: eso ya lo dice el motivo
		// «sin precio asignado» de la cola, y duplicarlo enterraría el aviso
		// que sí importa bajo cientos de fichas a medio hacer.
		margen := pricing.MargenExigido(input, channelInfo, reglas)
		if precio > 0 && !pricing.CubreCosto(precio, input.Cost, margen, channelInfo) {
			bajoCosto = append(bajoCosto, VarianteBajoCosto{
				VarianteID: input.VariantID,
				Precio:     precio,
				Neto:       pricing.NetoDelCanal(precio, channelInfo),
				Costo:      input.Cost,
				MargenPct:  margen,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	if err := s.GuardarPreciosEfectivos(ctx, efectivos); err != nil {
		return 0, err
	}
	if err := s.MarcarPreciosBajoCosto(ctx, cuentaID, bajoCosto); err != nil {
		return 0, err
	}

	return len(efectivos), nil
}

// MotivoBajoCosto es el motivo con el que la cola de atención señala una venta
// a pérdida. Estaba declarado en el comentario de migrations/005_sync.sql
// desde el principio y no lo generaba nadie.
const MotivoBajoCosto = "price_below_cost"

// VarianteBajoCosto es una variante cuyo precio publicable no cubre el coste,
// con las cifras que hacen falta para explicárselo a quien lo tiene que
// arreglar sin obligarle a rehacer la cuenta.
type VarianteBajoCosto struct {
	VarianteID int64
	Precio     float64
	Neto       float64
	Costo      float64
	MargenPct  float64
}

// MarcarPreciosBajoCosto deja la cola de atención de la cuenta diciendo
// exactamente lo que pasa ahora mismo: aparecen las que no cubren coste y
// desaparecen las que ya lo cubren.
//
// Se reconstruye entera en vez de ir marcando altas y bajas porque el recálculo
// de precios ya recorre el catálogo completo: así una subida de precio hace
// desaparecer el aviso en la misma pasada, sin esperar a que alguien lo
// resuelva a mano.
func (s *Store) MarcarPreciosBajoCosto(ctx context.Context, cuentaID int64, filas []VarianteBajoCosto) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ids := make([]int64, 0, len(filas))
	for _, f := range filas {
		ids = append(ids, f.VarianteID)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM attention_queue
		WHERE channel_account_id = $1 AND reason = $2 AND NOT (variant_id = ANY($3))`,
		cuentaID, MotivoBajoCosto, ids); err != nil {
		return fmt.Errorf("limpiando los avisos de precio bajo coste: %w", err)
	}

	for _, f := range filas {
		detalle := fmt.Sprintf(
			"se publica a %.0f, el canal deja %.0f y el coste con el margen mínimo (%.1f%%) es %.0f",
			f.Precio, f.Neto, f.MargenPct, f.Costo*(1+f.MargenPct/100))
		if _, err := tx.Exec(ctx, `
			INSERT INTO attention_queue (variant_id, channel_account_id, reason, detail, severity, last_seen_at)
			VALUES ($1, $2, $3, $4, 'blocking', now())
			ON CONFLICT (variant_id, channel_account_id, reason) DO UPDATE
			SET detail = EXCLUDED.detail, severity = 'blocking',
			    last_seen_at = now(), resolved_at = NULL, resolved_by = NULL`,
			f.VarianteID, cuentaID, MotivoBajoCosto, detalle); err != nil {
			return fmt.Errorf("marcando la variante %d por debajo de coste: %w", f.VarianteID, err)
		}
	}
	return tx.Commit(ctx)
}

// VariantesBajoCosto cuenta lo que se está publicando a pérdida ahora mismo.
// Es lo que la vigilancia convierte en aviso por correo.
func (s *Store) VariantesBajoCosto(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM attention_queue
		WHERE reason = $1 AND resolved_at IS NULL`, MotivoBajoCosto).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("contando precios por debajo de coste: %w", err)
	}
	return n, nil
}
