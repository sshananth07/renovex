package ai

import "testing"

func TestSpaceFingerprintSameInputSameHash(t *testing.T) {
	in := SpaceFingerprintInput{
		ScopeBrief: "Full renovation of a condo.",
		ExistingSpaces: []FingerprintSpaceRef{
			{ID: "s1", Name: "Kitchen", Type: "kitchen"},
		},
	}
	if SpaceFingerprint(in) != SpaceFingerprint(in) {
		t.Fatal("expected identical input to produce identical fingerprint")
	}
}

func TestSpaceFingerprintOrderIndependent(t *testing.T) {
	a := SpaceFingerprintInput{
		ScopeBrief: "brief",
		ExistingSpaces: []FingerprintSpaceRef{
			{ID: "s1", Name: "Kitchen", Type: "kitchen"},
			{ID: "s2", Name: "Bath", Type: "bathroom"},
		},
	}
	b := SpaceFingerprintInput{
		ScopeBrief: "brief",
		ExistingSpaces: []FingerprintSpaceRef{
			{ID: "s2", Name: "Bath", Type: "bathroom"},
			{ID: "s1", Name: "Kitchen", Type: "kitchen"},
		},
	}
	if SpaceFingerprint(a) != SpaceFingerprint(b) {
		t.Fatal("expected slice input order to not affect fingerprint after canonical sorting")
	}
}

func TestSpaceFingerprintChangedBriefChangesFingerprint(t *testing.T) {
	a := SpaceFingerprintInput{ScopeBrief: "brief one"}
	b := SpaceFingerprintInput{ScopeBrief: "brief two"}
	if SpaceFingerprint(a) == SpaceFingerprint(b) {
		t.Fatal("expected changed scope brief to change fingerprint")
	}
}

func TestSpaceFingerprintChangedExistingSpaceChangesFingerprint(t *testing.T) {
	a := SpaceFingerprintInput{ScopeBrief: "brief", ExistingSpaces: []FingerprintSpaceRef{{ID: "s1", Name: "Kitchen", Type: "kitchen"}}}
	b := SpaceFingerprintInput{ScopeBrief: "brief", ExistingSpaces: []FingerprintSpaceRef{{ID: "s1", Name: "Kitchen Renamed", Type: "kitchen"}}}
	if SpaceFingerprint(a) == SpaceFingerprint(b) {
		t.Fatal("expected changed Space identity/name/type to change fingerprint")
	}
}

func TestSpaceFingerprintHasVersionPrefix(t *testing.T) {
	fp := SpaceFingerprint(SpaceFingerprintInput{ScopeBrief: "x"})
	if len(fp) < len(fingerprintVersionPrefix) || fp[:len(fingerprintVersionPrefix)] != fingerprintVersionPrefix {
		t.Fatalf("expected fingerprint to start with %q, got %q", fingerprintVersionPrefix, fp)
	}
}

func TestWorkFingerprintOrderIndependentAndSensitive(t *testing.T) {
	base := WorkFingerprintInput{
		ScopeBrief: "brief",
		ConfirmedSpaces: []FingerprintSpaceRef{
			{ID: "s1", Name: "Kitchen", Type: "kitchen"},
			{ID: "s2", Name: "Bath", Type: "bathroom"},
		},
		ExistingWorkItems: []FingerprintWorkItemRef{
			{ID: "w1", SpaceID: "s1", Description: "Demo cabinets", WorkType: "demolition"},
		},
	}
	reordered := base
	reordered.ConfirmedSpaces = []FingerprintSpaceRef{
		{ID: "s2", Name: "Bath", Type: "bathroom"},
		{ID: "s1", Name: "Kitchen", Type: "kitchen"},
	}
	if WorkFingerprint(base) != WorkFingerprint(reordered) {
		t.Fatal("expected Space order to not affect Work fingerprint")
	}

	changed := base
	changed.ExistingWorkItems = []FingerprintWorkItemRef{
		{ID: "w1", SpaceID: "s1", Description: "Demo cabinets AND tiles", WorkType: "demolition"},
	}
	if WorkFingerprint(base) == WorkFingerprint(changed) {
		t.Fatal("expected changed WorkItem identity/description/type/space to change fingerprint")
	}
}

func TestResourceFingerprintOrderIndependentAndSensitive(t *testing.T) {
	base := ResourceFingerprintInput{
		ConfirmedWorkItems: []FingerprintWorkItemRef{
			{ID: "w1", Description: "Install tiles"},
			{ID: "w2", Description: "Install grout"},
		},
		ExistingRequirements: []FingerprintRequirementRef{
			{WorkItemID: "w1", ResourceType: "material", Name: "Tile Adhesive"},
		},
		MaterialCandidates: []FingerprintMaterialRef{
			{ID: "m1", Name: "Premium Tile Adhesive"},
		},
	}
	reordered := base
	reordered.ConfirmedWorkItems = []FingerprintWorkItemRef{
		{ID: "w2", Description: "Install grout"},
		{ID: "w1", Description: "Install tiles"},
	}
	if ResourceFingerprint(base) != ResourceFingerprint(reordered) {
		t.Fatal("expected WorkItem order to not affect Resource fingerprint")
	}

	changed := base
	changed.MaterialCandidates = []FingerprintMaterialRef{
		{ID: "m1", Name: "Premium Tile Adhesive"},
		{ID: "m2", Name: "Standard Tile Adhesive"},
	}
	if ResourceFingerprint(base) == ResourceFingerprint(changed) {
		t.Fatal("expected changed Material candidate set to change fingerprint")
	}

	changedExisting := base
	changedExisting.ExistingRequirements = nil
	if ResourceFingerprint(base) == ResourceFingerprint(changedExisting) {
		t.Fatal("expected changed existing-requirement state to change fingerprint")
	}
}
