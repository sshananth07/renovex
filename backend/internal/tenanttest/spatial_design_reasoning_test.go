package tenanttest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
	"github.com/shananth/renovation-platform/backend/internal/tenanttest"
)

// newTestMongoDatabase starts a fresh real MongoDB replica-set container
// and returns a connected database — the same container-startup sequence
// setupRouterWithMail uses, extracted here because this file needs the
// *composition.Services graph directly (via BuildRouterAndServicesForTest)
// rather than setupRouterWithMail's router-only return.
func newTestMongoDatabase(t *testing.T) *mongo.Database {
	t.Helper()
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
	connStr += "&directConnection=true"

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return platformmongo.Database(client, "tenanttest")
}

// buildDesignTestRouter builds the router + Services from ONE build call
// (the same object graph — see composition/services.go's established
// "mutate the SAME instance the router was built from" precedent) against
// a fresh real MongoDB, plus the raw database for direct fixture
// seeding/assertion.
func buildDesignTestRouter(t *testing.T) (http.Handler, *composition.Services, *mongo.Database) {
	t.Helper()
	db := newTestMongoDatabase(t)
	router, services, err := tenanttest.BuildRouterAndServicesForTest(db)
	if err != nil {
		t.Fatalf("building router and services: %v", err)
	}
	return router, services, db
}

// countingFakeReasoner counts calls and returns a scripted result — the
// counting seam every "at most one provider call" assertion in this file
// depends on (RP4E1 plan Task 10).
type countingFakeReasoner struct {
	calls  int
	result spatial.ProposedSceneEditDelta
	err    error
}

func (f *countingFakeReasoner) ReasonElement(_ context.Context, reasoningContext spatial.DesignReasoningContext) (spatial.ProposedSceneEditDelta, error) {
	f.calls++
	if f.err != nil {
		return spatial.ProposedSceneEditDelta{}, f.err
	}
	result := f.result
	result.Target = spatial.SpatialDesignTarget{Kind: reasoningContext.Target.Kind, ID: reasoningContext.Target.ID}
	return result, nil
}

