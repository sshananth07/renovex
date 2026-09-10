package spatial

import (
	"context"
	"errors"
	"io"
	"time"
)

// VisualAssetFormat is the rendering format of a visual asset. "glb" and
// "usdz" are the only formats PublishVisualAssetVersion accepts — "gltf"
// (which can reference external buffer/image files) is deliberately never
// publishable through this single-object authorized-asset model (RP4D §3).
type VisualAssetFormat string

const (
	VisualAssetFormatGLB  VisualAssetFormat = "glb"
	VisualAssetFormatUSDZ VisualAssetFormat = "usdz"
)

// VisualAssetPivot mirrors the Web VisualAssetNormalization.pivot contract
// (apps/web/.../three/assets/types.ts) — the loaded mesh's pivot policy
// relative to its own intrinsic bounds, never a substitute for RoomDraft's
// own authoritative dimensions.
type VisualAssetPivot string

const (
	VisualAssetPivotCenter       VisualAssetPivot = "center"
	VisualAssetPivotCenterBottom VisualAssetPivot = "center-bottom"
)

// VisualAssetNormalization mirrors the Web VisualAssetNormalization type
// exactly (intrinsicUnit is always "meters", upAxis is always "y" — both
// fixed by this project's confirmed coordinate contract, never per-asset
// configurable).
type VisualAssetNormalization struct {
	Pivot VisualAssetPivot `bson:"pivot" json:"pivot"`
}

// ErrInvalidVisualAssetRef is returned when a VisualAssetRef fails its own
// structural validation (empty AssetID, non-positive Version).
var ErrInvalidVisualAssetRef = errors.New("spatial: invalid visual asset reference")

// VisualAssetRef is the canonical, persisted per-element visual-asset
// binding stored on RoomDraftFixture/RoomDraftObject. It identifies a
// LOGICAL asset and an EXACT immutable version — never a URL, never a
// storage key, never a mutable "latest" pointer. RP4D's own domain-
// boundary rule: this is the ONLY new persisted field this slice adds to
// RoomDraft; everything else (format, normalization, byte access) lives on
// the server-side VisualAssetVersion record, resolved only at render time.
type VisualAssetRef struct {
	AssetID string `bson:"assetId" json:"assetId"`
	Version int    `bson:"version" json:"version"`
}

// Validate enforces VisualAssetRef's own structural invariants: a non-empty
// AssetID and a Version >= 1. It does NOT verify the referenced asset
// version actually exists or belongs to the caller's company — that
// requires Mongo access and is Service.SubmitEditOperation's job (RP4D's
// own documented boundary: local/structural validation here, external
// authorization at the service layer).
func (r VisualAssetRef) Validate() error {
	if r.AssetID == "" || r.Version < 1 {
		return ErrInvalidVisualAssetRef
	}
	return nil
}

// ErrVisualAssetVersionNotFound is returned when a (companyID, assetID,
// version) lookup finds nothing — including when the version exists but
// belongs to a different company (the query itself is the tenant
// boundary, matching this package's existing convention; a cross-company
// lookup is indistinguishable from a nonexistent one, on purpose).
var ErrVisualAssetVersionNotFound = errors.New("spatial: visual asset version not found")

// ErrVisualAssetSupportNotConfigured is returned when assign_visual_asset
// (or PublishVisualAssetVersion/CreateVisualAssetAccess) is called before
// SetVisualAssetSupport has wired a VisualAssetVersionRepository — a
// configuration error, never a panic.
var ErrVisualAssetSupportNotConfigured = errors.New("spatial: visual asset support not configured")

// ErrInvalidVisualAssetContent is returned when PublishVisualAssetVersion's
// content fails structural validation (empty bytes, wrong magic, an
// external buffer/image reference in a GLB, a malformed or non-STORED-
// compression USDZ archive).
var ErrInvalidVisualAssetContent = errors.New("spatial: invalid visual asset content")

// ErrVisualAssetVersionConflict is returned when a publish attempt for an
// already-published (companyID, assetID, version) submits content/metadata
// that does not exactly match what was already published — the compound
// unique index is the authoritative race guard; this error is what a
// non-identical retry gets instead of silently overwriting or silently
// adopting a different asset.
var ErrVisualAssetVersionConflict = errors.New("spatial: visual asset version conflict")

// VisualAssetVersion is one immutable, published visual-asset version.
// Once inserted, no field is ever mutated — a change publishes a NEW
// Version under the same AssetID instead. StorageKey is server-internal
// only: it must never appear in any DTO returned to a client, and never
// in RoomDraft.
type VisualAssetVersion struct {
	ID            string                   `bson:"_id,omitempty" json:"id"`
	CompanyID     string                   `bson:"companyId" json:"-"`
	AssetID       string                   `bson:"assetId" json:"assetId"`
	Version       int                      `bson:"version" json:"version"`
	Format        VisualAssetFormat        `bson:"format" json:"format"`
	DisplayName   string                   `bson:"displayName" json:"displayName"`
	Normalization VisualAssetNormalization `bson:"normalization" json:"normalization"`
	ContentType   string                   `bson:"contentType" json:"contentType"`
	ByteCount     int64                    `bson:"byteCount" json:"byteCount"`
	Checksum      string                   `bson:"checksum" json:"checksum"`
	StorageKey    string                   `bson:"storageKey" json:"-"`
	CreatedAt     time.Time                `bson:"createdAt" json:"-"`
	SchemaVersion int                      `bson:"schemaVersion" json:"-"`
}

// ReadAccess is a short-lived, provider-issued capability to fetch one
// VisualAssetVersion's bytes — never a substitute for the version's own
// identity, and never persisted anywhere (RP4D's own documented
// identity/access separation).
type ReadAccess struct {
	URL       string
	ExpiresAt time.Time
}

// VisualAssetReadAccessProvider is a provider-neutral seam for minting
// temporary read access to a published VisualAssetVersion's bytes. RP4D
// implements exactly one concrete provider (a local HMAC-capability +
// content-route implementation); a future Vercel Blob/S3-backed provider
// implements this same interface and returns ITS OWN signed URL, with zero
// change to Service.CreateVisualAssetAccess or any Web code.
type VisualAssetReadAccessProvider interface {
	CreateReadAccess(ctx context.Context, assetVersion VisualAssetVersion, ttl time.Duration) (ReadAccess, error)
}

// VisualAssetLocalCapabilityVerifier is a narrower, LOCAL-PROVIDER-SPECIFIC
// capability — deliberately NOT part of VisualAssetReadAccessProvider,
// since a future Vercel Blob/S3-backed provider's browser fetch never
// round-trips through this Go backend again to be verified here at all.
// Only the local HMAC provider (and this package's own content-serving
// route, which is itself a local-provider-only concept) needs this.
type VisualAssetLocalCapabilityVerifier interface {
	VerifyReadAccess(ctx context.Context, capability string) (companyID, assetID string, version int, err error)
}

// VisualAssetObjectStore is the narrow, spatial-package-owned object-store
// capability PublishVisualAssetVersion/the local content route need —
// matching ArtifactObjectStore's existing shape in this same package.
// Unlike ArtifactObjectStore, this interface includes Get: RP4D actually
// streams bytes back out to a client (through an authorized capability),
// where the artifact-upload flow never did.
type VisualAssetObjectStore interface {
	Put(ctx context.Context, key string, content io.Reader) (int64, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Exists(ctx context.Context, key string) (bool, error)
}
