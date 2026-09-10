package tenanttest_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// E3: Milestone 7 tenant isolation, exercised through the real HTTP router
// against a real MongoDB — Material Requirements, RFQs, Suppliers, Offerings
// and the preferred-supplier relationship.
//
// The two invariants under test are the ones a unit test cannot establish:
// every route is scoped to the authenticated company, and the cross-PROJECT
// claim filter of §7.2 holds end to end.

// --- fixtures ---

// buildProcurementProject returns a project with one live WorkItem and one
// Material, which is the minimum a Material Requirement needs.
func buildProcurementProject(t *testing.T, router http.Handler, company testCompany) (
	projectID, workItemID, materialID string) {
	t.Helper()

	_, projectID, _, _, workItemID = buildFullHierarchyForCompanyA(t, router, company)

	materialResp := doJSON(t, router, http.MethodPost, "/materials", company.accessToken,
		map[string]any{
			"name": "Portland Cement", "category": "cement", "unit": "bag",
			"specification": "OPC 50kg",
			// Reference price is required by the frozen M3 Material API. It is
			// informational here; M7 uses only the identity, unit and specification.
			"referencePriceAmount": 1850, "referencePriceCurrency": "MYR",
		})
	if materialResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material, got %d: %s",
			materialResp.Code, materialResp.Body.String())
	}
	return projectID, workItemID, mustField(t, materialResp, "id")
}

// createRequirement creates a manual Material Requirement and returns it.
func createRequirement(t *testing.T, router http.Handler, company testCompany,
	projectID, workItemID, materialID string) *httptest.ResponseRecorder {
	t.Helper()

	resp := doJSON(t, router, http.MethodPost, "/material-requirements", company.accessToken,
		map[string]any{
			"projectId": projectID, "workItemId": workItemID, "materialId": materialID,
			"quantityValue": "100", "quantityUnit": "bag",
			"procurementNotes": "deliver to site gate",
			"internalNotes":    "contractor-only margin note",
		})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material requirement, got %d: %s",
			resp.Code, resp.Body.String())
	}
	return resp
}

// reviewedRequirement creates a requirement and reviews it, so it is
// RFQ-eligible.
func reviewedRequirement(t *testing.T, router http.Handler, company testCompany,
	projectID, workItemID, materialID string) (requirementID string, revision float64) {
	t.Helper()

	created := createRequirement(t, router, company, projectID, workItemID, materialID)
	id := mustField(t, created, "id")

	reviewResp := doJSON(t, router, http.MethodPost,
		"/material-requirements/"+id+"/review", company.accessToken,
		map[string]any{"expectedRevision": mustNumberField(t, created, "revision")})
	if reviewResp.Code != http.StatusOK {
		t.Fatalf("expected 200 reviewing requirement, got %d: %s",
			reviewResp.Code, reviewResp.Body.String())
	}
	return id, mustNumberField(t, reviewResp, "revision")
}

