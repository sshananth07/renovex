// Command seed-spatial-fixture creates one explicitly targeted, fixture-sourced
// RoomDraft for the production spatial smoke test. It is an internal admin
// tool, never an HTTP endpoint and never a substitute for RoomPlan capture.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

const productionSeedGuard = "RENOVEX_ALLOW_PROD_SEED"

// fixtureClientCaptureIDPrefix is the required marker on the pre-existing,
// normal-API-created capture this internal tool may enrich. It prevents a
// production run from attaching test geometry to an ordinary capture.
const fixtureClientCaptureIDPrefix = "test-spatial-"

type fixtureAction string

const (
	fixtureActionSeed    fixtureAction = "seed"
	fixtureActionCleanup fixtureAction = "cleanup"
)

type seedTarget struct {
	CompanyID   string
	ProjectID   string
	SpaceID     string
	CaptureID   string
	RoomDraftID string
}

type fixtureActionRequest struct {
	Action fixtureAction
	Target seedTarget
}

type kitchenFixture struct {
	Walls           []spatial.RoomDraftWall
	Openings        []spatial.RoomDraftOpening
	Objects         []spatial.RoomDraftObject
	Sink            spatial.AddFixtureOperation
	ElectricalPoint spatial.AddServicePointOperation
}

type seedResult struct {
	Capture   spatial.SpatialCapture
	RoomDraft spatial.RoomDraft
}

// spatialFixtureSeeder is the narrow production service surface the command
// needs. *spatial.Service satisfies it; the interface only enables focused
// safety tests and does not introduce a parallel persistence path.
type spatialFixtureSeeder interface {
	GetCapture(context.Context, string, string) (spatial.SpatialCapture, error)
	PersistRoomDraft(context.Context, string, string, []spatial.RoomDraftWall, []spatial.RoomDraftOpening, []spatial.RoomDraftObject, spatial.SourceProvider) (spatial.RoomDraft, error)
	SubmitEditOperation(context.Context, string, spatial.SubmitEditOperationInput) (spatial.SubmitEditOperationResult, error)
	GetRoomDraft(context.Context, string, string) (spatial.RoomDraft, error)
}

type fixtureCleanupService interface {
	RemoveFixtureRoomDraft(context.Context, string, string, string) (spatial.FixtureRoomDraftRemoval, error)
}

func main() {
	request, err := parseFixtureActionRequest(os.Args[1:], os.Getenv(productionSeedGuard) == "true")
	if err != nil {
		fatal(err)
	}

	// This tool deliberately reads only its own minimum configuration instead
	// of loading the full API config/composition graph. The latter requires
	// unrelated JWT, mail, object-store, queue, and provider credentials even
	// though this CLI never serves HTTP, sends mail, or calls a provider.
	if os.Getenv("APP_ENV") != "production" {
		fatal(fmt.Errorf("refusing fixture action: APP_ENV must be production, got %q", os.Getenv("APP_ENV")))
	}
	mongoURI := os.Getenv("MONGO_URI")
	mongoDatabase := os.Getenv("MONGO_DATABASE")
	if mongoURI == "" || mongoDatabase == "" {
		fatal(errors.New("MONGO_URI and MONGO_DATABASE are required"))
	}
	if isLocalMongoURI(mongoURI) {
		fatal(errors.New("refusing fixture action: MONGO_URI must not point at a local MongoDB"))
	}

	// This exact marker is intentionally printed after both safety guards and
	// before constructing services or performing the first mutation.
	fmt.Printf("FIXTURE TARGET\naction: %s\ncompanyId: %s\nprojectId: %s\nspaceId: %s\ncaptureId: %s\nroomDraftId: %s\n", request.Action, request.Target.CompanyID, request.Target.ProjectID, request.Target.SpaceID, request.Target.CaptureID, request.Target.RoomDraftID)

	mongoClient, err := platformmongo.Connect(mongoURI)
	if err != nil {
		fatal(fmt.Errorf("connect MongoDB: %w", err))
	}
	defer func() {
		_ = platformmongo.Disconnect(context.Background(), mongoClient)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := platformmongo.Ping(ctx, mongoClient); err != nil {
		fatal(fmt.Errorf("ping MongoDB: %w", err))
	}
	service := newSpatialFixtureService(platformmongo.Database(mongoClient, mongoDatabase))
	switch request.Action {
	case fixtureActionSeed:
		result, seedErr := seedKitchenFixture(ctx, service, request.Target)
		if seedErr != nil {
			fatal(seedErr)
		}
		fmt.Printf("SEEDED FIXTURE\ncompanyId: %s\nprojectId: %s\nspaceId: %s\ncaptureId: %s\nroomDraftId: %s\nwebPath: /projects/%s/spaces/%s/spatial/%s\n", request.Target.CompanyID, request.Target.ProjectID, request.Target.SpaceID, result.Capture.ID, result.RoomDraft.ID, request.Target.ProjectID, request.Target.SpaceID, result.RoomDraft.ID)
	case fixtureActionCleanup:
		result, cleanupErr := cleanupKitchenFixture(ctx, service, request.Target)
		if cleanupErr != nil {
			fatal(cleanupErr)
		}
		fmt.Printf("CLEANED FIXTURE\ncompanyId: %s\ncaptureId: %s\nroomDraftId: %s\nroomDraftEditRecordsRemoved: %d\ncaptureRoomDraftAssociationCleared: true\n", request.Target.CompanyID, result.CaptureID, result.RoomDraftID, result.EditRecordCount)
	}
}

// newSpatialFixtureService constructs only the established spatial domain
// services this tool uses. It does not expose repositories to the caller and
// deliberately omits unrelated production subsystems and their secrets.
func newSpatialFixtureService(db *mongo.Database) *spatial.Service {
	captures := spatial.NewMongoCaptureRepository(db)
	versions := spatial.NewMongoRoomVersionRepository(db)
	spaceStates := spatial.NewMongoSpaceStateRepository(db)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	service := spatial.NewService(captures, versions, spaceStates, nil)
	service.SetRoomDraftSupport(drafts)
	service.SetRoomDraftEditSupport(edits)
	return service
}

func isLocalMongoURI(uri string) bool {
	lower := strings.ToLower(uri)
	for _, marker := range []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "seed-spatial-fixture:", err)
	os.Exit(1)
}

