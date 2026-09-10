package awards

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// Supplier outcomes and the privacy boundary (§8H).
//
// Comparison (F1) is contractor-facing and shows every Supplier's figures —
// that is its purpose. The privacy boundary runs the OPPOSITE way here: an
// outcome is Supplier-facing, and no Supplier may ever learn who else bid, what
// they charged, how many responded, or why a line went unbought.
//
// The projection is FROZEN at generation from the revision. A stored snapshot
// cannot drift, and it means a later correction cannot retroactively change
// what a Supplier was already told.

const AwardOutcomeSchemaVersion = 1

// Phase1AwardCurrency is the kernel's currency, aliased rather than redeclared
// so awards cannot drift from what Phase E accepts.
const Phase1AwardCurrency = procurementcalc.Phase1Currency

// The acknowledgement wording is fixed in one place so no caller can soften it.
// A Supplier could otherwise read an award as a Purchase Order, which it is
// explicitly not (§8J).
const outcomeNextSteps = "This award records our sourcing decision. It is not " +
	"a Purchase Order and creates no contractual commitment. Acknowledging " +
	"this outcome confirms receipt only."

type OutcomeResult string

const (
	OutcomeSelected     OutcomeResult = "selected"
	OutcomeUnsuccessful OutcomeResult = "unsuccessful"
)

// OutcomeParticipant is one Supplier holding an eligible submitted Offer
// Version against the issued RFQ version, whether or not they won.
type OutcomeParticipant struct {
	SupplierID     string
	SupplierName   string
	InvitationID   string
	OfferVersionID string

	// Withdrawn Suppliers receive no outcome: they removed themselves from
	// consideration before publication.
	Withdrawn bool
}

// OutcomeAwardedLine is a Supplier's OWN awarded line. It deliberately omits
// every field that could identify or price a competitor.
type OutcomeAwardedLine struct {
	IssuedRFQLineID string
	MaterialName    string
	Quantity        quantity.Quantity
	UnitPrice       money.Money
	LineSubtotal    money.Money
	LineTaxAmount   money.Money
	Brand           string
	SKU             string
	LeadTime        string
}

// OutcomeProjection is the frozen Supplier-safe snapshot.
//
// RespondentCount exists ONLY to be asserted zero. A count of respondents is
// itself commercially sensitive — it tells a Supplier how much competition they
// faced — so this field must never be populated.
type OutcomeProjection struct {
	Result OutcomeResult

	RFQNumber string
	RFQTitle  string

	AwardedLines   []OutcomeAwardedLine
	LineSubtotal   money.Money
	TaxTotal       money.Money
	ChargeTotal    money.Money
	DeliveryCharge money.Money
	AwardTotal     money.Money

	ContractorMessage string
	NextSteps         string

	RespondentCount int
}

type AwardOutcome struct {
	ID                 string
	CompanyID          string
	AwardChainID       string
	AwardRevisionID    string
	RFQChainID         string
	IssuedRFQVersionID string

	// Scoped to Supplier + Invitation (D3), never to the recipient identity
	// that submitted the offer: a replacement recipient may read the outcome
	// without gaining any access to the previous recipient's draft.
	SupplierID   string
	InvitationID string

	Result        OutcomeResult
	Projection    OutcomeProjection
	CreatedAt     time.Time
	SchemaVersion int
}

type OutcomeGenerationInput struct {
	Revision          AwardRevision
	Participants      []OutcomeParticipant
	RFQNumber         string
	RFQTitle          string
	ContractorMessage string
	GeneratedAt       time.Time
}

