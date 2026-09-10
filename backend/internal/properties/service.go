package properties

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrAddressRequired is returned when CreateProperty is given an empty address.
var ErrAddressRequired = errors.New("properties: address is required")

// ErrProjectNotFound is returned when the given projectID does not belong to the
// caller's company (per ProjectLookup) — a distinct sentinel value in this package,
// not an import of projects.ErrProjectNotFound (properties never imports projects'
// types, per ADR 0002).
var ErrProjectNotFound = errors.New("properties: project not found")

// ProjectLookup is the capability properties needs from projects: confirming a
// projectID belongs to the caller's company before creating or listing by it.
// Defined here (consumer-defines-interface); satisfied structurally by
// projects.Service with no import in either direction.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// Service implements Property CRUD and project-parent validation. It exposes no
// capability to any other module — nothing in M2 needs to look up a Property from
// another module (design spec §5.3, §8).
type Service struct {
	repo          PropertyRepository
	projectLookup ProjectLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup to
// validate parent Project references.
func NewService(repo PropertyRepository, projectLookup ProjectLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public PropertyRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Property owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("properties: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateProperty validates projectID belongs to companyID, validates address is
// non-empty, and persists a new Property. If projectID already has a Property, the
// repository's unique-index violation surfaces as ErrProjectAlreadyHasProperty
// (mapped to 409 at the handler).
func (s *Service) CreateProperty(ctx context.Context, companyID, projectID, address, propertyType, notes string) (Property, error) {
	if address == "" {
		return Property{}, ErrAddressRequired
	}
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Property{}, err
	}
	if !belongs {
		return Property{}, ErrProjectNotFound
	}
	return s.repo.Create(ctx, Property{
		CompanyID: companyID, ProjectID: projectID, Address: address,
		PropertyType: propertyType, Notes: notes, CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetProperty returns propertyID's Property, tenant-scoped to companyID.
func (s *Service) GetProperty(ctx context.Context, companyID, propertyID string) (Property, error) {
	return s.repo.FindByID(ctx, companyID, propertyID)
}

// ListPropertiesByProject validates projectID belongs to companyID before listing —
// a foreign projectID returns ErrProjectNotFound, never an empty list (design spec
// §10.4). Returns 0 or 1 Property by construction of the unique index.
func (s *Service) ListPropertiesByProject(ctx context.Context, companyID, projectID string) ([]Property, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// UpdateProperty updates propertyID's fields, tenant-scoped to companyID.
func (s *Service) UpdateProperty(ctx context.Context, companyID, propertyID, address, propertyType, notes string) (Property, error) {
	if address == "" {
		return Property{}, ErrAddressRequired
	}
	return s.repo.Update(ctx, companyID, propertyID, func(p *Property) {
		p.Address = address
		p.PropertyType = propertyType
		p.Notes = notes
	})
}
