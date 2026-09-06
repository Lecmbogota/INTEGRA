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
	for _, l := range d.Lineas {
		_, err = tx.Exec(ctx, `
			INSERT INTO channel_order_lines
			    (channel_order_id, external_line_id, external_variant_id, channel_sku,
			     variant_id, title, quantity, unit_price, total_price)
			VALUES ($1,$2,$3,$4,
			        (SELECT v.id FROM product_variants v
			         WHERE lower(v.sku) = lower($4) AND v.active LIMIT 1),
			        $5,$6,$7,$8)`,
			id, nulo(l.ExternalID), nulo(l.VarianteExt), nulo(l.SKU),
			nulo(l.Titulo), l.Cantidad, l.PrecioUnit, l.Total)
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
	Total     int     `json:"total"`
	Recibidos int     `json:"recibidos"`
	EnOdoo    int     `json:"en_odoo"`
	Fallidos  int     `json:"fallidos"`
	SinMapear int     `json:"lineas_sin_mapear"`
	MontoHoy  float64 `json:"monto_hoy"`
}

func (s *Store) ResumenOrdenes(ctx context.Context) (*ResumenOrdenes, error) {
	var r ResumenOrdenes
	err := s.pool.QueryRow(ctx, `
		SELECT
		  (SELECT count(*) FROM channel_orders),
		  (SELECT count(*) FROM channel_orders WHERE status IN ('received','mapped')),
		  (SELECT count(*) FROM channel_orders WHERE status = 'created_in_odoo'),
		  (SELECT count(*) FROM channel_orders WHERE status = 'failed'),
		  (SELECT count(*) FROM channel_order_lines WHERE variant_id IS NULL),
		  (SELECT COALESCE(sum(total_amount),0) FROM channel_orders
		     WHERE ordered_at >= date_trunc('day', now()))
	`).Scan(&r.Total, &r.Recibidos, &r.EnOdoo, &r.Fallidos, &r.SinMapear, &r.MontoHoy)
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
