package spatial_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func mustMarshalEdit(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("unexpected marshal error: %v", err)
	}
	return b
}

// newTestEditRecord always computes a REAL OperationFingerprint via
// spatial.ComputeOperationFingerprintForTest — never leaves it as a
// zero-value empty string, which would make two genuinely different
// operations compare as identical and silently defeat any test that
// depends on the fingerprint conflict/adoption distinction.
func newTestEditRecord(t *testing.T, companyID, roomDraftID, operationID string, kind spatial.EditOperationKind, op any, baseRevision int64) spatial.RoomDraftEditRecord {
	t.Helper()
	payload := mustMarshalEdit(t, op)
	return spatial.RoomDraftEditRecord{
		CompanyID: companyID, RoomDraftID: roomDraftID, OperationID: operationID,
		OperationKind: string(kind), OperationPayload: string(payload),
		OperationFingerprint: spatial.ComputeOperationFingerprintForTest(kind, payload),
		BaseRevision:         baseRevision, ResultingRevision: baseRevision + 1,
		CreatedAt: time.Now(), SchemaVersion: 1,
	}
}

func newTestRoomDraft(t *testing.T, ctx context.Context, drafts *spatial.MongoRoomDraftRepository, companyID string) spatial.RoomDraft {
	t.Helper()
	draft, err := drafts.Create(ctx, spatial.RoomDraft{
		CompanyID: companyID, CaptureID: "capture_1",
		Walls:          []spatial.RoomDraftWall{{ID: "wall_1", Start: spatial.RoomLocalPoint{}, End: spatial.RoomLocalPoint{X: 4}}},
		SourceProvider: spatial.SourceProviderRoomPlan,
		CreatedAt:      time.Now(), UpdatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating draft: %v", err)
	}
	return draft
}

// TestMongoRoomDraftEditRepository_ApplyAndRecord_AtomicallyUpdatesDraftAndInsertsRecord
// proves plan §RP4B's core atomicity requirement: a successful call updates
// BOTH the RoomDraft (CAS-guarded revision increment) and inserts the
// RoomDraftEditRecord, using a REAL Mongo multi-document transaction — not
// just application-level sequencing.
func TestMongoRoomDraftEditRepository_ApplyAndRecord_AtomicallyUpdatesDraftAndInsertsRecord(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")
	op := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: spatial.MeasurementStatusEstimated}
	updated, err := op.Apply(draft)
	if err != nil {
		t.Fatalf("unexpected error applying op: %v", err)
	}

	record := newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, op, draft.Revision)

	resultDraft, resultRecord, replayed, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated, draft.Revision, record)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replayed {
		t.Fatal("expected a fresh application, not a replay")
	}
	if resultDraft.Revision != draft.Revision+1 {
		t.Fatalf("expected revision incremented, got %d", resultDraft.Revision)
	}
	if resultRecord.ID == "" {
		t.Fatal("expected a non-empty record ID")
	}

	// Confirm BOTH sides actually persisted, independent of the returned
	// values — reopen from scratch.
	reopenedDraft, err := drafts.FindByID(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reopenedDraft.Revision != draft.Revision+1 {
		t.Fatalf("expected persisted revision %d, got %d", draft.Revision+1, reopenedDraft.Revision)
	}
	if reopenedDraft.Walls[0].Thickness == nil || *reopenedDraft.Walls[0].Thickness != 0.2 {
		t.Fatalf("expected persisted thickness change, got %+v", reopenedDraft.Walls[0].Thickness)
	}

	reopenedRecord, err := edits.FindByOperationID(ctx, "company_a", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reopenedRecord.RoomDraftID != draft.ID {
		t.Fatalf("expected persisted record roomDraftID %s, got %s", draft.ID, reopenedRecord.RoomDraftID)
	}
}

// TestMongoRoomDraftEditRepository_ApplyAndRecord_StaleRevisionRejected proves
// the CAS guard rejects a stale expectedRevision WITHOUT inserting an edit
// record — an unrecorded rejection is correct; an unrecorded SUCCESS would
// not be.
func TestMongoRoomDraftEditRepository_ApplyAndRecord_StaleRevisionRejected(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")
	op := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: spatial.MeasurementStatusEstimated}
	updated, _ := op.Apply(draft)

	record := newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, op, draft.Revision+99)

	_, _, _, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated, draft.Revision+99, record)
	if err != spatial.ErrRoomDraftRevisionMismatch {
		t.Fatalf("expected ErrRoomDraftRevisionMismatch, got %v", err)
	}

	// No edit record must exist — the rejected attempt left no trace.
	_, findErr := edits.FindByOperationID(ctx, "company_a", "op_1")
	if findErr != spatial.ErrRoomDraftEditRecordNotFound {
		t.Fatalf("expected no edit record to exist after a rejected CAS attempt, got %v", findErr)
	}
}

