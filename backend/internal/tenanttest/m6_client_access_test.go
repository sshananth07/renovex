package tenanttest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildFinalizedQuotation builds the full chain up to a finalized Quotation
// and returns the IDs M6's tests need.
func buildFinalizedQuotation(t *testing.T, router http.Handler, company testCompany) (projectID, quotationID string) {
	t.Helper()

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, company)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", company.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}
	quotationID = mustField(t, createResp, "id")

	finalizeResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", company.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing quotation, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	return projectID, quotationID
}

// shareQuotation shares and returns (grantID, rawToken).
func shareQuotation(t *testing.T, router http.Handler, company testCompany, quotationID string) (string, string) {
	t.Helper()
	resp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/share", company.accessToken, map[string]any{})
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201 sharing quotation, got %d: %s", resp.Code, resp.Body.String())
	}
	return mustField(t, resp, "grantId"), mustField(t, resp, "token")
}

// --- Share ---

func TestM6_ShareCreatesGrantAndReturnsTokenOnce(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-share@example.com", "M6 Share Co")
	projectID, quotationID := buildFinalizedQuotation(t, router, company)

	resp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/share", company.accessToken, map[string]any{})
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if body["token"] == nil || body["token"].(string) == "" {
		t.Fatal("expected a raw token on the first share")
	}
	if body["url"] == nil || !strings.Contains(body["url"].(string), body["token"].(string)) {
		t.Fatalf("expected the url to embed the token, got %v", body["url"])
	}
	if body["effectiveStatus"] != "active" {
		t.Fatalf("expected effectiveStatus active, got %v", body["effectiveStatus"])
	}

	// Project advanced to quotation_sent.
	projResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, company.accessToken, nil)
	if got := mustField(t, projResp, "status"); got != "quotation_sent" {
		t.Fatalf("expected project status quotation_sent, got %q", got)
	}
}

func TestM6_RepeatShareReturns200WithoutTokenOrURL(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-repeat@example.com", "M6 Repeat Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)

	shareQuotation(t, router, company, quotationID)

	repeat := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/share", company.accessToken, map[string]any{})
	if repeat.Code != http.StatusOK {
		t.Fatalf("expected 200 on repeat share, got %d: %s", repeat.Code, repeat.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(repeat.Body.Bytes(), &body)
	if _, hasToken := body["token"]; hasToken {
		t.Fatalf("expected NO token on a repeat share, got %s", repeat.Body.String())
	}
	if _, hasURL := body["url"]; hasURL {
		t.Fatalf("expected NO url on a repeat share, got %s", repeat.Body.String())
	}
}

func TestM6_ShareRejectsDraftQuotation(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-draft@example.com", "M6 Draft Co")
	projectID, estimateID, _ := buildFinalizedEstimate(t, router, company)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", company.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	draftID := mustField(t, createResp, "id")

	resp := doJSON(t, router, http.MethodPost, "/quotations/"+draftID+"/share", company.accessToken, map[string]any{})
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 sharing a draft, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestM6_ShareIsTenantScoped(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "m6-tenant-a@example.com", "M6 Tenant A")
	companyB := registerCompany(t, router, "m6-tenant-b@example.com", "M6 Tenant B")
	_, quotationID := buildFinalizedQuotation(t, router, companyA)

	resp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/share", companyB.accessToken, map[string]any{})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 cross-tenant, got %d: %s", resp.Code, resp.Body.String())
	}
}

// --- External view ---