// createRFQWithLine builds the smallest consistent RFQ chain for exercising
// line-scoped routes. The returned Revision is the post-append RFQ revision,
// which makes every hostile request structurally valid before tenancy is
// evaluated.
func createRFQWithLine(t *testing.T, router http.Handler, company testCompany,
	projectID, workItemID, materialID string) (
	rfqID, lineID, requirementID string, rfqRevision float64) {
	t.Helper()

	requirementID, requirementRevision := reviewedRequirement(
		t, router, company, projectID, workItemID, materialID)
	rfqResp := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/rfqs",
		company.accessToken, map[string]any{"deliveryAddress": "12 Site Road"})
	if rfqResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating rfq, got %d: %s",
			rfqResp.Code, rfqResp.Body.String())
	}
	rfqID = mustField(t, rfqResp, "id")

	addResp := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/lines",
		company.accessToken, map[string]any{
			"materialRequirementId":       requirementID,
			"expectedRequirementRevision": requirementRevision,
			"expectedRfqRevision":         mustNumberField(t, rfqResp, "revision"),
		})
	if addResp.Code != http.StatusOK {
		t.Fatalf("expected 200 adding rfq line, got %d: %s",
			addResp.Code, addResp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(addResp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	lines, ok := body["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("expected one RFQ line, got %+v", body["lines"])
	}
	line, ok := lines[0].(map[string]any)
	if !ok {
		t.Fatalf("expected an RFQ line object, got %+v", lines[0])
	}
	lineID, ok = line["id"].(string)
	if !ok || lineID == "" {
		t.Fatalf("expected RFQ line id, got %+v", line["id"])
	}
	return rfqID, lineID, requirementID, mustNumberField(t, addResp, "revision")
}

func createSupplier(t *testing.T, router http.Handler, company testCompany,
	name string) *httptest.ResponseRecorder {
	t.Helper()

	resp := doJSON(t, router, http.MethodPost, "/suppliers", company.accessToken,
		map[string]any{
			"name": name, "contactPerson": "Procurement Desk",
			"materialCategories": []string{"Cement"},
		})
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating supplier, got %d: %s", resp.Code, resp.Body.String())
	}
	return resp
}

// --- Material Requirements ---

func TestTenantIsolation_MaterialRequirementCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "mr-tenant-a@example.com", "MR Tenant A")
	companyB := registerCompany(t, router, "mr-tenant-b@example.com", "MR Tenant B")

	projectID, workItemID, materialID := buildProcurementProject(t, router, companyA)
	created := createRequirement(t, router, companyA, projectID, workItemID, materialID)
	requirementID := mustField(t, created, "id")

	t.Run("get", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodGet,
			"/material-requirements/"+requirementID, companyB.accessToken, nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("patch", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPatch,
			"/material-requirements/"+requirementID, companyB.accessToken,
			map[string]any{"expectedRevision": 0, "quantityValue": "999"})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("review", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/review", companyB.accessToken,
			map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("acknowledge unit", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/acknowledge-unit", companyB.accessToken,
			map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("archive", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/archive", companyB.accessToken,
			map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("read source discrepancy", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodGet,
			"/material-requirements/"+requirementID+"/source-discrepancy",
			companyB.accessToken, nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("resolve source discrepancy", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/source-discrepancy/resolve",
			companyB.accessToken, map[string]any{
				"action": "keep_current", "expectedRevision": 0,
				"expectedProposedFingerprint": "foreign-tenant-must-not-reach-source",
			})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("split", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/split", companyB.accessToken,
			map[string]any{
				"expectedRevision": 0,
				"children": []map[string]any{
					{"quantityValue": "40", "quantityUnit": "bag"},
					{"quantityValue": "60", "quantityUnit": "bag"},
				},
			})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("reconcile split", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost,
			"/material-requirements/"+requirementID+"/split/reconcile",
			companyB.accessToken, map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodDelete,
			"/material-requirements/"+requirementID, companyB.accessToken,
			map[string]any{"expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	// The requirement must be untouched after every rejected attempt.
	after := doJSON(t, router, http.MethodGet,
		"/material-requirements/"+requirementID, companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost access to its own requirement: %d", after.Code)
	}
	if mustField(t, after, "status") != "draft" {
		t.Errorf("status = %s, want draft — a rejected cross-tenant call mutated the record",
			mustField(t, after, "status"))
	}
}

// Unlike archive, a successful delete leaves nothing behind: the requirement
// is genuinely gone afterward, not merely marked terminal.
func TestMaterialRequirementDeletePermanentlyRemovesIt(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "mr-delete@example.com", "MR Delete Co")

	projectID, workItemID, materialID := buildProcurementProject(t, router, company)
	created := createRequirement(t, router, company, projectID, workItemID, materialID)
	requirementID := mustField(t, created, "id")

	resp := doJSON(t, router, http.MethodDelete,
		"/material-requirements/"+requirementID, company.accessToken,
		map[string]any{"expectedRevision": 0})
	if resp.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", resp.Code, resp.Body.String())
	}

	after := doJSON(t, router, http.MethodGet,
		"/material-requirements/"+requirementID, company.accessToken, nil)
	if after.Code != http.StatusNotFound {
		t.Fatalf("expected the deleted requirement to be gone, got %d: %s", after.Code, after.Body.String())
	}
}

// Manual creation carries three foreign-reference risks in one request:
// Project, WorkItem and Material. The service validates every one under the
// authenticated company and must stop at 404 before inserting a requirement.
func TestTenantIsolation_MaterialRequirementCreateRejectsForeignParents(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "mr-create-a@example.com", "MR Create A")
	companyB := registerCompany(t, router, "mr-create-b@example.com", "MR Create B")

	projectA, workItemA, materialA := buildProcurementProject(t, router, companyA)
	projectB, workItemB, materialB := buildProcurementProject(t, router, companyB)

	// Each parent is isolated independently. Supplying all three foreign ids in
	// one request would exercise only the first Project check and leave the
	// WorkItem and Material boundaries unproven.
	for _, tc := range []struct {
		name, projectID, workItemID, materialID string
	}{
		{"project", projectA, workItemA, materialA},
		{"work item", projectB, workItemA, materialB},
		{"material", projectB, workItemB, materialA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, http.MethodPost, "/material-requirements",
				companyB.accessToken, map[string]any{
					"projectId": tc.projectID, "workItemId": tc.workItemID,
					"materialId":    tc.materialID,
					"quantityValue": "100", "quantityUnit": "bag",
				})
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for a foreign %s, got %d: %s",
					tc.name, resp.Code, resp.Body.String())
			}
		})
	}

	// None of the three rejected requests may have inserted into either tenant.
	list := doJSON(t, router, http.MethodGet,
		"/material-requirements?projectId="+projectA, companyA.accessToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("company A cannot list its own requirements: %d: %s",
			list.Code, list.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if requirements, _ := body["materialRequirements"].([]any); len(requirements) != 0 {
		t.Fatalf("foreign create inserted %d requirements into company A, want none",
			len(requirements))
	}
	list = doJSON(t, router, http.MethodGet,
		"/material-requirements?projectId="+projectB, companyB.accessToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("company B cannot list its own requirements: %d: %s",
			list.Code, list.Body.String())
	}
	body = nil
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if requirements, _ := body["materialRequirements"].([]any); len(requirements) != 0 {
		t.Fatalf("foreign create inserted %d requirements into company B, want none",
			len(requirements))
	}
}

// A foreign projectId yields 404, never an empty list: an empty list would
// confirm the project exists.
func TestTenantIsolation_MaterialRequirementListRejectsForeignProject(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "mr-list-a@example.com", "MR List A")
	companyB := registerCompany(t, router, "mr-list-b@example.com", "MR List B")

	projectID, workItemID, materialID := buildProcurementProject(t, router, companyA)
	createRequirement(t, router, companyA, projectID, workItemID, materialID)

	resp := doJSON(t, router, http.MethodGet,
		"/material-requirements?projectId="+projectID, companyB.accessToken, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a foreign project, got %d: %s", resp.Code, resp.Body.String())
	}
}

