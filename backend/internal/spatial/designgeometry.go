package spatial

import (
	"errors"
	"math"
)

// ErrUnsupportedSpatialOperation is returned when a ProposedSpatialSpec's
// Kind is outside RP4E1's closed set (move_relative_to_nearest_wall |
// resize_axis) — this is a programming/contract error (Python's own schema
// already forbids this at its layer), kept as a defensive Go-side check
// rather than silently coercing an unknown kind into a no-op.
var ErrUnsupportedSpatialOperation = errors.New("spatial: unsupported spatial operation kind")

// FitStatus is the terminal classification of one resolveSpatialChanges
// call — Go-owned safety/fit classification (design spec §2.1, §19-21;
// never delegated to Python).
type FitStatus string

const (
	FitStatusClear   FitStatus = "clear"
	FitStatusWarning FitStatus = "warning"
	FitStatusBlocked FitStatus = "blocked"
)

// FitObservation is a non-blocking, informational note (e.g. unknown wall
// thickness) attached to a fit result.
type FitObservation struct {
	Code    string
	Message string
}

// FitWarning is a non-blocking but noteworthy fit outcome (e.g. reduced
// clearance) carrying the measured values that justify it.
type FitWarning struct {
	Code            string
	Message         string
	ClearanceBefore *float64
	ClearanceAfter  *float64
}

// FitBlocker is a hard rejection of a proposed spatial change.
type FitBlocker struct {
	Code    string
	Message string
}

// FitAnalysis is the Go-authoritative result of resolveSpatialChanges —
// never computed or asserted by Python.
type FitAnalysis struct {
	Status       FitStatus
	Observations []FitObservation
	Warnings     []FitWarning
	Blockers     []FitBlocker
}

// footprintRect is an oriented bounding rectangle in the room-local XZ
// plane: a center point, half-extents along its own local axes, and a
// rotation (radians) about Y.
type footprintRect struct {
	Center      RoomLocalPoint
	HalfExtentX float64
	HalfExtentZ float64
	RotationY   float64
}

// corners returns the four world-space XZ corners of the rectangle.
func (r footprintRect) corners() [4][2]float64 {
	cosT := math.Cos(r.RotationY)
	sinT := math.Sin(r.RotationY)
	local := [4][2]float64{
		{-r.HalfExtentX, -r.HalfExtentZ}, {r.HalfExtentX, -r.HalfExtentZ},
		{r.HalfExtentX, r.HalfExtentZ}, {-r.HalfExtentX, r.HalfExtentZ},
	}
	var world [4][2]float64
	for i, l := range local {
		world[i] = [2]float64{
			r.Center.X + l[0]*cosT - l[1]*sinT,
			r.Center.Z + l[0]*sinT + l[1]*cosT,
		}
	}
	return world
}

// quaternionYRotation extracts the rotation about Y from a unit quaternion
// assumed to represent a pure Y-axis rotation (the only rotation RP4E1's
// move/resize operations produce or consume).
func quaternionYRotation(q RoomLocalQuaternion) float64 {
	return 2 * math.Atan2(q.Y, q.W)
}

// buildFootprint constructs target's oriented bounding rectangle from its
// transform and dimensions in the room-local XZ plane (design spec fit
// helper step 1).
func buildFootprint(transform RoomLocalTransform, dimensions RoomLocalPoint) footprintRect {
	return footprintRect{
		Center:      transform.Position,
		HalfExtentX: dimensions.X / 2,
		HalfExtentZ: dimensions.Z / 2,
		RotationY:   quaternionYRotation(transform.Rotation),
	}
}

// roomPolygon attempts to reconstruct a closed, ordered room boundary from
// draft's walls. Returns ok=false if the walls do not form one closed,
// non-self-intersecting loop within cornerCoincidenceEpsilonMeters — the
// caller must then block spatial movement/resize with
// room_boundary_unresolvable rather than guessing (design spec fit helper
// step 2).
func roomPolygon(walls []RoomDraftWall) (points []RoomLocalPoint, ok bool) {
	if len(walls) < 3 {
		return nil, false
	}
	remaining := make([]RoomDraftWall, len(walls))
	copy(remaining, walls)

	ordered := []RoomLocalPoint{remaining[0].Start, remaining[0].End}
	remaining = append(remaining[:0], remaining[1:]...)

	for len(remaining) > 0 {
		last := ordered[len(ordered)-1]
		foundIndex := -1
		var nextPoint RoomLocalPoint
		for i, w := range remaining {
			if distanceXZ(last, w.Start) <= cornerCoincidenceEpsilonMeters {
				foundIndex, nextPoint = i, w.End
				break
			}
			if distanceXZ(last, w.End) <= cornerCoincidenceEpsilonMeters {
				foundIndex, nextPoint = i, w.Start
				break
			}
		}
		if foundIndex == -1 {
			return nil, false // chain broken: not a closed loop
		}
		ordered = append(ordered, nextPoint)
		remaining = append(remaining[:foundIndex], remaining[foundIndex+1:]...)
	}

	// The loop must close: the last point must coincide with the first.
	if distanceXZ(ordered[len(ordered)-1], ordered[0]) > cornerCoincidenceEpsilonMeters {
		return nil, false
	}
	return ordered[:len(ordered)-1], true
}

