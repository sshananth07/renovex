// Package workresources owns WorkResourceRequirement — a lightweight
// planning-domain record meaning only "this Work Item is expected to
// require this resource" (M8.5B-A design doc §11). It carries no
// authoritative cost/price/rate/quantity, selects no worker/supplier, and
// creates no CostItem or procurement demand; those remain
// materialrequirements/costs/labour's job, downstream of a contractor's
// explicit deterministic action.
//
// workresources never imports projects, work, or materials. It defines
// ProjectLookup, WorkItemLookup, and MaterialLookup, the capabilities it
// needs from those modules, satisfied structurally with no import in
// either direction (ADR 0002).
package workresources

import (
	"errors"
	"time"
)

// ResourceType is one of the three resource kinds M8.5B-A supports.
type ResourceType string

const (
	ResourceTypeMaterial  ResourceType = "material"
	ResourceTypeTrade     ResourceType = "trade"
	ResourceTypeEquipment ResourceType = "equipment"
)

// IsValid reports whether t is one of the three defined resource types.
func (t ResourceType) IsValid() bool {
	switch t {
	case ResourceTypeMaterial, ResourceTypeTrade, ResourceTypeEquipment:
		return true
	default:
		return false
	}
}

// Status is the requirement's lifecycle state. M8.5B-A defines only
// "confirmed" — there is no draft/review state in this milestone.
type Status string

const (
	StatusConfirmed Status = "confirmed"
)

// IsValid reports whether s is the one defined status.
func (s Status) IsValid() bool {
	return s == StatusConfirmed
}

// Source distinguishes manually-created requirements from AI-suggested
// ones. SourceManual is valid at the model level even though M8.5B-A ships
// no manual-create public endpoint, so a later manual-management UI can
// reuse this entity without redesigning it (design doc §30).
type Source string

const (
	SourceManual       Source = "manual"
	SourceAISuggestion Source = "ai_suggestion"
)

// IsValid reports whether s is one of the two defined sources.
func (s Source) IsValid() bool {
	switch s {
	case SourceManual, SourceAISuggestion:
		return true
	default:
		return false
	}
}

// WorkResourceRequirement is the lightweight Work -> Resource planning
// bridge (design doc §11). MaterialID is set only for resourceType=material
// and must be nil for trade/equipment. SourceSuggestionID is required when
// Source=ai_suggestion (the acceptance idempotency anchor, design doc
// §18.2) and is a nullable internal-only provenance field otherwise —
// never a public Huma request field.
type WorkResourceRequirement struct {
	ID         string
	CompanyID  string
	ProjectID  string
	WorkItemID string

	ResourceType ResourceType
	MaterialID   *string
	Name         string

	Status Status
	Source Source

	SourceSuggestionID *string

	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

var (
	ErrCompanyIDRequired          = errors.New("workresources: companyId is required")
	ErrProjectIDRequired          = errors.New("workresources: projectId is required")
	ErrWorkItemIDRequired         = errors.New("workresources: workItemId is required")
	ErrNameRequired               = errors.New("workresources: name is required")
	ErrInvalidResourceType        = errors.New("workresources: invalid resource type")
	ErrMaterialIDRequired         = errors.New("workresources: materialId is required for resourceType=material")
	ErrMaterialIDMustBeNil        = errors.New("workresources: materialId must be nil for trade/equipment")
	ErrInvalidStatus              = errors.New("workresources: invalid status")
	ErrInvalidSource              = errors.New("workresources: invalid source")
	ErrSourceSuggestionIDRequired = errors.New("workresources: sourceSuggestionId is required when source=ai_suggestion")
)

// Validate checks every field-level invariant described in the design doc.
// It does not check foreign-key existence (Project/WorkItem/Material
// belonging to the caller's company) — that is the Service's job, since it
// requires the consumer-defined lookup capabilities.
func (r WorkResourceRequirement) Validate() error {
	if r.CompanyID == "" {
		return ErrCompanyIDRequired
	}
	if r.ProjectID == "" {
		return ErrProjectIDRequired
	}
	if r.WorkItemID == "" {
		return ErrWorkItemIDRequired
	}
	if r.Name == "" {
		return ErrNameRequired
	}
	if !r.ResourceType.IsValid() {
		return ErrInvalidResourceType
	}
	if r.ResourceType == ResourceTypeMaterial {
		if r.MaterialID == nil {
			return ErrMaterialIDRequired
		}
	} else if r.MaterialID != nil {
		return ErrMaterialIDMustBeNil
	}
	if !r.Status.IsValid() {
		return ErrInvalidStatus
	}
	if !r.Source.IsValid() {
		return ErrInvalidSource
	}
	if r.Source == SourceAISuggestion && r.SourceSuggestionID == nil {
		return ErrSourceSuggestionIDRequired
	}
	return nil
}
