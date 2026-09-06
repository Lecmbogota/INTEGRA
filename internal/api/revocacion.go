package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Revocación de tokens al cerrar sesión.
//
// El token es un HMAC sin estado: vale por su firma y su fecha, así que
// cerrar sesión no lo invalidaba de ninguna manera. Borrar la cookie ayuda,
// pero solo en el navegador que la borró: cualquiera con una copia del token
// —la consola del navegador de un equipo compartido, un registro, un
// portapapeles— seguía entrando durante las 24 horas de vigencia.
//
// Aquí se guarda qué tokens dejaron de valer, hasta que caducan solos. Es una
// lista en memoria: reiniciar el proceso la olvida, y entonces un token
// revocado vuelve a valer hasta su caducidad. Se asume a propósito, porque la
// alternativa —una tabla y una consulta por petición— cuesta más de lo que
// vale para el tamaño de esta instalación. Queda dicho aquí para que quien
// lea esto lo sepa en vez de suponer que la revocación es definitiva.

type revocador struct {
	mu     sync.Mutex
	hasta  map[string]time.Time
	ahora  func() time.Time
	ultima time.Time
}

var tokensRevocados = &revocador{hasta: map[string]time.Time{}, ahora: time.Now}

// huella evita guardar el token entero: si alguien lee esta memoria o un
// volcado, no se lleva credenciales utilizables.
func huella(token string) string {
	suma := sha256.Sum256([]byte(token))
	return hex.EncodeToString(suma[:])
}

// Revocar deja fuera un token hasta el momento en que habría caducado solo.
func (rv *revocador) Revocar(token string, caduca time.Time) {
	if token == "" {
		return
	}
	rv.mu.Lock()
	defer rv.mu.Unlock()
	rv.limpiarBloqueado()
	rv.hasta[huella(token)] = caduca
}

func (rv *revocador) EstaRevocado(token string) bool {
	if token == "" {
		return false
	}
	rv.mu.Lock()
	defer rv.mu.Unlock()

	caduca, ok := rv.hasta[huella(token)]
	if !ok {
		return false
	}
	// Pasada su caducidad da igual: el propio token ya no vale por fecha.
	if rv.ahora().After(caduca) {
		delete(rv.hasta, huella(token))
		return false
	}
	return true
}

// limpiarBloqueado descarta lo caducado como mucho una vez por minuto, para
// que la lista no crezca sin fin sin pagar un barrido en cada petición.
func (rv *revocador) limpiarBloqueado() {
	ahora := rv.ahora()
	if ahora.Sub(rv.ultima) < time.Minute {
		return
	}
	rv.ultima = ahora
	for h, caduca := range rv.hasta {
		if ahora.After(caduca) {
			delete(rv.hasta, h)
		}
	}
}

// tokenDe saca el token de la petición, del mismo sitio de donde lo lee la
// validación: primero la cabecera, y si no, la cookie.
func tokenDe(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if c, err := r.Cookie("integra_token"); err == nil {
		return c.Value
	}
	return ""
}