// TestMongoRoomDraftEditRepository_ApplyAndRecord_ConcurrentWriters_OnlyOneWins
// proves the CAS guard is race-safe under REAL concurrent Mongo writers —
// matching the standard established by
// TestMongoCaptureRepository_ClientCaptureIDConcurrentCreate_OnlyOneWins and
// TestMongoSpaceStateRepository_ConcurrentSetCurrentRoomVersion_OnlyOneWins.
func TestMongoRoomDraftEditRepository_ApplyAndRecord_ConcurrentWriters_OnlyOneWins(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")

	const concurrentWriters = 10
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrentWriters; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			op := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1 * float64(i+1), Status: spatial.MeasurementStatusEstimated}
			updated, applyErr := op.Apply(draft)
			if applyErr != nil {
				t.Errorf("writer %d: unexpected apply error: %v", i, applyErr)
				return
			}
			record := newTestEditRecord(t, "company_a", draft.ID, "op_writer_"+string(rune('a'+i)), spatial.EditOpSetWallThickness, op, draft.Revision)
			_, _, _, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated, draft.Revision, record)
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			} else if err != spatial.ErrRoomDraftRevisionMismatch {
				t.Errorf("writer %d: unexpected error: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if successCount != 1 {
		t.Fatalf("expected exactly 1 writer to win the CAS race from the same base revision, got %d", successCount)
	}
}

// TestMongoRoomDraftEditRepository_ApplyAndRecord_DuplicateOperationID_AdoptsExisting
// proves the duplicate-key recovery path against a REAL unique-index
// violation. ApplyAndRecord's own contract (see its doc comment) is that
// OperationID de-duplication happens via a genuine concurrent race inside
// the transaction — a SEQUENTIAL retry after the draft's revision has
// already moved on correctly fails CAS first (proven by
// ApplyAndRecord_StaleRevisionRejected above; that is Service
// .SubmitEditOperation's job to intercept via FindByOperationID BEFORE
// ever calling ApplyAndRecord, which is exactly what makes the sequential
// case behave correctly end-to-end without ApplyAndRecord itself needing
// to special-case it). This test proves the TRUE race: two goroutines
// racing from the SAME base revision with the SAME OperationID/fingerprint
// — exactly one must win the CAS+insert, and the other must adopt its
// result rather than erroring.
func TestMongoRoomDraftEditRepository_ApplyAndRecord_DuplicateOperationID_AdoptsExisting(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")
	op := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: spatial.MeasurementStatusEstimated}
	updated, _ := op.Apply(draft)

	makeRecord := func() spatial.RoomDraftEditRecord {
		return newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, op, draft.Revision)
	}

	type callResult struct {
		draft    spatial.RoomDraft
		record   spatial.RoomDraftEditRecord
		replayed bool
		err      error
	}
	results := make([]callResult, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, rec, replayed, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated, draft.Revision, makeRecord())
			results[i] = callResult{draft: d, record: rec, replayed: replayed, err: err}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, r.err)
		}
	}
	if results[0].replayed == results[1].replayed {
		t.Fatalf("expected exactly one call to be the fresh application and the other a replay, got replayed=%v and replayed=%v", results[0].replayed, results[1].replayed)
	}
	if results[0].record.ID != results[1].record.ID {
		t.Fatalf("expected both calls to agree on the same record ID, got %s vs %s", results[0].record.ID, results[1].record.ID)
	}
	if results[0].draft.Revision != results[1].draft.Revision {
		t.Fatalf("expected both calls to agree on the same resulting revision, got %d vs %d", results[0].draft.Revision, results[1].draft.Revision)
	}

	// The draft must be at exactly revision+1 — not double-applied by the race.
	final, err := drafts.FindByID(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Revision != draft.Revision+1 {
		t.Fatalf("expected exactly one revision increment despite two racing ApplyAndRecord calls, got %d", final.Revision)
	}
}

