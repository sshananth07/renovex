package ai

import "strings"

// groundingOverlapThreshold is the minimum fraction of an excerpt's
// significant tokens that must appear in the brief for the excerpt to be
// considered grounded. Bounded token-overlap, not exact substring — the
// model may shorten/paraphrase an excerpt, but a claimed-explicit item must
// still be traceable to real brief text, not fabricated (T1.5 §2, §9).
const groundingOverlapThreshold = 0.8

// stopWords are excluded from grounding comparison so trivial connective
// overlap (e.g. "should be the") can't inflate a match.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "be": true,
	"to": true, "of": true, "and": true, "should": true, "will": true,
	"for": true, "in": true, "on": true, "it": true,
}

func significantTokens(s string) []string {
	fields := strings.Fields(normalizeMatchText(s))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, ".,;:!?\"'()")
		if f == "" || stopWords[f] {
			continue
		}
		out = append(out, f)
	}
	return out
}

// IsGrounded reports whether excerpt is supportable from brief text: either
// an exact normalized substring, or a bounded phrase match where at least
// groundingOverlapThreshold of the excerpt's significant tokens appear in
// the brief. An empty excerpt is never grounded (fails closed) — a
// suggestion claiming explicit evidence with no excerpt cannot be verified.
func IsGrounded(excerpt, brief string) bool {
	normalizedExcerpt := normalizeMatchText(excerpt)
	if normalizedExcerpt == "" {
		return false
	}
	normalizedBrief := normalizeMatchText(brief)
	if strings.Contains(normalizedBrief, normalizedExcerpt) {
		return true
	}

	tokens := significantTokens(excerpt)
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
	return float64(matched)/float64(len(tokens)) >= groundingOverlapThreshold
}
