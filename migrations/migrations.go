// Package migrations embeds the SQL schema migrations. Files are applied in
// lexical order and must be named NNNN_description.sql.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
