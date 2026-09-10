package spatial

import (
	"context"
	"errors"
	"time"
)

// ErrCaptureNotFound is returned when a SpatialCapture lookup finds no
// match — including a capture that exists but belongs to a different
// company.
var ErrCaptureNotFound = errors.New("spatial: capture not found")

// ErrIllegalCaptureTransition is returned when a requested status
// transition is not permitted from the capture's current status (design
// spec §4.1).
var ErrIllegalCaptureTransition = errors.New("spatial: illegal capture status transition")

// ErrRoomVersionNotFound is returned when a SpatialRoomVersion lookup finds
// no match.
var ErrRoomVersionNotFound = errors.New("spatial: room version not found")

// ErrSpaceStateRevisionMismatch is returned when a CAS-guarded
// SpatialSpaceState mutation's expected revision no longer matches the
// stored document — another confirmation raced ahead of this one (design
// spec §4.2).
var ErrSpaceStateRevisionMismatch = errors.New("spatial: space state changed since it was read")

// ErrArtifactNotFound is returned when a SpatialArtifact lookup finds no
// match — including an artifact that exists but belongs to a different
// company.
var ErrArtifactNotFound = errors.New("spatial: artifact not found")

// ErrInvalidArtifactKind is returned when RequestArtifactUpload is given a
// kind outside the V1 allowlist.
var ErrInvalidArtifactKind = errors.New("spatial: invalid artifact kind")

// ErrArtifactTooLarge is returned when a declared size exceeds
// maxArtifactSizeBytes.
var ErrArtifactTooLarge = errors.New("spatial: artifact exceeds maximum allowed size")

// ErrInvalidArtifactContentType is returned when a content type is outside
// the V1 allowlist.
var ErrInvalidArtifactContentType = errors.New("spatial: content type not allowed for spatial artifacts")

// ErrArtifactObjectMissing is returned by FinalizeArtifactUpload when the
// ObjectStore has no content under the artifact's key — the client claims to
// have uploaded but the bytes are not actually present.
var ErrArtifactObjectMissing = errors.New("spatial: artifact object not found in object store")

// ErrArtifactChecksumMismatch is returned by FinalizeArtifactUpload when the
// uploaded object's actual SHA-256 does not match the client-declared
// checksum.
var ErrArtifactChecksumMismatch = errors.New("spatial: artifact checksum mismatch")

// ErrArtifactUploadTokenInvalid is returned when a presented upload/download
// token does not match the stored hash, is expired, or the artifact carries
// no active token of the requested kind.
var ErrArtifactUploadTokenInvalid = errors.New("spatial: artifact access token invalid or expired")

// ErrRoomDraftNotFound is returned when a RoomDraft lookup finds no match —
// including a draft that exists but belongs to a different company.
var ErrRoomDraftNotFound = errors.New("spatial: room draft not found")

// ErrRoomDraftRevisionMismatch is returned when a CAS-guarded RoomDraft
// mutation's expected revision no longer matches the stored document.
var ErrRoomDraftRevisionMismatch = errors.New("spatial: room draft changed since it was read")

// ErrInvalidElementProvenance is returned when a RoomDraftFixture/
// RoomDraftServicePoint/RoomDraftConstraint's CreatedBy/Provenance
// combination violates the discriminated invariant (RP4A): capture-created
// elements require real, non-nil Provenance; contractor-created elements
// forbid a synthesized one.
var ErrInvalidElementProvenance = errors.New("spatial: element CreatedBy/Provenance combination is invalid")

// ErrInvalidEditOperation is returned when an edit operation's own shape
// (required fields, numeric bounds, enum values) fails validation, before
// any target-existence check against a RoomDraft (RP4A, design spec §8.13).
var ErrInvalidEditOperation = errors.New("spatial: invalid edit operation")

// ErrEditTargetNotFound is returned when an edit operation references a
// wall/opening/object/fixture/service point/constraint ID that does not
// exist in the target RoomDraft (RP4A).
var ErrEditTargetNotFound = errors.New("spatial: edit operation target not found in room draft")

// ErrClientCaptureIDConflict is returned by StartCapture when the given
// ClientCaptureID already exists for this company but against a different
// Space/Project than the current request (plan §RP3.5/§RP4B0). A genuine
// retry of the same logical StartCapture call always supplies the same
// spaceID/projectID alongside the same ClientCaptureID; a mismatch means
// the same client-generated ID was reused for what claims to be a
// different capture, which is refused rather than silently returning an
// unrelated existing capture or creating a second one.
var ErrClientCaptureIDConflict = errors.New("spatial: clientCaptureId already used for a different space")

// ErrOperationIDConflict is returned when an EditOperation submission's
// OperationID (plan §RP4B) already exists for this RoomDraft but with a
// different OperationFingerprint — the same idempotency key was reused for
// what claims to be a different edit, refused rather than silently
// adopting or overwriting the original result.
var ErrOperationIDConflict = errors.New("spatial: operationId already used for a different edit operation")