// Generation is project-scoped and tenant-scoped.
func TestTenantIsolation_MaterialRequirementGenerateRejectsForeignProject(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "mr-gen-a@example.com", "MR Gen A")
	companyB := registerCompany(t, router, "mr-gen-b@example.com", "MR Gen B")

	projectID, workItemID, materialID := buildProcurementProject(t, router, companyA)
	costItem := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken,
		map[string]any{
			"projectId": projectID, "workItemId": workItemID,
			"category": "material", "description": "Cement for E3 generation",
			"materialId": materialID, "quantityValue": "10", "quantityUnit": "bag",
			"unitPriceAmount": 1850, "currency": "MYR",
		})
	if costItem.Code != http.StatusOK {
		t.Fatalf("expected 200 creating generation source, got %d: %s",
			costItem.Code, costItem.Body.String())
	}

	resp := doJSON(t, router, http.MethodPost,
		"/projects/"+projectID+"/material-requirements/generate", companyB.accessToken, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 generating into a foreign project, got %d: %s",
			resp.Code, resp.Body.String())
	}

	// A real eligible source exists, so this proves the rejected call did not
	// partially generate an anchor behind the 404.
	list := doJSON(t, router, http.MethodGet,
		"/material-requirements?projectId="+projectID, companyA.accessToken, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("company A cannot list its requirements: %d: %s",
			list.Code, list.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(list.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if requirements, _ := body["materialRequirements"].([]any); len(requirements) != 0 {
		t.Fatalf("foreign generation created %d requirements, want none", len(requirements))
	}
}

// --- RFQs ---

func TestTenantIsolation_RFQCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-tenant-a@example.com", "RFQ Tenant A")
	companyB := registerCompany(t, router, "rfq-tenant-b@example.com", "RFQ Tenant B")

	projectID, _, _ := buildProcurementProject(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/rfqs",
		companyA.accessToken, map[string]any{
			"title": "Cement", "deliveryAddress": "12 Site Road",
		})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating rfq, got %d: %s", createResp.Code, createResp.Body.String())
	}
	rfqID := mustField(t, createResp, "id")

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"get", http.MethodGet, "/rfqs/" + rfqID, nil},
		{"patch", http.MethodPatch, "/rfqs/" + rfqID,
			map[string]any{"expectedRevision": 0, "title": "hijacked"}},
		{"ready", http.MethodPost, "/rfqs/" + rfqID + "/ready",
			map[string]any{"expectedRevision": 0}},
		{"reopen", http.MethodPost, "/rfqs/" + rfqID + "/reopen",
			map[string]any{"expectedRevision": 0}},
		{"delete", http.MethodDelete, "/rfqs/" + rfqID,
			map[string]any{"expectedRevision": 0}},
		{"claim-reconciliation", http.MethodGet, "/rfqs/" + rfqID + "/claim-reconciliation", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, tc.method, tc.path, companyB.accessToken, tc.body)
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}

	// Company A's RFQ survived every attempt.
	after := doJSON(t, router, http.MethodGet, "/rfqs/"+rfqID, companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost its own rfq: %d", after.Code)
	}
	if mustField(t, after, "title") != "Cement" {
		t.Errorf("title = %q, want Cement — a rejected cross-tenant call mutated the record",
			mustField(t, after, "title"))
	}
}

// Listing by a foreign Project must report 404. Returning an empty 200 would
// disclose that the Project identifier is valid in another tenant.
func TestTenantIsolation_RFQListRejectsForeignProject(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-list-a@example.com", "RFQ List A")
	companyB := registerCompany(t, router, "rfq-list-b@example.com", "RFQ List B")

	projectID, _, _ := buildProcurementProject(t, router, companyA)
	resp := doJSON(t, router, http.MethodGet, "/rfqs?projectId="+projectID,
		companyB.accessToken, nil)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 listing RFQs for a foreign Project, got %d: %s",
			resp.Code, resp.Body.String())
	}
}

