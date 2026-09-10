package ai

import (
	"context"
	"errors"
	"testing"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
)

// --- fakeRepo: in-memory Repository for orchestration tests ---

type fakeAIRepo struct {
	batchesByOp        map[string]AIGenerationBatch // companyID+"|"+operationID -> batch
	batchesByID        map[string]AIGenerationBatch
	suggestionsByBatch map[string][]AISuggestion
	suggestionsByID    map[string]AISuggestion
	nextBatchID        int
	nextSugID          int
}

func newFakeAIRepo() *fakeAIRepo {
	return &fakeAIRepo{
		batchesByOp:        map[string]AIGenerationBatch{},
		batchesByID:        map[string]AIGenerationBatch{},
		suggestionsByBatch: map[string][]AISuggestion{},
		suggestionsByID:    map[string]AISuggestion{},
	}
}

func opKey(companyID, operationID string) string { return companyID + "|" + operationID }

func (f *fakeAIRepo) CreateProcessingBatch(_ context.Context, batch AIGenerationBatch) (AIGenerationBatch, error) {
	key := opKey(batch.CompanyID, batch.OperationID)
	if _, exists := f.batchesByOp[key]; exists {
		return AIGenerationBatch{}, errors.New("fakeAIRepo: duplicate operationId")
	}
	f.nextBatchID++
	batch.ID = string(rune('A' + f.nextBatchID))
	f.batchesByOp[key] = batch
	f.batchesByID[batch.ID] = batch
	return batch, nil
}

func (f *fakeAIRepo) FindBatchByOperationID(_ context.Context, companyID, operationID string) (AIGenerationBatch, error) {
	b, ok := f.batchesByOp[opKey(companyID, operationID)]
	if !ok {
		return AIGenerationBatch{}, ErrBatchNotFound
	}
	return b, nil
}

func (f *fakeAIRepo) FindBatchByID(_ context.Context, companyID, batchID string) (AIGenerationBatch, error) {
	b, ok := f.batchesByID[batchID]
	if !ok || b.CompanyID != companyID {
		return AIGenerationBatch{}, ErrBatchNotFound
	}
	return b, nil
}

func (f *fakeAIRepo) ListBatchesByProject(_ context.Context, companyID, projectID string, batchType BatchType) ([]AIGenerationBatch, error) {
	var result []AIGenerationBatch
	for _, b := range f.batchesByID {
		if b.CompanyID == companyID && b.ProjectID == projectID && b.Type == batchType {
			result = append(result, b)
		}
	}
	return result, nil
}

func (f *fakeAIRepo) InsertSuggestionsForBatch(_ context.Context, batchID string, suggestions []AISuggestion) ([]AISuggestion, error) {
	inserted := make([]AISuggestion, len(suggestions))
	for i, s := range suggestions {
		f.nextSugID++
		s.ID = "sug_" + string(rune('a'+f.nextSugID))
		s.Revision = 1
		f.suggestionsByID[s.ID] = s
		inserted[i] = s
	}
	f.suggestionsByBatch[batchID] = append(f.suggestionsByBatch[batchID], inserted...)
	return inserted, nil
}

func (f *fakeAIRepo) ListSuggestionsByBatch(_ context.Context, companyID, batchID string) ([]AISuggestion, error) {
	var result []AISuggestion
	for _, seed := range f.suggestionsByBatch[batchID] {
		// suggestionsByID is the single source of truth after any
		// conditionalUpdate; suggestionsByBatch only tracks membership/order.
		current, ok := f.suggestionsByID[seed.ID]
		if !ok || current.CompanyID != companyID {
			continue
		}
		result = append(result, current)
	}
	return result, nil
}

func (f *fakeAIRepo) FindSuggestionByID(_ context.Context, companyID, suggestionID string) (AISuggestion, error) {
	s, ok := f.suggestionsByID[suggestionID]
	if !ok || s.CompanyID != companyID {
		return AISuggestion{}, ErrSuggestionNotFound
	}
	return s, nil
}

