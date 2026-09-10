package ai

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
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	router, api := platformhttp.NewRouter("ai-handler-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)
	return router, svc
}

func doPost(t *testing.T, router http.Handler, path string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func TestHandlerGenerationRequiresAuthentication(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	router, api := platformhttp.NewRouter("ai-noauth", "0.0.0")
	RegisterHandlers(api, svc)

	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerGenerationBlankOperationIDReturns422(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": ""})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerSpaceSuggestionsHappyPath(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Batch struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"batch"`
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Batch.Status != "completed" {
		t.Fatalf("expected completed batch, got %s", body.Batch.Status)
	}
	if len(body.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(body.Suggestions))
	}
}

// TestHandlerSpaceSuggestionDataUsesCamelCaseJSONKeys guards against a real
// bug found via live manual browser verification (M8.5B-A Task 16): the
// SuggestedData sub-structs (SpaceSuggestionData et al.) had no `json:` tags,
// so Go's default field-name-verbatim marshaling silently emitted
// PascalCase ("Name", "SpaceType") into the public suggestedData response
// body instead of the camelCase every other DTO in this codebase (and the
// frontend) expects. go build/vet/test never caught this because
// json.Unmarshal into a matching Go struct works regardless of tags — only
// a real camelCase-key assertion (or a real JS consumer) can catch it.
func TestHandlerSpaceSuggestionDataUsesCamelCaseJSONKeys(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Suggestions []struct {
			SuggestedData map[string]any `json:"suggestedData"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(body.Suggestions))
	}
	data := body.Suggestions[0].SuggestedData
	if _, ok := data["name"]; !ok {
		t.Fatalf("expected camelCase key %q in suggestedData, got keys %v", "name", keysOf(data))
	}
	if _, ok := data["spaceType"]; !ok {
		t.Fatalf("expected camelCase key %q in suggestedData, got keys %v", "spaceType", keysOf(data))
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestHandlerMissingScopeBriefReturns422SafeDetail(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	gw.projectScopeBrief["project_1"] = ""
	svc := NewService(repo, gw, client)
	router, api := platformhttp.NewRouter("ai-handler-blank-brief", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)

	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body %s", resp.Code, resp.Body.String())
	}
	if bytesContainsAny(resp.Body.Bytes(), "panic", "goroutine", "runtime error") {
		t.Fatalf("expected safe detail, got %s", resp.Body.String())
	}
}

func TestHandlerCrossTenantProjectReturns404(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	resp := doPost(t, router, "/projects/project_foreign/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerIdempotencyConflictReturns409(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	router, api := platformhttp.NewRouter("ai-idempotency-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)

	if resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"}); resp.Code != http.StatusOK {
		t.Fatalf("first call status = %d, body %s", resp.Code, resp.Body.String())
	}

	gw.projectScopeBrief["project_1"] = "A completely different brief."
	resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	if resp.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerListBatchesNewestFirst(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	if resp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"}); resp.Code != http.StatusOK {
		t.Fatalf("generate status = %d, body %s", resp.Code, resp.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/projects/project_1/ai/batches?type=space_suggestions", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Batches []struct {
			ID string `json:"id"`
		} `json:"batches"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(body.Batches))
	}
}

