package tenanttest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// testCompany holds one registered company's credentials and access token,
// used to authenticate requests as that company throughout the suite.
type testCompany struct {
	accessToken string
}

func setupRouter(t *testing.T) http.Handler {
	t.Helper()
	router, _ := setupRouterWithDatabase(t)
	return router
}

// setupRouterWithDatabase additionally exposes the database, which the Phase H
// acceptance journeys need in order to assert PERSISTED results through owning
// repositories rather than inferring storage from an HTTP response.
func setupRouterWithDatabase(t *testing.T) (http.Handler, *mongo.Database) {
	t.Helper()
	router, db, _ := setupRouterWithMail(t)
	return router, db
}

// recordingMailer accepts every message and keeps it in memory.
//
// The Phase H journeys need delivery to SUCCEED — an invitation is activated
// only by a successful send — and they assert that no raw credential reaches a
// message body.
type recordingMailer struct {
	mu       sync.Mutex
	messages []mail.Message
	fail     error
}

func (m *recordingMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.messages = append(m.messages, msg)
	return nil
}

// failNext makes every subsequent send fail, so a test can exercise the
// bounded delivery-failure path.
func (m *recordingMailer) failNext(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *recordingMailer) sent() []mail.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mail.Message(nil), m.messages...)
}

func setupRouterWithMail(t *testing.T) (
	http.Handler, *mongo.Database, *recordingMailer) {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7", mongodb.WithReplicaSet("rs0"))
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	// Keep discovery on Docker's mapped Windows endpoint; the single member's
	// advertised container address is not host-routable.
	connStr += "&directConnection=true"

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	db := platformmongo.Database(client, "tenanttest")
	mailer := &recordingMailer{}
	router, err := tenanttest.BuildRouterWithMailer(t, db, mailer)
	if err != nil {
		t.Fatalf("failed to build router: %v", err)
	}
	return router, db, mailer
}

func registerCompany(t *testing.T, router http.Handler, email, companyName string) testCompany {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"email": email, "password": "password123", "companyName": companyName,
	})
	req := httptest.NewRequest(http.MethodPost, "/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 registering %s, got %d: %s", email, rec.Code, rec.Body.String())
	}
	var resp struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse register response: %v", err)
	}
	return testCompany{accessToken: resp.AccessToken}
}

func doJSON(t *testing.T, router http.Handler, method, path, accessToken string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func mustField(t *testing.T, rec *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %s: %v", rec.Body.String(), err)
	}
	v, ok := resp[field].(string)
	if !ok {
		t.Fatalf("expected string field %q in response %s", field, rec.Body.String())
	}
	return v
}

// buildFullHierarchyForCompanyA creates Client -> Project -> Property -> Space ->
// WorkItem, all owned by companyA, and returns each resource's ID.
func buildFullHierarchyForCompanyA(t *testing.T, router http.Handler, companyA testCompany) (clientID, projectID, propertyID, spaceID, workItemID string) {
	t.Helper()

	rec := doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Ahmad"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating client, got %d: %s", rec.Code, rec.Body.String())
	}
	clientID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID, "name": "Ahmad Residence"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating project, got %d: %s", rec.Code, rec.Body.String())
	}
	projectID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/properties", companyA.accessToken, map[string]string{"projectId": projectID, "address": "123 Street"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating property, got %d: %s", rec.Code, rec.Body.String())
	}
	propertyID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/spaces", companyA.accessToken, map[string]string{"projectId": projectID, "name": "Master Bathroom", "type": "bathroom"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating space, got %d: %s", rec.Code, rec.Body.String())
	}
	spaceID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/work-items", companyA.accessToken, map[string]string{
		"projectId": projectID, "spaceId": spaceID, "description": "Install ceramic floor tiles",
		"workType": "tile_installation", "quantityValue": "30", "quantityUnit": "m2",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating work item, got %d: %s", rec.Code, rec.Body.String())
	}
	workItemID = mustField(t, rec, "id")

	return clientID, projectID, propertyID, spaceID, workItemID
}