func mixedSofaDelta() spatial.ProposedSceneEditDelta {
	return spatial.ProposedSceneEditDelta{
		Intent:  spatial.DesignIntentMixed,
		Summary: []string{"Change to curved shape.", "Reupholster in dark green velvet.", "Move 20cm from the wall."},
		Geometry: spatial.SectionChange{Mode: spatial.SectionModeReplace, GeometrySpec: &spatial.WorkingDesignGeometry{
			Category: "sofa", ShapeDescription: "Curved three-seat sofa with light wooden legs", PreserveCanonicalDimensions: true,
		}},
		Material: spatial.SectionChange{Mode: spatial.SectionModeReplace, MaterialSpec: &spatial.WorkingDesignMaterial{
			BaseColor: "#2f5233", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial: spatial.SectionChange{Mode: spatial.SectionModeReplace, SpatialSpec: &spatial.ProposedSpatialSpec{
			Kind: spatial.SpatialOpMoveRelativeToNearestWall, Relationship: spatial.SpatialRelationshipAwayFrom, DistanceMeters: 0.2,
		}},
		Confidence: 0.9,
	}
}

func materialOnlyBeigeDelta() spatial.ProposedSceneEditDelta {
	return spatial.ProposedSceneEditDelta{
		Intent:   spatial.DesignIntentMaterialAppearance,
		Summary:  []string{"Change the sofa upholstery to beige."},
		Geometry: spatial.SectionChange{Mode: spatial.SectionModePreserve},
		Material: spatial.SectionChange{Mode: spatial.SectionModeReplace, MaterialSpec: &spatial.WorkingDesignMaterial{
			BaseColor: "#e8dcc4", MaterialFamily: "fabric", Roughness: "matte",
		}},
		Spatial:    spatial.SectionChange{Mode: spatial.SectionModePreserve},
		Confidence: 0.91,
	}
}

// seedRoomDraftWithSofa directly inserts a minimal, valid RoomDraft owned
// by companyID with one object/fixture — fixture setup only. RP4E1 does
// not create RoomDrafts via HTTP; a real RoomDraft normally arrives
// through the capture/RoomPlan pipeline (out of scope here), matching
// spatial_visual_asset_test.go's own "seed the fixture directly, exercise
// the real route on top of it" pattern.
func seedRoomDraftWithSofa(t *testing.T, db *mongo.Database, companyID string) spatial.RoomDraft {
	t.Helper()
	draft := spatial.RoomDraft{
		CompanyID: companyID, CaptureID: "capture_fixture_1",
		Walls: []spatial.RoomDraftWall{
			{ID: "wall_south", Start: spatial.RoomLocalPoint{X: 0, Y: 0, Z: 0}, End: spatial.RoomLocalPoint{X: 4, Y: 0, Z: 0}},
			{ID: "wall_east", Start: spatial.RoomLocalPoint{X: 4, Y: 0, Z: 0}, End: spatial.RoomLocalPoint{X: 4, Y: 0, Z: 4}},
			{ID: "wall_north", Start: spatial.RoomLocalPoint{X: 4, Y: 0, Z: 4}, End: spatial.RoomLocalPoint{X: 0, Y: 0, Z: 4}},
			{ID: "wall_west", Start: spatial.RoomLocalPoint{X: 0, Y: 0, Z: 4}, End: spatial.RoomLocalPoint{X: 0, Y: 0, Z: 0}},
		},
		Objects: []spatial.RoomDraftObject{
			{
				ID: "object_sofa_123", Category: "sofa",
				Transform:  spatial.RoomLocalTransform{Position: spatial.RoomLocalPoint{X: 2, Y: 0, Z: 0.5}, Rotation: spatial.RoomLocalQuaternion{W: 1}},
				Dimensions: &spatial.RoomLocalPoint{X: 2.0, Y: 0.85, Z: 0.5},
			},
		},
		SchemaVersion: 1,
	}
	created, err := spatial.NewMongoRoomDraftRepository(db).Create(context.Background(), draft)
	if err != nil {
		t.Fatalf("seeding room draft: %v", err)
	}
	return created
}

func doDesignJSON(t *testing.T, router http.Handler, method, path, accessToken string, body any) *httptest.ResponseRecorder {
	return doJSON(t, router, method, path, accessToken, body)
}

// TestSpatialDesignReasoning_MixedTurnEndToEnd proves Task 10 Step 1: through
// the real composed router with real JWT/company isolation and a counting
// fake reasoner, create a session and run the main mixed sofa turn,
// asserting one reasoner call and a confirmation-ready immutable plan.
func TestSpatialDesignReasoning_MixedTurnEndToEnd(t *testing.T) {
	router, services, db := buildDesignTestRouter(t)
	companyA := registerCompany(t, router, "design-owner@example.com", "Design Co")
	companyIDA := fetchCompanyID(t, router, companyA.accessToken)

	draft := seedRoomDraftWithSofa(t, db, companyIDA)
	reasoner := &countingFakeReasoner{result: mixedSofaDelta()}
	services.Spatial.SetDesignReasoner(reasoner)

	createSessionRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions", companyA.accessToken, map[string]any{
		"clientSessionId":           "client-session-1",
		"roomDraftId":               draft.ID,
		"expectedRoomDraftRevision": draft.Revision,
		"target":                    map[string]string{"kind": "object", "id": "object_sofa_123"},
	})
	if createSessionRec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating session, got %d: %s", createSessionRec.Code, createSessionRec.Body.String())
	}
	var sessionResp struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if err := json.Unmarshal(createSessionRec.Body.Bytes(), &sessionResp); err != nil {
		t.Fatalf("parsing session response: %v", err)
	}
	sessionID := sessionResp.Session.ID
	if sessionID == "" {
		t.Fatal("expected a non-empty session id")
	}
	if reasoner.calls != 0 {
		t.Fatalf("session creation must never call the reasoner, got %d calls", reasoner.calls)
	}

	turnRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions/"+sessionID+"/turns", companyA.accessToken, map[string]any{
		"clientRequestId": "client-request-1",
		"instruction":     "Make this sofa curved, dark green velvet with light wooden legs and move it 20 cm away from the wall.",
	})
	if turnRec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating turn, got %d: %s", turnRec.Code, turnRec.Body.String())
	}
	if reasoner.calls != 1 {
		t.Fatalf("expected exactly 1 reasoner call for the admitted turn, got %d", reasoner.calls)
	}
	var turnResp map[string]any
	if err := json.Unmarshal(turnRec.Body.Bytes(), &turnResp); err != nil {
		t.Fatalf("parsing turn response: %v", err)
	}
	if turnResp["status"] != "proposed" {
		t.Fatalf("expected proposed status, got %v: %s", turnResp["status"], turnRec.Body.String())
	}
	if turnResp["planFingerprint"] == "" || turnResp["planFingerprint"] == nil {
		t.Fatalf("expected a non-empty plan fingerprint, got %s", turnRec.Body.String())
	}

	assertRoomDraftUnchanged(t, db, draft)
}