func (f *fakeAIRepo) ConditionalAccept(_ context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error {
	return f.conditionalUpdate(companyID, suggestionID, expectedRevision, SuggestionStatusAccepted, acceptedDomainObjectID)
}

func (f *fakeAIRepo) ConditionalModify(_ context.Context, companyID, suggestionID string, expectedRevision int64, acceptedDomainObjectID string) error {
	return f.conditionalUpdate(companyID, suggestionID, expectedRevision, SuggestionStatusModified, acceptedDomainObjectID)
}

func (f *fakeAIRepo) ConditionalReject(_ context.Context, companyID, suggestionID string, expectedRevision int64) error {
	return f.conditionalUpdate(companyID, suggestionID, expectedRevision, SuggestionStatusRejected, "")
}

func (f *fakeAIRepo) conditionalUpdate(companyID, suggestionID string, expectedRevision int64, status SuggestionStatus, domainObjID string) error {
	s, ok := f.suggestionsByID[suggestionID]
	if !ok || s.CompanyID != companyID || s.Status != SuggestionStatusPending || s.Revision != expectedRevision {
		return ErrSuggestionRevisionMismatch
	}
	s.Status = status
	s.Revision++
	if domainObjID != "" {
		s.AcceptedDomainObjectID = domainObjID
	}
	f.suggestionsByID[suggestionID] = s
	return nil
}

func (f *fakeAIRepo) MarkBatchCompleted(_ context.Context, companyID, batchID string) error {
	b, ok := f.batchesByID[batchID]
	if !ok || b.CompanyID != companyID {
		return ErrBatchNotFound
	}
	b.Status = BatchStatusCompleted
	f.batchesByID[batchID] = b
	f.batchesByOp[opKey(b.CompanyID, b.OperationID)] = b
	return nil
}

func (f *fakeAIRepo) MarkBatchFailed(_ context.Context, companyID, batchID, errorCode string) error {
	b, ok := f.batchesByID[batchID]
	if !ok || b.CompanyID != companyID {
		return ErrBatchNotFound
	}
	b.Status = BatchStatusFailed
	b.ErrorCode = errorCode
	f.batchesByID[batchID] = b
	f.batchesByOp[opKey(b.CompanyID, b.OperationID)] = b
	return nil
}

func (f *fakeAIRepo) DeleteSuggestionsByBatch(_ context.Context, batchID string) error {
	for _, s := range f.suggestionsByBatch[batchID] {
		delete(f.suggestionsByID, s.ID)
	}
	delete(f.suggestionsByBatch, batchID)
	return nil
}

// --- fakeDomainGateway: minimal DomainGateway for orchestration tests ---

type fakeDomainGateway struct {
	projectScopeBrief    map[string]string // projectID -> brief
	projectExists        map[string]bool   // projectID -> exists for caller company
	existingSpaces       map[string][]DomainGatewaySpace
	existingWorkItems    map[string][]DomainGatewayWorkItem
	materialCandidates   []DomainGatewayMaterial
	existingRequirements map[string][]DomainGatewayRequirement
}

func (f *fakeDomainGateway) GetProjectAIContext(_ context.Context, companyID, projectID string) (DomainGatewayProject, error) {
	if !f.projectExists[projectID] {
		return DomainGatewayProject{}, ErrGatewayProjectNotFound
	}
	return DomainGatewayProject{ID: projectID, ScopeBrief: f.projectScopeBrief[projectID]}, nil
}

func (f *fakeDomainGateway) ListSpacesForAI(_ context.Context, companyID, projectID string) ([]DomainGatewaySpace, error) {
	return f.existingSpaces[projectID], nil
}

func (f *fakeDomainGateway) ListWorkItemsForAI(_ context.Context, companyID, projectID string) ([]DomainGatewayWorkItem, error) {
	return f.existingWorkItems[projectID], nil
}

func (f *fakeDomainGateway) ListMaterialCandidatesForAI(_ context.Context, companyID string) ([]DomainGatewayMaterial, error) {
	return f.materialCandidates, nil
}

func (f *fakeDomainGateway) ListResourceRequirementsForAI(_ context.Context, companyID, projectID string) ([]DomainGatewayRequirement, error) {
	return f.existingRequirements[projectID], nil
}

// --- fakeAIClient: scripted platform/ai.Client responses ---

type fakeAIClient struct {
	spaceResp platformai.SpaceSuggestionResponse
	spaceErr  error
	// spaceRepairResp, when set, is returned instead of spaceResp for any
	// call whose RepairInstruction is non-empty — lets tests script the
	// bounded repair pass's response distinctly from the initial call.
	spaceRepairResp    *platformai.SpaceSuggestionResponse
	repairCallCount    int
	workResp           platformai.WorkItemSuggestionResponse
	workErr            error
	resourceResp       platformai.ResourceSuggestionResponse
	resourceErr        error
}

func (f *fakeAIClient) SuggestSpaces(_ context.Context, req platformai.SpaceSuggestionRequest) (platformai.SpaceSuggestionResponse, error) {
	if req.RepairInstruction != "" {
		f.repairCallCount++
		if f.spaceRepairResp != nil {
			return *f.spaceRepairResp, nil
		}
	}
	return f.spaceResp, f.spaceErr
}

func (f *fakeAIClient) SuggestWorkItems(_ context.Context, _ platformai.WorkItemSuggestionRequest) (platformai.WorkItemSuggestionResponse, error) {
	return f.workResp, f.workErr
}

func (f *fakeAIClient) SuggestResources(_ context.Context, _ platformai.ResourceSuggestionRequest) (platformai.ResourceSuggestionResponse, error) {
	return f.resourceResp, f.resourceErr
}

func newTestSpaceGatewayAndClient() (*fakeDomainGateway, *fakeAIClient) {
	gw := &fakeDomainGateway{
		projectExists:     map[string]bool{"project_1": true},
		projectScopeBrief: map[string]string{"project_1": "Full renovation of a condo."},
		existingSpaces:    map[string][]DomainGatewaySpace{},
	}
	client := &fakeAIClient{
		spaceResp: platformai.SpaceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "spaces-v1", SchemaVersion: 1,
			Suggestions: []platformai.SpaceSuggestion{
				{
					Name: "Kitchen", SpaceType: "kitchen", Rationale: "explicit",
					EvidenceType: "explicit", SourceExcerpt: "renovation of a condo",
				},
			},
		},
	}
	return gw, client
}

