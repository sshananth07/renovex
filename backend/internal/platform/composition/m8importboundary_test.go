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

// M8's import-boundary scan (design spec §2, ADR 0002).
//
// M8 owns four modules whose conceptual flow is strictly one-way:
//
//	rfqissuance -> supplieraccess -> supplieroffers -> awards
//
// That flow does NOT permit repository access across modules, and it does not
// permit a downstream module's types to travel upstream. Every cross-module
// dependency is a consumer-owned capability interface; composition adapters
// exist only where Go's exact-return-type rule forces a conversion.
//
// These tests assert the graph rather than trusting the comments describing it.

// m8Modules are the four packages M8 owns.
var m8Modules = []string{
	"rfqissuance",
	"supplieraccess",
	"supplieroffers",
	"awards",
}

// Every M8 module must exist with package documentation stating the boundary
// rule a future reader would otherwise have to rediscover from the spec.
func TestM8ModulesExistWithPackageDocumentation(t *testing.T) {
	root := backendRoot(t)

	cases := []struct {
		pkg     string
		mustSay []string
	}{
		{"rfqissuance", []string{
			"immutable",  // issued versions are never mutated
			"M7",         // it consumes the M7 ready snapshot
			"Invitation", // it owns invitations
		}},
		{"supplieraccess", []string{
			"never imports rfqissuance", // the rule this module must keep
			"session",
		}},
		{"supplieroffers", []string{
			"immutable",
			"never imports", // it reaches upstream only through capabilities
		}},
		{"awards", []string{
			"immutable",
			"never imports", // it reaches upstream only through capabilities
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

// moduleImports returns every non-test import in the package rooted at dir.
//
// Unlike productionImports it tolerates a package with no imports at all: a
// module whose only production file is doc.go legitimately imports nothing, and
// treating that as a vacuous scan would fail the boundary tests for a package
// that trivially satisfies them. The anti-vacuous guard is kept, but applied to
// the presence of production FILES rather than of imports.
func moduleImports(t *testing.T, dir string) map[string][]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	out := map[string][]string{}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
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
	if files == 0 {
		t.Fatalf("no production files found under %s; the scan would pass vacuously", dir)
	}
	return out
}

// No M8 module may import another M8 module. The flow between them is
// capability interfaces only, wired in the composition root.
func TestM8ModulesDoNotImportEachOther(t *testing.T) {
	root := backendRoot(t)

	for _, pkg := range m8Modules {
		for _, other := range m8Modules {
			if pkg == other {
				continue
			}
			t.Run(pkg+"/"+other, func(t *testing.T) {
				dir := filepath.Join(root, "internal", pkg)
				if _, err := os.Stat(dir); err != nil {
					t.Fatalf("module %s is missing: %v", pkg, err)
				}
				for file, imports := range moduleImports(t, dir) {
					for _, path := range imports {
						if path == modulePrefix+"internal/"+other {
							t.Errorf("%s/%s imports %s. Cross-module dependencies are "+
								"consumer-owned capability interfaces satisfied in the "+
								"composition root, never direct imports (design spec §2)",
								pkg, file, other)
						}
					}
				}
			})
		}
	}
}

// M8 must never write to M7's collections, which starts with never importing
// M7's modules. rfqissuance consumes the ready RFQ snapshot through an
// M8-owned capability satisfied by a composition adapter (design spec §2.1).
func TestM8ModulesDoNotImportM7Modules(t *testing.T) {
	root := backendRoot(t)

	m7 := []string{"rfqs", "materialrequirements", "suppliers"}

	for _, pkg := range m8Modules {
		dir := filepath.Join(root, "internal", pkg)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("module %s is missing: %v", pkg, err)
		}
		for _, m7pkg := range m7 {
			t.Run(pkg+"/"+m7pkg, func(t *testing.T) {
				for file, imports := range moduleImports(t, dir) {
					for _, path := range imports {
						if path == modulePrefix+"internal/"+m7pkg {
							t.Errorf("%s/%s imports %s. M8 never writes to an M7 collection "+
								"and reaches M7 only through an M8-owned capability satisfied "+
								"by a composition adapter (design spec §2.1)", pkg, file, m7pkg)
						}
					}
				}
			})
		}
	}
}

// The M7 modules must not learn about M8. rfqs consumes M8's issuance status
// through its OWN IssuanceStatusSource interface, wired via a setter in the
// composition root (design spec §1A.3) — never by importing rfqissuance.
func TestM7ModulesDoNotImportM8Modules(t *testing.T) {
	root := backendRoot(t)

	for _, m7pkg := range []string{"rfqs", "materialrequirements", "suppliers"} {
		for _, m8pkg := range m8Modules {
			t.Run(m7pkg+"/"+m8pkg, func(t *testing.T) {
				for file, imports := range productionImports(t,
					filepath.Join(root, "internal", m7pkg)) {
					for _, path := range imports {
						if path == modulePrefix+"internal/"+m8pkg {
							t.Errorf("%s/%s imports %s. rfqs reaches M8 only through its own "+
								"IssuanceStatusSource interface, wired by setter in the "+
								"composition root (design spec §1A.3)", m7pkg, file, m8pkg)
						}
					}
				}
			})
		}
	}
}

// Only the composition adapters — and internal/demoseed, a second
// development-only orchestration boundary (see isApprovedCrossDomainAdapter
// in importboundary_test.go) — may import two domain modules at once. This
// scans the whole tree rather than the packages the design happens to name,
// so a new offender anywhere fails.
//
// This supersedes the M7-only assertion by covering M7 and M8 together.
func TestOnlyCompositionAdaptersImportMultipleDomainModules(t *testing.T) {
	root := backendRoot(t)

	domainModules := map[string]bool{}
	for _, pkg := range append([]string{"rfqs", "materialrequirements", "suppliers"}, m8Modules...) {
		domainModules[modulePrefix+"internal/"+pkg] = true
	}

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

		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}
		scanned++

		count := 0
		for _, imp := range file.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if domainModules[p] {
				count++
			}
		}
		if count > 1 {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			slash := filepath.ToSlash(rel)
			// A composition root necessarily names every module it wires, and
			// internal/demoseed necessarily orchestrates many of them (see
			// isApprovedCrossDomainAdapter's doc comment in
			// importboundary_test.go).
			if isApprovedCrossDomainAdapter(slash) {
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
	want := []string{
		"internal/platform/composition/m7adapters.go",
		"internal/platform/composition/m8adapters.go",
		// Phase F adapters span rfqissuance and supplieroffers (producers) and
		// awards (consumer). Being adapters is what makes that permitted:
		// neither producer knows awards exists (§8A.1).
		"internal/platform/composition/m8awardadapters.go",
		// Phase E adapters span supplieraccess/rfqissuance (producers) and
		// supplieroffers (consumer). Being adapters is what makes that
		// permitted: neither producer knows supplieroffers exists.
		"internal/platform/composition/m8offeradapters.go",
		// The recipient-replacement coordinator spans rfqissuance (the
		// authoritative invitation) and supplieroffers (the draft workspace).
		// Being an adapter is exactly what makes that permitted: neither module
		// imports the other, and only primitives cross (§5.3A).
		"internal/platform/composition/m8offerworkspaceadapter.go",
		// The Supplier outcome capability spans supplieraccess (which owns the
		// Phase D session, binding and CSRF) and awards (which owns outcome and
		// acknowledgement state). Neither imports the other (D3, §8A.1).
		"internal/platform/composition/m8supplieroutcomeadapter.go",
	}

	if strings.Join(offenders, ",") != strings.Join(want, ",") {
		t.Errorf("files importing MORE THAN ONE domain module = %v, want exactly %v.\n"+
			"Composition adapters are the only place permitted to import two domain "+
			"modules at once (ADR 0002, design spec §2)", offenders, want)
	}
	t.Logf("scanned %d production files; multi-module importers: %v", scanned, offenders)
}

// The invitation-secret keyring is platform infrastructure with zero domain
// knowledge, which is what lets rfqissuance (derive, rotate) and supplieraccess
// (verify) both depend on it without depending on each other (§6.1A).
func TestSecretsPackageHasNoDomainImports(t *testing.T) {
	root := backendRoot(t)
	dir := filepath.Join(root, "internal", "platform", "secrets")

	for file, imports := range productionImports(t, dir) {
		for _, path := range imports {
			if strings.HasPrefix(path, modulePrefix) {
				t.Errorf("secrets/%s imports %s. The keyring must stay free of domain "+
					"knowledge so both rfqissuance and supplieraccess may depend on it "+
					"(design spec §6.1A, ADR 0002)", file, path)
			}
		}
	}
}