// ErrRoomDraftEditRecordNotFound is returned when a RoomDraftEditRecord
// lookup by OperationID finds no match — a genuine "no prior attempt"
// state, not an error condition for the caller.
var ErrRoomDraftEditRecordNotFound = errors.New("spatial: room draft edit record not found")

// ErrUnsupportedEditOperationKind is returned when a wire submission's
// "kind" discriminator does not match any of the 27 known
// EditOperationKind values (plan §RP4B) — distinct from
// ErrInvalidEditOperation, which is a KNOWN operation whose own fields
// fail shape validation. The future editor must be able to tell "you sent
// something we don't understand" apart from "you sent a known operation
// with invalid data."
var ErrUnsupportedEditOperationKind = errors.New("spatial: unsupported edit operation kind")

// ErrMalformedEditOperationPayload is returned when a wire submission's
// "kind" is recognized but its "payload" cannot be JSON-decoded into that
// operation's typed Go struct — a wire/transport-level failure, distinct
// from ErrInvalidEditOperation's domain-level validation failure.
var ErrMalformedEditOperationPayload = errors.New("spatial: malformed edit operation payload")

// CaptureRepository persists SpatialCaptures. spatial owns the
// spatial_captures collection exclusively.
type CaptureRepository interface {
	Create(ctx context.Context, c SpatialCapture) (SpatialCapture, error)
	FindByID(ctx context.Context, companyID, id string) (SpatialCapture, error)
	// UpdateStatus atomically transitions id from expectedStatus to
	// newStatus, returning ErrIllegalCaptureTransition if the stored
	// document's status does not match expectedStatus (optimistic guard —
	// not a general CAS revision, since capture status transitions are a
	// linear state machine with no concurrent-writer scenario in V1).
	UpdateStatus(ctx context.Context, companyID, id string, expectedStatus, newStatus CaptureStatus) (SpatialCapture, error)
	// MarkConfirmed transitions id from CaptureStatusReview to
	// CaptureStatusConfirmed and records roomVersionID in one call, so a
	// caller cannot observe a capture in status=confirmed with an empty
	// RoomVersionID.
	MarkConfirmed(ctx context.Context, companyID, id, roomVersionID string) (SpatialCapture, error)
	// SetRoomDraft records roomDraftID on id, tenant-scoped to companyID.
	// Idempotent: setting the same roomDraftID again succeeds without error
	// (plan §RP3 — a retried persistence call after a network blip must not
	// fail just because the association was already recorded).
	SetRoomDraft(ctx context.Context, companyID, id, roomDraftID string) (SpatialCapture, error)
	// ListBySpace returns every capture for spaceID, most recent first.
	ListBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialCapture, error)
	// FindByClientCaptureID resolves an existing capture by its
	// ClientCaptureID, tenant-scoped to companyID (plan §RP3.5/§RP4B0's
	// StartCapture idempotency). Returns ErrCaptureNotFound if no capture
	// has this ClientCaptureID yet — a genuine "no prior attempt" state,
	// not an error condition for the caller.
	FindByClientCaptureID(ctx context.Context, companyID, clientCaptureID string) (SpatialCapture, error)
}

// RoomVersionRepository persists immutable SpatialRoomVersions. spatial owns
// the spatial_room_versions collection exclusively.
type RoomVersionRepository interface {
	// Create inserts a new immutable SpatialRoomVersion with
	// Status=RoomVersionStatusCurrent. Callers must supersede the previous
	// current version (via Supersede) in the same logical confirmation
	// operation.
	Create(ctx context.Context, v SpatialRoomVersion) (SpatialRoomVersion, error)
	FindByID(ctx context.Context, companyID, id string) (SpatialRoomVersion, error)
	// Supersede transitions id from RoomVersionStatusCurrent to
	// RoomVersionStatusSuperseded. It is a no-op success if id does not
	// exist, so confirming a Space's very first RoomVersion (no previous
	// current version to supersede) does not need a conditional call site.
	Supersede(ctx context.Context, companyID, id string) error
	// ListBySpace returns every RoomVersion for spaceID, most recent first.
	ListBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialRoomVersion, error)
}

// ArtifactRepository persists SpatialArtifact metadata. spatial owns the
// spatial_artifacts collection exclusively. Binary content lives in
// ObjectStore, addressed by SpatialArtifact.ObjectKey.
type ArtifactRepository interface {
	Create(ctx context.Context, a SpatialArtifact) (SpatialArtifact, error)
	FindByID(ctx context.Context, companyID, id string) (SpatialArtifact, error)
	// FindByUploadTokenHash resolves an artifact by its current upload
	// token's hash alone, deliberately NOT tenant-scoped: the content-PUT
	// endpoint (Service.PutArtifactContent) authenticates solely via the
	// 256-bit token, exactly like access.HashAccessToken's external Client
	// routes resolve an opaque token without a prior company scope — the
	// token's entropy IS the security boundary here, not a companyID the
	// caller does not have. Returns ErrArtifactNotFound if no artifact's
	// stored UploadTokenHash matches — expiry is checked by the caller
	// (Service), not the repository, so the sentinel stays specific to "no
	// such token" versus "token exists but is expired."
	FindByUploadTokenHash(ctx context.Context, tokenHash string) (SpatialArtifact, error)
	// ReissueUploadToken overwrites id's UploadTokenHash/UploadTokenExpiresAt,
	// tenant-scoped. Used both for the initial upload-slot request and for
	// resuming an interrupted upload with a fresh token.
	ReissueUploadToken(ctx context.Context, companyID, id, tokenHash string, expiresAt time.Time) (SpatialArtifact, error)
	// MarkUploaded transitions id from ArtifactStatusPending to
	// ArtifactStatusUploaded, recording actualSize and clearing the upload
	// token (a finalized artifact no longer accepts uploads). Idempotent:
	// calling it again on an already-uploaded artifact with the same
	// actualSize succeeds without error (Task 3 TDD requirement: "duplicate/
	// idempotent finalize").
	MarkUploaded(ctx context.Context, companyID, id string, actualSize int64, uploadedAt time.Time) (SpatialArtifact, error)
	// ListByCapture returns every artifact for captureID, tenant-scoped.
	ListByCapture(ctx context.Context, companyID, captureID string) ([]SpatialArtifact, error)
}

