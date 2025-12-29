package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

// InfraConfig represents the Encore infrastructure configuration
type InfraConfig struct {
	SQLServers []SQLServer `json:"sql_servers"`
}

// SQLServer represents a PostgreSQL server configuration
type SQLServer struct {
	Host      string                    `json:"host"`
	TLSConfig *TLSConfig                `json:"tls_config,omitempty"`
	Databases map[string]DatabaseConfig `json:"databases"` // key is Encore DB name
}

// TLSConfig represents TLS settings for database connections
type TLSConfig struct {
	Disabled                       bool        `json:"disabled,omitempty"`
	CA                             string      `json:"ca,omitempty"`
	ClientCert                     *ClientCert `json:"client_cert,omitempty"`
	DisableTLSHostnameVerification bool        `json:"disable_tls_hostname_verification,omitempty"`
	DisableCAValidation            bool        `json:"disable_ca_validation,omitempty"`
}

// ClientCert represents client certificate configuration
type ClientCert struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// DatabaseConfig represents individual database connection config
type DatabaseConfig struct {
	Name           StringOrEnvRef `json:"name"`            // actual PG database name
	Username       StringOrEnvRef `json:"username"`        // database username
	Password       StringOrEnvRef `json:"password"`        // database password
	MinConnections *int           `json:"min_connections"` // optional min pool size
	MaxConnections *int           `json:"max_connections"` // optional max pool size
}

// StringOrEnvRef handles both string literals and {"$env": "VAR"} references
type StringOrEnvRef struct {
	Value  string
	EnvVar string
	IsEnv  bool
}

// UnmarshalJSON implements custom unmarshaling for StringOrEnvRef
func (s *StringOrEnvRef) UnmarshalJSON(data []byte) error {
	// Try parsing as simple string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Value = str
		s.IsEnv = false
		return nil
	}

	// Try parsing as {"$env": "VAR_NAME"} object
	var envRef struct {
		Env string `json:"$env"`
	}
	if err := json.Unmarshal(data, &envRef); err != nil {
		return fmt.Errorf("invalid value: expected string or {\"$env\": \"VAR_NAME\"}")
	}

	if envRef.Env == "" {
		return fmt.Errorf("empty $env reference")
	}

	s.EnvVar = envRef.Env
	s.IsEnv = true
	return nil
}

// Resolve returns the actual value, resolving env vars if needed
func (s *StringOrEnvRef) Resolve() (string, error) {
	if !s.IsEnv {
		return s.Value, nil
	}

	value := os.Getenv(s.EnvVar)
	if value == "" {
		return "", fmt.Errorf("environment variable %s is not set", s.EnvVar)
	}
	return value, nil
}

// MustResolve returns the value or panics if env var is not set
func (s *StringOrEnvRef) MustResolve() string {
	v, err := s.Resolve()
	if err != nil {
		panic(err)
	}
	return v
}

// String returns a safe representation (not revealing secrets)
func (s *StringOrEnvRef) String() string {
	if s.IsEnv {
		return fmt.Sprintf("$env:%s", s.EnvVar)
	}
	return s.Value
}

// LoadInfraConfig loads and parses an InfraConfig JSON file
func LoadInfraConfig(path string) (*InfraConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading infra config: %w", err)
	}

	var config InfraConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parsing infra config: %w", err)
	}

	return &config, nil
}

// GetMapping returns a DatabaseMapping for the given Encore database name
func (c *InfraConfig) GetMapping(encoreName string) (*types.DatabaseMapping, error) {
	for _, server := range c.SQLServers {
		if dbConfig, ok := server.Databases[encoreName]; ok {
			// Parse host and port
			host, port := parseHostPort(server.Host)

			// Resolve credentials
			username, err := dbConfig.Username.Resolve()
			if err != nil {
				return nil, fmt.Errorf("resolving username for %s: %w", encoreName, err)
			}

			password, err := dbConfig.Password.Resolve()
			if err != nil {
				return nil, fmt.Errorf("resolving password for %s: %w", encoreName, err)
			}

			// Resolve actual database name (defaults to Encore name if not specified)
			pgDBName, err := dbConfig.Name.Resolve()
			if err != nil {
				// If name resolution fails but value is empty, use encore name
				if dbConfig.Name.Value == "" && !dbConfig.Name.IsEnv {
					pgDBName = encoreName
				} else {
					return nil, fmt.Errorf("resolving database name for %s: %w", encoreName, err)
				}
			}
			if pgDBName == "" {
				pgDBName = encoreName
			}

			// Determine SSL mode
			// Default to disable; only enable if client cert is specified and TLS is not disabled
			sslMode := "disable"
			if server.TLSConfig != nil && !server.TLSConfig.Disabled && server.TLSConfig.ClientCert != nil {
				sslMode = "require"
			}

			return &types.DatabaseMapping{
				EncoreName: encoreName,
				PGDBName:   pgDBName,
				Host:       host,
				Port:       port,
				Username:   username,
				Password:   password,
				SSLMode:    sslMode,
			}, nil
		}
	}

	return nil, &types.ConfigError{
		Field:   "sql_servers.databases",
		Message: fmt.Sprintf("database %q not found in InfraConfig", encoreName),
	}
}

// ListDatabaseNames returns all Encore database names defined in the config
func (c *InfraConfig) ListDatabaseNames() []string {
	var names []string
	for _, server := range c.SQLServers {
		for name := range server.Databases {
			names = append(names, name)
		}
	}
	return names
}

// parseHostPort splits a host string into host and port components
func parseHostPort(hostStr string) (host, port string) {
	host = hostStr
	port = "5432" // default PostgreSQL port

	if idx := strings.LastIndex(hostStr, ":"); idx != -1 {
		host = hostStr[:idx]
		port = hostStr[idx+1:]
	}

	return host, port
}
