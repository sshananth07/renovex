package materials

import (
	"context"
	"errors"
)

// ErrMaterialNotFound is returned when a Material lookup finds no match —
// including a Material that exists but belongs to a different company.
var ErrMaterialNotFound = errors.New("materials: material not found")

// MaterialRepository persists Materials. materials owns the materials
// collection exclusively; no other module may query it directly.
type MaterialRepository interface {
	Create(ctx context.Context, m Material) (Material, error)
	FindByID(ctx context.Context, companyID, id string) (Material, error)
	List(ctx context.Context, companyID string) ([]Material, error)
	Update(ctx context.Context, companyID, id string, fn func(*Material)) (Material, error)
	// FindBySourceSuggestionID is the acceptance idempotency anchor: it looks
	// up a Material by its AI-suggestion provenance, tenant-scoped, so a
	// retried Create & Add acceptance can find and reuse an already-created
	// Material rather than creating a duplicate (M8.5B-A design doc §18.2).
	FindBySourceSuggestionID(ctx context.Context, companyID, sourceSuggestionID string) (Material, error)
}
