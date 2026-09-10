package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
)

// ErrScopeBriefEmpty is returned when Space/Work generation is attempted
// against a Project whose ScopeBrief is empty after trimming (design doc
// §9.1, §16: "scopeBrief trim non-empty else 422-domain error").
var ErrScopeBriefEmpty = errors.New("ai: project scope brief is empty")

// ErrGenerationInProgressLocal mirrors ErrGenerationInProgress at the
// service layer for the "same operationId+same fingerprint+processing"
// case (design doc §16); Service returns the errors.go sentinel directly.

// ErrNoConfirmedSpaces is returned when Work Item generation is attempted
// before at least one real Space exists (design doc §10.1 precondition).
var ErrNoConfirmedSpaces = errors.New("ai: at least one confirmed Space is required before Work Item generation")

// ErrNoConfirmedWorkItems is returned when Resource generation is attempted
// before at least one real WorkItem exists (design doc §12.1 precondition).
var ErrNoConfirmedWorkItems = errors.New("ai: at least one confirmed Work Item is required before Resource generation")

// ErrGatewayProjectNotFound is the DomainGateway-level project-not-found
// sentinel. Service maps it to the package-level ErrProjectNotFound so
// callers only ever match one sentinel regardless of which layer detected
// the missing project.
var ErrGatewayProjectNotFound = errors.New("aiintegration: project not found")

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company.
var ErrProjectNotFound = errors.New("ai: project not found")

// --- DomainGateway: the narrow read capability Service needs from
// aiintegration.Adapter, expressed in AI-owned primitive types so this
// package never imports aiintegration's or any domain module's types
// directly (ADR 0002 applied one level up: aiintegration is the ONLY
// adapter that imports domain-module types; internal/ai imports neither
// aiintegration's package-level structs nor any domain package). ---

type DomainGatewayProject struct {
	ID         string
	ScopeBrief string
}

type DomainGatewaySpace struct {
	ID   string
	Name string
	Type string
}

type DomainGatewayWorkItem struct {
	ID          string
	SpaceID     string
	Description string
	WorkType    string
}

type DomainGatewayMaterial struct {
	ID       string
	Name     string
	Category string
	Unit     string
}

type DomainGatewayRequirement struct {
	ID           string
	WorkItemID   string
	ResourceType string
	Name         string
}

// DomainGateway is the read capability Service needs to build generation
// context. internal/aiintegration.Adapter satisfies this structurally.
type DomainGateway interface {
	GetProjectAIContext(ctx context.Context, companyID, projectID string) (DomainGatewayProject, error)
	ListSpacesForAI(ctx context.Context, companyID, projectID string) ([]DomainGatewaySpace, error)
	ListWorkItemsForAI(ctx context.Context, companyID, projectID string) ([]DomainGatewayWorkItem, error)
	ListMaterialCandidatesForAI(ctx context.Context, companyID string) ([]DomainGatewayMaterial, error)
	ListResourceRequirementsForAI(ctx context.Context, companyID, projectID string) ([]DomainGatewayRequirement, error)
}

// AIClient is the capability Service needs from the Python AI service.
// platform/ai.Client satisfies this structurally.
type AIClient interface {
	SuggestSpaces(ctx context.Context, req platformai.SpaceSuggestionRequest) (platformai.SpaceSuggestionResponse, error)
	SuggestWorkItems(ctx context.Context, req platformai.WorkItemSuggestionRequest) (platformai.WorkItemSuggestionResponse, error)
	SuggestResources(ctx context.Context, req platformai.ResourceSuggestionRequest) (platformai.ResourceSuggestionResponse, error)
}

// GenerationResult is what a successful (or idempotently-retried) Suggest*
// call returns: the completed batch plus its reviewable suggestions.
type GenerationResult struct {
	Batch       AIGenerationBatch
	Suggestions []AISuggestion
}

// Service orchestrates AI generation: authorization, fingerprinting,
// idempotency, calling Python, output validation, and all-or-nothing
// suggestion persistence (design doc §16-§17). It also orchestrates
// governed suggestion acceptance/rejection through the narrow *Creator
// capabilities wired in by SetSpaceCreator/SetWorkItemCreator/etc.
type Service struct {
	repo   Repository
	domain DomainGateway
	client AIClient

	spaces    SpaceCreator
	workItems WorkItemCreator
	resources ResourceCreator
}

// NewService constructs a Service. Acceptance capabilities (SpaceCreator
// etc.) are wired in separately via SetSpaceCreator and friends, mirroring
// this codebase's SetX composition pattern for cycles the composition root
// must close after construction (e.g. rfqIssuanceService.
// SetSupplierRFQAccessAuthorizer in cmd/api/main.go).
func NewService(repo Repository, domain DomainGateway, client AIClient) *Service {
	return &Service{repo: repo, domain: domain, client: client}
}

// SetSpaceCreator wires the Space-acceptance capability.
func (s *Service) SetSpaceCreator(spaces SpaceCreator) {
	s.spaces = spaces
}

// SetWorkItemCreator wires the WorkItem-acceptance capability.
func (s *Service) SetWorkItemCreator(workItems WorkItemCreator) {
	s.workItems = workItems
}

// SetResourceCreator wires the Resource-acceptance capability.
func (s *Service) SetResourceCreator(resources ResourceCreator) {
	s.resources = resources
}

func normalizeSpaceKey(name, spaceType string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + strings.ToLower(strings.TrimSpace(spaceType))
}

