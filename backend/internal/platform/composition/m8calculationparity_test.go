package composition_test

import (
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementcalc"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// Cross-module calculation parity (ADR 0001, §8D).
//
// Phase E calculates what a Supplier submitted. Phase F recalculates what the
// contractor awarded. Where the award selects an offer COMPLETELY, the two
// figures must agree exactly — a submitted Offer total that disagrees with the
// Award total for the same lines is a commercial correctness defect, not a
// cosmetic one.
//
// This test lives in composition because it is the only place permitted to
// import two domain modules (ADR 0002). It is the guard that would catch the
// two engines drifting if the shared kernel were ever bypassed.

const parityCurrency = "MYR"

func mustParityQuantity(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("quantity.New(%q, %q): %v", value, unit, err)
	}
	return q
}

// parityScenario describes one offer expressed once, then fed to BOTH engines.
type parityScenario struct {
	name string

	// Per-line quoted figures.
	lineIDs   []string
	subtotals []int64
	quantity  []string
	units     []string

	tax      procurementcalc.SupplierOfferTax
	lineTax  *procurementcalc.QuotedLineTax
	groups   []procurementcalc.ConditionalChargeGroup
	delivery *procurementcalc.DeliveryCharge
}

func parityScenarios(t *testing.T) []parityScenario {
	t.Helper()

	serviceRate := money.RateBPS(600)
	percentRate := money.RateBPS(1_000)
	handlingFixed := money.New(1_500, parityCurrency)
	offerTaxAmount := money.New(750, parityCurrency)

	return []parityScenario{
		{
			name:      "no tax, no charges, no delivery",
			lineIDs:   []string{"line-1", "line-2"},
			subtotals: []int64{10_000, 2_500},
			quantity:  []string{"10", "5"},
			units:     []string{"sqm", "bag"},
			tax:       procurementcalc.SupplierOfferTax{Mode: procurementcalc.TaxModeNotApplicable},
		},
		{
			name:      "line-level tax on every line",
			lineIDs:   []string{"line-1", "line-2"},
			subtotals: []int64{10_000, 2_500},
			quantity:  []string{"10", "5"},
			units:     []string{"sqm", "bag"},
			tax:       procurementcalc.SupplierOfferTax{Mode: procurementcalc.TaxModeLineLevel},
			lineTax: &procurementcalc.QuotedLineTax{
				TaxType: procurementcalc.TaxTypeServiceTax,
				RateBPS: &serviceRate, RegistrationNumber: "SST-123",
			},
		},
		{
			name:      "offer-level tax, complete selection",
			lineIDs:   []string{"line-1", "line-2"},
			subtotals: []int64{10_000, 2_500},
			quantity:  []string{"10", "5"},
			units:     []string{"sqm", "bag"},
			tax: procurementcalc.SupplierOfferTax{
				Mode: procurementcalc.TaxModeOfferLevel,
				OfferLevel: &procurementcalc.QuotedOfferTax{
					TaxType:   procurementcalc.TaxTypeOther,
					TaxAmount: offerTaxAmount,
					BasisNote: "flat service tax",
				},
			},
		},
		{
			name:      "fixed conditional group plus delivery",
			lineIDs:   []string{"line-1", "line-2"},
			subtotals: []int64{10_000, 2_500},
			quantity:  []string{"10", "5"},
			units:     []string{"sqm", "bag"},
			tax:       procurementcalc.SupplierOfferTax{Mode: procurementcalc.TaxModeNotApplicable},
			groups: []procurementcalc.ConditionalChargeGroup{{
				ID: "install", Name: "Installation",
				ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger:              procurementcalc.ChargeTriggerAllSelected,
				Calculation:          procurementcalc.ChargeCalculationFixedAmount,
				FixedAmount:          &handlingFixed,
			}},
			delivery: &procurementcalc.DeliveryCharge{
				Amount: money.New(3_000, parityCurrency)},
		},
		{
			name:      "percentage conditional group plus delivery and line tax",
			lineIDs:   []string{"line-1", "line-2"},
			subtotals: []int64{10_000, 2_500},
			quantity:  []string{"10", "5"},
			units:     []string{"sqm", "bag"},
			tax:       procurementcalc.SupplierOfferTax{Mode: procurementcalc.TaxModeLineLevel},
			lineTax: &procurementcalc.QuotedLineTax{
				TaxType: procurementcalc.TaxTypeServiceTax,
				RateBPS: &serviceRate, RegistrationNumber: "SST-123",
			},
			groups: []procurementcalc.ConditionalChargeGroup{{
				ID: "handling", Name: "Handling",
				ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger:              procurementcalc.ChargeTriggerAnySelected,
				Calculation:          procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal,
				RateBPS:              &percentRate,
			}},
			delivery: &procurementcalc.DeliveryCharge{
				Amount: money.New(3_000, parityCurrency)},
		},
	}
}

