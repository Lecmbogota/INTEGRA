package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/mdv/integra/internal/auth"
)

var (
	// ErrUsuarioNoExiste distingue «no hay tal fila» de un fallo de la base.
	// El middleware necesita esa diferencia: lo primero revoca la sesión, lo
	// segundo es una avería y no debe echar a nadie.
	ErrUsuarioNoExiste = errors.New("el usuario no existe")

	// ErrUltimoAdmin evita el error irreversible: quitar al único
	// administrador activo deja el sistema sin nadie que pueda volver a
	// crear usuarios desde la interfaz.
	ErrUltimoAdmin = errors.New("es el último administrador activo: crea o activa otro antes de quitarlo")
)

// CrearUsuario registra un nuevo usuario en la base de datos.
func (s *Store) CrearUsuario(ctx context.Context, email, name, passwordHash, role string) (int64, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return 0, fmt.Errorf("el correo es obligatorio")
	}
	if role != auth.RolAdmin && role != auth.RolOperator && role != auth.RolViewer {
		return 0, fmt.Errorf("rol inválido: %q (debe ser admin, operator o viewer)", role)
	}

	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (email, name, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, email, name, passwordHash, role).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("creando usuario: %w", err)
	}
	return id, nil
}

// UsuarioPorEmail busca un usuario activo o inactivo por correo electrónico.
func (s *Store) UsuarioPorEmail(ctx context.Context, email string) (*auth.Usuario, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var u auth.Usuario
	var passHash string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, password_hash, role, active, last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1`, email).Scan(
		&u.ID, &u.Email, &u.Name, &passHash, &u.Role, &u.Active,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, "", err
	}
	return &u, passHash, nil
}

// UsuarioPorID busca un usuario por su clave primaria.
func (s *Store) UsuarioPorID(ctx context.Context, id int64) (*auth.Usuario, error) {
	var u auth.Usuario
	var passHash string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, name, password_hash, role, active, last_login_at, created_at, updated_at
		FROM users
		WHERE id = $1`, id).Scan(
		&u.ID, &u.Email, &u.Name, &passHash, &u.Role, &u.Active,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ListarUsuarios devuelve todos los usuarios ordenados por nombre.
func (s *Store) ListarUsuarios(ctx context.Context) ([]auth.Usuario, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, name, role, active, last_login_at, created_at, updated_at
		FROM users
		ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("listando usuarios: %w", err)
	}
	defer rows.Close()

	var out []auth.Usuario
	for rows.Next() {
		var u auth.Usuario
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ActualizarUsuario modifica nombre, rol y estado de activación.
func (s *Store) ActualizarUsuario(ctx context.Context, id int64, name, role string, active bool) error {
	if role != auth.RolAdmin && role != auth.RolOperator && role != auth.RolViewer {
		return fmt.Errorf("rol inválido: %q", role)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE users
		SET name = $2, role = $3, active = $4, updated_at = now()
		WHERE id = $1`, id, name, role, active)
	return err
}

// ActualizarPassword cambia la contraseña hasheada de un usuario.
func (s *Store) ActualizarPassword(ctx context.Context, id int64, nuevoHash string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users
		SET password_hash = $2, updated_at = now()
		WHERE id = $1`, id, nuevoHash)
	return err
}

// RegistrarLogin actualiza la marca de último acceso.
func (s *Store) RegistrarLogin(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET last_login_at = now() WHERE id = $1`, id)
	return err
}

// TotalUsuarios devuelve la cantidad de usuarios registrados.
func (s *Store) TotalUsuarios(ctx context.Context) (int, error) {
	var total int
	err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&total)
	return total, err
}

// ------------------------------------------------------- vigencia y bajas

// EstadoUsuario devuelve el rol y la activación actuales de un usuario.
//
// Es la consulta que respalda al middleware de sesión: el token va firmado y
// lleva dentro el rol, pero está firmado con lo que era verdad cuando se
// emitió. Un usuario borrado, desactivado o degradado seguiría entrando con su
// token viejo hasta que caduque si nadie mira la base.
//
// Se lee solo lo imprescindible —dos columnas— porque esto se ejecuta, a
// través de la caché del middleware, en el camino de cada petición.
func (s *Store) EstadoUsuario(ctx context.Context, id int64) (rol string, activo bool, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT role, active FROM users WHERE id = $1`, id).Scan(&rol, &activo)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("%w (id %d)", ErrUsuarioNoExiste, id)
	}
	if err != nil {
		return "", false, fmt.Errorf("consultando el estado del usuario %d: %w", id, err)
	}
	return rol, activo, nil
}

