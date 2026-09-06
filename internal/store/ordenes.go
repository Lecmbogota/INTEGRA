package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Orden es un pedido de un canal tal como lo guarda Integra.
type Orden struct {
	ID           int64        `json:"id"`
	CuentaID     int64        `json:"cuenta_id"`
	Canal        string       `json:"canal"`
	ExternalID   string       `json:"external_id"`
	Numero       string       `json:"numero"`
	EstadoCanal  string       `json:"estado_canal"`
	FechaPedido  time.Time    `json:"fecha_pedido"`
	Moneda       string       `json:"moneda"`
	Total        float64      `json:"total"`
	Envio        float64      `json:"envio"`
	Impuesto     float64      `json:"impuesto"`
	CompradorNom string       `json:"comprador"`
	Estado       string       `json:"estado"`
	OdooPedidoID *int64       `json:"odoo_pedido_id"`
	Error        string       `json:"error"`
	Intentos     int          `json:"intentos"`
	SincronAt    *time.Time   `json:"sincronizada_at"`
	Lineas       []LineaOrden `json:"lineas"`
}

// LineaOrden es una línea del pedido, ya emparejada con la variante local
// cuando el SKU se pudo reconocer.
type LineaOrden struct {
	ID         int64   `json:"id"`
	SKU        string  `json:"sku"`
	Titulo     string  `json:"titulo"`
	Cantidad   float64 `json:"cantidad"`
	PrecioUnit float64 `json:"precio_unitario"`
	Total      float64 `json:"total"`
	VarianteID *int64  `json:"variante_id"`
}

// DatosOrden es lo que entrega un adaptador para guardar.
type DatosOrden struct {
	CuentaID    int64
	ExternalID  string
	Numero      string
	EstadoCanal string
	FechaPedido time.Time
	Moneda      string
	Total       float64
	Envio       float64
	Impuesto    float64
	Comprador   string
	Documento   string
	Email       string
	Telefono    string
	Direccion   map[string]string
	Crudo       []byte
	Lineas      []DatosLinea
}

type DatosLinea struct {
	ExternalID  string
	VarianteExt string
	SKU         string
	Titulo      string
	Cantidad    float64
	PrecioUnit  float64
	Total       float64
}

// GuardarOrden inserta un pedido nuevo, o lo ignora si ya estaba.
//
// La idempotencia es lo esencial aquí: el sondeo relee ventanas solapadas y
// los webhooks se reenvían: sin la clave (cuenta, id externo) un mismo pedido
// acabaría creado varias veces en Odoo.
//
// Devuelve nuevo=false cuando el pedido ya existía.
func (s *Store) GuardarOrden(ctx context.Context, d DatosOrden) (id int64, nuevo bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	dir, err := json.Marshal(d.Direccion)
	if err != nil {
		return 0, false, err
	}
	crudo := d.Crudo
	if len(crudo) == 0 {
		crudo = []byte("{}")
	}

	// Si el pedido ya existía, solo se refresca lo que el canal puede haber
	// cambiado después de crearlo: su estado y la carga cruda. Las líneas, el
	// comprador y los importes se quedan como se ingirieron, que es lo que se
	// montó en Odoo. (xmax = 0) distingue la fila recién insertada de la
	// actualizada sin una segunda consulta.
	err = tx.QueryRow(ctx, `
		INSERT INTO channel_orders
		    (channel_account_id, external_order_id, external_number, channel_status,
		     ordered_at, currency, total_amount, shipping_amount, tax_amount,
		     buyer_name, buyer_document, buyer_email, buyer_phone,
		     shipping_address, raw_payload, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'received')
		ON CONFLICT (channel_account_id, external_order_id) DO UPDATE
		    SET channel_status = COALESCE(EXCLUDED.channel_status, channel_orders.channel_status),
		        raw_payload    = CASE WHEN EXCLUDED.raw_payload = '{}'::jsonb
		                              THEN channel_orders.raw_payload ELSE EXCLUDED.raw_payload END,
		        updated_at     = now()
		RETURNING id, (xmax = 0)`,
		d.CuentaID, d.ExternalID, nulo(d.Numero), nulo(d.EstadoCanal), d.FechaPedido,
		d.Moneda, d.Total, d.Envio, d.Impuesto,
		nulo(d.Comprador), nulo(d.Documento), nulo(d.Email), nulo(d.Telefono),
		dir, crudo).Scan(&id, &nuevo)
	if err != nil {
		return 0, false, fmt.Errorf("guardando el pedido %s: %w", d.ExternalID, err)
	}
	if !nuevo {
		return id, false, tx.Commit(ctx)
	}

	// Las líneas se emparejan por SKU con el catálogo. Una línea sin pareja se
	// guarda igual con variant_id nulo: perder el pedido sería peor que
	// tenerlo incompleto, y así se ve qué hay que arreglar.
	//
	// El SKU que manda el canal es el que tenía cuando se publicó, y ese no
	// cambia cuando alguien lo renombra en Odoo: ningún Update se lo lleva al
	// canal y en Falabella ni siquiera se puede. Por eso se busca primero
	// entre lo publicado en esta misma cuenta (channel_sku), que es lo que el
	// comprador vio, y solo después por el SKU actual de la variante. Mirar
	// solo el actual dejaba sin variante cada pedido de un producto
	// renombrado, y el montaje en Odoo lo agotaba en cinco intentos: una
	// venta cobrada que nunca llegaba.
	for _, l := range d.Lineas {
		_, err = tx.Exec(ctx, `
			INSERT INTO channel_order_lines
			    (channel_order_id, external_line_id, external_variant_id, channel_sku,
			     variant_id, title, quantity, unit_price, total_price)
			VALUES ($1,$2,$3,$4,
			        (SELECT v.id FROM product_variants v
			         LEFT JOIN variant_channel_listings vcl
			                ON vcl.variant_id = v.id AND vcl.channel_account_id = $9
			               AND lower(vcl.channel_sku) = lower($4)
			         WHERE v.active
			           AND (vcl.id IS NOT NULL OR lower(v.sku) = lower($4))
			         ORDER BY (vcl.id IS NOT NULL) DESC
			         LIMIT 1),
			        $5,$6,$7,$8)`,
			id, nulo(l.ExternalID), nulo(l.VarianteExt), nulo(l.SKU),
			nulo(l.Titulo), l.Cantidad, l.PrecioUnit, l.Total, d.CuentaID)
		if err != nil {
			return 0, false, fmt.Errorf("guardando línea de %s: %w", d.ExternalID, err)
		}
	}
	return id, true, tx.Commit(ctx)
}

