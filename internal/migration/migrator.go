package migration

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

// Migrator handles database migrations using golang-migrate
type Migrator struct {
	Verbose bool
}

// NewMigrator creates a new Migrator instance
func NewMigrator(verbose bool) *Migrator {
	return &Migrator{Verbose: verbose}
}

// Up applies pending migrations
// If steps is 0 or negative, applies all pending migrations
// If steps is positive, applies that many migrations
func (m *Migrator) Up(connStr, migrationsPath string, steps int) (*types.MigrationResult, error) {
	sourceURL := BuildSourceURL(migrationsPath)

	slog.Debug("creating migration instance",
		"source_url", sourceURL,
		"direction", "up",
	)

	mig, err := migrate.New(sourceURL, connStr)
	if err != nil {
		slog.Error("failed to create migrator", "error", err)
		return nil, fmt.Errorf("creating migrator: %w", err)
	}
	defer mig.Close()

	versionBefore, dirty, _ := mig.Version()
	slog.Debug("current migration state",
		"version", versionBefore,
		"dirty", dirty,
	)

	if dirty {
		slog.Error("database in dirty state", "version", versionBefore)
		return nil, fmt.Errorf("database is in dirty state at version %d, manual intervention required", versionBefore)
	}

	var migErr error
	if steps > 0 {
		slog.Debug("applying specific number of migrations", "steps", steps)
		migErr = mig.Steps(steps)
	} else {
		slog.Debug("applying all pending migrations")
		migErr = mig.Up()
	}

	// migrate.ErrNoChange is not an error for our purposes
	if migErr != nil && !errors.Is(migErr, migrate.ErrNoChange) {
		slog.Error("migration failed", "error", migErr)
		return nil, fmt.Errorf("running migrations: %w", migErr)
	}

	versionAfter, _, _ := mig.Version()
	slog.Debug("migration complete",
		"version_before", versionBefore,
		"version_after", versionAfter,
	)

	return &types.MigrationResult{
		Direction:     "up",
		VersionBefore: versionBefore,
		VersionAfter:  versionAfter,
	}, nil
}

// Down rolls back migrations
// If steps is 0 or negative, rolls back ALL migrations (dangerous!)
// If steps is positive, rolls back that many migrations
func (m *Migrator) Down(connStr, migrationsPath string, steps int) (*types.MigrationResult, error) {
	sourceURL := BuildSourceURL(migrationsPath)

	slog.Debug("creating migration instance",
		"source_url", sourceURL,
		"direction", "down",
	)

	mig, err := migrate.New(sourceURL, connStr)
	if err != nil {
		slog.Error("failed to create migrator", "error", err)
		return nil, fmt.Errorf("creating migrator: %w", err)
	}
	defer mig.Close()

	versionBefore, dirty, _ := mig.Version()
	slog.Debug("current migration state",
		"version", versionBefore,
		"dirty", dirty,
	)

	if dirty {
		slog.Error("database in dirty state", "version", versionBefore)
		return nil, fmt.Errorf("database is in dirty state at version %d, manual intervention required", versionBefore)
	}

	var migErr error
	if steps > 0 {
		slog.Debug("rolling back specific number of migrations", "steps", steps)
		// Negative steps for down migrations
		migErr = mig.Steps(-steps)
	} else {
		slog.Warn("rolling back ALL migrations")
		// Roll back all migrations
		migErr = mig.Down()
	}

	// migrate.ErrNoChange is not an error for our purposes
	if migErr != nil && !errors.Is(migErr, migrate.ErrNoChange) {
		slog.Error("migration rollback failed", "error", migErr)
		return nil, fmt.Errorf("running migrations: %w", migErr)
	}

	versionAfter, _, _ := mig.Version()
	slog.Debug("rollback complete",
		"version_before", versionBefore,
		"version_after", versionAfter,
	)

	return &types.MigrationResult{
		Direction:     "down",
		VersionBefore: versionBefore,
		VersionAfter:  versionAfter,
	}, nil
}

// Status returns the current migration version and dirty state
type Status struct {
	Version uint
	Dirty   bool
	Error   error
}

// GetStatus returns the current migration status for a database
func (m *Migrator) GetStatus(connStr, migrationsPath string) (*Status, error) {
	sourceURL := BuildSourceURL(migrationsPath)

	mig, err := migrate.New(sourceURL, connStr)
	if err != nil {
		return nil, fmt.Errorf("creating migrator: %w", err)
	}
	defer mig.Close()

	version, dirty, err := mig.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			// No migrations applied yet
			return &Status{Version: 0, Dirty: false}, nil
		}
		return nil, fmt.Errorf("getting version: %w", err)
	}

	return &Status{
		Version: version,
		Dirty:   dirty,
	}, nil
}

// Force sets the migration version without running any migrations
// This is useful for recovering from a dirty state
func (m *Migrator) Force(connStr, migrationsPath string, version int) error {
	sourceURL := BuildSourceURL(migrationsPath)

	mig, err := migrate.New(sourceURL, connStr)
	if err != nil {
		return fmt.Errorf("creating migrator: %w", err)
	}
	defer mig.Close()

	if err := mig.Force(version); err != nil {
		return fmt.Errorf("forcing version: %w", err)
	}

	return nil
}
