package suppliers_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Acceptance test 92: no HTTP request or DNS lookup is ever issued for
// ImageURL/ProductURL (design spec §4.4).
//
// This is asserted STRUCTURALLY, against the import graph and the AST, rather
// than with a network-refusing test double. A double can only observe the calls
// a test happens to exercise; an import that does not exist cannot make a
// request from any code path, including one a future edit adds. That is what
// makes "no web scraping" structural rather than aspirational, and it is why
// M7 has no SSRF surface and does not reject private or loopback hosts.
//
// The guard is PACKAGE-WIDE and names no file. An earlier version excluded
// "handler.go" by name, which a future handler_extra.go would have bypassed
// entirely — the exclusion, not the rule, was the weak point.
//
// Two rules, applied to every non-test file:
//
//  1. no network-CLIENT import at all (net, net/rpc, net/smtp, crypto/tls, …)
//  2. net/http may be imported only to SERVE; every client-side selector on it
//     is rejected
//
// net/url and net/mail are permitted everywhere: both parse, neither dials.

// networkClientImports can open a connection, resolve a name, or drive
// something that can. net/http is handled separately by rule 2, because
// serving is legitimate.
var networkClientImports = map[string]string{
	"net":              "can dial and resolve",
	"net/rpc":          "can dial",
	"net/smtp":         "can dial",
	"os/exec":          "can shell out to curl or wget",
	"database/sql":     "can dial",
	"crypto/tls":       "can dial",
	"golang.org/x/net": "can dial and resolve",
}

// parseOnlyImports live under net/ but only parse strings.
//
//	net/url  — what ValidateURL is built on. A blanket "net/..." prefix rule
//	           would forbid the very thing §4.4 requires.
//	net/mail — mail.ParseAddress validates an address. smtp is what sends, and
//	           that stays forbidden. Same convention as access/external.go.
var parseOnlyImports = map[string]bool{
	"net/url":  true,
	"net/mail": true,
}

// rule1Exempt are import paths rule 1 must not judge, because another rule
// governs them precisely.
//
// net/http is exempt here ONLY because rule 2 inspects every file that imports
// it and rejects the entire client half. Removing it from this map without
// keeping rule 2 would not tighten anything — it would make the handler
// unbuildable and invite someone to weaken the guard instead.
var rule1Exempt = map[string]bool{
	"net/http": true,
}

// httpClientSelectors are the net/http identifiers that can reach out, or that
// exist to. Server-side types and the Method/Status constants are absent by
// design — those are what a handler legitimately needs.
var httpClientSelectors = map[string]string{
	"Get":                   "issues a request",
	"Post":                  "issues a request",
	"PostForm":              "issues a request",
	"Head":                  "issues a request",
	"Do":                    "issues a request",
	"NewRequest":            "builds a request to send",
	"NewRequestWithContext": "builds a request to send",
	"Client":                "an http client exists to make requests",
	"DefaultClient":         "an http client exists to make requests",
	"Transport":             "carries requests",
	"DefaultTransport":      "carries requests",
	"RoundTripper":          "carries requests",
	"RoundTrip":             "carries requests",
	"ReadResponse":          "reads a response to a request we would have made",
	"ProxyFromEnvironment":  "configures outbound proxying",
	"ProxyURL":              "configures outbound proxying",
}

// productionFiles lists every non-test .go file in the package. It never names
// a file, so a new one is covered the moment it is added.
func productionFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// httpImportAlias reports the local name net/http is bound to in file, honouring
// an explicit alias. Without this, `import fetcher "net/http"` followed by
// fetcher.Get would slip past a check that only looks for the identifier
// "http".
//
// It returns "", false when the file does not import net/http, and "", false
// for a blank import (`_ "net/http"`), which binds no usable identifier.
func httpImportAlias(file *ast.File) (string, bool) {
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "net/http" {
			continue
		}
		if imp.Name == nil {
			return "http", true // default: the package's own name
		}
		switch imp.Name.Name {
		case "_":
			return "", false // imported for side effects; nothing is callable
		case ".":
			// A dot import puts Get, Client and friends in file scope
			// unqualified. Nothing here needs it, and it would defeat
			// selector-based detection, so it is refused outright.
			return ".", true
		default:
			return imp.Name.Name, true
		}
	}
	return "", false
}

// Rule 1, package-wide: no production file may import a network-client package.
func TestNoProductionFileImportsANetworkClientPackage(t *testing.T) {
	files := productionFiles(t)
	if len(files) == 0 {
		t.Fatal("no production files were inspected; the guard would pass vacuously")
	}

	fset := token.NewFileSet()
	for _, name := range files {
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("bad import path in %s: %v", name, imp.Path.Value)
			}
			if parseOnlyImports[path] || rule1Exempt[path] {
				continue
			}
			for bad, why := range networkClientImports {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %q (%s). M7 never fetches, resolves, proxies or "+
						"inspects a contractor-supplied URL — see design spec §4.4",
						name, path, why)
				}
			}
		}
	}
	t.Logf("checked %d production files: %v", len(files), files)
}

// Rule 2, package-wide: a file may import net/http to SERVE, but no file may
// use its client half.
//
// This is what lets the domain, service and repository files be held to "no
// net/http at all" while the handler still compiles — without naming either.
func TestNoProductionFileUsesTheHTTPClient(t *testing.T) {
	files := productionFiles(t)
	if len(files) == 0 {
		t.Fatal("no production files were inspected; the guard would pass vacuously")
	}

	fset := token.NewFileSet()
	inspected := 0
	for _, name := range files {
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		alias, imported := httpImportAlias(file)
		if !imported {
			continue
		}
		inspected++

		if alias == "." {
			t.Errorf("%s dot-imports net/http, which puts Get, Client and Transport in file "+
				"scope unqualified and defeats client-use detection", name)
			continue
		}

		for _, used := range clientSelectorsUsed(file, alias) {
			t.Errorf("%s uses %s.%s (%s). net/http may be imported only to SERVE "+
				"requests — M7 never fetches a contractor-supplied URL (§4.4)",
				name, alias, used, httpClientSelectors[used])
		}
	}
	t.Logf("inspected %d production file(s) importing net/http", inspected)
}