func TestM6_ExternalViewReturnsAllowlistedFieldsOnly(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-view@example.com", "M6 View Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	resp := doJSON(t, router, http.MethodGet, "/client/quotations/"+token, "", nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	raw := resp.Body.String()
	// No internal field may appear anywhere in the payload.
	forbidden := []string{
		"estimateId", "generatedSubtotal", "notes", "revision",
		"sourceWorkItemIds", "companyId", "projectId", "clientId", "schemaVersion",
		"finalizedAt", "createdAt", "costSubtotal", "pricingMode", "marginBps",
	}
	for _, f := range forbidden {
		if strings.Contains(raw, f) {
			t.Fatalf("forbidden internal field %q leaked into the client view: %s", f, raw)
		}
	}

	var body map[string]any
	_ = json.Unmarshal(resp.Body.Bytes(), &body)
	for _, required := range []string{"quotationNumber", "version", "companyName", "projectName", "currency", "lines", "subtotal", "total", "status"} {
		if _, ok := body[required]; !ok {
			t.Fatalf("expected client-visible field %q, got %s", required, raw)
		}
	}
	if body["status"] != "finalized" {
		t.Fatalf("expected finalized, got %v", body["status"])
	}
}

func TestM6_ExternalResponsesCarrySecurityHeaders(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-headers@example.com", "M6 Headers Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	resp := doJSON(t, router, http.MethodGet, "/client/quotations/"+token, "", nil)
	if got := resp.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("expected Cache-Control: no-store, max-age=0, got %q", got)
	}
	if got := resp.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("expected Pragma: no-cache, got %q", got)
	}
	if got := resp.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("expected Referrer-Policy: no-referrer, got %q", got)
	}
	if got := resp.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("expected X-Content-Type-Options: nosniff, got %q", got)
	}
}

// Every unusable-token shape must produce an IDENTICAL 410 response.
func TestM6_AllUnusableTokensReturnIdentical410(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-410@example.com", "M6 Gone Co")

	// Revoked.
	_, revokedQuotation := buildFinalizedQuotation(t, router, company)
	revokedGrant, revokedToken := shareQuotation(t, router, company, revokedQuotation)
	if r := doJSON(t, router, http.MethodPost, "/access-grants/"+revokedGrant+"/revoke", company.accessToken,
		map[string]any{"expectedRevision": 0}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 revoking, got %d: %s", r.Code, r.Body.String())
	}

	// Rotated (the pre-rotation token).
	_, rotatedQuotation := buildFinalizedQuotation(t, router, company)
	rotatedGrant, preRotationToken := shareQuotation(t, router, company, rotatedQuotation)
	if r := doJSON(t, router, http.MethodPost, "/access-grants/"+rotatedGrant+"/rotate", company.accessToken,
		map[string]any{"expectedRevision": 0}); r.Code != http.StatusCreated {
		t.Fatalf("expected 201 rotating, got %d: %s", r.Code, r.Body.String())
	}

	cases := map[string]string{
		"malformed": "!!!not-a-valid-token!!!",
		"unknown":   "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWo",
		"revoked":   revokedToken,
		"rotated":   preRotationToken,
	}

	var canonicalBody string
	for name, token := range cases {
		resp := doJSON(t, router, http.MethodGet, "/client/quotations/"+token, "", nil)
		if resp.Code != http.StatusGone {
			t.Fatalf("%s: expected 410, got %d: %s", name, resp.Code, resp.Body.String())
		}
		if canonicalBody == "" {
			canonicalBody = resp.Body.String()
			continue
		}
		if resp.Body.String() != canonicalBody {
			t.Fatalf("%s: expected an IDENTICAL body to other unusable tokens.\n got: %s\nwant: %s",
				name, resp.Body.String(), canonicalBody)
		}
	}
	if strings.Contains(strings.ToLower(canonicalBody), "revok") ||
		strings.Contains(strings.ToLower(canonicalBody), "expir") ||
		strings.Contains(strings.ToLower(canonicalBody), "supersed") {
		t.Fatalf("the uniform 410 body leaks the specific reason: %s", canonicalBody)
	}
}

// --- Versioning ---

