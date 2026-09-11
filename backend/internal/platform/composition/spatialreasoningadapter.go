package composition

import (
	"context"
	"errors"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// spatialReasoningAdapter adapts platformai.SpatialClient's transport DTOs
// to spatial.ElementReasoner's interface — the ONLY place spatial's
// primitive DesignReasoningContext is translated to the platform/ai wire
// contract, matching hunyuanProviderAdapter's exact "thin translation
// layer so internal/spatial never imports the platform package directly"
// precedent.
type spatialReasoningAdapter struct {
	client *platformai.SpatialClient
}

func NewSpatialReasoningAdapter(client *platformai.SpatialClient) *spatialReasoningAdapter {
	return &spatialReasoningAdapter{client: client}
}

func (a *spatialReasoningAdapter) ReasonElement(ctx context.Context, reasoningContext spatial.DesignReasoningContext) (spatial.ProposedSceneEditDelta, error) {
	req := toSpatialReasoningRequest(reasoningContext)
	resp, err := a.client.ReasonElement(ctx, req)
	if err != nil {
		return spatial.ProposedSceneEditDelta{}, mapPlatformSpatialError(err)
	}

	delta := fromSpatialProposedDelta(resp.Delta)
	if delta.Target.Kind != reasoningContext.Target.Kind || delta.Target.ID != reasoningContext.Target.ID {
		return spatial.ProposedSceneEditDelta{}, spatial.ErrDesignPlanTargetMismatch
	}
	return delta, nil
}

// mapPlatformSpatialError maps internal/platform/ai's transport sentinels
// to spatial's own design-reasoning sentinels — spatial never imports
// platform/ai's error types directly (consumer-defines-interface).
func mapPlatformSpatialError(err error) error {
	switch {
	case errors.Is(err, platformai.ErrProviderRateLimited):
		return spatial.ErrDesignReasoningProviderRejected
	case errors.Is(err, platformai.ErrProviderUnavailable), errors.Is(err, platformai.ErrServiceUnavailable):
		return spatial.ErrDesignReasoningProviderUnavailable
	case errors.Is(err, platformai.ErrProviderTimeout):
		return spatial.ErrDesignReasoningTimeout
	case errors.Is(err, platformai.ErrInvalidProviderResponse), errors.Is(err, platformai.ErrInvalidServiceResponse):
		return spatial.ErrDesignReasoningInvalidOutput
	case errors.Is(err, platformai.ErrInvalidRequest):
		return spatial.ErrDesignReasoningInvalidOutput
	default:
		// Unrecognized transport failure — treated as ambiguous provider
		// unavailability, never a definite local failure (spatial's design
		// service classifies this as needs_attention, not a plain failure).
		return spatial.ErrDesignReasoningProviderUnavailable
	}
}

func toSpatialReasoningRequest(c spatial.DesignReasoningContext) platformai.SpatialReasoningRequest {
	walls := make([]platformai.SpatialContextWall, 0, len(c.Walls))
	for _, w := range c.Walls {
		walls = append(walls, platformai.SpatialContextWall{ID: w.ID, Start: toSpatialPoint(w.Start), End: toSpatialPoint(w.End), Thickness: w.Thickness})
	}
	neighbors := make([]map[string]any, 0, len(c.Neighbors))
	for _, n := range c.Neighbors {
		neighbors = append(neighbors, map[string]any{
			"kind": n.Kind, "id": n.ID, "category": n.Category,
			"position": map[string]float64{"x": n.Position.X, "y": n.Position.Y, "z": n.Position.Z},
		})
	}

	var dimensions *platformai.SpatialRoomLocalPoint
	if c.Target.Dimensions != nil {
		p := toSpatialPoint(*c.Target.Dimensions)
		dimensions = &p
	}
	var attachedToWallID *string
	if c.Target.AttachedToWallID != "" {
		attachedToWallID = &c.Target.AttachedToWallID
	}

	allowedOps := []string{"move_relative_to_nearest_wall", "resize_axis"}

	return platformai.SpatialReasoningRequest{
		SchemaVersion:     1,
		TurnID:            c.TurnID,
		RoomDraftID:       c.RoomDraftID,
		RoomDraftRevision: c.RoomDraftRevision,
		SelectedElement: platformai.SpatialSelectedElement{
			Kind: string(c.Target.Kind), ID: c.Target.ID, Category: c.Target.Category,
			Transform:        toSpatialTransform(c.Target.Transform),
			Dimensions:       dimensions,
			AttachedToWallID: attachedToWallID,
			VisualAssetBound: c.Target.VisualAssetBound,
		},
		Context: platformai.SpatialReasoningNeighborhood{
			Walls: walls, Openings: []map[string]any{}, Neighbors: neighbors,
		},
		CurrentWorkingDesign:      toSpatialWorkingDesign(c.CurrentWorkingDesign),
		LastSuccessfulPlanSummary: emptyIfNil(c.LastSuccessfulPlanSummary),
		Instruction:               c.Instruction,
		AllowedSpatialOperations:  allowedOps,
		MaterialFamilyEnum:        []string{"fabric", "leather", "wood", "metal", "stone", "other"},
		RoughnessEnum:             []string{"matte", "satin", "glossy"},
		PreservationDefaults:      platformai.SpatialPreservationDefaults{Geometry: true, Material: true, Spatial: true},
	}
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func toSpatialPoint(p spatial.RoomLocalPoint) platformai.SpatialRoomLocalPoint {
	return platformai.SpatialRoomLocalPoint{X: p.X, Y: p.Y, Z: p.Z}
}

func toSpatialTransform(t spatial.RoomLocalTransform) platformai.SpatialRoomLocalTransform {
	return platformai.SpatialRoomLocalTransform{
		Position: toSpatialPoint(t.Position),
		Rotation: platformai.SpatialRoomLocalQuaternion{X: t.Rotation.X, Y: t.Rotation.Y, Z: t.Rotation.Z, W: t.Rotation.W},
	}
}

func toSpatialWorkingDesign(w spatial.WorkingDesign) platformai.SpatialWorkingDesign {
	out := platformai.SpatialWorkingDesign{ResolvedSpatialOperations: []map[string]any{}}
	if w.Geometry != nil {
		out.Geometry = &platformai.SpatialWorkingDesignGeometry{
			Category: w.Geometry.Category, ShapeDescription: w.Geometry.ShapeDescription,
			PreserveCanonicalDimensions: w.Geometry.PreserveCanonicalDimensions,
		}
	}
	if w.Material != nil {
		out.Material = &platformai.SpatialWorkingDesignMaterial{
			BaseColor: w.Material.BaseColor, MaterialFamily: w.Material.MaterialFamily,
			Roughness: w.Material.Roughness, Metallic: w.Material.Metallic,
		}
	}
	for _, op := range w.ResolvedSpatialOperations {
		out.ResolvedSpatialOperations = append(out.ResolvedSpatialOperations, op.Payload)
	}
	return out
}

func fromSpatialProposedDelta(d platformai.SpatialProposedSceneEditDelta) spatial.ProposedSceneEditDelta {
	blockers := make([]spatial.ProposedBlocker, 0, len(d.Blockers))
	for _, b := range d.Blockers {
		blockers = append(blockers, spatial.ProposedBlocker{Code: b.Code, Message: b.Message})
	}

	return spatial.ProposedSceneEditDelta{
		Target:      spatial.SpatialDesignTarget{Kind: spatial.DesignTargetKind(d.Target.Kind), ID: d.Target.ID},
		Intent:      spatial.DesignIntent(d.Intent),
		Summary:     d.Summary,
		Geometry:    fromSpatialGeometryChange(d.Geometry),
		Material:    fromSpatialMaterialChange(d.Material),
		Spatial:     fromSpatialSpatialChange(d.Spatial),
		Blockers:    blockers,
		Assumptions: d.Assumptions,
		ReviewNotes: d.ReviewNotes,
		Confidence:  d.Confidence,
	}
}

func fromSpatialGeometryChange(c platformai.SpatialGeometryChange) spatial.SectionChange {
	sc := spatial.SectionChange{Mode: spatial.SectionMode(c.Mode)}
	if c.Spec != nil {
		sc.GeometrySpec = &spatial.WorkingDesignGeometry{
			Category: c.Spec.Category, ShapeDescription: c.Spec.ShapeDescription,
			PreserveCanonicalDimensions: c.Spec.PreserveCanonicalDimensions,
		}
	}
	return sc
}

func fromSpatialMaterialChange(c platformai.SpatialMaterialChange) spatial.SectionChange {
	sc := spatial.SectionChange{Mode: spatial.SectionMode(c.Mode)}
	if c.Spec != nil {
		sc.MaterialSpec = &spatial.WorkingDesignMaterial{
			BaseColor: c.Spec.BaseColor, MaterialFamily: c.Spec.MaterialFamily,
			Roughness: c.Spec.Roughness, Metallic: c.Spec.Metallic,
		}
	}
	return sc
}

func fromSpatialSpatialChange(c platformai.SpatialSpatialChange) spatial.SectionChange {
	sc := spatial.SectionChange{Mode: spatial.SectionMode(c.Mode)}
	if c.Spec == nil {
		return sc
	}
	kind, _ := c.Spec["kind"].(string)
	spec := &spatial.ProposedSpatialSpec{Kind: spatial.SpatialOperationKind(kind)}
	switch spec.Kind {
	case spatial.SpatialOpMoveRelativeToNearestWall:
		if relationship, ok := c.Spec["relationship"].(string); ok {
			spec.Relationship = spatial.SpatialRelationship(relationship)
		}
		if distance, ok := c.Spec["distanceMeters"].(float64); ok {
			spec.DistanceMeters = distance
		}
	case spatial.SpatialOpResizeAxis:
		if axis, ok := c.Spec["axis"].(string); ok {
			spec.Axis = spatial.SpatialAxis(axis)
		}
		if delta, ok := c.Spec["deltaMeters"].(float64); ok {
			spec.DeltaMeters = delta
			spec.HasDelta = true
		}
		if target, ok := c.Spec["targetMeters"].(float64); ok {
			spec.TargetMeters = target
			spec.HasTarget = true
		}
	}
	sc.SpatialSpec = spec
	return sc
}