func TestSuggestSpacesHappyPathPersistsAndCompletes(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Batch.Status != BatchStatusCompleted {
		t.Fatalf("expected completed batch, got %s", result.Batch.Status)
	}
	if len(result.Suggestions) != 1 || result.Suggestions[0].SuggestedData.Space.Name != "Kitchen" {
		t.Fatalf("unexpected suggestions: %+v", result.Suggestions)
	}
}

func TestSuggestSpacesPersistsSourceBriefOnBatch(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Batch.SourceBrief != "Full renovation of a condo." {
		t.Fatalf("SourceBrief = %q, want the trimmed project brief", result.Batch.SourceBrief)
	}
}

func TestSuggestSpacesCoverageGapTriggersOneBoundedRepairCall(t *testing.T) {
	repo := newFakeAIRepo()
	gw := &fakeDomainGateway{
		projectExists: map[string]bool{"project_1": true},
		projectScopeBrief: map[string]string{
			"project_1": "Redo the kitchen and the master bedroom.",
		},
		existingSpaces: map[string][]DomainGatewaySpace{},
	}
	client := &fakeAIClient{
		spaceResp: platformai.SpaceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "spaces-v1", SchemaVersion: 1,
			Suggestions: []platformai.SpaceSuggestion{
				// Only Kitchen generated — "master bedroom" is a genuine
				// coverage gap the quality gate should detect and repair.
				{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: "explicit", SourceExcerpt: "redo the kitchen"},
			},
		},
		spaceRepairResp: &platformai.SpaceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "spaces-v1", SchemaVersion: 1,
			Suggestions: []platformai.SpaceSuggestion{
				{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: "explicit", SourceExcerpt: "redo the kitchen"},
				{Name: "Master Bedroom", SpaceType: "bedroom", EvidenceType: "explicit", SourceExcerpt: "master bedroom"},
			},
		},
	}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.repairCallCount != 1 {
		t.Fatalf("expected exactly 1 bounded repair call, got %d", client.repairCallCount)
	}
	names := map[string]bool{}
	for _, s := range result.Suggestions {
		names[s.SuggestedData.Space.Name] = true
	}
	if !names["Kitchen"] || !names["Master Bedroom"] {
		t.Fatalf("expected both Kitchen and Master Bedroom after repair, got %+v", names)
	}
}

