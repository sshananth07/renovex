package spatial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrSpaceNotFound is returned when the given spaceID does not belong to the
// caller's company/project (per SpaceLookup) — a distinct sentinel value in
// this package, not an import of spaces.ErrSpaceNotFound.
var ErrSpaceNotFound = errors.New("spatial: space not found")

// SpaceLookup is the capability spatial needs from spaces: confirming a
// spaceID belongs to the caller's company and project before starting a
// capture. Defined here (consumer-defines-interface); satisfied structurally
// by spaces.Service with no import in either direction (design spec §3).
type SpaceLookup interface {
	SpaceBelongsToProject(ctx context.Context, companyID, spaceID, projectID string) (bool, error)
}

// legalCaptureTransitions maps each CaptureStatus to the set of statuses it
// may transition to (design spec §4.1). CaptureStatusConfirmed and
// CaptureStatusSuperseded are terminal — no outgoing transitions.
var legalCaptureTransitions = map[CaptureStatus]map[CaptureStatus]bool{
	CaptureStatusDraft:     {CaptureStatusCapturing: true},
	CaptureStatusCapturing: {CaptureStatusUploading: true, CaptureStatusFailed: true},
	CaptureStatusUploading: {CaptureStatusUploaded: true, CaptureStatusFailed: true},
	CaptureStatusFailed:    {CaptureStatusCapturing: true, CaptureStatusUploading: true},
	CaptureStatusUploaded:  {CaptureStatusReview: true},
	CaptureStatusReview:    {CaptureStatusConfirmed: true},
}

// Service implements spatial capture lifecycle management and the
// CAS-protected current-room-version authority transition (design spec §4).
type Service struct {
	captures    CaptureRepository
	versions    RoomVersionRepository
	spaceStates SpaceStateRepository
	spaceLookup SpaceLookup

	// artifacts/objectStore are nil until SetArtifactSupport is called
	// (Task 3). Every Task 2 capture/room-version method works without
	// them; only artifact-specific methods in artifact_service.go require
	// them to be set.
	artifacts   ArtifactRepository
	objectStore ArtifactObjectStore

	// roomDrafts is nil until SetRoomDraftSupport is called (plan §RP3),
	// matching SetArtifactSupport's setter pattern so NewService's signature
	// is not re-broken by each new capability spatial gains.
	roomDrafts RoomDraftRepository

	// roomDraftEdits is nil until SetRoomDraftEditSupport is called (plan
	// §RP4B), same setter pattern.
	roomDraftEdits interface {
		RoomDraftEditRecordRepository
		RoomDraftEditApplier
	}

	// visualAssets/visualAssetObjectStore/visualAssetReadAccess are nil
	// until SetVisualAssetSupport is called (RP4D), same setter pattern.
	// Only assign_visual_asset's service-level authorization check and
	// PublishVisualAssetVersion/CreateVisualAssetAccess require these to
	// be set; clear_visual_asset needs none of them (nothing to
	// authorize — it only removes a reference).
	visualAssets           VisualAssetVersionRepository
	visualAssetObjectStore VisualAssetObjectStore
	visualAssetReadAccess  VisualAssetReadAccessProvider
	// visualAssetCapabilityVerifier is nil unless the wired
	// VisualAssetReadAccessProvider also implements
	// VisualAssetLocalCapabilityVerifier (true for the local HMAC
	// provider, never true for a future cloud-storage provider) — set
	// automatically by SetVisualAssetReadAccessProvider via an interface
	// type assertion, never a separate setter call.
	visualAssetCapabilityVerifier VisualAssetLocalCapabilityVerifier
	// visualAssetAccessTTL is the default TTL CreateVisualAssetAccess
	// mints capabilities with when a caller does not specify one
	// (RP4D §22 — configurable, defaults to 10 minutes if never set).
	visualAssetAccessTTL time.Duration

	// assetGeneration* are nil until SetAssetGenerationSupport is called
	// (RP4E0), same setter pattern as every other optional capability.
	assetGenerationJobs         AssetGenerationJobRepository
	assetGenerationStagingStore AssetGenerationObjectStore
	assetGenerationProvider     AssetGenerationProvider
	assetGenerationSourceAccess AssetGenerationSourceAccessProvider
	// assetGenerationSourceCapabilityVerifier is nil unless the wired
	// AssetGenerationSourceAccessProvider also implements
	// AssetGenerationSourceLocalCapabilityVerifier (true for the local
	// HMAC provider, never true for a future cloud-storage provider) —
	// set automatically by SetAssetGenerationSupport via an interface
	// type assertion, mirroring SetVisualAssetReadAccessProvider's exact
	// RP4D precedent.
	assetGenerationSourceCapabilityVerifier AssetGenerationSourceLocalCapabilityVerifier
	// assetGenerationLeaseTTL/HeartbeatInterval/ProviderTimeout are
	// configurable — composition root wires these from config; a zero
	// value means "use the documented default" (see
	// assetgeneration_service.go's default* constants).
	assetGenerationLeaseTTL          time.Duration
	assetGenerationHeartbeatInterval time.Duration
	assetGenerationProviderTimeout   time.Duration

	// designSessions/designTurns/designReasoner are nil until
	// SetDesignPlanningSupport is called (RP4E1), same optional setter
	// pattern as every other capability above.
	designSessions DesignSessionRepository
	designTurns    DesignTurnRepository
	designReasoner ElementReasoner

	// designGenerationAttempts/designAcceptances/designAcceptanceApplier
	// are nil until SetDesignGenerationSupport is called (RP4E2), same
	// optional setter pattern.
	designGenerationAttempts DesignGenerationAttemptRepository
	designAcceptances        DesignAcceptanceRepository
	designAcceptanceApplier  DesignAcceptanceApplier

	// designReferenceGenerator/Store/SourceAccess are nil until
	// SetDesignGenerationWorkerSupport is called (RP4E2 Gate 2) — the
	// worker-only dependencies ProcessOneDesignGenerationAttempt needs.
	// Deliberately separate from SetDesignGenerationSupport: a deployment
	// can register the public Confirm/Regenerate/Cancel/Use/List/Get
	// surface (Gate 1) without ever configuring the worker (e.g. the
	// tenanttest composition root, which never dispatches background work).
	designReferenceGenerator      ReferenceImageGenerator
	designReferenceStore          DesignReferenceImageObjectStore
	designReferenceSourceAccess   DesignReferenceSourceAccessProvider
	designGenerationWorkerTimeout time.Duration

	// generationWakePublisher is nil until SetGenerationWakePublisher is
	// called (RP4E2 Gate 4). Nil means "no queue configured" — every wake
	// call site treats a nil publisher exactly like a publish error: best
	// effort, never fails the caller's request (see notifyGenerationWake).
	generationWakePublisher GenerationWakePublisher
}

