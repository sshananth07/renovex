package ai

import (
	"context"
	"strings"
	"testing"
)

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func seedPendingSpaceSuggestion(t *testing.T, repo *fakeAIRepo, companyID, projectID string) AISuggestion {
	t.Helper()
	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID, Type: BatchTypeSpaceSuggestions,
		Status: BatchStatusProcessing, OperationID: "seed-op-" + projectID,
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{{
		CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
		Type: SuggestionTypeSpace, Status: SuggestionStatusPending,
		SuggestedData:    SuggestedData{Space: &SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen"}},
		InputFingerprint: "fp",
	}}); err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}
	if err := repo.MarkBatchCompleted(context.Background(), companyID, batch.ID); err != nil {
		t.Fatalf("seed complete: %v", err)
	}
	list, err := repo.ListSuggestionsByBatch(context.Background(), companyID, batch.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("seed list: %v / %d", err, len(list))
	}
	return list[0]
}

func TestRejectSuggestionSetsRejectedNoDomainObject(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	err := svc.RejectSuggestion(context.Background(), "company_a", sug.ID, sug.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusRejected {
		t.Fatalf("expected rejected, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != "" {
		t.Fatalf("expected no domain object created, got %s", updated.AcceptedDomainObjectID)
	}
}

func TestRejectSuggestionWrongRevisionReturns409(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	err := svc.RejectSuggestion(context.Background(), "company_a", sug.ID, sug.Revision+99)
	if err != ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch, got %v", err)
	}
}

func TestRejectSuggestionForeignReturns404(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	err := svc.RejectSuggestion(context.Background(), "company_b", sug.ID, sug.Revision)
	if err != ErrSuggestionNotFound {
		t.Fatalf("expected ErrSuggestionNotFound for cross-tenant reject (must not reveal existence via 409), got %v", err)
	}
}

func TestRejectSuggestionAlreadyTerminalRetryFails(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	if err := svc.RejectSuggestion(context.Background(), "company_a", sug.ID, sug.Revision); err != nil {
		t.Fatalf("unexpected error on first reject: %v", err)
	}
	err := svc.RejectSuggestion(context.Background(), "company_a", sug.ID, sug.Revision)
	if err != ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch on already-terminal retry, got %v", err)
	}
}

// --- Space acceptance ---

type fakeSpaceCreator struct {
	byID               map[string]SpaceAcceptanceResult
	bySourceSuggestion map[string]SpaceAcceptanceResult
	existingNormalized map[string]bool // normalize(name+type) -> exists
	nextID             int
	createErr          error
}

func newFakeSpaceCreator() *fakeSpaceCreator {
	return &fakeSpaceCreator{
		byID:               map[string]SpaceAcceptanceResult{},
		bySourceSuggestion: map[string]SpaceAcceptanceResult{},
		existingNormalized: map[string]bool{},
	}
}

