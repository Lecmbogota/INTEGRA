// Package migrate aplica el esquema versionado sobre PostgreSQL.
//
// Es un migrador propio en vez de una dependencia externa por dos razones:
// las migraciones ya viajan embebidas en el binario, y el formato de
// anotaciones se mantiene compatible con goose por si algún día conviene
// cambiar de herramienta.
//
// Garantías:
//   - cada migración corre dentro de una transacción; si algo falla, no queda
//     el esquema a medias
//   - un lock de sesión impide que dos procesos migren a la vez, que es lo que
//     pasaría al arrancar API y worker simultáneamente
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// lockID identifica el advisory lock. Es un número arbitrario pero fijo.
const lockID = 8_274_113_905

const marcaUp = "-- +goose Up"
const marcaDown = "-- +goose Down"

// Migracion es un fichero de esquema ya troceado.
type Migracion struct {
	Version string
	Nombre  string
	Up      string
	Down    string
}

// Estado describe una migración y si está aplicada.
type Estado struct {
	Version  string
	Nombre   string
	Aplicada bool
}

// Cargar lee y trocea las migraciones de un sistema de ficheros.
func Cargar(fsys fs.FS) ([]Migracion, error) {
	entradas, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("listando migraciones: %w", err)
	}
	if len(entradas) == 0 {
		return nil, fmt.Errorf("no se encontró ninguna migración")
	}
	sort.Strings(entradas)

	out := make([]Migracion, 0, len(entradas))
	for _, nombre := range entradas {
		raw, err := fs.ReadFile(fsys, nombre)
		if err != nil {
			return nil, fmt.Errorf("leyendo %s: %w", nombre, err)
		}
		m, err := trocear(nombre, string(raw))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}

	// Versiones duplicadas significan que alguien copió un fichero sin
	// renumerarlo: mejor fallar aquí que aplicar una sola de las dos.
	vistas := map[string]string{}
	for _, m := range out {
		if antes, ya := vistas[m.Version]; ya {
			return nil, fmt.Errorf("versión %s duplicada en %s y %s", m.Version, antes, m.Nombre)
		}
		vistas[m.Version] = m.Nombre
	}
	return out, nil
}

func trocear(nombre, contenido string) (Migracion, error) {
	base := filepath.Base(nombre)
	version, _, ok := strings.Cut(strings.TrimSuffix(base, ".sql"), "_")
	if !ok || version == "" {
		return Migracion{}, fmt.Errorf("%s: el nombre debe ser <version>_<descripcion>.sql", base)
	}

	iUp := strings.Index(contenido, marcaUp)
	if iUp < 0 {
		return Migracion{}, fmt.Errorf("%s: falta la marca %q", base, marcaUp)
	}
	iDown := strings.Index(contenido, marcaDown)

	var up, down string
	if iDown < 0 {
		up = contenido[iUp+len(marcaUp):]
	} else {
		if iDown < iUp {
			return Migracion{}, fmt.Errorf("%s: %q aparece antes que %q", base, marcaDown, marcaUp)
		}
		up = contenido[iUp+len(marcaUp) : iDown]
		down = contenido[iDown+len(marcaDown):]
	}

	up = strings.TrimSpace(up)
	if up == "" {
		return Migracion{}, fmt.Errorf("%s: la sección Up está vacía", base)
	}

	return Migracion{
		Version: version,
		Nombre:  base,
		Up:      up,
		Down:    strings.TrimSpace(down),
	}, nil
}

// asegurarTabla crea el registro de migraciones aplicadas.
func asegurarTabla(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			nombre     TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("creando schema_migrations: %w", err)
	}
	return nil
}

func aplicadas(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	filas, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("leyendo schema_migrations: %w", err)
	}
	defer filas.Close()

	out := map[string]bool{}
	for filas.Next() {
		var v string
		if err := filas.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, filas.Err()
}

