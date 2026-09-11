package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func TestParseSeedRequestRequiresBothProductionGuards(t *testing.T) {
	args := []string{
		"--company-id", "company_test",
		"--project-id", "project_test",
		"--space-id", "space_test",
		"--capture-id", "capture_test",
		"--confirm-production-seed",
	}

	if _, err := parseSeedRequest(args, false); err == nil {
		t.Fatal("expected RENOVEX_ALLOW_PROD_SEED guard to be required")
	}
	if _, err := parseSeedRequest(args[:len(args)-1], true); err == nil {
		t.Fatal("expected --confirm-production-seed guard to be required")
	}
}

func TestParseSeedRequestRequiresExplicitTargetIDs(t *testing.T) {
	_, err := parseSeedRequest([]string{
		"--company-id", "company_test",
		"--project-id", "project_test",
		"--space-id", "space_test",
		"--confirm-production-seed",
	}, true)
	if err == nil {
		t.Fatal("expected capture ID to be required")
	}
}

func TestParseFixtureActionRequestRequiresExactCleanupDraftID(t *testing.T) {
	_, err := parseFixtureActionRequest([]string{
		"cleanup",
		"--company-id", "company_test",
		"--project-id", "project_test",
		"--space-id", "space_test",
		"--capture-id", "capture_test",
		"--confirm-production-seed",
	}, true)
	if err == nil {
		t.Fatal("cleanup must require the exact fixture RoomDraft ID")
	}
}

func TestCleanupKitchenFixtureRemovesOnlyReportedFixtureIDs(t *testing.T) {
	service := &recordingFixtureCleanupService{}
	target := seedTarget{
		CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", CaptureID: "capture_test", RoomDraftID: "roomdraft_test",
	}
	result, err := cleanupKitchenFixture(context.Background(), service, target)
	if err != nil {
		t.Fatalf("cleanupKitchenFixture: %v", err)
	}
	if service.companyID != "company_test" || service.captureID != "capture_test" || service.roomDraftID != "roomdraft_test" {
		t.Fatalf("cleanup must use exactly the reported fixture IDs, got %+v", service)
	}
	if result.EditRecordCount != 2 || result.RoomDraftID != "roomdraft_test" {
		t.Fatalf("unexpected cleanup result: %+v", result)
	}
}

func TestGoldenKitchenFixtureUsesStableIDsAndCanonicalGeometry(t *testing.T) {
	fixture := goldenKitchenFixture()

	if len(fixture.Walls) != 4 || len(fixture.Openings) != 2 || len(fixture.Objects) != 3 {
		t.Fatalf("unexpected fixture geometry: walls=%d openings=%d objects=%d", len(fixture.Walls), len(fixture.Openings), len(fixture.Objects))
	}
	if fixture.Walls[0].ID != "wall_test_kitchen_south_v1" || fixture.Objects[0].ID != "object_test_kitchen_island_v1" {
		t.Fatalf("fixture IDs must be deterministic, got wall=%q object=%q", fixture.Walls[0].ID, fixture.Objects[0].ID)
	}
	if fixture.Walls[1].End.X != 5 || fixture.Walls[1].End.Y != 0 || fixture.Walls[1].End.Z != 4 {
		t.Fatalf("expected a 5m x 4m room in the Y-up frame, got %+v", fixture.Walls[1].End)
	}
	if fixture.Sink.ID != "fixture_test_kitchen_sink_v1" || fixture.ElectricalPoint.ID != "service_test_kitchen_electrical_v1" {
		t.Fatalf("fixture and service point IDs must be deterministic, got sink=%q electrical=%q", fixture.Sink.ID, fixture.ElectricalPoint.ID)
	}
}

