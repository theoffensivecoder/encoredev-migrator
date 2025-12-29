package migrate

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/theoffensivecoder/encoredev-migrator/internal/config"
	"github.com/theoffensivecoder/encoredev-migrator/internal/discovery"
	"github.com/theoffensivecoder/encoredev-migrator/internal/logging"
	"github.com/theoffensivecoder/encoredev-migrator/internal/migration"
	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

// Run executes the CLI application
func Run(ctx context.Context, args []string) error {
	app := &cli.Command{
		Name:  "encore-migrate",
		Usage: "Run database migrations for Encore.dev applications",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "config",
				Aliases:  []string{"c"},
				Usage:    "Path to InfraConfig JSON file",
				Required: true,
			},
			&cli.StringFlag{
				Name:    "app",
				Aliases: []string{"a"},
				Usage:   "Path to Encore application root",
				Value:   ".",
			},
			&cli.StringFlag{
				Name:    "manifest",
				Aliases: []string{"m"},
				Usage:   "Path to manifest file (overrides AST discovery)",
			},
			&cli.BoolFlag{
				Name:    "verbose",
				Aliases: []string{"v"},
				Usage:   "Enable verbose output",
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "Enable debug logging",
			},
			&cli.StringFlag{
				Name:  "host",
				Usage: "Override database host (e.g., tailscale-hostname:5432)",
			},
			&cli.StringFlag{
				Name:    "user",
				Aliases: []string{"u"},
				Usage:   "Override database username",
			},
			&cli.StringFlag{
				Name:    "password",
				Aliases: []string{"p"},
				Usage:   "Override database password",
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			logging.Setup(cmd.Bool("debug"))
			slog.Debug("debug logging enabled")
			return ctx, nil
		},
		Commands: []*cli.Command{
			upCommand(),
			downCommand(),
			statusCommand(),
			listCommand(),
			forceCommand(),
		},
	}

	return app.Run(ctx, args)
}

func upCommand() *cli.Command {
	return &cli.Command{
		Name:  "up",
		Usage: "Apply pending migrations",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "database",
				Aliases: []string{"d"},
				Usage:   "Specific Encore database name to migrate (default: all)",
			},
			&cli.IntFlag{
				Name:  "steps",
				Usage: "Number of migrations to apply (default: all pending)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runMigrations(ctx, cmd, "up")
		},
	}
}

func downCommand() *cli.Command {
	return &cli.Command{
		Name:  "down",
		Usage: "Rollback migrations",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "database",
				Aliases: []string{"d"},
				Usage:   "Specific Encore database name to migrate (default: all)",
			},
			&cli.IntFlag{
				Name:  "steps",
				Usage: "Number of migrations to rollback (default: 1)",
				Value: 1,
			},
			&cli.BoolFlag{
				Name:  "all",
				Usage: "Rollback all migrations (dangerous!)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runMigrations(ctx, cmd, "down")
		},
	}
}

func statusCommand() *cli.Command {
	return &cli.Command{
		Name:  "status",
		Usage: "Show migration status for all databases",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "database",
				Aliases: []string{"d"},
				Usage:   "Specific Encore database name to check (default: all)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return showStatus(ctx, cmd)
		},
	}
}

func listCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List discovered Encore databases",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return listDatabases(ctx, cmd)
		},
	}
}

func forceCommand() *cli.Command {
	return &cli.Command{
		Name:  "force",
		Usage: "Force set migration version (for recovery from dirty state)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "database",
				Aliases:  []string{"d"},
				Usage:    "Encore database name",
				Required: true,
			},
			&cli.IntFlag{
				Name:     "version",
				Usage:    "Version to set",
				Required: true,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return forceVersion(ctx, cmd)
		},
	}
}