func (f *fakeSpaceCreator) CreateSpaceFromAISuggestion(_ context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (SpaceAcceptanceResult, error) {
	if f.createErr != nil {
		return SpaceAcceptanceResult{}, f.createErr
	}
	if existing, ok := f.bySourceSuggestion[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	result := SpaceAcceptanceResult{ID: "space_" + string(rune('a'+f.nextID)), Name: name, SpaceType: spaceType}
	f.byID[result.ID] = result
	f.bySourceSuggestion[sourceSuggestionID] = result
	return result, nil
}

func (f *fakeSpaceCreator) FindSpaceBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (SpaceAcceptanceResult, error) {
	result, ok := f.bySourceSuggestion[sourceSuggestionID]
	if !ok {
		return SpaceAcceptanceResult{}, ErrSpaceAcceptanceNotFound
	}
	return result, nil
}

func (f *fakeSpaceCreator) SpaceLikelyDuplicate(_ context.Context, companyID, projectID, name, spaceType string) (bool, error) {
	return f.existingNormalized[normalize(name)+"|"+normalize(spaceType)], nil
}

func (f *fakeSpaceCreator) UpdateSpaceFromAISuggestion(_ context.Context, companyID, spaceID, name, spaceType, description string) (SpaceAcceptanceResult, error) {
	result := SpaceAcceptanceResult{ID: spaceID, Name: name, SpaceType: spaceType}
	f.byID[spaceID] = result
	return result, nil
}

func newSpaceAcceptanceService(repo *fakeAIRepo, creator *fakeSpaceCreator) *Service {
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	svc.SetSpaceCreator(creator)
	return svc
}

func TestAcceptSpaceSuggestionUnchangedCreatesRealSpace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	result, err := svc.AcceptSpaceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected a created Space ID")
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected accepted, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != result.ID {
		t.Fatalf("expected AcceptedDomainObjectID %s, got %s", result.ID, updated.AcceptedDomainObjectID)
	}
}

func TestUseSpaceDeltaSuggestionMutatesExistingSpaceInPlace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	creator.byID["space_existing"] = SpaceAcceptanceResult{ID: "space_existing", Name: "Bedroom", SpaceType: "bedroom"}
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	result, err := svc.UseSpaceDeltaSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "space_existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "space_existing" {
		t.Fatalf("expected mutation of the existing Space, got a new id %q (must never create a duplicate)", result.ID)
	}
	if result.Name != "Kitchen" {
		t.Fatalf("expected the Space updated to the suggestion's data, got %+v", result)
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusModified {
		t.Fatalf("expected modified, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != "space_existing" {
		t.Fatalf("expected AcceptedDomainObjectID space_existing, got %s", updated.AcceptedDomainObjectID)
	}
}

func TestListSuggestionsByBatchWithDeltaClassifiesFreshSpaceSuggestions(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	gw.existingSpaces["project_1"] = []DomainGatewaySpace{{ID: "space_kitchen", Name: "Kitchen", Type: "kitchen"}}
	svc := NewService(repo, gw, client)

	// Seed a NEW pending Space suggestion directly (Study — no current
	// authoritative match, no prior history).
	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: "company_a", ProjectID: "project_1", Type: BatchTypeSpaceSuggestions,
		Status: BatchStatusProcessing, OperationID: "op_delta_test", SourceBrief: "brief",
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: SuggestionTypeSpace, Status: SuggestionStatusPending,
			SuggestedData:    SuggestedData{Space: &SpaceSuggestionData{Name: "Study", SpaceType: "study", EvidenceType: EvidenceExplicit}},
			InputFingerprint: "fp",
		},
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: SuggestionTypeSpace, Status: SuggestionStatusPending,
			SuggestedData:    SuggestedData{Space: &SpaceSuggestionData{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit}},
			InputFingerprint: "fp",
		},
	}); err != nil {
		t.Fatalf("seed suggestions: %v", err)
	}
	if err := repo.MarkBatchCompleted(context.Background(), "company_a", batch.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	results, err := svc.ListSuggestionsByBatchWithDelta(context.Background(), "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	byName := map[string]SuggestionDelta{}
	for _, r := range results {
		byName[r.Suggestion.SuggestedData.Space.Name] = r
	}
	if byName["Study"].Classification != DeltaNew {
		t.Fatalf("expected Study to be NEW, got %+v", byName["Study"])
	}
	if byName["Kitchen"].Classification != DeltaUnchanged {
		t.Fatalf("expected Kitchen to be UNCHANGED (matches current authoritative Space), got %+v", byName["Kitchen"])
	}
	if byName["Kitchen"].CurrentAuthoritativeID != "space_kitchen" {
		t.Fatalf("expected Kitchen's authoritative id, got %+v", byName["Kitchen"])
	}
}

func TestListSuggestionsByBatchWithDeltaSkipsClassificationWhenNothingPending(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")
	// Terminalize it so nothing is pending.
	if err := repo.ConditionalReject(context.Background(), "company_a", sug.ID, sug.Revision); err != nil {
		t.Fatalf("seed reject: %v", err)
	}

	results, err := svc.ListSuggestionsByBatchWithDelta(context.Background(), "company_a", sug.BatchID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Classification != "" {
		t.Fatalf("expected no classification computed when nothing is pending, got %+v", results[0])
	}
}

func TestListSuggestionsByBatchWithDeltaClassifiesFreshWorkItemSuggestions(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestWorkGatewayAndClient()
	gw.existingWorkItems["project_1"] = append(gw.existingWorkItems["project_1"], DomainGatewayWorkItem{ID: "wi_existing", Description: "Site protection"})
	svc := NewService(repo, gw, client)

	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: "company_a", ProjectID: "project_1", Type: BatchTypeWorkItemSuggestions,
		Status: BatchStatusProcessing, OperationID: "op_wi_delta_test", SourceBrief: "brief",
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: SuggestionTypeWorkItem, Status: SuggestionStatusPending,
			SuggestedData:    SuggestedData{WorkItem: &WorkItemSuggestionData{Description: "Paint ceiling", ScopeLevel: ScopeLevelProject}},
			InputFingerprint: "fp",
		},
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: SuggestionTypeWorkItem, Status: SuggestionStatusPending,
			SuggestedData:    SuggestedData{WorkItem: &WorkItemSuggestionData{Description: "Site protection", ScopeLevel: ScopeLevelProject}},
			InputFingerprint: "fp",
		},
	}); err != nil {
		t.Fatalf("seed suggestions: %v", err)
	}
	if err := repo.MarkBatchCompleted(context.Background(), "company_a", batch.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	results, err := svc.ListSuggestionsByBatchWithDelta(context.Background(), "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	byDescription := map[string]SuggestionDelta{}
	for _, r := range results {
		byDescription[r.Suggestion.SuggestedData.WorkItem.Description] = r
	}
	if byDescription["Paint ceiling"].Classification != DeltaNew {
		t.Fatalf("expected Paint ceiling to be NEW, got %+v", byDescription["Paint ceiling"])
	}
	if byDescription["Site protection"].Classification != DeltaUnchanged {
		t.Fatalf("expected Site protection to be UNCHANGED, got %+v", byDescription["Site protection"])
	}
	if byDescription["Site protection"].CurrentAuthoritativeID != "wi_existing" {
		t.Fatalf("expected Site protection's authoritative id, got %+v", byDescription["Site protection"])
	}
}

func TestListSuggestionsByBatchWithDeltaClassifiesFreshResourceSuggestionsAsConflictNeverChanged(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	gw.existingRequirements["project_1"] = []DomainGatewayRequirement{{ID: "req_1", WorkItemID: "work_1", ResourceType: "trade", Name: "Tiler"}}
	svc := NewService(repo, gw, client)

	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: "company_a", ProjectID: "project_1", Type: BatchTypeResourceSuggestions,
		Status: BatchStatusProcessing, OperationID: "op_res_delta_test", SourceBrief: "brief",
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{
		{
			CompanyID: "company_a", ProjectID: "project_1", BatchID: batch.ID,
			Type: SuggestionTypeTradeResource, Status: SuggestionStatusPending,
			SuggestedData:    SuggestedData{TradeResource: &ResourceSuggestionData{WorkItemID: "work_1", Name: "Painter"}},
			InputFingerprint: "fp",
		},
	}); err != nil {
		t.Fatalf("seed suggestions: %v", err)
	}
	// Prior accepted suggestion links req_1 to a different name ("Tiler"
	// was accepted, current authoritative is still "Tiler") — a fresh
	// "Painter" suggestion for the same work item/resource type combo with
	// no matching current entity is just NEW here since there's no
	// trustworthy lineage from THIS suggestion to req_1. This test mainly
	// proves the wiring reaches ClassifyResourceDelta and never returns
	// DeltaChanged for any Resource suggestion.
	if err := repo.MarkBatchCompleted(context.Background(), "company_a", batch.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	results, err := svc.ListSuggestionsByBatchWithDelta(context.Background(), "company_a", batch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Classification == DeltaChanged {
		t.Fatalf("Resource suggestions must never be classified CHANGED, got %+v", results[0])
	}
}

func TestAcceptSpaceSuggestionOverrideMarksModified(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	override := &SpaceAcceptanceOverride{Name: "Master Bathroom", SpaceType: "bathroom"}
	_, err := svc.AcceptSpaceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, _ := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if updated.Status != SuggestionStatusModified {
		t.Fatalf("expected modified, got %s", updated.Status)
	}
}

func TestAcceptSpaceSuggestionDuplicateReturns409NoNewSpace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	creator.existingNormalized["kitchen|kitchen"] = true
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	_, err := svc.AcceptSpaceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, nil)
	if err != ErrSpaceLikelyDuplicate {
		t.Fatalf("expected ErrSpaceLikelyDuplicate, got %v", err)
	}
	if len(creator.byID) != 0 {
		t.Fatalf("expected no Space created, got %d", len(creator.byID))
	}
}

