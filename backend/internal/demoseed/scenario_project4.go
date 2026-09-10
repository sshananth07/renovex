package demoseed

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

type RequirementCreator interface {
	CreateManualRequirement(ctx context.Context, companyID, actorUserID string, input materialrequirements.CreateManualInput) (materialrequirements.MaterialRequirement, error)
	ListRequirementsByProject(ctx context.Context, companyID, projectID string) ([]materialrequirements.MaterialRequirement, error)
	ReviewRequirement(ctx context.Context, companyID, actorUserID, requirementID string, expectedRevision int64) (materialrequirements.MaterialRequirement, error)
}

type RFQCreator interface {
	CreateRFQ(ctx context.Context, companyID, actorUserID, projectID string, input rfqs.CreateRFQInput) (rfqs.RFQ, error)
	ListRFQsByProject(ctx context.Context, companyID, projectID string) ([]rfqs.RFQ, error)
	AddLine(ctx context.Context, companyID, actorUserID, rfqID string, expectedRevision int64, requirementID string, expectedRequirementRevision int64) (rfqs.RFQ, error)
	MarkReady(ctx context.Context, companyID, actorUserID, rfqID string, expectedRevision int64) (rfqs.RFQ, error)
}

type RFQIssuer interface {
	IssueVersion(ctx context.Context, companyID, actorUserID string, input rfqissuance.IssueVersionInput) (rfqissuance.IssuedRFQVersion, error)
	CreateInvitation(ctx context.Context, companyID, actorUserID string, input rfqissuance.CreateInvitationInput) (rfqissuance.SupplierInvitation, error)
	ListInvitations(ctx context.Context, companyID, rfqChainID string) ([]rfqissuance.SupplierInvitation, error)
	SendInvitation(ctx context.Context, companyID, actorUserID string, input rfqissuance.SendInvitationInput) (rfqissuance.SendResult, error)
}

func ensureReviewedRequirement(ctx context.Context, svc RequirementCreator, companyID, actorUserID, projectID, materialID, quantityValue, quantityUnit string) (materialrequirements.MaterialRequirement, error) {
	existing, err := svc.ListRequirementsByProject(ctx, companyID, projectID)
	if err != nil {
		return materialrequirements.MaterialRequirement{}, err
	}
	req, found := FindByName(existing, materialID, func(r materialrequirements.MaterialRequirement) string { return r.MaterialID })
	if !found {
		created, err := svc.CreateManualRequirement(ctx, companyID, actorUserID, materialrequirements.CreateManualInput{
			ProjectID: projectID, MaterialID: materialID,
			QuantityValue: quantityValue, QuantityUnit: quantityUnit,
		})
		if err != nil {
			return materialrequirements.MaterialRequirement{}, err
		}
		req = created
	}
	if req.Status == materialrequirements.RequirementStatusDraft {
		reviewed, err := svc.ReviewRequirement(ctx, companyID, actorUserID, req.ID, req.Revision)
		if err != nil {
			return materialrequirements.MaterialRequirement{}, err
		}
		req = reviewed
	}
	return req, nil
}

