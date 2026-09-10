package demoseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/access"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

type QuotationCreator interface {
	CreateQuotation(ctx context.Context, companyID, projectID, estimateID string) (quotations.Quotation, error)
	ListQuotationsByProject(ctx context.Context, companyID, projectID string) ([]quotations.Quotation, error)
	FinalizeQuotation(ctx context.Context, companyID, quotationID string, expectedRevision int64) (quotations.Quotation, error)
}

// QuotationSharer is the capability demoseed needs from access. Includes
// RotateGrant — the crash-recovery mechanism: a resumed scenario that finds
// the Project not yet quotation_approved but a live grant already exists
// uses RotateGrant (the real "resend client link" operation) to mint a
// fresh usable token rather than assuming a stale RawToken is still
// available (it never is, by design — GetShareStatus's RawToken is always
// empty).
type QuotationSharer interface {
	ShareQuotation(ctx context.Context, companyID, quotationID, actorUserID string) (access.GrantView, bool, error)
	GetShareStatus(ctx context.Context, companyID, quotationID string) (access.GrantView, error)
	RotateGrant(ctx context.Context, companyID, grantID, actorUserID string, expectedRevision int64) (access.GrantView, error)
	ViewQuotationByToken(ctx context.Context, rawToken string) (access.ClientQuotationView, error)
	SubmitClientDecision(ctx context.Context, in access.ClientDecisionInput) (access.ClientDecisionResult, error)
}

