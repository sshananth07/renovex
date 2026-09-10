package labour_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newLabourHandlerTestRouter(t *testing.T) http.Handler {
	t.Helper()
	svc, _, _, _ := newTestLabourService()

	router, api := platformhttp.NewRouter("labour-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	labour.RegisterHandlers(authedAPI, svc)
	return router
}

func doCreateLabourEntryRequest(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/labour-entries", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func errorLocations(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	var body struct {
		Errors []struct {
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v, body: %s", err, response.Body.String())
	}
	var locations []string
	for _, e := range body.Errors {
		locations = append(locations, e.Location)
	}
	return locations
}

func TestCreateLabourEntryAdHocMissingTradeReturns422WithFieldLocation(t *testing.T) {
	router := newLabourHandlerTestRouter(t)
	response := doCreateLabourEntryRequest(t, router, map[string]any{
		"projectId":     "project_1",
		"workItemId":    "work_item_1",
		"workerName":    "ali",
		"quantityValue": "12",
		"quantityUnit":  "hour",
		"rateAmount":    10000,
		"currency":      "MYR",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	if len(locations) != 1 || locations[0] != "body.trade" {
		t.Fatalf("expected exactly one error at body.trade, got %v", locations)
	}
}

func TestCreateLabourEntryAdHocMissingAllFieldsReturnsExhaustiveErrors(t *testing.T) {
	router := newLabourHandlerTestRouter(t)
	response := doCreateLabourEntryRequest(t, router, map[string]any{
		"projectId":     "project_1",
		"workItemId":    "work_item_1",
		"quantityValue": "12",
		"quantityUnit":  "hour",
		"currency":      "MYR",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	want := map[string]bool{"body.workerName": false, "body.trade": false, "body.rateAmount": false}
	for _, loc := range locations {
		if _, ok := want[loc]; ok {
			want[loc] = true
		}
	}
	for loc, found := range want {
		if !found {
			t.Fatalf("expected error at %s, got locations %v", loc, locations)
		}
	}
}

func TestCreateLabourEntryExistingWorkerModeValid(t *testing.T) {
	router := newLabourHandlerTestRouter(t)

	// Create a worker first via the service so we have a real workerId.
	createWorkerBody, _ := json.Marshal(map[string]any{
		"name": "Ahmad", "trade": "Tiler", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	createWorkerReq := httptest.NewRequest(http.MethodPost, "/workers", bytes.NewReader(createWorkerBody))
	createWorkerReq.Header.Set("Content-Type", "application/json")
	createWorkerResp := httptest.NewRecorder()
	router.ServeHTTP(createWorkerResp, createWorkerReq)
	if createWorkerResp.Code != http.StatusOK {
		t.Fatalf("create worker status = %d, body %s", createWorkerResp.Code, createWorkerResp.Body.String())
	}
	var worker struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createWorkerResp.Body.Bytes(), &worker); err != nil {
		t.Fatalf("decode worker response: %v", err)
	}

	response := doCreateLabourEntryRequest(t, router, map[string]any{
		"projectId":     "project_1",
		"workItemId":    "work_item_1",
		"workerId":      worker.ID,
		"quantityValue": "8",
		"quantityUnit":  "day",
		"currency":      "MYR",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCreateWorkerInvalidRateTypeRejectedAtRequestBoundary(t *testing.T) {
	router := newLabourHandlerTestRouter(t)
	raw, _ := json.Marshal(map[string]any{
		"name": "Ahmad", "rateType": "not_a_real_rate_type", "defaultRateAmount": 15000, "currency": "MYR",
	})
	request := httptest.NewRequest(http.MethodPost, "/workers", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestCreateWorkerValidRateTypesAcceptedAtRequestBoundary(t *testing.T) {
	router := newLabourHandlerTestRouter(t)
	for _, rt := range []string{"hourly", "daily", "fixed_project", "per_unit", "per_square_meter"} {
		raw, _ := json.Marshal(map[string]any{
			"name": "Worker " + rt, "rateType": rt, "defaultRateAmount": 15000, "currency": "MYR",
		})
		request := httptest.NewRequest(http.MethodPost, "/workers", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("rateType %s: status = %d, want 200, body %s", rt, response.Code, response.Body.String())
		}
	}
}
