package composition_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// registerHandlerCalls returns every "pkg.RegisterHandlers"-family selector
// call in the file at path — RegisterHandlers, RegisterExternalHandlers,
// RegisterSupplierHandlers, RegisterReconciliationHandlers, RegisterHealth,
// and RegisterMeHandler, which together are every function that mounts one
// or more HTTP operations onto a huma.API. Comparing these sets between
// cmd/api/main.go and schema_registration.go is what
// TestSchemaRegistrationMirrorsMainGoRouteInventory uses to catch drift: a
// route wired into the real server but never added to the schema-only
// composition would ship completely undocumented in the OpenAPI export.
func registerHandlerCalls(t *testing.T, path string) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if !strings.HasPrefix(name, "Register") {
			return true
		}
		found[pkg.Name+"."+name] = true
		return true
	})
	return found
}

// TestSchemaRegistrationMirrorsMainGoRouteInventory is Checkpoint 12's
// closure guard: composition.RegisterAllForSchema (used by cmd/openapi and
// the schema tests in this package) must call every Register* function the
// shipping route-mounting composition root (composition/httpapp.go, which
// cmd/api/main.go delegates to via composition.NewHTTPHandler) calls, on the
// same module packages. This is the same AST-comparison technique
// compositionroot_test.go already uses to keep the shipping route inventory
// and tenanttest/router.go in sync — applied here to the third composition
// root added in Checkpoint 6.
func TestSchemaRegistrationMirrorsMainGoRouteInventory(t *testing.T) {
	root := backendRoot(t)
	mainPath := filepath.Join(root, "internal", "platform", "composition", "httpapp.go")
	schemaPath := filepath.Join(root, "internal", "platform", "composition", "schema_registration.go")

	mainCalls := registerHandlerCalls(t, mainPath)
	schemaCalls := registerHandlerCalls(t, schemaPath)

	var missing []string
	for call := range mainCalls {
		if !schemaCalls[call] {
			missing = append(missing, call)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("schema_registration.go is missing Register* calls present in cmd/api/main.go: %v", missing)
	}

	var extra []string
	for call := range schemaCalls {
		if !mainCalls[call] {
			extra = append(extra, call)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("schema_registration.go has Register* calls not present in cmd/api/main.go: %v", extra)
	}
}