// bloquear toma el advisory lock para que dos procesos no migren a la vez.
func bloquear(ctx context.Context, conn *pgx.Conn) error {
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(lockID)); err != nil {
		return fmt.Errorf("tomando el lock de migración: %w", err)
	}
	return nil
}

func desbloquear(ctx context.Context, conn *pgx.Conn) {
	_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, int64(lockID))
}

// Up aplica las migraciones pendientes en orden y devuelve las aplicadas.
func Up(ctx context.Context, conn *pgx.Conn, migs []Migracion) ([]string, error) {
	if err := asegurarTabla(ctx, conn); err != nil {
		return nil, err
	}
	if err := bloquear(ctx, conn); err != nil {
		return nil, err
	}
	defer desbloquear(ctx, conn)

	ya, err := aplicadas(ctx, conn)
	if err != nil {
		return nil, err
	}

	var hechas []string
	for _, m := range migs {
		if ya[m.Version] {
			continue
		}
		if err := ejecutarEnTransaccion(ctx, conn, m.Up, registrar(m)); err != nil {
			return hechas, fmt.Errorf("aplicando %s: %w", m.Nombre, err)
		}
		hechas = append(hechas, m.Nombre)
	}
	return hechas, nil
}

// Down revierte la última migración aplicada.
func Down(ctx context.Context, conn *pgx.Conn, migs []Migracion) (string, error) {
	if err := asegurarTabla(ctx, conn); err != nil {
		return "", err
	}
	if err := bloquear(ctx, conn); err != nil {
		return "", err
	}
	defer desbloquear(ctx, conn)

	ya, err := aplicadas(ctx, conn)
	if err != nil {
		return "", err
	}

	for i := len(migs) - 1; i >= 0; i-- {
		m := migs[i]
		if !ya[m.Version] {
			continue
		}
		if m.Down == "" {
			return "", fmt.Errorf("%s no tiene sección Down: no se puede revertir", m.Nombre)
		}
		borrar := fmt.Sprintf("DELETE FROM schema_migrations WHERE version = %s;", literal(m.Version))
		if err := ejecutarEnTransaccion(ctx, conn, m.Down, borrar); err != nil {
			return "", fmt.Errorf("revirtiendo %s: %w", m.Nombre, err)
		}
		return m.Nombre, nil
	}
	return "", nil
}

// Status devuelve el estado de cada migración.
func Status(ctx context.Context, conn *pgx.Conn, migs []Migracion) ([]Estado, error) {
	if err := asegurarTabla(ctx, conn); err != nil {
		return nil, err
	}
	ya, err := aplicadas(ctx, conn)
	if err != nil {
		return nil, err
	}
	out := make([]Estado, 0, len(migs))
	for _, m := range migs {
		out = append(out, Estado{Version: m.Version, Nombre: m.Nombre, Aplicada: ya[m.Version]})
	}
	return out, nil
}

func registrar(m Migracion) string {
	return fmt.Sprintf(
		"INSERT INTO schema_migrations (version, nombre) VALUES (%s, %s);",
		literal(m.Version), literal(m.Nombre))
}

// ejecutarEnTransaccion manda el bloque completo por el protocolo simple.
//
// El protocolo extendido de pgx no admite varias sentencias en una llamada, y
// una migración son decenas. Se envuelve todo en BEGIN/COMMIT dentro del mismo
// envío: si una sentencia falla, PostgreSQL aborta y el COMMIT actúa de
// ROLLBACK, así que nunca queda un esquema parcial.
func ejecutarEnTransaccion(ctx context.Context, conn *pgx.Conn, cuerpo, cola string) error {
	sql := "BEGIN;\n" + cuerpo + "\n" + cola + "\nCOMMIT;"
	if _, err := conn.PgConn().Exec(ctx, sql).ReadAll(); err != nil {
		return err
	}
	return nil
}

// literal escapa una cadena para incrustarla en SQL.
//
// Las migraciones no llevan datos de usuario —solo nombres de fichero y
// versiones que salen del propio repositorio— pero escapar es gratis y evita
// que un nombre con comilla rompa el arranque.
func literal(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
