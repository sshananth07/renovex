package spatial

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateVisualAssetContent_AcceptsRealRP4C4Assets is a sanity check
// against the actual project-original GLB/USDZ files RP4C4 generated and
// checked in — proving this slice's structural validator doesn't
// false-reject the exact assets RP4D's own manual verification plans to
// publish.
func TestValidateVisualAssetContent_AcceptsRealRP4C4Assets(t *testing.T) {
	repoRoot := findRepoRootForTest(t)
	modelsDir := filepath.Join(repoRoot, "apps", "web", "public", "models")

	cases := []struct {
		file   string
		format VisualAssetFormat
	}{
		{"fixture-boiler-v1.glb", VisualAssetFormatGLB},
		{"fixture-electrical-panel-v1.glb", VisualAssetFormatGLB},
		{"object-sofa-v1.glb", VisualAssetFormatGLB},
		{"format-proof.usdz", VisualAssetFormatUSDZ},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join(modelsDir, tc.file)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("unexpected error reading %s: %v", path, err)
			}
			if err := validateVisualAssetContent(tc.format, content); err != nil {
				t.Fatalf("expected real RP4C4 asset %s to pass validation, got: %v", tc.file, err)
			}
		})
	}
}

func findRepoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("unexpected error getting working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "apps", "web", "public", "models")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root containing apps/web/public/models from %s", dir)
		}
		dir = parent
	}
}
