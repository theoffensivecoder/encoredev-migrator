package discovery

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/theoffensivecoder/encoredev-migrator/internal/types"
)

const encoreSQLDBImport = "encore.dev/storage/sqldb"

// ASTDiscoverer discovers Encore databases by parsing Go source files
type ASTDiscoverer struct {
	Verbose bool
	Errors  []error // Non-fatal errors encountered during discovery
}

// Discover walks the directory tree and finds all sqldb.NewDatabase calls
func (d *ASTDiscoverer) Discover(rootPath string) ([]types.EncoreDatabase, error) {
	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolving root path: %w", err)
	}

	var databases []types.EncoreDatabase

	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if entry.IsDir() {
			name := entry.Name()
			// Skip vendor, testdata, hidden directories, and common non-source dirs
			if name == "vendor" || name == "testdata" || name == "node_modules" ||
				strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				return filepath.SkipDir
			}
			return nil
		}

		// Only process Go files
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip test files
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		dbs, err := d.parseFile(path)
		if err != nil {
			// Record error but continue with other files
			d.Errors = append(d.Errors, &types.DiscoveryError{
				File:    path,
				Message: "failed to parse",
				Cause:   err,
			})
			return nil
		}

		databases = append(databases, dbs...)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	return databases, nil
}

// parseFile parses a single Go file and extracts database definitions
func (d *ASTDiscoverer) parseFile(filePath string) ([]types.EncoreDatabase, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Find the import alias for encore.dev/storage/sqldb
	sqldbAlias := findImportAlias(node, encoreSQLDBImport)
	if sqldbAlias == "" {
		// File doesn't import sqldb, skip it
		return nil, nil
	}

	var databases []types.EncoreDatabase

	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Check if this is a sqldb.NewDatabase call
		if !isNewDatabaseCall(call, sqldbAlias) {
			return true
		}

		db, err := d.extractDatabaseConfig(call, filePath)
		if err != nil {
			d.Errors = append(d.Errors, &types.DiscoveryError{
				File:    filePath,
				Message: "failed to extract database config",
				Cause:   err,
			})
			return true
		}

		if d.Verbose {
			fmt.Printf("Found database %q in %s\n", db.Name, filePath)
		}

		databases = append(databases, db)
		return true
	})

	return databases, nil
}

// extractDatabaseConfig extracts the database name and migrations path from a NewDatabase call
func (d *ASTDiscoverer) extractDatabaseConfig(call *ast.CallExpr, filePath string) (types.EncoreDatabase, error) {
	// NewDatabase takes 2 arguments: name (string) and config (DatabaseConfig)
	if len(call.Args) < 2 {
		return types.EncoreDatabase{}, fmt.Errorf("expected 2 arguments to NewDatabase, got %d", len(call.Args))
	}

	// Extract database name from first argument
	dbName, err := extractStringLiteral(call.Args[0])
	if err != nil {
		return types.EncoreDatabase{}, fmt.Errorf("extracting database name: %w", err)
	}

	// Extract migrations path from DatabaseConfig struct
	migrationsPath, err := extractMigrationsPath(call.Args[1])
	if err != nil {
		return types.EncoreDatabase{}, fmt.Errorf("extracting migrations path: %w", err)
	}

	// Resolve relative path from the Go file's directory
	fileDir := filepath.Dir(filePath)
	absPath := filepath.Join(fileDir, migrationsPath)

	// Clean the path
	absPath = filepath.Clean(absPath)

	// Verify the migrations directory exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		d.Errors = append(d.Errors, &types.DiscoveryError{
			File:    filePath,
			Message: fmt.Sprintf("migrations directory does not exist: %s", absPath),
		})
	}

	return types.EncoreDatabase{
		Name:           dbName,
		MigrationsPath: absPath,
		SourceFile:     filePath,
	}, nil
}

// findImportAlias finds the alias used for a package import
// Returns the alias name, or the package name if no alias, or empty string if not imported
func findImportAlias(node *ast.File, importPath string) string {
	for _, imp := range node.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if path != importPath {
			continue
		}

		// If there's an explicit alias, use it
		if imp.Name != nil {
			// Handle dot imports and blank imports
			if imp.Name.Name == "." || imp.Name.Name == "_" {
				return ""
			}
			return imp.Name.Name
		}

		// No alias, use the last component of the import path
		parts := strings.Split(path, "/")
		return parts[len(parts)-1]
	}
	return ""
}

// isNewDatabaseCall checks if a call expression is sqldb.NewDatabase
func isNewDatabaseCall(call *ast.CallExpr, sqldbAlias string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check the method name
	if sel.Sel.Name != "NewDatabase" {
		return false
	}

	// Check the package name
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}

	return ident.Name == sqldbAlias
}

// extractStringLiteral extracts a string value from an AST expression
func extractStringLiteral(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return "", fmt.Errorf("expected string literal, got %T", expr)
	}

	if lit.Kind != token.STRING {
		return "", fmt.Errorf("expected string literal, got %v", lit.Kind)
	}

	// Remove quotes from the string
	value := lit.Value
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1], nil
		}
		if value[0] == '`' && value[len(value)-1] == '`' {
			return value[1 : len(value)-1], nil
		}
	}

	return value, nil
}

// extractMigrationsPath extracts the Migrations field from a DatabaseConfig composite literal
func extractMigrationsPath(expr ast.Expr) (string, error) {
	// Handle: sqldb.DatabaseConfig{Migrations: "./migrations"}
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return "", fmt.Errorf("expected composite literal for DatabaseConfig, got %T", expr)
	}

	for _, elt := range composite.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "Migrations" {
			continue
		}

		return extractStringLiteral(kv.Value)
	}

	return "", fmt.Errorf("Migrations field not found in DatabaseConfig")
}
