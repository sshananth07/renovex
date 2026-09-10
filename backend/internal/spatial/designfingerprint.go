package spatial

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DesignExecutionFlags describes what a plan requires to move toward
// execution — a FUTURE, out-of-scope slice's concern (RP4E1 never calls
// Hunyuan or executes anything), but the flags themselves are computed and
// persisted now so the public turn DTO and fingerprint are stable across
// that later slice's arrival.
//
// TurnRequiresAssetGeneration/HunyuanRequired are DELIBERATELY split (RP4E1
// design amendment §2, the approved turn-local/cumulative execution-flag
// amendment): TurnRequiresAssetGeneration describes only this turn's own
// delta (false for a material-only turn); HunyuanRequired describes the
// CUMULATIVE working design (stays true across a later material-only turn
// if an earlier turn in the same session proposed unresolved custom
// geometry).
type DesignExecutionFlags struct {
	TurnRequiresAssetGeneration bool
	HunyuanRequired             bool
	RequiresConfirmation        bool
	Executable                  bool
}

// ValidatedSceneEditPlan is the Go-authoritative, fully validated result of
// one turn — the ONLY thing computeDesignPlanFingerprint ever hashes. It
// deliberately excludes provider/model metadata, confidence, prose-only
// review notes, timestamps, IDs, and runtime-notice copy (RP4E1 plan: "The
// plan fingerprint covers a typed, map-free[^1] structure... It excludes
// provider/model metadata, confidence, prose-only review notes,
// timestamps, IDs, and runtime-notice copy.")
//
// [^1] "map-free" in the plan's prose refers to the CONCEPTUAL shape (no
// untyped blobs of unrelated data smuggled in) — ResolvedSpatialOperation's
// own Payload is necessarily a map (it stores a canonical EditOperation
// wire payload), and Go's json.Marshal deterministically sorts map string
// keys, so encoding it through json.Marshal remains a stable, order-
// independent encoding (verified by this file's golden tests).
type ValidatedSceneEditPlan struct {
	Target                   SpatialDesignTarget
	BasedOnRoomDraftRevision int64
	WorkingDesign            WorkingDesign
	Fit                      FitAnalysis
	Execution                DesignExecutionFlags
}

// fingerprintSchemaVersion is versioned independently of any other schema
// version in this package — bumping it invalidates every previously
// computed fingerprint, which is only ever done deliberately.
const fingerprintSchemaVersion = 1

// designPlanFingerprintStruct is the exact typed, versioned shape that
// gets deterministically JSON-encoded and hashed. A field is included here
// if and only if changing it should count as "a materially different
// plan" per the plan document's own exclusion list.
type designPlanFingerprintStruct struct {
	FingerprintSchemaVersion  int
	Target                    SpatialDesignTarget
	BasedOnRoomDraftRevision  int64
	Geometry                  *WorkingDesignGeometry
	Material                  *WorkingDesignMaterial
	ResolvedSpatialOperations []ResolvedSpatialOperation
	FitStatus                 FitStatus
	FitObservations           []FitObservation
	FitWarnings               []FitWarning
	FitBlockers               []FitBlocker
	Execution                 DesignExecutionFlags
}

// computeDesignPlanFingerprint deterministically hashes plan's fingerprint-
// relevant fields — SHA-256 hex, matching money.RoundToMinorUnits-style
// "one centralized function, never reimplemented per-caller" convention
// for a different concern (fingerprinting rather than rounding).
// json.Marshal is used as the deterministic encoder: Go's encoding/json
// always sorts map[string]<T> keys alphabetically when marshaling
// (verified — see this file's TestComputeDesignPlanFingerprint_
// MapKeyOrderDoesNotAffectHash), so a ResolvedSpatialOperation.Payload map
// built in any insertion order encodes identically.
func computeDesignPlanFingerprint(plan ValidatedSceneEditPlan) (string, error) {
	canonical := designPlanFingerprintStruct{
		FingerprintSchemaVersion:  fingerprintSchemaVersion,
		Target:                    plan.Target,
		BasedOnRoomDraftRevision:  plan.BasedOnRoomDraftRevision,
		Geometry:                  plan.WorkingDesign.Geometry,
		Material:                  plan.WorkingDesign.Material,
		ResolvedSpatialOperations: plan.WorkingDesign.ResolvedSpatialOperations,
		FitStatus:                 plan.Fit.Status,
		FitObservations:           plan.Fit.Observations,
		FitWarnings:               plan.Fit.Warnings,
		FitBlockers:               plan.Fit.Blockers,
		Execution:                 plan.Execution,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("spatial: encoding design plan fingerprint: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
