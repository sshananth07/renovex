package demoseed

import (
	"context"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
)

type ClientCreator interface {
	CreateClient(ctx context.Context, companyID, name, phone, email, address, billingAddress, notes string) (clients.Client, error)
	ListClientsPaginated(ctx context.Context, companyID string, req pagination.Request) ([]clients.Client, int, error)
}

type ProjectCreator interface {
	CreateProject(ctx context.Context, companyID, clientID, name string) (projects.Project, error)
	ListProjectsPaginated(ctx context.Context, companyID, clientID string, req pagination.Request) ([]projects.Project, int, error)
	UpdateProjectStatus(ctx context.Context, companyID, projectID string, status projects.ProjectStatus) (projects.Project, error)
	GetProject(ctx context.Context, companyID, projectID string) (projects.Project, error)
}

type PropertyCreator interface {
	CreateProperty(ctx context.Context, companyID, projectID, address, propertyType, notes string) (properties.Property, error)
	ListPropertiesByProject(ctx context.Context, companyID, projectID string) ([]properties.Property, error)
}

type SpaceCreator interface {
	CreateSpace(ctx context.Context, companyID, projectID, name, spaceType, description string) (spaces.Space, error)
	ListSpacesPaginated(ctx context.Context, companyID, projectID string, req pagination.Request) ([]spaces.Space, int, error)
}

type WorkItemCreator interface {
	CreateWorkItem(ctx context.Context, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit string) (work.WorkItem, error)
	ListWorkItemsPaginatedByProject(ctx context.Context, companyID, projectID string, req pagination.Request) ([]work.WorkItem, int, error)
}

// listRequest builds a safe pagination.Request for demoseed's own listing
// needs (a large single page, no user-facing search/sort). A bare
// pagination.Request{Page, PageSize} literal produces an empty $sort field
// path and fails against real MongoDB — every List call in this package
// goes through ParseRequest with the module's own sort allowlist instead.
func listRequest(pageSize int, allowedSorts []string, defaultSort string, defaultOrder pagination.Order) (pagination.Request, error) {
	return pagination.ParseRequest(1, pageSize, "", "", "", allowedSorts, defaultSort, defaultOrder)
}

func ensureClient(ctx context.Context, svc ClientCreator, companyID, name, phone, email, address, notes string) (clients.Client, error) {
	req, err := listRequest(100, clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		return clients.Client{}, err
	}
	existing, _, err := svc.ListClientsPaginated(ctx, companyID, req)
	if err != nil {
		return clients.Client{}, err
	}
	if found, ok := FindByName(existing, name, func(c clients.Client) string { return c.Name }); ok {
		return found, nil
	}
	return svc.CreateClient(ctx, companyID, name, phone, email, address, address, notes)
}

func ensureProject(ctx context.Context, svc ProjectCreator, companyID, clientID, name string) (projects.Project, error) {
	req, err := listRequest(100, projects.ProjectSortFields, projects.ProjectDefaultSort, projects.ProjectDefaultOrder)
	if err != nil {
		return projects.Project{}, err
	}
	existing, _, err := svc.ListProjectsPaginated(ctx, companyID, "", req)
	if err != nil {
		return projects.Project{}, err
	}
	if found, ok := FindByName(existing, name, func(p projects.Project) string { return p.Name }); ok {
		return found, nil
	}
	return svc.CreateProject(ctx, companyID, clientID, name)
}

func ensureProperty(ctx context.Context, svc PropertyCreator, companyID, projectID, address, propertyType, notes string) (properties.Property, error) {
	existing, err := svc.ListPropertiesByProject(ctx, companyID, projectID)
	if err != nil {
		return properties.Property{}, err
	}
	if len(existing) > 0 {
		return existing[0], nil
	}
	return svc.CreateProperty(ctx, companyID, projectID, address, propertyType, notes)
}

func ensureSpace(ctx context.Context, svc SpaceCreator, companyID, projectID, name, spaceType, description string) (spaces.Space, error) {
	req, err := listRequest(100, spaces.SpaceSortFields, spaces.SpaceDefaultSort, spaces.SpaceDefaultOrder)
	if err != nil {
		return spaces.Space{}, err
	}
	existing, _, err := svc.ListSpacesPaginated(ctx, companyID, projectID, req)
	if err != nil {
		return spaces.Space{}, err
	}
	if found, ok := FindByName(existing, name, func(s spaces.Space) string { return s.Name }); ok {
		return found, nil
	}
	return svc.CreateSpace(ctx, companyID, projectID, name, spaceType, description)
}