func TestTenantIsolation_ClientCrossTenantAccessReturns404(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA@example.com", "Company A")
	companyB := registerCompany(t, router, "ownerB@example.com", "Company B")

	clientID, _, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/clients/"+clientID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's client, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/clients/"+clientID, companyB.accessToken, map[string]string{"name": "Hijacked"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B updating Company A's client, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_ProjectCrossTenantAccessAndForeignClientRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA2@example.com", "Company A2")
	companyB := registerCompany(t, router, "ownerB2@example.com", "Company B2")

	clientID, projectID, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company B tries to create a Project referencing Company A's real Client ID.
	rec = doJSON(t, router, http.MethodPost, "/projects", companyB.accessToken, map[string]string{"clientId": clientID, "name": "Stolen"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B creating project under Company A's client, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_PropertyCrossTenantAccessAndForeignProjectRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA3@example.com", "Company A3")
	companyB := registerCompany(t, router, "ownerB3@example.com", "Company B3")

	_, projectID, propertyID, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/properties/"+propertyID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's property, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/properties", companyB.accessToken, map[string]string{"projectId": projectID, "address": "Stolen"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B creating property under Company A's project, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_PropertyUniquenessReturns409ForSameCompany(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA3b@example.com", "Company A3b")

	_, projectID, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/properties", companyA.accessToken, map[string]string{"projectId": projectID, "address": "Second Property"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 creating a second property for the same project, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_SpaceCrossTenantAccessAndForeignProjectRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA4@example.com", "Company A4")
	companyB := registerCompany(t, router, "ownerB4@example.com", "Company B4")

	_, projectID, _, spaceID, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/spaces/"+spaceID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's space, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/spaces", companyB.accessToken, map[string]string{"projectId": projectID, "name": "Stolen"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B creating space under Company A's project, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_WorkItemCrossTenantAccessAndForeignParentsRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA5@example.com", "Company A5")
	companyB := registerCompany(t, router, "ownerB5@example.com", "Company B5")

	_, projectID, _, spaceID, workItemID := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/work-items/"+workItemID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's work item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/work-items", companyB.accessToken, map[string]string{
		"projectId": projectID, "description": "Stolen", "quantityValue": "1", "quantityUnit": "unit",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B creating work item under Company A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/work-items", companyB.accessToken, map[string]string{
		"projectId": projectID, "spaceId": spaceID, "description": "Stolen via space",
		"quantityValue": "1", "quantityUnit": "unit",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B creating work item under Company A's project+space, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_WorkItemRejectsSpaceFromDifferentProjectSameTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA6@example.com", "Company A6")

	clientID, project1ID, _, space1ID, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	// Create a second, independent Project (and no Property/Space needed) for the
	// same Company A — this is the lineage-mismatch scenario: same tenant, Space
	// really belongs to project1, but the WorkItem claims project2.
	rec := doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID, "name": "Second Project"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating second project, got %d: %s", rec.Code, rec.Body.String())
	}
	project2ID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/work-items", companyA.accessToken, map[string]string{
		"projectId": project2ID, "spaceId": space1ID, "description": "Mismatched lineage",
		"quantityValue": "1", "quantityUnit": "unit",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a Space/Project lineage mismatch within the same tenant, got %d: %s", rec.Code, rec.Body.String())
	}
	_ = project1ID
}

func TestTenantIsolation_IDSubstitutionNeverCrossesTenantBoundary(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA7@example.com", "Company A7")
	companyB := registerCompany(t, router, "ownerB7@example.com", "Company B7")

	_, projectID, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	realIDResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyB.accessToken, nil)
	randomIDResp := doJSON(t, router, http.MethodGet, "/projects/000000000000000000000000", companyB.accessToken, nil)

	if realIDResp.Code != randomIDResp.Code {
		t.Fatalf("expected identical status for real foreign ID (%d) and random ID (%d)", realIDResp.Code, randomIDResp.Code)
	}
	if realIDResp.Code != http.StatusNotFound {
		t.Fatalf("expected both to be 404, got %d", realIDResp.Code)
	}
}

func TestTenantIsolation_ListEndpointsScopedToOwnCompanyOnly(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA8@example.com", "Company A8")
	companyB := registerCompany(t, router, "ownerB8@example.com", "Company B8")

	buildFullHierarchyForCompanyA(t, router, companyA)
	// Company B has its own, separate hierarchy of the same resource types.
	buildFullHierarchyForCompanyA(t, router, companyB)

	rec := doJSON(t, router, http.MethodGet, "/clients", companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 listing clients, got %d: %s", rec.Code, rec.Body.String())
	}
	// GET /clients returns the canonical paginated envelope
	// ({items, page, pageSize, total}), not the pre-Checkpoint-8 bare
	// {"clients": [...]} array — an intentional breaking replacement.
	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if resp.Total != 1 {
		t.Fatalf("expected Company A's client total to be 1 (its own), got %d", resp.Total)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected Company A to see exactly 1 client (its own), got %d", len(resp.Items))
	}
}

func TestTenantIsolation_ListByParentReturns404ForForeignParentNotEmptyList(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA9@example.com", "Company A9")
	companyB := registerCompany(t, router, "ownerB9@example.com", "Company B9")

	_, projectID, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodGet, "/properties?projectId="+projectID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (not an empty list) for Company B listing Properties of Company A's Project, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_CompanyIDInRequestBodyIsIgnored(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA10@example.com", "Company A10")
	companyB := registerCompany(t, router, "ownerB10@example.com", "Company B10")
	_ = companyA

	// Company B sends a request body containing an extraneous companyId field
	// spoofing a different company. Design spec §16: either outcome is
	// acceptable — (a) rejected as invalid input (unknown field), or (b) it
	// succeeds and the created resource's companyId is Company B's. The
	// invariant under test is that Company A's id is never used, not which of
	// (a)/(b) occurs — Huma's strict unknown-field rejection produces (a) here.
	body := map[string]any{"name": "Spoofed", "companyId": "some-other-company-id"}
	rec := doJSON(t, router, http.MethodPost, "/clients", companyB.accessToken, body)

	if rec.Code == http.StatusUnprocessableEntity {
		// Outcome (a): rejected as invalid input. Nothing was created under any
		// company, so the negative invariant holds trivially.
		return
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected either 200 (companyId field ignored) or 422 (unknown field rejected), got %d: %s", rec.Code, rec.Body.String())
	}

	// Outcome (b): succeeded — verify the created resource belongs to Company B
	// (the authenticated caller), never to the spoofed companyId.
	createdID := mustField(t, rec, "id")

	listRec := doJSON(t, router, http.MethodGet, "/clients", companyB.accessToken, nil)
	var resp struct {
		Clients []map[string]any `json:"clients"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	found := false
	for _, c := range resp.Clients {
		if c["id"] == createdID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the created client to belong to Company B (the authenticated caller), not the spoofed companyId")
	}
}

// buildM3ResourcesForCompanyA creates a Material, a Worker, a LabourEntry
// (under projectID/workItemID), and a CostItem (under projectID), all owned
// by companyA, and returns each resource's ID plus the LabourEntry's linked
// CostItemID.
func buildM3ResourcesForCompanyA(t *testing.T, router http.Handler, companyA testCompany, projectID, workItemID string) (materialID, workerID, labourEntryID, costItemID, labourCostItemID string) {
	t.Helper()

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "OPC Cement 50kg", "category": "cement", "unit": "bag",
		"referencePriceAmount": 1850, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material, got %d: %s", rec.Code, rec.Body.String())
	}
	materialID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "trade": "Tiler", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating worker, got %d: %s", rec.Code, rec.Body.String())
	}
	workerID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating labour entry, got %d: %s", rec.Code, rec.Body.String())
	}
	labourEntryID = mustField(t, rec, "id")
	labourCostItemID = mustField(t, rec, "costItemId")

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "category": "permit", "description": "Renovation permit",
		"estimatedAmount": 150000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	costItemID = mustField(t, rec, "id")

	return materialID, workerID, labourEntryID, costItemID, labourCostItemID
}

func TestTenantIsolation_Material(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "matA@example.com", "Company A")
	companyB := registerCompany(t, router, "matB@example.com", "Company B")

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "Ceramic Tile", "unit": "m2", "referencePriceAmount": 3200, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material, got %d: %s", rec.Code, rec.Body.String())
	}
	materialID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodGet, "/materials/"+materialID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's material, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/materials/"+materialID, companyB.accessToken, map[string]any{
		"name": "hijacked", "unit": "m2", "referencePriceAmount": 1, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's material, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_Worker(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "workerA@example.com", "Company A")
	companyB := registerCompany(t, router, "workerB@example.com", "Company B")

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating worker, got %d: %s", rec.Code, rec.Body.String())
	}
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodGet, "/workers/"+workerID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's worker, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/workers/"+workerID, companyB.accessToken, map[string]any{
		"name": "hijacked", "rateType": "daily", "defaultRateAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's worker, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_LabourEntry(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "labourA@example.com", "Company A")
	companyB := registerCompany(t, router, "labourB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	_, workerA, labourEntryA, _, _ := buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/labour-entries/"+labourEntryA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/labour-entries/"+labourEntryA, companyB.accessToken, map[string]any{"notes": "hijacked"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
		"workerName": "X", "trade": "Y", "rateAmount": 1,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating labour entry against A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "workerId": workerA,
		"quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating labour entry with A's workerId, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_CostItem(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "costA@example.com", "Company A")
	companyB := registerCompany(t, router, "costB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	materialA, _, _, costItemA, _ := buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/cost-items/"+costItemA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's cost item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemA, companyB.accessToken, map[string]any{"description": "hijacked", "expectedRevision": 0})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's cost item, got %d: %s", rec.Code, rec.Body.String())
	}

	t.Run("record actual", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost, "/cost-items/"+costItemA+"/record-actual", companyB.accessToken,
			map[string]any{"expectedRevision": 0, "amount": 8500})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	t.Run("correct actual", func(t *testing.T) {
		resp := doJSON(t, router, http.MethodPost, "/cost-items/"+costItemA+"/correct-actual", companyB.accessToken,
			map[string]any{"expectedRevision": 0, "amount": 8500, "reason": "attempted cross-tenant"})
		if resp.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for company B, got %d: %s", resp.Code, resp.Body.String())
		}
	})

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "category": "permit", "description": "X", "estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "category": "permit", "description": "X", "estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's work item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "category": "material", "description": "X", "estimatedAmount": 1, "currency": "MYR",
		"materialId": materialA,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's material, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3ParentFilteredListsRejectForeignParent(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "listA@example.com", "Company A")
	companyB := registerCompany(t, router, "listB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/labour-entries?projectId="+projectA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing labour-entries with A's projectId, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/labour-entries?workItemId="+workItemA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing labour-entries with A's workItemId, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items?projectId="+projectA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing cost-items with A's projectId, got %d: %s", rec.Code, rec.Body.String())
	}

	// Explicitly required by review: both list-by-workItemId endpoints must
	// reject a foreign workItemId with 404, not just labour-entries.
	rec = doJSON(t, router, http.MethodGet, "/cost-items?workItemId="+workItemA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing cost-items with A's workItemId, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CompanyIDInRequestBodyIsIgnored(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "spoofA@example.com", "Company A")
	companyB := registerCompany(t, router, "spoofB@example.com", "Company B")

	_, projectB, _, _, workItemB := buildFullHierarchyForCompanyA(t, router, companyB)

	rec := doJSON(t, router, http.MethodPost, "/materials", companyB.accessToken, map[string]any{
		"name": "Spoofed", "unit": "m2", "referencePriceAmount": 1, "referencePriceCurrency": "MYR",
		"companyId": "should-be-ignored",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 (companyId silently ignored) or 422 (unknown field rejected), got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		materialID := mustField(t, rec, "id")
		checkRec := doJSON(t, router, http.MethodGet, "/materials/"+materialID, companyA.accessToken, nil)
		if checkRec.Code != http.StatusNotFound {
			t.Fatal("spoofed companyId must never let Company A see Company B's material")
		}
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectB, "workItemId": workItemB, "category": "permit", "description": "X",
		"estimatedAmount": 1, "currency": "MYR", "companyId": "should-be-ignored",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 or 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3WorkItemProjectLineageMismatch(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "lineageA@example.com", "Company A")

	_, _, _, _, workItemA1 := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Second Client"})
	clientID2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID2, "name": "Second Project"})
	projectA2 := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA1, // real WorkItem, but belongs to a different Project
		"quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
		"workerName": "X", "trade": "Y", "rateAmount": 1,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for mismatched Project/WorkItem lineage in labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA1, "category": "permit", "description": "X",
		"estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for mismatched Project/WorkItem lineage in cost item, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3WorkerReusableAcrossProjects(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "reuseA@example.com", "Company A")

	_, projectA1, _, _, workItemA1 := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA1, "workItemId": workItemA1, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 using the same worker on project A1, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Second Client"})
	clientID2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID2, "name": "Second Project"})
	projectA2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/work-items", companyA.accessToken, map[string]string{
		"projectId": projectA2, "description": "Second work item", "quantityValue": "1", "quantityUnit": "unit",
	})
	workItemA2 := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA2, "workerId": workerID,
		"quantityValue": "4", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 reusing the same worker on project A2, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3WorkerDefaultRateChangeDoesNotAffectExistingLabourEntry(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "snapshotA@example.com", "Company A")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	labourEntryID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/workers/"+workerID, companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 30000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 updating worker rate, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/labour-entries/"+labourEntryID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching labour entry, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	rateField := resp["rate"].(map[string]any)
	if int64(rateField["amount"].(float64)) != 15000 {
		t.Fatalf("expected LabourEntry.Rate to remain 15000 after Worker.DefaultRate changed, got %v", rateField["amount"])
	}
}

func TestTenantIsolation_M3MaterialReferencePriceChangeDoesNotAffectExistingCostItem(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "matsnapshotA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "Tile", "unit": "m2", "referencePriceAmount": 3200, "referencePriceCurrency": "MYR",
	})
	materialID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "material", "description": "Tiles", "materialId": materialID,
		"quantityValue": "10", "quantityUnit": "m2", "unitPriceAmount": 3200, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	costItemID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/materials/"+materialID, companyA.accessToken, map[string]any{
		"name": "Tile", "unit": "m2", "referencePriceAmount": 5000, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 updating material, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items/"+costItemID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	unitPriceField := resp["unitPrice"].(map[string]any)
	if int64(unitPriceField["amount"].(float64)) != 3200 {
		t.Fatalf("expected CostItem.UnitPrice to remain 3200 after Material.ReferencePrice changed, got %v", unitPriceField["amount"])
	}
}

func TestTenantIsolation_M3CostItemRejectsLabourCategory(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ledgerA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "labour", "description": "Should be rejected",
		"estimatedAmount": 1000, "currency": "MYR",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for category=labour via POST /cost-items, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CostItemCategoryLockOnceCommitted(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "lockA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "equipment", "description": "Tile cutter rental",
		"estimatedAmount": 50000, "currency": "MYR",
	})
	costItemID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID, companyA.accessToken, map[string]any{
		"description": "Tile cutter rental", "category": "transport", "expectedRevision": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 correcting category pre-commitment, got %d: %s", rec.Code, rec.Body.String())
	}
	revision := mustNumberField(t, rec, "revision")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken, map[string]any{
		"stage": "committed", "amount": 50000, "expectedRevision": revision,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 setting committed, got %d: %s", rec.Code, rec.Body.String())
	}
	revision = mustNumberField(t, rec, "revision")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID, companyA.accessToken, map[string]any{
		"description": "Tile cutter rental", "category": "equipment", "expectedRevision": revision,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 correcting category after committed is set, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CostItemCategoryCannotChangeToOrFromLabour(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "labourlockA@example.com", "Company A")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)

	// Direction 1: a manually-created CostItem may never be recategorized
	// into labour.
	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "equipment", "description": "Tile cutter rental",
		"estimatedAmount": 50000, "currency": "MYR",
	})
	costItemID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID, companyA.accessToken, map[string]any{
		"description": "Tile cutter rental", "category": "labour", "expectedRevision": 0,
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 changing category to labour, got %d: %s", rec.Code, rec.Body.String())
	}

	// Direction 2: a labour-generated CostItem (via LabourEntry) may never be
	// recategorized away from labour.
	rec = doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	workerID := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	labourCostItemID := mustField(t, rec, "costItemId")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+labourCostItemID, companyA.accessToken, map[string]any{
		"description": "Labour cost", "category": "transport", "expectedRevision": 0,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 changing a labour CostItem's category away from labour, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CostItemLifecycleAllFourCoexist(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "coexistA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "miscellaneous", "description": "Demo",
		"estimatedAmount": 100000, "currency": "MYR",
	})
	costItemID := mustField(t, rec, "id")
	revision := mustNumberField(t, rec, "revision")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken, map[string]any{
		"stage": "committed", "amount": 90000, "expectedRevision": revision,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 setting committed, got %d: %s", rec.Code, rec.Body.String())
	}
	revision = mustNumberField(t, rec, "revision")

	// Actual has its own dedicated entry point — the general-purpose
	// lifecycle route rejects stage=actual outright (it can never bypass the
	// Record/Correct audit trail).
	rec = doJSON(t, router, http.MethodPost, "/cost-items/"+costItemID+"/record-actual", companyA.accessToken, map[string]any{
		"amount": 90000, "expectedRevision": revision,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 recording actual, got %d: %s", rec.Code, rec.Body.String())
	}
	revision = mustNumberField(t, rec, "revision")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken, map[string]any{
		"stage": "paid", "amount": 90000, "expectedRevision": revision,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 setting paid, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items/"+costItemID, companyA.accessToken, nil)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	for _, field := range []string{"estimated", "committed", "actual", "paid"} {
		if resp[field] == nil {
			t.Fatalf("expected %s to be non-nil, all four lifecycle fields must coexist", field)
		}
	}
}

// mustNumberField parses rec's JSON body and returns the numeric value of
// the given top-level field as a float64 (mustField only supports string
// fields; Version/Revision/nested Money amounts are numbers/objects).
func mustNumberField(t *testing.T, rec *httptest.ResponseRecorder, field string) float64 {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %s: %v", rec.Body.String(), err)
	}
	v, ok := resp[field].(float64)
	if !ok {
		t.Fatalf("expected numeric field %q in response %s", field, rec.Body.String())
	}
	return v
}

// mustNestedAmount returns resp[outerField]["amount"] as a float64 — used
// for Money-shaped fields like costSubtotal/proposedSellingPrice, which
// are nested {"amount": ..., "currency": ...} objects, not flat fields.
func mustNestedAmount(t *testing.T, rec *httptest.ResponseRecorder, outerField string) float64 {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response %s: %v", rec.Body.String(), err)
	}
	outer, ok := resp[outerField].(map[string]any)
	if !ok {
		t.Fatalf("expected object field %q in response %s", outerField, rec.Body.String())
	}
	amount, ok := outer["amount"].(float64)
	if !ok {
		t.Fatalf("expected numeric \"amount\" inside field %q in response %s", outerField, rec.Body.String())
	}
	return amount
}

// buildProjectWithEstimatedCostItem creates a Client -> Project -> CostItem
// (with an Estimated amount) chain for companyA, returning the Project ID
// and the CostItem ID.
func buildProjectWithEstimatedCostItem(t *testing.T, router http.Handler, companyA testCompany) (projectID, costItemID string) {
	t.Helper()
	_, projectID, _, _, _ = buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "category": "material", "description": "Ceramic tiles",
		"estimatedAmount": 500000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	costItemID = mustField(t, rec, "id")
	return projectID, costItemID
}

func TestTenantIsolation_EstimatesFullMatrix(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-a@example.com", "Estimates Co A")
	companyB := registerCompany(t, router, "estimates-b@example.com", "Estimates Co B")

	projectA, _ := buildProjectWithEstimatedCostItem(t, router, companyA)

	createBody := map[string]any{"projectId": projectA, "pricingMode": "markup", "pricingRate": 2000}
	estimateResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken, createBody)
	if estimateResp.Code != http.StatusOK {
		t.Fatalf("expected estimate creation to succeed, got %d: %s", estimateResp.Code, estimateResp.Body.String())
	}
	estimateID := mustField(t, estimateResp, "id")

	// B cannot GET A's estimate.
	getResp := doJSON(t, router, http.MethodGet, "/estimates/"+estimateID, companyB.accessToken, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant GET, got %d", getResp.Code)
	}

	// B cannot GET latest for A's project.
	latestResp := doJSON(t, router, http.MethodGet, "/estimates/latest?projectId="+projectA, companyB.accessToken, nil)
	if latestResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant latest lookup, got %d", latestResp.Code)
	}

	// B cannot list A's project's estimates.
	listResp := doJSON(t, router, http.MethodGet, "/estimates?projectId="+projectA, companyB.accessToken, nil)
	if listResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant list, not an empty array, got %d: %s", listResp.Code, listResp.Body.String())
	}

	// B cannot refresh/reprice/finalize/version A's estimate.
	refreshResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/refresh", companyB.accessToken,
		map[string]any{"expectedRevision": 0})
	if refreshResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant refresh, got %d", refreshResp.Code)
	}

	pricingResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", companyB.accessToken,
		map[string]any{"pricingMode": "margin", "pricingRate": 1000, "expectedRevision": 0})
	if pricingResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant pricing update, got %d", pricingResp.Code)
	}

	finalizeRespB := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", companyB.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant finalize, got %d", finalizeRespB.Code)
	}

	versionRespB := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/versions", companyB.accessToken, map[string]any{})
	if versionRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant new-version creation, got %d", versionRespB.Code)
	}

	// B cannot create an estimate under A's project.
	createRespB := doJSON(t, router, http.MethodPost, "/estimates", companyB.accessToken, createBody)
	if createRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant estimate creation, got %d", createRespB.Code)
	}
}

func TestTenantIsolation_EstimateSnapshotImmutabilityAndLifecycle(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-lifecycle@example.com", "Estimates Lifecycle Co")

	projectID, costItemID := buildProjectWithEstimatedCostItem(t, router, companyA)

	// Create V1, record its snapshot.
	createResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating estimate, got %d: %s", createResp.Code, createResp.Body.String())
	}
	v1ID := mustField(t, createResp, "id")
	v1CostSubtotalBefore := mustNestedAmount(t, createResp, "costSubtotal")

	// Change the underlying CostItem's Estimated.
	lifecycleResp := doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken,
		map[string]any{"stage": "estimated", "amount": 999999, "expectedRevision": 0})
	if lifecycleResp.Code != http.StatusOK {
		t.Fatalf("expected cost item lifecycle update to succeed, got %d: %s", lifecycleResp.Code, lifecycleResp.Body.String())
	}
	costItemRevision := mustNumberField(t, lifecycleResp, "revision")

	// GET V1 -> unchanged.
	getV1Resp := doJSON(t, router, http.MethodGet, "/estimates/"+v1ID, companyA.accessToken, nil)
	if mustNestedAmount(t, getV1Resp, "costSubtotal") != v1CostSubtotalBefore {
		t.Fatalf("expected V1's CostSubtotal unchanged by a mere GET after the CostItem change")
	}

	// Refresh V1 -> picks up the change, same Version, Revision incremented.
	refreshResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/refresh", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("expected refresh to succeed, got %d: %s", refreshResp.Code, refreshResp.Body.String())
	}
	if mustNumberField(t, refreshResp, "version") != 1 {
		t.Fatalf("expected Version to remain 1 after refresh, got %v", mustNumberField(t, refreshResp, "version"))
	}
	if mustNumberField(t, refreshResp, "revision") != 1 {
		t.Fatalf("expected Revision incremented to 1 after refresh, got %v", mustNumberField(t, refreshResp, "revision"))
	}
	if mustNestedAmount(t, refreshResp, "costSubtotal") == v1CostSubtotalBefore {
		t.Fatal("expected CostSubtotal to reflect the updated CostItem.Estimated after refresh")
	}

	// Finalize V1 at the post-refresh revision.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 1})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	if mustField(t, finalizeResp, "status") != "finalized" {
		t.Fatal("expected status=finalized")
	}

	// Change the CostItem again, attempt refresh on the now-finalized V1 -> rejected.
	doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken,
		map[string]any{"stage": "estimated", "amount": 555555, "expectedRevision": costItemRevision})
	refreshFinalizedResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/refresh", companyA.accessToken,
		map[string]any{"expectedRevision": 1})
	if refreshFinalizedResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 refreshing a finalized estimate, got %d", refreshFinalizedResp.Code)
	}

	// Create V2 -> succeeds since V1 is finalized, reflects the latest cost change.
	versionResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/versions", companyA.accessToken, map[string]any{})
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected new-version creation to succeed from a finalized source, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
	v2ID := mustField(t, versionResp, "id")
	if mustNumberField(t, versionResp, "version") != 2 {
		t.Fatal("expected Version=2")
	}
	if mustNumberField(t, versionResp, "revision") != 0 {
		t.Fatal("expected new version to start at Revision=0")
	}

	// V1 remains unchanged.
	getV1AgainResp := doJSON(t, router, http.MethodGet, "/estimates/"+v1ID, companyA.accessToken, nil)
	if mustField(t, getV1AgainResp, "status") != "finalized" {
		t.Fatal("expected V1 to remain finalized")
	}

	// Attempting a new version from V2 (still a draft) is rejected.
	versionFromDraftResp := doJSON(t, router, http.MethodPost, "/estimates/"+v2ID+"/versions", companyA.accessToken, map[string]any{})
	if versionFromDraftResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 creating a new version from a draft source, got %d", versionFromDraftResp.Code)
	}
}

func TestTenantIsolation_EstimateFinalizeStaleRevisionRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-stale-revision@example.com", "Estimates Stale Revision Co")

	projectID, _ := buildProjectWithEstimatedCostItem(t, router, companyA)

	createResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	estimateID := mustField(t, createResp, "id")

	// Advance the draft's Revision via a pricing recalculation.
	repriceResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", companyA.accessToken,
		map[string]any{"pricingMode": "margin", "pricingRate": 1000, "expectedRevision": 0})
	if repriceResp.Code != http.StatusOK {
		t.Fatalf("expected pricing recalculation to succeed, got %d: %s", repriceResp.Code, repriceResp.Body.String())
	}

	// Attempt to finalize at the STALE revision 0 -- must be rejected.
	staleFinalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if staleFinalizeResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 finalizing at a stale revision, got %d: %s", staleFinalizeResp.Code, staleFinalizeResp.Body.String())
	}

	// Retry at the CURRENT revision (1) -- succeeds.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 1})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed at the current revision, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	// Retry finalize again (idempotent) -- succeeds regardless of the
	// expectedRevision supplied.
	retryFinalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 999})
	if retryFinalizeResp.Code != http.StatusOK {
		t.Fatalf("expected idempotent finalize retry to succeed regardless of expectedRevision, got %d", retryFinalizeResp.Code)
	}
}

func TestTenantIsolation_EstimateMixedCurrencyRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-mixed-currency@example.com", "Estimates Mixed Currency Co")

	_, projectID, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	createCostItemMYR := map[string]any{
		"projectId": projectID, "category": "material", "description": "MYR cost",
		"estimatedAmount": 50000, "currency": "MYR",
	}
	doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, createCostItemMYR)

	createCostItemSGD := map[string]any{
		"projectId": projectID, "category": "material", "description": "SGD cost",
		"estimatedAmount": 30000, "currency": "SGD",
	}
	doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, createCostItemSGD)

	createEstimateResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if createEstimateResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for mixed-currency estimate creation, got %d: %s", createEstimateResp.Code, createEstimateResp.Body.String())
	}
}

func TestTenantIsolation_EstimateInvalidPricingModeRejectedEverywhere(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-invalid-mode@example.com", "Estimates Invalid Mode Co")

	projectID, _ := buildProjectWithEstimatedCostItem(t, router, companyA)

	// POST /estimates with an invalid pricingMode.
	createResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "banana", "pricingRate": 2000})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode on create, got %d: %s", createResp.Code, createResp.Body.String())
	}

	// Create a valid estimate to exercise the other two endpoints against.
	validCreateResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if validCreateResp.Code != http.StatusOK {
		t.Fatalf("expected valid estimate creation to succeed, got %d: %s", validCreateResp.Code, validCreateResp.Body.String())
	}
	estimateID := mustField(t, validCreateResp, "id")

	// PATCH /estimates/{id}/pricing with an invalid pricingMode.
	pricingResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", companyA.accessToken,
		map[string]any{"pricingMode": "banana", "pricingRate": 2000, "expectedRevision": 0})
	if pricingResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode on pricing recalculation, got %d: %s", pricingResp.Code, pricingResp.Body.String())
	}

	// POST /estimates/{id}/versions with an invalid pricingMode override --
	// first finalize, since CreateNewVersion requires a finalized source.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	invalidModeStr := "banana"
	versionResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/versions", companyA.accessToken,
		map[string]any{"pricingMode": &invalidModeStr})
	if versionResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode override on new-version creation, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
}

func TestTenantIsolation_EstimateOnlyOneVersionPerCreatePost(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "estimates-only-v1@example.com", "Estimates Only V1 Co")

	projectID, _ := buildProjectWithEstimatedCostItem(t, router, companyA)

	firstResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if firstResp.Code != http.StatusOK {
		t.Fatalf("expected first POST /estimates to succeed, got %d: %s", firstResp.Code, firstResp.Body.String())
	}

	// Calling POST /estimates again for the same Project must NOT silently
	// mint V2 -- it must be rejected, since creating subsequent versions is
	// exclusively POST /estimates/{id}/versions' job.
	secondResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if secondResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 on second POST /estimates for the same project, got %d: %s", secondResp.Code, secondResp.Body.String())
	}

	// Confirm exactly one estimate exists for this project.
	listResp := doJSON(t, router, http.MethodGet, "/estimates?projectId="+projectID, companyA.accessToken, nil)
	var listBody struct {
		Estimates []map[string]any `json:"estimates"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}
	if len(listBody.Estimates) != 1 {
		t.Fatalf("expected exactly 1 estimate for the project, got %d", len(listBody.Estimates))
	}
}

// buildFinalizedEstimate creates a Project with one estimated CostItem
// attached to a real WorkItem, then creates and finalizes an Estimate
// against it — the minimum real prerequisite chain every M5 Quotation
// acceptance test needs (design spec §3: a Quotation may only reference a
// finalized Estimate). Returns the Project ID, the finalized Estimate ID,
// and the WorkItem ID the CostItem was attached to (needed by tests that
// verify SourceWorkItemIDs traceability).
func buildFinalizedEstimate(t *testing.T, router http.Handler, company testCompany) (projectID, estimateID, workItemID string) {
	t.Helper()

	_, projectID, _, _, workItemID = buildFullHierarchyForCompanyA(t, router, company)

	costItemResp := doJSON(t, router, http.MethodPost, "/cost-items", company.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Ceramic tiles", "estimatedAmount": 500000, "currency": "MYR",
	})
	if costItemResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", costItemResp.Code, costItemResp.Body.String())
	}

	createResp := doJSON(t, router, http.MethodPost, "/estimates", company.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating estimate, got %d: %s", createResp.Code, createResp.Body.String())
	}
	estID := mustField(t, createResp, "id")

	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estID+"/finalize", company.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing estimate, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	return projectID, estID, workItemID
}