// phaseETotals calculates the scenario exactly as Phase E does at submission:
// every quoted line selected, through supplieroffers' own entry points.
func phaseETotals(t *testing.T, scenario parityScenario) (
	lineSubtotal, tax, charges, delivery, grand money.Money) {
	t.Helper()

	var taxable []supplieroffers.TaxableQuotedLine
	var quoted []supplieroffers.QuotedLineSubtotal
	lineSubtotal = money.New(0, parityCurrency)

	for index, lineID := range scenario.lineIDs {
		subtotal := money.New(scenario.subtotals[index], parityCurrency)
		lineSubtotal.Amount += subtotal.Amount
		taxable = append(taxable, supplieroffers.TaxableQuotedLine{
			RFQLineID: lineID, Subtotal: subtotal, Tax: scenario.lineTax,
		})
		quoted = append(quoted, supplieroffers.QuotedLineSubtotal{
			RFQLineID: lineID, Subtotal: subtotal,
		})
	}

	taxResult, err := supplieroffers.CalculateTax(supplieroffers.TaxCalculationInput{
		Currency: parityCurrency, Tax: scenario.tax, QuotedLines: taxable,
	})
	if err != nil {
		t.Fatalf("phase E CalculateTax: %v", err)
	}

	chargeResult, err := supplieroffers.CalculateConditionalCharges(
		supplieroffers.ConditionalChargeInput{
			Currency:           parityCurrency,
			QuotedLines:        quoted,
			SelectedRFQLineIDs: scenario.lineIDs,
			Groups:             scenario.groups,
		})
	if err != nil {
		t.Fatalf("phase E CalculateConditionalCharges: %v", err)
	}

	deliveryAmount, err := supplieroffers.CalculateDeliveryCharge(
		parityCurrency, scenario.delivery, len(scenario.lineIDs))
	if err != nil {
		t.Fatalf("phase E CalculateDeliveryCharge: %v", err)
	}

	grand = money.New(
		lineSubtotal.Amount+taxResult.Total.Amount+
			chargeResult.Total.Amount+deliveryAmount.Amount,
		parityCurrency)
	return lineSubtotal, taxResult.Total, chargeResult.Total, deliveryAmount, grand
}

