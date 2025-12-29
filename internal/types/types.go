package types

import "fmt"

// EncoreDatabase represents a discovered Encore database configuration
type EncoreDatabase struct {
	Name           string // Encore database name (e.g., "users")
	MigrationsPath string // Absolute path to migrations directory
	SourceFile     string // Go file where this was discovered (for debugging)
}

// DatabaseMapping maps Encore DB name to actual PostgreSQL config
type DatabaseMapping struct {
	EncoreName string
	PGDBName   string
	Host       string
	Port       string
	Username   string
	Password   string
	SSLMode    string
}

// MigrationResult captures the outcome of a migration operation
type MigrationResult struct {
	Database      string
	Direction     string // "up" or "down"
	VersionBefore uint
	VersionAfter  uint
	Error         error
}

// DiscoveryError indicates a problem during database discovery
type DiscoveryError struct {
	File    string
	Message string
	Cause   error
}

func (e *DiscoveryError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("discovery error in %s: %s: %v", e.File, e.Message, e.Cause)
	}
	return fmt.Sprintf("discovery error in %s: %s", e.File, e.Message)
}

func (e *DiscoveryError) Unwrap() error {
	return e.Cause
}

// ConfigError indicates a problem with InfraConfig
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("config error in %s: %s", e.Field, e.Message)
}

// MigrationError wraps migration failures with context
type MigrationError struct {
	Database  string
	Direction string
	Version   uint
	Cause     error
}

func (e *MigrationError) Error() string {
	return fmt.Sprintf("migration %s failed for %s at version %d: %v",
		e.Direction, e.Database, e.Version, e.Cause)
}

func (e *MigrationError) Unwrap() error {
	return e.Cause
}
