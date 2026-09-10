package spatial

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
)

// --- fake ---

// fakeRoomDraftEditRepo implements both RoomDraftEditRecordRepository and
// RoomDraftEditApplier in-memory, mirroring MongoRoomDraftEditRepository's
// exact semantics: ApplyAndRecord is atomic (both the draft CAS-update and
// the record insert happen together, guarded by a mutex standing in for
// Mongo's transaction), and a duplicate OperationID resolves by adopting
// the existing record when fingerprints match, or ErrOperationIDConflict
// otherwise.
type fakeRoomDraftEditRepo struct {
	mu           sync.Mutex
	drafts       *fakeRoomDraftRepo
	recordsByOp  map[string]RoomDraftEditRecord // companyID+"|"+operationID -> record
	nextRecordID int
}

func newFakeRoomDraftEditRepo(drafts *fakeRoomDraftRepo) *fakeRoomDraftEditRepo {
	return &fakeRoomDraftEditRepo{drafts: drafts, recordsByOp: map[string]RoomDraftEditRecord{}}
}

func opKey(companyID, operationID string) string { return companyID + "|" + operationID }

func (f *fakeRoomDraftEditRepo) FindByOperationID(_ context.Context, companyID, operationID string) (RoomDraftEditRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.recordsByOp[opKey(companyID, operationID)]
	if !ok {
		return RoomDraftEditRecord{}, ErrRoomDraftEditRecordNotFound
	}
	return rec, nil
}

func (f *fakeRoomDraftEditRepo) ListByRoomDraft(_ context.Context, companyID, roomDraftID string) ([]RoomDraftEditRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []RoomDraftEditRecord
	for _, rec := range f.recordsByOp {
		if rec.CompanyID == companyID && rec.RoomDraftID == roomDraftID {
			out = append(out, rec)
		}
	}
	return out, nil
}

func (f *fakeRoomDraftEditRepo) ApplyAndRecord(ctx context.Context, companyID, roomDraftID string, updatedDraft RoomDraft, expectedRevision int64, record RoomDraftEditRecord) (RoomDraft, RoomDraftEditRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := opKey(companyID, record.OperationID)
	if existing, ok := f.recordsByOp[key]; ok {
		if existing.RoomDraftID != record.RoomDraftID || existing.OperationFingerprint != record.OperationFingerprint {
			return RoomDraft{}, RoomDraftEditRecord{}, false, ErrOperationIDConflict
		}
		draft, err := f.drafts.FindByID(ctx, companyID, roomDraftID)
		if err != nil {
			return RoomDraft{}, RoomDraftEditRecord{}, false, err
		}
		return draft, existing, true, nil
	}

	updated, err := f.drafts.Update(ctx, companyID, roomDraftID, updatedDraft, expectedRevision)
	if err != nil {
		return RoomDraft{}, RoomDraftEditRecord{}, false, err
	}

	f.nextRecordID++
	record.ID = "edit_" + string(rune('a'+f.nextRecordID))
	f.recordsByOp[key] = record
	return updated, record, false, nil
}

func newTestRoomDraftEditService() (*Service, *fakeRoomDraftRepo, *fakeRoomDraftEditRepo) {
	captures := newFakeCaptureRepo()
	versions := newFakeRoomVersionRepo()
	states := newFakeSpaceStateRepo()
	lookup := newFakeSpaceLookup()
	drafts := newFakeRoomDraftRepo()
	edits := newFakeRoomDraftEditRepo(drafts)
	svc := NewService(captures, versions, states, lookup)
	svc.SetRoomDraftSupport(drafts)
	svc.SetRoomDraftEditSupport(edits)
	return svc, drafts, edits
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	return b
}

// --- valid edit success / revision increment ---