func TestSuggestSpacesCoverageGapWithFailedRepairKeepsPreRepairResult(t *testing.T) {
	repo := newFakeAIRepo()
	gw := &fakeDomainGateway{
		projectExists:     map[string]bool{"project_1": true},
		projectScopeBrief: map[string]string{"project_1": "Redo the kitchen and the master bedroom."},
		existingSpaces:    map[string][]DomainGatewaySpace{},
	}
	client := &fakeAIClient{
		spaceResp: platformai.SpaceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "spaces-v1", SchemaVersion: 1,
			Suggestions: []platformai.SpaceSuggestion{
				{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: "explicit", SourceExcerpt: "redo the kitchen"},
			},
		},
		// No spaceRepairResp set -> repair call returns the zero-value
		// response (empty suggestions), simulating a repair pass that
		// found nothing to fix.
	}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 1 || result.Suggestions[0].SuggestedData.Space.Name != "Kitchen" {
		t.Fatalf("expected pre-repair Kitchen-only result retained, got %+v", result.Suggestions)
	}
}

func TestSuggestSpacesRejectsEmptyScopeBrief(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	gw.projectScopeBrief["project_1"] = "   "
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != ErrScopeBriefEmpty {
		t.Fatalf("expected ErrScopeBriefEmpty, got %v", err)
	}
}

func TestSuggestSpacesIdempotentSameOperationSameFingerprint(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	first, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	second, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if second.Batch.ID != first.Batch.ID {
		t.Fatalf("expected retry to return same batch, got first=%s second=%s", first.Batch.ID, second.Batch.ID)
	}
}

// TestSuggestSpacesAmbiguousRetryProducesExactlyOneBatchAndSuggestionSet
// simulates the "client lost the response but the server actually
// completed the write" ambiguous-retry scenario (design doc plan Task 18
// Step 3): the first call succeeds and persists a completed batch plus its
// suggestions; a retry with the identical operationId/fingerprint (as a
// real client would send after a timeout/dropped connection) must return
// the SAME batch and must not create a second batch or duplicate/re-insert
// suggestions.
func TestSuggestSpacesAmbiguousRetryProducesExactlyOneBatchAndSuggestionSet(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	first, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	retry, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error on ambiguous retry: %v", err)
	}
	if retry.Batch.ID != first.Batch.ID {
		t.Fatalf("expected retry to return the same batch, got first=%s retry=%s", first.Batch.ID, retry.Batch.ID)
	}
	if len(retry.Suggestions) != len(first.Suggestions) {
		t.Fatalf("expected retry to return the same suggestion count, got first=%d retry=%d", len(first.Suggestions), len(retry.Suggestions))
	}

	batchCount := 0
	for _, b := range repo.batchesByID {
		if b.ProjectID == "project_1" && b.Type == BatchTypeSpaceSuggestions {
			batchCount++
		}
	}
	if batchCount != 1 {
		t.Fatalf("expected exactly one persisted batch after ambiguous retry, got %d", batchCount)
	}

	stored, err := repo.ListSuggestionsByBatch(context.Background(), "company_a", first.Batch.ID)
	if err != nil {
		t.Fatalf("unexpected error listing suggestions: %v", err)
	}
	if len(stored) != len(first.Suggestions) {
		t.Fatalf("expected exactly one suggestion set (%d) persisted, got %d stored", len(first.Suggestions), len(stored))
	}
}

func TestSuggestSpacesIdempotencyConflictOnChangedFingerprint(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	svc := NewService(repo, gw, client)

	if _, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1"); err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	gw.projectScopeBrief["project_1"] = "A completely different brief."
	_, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != ErrGenerationIdempotencyConflict {
		t.Fatalf("expected ErrGenerationIdempotencyConflict, got %v", err)
	}
}

func TestSuggestSpacesFiltersExactDuplicates(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	gw.existingSpaces["project_1"] = []DomainGatewaySpace{{ID: "space_existing", Name: "Kitchen", Type: "kitchen"}}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 0 {
		t.Fatalf("expected exact-duplicate Kitchen suggestion filtered out, got %+v", result.Suggestions)
	}
}