// OrdenesPendientesOdoo lista lo que aún no se creó como pedido de venta.
func (s *Store) OrdenesPendientesOdoo(ctx context.Context, limite int) ([]Orden, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	filas, err := s.pool.Query(ctx, `
		SELECT o.id, o.channel_account_id, ch.code, o.external_order_id,
		       COALESCE(o.external_number,''), COALESCE(o.channel_status,''),
		       o.ordered_at, o.currency, o.total_amount, o.shipping_amount, o.tax_amount,
		       COALESCE(o.buyer_name,''), o.status::text, o.odoo_sale_order_id,
		       COALESCE(o.sync_error,''), o.sync_attempts, o.synced_at
		FROM channel_orders o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.status IN ('received','mapped','failed') AND o.sync_attempts < 5
		ORDER BY o.ordered_at
		LIMIT $1`, limite)
	if err != nil {
		return nil, fmt.Errorf("listando pedidos pendientes: %w", err)
	}
	defer filas.Close()

	var out []Orden
	for filas.Next() {
		var o Orden
		if err := filas.Scan(&o.ID, &o.CuentaID, &o.Canal, &o.ExternalID, &o.Numero,
			&o.EstadoCanal, &o.FechaPedido, &o.Moneda, &o.Total, &o.Envio, &o.Impuesto,
			&o.CompradorNom, &o.Estado, &o.OdooPedidoID, &o.Error, &o.Intentos, &o.SincronAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := filas.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Lineas, err = s.LineasDeOrden(ctx, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ReemparejarLineasHuerfanas vuelve a buscar la variante de las líneas que se
// guardaron sin ella, y devuelve al circuito los pedidos que fallaron por eso.
//
// El emparejamiento por SKU solo se hacía al insertar la línea. Un pedido de
// un producto que aún no se había sincronizado quedaba con variant_id nulo,
// fallaba cinco veces al montarse en Odoo y desaparecía de la cola de
// pendientes para siempre: ni sincronizar el catálogo después lo rescataba,
// porque las líneas de un pedido ya existente no se vuelven a tocar y el
// contador de intentos solo sube. Era un pedido cobrado que nunca llegaba a
// Odoo, sin más rastro que una alerta.
//
// Devuelve cuántas líneas se emparejaron y cuántos pedidos se reactivaron.
func (s *Store) ReemparejarLineasHuerfanas(ctx context.Context) (lineas, pedidos int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// El mismo criterio que al insertar la línea (ver GuardarOrden): primero
	// el SKU con el que esta cuenta publicó la variante, después el actual.
	// Sin la primera parte, un pedido de un SKU renombrado en Odoo no se
	// rescataba nunca: el catálogo ya no tiene el SKU que manda el canal.
	tag, err := tx.Exec(ctx, `
		UPDATE channel_order_lines l
		SET variant_id = e.variant_id
		FROM (
		    SELECT h.id,
		           (SELECT v.id FROM product_variants v
		            LEFT JOIN variant_channel_listings vcl
		                   ON vcl.variant_id = v.id
		                  AND vcl.channel_account_id = o.channel_account_id
		                  AND lower(vcl.channel_sku) = lower(h.channel_sku)
		            WHERE v.active
		              AND (vcl.id IS NOT NULL OR lower(v.sku) = lower(h.channel_sku))
		            ORDER BY (vcl.id IS NOT NULL) DESC
		            LIMIT 1) AS variant_id
		    FROM channel_order_lines h
		    JOIN channel_orders o ON o.id = h.channel_order_id
		    WHERE h.variant_id IS NULL AND h.channel_sku IS NOT NULL
		) e
		WHERE e.id = l.id AND e.variant_id IS NOT NULL`)
	if err != nil {
		return 0, 0, fmt.Errorf("reemparejando líneas de pedido: %w", err)
	}
	lineas = int(tag.RowsAffected())

	// Solo se reactiva lo que falló por falta de mapeo y ya no le falta: un
	// pedido con líneas todavía sin variante seguiría fallando igual, y
	// reiniciarle los intentos lo dejaría girando en la cola sin avanzar.
	tag, err = tx.Exec(ctx, `
		UPDATE channel_orders o
		SET status = 'received', sync_attempts = 0, sync_error = NULL, updated_at = now()
		WHERE o.status = 'failed'
		  AND o.odoo_sale_order_id IS NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM channel_order_lines l
		      WHERE l.channel_order_id = o.id AND l.variant_id IS NULL)`)
	if err != nil {
		return 0, 0, fmt.Errorf("reactivando pedidos emparejados: %w", err)
	}
	pedidos = int(tag.RowsAffected())

	return lineas, pedidos, tx.Commit(ctx)
}

// OrdenPendientePorID devuelve un pedido concreto si sigue pendiente de
// montarse en Odoo, o nil si ya se creó, se descartó o agotó los intentos.
//
// Existe aparte de OrdenesPendientesOdoo porque aquella corta a un límite:
// buscar dentro de esa lista hace que un pedido que quede fuera del corte se
// dé por montado sin haberse creado nunca.
func (s *Store) OrdenPendientePorID(ctx context.Context, id int64) (*Orden, error) {
	var o Orden
	err := s.pool.QueryRow(ctx, `
		SELECT o.id, o.channel_account_id, ch.code, o.external_order_id,
		       COALESCE(o.external_number,''), COALESCE(o.channel_status,''),
		       o.ordered_at, o.currency, o.total_amount, o.shipping_amount, o.tax_amount,
		       COALESCE(o.buyer_name,''), o.status::text, o.odoo_sale_order_id,
		       COALESCE(o.sync_error,''), o.sync_attempts, o.synced_at
		FROM channel_orders o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.id = $1 AND o.status IN ('received','mapped','failed') AND o.sync_attempts < 5`, id).
		Scan(&o.ID, &o.CuentaID, &o.Canal, &o.ExternalID, &o.Numero,
			&o.EstadoCanal, &o.FechaPedido, &o.Moneda, &o.Total, &o.Envio, &o.Impuesto,
			&o.CompradorNom, &o.Estado, &o.OdooPedidoID,
			&o.Error, &o.Intentos, &o.SincronAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("leyendo el pedido %d: %w", id, err)
	}
	lineas, err := s.LineasDeOrden(ctx, o.ID)
	if err != nil {
		return nil, err
	}
	o.Lineas = lineas
	return &o, nil
}

func (s *Store) LineasDeOrden(ctx context.Context, ordenID int64) ([]LineaOrden, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT id, COALESCE(channel_sku,''), COALESCE(title,''),
		       quantity, unit_price, total_price, variant_id
		FROM channel_order_lines WHERE channel_order_id = $1 ORDER BY id`, ordenID)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var out []LineaOrden
	for filas.Next() {
		var l LineaOrden
		if err := filas.Scan(&l.ID, &l.SKU, &l.Titulo, &l.Cantidad,
			&l.PrecioUnit, &l.Total, &l.VarianteID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, filas.Err()
}

// MarcarOrdenCreada anota el pedido de venta creado en Odoo.
func (s *Store) MarcarOrdenCreada(ctx context.Context, ordenID, odooPedidoID, odooPartnerID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_orders
		SET status = 'created_in_odoo', odoo_sale_order_id = $2, odoo_partner_id = $3,
		    sync_error = NULL, synced_at = now(), updated_at = now()
		WHERE id = $1`, ordenID, odooPedidoID, odooPartnerID)
	return err
}

// MarcarOrdenFallida cuenta el intento y guarda la causa.
func (s *Store) MarcarOrdenFallida(ctx context.Context, ordenID int64, causa string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_orders
		SET status = 'failed', sync_error = $2,
		    sync_attempts = sync_attempts + 1, updated_at = now()
		WHERE id = $1`, ordenID, causa)
	return err
}

// ListarOrdenes alimenta la pantalla de pedidos.
func (s *Store) ListarOrdenes(ctx context.Context, limite int) ([]Orden, error) {
	if limite <= 0 || limite > 200 {
		limite = 50
	}
	filas, err := s.pool.Query(ctx, `
		SELECT o.id, o.channel_account_id, ch.code, o.external_order_id,
		       COALESCE(o.external_number,''), COALESCE(o.channel_status,''),
		       o.ordered_at, o.currency, o.total_amount, o.shipping_amount, o.tax_amount,
		       COALESCE(o.buyer_name,''), o.status::text, o.odoo_sale_order_id,
		       COALESCE(o.sync_error,''), o.sync_attempts, o.synced_at
		FROM channel_orders o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		ORDER BY o.ordered_at DESC
		LIMIT $1`, limite)
	if err != nil {
		return nil, fmt.Errorf("listando pedidos: %w", err)
	}
	defer filas.Close()

	var out []Orden
	for filas.Next() {
		var o Orden
		if err := filas.Scan(&o.ID, &o.CuentaID, &o.Canal, &o.ExternalID, &o.Numero,
			&o.EstadoCanal, &o.FechaPedido, &o.Moneda, &o.Total, &o.Envio, &o.Impuesto,
			&o.CompradorNom, &o.Estado, &o.OdooPedidoID, &o.Error, &o.Intentos, &o.SincronAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, filas.Err()
}

// ResumenOrdenes cuenta el estado de la ingesta para el panel.
type ResumenOrdenes struct {
	Total     int `json:"total"`
	Recibidos int `json:"recibidos"`
	EnOdoo    int `json:"en_odoo"`
	Fallidos  int `json:"fallidos"`
	// Cancelados por el canal. Sin este contador, un pedido cancelado
	// desaparecía del desglose: contaba en el total y en ningún estado.
	Cancelados int     `json:"cancelados"`
	SinMapear  int     `json:"lineas_sin_mapear"`
	MontoHoy   float64 `json:"monto_hoy"`
}

func (s *Store) ResumenOrdenes(ctx context.Context) (*ResumenOrdenes, error) {
	var r ResumenOrdenes
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM channel_orders),
		  (SELECT count(*) FROM channel_orders WHERE status IN ('received','mapped')),
		  (SELECT count(*) FROM channel_orders WHERE status = 'created_in_odoo'),
		  (SELECT count(*) FROM channel_orders WHERE status = 'failed'),
		  (SELECT count(*) FROM channel_orders WHERE status = 'ignored'),
		  (SELECT count(*) FROM channel_order_lines WHERE variant_id IS NULL),
		  (SELECT COALESCE(sum(total_amount),0) FROM channel_orders
		     WHERE ordered_at >= date_trunc('day', now()))
	`).Scan(&r.Total, &r.Recibidos, &r.EnOdoo, &r.Fallidos, &r.Cancelados, &r.SinMapear, &r.MontoHoy)
	if err != nil {
		return nil, fmt.Errorf("resumen de pedidos: %w", err)
	}
	return &r, nil
}

// DatosComprador es lo que hace falta para crear el cliente en Odoo.
type DatosComprador struct {
	Nombre    string
	Email     string
	Telefono  string
	Documento string
	Ciudad    string
	Direccion string
}

func (s *Store) DatosCompradorDeOrden(ctx context.Context, ordenID int64) (*DatosComprador, error) {
	var d DatosComprador
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(buyer_name,''), COALESCE(buyer_email,''), COALESCE(buyer_phone,''),
		       COALESCE(buyer_document,''),
		       COALESCE(shipping_address->>'ciudad',''),
		       COALESCE(shipping_address->>'linea1','')
		FROM channel_orders WHERE id = $1`, ordenID).
		Scan(&d.Nombre, &d.Email, &d.Telefono, &d.Documento, &d.Ciudad, &d.Direccion)
	if err != nil {
		return nil, fmt.Errorf("leyendo el comprador del pedido %d: %w", ordenID, err)
	}
	return &d, nil
}

// OdooProductIDDeVariante devuelve el product.product de Odoo de una variante.
func (s *Store) OdooProductIDDeVariante(ctx context.Context, varianteID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT odoo_product_id FROM product_variants WHERE id = $1`, varianteID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("no existe la variante %d", varianteID)
	}
	return id, err
}

// WatermarkOrdenes devuelve desde cuándo pedir pedidos a un canal.
func (s *Store) WatermarkOrdenes(ctx context.Context, cuentaID int64) (time.Time, error) {
	var t *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT watermark FROM order_ingest_state WHERE channel_account_id = $1`, cuentaID).Scan(&t)
	if err == pgx.ErrNoRows || t == nil {
		return time.Time{}, nil
	}
	return *t, err
}

// ActualizarWatermarkOrdenes avanza la marca de agua de ingesta.
//
// Se retrasa un minuto a propósito: los canales indexan con retardo y una
// marca exacta se saltaría pedidos creados en el mismo segundo del corte.
func (s *Store) ActualizarWatermarkOrdenes(ctx context.Context, cuentaID int64, hasta time.Time, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO order_ingest_state (channel_account_id, watermark, last_run_at, last_error)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (channel_account_id) DO UPDATE
		SET watermark = EXCLUDED.watermark, last_run_at = now(),
		    last_error = EXCLUDED.last_error, updated_at = now()`,
		cuentaID, hasta.Add(-time.Minute), nulo(errMsg))
	return err
}