func TestM6_SharingV2SupersedesV1AndV1CannotBeReshared(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-supersede@example.com", "M6 Supersede Co")
	projectID, v1ID := buildFinalizedQuotation(t, router, company)
	_, v1Token := shareQuotation(t, router, company, v1ID)

	// Build V2 in the same chain.
	estResp := doJSON(t, router, http.MethodPost, "/estimates", company.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2500})
	if estResp.Code != http.StatusOK {
		// M5 allows only one estimate chain per project; create a new version instead.
		listResp := doJSON(t, router, http.MethodGet, "/estimates?projectId="+projectID, company.accessToken, nil)
		_ = listResp
	}
	v2Resp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/versions", company.accessToken,
		map[string]any{"estimateId": estimateIDForProject(t, router, company, projectID)})
	if v2Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating V2, got %d: %s", v2Resp.Code, v2Resp.Body.String())
	}
	v2ID := mustField(t, v2Resp, "id")
	if fin := doJSON(t, router, http.MethodPost, "/quotations/"+v2ID+"/finalize", company.accessToken,
		map[string]any{"expectedRevision": 0}); fin.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing V2, got %d: %s", fin.Code, fin.Body.String())
	}

	// Share V2 — supersedes V1.
	shareV2 := doJSON(t, router, http.MethodPost, "/quotations/"+v2ID+"/share", company.accessToken, map[string]any{})
	if shareV2.Code != http.StatusCreated {
		t.Fatalf("expected 201 sharing V2, got %d: %s", shareV2.Code, shareV2.Body.String())
	}

	// V1's token is now dead.
	if resp := doJSON(t, router, http.MethodGet, "/client/quotations/"+v1Token, "", nil); resp.Code != http.StatusGone {
		t.Fatalf("expected V1's token to be 410 after supersession, got %d", resp.Code)
	}

	// V1 cannot be re-shared.
	if resp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/share", company.accessToken,
		map[string]any{}); resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 re-sharing V1, got %d: %s", resp.Code, resp.Body.String())
	}
}

// estimateIDForProject returns the project's current finalized estimate.
func estimateIDForProject(t *testing.T, router http.Handler, company testCompany, projectID string) string {
	t.Helper()
	resp := doJSON(t, router, http.MethodGet, "/estimates?projectId="+projectID, company.accessToken, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 listing estimates, got %d: %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Estimates []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"estimates"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse estimates: %v", err)
	}
	for _, e := range body.Estimates {
		if e.Status == "finalized" {
			return e.ID
		}
	}
	t.Fatalf("no finalized estimate found for project %s: %s", projectID, resp.Body.String())
	return ""
}

// --- Client decisions ---

func TestM6_ClientAcceptUpdatesProjectAndLocksChain(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-accept@example.com", "M6 Accept Co")
	projectID, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	resp := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "",
		map[string]any{"clientName": "Ahmad", "clientEmail": "ahmad@example.com"})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 accepting, got %d: %s", resp.Code, resp.Body.String())
	}
	if got := mustField(t, resp, "status"); got != "accepted" {
		t.Fatalf("expected accepted, got %q", got)
	}

	projResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, company.accessToken, nil)
	if got := mustField(t, projResp, "status"); got != "quotation_approved" {
		t.Fatalf("expected quotation_approved, got %q", got)
	}

	// The contractor-facing share status reflects the decision.
	statusResp := doJSON(t, router, http.MethodGet, "/quotations/"+quotationID+"/share", company.accessToken, nil)
	var body map[string]any
	_ = json.Unmarshal(statusResp.Body.Bytes(), &body)
	decision, ok := body["decision"].(map[string]any)
	if !ok || decision["status"] != "accepted" {
		t.Fatalf("expected the decision surfaced to the contractor, got %s", statusResp.Body.String())
	}
}

func TestM6_RepeatAcceptIsIdempotent(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-idempotent@example.com", "M6 Idempotent Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	body := map[string]any{"clientName": "Ahmad"}
	if r := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "", body); r.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", r.Code, r.Body.String())
	}
	second := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "", body)
	if second.Code != http.StatusOK {
		t.Fatalf("expected a repeat accept to be idempotent 200, got %d: %s", second.Code, second.Body.String())
	}
	if got := mustField(t, second, "status"); got != "accepted" {
		t.Fatalf("expected accepted, got %q", got)
	}
}

