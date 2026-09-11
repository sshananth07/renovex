package spatial

import (
	"context"
	"errors"
	"testing"
)

// TestRemoveFixtureRoomDraft_RemovesOnlyMatchingFixture proves the production
// cleanup path cannot delete a normal RoomDraft: its capture association,
// capture ID, and fixture provenance must all match before the fixture-owned
// draft and its audit records are removed.
func TestRemoveFixtureRoomDraft_RemovesOnlyMatchingFixture(t *testing.T) {
	ctx := context.Background()
	captures := newFakeCaptureRepo()
	versions := newFakeRoomVersionRepo()
	states := newFakeSpaceStateRepo()
	lookup := newFakeSpaceLookup()
	drafts := newFakeRoomDraftRepo()
	edits := newFakeRoomDraftEditRepo(drafts)
	edits.captures = captures
	svc := NewService(captures, versions, states, lookup)
	svc.SetRoomDraftSupport(drafts)
	svc.SetRoomDraftEditSupport(edits)

	draft, err := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_test", CaptureID: "capture_test", SourceProvider: SourceProviderFixture,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	captures.byID["capture_test"] = SpatialCapture{
		ID: "capture_test", CompanyID: "company_test", ProjectID: "project_test", SpaceID: "space_test", RoomDraftID: draft.ID,
	}
	edits.recordsByOp[opKey("company_test", "fixture-op")] = RoomDraftEditRecord{
		ID: "edit_test", CompanyID: "company_test", RoomDraftID: draft.ID, OperationID: "fixture-op",
	}

	removed, err := svc.RemoveFixtureRoomDraft(ctx, "company_test", "capture_test", draft.ID)
	if err != nil {
		t.Fatalf("RemoveFixtureRoomDraft: %v", err)
	}
	if removed.RoomDraftID != draft.ID || removed.CaptureID != "capture_test" || removed.EditRecordCount != 1 {
		t.Fatalf("unexpected removal result: %+v", removed)
	}
	if _, err := drafts.FindByID(ctx, "company_test", draft.ID); !errors.Is(err, ErrRoomDraftNotFound) {
		t.Fatalf("fixture RoomDraft must be deleted, got %v", err)
	}
	remaining, err := edits.ListByRoomDraft(ctx, "company_test", draft.ID)
	if err != nil {
		t.Fatalf("list remaining edits: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("fixture audit edits must be deleted, got %+v", remaining)
	}
	capture, err := captures.FindByID(ctx, "company_test", "capture_test")
	if err != nil {
		t.Fatalf("load capture after cleanup: %v", err)
	}
	if capture.RoomDraftID != "" {
		t.Fatalf("capture RoomDraft association must be cleared, got %q", capture.RoomDraftID)
	}
}

func TestRemoveFixtureRoomDraft_RejectsNonFixtureWithoutMutation(t *testing.T) {
	ctx := context.Background()
	captures := newFakeCaptureRepo()
	versions := newFakeRoomVersionRepo()
	states := newFakeSpaceStateRepo()
	lookup := newFakeSpaceLookup()
	drafts := newFakeRoomDraftRepo()
	edits := newFakeRoomDraftEditRepo(drafts)
	edits.captures = captures
	svc := NewService(captures, versions, states, lookup)
	svc.SetRoomDraftSupport(drafts)
	svc.SetRoomDraftEditSupport(edits)

	draft, err := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_test", CaptureID: "capture_test", SourceProvider: SourceProviderRoomPlan,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	captures.byID["capture_test"] = SpatialCapture{ID: "capture_test", CompanyID: "company_test", RoomDraftID: draft.ID}

	_, err = svc.RemoveFixtureRoomDraft(ctx, "company_test", "capture_test", draft.ID)
	if !errors.Is(err, ErrFixtureRoomDraftNotRemovable) {
		t.Fatalf("expected ErrFixtureRoomDraftNotRemovable, got %v", err)
	}
	if _, err := drafts.FindByID(ctx, "company_test", draft.ID); err != nil {
		t.Fatalf("non-fixture RoomDraft must remain, got %v", err)
	}
	capture, err := captures.FindByID(ctx, "company_test", "capture_test")
	if err != nil {
		t.Fatalf("load capture: %v", err)
	}
	if capture.RoomDraftID != draft.ID {
		t.Fatalf("non-fixture capture association must remain, got %q", capture.RoomDraftID)
	}
}