func ensureIssuedRFQ(ctx context.Context, rfqSvc RFQCreator, issuerSvc RFQIssuer, companyID, actorUserID, projectID, title, deliveryAddress string, requirements []materialrequirements.MaterialRequirement, operationSlug string) (rfqissuance.IssuedRFQVersion, error) {
	existing, err := rfqSvc.ListRFQsByProject(ctx, companyID, projectID)
	if err != nil {
		return rfqissuance.IssuedRFQVersion{}, err
	}
	var rfq rfqs.RFQ
	if len(existing) > 0 {
		rfq = existing[0]
	} else {
		// ResponseDeadline must be set here: rfqissuance.IssueVersion reads
		// the M7 ready-RFQ snapshot and refuses with ErrResponseDeadlineRequired
		// if it is nil — there is no way to supply it later at issuance time.
		// DeliveryAddress must also be set here: rfqs.Service.MarkReady
		// refuses with ErrRFQDeliveryAddressRequired if it is empty (found
		// via the Task 18 acceptance suite against real domain services —
		// the fakes used by this file's own unit tests never enforce this
		// validation, so the gap was invisible until real Mongo/services
		// exercised it).
		responseDeadline := time.Now().UTC().Add(7 * 24 * time.Hour)
		created, err := rfqSvc.CreateRFQ(ctx, companyID, actorUserID, projectID, rfqs.CreateRFQInput{
			Title: title, DeliveryAddress: deliveryAddress, ResponseDeadline: &responseDeadline,
		})
		if err != nil {
			return rfqissuance.IssuedRFQVersion{}, err
		}
		rfq = created
	}

	if rfq.Status == rfqs.RFQStatusDraft {
		for _, req := range requirements {
			updated, err := rfqSvc.AddLine(ctx, companyID, actorUserID, rfq.ID, rfq.Revision, req.ID, req.Revision)
			if err != nil {
				return rfqissuance.IssuedRFQVersion{}, err
			}
			rfq = updated
		}
		ready, err := rfqSvc.MarkReady(ctx, companyID, actorUserID, rfq.ID, rfq.Revision)
		if err != nil {
			return rfqissuance.IssuedRFQVersion{}, err
		}
		rfq = ready
	}

	issued, err := issuerSvc.IssueVersion(ctx, companyID, actorUserID, rfqissuance.IssueVersionInput{
		RFQChainID: rfq.ChainID(), Currency: "MYR", OperationID: OperationID(operationSlug, "rfq-issue"),
	})
	if err != nil {
		return rfqissuance.IssuedRFQVersion{}, err
	}
	return issued, nil
}

func ensureSupplierInvitations(ctx context.Context, issuerSvc RFQIssuer, companyID, actorUserID, rfqChainID string, supplierDirectory map[string]suppliers.Supplier, supplierNames []string, operationSlug string) ([]rfqissuance.SupplierInvitation, error) {
	existing, err := issuerSvc.ListInvitations(ctx, companyID, rfqChainID)
	if err != nil {
		return nil, err
	}
	haveInvitation := func(supplierID string) bool {
		for _, inv := range existing {
			if inv.SupplierID == supplierID {
				return true
			}
		}
		return false
	}
	result := existing
	for _, name := range supplierNames {
		supplier := supplierDirectory[name]
		if haveInvitation(supplier.ID) {
			continue
		}
		created, err := issuerSvc.CreateInvitation(ctx, companyID, actorUserID, rfqissuance.CreateInvitationInput{
			RFQChainID: rfqChainID, SupplierID: supplier.ID,
			RecipientName: supplier.ContactPerson, RecipientEmail: supplier.Email,
			ExpiresAt: time.Now().UTC().Add(30 * 24 * time.Hour),
		})
		if err != nil {
			return nil, err
		}
		// A freshly-created invitation starts in status=draft and PermitsAccess
		// returns false until it is sent — "an explicit send is what activates
		// the invitation" (rfqissuance, §5). Without this, SubmitSupplierOffer's
		// OpenInvitation call always refuses with ErrInvalidSupplierCredential —
		// found via the Task 18 acceptance suite against real domain services,
		// invisible to this file's own fakes-based unit tests since the fakes
		// never modeled draft/active status at all.
		if _, err := issuerSvc.SendInvitation(ctx, companyID, actorUserID, rfqissuance.SendInvitationInput{
			InvitationID: created.ID,
			OperationID:  OperationID(operationSlug, "invite-send:"+supplier.ID),
		}); err != nil {
			return nil, err
		}
		result = append(result, created)
	}
	return result, nil
}

