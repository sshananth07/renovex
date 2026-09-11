package tenanttest_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// newStubAIService returns an httptest.Server that plays the Python
// ai-service's internal contract deterministically, so these Go-side tests
// exercise the full internal-auth/request/response wiring without requiring
// a real Python process. It requires the exact bearer token
// tenanttest.TestAIInternalToken, matching production's contract.
func newStubAIService(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+tenanttest.TestAIInternalToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/internal/v1/spaces/suggest":
			// evidenceType/sourceExcerpt (T1.5 quality gate) — sourceExcerpt
			// must actually ground against the brief text ("Full renovation
			// of a condo.") built by buildProjectWithScopeBrief, or
			// ApplySpaceQualityGate drops/downgrades the suggestion before
			// it ever reaches this test.
			_, _ = w.Write([]byte(`{
				"provider":"mock","model":"mock-v1","promptVersion":"spaces-v1","schemaVersion":1,
				"suggestions":[{"name":"Kitchen","spaceType":"kitchen","rationale":"explicit","confidence":0.9,"evidenceType":"explicit","sourceExcerpt":"Full renovation of a condo."}]
			}`))
		case "/internal/v1/work-items/suggest":
			// Echo back the first supplied Space's real ID so Go's domain
			// revalidation (spaceId must belong to the current Project)
			// accepts the suggestion — a static/fabricated ID would fail
			// lineage validation, same as production.
			var req struct {
				ProjectBrief string `json:"projectBrief"`
				Spaces       []struct {
					ID string `json:"id"`
				} `json:"spaces"`
			}
			_ = json.Unmarshal(body, &req)
			spaceID := ""
			if len(req.Spaces) > 0 {
				spaceID = req.Spaces[0].ID
			}
			// sourceExcerpt/materialSpecificity (T1.5 quality gate) —
			// sourceExcerpt must ground against req.ProjectBrief itself
			// (echoed back here, not the static fixture string above) or
			// ApplyWorkItemQualityGate drops the suggestion; a real
			// material ("ceramic floor tiles") is explicitly named in this
			// fixture's own description, so materialSpecificity=explicit
			// is accurate, not fabricated.
			_, _ = w.Write([]byte(`{
				"provider":"mock","model":"mock-v1","promptVersion":"work-items-v1","schemaVersion":1,
				"suggestions":[{"description":"Install ceramic floor tiles","workType":"tiling","scopeLevel":"space","spaceId":"` + spaceID + `","scopeOrigin":"explicit_scope","rationale":"explicit","confidence":0.9,"sourceExcerpt":"` + req.ProjectBrief + `","materialSpecificity":"explicit"}]
			}`))
		case "/internal/v1/resources/suggest":
			// Echo back the first supplied WorkItem's real ID for the same
			// lineage-validation reason as above.
			var req struct {
				WorkItems []struct {
					ID string `json:"id"`
				} `json:"workItems"`
			}
			_ = json.Unmarshal(body, &req)
			workItemID := ""
			if len(req.WorkItems) > 0 {
				workItemID = req.WorkItems[0].ID
			}
			_, _ = w.Write([]byte(`{
				"provider":"mock","model":"mock-v1","promptVersion":"resources-v1","schemaVersion":1,
				"suggestions":[{"resourceType":"trade","workItemId":"` + workItemID + `","name":"Tiler","candidateMaterialId":null,"rationale":"explicit","confidence":0.9}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func setupAIRouter(t *testing.T) http.Handler {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	connStr += "&directConnection=true"

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	db := platformmongo.Database(client, "tenanttest_ai")
	aiServer := newStubAIService(t)
	router, err := tenanttest.BuildRouterWithMailerAndAIServiceURL(t, db, nil, aiServer.URL)
	if err != nil {
		t.Fatalf("failed to build router: %v", err)
	}
	return router
}

// buildProjectWithScopeBrief creates Client -> Project and sets a non-empty
// ScopeBrief, returning the Project ID.
func buildProjectWithScopeBrief(t *testing.T, router http.Handler, company testCompany, brief string) string {
	t.Helper()
	rec := doJSON(t, router, http.MethodPost, "/clients", company.accessToken, map[string]string{"name": "Ahmad"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create client: %d %s", rec.Code, rec.Body.String())
	}
	clientID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/projects", company.accessToken, map[string]string{"clientId": clientID, "name": "Renovation"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create project: %d %s", rec.Code, rec.Body.String())
	}
	projectID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/projects/"+projectID+"/scope-brief", company.accessToken, map[string]string{"scopeBrief": brief})
	if rec.Code != http.StatusOK {
		t.Fatalf("set scope brief: %d %s", rec.Code, rec.Body.String())
	}
	return projectID
}

func TestAIRoutesRequireAuthentication(t *testing.T) {
	router := setupAIRouter(t)

	rec := doJSON(t, router, http.MethodPost, "/projects/x/ai/space-suggestions", "", map[string]any{"operationId": "op_1"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCompanyBCannotReadCompanyASpaceSuggestions(t *testing.T) {
	router := setupAIRouter(t)
	companyA := registerCompany(t, router, "owner-a@example.com", "Company A")
	companyB := registerCompany(t, router, "owner-b@example.com", "Company B")

	projectID := buildProjectWithScopeBrief(t, router, companyA, "Full renovation of a condo.")

	rec := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/space-suggestions", companyA.accessToken, map[string]any{"operationId": "op_1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("companyA generate: %d %s", rec.Code, rec.Body.String())
	}
	var genBody struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &genBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(genBody.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}

	// Company B cannot generate against Company A's Project.
	rec = doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/space-suggestions", companyB.accessToken, map[string]any{"operationId": "op_b1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB generating against companyA's project, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company B cannot list Company A's batches.
	rec = doJSON(t, router, http.MethodGet, "/projects/"+projectID+"/ai/batches?type=space_suggestions", companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Fatalf("unexpected status listing batches as companyB: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		var listBody struct {
			Batches []any `json:"batches"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(listBody.Batches) != 0 {
			t.Fatalf("expected companyB to see zero of companyA's batches, got %d", len(listBody.Batches))
		}
	}

	// Company B cannot read Company A's suggestions by batch id.
	rec = doJSON(t, router, http.MethodGet, "/ai-batches/"+genBody.Batch.ID+"/suggestions", companyB.accessToken, nil)
	if rec.Code == http.StatusOK {
		var listBody struct {
			Suggestions []any `json:"suggestions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(listBody.Suggestions) != 0 {
			t.Fatalf("expected companyB to see zero of companyA's suggestions, got %d", len(listBody.Suggestions))
		}
	}

	// Company B cannot accept Company A's suggestion.
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/accept", companyB.accessToken, map[string]any{"expectedRevision": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB accepting companyA's suggestion, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company B cannot reject Company A's suggestion either.
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/reject", companyB.accessToken, map[string]any{"expectedRevision": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB rejecting companyA's suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCompanyACanAcceptOwnSpaceSuggestionAndSeeRealSpace(t *testing.T) {
	router := setupAIRouter(t)
	companyA := registerCompany(t, router, "owner-a2@example.com", "Company A2")
	projectID := buildProjectWithScopeBrief(t, router, companyA, "Full renovation of a condo.")

	rec := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/space-suggestions", companyA.accessToken, map[string]any{"operationId": "op_accept_1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body.String())
	}
	var genBody struct {
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &genBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(genBody.Suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}

	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/accept", companyA.accessToken, map[string]any{"expectedRevision": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	var acceptBody struct {
		DomainObjectID string `json:"domainObjectId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &acceptBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if acceptBody.DomainObjectID == "" {
		t.Fatal("expected a created Space id")
	}

	// The accepted Space must show up through the ordinary Spaces list.
	rec = doJSON(t, router, http.MethodGet, "/spaces?projectId="+projectID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list spaces: %d %s", rec.Code, rec.Body.String())
	}
	var spacesBody struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spacesBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, s := range spacesBody.Items {
		if s.ID == acceptBody.DomainObjectID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected accepted Space %s to appear in normal Spaces list, got %+v", acceptBody.DomainObjectID, spacesBody.Items)
	}
}

// buildProjectWithSpaceAndWorkItem creates Client -> Project -> Space ->
// WorkItem, setting a non-empty ScopeBrief along the way, so both Work Item
// suggestion generation (requires >=1 confirmed Space) and Resource
// suggestion generation (requires >=1 confirmed WorkItem) can be exercised.
func buildProjectWithSpaceAndWorkItem(t *testing.T, router http.Handler, company testCompany) (projectID, workItemID string) {
	t.Helper()
	projectID = buildProjectWithScopeBrief(t, router, company, "Full renovation of a condo.")

	rec := doJSON(t, router, http.MethodPost, "/spaces", company.accessToken, map[string]string{
		"projectId": projectID, "name": "Kitchen", "type": "kitchen",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create space: %d %s", rec.Code, rec.Body.String())
	}
	spaceID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/work-items", company.accessToken, map[string]string{
		"projectId": projectID, "spaceId": spaceID, "description": "Install ceramic tiles",
		"workType": "tiling", "quantityValue": "30", "quantityUnit": "m2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create work item: %d %s", rec.Code, rec.Body.String())
	}
	workItemID = mustField(t, rec, "id")
	return projectID, workItemID
}

// TestCompanyBCannotReadCompanyAWorkItemSuggestions mirrors
// TestCompanyBCannotReadCompanyASpaceSuggestions for the work_item_suggestions
// batch type (design doc plan Task 18 Step 2 — the full HTTP matrix, not just
// Space suggestions).
func TestCompanyBCannotReadCompanyAWorkItemSuggestions(t *testing.T) {
	router := setupAIRouter(t)
	companyA := registerCompany(t, router, "owner-a-wi@example.com", "Company A WI")
	companyB := registerCompany(t, router, "owner-b-wi@example.com", "Company B WI")
	projectID, _ := buildProjectWithSpaceAndWorkItem(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/work-item-suggestions", companyA.accessToken, map[string]any{"operationId": "op_wi_1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("companyA generate work items: %d %s", rec.Code, rec.Body.String())
	}
	var genBody struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &genBody); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Company B cannot generate against Company A's Project.
	rec = doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/work-item-suggestions", companyB.accessToken, map[string]any{"operationId": "op_wi_b1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB generating work items against companyA's project, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company B cannot list Company A's work_item_suggestions batches.
	rec = doJSON(t, router, http.MethodGet, "/projects/"+projectID+"/ai/batches?type=work_item_suggestions", companyB.accessToken, nil)
	if rec.Code == http.StatusOK {
		var listBody struct {
			Batches []any `json:"batches"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(listBody.Batches) != 0 {
			t.Fatalf("expected companyB to see zero of companyA's work item batches, got %d", len(listBody.Batches))
		}
	} else if rec.Code != http.StatusNotFound {
		t.Fatalf("unexpected status listing work item batches as companyB: %d %s", rec.Code, rec.Body.String())
	}

	// Company B cannot read Company A's work item suggestions by batch id.
	rec = doJSON(t, router, http.MethodGet, "/ai-batches/"+genBody.Batch.ID+"/suggestions", companyB.accessToken, nil)
	if rec.Code == http.StatusOK {
		var listBody struct {
			Suggestions []any `json:"suggestions"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(listBody.Suggestions) != 0 {
			t.Fatalf("expected companyB to see zero of companyA's work item suggestions, got %d", len(listBody.Suggestions))
		}
	}

	if len(genBody.Suggestions) == 0 {
		t.Fatal("expected at least one work item suggestion to exercise accept/reject isolation")
	}

	// Company B cannot accept or reject Company A's work item suggestion.
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/accept", companyB.accessToken, map[string]any{
		"expectedRevision": 1,
		"workItem":         map[string]any{"description": "stolen", "scopeLevel": "project", "quantityValue": "1", "quantityUnit": "unit"},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB accepting companyA's work item suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/reject", companyB.accessToken, map[string]any{"expectedRevision": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB rejecting companyA's work item suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCompanyBCannotReadCompanyAResourceSuggestions mirrors the Space/Work
// Item cross-tenant tests for the resource_suggestions batch type, and also
// proves Company B cannot read Company A's WorkResourceRequirements via
// GET /work-resource-requirements (design doc plan Task 18 Step 2).
func TestCompanyBCannotReadCompanyAResourceSuggestions(t *testing.T) {
	router := setupAIRouter(t)
	companyA := registerCompany(t, router, "owner-a-res@example.com", "Company A Res")
	companyB := registerCompany(t, router, "owner-b-res@example.com", "Company B Res")
	projectID, workItemID := buildProjectWithSpaceAndWorkItem(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/resource-suggestions", companyA.accessToken, map[string]any{"operationId": "op_res_1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("companyA generate resources: %d %s", rec.Code, rec.Body.String())
	}
	var genBody struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &genBody); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Company B cannot generate against Company A's Project.
	rec = doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/ai/resource-suggestions", companyB.accessToken, map[string]any{"operationId": "op_res_b1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB generating resources against companyA's project, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(genBody.Suggestions) == 0 {
		t.Fatal("expected at least one resource suggestion to exercise accept/reject isolation")
	}

	// Company B cannot accept Company A's resource suggestion (trade/equipment
	// shape — the resource body needs no name override).
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/accept", companyB.accessToken, map[string]any{
		"expectedRevision": 1,
		"resource":         map[string]any{},
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB accepting companyA's resource suggestion, got %d: %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/reject", companyB.accessToken, map[string]any{"expectedRevision": 1})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for companyB rejecting companyA's resource suggestion, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company A accepts a resource suggestion for real, creating a
	// WorkResourceRequirement; Company B must never see it.
	rec = doJSON(t, router, http.MethodPost, "/ai-suggestions/"+genBody.Suggestions[0].ID+"/accept", companyA.accessToken, map[string]any{
		"expectedRevision": 1,
		"resource":         map[string]any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("companyA accept resource: %d %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/work-resource-requirements?projectId="+projectID+"&workItemId="+workItemID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("companyA list WRRs: %d %s", rec.Code, rec.Body.String())
	}
	var wrrBody struct {
		Requirements []any `json:"requirements"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wrrBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(wrrBody.Requirements) == 0 {
		t.Fatal("expected companyA to see its own accepted WorkResourceRequirement")
	}

	// Company B querying with Company A's projectId/workItemId must not see
	// Company A's WRRs — tenant scoping comes from the authenticated
	// company, not the query parameters.
	rec = doJSON(t, router, http.MethodGet, "/work-resource-requirements?projectId="+projectID+"&workItemId="+workItemID, companyB.accessToken, nil)
	if rec.Code == http.StatusOK {
		var bWrrBody struct {
			Requirements []any `json:"requirements"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &bWrrBody); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(bWrrBody.Requirements) != 0 {
			t.Fatalf("expected companyB to see zero of companyA's WorkResourceRequirements, got %d", len(bWrrBody.Requirements))
		}
	} else if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("unexpected status listing WRRs as companyB: %d %s", rec.Code, rec.Body.String())
	}
}

func TestDirectPythonAccessWithoutTokenFails(t *testing.T) {
	server := newStubAIService(t)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/internal/v1/spaces/suggest", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated direct access, got %d", resp.StatusCode)
	}
}
