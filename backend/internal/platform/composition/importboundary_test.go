package composition_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// E1's import-boundary scan: the module boundaries of ADR 0002 and design spec
// §1.1, asserted against the actual import graph.
//
// A comment claiming "rfqs never imports materialrequirements" is worth
// nothing once someone adds the import. These tests fail the moment the graph
// stops matching the design.

const modulePrefix = "github.com/shananth/renovation-platform/backend/"

// backendRoot walks up from this package to the module root, so the scan does
// not depend on where `go test` is invoked from.
func backendRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatalf("expected the module root at %s: %v", dir, err)
	}
	return dir
}

// productionImports returns every non-test import path in the package rooted at
// dir, keyed by the file that imports it.
func productionImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("bad import path in %s: %v", name, imp.Path.Value)
			}
			out[name] = append(out[name], path)
		}
	}
	if len(out) == 0 {
		t.Fatalf("no production files found under %s; the scan would pass vacuously", dir)
	}
	return out
}

// The dependency is ONE-WAY: rfqs -> materialrequirements, through the narrow
// capability interface only. rfqs must import no materialrequirements type at
// all (design spec §1.1, §7.1).
func TestRFQsProductionCodeDoesNotImportMaterialRequirements(t *testing.T) {
	root := backendRoot(t)
	for file, imports := range productionImports(t, filepath.Join(root, "internal", "rfqs")) {
		for _, path := range imports {
			if path == modulePrefix+"internal/materialrequirements" {
				t.Errorf("rfqs/%s imports materialrequirements. rfqs reaches a requirement "+
					"ONLY through MaterialRequirementSource, satisfied via the composition "+
					"adapter (ADR 0002, design spec §1.1)", file)
			}
		}
	}
}

// materialrequirements must never import rfqs: it owns the claim fields, but
// all orchestration is rfqs-owned, and it never inspects an RFQ line.
func TestMaterialRequirementsProductionCodeDoesNotImportRFQs(t *testing.T) {
	root := backendRoot(t)
	for file, imports := range productionImports(t,
		filepath.Join(root, "internal", "materialrequirements")) {
		for _, path := range imports {
			if path == modulePrefix+"internal/rfqs" {
				t.Errorf("materialrequirements/%s imports rfqs. This module never inspects "+
					"an RFQ line and never implements retry_line (design spec §7.1)", file)
			}
		}
	}
}

// costs supplies MaterialCostSource with FIVE PRIMITIVE counters precisely so
// it needs no adapter and no import of materialrequirements. If that ever
// became a named consumer-owned type, costs would have to import the consumer,
// inverting the dependency (design spec §1.2, §1.4.1).
func TestCostsProductionCodeDoesNotImportMaterialRequirements(t *testing.T) {
	root := backendRoot(t)
	for file, imports := range productionImports(t, filepath.Join(root, "internal", "costs")) {
		for _, path := range imports {
			if path == modulePrefix+"internal/materialrequirements" {
				t.Errorf("costs/%s imports materialrequirements. The visitor returns five "+
					"PRIMITIVE counters so costs.Service satisfies the interface directly, "+
					"with no adapter (design spec §1.2)", file)
			}
		}
	}
}

// suppliers is independent of both procurement modules: an indicative price has
// no path into an RFQ (design spec §4.2).
func TestSuppliersProductionCodeImportsNeitherProcurementModule(t *testing.T) {
	root := backendRoot(t)
	for file, imports := range productionImports(t, filepath.Join(root, "internal", "suppliers")) {
		for _, path := range imports {
			if path == modulePrefix+"internal/rfqs" ||
				path == modulePrefix+"internal/materialrequirements" {
				t.Errorf("suppliers/%s imports %s. Neither module may reach the other, which "+
					"is what leaves an indicative price no destination (design spec §4.2)",
					file, path)
			}
		}
	}
}

