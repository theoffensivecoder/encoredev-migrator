package manifest

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/theoffensivecoder/encoredev-migrator/internal/config"
	"github.com/theoffensivecoder/encoredev-migrator/internal/discovery"
	"gopkg.in/yaml.v3"
)

// GenerateOptions configures the manifest generation.
type GenerateOptions struct {
	AppPath    string // Source Encore app directory
	OutputPath string // Manifest output path
	CopyTo     string // Optional: copy migrations to this directory
	Format     string // yaml or json (auto-detected from OutputPath if empty)
	Verbose    bool
}

// Generator creates manifest files from discovered Encore databases.
type Generator struct {
	opts GenerateOptions
}

// NewGenerator creates a new manifest generator.
func NewGenerator(opts GenerateOptions) *Generator {
	return &Generator{opts: opts}
}

// Generate discovers databases, optionally copies migrations, and writes the manifest.
func (g *Generator) Generate() error {
	// Resolve app path
	appPath, err := filepath.Abs(g.opts.AppPath)
	if err != nil {
		return fmt.Errorf("resolving app path: %w", err)
	}

	slog.Debug("discovering databases", "app_path", appPath)

	// Discover databases using AST
	discoverer := discovery.New(discovery.Options{
		Verbose: g.opts.Verbose,
	})

	databases, err := discoverer.Discover(appPath)
	if err != nil {
		return fmt.Errorf("discovering databases: %w", err)
	}

	databases = discovery.DeduplicateDatabases(databases)

	if len(databases) == 0 {
		return fmt.Errorf("no databases found in %s", appPath)
	}

	slog.Info("discovered databases", "count", len(databases))
	for _, db := range databases {
		slog.Debug("found database",
			"name", db.Name,
			"migrations_path", db.MigrationsPath,
		)
	}

	// Resolve output path
	outputPath, err := filepath.Abs(g.opts.OutputPath)
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}
	outputDir := filepath.Dir(outputPath)

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	// Build manifest
	manifest := config.Manifest{
		Version:   "1",
		Databases: make([]config.ManifestDatabase, 0, len(databases)),
	}

	// Process each database
	for _, db := range databases {
		var migrationsPath string

		if g.opts.CopyTo != "" {
			// Copy migrations to target directory
			copyTo, err := filepath.Abs(g.opts.CopyTo)
			if err != nil {
				return fmt.Errorf("resolving copy-to path: %w", err)
			}

			targetDir := filepath.Join(copyTo, db.Name)

			slog.Info("copying migrations",
				"database", db.Name,
				"from", db.MigrationsPath,
				"to", targetDir,
			)

			if err := CopyDirectory(db.MigrationsPath, targetDir); err != nil {
				return fmt.Errorf("copying migrations for %q: %w", db.Name, err)
			}

			// Calculate relative path from manifest to copied migrations
			migrationsPath, err = filepath.Rel(outputDir, targetDir)
			if err != nil {
				slog.Warn("couldn't make path relative, using absolute",
					"database", db.Name,
					"error", err,
				)
				migrationsPath = targetDir
			}
		} else {
			// Use path relative to app root
			migrationsPath, err = filepath.Rel(appPath, db.MigrationsPath)
			if err != nil {
				slog.Warn("couldn't make path relative, using absolute",
					"database", db.Name,
					"error", err,
				)
				migrationsPath = db.MigrationsPath
			}
		}

		// Ensure path uses forward slashes for portability
		migrationsPath = filepath.ToSlash(migrationsPath)

		manifest.Databases = append(manifest.Databases, config.ManifestDatabase{
			Name:       db.Name,
			Migrations: migrationsPath,
		})
	}

	// Determine format
	format := g.opts.Format
	if format == "" {
		if strings.HasSuffix(outputPath, ".json") {
			format = "json"
		} else {
			format = "yaml"
		}
	}

	// Marshal manifest
	var data []byte
	if format == "json" {
		data, err = json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return fmt.Errorf("marshaling JSON: %w", err)
		}
		data = append(data, '\n')
	} else {
		data, err = yaml.Marshal(manifest)
		if err != nil {
			return fmt.Errorf("marshaling YAML: %w", err)
		}
	}

	// Write manifest file
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	slog.Info("manifest generated",
		"path", outputPath,
		"format", format,
		"databases", len(manifest.Databases),
	)

	return nil
}