// RoomDraftRepository persists RoomDrafts. spatial owns the
// spatial_room_drafts collection exclusively.
type RoomDraftRepository interface {
	// Create inserts a new RoomDraft with Revision=0.
	Create(ctx context.Context, d RoomDraft) (RoomDraft, error)
	FindByID(ctx context.Context, companyID, id string) (RoomDraft, error)
	// FindByCaptureID returns the RoomDraft associated with captureID, if
	// any (plan §RP3's "Continue Review loads the SAME persisted draft" —
	// callers use this to avoid ever creating a second draft for one
	// capture).
	FindByCaptureID(ctx context.Context, companyID, captureID string) (RoomDraft, error)
	// Update replaces id's Walls/Openings/Objects/OriginalBaseline under a
	// CAS guard on expectedRevision, incrementing Revision. Returns
	// ErrRoomDraftRevisionMismatch on a stale expectedRevision.
	Update(ctx context.Context, companyID, id string, d RoomDraft, expectedRevision int64) (RoomDraft, error)
}

// VisualAssetVersionRepository persists immutable, published
// VisualAssetVersions (RP4D). spatial owns the
// spatial_visual_asset_versions collection exclusively. There is
// deliberately no Update method — a published version never mutates; a
// change publishes a new Version under the same AssetID instead.
type VisualAssetVersionRepository interface {
	// Create inserts a new VisualAssetVersion. The caller (Service.
	// PublishVisualAssetVersion) is responsible for ensuring the object
	// store already holds the bytes at StorageKey before calling this —
	// the repository itself has no knowledge of object storage. Returns
	// ErrVisualAssetVersionConflict if a document with the same
	// (CompanyID, AssetID, Version) already exists with DIFFERENT
	// metadata; if the existing document is byte-for-byte/metadata
	// IDENTICAL, returns that existing document instead (idempotent
	// publish, not an error) — the compound unique index is the
	// authoritative race guard for concurrent first-publish attempts.
	Create(ctx context.Context, v VisualAssetVersion) (VisualAssetVersion, error)
	// FindByCompanyAssetVersion resolves an exact (companyID, assetID,
	// version) triple. Returns ErrVisualAssetVersionNotFound if no match —
	// including a version that exists but belongs to a different company
	// (the query itself is the tenant boundary, matching this package's
	// existing convention).
	FindByCompanyAssetVersion(ctx context.Context, companyID, assetID string, version int) (VisualAssetVersion, error)
}