// pointInPolygonXZ reports whether p lies inside the closed polygon
// defined by points (room-local XZ plane), using the standard ray-casting
// algorithm.
func pointInPolygonXZ(p RoomLocalPoint, polygon []RoomLocalPoint) bool {
	inside := false
	n := len(polygon)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi, pj := polygon[i], polygon[j]
		if ((pi.Z > p.Z) != (pj.Z > p.Z)) &&
			(p.X < (pj.X-pi.X)*(p.Z-pi.Z)/(pj.Z-pi.Z)+pi.X) {
			inside = !inside
		}
	}
	return inside
}

// footprintFullyInsidePolygon reports whether every corner of rect lies
// inside polygon.
func footprintFullyInsidePolygon(rect footprintRect, polygon []RoomLocalPoint) bool {
	for _, c := range rect.corners() {
		if !pointInPolygonXZ(RoomLocalPoint{X: c[0], Z: c[1]}, polygon) {
			return false
		}
	}
	return true
}

// nearestWall resolves the closest wall to center by point-to-segment
// distance, with a stable ID tie-break — matches
// buildDesignReasoningContext's own nearest-wall selection so the SAME
// wall a contractor sees in context is the one a "move away from the
// wall" instruction resolves against.
func nearestWall(center RoomLocalPoint, walls []RoomDraftWall) (RoomDraftWall, bool) {
	if len(walls) == 0 {
		return RoomDraftWall{}, false
	}
	best := walls[0]
	bestDistance := pointToSegmentDistanceXZ(center, best.Start, best.End)
	for _, w := range walls[1:] {
		d := pointToSegmentDistanceXZ(center, w.Start, w.End)
		if d < bestDistance || (d == bestDistance && w.ID < best.ID) {
			best, bestDistance = w, d
		}
	}
	return best, true
}

// wallInwardNormalXZ derives the wall's interior-facing unit normal from
// the room polygon's winding order (design spec fit helper step 3) — never
// guessed from wall direction alone, since a wall's direction alone is
// ambiguous as to which side is "inward."
func wallInwardNormalXZ(wall RoomDraftWall, polygon []RoomLocalPoint) (dx, dz float64) {
	// Wall direction unit vector.
	wdx := wall.End.X - wall.Start.X
	wdz := wall.End.Z - wall.Start.Z
	length := math.Hypot(wdx, wdz)
	if length == 0 {
		return 0, 0
	}
	wdx, wdz = wdx/length, wdz/length

	// Two candidate normals (perpendicular to wall direction); pick the one
	// pointing toward the polygon's centroid (interior).
	n1x, n1z := -wdz, wdx
	centroid := polygonCentroid(polygon)
	midX, midZ := (wall.Start.X+wall.End.X)/2, (wall.Start.Z+wall.End.Z)/2
	toCentroidX, toCentroidZ := centroid.X-midX, centroid.Z-midZ
	if n1x*toCentroidX+n1z*toCentroidZ >= 0 {
		return n1x, n1z
	}
	return -n1x, -n1z
}

func polygonCentroid(polygon []RoomLocalPoint) RoomLocalPoint {
	var sumX, sumZ float64
	for _, p := range polygon {
		sumX += p.X
		sumZ += p.Z
	}
	n := float64(len(polygon))
	return RoomLocalPoint{X: sumX / n, Z: sumZ / n}
}

