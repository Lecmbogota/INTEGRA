package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func nuevoParaTest(t *testing.T) *Cifrador {
	t.Helper()
	clave, err := GenerarClave()
	if err != nil {
		t.Fatalf("generando clave: %v", err)
	}
	c, err := DesdeBase64(clave)
	if err != nil {
		t.Fatalf("construyendo cifrador: %v", err)
	}
	return c
}

func TestIdaYVuelta(t *testing.T) {
	c := nuevoParaTest(t)

	casos := []string{
		"",
		"22ad8cb95fb88c0428a79230bf88e8a4db68f923",
		`{"access_token":"APP_USR-123","refresh_token":"TG-456"}`,
		"contraseña con acentos y símbolos: ñ€@#",
		strings.Repeat("x", 100_000),
	}

	for _, claro := range casos {
		cifrado, err := c.CifrarTexto(claro)
		if err != nil {
			t.Fatalf("cifrando %q: %v", Redactar(claro), err)
		}
		if bytes.Contains(cifrado, []byte(claro)) && claro != "" {
			t.Fatalf("el texto en claro aparece dentro del cifrado")
		}
		vuelta, err := c.DescifrarTexto(cifrado)
		if err != nil {
			t.Fatalf("descifrando: %v", err)
		}
		if vuelta != claro {
			t.Fatalf("ida y vuelta rota: se obtuvo %q", Redactar(vuelta))
		}
	}
}

func TestCifradosDistintosParaElMismoTexto(t *testing.T) {
	c := nuevoParaTest(t)
	const secreto = "misma-api-key-en-dos-cuentas"

	a, err := c.CifrarTexto(secreto)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.CifrarTexto(secreto)
	if err != nil {
		t.Fatal(err)
	}

	// Si dos cuentas comparten credencial, el contenido de la base de datos no
	// debe delatarlo.
	if bytes.Equal(a, b) {
		t.Fatal("cifrar dos veces el mismo texto produjo el mismo resultado")
	}
}

func TestClaveIncorrectaNoDescifra(t *testing.T) {
	a := nuevoParaTest(t)
	b := nuevoParaTest(t)

	cifrado, err := a.CifrarTexto("token-de-shopify")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.DescifrarTexto(cifrado); !errors.Is(err, ErrCifradoCorrupto) {
		t.Fatalf("se esperaba ErrCifradoCorrupto, se obtuvo %v", err)
	}
}

func TestManipulacionDetectada(t *testing.T) {
	c := nuevoParaTest(t)
	cifrado, err := c.CifrarTexto("credencial-de-falabella")
	if err != nil {
		t.Fatal(err)
	}

	// GCM autentica: cambiar un solo bit debe hacer fallar el descifrado.
	for _, pos := range []int{0, 1, len(cifrado) / 2, len(cifrado) - 1} {
		copia := append([]byte(nil), cifrado...)
		copia[pos] ^= 0x01
		if _, err := c.DescifrarTexto(copia); err == nil {
			t.Fatalf("no se detectó la manipulación del byte %d", pos)
		}
	}
}

func TestEntradasInvalidas(t *testing.T) {
	c := nuevoParaTest(t)

	if _, err := c.Descifrar(nil); !errors.Is(err, ErrCifradoCorrupto) {
		t.Errorf("nil debería dar ErrCifradoCorrupto, dio %v", err)
	}
	if _, err := c.Descifrar([]byte{0x01, 0x02}); !errors.Is(err, ErrCifradoCorrupto) {
		t.Errorf("entrada corta debería dar ErrCifradoCorrupto, dio %v", err)
	}

	// Versión de formato desconocida.
	cifrado, _ := c.CifrarTexto("x")
	cifrado[0] = 0x99
	if _, err := c.Descifrar(cifrado); !errors.Is(err, ErrCifradoCorrupto) {
		t.Errorf("versión desconocida debería dar ErrCifradoCorrupto, dio %v", err)
	}
}

func TestClaveDeTamanoIncorrecto(t *testing.T) {
	for _, n := range []int{0, 16, 31, 33, 64} {
		if _, err := Nuevo(make([]byte, n)); !errors.Is(err, ErrClaveInvalida) {
			t.Errorf("una clave de %d bytes debería ser rechazada, err=%v", n, err)
		}
	}
	if _, err := DesdeBase64("esto no es base64!!"); err == nil {
		t.Error("una clave que no es base64 debería ser rechazada")
	}
}

func TestGenerarClaveEsUsable(t *testing.T) {
	s, err := GenerarClave()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("la clave generada no es base64: %v", err)
	}
	if len(raw) != KeySize {
		t.Fatalf("la clave generada mide %d bytes y debería medir %d", len(raw), KeySize)
	}

	otra, _ := GenerarClave()
	if s == otra {
		t.Fatal("dos claves generadas seguidas salieron iguales")
	}
}

func TestRedactar(t *testing.T) {
	casos := map[string]string{
		"":     "****",
		"abc":  "****",
		"abcd": "****",
		"22ad8cb95fb88c0428a79230bf88e8a4db68f923": "****f923",
	}
	for entrada, esperado := range casos {
		if got := Redactar(entrada); got != esperado {
			t.Errorf("Redactar(%d chars) = %q, se esperaba %q", len(entrada), got, esperado)
		}
	}
}

func TestIgualEnTiempoConstante(t *testing.T) {
	if !IgualEnTiempoConstante("secreto", "secreto") {
		t.Error("dos cadenas iguales deberían comparar igual")
	}
	if IgualEnTiempoConstante("secreto", "secretO") {
		t.Error("dos cadenas distintas no deberían comparar igual")
	}
	if IgualEnTiempoConstante("corto", "mucho más largo") {
		t.Error("longitudes distintas no deberían comparar igual")
	}
}

func BenchmarkCifrar(b *testing.B) {
	clave, _ := GenerarClave()
	c, _ := DesdeBase64(clave)
	secreto := []byte(`{"access_token":"APP_USR-1234567890","refresh_token":"TG-abcdef"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Cifrar(secreto); err != nil {
			b.Fatal(err)
		}
	}
}