func TestSubmitEditOperation_ValidEdit_IncrementsRevision(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "capture_1",
		Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}},
	})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision, ActorUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Revision != draft.Revision+1 {
		t.Fatalf("expected revision %d, got %d", draft.Revision+1, result.RoomDraft.Revision)
	}
	if result.RoomDraft.Walls[0].Thickness == nil || *result.RoomDraft.Walls[0].Thickness != 0.2 {
		t.Fatalf("expected thickness applied, got %+v", result.RoomDraft.Walls[0].Thickness)
	}
	if result.Replayed {
		t.Fatal("expected a fresh application, not a replay")
	}
	if result.Record.BaseRevision != draft.Revision || result.Record.ResultingRevision != draft.Revision+1 {
		t.Fatalf("expected record base/resulting revisions %d/%d, got %d/%d", draft.Revision, draft.Revision+1, result.Record.BaseRevision, result.Record.ResultingRevision)
	}
	if result.Record.ActorUserID != "user_1" {
		t.Fatalf("expected actor recorded, got %q", result.Record.ActorUserID)
	}
}

// --- each operation kind reaches the application layer (representative sample across categories) ---

func TestSubmitEditOperation_EachOperationCategoryReachesApplicationLayer(t *testing.T) {
	cases := []struct {
		name              string
		kind              EditOperationKind
		op                any
		setupVisualAssets func(*testing.T, context.Context, *fakeVisualAssetVersionRepo)
		setup             func(*testing.T, context.Context, *fakeRoomDraftRepo) RoomDraft
		check             func(*testing.T, RoomDraft)
	}{
		{
			name: "move_wall",
			kind: EditOpMoveWall,
			op:   MoveWallOperation{WallID: "wall_1", Delta: RoomLocalPoint{X: 1}},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}}})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if d.Walls[0].Start.X != 1 {
					t.Fatalf("expected wall moved, got %+v", d.Walls[0].Start)
				}
			},
		},
		{
			name: "add_object",
			kind: EditOpAddObject,
			op:   AddObjectOperation{ID: "object_1", Category: "sofa"},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.Objects) != 1 {
					t.Fatalf("expected object added, got %+v", d.Objects)
				}
			},
		},
		{
			name: "add_fixture",
			kind: EditOpAddFixture,
			op:   AddFixtureOperation{ID: "fixture_1", Category: FixtureCategoryBoiler},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.Fixtures) != 1 {
					t.Fatalf("expected fixture added, got %+v", d.Fixtures)
				}
			},
		},
		{
			name: "add_service_point",
			kind: EditOpAddServicePoint,
			op:   AddServicePointOperation{ID: "sp_1", Kind: ServicePointKindElectrical},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.ServicePoints) != 1 {
					t.Fatalf("expected service point added, got %+v", d.ServicePoints)
				}
			},
		},
		{
			name: "add_constraint",
			kind: EditOpAddConstraint,
			op:   AddConstraintOperation{ID: "constraint_1", Kind: ConstraintKindColumn},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.Constraints) != 1 {
					t.Fatalf("expected constraint added, got %+v", d.Constraints)
				}
			},
		},
		{
			name: "apply_verified_measurement",
			kind: EditOpApplyVerifiedMeasurement,
			op: ApplyVerifiedMeasurementOperation{
				TargetKind: MeasurementTargetWall, TargetID: "wall_1", Field: MeasurementFieldHeight,
				Value: 2.4, Status: MeasurementStatusEstimated,
			},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if d.Walls[0].Height == nil || *d.Walls[0].Height != 2.4 {
					t.Fatalf("expected height applied, got %+v", d.Walls[0].Height)
				}
			},
		},
		{
			name: "remove_fixture",
			kind: EditOpRemoveFixture,
			op:   RemoveFixtureOperation{FixtureID: "fixture_1"},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{
					CompanyID: "company_a", CaptureID: "c1",
					Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
				})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.Fixtures) != 0 {
					t.Fatalf("expected fixture removed, got %+v", d.Fixtures)
				}
			},
		},
		{
			name: "remove_service_point",
			kind: EditOpRemoveServicePoint,
			op:   RemoveServicePointOperation{ServicePointID: "sp_1"},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{
					CompanyID: "company_a", CaptureID: "c1",
					ServicePoints: []RoomDraftServicePoint{{ID: "sp_1", Kind: ServicePointKindPlumbing, CreatedBy: ElementOriginContractor}},
				})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.ServicePoints) != 0 {
					t.Fatalf("expected service point removed, got %+v", d.ServicePoints)
				}
			},
		},
		{
			name: "remove_constraint",
			kind: EditOpRemoveConstraint,
			op:   RemoveConstraintOperation{ConstraintID: "constraint_1"},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{
					CompanyID: "company_a", CaptureID: "c1",
					Constraints: []RoomDraftConstraint{{ID: "constraint_1", Kind: ConstraintKindColumn, CreatedBy: ElementOriginContractor}},
				})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if len(d.Constraints) != 0 {
					t.Fatalf("expected constraint removed, got %+v", d.Constraints)
				}
			},
		},
		{
			name: "assign_visual_asset",
			kind: EditOpAssignVisualAsset,
			op:   AssignVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1", AssetID: "boiler-asset", Version: 1},
			// assign_visual_asset needs a PUBLISHED VisualAssetVersion to
			// authorize against (RP4D's own service-level pre-flight
			// check) — pre-published directly against the shared
			// visualAssets fake this table test's harness now always
			// wires, matching the real Service.SubmitEditOperation
			// dispatch it's exercising.
			setupVisualAssets: func(t *testing.T, ctx context.Context, visualAssets *fakeVisualAssetVersionRepo) {
				if _, err := visualAssets.Create(ctx, VisualAssetVersion{CompanyID: "company_a", AssetID: "boiler-asset", Version: 1, Format: VisualAssetFormatGLB}); err != nil {
					t.Fatalf("unexpected error pre-publishing visual asset: %v", err)
				}
			},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{
					CompanyID: "company_a", CaptureID: "c1",
					Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}},
				})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if d.Fixtures[0].VisualAsset == nil || d.Fixtures[0].VisualAsset.AssetID != "boiler-asset" {
					t.Fatalf("expected visual asset assigned, got %+v", d.Fixtures[0].VisualAsset)
				}
			},
		},
		{
			name: "clear_visual_asset",
			kind: EditOpClearVisualAsset,
			op:   ClearVisualAssetOperation{TargetKind: VisualAssetTargetFixture, TargetID: "fixture_1"},
			setup: func(t *testing.T, ctx context.Context, drafts *fakeRoomDraftRepo) RoomDraft {
				d, _ := drafts.Create(ctx, RoomDraft{
					CompanyID: "company_a", CaptureID: "c1",
					Fixtures: []RoomDraftFixture{{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, VisualAsset: &VisualAssetRef{AssetID: "a", Version: 1}}},
				})
				return d
			},
			check: func(t *testing.T, d RoomDraft) {
				if d.Fixtures[0].VisualAsset != nil {
					t.Fatalf("expected visual asset cleared, got %+v", d.Fixtures[0].VisualAsset)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, drafts, visualAssets := newTestRoomDraftEditServiceWithVisualAssets()
			ctx := context.Background()
			if tc.setupVisualAssets != nil {
				tc.setupVisualAssets(t, ctx, visualAssets)
			}
			draft := tc.setup(t, ctx, drafts)

			result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
				RoomDraftID: draft.ID, OperationID: "op_" + tc.name, Kind: tc.kind, Payload: mustMarshal(t, tc.op),
				ExpectedRevision: draft.Revision,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tc.check(t, result.RoomDraft)
		})
	}
}

