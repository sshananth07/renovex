package aiintegration

import (
	"context"
	"strings"

	"github.com/shananth/renovation-platform/backend/internal/ai"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

// This file satisfies internal/ai's SpaceCreator, WorkItemCreator, and
// ResourceCreator capability interfaces (Task 10) over the real
// spaces/work/materials/workresources services — the acceptance-side
// counterpart to adapter.go's read-only DomainGateway. It exists because Go
// interface satisfaction requires exact return types: ai.Service declares
// its own AI-owned result structs (ai.SpaceAcceptanceResult etc.), while the
// real services return their own domain structs. Without this shim,
// internal/ai would have to import spaces/work/materials/workresources
// directly (ADR 0002 forbids that one level up, the same way
// platform/composition's M7/M8 adapters exist for the same reason).

func normalizeText(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// --- spaces.Service -> ai.SpaceCreator ---

type spacesAcceptanceService interface {
	CreateSpaceFromAISuggestion(ctx context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (spaces.Space, error)
	FindSpaceBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (spaces.Space, error)
	ListSpacesPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]spaces.Space, int, error)
	UpdateSpace(ctx context.Context, companyID, spaceID, name, spaceType, description string) (spaces.Space, error)
}

// SpaceAcceptanceAdapter satisfies ai.SpaceCreator over spaces.Service.
type SpaceAcceptanceAdapter struct {
	spaces spacesAcceptanceService
}

// NewSpaceAcceptanceAdapter wraps a spaces.Service (or any equivalent).
func NewSpaceAcceptanceAdapter(spaces spacesAcceptanceService) *SpaceAcceptanceAdapter {
	return &SpaceAcceptanceAdapter{spaces: spaces}
}

func (a *SpaceAcceptanceAdapter) CreateSpaceFromAISuggestion(ctx context.Context, companyID, projectID, name, spaceType, description, sourceSuggestionID string) (ai.SpaceAcceptanceResult, error) {
	s, err := a.spaces.CreateSpaceFromAISuggestion(ctx, companyID, projectID, name, spaceType, description, sourceSuggestionID)
	if err != nil {
		return ai.SpaceAcceptanceResult{}, err
	}
	return ai.SpaceAcceptanceResult{ID: s.ID, Name: s.Name, SpaceType: s.Type}, nil
}

// UpdateSpaceFromAISuggestion mutates an existing authoritative Space
// in place (T1.5 PART C "Use suggestion" for a CHANGED delta item) — routes
// through the exact same spaces.Service.UpdateSpace real contractors use,
// never creating a duplicate.
func (a *SpaceAcceptanceAdapter) UpdateSpaceFromAISuggestion(ctx context.Context, companyID, spaceID, name, spaceType, description string) (ai.SpaceAcceptanceResult, error) {
	s, err := a.spaces.UpdateSpace(ctx, companyID, spaceID, name, spaceType, description)
	if err != nil {
		return ai.SpaceAcceptanceResult{}, err
	}
	return ai.SpaceAcceptanceResult{ID: s.ID, Name: s.Name, SpaceType: s.Type}, nil
}

func (a *SpaceAcceptanceAdapter) FindSpaceBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (ai.SpaceAcceptanceResult, error) {
	s, err := a.spaces.FindSpaceBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
	if err != nil {
		return ai.SpaceAcceptanceResult{}, err
	}
	return ai.SpaceAcceptanceResult{ID: s.ID, Name: s.Name, SpaceType: s.Type}, nil
}

// SpaceLikelyDuplicate reports whether projectID already has a current Space
// with a normalized name+type match (design doc §20.1). Uses the same
// bounded listing ai.Adapter uses for generation context, so no separate
// unbounded query path exists.
func (a *SpaceAcceptanceAdapter) SpaceLikelyDuplicate(ctx context.Context, companyID, projectID, name, spaceType string) (bool, error) {
	list, _, err := a.spaces.ListSpacesPaginated(ctx, companyID, projectID, aiListRequest())
	if err != nil {
		return false, err
	}
	target := normalizeText(name) + "|" + normalizeText(spaceType)
	for _, s := range list {
		if normalizeText(s.Name)+"|"+normalizeText(s.Type) == target {
			return true, nil
		}
	}
	return false, nil
}

// --- work.Service -> ai.WorkItemCreator ---

type workAcceptanceService interface {
	CreateWorkItemFromAISuggestion(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (work.WorkItem, error)
	FindWorkItemBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (work.WorkItem, error)
	ListWorkItemsPaginatedByProject(ctx context.Context, companyID, projectID string, req pagination.Request) ([]work.WorkItem, int, error)
	UpdateWorkItem(ctx context.Context, companyID, workItemID string, patch work.WorkItemPatch) (work.WorkItem, error)
}

// WorkItemAcceptanceAdapter satisfies ai.WorkItemCreator over work.Service.
type WorkItemAcceptanceAdapter struct {
	work workAcceptanceService
}

// NewWorkItemAcceptanceAdapter wraps a work.Service (or any equivalent).
func NewWorkItemAcceptanceAdapter(work workAcceptanceService) *WorkItemAcceptanceAdapter {
	return &WorkItemAcceptanceAdapter{work: work}
}

func (a *WorkItemAcceptanceAdapter) CreateWorkItemFromAISuggestion(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit, sourceSuggestionID string) (ai.WorkItemAcceptanceResult, error) {
	w, err := a.work.CreateWorkItemFromAISuggestion(ctx, companyID, projectID, spaceID, description, workType, quantityValue, unit, sourceSuggestionID)
	if err != nil {
		return ai.WorkItemAcceptanceResult{}, err
	}
	return ai.WorkItemAcceptanceResult{ID: w.ID, Description: w.Description}, nil
}

// UpdateWorkItemFromAISuggestion mutates an existing authoritative Work
// Item in place (T1.5B "Use suggestion" for a CHANGED delta item) — routes
// through the exact same work.Service.UpdateWorkItem real contractors use,
// touching only Description/WorkType (quantity/unit/space stay untouched —
// they are never part of a Work Item suggestion's own data, design doc
// §10.6).
func (a *WorkItemAcceptanceAdapter) UpdateWorkItemFromAISuggestion(ctx context.Context, companyID, workItemID, description, workType string) (ai.WorkItemAcceptanceResult, error) {
	w, err := a.work.UpdateWorkItem(ctx, companyID, workItemID, work.WorkItemPatch{
		Description: &description,
		WorkType:    &workType,
	})
	if err != nil {
		return ai.WorkItemAcceptanceResult{}, err
	}
	return ai.WorkItemAcceptanceResult{ID: w.ID, Description: w.Description}, nil
}

func (a *WorkItemAcceptanceAdapter) FindWorkItemBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (ai.WorkItemAcceptanceResult, error) {
	w, err := a.work.FindWorkItemBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
	if err != nil {
		return ai.WorkItemAcceptanceResult{}, err
	}
	return ai.WorkItemAcceptanceResult{ID: w.ID, Description: w.Description}, nil
}

// WorkItemLikelyDuplicate reports whether projectID already has a current
// WorkItem with a normalized description match, optionally scoped to
// spaceID (design doc §20.2, advisory only — contractor may override with
// AllowDuplicate).
func (a *WorkItemAcceptanceAdapter) WorkItemLikelyDuplicate(ctx context.Context, companyID, projectID string, spaceID *string, description string) (bool, error) {
	list, _, err := a.work.ListWorkItemsPaginatedByProject(ctx, companyID, projectID, aiListRequest())
	if err != nil {
		return false, err
	}
	target := normalizeText(description)
	for _, w := range list {
		if normalizeText(w.Description) != target {
			continue
		}
		if spaceID == nil && w.SpaceID == nil {
			return true, nil
		}
		if spaceID != nil && w.SpaceID != nil && *spaceID == *w.SpaceID {
			return true, nil
		}
	}
	return false, nil
}

// --- workresources.Service + materials.Service -> ai.ResourceCreator ---

type workResourcesAcceptanceService interface {
	CreateFromAISuggestion(ctx context.Context, companyID, projectID, workItemID string, resourceType workresources.ResourceType, materialID *string, name, sourceSuggestionID string) (workresources.WorkResourceRequirement, error)
	FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (workresources.WorkResourceRequirement, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]workresources.WorkResourceRequirement, error)
}

type materialsAcceptanceService interface {
	MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
	CreateMaterialFromAISuggestion(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (materials.Material, error)
	FindMaterialBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (materials.Material, error)
}

// ResourceAcceptanceAdapter satisfies ai.ResourceCreator over
// workresources.Service and materials.Service together. It is the one
// adapter in this file spanning two services because ai.ResourceCreator's
// single interface intentionally covers both the requirement and the
// Create & Add Material saga (design doc §12.5) as one governed operation.
type ResourceAcceptanceAdapter struct {
	resources workResourcesAcceptanceService
	materials materialsAcceptanceService
}

// NewResourceAcceptanceAdapter wraps a workresources.Service and a
// materials.Service (or equivalents).
func NewResourceAcceptanceAdapter(resources workResourcesAcceptanceService, materials materialsAcceptanceService) *ResourceAcceptanceAdapter {
	return &ResourceAcceptanceAdapter{resources: resources, materials: materials}
}

func toWorkresourcesType(t ai.ResourceRequirementType) workresources.ResourceType {
	switch t {
	case ai.ResourceRequirementTypeMaterial:
		return workresources.ResourceTypeMaterial
	case ai.ResourceRequirementTypeTrade:
		return workresources.ResourceTypeTrade
	case ai.ResourceRequirementTypeEquipment:
		return workresources.ResourceTypeEquipment
	default:
		return workresources.ResourceType(t)
	}
}

func (a *ResourceAcceptanceAdapter) CreateResourceRequirementFromAISuggestion(ctx context.Context, companyID, projectID, workItemID string, resourceType ai.ResourceRequirementType, materialID *string, name, sourceSuggestionID string) (ai.ResourceAcceptanceResult, error) {
	r, err := a.resources.CreateFromAISuggestion(ctx, companyID, projectID, workItemID, toWorkresourcesType(resourceType), materialID, name, sourceSuggestionID)
	if err != nil {
		return ai.ResourceAcceptanceResult{}, err
	}
	result := ai.ResourceAcceptanceResult{RequirementID: r.ID, Name: r.Name}
	if r.MaterialID != nil {
		result.MaterialID = *r.MaterialID
	}
	return result, nil
}

func (a *ResourceAcceptanceAdapter) FindResourceRequirementBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (ai.ResourceAcceptanceResult, error) {
	r, err := a.resources.FindBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
	if err != nil {
		return ai.ResourceAcceptanceResult{}, err
	}
	result := ai.ResourceAcceptanceResult{RequirementID: r.ID, Name: r.Name}
	if r.MaterialID != nil {
		result.MaterialID = *r.MaterialID
	}
	return result, nil
}

// ResourceRequirementLikelyDuplicate mirrors the deterministic dedupe key
// design doc §20.3 defines: same WorkItem + same resourceType + same
// materialId (material), or same WorkItem + same resourceType + normalized
// name (trade/equipment).
func (a *ResourceAcceptanceAdapter) ResourceRequirementLikelyDuplicate(ctx context.Context, companyID, projectID, workItemID string, resourceType ai.ResourceRequirementType, materialID *string, name string) (bool, error) {
	list, err := a.resources.ListByProject(ctx, companyID, projectID)
	if err != nil {
		return false, err
	}
	want := toWorkresourcesType(resourceType)
	for _, r := range list {
		if r.CompanyID != companyID || r.WorkItemID != workItemID || r.ResourceType != want {
			continue
		}
		if want == workresources.ResourceTypeMaterial {
			if materialID != nil && r.MaterialID != nil && *materialID == *r.MaterialID {
				return true, nil
			}
			continue
		}
		if normalizeText(r.Name) == normalizeText(name) {
			return true, nil
		}
	}
	return false, nil
}

func (a *ResourceAcceptanceAdapter) MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error) {
	return a.materials.MaterialBelongsToCompany(ctx, companyID, materialID)
}

func (a *ResourceAcceptanceAdapter) CreateMaterialFromAISuggestion(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency, sourceSuggestionID string) (ai.MaterialAcceptanceResult, error) {
	m, err := a.materials.CreateMaterialFromAISuggestion(ctx, companyID, name, category, specification, unit, referencePriceAmount, currency, sourceSuggestionID)
	if err != nil {
		return ai.MaterialAcceptanceResult{}, err
	}
	return ai.MaterialAcceptanceResult{ID: m.ID, Name: m.Name}, nil
}

func (a *ResourceAcceptanceAdapter) FindMaterialBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (ai.MaterialAcceptanceResult, error) {
	m, err := a.materials.FindMaterialBySourceSuggestionID(ctx, companyID, sourceSuggestionID)
	if err != nil {
		return ai.MaterialAcceptanceResult{}, err
	}
	return ai.MaterialAcceptanceResult{ID: m.ID, Name: m.Name}, nil
}
