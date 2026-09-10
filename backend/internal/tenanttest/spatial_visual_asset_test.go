package tenanttest_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// buildTestGLB constructs a minimal, self-contained, VALID GLB (matching
// this project's own confirmed real GLB binary layout — 12-byte header,
// one JSON chunk describing an empty scene) so RP4D's real
// validateVisualAssetContent structural check genuinely accepts it,
// rather than a fake/placeholder byte string that would be rejected before
// this test ever reaches the HTTP layer it's meant to exercise.
func buildTestGLB() []byte {
	jsonChunk := []byte(`{"asset":{"version":"2.0"}}`)
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	glb := []byte{}
	appendU32 := func(v uint32) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], v)
		glb = append(glb, b[:]...)
	}
	glb = append(glb, 'g', 'l', 'T', 'F')
	appendU32(2)
	appendU32(0) // patched below
	appendU32(uint32(len(jsonChunk)))
	glb = append(glb, 'J', 'S', 'O', 'N')
	glb = append(glb, jsonChunk...)
	total := uint32(len(glb))
	binary.LittleEndian.PutUint32(glb[8:12], total)
	return glb
}

func doRawGet(t *testing.T, router http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// TestVisualAssetAccess_FullLifecycleOverHTTP publishes a real visual asset
// version directly through Service.PublishVisualAssetVersion (RP4D's own
// internal-only publish API — no public HTTP route by design), mints an
// access capability through the real authenticated HTTP route, then fetches
// the actual bytes through the real unauthenticated content route — proving
// the full chain end to end, including that the returned bytes match
// exactly what was published.
func TestVisualAssetAccess_FullLifecycleOverHTTP(t *testing.T) {
	router, db := setupRouterWithDatabase(t)
	services, err := tenanttest.BuildServicesForTest(db)
	if err != nil {
		t.Fatalf("unexpected error building services: %v", err)
	}
	companyA := registerCompany(t, router, "visual-asset-owner@example.com", "Visual Asset Co")

	// Resolve companyA's real companyID the same way every other tenant
	// test does — by decoding the JWT access token's claims is overkill
	// here; instead, publish under a KNOWN companyID and prove isolation
	// via the actual authenticated /me identity resolved by the router
	// itself when minting access (the access route resolves companyID
	// from the authenticated principal, not from anything this test
	// supplies) — so we don't need to know companyA's ID up front at all,
	// we only need PublishVisualAssetVersion to target THE SAME company
	// that /auth/register just created. Fetch it via /me.
	meRec := doAuthedGet(t, router, "/auth/me", companyA.accessToken)
	var me struct {
		CompanyID string `json:"companyId"`
	}
	if err := json.Unmarshal(meRec.Body.Bytes(), &me); err != nil {
		t.Fatalf("failed to parse /me response %s: %v", meRec.Body.String(), err)
	}
	if me.CompanyID == "" {
		t.Fatalf("expected a non-empty companyId from /me, got %s", meRec.Body.String())
	}

	content := buildTestGLB()
	published, err := services.Spatial.PublishVisualAssetVersion(
		context.Background(), me.CompanyID, "boiler-asset", 1,
		spatial.VisualAssetFormatGLB, "Boiler v1",
		spatial.VisualAssetNormalization{Pivot: spatial.VisualAssetPivotCenterBottom},
		bytes.NewReader(content),
	)
	if err != nil {
		t.Fatalf("unexpected error publishing: %v", err)
	}
	if published.Checksum == "" {
		t.Fatalf("expected a computed checksum")
	}

	accessRec := doJSON(t, router, http.MethodPost, "/spatial/visual-assets/boiler-asset/versions/1/access", companyA.accessToken, nil)
	if accessRec.Code != http.StatusOK {
		t.Fatalf("expected 200 minting access, got %d: %s", accessRec.Code, accessRec.Body.String())
	}
	if cacheControl := accessRec.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("expected Cache-Control: no-store on the access response, got %q", cacheControl)
	}
	var accessResp struct {
		Asset struct {
			AssetID  string `json:"assetId"`
			Version  int    `json:"version"`
			Checksum string `json:"checksum"`
		} `json:"asset"`
		Access struct {
			URL string `json:"url"`
		} `json:"access"`
	}
	if err := json.Unmarshal(accessRec.Body.Bytes(), &accessResp); err != nil {
		t.Fatalf("failed to parse access response %s: %v", accessRec.Body.String(), err)
	}
	if accessResp.Asset.AssetID != "boiler-asset" || accessResp.Asset.Version != 1 {
		t.Fatalf("expected asset identity to match, got %+v", accessResp.Asset)
	}
	// "visual-assets/" alone is NOT a safe substring check here — the
	// legitimate content-route PATH is "/spatial/visual-assets/content",
	// which contains that exact text. Check for the STORAGE KEY shape
	// specifically instead: "visual-assets/<companyId>/<assetId>/v<n>/".
	if strings.Contains(accessRec.Body.String(), "storageKey") ||
		strings.Contains(accessRec.Body.String(), "visual-assets/"+me.CompanyID+"/") {
		t.Fatalf("expected no storage key to leak into the access response, got %s", accessRec.Body.String())
	}

	// The returned URL is absolute (per RP4D's cross-origin requirement) —
	// extract just the path+query to drive against this same test router.
	idx := strings.Index(accessResp.Access.URL, "/spatial/visual-assets/content")
	if idx < 0 {
		t.Fatalf("expected the access URL to point at the content route, got %s", accessResp.Access.URL)
	}
	contentPath := accessResp.Access.URL[idx:]

	contentRec := doRawGet(t, router, contentPath)
	if contentRec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching content, got %d: %s", contentRec.Code, contentRec.Body.String())
	}
	if !bytes.Equal(contentRec.Body.Bytes(), content) {
		t.Fatalf("expected fetched bytes to match published content exactly")
	}
	if contentType := contentRec.Header().Get("Content-Type"); contentType != "model/gltf-binary" {
		t.Fatalf("expected Content-Type model/gltf-binary, got %q", contentType)
	}
	if cacheControl := contentRec.Header().Get("Cache-Control"); cacheControl != "private, no-store" {
		t.Fatalf("expected Cache-Control: private, no-store on the content response, got %q", cacheControl)
	}
	if etag := contentRec.Header().Get("ETag"); etag == "" {
		t.Fatalf("expected a non-empty ETag")
	}
}