func TestAcceptSpaceSuggestionRetryAfterAmbiguousFailureRepairsAndReturnsSameSpace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	// Simulate: Space was created, but marking the suggestion terminal
	// failed (e.g. process crash) — the suggestion is still pending in the
	// repo, but a Space with this sourceSuggestionId already exists.
	preCreated, err := creator.CreateSpaceFromAISuggestion(context.Background(), "company_a", "project_1", "Kitchen", "kitchen", "", sug.ID)
	if err != nil {
		t.Fatalf("unexpected error pre-creating: %v", err)
	}

	result, err := svc.AcceptSpaceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, nil)
	if err != nil {
		t.Fatalf("unexpected error on repair retry: %v", err)
	}
	if result.ID != preCreated.ID {
		t.Fatalf("expected repair to return the pre-created Space %s, got %s", preCreated.ID, result.ID)
	}
	if len(creator.byID) != 1 {
		t.Fatalf("expected exactly one Space to exist, got %d", len(creator.byID))
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected repaired suggestion to be accepted, got %s", updated.Status)
	}
}

func TestAcceptSpaceSuggestionWrongRevisionReturnsConflict(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeSpaceCreator()
	svc := newSpaceAcceptanceService(repo, creator)
	sug := seedPendingSpaceSuggestion(t, repo, "company_a", "project_1")

	_, err := svc.AcceptSpaceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision+99, nil)
	if err != ErrSuggestionRevisionMismatch {
		t.Fatalf("expected ErrSuggestionRevisionMismatch, got %v", err)
	}
	if len(creator.byID) != 0 {
		t.Fatalf("expected no Space created for a rejected CAS, got %d", len(creator.byID))
	}
}

