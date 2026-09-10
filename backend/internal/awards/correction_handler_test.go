package awards_test

import (
	"net/http"
	"testing"
)

// F6 route contract (§8G). Correcting is owner/admin only: it publishes a new
// authoritative revision that a Supplier may be told about.

const correctionBase = revisionBase + "/corrections"

func correctionBody(reason string) map[string]any {
	return map[string]any{
		"operationId":  "op-2",
		"changeReason": reason,
		"decisions": []map[string]any{{
			"issuedRfqLineId": "line-1",
			"stableLineageId": "lineage-1",
			"decision":        "selected",
			"offerVersionId":  "offer-1",
			"offerLineId":     "ol-1",
		}},
	}
}

// An employee may prepare drafts but may not correct a published award.
func TestCorrectionRouteRefusesAnEmployeeWith403(t *testing.T) {
	router, _ := draftRouter(t, "employee", "company-1")

	rec := do(t, router, http.MethodPost, correctionBase,
		correctionBody("corrected quantity"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// A foreign tenant gets 404, never 403.
func TestCorrectionRouteReturns404ForForeignTenant(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-2")

	rec := do(t, router, http.MethodPost, correctionBase,
		correctionBody("corrected quantity"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// The change reason is required at the schema boundary, so an unexplained
// correction is refused before it reaches the service.
func TestCorrectionRouteRequiresAChangeReason(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-1")

	rec := do(t, router, http.MethodPost, correctionBase, correctionBody(""))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

// The correction route must be mounted in this checkpoint (§8A.1A).
func TestRegisterHandlersExposesTheCorrectionRoute(t *testing.T) {
	spec := openAPISpec(t)

	found := false
	for path, methods := range spec.Paths {
		for _, operation := range methods {
			if operation.OperationID == "awards-correct" {
				found = true
				want := "/rfq-chains/{rfqChainId}/issued-versions/{versionId}" +
					"/award-revisions/corrections"
				if path != want {
					t.Errorf("correction path = %q, want %q", path, want)
				}
			}
		}
	}
	if !found {
		t.Error("OpenAPI operation \"awards-correct\" is missing")
	}
}