// AssetGenerationObjectStore is the staging store for raw generated
// bytes BEFORE validation/publication (RP4E0 spec §7) — a distinct key
// space from VisualAssetObjectStore's final immutable keys.
type AssetGenerationObjectStore interface {
	Put(ctx context.Context, key string, content io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// DesignReferenceImageObjectStore is where a design generation attempt's
// validated reference image is written durably (RP4E2 Gate 2) — a distinct
// key space from both AssetGenerationObjectStore's staging keys and
// VisualAssetObjectStore's final published keys. Read back only by
// DesignReferenceSourceAccessProvider (to mint the Hunyuan-facing URL), never
// exposed to a client directly.
type DesignReferenceImageObjectStore interface {
	Put(ctx context.Context, key string, content io.Reader) (int64, error)
}

// AssetGenerationSourceAccessProvider mints a short-lived, provider-
// readable URL for an already-validated SpatialArtifact's bytes (RP4E0
// spec §6) — never a raw io.Reader proxy, never a client-supplied URL.
type AssetGenerationSourceAccessProvider interface {
	CreateSourceImageAccess(ctx context.Context, artifact SpatialArtifact, ttl time.Duration) (ReadAccess, error)
}

// AssetGenerationSourceLocalCapabilityVerifier is a narrower, LOCAL-
// PROVIDER-SPECIFIC capability — deliberately NOT part of
// AssetGenerationSourceAccessProvider, mirroring
// VisualAssetLocalCapabilityVerifier's exact RP4D precedent: a future
// cloud-storage provider's browser-side fetch never round-trips through
// this Go backend to be verified here at all.
type AssetGenerationSourceLocalCapabilityVerifier interface {
	VerifySourceImageAccess(ctx context.Context, capability string) (companyID, artifactID string, err error)
}

// DesignReferenceSourceAccessProvider mints a short-lived, provider-
// readable URL for an attempt-owned reference image object key (RP4E2 Gate
// 2) — a separate, narrower capability from AssetGenerationSourceAccessProvider
// because a design-reference image has no SpatialArtifact record at all (it
// is written directly to R2 by the reference-generation phase, never
// client-uploaded through the artifact pipeline). objectKey is always the
// attempt's own DesignReferenceImage.ObjectKey — never a caller-supplied
// path.
type DesignReferenceSourceAccessProvider interface {
	CreateReferenceImageAccess(ctx context.Context, companyID, objectKey string, ttl time.Duration) (ReadAccess, error)
}

// GenerationWakeKind distinguishes the two record types Gate 4's queue wake
// path can reference — always paired with that record's own ID, never
// authoritative state (the Mongo record remains the sole source of truth;
// see notifyGenerationWake's doc comment).
type GenerationWakeKind string

const (
	GenerationWakeKindDesignAttempt GenerationWakeKind = "design_attempt"
	GenerationWakeKindAssetJob      GenerationWakeKind = "asset_job"
)

// GenerationWakePublisher is the narrow capability Service needs to hint a
// background worker that a newly-reserved design generation attempt or
// asset generation job is ready for its first bounded processing step
// (RP4E2 Gate 4: "Go calls the protected Web enqueue route... The Web route
// publishes to the appropriate Vercel Queue topic"). Defined here
// (consumer-defines-interface); satisfied by a composition-root adapter
// that calls the Web project's shared-secret-protected enqueue route.
type GenerationWakePublisher interface {
	PublishGenerationWake(ctx context.Context, kind GenerationWakeKind, id string) error
}

// SetGenerationWakePublisher wires the optional Gate 4 queue-wake publisher
// into Service after construction — same optional setter pattern as every
// other capability. A nil/unset publisher is not an error: local
// development relies on StartAssetGenerationDispatchLoop-style polling
// instead, and every wake call site tolerates a nil publisher identically
// to a publish failure (see notifyGenerationWake).
func (s *Service) SetGenerationWakePublisher(publisher GenerationWakePublisher) {
	s.generationWakePublisher = publisher
}

// notifyGenerationWake is a best-effort hint, never a correctness
// requirement: the record was already durably written to Mongo by the
// caller before this runs, and every worker step re-derives what to do
// from Mongo state alone (claim-safety fencing, not queue delivery,
// decides every transition — RP4E2 Gate 4's own "at-least-once delivery is
// harmless" design). A nil publisher or a publish error is therefore
// swallowed here rather than propagated, so a Web-side outage or
// unconfigured queue never fails the Confirm/Regenerate/Submit call that
// triggered it — local polling (or the next successful wake) still finds
// the work.
func (s *Service) notifyGenerationWake(ctx context.Context, kind GenerationWakeKind, id string) {
	if s.generationWakePublisher == nil {
		return
	}
	_ = s.generationWakePublisher.PublishGenerationWake(ctx, kind, id)
}

// SetDesignGenerationWorkerSupport wires ProcessOneDesignGenerationAttempt's
// dependencies into Service after construction (RP4E2 Gate 2) — separate
// from SetDesignGenerationSupport (Gate 1's public-surface repositories) so
// a composition root can register the public routes without also standing
// up worker dependencies (mirrors SetAssetGenerationSupport vs.
// SetAssetGenerationTimings's own "capabilities vs. tuning" split).
// timeout is the worker's own bounded-step budget (documented default
// applies when zero — see designgeneration_worker.go's
// defaultDesignGenerationWorkerTimeout).
func (s *Service) SetDesignGenerationWorkerSupport(
	referenceGenerator ReferenceImageGenerator,
	referenceStore DesignReferenceImageObjectStore,
	referenceSourceAccess DesignReferenceSourceAccessProvider,
	timeout time.Duration,
) {
	s.designReferenceGenerator = referenceGenerator
	s.designReferenceStore = referenceStore
	s.designReferenceSourceAccess = referenceSourceAccess
	s.designGenerationWorkerTimeout = timeout
}

// SetAssetGenerationSupport wires the four asset-generation capabilities
// into Service after construction (RP4E0), same setter pattern as every
// other optional capability. Missing/nil dependencies at USE time
// (SubmitAssetGenerationJob/ProcessOneAssetGenerationJob) return
// ErrAssetGenerationNotConfigured cleanly rather than panicking.
func (s *Service) SetAssetGenerationSupport(
	jobs AssetGenerationJobRepository,
	stagingStore AssetGenerationObjectStore,
	provider AssetGenerationProvider,
	sourceAccess AssetGenerationSourceAccessProvider,
) {
	s.assetGenerationJobs = jobs
	s.assetGenerationStagingStore = stagingStore
	s.assetGenerationProvider = provider
	s.assetGenerationSourceAccess = sourceAccess
	s.assetGenerationSourceCapabilityVerifier, _ = sourceAccess.(AssetGenerationSourceLocalCapabilityVerifier)
}

// SetAssetGenerationTimings configures the lease TTL, heartbeat interval,
// and provider timeout ProcessOneAssetGenerationJob uses. A zero value
// for any field leaves that timing at its documented default.
func (s *Service) SetAssetGenerationTimings(leaseTTL, heartbeatInterval, providerTimeout time.Duration) {
	s.assetGenerationLeaseTTL = leaseTTL
	s.assetGenerationHeartbeatInterval = heartbeatInterval
	s.assetGenerationProviderTimeout = providerTimeout
}

// SetRoomDraftSupport wires roomDrafts into Service after construction.
func (s *Service) SetRoomDraftSupport(roomDrafts RoomDraftRepository) {
	s.roomDrafts = roomDrafts
}

// SetRoomDraftEditSupport wires roomDraftEdits into Service after
// construction (plan §RP4B). repo must implement both
// RoomDraftEditRecordRepository (audit/idempotency lookups) and
// RoomDraftEditApplier (the atomic CAS-update + record-insert
// transaction) — MongoRoomDraftEditRepository implements both, matching
// the design decision that one type owns both concerns since they share
// the same transaction boundary.
func (s *Service) SetRoomDraftEditSupport(repo interface {
	RoomDraftEditRecordRepository
	RoomDraftEditApplier
}) {
	s.roomDraftEdits = repo
}

// SetVisualAssetSupport wires visualAssets/objectStore/readAccess into
// Service after construction (RP4D), same setter pattern. readAccess may
// be nil if only PublishVisualAssetVersion/assign-time authorization is
// needed (e.g. in tests) — CreateVisualAssetAccess requires it to be set.
func (s *Service) SetVisualAssetSupport(visualAssets VisualAssetVersionRepository, objectStore VisualAssetObjectStore) {
	s.visualAssets = visualAssets
	s.visualAssetObjectStore = objectStore
}

// SetVisualAssetReadAccessProvider wires the read-access provider into
// Service separately from SetVisualAssetSupport, since a provider (e.g.
// the local HMAC-capability implementation) is composition-root-owned and
// may be constructed after the repository/object-store wiring.
func (s *Service) SetVisualAssetReadAccessProvider(provider VisualAssetReadAccessProvider) {
	s.visualAssetReadAccess = provider
	s.visualAssetCapabilityVerifier, _ = provider.(VisualAssetLocalCapabilityVerifier)
}

// SetVisualAssetAccessTTL configures the default TTL CreateVisualAssetAccess
// mints capabilities with. Composition root wires this from
// config.Config.VisualAssetAccessTTL.
func (s *Service) SetVisualAssetAccessTTL(ttl time.Duration) {
	s.visualAssetAccessTTL = ttl
}

// NewService constructs a Service backed by the given repositories,
// consuming spaceLookup to validate parent Space references.
func NewService(captures CaptureRepository, versions RoomVersionRepository, spaceStates SpaceStateRepository, spaceLookup SpaceLookup) *Service {
	return &Service{captures: captures, versions: versions, spaceStates: spaceStates, spaceLookup: spaceLookup}
}

// StartCapture validates spaceID belongs to companyID/projectID and creates
// a new SpatialCapture in status=draft, tagged with provider and assigned
// the next 1-based CaptureNumber for spaceID (design spec §8.22's "#1", "#2",
// "#3" Scan History display — many captures per Space, never overwritten).
// An empty provider defaults to CaptureProviderRoomPlan, the only active
// production provider.
//
// clientCaptureID is an optional idempotency key (plan §RP3.5/§RP4B0 — iOS
// supplies its stable LocalCaptureRun.id). When non-empty, a retried call
// with the SAME clientCaptureID returns the capture already created by the
// first successful attempt instead of creating a second one and advancing
// CaptureNumber again — the existing-capture lookup happens BEFORE
// CaptureNumber is computed, so a retry never consumes or wastes a
// sequence number. If clientCaptureID already exists for this company but
// against a different spaceID/projectID, this returns
// ErrClientCaptureIDConflict rather than silently returning the unrelated
// existing capture or creating a second one — a genuine retry of the same
// logical call always supplies the same space/project alongside the same
// clientCaptureID. Callers that pass an empty clientCaptureID (e.g. Web,
// or any client with no local stable capture identity) get the original
// non-idempotent behavior unchanged.
func (s *Service) StartCapture(ctx context.Context, companyID, projectID, spaceID string, provider CaptureProvider, clientCaptureID string) (SpatialCapture, error) {
	if clientCaptureID != "" {
		existing, err := s.captures.FindByClientCaptureID(ctx, companyID, clientCaptureID)
		if err == nil {
			if existing.SpaceID != spaceID || existing.ProjectID != projectID {
				return SpatialCapture{}, ErrClientCaptureIDConflict
			}
			return existing, nil
		}
		if !errors.Is(err, ErrCaptureNotFound) {
			return SpatialCapture{}, err
		}
	}

	belongs, err := s.spaceLookup.SpaceBelongsToProject(ctx, companyID, spaceID, projectID)
	if err != nil {
		return SpatialCapture{}, err
	}
	if !belongs {
		return SpatialCapture{}, ErrSpaceNotFound
	}
	if provider == "" {
		provider = CaptureProviderRoomPlan
	}
	existingForSpace, err := s.captures.ListBySpace(ctx, companyID, spaceID)
	if err != nil {
		return SpatialCapture{}, err
	}
	now := time.Now()
	created, err := s.captures.Create(ctx, SpatialCapture{
		CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID,
		Status: CaptureStatusDraft, CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
		Provider: provider, CaptureNumber: len(existingForSpace) + 1, ClientCaptureID: clientCaptureID,
	})
	if err != nil {
		return SpatialCapture{}, err
	}
	// Create's own duplicate-key handling (the authoritative race guard for
	// two concurrent first attempts racing past the FindByClientCaptureID
	// check above) may have returned the WINNING concurrent attempt's
	// capture rather than the one just constructed here — verify identity
	// the same way the pre-flight check does, for the same reason.
	if clientCaptureID != "" && created.ClientCaptureID == clientCaptureID &&
		(created.SpaceID != spaceID || created.ProjectID != projectID) {
		return SpatialCapture{}, ErrClientCaptureIDConflict
	}
	return created, nil
}

// SetRoomDraft records that captureID's RoomPlan output has been normalized
// and durably persisted as roomDraftID (plan §RP3's required ordering:
// persist raw result -> normalize RoomDraft -> persist RoomDraft -> THEN
// Room Review is presentable). Tenant-scoped to companyID. Idempotent:
// calling it again with the same roomDraftID succeeds without error.
func (s *Service) SetRoomDraft(ctx context.Context, companyID, captureID, roomDraftID string) (SpatialCapture, error) {
	return s.captures.SetRoomDraft(ctx, companyID, captureID, roomDraftID)
}

// PersistRoomDraft creates a new RoomDraft for captureID and links it to the
// capture in one operation — the durable-persistence step in plan §RP3's
// required ordering (RoomPlan completion -> persist source/raw result ->
// normalize RoomDraft -> persist RoomDraft -> THEN present Room Review).
// captureID must belong to companyID. Fails if captureID already has a
// RoomDraft (use UpdateRoomDraft for edits) — one capture has at most one
// RoomDraft, matching design spec §8.11's single mutable contractor-review
// model per capture.
func (s *Service) PersistRoomDraft(ctx context.Context, companyID, captureID string, walls []RoomDraftWall, openings []RoomDraftOpening, objects []RoomDraftObject, sourceProvider SourceProvider) (RoomDraft, error) {
	capture, err := s.captures.FindByID(ctx, companyID, captureID)
	if err != nil {
		return RoomDraft{}, err
	}
	if capture.RoomDraftID != "" {
		return RoomDraft{}, fmt.Errorf("spatial: capture %s already has a room draft; use UpdateRoomDraft", captureID)
	}

	now := time.Now()
	created, err := s.roomDrafts.Create(ctx, RoomDraft{
		CompanyID: companyID, CaptureID: captureID,
		Walls: walls, Openings: openings, Objects: objects,
		SourceProvider: sourceProvider,
		OriginalBaseline: &RoomDraftBaseline{
			Walls: walls, Openings: openings, Objects: objects,
		},
		CreatedAt: now, UpdatedAt: now, SchemaVersion: 1,
	})
	if err != nil {
		return RoomDraft{}, err
	}

	if _, err := s.captures.SetRoomDraft(ctx, companyID, captureID, created.ID); err != nil {
		return RoomDraft{}, err
	}
	return created, nil
}

// UpdateRoomDraft replaces roomDraftID's Walls/Openings/Objects under a CAS
// guard on expectedRevision, tenant-scoped to companyID. OriginalBaseline is
// preserved unchanged (design spec §8.14 "Reset to Scan" — an edit never
// destroys the normalized-from-capture baseline).
func (s *Service) UpdateRoomDraft(ctx context.Context, companyID, roomDraftID string, walls []RoomDraftWall, openings []RoomDraftOpening, objects []RoomDraftObject, expectedRevision int64) (RoomDraft, error) {
	existing, err := s.roomDrafts.FindByID(ctx, companyID, roomDraftID)
	if err != nil {
		return RoomDraft{}, err
	}
	existing.Walls = walls
	existing.Openings = openings
	existing.Objects = objects
	return s.roomDrafts.Update(ctx, companyID, roomDraftID, existing, expectedRevision)
}

// GetRoomDraft returns roomDraftID's RoomDraft, tenant-scoped to companyID.
func (s *Service) GetRoomDraft(ctx context.Context, companyID, roomDraftID string) (RoomDraft, error) {
	return s.roomDrafts.FindByID(ctx, companyID, roomDraftID)
}

// GetRoomDraftByCapture returns captureID's RoomDraft, tenant-scoped to
// companyID — the "Continue Review loads the SAME persisted draft" lookup
// (plan §RP3): callers use this instead of ever creating a second draft for
// one capture.
func (s *Service) GetRoomDraftByCapture(ctx context.Context, companyID, captureID string) (RoomDraft, error) {
	return s.roomDrafts.FindByCaptureID(ctx, companyID, captureID)
}

// ListRoomDraftEdits returns roomDraftID's edit-operation audit history,
// tenant-scoped to companyID, most recent first (plan §RP4B).
func (s *Service) ListRoomDraftEdits(ctx context.Context, companyID, roomDraftID string) ([]RoomDraftEditRecord, error) {
	return s.roomDraftEdits.ListByRoomDraft(ctx, companyID, roomDraftID)
}

// ErrRoomDraftHasNoBaseline is returned by ResetRoomDraftToBaseline when the
// draft has never been edited (OriginalBaseline is nil) — there is nothing
// to reset to that would differ from the draft's current state.
var ErrRoomDraftHasNoBaseline = errors.New("spatial: room draft has no original baseline to reset to")

// ResetRoomDraftToBaseline restores roomDraftID's Walls/Openings/Objects to
// exactly OriginalBaseline's contents (design spec §8.14/§8.25 "Reset to
// Scan", RP4A) and clears Fixtures/ServicePoints/Constraints entirely —
// contractor-created elements never existed in the capture baseline, so
// resetting to it removes them rather than leaving them orphaned. Stable
// IDs of restored walls/openings/objects are exactly OriginalBaseline's own
// (the same objects, same IDs — never regenerated). CAS-guarded on
// expectedRevision.
func (s *Service) ResetRoomDraftToBaseline(ctx context.Context, companyID, roomDraftID string, expectedRevision int64) (RoomDraft, error) {
	existing, err := s.roomDrafts.FindByID(ctx, companyID, roomDraftID)
	if err != nil {
		return RoomDraft{}, err
	}
	if existing.OriginalBaseline == nil {
		return RoomDraft{}, ErrRoomDraftHasNoBaseline
	}
	existing.Walls = existing.OriginalBaseline.Walls
	existing.Openings = existing.OriginalBaseline.Openings
	existing.Objects = existing.OriginalBaseline.Objects
	existing.Fixtures = nil
	existing.ServicePoints = nil
	existing.Constraints = nil
	return s.roomDrafts.Update(ctx, companyID, roomDraftID, existing, expectedRevision)
}

// ApplyEditOperation validates op and applies it to roomDraftID's RoomDraft
// under a CAS guard on expectedRevision (RP4A, design spec §8.13) — the
// single dispatch seam a future RP4B HTTP handler decodes a wire operation
// payload into and calls through.
func (s *Service) ApplyEditOperation(ctx context.Context, companyID, roomDraftID string, op EditOperation, expectedRevision int64) (RoomDraft, error) {
	if err := op.Validate(); err != nil {
		return RoomDraft{}, err
	}
	existing, err := s.roomDrafts.FindByID(ctx, companyID, roomDraftID)
	if err != nil {
		return RoomDraft{}, err
	}
	updated, err := op.Apply(existing)
	if err != nil {
		return RoomDraft{}, err
	}
	return s.roomDrafts.Update(ctx, companyID, roomDraftID, updated, expectedRevision)
}

// SubmitEditOperationInput is SubmitEditOperation's request shape (plan
// §RP4B) — the additive HTTP contract's decoded body. Kind/Payload are the
// wire discriminator + raw JSON exactly as submitted (Payload is re-hashed
// verbatim for the idempotency fingerprint, never re-encoded from a
// decoded value).
type SubmitEditOperationInput struct {
	RoomDraftID      string
	OperationID      string
	Kind             EditOperationKind
	Payload          json.RawMessage
	ExpectedRevision int64
	ActorUserID      string
}

// SubmitEditOperationResult is SubmitEditOperation's response shape:
// the resulting authoritative RoomDraft plus the audit/idempotency record
// that produced it (Replayed distinguishes "this OperationID was already
// applied, here is that original result" from a fresh application, so a
// caller can log/report the two cases differently without them being
// indistinguishable at the type level).
type SubmitEditOperationResult struct {
	RoomDraft RoomDraft
	Record    RoomDraftEditRecord
	Replayed  bool
}

// SubmitEditOperation is plan §RP4B's authoritative edit-application entry
// point — the seam a future HTTP handler calls directly. It decodes
// input.Kind/Payload into a concrete EditOperation
// (ErrUnsupportedEditOperationKind / ErrMalformedEditOperationPayload on
// failure — distinct from domain validation failure), validates its own
// shape (op.Validate()), and submits it through submitAgainstRoomDraft — the
// one shared pipeline every RoomDraft mutation in this package goes
// through (see that function's doc comment). There is no independent
// RoomDraft mutation path here or anywhere else in this file.
func (s *Service) SubmitEditOperation(ctx context.Context, companyID string, input SubmitEditOperationInput) (SubmitEditOperationResult, error) {
	op, err := decodeEditOperation(input.Kind, input.Payload)
	if err != nil {
		return SubmitEditOperationResult{}, err
	}
	if err := op.Validate(); err != nil {
		return SubmitEditOperationResult{}, err
	}

	// assign_visual_asset is the one operation whose full correctness
	// cannot be verified by Validate() alone (which is local/structural
	// only, per its own doc comment) — it references an external
	// VisualAssetVersion that must actually exist and belong to this
	// caller's company. This check runs OUTSIDE submitAgainstRoomDraft's
	// pure, in-memory Transform/Apply pipeline, exactly like op.Validate()
	// above; it is safe to run before the CAS transaction specifically
	// because published asset versions are immutable and deletion is out
	// of scope for RP4D — there is no window in which this answer could
	// go stale before the transaction commits. clear_visual_asset needs
	// no such check (nothing to authorize).
	if assign, ok := op.(*AssignVisualAssetOperation); ok {
		if s.visualAssets == nil {
			return SubmitEditOperationResult{}, ErrVisualAssetSupportNotConfigured
		}
		if _, err := s.visualAssets.FindByCompanyAssetVersion(ctx, companyID, assign.AssetID, assign.Version); err != nil {
			return SubmitEditOperationResult{}, err
		}
	}

	return s.submitAgainstRoomDraft(ctx, companyID, roomDraftMutationRequest{
		RoomDraftID:      input.RoomDraftID,
		OperationID:      input.OperationID,
		Kind:             string(input.Kind),
		Payload:          string(input.Payload),
		ExpectedRevision: input.ExpectedRevision,
		ActorUserID:      input.ActorUserID,
		Transform:        op.Apply,
	})
}

// resetToScanKind is the OperationKind literal recorded on a Reset-to-Scan's
// RoomDraftEditRecord. Deliberately NOT one of RP4A's 27 EditOperationKind
// constants and NOT dispatched through decodeEditOperation — Reset-to-Scan
// stays semantically outside the EditOperation vocabulary (RP4A never
// modeled it as one of the 27 operations; design spec §8.25 treats it as
// its own concept), while still sharing every piece of submission
// machinery (idempotency, fingerprint, CAS, audit) via submitAgainstRoomDraft.
const resetToScanKind = "reset_to_scan"

// SubmitResetToScan exposes ResetRoomDraftToBaseline through the same
// canonical edit transport as SubmitEditOperation (plan §RP4B §8) — a thin
// adapter that constructs the reset transform and submits it through the
// EXACT SAME submitAgainstRoomDraft pipeline SubmitEditOperation uses:
// same OperationID idempotency, same expected-revision/CAS semantics, same
// audit record shape. There is no separate mutation/persistence path for
// reset.
func (s *Service) SubmitResetToScan(ctx context.Context, companyID, roomDraftID, operationID string, expectedRevision int64, actorUserID string) (SubmitEditOperationResult, error) {
	resetTransform := func(draft RoomDraft) (RoomDraft, error) {
		if draft.OriginalBaseline == nil {
			return RoomDraft{}, ErrRoomDraftHasNoBaseline
		}
		reset := draft
		reset.Walls = draft.OriginalBaseline.Walls
		reset.Openings = draft.OriginalBaseline.Openings
		reset.Objects = draft.OriginalBaseline.Objects
		reset.Fixtures = nil
		reset.ServicePoints = nil
		reset.Constraints = nil
		return reset, nil
	}

	// Pre-reset snapshot for Undo Reset (§11), needed because
	// submitAgainstRoomDraft computes this request's OperationFingerprint
	// from req.Payload BEFORE running req.Transform, so the payload must
	// already exist by the time it's called. A retry of the SAME
	// operationId must reuse the ORIGINAL attempt's snapshot byte-for-byte
	// (checked here first) rather than recompute one from the current
	// draft — by retry time the draft has already been reset, so a freshly
	// computed "snapshot" would silently differ from the first attempt's
	// and be wrongly flagged as a conflicting reuse of the same
	// operationId. The actual mutation still goes through
	// submitAgainstRoomDraft's own authoritative read + CAS either way;
	// this lookup exists only to make the RECORDED payload stable across
	// retries.
	var payload string
	if existing, err := s.roomDraftEdits.FindByOperationID(ctx, companyID, operationID); err == nil {
		payload = existing.OperationPayload
	} else if !errors.Is(err, ErrRoomDraftEditRecordNotFound) {
		return SubmitEditOperationResult{}, err
	} else {
		current, err := s.roomDrafts.FindByID(ctx, companyID, roomDraftID)
		if err != nil {
			return SubmitEditOperationResult{}, err
		}
		marshalled, err := json.Marshal(resetSnapshot{
			Walls: current.Walls, Openings: current.Openings, Objects: current.Objects,
			Fixtures: current.Fixtures, ServicePoints: current.ServicePoints, Constraints: current.Constraints,
		})
		if err != nil {
			return SubmitEditOperationResult{}, err
		}
		payload = string(marshalled)
	}

	return s.submitAgainstRoomDraft(ctx, companyID, roomDraftMutationRequest{
		RoomDraftID:      roomDraftID,
		OperationID:      operationID,
		Kind:             resetToScanKind,
		Payload:          payload,
		ExpectedRevision: expectedRevision,
		ActorUserID:      actorUserID,
		Transform:        resetTransform,
	})
}

// resetSnapshot is Reset-to-Scan's own pre-reset undo data — exactly the
// fields ResetRoomDraftToBaseline/SubmitResetToScan's resetTransform
// mutates, nothing else. Recorded as the reset operation's own
// RoomDraftEditRecord.OperationPayload; never a full RoomDraft document.
type resetSnapshot struct {
	Walls         []RoomDraftWall         `json:"walls"`
	Openings      []RoomDraftOpening      `json:"openings"`
	Objects       []RoomDraftObject       `json:"objects"`
	Fixtures      []RoomDraftFixture      `json:"fixtures"`
	ServicePoints []RoomDraftServicePoint `json:"servicePoints"`
	Constraints   []RoomDraftConstraint   `json:"constraints"`
}

// ErrNotAResetOperation is returned by UndoResetToScan when resetOperationID
// refers to a real, previously-applied operation that was not itself a
// Reset-to-Scan — undoing it through this path would apply an unrelated
// operation's payload as if it were a reset snapshot.
var ErrNotAResetOperation = errors.New("spatial: referenced operation is not a reset-to-scan")

// UndoResetToScan restores roomDraftID to exactly the state it held
// immediately before the Reset-to-Scan identified by resetOperationID (§11)
// — looked up via the SAME RoomDraftEditRecord audit trail every other
// operation is recorded in, under the SAME expectedRevision CAS/idempotency
// pipeline (submitAgainstRoomDraft). This is one atomic history entry from
// the caller's perspective: exactly one new revision, never a replay of
// every operation reset would otherwise have undone individually.
func (s *Service) UndoResetToScan(ctx context.Context, companyID, roomDraftID, resetOperationID, undoOperationID string, expectedRevision int64, actorUserID string) (SubmitEditOperationResult, error) {
	resetRecord, err := s.roomDraftEdits.FindByOperationID(ctx, companyID, resetOperationID)
	if err != nil {
		return SubmitEditOperationResult{}, err
	}
	if resetRecord.RoomDraftID != roomDraftID || resetRecord.OperationKind != resetToScanKind {
		return SubmitEditOperationResult{}, ErrNotAResetOperation
	}
	var snapshot resetSnapshot
	if err := json.Unmarshal([]byte(resetRecord.OperationPayload), &snapshot); err != nil {
		return SubmitEditOperationResult{}, err
	}

	undoTransform := func(draft RoomDraft) (RoomDraft, error) {
		restored := draft
		restored.Walls = snapshot.Walls
		restored.Openings = snapshot.Openings
		restored.Objects = snapshot.Objects
		restored.Fixtures = snapshot.Fixtures
		restored.ServicePoints = snapshot.ServicePoints
		restored.Constraints = snapshot.Constraints
		return restored, nil
	}

	return s.submitAgainstRoomDraft(ctx, companyID, roomDraftMutationRequest{
		RoomDraftID:      roomDraftID,
		OperationID:      undoOperationID,
		Kind:             undoResetToScanKind,
		Payload:          resetRecord.OperationPayload,
		ExpectedRevision: expectedRevision,
		ActorUserID:      actorUserID,
		Transform:        undoTransform,
	})
}

// undoResetToScanKind mirrors resetToScanKind's precedent — its own audit
// literal, outside the EditOperationKind vocabulary, never dispatched
// through decodeEditOperation.
const undoResetToScanKind = "undo_reset_to_scan"

// roomDraftMutationRequest is submitAgainstRoomDraft's input: the shared
// submission-envelope fields every RoomDraft mutation needs (idempotency
// key, CAS revision, audit descriptor) plus Transform — the ONE thing that
// actually differs between an EditOperation and Reset-to-Scan.
type roomDraftMutationRequest struct {
	RoomDraftID      string
	OperationID      string
	ExpectedRevision int64
	ActorUserID      string
	// Kind/Payload are the audit/fingerprint descriptor recorded on the
	// resulting RoomDraftEditRecord — an EditOperationKind string + its raw
	// wire JSON for a real edit operation, or resetToScanKind + "" for
	// Reset-to-Scan. Never re-derived from Transform; callers supply
	// exactly what should be fingerprinted and audited.
	Kind    string
	Payload string
	// Transform is the actual draft mutation: the already-decoded-and-
	// validated EditOperation's Apply method, or the reset-to-baseline
	// closure. This is the only piece of logic that varies between
	// SubmitEditOperation and SubmitResetToScan.
	Transform func(RoomDraft) (RoomDraft, error)
}

// submitAgainstRoomDraft is the ONE shared submission pipeline every
// RoomDraft mutation in this package goes through — SubmitEditOperation and
// SubmitResetToScan are both thin adapters over this, differing only in
// what Transform does and what Kind/Payload get audited. It owns:
//
//  1. OperationID lookup FIRST, before any other work
//     (awards/finalisation_service.go's FinaliseAward precedent: "an
//     unknown-outcome retry resolves by operation ID FIRST" — a
//     lost-response retry must never re-apply the mutation). If found, the
//     fingerprint of THIS request is compared against the existing
//     record's: a match replays the original result; a mismatch returns
//     ErrOperationIDConflict (the same idempotency key reused for a
//     different mutation).
//  2. Loading the authoritative RoomDraft and running req.Transform against
//     it in-memory — the caller submits INTENT (an operation or a reset),
//     never a replacement draft; nothing client-supplied is trusted as
//     authoritative state.
//  3. Constructing the RoomDraftEditRecord audit/idempotency record.
//  4. Atomically CAS-updating the RoomDraft and inserting the record in one
//     transaction (RoomDraftEditApplier.ApplyAndRecord), guarded by
//     req.ExpectedRevision — a stale revision surfaces as
//     ErrRoomDraftRevisionMismatch, never silently overwriting a
//     concurrent writer's result.
//
// There is no other RoomDraft mutation entry point in this package: every
// caller that needs to change a RoomDraft's Walls/Openings/Objects/
// Fixtures/ServicePoints/Constraints under CAS + audit goes through here.
func (s *Service) submitAgainstRoomDraft(ctx context.Context, companyID string, req roomDraftMutationRequest) (SubmitEditOperationResult, error) {
	fingerprint := computeOperationFingerprint(EditOperationKind(req.Kind), []byte(req.Payload))

	if existing, err := s.roomDraftEdits.FindByOperationID(ctx, companyID, req.OperationID); err == nil {
		if existing.RoomDraftID != req.RoomDraftID || existing.OperationFingerprint != fingerprint {
			return SubmitEditOperationResult{}, ErrOperationIDConflict
		}
		draft, findErr := s.roomDrafts.FindByID(ctx, companyID, existing.RoomDraftID)
		if findErr != nil {
			return SubmitEditOperationResult{}, findErr
		}
		return SubmitEditOperationResult{RoomDraft: draft, Record: existing, Replayed: true}, nil
	} else if !errors.Is(err, ErrRoomDraftEditRecordNotFound) {
		return SubmitEditOperationResult{}, err
	}

	existingDraft, err := s.roomDrafts.FindByID(ctx, companyID, req.RoomDraftID)
	if err != nil {
		return SubmitEditOperationResult{}, err
	}
	updatedDraft, err := req.Transform(existingDraft)
	if err != nil {
		return SubmitEditOperationResult{}, err
	}

	record := RoomDraftEditRecord{
		CompanyID: companyID, RoomDraftID: req.RoomDraftID,
		OperationID: req.OperationID, OperationFingerprint: fingerprint,
		OperationKind: req.Kind, OperationPayload: req.Payload,
		BaseRevision: req.ExpectedRevision, ResultingRevision: req.ExpectedRevision + 1,
		ActorUserID: req.ActorUserID, CreatedAt: time.Now(), SchemaVersion: 1,
	}

	resultDraft, resultRecord, replayed, err := s.roomDraftEdits.ApplyAndRecord(ctx, companyID, req.RoomDraftID, updatedDraft, req.ExpectedRevision, record)
	if err != nil {
		return SubmitEditOperationResult{}, err
	}
	return SubmitEditOperationResult{RoomDraft: resultDraft, Record: resultRecord, Replayed: replayed}, nil
}

// AdvanceCapture transitions captureID to newStatus if legal from its
// current stored status, tenant-scoped to companyID.
func (s *Service) AdvanceCapture(ctx context.Context, companyID, captureID string, newStatus CaptureStatus) (SpatialCapture, error) {
	current, err := s.captures.FindByID(ctx, companyID, captureID)
	if err != nil {
		return SpatialCapture{}, err
	}
	if !legalCaptureTransitions[current.Status][newStatus] {
		return SpatialCapture{}, ErrIllegalCaptureTransition
	}
	return s.captures.UpdateStatus(ctx, companyID, captureID, current.Status, newStatus)
}

// ConfirmCapture is the authority transition (design spec §4.2): review ->
// confirmed. It creates an immutable SpatialRoomVersion, supersedes the
// Space's previous current version, and atomically updates
// SpatialSpaceState's current pointer under CAS. On a concurrent-confirmation
// race it retries against the freshly-read revision rather than surfacing
// the mismatch to the caller, since both confirmations are individually
// legitimate operations and the current-room invariant only requires that
// exactly one version end up current, not that later confirmations fail.
func (s *Service) ConfirmCapture(ctx context.Context, companyID, captureID string) (SpatialCapture, error) {
	capture, err := s.captures.FindByID(ctx, companyID, captureID)
	if err != nil {
		return SpatialCapture{}, err
	}
	if capture.Status != CaptureStatusReview {
		return SpatialCapture{}, ErrIllegalCaptureTransition
	}

	version, err := s.versions.Create(ctx, SpatialRoomVersion{
		CompanyID: companyID, ProjectID: capture.ProjectID, SpaceID: capture.SpaceID,
		CaptureID: captureID, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		return SpatialCapture{}, err
	}

	const maxRetries = 5
	for attempt := 0; attempt < maxRetries; attempt++ {
		state, err := s.spaceStates.FindOrCreateBySpace(ctx, companyID, capture.ProjectID, capture.SpaceID)
		if err != nil {
			return SpatialCapture{}, err
		}
		previousCurrentID := state.CurrentRoomVersionID

		_, err = s.spaceStates.SetCurrentRoomVersion(ctx, companyID, capture.SpaceID, version.ID, state.Revision)
		if errors.Is(err, ErrSpaceStateRevisionMismatch) {
			continue // another confirmation raced ahead; retry against fresh revision
		}
		if err != nil {
			return SpatialCapture{}, err
		}

		if previousCurrentID != "" {
			if err := s.versions.Supersede(ctx, companyID, previousCurrentID); err != nil {
				return SpatialCapture{}, err
			}
		}

		return s.captures.MarkConfirmed(ctx, companyID, captureID, version.ID)
	}
	return SpatialCapture{}, ErrSpaceStateRevisionMismatch
}

// GetCapture returns captureID's SpatialCapture, tenant-scoped to companyID.
func (s *Service) GetCapture(ctx context.Context, companyID, captureID string) (SpatialCapture, error) {
	return s.captures.FindByID(ctx, companyID, captureID)
}

// ListCapturesBySpace returns spaceID's captures, tenant-scoped to companyID.
func (s *Service) ListCapturesBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialCapture, error) {
	return s.captures.ListBySpace(ctx, companyID, spaceID)
}

// GetSpaceState returns spaceID's SpatialSpaceState, tenant-scoped to
// companyID, creating one if none exists yet.
func (s *Service) GetSpaceState(ctx context.Context, companyID, projectID, spaceID string) (SpatialSpaceState, error) {
	return s.spaceStates.FindOrCreateBySpace(ctx, companyID, projectID, spaceID)
}

// GetRoomVersion returns roomVersionID's SpatialRoomVersion, tenant-scoped to
// companyID.
func (s *Service) GetRoomVersion(ctx context.Context, companyID, roomVersionID string) (SpatialRoomVersion, error) {
	return s.versions.FindByID(ctx, companyID, roomVersionID)
}

// ListRoomVersionsBySpace returns spaceID's room versions, tenant-scoped to
// companyID.
func (s *Service) ListRoomVersionsBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialRoomVersion, error) {
	return s.versions.ListBySpace(ctx, companyID, spaceID)
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public repository interfaces. Only the real Mongo repositories
// implement it (spaces.companyBulkDeleter pattern).
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every SpatialCapture,
// SpatialRoomVersion, and SpatialSpaceState owned by companyID.
// Development-tool use only (demoseed reset). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	captureDeleter, ok := s.captures.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("spatial: capture repository %T does not support DeleteAllForCompany", s.captures)
	}
	if err := captureDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	versionDeleter, ok := s.versions.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("spatial: room version repository %T does not support DeleteAllForCompany", s.versions)
	}
	if err := versionDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	stateDeleter, ok := s.spaceStates.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("spatial: space state repository %T does not support DeleteAllForCompany", s.spaceStates)
	}
	if err := stateDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	// s.artifacts is nil until SetArtifactSupport is called (Task 3 is
	// optional at construction time — see SetArtifactSupport's doc comment).
	if s.artifacts != nil {
		artifactDeleter, ok := s.artifacts.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("spatial: artifact repository %T does not support DeleteAllForCompany", s.artifacts)
		}
		if err := artifactDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	// s.roomDrafts is nil until SetRoomDraftSupport is called (plan §RP3 —
	// same optional-at-construction pattern as SetArtifactSupport).
	if s.roomDrafts == nil {
		return nil
	}
	roomDraftDeleter, ok := s.roomDrafts.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("spatial: room draft repository %T does not support DeleteAllForCompany", s.roomDrafts)
	}
	return roomDraftDeleter.DeleteAllForCompany(ctx, companyID)
}
