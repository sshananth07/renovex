package costs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newCostsHandlerTestRouter(t *testing.T) http.Handler {
	t.Helper()
	svc, _ := newTestService()
	return newCostsHandlerTestRouterForService(t, svc, "company_a")
}

// newCostsHandlerTestRouterForService builds a second router against the
// SAME underlying svc/repo, authenticated as a different company — used to
// prove cross-tenant denial at the HTTP boundary rather than only at the
// service layer.
func newCostsHandlerTestRouterForService(t *testing.T, svc *costs.Service, companyID string) http.Handler {
	t.Helper()
	router, api := platformhttp.NewRouter("costs-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: companyID, Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	costs.RegisterHandlers(authedAPI, svc)
	return router
}

func doCreateCostItemRequest(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/cost-items", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func doCostItemRequest(t *testing.T, router http.Handler, method, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func mustCreateCostItemViaHandler(t *testing.T, router http.Handler) map[string]any {
	t.Helper()
	response := doCreateCostItemRequest(t, router, map[string]any{
		"projectId":       "project_1",
		"description":     "Cement",
		"category":        "material",
		"estimatedAmount": 100000,
		"currency":        "MYR",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("setup create: status = %d, want 200, body %s", response.Code, response.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created cost item: %v", err)
	}
	return created
}

func TestCreateCostItemPluralCategoryRejectedAtRequestBoundary(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	response := doCreateCostItemRequest(t, router, map[string]any{
		"projectId":       "project_1",
		"description":     "aaaaa",
		"category":        "materials",
		"estimatedAmount": 1000,
		"currency":        "MYR",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestCreateCostItemSingularCategoryAccepted(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	response := doCreateCostItemRequest(t, router, map[string]any{
		"projectId":       "project_1",
		"description":     "aaaaa",
		"category":        "material",
		"estimatedAmount": 1000,
		"currency":        "MYR",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCreateCostItemPublicEndpointCannotCreateLabourCategory(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	response := doCreateCostItemRequest(t, router, map[string]any{
		"projectId":       "project_1",
		"description":     "labour attempt",
		"category":        "labour",
		"estimatedAmount": 1000,
		"currency":        "MYR",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (labour rejected at request boundary), body %s", response.Code, response.Body.String())
	}
}

func TestCreateCostItemAllPublicCategoriesAccepted(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	for _, category := range []string{
		"material", "subcontractor", "equipment", "transport",
		"permit", "professional_fee", "utility", "miscellaneous",
	} {
		response := doCreateCostItemRequest(t, router, map[string]any{
			"projectId":       "project_1",
			"description":     "item for " + category,
			"category":        category,
			"estimatedAmount": 1000,
			"currency":        "MYR",
		})
		if response.Code != http.StatusOK {
			t.Fatalf("category %s: status = %d, want 200, body %s", category, response.Code, response.Body.String())
		}
	}
}

func TestUpdateLifecycleRouteRejectsStageActual(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	response := doCostItemRequest(t, router, http.MethodPatch, "/cost-items/"+created["id"].(string)+"/lifecycle", map[string]any{
		"expectedRevision": created["revision"],
		"stage":            "actual",
		"amount":           108000,
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (actual must use record/correct routes), body %s", response.Code, response.Body.String())
	}
}

func TestUpdateLifecycleRouteAcceptsCommitted(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	response := doCostItemRequest(t, router, http.MethodPatch, "/cost-items/"+created["id"].(string)+"/lifecycle", map[string]any{
		"expectedRevision": created["revision"],
		"stage":            "committed",
		"amount":           95000,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestUpdateLifecycleRouteRejectsStaleRevision(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	response := doCostItemRequest(t, router, http.MethodPatch, "/cost-items/"+created["id"].(string)+"/lifecycle", map[string]any{
		"expectedRevision": 99,
		"stage":            "committed",
		"amount":           95000,
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", response.Code, response.Body.String())
	}
}

func TestRecordActualRouteSetsTheFirstValue(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	response := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           108000,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	actual, ok := updated["actual"].(map[string]any)
	if !ok || actual["amount"] != float64(108000) {
		t.Fatalf("expected actual.amount 108000, got %+v", updated["actual"])
	}
}

func TestRecordActualRouteRejectsWhenAlreadyRecorded(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	first := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           108000,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("setup record: status = %d, body %s", first.Code, first.Body.String())
	}
	var afterFirst map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &afterFirst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": afterFirst["revision"],
		"amount":           120000,
	})
	if second.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (actual already recorded), body %s", second.Code, second.Body.String())
	}
}

func TestCorrectActualRoutePreservesThePreviousValue(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	recorded := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           850000,
	})
	if recorded.Code != http.StatusOK {
		t.Fatalf("setup record: status = %d, body %s", recorded.Code, recorded.Body.String())
	}
	var afterRecord map[string]any
	if err := json.Unmarshal(recorded.Body.Bytes(), &afterRecord); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	corrected := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/correct-actual", map[string]any{
		"expectedRevision": afterRecord["revision"],
		"amount":           85000,
		"reason":           "Decimal point error",
	})
	if corrected.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", corrected.Code, corrected.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(corrected.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	actual, ok := updated["actual"].(map[string]any)
	if !ok || actual["amount"] != float64(85000) {
		t.Fatalf("expected actual.amount 85000, got %+v", updated["actual"])
	}
	corrections, ok := updated["actualCorrections"].([]any)
	if !ok || len(corrections) != 1 {
		t.Fatalf("expected exactly 1 correction record, got %+v", updated["actualCorrections"])
	}
	first := corrections[0].(map[string]any)
	previous := first["previousAmount"].(map[string]any)
	if previous["amount"] != float64(850000) {
		t.Fatalf("expected previousAmount.amount 850000, got %+v", previous)
	}
}

func TestCorrectActualRouteRejectsBlankReason(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	recorded := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           850000,
	})
	if recorded.Code != http.StatusOK {
		t.Fatalf("setup record: status = %d, body %s", recorded.Code, recorded.Body.String())
	}
	var afterRecord map[string]any
	if err := json.Unmarshal(recorded.Body.Bytes(), &afterRecord); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	response := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/correct-actual", map[string]any{
		"expectedRevision": afterRecord["revision"],
		"amount":           85000,
		"reason":           "",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (reason required), body %s", response.Code, response.Body.String())
	}
}

func TestCorrectActualRouteRejectsWhenNoActualYetRecorded(t *testing.T) {
	router := newCostsHandlerTestRouter(t)
	created := mustCreateCostItemViaHandler(t, router)

	response := doCostItemRequest(t, router, http.MethodPost, "/cost-items/"+created["id"].(string)+"/correct-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           85000,
		"reason":           "some reason",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (nothing recorded yet to correct), body %s", response.Code, response.Body.String())
	}
}

func TestCorrectActualRouteIsTenantScopedAtTheHTTPBoundary(t *testing.T) {
	svc, _ := newTestService()
	routerA := newCostsHandlerTestRouterForService(t, svc, "company_a")
	routerB := newCostsHandlerTestRouterForService(t, svc, "company_b")

	created := mustCreateCostItemViaHandler(t, routerA)
	recorded := doCostItemRequest(t, routerA, http.MethodPost, "/cost-items/"+created["id"].(string)+"/record-actual", map[string]any{
		"expectedRevision": created["revision"],
		"amount":           850000,
	})
	if recorded.Code != http.StatusOK {
		t.Fatalf("setup record: status = %d, body %s", recorded.Code, recorded.Body.String())
	}
	var afterRecord map[string]any
	if err := json.Unmarshal(recorded.Body.Bytes(), &afterRecord); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	response := doCostItemRequest(t, routerB, http.MethodPost, "/cost-items/"+created["id"].(string)+"/correct-actual", map[string]any{
		"expectedRevision": afterRecord["revision"],
		"amount":           85000,
		"reason":           "attempted cross-tenant correction",
	})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (company_b must not see company_a's cost item), body %s", response.Code, response.Body.String())
	}
}

func TestLabourCostRecorderCanStillCreateLabourCategoryInternally(t *testing.T) {
	svc, repo := newTestService()
	id, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(1000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created, err := repo.FindByID(context.Background(), "company_a", id)
	if err != nil {
		t.Fatalf("unexpected error fetching created item: %v", err)
	}
	if created.Category != costs.CostCategoryLabour {
		t.Fatalf("expected category labour, got %s", created.Category)
	}
}
