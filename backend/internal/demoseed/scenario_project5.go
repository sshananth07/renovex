package demoseed

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// SeedProject5 seeds "Damansara Heights Residence" — extends Project 4's
// depth with a provisional Award draft and a finalised Award Revision, the
// furthest currently implemented procurement workflow (design spec §5,
// Project 5). Idempotent. This does NOT fake a supplier notification step
// — FinaliseAward's real behavior is exactly what runs.
func SeedProject5(ctx context.Context, deps ScenarioDependencies) (string, error) {
	client, err := ensureClient(ctx, deps.Clients, deps.CompanyID,
		"Wong & Associates Sdn Bhd", "+60 3-7955 3322", "office@wongassociates.example.com",
		"Damansara Heights, 50490 Kuala Lumpur", "Small commercial client; office renovation, budget-conscious.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, deps.Projects, deps.CompanyID, client.ID, "Damansara Heights Residence")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, deps.Properties, deps.CompanyID, project.ID,
		"5 Jalan Batai, Damansara Heights, 50490 Kuala Lumpur", "bungalow",
		"Single-storey bungalow, structural and finishing renovation."); err != nil {
		return "", err
	}

	mainSpace, err := ensureSpace(ctx, deps.Spaces, deps.CompanyID, project.ID, "Main Living Area", "living_room", "Open-plan living/dining/kitchen.")
	if err != nil {
		return "", err
	}

	structuralWork, err := ensureWorkItem(ctx, deps.WorkItems, deps.CompanyID, project.ID, stringPtr(mainSpace.ID),
		"Structural wall repairs and replastering", "structural", "35", "sqm")
	if err != nil {
		return "", err
	}
	tileWork, err := ensureWorkItem(ctx, deps.WorkItems, deps.CompanyID, project.ID, stringPtr(mainSpace.ID),
		"Living area re-tiling", "flooring", "45", "sqm")
	if err != nil {
		return "", err
	}

	cement := deps.MaterialCatalog["Cement"]
	sand := deps.MaterialCatalog["Sand"]
	tile := deps.MaterialCatalog["Porcelain Floor Tile"]

	existingCostItems, err := deps.CostItems.ListCostItemsByProject(ctx, deps.CompanyID, project.ID)
	if err != nil {
		return "", err
	}
	haveCostItem := func(description string) bool {
		_, found := FindByName(existingCostItems, description, func(c costs.CostItem) string { return c.Description })
		return found
	}
	if !haveCostItem("Cement for structural repairs") {
		qtyValue, qtyUnit := "25", "bag"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(structuralWork.ID), costs.CostCategoryMaterial,
			"Cement for structural repairs", &qtyValue, &qtyUnit, int64Ptr(2200), int64Ptr(55000), nil, nil, nil, "MYR", stringPtr(cement.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Sand for structural repairs") {
		qtyValue, qtyUnit := "2.5", "tonne"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(structuralWork.ID), costs.CostCategoryMaterial,
			"Sand for structural repairs", &qtyValue, &qtyUnit, int64Ptr(8000), int64Ptr(20000), nil, nil, nil, "MYR", stringPtr(sand.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Porcelain tile for living area") {
		qtyValue, qtyUnit := "45", "sqm"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(tileWork.ID), costs.CostCategoryMaterial,
			"Porcelain tile for living area", &qtyValue, &qtyUnit, int64Ptr(4500), int64Ptr(202500), nil, nil, nil, "MYR", stringPtr(tile.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}

	cementReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, cement.ID, "25", "bag")
	if err != nil {
		return "", err
	}
	sandReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, sand.ID, "2.5", "tonne")
	if err != nil {
		return "", err
	}
	tileReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, tile.ID, "45", "sqm")
	if err != nil {
		return "", err
	}

	issued, err := ensureIssuedRFQ(ctx, deps.RFQs, deps.RFQIssuance, deps.CompanyID, deps.ActorUserID, project.ID,
		"Damansara Heights renovation — structural and flooring materials",
		"5 Jalan Batai, Damansara Heights, 50490 Kuala Lumpur",
		[]materialrequirements.MaterialRequirement{cementReq, sandReq, tileReq}, "project5")
	if err != nil {
		return "", err
	}

	invitations, err := ensureSupplierInvitations(ctx, deps.RFQIssuance, deps.CompanyID, deps.ActorUserID, issued.RFQChainID,
		deps.SupplierDirectory, []string{"DemoBuild Materials Sdn Bhd", "Metro Tile Supply Sdn Bhd"}, "project5")
	if err != nil {
		return "", err
	}

	linePricesFor := func(materialUnitPrices map[string]int64) map[string]int64 {
		result := make(map[string]int64, len(issued.Lines))
		for _, line := range issued.Lines {
			if price, ok := materialUnitPrices[line.MaterialID]; ok {
				result[line.ID] = price
			}
		}
		return result
	}

	if len(invitations) >= 2 {
		if _, err := SubmitSupplierOffer(ctx, deps.SupplierAccess, deps.SupplierOffers,
			deps.InvitationKeyring, deps.CodeKeyring, invitations[0],
			linePricesFor(map[string]int64{cement.ID: 2280, sand.ID: 7900}), "project5:supplier1"); err != nil {
			return "", err
		}
		if _, err := SubmitSupplierOffer(ctx, deps.SupplierAccess, deps.SupplierOffers,
			deps.InvitationKeyring, deps.CodeKeyring, invitations[1],
			linePricesFor(map[string]int64{tile.ID: 4400}), "project5:supplier2"); err != nil {
			return "", err
		}
	}

	// CreateAwardDraft is safe to call unconditionally on every run — per
	// its own doc comment, adopting an already-open draft emits no
	// authoritative write. Its return is also the only way to learn the
	// real AwardChainID.
	draft, err := deps.Awards.CreateAwardDraft(ctx, deps.CompanyID, deps.ActorUserID, issued.ID)
	if err != nil {
		return "", err
	}

	if _, err := deps.Awards.GetCurrentAward(ctx, deps.CompanyID, issued.ID); err == nil {
		if _, err := UpdateProjectStatusToInProgress(ctx, deps.Projects, deps.CompanyID, project.ID); err != nil {
			return "", err
		}
		return project.ID, nil // already finalised — idempotent no-op
	} else if !errors.Is(err, awards.ErrAwardRevisionNotFound) {
		return "", err
	}

	// GetComparison is the SAME real, session-independent method the
	// frontend comparison view uses — it reads every submitted offer
	// version's lines for this issued RFQ version directly, keyed by
	// SupplierID, with no Supplier session needed.
	comparison, err := deps.Awards.GetComparison(ctx, deps.CompanyID, issued.ID, time.Now().UTC())
	if err != nil {
		return "", err
	}
	demoBuildSupplierID := deps.SupplierDirectory["DemoBuild Materials Sdn Bhd"].ID
	metroTileSupplierID := deps.SupplierDirectory["Metro Tile Supply Sdn Bhd"].ID

	for _, offer := range comparison.Offers {
		if offer.SupplierID != demoBuildSupplierID && offer.SupplierID != metroTileSupplierID {
			continue // not one of this scenario's two invited Suppliers
		}
		for _, line := range offer.Lines {
			if line.ResponseStatus != awards.OfferLineQuoted {
				continue // no_bid/unavailable lines are never selectable (§D1)
			}
			updated, err := deps.Awards.SelectAwardLine(ctx, deps.CompanyID, deps.ActorUserID, awards.SelectAwardLineInput{
				IssuedRFQVersionID: issued.ID, IssuedRFQLineID: line.IssuedRFQLineID,
				OfferVersionID: offer.OfferVersionID, OfferLineID: line.OfferLineID, ExpectedRevision: draft.Revision,
			})
			if err != nil {
				return "", err
			}
			draft = updated
		}
	}

	if _, err := deps.Awards.FinaliseAward(ctx, deps.CompanyID, deps.ActorUserID, awards.FinaliseAwardInput{
		IssuedRFQVersionID: issued.ID, OperationID: OperationID("project5", "award-finalise"),
		ChangeReason: "Initial award — demo scenario.", FinalisedAt: time.Now().UTC(),
	}); err != nil {
		return "", err
	}

	if _, err := UpdateProjectStatusToInProgress(ctx, deps.Projects, deps.CompanyID, project.ID); err != nil {
		return "", err
	}

	return project.ID, nil
}