// --- WorkItem acceptance ---

type fakeWorkItemCreator struct {
	byID               map[string]WorkItemAcceptanceResult
	bySourceSuggestion map[string]WorkItemAcceptanceResult
	validSpaceIDs      map[string]bool
	duplicateKeys      map[string]bool // normalized description|scope -> exists
	nextID             int
}

func newFakeWorkItemCreator() *fakeWorkItemCreator {
	return &fakeWorkItemCreator{
		byID: map[string]WorkItemAcceptanceResult{}, bySourceSuggestion: map[string]WorkItemAcceptanceResult{},
		validSpaceIDs: map[string]bool{}, duplicateKeys: map[string]bool{},
	}
}

func (f *fakeWorkItemCreator) CreateWorkItemFromAISuggestion(_ context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (WorkItemAcceptanceResult, error) {
	if existing, ok := f.bySourceSuggestion[sourceSuggestionID]; ok {
		return existing, nil
	}
	if spaceID != nil && !f.validSpaceIDs[*spaceID] {
		return WorkItemAcceptanceResult{}, ErrWorkItemAcceptanceSpaceNotFound
	}
	f.nextID++
	result := WorkItemAcceptanceResult{ID: "work_" + string(rune('a'+f.nextID)), Description: description}
	f.byID[result.ID] = result
	f.bySourceSuggestion[sourceSuggestionID] = result
	return result, nil
}

func (f *fakeWorkItemCreator) FindWorkItemBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (WorkItemAcceptanceResult, error) {
	result, ok := f.bySourceSuggestion[sourceSuggestionID]
	if !ok {
		return WorkItemAcceptanceResult{}, ErrWorkItemAcceptanceNotFound
	}
	return result, nil
}

func (f *fakeWorkItemCreator) WorkItemLikelyDuplicate(_ context.Context, companyID, projectID string, spaceID *string, description string) (bool, error) {
	key := normalize(description)
	if spaceID != nil {
		key = *spaceID + "|" + key
	}
	return f.duplicateKeys[key], nil
}

func (f *fakeWorkItemCreator) UpdateWorkItemFromAISuggestion(_ context.Context, companyID, workItemID, description, workType string) (WorkItemAcceptanceResult, error) {
	result := WorkItemAcceptanceResult{ID: workItemID, Description: description}
	f.byID[workItemID] = result
	return result, nil
}

func seedPendingWorkItemSuggestion(t *testing.T, repo *fakeAIRepo, companyID, projectID string, spaceID *string) AISuggestion {
	t.Helper()
	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID, Type: BatchTypeWorkItemSuggestions,
		Status: BatchStatusProcessing, OperationID: "seed-work-op-" + projectID,
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	scopeLevel := ScopeLevelProject
	if spaceID != nil {
		scopeLevel = ScopeLevelSpace
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{{
		CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
		Type: SuggestionTypeWorkItem, Status: SuggestionStatusPending,
		SuggestedData: SuggestedData{WorkItem: &WorkItemSuggestionData{
			Description: "Install ceramic floor tiles", WorkType: "tile_installation",
			ScopeLevel: scopeLevel, SpaceID: spaceID, ScopeOrigin: ScopeOriginExplicit,
		}},
		InputFingerprint: "fp",
	}}); err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}
	if err := repo.MarkBatchCompleted(context.Background(), companyID, batch.ID); err != nil {
		t.Fatalf("seed complete: %v", err)
	}
	list, err := repo.ListSuggestionsByBatch(context.Background(), companyID, batch.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("seed list: %v / %d", err, len(list))
	}
	return list[0]
}

func newWorkItemAcceptanceService(repo *fakeAIRepo, creator *fakeWorkItemCreator) *Service {
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	svc.SetWorkItemCreator(creator)
	return svc
}

