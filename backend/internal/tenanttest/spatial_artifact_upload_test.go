package tenanttest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// nestedField reads a dotted path ("artifact.id") out of a JSON response
// body, for endpoints whose response nests fields under a sub-object
// (unlike mustField, which only reads top-level string fields).
func nestedField(t *testing.T, rec *httptest.ResponseRecorder, path string) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %s: %v", rec.Body.String(), err)
	}
	var cur any = resp
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("expected object navigating to %q in response %s", path, rec.Body.String())
		}
		cur, ok = m[part]
		if !ok {
			t.Fatalf("expected field %q in response %s", path, rec.Body.String())
		}
	}
	s, ok := cur.(string)
	if !ok {
		t.Fatalf("expected string field %q in response %s", path, rec.Body.String())
	}
	return s
}

func doRawPut(t *testing.T, router http.Handler, path string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestSpatialArtifactUpload_FullLifecycleOverHTTP exercises Task 3's entire
// resumable-artifact-upload contract through real HTTP: request an upload
// slot, PUT the bytes to the token-authenticated content-proxy route, then
// finalize. This is the golden path the design spec's capture bundle (§9)
// and offline resumable upload (§10.2) workflow depends on.
func TestSpatialArtifactUpload_FullLifecycleOverHTTP(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "spatial-artifact-owner@example.com", "Spatial Artifact Co")
	_, projectID, _, spaceID, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	captureRec := doJSON(t, router, http.MethodPost, "/spatial/captures", companyA.accessToken,
		map[string]string{"projectId": projectID, "spaceId": spaceID})
	if captureRec.Code != http.StatusOK {
		t.Fatalf("expected 200 starting capture, got %d: %s", captureRec.Code, captureRec.Body.String())
	}
	captureID := mustField(t, captureRec, "id")

	content := []byte("fake rgb keyframe bytes for golden path test")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	uploadRec := doJSON(t, router, http.MethodPost, "/spatial/artifacts", companyA.accessToken, map[string]any{
		"captureId": captureID, "kind": "rgb_keyframe", "contentType": "image/jpeg",
		"declaredSize": len(content), "checksum": checksum,
	})
	if uploadRec.Code != http.StatusOK {
		t.Fatalf("expected 200 requesting upload, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
	artifactID := nestedField(t, uploadRec, "artifact.id")
	uploadToken := nestedField(t, uploadRec, "uploadToken")
	if artifactID == "" || uploadToken == "" {
		t.Fatalf("expected non-empty artifactID and uploadToken, got %q / %q", artifactID, uploadToken)
	}

	putRec := doRawPut(t, router, "/spatial/artifacts/"+artifactID+"/content",
		map[string]string{"X-Spatial-Upload-Token": uploadToken}, content)
	if putRec.Code != http.StatusOK && putRec.Code != http.StatusNoContent {
		t.Fatalf("expected success PUTting content, got %d: %s", putRec.Code, putRec.Body.String())
	}

	finalizeRec := doJSON(t, router, http.MethodPost, "/spatial/artifacts/"+artifactID+"/finalize", companyA.accessToken,
		map[string]string{"uploadToken": uploadToken})
	if finalizeRec.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing, got %d: %s", finalizeRec.Code, finalizeRec.Body.String())
	}
	if status := mustField(t, finalizeRec, "status"); status != "uploaded" {
		t.Fatalf("expected status uploaded after finalize, got %s", status)
	}
}

// TestSpatialArtifactUpload_ContentEndpointRejectsWrongToken proves the
// content-proxy route is genuinely gated by the token, not merely by
// knowing the artifact ID (design spec §11/§42: short-lived signed access).
func TestSpatialArtifactUpload_ContentEndpointRejectsWrongToken(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "spatial-artifact-owner2@example.com", "Spatial Artifact Co 2")
	_, projectID, _, spaceID, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	captureRec := doJSON(t, router, http.MethodPost, "/spatial/captures", companyA.accessToken,
		map[string]string{"projectId": projectID, "spaceId": spaceID})
	captureID := mustField(t, captureRec, "id")

	uploadRec := doJSON(t, router, http.MethodPost, "/spatial/artifacts", companyA.accessToken, map[string]any{
		"captureId": captureID, "kind": "rgb_keyframe", "contentType": "image/jpeg",
		"declaredSize": 4, "checksum": "deadbeef",
	})
	artifactID := nestedField(t, uploadRec, "artifact.id")

	putRec := doRawPut(t, router, "/spatial/artifacts/"+artifactID+"/content",
		map[string]string{"X-Spatial-Upload-Token": "totally-wrong-token"}, []byte("data"))
	if putRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong upload token, got %d: %s", putRec.Code, putRec.Body.String())
	}
}
