// Package migrations empaqueta el esquema SQL dentro del binario.
//
// Embebiéndolas, desplegar Integra es copiar un ejecutable: no hay que
// acordarse de subir también una carpeta de ficheros .sql, ni existe la
// posibilidad de que el binario y el esquema vayan desincronizados.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
