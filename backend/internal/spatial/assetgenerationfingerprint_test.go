package spatial

import "testing"

func TestComputeAssetGenerationFingerprint(t *testing.T) {
	a := computeAssetGenerationFingerprint("company_1", "artifact_1", "checksum_abc", 42)
	b := computeAssetGenerationFingerprint("company_1", "artifact_1", "checksum_abc", 42)
	if a != b {
		t.Error("expected the same inputs to produce the same fingerprint")
	}
	if a == "" {
		t.Error("expected a non-empty fingerprint")
	}

	differentSeed := computeAssetGenerationFingerprint("company_1", "artifact_1", "checksum_abc", 43)
	if a == differentSeed {
		t.Error("expected a different seed to change the fingerprint")
	}

	differentArtifact := computeAssetGenerationFingerprint("company_1", "artifact_2", "checksum_abc", 42)
	if a == differentArtifact {
		t.Error("expected a different sourceArtifactID to change the fingerprint")
	}

	differentChecksum := computeAssetGenerationFingerprint("company_1", "artifact_1", "checksum_xyz", 42)
	if a == differentChecksum {
		t.Error("expected a different source checksum to change the fingerprint")
	}

	differentCompany := computeAssetGenerationFingerprint("company_2", "artifact_1", "checksum_abc", 42)
	if a == differentCompany {
		t.Error("expected a different companyID to change the fingerprint")
	}
}
