package awards_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F7 route contract (§8H). The contractor outcome list is a read of an award
// record, so every company role may view it. The SUPPLIER outcome route is not
// mounted here: §8A.1 places it in supplieraccess, which consumes awards
// through the SupplierOutcomeSource capability.

const outcomeBase = revisionBase + "/revision-1/outcomes"

// Award records are viewable by every company role.
func TestOutcomeListRouteIsOpenToAllRoles(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			router, store := draftRouter(t, role, "company-1")
			seedOutcome(t, store)

			rec := do(t, router, http.MethodGet, outcomeBase, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// A foreign tenant gets 404, never an empty 200: an empty list would confirm
// the revision exists somewhere.
func TestOutcomeListRouteReturns404ForForeignTenant(t *testing.T) {
	router, store := draftRouter(t, "owner", "company-2")
	seedOutcome(t, store)

	rec := do(t, router, http.MethodGet, outcomeBase, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// The wire shape must not carry competitor data or unawarded reasons, even
// though the contractor is entitled to see them elsewhere: this DTO is the same
// one a Supplier-facing renderer consumes, so it stays privacy-safe by
// construction (§8H).
func TestOutcomeResponseCarriesNoCompetitorDataOrUnawardedReasons(t *testing.T) {
	router, store := draftRouter(t, "owner", "company-1")
	seedOutcome(t, store)

	rec := do(t, router, http.MethodGet, outcomeBase, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	for _, forbidden := range []string{
		"respondentCount", "unawardedReason", "unawardedLines",
		"grandAwardTotal", "supplierSummaries", "rank", "recommended",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("outcome response exposes %q", forbidden)
		}
	}

	var decoded struct {
		Outcomes []struct {
			Result     string `json:"result"`
			Projection struct {
				AwardTotal struct {
					Amount int64 `json:"amount"`
				} `json:"awardTotal"`
				AwardedLines []struct {
					IssuedRFQLineID string `json:"issuedRfqLineId"`
				} `json:"awardedLines"`
			} `json:"projection"`
		} `json:"outcomes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode outcomes: %v; body=%s", err, body)
	}
	if len(decoded.Outcomes) != 1 {
		t.Fatalf("outcomes = %d, want 1", len(decoded.Outcomes))
	}
	if decoded.Outcomes[0].Projection.AwardTotal.Amount != 13_600 {
		t.Errorf("award total = %d, want the Supplier's OWN 13600",
			decoded.Outcomes[0].Projection.AwardTotal.Amount)
	}
}

// The outcome list route must be mounted in this checkpoint (§8A.1A).
func TestRegisterHandlersExposesTheOutcomeListRoute(t *testing.T) {
	spec := openAPISpec(t)

	found := false
	for path, methods := range spec.Paths {
		for _, operation := range methods {
			if operation.OperationID == "awards-list-outcomes" {
				found = true
				want := "/rfq-chains/{rfqChainId}/issued-versions/{versionId}" +
					"/award-revisions/{revisionId}/outcomes"
				if path != want {
					t.Errorf("outcome path = %q, want %q", path, want)
				}
			}
		}
	}
	if !found {
		t.Error("OpenAPI operation \"awards-list-outcomes\" is missing")
	}
}

// The Supplier outcome route is deliberately NOT mounted by this module: it
// belongs to the Phase D session boundary in supplieraccess (§8A.1). Mounting
// it here would put Supplier session handling in the wrong module.
func TestAwardsDoesNotMountTheSupplierOutcomeRoute(t *testing.T) {
	spec := openAPISpec(t)

	for path := range spec.Paths {
		if strings.HasPrefix(path, "/supplier-access") {
			t.Errorf("awards mounts %q; Supplier routes belong to supplieraccess",
				path)
		}
	}
}

// The exported Supplier view carries the frozen projection and nothing that
// identifies the Supplier back to themselves.
func TestSupplierOutcomeViewNarrowsTheStoredOutcome(t *testing.T) {
	view := awards.ToSupplierOutcomeView(awards.AwardOutcome{
		ID: "outcome-1", SupplierID: "supplier-a",
		InvitationID: "invitation-a", Result: awards.OutcomeSelected,
	})
	if view.ID != "outcome-1" || view.Result != "selected" {
		t.Fatalf("view = %+v, want the outcome identity and result", view)
	}
}
