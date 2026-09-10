package work

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newWorkItemsHandlerTestRouter(t *testing.T) http.Handler {
	t.Helper()
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	spaceLookup := newFakeSpaceLookup()
	spaceLookup.companyOf["space_1"] = "company_a"
	spaceLookup.projectOf["space_1"] = "project_1"
	svc := NewService(newFakeWorkItemRepo(), projectLookup, spaceLookup)

	router, api := platformhttp.NewRouter("work-items-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)
	return router
}

func createWorkItemViaHTTP(t *testing.T, router http.Handler, body map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/work-items", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return created.ID
}

func doWorkItemPatchRequest(t *testing.T, router http.Handler, workItemID string, rawBody string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPatch, "/work-items/"+workItemID, bytes.NewReader([]byte(rawBody)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestWorkItemsPatchDescriptionOnlyEndToEnd(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "description": "Old", "quantityValue": "1", "quantityUnit": "unit",
	})

	response := doWorkItemPatchRequest(t, router, id, `{"description":"New"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var body workItemDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Description != "New" {
		t.Fatalf("Description = %q, want New", body.Description)
	}
	if body.QuantityValue != "1" || body.QuantityUnit != "unit" {
		t.Fatalf("Quantity = %s %s, want unchanged 1 unit", body.QuantityValue, body.QuantityUnit)
	}
}

func TestWorkItemsPatchSpaceIDExplicitNullClearsAssignment(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "spaceId": "space_1", "description": "Demo", "quantityValue": "1", "quantityUnit": "unit",
	})

	response := doWorkItemPatchRequest(t, router, id, `{"spaceId":null}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var body workItemDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SpaceID != "" {
		t.Fatalf("SpaceID = %q, want cleared to empty", body.SpaceID)
	}
}

func TestWorkItemsPatchSpaceIDOmittedPreservesAssignment(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "spaceId": "space_1", "description": "Demo", "quantityValue": "1", "quantityUnit": "unit",
	})

	response := doWorkItemPatchRequest(t, router, id, `{"description":"Updated"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var body workItemDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SpaceID != "space_1" {
		t.Fatalf("SpaceID = %q, want unchanged space_1", body.SpaceID)
	}
}

func TestWorkItemsPatchSpaceIDValueAssignsSpace(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "description": "Demo", "quantityValue": "1", "quantityUnit": "unit",
	})

	response := doWorkItemPatchRequest(t, router, id, `{"spaceId":"space_1"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var body workItemDTO
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.SpaceID != "space_1" {
		t.Fatalf("SpaceID = %q, want space_1", body.SpaceID)
	}
}

func TestWorkItemsPatchCancelledReturns409(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "description": "Demo", "quantityValue": "1", "quantityUnit": "unit",
	})

	cancelReq := httptest.NewRequest(http.MethodPatch, "/work-items/"+id+"/status", bytes.NewReader([]byte(`{"status":"cancelled"}`)))
	cancelReq.Header.Set("Content-Type", "application/json")
	cancelResp := httptest.NewRecorder()
	router.ServeHTTP(cancelResp, cancelReq)
	if cancelResp.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body %s", cancelResp.Code, cancelResp.Body.String())
	}

	response := doWorkItemPatchRequest(t, router, id, `{"description":"Should not apply"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", response.Code, response.Body.String())
	}
}

func TestWorkItemsPatchForeignSpaceReturns404(t *testing.T) {
	router := newWorkItemsHandlerTestRouter(t)
	id := createWorkItemViaHTTP(t, router, map[string]any{
		"projectId": "project_1", "description": "Demo", "quantityValue": "1", "quantityUnit": "unit",
	})

	response := doWorkItemPatchRequest(t, router, id, `{"spaceId":"nonexistent-space"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", response.Code, response.Body.String())
	}
}
