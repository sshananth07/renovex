package tenanttest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// fakeAssetGenerationProvider is a scriptable AssetGenerationProvider for
// the real-HTTP integration tests in this file — no live Hunyuan call is
// made; only the HTTP/tenant-scoping/JSON-shape layer around
// SubmitAssetGenerationJob/GetAssetGenerationJob is exercised here (the
// worker lifecycle itself is already exhaustively proven at the service
// layer in internal/spatial's own test suite). Submit/poll never call
// the provider at all, so this fake is wired only to satisfy
// SetAssetGenerationSupport's signature.
type fakeAssetGenerationProvider struct{}

func (fakeAssetGenerationProvider) StartShapeGeneration(ctx context.Context, source spatial.AssetGenerationSource, seed int64) (spatial.ProviderGenerationRef, error) {
	return spatial.ProviderGenerationRef{ID: "evt_test"}, nil
}

func (fakeAssetGenerationProvider) ResumeShapeGeneration(ctx context.Context, ref spatial.ProviderGenerationRef) (spatial.ProviderGenerationResult, error) {
	return spatial.ProviderGenerationResult{Status: spatial.ProviderGenerationPending}, nil
}

// fakeAssetGenerationSourceAccessProviderNoop stands in for the source-
// access provider — never actually called by submit/poll (only by the
// worker, which these HTTP-layer tests do not drive).
type fakeAssetGenerationSourceAccessProviderNoop struct{}

func (fakeAssetGenerationSourceAccessProviderNoop) CreateSourceImageAccess(ctx context.Context, artifact spatial.SpatialArtifact, ttl time.Duration) (spatial.ReadAccess, error) {
	return spatial.ReadAccess{}, nil
}

// seedRealCaptureAndArtifact drives the REAL HTTP client/project/space/
// capture/artifact-upload flow (clients -> projects -> spaces ->
// spatial captures -> artifact request/PUT/finalize) so
// SubmitAssetGenerationJob has a genuinely uploaded, decodable source
// image to validate against — exactly the same real path a production
// caller would use, not a shortcut into repository internals.
func seedRealCaptureAndArtifact(t *testing.T, router http.Handler, accessToken string) (artifactID string) {
	t.Helper()

	clientRec := doJSON(t, router, http.MethodPost, "/clients", accessToken, map[string]any{"name": "AssetGen Test Client"})
	if clientRec.Code != http.StatusCreated && clientRec.Code != http.StatusOK {
		t.Fatalf("expected 200/201 creating client, got %d: %s", clientRec.Code, clientRec.Body.String())
	}
	var client struct {
		ID string `json:"id"`
	}
	mustDecode(t, clientRec, &client)

	projectRec := doJSON(t, router, http.MethodPost, "/projects", accessToken, map[string]any{"clientId": client.ID, "name": "AssetGen Test Project"})
	var project struct {
		ID string `json:"id"`
	}
	mustDecode(t, projectRec, &project)

	spaceRec := doJSON(t, router, http.MethodPost, "/spaces", accessToken, map[string]any{"projectId": project.ID, "name": "AssetGen Test Space"})
	var space struct {
		ID string `json:"id"`
	}
	mustDecode(t, spaceRec, &space)

	captureRec := doJSON(t, router, http.MethodPost, "/spatial/captures", accessToken, map[string]any{"projectId": project.ID, "spaceId": space.ID})
	if captureRec.Code != http.StatusOK && captureRec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 starting capture, got %d: %s", captureRec.Code, captureRec.Body.String())
	}
	var capture struct {
		ID string `json:"id"`
	}
	mustDecode(t, captureRec, &capture)

	// A real, decodable minimal JPEG.
	var imgBuf bytes.Buffer
	if err := jpeg.Encode(&imgBuf, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil); err != nil {
		t.Fatalf("encoding test JPEG: %v", err)
	}
	content := imgBuf.Bytes()
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])

	uploadRec := doJSON(t, router, http.MethodPost, "/spatial/artifacts", accessToken, map[string]any{
		"captureId": capture.ID, "kind": "observation_photo", "contentType": "image/jpeg",
		"declaredSize": len(content), "checksum": checksum,
	})
	if uploadRec.Code != http.StatusOK && uploadRec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 requesting artifact upload, got %d: %s", uploadRec.Code, uploadRec.Body.String())
	}
	var uploadResp struct {
		Artifact struct {
			ID string `json:"id"`
		} `json:"artifact"`
		UploadToken string `json:"uploadToken"`
	}
	mustDecode(t, uploadRec, &uploadResp)

	putReq := httptest.NewRequest(http.MethodPut, "/spatial/artifacts/"+uploadResp.Artifact.ID+"/content", bytes.NewReader(content))
	putReq.Header.Set("X-Spatial-Upload-Token", uploadResp.UploadToken)
	putReq.Header.Set("Content-Type", "application/octet-stream")
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK && putRec.Code != http.StatusNoContent {
		t.Fatalf("expected 200/204 putting artifact content, got %d: %s", putRec.Code, putRec.Body.String())
	}

	finalizeRec := doJSON(t, router, http.MethodPost, "/spatial/artifacts/"+uploadResp.Artifact.ID+"/finalize", accessToken, map[string]any{
		"uploadToken": uploadResp.UploadToken,
	})
	if finalizeRec.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing artifact, got %d: %s", finalizeRec.Code, finalizeRec.Body.String())
	}

	return uploadResp.Artifact.ID
}