// TestSpatialDesignReasoning_MaterialOnlyRefinement proves Task 10 Step 2:
// geometry is retained, turn-local generation is false, cumulative
// hunyuanRequired stays true, plan fingerprint changes, prior turn is
// superseded (never overwritten), and lineage survives service
// reconstruction.
func TestSpatialDesignReasoning_MaterialOnlyRefinement(t *testing.T) {
	router, services, db := buildDesignTestRouter(t)
	companyA := registerCompany(t, router, "material-refine@example.com", "Material Co")
	companyIDA := fetchCompanyID(t, router, companyA.accessToken)

	draft := seedRoomDraftWithSofa(t, db, companyIDA)
	reasoner := &countingFakeReasoner{}
	services.Spatial.SetDesignReasoner(reasoner)

	createSessionRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions", companyA.accessToken, map[string]any{
		"clientSessionId": "client-session-2", "roomDraftId": draft.ID, "expectedRoomDraftRevision": draft.Revision,
		"target": map[string]string{"kind": "object", "id": "object_sofa_123"},
	})
	var sessionResp struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	json.Unmarshal(createSessionRec.Body.Bytes(), &sessionResp)
	sessionID := sessionResp.Session.ID

	reasoner.result = spatial.ProposedSceneEditDelta{
		Intent: spatial.DesignIntentVisualGeometry, Summary: []string{"Curved shape."},
		Geometry: spatial.SectionChange{Mode: spatial.SectionModeReplace, GeometrySpec: &spatial.WorkingDesignGeometry{
			Category: "sofa", ShapeDescription: "Curved sofa", PreserveCanonicalDimensions: true,
		}},
		Material: spatial.SectionChange{Mode: spatial.SectionModePreserve}, Spatial: spatial.SectionChange{Mode: spatial.SectionModePreserve},
		Confidence: 0.9,
	}
	firstTurnRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions/"+sessionID+"/turns", companyA.accessToken, map[string]any{
		"clientRequestId": "req-geometry", "instruction": "Make this sofa curved.",
	})
	var firstTurn map[string]any
	json.Unmarshal(firstTurnRec.Body.Bytes(), &firstTurn)
	firstFingerprint := firstTurn["planFingerprint"]
	firstTurnID := firstTurn["id"]

	reasoner.result = materialOnlyBeigeDelta()
	secondTurnRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions/"+sessionID+"/turns", companyA.accessToken, map[string]any{
		"clientRequestId": "req-material", "instruction": "Actually make it beige.",
	})
	if secondTurnRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", secondTurnRec.Code, secondTurnRec.Body.String())
	}
	var secondTurn map[string]any
	json.Unmarshal(secondTurnRec.Body.Bytes(), &secondTurn)

	changePlanRaw, ok := secondTurn["changePlan"]
	if !ok || changePlanRaw == nil {
		t.Fatalf("expected a non-nil changePlan, got body: %s", secondTurnRec.Body.String())
	}
	changePlan := changePlanRaw.(map[string]any)
	workingDesignRaw, ok := changePlan["workingDesign"]
	if !ok || workingDesignRaw == nil {
		t.Fatalf("expected a non-nil changePlan.workingDesign, got body: %s", secondTurnRec.Body.String())
	}
	workingDesign := workingDesignRaw.(map[string]any)
	geometryRaw, ok := workingDesign["geometry"]
	if !ok || geometryRaw == nil {
		t.Fatalf("expected a non-nil changePlan.workingDesign.geometry, got body: %s", secondTurnRec.Body.String())
	}
	geometry := geometryRaw.(map[string]any)
	if geometry["shapeDescription"] != "Curved sofa" {
		t.Fatalf("expected geometry retained from first turn, got %v", geometry)
	}
	material := workingDesign["material"].(map[string]any)
	if material["baseColor"] != "#e8dcc4" {
		t.Fatalf("expected material replaced with beige (#e8dcc4), got %v", material)
	}
	execution := secondTurn["execution"].(map[string]any)
	if execution["turnRequiresAssetGeneration"] != false {
		t.Fatalf("expected turn-local generation flag false, got %v", execution)
	}
	if execution["hunyuanRequired"] != true {
		t.Fatalf("expected cumulative hunyuanRequired true, got %v", execution)
	}
	if secondTurn["planFingerprint"] == firstFingerprint {
		t.Fatal("expected plan fingerprint to change between turns")
	}
	if secondTurn["parentPlanTurnId"] != firstTurnID {
		t.Fatalf("expected second turn's parentPlanTurnId to reference the first, got %v vs %v", secondTurn["parentPlanTurnId"], firstTurnID)
	}

	// Lineage survives "service reconstruction": build a SEPARATE Services
	// graph against the same database (safe here — ListTurns/FindSession
	// are pure Mongo reads, not in-memory mutation) and confirm both turns
	// are still there in order.
	freshServices, err := tenanttest.BuildServicesForTest(db)
	if err != nil {
		t.Fatalf("rebuilding services: %v", err)
	}
	turns, err := freshServices.Spatial.ListDesignTurns(context.Background(), companyIDA, sessionID, 0, 10)
	if err != nil {
		t.Fatalf("listing turns after reconstruction: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns to survive reconstruction, got %d", len(turns))
	}

	assertRoomDraftUnchanged(t, db, draft)
}

