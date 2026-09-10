package awards_test

import (
	"net/http"
	"strings"
	"testing"
)

// F8 route contract (§8I). Owner and admin only: sending is externally visible
// and cannot be unsent.

const notificationBase = revisionBase + "/revision-1"

func sendBody() map[string]any {
	return map[string]any{
		"operationId":       "op-send-1",
		"recipientIdentity": "buyer@example.test",
		"accessGeneration":  1,
	}
}

// An employee may prepare drafts but may not tell a Supplier anything.
func TestNotificationRoutesRefuseAnEmployeeWith403(t *testing.T) {
	router, store := draftRouter(t, "employee", "company-1")
	seedOutcome(t, store)

	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]any
	}{
		{"send", http.MethodPost,
			notificationBase + "/outcomes/outcome-1/notifications", sendBody()},
		{"retry", http.MethodPost,
			revisionBase[:strings.LastIndex(revisionBase, "/award-revisions")] +
				"/notifications/delivery-1/retry", sendBody()},
		{"list", http.MethodGet, notificationBase + "/notifications", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, router, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s",
					rec.Code, rec.Body.String())
			}
		})
	}
}

// A foreign tenant gets 404, never 403.
func TestNotificationSendReturns404ForForeignTenant(t *testing.T) {
	router, store := draftRouter(t, "owner", "company-2")
	seedOutcome(t, store)

	rec := do(t, router, http.MethodPost,
		notificationBase+"/outcomes/outcome-1/notifications", sendBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// A send returns 202, not 200: the transport has been asked, which is not the
// same as the Supplier having received it (§8I).
func TestNotificationSendReturns202(t *testing.T) {
	router, store := draftRouter(t, "owner", "company-1")
	seedOutcome(t, store)

	rec := do(t, router, http.MethodPost,
		notificationBase+"/outcomes/outcome-1/notifications", sendBody())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "delivered") {
		t.Error("the response must not claim delivery; `sent` is the ceiling")
	}
}

// The three notification routes must be mounted in this checkpoint (§8A.1A).
func TestRegisterHandlersExposesTheNotificationRoutes(t *testing.T) {
	spec := openAPISpec(t)

	want := map[string]bool{
		"awards-send-notification":  false,
		"awards-retry-notification": false,
		"awards-list-notifications": false,
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