func TestUseWorkItemDeltaSuggestionMutatesExistingWorkItemInPlace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	creator.byID["wi_existing"] = WorkItemAcceptanceResult{ID: "wi_existing", Description: "Install vinyl flooring"}
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	result, err := svc.UseWorkItemDeltaSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "wi_existing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != "wi_existing" {
		t.Fatalf("expected mutation of the existing Work Item, got a new id %q (must never create a duplicate)", result.ID)
	}
	if result.Description != "Install ceramic floor tiles" {
		t.Fatalf("expected the Work Item updated to the suggestion's data, got %+v", result)
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusModified {
		t.Fatalf("expected modified, got %s", updated.Status)
	}
	if updated.AcceptedDomainObjectID != "wi_existing" {
		t.Fatalf("expected AcceptedDomainObjectID wi_existing, got %s", updated.AcceptedDomainObjectID)
	}
}

func TestAcceptWorkItemSuggestionRequiresQuantityAndUnit(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		QuantityValue: "", QuantityUnit: "m2",
	})
	if err != ErrWorkItemAcceptanceQuantityRequired {
		t.Fatalf("expected ErrWorkItemAcceptanceQuantityRequired for missing value, got %v", err)
	}

	_, err = svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		QuantityValue: "30", QuantityUnit: "",
	})
	if err != ErrWorkItemAcceptanceQuantityRequired {
		t.Fatalf("expected ErrWorkItemAcceptanceQuantityRequired for missing unit, got %v", err)
	}
}

func TestAcceptWorkItemSuggestionProjectLevelSucceeds(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	result, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created WorkItem ID")
	}
}

func TestAcceptWorkItemSuggestionSpaceScopedValidatesSpaceID(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	spaceID := "space_1"
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", &spaceID)

	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		ScopeLevel: ScopeLevelSpace, SpaceID: &spaceID,
		QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != ErrWorkItemAcceptanceSpaceNotFound {
		t.Fatalf("expected ErrWorkItemAcceptanceSpaceNotFound for unregistered space, got %v", err)
	}

	creator.validSpaceIDs["space_1"] = true
	result, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		ScopeLevel: ScopeLevelSpace, SpaceID: &spaceID,
		QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != nil {
		t.Fatalf("unexpected error once space is valid: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created WorkItem ID")
	}
}

func TestAcceptWorkItemSuggestionProjectScopedRejectsNonNullSpace(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	spaceID := "space_1"
	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "x", WorkType: "y", ScopeLevel: ScopeLevelProject, SpaceID: &spaceID,
		QuantityValue: "1", QuantityUnit: "unit",
	})
	if err != ErrWorkItemAcceptanceInvalidScope {
		t.Fatalf("expected ErrWorkItemAcceptanceInvalidScope, got %v", err)
	}
}

func TestAcceptWorkItemSuggestionDuplicateWithoutAllowFlagReturnsConflict(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	creator.duplicateKeys[normalize("Install ceramic floor tiles")] = true
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != ErrWorkItemAcceptanceDuplicate {
		t.Fatalf("expected ErrWorkItemAcceptanceDuplicate, got %v", err)
	}
}

func TestAcceptWorkItemSuggestionAllowDuplicateBypassesCheck(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	creator.duplicateKeys[normalize("Install ceramic floor tiles")] = true
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	result, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		QuantityValue: "30", QuantityUnit: "m2", AllowDuplicate: true,
	})
	if err != nil {
		t.Fatalf("unexpected error with allowDuplicate=true: %v", err)
	}
	if result.ID == "" {
		t.Fatal("expected created WorkItem")
	}
}

func TestAcceptWorkItemSuggestionRetryReturnsSameWorkItem(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	input := WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		ScopeLevel: ScopeLevelProject, QuantityValue: "30", QuantityUnit: "m2",
	}
	first, err := creator.CreateWorkItemFromAISuggestion(context.Background(), "company_a", "project_1", nil,
		input.Description, input.WorkType, input.QuantityValue, input.QuantityUnit, sug.ID)
	if err != nil {
		t.Fatalf("unexpected error pre-creating: %v", err)
	}

	result, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, input)
	if err != nil {
		t.Fatalf("unexpected error on repair retry: %v", err)
	}
	if result.ID != first.ID {
		t.Fatalf("expected repair to return same WorkItem %s, got %s", first.ID, result.ID)
	}
	if len(creator.byID) != 1 {
		t.Fatalf("expected exactly one WorkItem to exist after repair, got %d", len(creator.byID))
	}

	updated, _ := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected repaired suggestion accepted, got %s", updated.Status)
	}
}

func TestAcceptWorkItemSuggestionEditedFieldsMarksModified(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install porcelain floor tiles (edited)", WorkType: "tile_installation",
		QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated, _ := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if updated.Status != SuggestionStatusModified {
		t.Fatalf("expected modified for an edited description, got %s", updated.Status)
	}
}