// SuggestSpaces runs one Space-generation cycle for projectID (design doc
// §9, §16-§18). See the flow comment inline for each step.
func (s *Service) SuggestSpaces(ctx context.Context, companyID, projectID, userID, operationID string) (GenerationResult, error) {
	project, err := s.domain.GetProjectAIContext(ctx, companyID, projectID)
	if err != nil {
		if errors.Is(err, ErrGatewayProjectNotFound) {
			return GenerationResult{}, ErrProjectNotFound
		}
		return GenerationResult{}, err
	}

	trimmedBrief := strings.TrimSpace(project.ScopeBrief)
	if trimmedBrief == "" {
		return GenerationResult{}, ErrScopeBriefEmpty
	}

	existingSpaces, err := s.domain.ListSpacesForAI(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}

	fingerprintRefs := make([]FingerprintSpaceRef, 0, len(existingSpaces))
	for _, sp := range existingSpaces {
		fingerprintRefs = append(fingerprintRefs, FingerprintSpaceRef{ID: sp.ID, Name: sp.Name, Type: sp.Type})
	}
	inputFingerprint := SpaceFingerprint(SpaceFingerprintInput{ScopeBrief: trimmedBrief, ExistingSpaces: fingerprintRefs})

	existing, existingErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID)
	if existingErr == nil {
		result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint)
		if checkErr != nil {
			return GenerationResult{}, checkErr
		}
		if ok {
			return result, nil
		}
	}

	batch, err := s.repo.CreateProcessingBatch(ctx, AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID,
		Type: BatchTypeSpaceSuggestions, Status: BatchStatusProcessing,
		OperationID: operationID, Provider: "", Model: "", PromptVersion: "spaces-v1",
		SchemaVersion: 1, InputFingerprint: inputFingerprint, SourceBrief: trimmedBrief,
		StartedAt: time.Now(), CreatedByUserID: userID, CreatedAt: time.Now(),
	})
	if err != nil {
		// Lost the create race against a concurrent identical request;
		// re-read and reconcile exactly like the pre-check above.
		existing, findErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID)
		if findErr == nil {
			result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint)
			if checkErr != nil {
				return GenerationResult{}, checkErr
			}
			if ok {
				return result, nil
			}
		}
		return GenerationResult{}, err
	}

	existingSpaceReqs := make([]platformai.ExistingSpace, 0, len(existingSpaces))
	for _, sp := range existingSpaces {
		existingSpaceReqs = append(existingSpaceReqs, platformai.ExistingSpace{ID: sp.ID, Name: sp.Name, Type: sp.Type})
	}

	resp, err := s.client.SuggestSpaces(ctx, platformai.SpaceSuggestionRequest{
		OperationID:    operationID,
		Project:        platformai.ProjectContext{ID: projectID, ScopeBrief: trimmedBrief},
		ExistingSpaces: existingSpaceReqs,
	})
	if err != nil {
		_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, mapClientErrorCode(err))
		return GenerationResult{}, err
	}

	items, metaBySpaceKey := toSpaceSuggestionData(resp.Suggestions)
	gateResult := ApplySpaceQualityGate(items, trimmedBrief)
	if len(gateResult.Gaps) > 0 {
		repairedItems, repairedResp, repairErr := s.repairSpaceCoverageGaps(ctx, operationID, projectID, trimmedBrief, existingSpaceReqs, gateResult)
		// A failed/empty repair attempt is not fatal (T1.5 §9: "retain
		// valid suggestions + surface/record quality failure rather than
		// synthesize potentially incorrect project entities") — proceed
		// with the pre-repair gateResult in that case.
		if repairErr == nil && len(repairedItems) > 0 {
			gateResult = ApplySpaceQualityGate(repairedItems, trimmedBrief)
			for k, v := range toSpaceMeta(repairedResp) {
				metaBySpaceKey[k] = v
			}
		}
	}

	existingKeys := make(map[string]bool, len(existingSpaces))
	for _, sp := range existingSpaces {
		existingKeys[normalizeSpaceKey(sp.Name, sp.Type)] = true
	}

	now := time.Now()
	suggestions := make([]AISuggestion, 0, len(gateResult.Items))
	for _, item := range gateResult.Items {
		if existingKeys[normalizeSpaceKey(item.Name, item.SpaceType)] {
			continue // exact current-space normalized duplicate: filter, do not persist (design doc §16)
		}
		meta := metaBySpaceKey[normalizeSpaceKey(item.Name, item.SpaceType)]
		suggestions = append(suggestions, AISuggestion{
			CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
			Type: SuggestionTypeSpace, Status: SuggestionStatusPending,
			Confidence:       meta.Confidence,
			Rationale:        meta.Rationale,
			SuggestedData:    SuggestedData{Space: &item},
			InputFingerprint: inputFingerprint,
			Revision:         1, CreatedAt: now, UpdatedAt: now,
		})
	}

	inserted, err := s.finalizeBatch(ctx, companyID, batch.ID, suggestions)
	if err != nil {
		return GenerationResult{}, err
	}

	return GenerationResult{
		Batch:       withCompletedStatus(batch),
		Suggestions: inserted,
	}, nil
}

// SpaceAcceptanceResult is the AI-owned snapshot of a Space created (or
// found) during acceptance.
type SpaceAcceptanceResult struct {
	ID        string
	Name      string
	SpaceType string
}

// SpaceAcceptanceOverride carries contractor-edited values for Edit & Accept
// (design doc §26.1). A nil override means "accept unchanged."
type SpaceAcceptanceOverride struct {
	Name        string
	SpaceType   string
	Description string
}

// SpaceCreator is the narrow capability Service needs from spaces.Service to
// accept a Space suggestion. spaces.Service satisfies this structurally
// through CreateSpaceFromAISuggestion/FindSpaceBySourceSuggestionID (Task 4)
// plus a duplicate-likelihood check the composition adapter derives from
// spaces.Service's existing lookups.
type SpaceCreator interface {
	CreateSpaceFromAISuggestion(ctx context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (SpaceAcceptanceResult, error)
	FindSpaceBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (SpaceAcceptanceResult, error)
	SpaceLikelyDuplicate(ctx context.Context, companyID, projectID, name, spaceType string) (bool, error)
	// UpdateSpaceFromAISuggestion mutates an existing authoritative Space by
	// ID (T1.5 PART C "Use suggestion" for a CHANGED delta item) — never
	// creates a duplicate.
	UpdateSpaceFromAISuggestion(ctx context.Context, companyID, spaceID, name, spaceType, description string) (SpaceAcceptanceResult, error)
}

// ErrSpaceAcceptanceNotFound mirrors spaces.ErrSpaceNotFound at this
// package's boundary.
var ErrSpaceAcceptanceNotFound = errors.New("ai: space not found")

// ErrSpaceLikelyDuplicate is returned when acceptance would create a Space
// that normalized-matches a current real Space (design doc §20.1) — no new
// Space is created.
var ErrSpaceLikelyDuplicate = errors.New("ai: space likely duplicate")

// AcceptSpaceSuggestion accepts (or edits & accepts) a pending Space
// suggestion (design doc §9.3, §25.2). override=nil means "accept as
// presented"; a non-nil override means the contractor edited fields, and
// the suggestion becomes modified rather than accepted (design doc §8.1).
//
// Idempotency/repair: if a prior attempt created the Space but failed to
// mark the suggestion terminal (process crash between the two writes), this
// call detects the existing Space by sourceSuggestionId, repairs the
// suggestion's terminal state, and returns that same Space rather than
// creating a duplicate (design doc §18.2).
func (s *Service) AcceptSpaceSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, override *SpaceAcceptanceOverride) (SpaceAcceptanceResult, error) {
	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return SpaceAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return SpaceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	if suggestion.SuggestedData.Space == nil {
		return SpaceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	name := suggestion.SuggestedData.Space.Name
	spaceType := suggestion.SuggestedData.Space.SpaceType
	description := ""
	if override != nil {
		name = override.Name
		spaceType = override.SpaceType
		description = override.Description
	}

	// Ambiguous-retry repair: if a Space already exists for this
	// suggestionId, a prior attempt got far enough to create it. Skip the
	// duplicate check and creation entirely — repairing the suggestion's
	// terminal state is the only remaining step.
	if existing, findErr := s.spaces.FindSpaceBySourceSuggestionID(ctx, companyID, suggestionID); findErr == nil {
		if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, existing.ID, override != nil); err != nil {
			return SpaceAcceptanceResult{}, err
		}
		return existing, nil
	}

	likelyDuplicate, err := s.spaces.SpaceLikelyDuplicate(ctx, companyID, suggestion.ProjectID, name, spaceType)
	if err != nil {
		return SpaceAcceptanceResult{}, err
	}
	if likelyDuplicate {
		return SpaceAcceptanceResult{}, ErrSpaceLikelyDuplicate
	}

	created, err := s.spaces.CreateSpaceFromAISuggestion(ctx, companyID, suggestion.ProjectID, name, spaceType, description, suggestionID)
	if err != nil {
		return SpaceAcceptanceResult{}, err
	}

	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, created.ID, override != nil); err != nil {
		return SpaceAcceptanceResult{}, err
	}
	return created, nil
}

// UseSpaceDeltaSuggestion applies a CHANGED-classified rerun suggestion to
// its linked authoritative Space in place (T1.5 PART C §16-17 "Use
// suggestion") — mutates via the existing spaces.Service.UpdateSpace path,
// never creating a duplicate. currentAuthoritativeSpaceID must be the exact
// ID ClassifySpaceDelta resolved (a trustworthy stable link) — callers must
// not pass a guessed ID. "Keep current" (the alternative to Use suggestion)
// requires no domain mutation at all — it is a pure suggestion-disposition
// no-op the caller can implement by simply not calling this.
func (s *Service) UseSpaceDeltaSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, currentAuthoritativeSpaceID string) (SpaceAcceptanceResult, error) {
	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return SpaceAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return SpaceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	if suggestion.SuggestedData.Space == nil {
		return SpaceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	data := suggestion.SuggestedData.Space
	updated, err := s.spaces.UpdateSpaceFromAISuggestion(ctx, companyID, currentAuthoritativeSpaceID, data.Name, data.SpaceType, "")
	if err != nil {
		return SpaceAcceptanceResult{}, err
	}

	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, updated.ID, true); err != nil {
		return SpaceAcceptanceResult{}, err
	}
	return updated, nil
}