// --- invalid operation rejection ---

func TestSubmitEditOperation_RejectsUnsupportedKind(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})

	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOperationKind("bogus_kind"),
		Payload: json.RawMessage(`{}`), ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrUnsupportedEditOperationKind) {
		t.Fatalf("expected ErrUnsupportedEditOperationKind, got %v", err)
	}
}

func TestSubmitEditOperation_RejectsMalformedPayload(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})

	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpMoveWall,
		Payload: json.RawMessage(`{"delta": "not-a-point"}`), ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrMalformedEditOperationPayload) {
		t.Fatalf("expected ErrMalformedEditOperationPayload, got %v", err)
	}
}

func TestSubmitEditOperation_RejectsDomainValidationFailure(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: -1, Status: MeasurementStatusEstimated})
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrInvalidEditOperation) {
		t.Fatalf("expected ErrInvalidEditOperation, got %v", err)
	}
}

func TestSubmitEditOperation_RejectsMissingTarget(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "nonexistent", Thickness: 0.2, Status: MeasurementStatusEstimated})
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrEditTargetNotFound) {
		t.Fatalf("expected ErrEditTargetNotFound, got %v", err)
	}
}

// --- stale revision conflict ---

func TestSubmitEditOperation_StaleRevision_ReturnsConflict(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision + 99,
	})
	if !errors.Is(err, ErrRoomDraftRevisionMismatch) {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}
}