// --- Resource acceptance ---

type fakeResourceCreator struct {
	byID                       map[string]ResourceAcceptanceResult
	bySourceSuggestion         map[string]ResourceAcceptanceResult
	validWorkItemIDs           map[string]bool
	validMaterialIDs           map[string]bool
	duplicateKeys              map[string]bool
	materialByID               map[string]MaterialAcceptanceResult
	materialBySourceSuggestion map[string]MaterialAcceptanceResult
	nextID                     int
	createResourceErr          error
	createMaterialErr          error
}

func newFakeResourceCreator() *fakeResourceCreator {
	return &fakeResourceCreator{
		byID: map[string]ResourceAcceptanceResult{}, bySourceSuggestion: map[string]ResourceAcceptanceResult{},
		validWorkItemIDs: map[string]bool{}, validMaterialIDs: map[string]bool{}, duplicateKeys: map[string]bool{},
		materialByID: map[string]MaterialAcceptanceResult{}, materialBySourceSuggestion: map[string]MaterialAcceptanceResult{},
	}
}

func (f *fakeResourceCreator) CreateResourceRequirementFromAISuggestion(_ context.Context, companyID, projectID, workItemID string, resourceType ResourceRequirementType, materialID *string, name, sourceSuggestionID string) (ResourceAcceptanceResult, error) {
	if f.createResourceErr != nil {
		return ResourceAcceptanceResult{}, f.createResourceErr
	}
	if existing, ok := f.bySourceSuggestion[sourceSuggestionID]; ok {
		return existing, nil
	}
	if !f.validWorkItemIDs[workItemID] {
		return ResourceAcceptanceResult{}, ErrResourceAcceptanceWorkItemNotFound
	}
	if resourceType == ResourceRequirementTypeMaterial {
		if materialID == nil || !f.validMaterialIDs[*materialID] {
			return ResourceAcceptanceResult{}, ErrResourceAcceptanceMaterialNotFound
		}
	}
	f.nextID++
	result := ResourceAcceptanceResult{RequirementID: "wrr_" + string(rune('a'+f.nextID)), Name: name}
	f.byID[result.RequirementID] = result
	f.bySourceSuggestion[sourceSuggestionID] = result
	return result, nil
}

func (f *fakeResourceCreator) FindResourceRequirementBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (ResourceAcceptanceResult, error) {
	result, ok := f.bySourceSuggestion[sourceSuggestionID]
	if !ok {
		return ResourceAcceptanceResult{}, ErrResourceAcceptanceNotFound
	}
	return result, nil
}

func (f *fakeResourceCreator) ResourceRequirementLikelyDuplicate(_ context.Context, companyID, projectID, workItemID string, resourceType ResourceRequirementType, materialID *string, name string) (bool, error) {
	key := workItemID + "|" + string(resourceType) + "|"
	if materialID != nil {
		key += *materialID
	} else {
		key += normalize(name)
	}
	return f.duplicateKeys[key], nil
}

func (f *fakeResourceCreator) MaterialBelongsToCompany(_ context.Context, companyID, materialID string) (bool, error) {
	return f.validMaterialIDs[materialID], nil
}