// repairSuggestionAcceptance marks suggestionID accepted (or modified, if
// edited) with acceptedDomainObjectID, tolerating the case where a prior
// attempt already advanced the suggestion's terminal state and revision —
// that is success too, not a fresh CAS failure, since the domain object
// this call was asked to converge on already matches.
func (s *Service) repairSuggestionAcceptance(ctx context.Context, companyID, suggestionID string, expectedRevision int64, domainObjectID string, modified bool) error {
	var err error
	if modified {
		err = s.repo.ConditionalModify(ctx, companyID, suggestionID, expectedRevision, domainObjectID)
	} else {
		err = s.repo.ConditionalAccept(ctx, companyID, suggestionID, expectedRevision, domainObjectID)
	}
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrSuggestionRevisionMismatch) {
		return err
	}
	// The CAS lost because a prior attempt already terminalized this
	// suggestion. That is only a genuine conflict if it terminalized to a
	// DIFFERENT domain object than the one we just converged on.
	current, findErr := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if findErr != nil {
		return err
	}
	if current.IsTerminal() && current.AcceptedDomainObjectID == domainObjectID {
		return nil
	}
	return err
}

// WorkItemAcceptanceResult is the AI-owned snapshot of a WorkItem created
// (or found) during acceptance.
type WorkItemAcceptanceResult struct {
	ID          string
	Description string
}

// WorkItemAcceptanceInput carries the contractor-confirmed fields required
// to accept a WorkItem suggestion (design doc §10.6): the AI suggestion
// itself never supplies quantity/unit, so the contractor must always supply
// them here, exactly as they would for a manual WorkItem create.
type WorkItemAcceptanceInput struct {
	Description    string
	WorkType       string
	ScopeLevel     ScopeLevel
	SpaceID        *string
	QuantityValue  string
	QuantityUnit   string
	AllowDuplicate bool
}

// WorkItemCreator is the narrow capability Service needs from work.Service
// to accept a WorkItem suggestion. work.Service satisfies this structurally
// through CreateWorkItemFromAISuggestion/FindWorkItemBySourceSuggestionID
// (Task 4) plus a duplicate-likelihood check the composition adapter
// derives from work.Service's existing lookups.
type WorkItemCreator interface {
	CreateWorkItemFromAISuggestion(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (WorkItemAcceptanceResult, error)
	FindWorkItemBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkItemAcceptanceResult, error)
	WorkItemLikelyDuplicate(ctx context.Context, companyID, projectID string, spaceID *string, description string) (bool, error)
	// UpdateWorkItemFromAISuggestion mutates an existing authoritative Work
	// Item by ID (T1.5B "Use suggestion" for a CHANGED delta item) — never
	// creates a duplicate.
	UpdateWorkItemFromAISuggestion(ctx context.Context, companyID, workItemID, description, workType string) (WorkItemAcceptanceResult, error)
}

var (
	ErrWorkItemAcceptanceNotFound         = errors.New("ai: work item not found")
	ErrWorkItemAcceptanceSpaceNotFound    = errors.New("ai: space not found")
	ErrWorkItemAcceptanceQuantityRequired = errors.New(
		"ai: quantity value and unit are required to accept a work item suggestion")
	ErrWorkItemAcceptanceInvalidScope = errors.New(
		"ai: scopeLevel=project requires spaceId to be null")
	ErrWorkItemAcceptanceDuplicate = errors.New("ai: work item likely duplicate")
)

// AcceptWorkItemSuggestion accepts (or edits & accepts) a pending WorkItem
// suggestion (design doc §10.6, §26). Unlike Space, the WorkItemAcceptanceInput
// is always required in full — Description/WorkType/ScopeLevel/SpaceID may
// be unchanged from the suggestion, but QuantityValue/QuantityUnit are
// NEVER present on the suggestion itself and must always come from input.
//
// The suggestion becomes `modified` only when the contractor changed an
// AI-suggested field (description/workType/scopeLevel/spaceId); supplying
// contractor-only quantity/unit does not, by itself, convert `accepted` to
// `modified` (design doc plan Task 10 Step 5).
func (s *Service) AcceptWorkItemSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, input WorkItemAcceptanceInput) (WorkItemAcceptanceResult, error) {
	if input.QuantityValue == "" || input.QuantityUnit == "" {
		return WorkItemAcceptanceResult{}, ErrWorkItemAcceptanceQuantityRequired
	}
	if input.ScopeLevel == ScopeLevelProject && input.SpaceID != nil {
		return WorkItemAcceptanceResult{}, ErrWorkItemAcceptanceInvalidScope
	}

	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return WorkItemAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return WorkItemAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	original := suggestion.SuggestedData.WorkItem
	if original == nil {
		return WorkItemAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	edited := input.Description != original.Description ||
		input.WorkType != original.WorkType ||
		input.ScopeLevel != original.ScopeLevel ||
		!samePtr(input.SpaceID, original.SpaceID)

	// Ambiguous-retry repair, same shape as Space acceptance.
	if existing, findErr := s.workItems.FindWorkItemBySourceSuggestionID(ctx, companyID, suggestionID); findErr == nil {
		if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, existing.ID, edited); err != nil {
			return WorkItemAcceptanceResult{}, err
		}
		return existing, nil
	}

	if !input.AllowDuplicate {
		likelyDuplicate, err := s.workItems.WorkItemLikelyDuplicate(ctx, companyID, suggestion.ProjectID, input.SpaceID, input.Description)
		if err != nil {
			return WorkItemAcceptanceResult{}, err
		}
		if likelyDuplicate {
			return WorkItemAcceptanceResult{}, ErrWorkItemAcceptanceDuplicate
		}
	}

	created, err := s.workItems.CreateWorkItemFromAISuggestion(ctx, companyID, suggestion.ProjectID, input.SpaceID,
		input.Description, input.WorkType, input.QuantityValue, input.QuantityUnit, suggestionID)
	if err != nil {
		return WorkItemAcceptanceResult{}, err
	}

	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, created.ID, edited); err != nil {
		return WorkItemAcceptanceResult{}, err
	}
	return created, nil
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// UseWorkItemDeltaSuggestion applies a CHANGED-classified rerun suggestion
// to its linked authoritative Work Item in place (T1.5B "Use suggestion")
// — mutates via the existing work.Service.UpdateWorkItem path, never
// creating a duplicate. currentAuthoritativeWorkItemID must be the exact ID
// ClassifyWorkItemDelta resolved (a trustworthy stable link) — callers must
// not pass a guessed ID. "Keep current" requires no domain mutation at all.
func (s *Service) UseWorkItemDeltaSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, currentAuthoritativeWorkItemID string) (WorkItemAcceptanceResult, error) {
	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return WorkItemAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return WorkItemAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	data := suggestion.SuggestedData.WorkItem
	if data == nil {
		return WorkItemAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	updated, err := s.workItems.UpdateWorkItemFromAISuggestion(ctx, companyID, currentAuthoritativeWorkItemID, data.Description, data.WorkType)
	if err != nil {
		return WorkItemAcceptanceResult{}, err
	}

	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, updated.ID, true); err != nil {
		return WorkItemAcceptanceResult{}, err
	}
	return updated, nil
}

// ResourceRequirementType mirrors workresources.ResourceType at this
// package's boundary (material/trade/equipment).
type ResourceRequirementType string

