package awards_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/awards"
)

// F5 route contract (§8F). Finalising is where the owner/admin boundary
// begins: it is irreversible and externally visible. Reading award records
// stays open to the employee who prepared the draft.

const revisionBase = "/rfq-chains/rfqchain-1/issued-versions/issued-1/award-revisions"

// An employee may prepare a draft but may NOT finalise it. They can already see
// the resource, so the refusal is 403 — not the 404 a foreign tenant gets.
func TestFinaliseRouteRefusesAnEmployeeWith403(t *testing.T) {
	router, _ := draftRouter(t, "employee", "company-1")

	rec := do(t, router, http.MethodPost, revisionBase,
		map[string]any{"operationId": "op-1"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// A foreign tenant gets 404 even on a privileged route: a 403 would confirm the
// issued version exists in some other company.
func TestFinaliseRouteReturns404ForForeignTenant(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-2")

	rec := do(t, router, http.MethodPost, revisionBase,
		map[string]any{"operationId": "op-1"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// Award records are viewable by every company role, including employees.
func TestAwardRevisionReadRoutesAreOpenToAllRoles(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			router, _ := draftRouter(t, role, "company-1")

			// The chain must exist before it can be listed.
			if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusCreated {
				t.Fatalf("create draft status = %d, want 201", rec.Code)
			}
			rec := do(t, router, http.MethodGet, revisionBase, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("list status = %d, want 200; body=%s",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// A revision list for a foreign tenant is 404, never an empty 200: an empty
// list would confirm the chain exists somewhere.
func TestAwardRevisionListReturns404ForForeignTenant(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-2")

	rec := do(t, router, http.MethodGet, revisionBase, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// The three F5 routes must be mounted in this checkpoint (§8A.1A): deferring
// them would defer the authorization tests above.
func TestRegisterHandlersExposesTheAwardRevisionRoutes(t *testing.T) {
	spec := openAPISpec(t)

	want := map[string]bool{
		"awards-finalise":       false,
		"awards-list-revisions": false,
		"awards-get-revision":   false,
	}
	for _, methods := range spec.Paths {
		for _, operation := range methods {
			if _, tracked := want[operation.OperationID]; tracked {
				want[operation.OperationID] = true
			}
		}
	}
	for operationID, present := range want {
		if !present {
			t.Errorf("OpenAPI operation %q is missing", operationID)
		}
	}
}

// openAPISpec renders the observable route contract once for route assertions.
func openAPISpec(t *testing.T) struct {
	Paths map[string]map[string]struct {
		OperationID string `json:"operationId"`
	} `json:"paths"`
} {
	t.Helper()

	router, _ := draftRouter(t, "owner", "company-1")
	rec := do(t, router, http.MethodGet, "/openapi.json", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d, want 200", rec.Code)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}
	return spec
}

var _ = awards.ErrAwardRevisionNotFound