func (f *fakeResourceCreator) CreateMaterialFromAISuggestion(_ context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (MaterialAcceptanceResult, error) {
	if f.createMaterialErr != nil {
		return MaterialAcceptanceResult{}, f.createMaterialErr
	}
	if existing, ok := f.materialBySourceSuggestion[sourceSuggestionID]; ok {
		return existing, nil
	}
	f.nextID++
	result := MaterialAcceptanceResult{ID: "material_" + string(rune('a'+f.nextID)), Name: name}
	f.materialByID[result.ID] = result
	f.materialBySourceSuggestion[sourceSuggestionID] = result
	f.validMaterialIDs[result.ID] = true
	return result, nil
}

func (f *fakeResourceCreator) FindMaterialBySourceSuggestionID(_ context.Context, companyID, sourceSuggestionID string) (MaterialAcceptanceResult, error) {
	result, ok := f.materialBySourceSuggestion[sourceSuggestionID]
	if !ok {
		return MaterialAcceptanceResult{}, ErrResourceAcceptanceMaterialNotFound
	}
	return result, nil
}

func seedPendingResourceSuggestion(t *testing.T, repo *fakeAIRepo, companyID, projectID, workItemID string, resourceType SuggestionType, candidateMaterialID *string) AISuggestion {
	t.Helper()
	batch, err := repo.CreateProcessingBatch(context.Background(), AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID, Type: BatchTypeResourceSuggestions,
		Status: BatchStatusProcessing, OperationID: "seed-resource-op-" + workItemID + string(resourceType),
	})
	if err != nil {
		t.Fatalf("seed batch: %v", err)
	}
	data := &ResourceSuggestionData{WorkItemID: workItemID, Name: "Tile Adhesive", CandidateMaterialID: candidateMaterialID}
	suggested := SuggestedData{}
	switch resourceType {
	case SuggestionTypeMaterialResource:
		suggested.MaterialResource = data
	case SuggestionTypeTradeResource:
		data.Name = "Tiler"
		suggested.TradeResource = data
	case SuggestionTypeEquipmentResource:
		data.Name = "Tile Cutter"
		suggested.EquipmentResource = data
	}
	if _, err := repo.InsertSuggestionsForBatch(context.Background(), batch.ID, []AISuggestion{{
		CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
		Type: resourceType, Status: SuggestionStatusPending,
		SuggestedData: suggested, InputFingerprint: "fp",
	}}); err != nil {
		t.Fatalf("seed suggestion: %v", err)
	}
	if err := repo.MarkBatchCompleted(context.Background(), companyID, batch.ID); err != nil {
		t.Fatalf("seed complete: %v", err)
	}
	list, err := repo.ListSuggestionsByBatch(context.Background(), companyID, batch.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("seed list: %v / %d", err, len(list))
	}
	return list[0]
}

func newResourceAcceptanceService(repo *fakeAIRepo, creator *fakeResourceCreator) *Service {
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)
	svc.SetResourceCreator(creator)
	return svc
}

func TestAcceptResourceSuggestionExistingMaterialCreatesRequirement(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	creator.validMaterialIDs["material_existing"] = true
	svc := newResourceAcceptanceService(repo, creator)
	materialID := "material_existing"
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeMaterialResource, &materialID)

	result, err := svc.AcceptMaterialResourceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, MaterialResourceAcceptanceInput{
		Mode: MaterialAcceptanceModeExisting, MaterialID: &materialID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequirementID == "" {
		t.Fatal("expected created WorkResourceRequirement")
	}
	if len(creator.materialByID) != 0 {
		t.Fatalf("expected no new Material created for existing-material mode, got %d", len(creator.materialByID))
	}

	updated, _ := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected accepted, got %s", updated.Status)
	}
}

func TestAcceptResourceSuggestionWrongCompanyMaterialReturns404(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeMaterialResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	foreignMaterial := "material_foreign"
	_, err := svc.AcceptMaterialResourceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, MaterialResourceAcceptanceInput{
		Mode: MaterialAcceptanceModeExisting, MaterialID: &foreignMaterial,
	})
	if err != ErrResourceAcceptanceMaterialNotFound {
		t.Fatalf("expected ErrResourceAcceptanceMaterialNotFound, got %v", err)
	}
}