func TestM6_RejectAfterAcceptanceIsRefused(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-reject-after@example.com", "M6 Reject After Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	if r := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "",
		map[string]any{}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 accepting, got %d: %s", r.Code, r.Body.String())
	}

	resp := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/reject", "",
		map[string]any{"comment": "changed my mind"})
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 rejecting after acceptance, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestM6_AcceptedChainCannotShareAnotherVersion(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-locked@example.com", "M6 Locked Co")
	projectID, v1ID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, v1ID)

	if r := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "",
		map[string]any{}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 accepting, got %d: %s", r.Code, r.Body.String())
	}

	v2Resp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/versions", company.accessToken,
		map[string]any{"estimateId": estimateIDForProject(t, router, company, projectID)})
	if v2Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating V2, got %d: %s", v2Resp.Code, v2Resp.Body.String())
	}
	v2ID := mustField(t, v2Resp, "id")
	doJSON(t, router, http.MethodPost, "/quotations/"+v2ID+"/finalize", company.accessToken,
		map[string]any{"expectedRevision": 0})

	resp := doJSON(t, router, http.MethodPost, "/quotations/"+v2ID+"/share", company.accessToken, map[string]any{})
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 sharing another version of an accepted chain, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestM6_RejectRequiresACommentAndDoesNotAdvanceProject(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-comment@example.com", "M6 Comment Co")
	projectID, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	blank := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/reject", "",
		map[string]any{"comment": "   "})
	if blank.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a blank comment, got %d: %s", blank.Code, blank.Body.String())
	}

	ok := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/reject", "",
		map[string]any{"comment": "Too expensive"})
	if ok.Code != http.StatusOK {
		t.Fatalf("expected 200 rejecting with a comment, got %d: %s", ok.Code, ok.Body.String())
	}

	// A rejection never advances the project past quotation_sent.
	projResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, company.accessToken, nil)
	if got := mustField(t, projResp, "status"); got != "quotation_sent" {
		t.Fatalf("expected the project to remain quotation_sent, got %q", got)
	}
}

func TestM6_ClientFieldValidation(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-validation@example.com", "M6 Validation Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	_, token := shareQuotation(t, router, company, quotationID)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"name too long", map[string]any{"clientName": strings.Repeat("a", 201)}},
		{"email too long", map[string]any{"clientEmail": strings.Repeat("a", 321)}},
		{"comment too long", map[string]any{"comment": strings.Repeat("a", 2001)}},
		{"invalid email", map[string]any{"clientEmail": "not-an-email"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, http.MethodPost, "/client/quotations/"+token+"/accept", "", tc.body)
			if resp.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 422, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}
}

// --- Rotation / revocation ---

func TestM6_RotationIssuesNewTokenAndKillsTheOld(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-rotate@example.com", "M6 Rotate Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	grantID, oldToken := shareQuotation(t, router, company, quotationID)

	resp := doJSON(t, router, http.MethodPost, "/access-grants/"+grantID+"/rotate", company.accessToken,
		map[string]any{"expectedRevision": 0})
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201 rotating, got %d: %s", resp.Code, resp.Body.String())
	}
	newToken := mustField(t, resp, "token")
	if newToken == oldToken {
		t.Fatal("expected a different token after rotation")
	}
	if mustField(t, resp, "grantId") == grantID {
		t.Fatal("expected rotation to create a new grant document")
	}

	if r := doJSON(t, router, http.MethodGet, "/client/quotations/"+oldToken, "", nil); r.Code != http.StatusGone {
		t.Fatalf("expected the old token to be 410, got %d", r.Code)
	}
	if r := doJSON(t, router, http.MethodGet, "/client/quotations/"+newToken, "", nil); r.Code != http.StatusOK {
		t.Fatalf("expected the new token to work, got %d: %s", r.Code, r.Body.String())
	}
}