func TestSuggestSpacesProviderFailureMarksBatchFailedWithNoSuggestions(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	client.spaceErr = platformai.ErrProviderUnavailable
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err == nil {
		t.Fatal("expected an error when the provider is unavailable")
	}

	batch, findErr := repo.FindBatchByOperationID(context.Background(), "company_a", "op_1")
	if findErr != nil {
		t.Fatalf("unexpected error finding batch: %v", findErr)
	}
	if batch.Status != BatchStatusFailed {
		t.Fatalf("expected failed batch status, got %s", batch.Status)
	}
	suggestions, _ := repo.ListSuggestionsByBatch(context.Background(), "company_a", batch.ID)
	if len(suggestions) != 0 {
		t.Fatalf("expected zero reviewable suggestions after provider failure, got %d", len(suggestions))
	}
}

func TestSuggestSpacesRejectsForeignProject(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	delete(gw.projectExists, "project_1")
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

// --- Work Item generation ---

func newTestWorkGatewayAndClient() (*fakeDomainGateway, *fakeAIClient) {
	gw := &fakeDomainGateway{
		projectExists:     map[string]bool{"project_1": true},
		projectScopeBrief: map[string]string{"project_1": "Full renovation of a condo. Replace flooring in the kitchen."},
		existingSpaces: map[string][]DomainGatewaySpace{
			"project_1": {{ID: "space_1", Name: "Kitchen", Type: "kitchen"}},
		},
		existingWorkItems: map[string][]DomainGatewayWorkItem{},
	}
	client := &fakeAIClient{
		workResp: platformai.WorkItemSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "work-items-v1", SchemaVersion: 1,
			Suggestions: []platformai.WorkItemSuggestion{
				{
					Description: "Replace flooring", WorkType: "flooring", ScopeLevel: "space", SpaceID: strPtr("space_1"),
					ScopeOrigin: "explicit_scope", SourceExcerpt: "replace flooring in the kitchen",
					MaterialSpecificity: "unspecified",
				},
				{
					Description: "Site protection", WorkType: "site_protection", ScopeLevel: "project", SpaceID: nil,
					ScopeOrigin: "supporting_scope", MaterialSpecificity: "unspecified",
				},
			},
		},
	}
	return gw, client
}

func strPtr(s string) *string { return &s }

func TestSuggestWorkItemsHappyPath(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestWorkGatewayAndClient()
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestWorkItems(context.Background(), "company_a", "project_1", "user_1", "op_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(result.Suggestions))
	}
}

func TestSuggestWorkItemsRequiresAtLeastOneConfirmedSpace(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestWorkGatewayAndClient()
	gw.existingSpaces["project_1"] = nil
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestWorkItems(context.Background(), "company_a", "project_1", "user_1", "op_2")
	if err != ErrNoConfirmedSpaces {
		t.Fatalf("expected ErrNoConfirmedSpaces, got %v", err)
	}
}

func TestSuggestWorkItemsValidatesSpaceScopedIDIsSupplied(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestWorkGatewayAndClient()
	client.workResp.Suggestions = []platformai.WorkItemSuggestion{
		{Description: "Bogus", WorkType: "x", ScopeLevel: "space", SpaceID: strPtr("space_not_supplied"), ScopeOrigin: "explicit_scope"},
	}
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestWorkItems(context.Background(), "company_a", "project_1", "user_1", "op_2")
	if err == nil {
		t.Fatal("expected an error for a space-scoped suggestion referencing an unsupplied spaceId")
	}

	batch, findErr := repo.FindBatchByOperationID(context.Background(), "company_a", "op_2")
	if findErr != nil {
		t.Fatalf("unexpected error: %v", findErr)
	}
	if batch.Status != BatchStatusFailed {
		t.Fatalf("expected failed batch, got %s", batch.Status)
	}
}

