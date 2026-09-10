package supplieroffers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// H1 acceptance defect coverage.
//
// SetOfferValidity has existed since Phase E, but no route exposed it, so the
// approved Supplier submission journey was impossible through production HTTP:
// submission hard-requires OfferValidUntil and nothing could set it.
//
// These cases pin the behaviour the ROUTE itself owns. Session validity, the
// invitation binding and CSRF are delegated to the Phase D capability and are
// proven there; what is new here is the date contract and the fact that an
// invalid date never reaches the service or the draft.

// recordingOfferAccess authorizes every request and records that it was asked,
// so a test can prove a rejected date never reached authorization or storage.
type recordingOfferAccess struct {
	calls int
	// seen records the authorization inputs, so a test can prove the request's
	// credentials actually reached the service rather than arriving empty.
	seen []supplieroffers.SupplierOfferMutationAuthorization
}

func (r *recordingOfferAccess) AuthorizeSupplierOfferRead(
	_ context.Context,
	_ supplieroffers.SupplierOfferReadAuthorization,
) (supplieroffers.AuthorizedSupplierOfferAccess, error) {
	r.calls++
	return supplieroffers.AuthorizedSupplierOfferAccess{}, supplieroffers.ErrSupplierOfferAccessInvalid
}

func (r *recordingOfferAccess) AuthorizeSupplierOfferMutation(
	_ context.Context,
	input supplieroffers.SupplierOfferMutationAuthorization,
) (supplieroffers.AuthorizedSupplierOfferAccess, error) {
	r.calls++
	r.seen = append(r.seen, input)
	return supplieroffers.AuthorizedSupplierOfferAccess{}, supplieroffers.ErrSupplierOfferAccessInvalid
}

func setValidity(t *testing.T, access *recordingOfferAccess, validUntil any) *httptest.ResponseRecorder {
	t.Helper()

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1.0.0"))
	supplieroffers.RegisterHandlers(api, supplieroffers.NewService(
		supplieroffers.WithSupplierOfferAccessAuthorizer(access)))

	body, err := json.Marshal(map[string]any{
		"draftId":          "draft-1",
		"expectedRevision": 3,
		"offerValidUntil":  validUntil,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPut,
		"/supplier-offers/invitation-1/draft/offer-validity", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-token")
	request.AddCookie(&http.Cookie{Name: "supplier_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "supplier_csrf", Value: "csrf-token"})

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	return response
}

// A validity date that is already past would produce an offer that is expired
// the moment it is submitted, so it is refused as unprocessable content rather
// than stored and failed later.
func TestSetOfferValidityRejectsAPastDateBeforeAuthorizing(t *testing.T) {
	access := &recordingOfferAccess{}
	response := setValidity(t, access,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339))

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if access.calls != 0 {
		t.Errorf("a structurally invalid date reached authorization %d times", access.calls)
	}
}

// Validity beyond the approved horizon is refused with the same bounded 422 the
// submission path uses, so the setter and submission cannot disagree.
func TestSetOfferValidityRejectsADateBeyondTheApprovedHorizon(t *testing.T) {
	access := &recordingOfferAccess{}
	response := setValidity(t, access,
		time.Now().UTC().Add(400*24*time.Hour).Format(time.RFC3339))

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", response.Code, response.Body.String())
	}
	if access.calls != 0 {
		t.Errorf("an over-horizon date reached authorization %d times", access.calls)
	}
}

// An acceptable date must NOT be refused by the route's own date contract: it
// has to reach the service, where authorization and the revision CAS decide the
// outcome. The unconfigured service here therefore reports a bounded 503 rather
// than one of the route's 422s.
//
// That the fully composed route then authorizes through Phase D and enforces
// the expected-revision CAS is proven end to end against the real router and
// MongoDB by the tenanttest H1 journey; this case only pins that a valid date
// is not rejected here.
func TestSetOfferValidityAcceptsADateWithinTheApprovedHorizon(t *testing.T) {
	response := setValidity(t, &recordingOfferAccess{},
		time.Now().UTC().Add(30*24*time.Hour).Format(time.RFC3339))

	if response.Code == http.StatusUnprocessableEntity {
		t.Fatalf("a date inside the approved horizon was refused: %s",
			response.Body.String())
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want the unconfigured service's bounded 503: %s",
			response.Code, response.Body.String())
	}
}
