package mongo_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// backendRoot resolves the module root the same way
// composition/importboundary_test.go does — this test lives two directories
// deeper (internal/platform/mongo vs internal/platform/composition), so the
// relative walk differs by one level.
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

// collectionNameConstantPattern matches "identifierContainingCollection =
// \"literal_name\"" — the naming convention every repository in this
// codebase uses for its collection-name constant (e.g. "collectionRFQs =
// \"rfqs\""), confirmed by direct grep across the module tree before this
// test was written.
var collectionNameConstantPattern = regexp.MustCompile(`(?i)collection\w*\s*=\s*"([a-z_]+)"`)

// excludedCollectionNames are literal names the scan would otherwise pick
// up but that are deliberately NOT part of the required schema manifest:
// "_readiness_probe" is a transient probe collection
// (mongo.transactionProbeCollection) that need not durably exist, and
// "demo_seed_manifest" belongs to internal/demoseed — dev-only seeding
// tooling the M8.5C plan explicitly excludes from the tester/production
// deployment ("cmd/demoseed must never run against tester/production").
var excludedCollectionNames = map[string]bool{
	"_readiness_probe":   true,
	"demo_seed_manifest": true,
}

// extractRepositoryCollectionNames walks every non-test .go file under
// internal/ (excluding internal/demoseed, dev-only tooling out of scope
// for the tester/production schema) looking for (a) db.Collection("literal")
// calls and (b) named collection-name constants ("collectionX =
// \"literal\""), returning the deduplicated set of literal collection names
// actually referenced by repository code. This is a source-text scan
// rather than a full AST import-graph walk (composition's drift guards use
// full AST parsing for a narrower, already-established purpose; this
// file's need — extracting string literals scattered across every domain
// module — is well served by the same regexp-based technique this
// codebase's own repository-name constants already follow uniformly).
func extractRepositoryCollectionNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	demoseedDir := filepath.Join(root, "internal", "demoseed")

	internalDir := filepath.Join(root, "internal")
	err := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == demoseedDir {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if len(path) > 8 && path[len(path)-8:] == "_test.go" {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		// (a) db.Collection("literal") call sites.
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Collection" {
				return true
			}
			if len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name := lit.Value[1 : len(lit.Value)-1] // strip surrounding quotes
			if name != "" && !excludedCollectionNames[name] {
				found[name] = true
			}
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", internalDir, err)
	}

	// (b) named collection-name constants (catches indirection like
	// "collection: db.Collection(collectionRFQs)" by extracting the
	// constant's own literal value directly, source-text based since a
	// second AST pass to resolve identifier-to-constant-value across files
	// would be substantially more machinery for the same result).
	err = filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == demoseedDir {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		if len(path) > 8 && path[len(path)-8:] == "_test.go" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range collectionNameConstantPattern.FindAllStringSubmatch(string(content), -1) {
			if !excludedCollectionNames[match[1]] {
				found[match[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", internalDir, err)
	}

	return found
}

// TestCollectionManifestMatchesRepositoryCollectionNames is the drift
// guard the M8.5C plan requires: "The drift test extracts repository
// collection names and compares them with this manifest so future
// repositories cannot be omitted silently."
func TestCollectionManifestMatchesRepositoryCollectionNames(t *testing.T) {
	root := backendRoot(t)
	actual := extractRepositoryCollectionNames(t, root)

	manifestSet := map[string]bool{}
	for _, name := range platformmongo.CollectionManifest {
		manifestSet[name] = true
	}

	var missingFromManifest []string
	for name := range actual {
		if !manifestSet[name] {
			missingFromManifest = append(missingFromManifest, name)
		}
	}
	sort.Strings(missingFromManifest)
	if len(missingFromManifest) > 0 {
		t.Errorf("repository code references collections not in CollectionManifest: %v. "+
			"A new repository's collection must be added to the manifest so a clean "+
			"Atlas bootstrap creates it.", missingFromManifest)
	}

	// schema_migrations is intentionally manifest-only (dbbootstrap creates
	// and writes to it; no repository constructs a MongoXRepository against
	// it), so it is excluded from the reverse direction check.
	var manifestOnly []string
	for _, name := range platformmongo.CollectionManifest {
		if name == "schema_migrations" {
			continue
		}
		if !actual[name] {
			manifestOnly = append(manifestOnly, name)
		}
	}
	sort.Strings(manifestOnly)
	if len(manifestOnly) > 0 {
		t.Errorf("CollectionManifest lists collections no repository actually references: %v. "+
			"Either a repository's collection-name literal changed, or this entry is stale "+
			"and should be removed from the manifest.", manifestOnly)
	}
}

func TestCollectionManifestHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range platformmongo.CollectionManifest {
		if seen[name] {
			t.Errorf("CollectionManifest contains duplicate entry %q", name)
		}
		seen[name] = true
	}
}

func TestMissingCollectionsReturnsOnlyAbsentNames(t *testing.T) {
	existing := []string{"users", "companies"}
	missing := platformmongo.MissingCollections(existing)
	if len(missing) == 0 {
		t.Fatal("expected at least one missing collection")
	}
	for _, name := range missing {
		if name == "users" || name == "companies" {
			t.Fatalf("expected already-existing collection %q to be excluded from missing list", name)
		}
	}
}

func TestUnexpectedCollectionsIgnoresSystemAndReadinessProbe(t *testing.T) {
	existing := append([]string{"system.views", "_readiness_probe", "some_leftover_collection"}, platformmongo.CollectionManifest...)
	unexpected := platformmongo.UnexpectedCollections(existing)
	if len(unexpected) != 1 || unexpected[0] != "some_leftover_collection" {
		t.Fatalf("expected exactly [some_leftover_collection], got %v", unexpected)
	}
}

func TestSortedCollectionManifestIsSorted(t *testing.T) {
	sorted := platformmongo.SortedCollectionManifest()
	if !sort.StringsAreSorted(sorted) {
		t.Fatal("expected SortedCollectionManifest to return a sorted slice")
	}
	if len(sorted) != len(platformmongo.CollectionManifest) {
		t.Fatalf("expected same length, got %d vs %d", len(sorted), len(platformmongo.CollectionManifest))
	}
}