// usuarioParaBaja localiza al usuario por correo dentro de la transacción y se
// niega si quitarlo dejaría el sistema sin ningún administrador activo.
//
// La fila se bloquea (FOR UPDATE) y, cuando el afectado es administrador, se
// bloquean también los demás administradores activos: sin eso dos bajas
// simultáneas podrían ver cada una «queda otro» y dejar cero.
func usuarioParaBaja(ctx context.Context, tx pgx.Tx, email string) (*auth.Usuario, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("el correo es obligatorio")
	}

	var u auth.Usuario
	err := tx.QueryRow(ctx, `
		SELECT id, email, name, role, active, last_login_at, created_at, updated_at
		FROM users WHERE email = $1 FOR UPDATE`, email).Scan(
		&u.ID, &u.Email, &u.Name, &u.Role, &u.Active,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrUsuarioNoExiste, email)
	}
	if err != nil {
		return nil, err
	}

	if u.Role == auth.RolAdmin && u.Active {
		var otros int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM (
				SELECT 1 FROM users
				WHERE role = 'admin' AND active AND id <> $1
				ORDER BY id FOR UPDATE
			) t`, u.ID).Scan(&otros); err != nil {
			return nil, err
		}
		if otros == 0 {
			return nil, fmt.Errorf("%s %w", u.Email, ErrUltimoAdmin)
		}
	}
	return &u, nil
}

// DesactivarUsuario marca a un usuario como inactivo sin borrar nada.
//
// Es la baja preferida: conserva la autoría de lo que hizo (los registros de
// auditoría y los precios que creó siguen apuntando a él) y se deshace con
// activar-usuario. Devuelve el usuario tal y como estaba antes.
func (s *Store) DesactivarUsuario(ctx context.Context, email string) (*auth.Usuario, error) {
	return s.cambiarActivacion(ctx, email, false)
}

// ActivarUsuario devuelve el acceso a un usuario desactivado.
func (s *Store) ActivarUsuario(ctx context.Context, email string) (*auth.Usuario, error) {
	return s.cambiarActivacion(ctx, email, true)
}

func (s *Store) cambiarActivacion(ctx context.Context, email string, activo bool) (*auth.Usuario, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	u, err := usuarioParaBaja(ctx, tx, email)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET active = $2, updated_at = now() WHERE id = $1`,
		u.ID, activo); err != nil {
		return nil, fmt.Errorf("cambiando la activación de %s: %w", u.Email, err)
	}
	return u, tx.Commit(ctx)
}

// BorrarUsuario elimina la fila del usuario.
//
// Las tablas que lo referencian lo hacen con ON DELETE SET NULL, así que nada
// se lleva por delante: lo que se pierde es la autoría, que pasa a figurar sin
// autor en la auditoría y en los precios. Por eso desactivar es casi siempre
// la opción correcta y esta exige confirmación explícita en la CLI.
func (s *Store) BorrarUsuario(ctx context.Context, email string) (*auth.Usuario, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	u, err := usuarioParaBaja(ctx, tx, email)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, u.ID); err != nil {
		return nil, fmt.Errorf("borrando a %s: %w", u.Email, err)
	}
	return u, tx.Commit(ctx)
}

// CambiarPassword sustituye la contraseña de un usuario.
//
// Existe porque sin esto una contraseña olvidada deja la cuenta inservible: no
// hay recuperación por correo ni forma de entrar a cambiarla desde dentro. El
// hash se calcula fuera y aquí solo se guarda, para que esta capa no tenga que
// saber nada de bcrypt.
//
// Devuelve el usuario para que quien llame pueda confirmar a quién se la
// cambió — con dos cuentas de correo parecidas, equivocarse es fácil.
func (s *Store) CambiarPassword(ctx context.Context, email, hash string) (*auth.Usuario, error) {
	var u auth.Usuario
	err := s.pool.QueryRow(ctx, `
		UPDATE users
		SET password_hash = $2, updated_at = now()
		WHERE lower(email) = lower(btrim($1))
		RETURNING id, email, name, role, active`,
		email, hash).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no existe ningún usuario con el correo %q", email)
	}
	if err != nil {
		return nil, fmt.Errorf("cambiando la contraseña: %w", err)
	}
	return &u, nil
}