// The line mutation and reconciliation routes all carry an RFQ id. A foreign
// caller must be stopped at the RFQ boundary before a requirement claim, line,
// or sort order can change.
func TestTenantIsolation_RFQLineMutationsRejectForeignRFQ(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-line-a@example.com", "RFQ Line A")
	companyB := registerCompany(t, router, "rfq-line-b@example.com", "RFQ Line B")

	projectID, workItemID, materialID := buildProcurementProject(t, router, companyA)
	rfqID, lineID, requirementID, revision := createRFQWithLine(
		t, router, companyA, projectID, workItemID, materialID)

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"add line", http.MethodPost, "/rfqs/" + rfqID + "/lines", map[string]any{
			"materialRequirementId": requirementID, "expectedRequirementRevision": 0,
			"expectedRfqRevision": revision,
		}},
		{"remove line", http.MethodDelete, "/rfqs/" + rfqID + "/lines/" + lineID,
			map[string]any{"expectedRevision": revision}},
		{"sort line", http.MethodPatch, "/rfqs/" + rfqID + "/lines/" + lineID + "/sort-order",
			map[string]any{"expectedRevision": revision, "sortOrder": 99}},
		{"reconcile claim", http.MethodPost, "/rfqs/" + rfqID + "/claim-reconciliation",
			map[string]any{"materialRequirementId": requirementID, "action": "release"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, tc.method, tc.path, companyB.accessToken, tc.body)
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for company B, got %d: %s",
					resp.Code, resp.Body.String())
			}
		})
	}

	// The owner's line remains present and unchanged after every rejected call.
	after := doJSON(t, router, http.MethodGet, "/rfqs/"+rfqID, companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost its own RFQ: %d: %s", after.Code, after.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(after.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	lines, _ := body["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("RFQ has %d lines after rejected foreign mutations, want 1", len(lines))
	}
	line, _ := lines[0].(map[string]any)
	if got, _ := line["id"].(string); got != lineID {
		t.Errorf("line id = %q, want %q", got, lineID)
	}
	if got, _ := line["sortOrder"].(float64); got != 0 {
		t.Errorf("sortOrder = %v, want 0 — a cross-tenant sort succeeded", line["sortOrder"])
	}
}

// RFQ numbering is COMPANY-SCOPED: each company's sequence starts at
// RFQ-000001, so a number never reveals another tenant's volume, and two
// companies may hold the same number (design spec §6.1, §12.2).
func TestTenantIsolation_RFQNumberingIsCompanyScoped(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-num-a@example.com", "RFQ Num A")
	companyB := registerCompany(t, router, "rfq-num-b@example.com", "RFQ Num B")

	projectA, _, _ := buildProcurementProject(t, router, companyA)
	projectB, _, _ := buildProcurementProject(t, router, companyB)

	// Company A allocates two numbers.
	var aNumbers []string
	for i := 0; i < 2; i++ {
		resp := doJSON(t, router, http.MethodPost, "/projects/"+projectA+"/rfqs",
			companyA.accessToken, map[string]any{"deliveryAddress": "12 Site Road"})
		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
		aNumbers = append(aNumbers, mustField(t, resp, "rfqNumber"))
	}
	if aNumbers[0] != "RFQ-000001" || aNumbers[1] != "RFQ-000002" {
		t.Fatalf("company A numbers = %v, want [RFQ-000001 RFQ-000002]", aNumbers)
	}

	// Company B's sequence is independent and starts at 1 — the same number,
	// legitimately held by a different tenant.
	respB := doJSON(t, router, http.MethodPost, "/projects/"+projectB+"/rfqs",
		companyB.accessToken, map[string]any{"deliveryAddress": "99 Other Road"})
	if respB.Code != http.StatusOK {
		t.Fatalf("expected 200 for company B, got %d: %s", respB.Code, respB.Body.String())
	}
	if got := mustField(t, respB, "rfqNumber"); got != "RFQ-000001" {
		t.Fatalf("company B first number = %s, want RFQ-000001 — the counter must be "+
			"company-scoped, or a number leaks another tenant's volume", got)
	}
}

// Creating an RFQ under another company's project is refused.
func TestTenantIsolation_RFQCreateRejectsForeignProject(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-proj-a@example.com", "RFQ Proj A")
	companyB := registerCompany(t, router, "rfq-proj-b@example.com", "RFQ Proj B")

	projectID, _, _ := buildProcurementProject(t, router, companyA)

	resp := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/rfqs",
		companyB.accessToken, map[string]any{"deliveryAddress": "hijack"})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
	}
}