func TestTenantIsolation_QuotationCreateFromFinalizedEstimate(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-create@example.com", "Quotations Create Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}
	if mustField(t, createResp, "quotationNumber") != "QT-000001" {
		t.Fatalf("expected QT-000001, got %s", mustField(t, createResp, "quotationNumber"))
	}
	if mustNumberField(t, createResp, "version") != 1 {
		t.Fatal("expected Version=1")
	}
	if mustField(t, createResp, "status") != "draft" {
		t.Fatal("expected status=draft")
	}

	var body map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	lines, ok := body["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("expected exactly 1 generated line (one WorkItem), got %+v", body["lines"])
	}
	line := lines[0].(map[string]any)
	sourceIDs, ok := line["sourceWorkItemIds"].([]any)
	if !ok || len(sourceIDs) != 1 || sourceIDs[0] != workItemID {
		t.Fatalf("expected sourceWorkItemIds=[%s], got %+v", workItemID, line["sourceWorkItemIds"])
	}

	subtotal := mustNestedAmount(t, createResp, "subtotal")
	generatedSubtotal := mustNestedAmount(t, createResp, "generatedSubtotal")
	if subtotal != generatedSubtotal {
		t.Fatalf("expected Subtotal == GeneratedSubtotal at generation time, got %v and %v", subtotal, generatedSubtotal)
	}
	total := mustNestedAmount(t, createResp, "total")
	if total != subtotal {
		t.Fatal("expected Total == Subtotal when TaxMode=none")
	}
}