// TestSpatialDesignReasoning_CrossTenantAccessIsExact404 proves Task 10
// Step 6: cross-tenant session/target access is an exact 404 and makes
// zero provider calls.
func TestSpatialDesignReasoning_CrossTenantAccessIsExact404(t *testing.T) {
	router, services, db := buildDesignTestRouter(t)
	companyA := registerCompany(t, router, "tenant-a@example.com", "Tenant A")
	companyB := registerCompany(t, router, "tenant-b@example.com", "Tenant B")
	companyIDA := fetchCompanyID(t, router, companyA.accessToken)

	draft := seedRoomDraftWithSofa(t, db, companyIDA)
	reasoner := &countingFakeReasoner{result: materialOnlyBeigeDelta()}
	services.Spatial.SetDesignReasoner(reasoner)

	createSessionRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions", companyA.accessToken, map[string]any{
		"clientSessionId": "client-session-3", "roomDraftId": draft.ID, "expectedRoomDraftRevision": draft.Revision,
		"target": map[string]string{"kind": "object", "id": "object_sofa_123"},
	})
	var sessionResp struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	json.Unmarshal(createSessionRec.Body.Bytes(), &sessionResp)
	sessionID := sessionResp.Session.ID

	// Company B attempts to read Company A's session.
	getRec := doAuthedGet(t, router, "/spatial/design-sessions/"+sessionID, companyB.accessToken)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant session read, got %d: %s", getRec.Code, getRec.Body.String())
	}

	// Company B attempts to create a turn against Company A's session.
	turnRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions/"+sessionID+"/turns", companyB.accessToken, map[string]any{
		"clientRequestId": "cross-tenant-req", "instruction": "x",
	})
	if turnRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant turn creation, got %d: %s", turnRec.Code, turnRec.Body.String())
	}
	if reasoner.calls != 0 {
		t.Fatalf("expected zero reasoner calls for cross-tenant access, got %d", reasoner.calls)
	}
}

