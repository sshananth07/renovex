package tenanttest_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const tenantTestJWTSecret = "tenanttest-fixed-secret-do-not-use-in-production"

func tokenWithRole(t *testing.T, ownerToken string, role identity.Role) string {
	t.Helper()

	// The tenant-test router intentionally uses a fixed secret. Re-signing the
	// owner's authenticated company identity lets this acceptance test exercise
	// route authorization without adding fake production membership endpoints.
	issuer := identity.NewJWTIssuer([]byte(tenantTestJWTSecret), time.Hour)
	claims, err := issuer.VerifyAccessToken(ownerToken)
	if err != nil {
		t.Fatalf("verify owner access token: %v", err)
	}
	token, err := issuer.IssueAccessToken(
		"m8-employee-user", claims.CompanyID, string(role))
	if err != nil {
		t.Fatalf("issue %s access token: %v", role, err)
	}
	return token
}

func decodeObject(t *testing.T, responseBody []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(responseBody, &body); err != nil {
		t.Fatalf("decode response %s: %v", responseBody, err)
	}
	return body
}

// Phase B's composed proof crosses the actual M7/M8 seam. It creates and readies
// an M7 RFQ, publishes immutable M8 versions, and then asks M7 whether reopening
// is permitted. Unit tests cannot prove that both composition adapters and the
// authenticated route middleware are wired together.
func TestM8RFQIssuance_ComposedLifecycleRolesAndTenantIsolation(t *testing.T) {
	router := setupRouter(t)
	owner := registerCompany(t, router, "m8-owner@example.com", "M8 Owner")
	foreign := registerCompany(t, router, "m8-foreign@example.com", "M8 Foreign")
	employeeToken := tokenWithRole(t, owner.accessToken, identity.RoleEmployee)
	adminToken := tokenWithRole(t, owner.accessToken, identity.RoleAdmin)

	projectID, workItemID, materialID := buildProcurementProject(t, router, owner)
	rfqID, m7LineID, requirementID, revision := createRFQWithLine(
		t, router, owner, projectID, workItemID, materialID)

	deadline := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	requiredBy := deadline.Add(7 * 24 * time.Hour)
	patched := doJSON(t, router, http.MethodPatch, "/rfqs/"+rfqID,
		owner.accessToken, map[string]any{
			"expectedRevision":     revision,
			"title":                "External cement procurement",
			"requiredByDate":       requiredBy.Format(time.RFC3339),
			"responseDeadline":     deadline.Format(time.RFC3339),
			"supplierInstructions": "Quote delivered price",
			// This field must stay on M7 and never enter an issued projection.
			"internalNotes": "target margin and internal negotiation position",
		})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch M7 RFQ: got %d: %s", patched.Code, patched.Body.String())
	}
	ready := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/ready",
		owner.accessToken, map[string]any{
			"expectedRevision": mustNumberField(t, patched, "revision"),
		})
	if ready.Code != http.StatusOK {
		t.Fatalf("ready M7 RFQ: got %d: %s", ready.Code, ready.Body.String())
	}
	readyRevision := mustNumberField(t, ready, "revision")

	// Publishing and reconciliation are irreversible/external transitions.
	employeeIssue := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/issue", employeeToken,
		map[string]any{"currency": "MYR", "operationId": "m8-employee-issue"})
	if employeeIssue.Code != http.StatusForbidden {
		t.Fatalf("employee issue status = %d, want 403: %s",
			employeeIssue.Code, employeeIssue.Body.String())
	}
	employeeReconcile := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/reconcile", employeeToken, nil)
	if employeeReconcile.Code != http.StatusForbidden {
		t.Fatalf("employee reconcile status = %d, want 403: %s",
			employeeReconcile.Code, employeeReconcile.Body.String())
	}

	unsupported := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/issue", owner.accessToken,
		map[string]any{"currency": "USD", "operationId": "m8-invalid-currency"})
	if unsupported.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported currency status = %d, want 422: %s",
			unsupported.Code, unsupported.Body.String())
	}

	issued := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/issue", owner.accessToken,
		map[string]any{"currency": "myr", "operationId": "m8-issue-v1"})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue Version 1: got %d: %s", issued.Code, issued.Body.String())
	}
	versionID := mustField(t, issued, "id")
	issuedBody := decodeObject(t, issued.Body.Bytes())
	if issuedBody["versionNumber"] != float64(1) || issuedBody["currency"] != "MYR" {
		t.Errorf("issued identity = version %v currency %v, want Version 1 MYR",
			issuedBody["versionNumber"], issuedBody["currency"])
	}
	for _, privateField := range []string{
		"internalNotes", "sourceFingerprint", "sourceM7RfqRevision",
	} {
		if _, exposed := issuedBody[privateField]; exposed {
			t.Errorf("issued HTTP projection exposed private/internal field %q", privateField)
		}
	}

	// HTTP boundary proof for invitations: a member of the valid tenant with an
	// insufficient role gets 403, while a privileged caller from another
	// tenant gets the repository's collapsed 404. Checking these together
	// prevents authorization changes from turning tenant isolation into an
	// existence oracle.
	supplier := createSupplier(t, router, owner, "M8 Invitation Supplier")
	invitation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/invitations", owner.accessToken,
		map[string]any{
			"supplierId":     mustField(t, supplier, "id"),
			"recipientName":  "Supplier Procurement",
			"recipientEmail": "quotes@m8-supplier.example",
			"expiresAt":      deadline.Add(14 * 24 * time.Hour).Format(time.RFC3339),
		})
	if invitation.Code != http.StatusCreated {
		t.Fatalf("create invitation: got %d: %s",
			invitation.Code, invitation.Body.String())
	}
	invitationID := mustField(t, invitation, "id")
	reactivationBody := map[string]any{
		"expectedRevision": mustNumberField(t, invitation, "revision"),
		"expiresAt":        deadline.Add(30 * 24 * time.Hour).Format(time.RFC3339),
		"operationId":      "m8-reactivate-http-boundary",
	}

	employeeReactivation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/invitations/"+invitationID+"/reactivate",
		employeeToken, reactivationBody)
	if employeeReactivation.Code != http.StatusForbidden {
		t.Fatalf("valid-tenant employee reactivation status = %d, want 403: %s",
			employeeReactivation.Code, employeeReactivation.Body.String())
	}

	foreignReactivation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/invitations/"+invitationID+"/reactivate",
		foreign.accessToken, reactivationBody)
	if foreignReactivation.Code != http.StatusNotFound {
		t.Fatalf("foreign-tenant reactivation status = %d, want 404: %s",
			foreignReactivation.Code, foreignReactivation.Body.String())
	}

	lines, ok := issuedBody["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("issued lines = %#v, want one immutable line", issuedBody["lines"])
	}
	line := lines[0].(map[string]any)
	if line["id"] == m7LineID ||
		line["sourceM7RfqLineId"] != m7LineID ||
		line["sourceMaterialRequirementId"] != requirementID {
		t.Errorf("issued-line identity/provenance = %#v", line)
	}

	secondInitial := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/issue", owner.accessToken,
		map[string]any{"currency": "MYR", "operationId": "m8-second-initial"})
	if secondInitial.Code != http.StatusConflict {
		t.Fatalf("second initial issue status = %d, want 409: %s",
			secondInitial.Code, secondInitial.Body.String())
	}

	adminReconcile := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/reconcile", adminToken, nil)
	if adminReconcile.Code != http.StatusOK {
		t.Fatalf("admin reconcile status = %d, want 200: %s",
			adminReconcile.Code, adminReconcile.Body.String())
	}

	for _, access := range []struct {
		name, token string
	}{
		{"owner", owner.accessToken},
		{"admin", adminToken},
		{"employee", employeeToken},
	} {
		t.Run(access.name+" can read", func(t *testing.T) {
			history := doJSON(t, router, http.MethodGet,
				"/rfq-chains/"+rfqID+"/versions", access.token, nil)
			if history.Code != http.StatusOK {
				t.Fatalf("list status = %d: %s", history.Code, history.Body.String())
			}
			version := doJSON(t, router, http.MethodGet,
				"/rfq-versions/"+versionID, access.token, nil)
			if version.Code != http.StatusOK {
				t.Fatalf("get status = %d: %s", version.Code, version.Body.String())
			}
		})
	}

	foreignGet := doJSON(t, router, http.MethodGet,
		"/rfq-versions/"+versionID, foreign.accessToken, nil)
	if foreignGet.Code != http.StatusNotFound {
		t.Fatalf("foreign get status = %d, want 404: %s",
			foreignGet.Code, foreignGet.Body.String())
	}
	foreignList := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqID+"/versions", foreign.accessToken, nil)
	if foreignList.Code != http.StatusNotFound {
		t.Fatalf("foreign list status = %d, want 404: %s",
			foreignList.Code, foreignList.Body.String())
	}

	draft := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/amendment-draft", employeeToken, nil)
	if draft.Code != http.StatusCreated {
		t.Fatalf("employee create amendment: got %d: %s", draft.Code, draft.Body.String())
	}
	draftBody := decodeObject(t, draft.Body.Bytes())
	draftLines := draftBody["lines"].([]any)
	existingDraftLine := draftLines[0].(map[string]any)
	amendedDeadline := deadline.Add(7 * 24 * time.Hour)
	amendedRequiredBy := requiredBy.Add(7 * 24 * time.Hour)
	updatedDraft := doJSON(t, router, http.MethodPatch,
		"/rfq-chains/"+rfqID+"/amendment-draft", employeeToken,
		map[string]any{
			"expectedRevision": draftBody["revision"],
			"requiredByDate":   amendedRequiredBy.Format(time.RFC3339),
			"responseDeadline": amendedDeadline.Format(time.RFC3339),
			"lines": []map[string]any{
				{
					"id": existingDraftLine["id"], "materialId": materialID,
					"materialName": "Portland Cement", "specification": "Low carbon OPC",
					"quantity":  map[string]any{"value": "125.5", "unit": "bag"},
					"sortOrder": 1,
				},
				{
					"materialId": materialID, "materialName": "Additional Cement",
					"quantity":  map[string]any{"value": "25", "unit": "bag"},
					"sortOrder": 2,
				},
			},
		})
	if updatedDraft.Code != http.StatusOK {
		t.Fatalf("employee patch amendment: got %d: %s",
			updatedDraft.Code, updatedDraft.Body.String())
	}
	updatedDraftBody := decodeObject(t, updatedDraft.Body.Bytes())
	if got := len(updatedDraftBody["lines"].([]any)); got != 2 {
		t.Fatalf("updated draft has %d lines, want 2", got)
	}

	employeeAmend := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/amendment-draft/issue", employeeToken,
		map[string]any{
			"expectedRevision": updatedDraftBody["revision"],
			"operationId":      "m8-employee-amend",
		})
	if employeeAmend.Code != http.StatusForbidden {
		t.Fatalf("employee amendment issue status = %d, want 403: %s",
			employeeAmend.Code, employeeAmend.Body.String())
	}

	amended := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/amendment-draft/issue", adminToken,
		map[string]any{
			"expectedRevision": updatedDraftBody["revision"],
			"operationId":      "m8-issue-v2",
		})
	if amended.Code != http.StatusCreated {
		t.Fatalf("admin amendment issue: got %d: %s", amended.Code, amended.Body.String())
	}
	amendedBody := decodeObject(t, amended.Body.Bytes())
	if amendedBody["versionNumber"] != float64(2) ||
		len(amendedBody["lines"].([]any)) != 2 {
		t.Errorf("amended version = %#v, want Version 2 with two lines", amendedBody)
	}

	// The real cycle-closing adapter must now prevent M7 from returning the RFQ
	// to a mutable state while external immutable versions exist.
	reopen := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/reopen",
		owner.accessToken, map[string]any{"expectedRevision": readyRevision})
	if reopen.Code != http.StatusConflict {
		t.Fatalf("reopen issued RFQ status = %d, want 409: %s",
			reopen.Code, reopen.Body.String())
	}
}
