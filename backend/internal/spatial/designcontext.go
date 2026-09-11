package spatial

import (
	"math"
	"sort"
)

// designContextMaxWalls/MaxOpenings/MaxNeighbors are the plan's context-
// minimization caps (RP4E1 plan "Context minimization and deterministic
// geometry"): nearest 8 walls, openings on those walls plus openings
// within the neighborhood radius capped at 8, objects/fixtures/constraints
// within the neighborhood radius capped at 16 total. Go sends Python only
// this bounded projection — never the full authoritative RoomDraft.
const (
	designContextMaxWalls     = 8
	designContextMaxOpenings  = 8
	designContextMaxNeighbors = 16
	// designContextMinRadiusMeters/designContextFootprintRadiusMultiplier
	// implement "max(3m, 2 x selected XZ footprint diagonal)".
	designContextMinRadiusMeters           = 3.0
	designContextFootprintRadiusMultiplier = 2.0
)

// DesignContextWall is the bounded wall projection sent to Python —
// mirrors Python's ContextWall (id, start, end, thickness).
type DesignContextWall struct {
	ID        string
	Start     RoomLocalPoint
	End       RoomLocalPoint
	Thickness *float64
}

// DesignContextNeighbor is a bounded object/fixture/constraint projection
// — only what Python needs to reason about spatial relationships, never
// full domain records.
type DesignContextNeighbor struct {
	Kind     string // "object" | "fixture" | "constraint"
	ID       string
	Category string
	Position RoomLocalPoint
}

// DesignReasoningContext is the bounded projection of an authoritative
// RoomDraft that Go sends to Python for one turn (RP4E1 plan "Context
// minimization"). It deliberately has no field for CompanyID, storage
// keys, credentials, or the full RoomDraft — only geometry Python needs to
// reason about the selected target's immediate surroundings.
type DesignReasoningContext struct {
	// These identifiers are copied from authoritative server state, never
	// supplied by the browser. Python validates them before provider selection
	// and uses TurnID as the safe cross-service correlation key.
	TurnID                    string
	RoomDraftID               string
	RoomDraftRevision         int64
	Target                    AuthorizedDesignTarget
	Walls                     []DesignContextWall
	Neighbors                 []DesignContextNeighbor
	CurrentWorkingDesign      WorkingDesign
	LastSuccessfulPlanSummary []string
	Instruction               string
}

