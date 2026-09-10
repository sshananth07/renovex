package supplieroffers

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

type offerMoneyDTO struct {
	AmountMinor int64  `json:"amountMinor"`
	Currency    string `json:"currency"`
}

type offerLineTaxDTO struct {
	TaxType            string `json:"taxType"`
	Exempt             bool   `json:"exempt"`
	RateBPS            *int64 `json:"rateBps"`
	RegistrationNumber string `json:"registrationNumber"`
	BasisNote          string `json:"basisNote"`
}

type offerVersionLineDTO struct {
	ID                       string           `json:"id"`
	RFQLineID                string           `json:"rfqLineId"`
	ResponseStatus           string           `json:"responseStatus"`
	QuotedQuantity           string           `json:"quotedQuantity"`
	QuotedUnit               string           `json:"quotedUnit"`
	UnitPriceExcludingTax    *offerMoneyDTO   `json:"unitPriceExcludingTax"`
	LineSubtotalExcludingTax *offerMoneyDTO   `json:"lineSubtotalExcludingTax"`
	LineTax                  *offerLineTaxDTO `json:"lineTax"`
	LineTaxAmount            offerMoneyDTO    `json:"lineTaxAmount"`
	Brand                    string           `json:"brand"`
	SKU                      string           `json:"sku"`
	ProductDescription       string           `json:"productDescription"`
	LeadTime                 string           `json:"leadTime"`
	SupplierLineNotes        string           `json:"supplierLineNotes"`
	CommercialExceptions     string           `json:"commercialExceptions"`
}

type offerTaxDTO struct {
	Mode       string            `json:"mode"`
	OfferLevel *offerLevelTaxDTO `json:"offerLevel"`
}

type offerLevelTaxDTO struct {
	TaxType            string        `json:"taxType"`
	TaxAmount          offerMoneyDTO `json:"taxAmount"`
	BasisNote          string        `json:"basisNote"`
	RegistrationNumber string        `json:"registrationNumber"`
}