func TestSuggestWorkItemsProjectScopedHasNilSpaceID(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestWorkGatewayAndClient()
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestWorkItems(context.Background(), "company_a", "project_1", "user_1", "op_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var foundProjectLevel bool
	for _, s := range result.Suggestions {
		if s.SuggestedData.WorkItem.ScopeLevel == ScopeLevelProject {
			foundProjectLevel = true
			if s.SuggestedData.WorkItem.SpaceID != nil {
				t.Fatalf("expected nil SpaceID for project-level suggestion, got %v", s.SuggestedData.WorkItem.SpaceID)
			}
		}
	}
	if !foundProjectLevel {
		t.Fatal("expected at least one project-level suggestion")
	}
}

// --- Resource generation ---

func newTestResourceGatewayAndClient() (*fakeDomainGateway, *fakeAIClient) {
	gw := &fakeDomainGateway{
		existingWorkItems: map[string][]DomainGatewayWorkItem{
			"project_1": {{ID: "work_1", Description: "Install ceramic tile adhesive flooring"}},
		},
		materialCandidates:   []DomainGatewayMaterial{{ID: "mat_1", Name: "Premium Tile Adhesive"}},
		existingRequirements: map[string][]DomainGatewayRequirement{},
	}
	client := &fakeAIClient{
		resourceResp: platformai.ResourceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "resources-v1", SchemaVersion: 1,
			Suggestions: []platformai.ResourceSuggestion{
				{ResourceType: "material", WorkItemID: "work_1", Name: "Tile Adhesive", CandidateMaterialID: strPtr("mat_1")},
				{ResourceType: "trade", WorkItemID: "work_1", Name: "Tiler"},
			},
		},
	}
	return gw, client
}

func TestSuggestResourcesHappyPath(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(result.Suggestions))
	}
}

func TestSuggestResourcesExactCatalogueMatchOverridesModelChoice(t *testing.T) {
	// Regression: Requested "Tile Adhesive" with an exact "Tile Adhesive"
	// catalogue entry present must never resolve to "Cement" merely
	// because Python's own candidateMaterialId pointed there.
	repo := newFakeAIRepo()
	gw := &fakeDomainGateway{
		existingWorkItems: map[string][]DomainGatewayWorkItem{
			"project_1": {{ID: "work_1", Description: "Install ceramic tile adhesive flooring"}},
		},
		materialCandidates: []DomainGatewayMaterial{
			{ID: "mat_cement", Name: "Cement", Category: "Flooring", Unit: "bag"},
			{ID: "mat_adhesive", Name: "Tile Adhesive", Category: "Flooring", Unit: "bag"},
		},
		existingRequirements: map[string][]DomainGatewayRequirement{},
	}
	client := &fakeAIClient{
		resourceResp: platformai.ResourceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "resources-v1", SchemaVersion: 1,
			Suggestions: []platformai.ResourceSuggestion{
				{ResourceType: "material", WorkItemID: "work_1", Name: "Tile Adhesive", CandidateMaterialID: strPtr("mat_cement")},
			},
		},
	}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(result.Suggestions))
	}
	got := result.Suggestions[0].SuggestedData.MaterialResource.CandidateMaterialID
	if got == nil || *got != "mat_adhesive" {
		t.Fatalf("CandidateMaterialID = %v, want mat_adhesive (exact match must win over model's Cement choice)", got)
	}
}

func TestSuggestResourcesFiltersUngroundedMaterialSuggestion(t *testing.T) {
	// Regression: a material-typed suggestion whose name is not grounded in
	// the parent Work Item's Description must be filtered out entirely,
	// not persisted as a definite requirement.
	repo := newFakeAIRepo()
	gw := &fakeDomainGateway{
		existingWorkItems: map[string][]DomainGatewayWorkItem{
			// No material named at all — a conditional/unspecified flooring item.
			"project_1": {{ID: "work_1", Description: "Replace damaged flooring where necessary"}},
		},
		materialCandidates:   []DomainGatewayMaterial{{ID: "mat_1", Name: "Porcelain Tile"}},
		existingRequirements: map[string][]DomainGatewayRequirement{},
	}
	client := &fakeAIClient{
		resourceResp: platformai.ResourceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "resources-v1", SchemaVersion: 1,
			Suggestions: []platformai.ResourceSuggestion{
				{ResourceType: "material", WorkItemID: "work_1", Name: "Porcelain Tile", CandidateMaterialID: strPtr("mat_1")},
				{ResourceType: "trade", WorkItemID: "work_1", Name: "Flooring Installer"},
			},
		},
	}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("expected only the trade suggestion to survive, got %d: %+v", len(result.Suggestions), result.Suggestions)
	}
	if result.Suggestions[0].Type != SuggestionTypeTradeResource {
		t.Fatalf("expected surviving suggestion to be the trade resource, got %+v", result.Suggestions[0])
	}
}

