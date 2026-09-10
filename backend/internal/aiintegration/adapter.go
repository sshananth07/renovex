// Package aiintegration is the composition adapter between internal/ai and
// existing Renovex domain modules (M8.5B-A design doc, Task 8 rationale).
// internal/ai needs structured context from Projects/Spaces/Work/Materials/
// WorkResources; those modules must not gain a reverse dependency on
// internal/ai (ADR 0002). This adapter calls existing SERVICES, never
// repositories, and translates their real return types into AI-owned
// primitive-only snapshot types — mirroring the composition.*Adapter
// pattern used elsewhere in this codebase (e.g.
// platform/composition/m7adapters.go) but scoped as its own package because
// internal/ai, not platform/composition, is the consumer.
package aiintegration

import (
	"context"
	"errors"
	"sort"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
	"github.com/shananth/renovation-platform/backend/internal/workresources"
)

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company — a distinct sentinel value in this package.
var ErrProjectNotFound = errors.New("aiintegration: project not found")

// maxMaterialCandidates bounds the Material catalog context sent to Python
// (design doc §8, "at most 100 catalog records" — no embeddings/vector
// DB/RAG in M8.5B-A).
const maxMaterialCandidates = 100

// defaultListPageSize is large enough to return "all" Spaces/WorkItems for
// a Project in one page for AI context purposes, since Projects in this
// milestone's scale never approach pagination's real page-size ceiling.
const defaultListPageSize = 500

// ProjectAIContext is the AI-owned snapshot of a Project's authoritative
// context. It carries only what M8.5B-A generation is allowed to see —
// never a projects.Project struct crossing the module boundary (ADR 0002).
type ProjectAIContext struct {
	ID         string
	ScopeBrief string
}

// SpaceAIContext is the AI-owned snapshot of one Space.
type SpaceAIContext struct {
	ID   string
	Name string
	Type string
}

// WorkItemAIContext is the AI-owned snapshot of one WorkItem. It carries no
// quantity/cost field — those are never part of AI input context in
// M8.5B-A.
type WorkItemAIContext struct {
	ID          string
	SpaceID     string
	Description string
	WorkType    string
}

// MaterialCandidateAIContext is the AI-owned snapshot of one Material
// catalog entry offered as an advisory candidate. Category/Unit are carried
// so Go's deterministic catalogue matcher (internal/ai/materialmatch.go)
// can use them as compatibility co-signals — they are never sent to the
// Python AI service itself (see platformai.MaterialCandidate, which stays
// ID+Name only).
type MaterialCandidateAIContext struct {
	ID       string
	Name     string
	Category string
	Unit     string
}

// ResourceRequirementAIContext is the AI-owned snapshot of one existing
// WorkResourceRequirement, used only for duplicate-avoidance context.
type ResourceRequirementAIContext struct {
	ID           string
	WorkItemID   string
	ResourceType string
	Name         string
}

// projectsService is the capability this adapter needs from projects.
type projectsService interface {
	GetProject(ctx context.Context, companyID, projectID string) (projects.Project, error)
}

// spacesService is the capability this adapter needs from spaces.
type spacesService interface {
	ListSpacesPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]spaces.Space, int, error)
}

// workService is the capability this adapter needs from work.
type workService interface {
	ListWorkItemsPaginatedByProject(ctx context.Context, companyID, projectID string, req pagination.Request) ([]work.WorkItem, int, error)
}

// materialsService is the capability this adapter needs from materials.
type materialsService interface {
	ListMaterials(ctx context.Context, companyID string) ([]materials.Material, error)
}

// workResourcesService is the capability this adapter needs from
// workresources.
type workResourcesService interface {
	ListByProject(ctx context.Context, companyID, projectID string) ([]workresources.WorkResourceRequirement, error)
}

// Adapter reads authorized context from existing domain services and
// translates it into AI-owned snapshot types.
type Adapter struct {
	projects      projectsService
	spaces        spacesService
	work          workService
	materials     materialsService
	workResources workResourcesService
}