// ------------------------------------------------------ bodega de la cuenta

// BodegaCuenta es una bodega de Odoo asignada a una cuenta de canal.
type BodegaCuenta struct {
	ID     int64  `json:"id"`      // id local en odoo_warehouses
	OdooID int64  `json:"odoo_id"` // id en Odoo: el que va en warehouse_id
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
}

// BodegaDeOrden elige de qué bodega debe salir un pedido.
//
// channel_account_warehouses existe desde el primer esquema y hasta ahora solo
// la leía la publicación (para no ofrecer en Falabella stock que no está en
// sus bodegas). El pedido, en cambio, se creaba sin warehouse_id, así que Odoo
// lo despachaba de la bodega por defecto: una venta de Falabella descontaba de
// la principal y dejaba el stock consignado en FB intacto, que es justo la
// mentira que la tabla existe para evitar.
//
// Cuando la cuenta tiene varias bodegas (Falabella tiene tres) Odoo solo
// acepta una en el pedido, así que se prefiere la que más unidades del pedido
// puede cubrir; a igualdad, la que más existencias tenga, y como último
// desempate el código, para que la elección sea reproducible.
//
// Devuelve nil sin error si la cuenta no tiene bodegas asignadas: entonces el
// pedido va sin warehouse_id y decide Odoo, que es lo que pasa hoy y no una
// regresión.
func (s *Store) BodegaDeOrden(ctx context.Context, ordenID int64) (*BodegaCuenta, error) {
	var b BodegaCuenta
	err := s.pool.QueryRow(ctx, `
		SELECT w.id, w.odoo_id, w.code, w.name
		FROM channel_orders o
		JOIN channel_account_warehouses caw ON caw.channel_account_id = o.channel_account_id
		JOIN odoo_warehouses w ON w.id = caw.odoo_warehouse_id AND w.active
		LEFT JOIN LATERAL (
		    -- Unidades del pedido que esta bodega cubre, y existencias totales.
		    -- GREATEST(...,0) descarta el stock negativo: una bodega en números
		    -- rojos no debe ganar el desempate.
		    SELECT COALESCE(sum(LEAST(l.quantity, GREATEST(COALESCE(vs.qty_on_hand,0),0))),0) AS cubre,
		           COALESCE(sum(GREATEST(COALESCE(vs.qty_on_hand,0),0)),0) AS total
		    FROM channel_order_lines l
		    LEFT JOIN variant_stock vs
		           ON vs.variant_id = l.variant_id AND vs.odoo_warehouse_id = w.id
		    WHERE l.channel_order_id = o.id
		) c ON TRUE
		WHERE o.id = $1
		ORDER BY c.cubre DESC, c.total DESC, w.code
		LIMIT 1`, ordenID).Scan(&b.ID, &b.OdooID, &b.Codigo, &b.Nombre)
	if err == pgx.ErrNoRows {
		return nil, nil // sin bodegas asignadas: decide Odoo
	}
	if err != nil {
		return nil, fmt.Errorf("eligiendo bodega del pedido %d: %w", ordenID, err)
	}
	return &b, nil
}