func TestTenantIsolation_QuotationCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-tenant-a@example.com", "Quotations Tenant A")
	companyB := registerCompany(t, router, "quotations-tenant-b@example.com", "Quotations Tenant B")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}
	quotationID := mustField(t, createResp, "id")

	getResp := doJSON(t, router, http.MethodGet, "/quotations/"+quotationID, companyB.accessToken, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for company B accessing company A's quotation, got %d", getResp.Code)
	}
}

func TestTenantIsolation_QuotationCrossProjectEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-cross-project@example.com", "Quotations Cross Project Co")

	_, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	otherProjectID, _, _ := buildFinalizedEstimate(t, router, companyA) // a second, unrelated Project

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": otherProjectID, "estimateId": estimateID})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a cross-project estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationCrossCompanyEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-xco-a@example.com", "Quotations XCo A")
	companyB := registerCompany(t, router, "quotations-xco-b@example.com", "Quotations XCo B")

	_, estimateIDFromA, _ := buildFinalizedEstimate(t, router, companyA)
	projectIDForB, _, _ := buildFinalizedEstimate(t, router, companyB)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyB.accessToken,
		map[string]any{"projectId": projectIDForB, "estimateId": estimateIDFromA})
	if createResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a cross-company estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationDraftEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-draft-est@example.com", "Quotations Draft Est Co")

	_, projectID, _, _, workItemID := buildFullHierarchyForCompanyA(t, router, companyA)
	doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Tiles", "estimatedAmount": 500000, "currency": "MYR",
	})
	createEstResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	draftEstimateID := mustField(t, createEstResp, "id") // deliberately NOT finalized

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": draftEstimateID})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a draft (non-finalized) estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationInternalCostInfoNeverLeaks(t *testing.T) {
	// Black-box proof that no internal cost/margin field name ever appears
	// anywhere in a Quotation HTTP response body (design spec §4/§28).
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-privacy@example.com", "Quotations Privacy Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}

	forbidden := []string{"snapshottedAmount", "costSubtotal", "pricingMode", "pricingRate",
		"projectedGrossProfit", "projectedGrossMarginBps", "category"}
	body := createResp.Body.String()
	for _, field := range forbidden {
		if strings.Contains(body, field) {
			t.Fatalf("found forbidden internal field %q in Quotation response body: %s", field, body)
		}
	}
}