func TestM6_RotationWithStaleRevisionIsRejected(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-stale@example.com", "M6 Stale Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	grantID, _ := shareQuotation(t, router, company, quotationID)

	resp := doJSON(t, router, http.MethodPost, "/access-grants/"+grantID+"/rotate", company.accessToken,
		map[string]any{"expectedRevision": 99})
	if resp.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a stale revision, got %d: %s", resp.Code, resp.Body.String())
	}
}

func TestM6_ManuallyRevokedGrantCanBeReplacedByRotation(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "m6-reshare@example.com", "M6 Reshare Co")
	_, quotationID := buildFinalizedQuotation(t, router, company)
	grantID, _ := shareQuotation(t, router, company, quotationID)

	if r := doJSON(t, router, http.MethodPost, "/access-grants/"+grantID+"/revoke", company.accessToken,
		map[string]any{"expectedRevision": 0}); r.Code != http.StatusOK {
		t.Fatalf("expected 200 revoking, got %d: %s", r.Code, r.Body.String())
	}

	resp := doJSON(t, router, http.MethodPost, "/access-grants/"+grantID+"/rotate", company.accessToken,
		map[string]any{"expectedRevision": 0})
	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201 rotating a manually-revoked grant, got %d: %s", resp.Code, resp.Body.String())
	}
	newToken := mustField(t, resp, "token")
	if r := doJSON(t, router, http.MethodGet, "/client/quotations/"+newToken, "", nil); r.Code != http.StatusOK {
		t.Fatalf("expected the replacement token to work, got %d", r.Code)
	}
}

// --- Tenant isolation on grant mutations ---

func TestM6_GrantMutationsAreTenantScoped(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "m6-grant-a@example.com", "M6 Grant A")
	companyB := registerCompany(t, router, "m6-grant-b@example.com", "M6 Grant B")
	_, quotationID := buildFinalizedQuotation(t, router, companyA)
	grantID, _ := shareQuotation(t, router, companyA, quotationID)

	for _, path := range []string{"/rotate", "/revoke"} {
		resp := doJSON(t, router, http.MethodPost, "/access-grants/"+grantID+path, companyB.accessToken,
			map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404 cross-tenant, got %d: %s", path, resp.Code, resp.Body.String())
		}
	}
}

// --- OpenAPI surface ---

func TestM6_OpenAPIRoutesAndSecurity(t *testing.T) {
	router := setupRouter(t)

	req, _ := http.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for openapi.json, got %d", rec.Code)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `json:"operationId"`
			Security    []map[string][]string `json:"security"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse openapi: %v", err)
	}

	authenticated := map[string]bool{
		"quotations-share": false, "quotations-share-status": false,
		"access-grants-rotate": false, "access-grants-revoke": false,
		"access-grants-update-expiry": false,
	}
	external := map[string]bool{
		"client-quotations-view": false, "client-quotations-accept": false,
		"client-quotations-reject": false, "client-quotations-request-changes": false,
	}

	for path, methods := range spec.Paths {
		for _, op := range methods {
			if _, ok := authenticated[op.OperationID]; ok {
				authenticated[op.OperationID] = true
				if len(op.Security) == 0 {
					t.Fatalf("%s (%s) must declare bearerAuth security", op.OperationID, path)
				}
			}
			if _, ok := external[op.OperationID]; ok {
				external[op.OperationID] = true
				// Phase G makes anonymous access explicit with one empty Security
				// Requirement Object. A named scheme would incorrectly imply that
				// the opaque-link Client must send an additional credential.
				if len(op.Security) != 1 || len(op.Security[0]) != 0 {
					t.Fatalf("%s (%s) must declare explicit anonymous security [{}], got %#v",
						op.OperationID, path, op.Security)
				}
			}
		}
	}
	for id, found := range authenticated {
		if !found {
			t.Fatalf("authenticated operation %q missing from the OpenAPI document", id)
		}
	}
	for id, found := range external {
		if !found {
			t.Fatalf("external operation %q missing from the OpenAPI document", id)
		}
	}
}
