// Package auth implementa la autenticación, gestión de usuarios, roles
// y verificación de tokens de sesión para la operación multiusuario.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Roles del sistema.
const (
	RolAdmin    = "admin"
	RolOperator = "operator"
	RolViewer   = "viewer"
)

var (
	ErrTokenInvalido = errors.New("token de sesión inválido")
	ErrTokenExpirado = errors.New("token de sesión expirado")
	ErrCredenciales  = errors.New("correo o contraseña incorrectos")
	ErrUsuarioInactivo = errors.New("el usuario está inactivo")
)

// Usuario representa una fila de la tabla users.
type Usuario struct {
	ID          int64      `json:"id"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Role        string     `json:"role"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Claims contiene la información firmada dentro del token.
type Claims struct {
	UserID    int64     `json:"uid"`
	Email     string    `json:"eml"`
	Role      string    `json:"rol"`
	ExpiresAt time.Time `json:"exp"`
}

// HashPassword genera el hash bcrypt de la contraseña.
func HashPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", fmt.Errorf("la contraseña debe tener al menos 6 caracteres")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hasheando contraseña: %w", err)
	}
	return string(hash), nil
}

// VerificarPassword compara la contraseña en claro contra el hash bcrypt.
func VerificarPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerarToken crea un token firmado con HMAC-SHA256.
func GenerarToken(userID int64, email, role string, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("clave secreta requerida para firmar token")
	}
	c := Claims{
		UserID:    userID,
		Email:     email,
		Role:      role,
		ExpiresAt: time.Now().Add(ttl),
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payloadB64))
	firmaB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return payloadB64 + "." + firmaB64, nil
}

// VerificarToken comprueba la firma y la fecha de expiración del token.
func VerificarToken(tokenStr string, secret []byte) (*Claims, error) {
	partes := strings.Split(tokenStr, ".")
	if len(partes) != 2 {
		return nil, ErrTokenInvalido
	}

	payloadB64, firmaB64 := partes[0], partes[1]

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payloadB64))
	firmaEsperada := mac.Sum(nil)

	firmaDada, err := base64.RawURLEncoding.DecodeString(firmaB64)
	if err != nil || !hmac.Equal(firmaEsperada, firmaDada) {
		return nil, ErrTokenInvalido
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrTokenInvalido
	}

	var c Claims
	if err := json.Unmarshal(payloadBytes, &c); err != nil {
		return nil, ErrTokenInvalido
	}

	if time.Now().After(c.ExpiresAt) {
		return nil, ErrTokenExpirado
	}

	return &c, nil
}

// NivelRol asigna una jerarquía numérica a los roles para evaluar permisos mínimos.
func NivelRol(rol string) int {
	switch rol {
	case RolAdmin:
		return 3
	case RolOperator:
		return 2
	case RolViewer:
		return 1
	default:
		return 0
	}
}

// TienePermiso indica si un rol cumple con el nivel mínimo requerido.
func TienePermiso(rolUsuario, rolMinimo string) bool {
	return NivelRol(rolUsuario) >= NivelRol(rolMinimo)
}
