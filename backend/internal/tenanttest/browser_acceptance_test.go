package tenanttest_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// corsWrappedRouter builds the real tenanttest router and wraps it with
// platformhttp.WrapCORS exactly as cmd/api/main.go wraps the production
// server's handler — tenanttest.BuildRouter itself does not apply CORS
// (Checkpoint 3 wired it only into main.go), so Checkpoint 13's browser
// journeys apply it here at the test boundary to exercise the real,
// unmodified CORS middleware against the real, unmodified router.
func corsWrappedRouter(t *testing.T) http.Handler {
	t.Helper()
	router := setupRouter(t)
	origins, err := config.NewAllowedOriginsForTest([]string{tenanttest.TestAllowedOrigin})
	if err != nil {
		t.Fatalf("building allowed origins: %v", err)
	}
	return platformhttp.WrapCORS(router, origins)
}

// --- Journey A: browser authentication ---

func TestBrowserAcceptance_JourneyA_Authentication(t *testing.T) {
	router := corsWrappedRouter(t)

	// Step 1+2: allowed-origin registration succeeds with exact credentialed
	// CORS headers.
	registerBody, _ := json.Marshal(map[string]string{
		"email": "owner@journeya.example.com", "password": "password123", "companyName": "Journey A Co",
	})
	registerReq := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerReq.Header.Set("Origin", tenanttest.TestAllowedOrigin)
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	if registerResp.Code != http.StatusOK {
		t.Fatalf("register status = %d, body %s", registerResp.Code, registerResp.Body.String())
	}
	if got := registerResp.Header().Get("Access-Control-Allow-Origin"); got != tenanttest.TestAllowedOrigin {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, tenanttest.TestAllowedOrigin)
	}
	if got := registerResp.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want true", got)
	}

	// Step 3: refresh cookie uses the development-equivalent tenanttest
	// policy (Secure=false — tenanttest.BuildRouter passes secureRefreshCookie=false
	// so httptest's plain HTTP client can exercise the flow, matching
	// Checkpoint 2's development configuration).
	var refreshCookie *http.Cookie
	for _, c := range registerResp.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatalf("no refresh_token cookie in register response: %s", registerResp.Body.String())
	}
	if refreshCookie.Secure {
		t.Fatal("expected Secure=false for tenanttest's development-equivalent cookie policy")
	}
	if refreshCookie.Path != "/auth" || !refreshCookie.HttpOnly {
		t.Fatalf("unexpected cookie attributes: %+v", refreshCookie)
	}

	var registerBodyDecoded struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(registerResp.Body.Bytes(), &registerBodyDecoded); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	// Step 4+5: access token calls GET /auth/me, returns authoritative User,
	// Company, and role.
	meReq := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+registerBodyDecoded.AccessToken)
	meResp := httptest.NewRecorder()
	router.ServeHTTP(meResp, meReq)
	if meResp.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status = %d, body %s", meResp.Code, meResp.Body.String())
	}
	var me struct {
		Email       string `json:"email"`
		CompanyName string `json:"companyName"`
		Role        string `json:"role"`
	}
	if err := json.Unmarshal(meResp.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode /auth/me response: %v", err)
	}
	if me.Email != "owner@journeya.example.com" {
		t.Fatalf("me.Email = %q, want owner@journeya.example.com", me.Email)
	}
	if me.CompanyName != "Journey A Co" {
		t.Fatalf("me.CompanyName = %q, want Journey A Co", me.CompanyName)
	}
	if me.Role != "owner" {
		t.Fatalf("me.Role = %q, want owner", me.Role)
	}

	// Step 6: refresh rotates the cookie from an allowed Origin.
	refreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	refreshReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshCookie.Value})
	refreshReq.Header.Set("Origin", tenanttest.TestAllowedOrigin)
	refreshResp := httptest.NewRecorder()
	router.ServeHTTP(refreshResp, refreshReq)
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body %s", refreshResp.Code, refreshResp.Body.String())
	}
	var rotatedCookie *http.Cookie
	for _, c := range refreshResp.Result().Cookies() {
		if c.Name == "refresh_token" {
			rotatedCookie = c
		}
	}
	if rotatedCookie == nil {
		t.Fatal("no rotated refresh_token cookie in refresh response")
	}
	if rotatedCookie.Value == refreshCookie.Value {
		t.Fatal("expected refresh to rotate the cookie value")
	}

	// Step 7: logout clears the cookie with matching attributes.
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: rotatedCookie.Value})
	logoutReq.Header.Set("Origin", tenanttest.TestAllowedOrigin)
	logoutResp := httptest.NewRecorder()
	router.ServeHTTP(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204, body %s", logoutResp.Code, logoutResp.Body.String())
	}
	var clearedCookie *http.Cookie
	for _, c := range logoutResp.Result().Cookies() {
		if c.Name == "refresh_token" {
			clearedCookie = c
		}
	}
	if clearedCookie == nil {
		t.Fatal("no cleared refresh_token cookie in logout response")
	}
	if clearedCookie.Path != refreshCookie.Path || clearedCookie.HttpOnly != refreshCookie.HttpOnly {
		t.Fatalf("logout cookie attributes do not match issuing cookie: %+v vs %+v", clearedCookie, refreshCookie)
	}
	if clearedCookie.MaxAge >= 0 {
		t.Fatalf("expected logout cookie MaxAge < 0 (expired), got %d", clearedCookie.MaxAge)
	}

	// Step 8: a later refresh (using the now-revoked rotated token) fails
	// with 401.
	laterRefreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	laterRefreshReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: rotatedCookie.Value})
	laterRefreshReq.Header.Set("Origin", tenanttest.TestAllowedOrigin)
	laterRefreshResp := httptest.NewRecorder()
	router.ServeHTTP(laterRefreshResp, laterRefreshReq)
	if laterRefreshResp.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout refresh status = %d, want 401, body %s", laterRefreshResp.Code, laterRefreshResp.Body.String())
	}
}

