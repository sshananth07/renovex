package spatial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"time"
)

// safeVisualAssetIDPattern is a narrow allowlist for AssetID values that
// will be embedded as a StorageKey path segment — deliberately stricter
// than VisualAssetRef.Validate()'s own bare non-empty check, since an
// AssetID used in an edit-operation binding never touches a filesystem
// path, but one used at PUBLISH time does. Letters, digits, hyphen,
// underscore only — no slashes, dots, or path-traversal-capable
// characters.
var safeVisualAssetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func isSafeVisualAssetIDSegment(s string) bool {
	return s != "" && safeVisualAssetIDPattern.MatchString(s)
}

// PublishVisualAssetVersion validates, checksums, structurally checks, and
// durably stores a new immutable VisualAssetVersion (RP4D §4). Every step
// runs in this exact order:
//  1. validate assetID (safe-character, non-empty) + version >= 1 + format
//     is glb/usdz;
//  2. buffer content while computing SHA-256 and byte count from the
//     ACTUAL bytes — never a caller-supplied checksum/size;
//  3. run minimal structural validation (validateVisualAssetContent);
//  4. derive StorageKey server-side from validated/safe-encoded segments;
//  5. store the immutable object BEFORE inserting the Mongo record — a
//     documented, accepted narrow failure mode: if the object-store write
//     succeeds but the Mongo insert then fails, the result is a harmless
//     orphaned object, never a Mongo record pointing at missing bytes.
//     This is deliberately not "fixed" with a cleanup job in RP4D;
//  6. insert under the compound unique index — a duplicate
//     (companyID, assetID, version) with IDENTICAL metadata adopts the
//     existing record (idempotent publish); conflicting metadata is
//     ErrVisualAssetVersionConflict.
func (s *Service) PublishVisualAssetVersion(
	ctx context.Context,
	companyID, assetID string,
	version int,
	format VisualAssetFormat,
	displayName string,
	normalization VisualAssetNormalization,
	content io.Reader,
) (VisualAssetVersion, error) {
	if !isSafeVisualAssetIDSegment(assetID) || version < 1 {
		return VisualAssetVersion{}, ErrInvalidVisualAssetRef
	}
	if format != VisualAssetFormatGLB && format != VisualAssetFormatUSDZ {
		return VisualAssetVersion{}, ErrInvalidVisualAssetContent
	}

	buf, err := io.ReadAll(content)
	if err != nil {
		return VisualAssetVersion{}, err
	}
	if err := validateVisualAssetContent(format, buf); err != nil {
		return VisualAssetVersion{}, err
	}
	sum := sha256.Sum256(buf)
	checksum := hex.EncodeToString(sum[:])
	byteCount := int64(len(buf))

	storageKey := fmt.Sprintf("visual-assets/%s/%s/v%d/%s.%s", companyID, assetID, version, checksum, format)

	if _, err := s.visualAssetObjectStore.Put(ctx, storageKey, bytes.NewReader(buf)); err != nil {
		return VisualAssetVersion{}, err
	}

	return s.visualAssets.Create(ctx, VisualAssetVersion{
		CompanyID: companyID, AssetID: assetID, Version: version,
		Format: format, DisplayName: displayName, Normalization: normalization,
		ContentType:   visualAssetContentType(format),
		ByteCount:     byteCount,
		Checksum:      checksum,
		StorageKey:    storageKey,
		CreatedAt:     time.Now(),
		SchemaVersion: 1,
	})
}

func visualAssetContentType(format VisualAssetFormat) string {
	switch format {
	case VisualAssetFormatGLB:
		return "model/gltf-binary"
	case VisualAssetFormatUSDZ:
		return "model/vnd.usdz+zip"
	default:
		return "application/octet-stream"
	}
}

// StreamVisualAssetContent verifies capability (a local-provider bearer
// token) and, if valid, resolves and returns the referenced
// VisualAssetVersion plus a reader over its immutable bytes. The caller is
// responsible for closing the returned io.ReadCloser. Deliberately returns
// one generic error for every failure mode (invalid signature, expired,
// unknown asset) — the content route maps ALL of them to the same 401, per
// RP4D §6's "never distinguishing 'expired' from 'tampered' from 'wrong
// asset'" requirement.
func (s *Service) StreamVisualAssetContent(ctx context.Context, capability string) (VisualAssetVersion, io.ReadCloser, error) {
	if s.visualAssetCapabilityVerifier == nil || s.visualAssets == nil || s.visualAssetObjectStore == nil {
		return VisualAssetVersion{}, nil, ErrVisualAssetSupportNotConfigured
	}
	companyID, assetID, version, err := s.visualAssetCapabilityVerifier.VerifyReadAccess(ctx, capability)
	if err != nil {
		return VisualAssetVersion{}, nil, err
	}
	assetVersion, err := s.visualAssets.FindByCompanyAssetVersion(ctx, companyID, assetID, version)
	if err != nil {
		return VisualAssetVersion{}, nil, err
	}
	content, err := s.visualAssetObjectStore.Get(ctx, assetVersion.StorageKey)
	if err != nil {
		return VisualAssetVersion{}, nil, err
	}
	return assetVersion, content, nil
}

// defaultVisualAssetAccessTTL is used when Service.visualAssetAccessTTL was
// never set via SetVisualAssetAccessTTL (RP4D §22's recommended
// local-verification default — kept in sync with
// config.defaultVisualAssetAccessTTL, duplicated rather than imported since
// spatial must not depend on platform/config).
const defaultVisualAssetAccessTTL = 10 * time.Minute

// CreateVisualAssetAccess authorizes and resolves assetID/version
// (tenant-scoped — 404 on miss/cross-company) and delegates temporary
// delivery entirely to the wired VisualAssetReadAccessProvider, using the
// TTL configured via SetVisualAssetAccessTTL (or a 10-minute default if
// never set). This method never constructs a URL or touches signing
// directly — that is the provider's job, so a future Vercel Blob/S3-backed
// provider can replace the local HMAC implementation with zero change here.
func (s *Service) CreateVisualAssetAccess(ctx context.Context, companyID, assetID string, version int) (VisualAssetVersion, ReadAccess, error) {
	if s.visualAssets == nil || s.visualAssetReadAccess == nil {
		return VisualAssetVersion{}, ReadAccess{}, ErrVisualAssetSupportNotConfigured
	}
	ttl := s.visualAssetAccessTTL
	if ttl <= 0 {
		ttl = defaultVisualAssetAccessTTL
	}
	assetVersion, err := s.visualAssets.FindByCompanyAssetVersion(ctx, companyID, assetID, version)
	if err != nil {
		return VisualAssetVersion{}, ReadAccess{}, err
	}
	access, err := s.visualAssetReadAccess.CreateReadAccess(ctx, assetVersion, ttl)
	if err != nil {
		return VisualAssetVersion{}, ReadAccess{}, err
	}
	return assetVersion, access, nil
}
