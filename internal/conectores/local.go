package conectores

import (
	"net"
	"net/url"
	"strings"
)

// EsLocal dice si una URL apunta a la propia máquina.
//
// Existe por una tensión real entre dos cosas ciertas a la vez. Exigir HTTPS a
// la tienda de WooCommerce es correcto: sobre HTTP la API solo admite OAuth
// 1.0a y la clave y el secreto viajarían en claro por la red. Pero la tienda
// de pruebas que se levanta con docker-compose.woocommerce.yml vive en
// http://localhost:8090, y montarle un certificado a una tienda de pruebas es
// más problema que solución. Sin esta excepción, el único canal que se puede
// ejercitar de verdad en local queda inutilizable.
//
// La excepción es segura porque el tráfico no sale de la máquina: no hay red
// donde interceptarlo. Se reconoce por dirección, no por texto: "localhost" a
// secas se puede escribir de muchas formas y comparar cadenas dejaría fuera
// 127.0.0.1, ::1 o un nombre que resuelva al bucle local.
func EsLocal(crudo string) bool {
	u, err := url.Parse(strings.TrimSpace(crudo))
	if err != nil || u.Host == "" {
		return false
	}
	anfitrion := u.Hostname()
	if strings.EqualFold(anfitrion, "localhost") {
		return true
	}
	if ip := net.ParseIP(anfitrion); ip != nil {
		return ip.IsLoopback()
	}
	// Un nombre que resuelve al bucle local también vale: es lo que ocurre con
	// las entradas del fichero hosts que se usan para probar.
	ips, err := net.LookupIP(anfitrion)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false
		}
	}
	return len(ips) > 0
}