// ------------------------------------- descuento inmediato del stock vendido

// ReservaStock es una cantidad apartada por un pedido ya ingerido y todavía no
// reflejado en el stock que Odoo reporta.
type ReservaStock struct {
	VarianteID int64   `json:"variante_id"`
	BodegaID   int64   `json:"bodega_id"`
	Cantidad   float64 `json:"cantidad"`
}

// DescontarStockPublicado baja el stock de las variantes vendidas en el acto,
// sin esperar al siguiente sync con Odoo, y deja asiento de lo descontado.
//
// El problema que resuelve es una ventana de sobreventa: entre la venta en un
// canal y el siguiente sync, los otros tres canales siguen ofreciendo unidades
// que ya no existen. El pedido además se crea en Odoo en borrador a propósito,
// así que Odoo tampoco baja su stock hasta que una persona lo confirma y lo
// despacha: la ventana no dura minutos, puede durar días.
//
// Se descuenta sobre variant_stock porque es de donde sale el stock publicable
// (CandidatosPublicacion) y por tanto lo único que hace que el motor de diff
// mande la cantidad nueva a los demás canales. Y como el sync reemplaza esa
// tabla entera, el descuento se asienta aparte en order_stock_reservations:
// ese libro es lo que permite volver a aplicarlo después de cada sync
// (ReaplicarReservasDeStock) y devolverlo exacto si el canal cancela.
//
// Es idempotente: un pedido que ya tiene asientos no se descuenta dos veces.
// Devuelve las unidades efectivamente descontadas.
func (s *Store) DescontarStockPublicado(ctx context.Context, ordenID int64) (float64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var cuentaID int64
	var estado string
	err = tx.QueryRow(ctx,
		`SELECT channel_account_id, status::text FROM channel_orders WHERE id = $1 FOR UPDATE`,
		ordenID).Scan(&cuentaID, &estado)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("no existe el pedido %d", ordenID)
	}
	if err != nil {
		return 0, err
	}
	// Un pedido que llegó ya cancelado no aparta nada: no hay venta viva.
	if estado == "ignored" {
		return 0, tx.Commit(ctx)
	}

	var asientos int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM order_stock_reservations WHERE channel_order_id = $1`,
		ordenID).Scan(&asientos); err != nil {
		return 0, fmt.Errorf("leyendo reservas del pedido %d: %w", ordenID, err)
	}
	if asientos > 0 {
		return 0, tx.Commit(ctx) // ya se descontó en una pasada anterior
	}

	type lineaPendiente struct {
		id       int64
		variante int64
		cantidad float64
	}
	filas, err := tx.Query(ctx, `
		SELECT id, variant_id, quantity FROM channel_order_lines
		WHERE channel_order_id = $1 AND variant_id IS NOT NULL ORDER BY id`, ordenID)
	if err != nil {
		return 0, err
	}
	var lineas []lineaPendiente
	for filas.Next() {
		var l lineaPendiente
		if err := filas.Scan(&l.id, &l.variante, &l.cantidad); err != nil {
			filas.Close()
			return 0, err
		}
		lineas = append(lineas, l)
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		return 0, err
	}

	var total float64
	for _, l := range lineas {
		if l.cantidad <= 0 {
			continue
		}
		// Solo las bodegas que alimentan esta cuenta, y todas si no tiene
		// ninguna asignada: es el mismo criterio con el que se calculó el
		// stock que se publicó, así que se descuenta de donde se ofreció.
		// FOR UPDATE serializa dos pedidos simultáneos de la misma variante.
		bodegas, err := tx.Query(ctx, `
			SELECT vs.odoo_warehouse_id, vs.qty_on_hand
			FROM variant_stock vs
			WHERE vs.variant_id = $1
			  AND vs.qty_on_hand > 0
			  AND (NOT EXISTS (SELECT 1 FROM channel_account_warehouses w
			                   WHERE w.channel_account_id = $2)
			       OR vs.odoo_warehouse_id IN (
			           SELECT w.odoo_warehouse_id FROM channel_account_warehouses w
			           WHERE w.channel_account_id = $2))
			ORDER BY vs.qty_on_hand DESC, vs.odoo_warehouse_id
			FOR UPDATE`, l.variante, cuentaID)
		if err != nil {
			return 0, err
		}
		type existencia struct {
			bodega int64
			qty    float64
		}
		var disponibles []existencia
		for bodegas.Next() {
			var e existencia
			if err := bodegas.Scan(&e.bodega, &e.qty); err != nil {
				bodegas.Close()
				return 0, err
			}
			disponibles = append(disponibles, e)
		}
		bodegas.Close()
		if err := bodegas.Err(); err != nil {
			return 0, err
		}

		// Se reparte de mayor a menor y nunca por debajo de cero: si el canal
		// vendió más de lo que había, lo que sobra ya se sobrevendió y bajar a
		// negativo solo lograría publicar cantidades negativas.
		restante := l.cantidad
		for _, e := range disponibles {
			if restante <= 0 {
				break
			}
			toma := e.qty
			if toma > restante {
				toma = restante
			}
			if _, err := tx.Exec(ctx, `
				UPDATE variant_stock SET qty_on_hand = qty_on_hand - $3, updated_at = now()
				WHERE variant_id = $1 AND odoo_warehouse_id = $2`,
				l.variante, e.bodega, toma); err != nil {
				return 0, fmt.Errorf("descontando stock de la variante %d: %w", l.variante, err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO order_stock_reservations
				    (channel_order_id, channel_order_line_id, variant_id, odoo_warehouse_id, qty)
				VALUES ($1,$2,$3,$4,$5)`,
				ordenID, l.id, l.variante, e.bodega, toma); err != nil {
				return 0, fmt.Errorf("asentando la reserva de la variante %d: %w", l.variante, err)
			}
			restante -= toma
			total += toma
		}
	}
	return total, tx.Commit(ctx)
}