const (
	ResourceRequirementTypeMaterial  ResourceRequirementType = "material"
	ResourceRequirementTypeTrade     ResourceRequirementType = "trade"
	ResourceRequirementTypeEquipment ResourceRequirementType = "equipment"
)

// ResourceAcceptanceResult is the AI-owned snapshot of a
// WorkResourceRequirement created (or found) during acceptance.
type ResourceAcceptanceResult struct {
	RequirementID string
	MaterialID    string // set only when a Material was linked/created in this acceptance
	Name          string
}

// MaterialAcceptanceResult is the AI-owned snapshot of a Material created
// (or found) during the Create & Add flow.
type MaterialAcceptanceResult struct {
	ID   string
	Name string
}

// MaterialAcceptanceMode selects between linking an existing catalog
// Material and creating a new one (design doc §12.4-12.5).
type MaterialAcceptanceMode string

const (
	MaterialAcceptanceModeExisting MaterialAcceptanceMode = "existing"
	MaterialAcceptanceModeCreate   MaterialAcceptanceMode = "create"
)

// NewMaterialInput carries the normal contractor-controlled Material fields
// required by the Create & Add flow (design doc §12.5). Only Name may be
// AI-suggested-prefilled; every other field remains contractor-supplied.
type NewMaterialInput struct {
	Name                   string
	Category               string
	Specification          string
	Unit                   string
	ReferencePriceAmount   int64
	ReferencePriceCurrency string
}

// MaterialResourceAcceptanceInput carries the contractor's decision for a
// material_resource suggestion.
type MaterialResourceAcceptanceInput struct {
	Mode        MaterialAcceptanceMode
	MaterialID  *string           // required when Mode == existing
	NewMaterial *NewMaterialInput // required when Mode == create
}

// ResourceCreator is the narrow capability Service needs from
// workresources.Service and materials.Service to accept a Resource
// suggestion. Both satisfy this structurally through the AI-creation seams
// built in Tasks 4-5.
type ResourceCreator interface {
	CreateResourceRequirementFromAISuggestion(ctx context.Context, companyID, projectID, workItemID string, resourceType ResourceRequirementType, materialID *string, name, sourceSuggestionID string) (ResourceAcceptanceResult, error)
	FindResourceRequirementBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (ResourceAcceptanceResult, error)
	ResourceRequirementLikelyDuplicate(ctx context.Context, companyID, projectID, workItemID string, resourceType ResourceRequirementType, materialID *string, name string) (bool, error)

	MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
	CreateMaterialFromAISuggestion(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (MaterialAcceptanceResult, error)
	FindMaterialBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (MaterialAcceptanceResult, error)
}

var (
	ErrResourceAcceptanceNotFound         = errors.New("ai: resource requirement not found")
	ErrResourceAcceptanceWorkItemNotFound = errors.New("ai: work item not found")
	ErrResourceAcceptanceMaterialNotFound = errors.New("ai: material not found")
	ErrResourceAcceptanceDuplicate        = errors.New("ai: resource requirement likely duplicate")
	ErrResourceAcceptanceModeRequired     = errors.New("ai: a material decision (existing or create) is required")
)

// AcceptMaterialResourceSuggestion accepts a pending material_resource
// suggestion (design doc §12.3-12.5). Existing mode links a contractor-
// chosen catalog Material; create mode runs the repairable Create & Add
// saga: create the Material (or find it if a prior attempt already did),
// then create the WorkResourceRequirement, then terminalize the suggestion
// — all three steps individually idempotent by sourceSuggestionId so a
// crash between any two steps is safely retried to completion without
// duplicating either the Material or the requirement (design doc §18.2,
// Task 10 Step 9-10). The browser never separately calls POST /materials
// and then accepts — this single operation owns the whole saga.
func (s *Service) AcceptMaterialResourceSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, input MaterialResourceAcceptanceInput) (ResourceAcceptanceResult, error) {
	if input.Mode != MaterialAcceptanceModeExisting && input.Mode != MaterialAcceptanceModeCreate {
		return ResourceAcceptanceResult{}, ErrResourceAcceptanceModeRequired
	}

	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return ResourceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	data := suggestion.SuggestedData.MaterialResource
	if data == nil {
		return ResourceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	// Ambiguous-retry repair for the requirement itself, same shape as
	// Space/WorkItem acceptance.
	if existing, findErr := s.resources.FindResourceRequirementBySourceSuggestionID(ctx, companyID, suggestionID); findErr == nil {
		if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, existing.RequirementID, false); err != nil {
			return ResourceAcceptanceResult{}, err
		}
		return existing, nil
	}

	var materialID string
	var materialResultID string
	if input.Mode == MaterialAcceptanceModeExisting {
		if input.MaterialID == nil {
			return ResourceAcceptanceResult{}, ErrResourceAcceptanceMaterialNotFound
		}
		belongs, err := s.resources.MaterialBelongsToCompany(ctx, companyID, *input.MaterialID)
		if err != nil {
			return ResourceAcceptanceResult{}, err
		}
		if !belongs {
			return ResourceAcceptanceResult{}, ErrResourceAcceptanceMaterialNotFound
		}
		materialID = *input.MaterialID
	} else {
		if input.NewMaterial == nil {
			return ResourceAcceptanceResult{}, ErrResourceAcceptanceModeRequired
		}
		// Step 1 of the saga: create (or, on retry, find) the Material by
		// this suggestion's provenance before creating the requirement.
		var material MaterialAcceptanceResult
		if existingMaterial, findErr := s.resources.FindMaterialBySourceSuggestionID(ctx, companyID, suggestionID); findErr == nil {
			material = existingMaterial
		} else {
			material, err = s.resources.CreateMaterialFromAISuggestion(ctx, companyID,
				input.NewMaterial.Name, input.NewMaterial.Category, input.NewMaterial.Specification, input.NewMaterial.Unit,
				input.NewMaterial.ReferencePriceAmount, input.NewMaterial.ReferencePriceCurrency, suggestionID)
			if err != nil {
				return ResourceAcceptanceResult{}, err
			}
		}
		materialID = material.ID
		materialResultID = material.ID
	}

	likelyDuplicate, err := s.resources.ResourceRequirementLikelyDuplicate(ctx, companyID, suggestion.ProjectID, data.WorkItemID, ResourceRequirementTypeMaterial, &materialID, data.Name)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}
	if likelyDuplicate {
		return ResourceAcceptanceResult{}, ErrResourceAcceptanceDuplicate
	}

	// Step 2 of the saga: create the requirement.
	created, err := s.resources.CreateResourceRequirementFromAISuggestion(ctx, companyID, suggestion.ProjectID,
		data.WorkItemID, ResourceRequirementTypeMaterial, &materialID, data.Name, suggestionID)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}
	created.MaterialID = materialResultID

	// Step 3 of the saga: terminalize the suggestion.
	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, created.RequirementID, false); err != nil {
		return ResourceAcceptanceResult{}, err
	}
	return created, nil
}

