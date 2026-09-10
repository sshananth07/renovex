package spatial

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// uploadTokenBytes is the entropy size of a raw artifact upload token.
// Matches access.GenerateAccessToken's precedent (256 bits).
const uploadTokenBytes = 32

// uploadTokenTTL is how long an issued upload token remains valid (design
// spec §42: short-lived signed URLs). A fresh RequestArtifactUpload or
// ResumeArtifactUpload call reissues a new token with a new deadline.
const uploadTokenTTL = 15 * time.Minute

// ArtifactObjectStore is the capability spatial needs from
// platform/objectstore to store and validate an uploaded artifact. Defined
// here (consumer-defines-interface, matching SpaceLookup) so spatial never
// imports the objectstore package directly — only the composition root
// wires a concrete adapter satisfying this narrow interface.
type ArtifactObjectStore interface {
	// Put writes content under key and returns the byte count written —
	// the content-proxy upload path (Go accepts bytes from the client and
	// forwards them to the store, since V1 has no client-facing presigned
	// URL for local filesystem storage).
	Put(ctx context.Context, key string, content io.Reader) (int64, error)
	Exists(ctx context.Context, key string) (bool, error)
	// ObjectChecksumAndSize returns the SHA-256 hex checksum and byte size
	// of the object stored under key. Returns ErrArtifactObjectMissing if
	// key does not exist.
	ObjectChecksumAndSize(ctx context.Context, key string) (checksum string, size int64, err error)
	// Get streams an artifact's stored bytes (RP4E0 — needed to actually
	// decode/validate a source image before spending GPU quota; not
	// needed by any RP1-RP4D caller until now).
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

// SetArtifactSupport wires artifacts and store into Service after
// construction. A setter (not a NewService parameter) because Task 2's
// capture/room-version behavior has no artifact dependency and every
// existing call site (composition root, all prior tests) must keep
// constructing Service exactly as before; NewService's signature is not
// re-broken by each new capability spatial gains.
func (s *Service) SetArtifactSupport(artifacts ArtifactRepository, store ArtifactObjectStore) {
	s.artifacts = artifacts
	s.objectStore = store
}

// hashArtifactToken returns the hex-encoded SHA-256 hash of a raw upload
// token — the only representation persisted (access.HashAccessToken
// pattern).
func hashArtifactToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func generateArtifactToken() (string, error) {
	buf := make([]byte, uploadTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// objectKeyFor builds the ObjectStore key for one artifact. Tenant- and
// capture-scoped so cross-tenant key collisions are structurally
// impossible, matching design spec §42's tenant-scoped metadata requirement
// extended to the storage layer.
func objectKeyFor(companyID, captureID, artifactID string) string {
	return fmt.Sprintf("spatial/%s/%s/%s", companyID, captureID, artifactID)
}

// RequestArtifactUpload validates captureID belongs to companyID, validates
// kind/contentType/declaredSize against the V1 allowlists, and creates a new
// pending SpatialArtifact with a fresh signed upload token. The raw token is
// returned exactly once and only its hash is persisted.
func (s *Service) RequestArtifactUpload(ctx context.Context, companyID, captureID string,
	kind ArtifactKind, contentType string, declaredSize int64, checksum string) (SpatialArtifact, string, error) {

	if _, err := s.captures.FindByID(ctx, companyID, captureID); err != nil {
		return SpatialArtifact{}, "", err
	}
	if !validArtifactKinds[kind] {
		return SpatialArtifact{}, "", ErrInvalidArtifactKind
	}
	if !allowedArtifactContentTypes[contentType] {
		return SpatialArtifact{}, "", ErrInvalidArtifactContentType
	}
	if declaredSize <= 0 || declaredSize > maxArtifactSizeBytes {
		return SpatialArtifact{}, "", ErrArtifactTooLarge
	}

	rawToken, err := generateArtifactToken()
	if err != nil {
		return SpatialArtifact{}, "", err
	}

	created, err := s.artifacts.Create(ctx, SpatialArtifact{
		CompanyID: companyID, CaptureID: captureID, Kind: kind, ContentType: contentType,
		DeclaredSize: declaredSize, Checksum: checksum, Status: ArtifactStatusPending,
		CreatedAt: time.Now(), SchemaVersion: 1,
		UploadTokenHash: hashArtifactToken(rawToken), UploadTokenExpiresAt: time.Now().Add(uploadTokenTTL),
	})
	if err != nil {
		return SpatialArtifact{}, "", err
	}
	created.ObjectKey = objectKeyFor(companyID, captureID, created.ID)
	return created, rawToken, nil
}

// ResumeArtifactUpload reissues a fresh upload token for an existing pending
// artifact — the partial-upload-resume path (design spec §10.2: "Uploads
// resume artifact-by-artifact"). The prior token stops working immediately.
func (s *Service) ResumeArtifactUpload(ctx context.Context, companyID, artifactID string) (SpatialArtifact, string, error) {
	existing, err := s.artifacts.FindByID(ctx, companyID, artifactID)
	if err != nil {
		return SpatialArtifact{}, "", err
	}
	if existing.Status == ArtifactStatusUploaded {
		existing.ObjectKey = objectKeyFor(companyID, existing.CaptureID, existing.ID)
		return existing, "", nil // already finalized; nothing to resume
	}

	rawToken, err := generateArtifactToken()
	if err != nil {
		return SpatialArtifact{}, "", err
	}
	updated, err := s.artifacts.ReissueUploadToken(ctx, companyID, artifactID, hashArtifactToken(rawToken), time.Now().Add(uploadTokenTTL))
	if err != nil {
		return SpatialArtifact{}, "", err
	}
	updated.ObjectKey = objectKeyFor(companyID, updated.CaptureID, updated.ID)
	return updated, rawToken, nil
}

// FinalizeArtifactUpload validates rawToken against the artifact's stored
// token (constant-time, expiry-checked), confirms the object actually
// landed in ObjectStore, verifies its checksum and size against what the
// client declared, and marks the artifact uploaded.
//
// Calling this again on an already-uploaded artifact succeeds idempotently
// without re-checking the token — a retried finalize call after a network
// blip that actually succeeded server-side must not fail just because the
// token was already cleared (Task 3 TDD requirement: duplicate/idempotent
// finalize).
func (s *Service) FinalizeArtifactUpload(ctx context.Context, companyID, artifactID, rawToken string) (SpatialArtifact, error) {
	artifact, err := s.artifacts.FindByID(ctx, companyID, artifactID)
	if err != nil {
		return SpatialArtifact{}, err
	}
	if artifact.Status == ArtifactStatusUploaded {
		return artifact, nil
	}

	if artifact.UploadTokenHash == "" ||
		subtle.ConstantTimeCompare([]byte(hashArtifactToken(rawToken)), []byte(artifact.UploadTokenHash)) != 1 ||
		time.Now().After(artifact.UploadTokenExpiresAt) {
		return SpatialArtifact{}, ErrArtifactUploadTokenInvalid
	}

	key := objectKeyFor(companyID, artifact.CaptureID, artifact.ID)
	exists, err := s.objectStore.Exists(ctx, key)
	if err != nil {
		return SpatialArtifact{}, err
	}
	if !exists {
		return SpatialArtifact{}, ErrArtifactObjectMissing
	}

	actualChecksum, actualSize, err := s.objectStore.ObjectChecksumAndSize(ctx, key)
	if err != nil {
		return SpatialArtifact{}, err
	}
	if actualChecksum != artifact.Checksum {
		return SpatialArtifact{}, ErrArtifactChecksumMismatch
	}

	return s.artifacts.MarkUploaded(ctx, companyID, artifactID, actualSize, time.Now())
}

// PutArtifactContent streams content to ObjectStore under artifactID's key,
// authorized solely by rawToken matching the artifact's stored upload
// token — NOT by companyID/bearer auth, matching a signed-URL's
// authorization model (design spec §11: "short-lived signed URLs"). This is
// the content-proxy path: Go accepts bytes from the client and forwards
// them to the store, since V1 local filesystem storage has no client-facing
// presigned URL. Unlike FinalizeArtifactUpload, an already-uploaded
// artifact's token has already been cleared, so a stray re-PUT after
// finalize correctly fails closed (ErrArtifactUploadTokenInvalid) rather
// than silently overwriting finalized content.
func (s *Service) PutArtifactContent(ctx context.Context, artifactID, rawToken string, content io.Reader) error {
	artifact, err := s.findArtifactByToken(ctx, rawToken)
	if err != nil {
		return err
	}
	if artifact.ID != artifactID {
		return ErrArtifactUploadTokenInvalid
	}

	key := objectKeyFor(artifact.CompanyID, artifact.CaptureID, artifact.ID)
	_, err = s.objectStore.Put(ctx, key, content)
	return err
}

// findArtifactByToken resolves rawToken to its artifact via the
// tenant-agnostic FindByUploadTokenHash (see that method's doc comment for
// why no companyID is available or needed here), then checks expiry.
func (s *Service) findArtifactByToken(ctx context.Context, rawToken string) (SpatialArtifact, error) {
	if rawToken == "" {
		return SpatialArtifact{}, ErrArtifactUploadTokenInvalid
	}
	artifact, err := s.artifacts.FindByUploadTokenHash(ctx, hashArtifactToken(rawToken))
	if err != nil {
		return SpatialArtifact{}, ErrArtifactUploadTokenInvalid
	}
	if time.Now().After(artifact.UploadTokenExpiresAt) {
		return SpatialArtifact{}, ErrArtifactUploadTokenInvalid
	}
	return artifact, nil
}

// ListArtifactsByCapture returns captureID's artifacts, tenant-scoped to
// companyID.
func (s *Service) ListArtifactsByCapture(ctx context.Context, companyID, captureID string) ([]SpatialArtifact, error) {
	return s.artifacts.ListByCapture(ctx, companyID, captureID)
}