// AssetGenerationJobRepository owns the durable SpatialAssetGenerationJob
// record — the M8.5C §37 JobExecution shared state plus asset-generation-
// specific checkpoints (RP4E0). See assetgenerationjob.go's isSafeToClaim
// for the exact claim-safety invariant ClaimNext's Mongo filter must
// implement. Every mutation method past ClaimNext/RenewLease is
// lease-fenced (filters on leaseOwner=workerID, not just id) — a worker
// whose lease was reclaimed by someone else can never overwrite a
// checkpoint the new lease-holder now owns; each fenced method returns
// ErrAssetGenerationJobNotClaimable when the fenced filter matches zero
// documents, and callers MUST stop processing immediately on that error.
type AssetGenerationJobRepository interface {
	Create(ctx context.Context, j SpatialAssetGenerationJob) (SpatialAssetGenerationJob, error)
	FindByID(ctx context.Context, companyID, id string) (SpatialAssetGenerationJob, error)
	FindByClientRequestID(ctx context.Context, companyID, clientRequestID string) (SpatialAssetGenerationJob, error)
	// ClaimNext atomically claims one claimable job for workerID, setting
	// LeaseOwner/LeaseExpiresAt/Status=processing. Returns
	// ErrAssetGenerationJobNotClaimable if nothing is claimable right
	// now. The filter used here MUST implement
	// SpatialAssetGenerationJob.isSafeToClaim's exact logic — proven by a
	// dedicated parity test.
	ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration, now time.Time) (SpatialAssetGenerationJob, error)
	// RenewLease extends an already-held lease — used by the worker's
	// heartbeat while a provider call is in flight. Fails
	// (ErrAssetGenerationJobNotClaimable) if this workerID no longer
	// holds the lease — the CALLER must treat this as a hard stop
	// signal, not a transient error to retry past.
	RenewLease(ctx context.Context, id, workerID string, newExpiresAt time.Time) error
	// SetProviderStarted durably persists ProviderStartedAt — called as
	// the LAST local step immediately before the Hunyuan POST, after
	// source-image access has already been successfully resolved —
	// never earlier.
	SetProviderStarted(ctx context.Context, id, workerID string, startedAt time.Time) error
	// SetProviderRequestID durably persists the Gradio event_id —
	// called IMMEDIATELY after the POST returns successfully.
	SetProviderRequestID(ctx context.Context, id, workerID, eventID string) error
	// SetGenerated durably persists the staging checkpoint — called the
	// instant raw bytes are fully stored, BEFORE validation.
	SetGenerated(ctx context.Context, id, workerID, objectKey, checksum string, byteCount int64) error
	// Complete marks the job finished successfully with its published
	// asset identity.
	Complete(ctx context.Context, id, workerID, resultAssetID string, resultVersion int) error
	// MarkFailed sets Status=failed (terminal).
	MarkFailed(ctx context.Context, id, workerID, failureCode, failureMessage string) error
	// MarkNeedsAttention sets Status=needs_attention (zero_gpu_quota /
	// provider_outcome_unknown — never auto-retried).
	MarkNeedsAttention(ctx context.Context, id, workerID, failureCode, failureMessage string) error
	// RequeueForRetry sets Status=pending with a backoff AvailableAt,
	// ALWAYS clears LeaseOwner/LeaseExpiresAt, and increments Attempt. If
	// incrementing Attempt would reach or exceed MaxAttempts, the job
	// transitions to terminal Status=failed instead of being requeued.
	// Used ONLY for local failures before ProviderStartedAt, or storage/
	// publication failures after GeneratedObjectKey is set — never after
	// a provider call with no terminal checkpoint yet.
	RequeueForRetry(ctx context.Context, id, workerID string, availableAt time.Time) error
}

// RoomDraftEditRecordRepository persists RoomDraftEditRecords. spatial owns
// the spatial_room_draft_edits collection exclusively. This is plan §RP4B's
// audit trail AND idempotency mechanism in one — see RoomDraftEditRecord's
// doc comment.
type RoomDraftEditRecordRepository interface {
	// FindByOperationID resolves an existing edit record by its
	// OperationID, tenant-scoped to companyID — plan §RP4B's "an
	// unknown-outcome retry resolves by operation ID FIRST"
	// (awards/finalisation_service.go's FinaliseAward precedent). Returns
	// ErrRoomDraftEditRecordNotFound if no record with this OperationID
	// exists yet.
	FindByOperationID(ctx context.Context, companyID, operationID string) (RoomDraftEditRecord, error)
	// ListByRoomDraft returns every edit record for roomDraftID, most
	// recent first — the audit history for one draft.
	ListByRoomDraft(ctx context.Context, companyID, roomDraftID string) ([]RoomDraftEditRecord, error)
}

// RoomDraftEditApplier is the atomic apply+record capability plan §RP4B
// requires: CAS-updating the RoomDraft and inserting its
// RoomDraftEditRecord must never produce a state where one succeeded and
// the other did not (an "unrecorded successful edit" or an audit record
// for an edit that never actually landed) — implemented via a single
// Mongo multi-document transaction, matching
// supplieroffers/eligibility_repository_mongo.go's established
// session.WithTransaction precedent, the only one found in this codebase
// for exactly this kind of cross-collection atomicity requirement.
type RoomDraftEditApplier interface {
	// ApplyAndRecord atomically CAS-updates roomDraftID to updatedDraft
	// (guarded by expectedRevision, exactly like RoomDraftRepository.Update)
	// AND inserts record in one transaction. record.BaseRevision must equal
	// expectedRevision and record.ResultingRevision must equal
	// expectedRevision+1 — the caller (Service) is responsible for setting
	// these correctly; this method does not compute them.
	//
	// On an OperationID collision (record.OperationID already exists for
	// this RoomDraft), this does NOT re-run the transaction: it is the
	// caller's job to check FindByOperationID first (matching
	// AwardRevision's InsertRevision precedent — an unknown-outcome retry
	// resolves by operation ID before any write is attempted). A
	// same-transaction duplicate-key race (two concurrent submissions of
	// the same OperationID) is resolved the same way InsertRevision
	// resolves it: by returning ErrOperationIDConflict or the adopted
	// existing record, per the fingerprint comparison the Service layer
	// performs. The returned bool is true when the adopted-existing-record
	// path was taken (a genuine concurrent-race replay), false when this
	// call's own write actually landed — explicit rather than left for the
	// caller to infer from revision arithmetic.
	ApplyAndRecord(ctx context.Context, companyID, roomDraftID string, updatedDraft RoomDraft, expectedRevision int64, record RoomDraftEditRecord) (RoomDraft, RoomDraftEditRecord, bool, error)
}

