// Package migrations embebe los archivos .sql dentro del binario.
// Así la imagen Docker no necesita copiar la carpeta migrations aparte.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
