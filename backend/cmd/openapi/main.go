// Command openapi generates the production OpenAPI document without
// connecting to MongoDB, SMTP, or opening a network listener — every handler
// is registered with nil service dependencies purely for schema
// construction (composition.RegisterAllForSchema). This lets a frontend
// build pipeline regenerate its TypeScript client from a single, fast,
// dependency-free command instead of needing the full backend stack running.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run builds the OpenAPI document and writes it to the location args
// specify, returning the process exit code. Extracted from main so it is
// testable without spawning a subprocess.
func run(args []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("openapi", flag.ContinueOnError)
	out := fs.String("out", "-", `output path, or "-" for stdout`)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	document, err := buildOpenAPIDocument()
	if err != nil {
		fmt.Fprintf(stdout, "openapi: %v\n", err)
		return 1
	}

	if *out == "-" {
		if _, err := stdout.Write(document); err != nil {
			return 1
		}
		return 0
	}

	if err := writeToFile(*out, document); err != nil {
		fmt.Fprintf(stdout, "openapi: %v\n", err)
		return 1
	}
	return 0
}

// buildOpenAPIDocument registers every production route with nil services
// (schema construction only — see composition.RegisterAllForSchema) and
// marshals the resulting OpenAPI document as JSON. The title/version here
// intentionally match cmd/api/main.go's platformhttp.NewRouter call, since
// this document must describe the same API, not a test double of it.
func buildOpenAPIDocument() ([]byte, error) {
	_, api := platformhttp.NewRouter("Renovation Project Intelligence API", "0.1.0")
	composition.RegisterAllForSchema(api)
	return json.MarshalIndent(api.OpenAPI(), "", "  ")
}

func writeToFile(path string, document []byte) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating output directory %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, document, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
