package workresources

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newHandlerTestRouter(t *testing.T) (http.Handler, *Service) {
	t.Helper()
	projectLookup := newFakeProjectLookup()
	projectLookup.belongsTo["project_1"] = "company_a"
	workItemLookup := newFakeWorkItemLookup()
	workItemLookup.companyOf["work_1"] = "company_a"
	workItemLookup.projectOf["work_1"] = "project_1"
	svc := NewService(newFakeRepo(), projectLookup, workItemLookup, newFakeMaterialLookup())

	router, api := platformhttp.NewRouter("workresources-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)
	return router, svc
}

func TestHandlerRequiresAuthentication(t *testing.T) {
	projectLookup := newFakeProjectLookup()
	svc := NewService(newFakeRepo(), projectLookup, newFakeWorkItemLookup(), newFakeMaterialLookup())
	router, api := platformhttp.NewRouter("workresources-noauth", "0.0.0")
	RegisterHandlers(api, svc)

	req := httptest.NewRequest(http.MethodGet, "/work-resource-requirements?projectId=project_1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerListByProjectRequiresProjectID(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/work-resource-requirements", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerListByProjectForeignReturns404(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/work-resource-requirements?projectId=project_foreign", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerListByProjectReturnsPlanningFieldsOnly(t *testing.T) {
	router, svc := newHandlerTestRouter(t)

	if _, err := svc.CreateFromAISuggestion(context.Background(), "company_a", "project_1", "work_1",
		ResourceTypeTrade, nil, "Tiler", "suggestion_1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/work-resource-requirements?projectId=project_1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}

	body := resp.Body.String()
	if bytes.Contains(resp.Body.Bytes(), []byte("companyId")) {
		t.Fatalf("response must not include companyId, got %s", body)
	}
	for _, forbidden := range []string{"price", "cost", "rate", "quantity"} {
		if bytesContainsCI(resp.Body.Bytes(), forbidden) {
			t.Fatalf("response must not include %q (no financial/quantity authority), got %s", forbidden, body)
		}
	}

	var decoded struct {
		Requirements []struct {
			ID           string `json:"id"`
			WorkItemID   string `json:"workItemId"`
			ResourceType string `json:"resourceType"`
			Name         string `json:"name"`
			Status       string `json:"status"`
			Source       string `json:"source"`
		} `json:"requirements"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(decoded.Requirements) != 1 || decoded.Requirements[0].Name != "Tiler" {
		t.Fatalf("expected 1 requirement (Tiler), got %+v", decoded.Requirements)
	}
}

func TestHandlerListByWorkItemMustBelongToSameProject(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/work-resource-requirements?projectId=project_1&workItemId=work_foreign", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", resp.Code, resp.Body.String())
	}
}

func bytesContainsCI(b []byte, substr string) bool {
	lower := bytes.ToLower(b)
	return bytes.Contains(lower, bytes.ToLower([]byte(substr)))
}