// SeedProject3 seeds "Mont Kiara Apartment Upgrade" — extends Project 2's
// depth through the full commercial workflow: finalized Estimate ->
// Quotation -> ShareQuotation (advances Project to quotation_sent) ->
// client-facing acceptance (advances Project to quotation_approved).
// Design spec §5, Project 3. Idempotent AND crash-recoverable: the
// completion signal is the PROJECT'S OWN STATUS (quotation_approved),
// never a locally-held RawToken, which is only ever available on the exact
// call that minted it.
func SeedProject3(
	ctx context.Context,
	clientsSvc ClientCreator, projectsSvc ProjectCreator, propertiesSvc PropertyCreator,
	spacesSvc SpaceCreator, workSvc WorkItemCreator, costsSvc CostItemCreator,
	workersSvc WorkerCreator, labourSvc LabourEntryCreator, estimatesSvc EstimateCreator,
	quotationsSvc QuotationCreator, accessSvc QuotationSharer,
	materialCatalog map[string]materials.Material,
	actorUserID, companyID string,
) (string, error) {
	client, err := ensureClient(ctx, clientsSvc, companyID,
		"Demo Property Holdings Sdn Bhd", "+60 3-2288 1199", "projects@demopropertyholdings.example.com",
		"Mont Kiara, 50480 Kuala Lumpur", "Corporate client managing several rental units; invoices go to their finance team.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, projectsSvc, companyID, client.ID, "Mont Kiara Apartment Upgrade")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, propertiesSvc, companyID, project.ID,
		"Block B-8-2, Mont Kiara, 50480 Kuala Lumpur", "condominium",
		"2-bedroom rental unit, full upgrade before re-letting."); err != nil {
		return "", err
	}

	livingRoom, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Living Room", "living_room", "Open-plan living/dining.")
	if err != nil {
		return "", err
	}
	bedroom, err := ensureSpace(ctx, spacesSvc, companyID, project.ID, "Bedroom", "bedroom", "Second bedroom, currently used as storage.")
	if err != nil {
		return "", err
	}

	paintWork, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, stringPtr(livingRoom.ID),
		"Repaint entire unit", "painting", "85", "sqm")
	if err != nil {
		return "", err
	}
	floorWork, err := ensureWorkItem(ctx, workSvc, companyID, project.ID, stringPtr(bedroom.ID),
		"Replace bedroom flooring", "flooring", "16", "sqm")
	if err != nil {
		return "", err
	}

	existingCostItems, err := costsSvc.ListCostItemsByProject(ctx, companyID, project.ID)
	if err != nil {
		return "", err
	}
	haveCostItem := func(description string) bool {
		_, found := FindByName(existingCostItems, description, func(c costs.CostItem) string { return c.Description })
		return found
	}
	paint := materialCatalog["Interior Paint"]
	tile := materialCatalog["Porcelain Floor Tile"]
	if !haveCostItem("Interior paint, full unit") {
		qtyValue, qtyUnit := "85", "sqm"
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(paintWork.ID), costs.CostCategoryMaterial,
			"Interior paint, full unit", &qtyValue, &qtyUnit,
			int64Ptr(8500), int64Ptr(722500), nil, nil, nil, "MYR", stringPtr(paint.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Laminate flooring supply and install") {
		qtyValue, qtyUnit := "16", "sqm"
		if _, err := costsSvc.CreateCostItem(ctx, companyID, project.ID, stringPtr(floorWork.ID), costs.CostCategoryMaterial,
			"Laminate flooring supply and install", &qtyValue, &qtyUnit,
			int64Ptr(6500), int64Ptr(104000), nil, nil, nil, "MYR", stringPtr(tile.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}

	estimate, err := estimatesSvc.GetLatestEstimate(ctx, companyID, project.ID)
	if err != nil {
		created, createErr := estimatesSvc.CreateEstimate(ctx, companyID, project.ID, estimates.PricingModeMarkup, money.RateBPS(3000))
		if createErr != nil {
			return "", createErr
		}
		estimate = created
	}
	if estimate.Status != estimates.EstimateStatusFinalized {
		finalized, err := estimatesSvc.FinalizeEstimate(ctx, companyID, estimate.ID, estimate.Revision)
		if err != nil {
			return "", err
		}
		estimate = finalized
	}

	existingQuotations, err := quotationsSvc.ListQuotationsByProject(ctx, companyID, project.ID)
	if err != nil {
		return "", err
	}
	var quotation quotations.Quotation
	if len(existingQuotations) > 0 {
		quotation = existingQuotations[0]
	} else {
		created, err := quotationsSvc.CreateQuotation(ctx, companyID, project.ID, estimate.ID)
		if err != nil {
			return "", err
		}
		quotation = created
	}
	if quotation.Status != quotations.QuotationStatusFinalized {
		finalized, err := quotationsSvc.FinalizeQuotation(ctx, companyID, quotation.ID, quotation.Revision)
		if err != nil {
			return "", err
		}
		quotation = finalized
	}

	// The AUTHORITATIVE completion signal for this whole scenario: if the
	// Project already reached quotation_approved, the client decision has
	// already been recorded — nothing more to do, regardless of what state
	// the grant/token happens to be in.
	current, err := projectsSvc.GetProject(ctx, companyID, project.ID)
	if err != nil {
		return "", err
	}
	if current.Status == projects.ProjectStatusQuotationApproved {
		return project.ID, nil
	}

	// Fail CLOSED here too. Only a confirmed access.ErrGrantNotFound proves
	// no grant exists yet — any other error from GetShareStatus (a timeout,
	// a dropped connection) must NOT be treated as "safe to mint a new
	// grant via ShareQuotation," or a transient infra hiccup could silently
	// create a second, redundant grant for the same Quotation instead of
	// surfacing the real failure.
	grant, err := accessSvc.GetShareStatus(ctx, companyID, quotation.ID)
	var rawToken string
	switch {
	case errors.Is(err, access.ErrGrantNotFound):
		// No grant yet — mint the first one.
		shared, _, shareErr := accessSvc.ShareQuotation(ctx, companyID, quotation.ID, actorUserID)
		if shareErr != nil {
			return "", shareErr
		}
		grant = shared
		rawToken = shared.RawToken
	case err != nil:
		return "", fmt.Errorf("demoseed: check for an existing quotation share grant: %w", err)
	default:
		// A grant already exists (from this run or a prior one), but per
		// GrantView's own documented behavior, GetShareStatus's RawToken is
		// ALWAYS empty — it is populated only on the call that just minted
		// it. Since the Project has not reached quotation_approved yet
		// (checked above), the decision genuinely has not happened —
		// rotate to obtain a FRESH usable token (the real "resend client
		// link" operation), rather than assuming a stale one survived.
		rotated, rotateErr := accessSvc.RotateGrant(ctx, companyID, grant.GrantID, actorUserID, grant.Revision)
		if rotateErr != nil {
			return "", rotateErr
		}
		grant = rotated
		rawToken = rotated.RawToken
	}

	if _, err := accessSvc.SubmitClientDecision(ctx, access.ClientDecisionInput{
		Token: rawToken, Status: "accepted",
		ClientName: "Demo Property Holdings Sdn Bhd", ClientEmail: "projects@demopropertyholdings.example.com",
		Comment: "Approved — please proceed with the works as quoted.",
	}); err != nil {
		return "", err
	}

	return project.ID, nil
}