func TestTenantIsolation_QuotationLineCurrencySubstitutionRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-currency@example.com", "Quotations Currency Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{}, "description": "Wrong Currency Line",
				"amount": map[string]any{"amount": 10000, "currency": "USD"}, "sortOrder": 0},
		},
	})
	if putResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a currency-substitution attempt, got %d: %s", putResp.Code, putResp.Body.String())
	}

	unchanged := doJSON(t, router, http.MethodGet, "/quotations/"+quotationID, companyA.accessToken, nil)
	if mustNumberField(t, unchanged, "revision") != 0 {
		t.Fatal("expected the draft's Revision unchanged after a rejected line submission")
	}
}

func TestTenantIsolation_QuotationDraftEditingAndOptimisticConcurrency(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-edit@example.com", "Quotations Edit Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	// Successful line edit.
	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{workItemID}, "description": "Wall Tiles - Complete",
				"amount": map[string]any{"amount": 600000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if putResp.Code != http.StatusOK {
		t.Fatalf("expected 200 replacing lines, got %d: %s", putResp.Code, putResp.Body.String())
	}
	if mustNumberField(t, putResp, "revision") != 1 {
		t.Fatal("expected Revision incremented to 1")
	}

	// Stale revision on a second attempt must be rejected.
	staleResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0, // stale — already consumed above
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{}, "description": "Should Fail",
				"amount": map[string]any{"amount": 100000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if staleResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a stale revision, got %d", staleResp.Code)
	}

	// Terms update.
	termsResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/terms", companyA.accessToken, map[string]any{
		"terms": "50% deposit, 50% on completion", "paymentSchedule": "Net 30", "expectedRevision": 1,
	})
	if termsResp.Code != http.StatusOK {
		t.Fatalf("expected 200 updating terms, got %d: %s", termsResp.Code, termsResp.Body.String())
	}
	if mustField(t, termsResp, "terms") != "50% deposit, 50% on completion" {
		t.Fatal("expected terms to be updated")
	}

	// Tax update.
	taxResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/tax", companyA.accessToken, map[string]any{
		"taxMode": "percentage", "taxLabel": "SST 6%", "taxRateBps": 600, "expectedRevision": 2,
	})
	if taxResp.Code != http.StatusOK {
		t.Fatalf("expected 200 updating tax, got %d: %s", taxResp.Code, taxResp.Body.String())
	}
	subtotalAfterTax := mustNestedAmount(t, taxResp, "subtotal")
	taxAmount := mustNestedAmount(t, taxResp, "taxAmount")
	total := mustNestedAmount(t, taxResp, "total")
	if total != subtotalAfterTax+taxAmount {
		t.Fatalf("expected Total == Subtotal + TaxAmount, got total=%v subtotal=%v tax=%v", total, subtotalAfterTax, taxAmount)
	}
}