func TestHandlerListSuggestionsByBatch(t *testing.T) {
	router, _ := newHandlerTestRouter(t)

	genResp := doPost(t, router, "/projects/project_1/ai/space-suggestions", map[string]any{"operationId": "op_1"})
	var genBody struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(genResp.Body.Bytes(), &genBody); err != nil {
		t.Fatalf("decode: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ai-batches/"+genBody.Batch.ID+"/suggestions", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Suggestions []struct {
			Status string `json:"status"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Suggestions) != 1 || body.Suggestions[0].Status != "pending" {
		t.Fatalf("unexpected suggestions: %+v", body.Suggestions)
	}
}

func bytesContainsAny(b []byte, needles ...string) bool {
	for _, n := range needles {
		if bytes.Contains(b, []byte(n)) {
			return true
		}
	}
	return false
}

// --- Accept/Reject endpoint wiring ---

func newAcceptanceTestRouter(t *testing.T) (http.Handler, *Service, *fakeSpaceCreator) {
	t.Helper()
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	spaceCreator := newFakeSpaceCreator()
	svc.SetSpaceCreator(spaceCreator)

	router, api := platformhttp.NewRouter("ai-acceptance-test", "0.0.0")
	authedAPI := huma.NewGroup(api)
	authedAPI.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		principal := identity.Principal{UserID: "user_1", CompanyID: "company_a", Role: "owner"}
		next(huma.WithContext(ctx, identity.ContextWithPrincipal(ctx.Context(), principal)))
	})
	RegisterHandlers(authedAPI, svc)
	return router, svc, spaceCreator
}

func TestHandlerAcceptSpaceSuggestionUnchanged(t *testing.T) {
	router, svc, _ := newAcceptanceTestRouter(t)
	sug := seedPendingSpaceSuggestion(t, svc.repo.(*fakeAIRepo), "company_a", "project_1")

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/accept", map[string]any{
		"expectedRevision": sug.Revision,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}
	var body struct {
		DomainObjectID string `json:"domainObjectId"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.DomainObjectID == "" {
		t.Fatal("expected a created domain object id")
	}
}

func TestHandlerAcceptSpaceSuggestionWrongRevisionReturns409(t *testing.T) {
	router, svc, _ := newAcceptanceTestRouter(t)
	sug := seedPendingSpaceSuggestion(t, svc.repo.(*fakeAIRepo), "company_a", "project_1")

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/accept", map[string]any{
		"expectedRevision": sug.Revision + 99,
	})
	if resp.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerAcceptForeignSuggestionReturns404(t *testing.T) {
	router, svc, _ := newAcceptanceTestRouter(t)
	sug := seedPendingSpaceSuggestion(t, svc.repo.(*fakeAIRepo), "company_b", "project_1")

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/accept", map[string]any{
		"expectedRevision": sug.Revision,
	})
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body %s", resp.Code, resp.Body.String())
	}
}

func TestHandlerAcceptSpaceSuggestionWithOverride(t *testing.T) {
	router, svc, _ := newAcceptanceTestRouter(t)
	sug := seedPendingSpaceSuggestion(t, svc.repo.(*fakeAIRepo), "company_a", "project_1")

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/accept", map[string]any{
		"expectedRevision": sug.Revision,
		"space":            map[string]any{"name": "Master Bathroom", "type": "bathroom", "description": ""},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}

	list, _ := svc.ListSuggestionsByBatch(context.Background(), "company_a", sug.BatchID)
	if len(list) != 1 || list[0].Status != SuggestionStatusModified {
		t.Fatalf("expected modified suggestion, got %+v", list)
	}
}

func TestHandlerRejectSuggestion(t *testing.T) {
	router, svc, _ := newAcceptanceTestRouter(t)
	sug := seedPendingSpaceSuggestion(t, svc.repo.(*fakeAIRepo), "company_a", "project_1")

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/reject", map[string]any{
		"expectedRevision": sug.Revision,
	})
	if resp.Code != http.StatusOK && resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body %s", resp.Code, resp.Body.String())
	}

	updated, err := svc.repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != SuggestionStatusRejected {
		t.Fatalf("expected rejected, got %s", updated.Status)
	}
}

func TestHandlerAcceptRequiresAuthentication(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	svc.SetSpaceCreator(newFakeSpaceCreator())
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	router, api := platformhttp.NewRouter("ai-acceptance-noauth", "0.0.0")
	RegisterHandlers(api, svc)

	resp := doPost(t, router, "/ai-suggestions/"+sug.ID+"/accept", map[string]any{"expectedRevision": sug.Revision})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body %s", resp.Code, resp.Body.String())
	}
}
