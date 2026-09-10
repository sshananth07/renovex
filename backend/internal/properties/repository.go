package properties

import (
	"context"
	"errors"
)

// ErrPropertyNotFound is returned when a Property lookup finds no match —
// including a Property that exists but belongs to a different company.
var ErrPropertyNotFound = errors.New("properties: property not found")

// ErrProjectAlreadyHasProperty is returned by Create when projectID already has a
// Property (enforced by the unique {companyId, projectId} index).
var ErrProjectAlreadyHasProperty = errors.New("properties: project already has a property")

// PropertyRepository persists Properties. properties owns the properties
// collection exclusively; no other module may query it directly.
type PropertyRepository interface {
	Create(ctx context.Context, p Property) (Property, error)
	FindByID(ctx context.Context, companyID, id string) (Property, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]Property, error)
	Update(ctx context.Context, companyID, id string, fn func(*Property)) (Property, error)
}