// --- Journey B: Origin rejection ---

func TestBrowserAcceptance_JourneyB_OriginRejection(t *testing.T) {
	router := corsWrappedRouter(t)
	company := registerCompany(t, router, "owner@journeyb.example.com", "Journey B Co")

	// Get a real refresh cookie to test against, by extracting it from a
	// fresh register call under the allowed origin.
	registerBody, _ := json.Marshal(map[string]string{
		"email": "second@journeyb.example.com", "password": "password123", "companyName": "Journey B Second Co",
	})
	registerReq := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(registerBody))
	registerReq.Header.Set("Content-Type", "application/json")
	registerResp := httptest.NewRecorder()
	router.ServeHTTP(registerResp, registerReq)
	var refreshCookie *http.Cookie
	for _, c := range registerResp.Result().Cookies() {
		if c.Name == "refresh_token" {
			refreshCookie = c
		}
	}
	if refreshCookie == nil {
		t.Fatalf("no refresh_token cookie: %s", registerResp.Body.String())
	}

	// Step 1: disallowed preflight is rejected.
	preflightReq := httptest.NewRequest(http.MethodOptions, "/clients", nil)
	preflightReq.Header.Set("Origin", "http://evil.example.com")
	preflightReq.Header.Set("Access-Control-Request-Method", "GET")
	preflightResp := httptest.NewRecorder()
	router.ServeHTTP(preflightResp, preflightReq)
	if preflightResp.Code != http.StatusForbidden {
		t.Fatalf("disallowed preflight status = %d, want 403", preflightResp.Code)
	}

	// Step 2: disallowed refresh is rejected.
	disallowedRefreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	disallowedRefreshReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshCookie.Value})
	disallowedRefreshReq.Header.Set("Origin", "http://evil.example.com")
	disallowedRefreshResp := httptest.NewRecorder()
	router.ServeHTTP(disallowedRefreshResp, disallowedRefreshReq)
	if disallowedRefreshResp.Code != http.StatusForbidden {
		t.Fatalf("disallowed-origin refresh status = %d, want 403, body %s", disallowedRefreshResp.Code, disallowedRefreshResp.Body.String())
	}

	// Step 3: Origin-absent non-browser refresh is allowed (no Origin, no
	// Sec-Fetch-Site — treated as a non-browser client).
	nonBrowserRefreshReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	nonBrowserRefreshReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: refreshCookie.Value})
	nonBrowserRefreshResp := httptest.NewRecorder()
	router.ServeHTTP(nonBrowserRefreshResp, nonBrowserRefreshReq)
	if nonBrowserRefreshResp.Code != http.StatusOK {
		t.Fatalf("origin-absent non-browser refresh status = %d, want 200, body %s", nonBrowserRefreshResp.Code, nonBrowserRefreshResp.Body.String())
	}
	var rotated struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(nonBrowserRefreshResp.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decode refresh response: %v", err)
	}
	var rotatedCookie *http.Cookie
	for _, c := range nonBrowserRefreshResp.Result().Cookies() {
		if c.Name == "refresh_token" {
			rotatedCookie = c
		}
	}
	if rotatedCookie == nil {
		t.Fatal("no rotated refresh_token cookie")
	}

	// Step 4: Origin-absent cross-site browser refresh is rejected
	// (Sec-Fetch-Site: cross-site with no Origin header).
	crossSiteReq := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	crossSiteReq.AddCookie(&http.Cookie{Name: "refresh_token", Value: rotatedCookie.Value})
	crossSiteReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSiteResp := httptest.NewRecorder()
	router.ServeHTTP(crossSiteResp, crossSiteReq)
	if crossSiteResp.Code != http.StatusForbidden {
		t.Fatalf("origin-absent cross-site refresh status = %d, want 403, body %s", crossSiteResp.Code, crossSiteResp.Body.String())
	}

	_ = company // referenced for symmetry with other journeys; not directly asserted here.
}

