package spatial

import "time"

// ArtifactKind is the type of binary content a SpatialArtifact holds
// (design spec §9, §11).
type ArtifactKind string

const (
	ArtifactKindRGBKeyframe            ArtifactKind = "rgb_keyframe"
	ArtifactKindDepthMap               ArtifactKind = "depth_map"
	ArtifactKindObservationPhoto       ArtifactKind = "observation_photo"
	ArtifactKindCaptureManifest        ArtifactKind = "capture_manifest"
	ArtifactKindStructuralGLB          ArtifactKind = "structural_glb"
	ArtifactKindRawReconstruction      ArtifactKind = "raw_reconstruction"
	ArtifactKindRefinedMesh            ArtifactKind = "refined_mesh"
	ArtifactKindTextureSet             ArtifactKind = "texture_set"
	ArtifactKindReconstructionManifest ArtifactKind = "reconstruction_manifest"
	// ArtifactKindCapturedRoomData is Apple's raw serialized CapturedRoomData
	// for a RoomPlan session (design spec §8.6.2, plan §RP3) — the durable
	// source-of-truth persisted before any RoomDraft normalization occurs.
	ArtifactKindCapturedRoomData ArtifactKind = "captured_room_data"
	// ArtifactKindRoomPlanProcessed is the RoomBuilder-processed CapturedRoom
	// result derived from ArtifactKindCapturedRoomData.
	ArtifactKindRoomPlanProcessed ArtifactKind = "roomplan_processed"
	// ArtifactKindRoomDraftJSON is a serialized RoomDraft snapshot artifact
	// (distinct from the authoritative RoomDraft persisted via
	// RoomDraftRepository — this is the artifact-store copy for sync/backup).
	ArtifactKindRoomDraftJSON ArtifactKind = "roomdraft_json"
	// ArtifactKindUSDZ is an optional Apple USDZ export of a captured room.
	ArtifactKindUSDZ ArtifactKind = "usdz"
)

// validArtifactKinds is used to reject an artifact kind outside the V1 set
// (Task 3 TDD requirement: "invalid artifact kind").
var validArtifactKinds = map[ArtifactKind]bool{
	ArtifactKindRGBKeyframe: true, ArtifactKindDepthMap: true, ArtifactKindObservationPhoto: true,
	ArtifactKindCaptureManifest: true, ArtifactKindStructuralGLB: true, ArtifactKindRawReconstruction: true,
	ArtifactKindRefinedMesh: true, ArtifactKindTextureSet: true, ArtifactKindReconstructionManifest: true,
	ArtifactKindCapturedRoomData: true, ArtifactKindRoomPlanProcessed: true,
	ArtifactKindRoomDraftJSON: true, ArtifactKindUSDZ: true,
}

// ArtifactStatus tracks whether an artifact's bytes have been confirmed
// present and valid in the ObjectStore.
type ArtifactStatus string

const (
	// ArtifactStatusPending means an upload slot was issued but the client
	// has not yet (successfully) finalized the upload. Resumable: the same
	// artifact can be retried without creating a duplicate record.
	ArtifactStatusPending ArtifactStatus = "pending"
	// ArtifactStatusUploaded means FinalizeUpload validated the object's
	// existence, size, and checksum against the ObjectStore.
	ArtifactStatusUploaded ArtifactStatus = "uploaded"
)

// maxArtifactSizeBytes bounds a single artifact's declared size (design spec
// §42: "upload size/count limits"). 200 MiB comfortably covers a raw
// reconstruction bundle entry while rejecting pathological uploads.
const maxArtifactSizeBytes = 200 * 1024 * 1024

// allowedArtifactContentTypes is the V1 content-type allowlist (design spec
// §42: "allowed content types"). Kept narrow and explicit rather than a
// prefix match, since spatial artifacts are a closed set of known formats.
var allowedArtifactContentTypes = map[string]bool{
	"image/jpeg":               true,
	"image/png":                true,
	"application/octet-stream": true, // depth maps, raw reconstruction blobs, CapturedRoomData
	"model/gltf-binary":        true, // .glb
	"application/json":         true, // manifests, RoomDraft snapshots
	"model/vnd.usdz+zip":       true, // Apple USDZ export
}

// SpatialArtifact is metadata for one binary object belonging to a
// SpatialCapture (design spec §9, §11). The binary content itself lives in
// ObjectStore, addressed by ObjectKey — never in this record and never
// directly in MongoDB.
type SpatialArtifact struct {
	ID            string         `bson:"_id,omitempty" json:"id"`
	CompanyID     string         `bson:"companyId" json:"companyId"`
	CaptureID     string         `bson:"captureId" json:"captureId"`
	Kind          ArtifactKind   `bson:"kind" json:"kind"`
	ObjectKey     string         `bson:"objectKey" json:"-"` // never exposed to clients directly; only via signed URL
	ContentType   string         `bson:"contentType" json:"contentType"`
	DeclaredSize  int64          `bson:"declaredSize" json:"declaredSize"`
	ActualSize    int64          `bson:"actualSize,omitempty" json:"actualSize,omitempty"`
	Checksum      string         `bson:"checksum" json:"checksum"` // client-declared SHA-256 hex, verified on finalize
	Status        ArtifactStatus `bson:"status" json:"status"`
	CreatedAt     time.Time      `bson:"createdAt" json:"createdAt"`
	UploadedAt    *time.Time     `bson:"uploadedAt,omitempty" json:"uploadedAt,omitempty"`
	SchemaVersion int            `bson:"schemaVersion" json:"schemaVersion"`

	// UploadTokenHash/UploadTokenExpiresAt gate PUTting bytes to ObjectKey.
	// Reissued (overwritten) on every RequestArtifactUpload call, so a
	// resumed upload after a fresh signed-URL request invalidates any
	// earlier token for the same artifact rather than accumulating valid
	// tokens (design spec §42: short-lived signed URLs).
	UploadTokenHash      string    `bson:"uploadTokenHash,omitempty" json:"-"`
	UploadTokenExpiresAt time.Time `bson:"uploadTokenExpiresAt,omitempty" json:"-"`
}