func TestSuggestResourcesNoMatchNullsCandidateRatherThanForcingBadMatch(t *testing.T) {
	repo := newFakeAIRepo()
	gw := &fakeDomainGateway{
		existingWorkItems: map[string][]DomainGatewayWorkItem{
			"project_1": {{ID: "work_1", Description: "Install premium waterproof membrane sealant"}},
		},
		materialCandidates: []DomainGatewayMaterial{
			{ID: "mat_unrelated", Name: "Cement", Category: "Flooring", Unit: "bag"},
		},
		existingRequirements: map[string][]DomainGatewayRequirement{},
	}
	client := &fakeAIClient{
		resourceResp: platformai.ResourceSuggestionResponse{
			Provider: "mock", Model: "mock-v1", PromptVersion: "resources-v1", SchemaVersion: 1,
			Suggestions: []platformai.ResourceSuggestion{
				{ResourceType: "material", WorkItemID: "work_1", Name: "Waterproof Membrane Sealant", CandidateMaterialID: strPtr("mat_unrelated")},
			},
		},
	}
	svc := NewService(repo, gw, client)

	result, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(result.Suggestions))
	}
	got := result.Suggestions[0].SuggestedData.MaterialResource.CandidateMaterialID
	if got != nil {
		t.Fatalf("CandidateMaterialID = %v, want nil (no trustworthy match, must not force Cement)", *got)
	}
}

func TestSuggestResourcesRequiresAtLeastOneConfirmedWorkItem(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	gw.existingWorkItems["project_1"] = nil
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err != ErrNoConfirmedWorkItems {
		t.Fatalf("expected ErrNoConfirmedWorkItems, got %v", err)
	}
}

func TestSuggestResourcesValidatesWorkItemIDIsSupplied(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	client.resourceResp.Suggestions = []platformai.ResourceSuggestion{
		{ResourceType: "material", WorkItemID: "work_not_supplied", Name: "Bogus"},
	}
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err == nil {
		t.Fatal("expected an error for a suggestion referencing an unsupplied workItemId")
	}
}

func TestSuggestResourcesValidatesCandidateMaterialIDIsSupplied(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	client.resourceResp.Suggestions = []platformai.ResourceSuggestion{
		{ResourceType: "material", WorkItemID: "work_1", Name: "Bogus", CandidateMaterialID: strPtr("mat_not_supplied")},
	}
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err == nil {
		t.Fatal("expected an error for a suggestion referencing an unsupplied candidateMaterialId")
	}
}

// --- Provider-failure/compensation across all three types ---

func TestSuggestSpacesInvalidResponseMarksBatchFailed(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestSpaceGatewayAndClient()
	client.spaceErr = platformai.ErrInvalidProviderResponse
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestSpaces(context.Background(), "company_a", "project_1", "user_1", "op_1")
	if err == nil {
		t.Fatal("expected an error")
	}
	batch, _ := repo.FindBatchByOperationID(context.Background(), "company_a", "op_1")
	if batch.Status != BatchStatusFailed {
		t.Fatalf("expected failed batch, got %s", batch.Status)
	}
}

func TestSuggestResourcesProviderFailureMarksBatchFailedWithNoSuggestions(t *testing.T) {
	repo := newFakeAIRepo()
	gw, client := newTestResourceGatewayAndClient()
	client.resourceErr = platformai.ErrProviderUnavailable
	svc := NewService(repo, gw, client)

	_, err := svc.SuggestResources(context.Background(), "company_a", "project_1", "user_1", "op_3")
	if err == nil {
		t.Fatal("expected an error")
	}
	batch, _ := repo.FindBatchByOperationID(context.Background(), "company_a", "op_3")
	if batch.Status != BatchStatusFailed {
		t.Fatalf("expected failed batch, got %s", batch.Status)
	}
	suggestions, _ := repo.ListSuggestionsByBatch(context.Background(), "company_a", batch.ID)
	if len(suggestions) != 0 {
		t.Fatalf("expected zero reviewable suggestions, got %d", len(suggestions))
	}
}