func TestAcceptResourceSuggestionCreateAndAddNewMaterial(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeMaterialResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	result, err := svc.AcceptMaterialResourceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, MaterialResourceAcceptanceInput{
		Mode: MaterialAcceptanceModeCreate,
		NewMaterial: &NewMaterialInput{
			Name: "Tile Spacers", Category: "tile", Unit: "bag",
			ReferencePriceAmount: 500, ReferencePriceCurrency: "USD",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.MaterialID == "" {
		t.Fatal("expected created Material")
	}
	if result.RequirementID == "" {
		t.Fatal("expected created WorkResourceRequirement")
	}
	if len(creator.materialByID) != 1 {
		t.Fatalf("expected exactly one Material created, got %d", len(creator.materialByID))
	}
}

func TestAcceptResourceSuggestionCreateAndAddRepairsAfterPartialFailure(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeMaterialResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	// Simulate: Material was created (source suggestion recorded) but the
	// WorkResourceRequirement create failed before the suggestion could be
	// marked terminal.
	preCreatedMaterial, err := creator.CreateMaterialFromAISuggestion(context.Background(), "company_a",
		"Tile Spacers", "tile", "", "bag", 500, "USD", sug.ID)
	if err != nil {
		t.Fatalf("unexpected error pre-creating material: %v", err)
	}

	result, err := svc.AcceptMaterialResourceSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, MaterialResourceAcceptanceInput{
		Mode: MaterialAcceptanceModeCreate,
		NewMaterial: &NewMaterialInput{
			Name: "Tile Spacers", Category: "tile", Unit: "bag",
			ReferencePriceAmount: 500, ReferencePriceCurrency: "USD",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error on repair retry: %v", err)
	}
	if result.MaterialID != preCreatedMaterial.ID {
		t.Fatalf("expected repair to reuse pre-created Material %s, got %s", preCreatedMaterial.ID, result.MaterialID)
	}
	if len(creator.materialByID) != 1 {
		t.Fatalf("expected exactly one Material to exist after repair, got %d", len(creator.materialByID))
	}
	// The Create & Add saga's second write is the WorkResourceRequirement
	// linked to the Material — the repair path must also produce (and not
	// duplicate) that, not just the Material half of the pair.
	if result.RequirementID == "" {
		t.Fatal("expected repair to also produce a WorkResourceRequirement id, got empty")
	}
	if len(creator.byID) != 1 {
		t.Fatalf("expected exactly one WorkResourceRequirement to exist after repair, got %d", len(creator.byID))
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected repaired suggestion to be accepted, got %s", updated.Status)
	}
}

// TestAcceptTradeSuggestionRetryAfterAmbiguousFailureRepairsAndReturnsSameRequirement
// covers the third repair scenario from design doc plan Task 18 Step 4
// (Space, Work, Material+WRR are explicitly named, but Trade/Equipment
// share the exact same repairSuggestionAcceptance code path in
// AcceptTradeOrEquipmentSuggestion — this proves that path too, not just
// the Material one).
func TestAcceptTradeSuggestionRetryAfterAmbiguousFailureRepairsAndReturnsSameRequirement(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeTradeResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	// Simulate: the WorkResourceRequirement was created, but marking the
	// suggestion terminal failed before the process could complete —
	// the suggestion is still pending in the repo, but a requirement with
	// this sourceSuggestionId already exists.
	preCreated, err := creator.CreateResourceRequirementFromAISuggestion(context.Background(), "company_a", "project_1", "work_1", ResourceRequirementTypeTrade, nil, "Tiler", sug.ID)
	if err != nil {
		t.Fatalf("unexpected error pre-creating requirement: %v", err)
	}

	result, err := svc.AcceptTradeOrEquipmentSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "")
	if err != nil {
		t.Fatalf("unexpected error on repair retry: %v", err)
	}
	if result.RequirementID != preCreated.RequirementID {
		t.Fatalf("expected repair to return the pre-created requirement %s, got %s", preCreated.RequirementID, result.RequirementID)
	}
	if len(creator.byID) != 1 {
		t.Fatalf("expected exactly one WorkResourceRequirement to exist after repair, got %d", len(creator.byID))
	}

	updated, findErr := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected repaired suggestion to be accepted, got %s", updated.Status)
	}
}

func TestAcceptTradeSuggestionCreatesRequirementOnly(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeTradeResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	result, err := svc.AcceptTradeOrEquipmentSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequirementID == "" {
		t.Fatal("expected created WorkResourceRequirement")
	}
}

func TestAcceptEquipmentSuggestionCreatesRequirementOnly(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeEquipmentResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	result, err := svc.AcceptTradeOrEquipmentSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequirementID == "" {
		t.Fatal("expected created WorkResourceRequirement")
	}
}

func TestAcceptResourceSuggestionDuplicateReturnsConflict(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeResourceCreator()
	creator.validWorkItemIDs["work_1"] = true
	creator.duplicateKeys["work_1|trade|tiler"] = true
	sug := seedPendingResourceSuggestion(t, repo, "company_a", "project_1", "work_1", SuggestionTypeTradeResource, nil)
	svc := newResourceAcceptanceService(repo, creator)

	_, err := svc.AcceptTradeOrEquipmentSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, "")
	if err != ErrResourceAcceptanceDuplicate {
		t.Fatalf("expected ErrResourceAcceptanceDuplicate, got %v", err)
	}
}

func TestAcceptWorkItemSuggestionQuantityOnlyDoesNotMarkModified(t *testing.T) {
	repo := newFakeAIRepo()
	creator := newFakeWorkItemCreator()
	svc := newWorkItemAcceptanceService(repo, creator)
	sug := seedPendingWorkItemSuggestion(t, repo, "company_a", "project_1", nil)

	_, err := svc.AcceptWorkItemSuggestion(context.Background(), "company_a", sug.ID, sug.Revision, WorkItemAcceptanceInput{
		Description: "Install ceramic floor tiles", WorkType: "tile_installation",
		ScopeLevel: ScopeLevelProject, QuantityValue: "30", QuantityUnit: "m2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	updated, _ := repo.FindSuggestionByID(context.Background(), "company_a", sug.ID)
	if updated.Status != SuggestionStatusAccepted {
		t.Fatalf("expected accepted (contractor-only quantity/unit must not itself trigger modified), got %s", updated.Status)
	}
}
