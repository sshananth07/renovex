package suppliers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

func newSuppliersHandlerTestRouter(t *testing.T) (http.Handler, *suppliers.Service) {
	t.Helper()
	svc, _, _, _, _ := newService(t)

	router, api := platformhttp.NewRouter("suppliers-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	suppliers.RegisterHandlers(authedAPI, svc)
	return router, svc
}

func doCreateOfferingRequest(t *testing.T, router http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/supplier-offerings", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestCreateOfferingPriceWithoutAsOfRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":              supplier.ID,
		"productName":             "aaaaaa",
		"unit":                    "bag",
		"indicativePriceAmount":   10000,
		"indicativePriceCurrency": "MYR",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	if !containsLocation(locations, "body.indicativePriceAsOf") {
		t.Fatalf("expected error at body.indicativePriceAsOf, got %v", locations)
	}
}

func TestCreateOfferingPriceWithoutUnitRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":              supplier.ID,
		"productName":             "aaaaaa",
		"indicativePriceAmount":   10000,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-11T00:00:00Z",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	if !containsLocation(locations, "body.unit") {
		t.Fatalf("expected error at body.unit, got %v", locations)
	}
}

func TestCreateOfferingZeroPriceRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":              supplier.ID,
		"productName":             "aaaaaa",
		"unit":                    "bag",
		"indicativePriceAmount":   0,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-11T00:00:00Z",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	if !containsLocation(locations, "body.indicativePriceAmount") {
		t.Fatalf("expected error at body.indicativePriceAmount, got %v", locations)
	}
}

func TestCreateOfferingNegativePriceRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":              supplier.ID,
		"productName":             "aaaaaa",
		"unit":                    "bag",
		"indicativePriceAmount":   -100,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-11T00:00:00Z",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestCreateOfferingAsOfWithoutPriceRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":          supplier.ID,
		"productName":         "aaaaaa",
		"unit":                "bag",
		"indicativePriceAsOf": "2026-08-11T00:00:00Z",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
	locations := errorLocations(t, response)
	if !containsLocation(locations, "body.indicativePriceAsOf") {
		t.Fatalf("expected error at body.indicativePriceAsOf, got %v", locations)
	}
}

func TestCreateOfferingValidPricedOfferingAccepted(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":              supplier.ID,
		"productName":             "aaaaaa",
		"unit":                    "bag",
		"indicativePriceAmount":   10000,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-11T00:00:00Z",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func TestCreateOfferingValidUnpricedOfferingAccepted(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	response := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "aaaaaa",
		"unit":        "bag",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
}

func doPatchOfferingRequest(t *testing.T, router http.Handler, offeringID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/supplier-offerings/"+offeringID, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestUpdateOfferingChangesProductNameAndPrice(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	if create.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200, body %s", create.Code, create.Body.String())
	}
	var created struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision":        created.Revision,
		"productName":             "New Name",
		"indicativePriceAmount":   13000,
		"indicativePriceCurrency": "MYR",
		"indicativePriceAsOf":     "2026-08-20T00:00:00Z",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body %s", response.Code, response.Body.String())
	}
	var updated struct {
		ProductName     string `json:"productName"`
		IndicativePrice struct {
			Amount int64 `json:"amount"`
		} `json:"indicativePrice"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated offering: %v", err)
	}
	if updated.ProductName != "New Name" {
		t.Errorf("productName = %q, want %q", updated.ProductName, "New Name")
	}
	if updated.IndicativePrice.Amount != 13000 {
		t.Errorf("indicativePrice.amount = %d, want 13000", updated.IndicativePrice.Amount)
	}
}

func TestUpdateOfferingStaleRevisionRejectedAtRequestBoundary(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision": 999,
		"productName":      "New Name",
	})
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", response.Code, response.Body.String())
	}
}

func TestUpdateOfferingSupplierIDMismatchRejected(t *testing.T) {
	router, svc := newSuppliersHandlerTestRouter(t)
	supplier := createSupplier(t, svc, "ABC Materials")
	other := createSupplier(t, svc, "Other Materials")
	create := doCreateOfferingRequest(t, router, map[string]any{
		"supplierId":  supplier.ID,
		"productName": "Old Name",
		"unit":        "bag",
	})
	var created struct {
		ID       string `json:"id"`
		Revision int64  `json:"revision"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created offering: %v", err)
	}

	response := doPatchOfferingRequest(t, router, created.ID, map[string]any{
		"expectedRevision": created.Revision,
		"supplierId":       other.ID,
	})
	if response.Code != http.StatusUnprocessableEntity && response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 422 or 409 for a supplier-id change attempt, body %s", response.Code, response.Body.String())
	}
}

func TestUpdateOfferingUnknownIDIsNotFound(t *testing.T) {
	router, _ := newSuppliersHandlerTestRouter(t)
	response := doPatchOfferingRequest(t, router, "000000000000000000000000", map[string]any{
		"expectedRevision": 0,
		"productName":      "New Name",
	})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", response.Code, response.Body.String())
	}
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

func containsLocation(locations []string, want string) bool {
	for _, loc := range locations {
		if loc == want {
			return true
		}
	}
	return false
}
