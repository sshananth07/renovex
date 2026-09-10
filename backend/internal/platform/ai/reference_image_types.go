package ai

// Reference-image wire types mirror the Python ai-service's
// ReferenceImageRequest/ReferenceImageResult schemas
// (ai-service/app/schemas/reference_image.py) field-for-field — pure
// transport DTOs, same "no domain knowledge" boundary as spatial_types.go.

// ReferenceTargetDimensions mirrors Python's ReferenceTargetDimensions.
type ReferenceTargetDimensions struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Depth  float64 `json:"depth"`
}

// ReferenceTarget mirrors Python's ReferenceTarget.
type ReferenceTarget struct {
	Kind             string                     `json:"kind"`
	ID               string                     `json:"id"`
	Category         string                     `json:"category"`
	DimensionsMeters *ReferenceTargetDimensions `json:"dimensionsMeters,omitempty"`
}

// ReferenceAssetGenerationSpec mirrors Python's AssetGenerationSpec.
type ReferenceAssetGenerationSpec struct {
	Category                    string `json:"category"`
	ShapeDescription            string `json:"shapeDescription"`
	PreserveCanonicalDimensions bool   `json:"preserveCanonicalDimensions"`
}

// ReferenceMaterialAppearance mirrors Python's MaterialAppearance.
type ReferenceMaterialAppearance struct {
	BaseColor      string `json:"baseColor"`
	MaterialFamily string `json:"materialFamily"`
	Roughness      string `json:"roughness"`
	Metallic       bool   `json:"metallic"`
}

// ReferenceRenderBrief mirrors Python's RenderBrief.
type ReferenceRenderBrief struct {
	View              string `json:"view"`
	Isolated          bool   `json:"isolated"`
	FullObjectVisible bool   `json:"fullObjectVisible"`
	Background        string `json:"background"`
	NoText            bool   `json:"noText"`
	NoPeople          bool   `json:"noPeople"`
	NoRoom            bool   `json:"noRoom"`
}

// ReferenceImageRequest mirrors Python's ReferenceImageRequest exactly —
// the internal Go->Python request body for
// POST /internal/v1/spatial/reference-images/generate.
type ReferenceImageRequest struct {
	SchemaVersion       string                       `json:"schemaVersion"`
	DesignSessionID     string                       `json:"designSessionId"`
	TurnID              string                       `json:"turnId"`
	PlanFingerprint     string                       `json:"planFingerprint"`
	Target              ReferenceTarget              `json:"target"`
	AssetGenerationSpec ReferenceAssetGenerationSpec `json:"assetGenerationSpec"`
	MaterialAppearance  *ReferenceMaterialAppearance `json:"materialAppearance,omitempty"`
	RenderBrief         ReferenceRenderBrief         `json:"renderBrief"`
	PromptVersion       string                       `json:"promptVersion"`
	Seed                int64                        `json:"seed"`
}

// ReferenceImageResponse mirrors Python's ReferenceImageResult exactly.
type ReferenceImageResponse struct {
	SchemaVersion     string `json:"schemaVersion"`
	ImageBase64       string `json:"imageBase64"`
	ContentType       string `json:"contentType"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	ProviderRequestID string `json:"providerRequestId,omitempty"`
	Seed              int64  `json:"seed"`
	PromptVersion     string `json:"promptVersion"`
}