func parseSeedRequest(args []string, allowProductionSeed bool) (seedTarget, error) {
	request, err := parseFixtureActionRequest(append([]string{string(fixtureActionSeed)}, args...), allowProductionSeed)
	return request.Target, err
}

func parseFixtureActionRequest(args []string, allowProductionSeed bool) (fixtureActionRequest, error) {
	if !allowProductionSeed {
		return fixtureActionRequest{}, fmt.Errorf("%s=true is required before this command can run", productionSeedGuard)
	}
	if len(args) == 0 || (args[0] != string(fixtureActionSeed) && args[0] != string(fixtureActionCleanup)) {
		return fixtureActionRequest{}, errors.New("usage: seed-spatial-fixture <seed|cleanup> [flags]")
	}
	action := fixtureAction(args[0])
	flags := flag.NewFlagSet("seed-spatial-fixture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	companyID := flags.String("company-id", "", "explicit production company ID")
	projectID := flags.String("project-id", "", "explicit dedicated test project ID")
	spaceID := flags.String("space-id", "", "explicit dedicated Kitchen space ID")
	captureID := flags.String("capture-id", "", "explicit normal-API-created capture ID")
	roomDraftID := flags.String("room-draft-id", "", "exact fixture RoomDraft ID reported by seed")
	confirm := flags.Bool("confirm-production-seed", false, "required explicit mutation confirmation")
	if err := flags.Parse(args[1:]); err != nil {
		return fixtureActionRequest{}, err
	}
	if !*confirm {
		return fixtureActionRequest{}, errors.New("--confirm-production-seed is required")
	}
	target := seedTarget{CompanyID: *companyID, ProjectID: *projectID, SpaceID: *spaceID, CaptureID: *captureID, RoomDraftID: *roomDraftID}
	if target.CompanyID == "" || target.CaptureID == "" {
		return fixtureActionRequest{}, errors.New("--company-id and --capture-id are required")
	}
	if action == fixtureActionSeed && (target.ProjectID == "" || target.SpaceID == "") {
		return fixtureActionRequest{}, errors.New("seed requires --project-id and --space-id")
	}
	if action == fixtureActionCleanup && target.RoomDraftID == "" {
		return fixtureActionRequest{}, errors.New("cleanup requires --room-draft-id")
	}
	return fixtureActionRequest{Action: action, Target: target}, nil
}

func goldenKitchenFixture() kitchenFixture {
	const height = 2.7
	const thickness = 0.12
	identity := spatial.RoomLocalQuaternion{W: 1}
	provenance := func(sourceID string) spatial.ElementProvenance {
		return spatial.ElementProvenance{Provider: spatial.SourceProviderFixture, SourceElementIdentifier: sourceID, SourceCaptureIdentifier: "fixture-test-kitchen-v1"}
	}
	point := func(x, y, z float64) spatial.RoomLocalPoint { return spatial.RoomLocalPoint{X: x, Y: y, Z: z} }
	transform := func(x, y, z float64) spatial.RoomLocalTransform {
		return spatial.RoomLocalTransform{Position: point(x, y, z), Rotation: identity}
	}

	return kitchenFixture{
		Walls: []spatial.RoomDraftWall{
			{ID: "wall_test_kitchen_south_v1", Start: point(0, 0, 0), End: point(5, 0, 0), Height: ptr(height), Thickness: ptr(thickness), ThicknessStatus: spatial.MeasurementStatusEstimated, Provenance: provenance("fixture-wall-south")},
			{ID: "wall_test_kitchen_east_v1", Start: point(5, 0, 0), End: point(5, 0, 4), Height: ptr(height), Thickness: ptr(thickness), ThicknessStatus: spatial.MeasurementStatusEstimated, Provenance: provenance("fixture-wall-east")},
			{ID: "wall_test_kitchen_north_v1", Start: point(5, 0, 4), End: point(0, 0, 4), Height: ptr(height), Thickness: ptr(thickness), ThicknessStatus: spatial.MeasurementStatusEstimated, Provenance: provenance("fixture-wall-north")},
			{ID: "wall_test_kitchen_west_v1", Start: point(0, 0, 4), End: point(0, 0, 0), Height: ptr(height), Thickness: ptr(thickness), ThicknessStatus: spatial.MeasurementStatusEstimated, Provenance: provenance("fixture-wall-west")},
		},
		Openings: []spatial.RoomDraftOpening{
			{ID: "opening_test_kitchen_door_v1", ParentWallID: "wall_test_kitchen_south_v1", Kind: spatial.OpeningKindDoor, Profile: spatial.OpeningProfileRectangle, Transform: transform(0.9, 1.05, 0), Width: ptr(0.9), Height: ptr(2.1), OffsetAlongWall: ptr(0.45), Door: &spatial.DoorMetadata{LeafCount: 1, Hinge: spatial.DoorHingeLeft, Swing: spatial.DoorSwingInward}, Provenance: provenance("fixture-door-south")},
			{ID: "opening_test_kitchen_window_v1", ParentWallID: "wall_test_kitchen_north_v1", Kind: spatial.OpeningKindWindow, Profile: spatial.OpeningProfileRectangle, Transform: transform(4.1, 1.6, 4), Width: ptr(1.2), Height: ptr(1.2), OffsetAlongWall: ptr(0.3), SillHeight: ptr(1.0), Provenance: provenance("fixture-window-north")},
		},
		Objects: []spatial.RoomDraftObject{
			{ID: "object_test_kitchen_island_v1", Category: "kitchen_island", Transform: transform(2.5, 0.45, 2.0), Dimensions: ptr(point(1.8, 0.9, 0.9)), Provenance: provenance("fixture-island")},
			{ID: "object_test_kitchen_refrigerator_v1", Category: "refrigerator", Transform: transform(4.45, 1.0, 3.35), Dimensions: ptr(point(0.7, 2.0, 0.7)), Provenance: provenance("fixture-refrigerator")},
			{ID: "object_test_kitchen_counter_v1", Category: "base_cabinet_counter", Transform: transform(2.5, 0.45, 3.65), Dimensions: ptr(point(4.2, 0.9, 0.65)), Provenance: provenance("fixture-counter-run")},
		},
		// The current fixed-fixture vocabulary has no separate sink category;
		// `other` is the canonical bounded category for this physical sink.
		Sink:            spatial.AddFixtureOperation{ID: "fixture_test_kitchen_sink_v1", Category: spatial.FixtureCategoryOther, Transform: transform(2.4, 0.9, 3.65), Dimensions: ptr(point(0.6, 0.4, 0.5))},
		ElectricalPoint: spatial.AddServicePointOperation{ID: "service_test_kitchen_electrical_v1", Kind: spatial.ServicePointKindElectrical, Position: point(0, 1.2, 2.5), ParentWallID: "wall_test_kitchen_west_v1"},
	}
}

func ptr[T any](value T) *T { return &value }

func seedKitchenFixture(ctx context.Context, service spatialFixtureSeeder, target seedTarget) (seedResult, error) {
	capture, err := service.GetCapture(ctx, target.CompanyID, target.CaptureID)
	if err != nil {
		return seedResult{}, fmt.Errorf("load capture: %w", err)
	}
	if capture.CompanyID != target.CompanyID || capture.ProjectID != target.ProjectID || capture.SpaceID != target.SpaceID {
		return seedResult{}, errors.New("capture does not belong to the explicit company/project/space target")
	}
	if !strings.HasPrefix(capture.ClientCaptureID, fixtureClientCaptureIDPrefix) {
		return seedResult{}, fmt.Errorf("capture must use a %q client capture ID marker", fixtureClientCaptureIDPrefix)
	}
	if capture.RoomDraftID != "" {
		return seedResult{}, fmt.Errorf("capture %s already has RoomDraft %s; refusing to mutate it", capture.ID, capture.RoomDraftID)
	}

	fixture := goldenKitchenFixture()
	draft, err := service.PersistRoomDraft(ctx, target.CompanyID, target.CaptureID, fixture.Walls, fixture.Openings, fixture.Objects, spatial.SourceProviderFixture)
	if err != nil {
		return seedResult{}, fmt.Errorf("persist fixture RoomDraft: %w", err)
	}
	if draft.ID == "" {
		return seedResult{}, errors.New("PersistRoomDraft returned an empty RoomDraft ID")
	}

	draft, err = submitFixtureEdit(ctx, service, target.CompanyID, draft, "seed-spatial-fixture-v1-add-sink", fixture.Sink)
	if err != nil {
		return seedResult{}, err
	}
	draft, err = submitFixtureEdit(ctx, service, target.CompanyID, draft, "seed-spatial-fixture-v1-add-electrical", fixture.ElectricalPoint)
	if err != nil {
		return seedResult{}, err
	}

	linkedCapture, err := service.GetCapture(ctx, target.CompanyID, target.CaptureID)
	if err != nil {
		return seedResult{}, fmt.Errorf("verify capture linkage: %w", err)
	}
	if linkedCapture.RoomDraftID != draft.ID {
		return seedResult{}, fmt.Errorf("capture RoomDraft linkage mismatch: got %q, want %q", linkedCapture.RoomDraftID, draft.ID)
	}
	verifiedDraft, err := service.GetRoomDraft(ctx, target.CompanyID, draft.ID)
	if err != nil {
		return seedResult{}, fmt.Errorf("verify persisted RoomDraft: %w", err)
	}
	if len(verifiedDraft.Walls) != 4 || len(verifiedDraft.Openings) != 2 || len(verifiedDraft.Objects) != 3 || len(verifiedDraft.Fixtures) != 1 || len(verifiedDraft.ServicePoints) != 1 {
		return seedResult{}, fmt.Errorf("persisted RoomDraft geometry is incomplete: walls=%d openings=%d objects=%d fixtures=%d servicePoints=%d", len(verifiedDraft.Walls), len(verifiedDraft.Openings), len(verifiedDraft.Objects), len(verifiedDraft.Fixtures), len(verifiedDraft.ServicePoints))
	}
	if verifiedDraft.OriginalBaseline == nil || verifiedDraft.Revision != draft.Revision {
		return seedResult{}, errors.New("persisted RoomDraft baseline or revision verification failed")
	}
	return seedResult{Capture: linkedCapture, RoomDraft: verifiedDraft}, nil
}

func cleanupKitchenFixture(ctx context.Context, service fixtureCleanupService, target seedTarget) (spatial.FixtureRoomDraftRemoval, error) {
	if target.CompanyID == "" || target.CaptureID == "" || target.RoomDraftID == "" {
		return spatial.FixtureRoomDraftRemoval{}, errors.New("cleanup requires company, capture, and RoomDraft IDs")
	}
	result, err := service.RemoveFixtureRoomDraft(ctx, target.CompanyID, target.CaptureID, target.RoomDraftID)
	if err != nil {
		return spatial.FixtureRoomDraftRemoval{}, fmt.Errorf("remove fixture RoomDraft: %w", err)
	}
	return result, nil
}

func submitFixtureEdit(ctx context.Context, service spatialFixtureSeeder, companyID string, draft spatial.RoomDraft, operationID string, op spatial.EditOperation) (spatial.RoomDraft, error) {
	payload, err := json.Marshal(op)
	if err != nil {
		return spatial.RoomDraft{}, fmt.Errorf("marshal %s: %w", operationID, err)
	}
	result, err := service.SubmitEditOperation(ctx, companyID, spatial.SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: operationID, Kind: op.OperationKind(), Payload: payload, ExpectedRevision: draft.Revision,
	})
	if err != nil {
		return spatial.RoomDraft{}, fmt.Errorf("submit %s: %w", operationID, err)
	}
	return result.RoomDraft, nil
}
