package procurementcalc

import (
	"errors"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestCalculateConditionalChargesAnySelectedAppliesFixedAmountOnce(t *testing.T) {
	fixed := money.New(15_000, "MYR")
	result, err := CalculateConditionalCharges(ConditionalChargeInput{
		Currency: "MYR",
		QuotedLines: []QuotedLineSubtotal{
			{RFQLineID: "cabinets", Subtotal: money.New(100_000, "MYR")},
			{RFQLineID: "countertop", Subtotal: money.New(50_000, "MYR")},
			{RFQLineID: "sink", Subtotal: money.New(20_000, "MYR")},
		},
		SelectedRFQLineIDs: []string{"cabinets"},
		Groups: []ConditionalChargeGroup{{
			ID:                   "handling",
			Name:                 "Delivery handling",
			ApplicableRFQLineIDs: []string{"cabinets", "countertop"},
			Trigger:              ChargeTriggerAnySelected,
			Calculation:          ChargeCalculationFixedAmount,
			FixedAmount:          &fixed,
		}},
	})
	if err != nil {
		t.Fatalf("CalculateConditionalCharges returned an unexpected error: %v", err)
	}
	if result.Total != money.New(15_000, "MYR") {
		t.Fatalf("charge total = %+v, want MYR 150.00 once", result.Total)
	}
	if len(result.Groups) != 1 || !result.Groups[0].Triggered {
		t.Fatalf("group results = %+v, want the handling group triggered", result.Groups)
	}
}

func TestCalculateConditionalChargesRejectsInvalidGroups(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ConditionalChargeInput)
	}{
		{
			name: "unsupported currency",
			mutate: func(input *ConditionalChargeInput) {
				input.Currency = "USD"
				input.Groups[0].FixedAmount = moneyPointer(money.New(100, "USD"))
			},
		},
		{
			name: "group requires line",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].ApplicableRFQLineIDs = nil
			},
		},
		{
			name: "duplicate line inside group",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].ApplicableRFQLineIDs = []string{"line-1", "line-1"}
			},
		},
		{
			name: "line overlaps groups",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups = append(input.Groups, ConditionalChargeGroup{
					ID: "group-2", Name: "Second",
					ApplicableRFQLineIDs: []string{"line-1"},
					Trigger:              ChargeTriggerAnySelected, Calculation: ChargeCalculationFixedAmount,
					FixedAmount: moneyPointer(money.New(100, "MYR")),
				})
			},
		},
		{
			name: "unknown group line",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].ApplicableRFQLineIDs = []string{"unknown"}
			},
		},
		{
			name: "unknown selected line",
			mutate: func(input *ConditionalChargeInput) {
				input.SelectedRFQLineIDs = []string{"unknown"}
			},
		},
		{
			name: "duplicate quoted line",
			mutate: func(input *ConditionalChargeInput) {
				input.QuotedLines = append(input.QuotedLines, input.QuotedLines[0])
			},
		},
		{
			name: "quoted subtotal currency mismatch",
			mutate: func(input *ConditionalChargeInput) {
				input.QuotedLines[0].Subtotal = money.New(1_000, "USD")
			},
		},
		{
			name: "blank group name",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Name = " "
			},
		},
		{
			name: "group name too long",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Name = strings.Repeat("n", MaxConditionalChargeNameLength+1)
			},
		},
		{
			name: "group description too long",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Description =
					strings.Repeat("d", MaxConditionalChargeDescriptionLength+1)
			},
		},
		{
			name: "unknown trigger",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Trigger = "sometimes"
			},
		},
		{
			name: "threshold required",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Trigger = ChargeTriggerSelectedSubtotalAtLeast
			},
		},
		{
			name: "threshold must be positive",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Trigger = ChargeTriggerSelectedSubtotalAtLeast
				input.Groups[0].Threshold = moneyPointer(money.New(0, "MYR"))
			},
		},
		{
			name: "threshold currency mismatch",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Trigger = ChargeTriggerSelectedSubtotalAtLeast
				input.Groups[0].Threshold = moneyPointer(money.New(100, "USD"))
			},
		},
		{
			name: "threshold forbidden for any selected",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Threshold = moneyPointer(money.New(100, "MYR"))
			},
		},
		{
			name: "fixed amount required",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].FixedAmount = nil
			},
		},
		{
			name: "fixed amount must be positive",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].FixedAmount = moneyPointer(money.New(0, "MYR"))
			},
		},
		{
			name: "fixed amount currency mismatch",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].FixedAmount = moneyPointer(money.New(100, "USD"))
			},
		},
		{
			name: "fixed calculation forbids rate",
			mutate: func(input *ConditionalChargeInput) {
				rate := money.RateBPS(500)
				input.Groups[0].RateBPS = &rate
			},
		},
		{
			name: "percentage requires rate",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Calculation = ChargeCalculationPercentageOfSelectedSubtotal
				input.Groups[0].FixedAmount = nil
			},
		},
		{
			name: "percentage rate must be in range",
			mutate: func(input *ConditionalChargeInput) {
				rate := money.RateBPS(10_001)
				input.Groups[0].Calculation = ChargeCalculationPercentageOfSelectedSubtotal
				input.Groups[0].FixedAmount = nil
				input.Groups[0].RateBPS = &rate
			},
		},
		{
			name: "percentage forbids fixed amount",
			mutate: func(input *ConditionalChargeInput) {
				rate := money.RateBPS(500)
				input.Groups[0].Calculation = ChargeCalculationPercentageOfSelectedSubtotal
				input.Groups[0].RateBPS = &rate
			},
		},
		{
			name: "unknown calculation",
			mutate: func(input *ConditionalChargeInput) {
				input.Groups[0].Calculation = "tiered"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := validConditionalChargeInput()
			tt.mutate(&input)

			_, err := CalculateConditionalCharges(input)
			if !errors.Is(err, ErrInvalidConditionalCharge) {
				t.Fatalf("CalculateConditionalCharges error = %v, want ErrInvalidConditionalCharge", err)
			}
		})
	}
}

