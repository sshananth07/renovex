package composition_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func powershellExecutable(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"pwsh", "powershell.exe", "powershell"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Fatal("PowerShell is required to exercise scripts/verify-format.ps1")
	return ""
}

func runFormattingGate(t *testing.T, target string) (string, error) {
	t.Helper()
	script := filepath.Join(backendRoot(t), "scripts", "verify-format.ps1")
	cmd := exec.Command(powershellExecutable(t), "-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", script, target)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// The repository gate must execute gofmt, not merely check that a script file
// exists. A deliberately malformed fixture proves the command can fail and
// reports the file the contractor needs to format.
func TestVerifyFormatScriptRejectsAnUnformattedTree(t *testing.T) {
	target := t.TempDir()
	badFile := filepath.Join(target, "unformatted.go")
	if err := os.WriteFile(badFile, []byte("package probe\nfunc f(){println(\"x\")}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runFormattingGate(t, target)
	if err == nil {
		t.Fatalf("formatting gate accepted an unformatted Go file; output: %s", output)
	}
	if !strings.Contains(output, "unformatted.go") {
		t.Fatalf("failure output does not identify the unformatted file: %s", output)
	}
}

// A clean tree is the success path E4 and future CI use. This also proves the
// script accepts an explicit target rather than depending on the caller's cwd.
func TestVerifyFormatScriptAcceptsAFormattedTree(t *testing.T) {
	target := t.TempDir()
	goodFile := filepath.Join(target, "formatted.go")
	if err := os.WriteFile(goodFile, []byte("package probe\n\nfunc f() { println(\"x\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runFormattingGate(t, target)
	if err != nil {
		t.Fatalf("formatting gate rejected a formatted Go file: %v\n%s", err, output)
	}
}
