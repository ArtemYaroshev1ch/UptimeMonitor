package postgres

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/artemyaroshev1ch/uptime-monitor/migrations"
)

// NewMigrator returns a goose provider for the migrations embedded in the
// binary. Applied versions are recorded in the goose_db_version table.
func NewMigrator(db *sql.DB) (*goose.Provider, error) {
	// A session-level advisory lock serializes concurrent runs: if two
	// deploys start "migrate up" at the same time, the second one waits for
	// the first instead of applying the same migration twice. The lock is
	// released when the session ends, even if the process is killed.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("postgres: migration lock: %w", err)
	}

	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("postgres: migrations: %w", err)
	}
	return p, nil
}
