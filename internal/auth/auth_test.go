package auth

import (
	"testing"
	"time"
)

func TestHashYVerificarPassword(t *testing.T) {
	pass := "secreto123"
	hash, err := HashPassword(pass)
	if err != nil {
		t.Fatalf("error hasheando: %v", err)
	}

	if !VerificarPassword(hash, pass) {
		t.Fatal("verificación falló para contraseña correcta")
	}

	if VerificarPassword(hash, "otra_clave") {
		t.Fatal("verificación pasó para contraseña incorrecta")
	}
}

func TestTokens(t *testing.T) {
	secret := []byte("mi-clave-super-secreta-32-bytes!!")
	token, err := GenerarToken(42, "admin@mdv.com", RolAdmin, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("error generando token: %v", err)
	}

	claims, err := VerificarToken(token, secret)
	if err != nil {
		t.Fatalf("error verificando token: %v", err)
	}

	if claims.UserID != 42 || claims.Email != "admin@mdv.com" || claims.Role != RolAdmin {
		t.Fatalf("claims incorrectos: %+v", claims)
	}

	// Token manipulado
	_, err = VerificarToken(token+"bad", secret)
	if err != ErrTokenInvalido {
		t.Fatalf("esperaba ErrTokenInvalido, obtuve: %v", err)
	}

	// Token expirado
	expToken, err := GenerarToken(42, "admin@mdv.com", RolAdmin, secret, -1*time.Minute)
	if err != nil {
		t.Fatalf("error generando token: %v", err)
	}
	_, err = VerificarToken(expToken, secret)
	if err != ErrTokenExpirado {
		t.Fatalf("esperaba ErrTokenExpirado, obtuve: %v", err)
	}
}

func TestPermisos(t *testing.T) {
	if !TienePermiso(RolAdmin, RolOperator) {
		t.Fatal("admin debería tener permiso de operator")
	}
	if !TienePermiso(RolOperator, RolViewer) {
		t.Fatal("operator debería tener permiso de viewer")
	}
	if TienePermiso(RolViewer, RolAdmin) {
		t.Fatal("viewer NO debería tener permiso de admin")
	}
}
