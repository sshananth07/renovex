package spatial

import "context"

// ReferenceImageTarget is the bounded target description a
// ReferenceImageGenerator call needs — never a full AuthorizedDesignTarget
// (which carries fields the reference-image prompt has no use for) and
// never raw RoomDraft JSON (RP4E2 plan's explicit boundary: "It never
// includes arbitrary RoomDraft JSON, URLs, tenant data, or credentials").
type ReferenceImageTarget struct {
	Kind       DesignTargetKind
	ID         string
	Category   string
	Dimensions *RoomLocalPoint
}

// ReferenceImageGenerationRequest is ReferenceImageGenerator's input —
// consumer-defined (spatial declares the interface it needs; the
// composition root supplies a Python-backed implementation), matching
// ElementReasoner's exact "spatial owns zero platform/ai import" convention
// (design_service.go's ElementReasoner doc comment).
type ReferenceImageGenerationRequest struct {
	DesignSessionID string
	TurnID          string
	PlanFingerprint string
	Target          ReferenceImageTarget
	Geometry        WorkingDesignGeometry
	Appearance      *VisualAppearance
	PromptVersion   string
	Seed            int64
}

// ReferenceImageGenerationResult is ReferenceImageGenerator's output — the
// validated image bytes plus audit-only provider metadata. This result is
// never returned to a client directly; the design generation service
// stores it under R2 and exposes only DesignReferenceImage's safe subset.
type ReferenceImageGenerationResult struct {
	ImageBytes        []byte
	ContentType       string
	Provider          string
	Model             string
	ProviderRequestID string
	Seed              int64
	PromptVersion     string
}

// ReferenceImageGenerator is the ONE outbound call a design generation
// attempt's reference phase makes (RP4E2 Gate 2) — consumer-defined,
// mirroring ElementReasoner exactly. This package has zero import of
// internal/platform/ai; the composition root supplies a Python-backed
// implementation (see composition/referenceimageadapter.go).
type ReferenceImageGenerator interface {
	GenerateReference(ctx context.Context, req ReferenceImageGenerationRequest) (ReferenceImageGenerationResult, error)
}
