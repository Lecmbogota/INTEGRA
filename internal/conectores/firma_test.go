package conectores

import (
	"strings"
	"testing"
)

// La firma de Falabella es el punto donde un error es más difícil de
// diagnosticar: el servidor solo responde «firma inválida» sin decir en qué
// difiere. Estos tests fijan las tres reglas que la componen.

func TestFirmaOrdenaLosParametrosAlfabeticamente(t *testing.T) {
	// Se pasan desordenados a propósito.
	got := FirmarFalabella(map[string]string{
		"Version": "1.0", "Action": "GetSeller", "UserID": "a@b.com",
	}, "clave")

	base := got[:strings.Index(got, "&Signature=")]
	if base != "Action=GetSeller&UserID=a%40b.com&Version=1.0" {
		t.Fatalf("los parámetros deben ir ordenados y codificados: %s", base)
	}
}

// El @ del correo y los dos puntos de la marca de tiempo deben ir escapados:
// mandarlos crudos es el fallo clásico contra esta API.
func TestFirmaEscapaLosCaracteresReservados(t *testing.T) {
	got := FirmarFalabella(map[string]string{
		"UserID": "contabilidad@mdv.com", "Timestamp": "2026-08-22T19:30:00-0500",
	}, "clave")

	if strings.Contains(got[:strings.Index(got, "&Signature=")], "@") {
		t.Error("el @ del UserID debe ir codificado como %40")
	}
	if !strings.Contains(got, "2026-08-22T19%3A30%3A00-0500") {
		t.Error("los dos puntos de la marca de tiempo deben ir codificados como %3A")
	}
}

func TestFirmaCambiaConLaClave(t *testing.T) {
	params := map[string]string{"Action": "GetSeller", "UserID": "a@b.com"}
	a := FirmarFalabella(params, "clave-uno")
	b := FirmarFalabella(params, "clave-dos")
	if a == b {
		t.Fatal("dos claves distintas no pueden producir la misma firma")
	}
}

func TestFirmaEsDeterminista(t *testing.T) {
	params := map[string]string{"Action": "GetSeller", "UserID": "a@b.com"}
	if FirmarFalabella(params, "clave") != FirmarFalabella(params, "clave") {
		t.Fatal("la misma entrada debe producir la misma firma")
	}
}

func TestParamsTraenLoObligatorio(t *testing.T) {
	p := ParamsFalabella("GetOrders", "a@b.com")
	for _, clave := range []string{"Action", "Format", "Timestamp", "UserID", "Version"} {
		if p[clave] == "" {
			t.Errorf("falta el parámetro obligatorio %s", clave)
		}
	}
	if p["Action"] != "GetOrders" {
		t.Errorf("Action debía ser GetOrders, es %q", p["Action"])
	}
}
