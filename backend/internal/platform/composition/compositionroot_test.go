package composition_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// E2's guard: cmd/api/main.go and tenanttest.BuildRouter must wire the M7
// modules IDENTICALLY.
//
// tenanttest exists so integration tests exercise the real composition. That
// only holds if it stays in sync: a route registered in main.go but not in
// tenanttest would ship completely untested by the tenant-isolation suite, and
// the drift would be silent.
//
// Since demo data seeding's Task 1 (docs/superpowers/plans/2026-08-18-demo-
// data-seeding.md), the SERVICE-CONSTRUCTION half of this guarantee no
// longer needs comparing two independent inline wirings: cmd/api/main.go and
// internal/tenanttest/router.go both delegate to the single canonical
// composition.BuildServices (internal/platform/composition/services.go), so
// they cannot drift apart on construction by definition. What this file's
// tests verify instead: (1) services.go itself makes every required M7
// construction call, and (2) both entrypoints actually call BuildServices
// rather than reintroducing their own inline wiring. Route MOUNTING remains
// a genuine per-entrypoint concern (BuildServices does no HTTP wiring), so
// TestMilestone7HandlersAreRegisteredOnTheAuthenticatedGroup below still
// compares the two files directly.
//
// Asserted against the ASTs of the relevant files rather than by reading
// them.

// m7Constructors are the calls both roots must make. Each is
// "package.Function" as written in source.
var m7Constructors = []string{
	// Repositories.
	"materialrequirements.NewMongoMaterialRequirementRepository",
	"rfqs.NewMongoRFQRepository",
	"rfqs.NewMongoRFQCounterRepository",
	"suppliers.NewMongoSupplierRepository",
	"suppliers.NewMongoSupplierOfferingRepository",
	"suppliers.NewMongoPreferenceRepository",

	// Services.
	"materialrequirements.NewService",
	"rfqs.NewService",
	"suppliers.NewService",

	// The M8 seam. M7 wires the no-op so reopen always succeeds; M8 swaps in
	// the real adapter with no change to service logic (design spec §6.4).
	"rfqs.NoExternalIssuanceSource",

	// The one adapter permitted to import both procurement modules.
	"composition.NewMaterialRequirementSourceAdapter",

	// Handlers.
	"materialrequirements.RegisterHandlers",
	"rfqs.RegisterHandlers",
	"suppliers.RegisterHandlers",
}

// selectorCalls returns every "pkg.Ident" selector expression in the file,
// deduplicated. Composite literals (rfqs.NoExternalIssuanceSource{}) and plain
// calls both surface as selectors, so one walk covers both.
func selectorCalls(t *testing.T, path string) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		found[pkg.Name+"."+sel.Sel.Name] = true
		return true
	})
	return found
}

// bareCallNames returns every bare (unqualified) function-call identifier in
// the file — e.g. "NewMaterialRequirementSourceAdapter(x)" inside
// services.go itself, which is package composition and so never qualifies
// its own package's adapter constructors with a "composition." prefix the
// way an external caller (main.go, router.go) must.
func bareCallNames(t *testing.T, path string) map[string]bool {
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
		if ident, ok := call.Fun.(*ast.Ident); ok {
			found[ident.Name] = true
		}
		return true
	})
	return found
}

// wantedInServicesGo reports whether services.go itself makes the call
// named want. want is written in "pkg.Function" form as an external caller
// (main.go, router.go) would need to write it. Since services.go lives
// INSIDE package composition, a "composition.X" entry is satisfied by
// either the qualified form (defensive — same-package code never actually
// writes this) or the bare "X" form services.go's own calls use.
func wantedInServicesGo(qualified, bare map[string]bool, want string) bool {
	if qualified[want] {
		return true
	}
	if pkg, fn, ok := strings.Cut(want, "."); ok && pkg == "composition" {
		return bare[fn]
	}
	return false
}

// composition.BuildServices is the SOLE place M7 construction calls need to
// appear now. Every entrypoint that needs the service graph must delegate
// to it rather than reintroducing its own inline wiring.
func servicesGoPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(backendRoot(t), "internal", "platform", "composition", "services.go")
}

// registerHandlersCalls are the subset of a constructor list that mount HTTP
// routes rather than construct services. composition.BuildServices does no
// HTTP wiring at all (Task 1's design), so these remain a genuine
// per-entrypoint concern checked against main.go/router.go directly, never
// against services.go.
func isRegisterHandlersCall(want string) bool {
	_, fn, ok := strings.Cut(want, ".")
	return ok && strings.HasPrefix(fn, "Register")
}

