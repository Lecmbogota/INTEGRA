// Package store es la capa de acceso a PostgreSQL.
//
// Se escribe SQL a mano en vez de usar un ORM: la tabla de publicaciones por
// variante y cuenta crece con el producto de catálogo × cuentas, y ahí conviene
// controlar exactamente qué consulta se ejecuta.
package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ pool *pgxpool.Pool }

// New abre el pool de conexiones.
func New(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("cadena de conexión inválida: %w", err)
	}
	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("abriendo el pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL no responde: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// ------------------------------------------------------------------ marcas

// NormalizarMarca reduce un literal de marca a su forma canónica.
//
// En Odoo conviven "RingConn", "Ringconn" y "RINGCONN" como valores distintos
// del mismo campo. Sin normalizar, la misma marca aparecería tres veces.
func NormalizarMarca(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// ResolverMarca traduce un literal de Odoo a una marca, creándola si es nueva.
//
// Crear la marca al vuelo es deliberado: obligar a darlas de alta a mano antes
// de la primera sincronización significaría que nada se sincroniza hasta que
// alguien teclee treinta nombres. Se crean y luego se depuran desde la interfaz.
func (s *Store) ResolverMarca(ctx context.Context, literal string) (int64, error) {
	norm := NormalizarMarca(literal)
	if norm == "" {
		return 0, nil
	}

	var id int64
	err := s.pool.QueryRow(ctx,
		`SELECT brand_id FROM brand_aliases WHERE alias_norm = $1`, norm).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return 0, fmt.Errorf("buscando alias de marca: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// El código de marca es la forma normalizada; el nombre visible es el
	// primer literal que apareció, que suele ser el mejor escrito.
	err = tx.QueryRow(ctx, `
		INSERT INTO brands (code, name) VALUES ($1, $2)
		ON CONFLICT (code) DO UPDATE SET updated_at = now()
		RETURNING id`, norm, strings.TrimSpace(literal)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("creando marca %q: %w", literal, err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO brand_aliases (brand_id, alias_norm, alias_raw)
		VALUES ($1, $2, $3) ON CONFLICT (alias_norm) DO NOTHING`,
		id, norm, strings.TrimSpace(literal))
	if err != nil {
		return 0, fmt.Errorf("creando alias %q: %w", literal, err)
	}

	return id, tx.Commit(ctx)
}

// -------------------------------------------------------------- conexiones

// GuardarConexionOdoo da de alta o actualiza una conexión. La clave llega ya
// cifrada: este paquete nunca ve credenciales en claro.
//
// La conexión guardada pasa a ser la ÚNICA activa. Integra sincroniza contra
// un solo Odoo, y sin esto conectar una instancia nueva dejaba la anterior
// activa: como ConexionOdooActiva toma la de menor id, se seguía leyendo de
// la vieja y el cambio no surtía efecto.
func (s *Store) GuardarConexionOdoo(ctx context.Context, nombre, url, db, usuario string, apiKeyCifrada []byte, tz string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO odoo_connections (name, base_url, database, username, api_key_enc, timezone, active)
		VALUES ($1, $2, $3, $4, $5, $6, TRUE)
		ON CONFLICT (base_url, database) DO UPDATE
		SET name = EXCLUDED.name, username = EXCLUDED.username,
		    api_key_enc = EXCLUDED.api_key_enc, timezone = EXCLUDED.timezone,
		    active = TRUE, updated_at = now()
		RETURNING id`, nombre, url, db, usuario, apiKeyCifrada, tz).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("guardando la conexión a Odoo: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE odoo_connections SET active = FALSE, updated_at = now()
		WHERE id <> $1 AND active`, id); err != nil {
		return 0, fmt.Errorf("desactivando las conexiones anteriores: %w", err)
	}
	return id, tx.Commit(ctx)
}

type ConexionOdoo struct {
	ID            int64
	Nombre        string
	BaseURL       string
	Database      string
	Username      string
	APIKeyCifrada []byte
	Timezone      string
	Watermark     *time.Time
}

func (s *Store) ConexionOdooActiva(ctx context.Context) (*ConexionOdoo, error) {
	var c ConexionOdoo
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, base_url, database, username, api_key_enc, timezone, watermark
		FROM odoo_connections WHERE active ORDER BY id LIMIT 1`).
		Scan(&c.ID, &c.Nombre, &c.BaseURL, &c.Database, &c.Username,
			&c.APIKeyCifrada, &c.Timezone, &c.Watermark)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no hay ninguna conexión a Odoo configurada. Ejecuta: integra conectar-odoo")
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) ActualizarWatermark(ctx context.Context, conexionID int64, hasta time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE odoo_connections SET watermark = $2, last_sync_at = now() WHERE id = $1`,
		conexionID, hasta)
	return err
}

// ConexionOdooPorID devuelve una conexión concreta, clave cifrada incluida.
// La clave solo sale de aquí para descifrarse en memoria; nunca se serializa.
func (s *Store) ConexionOdooPorID(ctx context.Context, id int64) (*ConexionOdoo, error) {
	var c ConexionOdoo
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, base_url, database, username, api_key_enc, timezone, watermark
		FROM odoo_connections WHERE id = $1`, id).
		Scan(&c.ID, &c.Nombre, &c.BaseURL, &c.Database, &c.Username,
			&c.APIKeyCifrada, &c.Timezone, &c.Watermark)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("no existe la conexión %d", id)
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ActualizarConexionOdoo edita los datos de una conexión. Si claveCifrada es
// nil se conserva la que ya había: cambiar la URL no obliga a repetir la clave.
//
// Cambiar la instancia apunta el sync a otra base: el catálogo nuevo entra
// con filas propias y el de la instancia anterior queda intacto (y huérfano
// hasta que se migre o se borre).
func (s *Store) ActualizarConexionOdoo(ctx context.Context, id int64, nombre, url, db, usuario, tz string, claveCifrada []byte) error {
	var err error
	if claveCifrada == nil {
		_, err = s.pool.Exec(ctx, `
			UPDATE odoo_connections
			SET name = $2, base_url = $3, database = $4, username = $5,
			    timezone = $6, updated_at = now()
			WHERE id = $1`, id, nombre, url, db, usuario, tz)
	} else {
		_, err = s.pool.Exec(ctx, `
			UPDATE odoo_connections
			SET name = $2, base_url = $3, database = $4, username = $5,
			    timezone = $6, api_key_enc = $7, updated_at = now()
			WHERE id = $1`, id, nombre, url, db, usuario, tz, claveCifrada)
	}
	if err != nil {
		return fmt.Errorf("actualizando la conexión %d: %w", id, err)
	}
	return nil
}

// ActivarConexionOdoo convierte una conexión en la única activa. Integra
// sincroniza contra un solo Odoo a la vez.
func (s *Store) ActivarConexionOdoo(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var existe bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM odoo_connections WHERE id = $1)`, id).Scan(&existe); err != nil {
		return err
	}
	if !existe {
		return fmt.Errorf("no existe la conexión %d", id)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE odoo_connections SET active = FALSE, updated_at = now() WHERE active`,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE odoo_connections SET active = TRUE, updated_at = now() WHERE id = $1`, id,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// --------------------------------------------------------------- almacenes

type Almacen struct {
	OdooID     int64
	Codigo     string
	Nombre     string
	LotStockID int64
}

func (s *Store) UpsertAlmacenes(ctx context.Context, conexionID int64, as []Almacen) (map[int64]int64, error) {
	out := make(map[int64]int64, len(as))
	for _, a := range as {
		var id int64
		err := s.pool.QueryRow(ctx, `
			INSERT INTO odoo_warehouses (odoo_connection_id, odoo_id, code, name, lot_stock_id)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (odoo_connection_id, odoo_id) DO UPDATE
			SET code = EXCLUDED.code, name = EXCLUDED.name, lot_stock_id = EXCLUDED.lot_stock_id
			RETURNING id`, conexionID, a.OdooID, a.Codigo, a.Nombre, a.LotStockID).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("guardando almacén %s: %w", a.Codigo, err)
		}
		out[a.OdooID] = id
	}
	return out, nil
}

// --------------------------------------------------------------- productos

// Identidad es lo único que Odoo aporta de cada producto: quién es (SKU y
// nombre). Todo lo demás —precio, marca, descripción, categoría, exclusión—
// es propiedad de Integra y el sync no lo toca.
type Identidad struct {
	OdooTemplateID int64
	OdooProductID  int64
	Nombre         string
	SKU            string
	OdooWriteDate  *time.Time
}

// UpsertIdentidad guarda o refresca la identidad de un producto sin rozar los
// campos propiedad de Integra: el ON CONFLICT solo lista nombre, SKU y fechas,
// de modo que una edición hecha en la interfaz sobrevive a cualquier sync.
//
// Va en una transacción porque un producto sin su variante no es publicable, y
// dejar la mitad escrita convertiría un fallo de red en datos corruptos.
func (s *Store) UpsertIdentidad(ctx context.Context, conexionID int64, ident Identidad) (prodID, varID int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `
		INSERT INTO products (odoo_connection_id, odoo_template_id, name, odoo_write_date, synced_at)
		VALUES ($1,$2,$3,$4, now())
		ON CONFLICT (odoo_connection_id, odoo_template_id) DO UPDATE
		SET name = EXCLUDED.name, odoo_write_date = EXCLUDED.odoo_write_date,
		    synced_at = now(), updated_at = now()
		RETURNING id`,
		conexionID, ident.OdooTemplateID, ident.Nombre, ident.OdooWriteDate).Scan(&prodID)
	if err != nil {
		return 0, 0, fmt.Errorf("guardando producto %d: %w", ident.OdooTemplateID, err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO product_variants (product_id, odoo_product_id, sku, odoo_write_date, synced_at)
		VALUES ($1,$2,$3,$4, now())
		ON CONFLICT (product_id, odoo_product_id) DO UPDATE
		SET sku = EXCLUDED.sku, odoo_write_date = EXCLUDED.odoo_write_date,
		    synced_at = now(), updated_at = now()
		RETURNING id`,
		prodID, ident.OdooProductID, nulo(ident.SKU), ident.OdooWriteDate).Scan(&varID)
	if err != nil {
		return 0, 0, fmt.Errorf("guardando variante %d: %w", ident.OdooProductID, err)
	}

	return prodID, varID, tx.Commit(ctx)
}

// VariantesPorOdooID mapea el identificador de product.product de Odoo a la
// variante local, para poder aplicar el stock a todo el catálogo aunque la
// lectura de productos haya sido incremental.
func (s *Store) VariantesPorOdooID(ctx context.Context, conexionID int64) (map[int64]int64, error) {
	filas, err := s.pool.Query(ctx, `
		SELECT v.odoo_product_id, v.id
		FROM product_variants v
		JOIN products p ON p.id = v.product_id
		WHERE p.odoo_connection_id = $1`, conexionID)
	if err != nil {
		return nil, fmt.Errorf("mapeando variantes: %w", err)
	}
	defer filas.Close()

	out := map[int64]int64{}
	for filas.Next() {
		var odooID, varID int64
		if err := filas.Scan(&odooID, &varID); err != nil {
			return nil, err
		}
		out[odooID] = varID
	}
	return out, filas.Err()
}

// FilaStock es la existencia de una variante en un almacén.
type FilaStock struct {
	VarianteID int64
	AlmacenID  int64
	OnHand     float64
	Forecast   float64
	Free       float64
}

// ReemplazarStock sustituye todo el stock por la foto recién leída de Odoo.
//
// Borrar e insertar en una transacción evita el fallo clásico del upsert: una
// bodega que se vació del todo desaparece de stock.quant, y con un upsert su
// fila vieja se quedaría publicando existencias que ya no hay.
func (s *Store) ReemplazarStock(ctx context.Context, filas []FilaStock) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM variant_stock`); err != nil {
		return fmt.Errorf("limpiando el stock: %w", err)
	}
	for _, f := range filas {
		if f.OnHand == 0 && f.Forecast == 0 && f.Free == 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO variant_stock (variant_id, odoo_warehouse_id, qty_on_hand, qty_forecast, qty_free, updated_at)
			VALUES ($1,$2,$3,$4,$5, now())`,
			f.VarianteID, f.AlmacenID, f.OnHand, f.Forecast, f.Free); err != nil {
			return fmt.Errorf("guardando stock de la variante %d: %w", f.VarianteID, err)
		}
	}
	return tx.Commit(ctx)
}

func nulo(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