// SeedProject4 seeds "Subang Family Home Renovation" — active procurement:
// reviewed Material Requirements -> issued RFQ -> 2 competing Supplier
// Offers via the real Supplier journey (design spec §5, Project 4).
// actorUserID is deps.ActorUserID — the REAL resolved demo User ID, not a
// placeholder string. Idempotent.
func SeedProject4(ctx context.Context, deps ScenarioDependencies) (string, error) {
	client, err := ensureClient(ctx, deps.Clients, deps.CompanyID,
		"Chong Family Trust", "+60 12-666 7788", "chong.family@example.com",
		"Subang Jaya, 47500 Selangor", "Family home, multi-generational household; decisions go through the eldest son.")
	if err != nil {
		return "", err
	}

	project, err := ensureProject(ctx, deps.Projects, deps.CompanyID, client.ID, "Subang Family Home Renovation")
	if err != nil {
		return "", err
	}

	if _, err := ensureProperty(ctx, deps.Properties, deps.CompanyID, project.ID,
		"22 Jalan SS15/4, 47500 Subang Jaya, Selangor", "terrace_house",
		"Double-storey terrace, whole-house renovation."); err != nil {
		return "", err
	}

	structuralSpace, err := ensureSpace(ctx, deps.Spaces, deps.CompanyID, project.ID, "Ground Floor", "living_room", "Ground floor structural and finishing works.")
	if err != nil {
		return "", err
	}

	cementWork, err := ensureWorkItem(ctx, deps.WorkItems, deps.CompanyID, project.ID, stringPtr(structuralSpace.ID),
		"Ground floor wall repairs and replastering", "structural", "40", "sqm")
	if err != nil {
		return "", err
	}
	tileWork, err := ensureWorkItem(ctx, deps.WorkItems, deps.CompanyID, project.ID, stringPtr(structuralSpace.ID),
		"Ground floor re-tiling", "flooring", "55", "sqm")
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
	if !haveCostItem("Cement for wall repairs") {
		qtyValue, qtyUnit := "30", "bag"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(cementWork.ID), costs.CostCategoryMaterial,
			"Cement for wall repairs", &qtyValue, &qtyUnit, int64Ptr(2200), int64Ptr(66000), nil, nil, nil, "MYR", stringPtr(cement.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Sand for wall repairs") {
		qtyValue, qtyUnit := "3", "tonne"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(cementWork.ID), costs.CostCategoryMaterial,
			"Sand for wall repairs", &qtyValue, &qtyUnit, int64Ptr(8000), int64Ptr(24000), nil, nil, nil, "MYR", stringPtr(sand.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}
	if !haveCostItem("Porcelain tile for ground floor") {
		qtyValue, qtyUnit := "55", "sqm"
		if _, err := deps.CostItems.CreateCostItem(ctx, deps.CompanyID, project.ID, stringPtr(tileWork.ID), costs.CostCategoryMaterial,
			"Porcelain tile for ground floor", &qtyValue, &qtyUnit, int64Ptr(4500), int64Ptr(247500), nil, nil, nil, "MYR", stringPtr(tile.ID), time.Now(), ""); err != nil {
			return "", err
		}
	}

	cementReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, cement.ID, "30", "bag")
	if err != nil {
		return "", err
	}
	sandReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, sand.ID, "3", "tonne")
	if err != nil {
		return "", err
	}
	tileReq, err := ensureReviewedRequirement(ctx, deps.Requirements, deps.CompanyID, deps.ActorUserID, project.ID, tile.ID, "55", "sqm")
	if err != nil {
		return "", err
	}

	issued, err := ensureIssuedRFQ(ctx, deps.RFQs, deps.RFQIssuance, deps.CompanyID, deps.ActorUserID, project.ID,
		"Subang renovation — structural and flooring materials",
		"22 Jalan SS15/4, 47500 Subang Jaya, Selangor",
		[]materialrequirements.MaterialRequirement{cementReq, sandReq, tileReq}, "project4")
	if err != nil {
		return "", err
	}

	invitations, err := ensureSupplierInvitations(ctx, deps.RFQIssuance, deps.CompanyID, deps.ActorUserID, issued.RFQChainID,
		deps.SupplierDirectory, []string{"DemoBuild Materials Sdn Bhd", "Metro Tile Supply Sdn Bhd"}, "project4")
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
			linePricesFor(map[string]int64{cement.ID: 2350, sand.ID: 8200}),
			"project4:supplier1"); err != nil {
			return "", err
		}
		if _, err := SubmitSupplierOffer(ctx, deps.SupplierAccess, deps.SupplierOffers,
			deps.InvitationKeyring, deps.CodeKeyring, invitations[1],
			linePricesFor(map[string]int64{tile.ID: 4350}),
			"project4:supplier2"); err != nil {
			return "", err
		}
	}

	if _, err := UpdateProjectStatusToInProgress(ctx, deps.Projects, deps.CompanyID, project.ID); err != nil {
		return "", err
	}

	return project.ID, nil
}