// compositionRoots construct services and mount routes, so they necessarily
// name every module — that is what a composition root IS. They are not
// domain code and carry no business logic.
//
// The rule being enforced is about DOMAIN code: no module may reach another
// module's types. Listing these by path keeps that rule exact instead of
// silently widening it, and a file appearing here without belonging still
// fails via the count check below.
var compositionRoots = map[string]bool{
	// services.go is the canonical composition root: the ONLY place that
	// constructs the full service graph (repository -> service wiring, in
	// acyclic order). cmd/api/main.go and internal/tenanttest/router.go both
	// call composition.BuildServices rather than wiring the graph
	// themselves (demo data seeding, docs/superpowers/plans/2026-08-18-
	// demo-data-seeding.md, Task 1) — they remain their own composition
	// roots for HTTP-layer concerns (config loading, server lifecycle),
	// which is why they stay listed too, but neither imports every domain
	// module directly anymore except through this file.
	"internal/platform/composition/services.go": true,
	"cmd/api/main.go":               true,
	"internal/tenanttest/router.go": true,
	// httpapp.go is the canonical ROUTE-MOUNTING composition root (M8.5C
	// RP4E2/deployment plan: "Refactor Go router construction once so
	// cmd/api and api/index.go use the same handler") — the same relationship
	// to cmd/api/main.go/api/index.go that services.go has to
	// BuildServices's callers. cmd/api/main.go now calls
	// composition.NewHTTPHandler rather than mounting routes itself, so this
	// file (not main.go) is where every domain module's RegisterHandlers
	// call actually lives.
	"internal/platform/composition/httpapp.go": true,
	// schema_registration.go registers every production handler with nil
	// services purely for OpenAPI schema construction (F0A Checkpoint 6) — a
	// composition root by the same definition as the others: it necessarily
	// names every module because that is what building the full route
	// inventory requires, and it carries no business logic of its own.
	"internal/platform/composition/schema_registration.go": true,
}

// demoseedPackagePrefix is the ONE whole-package exemption from the
// one-domain-module-per-file rule, alongside the individually-listed files
// in compositionRoots and each test's own hardcoded adapter-file `want`
// list. Every other adapter (m7adapters.go, m8adapters.go, etc.) stays
// named explicitly per-file — deliberately, so the allowlist documents
// exactly which files exist rather than exempting a whole directory. This
// package is the one exception: docs/superpowers/plans/2026-08-18-demo-
// data-seeding.md's internal/demoseed is a SECOND, development-only
// composition/orchestration boundary, distinct from
// internal/platform/composition's production one. It drives realistic
// end-to-end demo scenarios across ~20 domain modules (Client -> Project ->
// Work -> Material -> Requirement -> RFQ -> Award, etc.) — a cross-module
// ORCHESTRATION story, not a domain-to-domain COUPLING, which is the
// distinction this rule actually protects against (a domain module reaching
// into another domain module's internals). demoseed owns no domain
// invariants, writes no foreign collection directly, and duplicates no
// domain business logic; it only calls exported service methods through
// interfaces it defines itself. Its file count grows with every scenario
// task in that plan, so exempting it per-file (like the other adapters)
// would silently narrow the exemption by omission with every new file
// instead of by a deliberate decision. See
// TestProductionCodeNeverImportsDemoseed for the compensating boundary that
// keeps this exemption from leaking the other direction.
const demoseedPackagePrefix = "internal/demoseed/"

// isApprovedCrossDomainAdapter reports whether slash (a "/"-separated path
// relative to the backend module root) is exempt from the
// one-domain-module-per-file rule — either because it is an individually
// listed composition-root file (compositionRoots) or falls under
// internal/demoseed's whole-package exemption above. Both
// TestOnlyApprovedCompositionAdaptersImportBothProcurementModules and
// TestOnlyCompositionAdaptersImportMultipleDomainModules call this so the
// two tests cannot silently drift apart on what counts as approved. Neither
// test's own hardcoded per-file `want` list (the other named adapters) is
// affected — those stay exactly as precise as before.
func isApprovedCrossDomainAdapter(slash string) bool {
	return compositionRoots[slash] || strings.HasPrefix(slash, demoseedPackagePrefix)
}

// Only approved cross-domain adapters may import both procurement modules —
// originally just the composition adapter (hence this test's former name,
// TestCompositionIsTheOnlyPlaceImportingBothProcurementModules), now also
// internal/demoseed (see isApprovedCrossDomainAdapter's doc comment). This
// is the strongest statement of the boundary: it scans the entire tree
// rather than the packages the design happens to mention.
func TestOnlyApprovedCompositionAdaptersImportBothProcurementModules(t *testing.T) {
	root := backendRoot(t)
	const (
		mrPath   = modulePrefix + "internal/materialrequirements"
		rfqsPath = modulePrefix + "internal/rfqs"
	)

	fset := token.NewFileSet()
	var offenders, roots []string
	scanned := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendored and hidden trees.
			base := filepath.Base(path)
			if base == "vendor" || (strings.HasPrefix(base, ".") && base != "." && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		scanned++

		importsMR, importsRFQs := false, false
		for _, imp := range file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			switch p {
			case mrPath:
				importsMR = true
			case rfqsPath:
				importsRFQs = true
			}
		}
		if importsMR && importsRFQs {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			slash := filepath.ToSlash(rel)
			if isApprovedCrossDomainAdapter(slash) {
				roots = append(roots, slash)
				return nil
			}
			offenders = append(offenders, slash)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("no production files were scanned; the guard would pass vacuously")
	}

	sort.Strings(offenders)
	// m7adapters.go is the one named, hardcoded exception here (it is not a
	// compositionRoots entry, and predates internal/demoseed's whole-package
	// exemption) — kept exactly as precise as the original version of this
	// test, which asserted this same file as the sole accepted offender.
	want := []string{"internal/platform/composition/m7adapters.go"}

	if len(offenders) != len(want) || (len(offenders) == 1 && offenders[0] != want[0]) {
		t.Errorf("files importing BOTH procurement modules = %v, want exactly %v.\n"+
			"Only an approved cross-domain adapter (the composition adapter, or "+
			"internal/demoseed) is permitted to import two domain modules at once "+
			"(ADR 0002, design spec §1.4.1)", offenders, want)
	}
	t.Logf("scanned %d production files; both-module importers: %v; approved adapters (internal/demoseed etc.): %v", scanned, offenders, roots)
}

