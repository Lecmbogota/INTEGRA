package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Bodegas por cuenta: qué almacenes de Odoo alimentan el stock que se publica
// en cada canal.
//
// channel_account_warehouses existe desde el primer esquema y la leen la
// publicación (CandidatosPublicacion) y el pedido (BodegaDeOrden,
// DescontarStockPublicado), pero no la escribía nada: sin pantalla ni
// endpoint ninguna cuenta tenía bodegas, y el valor por defecto de «sin filas
// = todas» hacía que cada canal publicara también Muestras, Garantías y el
// stock consignado en las tres bodegas de Falabella. WooCommerce ofrecía
// unidades que estaban en el centro de distribución de Falabella, y la venta
// había que cancelarla. Este fichero es lo único que rellena la tabla, y una
// cuenta sin filas ya no publica.

// ErrCuentaSinBodegas es la negativa a publicar una cuenta a la que nadie ha
// asignado bodegas. Lo devuelve CandidatosPublicacion; el planificador lo
// distingue para avisar y seguir con las demás cuentas en vez de detener la
// pasada entera.
var ErrCuentaSinBodegas = errors.New("no tiene bodegas asignadas: no se publica nada en ese canal hasta asignarlas en «Cuentas»")

// RechazoBodegas es una asignación que no se acepta por los datos de quien la
// pide, no por una avería: la API la devuelve con su motivo en vez de como
// error interno.
type RechazoBodegas struct{ Motivo string }

func (e *RechazoBodegas) Error() string { return e.Motivo }

// BodegaAsignable es una bodega de Odoo vista desde una cuenta: si la
// alimenta o no, y cuántas unidades tiene, para que quien asigna sepa qué
// está eligiendo.
type BodegaAsignable struct {
	ID       int64  `json:"id"`
	OdooID   int64  `json:"odoo_id"`
	Codigo   string `json:"codigo"`
	Nombre   string `json:"nombre"`
	Conexion string `json:"conexion"`
	Unidades int    `json:"unidades"`
	Asignada bool   `json:"asignada"`
}

// BodegasDeCuenta lista las bodegas de las conexiones activas de Odoo,
// marcando las que alimentan la cuenta.
func (s *Store) BodegasDeCuenta(ctx context.Context, cuentaID int64) ([]BodegaAsignable, error) {
	if err := existeCuenta(ctx, s.pool, cuentaID); err != nil {
		return nil, err
	}
	filas, err := s.pool.Query(ctx, `
		SELECT w.id, w.odoo_id, w.code, w.name, c.name,
		       COALESCE((SELECT sum(vs.qty_on_hand) FROM variant_stock vs
		                 WHERE vs.odoo_warehouse_id = w.id), 0)::int,
		       EXISTS (SELECT 1 FROM channel_account_warehouses caw
		               WHERE caw.channel_account_id = $1 AND caw.odoo_warehouse_id = w.id)
		FROM odoo_warehouses w
		JOIN odoo_connections c ON c.id = w.odoo_connection_id
		WHERE w.active AND c.active
		ORDER BY c.id, w.code`, cuentaID)
	if err != nil {
		return nil, fmt.Errorf("listando bodegas de la cuenta %d: %w", cuentaID, err)
	}
	defer filas.Close()

	var out []BodegaAsignable
	for filas.Next() {
		var b BodegaAsignable
		if err := filas.Scan(&b.ID, &b.OdooID, &b.Codigo, &b.Nombre, &b.Conexion,
			&b.Unidades, &b.Asignada); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, filas.Err()
}

// AsignarBodegas fija qué bodegas alimentan una cuenta reemplazando la
// asignación entera: lo que llega es la lista completa de casillas marcadas,
// no un delta.
//
// No se acepta dejar la cuenta sin ninguna. Una cuenta sin bodegas no publica
// nada, y llegar a ese estado desmarcando casillas sería parar un canal sin
// querer; el estado «sin asignar» solo existe para las cuentas recién
// conectadas, a las que alguien tiene que decidirles las bodegas a propósito.
func (s *Store) AsignarBodegas(ctx context.Context, cuentaID int64, bodegaIDs []int64) error {
	// Sin repetidos: la clave primaria los rechazaría con un error de la base
	// en vez de con uno que se entienda, y una casilla marcada dos veces no
	// es un error de quien asigna.
	ids := make([]int64, 0, len(bodegaIDs))
	vistas := make(map[int64]bool, len(bodegaIDs))
	for _, id := range bodegaIDs {
		if !vistas[id] {
			vistas[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return &RechazoBodegas{"hay que asignar al menos una bodega: una cuenta sin bodegas no publica nada"}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := existeCuenta(ctx, tx, cuentaID); err != nil {
		return err
	}
	// Solo bodegas vivas de conexiones activas: una conexión apagada ya no
	// recibe sync, y publicar desde sus bodegas sería publicar el stock
	// congelado en el momento en que se apagó.
	var validas int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM odoo_warehouses w
		JOIN odoo_connections c ON c.id = w.odoo_connection_id
		WHERE w.id = ANY($1) AND w.active AND c.active`, ids).Scan(&validas); err != nil {
		return fmt.Errorf("comprobando las bodegas: %w", err)
	}
	if validas != len(ids) {
		return &RechazoBodegas{"alguna de las bodegas no existe o su conexión de Odoo no está activa"}
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM channel_account_warehouses WHERE channel_account_id = $1`, cuentaID); err != nil {
		return fmt.Errorf("retirando la asignación anterior: %w", err)
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `
			INSERT INTO channel_account_warehouses (channel_account_id, odoo_warehouse_id)
			VALUES ($1, $2)`, cuentaID, id); err != nil {
			return fmt.Errorf("asignando la bodega %d: %w", id, err)
		}
	}
	return tx.Commit(ctx)
}

// consultor es lo que el pool y una transacción tienen en común, para que la
// comprobación de la cuenta valga dentro y fuera de la transacción.
type consultor interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func existeCuenta(ctx context.Context, q consultor, cuentaID int64) error {
	var existe bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_accounts WHERE id = $1)`, cuentaID).Scan(&existe); err != nil {
		return fmt.Errorf("comprobando la cuenta %d: %w", cuentaID, err)
	}
	if !existe {
		return &RechazoBodegas{fmt.Sprintf("no existe la cuenta %d", cuentaID)}
	}
	return nil
}