func mustDecode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("failed to decode response %s: %v", rec.Body.String(), err)
	}
}

// TestAssetGenerationJob_SubmitAndPollOverHTTP proves the submit/poll
// routes' real HTTP behavior: authentication, correct JSON DTO shape,
// and — critically — that GeneratedObjectKey/ProviderRequestID never
// appear in the response body (RP4E0's infrastructure-only invariant).
func TestAssetGenerationJob_SubmitAndPollOverHTTP(t *testing.T) {
	_, db := setupRouterWithDatabase(t) // establishes a real Mongo container; the throwaway router here is discarded
	router, services, err := tenanttest.BuildRouterAndServicesForTest(t, db)
	if err != nil {
		t.Fatalf("unexpected error building router: %v", err)
	}
	// Wire a fake provider directly onto the SAME Service instance the
	// router's routes were registered against — mutating a separately
	// built composition.Services graph (even one sharing the same
	// database) would have zero effect on what the router actually
	// calls, since SetAssetGenerationSupport mutates in-memory struct
	// fields, not Mongo state.
	services.Spatial.SetAssetGenerationSupport(
		spatial.NewMongoAssetGenerationJobRepository(db),
		nil, // staging store not needed — submit/poll never touch it
		fakeAssetGenerationProvider{},
		fakeAssetGenerationSourceAccessProviderNoop{},
	)
	if err := spatial.NewMongoAssetGenerationJobRepository(db).EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	companyA := registerCompany(t, router, "assetgen-owner@example.com", "AssetGen Co")
	artifactID := seedRealCaptureAndArtifact(t, router, companyA.accessToken)

	submitRec := doJSON(t, router, http.MethodPost, "/spatial/asset-generation-jobs", companyA.accessToken, map[string]any{
		"clientRequestId": "req_http_1", "sourceArtifactId": artifactID, "seed": 42,
	})
	if submitRec.Code != http.StatusOK && submitRec.Code != http.StatusCreated {
		t.Fatalf("expected 200/201 submitting, got %d: %s", submitRec.Code, submitRec.Body.String())
	}
	var submitted struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	mustDecode(t, submitRec, &submitted)
	if submitted.ID == "" || submitted.Status != "pending" {
		t.Fatalf("expected a pending job with a real ID, got %+v", submitted)
	}
	if strings.Contains(submitRec.Body.String(), "generatedObjectKey") || strings.Contains(submitRec.Body.String(), "providerRequestId") {
		t.Fatalf("expected no infrastructure-only fields in the submit response, got %s", submitRec.Body.String())
	}

	pollRec := doJSON(t, router, http.MethodGet, "/spatial/asset-generation-jobs/"+submitted.ID, companyA.accessToken, nil)
	if pollRec.Code != http.StatusOK {
		t.Fatalf("expected 200 polling, got %d: %s", pollRec.Code, pollRec.Body.String())
	}
	var polled struct {
		ID string `json:"id"`
	}
	mustDecode(t, pollRec, &polled)
	if polled.ID != submitted.ID {
		t.Fatalf("expected the polled job to match the submitted job, got %+v", polled)
	}
	if strings.Contains(pollRec.Body.String(), "generatedObjectKey") || strings.Contains(pollRec.Body.String(), "providerRequestId") {
		t.Fatalf("expected no infrastructure-only fields in the poll response, got %s", pollRec.Body.String())
	}
}

// TestAssetGenerationJob_CrossCompanyDenied proves company B cannot poll
// company A's job merely by knowing its ID.
func TestAssetGenerationJob_CrossCompanyDenied(t *testing.T) {
	router, db := setupRouterWithDatabase(t)
	router, services, err := tenanttest.BuildRouterAndServicesForTest(t, db)
	if err != nil {
		t.Fatalf("unexpected error building services: %v", err)
	}
	services.Spatial.SetAssetGenerationSupport(
		spatial.NewMongoAssetGenerationJobRepository(db),
		nil,
		fakeAssetGenerationProvider{},
		fakeAssetGenerationSourceAccessProviderNoop{},
	)
	if err := spatial.NewMongoAssetGenerationJobRepository(db).EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("EnsureIndexes: %v", err)
	}

	companyA := registerCompany(t, router, "assetgen-a@example.com", "AssetGen Co A")
	companyB := registerCompany(t, router, "assetgen-b@example.com", "AssetGen Co B")
	artifactID := seedRealCaptureAndArtifact(t, router, companyA.accessToken)

	submitRec := doJSON(t, router, http.MethodPost, "/spatial/asset-generation-jobs", companyA.accessToken, map[string]any{
		"clientRequestId": "req_cross_1", "sourceArtifactId": artifactID, "seed": 42,
	})
	var submitted struct {
		ID string `json:"id"`
	}
	mustDecode(t, submitRec, &submitted)

	pollRec := doJSON(t, router, http.MethodGet, "/spatial/asset-generation-jobs/"+submitted.ID, companyB.accessToken, nil)
	if pollRec.Code == http.StatusOK {
		t.Fatalf("expected company B to be denied access to company A's job, got 200: %s", pollRec.Body.String())
	}
}
