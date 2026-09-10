package aiintegration

import (
	"context"

	"github.com/shananth/renovation-platform/backend/internal/ai"
)

// DomainGatewayAdapter satisfies ai.DomainGateway over an *Adapter, field-
// translating aiintegration's own AI-owned snapshot types
// (ProjectAIContext, SpaceAIContext, ...) into ai's independently-declared
// DomainGatewayProject/DomainGatewaySpace/... types. It exists purely
// because Go interface satisfaction requires exact return types — both
// sides already agree on field shape; this is translation, not a policy
// decision, unlike the acceptance adapters in acceptance_adapter.go.
type DomainGatewayAdapter struct {
	inner *Adapter
}

// NewDomainGatewayAdapter wraps an *Adapter so it satisfies ai.DomainGateway.
func NewDomainGatewayAdapter(inner *Adapter) *DomainGatewayAdapter {
	return &DomainGatewayAdapter{inner: inner}
}

func (g *DomainGatewayAdapter) GetProjectAIContext(ctx context.Context, companyID, projectID string) (ai.DomainGatewayProject, error) {
	p, err := g.inner.GetProjectAIContext(ctx, companyID, projectID)
	if err != nil {
		if err == ErrProjectNotFound {
			return ai.DomainGatewayProject{}, ai.ErrGatewayProjectNotFound
		}
		return ai.DomainGatewayProject{}, err
	}
	return ai.DomainGatewayProject{ID: p.ID, ScopeBrief: p.ScopeBrief}, nil
}

func (g *DomainGatewayAdapter) ListSpacesForAI(ctx context.Context, companyID, projectID string) ([]ai.DomainGatewaySpace, error) {
	list, err := g.inner.ListSpacesForAI(ctx, companyID, projectID)
	if err == ErrProjectNotFound {
		return nil, ai.ErrGatewayProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]ai.DomainGatewaySpace, 0, len(list))
	for _, s := range list {
		result = append(result, ai.DomainGatewaySpace{ID: s.ID, Name: s.Name, Type: s.Type})
	}
	return result, nil
}

func (g *DomainGatewayAdapter) ListWorkItemsForAI(ctx context.Context, companyID, projectID string) ([]ai.DomainGatewayWorkItem, error) {
	list, err := g.inner.ListWorkItemsForAI(ctx, companyID, projectID)
	if err == ErrProjectNotFound {
		return nil, ai.ErrGatewayProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]ai.DomainGatewayWorkItem, 0, len(list))
	for _, w := range list {
		result = append(result, ai.DomainGatewayWorkItem{ID: w.ID, SpaceID: w.SpaceID, Description: w.Description, WorkType: w.WorkType})
	}
	return result, nil
}

func (g *DomainGatewayAdapter) ListMaterialCandidatesForAI(ctx context.Context, companyID string) ([]ai.DomainGatewayMaterial, error) {
	list, err := g.inner.ListMaterialCandidatesForAI(ctx, companyID)
	if err != nil {
		return nil, err
	}
	result := make([]ai.DomainGatewayMaterial, 0, len(list))
	for _, m := range list {
		result = append(result, ai.DomainGatewayMaterial{ID: m.ID, Name: m.Name, Category: m.Category, Unit: m.Unit})
	}
	return result, nil
}

func (g *DomainGatewayAdapter) ListResourceRequirementsForAI(ctx context.Context, companyID, projectID string) ([]ai.DomainGatewayRequirement, error) {
	list, err := g.inner.ListResourceRequirementsForAI(ctx, companyID, projectID)
	if err == ErrProjectNotFound {
		return nil, ai.ErrGatewayProjectNotFound
	}
	if err != nil {
		return nil, err
	}
	result := make([]ai.DomainGatewayRequirement, 0, len(list))
	for _, r := range list {
		result = append(result, ai.DomainGatewayRequirement{ID: r.ID, WorkItemID: r.WorkItemID, ResourceType: r.ResourceType, Name: r.Name})
	}
	return result, nil
}