// footprintSeparation computes the minimum center-to-center-adjusted
// clearance between two axis-unaligned rectangles by sampling the minimum
// distance between their corner/edge sets — a conservative approximation
// sufficient for RP4E1's warning/overlap classification without a full SAT
// polygon-distance implementation.
func footprintSeparation(a, b footprintRect) float64 {
	aCorners := a.corners()
	bCorners := b.corners()
	// If any corner of one rect is inside the other, they overlap (separation <= 0).
	aPoly := rectToPolygon(aCorners)
	bPoly := rectToPolygon(bCorners)
	for _, c := range aCorners {
		if pointInPolygonXZ(RoomLocalPoint{X: c[0], Z: c[1]}, bPoly) {
			return -1
		}
	}
	for _, c := range bCorners {
		if pointInPolygonXZ(RoomLocalPoint{X: c[0], Z: c[1]}, aPoly) {
			return -1
		}
	}
	// Otherwise, the minimum corner-to-corner distance is a safe
	// (possibly slightly pessimistic) separation estimate.
	minDist := math.Inf(1)
	for _, ac := range aCorners {
		for _, bc := range bCorners {
			d := math.Hypot(ac[0]-bc[0], ac[1]-bc[1])
			if d < minDist {
				minDist = d
			}
		}
	}
	return minDist
}

func rectToPolygon(corners [4][2]float64) []RoomLocalPoint {
	points := make([]RoomLocalPoint, len(corners))
	for i, c := range corners {
		points[i] = RoomLocalPoint{X: c[0], Z: c[1]}
	}
	return points
}

