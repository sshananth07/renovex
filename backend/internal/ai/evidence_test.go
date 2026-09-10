package ai

import "testing"

const condoBrief = `Full renovation of a 3-bedroom condominium unit. Scope includes the
master bedroom, ensuite bathroom, two additional bedrooms, and a shared
bathroom. Also redo the kitchen and the living and dining area. Include the
balcony and the utility yard. Replace damaged flooring where necessary.`

func TestIsGroundedExactSubstringMatches(t *testing.T) {
	if !IsGrounded("master bedroom", condoBrief) {
		t.Fatal("expected exact substring excerpt to be grounded")
	}
}

func TestIsGroundedNormalizesWhitespaceAndCase(t *testing.T) {
	if !IsGrounded("  MASTER   Bedroom ", condoBrief) {
		t.Fatal("expected case/whitespace-insensitive match to be grounded")
	}
}

func TestIsGroundedAllowsBoundedPhraseShortening(t *testing.T) {
	// The model may shorten "Replace damaged flooring where necessary" to a
	// shorter paraphrase; bounded token-overlap should still ground it.
	if !IsGrounded("damaged flooring necessary", condoBrief) {
		t.Fatal("expected shortened phrase with overlapping tokens to be grounded")
	}
}

func TestIsGroundedRejectsFabricatedExcerpt(t *testing.T) {
	// Regression: "Kitchen flooring should be replaced" was never said —
	// the brief only has a conditional, space-unspecified flooring
	// statement. This must NOT ground as explicit.
	if IsGrounded("Kitchen flooring should be replaced", condoBrief) {
		t.Fatal("expected fabricated excerpt to fail grounding")
	}
}

func TestIsGroundedRejectsUnrelatedText(t *testing.T) {
	if IsGrounded("install a swimming pool", condoBrief) {
		t.Fatal("expected unrelated excerpt to fail grounding")
	}
}

func TestIsGroundedEmptyExcerptFailsClosed(t *testing.T) {
	if IsGrounded("", condoBrief) {
		t.Fatal("expected empty excerpt to fail grounding, not pass trivially")
	}
}
