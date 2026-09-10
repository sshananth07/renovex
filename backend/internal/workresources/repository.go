package workresources

import (
	"context"
	"errors"
)

// ErrRequirementNotFound is returned when a WorkResourceRequirement lookup
// finds no match — including one that exists but belongs to a different
// company.
var ErrRequirementNotFound = errors.New("workresources: requirement not found")

// DuplicateKey identifies the deterministic dedupe key for a requirement
// (design doc §20.3):
//
//	material:          companyId + workItemId + resourceType + materialId
//	trade/equipment:    companyId + workItemId + resourceType + normalizedName
//
// Exactly one of MaterialID or NormalizedName is set, matching ResourceType.
type DuplicateKey struct {
	CompanyID      string
	WorkItemID     string
	ResourceType   ResourceType
	MaterialID     string // set only when ResourceType == material
	NormalizedName string // set only when ResourceType != material
}

// WorkResourceRequirementRepository persists WorkResourceRequirements.
// workresources owns the work_resource_requirements collection exclusively;
// no other module may query it directly.
type WorkResourceRequirementRepository interface {
	Create(ctx context.Context, r WorkResourceRequirement) (WorkResourceRequirement, error)
	FindByID(ctx context.Context, companyID, id string) (WorkResourceRequirement, error)
	// FindBySourceSuggestionID is the acceptance idempotency anchor: it looks
	// up a requirement by its AI-suggestion provenance, tenant-scoped, so a
	// retried acceptance can find and reuse an already-created requirement
	// rather than creating a duplicate (design doc §18.2).
	FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (WorkResourceRequirement, error)
	// FindByDuplicateKey looks up an existing requirement matching key,
	// tenant-scoped. Used to detect a same-resource duplicate from a
	// DIFFERENT AI suggestion (design doc §20.3) before create — distinct
	// from FindBySourceSuggestionID, which detects a retry of the SAME
	// suggestion.
	FindByDuplicateKey(ctx context.Context, key DuplicateKey) (WorkResourceRequirement, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]WorkResourceRequirement, error)
	ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]WorkResourceRequirement, error)
}
