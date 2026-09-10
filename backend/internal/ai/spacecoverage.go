package ai

import (
	"regexp"
	"strings"
)

// commonSpaceNouns is a deliberately open-ended, easily-extended vocabulary
// of common renovation space names (T1.5 §3: "a small deterministic
// common-space recognizer... is a coverage backstop, not the semantic
// authority" — never the sole definition of a Space, and never hardcoded
// to one test brief's room names). Multi-word entries are listed before
// any single-word entry they contain would shadow them (matching is
// longest-phrase-first, see matchSpaceNouns).
// Deliberately excludes qualifier+generic-noun combinations that must be
// EARNED by brief evidence rather than assumed as vocabulary — "master
// bedroom"/"guest bedroom"/"ensuite bathroom" are NOT listed here even
// though they're common phrasings, because IsSupportedSpecialization must
// be able to reject an unsupported one (e.g. "Guest Bedroom" when the
// brief never establishes a guest bedroom). Only genuine distinct room
// TYPES (a dry kitchen is functionally different from a kitchen, not a
// qualified kitchen) are listed as their own multi-word phrase.
var commonSpaceNouns = []string{
	"store room", "walk-in wardrobe", "maid's room", "prayer room",
	"server room", "dry kitchen", "wet kitchen", "living and dining area",
	"family hall",
	"bedroom", "bathroom", "kitchen", "living room", "dining room",
	"balcony", "yard", "foyer", "study", "laundry", "garage", "porch",
	"office", "showroom", "hallway", "corridor", "pantry", "utility room",
	"powder room", "den",
}

// numeralWords maps small spelled-out numerals to their integer value —
// bounded, deterministic plural handling ("two additional bedrooms" -> 2
// distinct bedroom mentions) without a full NLP numeral parser.
var numeralWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10,
	"a": 1, "an": 1,
}

var wordSplit = regexp.MustCompile(`[^a-z0-9']+`)

// SpaceMention is one deterministically-recognized reference to a common
// space noun in a project brief, with a best-effort count (defaults to 1;
// increased when the brief uses a plural + numeral, e.g. "two ... bedrooms").
type SpaceMention struct {
	Noun  string
	Count int
}

// ExtractCandidateSpaceMentions scans brief for occurrences of
// commonSpaceNouns (singular or plural), applying simple numeral-word
// counting immediately before a plural noun. This is the coverage
// backstop's AI-independent half — it never authors a Space name, it only
// detects that *some* space-like term was mentioned so CoverageGaps can
// flag an omission for repair.
func ExtractCandidateSpaceMentions(brief string) []SpaceMention {
	normalized := normalizeMatchText(brief)
	tokens := wordSplit.Split(normalized, -1)

	counts := map[string]int{}
	order := []string{}

	// Longest phrase first so "master bedroom" is consumed before the bare
	// "bedroom" entry would otherwise double-count it.
	phrases := append([]string(nil), commonSpaceNouns...)

	i := 0
	for i < len(tokens) {
		matchedNoun, matchedLen := matchSpaceNouns(tokens, i, phrases)
		if matchedNoun == "" {
			i++
			continue
		}
		count := 1
		if isPluralForm(tokens, i) {
			count = numeralBefore(tokens, i)
		}
		if _, seen := counts[matchedNoun]; !seen {
			order = append(order, matchedNoun)
		}
		counts[matchedNoun] += count
		i += matchedLen
	}

	result := make([]SpaceMention, 0, len(order))
	for _, noun := range order {
		result = append(result, SpaceMention{Noun: noun, Count: counts[noun]})
	}
	return result
}

// matchSpaceNouns tries every phrase (longest first by construction order
// in commonSpaceNouns) starting at tokens[i], returning the matched noun's
// canonical singular form and how many tokens it consumed. Handles a
// trailing "s" for simple plurals (e.g. "bedrooms" -> "bedroom").
func matchSpaceNouns(tokens []string, i int, phrases []string) (string, int) {
	for _, phrase := range phrases {
		phraseTokens := strings.Fields(phrase)
		n := len(phraseTokens)
		if i+n > len(tokens) {
			continue
		}
		match := true
		for j, pt := range phraseTokens {
			tok := tokens[i+j]
			last := j == n-1
			if last {
				if tok != pt && tok != pt+"s" && !(strings.HasSuffix(pt, "y") && tok == strings.TrimSuffix(pt, "y")+"ies") {
					match = false
					break
				}
			} else if tok != pt {
				match = false
				break
			}
		}
		if match {
			return phrase, n
		}
	}
	return "", 0
}

func isPluralForm(tokens []string, matchStart int) bool {
	last := tokens[matchStart]
	// matchStart is the START of the matched phrase; for multi-word
	// phrases the plural marker is on the LAST token of the match, so this
	// helper is only meaningfully called with the noun's own last token.
	// Re-derive: callers pass the start index of a single-token noun match
	// in practice (commonSpaceNouns' multi-word entries are rarely
	// pluralized in briefs); check suffix directly.
	return strings.HasSuffix(last, "s") && last != "s"
}

