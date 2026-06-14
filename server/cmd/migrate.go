package cmd

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/catalystcommunity/piler/coredb"
	"github.com/catalystcommunity/piler/server/internal/config"
)

// Migrate runs database migrations and exits.
func Migrate(flags map[string]string) error {
	config.ApplyFlags(flags)
	return RunMigrations()
}

// RunMigrations applies all pending goose migrations from the embedded
// coredb FS. Safe to call on every boot.
func RunMigrations() error {
	db, err := sql.Open("pgx", config.DBUri)
	if err != nil {
		return fmt.Errorf("opening db for migrations: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(coredb.Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}
