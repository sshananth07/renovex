package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesToStdoutWhenOutIsDash(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"-out", "-"}, &stdout)
	if code != 0 {
		t.Fatalf("exit code = %d, stdout: %s", code, stdout.String())
	}

	var document map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("stdout is not valid JSON: %v; body: %s", err, stdout.String())
	}
	if document["openapi"] == nil {
		t.Fatalf("expected an \"openapi\" field in the document, got %v", document)
	}
}

func TestRunWritesToFileAndCreatesParentDirectories(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "nested", "deeper", "openapi.json")

	var stdout bytes.Buffer
	code := run([]string{"-out", outPath}, &stdout)
	if code != 0 {
		t.Fatalf("exit code = %d, stdout: %s", code, stdout.String())
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("output file is not valid JSON: %v", err)
	}
}

func TestRunProducesOpenAPI31Document(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"-out", "-"}, &stdout)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}

	var document struct {
		OpenAPI string `json:"openapi"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("decoding document: %v", err)
	}
	if document.OpenAPI[:4] != "3.1." && document.OpenAPI != "3.1" {
		t.Fatalf("openapi version = %q, want 3.1.x", document.OpenAPI)
	}
}

func TestRunProducesUniqueOperationIDs(t *testing.T) {
	var stdout bytes.Buffer
	code := run([]string{"-out", "-"}, &stdout)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}

	var document struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("decoding document: %v", err)
	}

	seen := map[string]bool{}
	for _, methods := range document.Paths {
		for _, op := range methods {
			if op.OperationID == "" {
				continue
			}
			if seen[op.OperationID] {
				t.Fatalf("duplicate operationId %q", op.OperationID)
			}
			seen[op.OperationID] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("expected at least one operation in the document")
	}
}

func TestRunInvalidOutputLocationReturnsNonZero(t *testing.T) {
	// A path whose parent cannot be created (a regular file used as a
	// directory component) must fail cleanly with a non-zero exit code, not
	// panic and not silently succeed.
	dir := t.TempDir()
	blockingFile := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("seeding blocking file: %v", err)
	}
	invalidOut := filepath.Join(blockingFile, "openapi.json")

	var stdout bytes.Buffer
	code := run([]string{"-out", invalidOut}, &stdout)
	if code == 0 {
		t.Fatal("expected a non-zero exit code for an invalid output location")
	}
}

func TestRunTwoConsecutiveRunsProduceByteIdenticalOutput(t *testing.T) {
	var first, second bytes.Buffer
	if code := run([]string{"-out", "-"}, &first); code != 0 {
		t.Fatalf("first run exit code = %d", code)
	}
	if code := run([]string{"-out", "-"}, &second); code != 0 {
		t.Fatalf("second run exit code = %d", code)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("two consecutive runs produced different output")
	}
}