// buildDesignReasoningContext selects the bounded neighborhood around
// target from draft: the nearest designContextMaxWalls walls by
// center-to-segment distance (stable tie-break by wall ID), and every
// object/fixture/constraint (excluding target itself) within
// max(3m, 2 x footprint diagonal), sorted by distance then kind then ID,
// capped at designContextMaxNeighbors. Selection is a pure, deterministic
// function of draft's contents — independent of slice arrival order (RP4E1
// plan Task 5 Step 2: "shuffled input order... deterministic nearest
// selection").
func buildDesignReasoningContext(draft RoomDraft, target AuthorizedDesignTarget, workingDesign WorkingDesign, lastSuccessfulPlanSummary []string, instruction string) (DesignReasoningContext, error) {
	targetCenter := target.Transform.Position

	type wallDistance struct {
		wall     RoomDraftWall
		distance float64
	}
	wallDistances := make([]wallDistance, 0, len(draft.Walls))
	for _, w := range draft.Walls {
		wallDistances = append(wallDistances, wallDistance{wall: w, distance: pointToSegmentDistanceXZ(targetCenter, w.Start, w.End)})
	}
	sort.Slice(wallDistances, func(i, j int) bool {
		if wallDistances[i].distance != wallDistances[j].distance {
			return wallDistances[i].distance < wallDistances[j].distance
		}
		return wallDistances[i].wall.ID < wallDistances[j].wall.ID // stable tie-break
	})
	walls := make([]DesignContextWall, 0, designContextMaxWalls)
	for i, wd := range wallDistances {
		if i >= designContextMaxWalls {
			break
		}
		walls = append(walls, DesignContextWall{ID: wd.wall.ID, Start: wd.wall.Start, End: wd.wall.End, Thickness: wd.wall.Thickness})
	}

	radius := designContextMinRadiusMeters
	if target.Dimensions != nil {
		diagonal := math.Hypot(target.Dimensions.X, target.Dimensions.Z)
		if candidate := designContextFootprintRadiusMultiplier * diagonal; candidate > radius {
			radius = candidate
		}
	}

	type neighborDistance struct {
		neighbor DesignContextNeighbor
		distance float64
	}
	neighborDistances := make([]neighborDistance, 0)
	for _, obj := range draft.Objects {
		if target.Kind == DesignTargetKindObject && obj.ID == target.ID {
			continue
		}
		d := distanceXZ(targetCenter, obj.Transform.Position)
		if d > radius {
			continue
		}
		neighborDistances = append(neighborDistances, neighborDistance{
			neighbor: DesignContextNeighbor{Kind: "object", ID: obj.ID, Category: obj.Category, Position: obj.Transform.Position},
			distance: d,
		})
	}
	for _, fx := range draft.Fixtures {
		if target.Kind == DesignTargetKindFixture && fx.ID == target.ID {
			continue
		}
		d := distanceXZ(targetCenter, fx.Transform.Position)
		if d > radius {
			continue
		}
		neighborDistances = append(neighborDistances, neighborDistance{
			neighbor: DesignContextNeighbor{Kind: "fixture", ID: fx.ID, Category: string(fx.Category), Position: fx.Transform.Position},
			distance: d,
		})
	}
	for _, c := range draft.Constraints {
		d := distanceXZ(targetCenter, c.Transform.Position)
		if d > radius {
			continue
		}
		neighborDistances = append(neighborDistances, neighborDistance{
			neighbor: DesignContextNeighbor{Kind: "constraint", ID: c.ID, Category: string(c.Kind), Position: c.Transform.Position},
			distance: d,
		})
	}
	sort.Slice(neighborDistances, func(i, j int) bool {
		if neighborDistances[i].distance != neighborDistances[j].distance {
			return neighborDistances[i].distance < neighborDistances[j].distance
		}
		if neighborDistances[i].neighbor.Kind != neighborDistances[j].neighbor.Kind {
			return neighborDistances[i].neighbor.Kind < neighborDistances[j].neighbor.Kind
		}
		return neighborDistances[i].neighbor.ID < neighborDistances[j].neighbor.ID
	})
	neighbors := make([]DesignContextNeighbor, 0, designContextMaxNeighbors)
	for i, nd := range neighborDistances {
		if i >= designContextMaxNeighbors {
			break
		}
		neighbors = append(neighbors, nd.neighbor)
	}

	return DesignReasoningContext{
		RoomDraftID: draft.ID, RoomDraftRevision: draft.Revision,
		Target: target, Walls: walls, Neighbors: neighbors,
		CurrentWorkingDesign: workingDesign, LastSuccessfulPlanSummary: lastSuccessfulPlanSummary,
		Instruction: instruction,
	}, nil
}

// distanceXZ is the planar (room-local XZ) Euclidean distance between two
// points, ignoring Y (height) — matches design spec's XZ-plane fit-helper
// convention.
func distanceXZ(a, b RoomLocalPoint) float64 {
	dx := a.X - b.X
	dz := a.Z - b.Z
	return math.Hypot(dx, dz)
}

// pointToSegmentDistanceXZ returns the shortest planar distance from p to
// the line segment [segStart, segEnd], projected onto the XZ plane.
func pointToSegmentDistanceXZ(p, segStart, segEnd RoomLocalPoint) float64 {
	segDX := segEnd.X - segStart.X
	segDZ := segEnd.Z - segStart.Z
	lengthSquared := segDX*segDX + segDZ*segDZ
	if lengthSquared == 0 {
		return distanceXZ(p, segStart)
	}
	t := ((p.X-segStart.X)*segDX + (p.Z-segStart.Z)*segDZ) / lengthSquared
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	closest := RoomLocalPoint{X: segStart.X + t*segDX, Z: segStart.Z + t*segDZ}
	return distanceXZ(p, closest)
}
