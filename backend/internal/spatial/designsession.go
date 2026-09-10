package spatial

import (
	"errors"
	"math"
	"time"
)

// DesignTargetKind is the closed set of RoomDraft element kinds RP4E1
// sessions may target. Restricted to object|fixture — the only two kinds
// with RP4D visual-asset bindings and complete category/transform/
// dimensions fields (RP4E1 design amendment §5, repository audit table:
// "Do not reuse either enum blindly. Define SpatialDesignTargetKind =
// object | fixture; reject wall/opening/servicePoint/constraint at session
// creation with typed 422"). Deliberately NOT reusing the Web
// SelectionKind or VisualAssetTargetKind enums — a distinct Go type for a
// distinct, narrower domain concern.
type DesignTargetKind string

const (
	DesignTargetKindObject  DesignTargetKind = "object"
	DesignTargetKindFixture DesignTargetKind = "fixture"
)

// ErrUnsupportedDesignTargetKind is returned when a session-creation
// request names a target kind outside {object, fixture} — wall, opening,
// servicePoint, and constraint are all rejected here (RP4E1 design
// amendment §1).
var ErrUnsupportedDesignTargetKind = errors.New("spatial: unsupported design session target kind")

// ErrDesignTargetNotFound is returned when the named (kind, id) does not
// resolve to an existing element of EXACTLY that kind in the given
// RoomDraft — including when an id exists under a DIFFERENT kind (an
// object's id presented as a fixture target never falls back to the
// object), so a cross-kind ID collision never silently resolves (RP4E1
// plan Task 5 Step 1 table case).
var ErrDesignTargetNotFound = errors.New("spatial: design session target not found in room draft")

// ErrInvalidDesignTargetTransform is returned when the resolved target's
// transform contains a non-finite (NaN/Infinity) position or rotation
// component — never trusted as a design-reasoning context input.
var ErrInvalidDesignTargetTransform = errors.New("spatial: design session target has an invalid (non-finite) transform")

// SpatialDesignTarget is the wire-level (kind, id) pair a caller supplies
// when creating a design session — the untrusted input to
// resolveDesignTarget.
type SpatialDesignTarget struct {
	Kind DesignTargetKind `bson:"kind" json:"kind"`
	ID   string           `bson:"id" json:"id"`
}

// AuthorizedDesignTarget is the Go-resolved, tenant-verified target a
// design session is pinned to — the ONLY form of target identity the
// reasoning pipeline (context building, Python dispatch, plan validation)
// is ever given. It carries exactly what RP4E1's internal Python contract
// needs to describe the selected element (RP4E1 plan "Internal Python
// contract": category, canonical transform, optional dimensions, fixture
// attachment state, and only visualAssetBound: bool) — never a URL,
// storage key, or credential.
type AuthorizedDesignTarget struct {
	Kind             DesignTargetKind
	ID               string
	Category         string
	Transform        RoomLocalTransform
	Dimensions       *RoomLocalPoint
	AttachedToWallID string
	VisualAssetBound bool
}

// resolveDesignTarget resolves target against draft's authoritative
// Objects/Fixtures, enforcing that:
//  1. only object|fixture kinds are supported (ErrUnsupportedDesignTargetKind);
//  2. the id must exist under EXACTLY the requested kind — an id that
//     exists only under the OTHER kind is ErrDesignTargetNotFound, not a
//     silent cross-kind resolution;
//  3. the resolved element's transform must be entirely finite
//     (ErrInvalidDesignTargetTransform) — a capture artifact with a
//     corrupted/degenerate transform is never handed to Python or used for
//     fit calculations.
//
// Missing Dimensions is NOT an error here — resolution succeeds with a nil
// Dimensions, and it is downstream spatial-operation resolution's job to
// block a SPATIAL change (never a material/geometry-only one) that
// requires authoritative dimensions that are not present (RP4E1 plan
// "Missing selected dimensions is a blocker for spatial changes").
func resolveDesignTarget(draft RoomDraft, target SpatialDesignTarget) (AuthorizedDesignTarget, error) {
	switch target.Kind {
	case DesignTargetKindObject:
		for _, obj := range draft.Objects {
			if obj.ID != target.ID {
				continue
			}
			if !transformIsFinite(obj.Transform) {
				return AuthorizedDesignTarget{}, ErrInvalidDesignTargetTransform
			}
			return AuthorizedDesignTarget{
				Kind: DesignTargetKindObject, ID: obj.ID, Category: obj.Category,
				Transform: obj.Transform, Dimensions: obj.Dimensions,
				VisualAssetBound: obj.VisualAsset != nil,
			}, nil
		}
		return AuthorizedDesignTarget{}, ErrDesignTargetNotFound
	case DesignTargetKindFixture:
		for _, fx := range draft.Fixtures {
			if fx.ID != target.ID {
				continue
			}
			if !transformIsFinite(fx.Transform) {
				return AuthorizedDesignTarget{}, ErrInvalidDesignTargetTransform
			}
			return AuthorizedDesignTarget{
				Kind: DesignTargetKindFixture, ID: fx.ID, Category: string(fx.Category),
				Transform: fx.Transform, Dimensions: fx.Dimensions,
				AttachedToWallID: fx.ParentWallID, VisualAssetBound: fx.VisualAsset != nil,
			}, nil
		}
		return AuthorizedDesignTarget{}, ErrDesignTargetNotFound
	default:
		return AuthorizedDesignTarget{}, ErrUnsupportedDesignTargetKind
	}
}