func validConditionalChargeInput() ConditionalChargeInput {
	return ConditionalChargeInput{
		Currency: "MYR",
		QuotedLines: []QuotedLineSubtotal{
			{RFQLineID: "line-1", Subtotal: money.New(1_000, "MYR")},
			{RFQLineID: "line-2", Subtotal: money.New(2_000, "MYR")},
		},
		SelectedRFQLineIDs: []string{"line-1"},
		Groups: []ConditionalChargeGroup{{
			ID: "group-1", Name: "Handling",
			ApplicableRFQLineIDs: []string{"line-1"},
			Trigger:              ChargeTriggerAnySelected, Calculation: ChargeCalculationFixedAmount,
			FixedAmount: moneyPointer(money.New(100, "MYR")),
		}},
	}
}

func moneyPointer(value money.Money) *money.Money {
	return &value
}

func TestCalculateConditionalChargesTriggerAndCalculationSemantics(t *testing.T) {
	fixed := money.New(15_000, "MYR")
	threshold := money.New(15_000, "MYR")
	rate := money.RateBPS(5000)
	quoted := []QuotedLineSubtotal{
		{RFQLineID: "line-1", Subtotal: money.New(10_000, "MYR")},
		{RFQLineID: "line-2", Subtotal: money.New(5_000, "MYR")},
		{RFQLineID: "unrelated", Subtotal: money.New(100_000, "MYR")},
	}

	tests := []struct {
		name      string
		selected  []string
		group     ConditionalChargeGroup
		triggered bool
		subtotal  money.Money
		amount    money.Money
	}{
		{
			name:     "all selected remains false for partial group",
			selected: []string{"line-1"},
			group: ConditionalChargeGroup{
				ID: "group", Name: "all", ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger: ChargeTriggerAllSelected, Calculation: ChargeCalculationFixedAmount,
				FixedAmount: &fixed,
			},
			triggered: false,
			subtotal:  money.New(10_000, "MYR"),
			amount:    money.New(0, "MYR"),
		},
		{
			name:     "all selected applies once",
			selected: []string{"line-1", "line-2"},
			group: ConditionalChargeGroup{
				ID: "group", Name: "all", ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger: ChargeTriggerAllSelected, Calculation: ChargeCalculationFixedAmount,
				FixedAmount: &fixed,
			},
			triggered: true,
			subtotal:  money.New(15_000, "MYR"),
			amount:    money.New(15_000, "MYR"),
		},
		{
			name:     "threshold triggers at exact boundary",
			selected: []string{"line-1", "line-2"},
			group: ConditionalChargeGroup{
				ID: "group", Name: "threshold", ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger: ChargeTriggerSelectedSubtotalAtLeast, Threshold: &threshold,
				Calculation: ChargeCalculationFixedAmount, FixedAmount: &fixed,
			},
			triggered: true,
			subtotal:  money.New(15_000, "MYR"),
			amount:    money.New(15_000, "MYR"),
		},
		{
			name:     "percentage uses selected group subtotal only and rounds",
			selected: []string{"line-1", "unrelated"},
			group: ConditionalChargeGroup{
				ID: "group", Name: "percentage", ApplicableRFQLineIDs: []string{"line-1", "line-2"},
				Trigger:     ChargeTriggerAnySelected,
				Calculation: ChargeCalculationPercentageOfSelectedSubtotal, RateBPS: &rate,
			},
			triggered: true,
			subtotal:  money.New(10_000, "MYR"),
			amount:    money.New(5_000, "MYR"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CalculateConditionalCharges(ConditionalChargeInput{
				Currency:           "MYR",
				QuotedLines:        quoted,
				SelectedRFQLineIDs: tt.selected,
				Groups:             []ConditionalChargeGroup{tt.group},
			})
			if err != nil {
				t.Fatalf("CalculateConditionalCharges returned an unexpected error: %v", err)
			}
			if len(result.Groups) != 1 {
				t.Fatalf("group count = %d, want 1", len(result.Groups))
			}
			got := result.Groups[0]
			if got.Triggered != tt.triggered || got.GroupSubtotal != tt.subtotal ||
				got.Amount != tt.amount {
				t.Fatalf("group result = %+v, want triggered=%v subtotal=%+v amount=%+v",
					got, tt.triggered, tt.subtotal, tt.amount)
			}
			if result.Total != tt.amount {
				t.Fatalf("total = %+v, want %+v", result.Total, tt.amount)
			}
		})
	}
}