type offerChargeGroupDTO struct {
	ID                   string         `json:"id"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	ApplicableRFQLineIDs []string       `json:"applicableRfqLineIds"`
	Trigger              string         `json:"trigger"`
	Threshold            *offerMoneyDTO `json:"threshold"`
	Calculation          string         `json:"calculation"`
	FixedAmount          *offerMoneyDTO `json:"fixedAmount"`
	RateBPS              *int64         `json:"rateBps"`
}

type offerDeliveryChargeDTO struct {
	Amount offerMoneyDTO `json:"amount"`
}

type offerWithdrawalProjectionDTO struct {
	WithdrawnAt time.Time `json:"withdrawnAt"`
	Reason      string    `json:"reason"`
}

type offerVersionDetailDTO struct {
	ID                   string                        `json:"id"`
	IssuedRFQVersionID   string                        `json:"issuedRfqVersionId"`
	VersionNumber        int                           `json:"versionNumber"`
	Currency             string                        `json:"currency"`
	SubmittedAt          time.Time                     `json:"submittedAt"`
	OfferValidUntil      time.Time                     `json:"offerValidUntil"`
	PublicStatus         OfferVersionStatus            `json:"publicStatus"`
	IsSuperseded         bool                          `json:"isSuperseded"`
	CanWithdraw          bool                          `json:"canWithdraw"`
	Lines                []offerVersionLineDTO         `json:"lines"`
	Tax                  offerTaxDTO                   `json:"tax"`
	ChargeGroups         []offerChargeGroupDTO         `json:"chargeGroups"`
	DeliveryCharge       *offerDeliveryChargeDTO       `json:"deliveryCharge"`
	QuotedLineSubtotal   offerMoneyDTO                 `json:"quotedLineSubtotal"`
	QuotedTaxTotal       offerMoneyDTO                 `json:"quotedTaxTotal"`
	FullOfferChargeTotal offerMoneyDTO                 `json:"fullOfferChargeTotal"`
	DeliveryChargeTotal  offerMoneyDTO                 `json:"deliveryChargeTotal"`
	GrandTotal           offerMoneyDTO                 `json:"grandTotal"`
	SupplierNotes        string                        `json:"supplierNotes"`
	Withdrawal           *offerWithdrawalProjectionDTO `json:"withdrawal"`
}

type offerVersionSummaryDTO struct {
	ID                 string             `json:"id"`
	IssuedRFQVersionID string             `json:"issuedRfqVersionId"`
	VersionNumber      int                `json:"versionNumber"`
	Currency           string             `json:"currency"`
	SubmittedAt        time.Time          `json:"submittedAt"`
	OfferValidUntil    time.Time          `json:"offerValidUntil"`
	PublicStatus       OfferVersionStatus `json:"publicStatus"`
	IsSuperseded       bool               `json:"isSuperseded"`
	CanWithdraw        bool               `json:"canWithdraw"`
	GrandTotal         offerMoneyDTO      `json:"grandTotal"`
}

type offerHistoryOutput struct {
	Status int
	Body   map[string]any
}
type offerDetailOutput struct {
	Status int
	Body   map[string]any
}

func registerSupplierOfferHistoryHandlers(
	api huma.API, service *Service, routes supplierOfferRouteSet,
) {
	huma.Register(api, huma.Operation{
		OperationID: routes.operation("list-versions"), Method: http.MethodGet,
		Path:     routes.base + "/versions",
		Summary:  "List submitted offer versions",
		Security: platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionToken string `cookie:"supplier_session"`
		InvitationID string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
		PageSize     int    `query:"pageSize" minimum:"0" maximum:"100"`
		Cursor       int    `query:"cursor" minimum:"0"`
	}) (*offerHistoryOutput, error) {
		page, err := service.ListSupplierOfferVersions(ctx, SupplierOfferHistoryInput{
			SupplierOfferReadContextInput: SupplierOfferReadContextInput{
				SessionToken: input.SessionToken, InvitationID: input.InvitationID,
				AccessedAt: time.Now().UTC(),
			}, PageSize: input.PageSize, Cursor: input.Cursor,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		items := make([]offerVersionSummaryDTO, 0, len(page.Versions))
		for _, version := range page.Versions {
			items = append(items, offerVersionSummaryDTO{
				ID: version.ID, IssuedRFQVersionID: version.IssuedRFQVersionID,
				VersionNumber: version.VersionNumber, Currency: version.Currency,
				SubmittedAt: version.SubmittedAt, OfferValidUntil: version.OfferValidUntil,
				PublicStatus: version.PublicStatus, IsSuperseded: version.IsSuperseded,
				CanWithdraw: version.CanWithdraw, GrandTotal: toOfferMoneyDTO(version.GrandTotal),
			})
		}
		return &offerHistoryOutput{Status: http.StatusOK,
			Body: map[string]any{"versions": items, "nextCursor": page.NextCursor}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: routes.operation("get-version"), Method: http.MethodGet,
		Path:     routes.base + "/versions/{versionId}",
		Summary:  "Read a submitted offer version",
		Security: platformhttp.SupplierSessionSecurityRequirements(),
	}, func(ctx context.Context, input *struct {
		SessionToken string `cookie:"supplier_session"`
		InvitationID string `path:"invitationId" maxLength:"128" pattern:"^[!-~]+$"`
		VersionID    string `path:"versionId" maxLength:"128" pattern:"^[!-~]+$"`
	}) (*offerDetailOutput, error) {
		projection, err := service.GetSupplierOfferVersion(ctx, SupplierOfferVersionDetailInput{
			SupplierOfferReadContextInput: SupplierOfferReadContextInput{
				SessionToken: input.SessionToken, InvitationID: input.InvitationID,
				AccessedAt: time.Now().UTC(),
			}, OfferVersionID: input.VersionID,
		})
		if err != nil {
			return nil, MapSupplierOfferError(err)
		}
		return &offerDetailOutput{Status: http.StatusOK,
			Body: offerVersionDetailBody(projection)}, nil
	})
}

func toOfferMoneyDTO(value money.Money) offerMoneyDTO {
	return offerMoneyDTO{AmountMinor: value.Amount, Currency: value.Currency}
}

func offerVersionDetailBody(projection SupplierOfferVersionProjection) map[string]any {
	v := projection.Version
	lines := make([]offerVersionLineDTO, 0, len(v.Lines))
	for _, line := range v.Lines {
		dto := offerVersionLineDTO{ID: line.ID, RFQLineID: line.RFQLineID,
			ResponseStatus: string(line.ResponseStatus), Brand: line.Brand, SKU: line.SKU,
			ProductDescription: line.ProductDescription, LeadTime: line.LeadTime,
			SupplierLineNotes:    line.SupplierLineNotes,
			CommercialExceptions: line.CommercialExceptions,
			LineTaxAmount:        toOfferMoneyDTO(line.LineTaxAmount)}
		if line.QuotedQuantity != nil {
			dto.QuotedQuantity = line.QuotedQuantity.Value.String()
			dto.QuotedUnit = line.QuotedQuantity.Unit
		}
		if line.UnitPriceExcludingTax != nil {
			value := toOfferMoneyDTO(*line.UnitPriceExcludingTax)
			dto.UnitPriceExcludingTax = &value
		}
		if line.LineSubtotalExcludingTax != nil {
			value := toOfferMoneyDTO(*line.LineSubtotalExcludingTax)
			dto.LineSubtotalExcludingTax = &value
		}
		if line.LineTax != nil {
			dto.LineTax = &offerLineTaxDTO{TaxType: string(line.LineTax.TaxType), Exempt: line.LineTax.Exempt,
				RegistrationNumber: line.LineTax.RegistrationNumber, BasisNote: line.LineTax.BasisNote}
			if line.LineTax.RateBPS != nil {
				rate := int64(*line.LineTax.RateBPS)
				dto.LineTax.RateBPS = &rate
			}
		}
		lines = append(lines, dto)
	}
	tax := offerTaxDTO{Mode: string(v.Tax.Mode)}
	if v.Tax.OfferLevel != nil {
		tax.OfferLevel = &offerLevelTaxDTO{TaxType: string(v.Tax.OfferLevel.TaxType),
			TaxAmount: toOfferMoneyDTO(v.Tax.OfferLevel.TaxAmount), BasisNote: v.Tax.OfferLevel.BasisNote,
			RegistrationNumber: v.Tax.OfferLevel.RegistrationNumber}
	}
	groups := make([]offerChargeGroupDTO, 0, len(v.ChargeGroups))
	for _, group := range v.ChargeGroups {
		dto := offerChargeGroupDTO{ID: group.ID, Name: group.Name, Description: group.Description,
			ApplicableRFQLineIDs: group.ApplicableRFQLineIDs, Trigger: string(group.Trigger), Calculation: string(group.Calculation)}
		if group.Threshold != nil {
			x := toOfferMoneyDTO(*group.Threshold)
			dto.Threshold = &x
		}
		if group.FixedAmount != nil {
			x := toOfferMoneyDTO(*group.FixedAmount)
			dto.FixedAmount = &x
		}
		if group.RateBPS != nil {
			x := int64(*group.RateBPS)
			dto.RateBPS = &x
		}
		groups = append(groups, dto)
	}
	var delivery *offerDeliveryChargeDTO
	if v.DeliveryCharge != nil {
		delivery = &offerDeliveryChargeDTO{Amount: toOfferMoneyDTO(v.DeliveryCharge.Amount)}
	}
	var withdrawal *offerWithdrawalProjectionDTO
	if projection.Withdrawal != nil {
		withdrawal = &offerWithdrawalProjectionDTO{WithdrawnAt: projection.Withdrawal.WithdrawnAt, Reason: projection.Withdrawal.Reason}
	}
	return map[string]any{"id": v.ID, "issuedRfqVersionId": v.IssuedRFQVersionID, "versionNumber": v.VersionNumber,
		"currency": v.Currency, "submittedAt": v.SubmittedAt, "offerValidUntil": v.OfferValidUntil,
		"publicStatus": projection.PublicStatus, "isSuperseded": projection.IsSuperseded, "canWithdraw": projection.CanWithdraw,
		"lines": lines, "tax": tax, "chargeGroups": groups, "deliveryCharge": delivery,
		"quotedLineSubtotal": toOfferMoneyDTO(v.QuotedLineSubtotal), "quotedTaxTotal": toOfferMoneyDTO(v.QuotedTaxTotal),
		"fullOfferChargeTotal": toOfferMoneyDTO(v.FullOfferChargeTotal), "deliveryChargeTotal": toOfferMoneyDTO(v.DeliveryChargeTotal),
		"grandTotal": toOfferMoneyDTO(v.GrandTotal), "supplierNotes": v.SupplierNotes, "withdrawal": withdrawal}
}
