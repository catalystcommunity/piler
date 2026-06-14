// Package coredb is the embedded migrations module for piler's long-term
// state. The server runs these with goose on startup (and via the `migrate`
// subcommand). Keeping migrations in their own module mirrors longhouse and
// lets other binaries embed the same schema.
package coredb

import "embed"

// Migrations holds the SQL migration files, embedded at build time so the
// server binary is self-contained (no migrations directory to ship).
//
//go:embed migrations/*.sql
var Migrations embed.FS