// --- two concurrent writers from the same revision ---

func TestSubmitEditOperation_TwoConcurrentWritersFromSameRevision_OnlyOneSucceeds(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payloadA := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1, Status: MeasurementStatusEstimated})
	payloadB := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})

	var wg sync.WaitGroup
	var errA, errB error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errA = svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
			RoomDraftID: draft.ID, OperationID: "op_A", Kind: EditOpSetWallThickness, Payload: payloadA,
			ExpectedRevision: draft.Revision,
		})
	}()
	go func() {
		defer wg.Done()
		_, errB = svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
			RoomDraftID: draft.ID, OperationID: "op_B", Kind: EditOpSetWallThickness, Payload: payloadB,
			ExpectedRevision: draft.Revision,
		})
	}()
	wg.Wait()

	successCount := 0
	if errA == nil {
		successCount++
	} else if !errors.Is(errA, ErrRoomDraftRevisionMismatch) {
		t.Fatalf("writer A: unexpected error: %v", errA)
	}
	if errB == nil {
		successCount++
	} else if !errors.Is(errB, ErrRoomDraftRevisionMismatch) {
		t.Fatalf("writer B: unexpected error: %v", errB)
	}
	if successCount != 1 {
		t.Fatalf("expected exactly 1 writer to succeed from the same base revision, got %d", successCount)
	}
}

// --- retry of successful request after simulated response loss ---

func TestSubmitEditOperation_RetryAfterSimulatedResponseLoss_ReplaysOriginalResult(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	input := SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	}

	first, err := svc.SubmitEditOperation(ctx, "company_a", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Retry: same OperationID, same payload, SAME (now stale) ExpectedRevision
	// — simulating a client that never saw the first response and retries
	// with its original request exactly as sent.
	retry, err := svc.SubmitEditOperation(ctx, "company_a", input)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	if !retry.Replayed {
		t.Fatal("expected the retry to be marked as replayed")
	}
	if retry.RoomDraft.Revision != first.RoomDraft.Revision {
		t.Fatalf("expected retry to return the same resulting revision %d, got %d", first.RoomDraft.Revision, retry.RoomDraft.Revision)
	}
	if retry.Record.ID != first.Record.ID {
		t.Fatalf("expected retry to return the same edit record, got different IDs %s vs %s", first.Record.ID, retry.Record.ID)
	}

	// Prove the operation was NOT applied twice: only one further 0.2->X
	// change would double-apply if this were broken, but there's a simpler
	// direct check — the draft's revision only advanced by 1 total across
	// both calls.
	final, err := drafts.FindByID(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Revision != draft.Revision+1 {
		t.Fatalf("expected exactly one revision increment across both calls, got revision %d (started at %d)", final.Revision, draft.Revision)
	}
}

// --- conflicting idempotency-key reuse ---

func TestSubmitEditOperation_ConflictingOperationIDReuse_ReturnsConflict(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payloadA := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1, Status: MeasurementStatusEstimated})
	_, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payloadA,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Same OperationID, but genuinely DIFFERENT operation content.
	payloadB := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.9, Status: MeasurementStatusEstimated})
	_, err = svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payloadB,
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrOperationIDConflict) {
		t.Fatalf("expected ErrOperationIDConflict, got %v", err)
	}
}

// --- reset-to-scan through transport ---