func TestTenantIsolation_QuotationReplaceLinesSourceWorkItemIDsRequiredKey(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-swid-required@example.com", "Quotations SWID Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	// Omitting the sourceWorkItemIds key entirely must be rejected — it is
	// required traceability metadata, distinguishing "no change" from
	// "clear the traceability" (design spec §5.3/§13.1).
	omittedResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"description": "Missing SourceWorkItemIDs Key",
				"amount": map[string]any{"amount": 600000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if omittedResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 omitting sourceWorkItemIds entirely, got %d: %s", omittedResp.Code, omittedResp.Body.String())
	}

	// An explicit empty array must be accepted — a fully contractor-authored
	// line with no WorkItem traceability.
	emptyArrayResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{}, "description": "Explicit Empty Array",
				"amount": map[string]any{"amount": 600000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if emptyArrayResp.Code != http.StatusOK {
		t.Fatalf("expected 200 with an explicit empty sourceWorkItemIds array, got %d: %s", emptyArrayResp.Code, emptyArrayResp.Body.String())
	}

	// Also confirm a non-empty sourceWorkItemIds array continues to work,
	// proving this test's 422/200 split isn't accidental.
	populatedResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 1,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{workItemID}, "description": "Populated WorkItem Traceability",
				"amount": map[string]any{"amount": 600000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if populatedResp.Code != http.StatusOK {
		t.Fatalf("expected 200 with a populated sourceWorkItemIds array, got %d: %s", populatedResp.Code, populatedResp.Body.String())
	}
}

func TestTenantIsolation_QuotationFinalizedImmutability(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-immutable@example.com", "Quotations Immutable Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	finalizeResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	if mustField(t, finalizeResp, "status") != "finalized" {
		t.Fatal("expected status=finalized")
	}

	// Every mutating endpoint must now reject with 409.
	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0, "lines": []map[string]any{{"sourceWorkItemIds": []string{}, "description": "X", "amount": map[string]any{"amount": 100, "currency": "MYR"}}},
	})
	if putResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 replacing lines on a finalized quotation, got %d", putResp.Code)
	}

	termsResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/terms", companyA.accessToken,
		map[string]any{"terms": "X", "expectedRevision": 0})
	if termsResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 updating terms on a finalized quotation, got %d", termsResp.Code)
	}

	taxResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/tax", companyA.accessToken,
		map[string]any{"taxMode": "none", "expectedRevision": 0})
	if taxResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 updating tax on a finalized quotation, got %d", taxResp.Code)
	}

	// Idempotent finalize retry -> 200, unchanged.
	retryResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 999})
	if retryResp.Code != http.StatusOK {
		t.Fatalf("expected 200 on an idempotent finalize retry, got %d: %s", retryResp.Code, retryResp.Body.String())
	}
}