// net/url IS imported, and that is correct: parsing a URL string is not
// dereferencing it. Asserted so a future reader does not "tidy" it into the
// forbidden list.
func TestSuppliersMayStillParseURLs(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", "supplier.go"), nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "net/url" {
			return
		}
	}
	t.Error("supplier.go no longer imports net/url; ValidateURL must still PARSE urls — " +
		"it simply must never dereference them")
}

// --- self-verification ---
//
// Each guard above must be able to FAIL. Without these, a bug in the walk
// (wrong directory, wrong suffix filter, an alias it cannot resolve) would let
// the real assertion pass silently — the vacuous-pass trap that let three
// mutations survive earlier in this milestone.

// probeFile writes src to a temp dir and parses it.
func probeFile(t *testing.T, src string, mode parser.Mode) *ast.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, mode)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// The import walk must see a forbidden import.
func TestImportGuardDetectsAForbiddenImport(t *testing.T) {
	file := probeFile(t, `package suppliers

import (
	"crypto/tls"
	"strings"
)

var _ = tls.Dial
var _ = strings.TrimSpace
`, parser.ImportsOnly)

	found := false
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if _, bad := networkClientImports[p]; bad {
			found = true
		}
	}
	if !found {
		t.Fatal("the import walk failed to see crypto/tls in a file that plainly imports it; " +
			"the real guard cannot be trusted")
	}
}

// The client-use walk must see a client call under the DEFAULT name.
func TestClientUseGuardDetectsAPlainClientCall(t *testing.T) {
	file := probeFile(t, `package suppliers

import "net/http"

func fetch() { _, _ = http.Get("https://example.com") }
`, 0)

	alias, imported := httpImportAlias(file)
	if !imported || alias != "http" {
		t.Fatalf("alias resolution = %q/%v, want http/true", alias, imported)
	}
	if !usesClientSelector(file, alias) {
		t.Fatal("the AST walk failed to see http.Get in a file that plainly calls it")
	}
}

// The client-use walk must see a client call under an ALIAS. This is the case
// a naive `pkg.Name == "http"` check misses entirely.
func TestClientUseGuardDetectsAnAliasedClientCall(t *testing.T) {
	file := probeFile(t, `package suppliers

import fetcher "net/http"

func fetch() { _, _ = fetcher.Get("https://example.com") }
`, 0)

	alias, imported := httpImportAlias(file)
	if !imported {
		t.Fatal("alias resolution did not detect the net/http import")
	}
	if alias != "fetcher" {
		t.Fatalf("alias = %q, want fetcher — an aliased import must be resolved, or "+
			"`import fetcher \"net/http\"` bypasses the guard", alias)
	}
	if !usesClientSelector(file, alias) {
		t.Fatal("the AST walk failed to see fetcher.Get; alias resolution is not wired " +
			"into the selector check")
	}
}

// A dot import is detected and refused.
func TestClientUseGuardDetectsADotImport(t *testing.T) {
	file := probeFile(t, `package suppliers

import . "net/http"

var _ = DefaultClient
`, 0)

	alias, imported := httpImportAlias(file)
	if !imported || alias != "." {
		t.Fatalf("alias resolution = %q/%v, want ./true for a dot import", alias, imported)
	}
}

// Server-side use must NOT trip the guard, or the rule would be unusable and
// someone would weaken it.
func TestClientUseGuardIgnoresServerSideUse(t *testing.T) {
	file := probeFile(t, `package suppliers

import "net/http"

const (
	m = http.MethodPost
	s = http.StatusNoContent
	d = http.StatusText(http.StatusOK)
)
`, 0)

	alias, imported := httpImportAlias(file)
	if !imported {
		t.Fatal("expected the net/http import to be detected")
	}
	if usesClientSelector(file, alias) {
		t.Error("server-side constants were reported as client use; the guard must permit " +
			"http.MethodPost, http.StatusNoContent and friends")
	}
}

// A file with no net/http import reports no alias, so rule 2 skips it.
func TestAliasResolutionReportsAbsence(t *testing.T) {
	file := probeFile(t, `package suppliers

import "strings"

var _ = strings.TrimSpace
`, 0)

	if _, imported := httpImportAlias(file); imported {
		t.Error("a file that does not import net/http must report no alias")
	}
}

// A blank import binds nothing callable.
func TestAliasResolutionIgnoresABlankImport(t *testing.T) {
	file := probeFile(t, `package suppliers

import _ "net/http"
`, 0)

	if _, imported := httpImportAlias(file); imported {
		t.Error("a blank import binds no usable identifier and must report no alias")
	}
}

// clientSelectorsUsed returns every net/http client-side selector the file
// invokes through alias.
//
// The real guard and the self-verification tests both call THIS function, so a
// self-verification pass is evidence about the code that actually runs rather
// than about a lookalike that could drift from it.
func clientSelectorsUsed(file *ast.File, alias string) []string {
	var used []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != alias {
			return true
		}
		if _, bad := httpClientSelectors[sel.Sel.Name]; bad {
			used = append(used, sel.Sel.Name)
		}
		return true
	})
	return used
}

// usesClientSelector is the boolean form the self-verification tests read more
// naturally with.
func usesClientSelector(file *ast.File, alias string) bool {
	return len(clientSelectorsUsed(file, alias)) != 0
}