// composition.BuildServices must make every M7 SERVICE-CONSTRUCTION call —
// it is the one canonical composition root now. Route-mounting calls
// (RegisterHandlers) are checked separately against the two HTTP
// entrypoints, since BuildServices does no HTTP wiring.
func TestBothCompositionRootsWireMilestone7Identically(t *testing.T) {
	path := servicesGoPath(t)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("composition root services.go is missing: %v", err)
	}
	qualified := selectorCalls(t, path)
	bare := bareCallNames(t, path)

	var routeMountingCalls []string
	for _, want := range m7Constructors {
		if isRegisterHandlersCall(want) {
			routeMountingCalls = append(routeMountingCalls, want)
			continue
		}
		if !wantedInServicesGo(qualified, bare, want) {
			t.Errorf("composition/services.go does not call %s. The canonical "+
				"composition root must wire every M7 construction call, or an "+
				"entrypoint delegating to it would silently lose functionality", want)
		}
	}

	assertBothEntrypointsDelegateToBuildServices(t)
	assertMainDelegatesToNewHTTPHandler(t)
	assertBothEntrypointsMakeEveryCall(t, routeMountingCalls)
}

// assertBothEntrypointsMakeEveryCall verifies every call in wantCalls is
// made somewhere in each HTTP entrypoint's route-mounting code — cmd/api's
// route mounting now lives in composition/httpapp.go (NewHTTPHandler),
// which cmd/api/main.go delegates to (see
// assertMainDelegatesToNewHTTPHandler), while internal/tenanttest/router.go
// still mounts its own routes directly (it needs test-only cookie/logger/
// mailer overrides NewHTTPHandler does not take, so it is not a drop-in
// caller of the same function) — both are still checked against the SAME
// wantCalls set, so route-mounting drift between what ships and what
// tenanttest exercises remains caught.
func assertBothEntrypointsMakeEveryCall(t *testing.T, wantCalls []string) {
	t.Helper()
	if len(wantCalls) == 0 {
		return
	}
	root := backendRoot(t)
	for name, path := range map[string]string{
		"internal/platform/composition/httpapp.go": filepath.Join(root, "internal", "platform", "composition", "httpapp.go"),
		"internal/tenanttest/router.go":            filepath.Join(root, "internal", "tenanttest", "router.go"),
	} {
		calls := selectorCalls(t, path)
		for _, want := range wantCalls {
			if !calls[want] {
				t.Errorf("%s does not call %s. Both HTTP entrypoints must mount the "+
					"same routes, or tenanttest exercises a different application than "+
					"the one that ships", name, want)
			}
		}
	}
}

// assertMainDelegatesToNewHTTPHandler verifies cmd/api/main.go calls
// composition.NewHTTPHandler rather than mounting routes itself — the
// route-mounting counterpart to assertBothEntrypointsDelegateToBuildServices
// below, so route-mounting logic has exactly one canonical implementation
// (composition/httpapp.go) rather than main.go silently reintroducing its
// own inline wiring that could drift from what assertBothEntrypointsMakeEveryCall
// actually checks.
func assertMainDelegatesToNewHTTPHandler(t *testing.T) {
	t.Helper()
	root := backendRoot(t)
	path := filepath.Join(root, "cmd", "api", "main.go")
	calls := selectorCalls(t, path)
	if !calls["composition.NewHTTPHandler"] {
		t.Errorf("cmd/api/main.go does not call composition.NewHTTPHandler — it must " +
			"delegate route mounting to the canonical composition root rather than " +
			"wiring routes itself, or it can silently drift from what actually ships")
	}
}

// assertBothEntrypointsDelegateToBuildServices is the replacement for
// comparing two independent inline wirings: cmd/api/main.go and
// internal/tenanttest/router.go must both call composition.BuildServices,
// which is what makes their construction identical BY CONSTRUCTION rather
// than by a comparison test that a future edit could silently stop
// satisfying (demo data seeding plan, Task 1).
func assertBothEntrypointsDelegateToBuildServices(t *testing.T) {
	t.Helper()
	root := backendRoot(t)
	for name, path := range map[string]string{
		"cmd/api/main.go":               filepath.Join(root, "cmd", "api", "main.go"),
		"internal/tenanttest/router.go": filepath.Join(root, "internal", "tenanttest", "router.go"),
	} {
		calls := selectorCalls(t, path)
		if !calls["composition.BuildServices"] {
			t.Errorf("%s does not call composition.BuildServices — it must delegate "+
				"the full service graph to the canonical composition root rather than "+
				"wiring services itself, or it can silently drift from what actually "+
				"ships", name)
		}
	}
}