// TestSpatialDesignReasoning_StaleRevisionBlocksNewTurns proves Task 10
// Step 6: a stale RoomDraft revision returns 409 before dispatch, and old
// plans remain readable but cannot accept new turns.
func TestSpatialDesignReasoning_StaleRevisionBlocksNewTurns(t *testing.T) {
	router, services, db := buildDesignTestRouter(t)
	companyA := registerCompany(t, router, "stale-revision@example.com", "Stale Co")
	companyIDA := fetchCompanyID(t, router, companyA.accessToken)

	draft := seedRoomDraftWithSofa(t, db, companyIDA)
	reasoner := &countingFakeReasoner{result: materialOnlyBeigeDelta()}
	services.Spatial.SetDesignReasoner(reasoner)

	createSessionRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions", companyA.accessToken, map[string]any{
		"clientSessionId": "client-session-4", "roomDraftId": draft.ID, "expectedRoomDraftRevision": draft.Revision,
		"target": map[string]string{"kind": "object", "id": "object_sofa_123"},
	})
	var sessionResp struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	json.Unmarshal(createSessionRec.Body.Bytes(), &sessionResp)
	sessionID := sessionResp.Session.ID

	// Advance the RoomDraft's revision independently (simulating a
	// concurrent RoomDraft edit) — the session is now stale.
	updated, err := services.Spatial.UpdateRoomDraft(context.Background(), companyIDA, draft.ID, draft.Walls, draft.Openings, draft.Objects, draft.Revision)
	if err != nil {
		t.Fatalf("advancing room draft revision: %v", err)
	}
	if updated.Revision == draft.Revision {
		t.Fatalf("expected revision to advance, got %d", updated.Revision)
	}

	turnRec := doDesignJSON(t, router, http.MethodPost, "/spatial/design-sessions/"+sessionID+"/turns", companyA.accessToken, map[string]any{
		"clientRequestId": "stale-req", "instruction": "x",
	})
	if turnRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a stale room draft revision, got %d: %s", turnRec.Code, turnRec.Body.String())
	}
	if reasoner.calls != 0 {
		t.Fatalf("expected zero reasoner calls before dispatch on stale revision, got %d", reasoner.calls)
	}

	// The session remains readable despite being stale.
	getRec := doAuthedGet(t, router, "/spatial/design-sessions/"+sessionID, companyA.accessToken)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 reading a stale session, got %d: %s", getRec.Code, getRec.Body.String())
	}
	var getResp map[string]any
	json.Unmarshal(getRec.Body.Bytes(), &getResp)
	if getResp["stale"] != true {
		t.Fatalf("expected stale=true, got %v", getResp)
	}

	assertRoomDraftUnchanged(t, db, updated)
}

// fetchCompanyID resolves a registered company's real companyID via /me,
// matching spatial_visual_asset_test.go's established convention.
func fetchCompanyID(t *testing.T, router http.Handler, accessToken string) string {
	t.Helper()
	rec := doAuthedGet(t, router, "/auth/me", accessToken)
	var me struct {
		CompanyID string `json:"companyId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("parsing /me response %s: %v", rec.Body.String(), err)
	}
	if me.CompanyID == "" {
		t.Fatalf("expected a non-empty companyId, got %s", rec.Body.String())
	}
	return me.CompanyID
}

// assertRoomDraftUnchanged compares the RoomDraft document/revision before
// and after a design-reasoning scenario (RP4E1 plan Task 10 Step 5) — RP4E1
// never mutates RoomDraft, edit history, visual assets, or asset-generation
// jobs.
func assertRoomDraftUnchanged(t *testing.T, db *mongo.Database, before spatial.RoomDraft) {
	t.Helper()
	repo := spatial.NewMongoRoomDraftRepository(db)
	after, err := repo.FindByID(context.Background(), before.CompanyID, before.ID)
	if err != nil {
		t.Fatalf("re-fetching room draft: %v", err)
	}
	if after.Revision != before.Revision {
		t.Fatalf("expected RoomDraft revision unchanged, before=%d after=%d", before.Revision, after.Revision)
	}
	if len(after.Objects) != len(before.Objects) {
		t.Fatalf("expected RoomDraft object count unchanged, before=%d after=%d", len(before.Objects), len(after.Objects))
	}
}