// DevolverStockReservado deshace el descuento de un pedido y cierra sus
// asientos. Se llama cuando el canal cancela: las unidades vuelven a estar a
// la venta en los cuatro canales sin esperar al sync.
//
// El alta usa upsert porque entre el descuento y la devolución puede haber
// corrido un sync que borró la fila de esa variante y bodega.
func (s *Store) DevolverStockReservado(ctx context.Context, ordenID int64, motivo string) (float64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	filas, err := tx.Query(ctx, `
		UPDATE order_stock_reservations
		SET released_at = now(), released_reason = $2
		WHERE channel_order_id = $1 AND released_at IS NULL
		RETURNING variant_id, odoo_warehouse_id, qty`, ordenID, nulo(motivo))
	if err != nil {
		return 0, fmt.Errorf("liberando reservas del pedido %d: %w", ordenID, err)
	}
	var reservas []ReservaStock
	for filas.Next() {
		var r ReservaStock
		if err := filas.Scan(&r.VarianteID, &r.BodegaID, &r.Cantidad); err != nil {
			filas.Close()
			return 0, err
		}
		reservas = append(reservas, r)
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		return 0, err
	}

	var total float64
	for _, r := range reservas {
		if _, err := tx.Exec(ctx, `
			INSERT INTO variant_stock (variant_id, odoo_warehouse_id, qty_on_hand, updated_at)
			VALUES ($1,$2,$3, now())
			ON CONFLICT (variant_id, odoo_warehouse_id) DO UPDATE
			SET qty_on_hand = variant_stock.qty_on_hand + EXCLUDED.qty_on_hand,
			    updated_at = now()`, r.VarianteID, r.BodegaID, r.Cantidad); err != nil {
			return 0, fmt.Errorf("devolviendo stock de la variante %d: %w", r.VarianteID, err)
		}
		total += r.Cantidad
	}
	return total, tx.Commit(ctx)
}