// AcceptTradeOrEquipmentSuggestion accepts a pending trade_resource or
// equipment_resource suggestion (design doc §12.6-12.7). It creates a
// confirmed requirement only — never a Worker, LabourEntry, labour rate, or
// equipment cost. editedName="" means accept the suggested name unchanged.
func (s *Service) AcceptTradeOrEquipmentSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64, editedName string) (ResourceAcceptanceResult, error) {
	suggestion, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}
	if suggestion.Status != SuggestionStatusPending || suggestion.Revision != expectedRevision {
		return ResourceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	var data *ResourceSuggestionData
	var resourceType ResourceRequirementType
	switch suggestion.Type {
	case SuggestionTypeTradeResource:
		data = suggestion.SuggestedData.TradeResource
		resourceType = ResourceRequirementTypeTrade
	case SuggestionTypeEquipmentResource:
		data = suggestion.SuggestedData.EquipmentResource
		resourceType = ResourceRequirementTypeEquipment
	default:
		return ResourceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}
	if data == nil {
		return ResourceAcceptanceResult{}, ErrSuggestionRevisionMismatch
	}

	name := data.Name
	edited := false
	if editedName != "" && editedName != data.Name {
		name = editedName
		edited = true
	}

	if existing, findErr := s.resources.FindResourceRequirementBySourceSuggestionID(ctx, companyID, suggestionID); findErr == nil {
		if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, existing.RequirementID, edited); err != nil {
			return ResourceAcceptanceResult{}, err
		}
		return existing, nil
	}

	likelyDuplicate, err := s.resources.ResourceRequirementLikelyDuplicate(ctx, companyID, suggestion.ProjectID, data.WorkItemID, resourceType, nil, name)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}
	if likelyDuplicate {
		return ResourceAcceptanceResult{}, ErrResourceAcceptanceDuplicate
	}

	created, err := s.resources.CreateResourceRequirementFromAISuggestion(ctx, companyID, suggestion.ProjectID,
		data.WorkItemID, resourceType, nil, name, suggestionID)
	if err != nil {
		return ResourceAcceptanceResult{}, err
	}

	if err := s.repairSuggestionAcceptance(ctx, companyID, suggestionID, expectedRevision, created.RequirementID, edited); err != nil {
		return ResourceAcceptanceResult{}, err
	}
	return created, nil
}

// RejectSuggestion transitions a pending suggestion to rejected, creating no
// domain object (design doc §8.1, §10.1). expectedRevision guards against a
// stale/racing client: a wrong revision or an already-terminal suggestion
// returns ErrSuggestionRevisionMismatch (409), while a suggestion that does
// not exist for this tenant at all returns ErrSuggestionNotFound (404) — a
// foreign company's suggestion ID must 404, never 409, so a caller cannot
// distinguish "wrong revision" from "not yours" and thereby confirm the ID
// exists in another tenant (design doc §36, tenant isolation).
func (s *Service) RejectSuggestion(ctx context.Context, companyID, suggestionID string, expectedRevision int64) error {
	if _, err := s.repo.FindSuggestionByID(ctx, companyID, suggestionID); err != nil {
		return err
	}
	return s.repo.ConditionalReject(ctx, companyID, suggestionID, expectedRevision)
}

// ListBatches returns companyID's generation batches for projectID and
// batchType, newest first.
func (s *Service) ListBatches(ctx context.Context, companyID, projectID string, batchType BatchType) ([]AIGenerationBatch, error) {
	return s.repo.ListBatchesByProject(ctx, companyID, projectID, batchType)
}

// ListSuggestionsByBatch returns every suggestion (any lifecycle status)
// belonging to batchID, tenant-scoped to companyID.
func (s *Service) ListSuggestionsByBatch(ctx context.Context, companyID, batchID string) ([]AISuggestion, error) {
	return s.repo.ListSuggestionsByBatch(ctx, companyID, batchID)
}

// SuggestionDelta pairs a persisted AISuggestion with its (non-authoritative,
// non-persisted) rerun delta classification (T1.5 PART C §16-17). Computed
// fresh on every read — never stored — so it always reflects current
// authoritative project state.
type SuggestionDelta struct {
	Suggestion             AISuggestion
	Classification         DeltaClassification
	CurrentAuthoritativeID string
}

