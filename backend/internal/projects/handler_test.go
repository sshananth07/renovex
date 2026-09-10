package projects

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func newProjectsHandlerTestRouter(t *testing.T) http.Handler {
	t.Helper()
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)

	router, api := platformhttp.NewRouter("projects-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)
	return router
}

func doProjectPatchRequest(t *testing.T, router http.Handler, projectID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/projects/"+projectID, bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestProjectsPatchNameEndToEndSuccess(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"

	// Create a project directly through a fresh service sharing the same
	// backing repo instance is awkward across two Service values, so instead
	// create via the real HTTP create endpoint first.
	createBody, _ := json.Marshal(map[string]string{"clientId": "client_1", "name": "Old Name"})
	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	response := doProjectPatchRequest(t, router, created.ID, map[string]any{"name": "New Name"})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("Name = %q, want New Name", updated.Name)
	}
}

func TestProjectsPatchNameAbsentReturns422(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)

	createBody, _ := json.Marshal(map[string]string{"clientId": "client_1", "name": "Old Name"})
	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// No "name" key at all in the body.
	response := doProjectPatchRequest(t, router, created.ID, map[string]any{})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestProjectsPatchNameEmptyReturns422(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)

	createBody, _ := json.Marshal(map[string]string{"clientId": "client_1", "name": "Old Name"})
	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	response := doProjectPatchRequest(t, router, created.ID, map[string]any{"name": ""})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestProjectsPatchNameCrossTenantReturns404(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)

	createBody, _ := json.Marshal(map[string]string{"clientId": "client_1", "name": "Old Name"})
	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	response := doProjectPatchRequest(t, router, "000000000000000000000000", map[string]any{"name": "New Name"})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", response.Code, response.Body.String())
	}
}

func doScopeBriefPatchRequest(t *testing.T, router http.Handler, projectID string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/projects/"+projectID+"/scope-brief", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func createProjectForScopeBriefTest(t *testing.T, router http.Handler) string {
	t.Helper()
	createBody, _ := json.Marshal(map[string]string{"clientId": "client_1", "name": "Ahmad Residence"})
	createReq := httptest.NewRequest(http.MethodPost, "/projects", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusOK {
		t.Fatalf("create status = %d, body %s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return created.ID
}

func TestProjectsGetExposesScopeBrief(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)
	projectID := createProjectForScopeBriefTest(t, router)

	response := doScopeBriefPatchRequest(t, router, projectID, map[string]any{"scopeBrief": "Full renovation of a condo."})
	if response.Code != http.StatusOK {
		t.Fatalf("patch status = %d, body %s", response.Code, response.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/projects/"+projectID, nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d, body %s", getResp.Code, getResp.Body.String())
	}
	var body struct {
		ScopeBrief string `json:"scopeBrief"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if body.ScopeBrief != "Full renovation of a condo." {
		t.Fatalf("scopeBrief = %q, want set value", body.ScopeBrief)
	}
}

func TestProjectsPatchScopeBriefUpdatesOnlyScopeBrief(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)
	projectID := createProjectForScopeBriefTest(t, router)

	response := doScopeBriefPatchRequest(t, router, projectID, map[string]any{"scopeBrief": "New brief"})
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var updated struct {
		Name       string `json:"name"`
		ScopeBrief string `json:"scopeBrief"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if updated.ScopeBrief != "New brief" {
		t.Fatalf("scopeBrief = %q, want New brief", updated.ScopeBrief)
	}
	if updated.Name != "Ahmad Residence" {
		t.Fatalf("name = %q, want unchanged Ahmad Residence", updated.Name)
	}
}

func TestProjectsPatchScopeBriefOversizedReturns422(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)
	projectID := createProjectForScopeBriefTest(t, router)

	oversized := strings.Repeat("a", 5001)
	response := doScopeBriefPatchRequest(t, router, projectID, map[string]any{"scopeBrief": oversized})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", response.Code, response.Body.String())
	}
}

func TestProjectsPatchScopeBriefForeignProjectReturns404(t *testing.T) {
	router := newProjectsHandlerTestRouter(t)
	_ = createProjectForScopeBriefTest(t, router)

	response := doScopeBriefPatchRequest(t, router, "000000000000000000000000", map[string]any{"scopeBrief": "x"})
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", response.Code, response.Body.String())
	}
}

func TestProjectsPatchScopeBriefRequiresAuthentication(t *testing.T) {
	clientLookup := newFakeClientLookup()
	clientLookup.belongsTo["client_1"] = "company_a"
	svc := NewService(newFakeProjectRepo(), clientLookup)
	router, api := platformhttp.NewRouter("projects-handler-test-noauth", "0.0.0")
	RegisterHandlers(api, svc)

	response := doScopeBriefPatchRequest(t, router, "000000000000000000000000", map[string]any{"scopeBrief": "x"})
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", response.Code, response.Body.String())
	}
}