// GenerateOutcomes produces one frozen outcome per participating Supplier.
func GenerateOutcomes(
	input OutcomeGenerationInput,
) ([]AwardOutcome, error) {
	revision := input.Revision
	currency := revision.GrandAwardTotal.Currency
	if currency == "" {
		currency = Phase1AwardCurrency
	}

	// Index the revision by Supplier so each projection is built from that
	// Supplier's own rows only. Nothing else is ever read into a projection.
	linesBySupplier := map[string][]OutcomeAwardedLine{}
	for _, line := range revision.AwardedLines {
		linesBySupplier[line.SupplierID] = append(
			linesBySupplier[line.SupplierID], OutcomeAwardedLine{
				IssuedRFQLineID: line.IssuedRFQLineID,
				MaterialName:    line.MaterialName,
				Quantity:        line.Quantity,
				UnitPrice:       line.UnitPriceExcludingTax,
				LineSubtotal:    line.LineSubtotal,
				LineTaxAmount:   line.LineTaxAmount,
				Brand:           line.Brand,
				SKU:             line.SKU,
				LeadTime:        line.LeadTime,
			})
	}
	summaryBySupplier := map[string]AwardSupplierSummary{}
	for _, summary := range revision.SupplierSummaries {
		summaryBySupplier[summary.SupplierID] = summary
	}

	outcomes := make([]AwardOutcome, 0, len(input.Participants))
	for _, participant := range input.Participants {
		// A Supplier who withdrew before publication receives none.
		if participant.Withdrawn {
			continue
		}

		awarded := linesBySupplier[participant.SupplierID]
		result := OutcomeUnsuccessful
		if len(awarded) > 0 {
			result = OutcomeSelected
		}

		projection := OutcomeProjection{
			Result:    result,
			RFQNumber: input.RFQNumber,
			RFQTitle:  input.RFQTitle,
			// Zero money in the RFQ's currency, so an unsuccessful projection
			// carries a well-formed absence rather than an empty struct.
			LineSubtotal:      money.New(0, currency),
			TaxTotal:          money.New(0, currency),
			ChargeTotal:       money.New(0, currency),
			DeliveryCharge:    money.New(0, currency),
			AwardTotal:        money.New(0, currency),
			ContractorMessage: input.ContractorMessage,
		}

		if result == OutcomeSelected {
			// Sorted so a regenerated outcome is byte-identical to the first.
			sort.Slice(awarded, func(i, j int) bool {
				return awarded[i].IssuedRFQLineID < awarded[j].IssuedRFQLineID
			})
			projection.AwardedLines = awarded

			// This Supplier's OWN total, never the grand award total, which
			// would disclose the size of the whole award.
			summary := summaryBySupplier[participant.SupplierID]
			projection.LineSubtotal = summary.LineSubtotal
			projection.TaxTotal = summary.TaxTotal
			projection.ChargeTotal = summary.ChargeTotal
			projection.DeliveryCharge = summary.DeliveryCharge
			projection.AwardTotal = summary.SupplierTotal
			projection.NextSteps = outcomeNextSteps
		}

		outcomes = append(outcomes, AwardOutcome{
			CompanyID:          revision.CompanyID,
			AwardChainID:       revision.AwardChainID,
			AwardRevisionID:    revision.ID,
			RFQChainID:         revision.RFQChainID,
			IssuedRFQVersionID: revision.IssuedRFQVersionID,
			SupplierID:         participant.SupplierID,
			InvitationID:       participant.InvitationID,
			Result:             result,
			Projection:         projection,
			CreatedAt:          input.GeneratedAt,
			SchemaVersion:      AwardOutcomeSchemaVersion,
		})
	}

	// Deterministic order so concurrent generation converges on the same set.
	sort.Slice(outcomes, func(i, j int) bool {
		return outcomes[i].SupplierID < outcomes[j].SupplierID
	})
	return outcomes, nil
}

// renderProjection flattens a projection to text.
//
// It exists so a leakage test can assert over EVERYTHING the projection could
// carry, rather than over the handful of fields a reviewer happened to think
// of. A new field that leaks a competitor's name fails the test automatically.
func renderProjection(projection OutcomeProjection) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d",
		projection.Result, projection.RFQNumber, projection.RFQTitle,
		projection.ContractorMessage, projection.NextSteps,
		projection.LineSubtotal.Amount, projection.TaxTotal.Amount,
		projection.ChargeTotal.Amount, projection.DeliveryCharge.Amount,
		projection.AwardTotal.Amount, projection.RespondentCount)
	for _, line := range projection.AwardedLines {
		fmt.Fprintf(&builder, "|%s|%s|%s|%s|%s|%d|%d|%d",
			line.IssuedRFQLineID, line.MaterialName, line.Brand, line.SKU,
			line.LeadTime, line.UnitPrice.Amount, line.LineSubtotal.Amount,
			line.LineTaxAmount.Amount)
	}
	return builder.String()
}