// ListSuggestionsByBatchWithDelta returns batchID's suggestions the same as
// ListSuggestionsByBatch, each annotated with a delta classification.
// Reconciliation runs for Space, Work Item, and Resource suggestions
// (T1.5B closure) and only when the batch actually has pending suggestions
// of that type worth classifying — an already-fully-resolved batch skips
// the extra project-history/domain-gateway calls entirely, since the
// classification would have no active-review effect.
func (s *Service) ListSuggestionsByBatchWithDelta(ctx context.Context, companyID, batchID string) ([]SuggestionDelta, error) {
	suggestions, err := s.repo.ListSuggestionsByBatch(ctx, companyID, batchID)
	if err != nil {
		return nil, err
	}
	result := make([]SuggestionDelta, len(suggestions))
	for i, sug := range suggestions {
		result[i] = SuggestionDelta{Suggestion: sug}
	}
	if len(suggestions) == 0 {
		return result, nil
	}
	projectID := suggestions[0].ProjectID

	switch suggestions[0].Type {
	case SuggestionTypeSpace:
		if err := s.applySpaceDelta(ctx, companyID, batchID, projectID, suggestions, result); err != nil {
			return nil, err
		}
	case SuggestionTypeWorkItem:
		if err := s.applyWorkItemDelta(ctx, companyID, batchID, projectID, suggestions, result); err != nil {
			return nil, err
		}
	case SuggestionTypeMaterialResource, SuggestionTypeTradeResource, SuggestionTypeEquipmentResource:
		if err := s.applyResourceDelta(ctx, companyID, batchID, projectID, suggestions, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func hasPendingOfType(suggestions []AISuggestion, t SuggestionType) bool {
	for _, sug := range suggestions {
		if sug.Type == t && sug.Status == SuggestionStatusPending {
			return true
		}
	}
	return false
}

// batchSourceBrief finds the SourceBrief of the batch identified by
// batchID within batches (already fetched via ListBatchesByProject).
func batchSourceBrief(batches []AIGenerationBatch, batchID string) string {
	for _, b := range batches {
		if b.ID == batchID {
			return b.SourceBrief
		}
	}
	return ""
}

func (s *Service) applySpaceDelta(ctx context.Context, companyID, batchID, projectID string, suggestions []AISuggestion, result []SuggestionDelta) error {
	if !hasPendingOfType(suggestions, SuggestionTypeSpace) {
		return nil
	}
	currentSpaces, err := s.domain.ListSpacesForAI(ctx, companyID, projectID)
	if err != nil {
		return err
	}
	priorBatches, err := s.repo.ListBatchesByProject(ctx, companyID, projectID, BatchTypeSpaceSuggestions)
	if err != nil {
		return err
	}
	var priorDecisions []PriorSpaceDecision
	for _, b := range priorBatches {
		if b.ID == batchID || b.Status != BatchStatusCompleted {
			continue
		}
		priorSugs, err := s.repo.ListSuggestionsByBatch(ctx, companyID, b.ID)
		if err != nil {
			return err
		}
		for _, ps := range priorSugs {
			if ps.Type != SuggestionTypeSpace || ps.SuggestedData.Space == nil {
				continue
			}
			priorDecisions = append(priorDecisions, PriorSpaceDecision{
				Data: *ps.SuggestedData.Space, Status: ps.Status,
				SourceBrief: b.SourceBrief, AcceptedDomainObjectID: ps.AcceptedDomainObjectID,
			})
		}
	}
	currentBrief := batchSourceBrief(priorBatches, batchID)

	var fresh []SpaceSuggestionData
	var freshIndex []int
	for i, sug := range suggestions {
		if sug.Type == SuggestionTypeSpace && sug.Status == SuggestionStatusPending && sug.SuggestedData.Space != nil {
			fresh = append(fresh, *sug.SuggestedData.Space)
			freshIndex = append(freshIndex, i)
		}
	}
	deltaItems := ClassifySpaceDelta(fresh, currentSpaces, priorDecisions, currentBrief)
	for j, item := range deltaItems {
		i := freshIndex[j]
		result[i].Classification = item.Classification
		result[i].CurrentAuthoritativeID = item.CurrentAuthoritativeID
	}
	return nil
}

func (s *Service) applyWorkItemDelta(ctx context.Context, companyID, batchID, projectID string, suggestions []AISuggestion, result []SuggestionDelta) error {
	if !hasPendingOfType(suggestions, SuggestionTypeWorkItem) {
		return nil
	}
	currentWorkItems, err := s.domain.ListWorkItemsForAI(ctx, companyID, projectID)
	if err != nil {
		return err
	}
	priorBatches, err := s.repo.ListBatchesByProject(ctx, companyID, projectID, BatchTypeWorkItemSuggestions)
	if err != nil {
		return err
	}
	var priorDecisions []PriorWorkItemDecision
	for _, b := range priorBatches {
		if b.ID == batchID || b.Status != BatchStatusCompleted {
			continue
		}
		priorSugs, err := s.repo.ListSuggestionsByBatch(ctx, companyID, b.ID)
		if err != nil {
			return err
		}
		for _, ps := range priorSugs {
			if ps.Type != SuggestionTypeWorkItem || ps.SuggestedData.WorkItem == nil {
				continue
			}
			priorDecisions = append(priorDecisions, PriorWorkItemDecision{
				Data: *ps.SuggestedData.WorkItem, Status: ps.Status,
				SourceBrief: b.SourceBrief, AcceptedDomainObjectID: ps.AcceptedDomainObjectID,
			})
		}
	}
	currentBrief := batchSourceBrief(priorBatches, batchID)

	var fresh []WorkItemSuggestionData
	var freshIndex []int
	for i, sug := range suggestions {
		if sug.Type == SuggestionTypeWorkItem && sug.Status == SuggestionStatusPending && sug.SuggestedData.WorkItem != nil {
			fresh = append(fresh, *sug.SuggestedData.WorkItem)
			freshIndex = append(freshIndex, i)
		}
	}
	deltaItems := ClassifyWorkItemDelta(fresh, currentWorkItems, priorDecisions, currentBrief)
	for j, item := range deltaItems {
		i := freshIndex[j]
		result[i].Classification = item.Classification
		result[i].CurrentAuthoritativeID = item.CurrentAuthoritativeID
	}
	return nil
}

func (s *Service) applyResourceDelta(ctx context.Context, companyID, batchID, projectID string, suggestions []AISuggestion, result []SuggestionDelta) error {
	hasPending := hasPendingOfType(suggestions, SuggestionTypeMaterialResource) ||
		hasPendingOfType(suggestions, SuggestionTypeTradeResource) ||
		hasPendingOfType(suggestions, SuggestionTypeEquipmentResource)
	if !hasPending {
		return nil
	}
	currentRequirements, err := s.domain.ListResourceRequirementsForAI(ctx, companyID, projectID)
	if err != nil {
		return err
	}
	priorBatches, err := s.repo.ListBatchesByProject(ctx, companyID, projectID, BatchTypeResourceSuggestions)
	if err != nil {
		return err
	}
	var priorDecisions []PriorResourceDecision
	for _, b := range priorBatches {
		if b.ID == batchID || b.Status != BatchStatusCompleted {
			continue
		}
		priorSugs, err := s.repo.ListSuggestionsByBatch(ctx, companyID, b.ID)
		if err != nil {
			return err
		}
		for _, ps := range priorSugs {
			data := resourceSuggestionDataFor(ps)
			if data == nil {
				continue
			}
			priorDecisions = append(priorDecisions, PriorResourceDecision{
				WorkItemID: data.WorkItemID, ResourceType: resourceTypeString(ps.Type), Name: data.Name,
				Status: ps.Status, SourceBrief: b.SourceBrief, AcceptedDomainObjectID: ps.AcceptedDomainObjectID,
			})
		}
	}
	currentBrief := batchSourceBrief(priorBatches, batchID)

	var fresh []ResourceDeltaItem
	var freshIndex []int
	for i, sug := range suggestions {
		if sug.Status != SuggestionStatusPending {
			continue
		}
		data := resourceSuggestionDataFor(sug)
		if data == nil {
			continue
		}
		fresh = append(fresh, ResourceDeltaItem{WorkItemID: data.WorkItemID, ResourceType: resourceTypeString(sug.Type), Name: data.Name})
		freshIndex = append(freshIndex, i)
	}
	deltaItems := ClassifyResourceDelta(fresh, currentRequirements, priorDecisions, currentBrief)
	for j, item := range deltaItems {
		i := freshIndex[j]
		result[i].Classification = item.Classification
		result[i].CurrentAuthoritativeID = item.CurrentAuthoritativeID
	}
	return nil
}

func resourceSuggestionDataFor(sug AISuggestion) *ResourceSuggestionData {
	switch sug.Type {
	case SuggestionTypeMaterialResource:
		return sug.SuggestedData.MaterialResource
	case SuggestionTypeTradeResource:
		return sug.SuggestedData.TradeResource
	case SuggestionTypeEquipmentResource:
		return sug.SuggestedData.EquipmentResource
	default:
		return nil
	}
}

func resourceTypeString(t SuggestionType) string {
	switch t {
	case SuggestionTypeMaterialResource:
		return "material"
	case SuggestionTypeTradeResource:
		return "trade"
	case SuggestionTypeEquipmentResource:
		return "equipment"
	default:
		return ""
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public Repository interface. Only the real Mongo repository
// implements it; a fake used in unrelated tests simply does not satisfy
// this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every AIGenerationBatch and
// AISuggestion owned by companyID, across both of this module's
// collections. Development-tool use only (demoseed reset, design spec
// §6.6) — no production code path calls this. Idempotent: calling it when
// nothing remains for companyID is a no-op success, not an error.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("ai: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// reconcileExistingBatch implements design doc §18.1's idempotency matrix
// for a batch already found under operationId:
//
//	same operationId + same fingerprint + completed  -> return existing result
//	same operationId + same fingerprint + processing -> ErrGenerationInProgress
//	same operationId + different fingerprint         -> ErrGenerationIdempotencyConflict
//
// ok=false with a nil error means "no reconciliation applies, proceed as a
// fresh generation" — which only happens for a fingerprint match against a
// FAILED batch, letting a failed generation be retried under the same
// operationId once the caller fixes whatever caused the failure.
func (s *Service) reconcileExistingBatch(ctx context.Context, companyID string, existing AIGenerationBatch, inputFingerprint string) (GenerationResult, bool, error) {
	if existing.InputFingerprint != inputFingerprint {
		return GenerationResult{}, false, ErrGenerationIdempotencyConflict
	}
	switch existing.Status {
	case BatchStatusCompleted:
		suggestions, err := s.repo.ListSuggestionsByBatch(ctx, companyID, existing.ID)
		if err != nil {
			return GenerationResult{}, false, err
		}
		return GenerationResult{Batch: existing, Suggestions: suggestions}, true, nil
	case BatchStatusProcessing:
		return GenerationResult{}, false, ErrGenerationInProgress
	default: // failed: allow a fresh attempt
		return GenerationResult{}, false, nil
	}
}

// finalizeBatch persists suggestions and marks the batch completed,
// returning the suggestions with their generated IDs populated so the
// caller's GenerationResult reflects real, queryable IDs rather than the
// pre-insert zero value. If suggestion insert fails, it compensates by
// deleting any partial insert and marking the batch failed, so zero
// partially-usable suggestions are ever reviewable (design doc §17).
func (s *Service) finalizeBatch(ctx context.Context, companyID, batchID string, suggestions []AISuggestion) ([]AISuggestion, error) {
	inserted, err := s.repo.InsertSuggestionsForBatch(ctx, batchID, suggestions)
	if err != nil {
		_ = s.repo.DeleteSuggestionsByBatch(ctx, batchID)
		_ = s.repo.MarkBatchFailed(ctx, companyID, batchID, "PERSISTENCE_FAILED")
		return nil, err
	}
	if err := s.repo.MarkBatchCompleted(ctx, companyID, batchID); err != nil {
		return nil, err
	}
	return inserted, nil
}

func withCompletedStatus(b AIGenerationBatch) AIGenerationBatch {
	b.Status = BatchStatusCompleted
	now := time.Now()
	b.CompletedAt = &now
	return b
}

// SuggestWorkItems runs one Work Item-generation cycle for projectID
// (design doc §10, §16-§18). Requires at least one confirmed Space to
// already exist. AI output never supplies quantity/unit — the platform/ai
// WorkItemSuggestion schema has no such field at all — so no validation for
// it belongs here.
func (s *Service) SuggestWorkItems(ctx context.Context, companyID, projectID, userID, operationID string) (GenerationResult, error) {
	project, err := s.domain.GetProjectAIContext(ctx, companyID, projectID)
	if err != nil {
		if errors.Is(err, ErrGatewayProjectNotFound) {
			return GenerationResult{}, ErrProjectNotFound
		}
		return GenerationResult{}, err
	}
	trimmedBrief := strings.TrimSpace(project.ScopeBrief)
	if trimmedBrief == "" {
		return GenerationResult{}, ErrScopeBriefEmpty
	}

	confirmedSpaces, err := s.domain.ListSpacesForAI(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}
	if len(confirmedSpaces) == 0 {
		return GenerationResult{}, ErrNoConfirmedSpaces
	}

	existingWorkItems, err := s.domain.ListWorkItemsForAI(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}

	spaceRefs := make([]FingerprintSpaceRef, 0, len(confirmedSpaces))
	validSpaceIDs := make(map[string]bool, len(confirmedSpaces))
	spaceContexts := make([]platformai.SpaceContext, 0, len(confirmedSpaces))
	for _, sp := range confirmedSpaces {
		spaceRefs = append(spaceRefs, FingerprintSpaceRef{ID: sp.ID, Name: sp.Name, Type: sp.Type})
		validSpaceIDs[sp.ID] = true
		spaceContexts = append(spaceContexts, platformai.SpaceContext{ID: sp.ID, Name: sp.Name, Type: sp.Type})
	}
	workItemRefs := make([]FingerprintWorkItemRef, 0, len(existingWorkItems))
	existingWorkItemReqs := make([]platformai.ExistingWorkItem, 0, len(existingWorkItems))
	for _, w := range existingWorkItems {
		workItemRefs = append(workItemRefs, FingerprintWorkItemRef{ID: w.ID, SpaceID: w.SpaceID, Description: w.Description, WorkType: w.WorkType})
		existingWorkItemReqs = append(existingWorkItemReqs, platformai.ExistingWorkItem{ID: w.ID, SpaceID: w.SpaceID, Description: w.Description, WorkType: w.WorkType})
	}
	inputFingerprint := WorkFingerprint(WorkFingerprintInput{
		ScopeBrief: trimmedBrief, ConfirmedSpaces: spaceRefs, ExistingWorkItems: workItemRefs,
	})

	if existing, existingErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID); existingErr == nil {
		if result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint); checkErr != nil {
			return GenerationResult{}, checkErr
		} else if ok {
			return result, nil
		}
	}

	batch, err := s.repo.CreateProcessingBatch(ctx, AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID,
		Type: BatchTypeWorkItemSuggestions, Status: BatchStatusProcessing,
		OperationID: operationID, PromptVersion: "work-items-v1",
		SchemaVersion: 1, InputFingerprint: inputFingerprint, SourceBrief: trimmedBrief,
		StartedAt: time.Now(), CreatedByUserID: userID, CreatedAt: time.Now(),
	})
	if err != nil {
		if existing, findErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID); findErr == nil {
			if result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint); checkErr != nil {
				return GenerationResult{}, checkErr
			} else if ok {
				return result, nil
			}
		}
		return GenerationResult{}, err
	}

	resp, err := s.client.SuggestWorkItems(ctx, platformai.WorkItemSuggestionRequest{
		OperationID: operationID, ProjectBrief: trimmedBrief,
		Spaces: spaceContexts, ExistingWorkItems: existingWorkItemReqs,
	})
	if err != nil {
		_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, mapClientErrorCode(err))
		return GenerationResult{}, err
	}

	workMeta := make(map[string]spaceSuggestionMeta, len(resp.Suggestions)) // keyed by description (stable across the gate: gate never rewrites Description)
	items := make([]WorkItemSuggestionData, 0, len(resp.Suggestions))
	for _, sug := range resp.Suggestions {
		if sug.ScopeLevel == "space" {
			if sug.SpaceID == nil || !validSpaceIDs[*sug.SpaceID] {
				_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, "INVALID_PROVIDER_RESPONSE")
				return GenerationResult{}, ErrInvalidProviderResponseFromAI
			}
		} else if sug.SpaceID != nil {
			_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, "INVALID_PROVIDER_RESPONSE")
			return GenerationResult{}, ErrInvalidProviderResponseFromAI
		}
		workMeta[sug.Description] = spaceSuggestionMeta{Confidence: sug.Confidence, Rationale: sug.Rationale}
		items = append(items, WorkItemSuggestionData{
			Description: sug.Description, WorkType: sug.WorkType,
			ScopeLevel: ScopeLevel(sug.ScopeLevel), SpaceID: sug.SpaceID,
			ScopeOrigin: ScopeOrigin(sug.ScopeOrigin), SourceExcerpt: sug.SourceExcerpt,
			MaterialSpecificity: MaterialSpecificity(sug.MaterialSpecificity),
		})
	}

	gateResult := ApplyWorkItemQualityGate(items, trimmedBrief)

	now := time.Now()
	suggestions := make([]AISuggestion, 0, len(gateResult.Items))
	for _, item := range gateResult.Items {
		meta := workMeta[item.Description]
		suggestions = append(suggestions, AISuggestion{
			CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
			Type: SuggestionTypeWorkItem, Status: SuggestionStatusPending,
			Confidence:       meta.Confidence,
			Rationale:        meta.Rationale,
			SuggestedData:    SuggestedData{WorkItem: &item},
			InputFingerprint: inputFingerprint,
			Revision:         1, CreatedAt: now, UpdatedAt: now,
		})
	}

	inserted, err := s.finalizeBatch(ctx, companyID, batch.ID, suggestions)
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Batch: withCompletedStatus(batch), Suggestions: inserted}, nil
}