// Acceptance case 100: even when the RFQ itself belongs to the caller, its
// MaterialRequirementSource capability must not resolve another company's
// valid requirement id.
func TestTenantIsolation_RFQCannotClaimForeignTenantRequirement(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "rfq-claim-a@example.com", "RFQ Claim A")
	companyB := registerCompany(t, router, "rfq-claim-b@example.com", "RFQ Claim B")

	projectA, workItemA, materialA := buildProcurementProject(t, router, companyA)
	requirementID, requirementRevision := reviewedRequirement(
		t, router, companyA, projectA, workItemA, materialA)

	projectB, _, _ := buildProcurementProject(t, router, companyB)
	rfqResp := doJSON(t, router, http.MethodPost, "/projects/"+projectB+"/rfqs",
		companyB.accessToken, map[string]any{"deliveryAddress": "99 Other Road"})
	if rfqResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating company B RFQ, got %d: %s",
			rfqResp.Code, rfqResp.Body.String())
	}

	addResp := doJSON(t, router, http.MethodPost,
		"/rfqs/"+mustField(t, rfqResp, "id")+"/lines", companyB.accessToken,
		map[string]any{
			"materialRequirementId":       requirementID,
			"expectedRequirementRevision": requirementRevision,
			"expectedRfqRevision":         mustNumberField(t, rfqResp, "revision"),
		})
	if addResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 claiming another tenant's requirement, got %d: %s",
			addResp.Code, addResp.Body.String())
	}

	// Company A's requirement is still unclaimed; the valid foreign id leaked
	// neither data nor a claim side effect.
	reqResp := doJSON(t, router, http.MethodGet,
		"/material-requirements/"+requirementID, companyA.accessToken, nil)
	if reqResp.Code != http.StatusOK {
		t.Fatalf("company A lost its requirement: %d: %s",
			reqResp.Code, reqResp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(reqResp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if chainID, present := body["activeRfqChainId"]; present && chainID != "" {
		t.Errorf("activeRfqChainId = %v, want absent after a rejected foreign claim", chainID)
	}
}

// The §7.2 CROSS-PROJECT claim filter, end to end: a requirement belonging to
// a different Project of the SAME company must not be claimable, and no claim
// may be created.
func TestTenantIsolation_RFQCannotClaimARequirementFromAnotherProject(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "rfq-xproj@example.com", "RFQ Cross Project")

	projectOne, workItemOne, materialOne := buildProcurementProject(t, router, company)
	requirementID, requirementRevision := reviewedRequirement(t, router, company,
		projectOne, workItemOne, materialOne)

	// A SECOND project in the same company.
	projectTwo, _, _ := buildProcurementProject(t, router, company)
	rfqResp := doJSON(t, router, http.MethodPost, "/projects/"+projectTwo+"/rfqs",
		company.accessToken, map[string]any{"deliveryAddress": "12 Site Road"})
	if rfqResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating rfq, got %d: %s", rfqResp.Code, rfqResp.Body.String())
	}
	rfqID := mustField(t, rfqResp, "id")

	addResp := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/lines",
		company.accessToken, map[string]any{
			"materialRequirementId":       requirementID,
			"expectedRequirementRevision": requirementRevision,
			"expectedRfqRevision":         mustNumberField(t, rfqResp, "revision"),
		})
	if addResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 claiming a requirement from another project, got %d: %s\n"+
			"projectId is enforced INSIDE the atomic claim filter, so cross-project "+
			"contamination must be structurally impossible (design spec §7.2)",
			addResp.Code, addResp.Body.String())
	}

	// No claim was created: the requirement is still unclaimed.
	reqResp := doJSON(t, router, http.MethodGet,
		"/material-requirements/"+requirementID, company.accessToken, nil)
	if reqResp.Code != http.StatusOK {
		t.Fatal("the requirement disappeared")
	}
	var body map[string]any
	if err := json.Unmarshal(reqResp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if chainID, present := body["activeRfqChainId"]; present && chainID != "" {
		t.Errorf("activeRfqChainId = %v, want absent — a rejected cross-project claim must "+
			"create NO claim", chainID)
	}
}

// The happy path proves the cross-project rejection above is not simply "no
// claim ever works": a SAME-project requirement claims successfully, and the
// line carries no internalNotes.
func TestTenantIsolation_RFQLineSnapshotExcludesInternalNotes(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "rfq-line@example.com", "RFQ Line Co")

	projectID, workItemID, materialID := buildProcurementProject(t, router, company)
	requirementID, requirementRevision := reviewedRequirement(t, router, company,
		projectID, workItemID, materialID)

	rfqResp := doJSON(t, router, http.MethodPost, "/projects/"+projectID+"/rfqs",
		company.accessToken, map[string]any{"deliveryAddress": "12 Site Road"})
	if rfqResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rfqResp.Code, rfqResp.Body.String())
	}
	rfqID := mustField(t, rfqResp, "id")

	addResp := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/lines",
		company.accessToken, map[string]any{
			"materialRequirementId":       requirementID,
			"expectedRequirementRevision": requirementRevision,
			"expectedRfqRevision":         mustNumberField(t, rfqResp, "revision"),
		})
	if addResp.Code != http.StatusOK {
		t.Fatalf("expected 200 adding a same-project line, got %d: %s",
			addResp.Code, addResp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(addResp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	lines, ok := body["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("expected exactly one line, got %+v", body["lines"])
	}
	line := lines[0].(map[string]any)

	// An RFQ line ASKS a supplier to quote; it never carries the contractor's
	// private note, cost or margin (design spec §6.2).
	for _, banned := range []string{
		"internalNotes", "cost", "margin", "unitPrice", "amount", "price",
		"snapshotFingerprint",
	} {
		if _, present := line[banned]; present {
			t.Errorf("rfq line exposes %q: %+v", banned, line)
		}
	}
	if line["procurementNotes"] != "deliver to site gate" {
		t.Errorf("procurementNotes = %v, want the supplier-visible note", line["procurementNotes"])
	}
}

// --- Suppliers ---

func TestTenantIsolation_SupplierCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "sup-tenant-a@example.com", "Sup Tenant A")
	companyB := registerCompany(t, router, "sup-tenant-b@example.com", "Sup Tenant B")

	created := createSupplier(t, router, companyA, "ABC Materials")
	supplierID := mustField(t, created, "id")

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"get", http.MethodGet, "/suppliers/" + supplierID, nil},
		{"patch", http.MethodPatch, "/suppliers/" + supplierID,
			map[string]any{"expectedRevision": 0, "contactPerson": "hijacked"}},
		{"set active", http.MethodPost, "/suppliers/" + supplierID + "/active",
			map[string]any{"expectedRevision": 0, "active": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, tc.method, tc.path, companyB.accessToken, tc.body)
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}

	after := doJSON(t, router, http.MethodGet, "/suppliers/"+supplierID, companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost its own supplier: %d", after.Code)
	}
	var afterBody map[string]any
	if err := json.Unmarshal(after.Body.Bytes(), &afterBody); err != nil {
		t.Fatalf("failed to parse supplier after rejected cross-tenant writes: %v", err)
	}
	if got, _ := afterBody["contactPerson"].(string); got != "Procurement Desk" {
		t.Errorf("contactPerson = %q, want Procurement Desk — a cross-tenant patch mutated "+
			"the supplier", got)
	}
	if active, ok := afterBody["active"].(bool); !ok || !active {
		t.Errorf("active = %v, want true — a cross-tenant deactivation succeeded",
			afterBody["active"])
	}
}