// Every M7 repository must have EnsureIndexes called in the canonical
// composition root.
//
// A missing call is invisible until a unique index silently fails to protect an
// invariant — the supplier name uniqueness and the RFQ number uniqueness both
// depend on this.
func TestBothCompositionRootsEnsureMilestone7Indexes(t *testing.T) {
	repoVars := []string{
		"materialRequirementRepo",
		"rfqRepo",
		"rfqCounterRepo",
		"supplierRepo",
		"supplierOfferingRepo",
		"preferenceRepo",
	}

	for name, path := range map[string]string{
		"internal/platform/composition/services.go": servicesGoPath(t),
	} {
		ensured := ensureIndexReceivers(t, path)
		for _, repo := range repoVars {
			if !ensured[repo] {
				t.Errorf("%s never calls %s.EnsureIndexes. Without it a unique index may "+
					"not exist, so an invariant the design relies on is unprotected",
					name, repo)
			}
		}
	}
}

// ensureIndexReceivers returns the receiver identifier of every
// `x.EnsureIndexes` selector in the file, whether called or passed as a value.
func ensureIndexReceivers(t *testing.T, path string) map[string]bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	out := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "EnsureIndexes" {
			return true
		}
		if recv, ok := sel.X.(*ast.Ident); ok {
			out[recv.Name] = true
		}
		return true
	})
	return out
}

// The guard must be able to fail: a constructor absent from a root must be
// detected. Without this, a bug in the AST walk would let the real assertion
// pass silently.
func TestCompositionRootGuardDetectsAMissingCall(t *testing.T) {
	const sample = `package main

func main() {
	_ = rfqs.NewMongoRFQRepository(nil)
	// suppliers.NewService is deliberately absent.
}
`
	path := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := selectorCalls(t, path)
	if !calls["rfqs.NewMongoRFQRepository"] {
		t.Fatal("the walk failed to see a call that is plainly present; the real guard " +
			"cannot be trusted")
	}
	if calls["suppliers.NewService"] {
		t.Fatal("the walk reported a call that is absent")
	}
}

// The EnsureIndexes walk must see both call shapes: invoked, and passed as a
// func value. tenanttest uses the latter, so a walk that only recognised calls
// would report a false failure and invite someone to weaken the guard.
func TestEnsureIndexWalkSeesBothCallShapes(t *testing.T) {
	const sample = `package main

func main() {
	_ = invokedRepo.EnsureIndexes(nil)
	_ = []any{passedRepo.EnsureIndexes}
}
`
	path := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(path, []byte(sample), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ensureIndexReceivers(t, path)
	for _, want := range []string{"invokedRepo", "passedRepo"} {
		if !got[want] {
			t.Errorf("the walk missed %s.EnsureIndexes; both roots express this "+
				"differently, so both shapes must be recognised", want)
		}
	}
}

// The M7 handler set must be registered on the AUTHENTICATED group in both
// roots. Registering on the base api would expose contractor procurement data
// without a bearer token.
func TestMilestone7HandlersAreRegisteredOnTheAuthenticatedGroup(t *testing.T) {
	root := backendRoot(t)

	for name, path := range map[string]string{
		"cmd/api/main.go":               filepath.Join(root, "cmd", "api", "main.go"),
		"internal/tenanttest/router.go": filepath.Join(root, "internal", "tenanttest", "router.go"),
	} {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		var wrong []string
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "RegisterHandlers" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch pkg.Name {
			case "materialrequirements", "rfqs", "suppliers":
			default:
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			arg, ok := call.Args[0].(*ast.Ident)
			if !ok || arg.Name != "authedAPI" {
				wrong = append(wrong, pkg.Name)
			}
			return true
		})

		sort.Strings(wrong)
		if len(wrong) != 0 {
			t.Errorf("%s registers %v on something other than authedAPI. Every M7 route is "+
				"contractor-only and must require a bearer token", name, wrong)
		}
	}
}

// tenanttest must never be imported by production code: it wires fixed test
// secrets and a discard logger.
func TestTenanttestIsNeverImportedByProductionCode(t *testing.T) {
	root := backendRoot(t)
	const tenanttestPath = modulePrefix + "internal/tenanttest"

	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || (strings.HasPrefix(base, ".") && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		// tenanttest's own files legitimately live in that package.
		if strings.Contains(filepath.ToSlash(path), "/internal/tenanttest/") {
			return nil
		}

		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == tenanttestPath {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) != 0 {
		t.Errorf("production files import tenanttest: %v. It wires fixed test secrets and "+
			"must never reach a running server", offenders)
	}
}