// SuggestResources runs one Resource-generation cycle for projectID (design
// doc §12, §16-§18). Requires at least one confirmed WorkItem to already
// exist. Every workItemId and non-nil candidateMaterialId in the response
// must belong to the supplied context (design doc §15) — a violation fails
// the batch rather than persisting an unverifiable suggestion.
func (s *Service) SuggestResources(ctx context.Context, companyID, projectID, userID, operationID string) (GenerationResult, error) {
	confirmedWorkItems, err := s.domain.ListWorkItemsForAI(ctx, companyID, projectID)
	if err != nil {
		if errors.Is(err, ErrGatewayProjectNotFound) {
			return GenerationResult{}, ErrProjectNotFound
		}
		return GenerationResult{}, err
	}
	if len(confirmedWorkItems) == 0 {
		return GenerationResult{}, ErrNoConfirmedWorkItems
	}

	existingRequirements, err := s.domain.ListResourceRequirementsForAI(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}
	materialCandidates, err := s.domain.ListMaterialCandidatesForAI(ctx, companyID)
	if err != nil {
		return GenerationResult{}, err
	}

	workItemRefs := make([]FingerprintWorkItemRef, 0, len(confirmedWorkItems))
	validWorkItemIDs := make(map[string]bool, len(confirmedWorkItems))
	workItemContexts := make([]platformai.WorkItemContext, 0, len(confirmedWorkItems))
	for _, w := range confirmedWorkItems {
		workItemRefs = append(workItemRefs, FingerprintWorkItemRef{ID: w.ID, SpaceID: w.SpaceID, Description: w.Description, WorkType: w.WorkType})
		validWorkItemIDs[w.ID] = true
		workItemContexts = append(workItemContexts, platformai.WorkItemContext{ID: w.ID, Description: w.Description})
	}
	requirementRefs := make([]FingerprintRequirementRef, 0, len(existingRequirements))
	existingRequirementReqs := make([]platformai.ExistingRequirement, 0, len(existingRequirements))
	for _, r := range existingRequirements {
		requirementRefs = append(requirementRefs, FingerprintRequirementRef{WorkItemID: r.WorkItemID, ResourceType: r.ResourceType, Name: r.Name})
		existingRequirementReqs = append(existingRequirementReqs, platformai.ExistingRequirement{WorkItemID: r.WorkItemID, ResourceType: r.ResourceType, Name: r.Name})
	}
	materialRefs := make([]FingerprintMaterialRef, 0, len(materialCandidates))
	validMaterialIDs := make(map[string]bool, len(materialCandidates))
	materialCandidateReqs := make([]platformai.MaterialCandidate, 0, len(materialCandidates))
	for _, m := range materialCandidates {
		materialRefs = append(materialRefs, FingerprintMaterialRef{ID: m.ID, Name: m.Name})
		validMaterialIDs[m.ID] = true
		materialCandidateReqs = append(materialCandidateReqs, platformai.MaterialCandidate{ID: m.ID, Name: m.Name})
	}
	inputFingerprint := ResourceFingerprint(ResourceFingerprintInput{
		ConfirmedWorkItems: workItemRefs, ExistingRequirements: requirementRefs, MaterialCandidates: materialRefs,
	})

	if existing, existingErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID); existingErr == nil {
		if result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint); checkErr != nil {
			return GenerationResult{}, checkErr
		} else if ok {
			return result, nil
		}
	}

	batch, err := s.repo.CreateProcessingBatch(ctx, AIGenerationBatch{
		CompanyID: companyID, ProjectID: projectID,
		Type: BatchTypeResourceSuggestions, Status: BatchStatusProcessing,
		OperationID: operationID, PromptVersion: "resources-v1",
		SchemaVersion: 1, InputFingerprint: inputFingerprint,
		StartedAt: time.Now(), CreatedByUserID: userID, CreatedAt: time.Now(),
	})
	descriptionByWorkItemID := make(map[string]string, len(confirmedWorkItems))
	for _, w := range confirmedWorkItems {
		descriptionByWorkItemID[w.ID] = w.Description
	}
	if err != nil {
		if existing, findErr := s.repo.FindBatchByOperationID(ctx, companyID, operationID); findErr == nil {
			if result, ok, checkErr := s.reconcileExistingBatch(ctx, companyID, existing, inputFingerprint); checkErr != nil {
				return GenerationResult{}, checkErr
			} else if ok {
				return result, nil
			}
		}
		return GenerationResult{}, err
	}

	resp, err := s.client.SuggestResources(ctx, platformai.ResourceSuggestionRequest{
		OperationID: operationID, WorkItems: workItemContexts,
		MaterialCandidates: materialCandidateReqs, ExistingRequirements: existingRequirementReqs,
	})
	if err != nil {
		_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, mapClientErrorCode(err))
		return GenerationResult{}, err
	}

	now := time.Now()
	suggestions := make([]AISuggestion, 0, len(resp.Suggestions))
	for _, sug := range resp.Suggestions {
		if !validWorkItemIDs[sug.WorkItemID] {
			_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, "INVALID_PROVIDER_RESPONSE")
			return GenerationResult{}, ErrInvalidProviderResponseFromAI
		}
		if sug.CandidateMaterialID != nil && !validMaterialIDs[*sug.CandidateMaterialID] {
			_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, "INVALID_PROVIDER_RESPONSE")
			return GenerationResult{}, ErrInvalidProviderResponseFromAI
		}

		if sug.ResourceType == "material" && !MaterialSuggestionGrounded(sug.Name, descriptionByWorkItemID[sug.WorkItemID]) {
			// T1.5 §7: a material-specific resource requires the material
			// to actually be grounded in the Work Item's own Description —
			// filtered (not batch-failed), matching "drop the unsupported
			// suggestion" rather than "reject the whole generation."
			continue
		}

		// T1.5 §8: deterministic catalogue matching overrides/validates
		// Python's own candidateMaterialId rather than trusting it outright.
		candidateMaterialID := sug.CandidateMaterialID
		if sug.ResourceType == "material" {
			match := MatchMaterial(sug.Name, "", "", materialCandidates, sug.CandidateMaterialID)
			if match.Tier == TierNone {
				candidateMaterialID = nil
			} else {
				candidateMaterialID = &match.MaterialID
			}
		}

		data := &ResourceSuggestionData{WorkItemID: sug.WorkItemID, Name: sug.Name, CandidateMaterialID: candidateMaterialID}
		suggestion := AISuggestion{
			CompanyID: companyID, ProjectID: projectID, BatchID: batch.ID,
			Status: SuggestionStatusPending, Confidence: sug.Confidence, Rationale: sug.Rationale,
			InputFingerprint: inputFingerprint, Revision: 1, CreatedAt: now, UpdatedAt: now,
		}
		switch sug.ResourceType {
		case "material":
			suggestion.Type = SuggestionTypeMaterialResource
			suggestion.SuggestedData = SuggestedData{MaterialResource: data}
		case "trade":
			suggestion.Type = SuggestionTypeTradeResource
			suggestion.SuggestedData = SuggestedData{TradeResource: data}
		case "equipment":
			suggestion.Type = SuggestionTypeEquipmentResource
			suggestion.SuggestedData = SuggestedData{EquipmentResource: data}
		default:
			_ = s.repo.MarkBatchFailed(ctx, companyID, batch.ID, "INVALID_PROVIDER_RESPONSE")
			return GenerationResult{}, ErrInvalidProviderResponseFromAI
		}
		suggestions = append(suggestions, suggestion)
	}

	inserted, err := s.finalizeBatch(ctx, companyID, batch.ID, suggestions)
	if err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Batch: withCompletedStatus(batch), Suggestions: inserted}, nil
}