func TestSubmitResetToScan_ThroughTransport_RestoresBaselineAndAdvancesRevision(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineWall := RoomDraftWall{ID: "wall_original", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Walls:            []RoomDraftWall{baselineWall},
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{baselineWall}},
	})
	edited := draft
	edited.Walls[0].Start = RoomLocalPoint{X: 99}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, edited, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error setting up edited state: %v", err)
	}

	result, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_1", updated.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Walls[0].Start != (RoomLocalPoint{}) {
		t.Fatalf("expected wall restored to baseline, got %+v", result.RoomDraft.Walls[0].Start)
	}
	if result.RoomDraft.Revision != updated.Revision+1 {
		t.Fatalf("expected revision advanced exactly once, got %d", result.RoomDraft.Revision)
	}
	if result.Record.OperationKind != "reset_to_scan" {
		t.Fatalf("expected operationKind reset_to_scan, got %s", result.Record.OperationKind)
	}
}

func TestSubmitResetToScan_ClearsObjectAppearanceAndAllFixtureAppearance(t *testing.T) {
	// RP4E2/M8.5C amendment: Reset-to-Scan restores captured objects to no
	// appearance override (baseline objects never had one — captured at
	// scan time, before any appearance could be set) and removes contractor
	// fixtures entirely (already-established RP4A behavior), so a fixture's
	// appearance is cleared as a side effect of the fixture itself
	// disappearing. No special-casing was needed for either — this test
	// proves that, rather than just asserting it by inspection.
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineObject := RoomDraftObject{ID: "object_1", Category: "sofa"}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID:        "company_a",
		CaptureID:        "c1",
		Objects:          []RoomDraftObject{baselineObject},
		OriginalBaseline: &RoomDraftBaseline{Objects: []RoomDraftObject{baselineObject}},
	})

	edited := draft
	edited.Objects[0].Appearance = appearancePtr(validAppearance())
	edited.Fixtures = []RoomDraftFixture{
		{ID: "fixture_1", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor, Appearance: appearancePtr(validAppearance())},
	}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, edited, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error setting up edited state: %v", err)
	}

	result, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_appearance", updated.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Objects[0].Appearance != nil {
		t.Fatalf("expected object appearance cleared by baseline restore, got %+v", result.RoomDraft.Objects[0].Appearance)
	}
	if len(result.RoomDraft.Fixtures) != 0 {
		t.Fatalf("expected all contractor fixtures cleared, got %+v", result.RoomDraft.Fixtures)
	}
}

func TestSubmitResetToScan_RetryWithSameOperationID_IsIdempotent(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineWall := RoomDraftWall{ID: "wall_original"}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{baselineWall}},
	})

	first, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_1", draft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	retry, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_1", draft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if !retry.Replayed {
		t.Fatal("expected retry to be replayed")
	}
	if retry.RoomDraft.Revision != first.RoomDraft.Revision {
		t.Fatalf("expected same resulting revision, got %d vs %d", first.RoomDraft.Revision, retry.RoomDraft.Revision)
	}

	final, err := drafts.FindByID(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Revision != draft.Revision+1 {
		t.Fatalf("expected exactly one revision increment across both calls, got %d", final.Revision)
	}
}

// --- tenant/company isolation ---

// --- Undo Reset-to-Scan (M8.5C reversible-editing patch, §11) ---
//
// Reset's own RoomDraftEditRecord.OperationPayload carries the pre-reset
// snapshot of the fields reset mutates (Walls/Openings/Objects/Fixtures/
// ServicePoints/Constraints) — server-captured from `existingDraft` inside
// submitAgainstRoomDraft, never trusted from the client. UndoResetToScan
// looks that record up by the reset's own operationId and restores the
// snapshot through the SAME CAS/idempotency pipeline every other mutation
// uses — no raw document replacement.