// TestVisualAssetAccess_CrossCompanyDenied proves company B cannot mint
// access to company A's published asset merely by knowing its assetId —
// the authenticated access route resolves companyID from the caller's OWN
// principal, never from anything the caller supplies.
func TestVisualAssetAccess_CrossCompanyDenied(t *testing.T) {
	router, db := setupRouterWithDatabase(t)
	services, err := tenanttest.BuildServicesForTest(db)
	if err != nil {
		t.Fatalf("unexpected error building services: %v", err)
	}
	companyA := registerCompany(t, router, "visual-asset-a@example.com", "Visual Asset Co A")
	companyB := registerCompany(t, router, "visual-asset-b@example.com", "Visual Asset Co B")

	meRec := doAuthedGet(t, router, "/auth/me", companyA.accessToken)
	var me struct {
		CompanyID string `json:"companyId"`
	}
	_ = json.Unmarshal(meRec.Body.Bytes(), &me)

	_, err = services.Spatial.PublishVisualAssetVersion(
		context.Background(), me.CompanyID, "boiler-asset", 1,
		spatial.VisualAssetFormatGLB, "Boiler v1",
		spatial.VisualAssetNormalization{Pivot: spatial.VisualAssetPivotCenterBottom},
		bytes.NewReader(buildTestGLB()),
	)
	if err != nil {
		t.Fatalf("unexpected error publishing: %v", err)
	}

	// Company B attempts to mint access to the SAME assetId/version.
	accessRec := doJSON(t, router, http.MethodPost, "/spatial/visual-assets/boiler-asset/versions/1/access", companyB.accessToken, nil)
	if accessRec.Code == http.StatusOK {
		t.Fatalf("expected company B to be denied access to company A's asset, got 200: %s", accessRec.Body.String())
	}
}

// TestVisualAssetAccess_ContentEndpointRejectsTamperedCapability proves the
// content route is genuinely gated by capability SIGNATURE verification,
// not merely by a well-formed-looking query parameter.
func TestVisualAssetAccess_ContentEndpointRejectsTamperedCapability(t *testing.T) {
	router, db := setupRouterWithDatabase(t)
	services, err := tenanttest.BuildServicesForTest(db)
	if err != nil {
		t.Fatalf("unexpected error building services: %v", err)
	}
	companyA := registerCompany(t, router, "visual-asset-tamper@example.com", "Visual Asset Tamper Co")

	meRec := doAuthedGet(t, router, "/auth/me", companyA.accessToken)
	var me struct {
		CompanyID string `json:"companyId"`
	}
	_ = json.Unmarshal(meRec.Body.Bytes(), &me)

	_, err = services.Spatial.PublishVisualAssetVersion(
		context.Background(), me.CompanyID, "boiler-asset", 1,
		spatial.VisualAssetFormatGLB, "Boiler v1",
		spatial.VisualAssetNormalization{Pivot: spatial.VisualAssetPivotCenterBottom},
		bytes.NewReader(buildTestGLB()),
	)
	if err != nil {
		t.Fatalf("unexpected error publishing: %v", err)
	}

	accessRec := doJSON(t, router, http.MethodPost, "/spatial/visual-assets/boiler-asset/versions/1/access", companyA.accessToken, nil)
	var accessResp struct {
		Access struct {
			URL string `json:"url"`
		} `json:"access"`
	}
	_ = json.Unmarshal(accessRec.Body.Bytes(), &accessResp)
	idx := strings.Index(accessResp.Access.URL, "/spatial/visual-assets/content")
	contentPath := accessResp.Access.URL[idx:] + "TAMPERED"

	contentRec := doRawGet(t, router, contentPath)
	if contentRec.Code == http.StatusOK {
		t.Fatalf("expected a tampered capability to be rejected, got 200")
	}
}

func doAuthedGet(t *testing.T, router http.Handler, path, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}
