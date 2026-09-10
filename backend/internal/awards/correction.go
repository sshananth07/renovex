package awards

import (
	"sort"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// Award correction calculation (§8G).
//
// A correction is a LOCKED BASELINE plus a CORRECTION DELTA. It CANNOT rerun
// ordinary F3 validation over the whole award: F3 requires each selected Offer
// Version to be the current submitted version, hold an `eligible` gate, and be
// unexpired. A version already awarded by the current revision will typically
// satisfy NONE of those — its gate is terminally `awarded`, the Supplier may
// have submitted a later version since, and the original validity date may have
// passed. Revalidating it would reject exactly the monotonic corrections F6
// exists to permit.
//
// Corrections are commercially MONOTONIC. An award, once published, is never
// reduced, removed or reassigned: a published revision is authoritative
// evidence that a Supplier was selected, and an outcome may already have told
// them so. Reversing one is a RESCISSION, which is out of scope for M8 and must
// not be approximated here.

type CorrectionCalculationInput struct {
	IssuedRFQ IssuedRFQSnapshot

	// BaselineRevision is the CURRENT authoritative revision. Its awarded
	// selections are copied exactly and never revalidated.
	BaselineRevision AwardRevision

	// ProposedSelections is baseline + delta: the complete decision set the
	// correction publishes.
	ProposedSelections []AwardLineSelection

	// OfferVersions must supply every referenced version, baseline and delta
	// alike. Baseline versions are used only for their frozen commercial
	// figures, never revalidated for eligibility.
	OfferVersions map[string]OfferVersionSnapshot

	CalculatedAt time.Time
}

// CorrectionCalculation extends an award calculation with what the correction
// must additionally claim and what it newly commits.
type CorrectionCalculation struct {
	AwardCalculation

	// NewlyAwardedLineages is the DELTA only. Baseline lineages already hold
	// terminal claims; re-acquiring them would fail, since a terminal claim is
	// not `eligible` (§8A.2).
	NewlyAwardedLineages []string

	// NewlyClaimedOfferVersions is likewise delta-only: a baseline version's
	// gate is already terminally `awarded`.
	NewlyClaimedOfferVersions []string

	// CorrectionDeltaTotal is the increment over the frozen baseline total.
	CorrectionDeltaTotal money.Money
}

// CalculateCorrection produces a superseding award from a locked baseline plus
// a validated delta.
//
// It is deliberately NOT the initial-award path: using CalculateAward here
// would revalidate the baseline and reject valid corrections.
func CalculateCorrection(
	input CorrectionCalculationInput,
) (CorrectionCalculation, error) {
	baseline := input.BaselineRevision
	currency := input.IssuedRFQ.Currency

	// Index the baseline's awarded lineages, which are the commitments the
	// correction may never weaken.
	baselineByLineage := make(map[string]AwardedLine, len(baseline.AwardedLines))
	baselineVersions := map[string]bool{}
	for _, line := range baseline.AwardedLines {
		baselineByLineage[line.StableLineageID] = line
		baselineVersions[line.OfferVersionID] = true
	}
	// Frozen per-version contributions: the correction charges only the
	// increment over these, so delivery is paid once across revisions.
	frozen := make(map[string]money.Money, len(baseline.SupplierSummaries))
	for _, summary := range baseline.SupplierSummaries {
		frozen[summary.OfferVersionID] = summary.SupplierTotal
	}

	proposedByLineage := make(map[string]AwardLineSelection,
		len(input.ProposedSelections))
	for _, selection := range input.ProposedSelections {
		proposedByLineage[selection.StableLineageID] = selection
	}

	// MONOTONICITY, checked before any arithmetic. Every lineage awarded by the
	// baseline must retain the SAME Supplier, Offer Version and Offer line.
	for lineage, awarded := range baselineByLineage {
		proposed, present := proposedByLineage[lineage]
		switch {
		case !present:
			// Removing it would let a lineage be re-awarded to a competitor
			// after its Supplier was told they won.
			return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
		case proposed.Unawarded:
			// A removal wearing a different hat.
			return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
		case proposed.OfferVersionID != awarded.OfferVersionID,
			proposed.OfferLineID != awarded.OfferLineID:
			// Reassignment: the exact sequence §8G forbids.
			return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
		}
	}

	// Partition the proposal. Baseline selections are copied; delta selections
	// pass the FULL current F3 validation.
	delta := map[string]bool{}
	for _, selection := range input.ProposedSelections {
		if selection.Unawarded {
			continue
		}
		if _, inBaseline := baselineByLineage[selection.StableLineageID]; !inBaseline {
			delta[selection.StableLineageID] = true
		}
	}

	// Validate delta versions against current state. A Supplier who withdrew
	// after the original award cannot be awarded by a correction.
	for _, selection := range input.ProposedSelections {
		if selection.Unawarded || !delta[selection.StableLineageID] {
			continue
		}
		version, supplied := input.OfferVersions[selection.OfferVersionID]
		if !supplied {
			return CorrectionCalculation{}, ErrOfferVersionNotSelectable
		}
		// A version already in the baseline keeps its locked status even when
		// the delta adds another of its lines: it was validated when awarded.
		if baselineVersions[version.ID] {
			// D1 makes this unreachable for a well-formed award: an
			// offer_level version is awarded with EVERY positively quoted
			// line, so no line of it remains to add later. Reaching here means
			// the baseline violated D1, which is a contract violation rather
			// than a valid correction (§8G).
			if version.Tax.Mode == OfferTaxOfferLevel {
				return CorrectionCalculation{}, ErrOfferLevelTaxRequiresComplete
			}
			continue
		}
		if err := validateSelectableVersion(
			version, input.IssuedRFQ, input.CalculatedAt, false); err != nil {
			return CorrectionCalculation{}, err
		}
	}

	// Calculate cumulatively over baseline + delta, with the baseline treated
	// as locked. calculateAward subtracts each version's frozen contribution,
	// so delivery is charged once and percentage groups re-trigger correctly.
	calculation, err := calculateAward(AwardCalculationInput{
		IssuedRFQ:     input.IssuedRFQ,
		Selections:    input.ProposedSelections,
		OfferVersions: input.OfferVersions,
		CalculatedAt:  input.CalculatedAt,
	}, frozen)
	if err != nil {
		return CorrectionCalculation{}, err
	}

	// The delta total is what this correction newly commits; the published
	// total is the cumulative figure.
	deltaTotal := calculation.GrandAwardTotal
	cumulative := money.New(
		baseline.GrandAwardTotal.Amount+deltaTotal.Amount, currency)

	// A cumulative total below the published one would reduce a commitment.
	if cumulative.Amount < baseline.GrandAwardTotal.Amount {
		return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
	}

	// Per-line monotonicity: no awarded line's own contribution may shrink.
	correctedByLineage := make(map[string]AwardedLine, len(calculation.AwardedLines))
	for _, line := range calculation.AwardedLines {
		correctedByLineage[line.StableLineageID] = line
	}
	for lineage, awarded := range baselineByLineage {
		corrected, present := correctedByLineage[lineage]
		if !present {
			return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
		}
		if corrected.LineSubtotal.Amount < awarded.LineSubtotal.Amount {
			return CorrectionCalculation{}, ErrAwardCorrectionNotMonotonic
		}
	}

	calculation.GrandAwardTotal = cumulative

	newLineages := make([]string, 0, len(delta))
	for lineage := range delta {
		newLineages = append(newLineages, lineage)
	}
	sort.Strings(newLineages)

	newVersions := make([]string, 0)
	seenVersion := map[string]bool{}
	for _, selection := range input.ProposedSelections {
		if selection.Unawarded || !delta[selection.StableLineageID] {
			continue
		}
		// Only versions the baseline never claimed need a new gate: an
		// already-awarded gate is terminal and cannot be re-claimed.
		if baselineVersions[selection.OfferVersionID] ||
			seenVersion[selection.OfferVersionID] {
			continue
		}
		seenVersion[selection.OfferVersionID] = true
		newVersions = append(newVersions, selection.OfferVersionID)
	}
	sort.Strings(newVersions)

	// Recompute the fingerprint over the cumulative figures, so a correction is
	// never mistaken for the revision it supersedes.
	calculation.SelectionFingerprint = selectionFingerprint(
		input.IssuedRFQ, calculation)

	return CorrectionCalculation{
		AwardCalculation:          calculation,
		NewlyAwardedLineages:      newLineages,
		NewlyClaimedOfferVersions: newVersions,
		CorrectionDeltaTotal:      deltaTotal,
	}, nil
}