// NewAdapter constructs an Adapter over the given domain services.
func NewAdapter(projects projectsService, spaces spacesService, work workService, materials materialsService, workResources workResourcesService) *Adapter {
	return &Adapter{projects: projects, spaces: spaces, work: work, materials: materials, workResources: workResources}
}

func aiListRequest() pagination.Request {
	return pagination.Request{Page: 1, PageSize: defaultListPageSize, Sort: "createdAt", Order: pagination.OrderAsc}
}

// GetProjectAIContext returns projectID's tenant-scoped ScopeBrief.
func (a *Adapter) GetProjectAIContext(ctx context.Context, companyID, projectID string) (ProjectAIContext, error) {
	p, err := a.projects.GetProject(ctx, companyID, projectID)
	if errors.Is(err, projects.ErrProjectNotFound) {
		return ProjectAIContext{}, ErrProjectNotFound
	}
	if err != nil {
		return ProjectAIContext{}, err
	}
	return ProjectAIContext{ID: p.ID, ScopeBrief: p.ScopeBrief}, nil
}

// ListSpacesForAI returns the current Spaces belonging to projectID, tenant-
// scoped to companyID.
func (a *Adapter) ListSpacesForAI(ctx context.Context, companyID, projectID string) ([]SpaceAIContext, error) {
	list, _, err := a.spaces.ListSpacesPaginated(ctx, companyID, projectID, aiListRequest())
	if errors.Is(err, spaces.ErrProjectNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]SpaceAIContext, 0, len(list))
	for _, s := range list {
		result = append(result, SpaceAIContext{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return result, nil
}

// ListWorkItemsForAI returns the current WorkItems belonging to projectID,
// tenant-scoped to companyID.
func (a *Adapter) ListWorkItemsForAI(ctx context.Context, companyID, projectID string) ([]WorkItemAIContext, error) {
	list, _, err := a.work.ListWorkItemsPaginatedByProject(ctx, companyID, projectID, aiListRequest())
	if errors.Is(err, work.ErrProjectNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]WorkItemAIContext, 0, len(list))
	for _, w := range list {
		ctx := WorkItemAIContext{ID: w.ID, Description: w.Description, WorkType: w.WorkType}
		if w.SpaceID != nil {
			ctx.SpaceID = *w.SpaceID
		}
		result = append(result, ctx)
	}
	return result, nil
}

// ListMaterialCandidatesForAI returns a deterministic, bounded set of the
// caller company's Material catalog — stable-sorted by normalized name then
// ID, capped at maxMaterialCandidates (design doc §8: no embeddings/vector
// DB/RAG in M8.5B-A).
func (a *Adapter) ListMaterialCandidatesForAI(ctx context.Context, companyID string) ([]MaterialCandidateAIContext, error) {
	list, err := a.materials.ListMaterials(ctx, companyID)
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID < list[j].ID
	})
	if len(list) > maxMaterialCandidates {
		list = list[:maxMaterialCandidates]
	}
	result := make([]MaterialCandidateAIContext, 0, len(list))
	for _, m := range list {
		result = append(result, MaterialCandidateAIContext{ID: m.ID, Name: m.Name, Category: m.Category, Unit: m.Unit})
	}
	return result, nil
}

// ListResourceRequirementsForAI returns the current WorkResourceRequirements
// belonging to projectID, tenant-scoped to companyID — used only for
// duplicate-avoidance context in Resource generation.
func (a *Adapter) ListResourceRequirementsForAI(ctx context.Context, companyID, projectID string) ([]ResourceRequirementAIContext, error) {
	list, err := a.workResources.ListByProject(ctx, companyID, projectID)
	if errors.Is(err, workresources.ErrProjectNotFound) {
		return nil, ErrProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]ResourceRequirementAIContext, 0, len(list))
	for _, r := range list {
		result = append(result, ResourceRequirementAIContext{
			ID: r.ID, WorkItemID: r.WorkItemID, ResourceType: string(r.ResourceType), Name: r.Name,
		})
	}
	return result, nil
}
