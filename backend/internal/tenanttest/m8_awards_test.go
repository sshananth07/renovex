package tenanttest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// Composed Phase F acceptance (M8 §8K).
//
// This exercises the award surface through the REAL authenticated router and
// real MongoDB, with awards reaching rfqissuance and supplieroffers only
// through the composition adapters — the same wiring cmd/api uses.
//
// It deliberately does not re-prove what the awards package already proves in
// isolation. What it proves is that the composed whole is reachable, correctly
// authorized, and tenant-isolated.

// issueRFQForAwards drives the M7 → M8 issuance journey and returns the RFQ
// chain and issued version an award is made against.
func issueRFQForAwards(
	t *testing.T,
	router http.Handler,
	owner testCompany,
	suffix string,
) (rfqChainID string, issuedVersionID string) {
	t.Helper()

	projectID, workItemID, materialID := buildProcurementProject(t, router, owner)
	rfqID, _, _, revision := createRFQWithLine(
		t, router, owner, projectID, workItemID, materialID)

	deadline := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	patched := doJSON(t, router, http.MethodPatch, "/rfqs/"+rfqID,
		owner.accessToken, map[string]any{
			"expectedRevision":     revision,
			"title":                "Award acceptance " + suffix,
			"requiredByDate":       deadline.Add(7 * 24 * time.Hour).Format(time.RFC3339),
			"responseDeadline":     deadline.Format(time.RFC3339),
			"supplierInstructions": "Quote delivered price",
		})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch RFQ: %d: %s", patched.Code, patched.Body.String())
	}
	ready := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/ready",
		owner.accessToken, map[string]any{
			"expectedRevision": mustNumberField(t, patched, "revision"),
		})
	if ready.Code != http.StatusOK {
		t.Fatalf("ready RFQ: %d: %s", ready.Code, ready.Body.String())
	}

	issued := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqID+"/issue", owner.accessToken,
		map[string]any{"currency": "MYR", "operationId": "award-issue-" + suffix})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue Version 1: %d: %s", issued.Code, issued.Body.String())
	}
	return rfqID, mustField(t, issued, "id")
}

func awardBase(rfqChainID, versionID string) string {
	return "/rfq-chains/" + rfqChainID + "/issued-versions/" + versionID
}

// The composed award surface is reachable, and the role boundary holds exactly
// where §8C places it: employees prepare drafts, owner/admin finalise.
func TestM8Awards_ComposedSurfaceRolesAndTenantIsolation(t *testing.T) {
	router := setupRouter(t)
	owner := registerCompany(t, router, "m8f-owner@example.com", "M8F Owner")
	foreign := registerCompany(t, router, "m8f-foreign@example.com", "M8F Foreign")
	employeeToken := tokenWithRole(t, owner.accessToken, identity.RoleEmployee)
	adminToken := tokenWithRole(t, owner.accessToken, identity.RoleAdmin)

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "roles")
	base := awardBase(rfqChainID, versionID)

	// --- Comparison: preparation, so every company role may read it (§8B) ---
	for role, token := range map[string]string{
		"owner":    owner.accessToken,
		"admin":    adminToken,
		"employee": employeeToken,
	} {
		comparison := doJSON(t, router, http.MethodGet,
			base+"/comparison", token, nil)
		if comparison.Code != http.StatusOK {
			t.Fatalf("%s comparison status = %d, want 200: %s",
				role, comparison.Code, comparison.Body.String())
		}
		body := decodeObject(t, comparison.Body.Bytes())
		// M8 ranks nothing and recommends no winner.
		if authoritative, ok := body["authoritative"].(bool); !ok || authoritative {
			t.Errorf("%s comparison claims to be authoritative", role)
		}
	}

	// A foreign tenant must not learn the issued version exists.
	foreignComparison := doJSON(t, router, http.MethodGet,
		base+"/comparison", foreign.accessToken, nil)
	if foreignComparison.Code != http.StatusNotFound {
		t.Fatalf("foreign comparison status = %d, want 404: %s",
			foreignComparison.Code, foreignComparison.Body.String())
	}

	// --- Draft: employees may prepare (Decision A §1A.1) ---
	created := doJSON(t, router, http.MethodPost,
		base+"/award-draft", employeeToken, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("employee create draft status = %d, want 201: %s",
			created.Code, created.Body.String())
	}
	draftRevision := mustNumberField(t, created, "revision")

	read := doJSON(t, router, http.MethodGet, base+"/award-draft",
		employeeToken, nil)
	if read.Code != http.StatusOK {
		t.Fatalf("employee read draft status = %d, want 200: %s",
			read.Code, read.Body.String())
	}

	// A foreign tenant sees no draft, even though one exists in another company.
	foreignDraft := doJSON(t, router, http.MethodGet, base+"/award-draft",
		foreign.accessToken, nil)
	if foreignDraft.Code != http.StatusNotFound {
		t.Fatalf("foreign draft status = %d, want 404: %s",
			foreignDraft.Code, foreignDraft.Body.String())
	}

	// --- Finalisation: owner/admin ONLY, because it is irreversible and
	// externally visible (§8F) ---
	employeeFinalise := doJSON(t, router, http.MethodPost,
		base+"/award-revisions", employeeToken,
		map[string]any{"operationId": "award-finalise-employee"})
	if employeeFinalise.Code != http.StatusForbidden {
		t.Fatalf("employee finalise status = %d, want 403: %s",
			employeeFinalise.Code, employeeFinalise.Body.String())
	}

	// A foreign tenant gets 404 even on a privileged route: 403 would confirm
	// the resource exists.
	foreignFinalise := doJSON(t, router, http.MethodPost,
		base+"/award-revisions", foreign.accessToken,
		map[string]any{"operationId": "award-finalise-foreign"})
	if foreignFinalise.Code != http.StatusNotFound {
		t.Fatalf("foreign finalise status = %d, want 404: %s",
			foreignFinalise.Code, foreignFinalise.Body.String())
	}

	// Reconciliation and correction are likewise owner/admin only.
	employeeReconcile := doJSON(t, router, http.MethodPost,
		base+"/award-reconciliation", employeeToken,
		map[string]any{"operationId": "award-reconcile-employee"})
	if employeeReconcile.Code != http.StatusForbidden {
		t.Fatalf("employee reconcile status = %d, want 403: %s",
			employeeReconcile.Code, employeeReconcile.Body.String())
	}
	employeeCorrect := doJSON(t, router, http.MethodPost,
		base+"/award-revisions/corrections", employeeToken,
		map[string]any{
			"operationId": "award-correct-employee", "changeReason": "test",
			"decisions": []map[string]any{},
		})
	if employeeCorrect.Code != http.StatusForbidden {
		t.Fatalf("employee correct status = %d, want 403: %s",
			employeeCorrect.Code, employeeCorrect.Body.String())
	}

	// --- Award records are viewable by every role (§8F) ---
	for role, token := range map[string]string{
		"owner":    owner.accessToken,
		"admin":    adminToken,
		"employee": employeeToken,
	} {
		list := doJSON(t, router, http.MethodGet,
			base+"/award-revisions", token, nil)
		if list.Code != http.StatusOK {
			t.Fatalf("%s revision list status = %d, want 200: %s",
				role, list.Code, list.Body.String())
		}
	}

	// Discard closes the loop: the employee who opened the draft may discard it.
	discard := doJSON(t, router, http.MethodDelete,
		base+"/award-draft?expectedRevision="+itoa(int(draftRevision)),
		employeeToken, nil)
	if discard.Code != http.StatusNoContent {
		t.Fatalf("employee discard status = %d, want 204: %s",
			discard.Code, discard.Body.String())
	}
}