// ErrDesignSessionNotFound is returned when a SpatialDesignSession lookup
// finds no match — including a session that exists but belongs to a
// different company (foreign and missing sessions are deliberately
// indistinguishable 404s, RP4E1 plan's public contract).
var ErrDesignSessionNotFound = errors.New("spatial: design session not found")

// ErrDesignSessionRequestConflict is returned when the same
// (companyId, clientSessionId) is reused with DIFFERENT canonical request
// content — the same idempotency key reused for a different request.
var ErrDesignSessionRequestConflict = errors.New("spatial: design session request conflict")

// ErrDesignTurnNotFound is returned when a SpatialDesignTurn lookup finds
// no match.
var ErrDesignTurnNotFound = errors.New("spatial: design turn not found")

// ErrDesignTurnRequestConflict is returned when the same
// (companyId, sessionId, clientRequestId) is reused with a DIFFERENT
// request fingerprint.
var ErrDesignTurnRequestConflict = errors.New("spatial: design turn request conflict")

// ErrDesignTurnInProgress is returned when a session already has a
// distinct active turn (ActiveTurnID set) and a NEW, different
// clientRequestId is submitted concurrently — RP4E1's public contract:
// "A concurrent distinct request while activeTurnId is set returns 409
// design_turn_in_progress."
var ErrDesignTurnInProgress = errors.New("spatial: another design turn is already in progress for this session")

// ErrDesignTurnNotClaimable is returned when a state-fencing mutation
// (MarkProviderStarted/FinishTurn) targets a turn that is no longer the
// session's active turn or is not in the expected pre-mutation status —
// the caller must treat this as a hard stop, exactly like
// ErrAssetGenerationJobNotClaimable's fencing precedent in this package.
var ErrDesignTurnNotClaimable = errors.New("spatial: design turn is not in a claimable state for this mutation")

// DesignSessionRepository persists SpatialDesignSessions. spatial owns the
// spatial_design_sessions collection exclusively.
type DesignSessionRepository interface {
	// CreateOrGetSession inserts session, or — if a session already exists
	// for (session.CompanyID, session.ClientSessionID) — returns the
	// EXISTING session if its stored SessionRequestFingerprint matches
	// session's (idempotent replay), or ErrDesignSessionRequestConflict if
	// it does not (RP4E1 plan's public contract for
	// POST /spatial/design-sessions).
	CreateOrGetSession(ctx context.Context, session SpatialDesignSession) (SpatialDesignSession, error)
	// FindSession resolves id, tenant-scoped to companyID. Returns
	// ErrDesignSessionNotFound for both a missing and a foreign session —
	// deliberately indistinguishable.
	FindSession(ctx context.Context, companyID, id string) (SpatialDesignSession, error)
}

// FinishTurnInput is FinishTurn's request shape — the terminal state a
// durable turn transitions into, computed entirely by the caller (the
// design service) BEFORE this call. FinishTurn's only job is to persist
// that terminal state atomically alongside the session-level effects a
// SUCCESSFUL (Proposed) turn has (clearing ActiveTurnID, advancing
// LatestTurnID/LatestReadyPlanTurnID, replacing CurrentWorkingDesign) —
// see this type's Status field for which effects apply.
type FinishTurnInput struct {
	CompanyID string
	SessionID string
	TurnID    string

	Status SpatialDesignTurnStatus
	// ProposedDelta/ValidatedPlan/PlanFingerprint are set only for a
	// successful (Proposed) or Blocked terminal turn.
	ProposedDelta   *ProposedSceneEditDelta
	ValidatedPlan   *ValidatedSceneEditPlan
	PlanFingerprint string
	// WorkingDesign is the NEW cumulative working design to store on the
	// session — set only when Status == SpatialDesignTurnStatusProposed
	// (a successfully merged, non-blocked turn). Left zero-value for every
	// other terminal status, since only a successful turn ever changes
	// CurrentWorkingDesign (RP4E1 plan: "A failed turn advances history but
	// never changes CurrentWorkingDesign or LatestReadyPlanTurnID").
	WorkingDesign WorkingDesign
	// SafeFailureCode is set only for Failed/NeedsAttention/Blocked
	// terminal turns — a stable, contractor-safe code, never a raw
	// provider error string.
	SafeFailureCode string
}

