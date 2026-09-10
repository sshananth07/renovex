package spatial

import "testing"

func basePlan() ValidatedSceneEditPlan {
	return ValidatedSceneEditPlan{
		Target:                   SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_sofa_123"},
		BasedOnRoomDraftRevision: 17,
		WorkingDesign: WorkingDesign{
			Geometry: &WorkingDesignGeometry{Category: "sofa", ShapeDescription: "Curved sofa", PreserveCanonicalDimensions: true},
			Material: &WorkingDesignMaterial{BaseColor: "#c8a464", MaterialFamily: "fabric", Roughness: "matte"},
			ResolvedSpatialOperations: []ResolvedSpatialOperation{
				{Kind: EditOpMoveObject, Payload: map[string]any{"objectId": "object_sofa_123", "position": map[string]float64{"x": 1, "y": 0, "z": 2}}},
			},
		},
		Fit: FitAnalysis{Status: FitStatusClear},
		Execution: DesignExecutionFlags{
			TurnRequiresAssetGeneration: false, HunyuanRequired: true, RequiresConfirmation: true, Executable: true,
		},
	}
}

func TestComputeDesignPlanFingerprint_IdenticalPlansHashEqual(t *testing.T) {
	a := basePlan()
	b := basePlan()

	fpA, err := computeDesignPlanFingerprint(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fpB, err := computeDesignPlanFingerprint(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fpA != fpB {
		t.Fatalf("expected identical plans to hash equal, got %s vs %s", fpA, fpB)
	}
}

func TestComputeDesignPlanFingerprint_MapKeyOrderDoesNotAffectHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	// Reassign payload map with different insertion order — Go map
	// iteration order is randomized, so this alone already exercises
	// nondeterminism if the fingerprint encoder is not order-independent,
	// but reconstruct explicitly with reversed key insertion for clarity.
	b.WorkingDesign.ResolvedSpatialOperations[0].Payload = map[string]any{
		"position": map[string]float64{"z": 2, "y": 0, "x": 1}, "objectId": "object_sofa_123",
	}

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA != fpB {
		t.Fatalf("expected map key order to not affect hash, got %s vs %s", fpA, fpB)
	}
}

func TestComputeDesignPlanFingerprint_TargetChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.Target.ID = "object_other_999"

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected target change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_RevisionChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.BasedOnRoomDraftRevision = 18

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected revision change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_GeometryChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.WorkingDesign.Geometry.ShapeDescription = "A totally different shape"

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected geometry change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_MaterialChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.WorkingDesign.Material.BaseColor = "#2f4f3a"

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected material change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_ResolvedOperationChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.WorkingDesign.ResolvedSpatialOperations[0].Payload["position"] = map[string]float64{"x": 99, "y": 0, "z": 2}

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected resolved operation change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_FitResultChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.Fit = FitAnalysis{Status: FitStatusWarning, Warnings: []FitWarning{{Code: "reduced_clearance", Message: "x"}}}

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected fit result change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_ExecutionFlagChangeAltersHash(t *testing.T) {
	a := basePlan()
	b := basePlan()
	b.Execution.HunyuanRequired = false

	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA == fpB {
		t.Fatal("expected execution flag change to alter hash")
	}
}

func TestComputeDesignPlanFingerprint_ConfidenceDoesNotAffectHash(t *testing.T) {
	// Confidence is not even a field on ValidatedSceneEditPlan — it lives
	// only on the turn's Review metadata, which computeDesignPlanFingerprint
	// never receives. This test documents that boundary via the plan's own
	// type shape rather than needing a confidence field to vary.
	a := basePlan()
	b := basePlan()
	fpA, _ := computeDesignPlanFingerprint(a)
	fpB, _ := computeDesignPlanFingerprint(b)
	if fpA != fpB {
		t.Fatal("expected identical plans (confidence is not part of the fingerprinted struct) to hash equal")
	}
}