// ReaplicarReservasDeStock vuelve a descontar del stock recién traído de Odoo
// lo que sigue apartado por pedidos vivos.
//
// Existe porque el sync reemplaza variant_stock entero: sin esta pasada, cada
// sincronización resucitaría las unidades vendidas mientras el pedido siga en
// borrador en Odoo (que es donde se deja a propósito), y la ventana de
// sobreventa volvería a abrirse sola cada pocas horas.
//
// Va justo después de ReemplazarStock, en la misma pasada del sync.
//
// Antes de reaplicar cierra los asientos vencidos. Sin ese cierre, un pedido
// ya despachado seguiría restando por encima del descuento que Odoo ya hizo, y
// el stock publicado se hundiría una unidad por venta sin retorno. El
// vencimiento es una válvula de seguridad, no la regla buena: la regla buena
// es cerrar cuando Odoo mueva la unidad, y eso exige seguir el estado del
// sale.order, que está pendiente de decidir (ver migración 020).
func (s *Store) ReaplicarReservasDeStock(ctx context.Context) (int, error) {
	if _, err := s.pool.Exec(ctx, `
		UPDATE order_stock_reservations
		SET released_at = now(), released_reason = 'vencida: se supone despachada en Odoo'
		WHERE released_at IS NULL AND expires_at <= now()`); err != nil {
		return 0, fmt.Errorf("cerrando reservas vencidas: %w", err)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE variant_stock vs
		SET qty_on_hand = GREATEST(vs.qty_on_hand - r.total, 0), updated_at = now()
		FROM (SELECT variant_id, odoo_warehouse_id, sum(qty) AS total
		      FROM order_stock_reservations
		      WHERE released_at IS NULL
		      GROUP BY variant_id, odoo_warehouse_id) r
		WHERE vs.variant_id = r.variant_id AND vs.odoo_warehouse_id = r.odoo_warehouse_id`)
	if err != nil {
		return 0, fmt.Errorf("reaplicando reservas de stock: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// ReservasAbiertasDeOrden lista lo que un pedido tiene apartado.
func (s *Store) ReservasAbiertasDeOrden(ctx context.Context, ordenID int64) ([]ReservaStock, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT variant_id, odoo_warehouse_id, qty
		FROM order_stock_reservations
		WHERE channel_order_id = $1 AND released_at IS NULL
		ORDER BY variant_id, odoo_warehouse_id`, ordenID)
	if err != nil {
		return nil, err
	}
	defer filas.Close()

	var out []ReservaStock
	for filas.Next() {
		var r ReservaStock
		if err := filas.Scan(&r.VarianteID, &r.BodegaID, &r.Cantidad); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, filas.Err()
}

// ------------------------------------------------------------ cancelaciones

// Cancelacion es el resultado de marcar cancelado un pedido.
type Cancelacion struct {
	// Cambio es falso si el pedido ya estaba cancelado: evita repetir la
	// devolución de stock y la alerta en cada pasada del sondeo.
	Cambio       bool
	Numero       string
	Canal        string
	CuentaID     int64
	OdooPedidoID *int64
}

// MarcarOrdenCancelada refleja que el canal canceló un pedido ya ingerido.
//
// Hasta ahora un pedido cancelado después de ingerido seguía contando como
// venta viva: si aún no se había montado, se creaba igual en Odoo, y si ya
// estaba montado nadie se enteraba.
//
// A propósito NO se toca el sale.order de Odoo. Cancelarlo mueve reservas y,
// si alguien ya lo confirmó o facturó, contabilidad: esa decisión es de una
// persona, igual que la confirmación. Lo que hace esta función es sacarlo de
// la cola de montaje, dejar el motivo en sync_error y devolver el id de Odoo
// para que quien avise pueda nombrarlo.
func (s *Store) MarcarOrdenCancelada(ctx context.Context, ordenID int64, estadoCanal string) (Cancelacion, error) {
	var c Cancelacion
	err := s.pool.QueryRow(ctx, `
		UPDATE channel_orders o
		SET status = 'ignored',
		    channel_status = COALESCE($2, o.channel_status),
		    sync_error = $3,
		    updated_at = now()
		WHERE o.id = $1 AND o.status <> 'ignored'
		RETURNING o.channel_account_id, COALESCE(o.external_number, o.external_order_id),
		          o.odoo_sale_order_id,
		          (SELECT ch.code FROM channel_accounts a
		             JOIN channels ch ON ch.id = a.channel_id
		            WHERE a.id = o.channel_account_id)`,
		ordenID, nulo(estadoCanal), "cancelado en el canal"+entreParentesis(estadoCanal)).
		Scan(&c.CuentaID, &c.Numero, &c.OdooPedidoID, &c.Canal)
	if err == pgx.ErrNoRows {
		return Cancelacion{}, nil // ya estaba cancelado
	}
	if err != nil {
		return Cancelacion{}, fmt.Errorf("cancelando el pedido %d: %w", ordenID, err)
	}
	c.Cambio = true
	return c, nil
}

func entreParentesis(s string) string {
	if s == "" {
		return ""
	}
	return " (" + s + ")"
}
