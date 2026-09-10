package spatial

import (
	"errors"
	"time"
)

// DesignGenerationKind classifies what a DesignGenerationAttempt actually
// needs to produce, derived from the confirmed plan's execution flags
// (RP4E2/M8.5C plan): material_only never touches the Python reference
// service or Hunyuan; geometry/mixed both do.
type DesignGenerationKind string

const (
	DesignGenerationKindMaterialOnly DesignGenerationKind = "material_only"
	DesignGenerationKindGeometry     DesignGenerationKind = "geometry"
	DesignGenerationKindMixed        DesignGenerationKind = "mixed"
)

// DesignGenerationStatus is the attempt's durable lifecycle (RP4E2/M8.5C
// plan's exact state list). Provider-call checkpoints on the attempt itself
// (ReferenceProviderStartedAt) are the at-most-once dispatch markers,
// mirroring SpatialDesignTurn.ProviderStartedAt's convention exactly.
type DesignGenerationStatus string

const (
	DesignGenerationStatusReserved                  DesignGenerationStatus = "reserved"
	DesignGenerationStatusGeneratingReference       DesignGenerationStatus = "generating_reference"
	DesignGenerationStatusReferenceReady            DesignGenerationStatus = "reference_ready"
	DesignGenerationStatusAssetGenerationPending    DesignGenerationStatus = "asset_generation_pending"
	DesignGenerationStatusAssetGenerationProcessing DesignGenerationStatus = "asset_generation_processing"
	DesignGenerationStatusConceptReady              DesignGenerationStatus = "concept_ready"
	DesignGenerationStatusFailed                    DesignGenerationStatus = "failed"
	DesignGenerationStatusNeedsAttention            DesignGenerationStatus = "needs_attention"
	DesignGenerationStatusAbandoned                 DesignGenerationStatus = "abandoned"
	DesignGenerationStatusAccepted                  DesignGenerationStatus = "accepted"
)

// isTerminalDesignGenerationStatus mirrors isTerminalDesignTurnStatus's
// convention for the attempt lifecycle — a terminal attempt is never
// resumed or redispatched; Regenerate always creates a NEW attempt number.
func isTerminalDesignGenerationStatus(status DesignGenerationStatus) bool {
	switch status {
	case DesignGenerationStatusConceptReady, DesignGenerationStatusFailed,
		DesignGenerationStatusNeedsAttention, DesignGenerationStatusAbandoned,
		DesignGenerationStatusAccepted:
		return true
	default:
		return false
	}
}

// DesignReferenceImage is the durable record of the one Cloudflare
// FLUX/mock reference image an attempt generated — provider/model identity
// is stored for audit but never leaves the backend (RP4E2 plan: "Public
// progress exposes only stage, status, safe copy").
type DesignReferenceImage struct {
	ObjectKey         string
	ChecksumSHA256    string
	ContentType       string // image/jpeg or image/png
	SizeBytes         int64
	Width             int
	Height            int
	Provider          string // cloudflare_flux or mock; private DTO only
	ProviderRequestID string
	Model             string
	PromptVersion     string
	Seed              int64
	CreatedAt         time.Time
}

// ConceptBindingAction/ConceptAppearanceAction describe what Use Design
// must do to the RoomDraft for this concept's geometry/appearance
// respectively — computed once when the attempt reaches concept_ready, so
// the Use handler's fixed operation order (RP4E2 plan) never has to
// re-derive intent from raw working-design state.
type ConceptBindingAction string

const (
	ConceptBindingActionPreserve ConceptBindingAction = "preserve"
	ConceptBindingActionAssign   ConceptBindingAction = "assign"
	ConceptBindingActionClear    ConceptBindingAction = "clear"
)

type ConceptAppearanceAction string

const (
	ConceptAppearanceActionPreserve ConceptAppearanceAction = "preserve"
	ConceptAppearanceActionSet      ConceptAppearanceAction = "set"
	ConceptAppearanceActionClear    ConceptAppearanceAction = "clear"
)

