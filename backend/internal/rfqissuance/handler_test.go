package rfqissuance_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Handler error mapping (design spec §14) and the role boundary (§1A.1, §13).
//
// The distinction that matters most: a FOREIGN TENANT gets 404 — it must not
// learn the resource exists — while a valid tenant with an insufficient role
// gets 403, because they can already see the resource and are simply not
// permitted this transition.

func statusOf(t *testing.T, err error) int {
	t.Helper()
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected a huma.StatusError, got %T (%v)", err, err)
	}
	return statusErr.GetStatus()
}

func TestMapIssuanceErrorAssignsTheSpecifiedStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		// 404: absent, or outside this tenant. The two are indistinguishable
		// by design.
		{"chain not found", rfqissuance.ErrIssuanceChainNotFound, http.StatusNotFound},
		{"version not found", rfqissuance.ErrIssuedVersionNotFound, http.StatusNotFound},
		{"draft not found", rfqissuance.ErrAmendmentDraftNotFound, http.StatusNotFound},
		{"invitation not found", rfqissuance.ErrInvitationNotFound, http.StatusNotFound},

		// 409: a concurrent change or a state conflict the caller can retry
		// against fresh state.
		{"revision mismatch", rfqissuance.ErrRevisionMismatch, http.StatusConflict},
		{"rfq not ready", rfqissuance.ErrRFQNotReady, http.StatusConflict},
		{"version exists", rfqissuance.ErrVersionAlreadyExists, http.StatusConflict},
		{"operation reused", rfqissuance.ErrOperationAlreadyUsed, http.StatusConflict},
		{"draft exists", rfqissuance.ErrAmendmentDraftAlreadyExists, http.StatusConflict},
		{"stale base", rfqissuance.ErrStaleBaseVersion, http.StatusConflict},
		{"invitation already active", rfqissuance.ErrInvitationAlreadyActive,
			http.StatusConflict},
		{"invitation not reactivatable", rfqissuance.ErrInvitationNotReactivatable,
			http.StatusConflict},

		// 422: the request is understood but the content cannot be issued.
		{"deadline required", rfqissuance.ErrResponseDeadlineRequired,
			http.StatusUnprocessableEntity},
		{"currency required", rfqissuance.ErrCurrencyRequired, http.StatusUnprocessableEntity},
		{"invalid currency", rfqissuance.ErrInvalidCurrency, http.StatusUnprocessableEntity},
		{"operation id required", rfqissuance.ErrOperationIDRequired,
			http.StatusUnprocessableEntity},
		{"material id required", rfqissuance.ErrMaterialIDRequired,
			http.StatusUnprocessableEntity},
		{"invalid quantity", rfqissuance.ErrInvalidQuantity,
			http.StatusUnprocessableEntity},
		{"input limit", rfqissuance.ErrInputLimitExceeded,
			http.StatusUnprocessableEntity},
		{"business date", rfqissuance.ErrInvalidBusinessDate,
			http.StatusUnprocessableEntity},

		// 503: a wiring fault, never the caller's fault.
		{"not configured", rfqissuance.ErrIssuanceNotConfigured, http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := statusOf(t, rfqissuance.MapIssuanceError(tc.err))
			if got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

// An unrecognised error must NOT be laundered into a domain status. Mapping an
// infrastructure failure to 409 would tell a client to retry something that
// will never succeed.
func TestMapIssuanceErrorPassesUnknownErrorsThrough(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")

	got := rfqissuance.MapIssuanceError(sentinel)

	if !errors.Is(got, sentinel) {
		t.Errorf("an unrecognised error must pass through unchanged, got %v", got)
	}
}

// §4.1A end to end: the missing-deadline refusal surfaces as 422, which is what
// tells the contractor to set a deadline rather than to retry.
func TestMissingResponseDeadlineSurfacesAs422(t *testing.T) {
	if got := statusOf(t, rfqissuance.MapIssuanceError(
		rfqissuance.ErrResponseDeadlineRequired)); got != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", got)
	}
}

// The OpenAPI surface is the observable route contract. Version history and
// bounded reconciliation are required Phase B capabilities; mounting only the
// create/get handlers would leave both unreachable even though the service
// methods exist.
func TestRegisterHandlersExposesVersionHistoryAndReconciliation(t *testing.T) {
	router, api := platformhttp.NewRouter("rfqissuance-test", "0.0.0")
	rfqissuance.RegisterHandlers(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
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

	found := map[string]bool{
		"rfq-issuance-list-versions":   false,
		"rfq-issuance-reconcile-chain": false,
	}
	for _, methods := range spec.Paths {
		for _, operation := range methods {
			if _, tracked := found[operation.OperationID]; tracked {
				found[operation.OperationID] = true
			}
		}
	}
	for operationID, present := range found {
		if !present {
			t.Errorf("OpenAPI operation %q is missing", operationID)
		}
	}
}