func TestUndoResetToScan_RestoresExactPreResetState(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineWall := RoomDraftWall{ID: "wall_original"}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Walls:            []RoomDraftWall{baselineWall},
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{baselineWall}},
	})

	editedWall := RoomDraftWall{ID: "wall_original", Start: RoomLocalPoint{X: 5}}
	preResetFixture := RoomDraftFixture{ID: "fixture_manual", Category: FixtureCategoryBoiler, CreatedBy: ElementOriginContractor}
	edited := draft
	edited.Walls = []RoomDraftWall{editedWall}
	edited.Fixtures = []RoomDraftFixture{preResetFixture}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, edited, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected error setting up pre-reset state: %v", err)
	}

	resetResult, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_undo", updated.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error resetting: %v", err)
	}
	if resetResult.RoomDraft.Walls[0] != baselineWall {
		t.Fatalf("expected reset to restore baseline wall, got %+v", resetResult.RoomDraft.Walls[0])
	}

	undoResult, err := svc.UndoResetToScan(ctx, "company_a", draft.ID, "reset_op_undo", "undo_op_1", resetResult.RoomDraft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error undoing reset: %v", err)
	}
	if undoResult.RoomDraft.Walls[0] != editedWall {
		t.Fatalf("expected undo to restore exact pre-reset wall, got %+v", undoResult.RoomDraft.Walls[0])
	}
	if len(undoResult.RoomDraft.Fixtures) != 1 || undoResult.RoomDraft.Fixtures[0] != preResetFixture {
		t.Fatalf("expected undo to restore exact pre-reset fixture, got %+v", undoResult.RoomDraft.Fixtures)
	}
	if undoResult.RoomDraft.Revision != resetResult.RoomDraft.Revision+1 {
		t.Fatalf("expected revision to advance forward, never decrement, got %d", undoResult.RoomDraft.Revision)
	}
}

func TestUndoResetToScan_RejectsWhenReferencedOperationIsNotAReset(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Objects:          []RoomDraftObject{{ID: "object_1", Category: "sofa"}},
		OriginalBaseline: &RoomDraftBaseline{Objects: []RoomDraftObject{{ID: "object_1", Category: "sofa"}}},
	})
	editResult, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "move_op_1",
		Kind: EditOpMoveObject, Payload: mustMarshal(t, MoveObjectOperation{ObjectID: "object_1", Position: RoomLocalPoint{X: 2}}),
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UndoResetToScan(ctx, "company_a", draft.ID, "move_op_1", "undo_op_1", editResult.RoomDraft.Revision, "user_1")
	if !errors.Is(err, ErrNotAResetOperation) {
		t.Fatalf("expected ErrNotAResetOperation, got %v", err)
	}
}

func TestUndoResetToScan_UnknownOperationID_ReturnsNotFound(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1"})

	_, err := svc.UndoResetToScan(ctx, "company_a", draft.ID, "reset_never_happened", "undo_op_1", draft.Revision, "user_1")
	if !errors.Is(err, ErrRoomDraftEditRecordNotFound) {
		t.Fatalf("expected ErrRoomDraftEditRecordNotFound, got %v", err)
	}
}

func TestUndoResetToScan_RetryWithSameOperationID_IsIdempotent(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	baselineWall := RoomDraftWall{ID: "wall_original"}
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Walls:            []RoomDraftWall{baselineWall},
		OriginalBaseline: &RoomDraftBaseline{Walls: []RoomDraftWall{baselineWall}},
	})
	edited := draft
	edited.Walls = []RoomDraftWall{{ID: "wall_original", Start: RoomLocalPoint{X: 5}}}
	updated, err := drafts.Update(ctx, "company_a", draft.ID, edited, draft.Revision)
	if err != nil {
		t.Fatalf("unexpected setup error: %v", err)
	}
	resetResult, err := svc.SubmitResetToScan(ctx, "company_a", draft.ID, "reset_op_idem", updated.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error resetting: %v", err)
	}

	first, err := svc.UndoResetToScan(ctx, "company_a", draft.ID, "reset_op_idem", "undo_op_idem", resetResult.RoomDraft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error on first undo: %v", err)
	}
	second, err := svc.UndoResetToScan(ctx, "company_a", draft.ID, "reset_op_idem", "undo_op_idem", resetResult.RoomDraft.Revision, "user_1")
	if err != nil {
		t.Fatalf("unexpected error on retried undo: %v", err)
	}
	if second.Record.ID != first.Record.ID || second.RoomDraft.Revision != first.RoomDraft.Revision {
		t.Fatalf("expected retry to replay the same result, got first=%+v second=%+v", first, second)
	}
}

