package discovery

import (
	"fmt"

	"github.com/theoffensivecoder/encoredev-migrator/internal/config"
	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

// Discoverer finds Encore database configurations
type Discoverer interface {
	Discover(rootPath string) ([]types.EncoreDatabase, error)
}

// Options configures the discovery process
type Options struct {
	ManifestPath string // If set, use manifest instead of AST discovery
	Verbose      bool
}

// New creates a Discoverer based on options
func New(opts Options) Discoverer {
	if opts.ManifestPath != "" {
		return &manifestDiscoverer{
			path:    opts.ManifestPath,
			verbose: opts.Verbose,
		}
	}
	return &ASTDiscoverer{
		Verbose: opts.Verbose,
	}
}

// manifestDiscoverer uses a manifest file for database discovery
type manifestDiscoverer struct {
	path    string
	verbose bool
}

// Discover loads databases from the manifest file
func (d *manifestDiscoverer) Discover(rootPath string) ([]types.EncoreDatabase, error) {
	if d.verbose {
		fmt.Printf("Loading databases from manifest: %s\n", d.path)
	}

	databases, err := config.LoadManifest(d.path, rootPath)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	if d.verbose {
		for _, db := range databases {
			fmt.Printf("Found database %q with migrations at %s\n", db.Name, db.MigrationsPath)
		}
	}

	return databases, nil
}

// FilterDatabases filters a list of databases to only include the specified one
func FilterDatabases(databases []types.EncoreDatabase, targetDB string) []types.EncoreDatabase {
	if targetDB == "" {
		return databases
	}

	for _, db := range databases {
		if db.Name == targetDB {
			return []types.EncoreDatabase{db}
		}
	}

	return nil
}

// DeduplicateDatabases removes duplicate database entries (same name)
func DeduplicateDatabases(databases []types.EncoreDatabase) []types.EncoreDatabase {
	seen := make(map[string]bool)
	var result []types.EncoreDatabase

	for _, db := range databases {
		if !seen[db.Name] {
			seen[db.Name] = true
			result = append(result, db)
		}
	}

	return result
}