// ErrInvalidProviderResponseFromAI is returned when Python's own output
// revalidation somehow let an invalid contextual reference through and Go's
// independent revalidation caught it (design doc §15: "Go validates every
// Space/WorkItem/Material reference returned by Python" — untrusted from
// the business-domain perspective regardless of Python's own checks).
var ErrInvalidProviderResponseFromAI = errors.New("ai: provider response references context outside the supplied set")

// mapClientErrorCode derives a stable internal errorCode string to persist
// on a failed batch from a platform/ai typed sentinel — never the raw error
// text, which could embed transport details.
func mapClientErrorCode(err error) string {
	switch {
	case errors.Is(err, platformai.ErrProviderRateLimited):
		return "PROVIDER_RATE_LIMITED"
	case errors.Is(err, platformai.ErrProviderTimeout):
		return "PROVIDER_TIMEOUT"
	case errors.Is(err, platformai.ErrInvalidProviderResponse):
		return "INVALID_PROVIDER_RESPONSE"
	case errors.Is(err, platformai.ErrInvalidRequest):
		return "INVALID_AI_REQUEST"
	case errors.Is(err, platformai.ErrInvalidServiceResponse):
		return "INVALID_PROVIDER_RESPONSE"
	default:
		return "AI_SERVICE_UNAVAILABLE"
	}
}
