package ai

import (
	"strings"
	"testing"
)

func TestExtractCandidateSpacesFindsAllNamedRooms(t *testing.T) {
	got := ExtractCandidateSpaceMentions(condoBrief)
	want := map[string]int{
		// "3-bedroom condominium" (1) + master bedroom (1) + "two additional
		// bedrooms" (2) = 4 distinct bedroom references in condoBrief.
		"bedroom":  4,
		"bathroom": 2, // ensuite bathroom + shared bathroom
		"kitchen":  1,
		"balcony":  1,
		"yard":     1,
	}
	// Aggregate by base noun (e.g. "bedroom" and "master bedroom" both count
	// toward "bedroom") since ExtractCandidateSpaceMentions tracks each
	// recognized phrase separately, not by base noun.
	counts := map[string]int{}
	for _, m := range got {
		for base := range want {
			if m.Noun == base || strings.HasSuffix(m.Noun, " "+base) {
				counts[base] += m.Count
			}
		}
	}
	for noun, wantCount := range want {
		if counts[noun] != wantCount {
			t.Errorf("noun %q: count = %d, want %d (all mentions: %+v)", noun, counts[noun], wantCount, got)
		}
	}
}

func TestExtractCandidateSpacesHandlesNumeralWords(t *testing.T) {
	got := ExtractCandidateSpaceMentions("two additional bedrooms need repainting")
	found := false
	for _, m := range got {
		if m.Noun == "bedroom" && m.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected bedroom count=2 from 'two additional bedrooms', got %+v", got)
	}
}

func TestExtractCandidateSpacesOpenEndedVocabulary(t *testing.T) {
	brief := "Renovate the foyer, the study, a store room, the laundry, and the maid's room."
	got := ExtractCandidateSpaceMentions(brief)
	nouns := map[string]bool{}
	for _, m := range got {
		nouns[m.Noun] = true
	}
	for _, want := range []string{"foyer", "study", "store room", "laundry", "maid's room"} {
		if !nouns[want] {
			t.Errorf("expected %q to be recognized, got %+v", want, got)
		}
	}
}

func TestCoverageGapsDetectsMissingExplicitSpace(t *testing.T) {
	mentions := ExtractCandidateSpaceMentions(condoBrief)
	generated := []SpaceSuggestionData{
		{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit},
		{Name: "Master Bedroom", SpaceType: "bedroom", EvidenceType: EvidenceExplicit},
	}
	gaps := CoverageGaps(mentions, generated)
	if len(gaps) == 0 {
		t.Fatal("expected coverage gaps for missing bathroom/balcony/yard/dining spaces")
	}
	for _, g := range gaps {
		if g.Noun == "kitchen" {
			t.Errorf("kitchen was generated, should not appear as a gap: %+v", gaps)
		}
	}
}

func TestCoverageGapsEmptyWhenFullyCovered(t *testing.T) {
	mentions := []SpaceMention{{Noun: "kitchen", Count: 1}}
	generated := []SpaceSuggestionData{{Name: "Kitchen", SpaceType: "kitchen", EvidenceType: EvidenceExplicit}}
	gaps := CoverageGaps(mentions, generated)
	if len(gaps) != 0 {
		t.Fatalf("expected no gaps, got %+v", gaps)
	}
}

func TestCoverageGapsRequiresCountNotJustPresence(t *testing.T) {
	// Two bedrooms mentioned but only one generated -> still a gap (count-aware).
	mentions := []SpaceMention{{Noun: "bedroom", Count: 2}}
	generated := []SpaceSuggestionData{{Name: "Master Bedroom", SpaceType: "bedroom", EvidenceType: EvidenceExplicit}}
	gaps := CoverageGaps(mentions, generated)
	if len(gaps) != 1 || gaps[0].Noun != "bedroom" {
		t.Fatalf("expected one bedroom gap, got %+v", gaps)
	}
}

func TestIsSupportedSpecializationAllowsQualifierNearNoun(t *testing.T) {
	brief := "The master bedroom needs new flooring."
	if !IsSupportedSpecialization("Master Bedroom", "bedroom", brief) {
		t.Fatal("expected 'master bedroom' specialization to be supported by adjacent brief text")
	}
}

func TestIsSupportedSpecializationRejectsUnsupportedQualifier(t *testing.T) {
	brief := "Repaint the bedroom."
	if IsSupportedSpecialization("Guest Bedroom", "bedroom", brief) {
		t.Fatal("expected 'guest bedroom' to be rejected — brief never establishes a guest bedroom")
	}
}

func TestIsSupportedSpecializationAllowsPlainNormalizationWithNoQualifier(t *testing.T) {
	brief := "Redo the kitchen."
	if !IsSupportedSpecialization("Kitchen", "kitchen", brief) {
		t.Fatal("plain unspecialized name should always be supported")
	}
}

func TestIsSupportedSpecializationAllowsNumberedNormalization(t *testing.T) {
	brief := "two additional bedrooms need repainting"
	if !IsSupportedSpecialization("Bedroom 2", "bedroom", brief) {
		t.Fatal("expected numbered normalization ('Bedroom 2') from a plural numeral mention to be supported")
	}
}