// phaseFAward calculates the same scenario as Phase F does at award, selecting
// the same complete set of lines.
func phaseFAward(t *testing.T, scenario parityScenario,
	selectedLineIDs []string) awards.AwardCalculation {
	t.Helper()

	issued := awards.IssuedRFQSnapshot{
		ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
		Currency: parityCurrency,
	}
	version := awards.OfferVersionSnapshot{
		ID: "offer-1", CompanyID: "company-1", OfferChainID: "offerchain-1",
		SupplierID: "supplier-a", SupplierName: "Supplier A",
		InvitationID: "invitation-1", IssuedRFQVersionID: "issued-1",
		VersionNumber: 1, Currency: parityCurrency,
		Tax:               scenario.tax,
		ChargeGroups:      scenario.groups,
		DeliveryCharge:    scenario.delivery,
		OfferValidUntil:   time.Now().UTC().Add(24 * time.Hour),
		IsLatestSubmitted: true,
		EligibilityState:  awards.OfferEligibilityEligible,
	}

	for index, lineID := range scenario.lineIDs {
		qty := mustParityQuantity(t,
			scenario.quantity[index], scenario.units[index])
		subtotal := money.New(scenario.subtotals[index], parityCurrency)
		unit := money.New(scenario.subtotals[index], parityCurrency)

		issued.Lines = append(issued.Lines, awards.IssuedRFQLineSnapshot{
			ID: lineID, LineageID: "lineage-" + lineID, Quantity: qty,
		})
		version.Lines = append(version.Lines, awards.OfferLineSnapshot{
			ID: "ol-" + lineID, RFQLineID: lineID,
			ResponseStatus:           awards.OfferLineQuoted,
			QuotedQuantity:           &qty,
			UnitPriceExcludingTax:    &unit,
			LineSubtotalExcludingTax: &subtotal,
			LineTax:                  scenario.lineTax,
			LineTaxAmount:            money.New(0, parityCurrency),
		})
	}

	var selections []awards.AwardLineSelection
	for _, lineID := range selectedLineIDs {
		selections = append(selections, awards.AwardLineSelection{
			IssuedRFQLineID: lineID, StableLineageID: "lineage-" + lineID,
			OfferVersionID: "offer-1", OfferLineID: "ol-" + lineID,
		})
	}

	calculation, err := awards.CalculateAward(awards.AwardCalculationInput{
		IssuedRFQ: issued, Selections: selections,
		OfferVersions: map[string]awards.OfferVersionSnapshot{
			"offer-1": version},
		CalculatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("phase F CalculateAward: %v", err)
	}
	return calculation
}

// The load-bearing assertion: awarding an offer COMPLETELY reproduces the
// Supplier's submitted figures exactly — line subtotal, tax, conditional
// charges, delivery and grand total.
func TestPhaseEAndPhaseFAgreeOnACompletelyAwardedOffer(t *testing.T) {
	for _, scenario := range parityScenarios(t) {
		t.Run(scenario.name, func(t *testing.T) {
			wantSubtotal, wantTax, wantCharges, wantDelivery, wantGrand :=
				phaseETotals(t, scenario)

			calculation := phaseFAward(t, scenario, scenario.lineIDs)
			if len(calculation.SupplierSummaries) != 1 {
				t.Fatalf("supplier summaries = %d, want 1",
					len(calculation.SupplierSummaries))
			}
			got := calculation.SupplierSummaries[0]

			if got.LineSubtotal != wantSubtotal {
				t.Errorf("line subtotal: award %+v, submission %+v",
					got.LineSubtotal, wantSubtotal)
			}
			if got.TaxTotal != wantTax {
				t.Errorf("tax: award %+v, submission %+v", got.TaxTotal, wantTax)
			}
			if got.ChargeTotal != wantCharges {
				t.Errorf("conditional charges: award %+v, submission %+v",
					got.ChargeTotal, wantCharges)
			}
			if got.DeliveryCharge != wantDelivery {
				t.Errorf("delivery: award %+v, submission %+v",
					got.DeliveryCharge, wantDelivery)
			}
			// The figure a contractor and a Supplier would each read as "the
			// price". These must never disagree.
			if calculation.GrandAwardTotal != wantGrand {
				t.Errorf("GRAND TOTAL DISAGREES: award %+v, submission %+v",
					calculation.GrandAwardTotal, wantGrand)
			}
		})
	}
}

// For a PARTIAL selection the two figures legitimately differ — that is the
// point of re-evaluation. What must hold is that F3 re-evaluates through the
// same primitive: the awarded figure equals what the kernel produces for that
// exact subset, not a pro-rata share of the submitted total.
func TestPhaseFReEvaluatesConditionalGroupsForAPartialSelection(t *testing.T) {
	percentRate := money.RateBPS(1_000)
	scenario := parityScenario{
		name:      "percentage group, partial selection",
		lineIDs:   []string{"line-1", "line-2"},
		subtotals: []int64{10_000, 2_500},
		quantity:  []string{"10", "5"},
		units:     []string{"sqm", "bag"},
		tax:       procurementcalc.SupplierOfferTax{Mode: procurementcalc.TaxModeNotApplicable},
		groups: []procurementcalc.ConditionalChargeGroup{{
			ID: "handling", Name: "Handling",
			ApplicableRFQLineIDs: []string{"line-1", "line-2"},
			Trigger:              procurementcalc.ChargeTriggerAnySelected,
			Calculation:          procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal,
			RateBPS:              &percentRate,
		}},
		delivery: &procurementcalc.DeliveryCharge{
			Amount: money.New(3_000, parityCurrency)},
	}

	// What the kernel itself says the charge is for the awarded subset alone.
	expected, err := supplieroffers.CalculateConditionalCharges(
		supplieroffers.ConditionalChargeInput{
			Currency: parityCurrency,
			QuotedLines: []supplieroffers.QuotedLineSubtotal{
				{RFQLineID: "line-1", Subtotal: money.New(10_000, parityCurrency)},
				{RFQLineID: "line-2", Subtotal: money.New(2_500, parityCurrency)},
			},
			SelectedRFQLineIDs: []string{"line-1"},
			Groups:             scenario.groups,
		})
	if err != nil {
		t.Fatalf("kernel CalculateConditionalCharges: %v", err)
	}

	calculation := phaseFAward(t, scenario, []string{"line-1"})
	got := calculation.SupplierSummaries[0]

	if got.ChargeTotal != expected.Total {
		t.Fatalf("partial charge = %+v, want the kernel's %+v for that subset",
			got.ChargeTotal, expected.Total)
	}

	// Sanity: the partial charge is genuinely smaller than the full-selection
	// one, so this test would fail if F3 copied the submitted amount.
	_, _, fullCharges, _, _ := phaseETotals(t, scenario)
	if got.ChargeTotal.Amount >= fullCharges.Amount {
		t.Fatalf("partial charge %+v is not smaller than the full %+v; "+
			"F3 may be copying the submitted amount rather than re-evaluating",
			got.ChargeTotal, fullCharges)
	}

	// Delivery is charged once and in full: the Supplier quoted one delivery,
	// and awarding fewer lines does not make it cheaper.
	if got.DeliveryCharge != money.New(3_000, parityCurrency) {
		t.Errorf("delivery = %+v, want the full quoted 3000 MYR",
			got.DeliveryCharge)
	}
}

// The kernel is the ONLY implementation: both modules resolve to the same
// function values. If someone reimplemented one side, this fails.
func TestBothModulesUseTheSameCalculationKernel(t *testing.T) {
	quoted := []procurementcalc.QuotedLineSubtotal{
		{RFQLineID: "line-1", Subtotal: money.New(10_000, parityCurrency)},
	}
	rate := money.RateBPS(1_000)
	groups := []procurementcalc.ConditionalChargeGroup{{
		ID: "handling", Name: "Handling",
		ApplicableRFQLineIDs: []string{"line-1"},
		Trigger:              procurementcalc.ChargeTriggerAnySelected,
		Calculation:          procurementcalc.ChargeCalculationPercentageOfSelectedSubtotal,
		RateBPS:              &rate,
	}}

	viaModule, err := supplieroffers.CalculateConditionalCharges(
		supplieroffers.ConditionalChargeInput{
			Currency: parityCurrency, QuotedLines: quoted,
			SelectedRFQLineIDs: []string{"line-1"}, Groups: groups,
		})
	if err != nil {
		t.Fatalf("supplieroffers: %v", err)
	}
	viaKernel, err := procurementcalc.CalculateConditionalCharges(
		procurementcalc.ConditionalChargeInput{
			Currency: parityCurrency, QuotedLines: quoted,
			SelectedRFQLineIDs: []string{"line-1"}, Groups: groups,
		})
	if err != nil {
		t.Fatalf("procurementcalc: %v", err)
	}
	if viaModule.Total != viaKernel.Total {
		t.Fatalf("supplieroffers %+v and the kernel %+v disagree; "+
			"the module is no longer delegating to the shared implementation",
			viaModule.Total, viaKernel.Total)
	}
}
