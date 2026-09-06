package conectores

import "testing"

// Exigir https a la tienda de WooCommerce es correcto —sobre http su API solo
// admite OAuth 1.0a y el secreto viajaría en claro—, pero dejaba inutilizable
// la tienda de pruebas local, que es el único canal de los cuatro que se puede
// ejercitar de verdad sin credenciales de nadie. La excepción tiene que ser
// exactamente eso: la propia máquina y nada más.

func TestSeReconoceLaPropiaMaquina(t *testing.T) {
	locales := []string{
		"http://localhost:8090",
		"http://LOCALHOST:8090/",
		"https://localhost",
		"http://127.0.0.1:8090",
		"http://127.1.2.3:80", // todo 127.x.x.x es bucle local
		"http://[::1]:8090",
	}
	for _, u := range locales {
		if !EsLocal(u) {
			t.Errorf("%q es la propia máquina y no se reconoció: la tienda de pruebas quedaría inservible", u)
		}
	}
}

func TestNoSeConfundeNadaDeFueraConLoLocal(t *testing.T) {
	// El riesgo de esta excepción es dejar pasar por http algo que sí sale a
	// la red. Los nombres que solo *contienen* localhost son el caso clásico.
	ajenos := []string{
		"http://tienda.com",
		"http://localhost.evil.com",     // subdominio de un dominio ajeno
		"http://notlocalhost",           // contiene el texto, no es la máquina
		"http://192.168.1.50",           // la red local no es la propia máquina
		"http://10.0.0.5",               // ídem
		"http://mitienda.com/localhost", // el texto va en la ruta
		"",                              // vacío no es local
		"no-es-una-url",                 // sin servidor
	}
	for _, u := range ajenos {
		if EsLocal(u) {
			t.Errorf("%q NO es la propia máquina: dejarlo pasar por http expondría el secreto", u)
		}
	}
}
