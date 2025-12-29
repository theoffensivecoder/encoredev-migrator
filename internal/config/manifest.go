package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
	"gopkg.in/yaml.v3"
)

// Manifest provides manual override for database discovery
type Manifest struct {
	Version   string             `yaml:"version" json:"version"`
	Databases []ManifestDatabase `yaml:"databases" json:"databases"`
}

// ManifestDatabase defines a database in the manifest
type ManifestDatabase struct {
	Name       string `yaml:"name" json:"name"`
	Migrations string `yaml:"migrations" json:"migrations"`
}

// LoadManifest loads a manifest file and returns discovered databases
func LoadManifest(manifestPath string, rootDir string) ([]types.EncoreDatabase, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	var manifest Manifest

	// Try YAML first, then JSON
	if strings.HasSuffix(manifestPath, ".json") {
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parsing JSON manifest: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("parsing YAML manifest: %w", err)
		}
	}

	if len(manifest.Databases) == 0 {
		return nil, fmt.Errorf("manifest contains no databases")
	}

	var databases []types.EncoreDatabase
	for _, db := range manifest.Databases {
		if db.Name == "" {
			return nil, fmt.Errorf("manifest database entry missing name")
		}
		if db.Migrations == "" {
			return nil, fmt.Errorf("manifest database %q missing migrations path", db.Name)
		}

		// Resolve relative path from the root directory
		migrationsPath := db.Migrations
		if !filepath.IsAbs(migrationsPath) {
			migrationsPath = filepath.Join(rootDir, db.Migrations)
		}

		// Validate the migrations directory exists
		if _, err := os.Stat(migrationsPath); os.IsNotExist(err) {
			return nil, fmt.Errorf("migrations directory for %q does not exist: %s", db.Name, migrationsPath)
		}

		databases = append(databases, types.EncoreDatabase{
			Name:           db.Name,
			MigrationsPath: migrationsPath,
			SourceFile:     manifestPath,
		})
	}

	return databases, nil
}

// DefaultManifestPaths returns the default paths to look for a manifest file
func DefaultManifestPaths() []string {
	return []string{
		"encore-databases.yaml",
		"encore-databases.yml",
		"encore-databases.json",
		".encore/databases.yaml",
		".encore/databases.yml",
		".encore/databases.json",
	}
}

// FindManifest looks for a manifest file in the given directory
func FindManifest(rootDir string) string {
	for _, name := range DefaultManifestPaths() {
		path := filepath.Join(rootDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