// A supplier listing never shows another company's directory.
func TestTenantIsolation_SupplierListIsCompanyScoped(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "sup-list-a@example.com", "Sup List A")
	companyB := registerCompany(t, router, "sup-list-b@example.com", "Sup List B")

	createSupplier(t, router, companyA, "ABC Materials")
	createSupplier(t, router, companyA, "XYZ Supplies")
	createSupplier(t, router, companyB, "Other Tenant Supplier")

	resp := doJSON(t, router, http.MethodGet, "/suppliers", companyB.accessToken, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	list, _ := body["suppliers"].([]any)
	if len(list) != 1 {
		t.Fatalf("company B sees %d suppliers, want only its own 1", len(list))
	}
	if name := list[0].(map[string]any)["name"]; name != "Other Tenant Supplier" {
		t.Errorf("company B sees %v", name)
	}
}

// The supplier name is unique PER COMPANY, so two tenants may hold the same
// name without colliding (design spec §4.1).
func TestTenantIsolation_SupplierNameUniquenessIsPerCompany(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "sup-uniq-a@example.com", "Sup Uniq A")
	companyB := registerCompany(t, router, "sup-uniq-b@example.com", "Sup Uniq B")

	createSupplier(t, router, companyA, "ABC Materials")

	// The same normalized name collides WITHIN company A...
	dup := doJSON(t, router, http.MethodPost, "/suppliers", companyA.accessToken,
		map[string]any{"name": "  abc   MATERIALS "})
	if dup.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a duplicate name in the same company, got %d: %s",
			dup.Code, dup.Body.String())
	}

	// ...but company B may use it freely.
	createSupplier(t, router, companyB, "ABC Materials")
}

// --- Offerings ---

func TestTenantIsolation_SupplierOfferingCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-tenant-a@example.com", "Off Tenant A")
	companyB := registerCompany(t, router, "off-tenant-b@example.com", "Off Tenant B")

	supplier := createSupplier(t, router, companyA, "ABC Materials")
	supplierID := mustField(t, supplier, "id")

	offeringResp := doJSON(t, router, http.MethodPost, "/supplier-offerings",
		companyA.accessToken, map[string]any{
			"supplierId": supplierID, "productName": "OPC Cement 50kg", "unit": "bag",
		})
	if offeringResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating offering, got %d: %s",
			offeringResp.Code, offeringResp.Body.String())
	}
	offeringID := mustField(t, offeringResp, "id")

	for _, tc := range []struct {
		name, method, path string
		body               any
	}{
		{"get", http.MethodGet, "/supplier-offerings/" + offeringID, nil},
		{"patch", http.MethodPatch, "/supplier-offerings/" + offeringID,
			map[string]any{"expectedRevision": 0, "productName": "hijacked"}},
		{"set active", http.MethodPost, "/supplier-offerings/" + offeringID + "/active",
			map[string]any{"expectedRevision": 0, "active": false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, tc.method, tc.path, companyB.accessToken, tc.body)
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
			}
		})
	}

	after := doJSON(t, router, http.MethodGet, "/supplier-offerings/"+offeringID,
		companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost its own offering: %d", after.Code)
	}
	if mustField(t, after, "productName") != "OPC Cement 50kg" {
		t.Error("a cross-tenant patch mutated the offering")
	}
}

