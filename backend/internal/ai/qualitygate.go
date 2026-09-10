package ai

import "strings"

// SpaceQualityGateResult is the outcome of running deterministic quality
// checks over one Space-generation response (T1.5 §9): corrected/retained
// items, dropped fabricated items, and coverage gaps a caller may route
// through one bounded repair call.
type SpaceQualityGateResult struct {
	Items   []SpaceSuggestionData
	Dropped []SpaceSuggestionData
	Gaps    []SpaceMention
}

// ApplySpaceQualityGate runs the deterministic Space checks from T1.5 §3,
// §5, §9 over items against brief: grounding-verifies every explicit claim
// (downgrading or dropping fabricated ones), corrects unsupported
// specializations, and reports coverage gaps for the caller's bounded
// repair pass. Never authors a new Space itself.
func ApplySpaceQualityGate(items []SpaceSuggestionData, brief string) SpaceQualityGateResult {
	mentions := ExtractCandidateSpaceMentions(brief)
	mentionedNouns := make(map[string]bool, len(mentions))
	for _, m := range mentions {
		mentionedNouns[m.Noun] = true
	}

	var kept, dropped []SpaceSuggestionData
	for _, item := range items {
		if item.EvidenceType == EvidenceExplicit {
			if !IsGrounded(item.SourceExcerpt, brief) {
				if !otherwiseSupportedSpace(item, mentionedNouns) {
					dropped = append(dropped, item)
					continue
				}
				item.EvidenceType = EvidenceDerived
			}
		}
		if item.EvidenceType == EvidenceExplicit {
			item = correctUnsupportedSpecialization(item, brief)
		}
		kept = append(kept, item)
	}

	gaps := CoverageGaps(mentions, kept)
	return SpaceQualityGateResult{Items: kept, Dropped: dropped, Gaps: gaps}
}

// otherwiseSupportedSpace reports whether item's own name/type corresponds
// to a common-space noun the deterministic backstop actually found in the
// brief, even though its claimed excerpt didn't ground — this is the
// "is the underlying suggestion otherwise supported?" branch from T1.5 §3's
// amendment: a hallucinated item must not survive by merely downgrading.
func otherwiseSupportedSpace(item SpaceSuggestionData, mentionedNouns map[string]bool) bool {
	normalizedName := normalizeMatchText(item.Name)
	for noun := range mentionedNouns {
		if normalizedName == noun || strings.HasSuffix(normalizedName, " "+noun) {
			return true
		}
	}
	return false
}

// correctUnsupportedSpecialization strips an unsupported qualifier from
// item's Name (e.g. "Guest Bedroom" -> "Bedroom") when brief never
// establishes it, per IsSupportedSpecialization's token-adjacency check.
// The base noun is recovered from SpaceType/Name's own trailing tokens
// rather than guessed.
func correctUnsupportedSpecialization(item SpaceSuggestionData, brief string) SpaceSuggestionData {
	baseNoun := baseNounFor(item)
	if baseNoun == "" {
		return item
	}
	if IsSupportedSpecialization(item.Name, baseNoun, brief) {
		return item
	}
	item.Name = strings.Title(baseNoun) //nolint:staticcheck // simple title-case for a short noun phrase, not locale text
	return item
}

// baseNounFor finds which recognized commonSpaceNouns entry item's Name
// ends with, so correctUnsupportedSpecialization knows what to fall back
// to. Returns "" if the name doesn't correspond to a recognized noun (in
// which case specialization correction is skipped — nothing to fall back
// to that wouldn't itself be a guess).
func baseNounFor(item SpaceSuggestionData) string {
	normalizedName := normalizeMatchText(item.Name)
	best := ""
	for _, noun := range commonSpaceNouns {
		if normalizedName == noun || strings.HasSuffix(normalizedName, " "+noun) {
			if len(noun) > len(best) {
				best = noun
			}
		}
	}
	return best
}