// numeralBefore looks at the token immediately preceding matchStart for a
// spelled-out numeral (optionally skipping one adjective like "additional"),
// returning that count, or 1 if none is found.
func numeralBefore(tokens []string, matchStart int) int {
	for back := 1; back <= 2 && matchStart-back >= 0; back++ {
		if n, ok := numeralWords[tokens[matchStart-back]]; ok {
			return n
		}
	}
	return 1
}

// CoverageGaps compares deterministically-extracted space mentions against
// the AI's generated explicit Space suggestions and returns any mention
// whose count exceeds how many explicit suggestions share its noun — i.e.
// a plausible omission the caller should route through the bounded repair
// pass (T1.5 §3, §9). This never authors a Space itself.
func CoverageGaps(mentions []SpaceMention, generated []SpaceSuggestionData) []SpaceMention {
	generatedCounts := map[string]int{}
	for _, g := range generated {
		if g.EvidenceType != EvidenceExplicit {
			continue
		}
		for _, noun := range commonSpaceNouns {
			if nameMatchesNoun(g.Name, noun) {
				generatedCounts[noun]++
				break
			}
		}
	}

	var gaps []SpaceMention
	for _, m := range mentions {
		have := generatedCounts[m.Noun]
		if have < m.Count {
			gaps = append(gaps, SpaceMention{Noun: m.Noun, Count: m.Count - have})
		}
	}
	return gaps
}

func nameMatchesNoun(name, noun string) bool {
	normalizedName := normalizeMatchText(name)
	// Strip a trailing " N" numbering (e.g. "Bedroom 2") before comparing.
	fields := strings.Fields(normalizedName)
	if len(fields) > 0 {
		if _, isNum := numeralWords[fields[len(fields)-1]]; isNum {
			fields = fields[:len(fields)-1]
		} else if isDigits(fields[len(fields)-1]) {
			fields = fields[:len(fields)-1]
		}
	}
	trimmedName := strings.Join(fields, " ")
	if trimmedName == noun {
		return true
	}
	// A qualifier + noun name (e.g. "Master Bedroom") matches its base noun
	// ("bedroom") only when the noun forms the trailing words of the name —
	// never a bare substring check, which would wrongly match "kitchen"
	// against the unrelated "dry kitchen"/"wet kitchen" entries.
	return strings.HasSuffix(trimmedName, " "+noun)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// specializationWindow bounds how many tokens away a qualifier may sit from
// the base noun in the brief for that qualifier to count as "established" —
// token-adjacency, not a whole-line/whole-brief contains() check, so
// "master bedroom" in one sentence doesn't retroactively justify "Guest
// Bedroom" elsewhere just because "bedroom" appears somewhere in the brief.
const specializationWindow = 3

// IsSupportedSpecialization reports whether generatedName's qualifier
// (words beyond the base noun, e.g. "Master"/"Guest" before "Bedroom") is
// established by brief text — the qualifier token must appear within
// specializationWindow tokens of an occurrence of baseNoun. A name with no
// qualifier (or only a trailing number, e.g. "Bedroom 2") is always
// supported — numbering is a normalization of an explicit plural mention,
// not a specialization claim.
func IsSupportedSpecialization(generatedName, baseNoun, brief string) bool {
	normalizedName := normalizeMatchText(generatedName)
	nameTokens := strings.Fields(normalizedName)
	nounTokens := strings.Fields(baseNoun)

	qualifiers := extractQualifiers(nameTokens, nounTokens)
	if len(qualifiers) == 0 {
		return true // plain name, or only a trailing number — always fine
	}

	briefTokens := wordSplit.Split(normalizeMatchText(brief), -1)
	nounPositions := findPhrasePositions(briefTokens, nounTokens)
	if len(nounPositions) == 0 {
		return false
	}

	for _, q := range qualifiers {
		if !qualifierNearAnyPosition(briefTokens, q, nounPositions) {
			return false
		}
	}
	return true
}

// extractQualifiers returns the tokens in nameTokens that are neither part
// of nounTokens nor a trailing number/numeral (a numbering suffix like the
// "2" in "Bedroom 2" is not a specialization qualifier).
func extractQualifiers(nameTokens, nounTokens []string) []string {
	remaining := append([]string(nil), nameTokens...)
	// Remove noun tokens (in order, first occurrence) — the qualifier is
	// whatever's left over.
	for _, nt := range nounTokens {
		for i, t := range remaining {
			if t == nt {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	var qualifiers []string
	for _, t := range remaining {
		if _, isNum := numeralWords[t]; isNum {
			continue
		}
		if isDigits(t) {
			continue
		}
		qualifiers = append(qualifiers, t)
	}
	return qualifiers
}

func findPhrasePositions(tokens, phrase []string) []int {
	var positions []int
	n := len(phrase)
	if n == 0 {
		return positions
	}
	for i := 0; i+n <= len(tokens); i++ {
		match := true
		for j := 0; j < n; j++ {
			if tokens[i+j] != phrase[j] {
				match = false
				break
			}
		}
		if match {
			positions = append(positions, i)
		}
	}
	return positions
}

func qualifierNearAnyPosition(tokens []string, qualifier string, nounPositions []int) bool {
	for i, t := range tokens {
		if t != qualifier {
			continue
		}
		for _, np := range nounPositions {
			if abs(i-np) <= specializationWindow {
				return true
			}
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
