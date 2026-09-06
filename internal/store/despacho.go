package store

import (
	"context"
	"fmt"
	"time"
)

// Despacho: confirmar al canal que el pedido salió.
//
// Es el otro extremo del ciclo. Sin esto, Integra sabe vender y sabe montar el
// pedido en Odoo, pero el marketplace se queda esperando: MercadoLibre y
// Falabella miden el tiempo hasta el despacho y, pasado el plazo, cancelan,
// reembolsan al comprador y bajan la reputación del vendedor.

// OrdenPorDespachar es un pedido montado en Odoo al que todavía no se le ha
// confirmado el despacho al canal.
type OrdenPorDespachar struct {
	ID             int64
	CuentaID       int64
	Canal          string
	ExternalID     string
	Numero         string
	OdooPedidoID   int64
	Guia           string
	Transportadora string
	OdooDoneAt     *time.Time
	Intentos       int
}

// OrdenesPorDespachar devuelve los pedidos que ya están en Odoo y a los que no
// se ha confirmado el despacho.
//
// Se excluyen los cancelados —que aquí se marcan como 'ignored'—: confirmar el
// despacho de un pedido que el canal ya anuló haría que el marketplace
// esperara una mercancía que nadie va a mandar.
func (s *Store) OrdenesPorDespachar(ctx context.Context, limite int) ([]OrdenPorDespachar, error) {
	if limite <= 0 || limite > 500 {
		limite = 100
	}
	filas, err := s.pool.Query(ctx, `
		SELECT o.id, o.channel_account_id, ch.code, o.external_order_id,
		       COALESCE(o.external_number, ''), o.odoo_sale_order_id,
		       COALESCE(o.tracking_number, ''), COALESCE(o.carrier, ''),
		       o.odoo_done_at, o.dispatch_attempts
		FROM channel_orders o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.odoo_sale_order_id IS NOT NULL
		  AND o.dispatched_at IS NULL
		  AND o.status = 'created_in_odoo'
		  AND a.active
		ORDER BY o.ordered_at
		LIMIT $1`, limite)
	if err != nil {
		return nil, fmt.Errorf("listando pedidos por despachar: %w", err)
	}
	defer filas.Close()

	out := []OrdenPorDespachar{}
	for filas.Next() {
		var o OrdenPorDespachar
		if err := filas.Scan(&o.ID, &o.CuentaID, &o.Canal, &o.ExternalID, &o.Numero,
			&o.OdooPedidoID, &o.Guia, &o.Transportadora, &o.OdooDoneAt, &o.Intentos); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, filas.Err()
}

// OrdenPorDespacharID trae uno concreto, para el despacho a mano desde la
// pantalla de Pedidos.
func (s *Store) OrdenPorDespacharID(ctx context.Context, id int64) (*OrdenPorDespachar, error) {
	var o OrdenPorDespachar
	err := s.pool.QueryRow(ctx, `
		SELECT o.id, o.channel_account_id, ch.code, o.external_order_id,
		       COALESCE(o.external_number, ''), COALESCE(o.odoo_sale_order_id, 0),
		       COALESCE(o.tracking_number, ''), COALESCE(o.carrier, ''),
		       o.odoo_done_at, o.dispatch_attempts
		FROM channel_orders o
		JOIN channel_accounts a ON a.id = o.channel_account_id
		JOIN channels ch ON ch.id = a.channel_id
		WHERE o.id = $1`, id).
		Scan(&o.ID, &o.CuentaID, &o.Canal, &o.ExternalID, &o.Numero,
			&o.OdooPedidoID, &o.Guia, &o.Transportadora, &o.OdooDoneAt, &o.Intentos)
	if err != nil {
		return nil, fmt.Errorf("leyendo el pedido %d: %w", id, err)
	}
	return &o, nil
}

// GuardarGuia anota la guía y la transportadora que escribió el operador.
//
// Se guarda antes de llamar al canal y no dentro de la confirmación: si la
// llamada falla, el dato tecleado no se pierde y el reintento lo usa sin que
// nadie tenga que volver a escribirlo.
func (s *Store) GuardarGuia(ctx context.Context, ordenID int64, guia, transportadora string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE channel_orders
		SET tracking_number = NULLIF($2, ''), carrier = NULLIF($3, ''), updated_at = now()
		WHERE id = $1`, ordenID, guia, transportadora)
	if err != nil {
		return fmt.Errorf("guardando la guía del pedido %d: %w", ordenID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no existe el pedido %d", ordenID)
	}
	return nil
}

// MarcarSalidaDeBodega anota que Odoo validó el albarán.
//
// Es distinto de haber avisado al canal, y por eso son dos columnas: entre que
// la mercancía sale y que el marketplace se entera puede fallar la llamada, y
// mezclarlo haría creer que el canal ya lo sabe.
func (s *Store) MarcarSalidaDeBodega(ctx context.Context, ordenID int64, cuando time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_orders SET odoo_done_at = $2, updated_at = now()
		WHERE id = $1 AND odoo_done_at IS NULL`, ordenID, cuando)
	return err
}

// MarcarDespachoConfirmado sella que el canal ya lo sabe.
func (s *Store) MarcarDespachoConfirmado(ctx context.Context, ordenID int64, guia, transportadora string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE channel_orders
		SET dispatched_at = now(),
		    tracking_number = COALESCE(NULLIF($2, ''), tracking_number),
		    carrier = COALESCE(NULLIF($3, ''), carrier),
		    dispatch_error = NULL, updated_at = now()
		WHERE id = $1`, ordenID, guia, transportadora)
	if err != nil {
		return fmt.Errorf("marcando el despacho del pedido %d: %w", ordenID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no existe el pedido %d", ordenID)
	}
	return nil
}

// AnotarFalloDespacho deja constancia de por qué no se pudo avisar al canal.
//
// El contador sube para que la pantalla distinga «acaba de fallar» de «lleva
// veinte intentos fallando», que es la diferencia entre esperar y actuar.
func (s *Store) AnotarFalloDespacho(ctx context.Context, ordenID int64, causa string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE channel_orders
		SET dispatch_error = $2, dispatch_attempts = dispatch_attempts + 1, updated_at = now()
		WHERE id = $1`, ordenID, causa)
	return err
}