// transformIsFinite reports whether every component of t's position and
// rotation is a finite float64 — the shared guard resolveDesignTarget,
// context-building, and fit geometry all use before trusting a transform.
func transformIsFinite(t RoomLocalTransform) bool {
	values := []float64{
		t.Position.X, t.Position.Y, t.Position.Z,
		t.Rotation.X, t.Rotation.Y, t.Rotation.Z, t.Rotation.W,
	}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// SpatialDesignSessionStatus is RP4E1's reachable session lifecycle — see
// RP4E1 design amendment §3: only "active" is produced/read in this slice.
type SpatialDesignSessionStatus string

const SpatialDesignSessionStatusActive SpatialDesignSessionStatus = "active"

// SpatialDesignSession is the durable per-(RoomDraft, target) conversational
// design-reasoning session record (RP4E1 plan "Go domain and persistence
// model"), persisted in the spatial_design_sessions collection.
type SpatialDesignSession struct {
	ID              string `bson:"_id,omitempty" json:"id"`
	CompanyID       string `bson:"companyId" json:"companyId"`
	ProjectID       string `bson:"projectId" json:"projectId"`
	SpaceID         string `bson:"spaceId" json:"spaceId"`
	RoomDraftID     string `bson:"roomDraftId" json:"roomDraftId"`
	CreatedByUserID string `bson:"createdByUserId" json:"createdByUserId"`

	ClientSessionID           string              `bson:"clientSessionId" json:"clientSessionId"`
	SessionRequestFingerprint string              `bson:"sessionRequestFingerprint" json:"-"`
	Target                    SpatialDesignTarget `bson:"target" json:"target"`
	// BasedOnRoomDraftRevision pins this session to its creation-time
	// RoomDraft revision (RP4E1 design amendment §1). A later RoomDraft
	// revision change makes the session stale and read-only — RP4E1 never
	// silently rebases it onto the new revision.
	BasedOnRoomDraftRevision int64 `bson:"basedOnRoomDraftRevision" json:"basedOnRoomDraftRevision"`

	Status SpatialDesignSessionStatus `bson:"status" json:"status"`
	// CurrentWorkingDesign is the session's latest PROPOSED cumulative
	// design state — the running merge of every successfully validated
	// turn's delta, reviewable but not yet bound to the RoomDraft. It is
	// distinct from AcceptedDesign (RP4E2/M8.5C amendment, plan repository-
	// findings row 1): a proposal and a persisted RoomDraft result cannot be
	// represented by the same field. CurrentWorkingDesign may differ from
	// AcceptedDesign while a plan is under review; Use Design (the only
	// write path) sets them equal and clears ResolvedSpatialOperations in
	// both, since the absolute placement is then owned by RoomDraft itself.
	CurrentWorkingDesign WorkingDesign `bson:"currentWorkingDesign" json:"currentWorkingDesign"`
	// AcceptedDesign is the last semantic geometry/material state actually
	// persisted to the RoomDraft via Use Design — nil until the session's
	// first acceptance. A later proposal's reasoning context may still see
	// AcceptedDesign's geometry/material, but EXECUTION (whether Hunyuan
	// generation is still required) compares working geometry against THIS
	// field, not against "any cumulative custom geometry" — so a material-
	// only refinement after an accepted geometry never wrongly re-requests
	// generation for a mesh that is already bound.
	AcceptedDesign *WorkingDesign `bson:"acceptedDesign,omitempty" json:"acceptedDesign,omitempty"`
	// AcceptedTurnID/AcceptedAttemptID identify the turn and generation
	// attempt behind the most recent acceptance — nil/empty until the first
	// Use Design. Set only by that write path (RP4E2 Gate 1), never inferred
	// from LatestReadyPlanTurnID.
	AcceptedTurnID        string `bson:"acceptedTurnId,omitempty" json:"acceptedTurnId,omitempty"`
	AcceptedAttemptID     string `bson:"acceptedAttemptId,omitempty" json:"acceptedAttemptId,omitempty"`
	LatestTurnID          string `bson:"latestTurnId,omitempty" json:"latestTurnId,omitempty"`
	LatestReadyPlanTurnID string `bson:"latestReadyPlanTurnId,omitempty" json:"latestReadyPlanTurnId,omitempty"`
	// LatestGenerationAttemptID/LatestReadyAttemptID track the newest
	// attempt (of any status) and the newest attempt to reach concept_ready,
	// respectively — deterministic session restore and Use eligibility
	// (RP4E2 plan). Regenerate leaves the previous ready concept viewable
	// (LatestReadyAttemptID unchanged) while a new attempt runs; only when
	// the NEW attempt itself becomes ready does it replace
	// LatestReadyAttemptID. Older ready attempts remain immutable history
	// but can never be accepted — only the attempt matching
	// LatestReadyAttemptID is eligible for Use Design.
	LatestGenerationAttemptID string `bson:"latestGenerationAttemptId,omitempty" json:"latestGenerationAttemptId,omitempty"`
	LatestReadyAttemptID      string `bson:"latestReadyAttemptId,omitempty" json:"latestReadyAttemptId,omitempty"`
	// ActiveTurnID is non-empty exactly while a turn is reserved/reasoning
	// for this session — a concurrent distinct request while it is set
	// returns design_turn_in_progress (RP4E1 plan's public contract).
	ActiveTurnID     string `bson:"activeTurnId,omitempty" json:"-"`
	LastTurnSequence int64  `bson:"lastTurnSequence" json:"-"`

	Revision      int64     `bson:"revision" json:"-"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
	SchemaVersion int       `bson:"schemaVersion" json:"schemaVersion"`
}

// SpatialDesignTurnStatus is RP4E1's reachable turn lifecycle (RP4E1
// design amendment §3) — confirmed/completed/cancelled belong to a later
// execution slice and are deliberately not modeled here.
type SpatialDesignTurnStatus string

const (
	SpatialDesignTurnStatusReserved       SpatialDesignTurnStatus = "reserved"
	SpatialDesignTurnStatusReasoning      SpatialDesignTurnStatus = "reasoning"
	SpatialDesignTurnStatusProposed       SpatialDesignTurnStatus = "proposed"
	SpatialDesignTurnStatusBlocked        SpatialDesignTurnStatus = "blocked"
	SpatialDesignTurnStatusFailed         SpatialDesignTurnStatus = "failed"
	SpatialDesignTurnStatusNeedsAttention SpatialDesignTurnStatus = "needs_attention"
	SpatialDesignTurnStatusSuperseded     SpatialDesignTurnStatus = "superseded"
	SpatialDesignTurnStatusStale          SpatialDesignTurnStatus = "stale"
)

// SpatialDesignTurn is one immutable reasoning turn (RP4E1 plan "Go domain
// and persistence model"), persisted in the spatial_design_turns
// collection. A turn is never overwritten after reaching a terminal
// status — refinement always creates a NEW turn.
type SpatialDesignTurn struct {
	ID                 string `bson:"_id,omitempty" json:"id"`
	CompanyID          string `bson:"companyId" json:"companyId"`
	SessionID          string `bson:"sessionId" json:"sessionId"`
	ClientRequestID    string `bson:"clientRequestId" json:"clientRequestId"`
	RequestFingerprint string `bson:"requestFingerprint" json:"-"`

	Sequence       int64  `bson:"sequence" json:"sequence"`
	PreviousTurnID string `bson:"previousTurnId,omitempty" json:"previousTurnId,omitempty"`
	// ParentPlanTurnID points only to the last SUCCESSFULLY validated
	// cumulative plan turn — a failed turn advances PreviousTurnID history
	// but never becomes a ParentPlanTurnID for a later turn.
	ParentPlanTurnID string `bson:"parentPlanTurnId,omitempty" json:"parentPlanTurnId,omitempty"`

	BaseSessionRevision      int64  `bson:"baseSessionRevision" json:"-"`
	BasedOnRoomDraftRevision int64  `bson:"basedOnRoomDraftRevision" json:"basedOnRoomDraftRevision"`
	Instruction              string `bson:"instruction" json:"instruction"`

	Status SpatialDesignTurnStatus `bson:"status" json:"status"`
	// ProviderStartedAt is recorded durably BEFORE the outbound GLM call —
	// the at-most-once dispatch marker (RP4E1 plan's global constraint: "Go
	// guarantees at-most-once dispatch per durable turn by recording
	// providerStartedAt before the call and never redispatching that
	// turn"). Once set, this turn is never dispatched again under any
	// circumstance.
	ProviderStartedAt *time.Time `bson:"providerStartedAt,omitempty" json:"-"`

	ProposedDelta   *ProposedSceneEditDelta `bson:"proposedDelta,omitempty" json:"-"`
	ValidatedPlan   *ValidatedSceneEditPlan `bson:"validatedPlan,omitempty" json:"-"`
	PlanFingerprint string                  `bson:"planFingerprint,omitempty" json:"planFingerprint,omitempty"`
	SafeFailureCode string                  `bson:"safeFailureCode,omitempty" json:"safeFailureCode,omitempty"`

	StartedAt     time.Time  `bson:"startedAt" json:"-"`
	CreatedAt     time.Time  `bson:"createdAt" json:"createdAt"`
	CompletedAt   *time.Time `bson:"completedAt,omitempty" json:"completedAt,omitempty"`
	SchemaVersion int        `bson:"schemaVersion" json:"schemaVersion"`
}