// TestProductionCodeNeverImportsDemoseed is the compensating boundary for
// internal/demoseed's whole-package cross-domain-import exemption above:
// granting demoseed broad visibility into every domain module's types must
// not let that visibility leak the other direction. The intended dependency
// direction is cmd/demoseed -> internal/demoseed -> domain services, and
// NEVER domain package -> internal/demoseed or cmd/api -> internal/demoseed.
// internal/demoseed is a development-only tool (environment-guarded, see
// demoseed.CheckEnvironmentGuard) and must never become a production
// application dependency.
func TestProductionCodeNeverImportsDemoseed(t *testing.T) {
	root := backendRoot(t)
	const demoseedImportPath = modulePrefix + "internal/demoseed"

	fset := token.NewFileSet()
	var offenders []string
	scanned := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || (strings.HasPrefix(base, ".") && base != "." && path != root) {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		slash := filepath.ToSlash(rel)
		// internal/demoseed's own files, and cmd/demoseed (its one
		// legitimate entrypoint), are exempt from this check by definition.
		if strings.HasPrefix(slash, demoseedPackagePrefix) || strings.HasPrefix(slash, "cmd/demoseed/") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		scanned++

		for _, imp := range file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if p == demoseedImportPath {
				offenders = append(offenders, slash)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 {
		t.Fatal("no production files were scanned; the guard would pass vacuously")
	}

	sort.Strings(offenders)
	if len(offenders) != 0 {
		t.Errorf("files outside internal/demoseed and cmd/demoseed import internal/demoseed = %v, want none.\n"+
			"internal/demoseed is a development-only tool; it may depend on domain "+
			"services, but nothing outside it (and its own cmd/demoseed entrypoint) "+
			"may depend on it", offenders)
	}
	t.Logf("scanned %d production files outside internal/demoseed and cmd/demoseed; importers of demoseed: %v", scanned, offenders)
}

// The stale M0 scaffolding must be gone. Its comment claimed a
// single-`procurement`-package design that the three-module correction
// superseded, and claimed ownership of RFQ Invitations and Supplier Offers,
// which are M8's (design spec §1.1).
func TestStaleProcurementPackageIsDeleted(t *testing.T) {
	root := backendRoot(t)
	path := filepath.Join(root, "internal", "procurement")

	if _, err := os.Stat(path); err == nil {
		t.Errorf("internal/procurement still exists. It is stale M0 scaffolding: its "+
			"comment describes a single-package design superseded by the three-module "+
			"correction, and claims ownership of RFQ Invitations and Supplier Offers, "+
			"which are M8's (%s)", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", path, err)
	}
}

// The three M7 modules must exist and carry package documentation reflecting
// the three-module design.
func TestThreeModulePackageDocumentationExists(t *testing.T) {
	root := backendRoot(t)

	// Each module's doc must state the boundary rule a future reader would
	// otherwise have to rediscover from the spec.
	cases := []struct {
		pkg     string
		mustSay []string
	}{
		{"materialrequirements", []string{
			"rfqs -> materialrequirements", // the one-way dependency
			"never imports rfqs",           // the rule this module must keep
		}},
		{"rfqs", []string{
			"rfqs -> materialrequirements",
			"M8",
		}},
		{"suppliers", []string{
			"never fetches", // the §4.4 no-network guarantee
			"M8",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.pkg, func(t *testing.T) {
			dir := filepath.Join(root, "internal", tc.pkg)
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("module %s is missing: %v", tc.pkg, err)
			}

			doc := packageDoc(t, dir)
			if doc == "" {
				t.Fatalf("%s has no package documentation", tc.pkg)
			}
			for _, phrase := range tc.mustSay {
				if !strings.Contains(doc, phrase) {
					t.Errorf("%s package doc does not mention %q", tc.pkg, phrase)
				}
			}
		})
	}
}

// packageDoc returns the package comment from whichever production file in dir
// carries one.
func packageDoc(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		if file.Doc != nil && file.Doc.Text() != "" {
			return file.Doc.Text()
		}
	}
	return ""
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