// An offering may not reference a supplier belonging to another company.
func TestTenantIsolation_SupplierOfferingCannotReferenceAForeignSupplier(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-xref-a@example.com", "Off XRef A")
	companyB := registerCompany(t, router, "off-xref-b@example.com", "Off XRef B")

	supplier := createSupplier(t, router, companyA, "ABC Materials")
	supplierID := mustField(t, supplier, "id")

	resp := doJSON(t, router, http.MethodPost, "/supplier-offerings", companyB.accessToken,
		map[string]any{
			"supplierId": supplierID, "productName": "Hijacked", "unit": "bag",
		})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 referencing a foreign supplier, got %d: %s",
			resp.Code, resp.Body.String())
	}
}

// A linked Material is optional, but when supplied it is a parent reference
// and must be resolved under the authenticated company.
func TestTenantIsolation_SupplierOfferingCannotReferenceAForeignMaterial(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-mat-a@example.com", "Off Mat A")
	companyB := registerCompany(t, router, "off-mat-b@example.com", "Off Mat B")

	_, _, materialA := buildProcurementProject(t, router, companyA)
	supplierB := createSupplier(t, router, companyB, "Company B Supplier")

	resp := doJSON(t, router, http.MethodPost, "/supplier-offerings",
		companyB.accessToken, map[string]any{
			"supplierId": mustField(t, supplierB, "id"), "materialId": materialA,
			"productName": "Foreign-linked cement", "unit": "bag",
		})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 linking a foreign Material, got %d: %s",
			resp.Code, resp.Body.String())
	}
}

// The same Material ownership check applies when an existing offering gains
// or changes its optional catalog link.
func TestTenantIsolation_SupplierOfferingUpdateRejectsForeignMaterial(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-edit-mat-a@example.com", "Off Edit Mat A")
	companyB := registerCompany(t, router, "off-edit-mat-b@example.com", "Off Edit Mat B")

	_, _, materialA := buildProcurementProject(t, router, companyA)
	supplierB := createSupplier(t, router, companyB, "Company B Supplier")
	created := doJSON(t, router, http.MethodPost, "/supplier-offerings",
		companyB.accessToken, map[string]any{
			"supplierId":  mustField(t, supplierB, "id"),
			"productName": "Company B cement", "unit": "bag",
		})
	if created.Code != http.StatusOK {
		t.Fatalf("expected 200 creating company B offering, got %d: %s",
			created.Code, created.Body.String())
	}

	resp := doJSON(t, router, http.MethodPatch,
		"/supplier-offerings/"+mustField(t, created, "id"), companyB.accessToken,
		map[string]any{
			"expectedRevision": mustNumberField(t, created, "revision"),
			"materialId":       materialA,
		})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 linking a foreign Material during update, got %d: %s",
			resp.Code, resp.Body.String())
	}
}

func TestTenantIsolation_SupplierOfferingListIsCompanyScoped(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-list-a@example.com", "Off List A")
	companyB := registerCompany(t, router, "off-list-b@example.com", "Off List B")

	for _, company := range []testCompany{companyA, companyB} {
		supplier := createSupplier(t, router, company, "ABC Materials")
		resp := doJSON(t, router, http.MethodPost, "/supplier-offerings", company.accessToken,
			map[string]any{
				"supplierId":  mustField(t, supplier, "id"),
				"productName": "OPC Cement", "unit": "bag",
			})
		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.Code, resp.Body.String())
		}
	}

	resp := doJSON(t, router, http.MethodGet, "/supplier-offerings", companyA.accessToken, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	list, _ := body["offerings"].([]any)
	if len(list) != 1 {
		t.Fatalf("company A sees %d offerings, want only its own 1", len(list))
	}
}

// supplierId and materialId are parent filters, not opaque search strings.
// A foreign parent must return 404 rather than an empty 200, which would
// confirm that the guessed identifier exists in another tenant (§14.3).
func TestTenantIsolation_SupplierOfferingListRejectsForeignParentFilters(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "off-filter-a@example.com", "Off Filter A")
	companyB := registerCompany(t, router, "off-filter-b@example.com", "Off Filter B")

	_, _, materialA := buildProcurementProject(t, router, companyA)
	supplierA := createSupplier(t, router, companyA, "Company A Supplier")

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"supplier", "supplierId=" + mustField(t, supplierA, "id")},
		{"material", "materialId=" + materialA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, router, http.MethodGet,
				"/supplier-offerings?"+tc.query, companyB.accessToken, nil)
			if resp.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for a foreign parent filter, got %d: %s",
					resp.Code, resp.Body.String())
			}
		})
	}
}

// --- Preferred supplier ---

func TestTenantIsolation_PreferredSupplierCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "pref-tenant-a@example.com", "Pref Tenant A")
	companyB := registerCompany(t, router, "pref-tenant-b@example.com", "Pref Tenant B")

	_, _, materialID := buildProcurementProject(t, router, companyA)
	supplier := createSupplier(t, router, companyA, "ABC Materials")
	supplierID := mustField(t, supplier, "id")

	setResp := doJSON(t, router, http.MethodPut,
		"/materials/"+materialID+"/preferred-supplier", companyA.accessToken,
		map[string]any{"supplierId": supplierID, "expectedRevision": 0})
	if setResp.Code != http.StatusOK {
		t.Fatalf("expected 200 setting preference, got %d: %s", setResp.Code, setResp.Body.String())
	}

	t.Run("get", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodGet,
			"/materials/"+materialID+"/preferred-supplier", companyB.accessToken, nil)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("delete", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodDelete,
			"/materials/"+materialID+"/preferred-supplier", companyB.accessToken,
			map[string]any{"expectedRevision": 1})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	// Company A's preference survived.
	after := doJSON(t, router, http.MethodGet,
		"/materials/"+materialID+"/preferred-supplier", companyA.accessToken, nil)
	if after.Code != http.StatusOK {
		t.Fatalf("company A lost its own preference: %d", after.Code)
	}
	if mustField(t, after, "supplierId") != supplierID {
		t.Error("a cross-tenant call altered the preference")
	}
}