func runMigrations(ctx context.Context, cmd *cli.Command, direction string) error {
	infraConfig, databases, err := loadConfigAndDiscover(cmd)
	if err != nil {
		return err
	}

	// Filter to specific database if requested
	targetDB := cmd.String("database")
	if targetDB != "" {
		slog.Debug("filtering to specific database", "database", targetDB)
		databases = discovery.FilterDatabases(databases, targetDB)
		if len(databases) == 0 {
			return fmt.Errorf("database %q not found", targetDB)
		}
	}

	if len(databases) == 0 {
		return fmt.Errorf("no databases found")
	}

	slog.Info("starting migrations", "direction", direction, "database_count", len(databases))

	migrator := migration.NewMigrator(cmd.Bool("verbose"))
	var errs []string

	for _, db := range databases {
		mapping, err := infraConfig.GetMapping(db.Name)
		if err != nil {
			slog.Warn("skipping database - no config found", "database", db.Name, "error", err)
			fmt.Fprintf(os.Stderr, "Warning: skipping %q: %v\n", db.Name, err)
			continue
		}

		// Apply host override if provided
		applyConnectionOverrides(cmd, mapping)

		slog.Debug("resolved database mapping",
			"encore_name", db.Name,
			"pg_database", mapping.PGDBName,
			"host", mapping.Host,
			"port", mapping.Port,
			"user", mapping.Username,
			"migrations_path", db.MigrationsPath,
		)

		connStr, err := migration.BuildConnectionString(mapping)
		if err != nil {
			return fmt.Errorf("building connection string for %q: %w", db.Name, err)
		}

		slog.Info("connecting to database",
			"encore_name", db.Name,
			"pg_database", mapping.PGDBName,
			"host", mapping.Host,
			"port", mapping.Port,
		)

		fmt.Printf("Migrating %q (%s)...\n", db.Name, mapping.PGDBName)

		var result *types.MigrationResult
		if direction == "up" {
			steps := int(cmd.Int("steps"))
			slog.Debug("applying up migrations", "database", db.Name, "steps", steps)
			result, err = migrator.Up(connStr, db.MigrationsPath, steps)
		} else {
			steps := int(cmd.Int("steps"))
			if cmd.Bool("all") {
				steps = 0
				slog.Warn("rolling back ALL migrations", "database", db.Name)
			}
			slog.Debug("applying down migrations", "database", db.Name, "steps", steps)
			result, err = migrator.Down(connStr, db.MigrationsPath, steps)
		}

		if err != nil {
			slog.Error("migration failed", "database", db.Name, "error", err)
			errs = append(errs, fmt.Sprintf("%s: %v", db.Name, err))
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			continue
		}

		if result.VersionBefore == result.VersionAfter {
			slog.Info("no migration changes", "database", db.Name, "version", result.VersionAfter)
			fmt.Printf("  No changes (version %d)\n", result.VersionAfter)
		} else {
			slog.Info("migration completed",
				"database", db.Name,
				"version_before", result.VersionBefore,
				"version_after", result.VersionAfter,
			)
			fmt.Printf("  Version: %d -> %d\n", result.VersionBefore, result.VersionAfter)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("migration errors:\n  %s", strings.Join(errs, "\n  "))
	}

	return nil
}

func showStatus(ctx context.Context, cmd *cli.Command) error {
	infraConfig, databases, err := loadConfigAndDiscover(cmd)
	if err != nil {
		return err
	}

	// Filter to specific database if requested
	targetDB := cmd.String("database")
	if targetDB != "" {
		databases = discovery.FilterDatabases(databases, targetDB)
		if len(databases) == 0 {
			return fmt.Errorf("database %q not found", targetDB)
		}
	}

	if len(databases) == 0 {
		return fmt.Errorf("no databases found")
	}

	migrator := migration.NewMigrator(cmd.Bool("verbose"))

	fmt.Printf("%-20s %-30s %-10s %-10s\n", "DATABASE", "PG_NAME", "VERSION", "DIRTY")
	fmt.Println(strings.Repeat("-", 70))

	for _, db := range databases {
		mapping, err := infraConfig.GetMapping(db.Name)
		if err != nil {
			slog.Debug("no config for database", "database", db.Name, "error", err)
			fmt.Printf("%-20s %-30s %-10s %-10s\n", db.Name, "N/A", "error", err.Error())
			continue
		}

		// Apply host override if provided
		applyConnectionOverrides(cmd, mapping)

		slog.Debug("checking status",
			"encore_name", db.Name,
			"pg_database", mapping.PGDBName,
			"host", mapping.Host,
		)

		connStr, err := migration.BuildConnectionString(mapping)
		if err != nil {
			fmt.Printf("%-20s %-30s %-10s %-10s\n", db.Name, mapping.PGDBName, "error", err.Error())
			continue
		}

		status, err := migrator.GetStatus(connStr, db.MigrationsPath)
		if err != nil {
			slog.Debug("failed to get status", "database", db.Name, "error", err)
			fmt.Printf("%-20s %-30s %-10s %-10s\n", db.Name, mapping.PGDBName, "error", err.Error())
			continue
		}

		dirtyStr := "no"
		if status.Dirty {
			dirtyStr = "YES"
		}

		slog.Debug("database status",
			"database", db.Name,
			"version", status.Version,
			"dirty", status.Dirty,
		)

		fmt.Printf("%-20s %-30s %-10d %-10s\n", db.Name, mapping.PGDBName, status.Version, dirtyStr)
	}

	return nil
}

func listDatabases(ctx context.Context, cmd *cli.Command) error {
	appPath := cmd.String("app")
	if appPath == "" {
		appPath = "."
	}

	absPath, err := filepath.Abs(appPath)
	if err != nil {
		return fmt.Errorf("resolving app path: %w", err)
	}

	slog.Debug("discovering databases", "app_path", absPath)

	discoverer := discovery.New(discovery.Options{
		ManifestPath: cmd.String("manifest"),
		Verbose:      cmd.Bool("verbose"),
	})

	databases, err := discoverer.Discover(absPath)
	if err != nil {
		return fmt.Errorf("discovering databases: %w", err)
	}

	slog.Debug("discovery complete", "database_count", len(databases))

	if len(databases) == 0 {
		fmt.Println("No databases found.")
		return nil
	}

	fmt.Printf("%-20s %-50s\n", "DATABASE", "MIGRATIONS PATH")
	fmt.Println(strings.Repeat("-", 70))

	for _, db := range databases {
		fmt.Printf("%-20s %-50s\n", db.Name, db.MigrationsPath)
	}

	return nil
}

func forceVersion(ctx context.Context, cmd *cli.Command) error {
	infraConfig, databases, err := loadConfigAndDiscover(cmd)
	if err != nil {
		return err
	}

	targetDB := cmd.String("database")
	databases = discovery.FilterDatabases(databases, targetDB)
	if len(databases) == 0 {
		return fmt.Errorf("database %q not found", targetDB)
	}

	db := databases[0]
	mapping, err := infraConfig.GetMapping(db.Name)
	if err != nil {
		return fmt.Errorf("getting config for %q: %w", db.Name, err)
	}

	// Apply host override if provided
	applyConnectionOverrides(cmd, mapping)

	connStr, err := migration.BuildConnectionString(mapping)
	if err != nil {
		return fmt.Errorf("building connection string: %w", err)
	}

	version := int(cmd.Int("version"))

	slog.Warn("forcing migration version",
		"database", db.Name,
		"version", version,
	)

	migrator := migration.NewMigrator(cmd.Bool("verbose"))

	if err := migrator.Force(connStr, db.MigrationsPath, version); err != nil {
		return fmt.Errorf("forcing version: %w", err)
	}

	slog.Info("version forced", "database", db.Name, "version", version)
	fmt.Printf("Forced %q to version %d\n", db.Name, version)
	return nil
}

func loadConfigAndDiscover(cmd *cli.Command) (*config.InfraConfig, []types.EncoreDatabase, error) {
	// Load InfraConfig
	configPath := cmd.String("config")
	slog.Debug("loading infra config", "path", configPath)

	infraConfig, err := config.LoadInfraConfig(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading InfraConfig: %w", err)
	}

	slog.Debug("infra config loaded", "sql_servers", len(infraConfig.SQLServers))

	// Get app path
	appPath := cmd.String("app")
	if appPath == "" {
		appPath = "."
	}

	absPath, err := filepath.Abs(appPath)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving app path: %w", err)
	}

	// Discover databases
	manifestPath := cmd.String("manifest")
	slog.Debug("discovering databases",
		"app_path", absPath,
		"manifest_path", manifestPath,
	)

	discoverer := discovery.New(discovery.Options{
		ManifestPath: manifestPath,
		Verbose:      cmd.Bool("verbose"),
	})

	databases, err := discoverer.Discover(absPath)
	if err != nil {
		return nil, nil, fmt.Errorf("discovering databases: %w", err)
	}

	// Deduplicate
	databases = discovery.DeduplicateDatabases(databases)

	slog.Debug("databases discovered", "count", len(databases))
	for _, db := range databases {
		slog.Debug("found database",
			"name", db.Name,
			"migrations_path", db.MigrationsPath,
			"source_file", db.SourceFile,
		)
	}

	return infraConfig, databases, nil
}

// applyConnectionOverrides applies CLI flag overrides for host, user, and password
func applyConnectionOverrides(cmd *cli.Command, mapping *types.DatabaseMapping) {
	// Host override
	if hostOverride := cmd.String("host"); hostOverride != "" {
		originalHost := mapping.Host
		originalPort := mapping.Port

		// Parse host:port if port is included
		if idx := strings.LastIndex(hostOverride, ":"); idx != -1 {
			mapping.Host = hostOverride[:idx]
			mapping.Port = hostOverride[idx+1:]
		} else {
			mapping.Host = hostOverride
		}

		slog.Info("host override applied",
			"original_host", originalHost,
			"original_port", originalPort,
			"new_host", mapping.Host,
			"new_port", mapping.Port,
		)
	}

	// Username override
	if userOverride := cmd.String("user"); userOverride != "" {
		slog.Info("user override applied",
			"original_user", mapping.Username,
			"new_user", userOverride,
		)
		mapping.Username = userOverride
	}

	// Password override
	if passOverride := cmd.String("password"); passOverride != "" {
		slog.Info("password override applied")
		mapping.Password = passOverride
	}
}