// DesignTurnRepository persists SpatialDesignTurns and owns the atomic
// reserve/finalize lifecycle every durable turn goes through. spatial owns
// the spatial_design_turns collection exclusively.
type DesignTurnRepository interface {
	// ReserveTurn inserts turn (Status must be
	// SpatialDesignTurnStatusReserved) atomically alongside setting the
	// parent session's ActiveTurnID/LatestTurnID and advancing
	// LastTurnSequence — one Mongo transaction spans both collections,
	// matching MongoRoomDraftEditRepository.ApplyAndRecord's established
	// precedent for "two collections, one atomic outcome."
	//
	// If a turn already exists for
	// (turn.CompanyID, turn.SessionID, turn.ClientRequestID): returns that
	// EXISTING turn with wasReplay=true if its RequestFingerprint matches
	// turn's (idempotent replay, zero provider calls); returns
	// ErrDesignTurnRequestConflict if it does not.
	//
	// If the session already has a DIFFERENT active turn (a distinct
	// clientRequestId), returns ErrDesignTurnInProgress.
	ReserveTurn(ctx context.Context, turn SpatialDesignTurn) (result SpatialDesignTurn, wasReplay bool, err error)
	// MarkProviderStarted durably persists ProviderStartedAt on turnID —
	// called as the LAST local step immediately before the outbound GLM
	// call. Fenced: only succeeds if turnID is still the session's
	// ActiveTurnID; returns ErrDesignTurnNotClaimable otherwise.
	MarkProviderStarted(ctx context.Context, companyID, turnID string, startedAt time.Time) error
	// FinishTurn atomically persists input's terminal turn state AND, for
	// a Proposed (successful, non-blocked) status, the session-level
	// effects described on FinishTurnInput — one transaction, matching the
	// same "two collections, one atomic outcome" precedent ReserveTurn
	// uses. Idempotent: calling it again with the SAME (companyID,
	// sessionID, turnID) and an already-terminal stored status returns the
	// STORED terminal result unchanged (never overwritten by a second,
	// possibly-different computed result — matching the "duplicate
	// completion adopts the stored terminal result" requirement).
	FinishTurn(ctx context.Context, input FinishTurnInput) (SpatialDesignTurn, error)
	// FindTurn resolves id, tenant-scoped to companyID.
	FindTurn(ctx context.Context, companyID, id string) (SpatialDesignTurn, error)
	// FindTurnByClientRequestID resolves an existing turn by
	// (companyID, sessionID, clientRequestID). Returns ErrDesignTurnNotFound
	// if none exists yet — a genuine "no prior attempt" state.
	FindTurnByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (SpatialDesignTurn, error)
	// ListTurns returns sessionID's turns newest-first (by Sequence
	// descending), tenant-scoped to companyID. beforeSequence is exclusive
	// (0 means "no lower bound" — the very first page); limit bounds the
	// page size (callers are responsible for the plan's default-20/cap-50
	// policy).
	ListTurns(ctx context.Context, companyID, sessionID string, beforeSequence int64, limit int) ([]SpatialDesignTurn, error)
	// RecoverInterruptedTurns finds every turn whose ProviderStartedAt is
	// set, whose Status is still SpatialDesignTurnStatusReasoning (the
	// provider call was dispatched but never durably finalized — e.g. a
	// process crash), and whose StartedAt is older than olderThan. Each
	// such turn atomically transitions to SpatialDesignTurnStatusNeedsAttention
	// and has its session's ActiveTurnID cleared — and is thereafter NEVER
	// dispatched again (RP4E1 plan: "an old reasoning turn becomes
	// needs_attention, active turn is cleared atomically, and it can never
	// be dispatched again"). Returns the recovered turns.
	RecoverInterruptedTurns(ctx context.Context, olderThan time.Duration) ([]SpatialDesignTurn, error)
}

// --- RP4E2 design generation attempt / acceptance repositories ---
//
// This interface currently covers exactly what Gate 1 (Confirm/Regenerate/
// Cancel/Use, including the material-only path that reaches concept_ready
// directly with no Python/Hunyuan involvement) needs. The worker-phase
// checkpoint methods (reference-provider marker, reference-ready,
// asset-generation-job linkage/processing) are Gate 2's concern — added
// alongside the bounded worker implementation that actually calls them,
// not spuriously ahead of that need.

