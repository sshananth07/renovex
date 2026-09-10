package demoseed

import (
	"context"

	"github.com/shananth/renovation-platform/backend/internal/projects"
)

type ScopeBriefSetter interface {
	UpdateProjectScopeBrief(ctx context.Context, companyID, projectID, scopeBrief string) (projects.Project, error)
}

const project6ScopeBrief = `Full condo unit renovation for a 900 sqft 2-bedroom unit at KL Eco City. ` +
	`Scope includes: full repaint of all rooms, kitchen cabinet replacement, ` +
	`bathroom re-tiling (both bathrooms), living room flooring replacement with ` +
	`laminate, and general electrical point additions in the living room and ` +
	`master bedroom. Client wants a modern minimalist finish, light neutral ` +
	`colour palette. Target completion within 8 weeks. Budget-conscious but ` +
	`open to quality materials for high-visibility areas (living room flooring, ` +
	`kitchen cabinets).`

// SeedProject6 seeds "KL Eco City Condo Renovation" — deliberately shallow:
// Client -> Project -> Property -> Scope Brief only (design spec §5,
// Project 6: AI Demo). No AI service calls happen during seeding. Idempotent.
func SeedProject6(
	ctx context.Context,
	clientsSvc ClientCreator,
	projectsSvc ProjectCreator,
	scopeSvc ScopeBriefSetter,
	propertiesSvc PropertyCreator,
	companyID string,
) (string, error) {
	client, err := ensureClient(ctx, clientsSvc, companyID,
		"KL Eco City Sdn Bhd", "+60 3-2166 8800", "renovations@klecocity.example.com",
		"KL Eco City, 55100 Kuala Lumpur", "Corporate landlord; unit being refreshed between tenancies.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, projectsSvc, companyID, client.ID, "KL Eco City Condo Renovation")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, propertiesSvc, companyID, project.ID,
		"Tower 3, Unit 21-05, KL Eco City, 55100 Kuala Lumpur", "condominium",
		"2-bedroom unit, approx 900 sqft, previously tenanted."); err != nil {
		return "", err
	}

	if project.ScopeBrief == "" {
		if _, err := scopeSvc.UpdateProjectScopeBrief(ctx, companyID, project.ID, project6ScopeBrief); err != nil {
			return "", err
		}
	}

	return project.ID, nil
}
