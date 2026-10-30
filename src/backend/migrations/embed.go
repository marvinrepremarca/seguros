// Package migrations embebe las migraciones SQL de todos los módulos: una carpeta por módulo
// (migrations/<modulo>/NNNNN_descripcion.sql), aplicadas por cmd/migrate. Nunca edites una
// migración ya aplicada: crea una nueva (CLAUDE.md regla 11).
package migrations

import "embed"

// FS contiene las carpetas de migraciones por módulo.
//
//go:embed *
var FS embed.FS