// --- Journey C: paginated project setup ---

func TestBrowserAcceptance_JourneyC_PaginatedProjectSetup(t *testing.T) {
	router := setupRouter(t)
	company := registerCompany(t, router, "owner@journeyc.example.com", "Journey C Co")

	// Step 1: create multiple Clients.
	clientNames := []string{"Ahmad Rahman", "Siti Aminah", "Bala Krishnan"}
	clientIDs := make([]string, 0, len(clientNames))
	for _, name := range clientNames {
		rec := doJSON(t, router, http.MethodPost, "/clients", company.accessToken, map[string]string{"name": name})
		if rec.Code != http.StatusOK {
			t.Fatalf("create client %q status = %d, body %s", name, rec.Code, rec.Body.String())
		}
		clientIDs = append(clientIDs, mustField(t, rec, "id"))
	}

	// Step 2: search and paginate Clients.
	listRec := doJSON(t, router, http.MethodGet, "/clients?search=Ahmad&page=1&pageSize=25", company.accessToken, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list clients status = %d, body %s", listRec.Code, listRec.Body.String())
	}
	var clientList pagination.Response[map[string]any]
	if err := json.Unmarshal(listRec.Body.Bytes(), &clientList); err != nil {
		t.Fatalf("decode client list: %v", err)
	}
	if clientList.Total != 1 {
		t.Fatalf("search 'Ahmad' total = %d, want 1", clientList.Total)
	}

	// Step 3: partially update one Client without clearing omitted values.
	patchRec := doJSON(t, router, http.MethodPatch, "/clients/"+clientIDs[0], company.accessToken, map[string]string{"phone": "0123456789"})
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch client status = %d, body %s", patchRec.Code, patchRec.Body.String())
	}
	var patchedClient struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	}
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patchedClient); err != nil {
		t.Fatalf("decode patched client: %v", err)
	}
	if patchedClient.Name != "Ahmad Rahman" {
		t.Fatalf("patched client Name = %q, want unchanged Ahmad Rahman", patchedClient.Name)
	}
	if patchedClient.Phone != "0123456789" {
		t.Fatalf("patched client Phone = %q, want 0123456789", patchedClient.Phone)
	}

	// Step 4: create multiple Projects.
	projectRec1 := doJSON(t, router, http.MethodPost, "/projects", company.accessToken, map[string]string{"clientId": clientIDs[0], "name": "Bathroom Reno"})
	if projectRec1.Code != http.StatusOK {
		t.Fatalf("create project 1 status = %d, body %s", projectRec1.Code, projectRec1.Body.String())
	}
	project1ID := mustField(t, projectRec1, "id")

	projectRec2 := doJSON(t, router, http.MethodPost, "/projects", company.accessToken, map[string]string{"clientId": clientIDs[1], "name": "Kitchen Remodel"})
	if projectRec2.Code != http.StatusOK {
		t.Fatalf("create project 2 status = %d, body %s", projectRec2.Code, projectRec2.Body.String())
	}

	// Step 5: filter Projects by Client.
	byClientRec := doJSON(t, router, http.MethodGet, "/projects?clientId="+clientIDs[0], company.accessToken, nil)
	if byClientRec.Code != http.StatusOK {
		t.Fatalf("filter projects by client status = %d, body %s", byClientRec.Code, byClientRec.Body.String())
	}
	var projectsByClient pagination.Response[map[string]any]
	if err := json.Unmarshal(byClientRec.Body.Bytes(), &projectsByClient); err != nil {
		t.Fatalf("decode projects by client: %v", err)
	}
	if projectsByClient.Total != 1 {
		t.Fatalf("projects filtered by client total = %d, want 1", projectsByClient.Total)
	}

	// Step 6: search and sort Projects.
	searchSortRec := doJSON(t, router, http.MethodGet, "/projects?search=Kitchen&sort=name&order=asc", company.accessToken, nil)
	if searchSortRec.Code != http.StatusOK {
		t.Fatalf("search+sort projects status = %d, body %s", searchSortRec.Code, searchSortRec.Body.String())
	}
	var searchedProjects pagination.Response[map[string]any]
	if err := json.Unmarshal(searchSortRec.Body.Bytes(), &searchedProjects); err != nil {
		t.Fatalf("decode searched projects: %v", err)
	}
	if searchedProjects.Total != 1 {
		t.Fatalf("search 'Kitchen' total = %d, want 1", searchedProjects.Total)
	}

	// Step 7: rename one Project.
	renameRec := doJSON(t, router, http.MethodPatch, "/projects/"+project1ID, company.accessToken, map[string]string{"name": "Master Bathroom Reno"})
	if renameRec.Code != http.StatusOK {
		t.Fatalf("rename project status = %d, body %s", renameRec.Code, renameRec.Body.String())
	}
	var renamedProject struct {
		Name     string `json:"name"`
		ClientID string `json:"clientId"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(renameRec.Body.Bytes(), &renamedProject); err != nil {
		t.Fatalf("decode renamed project: %v", err)
	}
	if renamedProject.Name != "Master Bathroom Reno" {
		t.Fatalf("renamed project Name = %q, want Master Bathroom Reno", renamedProject.Name)
	}
	if renamedProject.ClientID != clientIDs[0] {
		t.Fatalf("renamed project ClientID = %q, want unchanged %q", renamedProject.ClientID, clientIDs[0])
	}

	// Step 8: create Property and Spaces.
	propRec := doJSON(t, router, http.MethodPost, "/properties", company.accessToken, map[string]string{"projectId": project1ID, "address": "123 Jalan Test"})
	if propRec.Code != http.StatusOK {
		t.Fatalf("create property status = %d, body %s", propRec.Code, propRec.Body.String())
	}

	spaceNames := []string{"Master Bathroom", "Guest Bathroom", "Powder Room"}
	spaceIDs := make([]string, 0, len(spaceNames))
	for _, name := range spaceNames {
		rec := doJSON(t, router, http.MethodPost, "/spaces", company.accessToken, map[string]string{
			"projectId": project1ID, "name": name, "type": "bathroom",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("create space %q status = %d, body %s", name, rec.Code, rec.Body.String())
		}
		spaceIDs = append(spaceIDs, mustField(t, rec, "id"))
	}

	// Step 9: paginate Spaces.
	spacesPageRec := doJSON(t, router, http.MethodGet, "/spaces?projectId="+project1ID+"&page=1&pageSize=2", company.accessToken, nil)
	if spacesPageRec.Code != http.StatusOK {
		t.Fatalf("paginate spaces status = %d, body %s", spacesPageRec.Code, spacesPageRec.Body.String())
	}
	var spacesPage pagination.Response[map[string]any]
	if err := json.Unmarshal(spacesPageRec.Body.Bytes(), &spacesPage); err != nil {
		t.Fatalf("decode spaces page: %v", err)
	}
	if spacesPage.Total != 3 {
		t.Fatalf("spaces total = %d, want 3", spacesPage.Total)
	}
	if len(spacesPage.Items) != 2 {
		t.Fatalf("spaces page items = %d, want 2 (pageSize)", len(spacesPage.Items))
	}

	// Step 10: create Work Items.
	workItemRec1 := doJSON(t, router, http.MethodPost, "/work-items", company.accessToken, map[string]string{
		"projectId": project1ID, "spaceId": spaceIDs[0], "description": "Install ceramic tiles",
		"workType": "tiling", "quantityValue": "30", "quantityUnit": "m2",
	})
	if workItemRec1.Code != http.StatusOK {
		t.Fatalf("create work item 1 status = %d, body %s", workItemRec1.Code, workItemRec1.Body.String())
	}
	workItem1ID := mustField(t, workItemRec1, "id")

	workItemRec2 := doJSON(t, router, http.MethodPost, "/work-items", company.accessToken, map[string]string{
		"projectId": project1ID, "description": "Paint walls",
		"workType": "painting", "quantityValue": "50", "quantityUnit": "m2",
	})
	if workItemRec2.Code != http.StatusOK {
		t.Fatalf("create work item 2 status = %d, body %s", workItemRec2.Code, workItemRec2.Body.String())
	}

	// Step 11: paginate and search Work Items.
	workItemsSearchRec := doJSON(t, router, http.MethodGet, "/work-items?projectId="+project1ID+"&search=tiling", company.accessToken, nil)
	if workItemsSearchRec.Code != http.StatusOK {
		t.Fatalf("search work items status = %d, body %s", workItemsSearchRec.Code, workItemsSearchRec.Body.String())
	}
	var workItemsSearch pagination.Response[map[string]any]
	if err := json.Unmarshal(workItemsSearchRec.Body.Bytes(), &workItemsSearch); err != nil {
		t.Fatalf("decode work items search: %v", err)
	}
	if workItemsSearch.Total != 1 {
		t.Fatalf("search 'tiling' total = %d, want 1", workItemsSearch.Total)
	}

	// Step 12: edit description, quantity, work type, and Space.
	editRec := doJSON(t, router, http.MethodPatch, "/work-items/"+workItem1ID, company.accessToken, map[string]any{
		"description": "Install premium ceramic tiles", "quantityValue": "35", "quantityUnit": "m2",
		"workType": "premium_tiling", "spaceId": spaceIDs[1],
	})
	if editRec.Code != http.StatusOK {
		t.Fatalf("edit work item status = %d, body %s", editRec.Code, editRec.Body.String())
	}
	var editedWorkItem struct {
		Description   string `json:"description"`
		QuantityValue string `json:"quantityValue"`
		QuantityUnit  string `json:"quantityUnit"`
		WorkType      string `json:"workType"`
		SpaceID       string `json:"spaceId"`
	}
	if err := json.Unmarshal(editRec.Body.Bytes(), &editedWorkItem); err != nil {
		t.Fatalf("decode edited work item: %v", err)
	}
	if editedWorkItem.Description != "Install premium ceramic tiles" {
		t.Fatalf("edited Description = %q", editedWorkItem.Description)
	}
	if editedWorkItem.QuantityValue != "35" || editedWorkItem.QuantityUnit != "m2" {
		t.Fatalf("edited Quantity = %s %s, want 35 m2", editedWorkItem.QuantityValue, editedWorkItem.QuantityUnit)
	}
	if editedWorkItem.WorkType != "premium_tiling" {
		t.Fatalf("edited WorkType = %q", editedWorkItem.WorkType)
	}
	if editedWorkItem.SpaceID != spaceIDs[1] {
		t.Fatalf("edited SpaceID = %q, want %q", editedWorkItem.SpaceID, spaceIDs[1])
	}

	// Step 13: clear Space using null.
	clearSpaceRec := doJSON(t, router, http.MethodPatch, "/work-items/"+workItem1ID, company.accessToken, map[string]any{"spaceId": nil})
	if clearSpaceRec.Code != http.StatusOK {
		t.Fatalf("clear space status = %d, body %s", clearSpaceRec.Code, clearSpaceRec.Body.String())
	}
	var clearedWorkItem struct {
		SpaceID string `json:"spaceId"`
	}
	if err := json.Unmarshal(clearSpaceRec.Body.Bytes(), &clearedWorkItem); err != nil {
		t.Fatalf("decode cleared work item: %v", err)
	}
	if clearedWorkItem.SpaceID != "" {
		t.Fatalf("cleared SpaceID = %q, want empty", clearedWorkItem.SpaceID)
	}

	// Step 14: cancel the Work Item.
	cancelRec := doJSON(t, router, http.MethodPatch, "/work-items/"+workItem1ID+"/status", company.accessToken, map[string]string{"status": "cancelled"})
	if cancelRec.Code != http.StatusOK {
		t.Fatalf("cancel work item status = %d, body %s", cancelRec.Code, cancelRec.Body.String())
	}

	// Step 15: confirm editing the cancelled Work Item returns 409.
	editCancelledRec := doJSON(t, router, http.MethodPatch, "/work-items/"+workItem1ID, company.accessToken, map[string]any{"description": "Should not apply"})
	if editCancelledRec.Code != http.StatusConflict {
		t.Fatalf("edit cancelled work item status = %d, want 409, body %s", editCancelledRec.Code, editCancelledRec.Body.String())
	}
}

// --- Journey D: tenant isolation ---

func TestBrowserAcceptance_JourneyD_TenantIsolation(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "owner@journeyd-a.example.com", "Journey D Co A")
	companyB := registerCompany(t, router, "owner@journeyd-b.example.com", "Journey D Co B")

	// Seed a Client under Company A only.
	createRec := doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Company A Only Client"})
	if createRec.Code != http.StatusOK {
		t.Fatalf("create client status = %d, body %s", createRec.Code, createRec.Body.String())
	}
	clientID := mustField(t, createRec, "id")

	// Totals exclude foreign records: Company B's paginated list must not
	// see Company A's client.
	listRec := doJSON(t, router, http.MethodGet, "/clients", companyB.accessToken, nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list clients (company B) status = %d, body %s", listRec.Code, listRec.Body.String())
	}
	var listB pagination.Response[map[string]any]
	if err := json.Unmarshal(listRec.Body.Bytes(), &listB); err != nil {
		t.Fatalf("decode company B client list: %v", err)
	}
	if listB.Total != 0 {
		t.Fatalf("company B client total = %d, want 0 (must exclude company A's client)", listB.Total)
	}

	// Search excludes foreign records.
	searchRec := doJSON(t, router, http.MethodGet, "/clients?search=Company+A+Only", companyB.accessToken, nil)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search clients (company B) status = %d, body %s", searchRec.Code, searchRec.Body.String())
	}
	var searchB pagination.Response[map[string]any]
	if err := json.Unmarshal(searchRec.Body.Bytes(), &searchB); err != nil {
		t.Fatalf("decode company B client search: %v", err)
	}
	if searchB.Total != 0 {
		t.Fatalf("company B search total = %d, want 0", searchB.Total)
	}

	// Direct updates return neutral not-found behavior for a cross-tenant
	// patch attempt.
	patchRec := doJSON(t, router, http.MethodPatch, "/clients/"+clientID, companyB.accessToken, map[string]string{"phone": "0199999999"})
	if patchRec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant patch status = %d, want 404, body %s", patchRec.Code, patchRec.Body.String())
	}

	// Request IDs and privacy headers are still present on errors.
	if got := patchRec.Header().Get("X-Request-ID"); got == "" {
		t.Error("expected X-Request-ID on the 404 error response")
	}
	if got := patchRec.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Errorf("Cache-Control on error response = %q, want no-store, max-age=0", got)
	}
}
