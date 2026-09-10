package ai

import (
	"context"
	"strings"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
)

// spaceSuggestionMeta carries the AI-owned pass-through fields
// (confidence/rationale) that ApplySpaceQualityGate's pure signature
// doesn't need to see or preserve itself — looked up by normalized
// name+type key after the gate runs. A corrected item whose Name changed
// (e.g. an unsupported specialization stripped) simply loses its original
// rationale/confidence rather than carrying stale text — acceptable
// degradation, never a crash.
type spaceSuggestionMeta struct {
	Confidence *float64
	Rationale  string
}

// toSpaceSuggestionData converts the Python wire response into the
// persisted-shape SpaceSuggestionData the quality gate operates on, plus a
// lookup of each item's confidence/rationale by normalized key.
func toSpaceSuggestionData(sugs []platformai.SpaceSuggestion) ([]SpaceSuggestionData, map[string]spaceSuggestionMeta) {
	items := make([]SpaceSuggestionData, 0, len(sugs))
	meta := toSpaceMeta(sugs)
	for _, sug := range sugs {
		items = append(items, SpaceSuggestionData{
			Name: sug.Name, SpaceType: sug.SpaceType,
			EvidenceType:  EvidenceType(sug.EvidenceType),
			SourceExcerpt: sug.SourceExcerpt,
		})
	}
	return items, meta
}

func toSpaceMeta(sugs []platformai.SpaceSuggestion) map[string]spaceSuggestionMeta {
	meta := make(map[string]spaceSuggestionMeta, len(sugs))
	for _, sug := range sugs {
		meta[normalizeSpaceKey(sug.Name, sug.SpaceType)] = spaceSuggestionMeta{Confidence: sug.Confidence, Rationale: sug.Rationale}
	}
	return meta
}

// maxRepairAttempts bounds the quality gate's bounded repair pass to
// exactly one extra AI call per generation — never an open retry loop
// (T1.5 §9).
const maxRepairAttempts = 1

// repairSpaceCoverageGaps issues at most one bounded structured repair call
// through the same AIClient interface used for normal generation, asking
// the provider to repair specific coverage omissions only. Returns the
// repaired suggestion set (empty + nil error if the provider call itself
// failed, so the caller keeps its pre-repair result rather than treating
// this as fatal).
func (s *Service) repairSpaceCoverageGaps(
	ctx context.Context, operationID, projectID, brief string,
	existingSpaceReqs []platformai.ExistingSpace, gateResult SpaceQualityGateResult,
) ([]SpaceSuggestionData, []platformai.SpaceSuggestion, error) {
	instruction := buildSpaceRepairInstruction(gateResult)
	resp, err := s.client.SuggestSpaces(ctx, platformai.SpaceSuggestionRequest{
		OperationID:       operationID + ":repair",
		Project:           platformai.ProjectContext{ID: projectID, ScopeBrief: brief},
		ExistingSpaces:    existingSpaceReqs,
		RepairInstruction: instruction,
	})
	if err != nil {
		return nil, nil, err
	}

	// The repair response replaces gaps only — merge with the original
	// gate-passed items (never drop items the repair call didn't
	// mention), keyed by normalized name+type to avoid duplicating an
	// item the repair pass re-suggested.
	merged := append([]SpaceSuggestionData(nil), gateResult.Items...)
	seen := make(map[string]bool, len(merged))
	for _, item := range merged {
		seen[normalizeSpaceKey(item.Name, item.SpaceType)] = true
	}
	for _, sug := range resp.Suggestions {
		key := normalizeSpaceKey(sug.Name, sug.SpaceType)
		if seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, SpaceSuggestionData{
			Name: sug.Name, SpaceType: sug.SpaceType,
			EvidenceType: EvidenceType(sug.EvidenceType), SourceExcerpt: sug.SourceExcerpt,
		})
	}
	return merged, resp.Suggestions, nil
}

func buildSpaceRepairInstruction(gateResult SpaceQualityGateResult) string {
	var missing []string
	for _, gap := range gateResult.Gaps {
		missing = append(missing, gap.Noun)
	}
	return "Repair omissions only: the previous generation appears to be missing explicit Space suggestions for: " +
		strings.Join(missing, ", ") + ". Add only the missing Spaces; do not repeat or change any already-suggested Space."
}
