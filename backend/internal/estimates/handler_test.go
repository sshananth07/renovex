package estimates_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newEstimatesHandlerTestRouter(t *testing.T, costSource estimates.EstimatedCostSource) http.Handler {
	t.Helper()
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	router, api := platformhttp.NewRouter("estimates-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	estimates.RegisterHandlers(authedAPI, svc)
	return router
}

func doCreateEstimateRequest(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/estimates", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCreateEstimateInvalidPricingModeRejectedAtRequestBoundary(t *testing.T) {
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(50000, "MYR")},
	}}
	router := newEstimatesHandlerTestRouter(t, costSource)
	response := doCreateEstimateRequest(t, router, map[string]any{
		"projectId":   "project_1",
		"pricingMode": "not_a_real_mode",
		"pricingRate": 2000,
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestCreateEstimateMarkupModeAccepted(t *testing.T) {
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(50000, "MYR")},
	}}
	router := newEstimatesHandlerTestRouter(t, costSource)
	response := doCreateEstimateRequest(t, router, map[string]any{
		"projectId":   "project_1",
		"pricingMode": "markup",
		"pricingRate": 2000,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCreateEstimateMarginModeAccepted(t *testing.T) {
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(50000, "MYR")},
	}}
	router := newEstimatesHandlerTestRouter(t, costSource)
	response := doCreateEstimateRequest(t, router, map[string]any{
		"projectId":   "project_1",
		"pricingMode": "margin",
		"pricingRate": 2000,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCreateEstimateNoEstimatedCostsReturns422(t *testing.T) {
	router := newEstimatesHandlerTestRouter(t, fakeCostSource{})
	response := doCreateEstimateRequest(t, router, map[string]any{
		"projectId":   "project_1",
		"pricingMode": "markup",
		"pricingRate": 2000,
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (no estimated costs), body %s", response.Code, response.Body.String())
	}
}
