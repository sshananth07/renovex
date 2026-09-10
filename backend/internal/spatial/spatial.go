// Package spatial owns Spatial Intelligence state — room capture, confirmed
// geometry versioning, and the current-room pointer for an existing Renovex
// Space. See docs/superpowers/specs/M8.5C-Spatial-Intelligence-Design-Spec-Refined.md
// §3-4.
//
// spatial never imports spaces, projects, or companies directly. It defines
// SpaceLookup, the one capability it needs from spaces, satisfied structurally
// by spaces.Service with no import in either direction (design spec §3, same
// pattern as spaces.ProjectLookup).
package spatial

import "time"

// CaptureStatus is the lifecycle state of a SpatialCapture (design spec
// §4.1). Transitions are enforced by Service, not by callers.
type CaptureStatus string

const (
	CaptureStatusDraft      CaptureStatus = "draft"
	CaptureStatusCapturing  CaptureStatus = "capturing"
	CaptureStatusUploading  CaptureStatus = "uploading"
	CaptureStatusUploaded   CaptureStatus = "uploaded"
	CaptureStatusReview     CaptureStatus = "review"
	CaptureStatusConfirmed  CaptureStatus = "confirmed"
	CaptureStatusSuperseded CaptureStatus = "superseded"
	// CaptureStatusFailed is a retry branch reachable from capturing or
	// uploading (design spec §4.1: "Failure/retry branches are allowed
	// during capture and upload").
	CaptureStatusFailed CaptureStatus = "failed"
)

// CaptureProvider identifies which capture source produced a SpatialCapture
// (design spec §8.5-§8.7, plan §RP3). RoomPlan is the active production
// provider; ArCore is reserved for the frozen Android provider if it is ever
// revived (design spec §0). Defaults to CaptureProviderRoomPlan for callers
// that do not specify one, since RoomPlan is the only active provider today.
type CaptureProvider string

const (
	CaptureProviderRoomPlan CaptureProvider = "roomplan"
	CaptureProviderArCore   CaptureProvider = "arcore"
)

// SpatialCapture represents one scanning attempt for one existing Renovex
// Space. A Space may have multiple captures (design spec §4), each an
// independently retained capture run — a later capture never overwrites an
// earlier one (design spec §8.22, plan §RP3).
type SpatialCapture struct {
	ID        string        `bson:"_id,omitempty" json:"id"`
	CompanyID string        `bson:"companyId" json:"companyId"`
	ProjectID string        `bson:"projectId" json:"projectId"`
	SpaceID   string        `bson:"spaceId" json:"spaceId"`
	Status    CaptureStatus `bson:"status" json:"status"`
	CreatedAt time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt time.Time     `bson:"updatedAt" json:"updatedAt"`
	// RoomVersionID is set only once this capture has been confirmed into an
	// immutable SpatialRoomVersion (status=confirmed).
	RoomVersionID string `bson:"roomVersionId,omitempty" json:"roomVersionId,omitempty"`
	// Provider identifies which capture source produced this run.
	Provider CaptureProvider `bson:"provider" json:"provider"`
	// CaptureNumber is this capture's 1-based ordinal among every capture
	// ever started for its Space (design spec §8.22's "#1", "#2", "#3" Scan
	// History display) — assigned once at creation and never reused, so it
	// stays a stable display identity independent of list ordering/deletion.
	CaptureNumber int `bson:"captureNumber" json:"captureNumber"`
	// RoomDraftID is set once this capture's RoomPlan output has been
	// normalized and persisted as a RoomDraft (plan §RP3's required
	// ordering: persist raw result -> normalize RoomDraft -> persist
	// RoomDraft -> THEN Room Review is presentable). Empty until that
	// ordering completes for this capture.
	RoomDraftID string `bson:"roomDraftId,omitempty" json:"roomDraftId,omitempty"`
	// ClientCaptureID is the client-generated stable ID for this capture
	// (iOS's LocalCaptureRun.id) — an optional idempotency key (plan
	// §RP3.5/§RP4B0) letting StartCapture be safely retried after a lost
	// response without creating a duplicate capture or advancing
	// CaptureNumber twice. Empty for callers that predate this field (Web,
	// or any client that does not maintain a local stable capture
	// identity) — StartCapture behaves exactly as before when it is empty.
	// Enforced unique per company by a partial Mongo index (only applies
	// when non-empty), never a plain unique index, so pre-existing
	// documents with no ClientCaptureID never collide with each other.
	ClientCaptureID string `bson:"clientCaptureId,omitempty" json:"clientCaptureId,omitempty"`
	SchemaVersion   int    `bson:"schemaVersion" json:"schemaVersion"`
}

// RoomVersionStatus distinguishes the current confirmed version from
// historical (superseded) versions of a Space's confirmed geometry.
type RoomVersionStatus string

const (
	RoomVersionStatusCurrent    RoomVersionStatus = "current"
	RoomVersionStatusSuperseded RoomVersionStatus = "superseded"
)

// SpatialRoomVersion is contractor-confirmed geometry (design spec §2.1,
// §4.2). Once created it is immutable — Service exposes no update path for
// its geometry fields, only the status transition current->superseded
// performed atomically alongside a new version's creation.
//
// V1 geometry payload (walls, openings, obstacles, service points) is
// deliberately deferred to Task 7 (Capture Review, Server Confirmation, and
// Authoritative Quantities); this task establishes only the version
// envelope and the current-room invariant.
type SpatialRoomVersion struct {
	ID            string            `bson:"_id,omitempty" json:"id"`
	CompanyID     string            `bson:"companyId" json:"companyId"`
	ProjectID     string            `bson:"projectId" json:"projectId"`
	SpaceID       string            `bson:"spaceId" json:"spaceId"`
	CaptureID     string            `bson:"captureId" json:"captureId"`
	Status        RoomVersionStatus `bson:"status" json:"status"`
	CreatedAt     time.Time         `bson:"createdAt" json:"createdAt"`
	SchemaVersion int               `bson:"schemaVersion" json:"schemaVersion"`
}

// SpatialSpaceState maintains the relationship between an existing Renovex
// Space and its spatial data, including the current room version pointer
// (design spec §3). It is created lazily on first capture rather than
// requiring an explicit provisioning step.
type SpatialSpaceState struct {
	ID                   string `bson:"_id,omitempty" json:"id"`
	CompanyID            string `bson:"companyId" json:"companyId"`
	ProjectID            string `bson:"projectId" json:"projectId"`
	SpaceID              string `bson:"spaceId" json:"spaceId"`
	CurrentRoomVersionID string `bson:"currentRoomVersionId,omitempty" json:"currentRoomVersionId,omitempty"`
	// Revision guards the CAS-protected transition that swaps
	// CurrentRoomVersionID (design spec §4.2, access.AccessGrant.Revision
	// pattern).
	Revision      int64     `bson:"revision" json:"-"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt" json:"updatedAt"`
	SchemaVersion int       `bson:"schemaVersion" json:"schemaVersion"`
}
