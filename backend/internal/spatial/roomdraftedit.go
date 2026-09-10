package spatial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// This file implements plan §RP4B's edit-application transport/persistence
// layer: the authoritative HTTP contract for submitting RP4A EditOperations
// against a server-backed RoomDraft, with CAS revisioning and idempotent
// retry. Reuses RoomDraftRepository.Update's existing CAS pattern (RP3/RP4A)
// and mirrors awards/finalisation_service.go's InsertRevision precedent —
// "an unknown-outcome retry resolves by operation ID FIRST" — as closely as
// possible, per explicit design direction: one persisted record
// (RoomDraftEditRecord) serves as BOTH the audit trail (design spec
// requirement) and the idempotency mechanism, never a separate TTL'd cache.

// RoomDraftEditRecord is the immutable record of one successfully applied
// edit operation (plan §RP4B) — simultaneously the audit trail (what
// mutation produced each revision) and the idempotency mechanism (a retried
// request with the same OperationID adopts this record's result instead of
// re-applying the operation). Never mutated after insert, matching
// AwardRevision's "prior revisions are never mutated" precedent.
type RoomDraftEditRecord struct {
	ID          string `bson:"_id,omitempty" json:"id"`
	CompanyID   string `bson:"companyId" json:"companyId"`
	RoomDraftID string `bson:"roomDraftId" json:"roomDraftId"`
	// OperationID is the client-supplied idempotency key — unique per
	// (CompanyID, RoomDraftID). A retry carrying the same OperationID and
	// the same OperationFingerprint adopts this record; a retry carrying
	// the same OperationID but a DIFFERENT fingerprint is a conflict (the
	// key was reused for what claims to be a different edit).
	OperationID string `bson:"operationId" json:"operationId"`
	// OperationFingerprint is the SHA-256 hex digest of the canonical wire
	// JSON (kind + payload) that produced this record — the content-fingerprint
	// comparison AwardRevision's InsertRevision uses, generalized to work
	// across 27 operation types without a hand-written per-field formatter.
	OperationFingerprint string `bson:"operationFingerprint" json:"-"`
	// OperationKind/OperationPayload preserve exactly what was submitted,
	// for audit/history purposes — OperationPayload is the raw wire JSON
	// (the operation's own typed fields), not re-derived from the applied
	// RoomDraft.
	OperationKind     string `bson:"operationKind" json:"operationKind"`
	OperationPayload  string `bson:"operationPayload" json:"operationPayload"`
	BaseRevision      int64  `bson:"baseRevision" json:"baseRevision"`
	ResultingRevision int64  `bson:"resultingRevision" json:"resultingRevision"`
	// ActorUserID is the authenticated principal who submitted the edit,
	// when available (matches AwardRevision.FinalisedByUserID's pattern).
	ActorUserID   string    `bson:"actorUserId,omitempty" json:"actorUserId,omitempty"`
	CreatedAt     time.Time `bson:"createdAt" json:"createdAt"`
	SchemaVersion int       `bson:"schemaVersion" json:"schemaVersion"`
}

// computeOperationFingerprint returns the SHA-256 hex digest of kind's wire
// discriminator concatenated with payload's canonical JSON bytes — the
// content-fingerprint AwardRevision.SelectionFingerprint's comparison
// generalizes to. payload must be the EXACT bytes submitted on the wire
// (never a value re-encoded after decoding), so two submissions are only
// ever considered identical if their literal request bodies were identical
// — matching plan §RP4B's "do not rely on operation equality heuristics
// alone."
func computeOperationFingerprint(kind EditOperationKind, payload json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0}) // separator so kind+payload cannot collide with a differently-split concatenation
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

// ComputeOperationFingerprintForTest exposes computeOperationFingerprint to
// external (spatial_test) test files — matching
// config.NewAllowedOriginsForTest's existing "exported test-only helper"
// convention. Production callers always go through Service
// .SubmitEditOperation/SubmitResetToScan, which compute this internally;
// this exists only so repository-level tests calling
// RoomDraftEditApplier.ApplyAndRecord directly (bypassing the Service
// layer) can construct a RoomDraftEditRecord whose OperationFingerprint is
// real rather than an empty string.
func ComputeOperationFingerprintForTest(kind EditOperationKind, payload json.RawMessage) string {
	return computeOperationFingerprint(kind, payload)
}

