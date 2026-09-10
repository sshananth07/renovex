package ai

import "strings"

// MatchTier identifies which deterministic (or validated-model) rule
// produced a MatchResult (T1.5 §8: catalogue matching must be deterministic
// where deterministic matching is possible). Category/Unit are compatibility
// co-signals, not standalone winning tiers — two candidates sharing a
// category and unit (e.g. "Cement" and "Tile Adhesive", both
// Flooring/bag) must never be confused for each other by tier alone.
type MatchTier string

const (
	// TierExactName: normalized (lowercased, whitespace-collapsed) name
	// matches a candidate exactly. Wins immediately.
	TierExactName MatchTier = "exact_name"
	// TierAlias is reserved for a known alias/synonym field on Material.
	// No such field exists yet (Material carries Name/Category/
	// Specification/Unit only) — this tier is a documented no-op until one
	// is added, kept in the tier vocabulary so adding an alias field later
	// only means filling in matchAlias, not re-deriving the tier order.
	TierAlias MatchTier = "alias"
	// TierStrongLexical: strong lexical similarity to a candidate AND that
	// candidate is category/unit-compatible with the request. Category/
	// Unit alone never win; they gate which lexical near-matches are
	// trustworthy.
	TierStrongLexical MatchTier = "strong_lexical"
	// TierValidatedModel: no deterministic tier won, but the AI-proposed
	// candidate ID was supplied, is present in the authorized candidate
	// list, and passes hard category/unit compatibility validation.
	TierValidatedModel MatchTier = "validated_model"
	// TierNone: no candidate is trustworthy. Never force a weak match.
	TierNone MatchTier = "no_match"
)

// MatchResult is the outcome of matching one suggested resource name against
// a company's Material catalogue.
type MatchResult struct {
	MaterialID string
	Tier       MatchTier
	// Confidence is derived deterministically from Tier, never a model
	// score — see tierConfidence.
	Confidence float64
}

func tierConfidence(tier MatchTier) float64 {
	switch tier {
	case TierExactName, TierAlias:
		return 1.0
	case TierStrongLexical:
		return 0.85
	case TierValidatedModel:
		return 0.6
	default:
		return 0.0
	}
}

func normalizeMatchText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// compatible reports whether candidate is a compatible co-signal for the
// requested category/unit. Empty requested values impose no constraint
// (caller didn't supply one, e.g. exact-name-only callers).
func compatible(candidate DomainGatewayMaterial, category, unit string) bool {
	if category != "" && candidate.Category != "" && !strings.EqualFold(candidate.Category, category) {
		return false
	}
	if unit != "" && candidate.Unit != "" && !strings.EqualFold(candidate.Unit, unit) {
		return false
	}
	return true
}

// lexicalSimilarity returns a 0..1 token-overlap ratio (Jaccard over
// whitespace-split normalized tokens) — a small, dependency-free, bounded
// deterministic similarity measure. Good enough to distinguish "Tile
// Adhesive"/"Tile Adhesives" from "Tiler Labour Kit" without pulling in an
// edit-distance library for one helper.
func lexicalSimilarity(a, b string) float64 {
	ta := strings.Fields(normalizeMatchText(a))
	tb := strings.Fields(normalizeMatchText(b))
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	setB := make(map[string]bool, len(tb))
	for _, t := range tb {
		setB[strings.TrimSuffix(t, "s")] = true
	}
	matches := 0
	for _, t := range ta {
		if setB[strings.TrimSuffix(t, "s")] {
			matches++
		}
	}
	union := len(ta)
	for _, t := range tb {
		key := strings.TrimSuffix(t, "s")
		found := false
		for _, u := range ta {
			if strings.TrimSuffix(u, "s") == key {
				found = true
				break
			}
		}
		if !found {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(matches) / float64(union)
}

const strongLexicalThreshold = 0.6

// validatedModelLexicalFloor is the minimum lexical similarity a model-
// proposed candidate must clear, in addition to category/unit
// compatibility, to be accepted as TierValidatedModel. Category/unit alone
// are too weak a signal when the caller has no requested category/unit to
// compare against (both empty imposes no constraint) — this floor is what
// stops an arbitrary same-category sibling (e.g. "Cement" proposed for
// "Waterproof Membrane Sealant") from being accepted just because nothing
// ruled it out.
const validatedModelLexicalFloor = 0.2

// MatchMaterial ranks candidates against a suggested resource name and
// returns the single deterministic (or validated-model) winner, per T1.5
// §8: exact normalized name -> alias -> category/unit-gated strong lexical
// -> validated model-proposed candidate -> no match. category/unit describe
// the requested resource's own compatibility signals (may be empty when
// unknown). modelProposedID is the AI's own candidateMaterialId suggestion,
// if any — used only as a last-resort fallback, never trusted outright.
func MatchMaterial(suggestedName, category, unit string, candidates []DomainGatewayMaterial, modelProposedID *string) MatchResult {
	normalizedSuggested := normalizeMatchText(suggestedName)

	for _, c := range candidates {
		if normalizeMatchText(c.Name) == normalizedSuggested {
			return MatchResult{MaterialID: c.ID, Tier: TierExactName, Confidence: tierConfidence(TierExactName)}
		}
	}

	// TierAlias: no alias field exists on Material yet (see doc comment on
	// TierAlias) — nothing to check, intentionally falls through.

	bestID := ""
	bestScore := 0.0
	for _, c := range candidates {
		if !compatible(c, category, unit) {
			continue
		}
		score := lexicalSimilarity(suggestedName, c.Name)
		if score > bestScore {
			bestScore = score
			bestID = c.ID
		}
	}
	if bestID != "" && bestScore >= strongLexicalThreshold {
		return MatchResult{MaterialID: bestID, Tier: TierStrongLexical, Confidence: tierConfidence(TierStrongLexical)}
	}

	if modelProposedID != nil && *modelProposedID != "" {
		for _, c := range candidates {
			if c.ID != *modelProposedID {
				continue
			}
			if !compatible(c, category, unit) {
				return MatchResult{Tier: TierNone}
			}
			if lexicalSimilarity(suggestedName, c.Name) < validatedModelLexicalFloor {
				// Category/unit compatibility alone is too weak a signal
				// (vacuous when the caller has no requested category/unit)
				// — the name must at least be plausibly related.
				return MatchResult{Tier: TierNone}
			}
			return MatchResult{MaterialID: c.ID, Tier: TierValidatedModel, Confidence: tierConfidence(TierValidatedModel)}
		}
		// Proposed ID not in the authorized candidate list at all.
		return MatchResult{Tier: TierNone}
	}

	return MatchResult{Tier: TierNone}
}