// DesignConcept is an attempt's candidate result — the exact placement,
// dimensions, and geometry/appearance binding actions Use Design will apply
// to the session's target. VisualAsset is nil until the linked RP4E0 job
// publishes a VisualAssetVersion (geometry/mixed attempts only); a
// material_only attempt never populates it.
type DesignConcept struct {
	Target           SpatialDesignTarget
	Transform        RoomLocalTransform
	Dimensions       *RoomLocalPoint
	VisualAction     ConceptBindingAction
	AppearanceAction ConceptAppearanceAction
	Appearance       *VisualAppearance
	VisualAsset      *VisualAssetRef
}

// DesignGenerationAttempt is one durable Confirm/Regenerate attempt (RP4E2
// plan's canonical shape) — a separate record from SpatialDesignTurn:
// Confirm/Regenerate never mutate a turn, they create/replace an attempt
// tied to the turn's already-validated, already-fingerprinted plan.
type DesignGenerationAttempt struct {
	ID          string
	CompanyID   string
	ProjectID   string
	SpaceID     string
	RoomDraftID string

	SessionID       string
	TurnID          string
	PlanFingerprint string
	AttemptNumber   int64

	ClientRequestID    string
	RequestFingerprint string

	BasedOnRoomDraftRevision int64

	Kind   DesignGenerationKind
	Status DesignGenerationStatus
	// ActiveSlot is non-empty exactly while this attempt is non-terminal —
	// mirrors SpatialDesignSession.ActiveTurnID's "presence is the state"
	// convention, backing the partial unique index that enforces one
	// active attempt per turn.
	ActiveSlot string

	TargetSnapshot AuthorizedDesignTarget

	ReferenceProviderStartedAt *time.Time
	ReferenceImage             *DesignReferenceImage

	AssetGenerationJobID string

	Candidate DesignConcept

	SafeFailureCode string

	CancelClientRequestID    string
	CancelRequestFingerprint string

	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time

	CompletedAt *time.Time
	AbandonedAt *time.Time
	AcceptedAt  *time.Time

	SchemaVersion int
}

// DesignAcceptance is the immutable record of one Use Design transaction
// (RP4E2 plan) — the audit/idempotency record for the ONLY path that ever
// writes a design-session concept into the RoomDraft.
type DesignAcceptance struct {
	ID        string
	CompanyID string

	SessionID       string
	TurnID          string
	AttemptID       string
	PlanFingerprint string

	ClientRequestID    string
	RequestFingerprint string

	RoomDraftID                string
	BaseRoomDraftRevision      int64
	ResultingRoomDraftRevision int64

	AppliedOperationIDs []string

	AcceptedDesign WorkingDesign

	CreatedByUserID string
	CreatedAt       time.Time

	SchemaVersion int
}

// Sentinel errors for the RP4E2 generation-attempt/acceptance domain,
// following this package's existing "spatial owns its own sentinels"
// convention (design_service.go's ElementReasoner doc comment).
var (
	ErrDesignGenerationAttemptNotFound = errors.New("spatial: design generation attempt not found")
	ErrDesignGenerationRequestConflict = errors.New("spatial: design generation request id conflict")
	ErrDesignGenerationInProgress      = errors.New("spatial: a design generation attempt is already in progress for this turn")
	ErrDesignGenerationNotClaimable    = errors.New("spatial: design generation attempt not currently claimable")
	ErrDesignGenerationAlreadyTerminal = errors.New("spatial: design generation attempt already reached a terminal status")
	ErrDesignAcceptanceNotFound        = errors.New("spatial: design acceptance not found")
	ErrDesignAcceptanceRequestConflict = errors.New("spatial: design acceptance request id conflict")
	ErrDesignAttemptNotReady           = errors.New("spatial: design generation attempt is not concept_ready")
	ErrDesignAttemptNotLatestReady     = errors.New("spatial: design generation attempt is not the session's latest ready attempt")
	ErrDesignAttemptAbandoned          = errors.New("spatial: design generation attempt was abandoned and can never be used")
)
