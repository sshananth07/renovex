package awards_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// Phase F route contract (§8A.1A, §8K isolation rules).
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

// Each bounded Phase F failure maps to the exact status the spec fixes. An
// unrecognised error must become a generic 503 rather than leaking its text.
func TestMapAwardErrorAssignsTheSpecifiedStatuses(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		// 404: absent, or outside this tenant. Indistinguishable by design.
		{"issued rfq", awards.ErrIssuedRFQNotFound, http.StatusNotFound},
		{"chain", awards.ErrAwardChainNotFound, http.StatusNotFound},
		{"draft", awards.ErrAwardDraftNotFound, http.StatusNotFound},
		{"revision", awards.ErrAwardRevisionNotFound, http.StatusNotFound},
		{"outcome", awards.ErrAwardOutcomeNotFound, http.StatusNotFound},
		{"delivery", awards.ErrAwardDeliveryNotFound, http.StatusNotFound},

		// 409: a concurrent change the caller can retry against fresh state.
		{"draft conflict", awards.ErrAwardDraftConflict, http.StatusConflict},
		{"revision conflict", awards.ErrAwardRevisionConflict, http.StatusConflict},
		{"offer not eligible", awards.ErrOfferVersionNotEligible, http.StatusConflict},
		{"line claimed", awards.ErrRFQLineAwardConflict, http.StatusConflict},

		// 422: understood, but this content cannot be awarded.
		{"not selectable", awards.ErrOfferVersionNotSelectable,
			http.StatusUnprocessableEntity},
		{"expired", awards.ErrOfferVersionExpired, http.StatusUnprocessableEntity},
		{"not quoted", awards.ErrOfferLineNotQuoted, http.StatusUnprocessableEntity},
		{"quantity mismatch", awards.ErrQuantityOrUnitMismatch,
			http.StatusUnprocessableEntity},
		{"currency mismatch", awards.ErrCurrencyMismatch,
			http.StatusUnprocessableEntity},
		{"offer level tax", awards.ErrOfferLevelTaxRequiresComplete,
			http.StatusUnprocessableEntity},
		{"charges unresolvable", awards.ErrConditionalChargesNotResolvable,
			http.StatusUnprocessableEntity},
		{"already awarded", awards.ErrRFQLineAlreadyAwarded,
			http.StatusUnprocessableEntity},
		{"not monotonic", awards.ErrAwardCorrectionNotMonotonic,
			http.StatusUnprocessableEntity},
		{"unawarded reason", awards.ErrInvalidUnawardedReason,
			http.StatusUnprocessableEntity},
		{"change reason", awards.ErrChangeReasonRequired,
			http.StatusUnprocessableEntity},

		// 503: the bounded crash-window read. It must never report "no award".
		{"finalisation pending", awards.ErrAwardFinalisationPending,
			http.StatusServiceUnavailable},
		{"unknown", errors.New("some infrastructure detail"),
			http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusOf(t, awards.MapAwardError(tc.err)); got != tc.want {
				t.Errorf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

// An unrecognised error's message must not reach the client: it could carry
// infrastructure detail a caller has no business seeing.
func TestMapAwardErrorDoesNotLeakUnknownErrorText(t *testing.T) {
	err := awards.MapAwardError(errors.New("dial tcp 10.0.0.5:27017: refused"))
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected a huma.StatusError, got %T", err)
	}
	if got := statusErr.Error(); got == "dial tcp 10.0.0.5:27017: refused" {
		t.Fatalf("mapped error leaked the underlying message: %q", got)
	}
}

// The OpenAPI surface is the observable route contract. F1 must mount its own
// route in its own checkpoint (§8A.1A) — deferring it would defer the
// authorization and isolation tests that prove it.
func TestRegisterHandlersExposesTheComparisonRoute(t *testing.T) {
	router, api := platformhttp.NewRouter("awards-test", "0.0.0")
	awards.RegisterHandlers(api, nil)

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

	found := false
	for path, methods := range spec.Paths {
		for _, operation := range methods {
			if operation.OperationID == "awards-get-comparison" {
				found = true
				if path != "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/comparison" {
					t.Errorf("comparison path = %q, not the path §8B fixes", path)
				}
			}
		}
	}
	if !found {
		t.Error("OpenAPI operation \"awards-get-comparison\" is missing")
	}
}

type stubComparisonSource struct {
	issued    awards.IssuedRFQSnapshot
	found     bool
	versions  []awards.OfferVersionSnapshot
	companyID string
}

func (s *stubComparisonSource) GetIssuedRFQForAward(
	_ context.Context, companyID, _ string,
) (awards.IssuedRFQSnapshot, bool, error) {
	s.companyID = companyID
	// The adapter is company-scoped, so another tenant's issued version is
	// simply absent rather than visible-but-refused.
	if companyID != s.issued.CompanyID {
		return awards.IssuedRFQSnapshot{}, false, nil
	}
	return s.issued, s.found, nil
}

func (s *stubComparisonSource) GetOfferVersionForAward(
	_ context.Context, _, _ string,
) (awards.OfferVersionSnapshot, bool, error) {
	return awards.OfferVersionSnapshot{}, false, nil
}

func (s *stubComparisonSource) ListOfferVersionsForIssuedRFQVersion(
	_ context.Context, companyID, _ string,
) ([]awards.OfferVersionSnapshot, error) {
	if companyID != s.issued.CompanyID {
		return nil, nil
	}
	return s.versions, nil
}

func comparisonTestService() (*awards.Service, *stubComparisonSource) {
	subtotal := money.New(1000, "MYR")
	source := &stubComparisonSource{
		issued: awards.IssuedRFQSnapshot{
			ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
			RFQNumber: "RFQ-0001", VersionNumber: 1, Currency: "MYR",
			Lines: []awards.IssuedRFQLineSnapshot{
				{ID: "line-1", LineageID: "lineage-1", MaterialName: "Tile"},
			},
		},
		found: true,
		versions: []awards.OfferVersionSnapshot{{
			ID: "offer-1", CompanyID: "company-1", SupplierID: "supplier-a",
			SupplierName: "Supplier A", InvitationID: "invitation-a",
			IssuedRFQVersionID: "issued-1", VersionNumber: 1, Currency: "MYR",
			Lines: []awards.OfferLineSnapshot{{
				ID: "ol-1", RFQLineID: "line-1",
				ResponseStatus:           awards.OfferLineQuoted,
				LineSubtotalExcludingTax: &subtotal,
				LineTaxAmount:            money.New(0, "MYR"),
			}},
			Tax:               awards.OfferTaxRule{Mode: awards.OfferTaxNotApplicable},
			OfferValidUntil:   time.Now().UTC().Add(720 * time.Hour),
			IsLatestSubmitted: true,
			EligibilityState:  awards.OfferEligibilityEligible,
		}},
	}
	service := awards.NewService(
		awards.WithIssuedRFQSource(source),
		awards.WithOfferVersionSource(source),
	)
	return service, source
}

func comparisonRequest(t *testing.T, role, companyID string) *httptest.ResponseRecorder {
	t.Helper()
	service, _ := comparisonTestService()
	router, api := platformhttp.NewRouter("awards-test", "0.0.0")

	// Stand in for the authenticated group: the real composition root mounts
	// these routes behind auth middleware that populates the same Principal.
	api.UseMiddleware(func(hctx huma.Context, next func(huma.Context)) {
		ctx := identity.ContextWithPrincipal(hctx.Context(), identity.Principal{
			UserID: "user-1", CompanyID: companyID, Role: role,
		})
		next(huma.WithContext(hctx, ctx))
	})
	awards.RegisterHandlers(api, service)

	req := httptest.NewRequest(http.MethodGet,
		"/rfq-chains/rfqchain-1/issued-versions/issued-1/comparison", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// Comparison is preparation, not an externally visible transition, so all three
// company roles may read it (§8B, Decision A §1A.1).
func TestComparisonRouteIsReadableByOwnerAdminAndEmployee(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			rec := comparisonRequest(t, role, "company-1")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A caller outside the tenant gets 404, never 403: a 403 would confirm the
// issued version exists.
func TestComparisonRouteReturns404ForForeignTenant(t *testing.T) {
	rec := comparisonRequest(t, "owner", "company-2")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// The response must carry the indicative labelling through to the wire, so a
// client cannot mistake a comparison subtotal for the award figure.
func TestComparisonResponseLabelsSubtotalsIndicative(t *testing.T) {
	rec := comparisonRequest(t, "owner", "company-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Authoritative bool `json:"authoritative"`
		Offers        []struct {
			OfferVersionID           string `json:"offerVersionId"`
			IndicativeQuotedSubtotal struct {
				Indicative bool `json:"indicative"`
				Amount     struct {
					Amount int64 `json:"amount"`
				} `json:"amount"`
			} `json:"indicativeQuotedSubtotal"`
			Lines []struct {
				Selectable bool `json:"selectable"`
			} `json:"lines"`
		} `json:"offers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode comparison: %v; body=%s", err, rec.Body.String())
	}
	if body.Authoritative {
		t.Error("the comparison response must not claim to be authoritative")
	}
	if len(body.Offers) != 1 {
		t.Fatalf("offers = %d, want 1", len(body.Offers))
	}
	if !body.Offers[0].IndicativeQuotedSubtotal.Indicative {
		t.Error("subtotal lost its indicative label on the wire")
	}
	if got := body.Offers[0].IndicativeQuotedSubtotal.Amount.Amount; got != 1000 {
		t.Errorf("subtotal = %d, want 1000", got)
	}
	if !body.Offers[0].Lines[0].Selectable {
		t.Error("a quoted line on the current version must be selectable")
	}
}