// WorkItemQualityGateResult is the outcome of running deterministic quality
// checks over one Work Item-generation response.
type WorkItemQualityGateResult struct {
	Items   []WorkItemSuggestionData
	Dropped []WorkItemSuggestionData
}

// ApplyWorkItemQualityGate runs the deterministic Work Item checks from
// T1.5 §4, §5, §9 over items against brief: grounding-verifies every
// explicit claim, and reverts an ungrounded space-scoped conditional claim
// to project-level scope (preserving "affected space = unknown /
// project-wide conditional" rather than silently promoting a conditional
// statement into definite space-scoped work).
func ApplyWorkItemQualityGate(items []WorkItemSuggestionData, brief string) WorkItemQualityGateResult {
	var kept, dropped []WorkItemSuggestionData
	for _, item := range items {
		if item.ScopeOrigin == ScopeOriginExplicit {
			if !IsGrounded(item.SourceExcerpt, brief) {
				if !otherwiseSupportedWorkItem(item, brief) {
					dropped = append(dropped, item)
					continue
				}
				item.ScopeOrigin = ScopeOriginSupporting
			} else if item.ScopeLevel == ScopeLevelSpace && !spaceGroundedForWorkItem(item, brief) {
				// The work itself is grounded, but the specific Space
				// assignment is not supported by the same excerpt — revert
				// to project-level/unknown rather than keep a definite but
				// unsupported space claim (T1.5 §5: preserve conditional
				// language, never silently promote to a specific space).
				item.ScopeLevel = ScopeLevelProject
				item.SpaceID = nil
			}
		}
		kept = append(kept, item)
	}
	return WorkItemQualityGateResult{Items: kept, Dropped: dropped}
}

func otherwiseSupportedWorkItem(item WorkItemSuggestionData, brief string) bool {
	tokens := significantTokens(item.Description)
	if len(tokens) == 0 {
		return false
	}
	briefTokens := make(map[string]bool)
	for _, t := range significantTokens(brief) {
		briefTokens[t] = true
	}
	matched := 0
	for _, t := range tokens {
		if briefTokens[t] {
			matched++
		}
	}
	// Looser bar than IsGrounded's explicit-claim threshold — this only
	// decides downgrade-vs-drop, not explicit-vs-derived.
	return len(tokens) > 0 && float64(matched)/float64(len(tokens)) >= 0.5
}

// MaterialSuggestionGrounded reports whether a material-typed Resource
// suggestion's name is actually grounded in the parent Work Item's current
// authoritative Description text (T1.5 §7: resource generation must use
// accepted authoritative Work Items — no persisted MaterialSpecificity flag
// is threaded through, since the domain WorkItem carries no AI-provenance
// field; Description is re-checked directly, tolerant of paraphrase via the
// same bounded token-overlap approach as IsGrounded/otherwiseSupportedWorkItem).
// Trade/equipment suggestions never call this — they aren't material-
// specific by nature and are exempt from this check by design.
func MaterialSuggestionGrounded(suggestedMaterialName, workItemDescription string) bool {
	tokens := significantTokens(suggestedMaterialName)
	if len(tokens) == 0 {
		return false
	}
	descTokens := make(map[string]bool)
	for _, t := range significantTokens(workItemDescription) {
		descTokens[t] = true
	}
	matched := 0
	for _, t := range tokens {
		if descTokens[t] {
			matched++
		}
	}
	return float64(matched)/float64(len(tokens)) >= groundingOverlapThreshold
}

// spaceGroundedForWorkItem reports whether item's SourceExcerpt itself
// establishes a specific space, rather than being a generic/conditional
// statement a downstream stage merely attached a SpaceID to. A simple,
// deterministic proxy: the excerpt must contain a recognized common-space
// noun mention — "replace damaged flooring where necessary" contains none,
// so a Kitchen SpaceID attached to it is unsupported by its own excerpt.
func spaceGroundedForWorkItem(item WorkItemSuggestionData, brief string) bool {
	mentions := ExtractCandidateSpaceMentions(item.SourceExcerpt)
	return len(mentions) > 0
}