// Finalising with an incomplete draft is refused at the composed boundary, so
// an award can never be published with lines left undecided (§8C).
func TestM8Awards_ComposedFinalisationRequiresACompleteDraft(t *testing.T) {
	router := setupRouter(t)
	owner := registerCompany(t, router, "m8f-incomplete@example.com", "M8F Inc")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "incomplete")
	base := awardBase(rfqChainID, versionID)

	if created := doJSON(t, router, http.MethodPost,
		base+"/award-draft", owner.accessToken, nil); created.Code != http.StatusCreated {
		t.Fatalf("create draft: %d: %s", created.Code, created.Body.String())
	}

	// No Supplier has quoted and no line is decided, so finalisation must fail
	// on completeness rather than publishing an empty award.
	finalised := doJSON(t, router, http.MethodPost,
		base+"/award-revisions", owner.accessToken,
		map[string]any{"operationId": "award-finalise-incomplete"})
	if finalised.Code != http.StatusUnprocessableEntity {
		t.Fatalf("incomplete finalise status = %d, want 422: %s",
			finalised.Code, finalised.Body.String())
	}
}

// The Supplier outcome routes are mounted by supplieraccess behind the Phase D
// session, NOT by awards (§8A.1, D3). Without a session they are refused, and
// they never appear under a contractor-authenticated path.
func TestM8Awards_SupplierOutcomeRoutesRequireAPhaseDSession(t *testing.T) {
	router := setupRouter(t)

	// No supplier_session cookie: the read must not succeed.
	outcome := doJSON(t, router, http.MethodGet,
		"/supplier-access/outcomes/outcome-1?invitationId=invitation-1", "", nil)
	if outcome.Code == http.StatusOK {
		t.Fatalf("outcome read succeeded without a Phase D session: %s",
			outcome.Body.String())
	}

	// Acknowledgement additionally requires CSRF, so it must also be refused.
	acknowledged := doJSON(t, router, http.MethodPost,
		"/supplier-access/outcomes/outcome-1/acknowledgements", "",
		map[string]any{
			"invitationId": "invitation-1", "operationId": "ack-1",
		})
	if acknowledged.Code == http.StatusOK ||
		acknowledged.Code == http.StatusCreated {
		t.Fatalf("acknowledgement succeeded without a session: %s",
			acknowledged.Body.String())
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