// DesignGenerationAttemptRepository persists DesignGenerationAttempts and
// owns the atomic reserve/finalize lifecycle every attempt goes through.
// spatial owns the spatial_design_generation_attempts collection
// exclusively.
type DesignGenerationAttemptRepository interface {
	// ReserveAttempt inserts attempt (Status must be
	// DesignGenerationStatusReserved, ActiveSlot must be non-empty)
	// atomically alongside setting the parent session's
	// LatestGenerationAttemptID. The partial unique index on
	// (companyId, turnId, activeSlot) is the authoritative guard against two
	// concurrent active attempts for one turn — a violation surfaces as
	// ErrDesignGenerationInProgress.
	//
	// If an attempt already exists for
	// (attempt.CompanyID, attempt.SessionID, attempt.ClientRequestID):
	// returns that EXISTING attempt with wasReplay=true if its
	// RequestFingerprint matches attempt's; returns
	// ErrDesignGenerationRequestConflict if it does not.
	ReserveAttempt(ctx context.Context, attempt DesignGenerationAttempt) (result DesignGenerationAttempt, wasReplay bool, err error)
	// CompleteWithConcept atomically finalizes attemptID as concept_ready
	// with the given candidate, clears ActiveSlot, and — for a NEWER attempt
	// number than the session's current LatestReadyAttemptID for this turn —
	// advances the session's LatestReadyAttemptID. An older/superseded
	// attempt completing late never regresses LatestReadyAttemptID. Used by
	// Gate 1's material-only Confirm path (direct to concept_ready) and by
	// Gate 2's worker once a linked RP4E0 job publishes a result.
	CompleteWithConcept(ctx context.Context, companyID, attemptID string, candidate DesignConcept) (DesignGenerationAttempt, error)
	// Fail atomically transitions attemptID to Failed or NeedsAttention
	// (never auto-retried for NeedsAttention) and clears ActiveSlot.
	Fail(ctx context.Context, companyID, attemptID string, status DesignGenerationStatus, safeFailureCode string) (DesignGenerationAttempt, error)
	// Cancel marks attemptID Abandoned and clears ActiveSlot — idempotent:
	// repeated calls return the same abandoned attempt. Terminal abandonment
	// wins over any later provider result; an abandoned attempt can never
	// transition to concept_ready or be accepted.
	Cancel(ctx context.Context, companyID, attemptID, cancelClientRequestID, cancelRequestFingerprint string) (DesignGenerationAttempt, error)
	// FindAttempt resolves id, tenant-scoped to companyID.
	FindAttempt(ctx context.Context, companyID, id string) (DesignGenerationAttempt, error)
	// FindAttemptByClientRequestID resolves an existing attempt by
	// (companyID, sessionID, clientRequestID). Returns
	// ErrDesignGenerationAttemptNotFound if none exists yet.
	FindAttemptByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (DesignGenerationAttempt, error)

	// --- Gate 2 worker-phase methods ---
	//
	// Unlike AssetGenerationJobRepository's ClaimNext, these are NOT
	// lease/workerID-fenced — only one active (ActiveSlot-holding) attempt
	// ever exists per turn by construction (ReserveAttempt's own partial
	// unique index), and RP4E2's worker is a single bounded step per HTTP
	// call, never a long-lived goroutine racing itself. The fencing that
	// DOES matter — "cancel wins over late completion" — is achieved the
	// same way Fail/Cancel/CompleteWithConcept already achieve it: every
	// mutation's filter requires ActiveSlot to still be present, so a
	// Cancel that has already cleared it makes every subsequent worker
	// write here return ErrDesignGenerationNotClaimable, never silently
	// overwrite an abandoned attempt.

	// ClaimNextGenerationPhase atomically claims one attempt eligible for
	// the reference-generation phase — Status must be
	// DesignGenerationStatusReserved (a freshly-confirmed geometry/mixed
	// attempt) with ReferenceProviderStartedAt unset (never re-claim an
	// attempt whose reference call may already be in flight — that is the
	// one dangerous state, mirroring SpatialAssetGenerationJob's own
	// "started, no checkpoint yet" exclusion). Transitions Status to
	// generating_reference as part of the same atomic claim. Returns
	// ErrDesignGenerationNotClaimable if nothing is claimable right now —
	// the normal idle case, not an error condition callers should log.
	ClaimNextGenerationPhase(ctx context.Context) (DesignGenerationAttempt, error)
	// SetReferenceProviderStarted durably persists
	// ReferenceProviderStartedAt — called as the LAST local step
	// immediately before the Python reference-image call, mirroring
	// SpatialAssetGenerationJob.ProviderStartedAt's exact convention. Once
	// set, this attempt is permanently excluded from
	// ClaimNextGenerationPhase (the dangerous-state guard above) — a crash
	// here surfaces as needs_attention via Fail, never a silent second
	// reference-image call.
	SetReferenceProviderStarted(ctx context.Context, companyID, attemptID string, startedAt time.Time) error
	// SetReferenceReady atomically persists the validated reference image
	// and transitions Status to reference_ready. Fenced on ActiveSlot still
	// being present (see the block comment above).
	SetReferenceReady(ctx context.Context, companyID, attemptID string, image DesignReferenceImage) (DesignGenerationAttempt, error)
	// SetAssetGenerationPending links the RP4E0 job this attempt submitted
	// to and transitions Status to asset_generation_pending — called
	// immediately after SubmitAssetGenerationJobFromDesignReference
	// succeeds, before the Vercel Queue wake is published (Gate 4), so the
	// linkage is durable before any dispatch signal goes out. Fenced on
	// ActiveSlot still being present.
	SetAssetGenerationPending(ctx context.Context, companyID, attemptID, assetGenerationJobID string) (DesignGenerationAttempt, error)
	// SetAssetGenerationProcessing transitions Status from
	// asset_generation_pending to asset_generation_processing — a
	// visibility-only transition (the linked RP4E0 job's own state machine
	// is authoritative; this just reflects "a worker has picked it up" back
	// onto the attempt for the public progress UI). Fenced on ActiveSlot
	// still being present.
	SetAssetGenerationProcessing(ctx context.Context, companyID, attemptID string) (DesignGenerationAttempt, error)

	// ListAttempts returns sessionID's attempts newest-first, optionally
	// filtered to one turnID (empty means all turns), tenant-scoped.
	ListAttempts(ctx context.Context, companyID, sessionID, turnID string, limit int) ([]DesignGenerationAttempt, error)
	// FindAttemptByAssetGenerationJobID resolves the (at most one) attempt
	// linked to jobID via SetAssetGenerationPending — the reconciliation
	// step's lookup (RP4E0 job completion -> which design attempt, if any,
	// should advance to concept_ready). Returns
	// ErrDesignGenerationAttemptNotFound if the job was never linked to a
	// design attempt (i.e. it came from RP4E0's own public route, a plain
	// capture-artifact source).
	FindAttemptByAssetGenerationJobID(ctx context.Context, companyID, jobID string) (DesignGenerationAttempt, error)
}

