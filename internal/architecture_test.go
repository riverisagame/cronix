package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDDDArchitectureBoundaries(t *testing.T) {
	// 1. Verify that the necessary DDD directories exist
	requiredDirs := []string{
		"domain",
		"application",
		"infrastructure",
		"interfaces",
	}

	for _, dir := range requiredDirs {
		path := filepath.Join(".", dir)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Errorf("DDD Violation: Required directory %s does not exist", path)
		}
	}

	// If directories don't exist, we can't test imports yet, so stop here to fail predictably
	if t.Failed() {
		t.FailNow()
	}

	// 2. Verify Dependency Rule: Domain layer must not depend on Application, Infrastructure, or Interfaces
	domainDir := filepath.Join(".", "domain")
	fset := token.NewFileSet()

	err := filepath.Walk(domainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") {
			node, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("Failed to parse Go file %s: %v", path, err)
			}

			for _, imp := range node.Imports {
				importPath := strings.Trim(imp.Path.Value, "\"")
				
				// Domain must not depend on outer layers
				forbiddenPrefixes := []string{
					"cronix/internal/application",
					"cronix/internal/infrastructure",
					"cronix/internal/interfaces",
				}

				for _, forbidden := range forbiddenPrefixes {
					if strings.HasPrefix(importPath, forbidden) {
						t.Errorf("DDD Violation in %s: Domain layer must not import %s", path, importPath)
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		t.Fatalf("Failed to walk domain directory: %v", err)
	}
}
