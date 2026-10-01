// Package db holds the database schema of the service.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedded embed.FS

// NewProvider returns a goose provider for the embedded migrations.
// Provider.Close closes sqlDB.
func NewProvider(sqlDB *sql.DB) (*goose.Provider, error) {
	migrations, err := fs.Sub(embedded, "migrations")
	if err != nil {
		return nil, fmt.Errorf("open the migrations: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations)
	if err != nil {
		return nil, fmt.Errorf("make the goose provider: %w", err)
	}

	return provider, nil
}
