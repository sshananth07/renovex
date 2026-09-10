package spatial

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// --- fake VisualAssetVersionRepository ---

type fakeVisualAssetVersionRepo struct {
	byKey map[string]VisualAssetVersion // key: companyID + "|" + assetID + "|" + version
}

func newFakeVisualAssetVersionRepo() *fakeVisualAssetVersionRepo {
	return &fakeVisualAssetVersionRepo{byKey: map[string]VisualAssetVersion{}}
}

func visualAssetVersionKey(companyID, assetID string, version int) string {
	return fmt.Sprintf("%s|%s|%d", companyID, assetID, version)
}

func (r *fakeVisualAssetVersionRepo) Create(ctx context.Context, v VisualAssetVersion) (VisualAssetVersion, error) {
	key := visualAssetVersionKey(v.CompanyID, v.AssetID, v.Version)
	if existing, ok := r.byKey[key]; ok {
		if existing.Checksum == v.Checksum && existing.Format == v.Format && existing.ByteCount == v.ByteCount {
			return existing, nil
		}
		return VisualAssetVersion{}, ErrVisualAssetVersionConflict
	}
	v.ID = key
	r.byKey[key] = v
	return v, nil
}

func (r *fakeVisualAssetVersionRepo) FindByCompanyAssetVersion(ctx context.Context, companyID, assetID string, version int) (VisualAssetVersion, error) {
	v, ok := r.byKey[visualAssetVersionKey(companyID, assetID, version)]
	if !ok {
		return VisualAssetVersion{}, ErrVisualAssetVersionNotFound
	}
	return v, nil
}

func newTestRoomDraftEditServiceWithVisualAssets() (*Service, *fakeRoomDraftRepo, *fakeVisualAssetVersionRepo) {
	svc, drafts, _ := newTestRoomDraftEditService()
	visualAssets := newFakeVisualAssetVersionRepo()
	svc.SetVisualAssetSupport(visualAssets, nil)
	return svc, drafts, visualAssets
}

// --- assign_visual_asset service-level authorization ---

func TestSubmitEditOperation_AssignVisualAsset_RejectsNonexistentAsset(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditServiceWithVisualAssets()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
	})

	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "nonexistent-asset", Version: 1}
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpAssignVisualAsset, Payload: mustMarshal(t, op),
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrVisualAssetVersionNotFound) {
		t.Fatalf("expected ErrVisualAssetVersionNotFound, got %v", err)
	}
}

func TestSubmitEditOperation_AssignVisualAsset_RejectsOtherCompanyAsset(t *testing.T) {
	svc, drafts, visualAssets := newTestRoomDraftEditServiceWithVisualAssets()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
	})
	// Published under a DIFFERENT company.
	_, _ = visualAssets.Create(ctx, VisualAssetVersion{CompanyID: "company_b", AssetID: "boiler-asset", Version: 1, Format: VisualAssetFormatGLB})

	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "boiler-asset", Version: 1}
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpAssignVisualAsset, Payload: mustMarshal(t, op),
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrVisualAssetVersionNotFound) {
		t.Fatalf("expected ErrVisualAssetVersionNotFound (cross-company hidden as not-found), got %v", err)
	}
}

func TestSubmitEditOperation_AssignVisualAsset_SucceedsForSameCompanyAsset(t *testing.T) {
	svc, drafts, visualAssets := newTestRoomDraftEditServiceWithVisualAssets()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
	})
	_, _ = visualAssets.Create(ctx, VisualAssetVersion{CompanyID: "company_a", AssetID: "boiler-asset", Version: 1, Format: VisualAssetFormatGLB})

	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "boiler-asset", Version: 1}
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpAssignVisualAsset, Payload: mustMarshal(t, op),
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Fixtures[0].VisualAsset == nil || result.RoomDraft.Fixtures[0].VisualAsset.AssetID != "boiler-asset" {
		t.Fatalf("expected visual asset assigned, got %+v", result.RoomDraft.Fixtures[0].VisualAsset)
	}
}

func TestSubmitEditOperation_AssignVisualAsset_ReturnsErrorNotPanicWhenVisualAssetSupportUnwired(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService() // deliberately never calls SetVisualAssetSupport
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
	})

	op := AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "a", Version: 1}
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpAssignVisualAsset, Payload: mustMarshal(t, op),
		ExpectedRevision: draft.Revision,
	})
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
}

func TestSubmitEditOperation_ClearVisualAsset_RequiresNoAssetAuthorization(t *testing.T) {
	// clear_visual_asset needs no VisualAssetVersionRepository lookup at
	// all — nothing to authorize, it only removes a reference. Proven by
	// succeeding even with a repository that would reject everything.
	svc, drafts, _ := newTestRoomDraftEditService()
	svc.SetVisualAssetSupport(nil, nil) // no visualAssets repo wired at all
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, VisualAsset: &VisualAssetRef{AssetID: "a", Version: 1}}},
	})

	op := ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1"}
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpClearVisualAsset, Payload: mustMarshal(t, op),
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Fixtures[0].VisualAsset != nil {
		t.Fatalf("expected visual asset cleared, got %+v", result.RoomDraft.Fixtures[0].VisualAsset)
	}
}