// TestMongoRoomDraftEditRepository_ApplyAndRecord_ConflictingFingerprint_ReturnsConflict
// proves the OperationID-reuse-with-different-content case. Per
// ApplyAndRecord's contract, this is exercised the same way Service
// .SubmitEditOperation exercises it in practice: the SECOND call supplies
// an ExpectedRevision that still matches the CURRENT draft state (i.e. the
// caller genuinely believes this is a fresh, valid edit against current
// state) but reuses an OperationID already recorded for different content
// — the duplicate-key branch must still catch this and refuse it as a
// conflict rather than silently applying a second, differently-shaped edit
// under the same key.
func TestMongoRoomDraftEditRepository_ApplyAndRecord_ConflictingFingerprint_ReturnsConflict(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")
	opA := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1, Status: spatial.MeasurementStatusEstimated}
	updatedA, _ := opA.Apply(draft)

	recordA := newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, opA, draft.Revision)
	updatedDraft, _, _, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updatedA, draft.Revision, recordA)
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	// Second call at the CURRENT (now-correct) revision, same OperationID,
	// DIFFERENT operation content/fingerprint — this is the actual
	// misuse case: reusing an idempotency key for an unrelated edit.
	opB := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.9, Status: spatial.MeasurementStatusEstimated}
	updatedB, _ := opB.Apply(updatedDraft)
	recordB := newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, opB, updatedDraft.Revision)
	_, _, _, err = edits.ApplyAndRecord(ctx, "company_a", draft.ID, updatedB, updatedDraft.Revision, recordB)
	if err != spatial.ErrOperationIDConflict {
		t.Fatalf("expected ErrOperationIDConflict, got %v", err)
	}
}

func TestMongoRoomDraftEditRepository_ListByRoomDraft_ReturnsMostRecentFirst(t *testing.T) {
	db := setupDB(t)
	drafts := spatial.NewMongoRoomDraftRepository(db)
	edits := spatial.NewMongoRoomDraftEditRepository(db)
	ctx := context.Background()
	if err := drafts.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := edits.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	draft := newTestRoomDraft(t, ctx, drafts, "company_a")
	op1 := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.1, Status: spatial.MeasurementStatusEstimated}
	updated1, _ := op1.Apply(draft)
	record1 := newTestEditRecord(t, "company_a", draft.ID, "op_1", spatial.EditOpSetWallThickness, op1, draft.Revision)
	updatedDraft, _, _, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated1, draft.Revision, record1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	op2 := spatial.SetWallThicknessOperation{WallID: "wall_1", Thickness: 0.2, Status: spatial.MeasurementStatusEstimated}
	updated2, _ := op2.Apply(updatedDraft)
	record2 := newTestEditRecord(t, "company_a", draft.ID, "op_2", spatial.EditOpSetWallThickness, op2, updatedDraft.Revision)
	if _, _, _, err := edits.ApplyAndRecord(ctx, "company_a", draft.ID, updated2, updatedDraft.Revision, record2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	history, err := edits.ListByRoomDraft(ctx, "company_a", draft.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 records, got %d", len(history))
	}
	if history[0].OperationID != "op_2" {
		t.Fatalf("expected most recent (op_2) first, got %s", history[0].OperationID)
	}
}