func TestTenantIsolation_QuotationNewVersionFreshRegeneration(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-newversion@example.com", "Quotations NewVersion Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	v1ID := mustField(t, createResp, "id")
	quotationNumber := mustField(t, createResp, "quotationNumber")

	// Hand-edit V1's line before finalizing.
	doJSON(t, router, http.MethodPut, "/quotations/"+v1ID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{workItemID}, "description": "Hand-Edited Line",
				"amount": map[string]any{"amount": 999900, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	finalizeV1Resp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 1})
	if finalizeV1Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing v1, got %d: %s", finalizeV1Resp.Code, finalizeV1Resp.Body.String())
	}

	// A fresh, different finalized Estimate for the SAME Project — since
	// M4's own rule allows only one Estimate chain per Project via
	// POST /estimates, use POST /estimates/{id}/versions against the
	// already-finalized first estimate instead.
	costItemResp := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Additional tiles", "estimatedAmount": 300000, "currency": "MYR",
	})
	if costItemResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a second cost item, got %d: %s", costItemResp.Code, costItemResp.Body.String())
	}
	versionEstResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/versions", companyA.accessToken, map[string]any{})
	if versionEstResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a new estimate version, got %d: %s", versionEstResp.Code, versionEstResp.Body.String())
	}
	estimate2ID := mustField(t, versionEstResp, "id")
	finalizeEst2Resp := doJSON(t, router, http.MethodPost, "/estimates/"+estimate2ID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeEst2Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing the second estimate, got %d: %s", finalizeEst2Resp.Code, finalizeEst2Resp.Body.String())
	}

	versionResp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/versions", companyA.accessToken,
		map[string]any{"estimateId": estimate2ID})
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a new quotation version, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
	if mustNumberField(t, versionResp, "version") != 2 {
		t.Fatal("expected Version=2")
	}
	if mustField(t, versionResp, "quotationNumber") != quotationNumber {
		t.Fatal("expected the same QuotationNumber carried forward")
	}
	v2GeneratedSubtotal := mustNestedAmount(t, versionResp, "generatedSubtotal")
	if v2GeneratedSubtotal == 999900 {
		t.Fatal("expected V2's GeneratedSubtotal to be a FRESH allocation, NOT V1's hand-edited amount carried forward")
	}
}

