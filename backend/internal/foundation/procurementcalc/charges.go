package procurementcalc

import (
	"strings"
	"unicode/utf8"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

const (
	MaxConditionalChargeNameLength        = 120
	MaxConditionalChargeDescriptionLength = 500
)

type ChargeTrigger string

const (
	ChargeTriggerAnySelected             ChargeTrigger = "any_selected"
	ChargeTriggerAllSelected             ChargeTrigger = "all_selected"
	ChargeTriggerSelectedSubtotalAtLeast ChargeTrigger = "selected_subtotal_at_least"
)

type ChargeCalculation string

const (
	ChargeCalculationFixedAmount                  ChargeCalculation = "fixed_amount"
	ChargeCalculationPercentageOfSelectedSubtotal ChargeCalculation = "percentage_of_selected_subtotal"
)

// ConditionalChargeGroup stores a Supplier-authored rule, never a
// browser-calculated charge total.
type ConditionalChargeGroup struct {
	ID                   string            `bson:"id"`
	Name                 string            `bson:"name"`
	Description          string            `bson:"description,omitempty"`
	ApplicableRFQLineIDs []string          `bson:"applicableRFQLineIds"`
	Trigger              ChargeTrigger     `bson:"trigger"`
	Threshold            *money.Money      `bson:"threshold,omitempty"`
	Calculation          ChargeCalculation `bson:"calculation"`
	FixedAmount          *money.Money      `bson:"fixedAmount,omitempty"`
	RateBPS              *money.RateBPS    `bson:"rateBPS,omitempty"`
}

type QuotedLineSubtotal struct {
	RFQLineID string
	Subtotal  money.Money
}

type ConditionalChargeInput struct {
	Currency           string
	QuotedLines        []QuotedLineSubtotal
	SelectedRFQLineIDs []string
	Groups             []ConditionalChargeGroup
}

type CalculatedConditionalCharge struct {
	GroupID       string
	Triggered     bool
	GroupSubtotal money.Money
	Amount        money.Money
}

type ConditionalChargeResult struct {
	Groups []CalculatedConditionalCharge
	Total  money.Money
}

// CalculateConditionalCharges evaluates each immutable rule against only the
// exact selected quoted lines. Tax, delivery and other groups cannot enter the
// group subtotal through this input shape.
func CalculateConditionalCharges(input ConditionalChargeInput) (ConditionalChargeResult, error) {
	if err := validateConditionalChargeInput(input); err != nil {
		return ConditionalChargeResult{}, err
	}

	quoted := make(map[string]money.Money, len(input.QuotedLines))
	for _, line := range input.QuotedLines {
		quoted[line.RFQLineID] = line.Subtotal
	}
	selected := make(map[string]bool, len(input.SelectedRFQLineIDs))
	for _, lineID := range input.SelectedRFQLineIDs {
		selected[lineID] = true
	}

	result := ConditionalChargeResult{
		Groups: make([]CalculatedConditionalCharge, 0, len(input.Groups)),
		Total:  money.New(0, input.Currency),
	}
	for _, group := range input.Groups {
		groupSubtotal := money.New(0, input.Currency)
		selectedCount := 0
		for _, lineID := range group.ApplicableRFQLineIDs {
			subtotal, ok := quoted[lineID]
			if !ok {
				return ConditionalChargeResult{}, ErrInvalidConditionalCharge
			}
			if selected[lineID] {
				selectedCount++
				var err error
				groupSubtotal, err = groupSubtotal.Add(subtotal)
				if err != nil {
					return ConditionalChargeResult{}, ErrInvalidConditionalCharge
				}
			}
		}

		triggered := false
		switch group.Trigger {
		case ChargeTriggerAnySelected:
			triggered = selectedCount > 0
		case ChargeTriggerAllSelected:
			triggered = len(group.ApplicableRFQLineIDs) > 0 &&
				selectedCount == len(group.ApplicableRFQLineIDs)
		case ChargeTriggerSelectedSubtotalAtLeast:
			if group.Threshold == nil ||
				group.Threshold.Currency != input.Currency ||
				group.Threshold.Amount <= 0 {
				return ConditionalChargeResult{}, ErrInvalidConditionalCharge
			}
			triggered = groupSubtotal.Amount >= group.Threshold.Amount
		default:
			return ConditionalChargeResult{}, ErrInvalidConditionalCharge
		}

		amount := money.New(0, input.Currency)
		if triggered {
			switch group.Calculation {
			case ChargeCalculationFixedAmount:
				if group.FixedAmount == nil {
					return ConditionalChargeResult{}, ErrInvalidConditionalCharge
				}
				// A fixed group applies once regardless of how many member
				// lines caused the trigger.
				amount = *group.FixedAmount
			case ChargeCalculationPercentageOfSelectedSubtotal:
				if group.RateBPS == nil {
					return ConditionalChargeResult{}, ErrInvalidConditionalCharge
				}
				amount = money.ApplyRateBPS(groupSubtotal, *group.RateBPS)
			default:
				return ConditionalChargeResult{}, ErrInvalidConditionalCharge
			}
		}
		result.Groups = append(result.Groups, CalculatedConditionalCharge{
			GroupID:       group.ID,
			Triggered:     triggered,
			GroupSubtotal: groupSubtotal,
			Amount:        amount,
		})
		total, err := result.Total.Add(amount)
		if err != nil {
			return ConditionalChargeResult{}, ErrInvalidConditionalCharge
		}
		result.Total = total
	}
	return result, nil
}

func validateConditionalChargeInput(input ConditionalChargeInput) error {
	if input.Currency != Phase1Currency {
		return ErrInvalidConditionalCharge
	}

	quoted := make(map[string]struct{}, len(input.QuotedLines))
	for _, line := range input.QuotedLines {
		if strings.TrimSpace(line.RFQLineID) == "" ||
			line.Subtotal.Amount <= 0 ||
			line.Subtotal.Currency != input.Currency {
			return ErrInvalidConditionalCharge
		}
		if _, duplicate := quoted[line.RFQLineID]; duplicate {
			return ErrInvalidConditionalCharge
		}
		quoted[line.RFQLineID] = struct{}{}
	}

	selected := make(map[string]struct{}, len(input.SelectedRFQLineIDs))
	for _, lineID := range input.SelectedRFQLineIDs {
		if _, exists := quoted[lineID]; !exists {
			return ErrInvalidConditionalCharge
		}
		if _, duplicate := selected[lineID]; duplicate {
			return ErrInvalidConditionalCharge
		}
		selected[lineID] = struct{}{}
	}

	groupIDs := make(map[string]struct{}, len(input.Groups))
	lineOwners := make(map[string]string)
	for _, group := range input.Groups {
		if strings.TrimSpace(group.ID) == "" ||
			strings.TrimSpace(group.Name) == "" ||
			utf8.RuneCountInString(strings.TrimSpace(group.Name)) >
				MaxConditionalChargeNameLength ||
			utf8.RuneCountInString(strings.TrimSpace(group.Description)) >
				MaxConditionalChargeDescriptionLength ||
			len(group.ApplicableRFQLineIDs) == 0 {
			return ErrInvalidConditionalCharge
		}
		if _, duplicate := groupIDs[group.ID]; duplicate {
			return ErrInvalidConditionalCharge
		}
		groupIDs[group.ID] = struct{}{}

		withinGroup := make(map[string]struct{}, len(group.ApplicableRFQLineIDs))
		for _, lineID := range group.ApplicableRFQLineIDs {
			if _, exists := quoted[lineID]; !exists {
				return ErrInvalidConditionalCharge
			}
			if _, duplicate := withinGroup[lineID]; duplicate {
				return ErrInvalidConditionalCharge
			}
			withinGroup[lineID] = struct{}{}
			if _, overlaps := lineOwners[lineID]; overlaps {
				return ErrInvalidConditionalCharge
			}
			lineOwners[lineID] = group.ID
		}

		switch group.Trigger {
		case ChargeTriggerAnySelected, ChargeTriggerAllSelected:
			if group.Threshold != nil {
				return ErrInvalidConditionalCharge
			}
		case ChargeTriggerSelectedSubtotalAtLeast:
			if group.Threshold == nil ||
				group.Threshold.Amount <= 0 ||
				group.Threshold.Currency != input.Currency {
				return ErrInvalidConditionalCharge
			}
		default:
			return ErrInvalidConditionalCharge
		}

		switch group.Calculation {
		case ChargeCalculationFixedAmount:
			if group.FixedAmount == nil ||
				group.FixedAmount.Amount <= 0 ||
				group.FixedAmount.Currency != input.Currency ||
				group.RateBPS != nil {
				return ErrInvalidConditionalCharge
			}
		case ChargeCalculationPercentageOfSelectedSubtotal:
			if group.RateBPS == nil ||
				*group.RateBPS < 1 ||
				*group.RateBPS > money.BasisPointsDenominator ||
				group.FixedAmount != nil {
				return ErrInvalidConditionalCharge
			}
		default:
			return ErrInvalidConditionalCharge
		}
	}
	return nil
}
