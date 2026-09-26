package db

import "embed"

// Migrations holds the goose SQL migrations as migrations/<version>_<name>.sql.
// Each file has an Up and a Down section.
//
//go:embed migrations/*.sql
var Migrations embed.FS