// decodeEditOperation is the single wire-decode dispatch point: given the
// "kind" discriminator and the raw "payload" JSON, it returns the concrete
// EditOperation value that matches. Returns ErrUnsupportedEditOperationKind
// for an unrecognized kind, ErrMalformedEditOperationPayload if the payload
// does not decode into that operation's typed fields — both distinct from
// ErrInvalidEditOperation, which is raised only once the operation is
// successfully decoded and its own Validate() runs.
func decodeEditOperation(kind EditOperationKind, payload json.RawMessage) (EditOperation, error) {
	var op EditOperation
	switch kind {
	case EditOpMoveCorner:
		op = &MoveCornerOperation{}
	case EditOpMoveWall:
		op = &MoveWallOperation{}
	case EditOpSetWallThickness:
		op = &SetWallThicknessOperation{}
	case EditOpAddOpening:
		op = &AddOpeningOperation{}
	case EditOpRemoveOpening:
		op = &RemoveOpeningOperation{}
	case EditOpMoveOpening:
		op = &MoveOpeningOperation{}
	case EditOpResizeOpening:
		op = &ResizeOpeningOperation{}
	case EditOpReclassifyOpening:
		op = &ReclassifyOpeningOperation{}
	case EditOpSetDoorLeafCount:
		op = &SetDoorLeafCountOperation{}
	case EditOpSetDoorHinge:
		op = &SetDoorHingeOperation{}
	case EditOpSetDoorSwing:
		op = &SetDoorSwingOperation{}
	case EditOpSetDoorOpenDirection:
		op = &SetDoorOpenDirectionOperation{}
	case EditOpAddObject:
		op = &AddObjectOperation{}
	case EditOpMoveObject:
		op = &MoveObjectOperation{}
	case EditOpRotateObject:
		op = &RotateObjectOperation{}
	case EditOpResizeObject:
		op = &ResizeObjectOperation{}
	case EditOpReclassifyObject:
		op = &ReclassifyObjectOperation{}
	case EditOpRemoveObject:
		op = &RemoveObjectOperation{}
	case EditOpRestoreElement:
		op = &RestoreElementOperation{}
	case EditOpAddFixture:
		op = &AddFixtureOperation{}
	case EditOpMoveFixture:
		op = &MoveFixtureOperation{}
	case EditOpResizeFixture:
		op = &ResizeFixtureOperation{}
	case EditOpReclassifyFixture:
		op = &ReclassifyFixtureOperation{}
	case EditOpRemoveFixture:
		op = &RemoveFixtureOperation{}
	case EditOpAddServicePoint:
		op = &AddServicePointOperation{}
	case EditOpMoveServicePoint:
		op = &MoveServicePointOperation{}
	case EditOpRemoveServicePoint:
		op = &RemoveServicePointOperation{}
	case EditOpAddConstraint:
		op = &AddConstraintOperation{}
	case EditOpMoveConstraint:
		op = &MoveConstraintOperation{}
	case EditOpRemoveConstraint:
		op = &RemoveConstraintOperation{}
	case EditOpApplyVerifiedMeasurement:
		op = &ApplyVerifiedMeasurementOperation{}
	case EditOpAssignVisualAsset:
		op = &AssignVisualAssetOperation{}
	case EditOpClearVisualAsset:
		op = &ClearVisualAssetOperation{}
	case EditOpSetVisualAppearance:
		op = &SetVisualAppearanceOperation{}
	case EditOpClearVisualAppearance:
		op = &ClearVisualAppearanceOperation{}
	default:
		return nil, ErrUnsupportedEditOperationKind
	}
	if err := json.Unmarshal(payload, op); err != nil {
		return nil, ErrMalformedEditOperationPayload
	}
	return op, nil
}
