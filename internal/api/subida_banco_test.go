package api

import (
	"strings"
	"testing"
)

// Las fotos del fabricante llegan como «SKU-1.jpg», «SKU-2.jpg»: sacar el
// SKU del nombre es lo que hace útil subir una carpeta entera. Pero un SKU
// que termina en número no es una serie, y quitárselo lo dejaría sin dueño.
func TestElSKUSeSacaDelNombreSinRomperLosQueTerminanEnNumero(t *testing.T) {
	casos := map[string]string{
		"HDWT860UZSVA-2.jpg":     "HDWT860UZSVA-2 | HDWT860UZSVA",
		"HDWT860UZSVA_3.JPG":     "HDWT860UZSVA_3 | HDWT860UZSVA",
		"HDWT860UZSVA (4).png":   "HDWT860UZSVA (4) | HDWT860UZSVA",
		"HDWT860UZSVA.jpg":       "HDWT860UZSVA",
		"R6-HS-1000.jpg":         "R6-HS-1000",
		"R6-HS-1000-1.jpg":       "R6-HS-1000-1 | R6-HS-1000",
		"AO-PR-1008-BD.webp":     "AO-PR-1008-BD",
		// «-32» puede ser 32 GB o la foto 32: se ofrecen los dos, y como el
		// exacto se prueba primero, un SKU real que termine así gana siempre.
		"  KVR56U46BS8-32 .jpeg": "KVR56U46BS8-32 | KVR56U46BS8",
		".jpg":                   "",
	}
	for nombre, espera := range casos {
		got := strings.Join(candidatosSKU(nombre), " | ")
		if got != espera {
			t.Errorf("%q: candidatos %q, se esperaba %q", nombre, got, espera)
		}
	}
}
