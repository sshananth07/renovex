package spatial

import "testing"

func TestConfirmRequestFingerprint_SameInputsSameHash(t *testing.T) {
	a := confirmRequestFingerprint("session_1", "turn_1", "fp_abc", 17)
	b := confirmRequestFingerprint("session_1", "turn_1", "fp_abc", 17)
	if a != b {
		t.Fatal("expected identical inputs to hash identically")
	}
}

func TestConfirmRequestFingerprint_DifferentRevisionDifferentHash(t *testing.T) {
	a := confirmRequestFingerprint("session_1", "turn_1", "fp_abc", 17)
	b := confirmRequestFingerprint("session_1", "turn_1", "fp_abc", 18)
	if a == b {
		t.Fatal("expected different revision to alter hash")
	}
}

func TestConfirmAndRegenerateFingerprints_SameInputsDifferentHash(t *testing.T) {
	// Confirm and Regenerate share the same field surface but must never
	// collide — a replayed Confirm request must never be mistaken for a
	// Regenerate request carrying identical session/turn/plan/revision.
	confirm := confirmRequestFingerprint("session_1", "turn_1", "fp_abc", 17)
	regenerate := regenerateRequestFingerprint("session_1", "turn_1", "fp_abc", 17)
	if confirm == regenerate {
		t.Fatal("expected confirm and regenerate fingerprints to differ even with identical fields")
	}
}

func TestCancelRequestFingerprint_SameInputsSameHash(t *testing.T) {
	a := cancelRequestFingerprint("session_1", "attempt_1")
	b := cancelRequestFingerprint("session_1", "attempt_1")
	if a != b {
		t.Fatal("expected identical inputs to hash identically")
	}
}

func TestCancelRequestFingerprint_DifferentAttemptDifferentHash(t *testing.T) {
	a := cancelRequestFingerprint("session_1", "attempt_1")
	b := cancelRequestFingerprint("session_1", "attempt_2")
	if a == b {
		t.Fatal("expected different attempt id to alter hash")
	}
}

func TestUseRequestFingerprint_SameInputsSameHash(t *testing.T) {
	a := useRequestFingerprint("session_1", "attempt_1", "fp_abc", 17)
	b := useRequestFingerprint("session_1", "attempt_1", "fp_abc", 17)
	if a != b {
		t.Fatal("expected identical inputs to hash identically")
	}
}

func TestUseRequestFingerprint_DifferentPlanFingerprintDifferentHash(t *testing.T) {
	a := useRequestFingerprint("session_1", "attempt_1", "fp_abc", 17)
	b := useRequestFingerprint("session_1", "attempt_1", "fp_xyz", 17)
	if a == b {
		t.Fatal("expected different plan fingerprint to alter hash")
	}
}

func TestAllFourFingerprintKinds_NeverCollideAcrossKinds(t *testing.T) {
	fingerprints := []string{
		confirmRequestFingerprint("s1", "t1", "fp1", 1),
		regenerateRequestFingerprint("s1", "t1", "fp1", 1),
		cancelRequestFingerprint("s1", "t1"),
		useRequestFingerprint("s1", "t1", "fp1", 1),
	}
	seen := map[string]bool{}
	for _, fp := range fingerprints {
		if seen[fp] {
			t.Fatalf("fingerprint collision across request kinds: %q", fp)
		}
		seen[fp] = true
	}
}