// DesignAcceptanceRepository persists immutable DesignAcceptances. spatial
// owns the spatial_design_acceptances collection exclusively.
type DesignAcceptanceRepository interface {
	// FindAcceptanceByClientRequestID resolves an existing acceptance by
	// (companyID, sessionID, clientRequestID) — Use's idempotency-first
	// lookup. Returns ErrDesignAcceptanceNotFound if none exists yet.
	FindAcceptanceByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (DesignAcceptance, error)
	// FindAcceptanceByAttemptID resolves the (at most one) acceptance that
	// used attemptID — the unique (companyId, attemptId) index's read side.
	FindAcceptanceByAttemptID(ctx context.Context, companyID, attemptID string) (DesignAcceptance, error)
}

// DesignAcceptanceApplier is the atomic Use Design transaction (RP4E2
// plan): applying the fixed-order canonical operation batch to the
// RoomDraft, inserting every resulting RoomDraftEditRecord, inserting the
// DesignAcceptance, marking the attempt accepted, and updating the
// session's AcceptedDesign/AcceptedTurnID/AcceptedAttemptID/
// CurrentWorkingDesign/BasedOnRoomDraftRevision must all land together or
// none does — one Mongo transaction spanning four collections, matching
// MongoRoomDraftEditRepository.ApplyAndRecord's established "N collections,
// one atomic outcome" precedent, generalized from one edit record to a
// batch plus the session/attempt/acceptance side effects Use Design alone
// produces.
type DesignAcceptanceApplier interface {
	// ApplyAcceptance runs input's fixed-order operation batch against the
	// authoritative RoomDraft under one CAS check at
	// input.ExpectedRoomDraftRevision, inserts one RoomDraftEditRecord per
	// applied operation, inserts the DesignAcceptance, marks
	// input.AttemptID accepted, and updates the session as described above.
	// A stale revision returns ErrRoomDraftRevisionMismatch with nothing
	// partially applied. On an OperationID collision for
	// (input.CompanyID, input.SessionID, input.ClientRequestID), adopts the
	// existing DesignAcceptance if its fingerprint matches (idempotent
	// replay) or returns ErrDesignAcceptanceRequestConflict.
	ApplyAcceptance(ctx context.Context, input ApplyAcceptanceInput) (ApplyAcceptanceResult, error)
}

// ApplyAcceptanceInput is ApplyAcceptance's request shape — every field the
// Service layer has already validated/resolved BEFORE the transaction
// (attempt readiness, revision staleness, fixed operation order) so the
// repository layer only executes, never re-derives business rules.
type ApplyAcceptanceInput struct {
	CompanyID string
	SessionID string
	TurnID    string
	AttemptID string

	PlanFingerprint    string
	ClientRequestID    string
	RequestFingerprint string

	RoomDraftID               string
	ExpectedRoomDraftRevision int64

	// Operations is the fixed-order canonical batch: resolved move/resize
	// operations, then set_visual_appearance/clear_visual_appearance, then
	// assign_visual_asset last (RP4E2 plan's exact required order). Each
	// entry's Kind/Payload becomes one immutable RoomDraftEditRecord.
	Operations []PendingCanonicalOperation

	AcceptedDesign WorkingDesign
	ActorUserID    string
}

// PendingCanonicalOperation is one already-decoded-and-validated
// EditOperation plus its audit-record Kind/Payload — the Service layer
// builds these; the repository layer only applies them in order.
type PendingCanonicalOperation struct {
	Kind      EditOperationKind
	Payload   string
	Transform func(RoomDraft) (RoomDraft, error)
}

// ApplyAcceptanceResult is ApplyAcceptance's response shape.
type ApplyAcceptanceResult struct {
	RoomDraft           RoomDraft
	Acceptance          DesignAcceptance
	AppliedOperationIDs []string
	Replayed            bool
}

// SpaceStateRepository persists SpatialSpaceState. spatial owns the
// spatial_space_states collection exclusively.
type SpaceStateRepository interface {
	// FindOrCreateBySpace returns spaceID's SpatialSpaceState, creating one
	// with Revision=0 and no CurrentRoomVersionID if none exists yet
	// (design spec §3: "created lazily on first capture").
	FindOrCreateBySpace(ctx context.Context, companyID, projectID, spaceID string) (SpatialSpaceState, error)
	// SetCurrentRoomVersion atomically sets CurrentRoomVersionID to
	// roomVersionID and increments Revision, guarded by expectedRevision
	// matching the stored document's Revision. Returns
	// ErrSpaceStateRevisionMismatch on a stale expectedRevision (design
	// spec §4.2, access.MongoAccessGrantRepository.Revoke pattern).
	SetCurrentRoomVersion(ctx context.Context, companyID, spaceID, roomVersionID string, expectedRevision int64) (SpatialSpaceState, error)
}