func TestSeedKitchenFixtureUsesPersistThenAuditedEdits(t *testing.T) {
	service := &recordingSpatialSeeder{
		capture: spatial.SpatialCapture{ID: "capture_test", CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", ClientCaptureID: "test-spatial-kitchen-v1"},
	}
	result, err := seedKitchenFixture(context.Background(), service, seedTarget{
		CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", CaptureID: "capture_test",
	})
	if err != nil {
		t.Fatalf("seedKitchenFixture: %v", err)
	}
	if service.persistCalls != 1 {
		t.Fatalf("expected PersistRoomDraft exactly once, got %d", service.persistCalls)
	}
	if len(service.editInputs) != 2 {
		t.Fatalf("expected sink and electrical audited edits, got %d", len(service.editInputs))
	}
	if service.editInputs[0].Kind != spatial.EditOpAddFixture || service.editInputs[0].ExpectedRevision != 1 {
		t.Fatalf("unexpected first edit: %+v", service.editInputs[0])
	}
	if service.editInputs[1].Kind != spatial.EditOpAddServicePoint || service.editInputs[1].ExpectedRevision != 2 {
		t.Fatalf("unexpected second edit: %+v", service.editInputs[1])
	}
	if result.RoomDraft.ID != "roomdraft_test" || result.RoomDraft.Revision != 3 || result.Capture.RoomDraftID != "roomdraft_test" {
		t.Fatalf("unexpected verified linkage: %+v", result)
	}
}

func TestSeedKitchenFixtureRefusesCaptureThatAlreadyHasDraft(t *testing.T) {
	service := &recordingSpatialSeeder{
		capture: spatial.SpatialCapture{ID: "capture_test", CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", ClientCaptureID: "test-spatial-kitchen-v1", RoomDraftID: "existing_draft"},
	}
	_, err := seedKitchenFixture(context.Background(), service, seedTarget{
		CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", CaptureID: "capture_test",
	})
	if err == nil {
		t.Fatal("expected an already-linked capture to be rejected")
	}
	if service.persistCalls != 0 || len(service.editInputs) != 0 {
		t.Fatal("existing capture must not be mutated")
	}
}

func TestSeedKitchenFixtureRefusesUnmarkedCapture(t *testing.T) {
	service := &recordingSpatialSeeder{
		capture: spatial.SpatialCapture{ID: "capture_test", CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", ClientCaptureID: "ordinary-capture"},
	}
	_, err := seedKitchenFixture(context.Background(), service, seedTarget{
		CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", CaptureID: "capture_test",
	})
	if err == nil {
		t.Fatal("expected an unmarked capture to be refused")
	}
	if service.persistCalls != 0 {
		t.Fatal("an unmarked capture must not be mutated")
	}
}

type recordingSpatialSeeder struct {
	capture      spatial.SpatialCapture
	draft        spatial.RoomDraft
	persistCalls int
	editInputs   []spatial.SubmitEditOperationInput
}

func (s *recordingSpatialSeeder) GetCapture(_ context.Context, _ string, _ string) (spatial.SpatialCapture, error) {
	return s.capture, nil
}

func (s *recordingSpatialSeeder) PersistRoomDraft(_ context.Context, _ string, _ string, walls []spatial.RoomDraftWall, openings []spatial.RoomDraftOpening, objects []spatial.RoomDraftObject, _ spatial.SourceProvider) (spatial.RoomDraft, error) {
	s.persistCalls++
	s.draft = spatial.RoomDraft{
		ID: "roomdraft_test", Walls: walls, Openings: openings, Objects: objects, Revision: 1,
		OriginalBaseline: &spatial.RoomDraftBaseline{Walls: walls, Openings: openings, Objects: objects},
	}
	s.capture.RoomDraftID = s.draft.ID
	return s.draft, nil
}

func (s *recordingSpatialSeeder) SubmitEditOperation(_ context.Context, _ string, input spatial.SubmitEditOperationInput) (spatial.SubmitEditOperationResult, error) {
	s.editInputs = append(s.editInputs, input)
	s.draft.Revision++
	if input.Kind == spatial.EditOpAddFixture {
		var op spatial.AddFixtureOperation
		_ = json.Unmarshal(input.Payload, &op)
		s.draft.Fixtures = append(s.draft.Fixtures, spatial.RoomDraftFixture{ID: op.ID})
	}
	if input.Kind == spatial.EditOpAddServicePoint {
		var op spatial.AddServicePointOperation
		_ = json.Unmarshal(input.Payload, &op)
		s.draft.ServicePoints = append(s.draft.ServicePoints, spatial.RoomDraftServicePoint{ID: op.ID})
	}
	return spatial.SubmitEditOperationResult{RoomDraft: s.draft}, nil
}

func (s *recordingSpatialSeeder) GetRoomDraft(_ context.Context, _ string, _ string) (spatial.RoomDraft, error) {
	return s.draft, nil
}

type recordingFixtureCleanupService struct {
	companyID   string
	captureID   string
	roomDraftID string
}

func (s *recordingFixtureCleanupService) RemoveFixtureRoomDraft(_ context.Context, companyID, captureID, roomDraftID string) (spatial.FixtureRoomDraftRemoval, error) {
	s.companyID = companyID
	s.captureID = captureID
	s.roomDraftID = roomDraftID
	return spatial.FixtureRoomDraftRemoval{CaptureID: captureID, RoomDraftID: roomDraftID, EditRecordCount: 2}, nil
}
