// Package crypto cifra las credenciales que Integra guarda en PostgreSQL.
//
// Cada conexión a Odoo y cada cuenta de canal tiene claves de API y tokens
// OAuth. Con 16 cuentas y una conexión a Odoo, una filtración de la base de
// datos entregaría el control de las tiendas. Por eso todo se guarda cifrado
// con AES-256-GCM y solo se descifra en memoria, justo antes de usarlo.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize es el tamaño de clave de AES-256.
	KeySize = 32
	// versionByte permite rotar el formato sin romper lo ya cifrado.
	versionByte = 0x01
)

var (
	// ErrClaveInvalida indica que la clave maestra no mide 32 bytes.
	ErrClaveInvalida = errors.New("la clave maestra debe medir exactamente 32 bytes")
	// ErrCifradoCorrupto indica que el texto cifrado no es descifrable.
	ErrCifradoCorrupto = errors.New("texto cifrado corrupto o clave incorrecta")
)

// Cifrador cifra y descifra secretos con una clave maestra.
type Cifrador struct {
	clave []byte
	aead  cipher.AEAD
}

// Nuevo construye un cifrador a partir de una clave maestra de 32 bytes.
func Nuevo(claveMaestra []byte) (*Cifrador, error) {
	if len(claveMaestra) != KeySize {
		return nil, fmt.Errorf("%w (recibidos %d)", ErrClaveInvalida, len(claveMaestra))
	}
	bloque, err := aes.NewCipher(claveMaestra)
	if err != nil {
		return nil, fmt.Errorf("construyendo AES: %w", err)
	}
	aead, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, fmt.Errorf("construyendo GCM: %w", err)
	}
	k := make([]byte, len(claveMaestra))
	copy(k, claveMaestra)
	return &Cifrador{clave: k, aead: aead}, nil
}

// Clave devuelve los bytes de la clave maestra.
func (c *Cifrador) Clave() []byte {
	return c.clave
}

// ClaveBase64 devuelve la clave maestra codificada en base64.
func (c *Cifrador) ClaveBase64() string {
	return base64.StdEncoding.EncodeToString(c.clave)
}

// DesdeBase64 construye un cifrador a partir de una clave en base64, que es el
// formato en el que viaja por variable de entorno.
func DesdeBase64(s string) (*Cifrador, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("la clave maestra no es base64 válido: %w", err)
	}
	return Nuevo(raw)
}

// GenerarClave produce una clave maestra nueva, lista para pegar en el entorno.
func GenerarClave() (string, error) {
	k := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", fmt.Errorf("no hay entropía disponible: %w", err)
	}
	return base64.StdEncoding.EncodeToString(k), nil
}

// Cifrar devuelve version || nonce || ciphertext || tag.
//
// El nonce se genera al azar en cada llamada: cifrar dos veces el mismo secreto
// produce salidas distintas, así que nadie puede deducir del contenido de la
// base de datos que dos cuentas comparten credenciales.
func (c *Cifrador) Cifrar(claro []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("no hay entropía disponible: %w", err)
	}

	salida := make([]byte, 0, 1+len(nonce)+len(claro)+c.aead.Overhead())
	salida = append(salida, versionByte)
	salida = append(salida, nonce...)
	return c.aead.Seal(salida, nonce, claro, nil), nil
}

// Descifrar revierte Cifrar.
func (c *Cifrador) Descifrar(cifrado []byte) ([]byte, error) {
	minimo := 1 + c.aead.NonceSize() + c.aead.Overhead()
	if len(cifrado) < minimo {
		return nil, fmt.Errorf("%w: mide %d bytes y el mínimo es %d",
			ErrCifradoCorrupto, len(cifrado), minimo)
	}
	if cifrado[0] != versionByte {
		return nil, fmt.Errorf("%w: versión de formato desconocida 0x%02x",
			ErrCifradoCorrupto, cifrado[0])
	}

	nonce := cifrado[1 : 1+c.aead.NonceSize()]
	cuerpo := cifrado[1+c.aead.NonceSize():]

	claro, err := c.aead.Open(nil, nonce, cuerpo, nil)
	if err != nil {
		// El error de GCM no se propaga: distinguir "clave incorrecta" de
		// "datos manipulados" solo ayuda a quien ataca.
		return nil, ErrCifradoCorrupto
	}
	return claro, nil
}

// CifrarTexto es la variante cómoda para secretos que son cadenas.
func (c *Cifrador) CifrarTexto(s string) ([]byte, error) { return c.Cifrar([]byte(s)) }

// DescifrarTexto es la variante cómoda para secretos que son cadenas.
func (c *Cifrador) DescifrarTexto(b []byte) (string, error) {
	claro, err := c.Descifrar(b)
	if err != nil {
		return "", err
	}
	return string(claro), nil
}

// Redactar sustituye un secreto por una versión inofensiva para los logs.
//
// Se conservan los cuatro últimos caracteres, lo justo para poder decir "la
// clave que termina en 923" al depurar, sin exponer nada útil.
func Redactar(secreto string) string {
	const visibles = 4
	if len(secreto) <= visibles {
		return "****"
	}
	return "****" + secreto[len(secreto)-visibles:]
}

// IgualEnTiempoConstante compara dos secretos sin filtrar información por el
// tiempo que tarda la comparación.
func IgualEnTiempoConstante(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