// resolveSpatialChanges resolves delta's Spatial section (if any) against
// draft's authoritative geometry, producing absolute canonical operations
// plus a Go-authoritative FitAnalysis. It never mutates draft or applies
// anything — see this file's package-level doc and the RP4E1 design
// amendment for the "AI suggests, Go calculates" invariant this enforces.
func resolveSpatialChanges(draft RoomDraft, target AuthorizedDesignTarget, previousWorking WorkingDesign, delta ProposedSceneEditDelta) ([]ResolvedSpatialOperation, FitAnalysis, error) {
	if delta.Spatial.Mode != SectionModeReplace {
		return nil, FitAnalysis{Status: FitStatusClear}, nil
	}
	spec := delta.Spatial.SpatialSpec
	if spec == nil {
		return nil, FitAnalysis{}, ErrInvalidSectionChange
	}
	if spec.Kind != SpatialOpMoveRelativeToNearestWall && spec.Kind != SpatialOpResizeAxis {
		return nil, FitAnalysis{}, ErrUnsupportedSpatialOperation
	}

	if target.Dimensions == nil {
		return nil, FitAnalysis{
			Status:   FitStatusBlocked,
			Blockers: []FitBlocker{{Code: "missing_dimensions", Message: "The selected element has no known dimensions, so spatial changes cannot be safely resolved."}},
		}, nil
	}

	polygon, polygonOK := roomPolygon(draft.Walls)
	if !polygonOK {
		return nil, FitAnalysis{
			Status:   FitStatusBlocked,
			Blockers: []FitBlocker{{Code: "room_boundary_unresolvable", Message: "The room boundary could not be resolved from the current wall geometry, so spatial movement is blocked."}},
		}, nil
	}

	currentFootprint := buildFootprint(target.Transform, *target.Dimensions)
	newTransform := target.Transform
	var op ResolvedSpatialOperation
	var observations []FitObservation

	switch spec.Kind {
	case SpatialOpMoveRelativeToNearestWall:
		wall, found := nearestWall(target.Transform.Position, draft.Walls)
		if !found {
			return nil, FitAnalysis{
				Status:   FitStatusBlocked,
				Blockers: []FitBlocker{{Code: "room_boundary_unresolvable", Message: "No wall could be resolved to move relative to."}},
			}, nil
		}
		if wall.Thickness == nil {
			observations = append(observations, FitObservation{Code: "unknown_wall_thickness", Message: "The nearest wall's thickness is not confirmed; clearance to it is approximate."})
		}
		nx, nz := wallInwardNormalXZ(wall, polygon)
		sign := 1.0
		if spec.Relationship == SpatialRelationshipToward {
			sign = -1.0
		}
		newTransform.Position.X = target.Transform.Position.X + sign*nx*spec.DistanceMeters
		newTransform.Position.Z = target.Transform.Position.Z + sign*nz*spec.DistanceMeters

		// Payload keys/shape must match MoveObjectOperation/MoveFixtureOperation's
		// ACTUAL json tags exactly (editoperation.go) — this is the payload
		// a future Use Design decodes back through decodeEditOperation and
		// applies via the existing EditOperation pipeline, so it is never a
		// bespoke shape invented here. MoveObjectOperation takes only a
		// position; MoveFixtureOperation takes a full transform (rotation
		// included) since fixtures can be wall-mounted — RP4E1's spatial
		// operations never rotate, so the existing rotation is carried
		// through unchanged.
		kind := EditOpMoveObject
		var payload map[string]any
		if target.Kind == DesignTargetKindFixture {
			kind = EditOpMoveFixture
			payload = map[string]any{"fixtureId": target.ID, "transform": map[string]any{
				"position": map[string]float64{"x": newTransform.Position.X, "y": newTransform.Position.Y, "z": newTransform.Position.Z},
				"rotation": map[string]float64{"x": target.Transform.Rotation.X, "y": target.Transform.Rotation.Y, "z": target.Transform.Rotation.Z, "w": target.Transform.Rotation.W},
			}}
		} else {
			payload = map[string]any{"objectId": target.ID, "position": map[string]float64{
				"x": newTransform.Position.X, "y": newTransform.Position.Y, "z": newTransform.Position.Z,
			}}
		}
		op = ResolvedSpatialOperation{Kind: kind, Payload: payload}

	case SpatialOpResizeAxis:
		newDimensions := *target.Dimensions
		current := axisValue(newDimensions, spec.Axis)
		var newValue float64
		if spec.HasTarget {
			newValue = spec.TargetMeters
		} else {
			newValue = current + spec.DeltaMeters
		}
		if newValue <= 0 {
			return nil, FitAnalysis{
				Status:   FitStatusBlocked,
				Blockers: []FitBlocker{{Code: "invalid_resize", Message: "The requested resize would produce a non-positive dimension."}},
			}, nil
		}
		setAxisValue(&newDimensions, spec.Axis, newValue)
		currentFootprint = buildFootprint(target.Transform, newDimensions)

		// ResizeObjectOperation/ResizeFixtureOperation both take
		// {<id-field>, dimensions} — no axis field exists on either (a
		// resize always supplies the full resulting Dimensions, matching
		// their actual json tags in editoperation.go).
		kind := EditOpResizeObject
		idField := "objectId"
		if target.Kind == DesignTargetKindFixture {
			kind = EditOpResizeFixture
			idField = "fixtureId"
		}
		payload := map[string]any{idField: target.ID, "dimensions": map[string]float64{
			"x": newDimensions.X, "y": newDimensions.Y, "z": newDimensions.Z,
		}}
		op = ResolvedSpatialOperation{Kind: kind, Payload: payload}
	}

	candidateFootprint := currentFootprint
	if spec.Kind == SpatialOpMoveRelativeToNearestWall {
		candidateFootprint = buildFootprint(newTransform, *target.Dimensions)
	}

	if !footprintFullyInsidePolygon(candidateFootprint, polygon) {
		return nil, FitAnalysis{
			Status:       FitStatusBlocked,
			Blockers:     []FitBlocker{{Code: "outside_room_boundary", Message: "The resulting position would fall outside the room boundary."}},
			Observations: observations,
		}, nil
	}

	var warnings []FitWarning
	for _, obj := range draft.Objects {
		if target.Kind == DesignTargetKindObject && obj.ID == target.ID {
			continue
		}
		if obj.Dimensions == nil {
			continue
		}
		neighborFootprint := buildFootprint(obj.Transform, *obj.Dimensions)
		before := footprintSeparation(currentFootprint, neighborFootprint)
		after := footprintSeparation(candidateFootprint, neighborFootprint)
		if after <= 0 {
			return nil, FitAnalysis{
				Status:       FitStatusBlocked,
				Blockers:     []FitBlocker{{Code: "collision", Message: "The resulting position would overlap another element (" + obj.ID + ")."}},
				Observations: observations,
			}, nil
		}
		if after < before {
			b, a := before, after
			warnings = append(warnings, FitWarning{
				Code: "reduced_clearance", Message: "Clearance to " + obj.ID + " is reduced by this change.",
				ClearanceBefore: &b, ClearanceAfter: &a,
			})
		}
	}

	status := FitStatusClear
	if len(warnings) > 0 {
		status = FitStatusWarning
	}
	return []ResolvedSpatialOperation{op}, FitAnalysis{Status: status, Observations: observations, Warnings: warnings}, nil
}

func axisValue(p RoomLocalPoint, axis SpatialAxis) float64 {
	switch axis {
	case SpatialAxisX:
		return p.X
	case SpatialAxisY:
		return p.Y
	default:
		return p.Z
	}
}

func setAxisValue(p *RoomLocalPoint, axis SpatialAxis, value float64) {
	switch axis {
	case SpatialAxisX:
		p.X = value
	case SpatialAxisY:
		p.Y = value
	default:
		p.Z = value
	}
}