// A preference may not name a supplier from another company, and the material
// must belong to the caller.
func TestTenantIsolation_PreferredSupplierRejectsForeignReferences(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "pref-xref-a@example.com", "Pref XRef A")
	companyB := registerCompany(t, router, "pref-xref-b@example.com", "Pref XRef B")

	_, _, materialA := buildProcurementProject(t, router, companyA)
	supplierA := mustField(t, createSupplier(t, router, companyA, "ABC Materials"), "id")

	_, _, materialB := buildProcurementProject(t, router, companyB)
	supplierB := mustField(t, createSupplier(t, router, companyB, "Other Supplier"), "id")

	t.Run("foreign material", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPut,
			"/materials/"+materialA+"/preferred-supplier", companyB.accessToken,
			map[string]any{"supplierId": supplierB, "expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("foreign supplier", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPut,
			"/materials/"+materialB+"/preferred-supplier", companyB.accessToken,
			map[string]any{"supplierId": supplierA, "expectedRevision": 0})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", resp.Code, resp.Body.String())
		}
	})
}

// --- Authentication ---

// Every M7 route requires a bearer token. A route registered on the base api
// rather than the authenticated group would expose contractor procurement data
// outright, so this is checked per route rather than assumed from the wiring.
func TestTenantIsolation_AllMilestone7RoutesRequireAuth(t *testing.T) {
	router := setupRouter(t)

	routes := []struct{ method, path string }{
		{http.MethodPost, "/projects/p1/material-requirements/generate"},
		{http.MethodPost, "/material-requirements"},
		{http.MethodGet, "/material-requirements?projectId=p1"},
		{http.MethodGet, "/material-requirements/mr1"},
		{http.MethodPatch, "/material-requirements/mr1"},
		{http.MethodPost, "/material-requirements/mr1/review"},
		{http.MethodPost, "/material-requirements/mr1/acknowledge-unit"},
		{http.MethodPost, "/material-requirements/mr1/archive"},
		{http.MethodDelete, "/material-requirements/mr1"},
		{http.MethodGet, "/material-requirements/mr1/source-discrepancy"},
		{http.MethodPost, "/material-requirements/mr1/source-discrepancy/resolve"},
		{http.MethodPost, "/material-requirements/mr1/split"},
		{http.MethodPost, "/material-requirements/mr1/split/reconcile"},

		{http.MethodPost, "/projects/p1/rfqs"},
		{http.MethodGet, "/rfqs?projectId=p1"},
		{http.MethodGet, "/rfqs/r1"},
		{http.MethodPatch, "/rfqs/r1"},
		{http.MethodPost, "/rfqs/r1/lines"},
		{http.MethodDelete, "/rfqs/r1/lines/l1"},
		{http.MethodPatch, "/rfqs/r1/lines/l1/sort-order"},
		{http.MethodGet, "/rfqs/r1/claim-reconciliation"},
		{http.MethodPost, "/rfqs/r1/claim-reconciliation"},
		{http.MethodPost, "/rfqs/r1/ready"},
		{http.MethodPost, "/rfqs/r1/reopen"},
		{http.MethodDelete, "/rfqs/r1"},

		{http.MethodPost, "/suppliers"},
		{http.MethodGet, "/suppliers"},
		{http.MethodGet, "/suppliers/s1"},
		{http.MethodPatch, "/suppliers/s1"},
		{http.MethodPost, "/suppliers/s1/active"},
		{http.MethodPost, "/supplier-offerings"},
		{http.MethodGet, "/supplier-offerings"},
		{http.MethodGet, "/supplier-offerings/o1"},
		{http.MethodPatch, "/supplier-offerings/o1"},
		{http.MethodPost, "/supplier-offerings/o1/active"},
		{http.MethodGet, "/materials/m1/preferred-supplier"},
		{http.MethodPut, "/materials/m1/preferred-supplier"},
		{http.MethodDelete, "/materials/m1/preferred-supplier"},
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			// No access token at all.
			resp := doJSON(t, router, r.method, r.path, "", map[string]any{})
			if resp.Code != http.StatusUnauthorized {
				t.Errorf("expected 401 without a bearer token, got %d: %s",
					resp.Code, resp.Body.String())
			}
		})
	}

	if len(routes) != 38 {
		t.Errorf("checked %d routes; M7 declares 13 material-requirement, 12 rfq and 13 "+
			"supplier routes = 38", len(routes))
	}
}