func TestSubmitEditOperation_CrossTenantRoomDraft_RejectsAsNotFound(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	_, err := svc.SubmitEditOperation(ctx, "company_b", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if !errors.Is(err, ErrRoomDraftNotFound) {
		t.Fatalf("expected ErrRoomDraftNotFound for cross-tenant access, got %v", err)
	}
}

func TestSubmitEditOperation_SameOperationIDDifferentCompanies_DoNotCollide(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draftA, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})
	draftB, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_b", CaptureID: "c2", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	_, errA := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draftA.ID, OperationID: "shared_op_id", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draftA.Revision,
	})
	if errA != nil {
		t.Fatalf("unexpected error for company_a: %v", errA)
	}
	_, errB := svc.SubmitEditOperation(ctx, "company_b", SubmitEditOperationInput{
		RoomDraftID: draftB.ID, OperationID: "shared_op_id", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draftB.Revision,
	})
	if errB != nil {
		t.Fatalf("expected no collision across companies sharing an OperationID, got %v", errB)
	}
}

// --- stable Renovex identities ---

func TestSubmitEditOperation_PreservesStableIDsOfUntouchedElements(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{
		CompanyID: "company_a", CaptureID: "c1",
		Walls: []RoomDraftWall{
			{ID: "wall_stable_1", Start: RoomLocalPoint{}, End: RoomLocalPoint{X: 4}},
			{ID: "wall_stable_2", Start: RoomLocalPoint{X: 4}, End: RoomLocalPoint{X: 4, Z: 3}},
		},
	})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_stable_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RoomDraft.Walls[0].ID != "wall_stable_1" || result.RoomDraft.Walls[1].ID != "wall_stable_2" {
		t.Fatalf("expected stable wall IDs preserved, got %+v", result.RoomDraft.Walls)
	}
}

// --- operation history / audit record correctness ---

func TestListRoomDraftEdits_ReturnsAuditHistory(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload1 := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1, Status: MeasurementStatusEstimated})
	result1, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload1,
		ExpectedRevision: draft.Revision, ActorUserID: "user_1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload2 := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	_, err = svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_2", Kind: EditOpSetWallThickness, Payload: payload2,
		ExpectedRevision: result1.RoomDraft.Revision, ActorUserID: "user_2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	history, err := svc.ListRoomDraftEdits(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(history))
	}
	opIDs := map[string]bool{}
	for _, rec := range history {
		opIDs[rec.OperationID] = true
		if rec.RoomDraftID != draft.ID {
			t.Fatalf("expected roomDraftID %s on every record, got %s", draft.ID, rec.RoomDraftID)
		}
		if rec.OperationKind != string(EditOpSetWallThickness) {
			t.Fatalf("expected operationKind set_wall_thickness, got %s", rec.OperationKind)
		}
	}
	if !opIDs["op_1"] || !opIDs["op_2"] {
		t.Fatalf("expected both operation IDs present in history, got %+v", opIDs)
	}
}

// --- persistence/reopen at the new revision ---

func TestSubmitEditOperation_ReopenAfterApply_ShowsNewRevisionAndAppliedChange(t *testing.T) {
	svc, drafts, _ := newTestRoomDraftEditService()
	ctx := context.Background()
	draft, _ := drafts.Create(ctx, RoomDraft{CompanyID: "company_a", CaptureID: "c1", Walls: []RoomDraftWall{{ID: "wall_1"}}})

	payload := mustMarshal(t, SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: MeasurementStatusEstimated})
	result, err := svc.SubmitEditOperation(ctx, "company_a", SubmitEditOperationInput{
		RoomDraftID: draft.ID, OperationID: "op_1", Kind: EditOpSetWallThickness, Payload: payload,
		ExpectedRevision: draft.Revision,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reopened, err := svc.GetRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reopened.Revision != result.RoomDraft.Revision {
		t.Fatalf("expected reopened draft at revision %d, got %d", result.RoomDraft.Revision, reopened.Revision)
	}
	if reopened.Walls[0].Thickness == nil || *reopened.Walls[0].Thickness != 0.2 {
		t.Fatalf("expected applied change to persist across reopen, got %+v", reopened.Walls[0].Thickness)
	}
}
