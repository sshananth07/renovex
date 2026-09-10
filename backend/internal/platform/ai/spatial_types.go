package ai

// Spatial reasoning wire types mirror the Python ai-service's
// SpatialReasoningRequest/ProposedSceneEditDelta schemas
// (ai-service/app/schemas/spatial_reasoning.py) field-for-field. These are
// pure transport DTOs — spatial's own domain types
// (backend/internal/spatial/designcontext.go, designplan.go) are mapped
// into these at the composition boundary
// (spatialreasoningadapter.go), never used directly here, so this package
// carries zero spatial-domain knowledge (matching this package's existing
// "no domain knowledge" boundary rule).

// SpatialRoomLocalPoint mirrors Python's RoomLocalPointModel.
type SpatialRoomLocalPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// SpatialRoomLocalQuaternion mirrors Python's RoomLocalQuaternionModel.
type SpatialRoomLocalQuaternion struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
	W float64 `json:"w"`
}

// SpatialRoomLocalTransform mirrors Python's RoomLocalTransformModel.
type SpatialRoomLocalTransform struct {
	Position SpatialRoomLocalPoint      `json:"position"`
	Rotation SpatialRoomLocalQuaternion `json:"rotation"`
}

// SpatialSelectedElement mirrors Python's SelectedElementContext.
type SpatialSelectedElement struct {
	Kind             string                    `json:"kind"`
	ID               string                    `json:"id"`
	Category         string                    `json:"category"`
	Transform        SpatialRoomLocalTransform `json:"transform"`
	Dimensions       *SpatialRoomLocalPoint    `json:"dimensions,omitempty"`
	AttachedToWallID *string                   `json:"attachedToWallId,omitempty"`
	VisualAssetBound bool                      `json:"visualAssetBound"`
}

// SpatialContextWall mirrors Python's ContextWall.
type SpatialContextWall struct {
	ID        string                `json:"id"`
	Start     SpatialRoomLocalPoint `json:"start"`
	End       SpatialRoomLocalPoint `json:"end"`
	Thickness *float64              `json:"thickness,omitempty"`
}

// SpatialReasoningNeighborhood mirrors Python's DesignReasoningNeighborhood.
type SpatialReasoningNeighborhood struct {
	Walls     []SpatialContextWall `json:"walls"`
	Openings  []map[string]any     `json:"openings"`
	Neighbors []map[string]any     `json:"neighbors"`
}

// SpatialWorkingDesignGeometry mirrors Python's GeometrySpec.
type SpatialWorkingDesignGeometry struct {
	Category                    string `json:"category"`
	ShapeDescription            string `json:"shapeDescription"`
	PreserveCanonicalDimensions bool   `json:"preserveCanonicalDimensions"`
}

// SpatialWorkingDesignMaterial mirrors Python's MaterialSpec.
type SpatialWorkingDesignMaterial struct {
	BaseColor      string `json:"baseColor"`
	MaterialFamily string `json:"materialFamily"`
	Roughness      string `json:"roughness"`
	Metallic       bool   `json:"metallic"`
}

// SpatialWorkingDesign mirrors Python's WorkingDesign.
type SpatialWorkingDesign struct {
	Geometry                  *SpatialWorkingDesignGeometry `json:"geometry,omitempty"`
	Material                  *SpatialWorkingDesignMaterial `json:"material,omitempty"`
	ResolvedSpatialOperations []map[string]any              `json:"resolvedSpatialOperations"`
}

// SpatialPreservationDefaults mirrors Python's PreservationDefaults.
type SpatialPreservationDefaults struct {
	Geometry bool `json:"geometry"`
	Material bool `json:"material"`
	Spatial  bool `json:"spatial"`
}

// SpatialReasoningRequest mirrors Python's SpatialReasoningRequest exactly
// — the internal Go->Python request body for
// POST /internal/v1/spatial/element-proposals/reason.
type SpatialReasoningRequest struct {
	SchemaVersion             int                          `json:"schemaVersion"`
	TurnID                    string                       `json:"turnId"`
	RoomDraftID               string                       `json:"roomDraftId"`
	RoomDraftRevision         int64                        `json:"roomDraftRevision"`
	SelectedElement           SpatialSelectedElement       `json:"selectedElement"`
	Context                   SpatialReasoningNeighborhood `json:"context"`
	CurrentWorkingDesign      SpatialWorkingDesign         `json:"currentWorkingDesign"`
	LastSuccessfulPlanSummary []string                     `json:"lastSuccessfulPlanSummary"`
	Instruction               string                       `json:"instruction"`
	AllowedSpatialOperations  []string                     `json:"allowedSpatialOperations"`
	MaterialFamilyEnum        []string                     `json:"materialFamilyEnum"`
	RoughnessEnum             []string                     `json:"roughnessEnum"`
	PreservationDefaults      SpatialPreservationDefaults  `json:"preservationDefaults"`
}

// SpatialTargetRef mirrors Python's SelectedTargetRef.
type SpatialTargetRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// SpatialGeometryChange mirrors Python's GeometryChange.
type SpatialGeometryChange struct {
	Mode string                        `json:"mode"`
	Spec *SpatialWorkingDesignGeometry `json:"spec,omitempty"`
}

// SpatialMaterialChange mirrors Python's MaterialChange.
type SpatialMaterialChange struct {
	Mode string                        `json:"mode"`
	Spec *SpatialWorkingDesignMaterial `json:"spec,omitempty"`
}

// SpatialSpatialChange mirrors Python's SpatialChange. Spec is left as a
// raw map — the closed discriminated union (move_relative_to_nearest_wall
// | resize_axis) is decoded/validated by spatial's own domain code
// (designplan.go), not by this transport package.
type SpatialSpatialChange struct {
	Mode string         `json:"mode"`
	Spec map[string]any `json:"spec,omitempty"`
}

// SpatialProposedBlocker mirrors Python's ProposedBlocker.
type SpatialProposedBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SpatialProposedSceneEditDelta mirrors Python's ProposedSceneEditDelta —
// the untrusted, Go-revalidated proposal returned inside
// SpatialReasoningResponse.
type SpatialProposedSceneEditDelta struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Target        SpatialTargetRef         `json:"target"`
	Intent        string                   `json:"intent"`
	Summary       []string                 `json:"summary"`
	Geometry      SpatialGeometryChange    `json:"geometry"`
	Material      SpatialMaterialChange    `json:"material"`
	Spatial       SpatialSpatialChange     `json:"spatial"`
	Blockers      []SpatialProposedBlocker `json:"blockers"`
	Assumptions   []string                 `json:"assumptions"`
	ReviewNotes   []string                 `json:"reviewNotes"`
	Confidence    float64                  `json:"confidence"`
}

// SpatialReasoningResponse mirrors Python's SpatialReasoningResult exactly.
type SpatialReasoningResponse struct {
	Delta         SpatialProposedSceneEditDelta `json:"delta"`
	Provider      string                        `json:"provider"`
	Model         string                        `json:"model"`
	PromptVersion string                        `json:"promptVersion"`
	SchemaVersion int                           `json:"schemaVersion"`
}
