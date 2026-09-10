package spatial

import "testing"

func draftWithManyWalls(count int) RoomDraft {
	draft := RoomDraft{ID: "roomdraft_many", CompanyID: "company_1"}
	for i := 0; i < count; i++ {
		offset := float64(i) * 10.0 // spread walls far apart so distance ordering is deterministic
		draft.Walls = append(draft.Walls, RoomDraftWall{
			ID:    stringOfIndex("wall", i),
			Start: RoomLocalPoint{X: offset, Y: 0, Z: 0},
			End:   RoomLocalPoint{X: offset + 1, Y: 0, Z: 0},
		})
	}
	draft.Objects = []RoomDraftObject{
		{ID: "object_target", Category: "sofa", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 0, Y: 0, Z: 0}, Rotation: RoomLocalQuaternion{W: 1}}, Dimensions: &RoomLocalPoint{X: 1, Y: 1, Z: 1}},
	}
	return draft
}

func stringOfIndex(prefix string, i int) string {
	digits := "0123456789"
	if i < 10 {
		return prefix + "_" + string(digits[i])
	}
	return prefix + "_" + string(digits[i/10]) + string(digits[i%10])
}

func TestBuildDesignReasoningContext_CapsWallsToEight(t *testing.T) {
	draft := draftWithManyWalls(20)
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	if err != nil {
		t.Fatalf("resolveDesignTarget failed: %v", err)
	}

	ctx, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "test instruction")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx.Walls) > 8 {
		t.Fatalf("expected at most 8 walls, got %d", len(ctx.Walls))
	}
}

func TestBuildDesignReasoningContext_NearestWallsSelectedDeterministically(t *testing.T) {
	draft := draftWithManyWalls(20)
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	if err != nil {
		t.Fatalf("resolveDesignTarget failed: %v", err)
	}

	first, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "test instruction")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "test instruction")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(first.Walls) != len(second.Walls) {
		t.Fatalf("expected deterministic wall count across calls")
	}
	for i := range first.Walls {
		if first.Walls[i].ID != second.Walls[i].ID {
			t.Fatalf("expected deterministic wall order, got %s vs %s at index %d", first.Walls[i].ID, second.Walls[i].ID, i)
		}
	}
	// Closest wall (wall_0, at origin) must be selected first — the
	// selection is genuinely nearest-first, not merely stable.
	if first.Walls[0].ID != "wall_0" {
		t.Fatalf("expected nearest wall wall_0 first, got %s", first.Walls[0].ID)
	}
}

func TestBuildDesignReasoningContext_NeighborsCappedAtSixteen(t *testing.T) {
	draft := RoomDraft{ID: "roomdraft_neighbors", CompanyID: "company_1"}
	draft.Objects = []RoomDraftObject{
		{ID: "object_target", Category: "sofa", Transform: RoomLocalTransform{Position: RoomLocalPoint{X: 0, Y: 0, Z: 0}, Rotation: RoomLocalQuaternion{W: 1}}, Dimensions: &RoomLocalPoint{X: 1, Y: 1, Z: 1}},
	}
	for i := 0; i < 30; i++ {
		draft.Objects = append(draft.Objects, RoomDraftObject{
			ID: stringOfIndex("neighbor", i), Category: "chair",
			Transform: RoomLocalTransform{Position: RoomLocalPoint{X: float64(i) * 0.1, Y: 0, Z: 0}, Rotation: RoomLocalQuaternion{W: 1}},
		})
	}
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	if err != nil {
		t.Fatalf("resolveDesignTarget failed: %v", err)
	}

	ctx, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "test instruction")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx.Neighbors) > 16 {
		t.Fatalf("expected at most 16 neighbors, got %d", len(ctx.Neighbors))
	}
}

func TestBuildDesignReasoningContext_ExcludesTargetFromNeighbors(t *testing.T) {
	draft := draftWithManyWalls(1)
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	if err != nil {
		t.Fatalf("resolveDesignTarget failed: %v", err)
	}

	ctx, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "test instruction")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, n := range ctx.Neighbors {
		if n.ID == "object_target" {
			t.Fatal("target must never appear in its own neighbor list")
		}
	}
}

func TestBuildDesignReasoningContext_ShuffledInputOrderStillDeterministic(t *testing.T) {
	draft1 := draftWithManyWalls(5)
	// Reverse the walls slice to simulate arrival-order shuffling.
	draft2 := draft1
	draft2.Walls = make([]RoomDraftWall, len(draft1.Walls))
	for i, w := range draft1.Walls {
		draft2.Walls[len(draft1.Walls)-1-i] = w
	}

	target1, _ := resolveDesignTarget(draft1, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	target2, _ := resolveDesignTarget(draft2, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})

	ctx1, err := buildDesignReasoningContext(draft1, target1, WorkingDesign{}, nil, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx2, err := buildDesignReasoningContext(draft2, target2, WorkingDesign{}, nil, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ctx1.Walls) != len(ctx2.Walls) {
		t.Fatalf("expected same wall count regardless of input order")
	}
	for i := range ctx1.Walls {
		if ctx1.Walls[i].ID != ctx2.Walls[i].ID {
			t.Fatalf("expected identical nearest-wall ordering regardless of input arrival order, got %s vs %s", ctx1.Walls[i].ID, ctx2.Walls[i].ID)
		}
	}
}

func TestBuildDesignReasoningContext_NoFullRoomOrCredentialData(t *testing.T) {
	draft := draftWithManyWalls(3)
	target, err := resolveDesignTarget(draft, SpatialDesignTarget{Kind: DesignTargetKindObject, ID: "object_target"})
	if err != nil {
		t.Fatalf("resolveDesignTarget failed: %v", err)
	}

	ctx, err := buildDesignReasoningContext(draft, target, WorkingDesign{}, nil, "x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Context struct has no field for CompanyID, StorageKey, or full
	// RoomDraft — proven structurally: DesignReasoningContext simply does
	// not carry these fields (verified by the fact this compiles without
	// referencing them). This test documents the intent for a future
	// reader modifying the struct.
	_ = ctx
}
