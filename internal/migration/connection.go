package migration

import (
	"fmt"
	"net/url"

	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

// BuildConnectionString creates a PostgreSQL connection URL from DatabaseMapping
func BuildConnectionString(mapping *types.DatabaseMapping) (string, error) {
	if mapping.Host == "" {
		return "", fmt.Errorf("host is required")
	}
	if mapping.PGDBName == "" {
		return "", fmt.Errorf("database name is required")
	}
	if mapping.Username == "" {
		return "", fmt.Errorf("username is required")
	}

	// URL-encode password (may contain special chars)
	password := url.QueryEscape(mapping.Password)

	port := mapping.Port
	if port == "" {
		port = "5432"
	}

	sslMode := mapping.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	// Format: postgres://user:pass@host:port/dbname?sslmode=disable
	connStr := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(mapping.Username),
		password,
		mapping.Host,
		port,
		mapping.PGDBName,
		sslMode,
	)

	return connStr, nil
}

// BuildSourceURL creates a file source URL for golang-migrate
func BuildSourceURL(migrationsPath string) string {
	return fmt.Sprintf("file://%s", migrationsPath)
}