func ensureWorkItem(ctx context.Context, svc WorkItemCreator, companyID, projectID string, spaceID *string, description, workType, quantityValue, unit string) (work.WorkItem, error) {
	req, err := listRequest(100, work.WorkItemSortFields, work.WorkItemDefaultSort, work.WorkItemDefaultOrder)
	if err != nil {
		return work.WorkItem{}, err
	}
	existing, _, err := svc.ListWorkItemsPaginatedByProject(ctx, companyID, projectID, req)
	if err != nil {
		return work.WorkItem{}, err
	}
	if found, ok := FindByName(existing, description, func(w work.WorkItem) string { return w.Description }); ok {
		return found, nil
	}
	return svc.CreateWorkItem(ctx, companyID, projectID, spaceID, description, workType, quantityValue, unit)
}

func stringPtr(v string) *string { return &v }
func int64Ptr(v int64) *int64    { return &v }

// UpdateProjectStatusToInProgress advances projectID to "in_progress" if it
// is not already there — design spec §5's table: Projects 4/5 have no
// dedicated auto-advancement path for active procurement.
func UpdateProjectStatusToInProgress(ctx context.Context, svc ProjectCreator, companyID, projectID string) (projects.Project, error) {
	return svc.UpdateProjectStatus(ctx, companyID, projectID, projects.ProjectStatusInProgress)
}

// SeedProject1 seeds "Taman Tun Condo Refresh" — Client -> Project ->
// Property -> Spaces -> Work Items only (design spec §5, Project 1: Early
// Setup). No commercial data. Idempotent.
func SeedProject1(
	ctx context.Context,
	clientsSvc ClientCreator,
	projectsSvc ProjectCreator,
	propertiesSvc PropertyCreator,
	spacesSvc SpaceCreator,
	workSvc WorkItemCreator,
	companyID string,
) (string, error) {
	client, err := ensureClient(ctx, clientsSvc, companyID,
		"Amir & Nadia Rahman", "+60 12-345 6789", "amir.rahman@example.com",
		"Taman Tun Dr Ismail, 60000 Kuala Lumpur", "Referred by a mutual friend; prefers WhatsApp updates.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, projectsSvc, companyID, client.ID, "Taman Tun Condo Refresh")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, propertiesSvc, companyID, project.ID,
		"Block C-12-3A, Taman Tun Dr Ismail, 60000 Kuala Lumpur", "condominium",
		"3-bedroom unit, approx 1,100 sqft, built 2015."); err != nil {
		return "", err
	}

	livingRoom, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Living Room", "living_room", "Open-plan living/dining area.")
	if err != nil {
		return "", err
	}
	kitchen, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Kitchen", "kitchen", "Galley-style wet kitchen.")
	if err != nil {
		return "", err
	}
	masterBedroom, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Master Bedroom", "bedroom", "Master bedroom with attached bathroom.")
	if err != nil {
		return "", err
	}
	bathroom, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Bathroom", "bathroom", "Common bathroom.")
	if err != nil {
		return "", err
	}

	spaceWorkItems := []struct {
		spaceID                          string
		description, workType, qty, unit string
	}{
		{livingRoom.ID, "Repaint living room walls and ceiling", "painting", "35", "sqm"},
		{livingRoom.ID, "Replace living room flooring with laminate", "flooring", "28", "sqm"},
		{kitchen.ID, "Install new kitchen cabinet hardware", "carpentry", "12", "unit"},
		{kitchen.ID, "Re-tile kitchen backsplash", "flooring", "8", "sqm"},
		{masterBedroom.ID, "Repaint master bedroom", "painting", "24", "sqm"},
		{bathroom.ID, "Replace bathroom waterproofing membrane", "plumbing", "6", "sqm"},
		{bathroom.ID, "Install new bathroom fixtures", "plumbing", "1", "lot"},
	}
	for _, item := range spaceWorkItems {
		spaceID := item.spaceID
		if _, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, &spaceID, item.description, item.workType, item.qty, item.unit); err != nil {
			return "", err
		}
	}

	if _, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, nil,
		"General electrical rewiring survey", "electrical", "1", "lot"); err != nil {
		return "", err
	}

	return project.ID, nil
}