func TestTenantIsolation_QuotationFinalizeNeverTouchesProjectStatus(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-project-status@example.com", "Quotations Project Status Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)

	beforeResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyA.accessToken, nil)
	statusBefore := mustField(t, beforeResp, "status")

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")
	finalizeResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing quotation, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	afterResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyA.accessToken, nil)
	statusAfter := mustField(t, afterResp, "status")
	if statusBefore != statusAfter {
		t.Fatalf("expected Project.Status unchanged by Quotation.finalize, was %q now %q", statusBefore, statusAfter)
	}
}

func TestTenantIsolation_AllQuotationEndpointsRequireBearerAuth(t *testing.T) {
	router := setupRouter(t)
	openAPIResp := doJSON(t, router, http.MethodGet, "/openapi.json", "", nil)
	if openAPIResp.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching openapi.json, got %d", openAPIResp.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(openAPIResp.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse openapi.json: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("expected a paths object in openapi.json")
	}
	quotationsPathCount := 0
	for path, methods := range paths {
		if !strings.HasPrefix(path, "/quotations") {
			continue
		}
		methodsMap, ok := methods.(map[string]any)
		if !ok {
			continue
		}
		for _, opRaw := range methodsMap {
			op, ok := opRaw.(map[string]any)
			if !ok {
				continue
			}
			security, ok := op["security"].([]any)
			if !ok || len(security) == 0 {
				t.Fatalf("expected every /quotations operation to declare bearerAuth security, path %s has none: %+v", path, op)
			}
			quotationsPathCount++
		}
	}
	// M5 shipped 8 /quotations operations; M6 added share + share-status, so
	// the floor is 10. The assertion that matters is the loop above — EVERY
	// /quotations operation declares security — not a frozen count, which
	// would fail every time a later milestone legitimately adds a route.
	const minQuotationsOperations = 10
	if quotationsPathCount < minQuotationsOperations {
		t.Fatalf("expected at least %d /quotations operations declaring security, found %d",
			minQuotationsOperations, quotationsPathCount)
	}
}

// M8.5C: Spatial Intelligence tenant isolation. Mirrors
// TestTenantIsolation_SpaceCrossTenantAccessAndForeignProjectRejected's
// shape — Company B must never read or create spatial captures against
// Company A's Space, and a same-tenant lineage mismatch (Space real project
// vs claimed project) must also be rejected, matching spatial.Service's
// SpaceLookup.SpaceBelongsToProject check (design spec §3, "Final
// Architectural Invariants" #1-2).
func TestTenantIsolation_SpatialCaptureCrossTenantAccessAndForeignParentsRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ownerA8@example.com", "Company A8")
	companyB := registerCompany(t, router, "ownerB8@example.com", "Company B8")

	_, projectID, _, spaceID, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/spatial/captures", companyA.accessToken,
		map[string]string{"projectId": projectID, "spaceId": spaceID})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 starting a capture as the owning company, got %d: %s", rec.Code, rec.Body.String())
	}
	captureID := mustField(t, rec, "id")

	// Company B must not be able to read Company A's capture.
	rec = doJSON(t, router, http.MethodGet, "/spatial/captures/"+captureID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B reading Company A's capture, got %d: %s", rec.Code, rec.Body.String())
	}

	// Company B must not be able to start a capture under Company A's Space.
	rec = doJSON(t, router, http.MethodPost, "/spatial/captures", companyB.accessToken,
		map[string]string{"projectId": projectID, "spaceId": spaceID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for Company B starting a capture under Company A's space, got %d: %s", rec.Code, rec.Body.String())
	}

	// Same-tenant lineage mismatch: Space really belongs to projectID, but the
	// request claims a different, otherwise-valid Company A project.
	clientRec := doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Second Spatial Client"})
	if clientRec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating second client, got %d: %s", clientRec.Code, clientRec.Body.String())
	}
	clientID := mustField(t, clientRec, "id")
	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID, "name": "Second Spatial Project"})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating second project, got %d: %s", rec.Code, rec.Body.String())
	}
	project2ID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/spatial/captures", companyA.accessToken,
		map[string]string{"projectId": project2ID, "spaceId": spaceID})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a Space/Project lineage mismatch within the same tenant, got %d: %s", rec.Code, rec.Body.String())
	}
}
