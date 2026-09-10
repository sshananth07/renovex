package spatial_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func TestMongoArtifactRepository_CreateAndFindByID(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc123",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
		UploadTokenHash: "tokenhash1", UploadTokenExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Status != spatial.ArtifactStatusPending {
		t.Fatalf("expected pending, got %s", found.Status)
	}
}

func TestMongoArtifactRepository_FindByID_CrossTenantDenied(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	created, _ := repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc123",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
	})

	_, err := repo.FindByID(ctx, "company_b", created.ID)
	if err != spatial.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound for cross-tenant read, got %v", err)
	}
}

func TestMongoArtifactRepository_FindByUploadTokenHash(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	created, _ := repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc123",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
		UploadTokenHash: "uniquetokenhash", UploadTokenExpiresAt: time.Now().Add(time.Hour),
	})

	found, err := repo.FindByUploadTokenHash(ctx, "uniquetokenhash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected to find created artifact, got different ID")
	}

	_, err = repo.FindByUploadTokenHash(ctx, "nonexistenttokenhash")
	if err != spatial.ErrArtifactNotFound {
		t.Fatalf("expected ErrArtifactNotFound for unknown token, got %v", err)
	}
}

func TestMongoArtifactRepository_ReissueUploadToken(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	created, _ := repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc123",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
		UploadTokenHash: "oldtoken", UploadTokenExpiresAt: time.Now().Add(time.Hour),
	})

	newExpiry := time.Now().Add(2 * time.Hour)
	updated, err := repo.ReissueUploadToken(ctx, "company_a", created.ID, "newtoken", newExpiry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.UploadTokenHash != "newtoken" {
		t.Fatalf("expected new token hash, got %s", updated.UploadTokenHash)
	}

	// Old token must no longer resolve.
	_, err = repo.FindByUploadTokenHash(ctx, "oldtoken")
	if err != spatial.ErrArtifactNotFound {
		t.Fatalf("expected old token to no longer resolve, got %v", err)
	}
}

func TestMongoArtifactRepository_MarkUploaded_IdempotentAndClearsToken(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	created, _ := repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc123",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
		UploadTokenHash: "sometoken", UploadTokenExpiresAt: time.Now().Add(time.Hour),
	})

	uploaded, err := repo.MarkUploaded(ctx, "company_a", created.ID, 1024, time.Now())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uploaded.Status != spatial.ArtifactStatusUploaded {
		t.Fatalf("expected uploaded, got %s", uploaded.Status)
	}
	if uploaded.UploadTokenHash != "" {
		t.Fatalf("expected token cleared after upload, got %s", uploaded.UploadTokenHash)
	}

	// Idempotent re-call.
	uploaded2, err := repo.MarkUploaded(ctx, "company_a", created.ID, 1024, time.Now())
	if err != nil {
		t.Fatalf("unexpected error on repeat MarkUploaded: %v", err)
	}
	if uploaded2.Status != spatial.ArtifactStatusUploaded {
		t.Fatalf("expected still uploaded, got %s", uploaded2.Status)
	}
}

func TestMongoArtifactRepository_ListByCapture(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_2", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "def",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
	})

	list, err := repo.ListByCapture(ctx, "company_a", "capture_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact for capture_1, got %d", len(list))
	}
}

func TestMongoArtifactRepository_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoArtifactRepository(db)
	ctx := context.Background()

	repo.Create(ctx, spatial.SpatialArtifact{
		CompanyID: "company_a", CaptureID: "capture_1", Kind: spatial.ArtifactKindRGBKeyframe,
		ContentType: "image/jpeg", DeclaredSize: 1024, Checksum: "abc",
		Status: spatial.ArtifactStatusPending, CreatedAt: time.Now(), SchemaVersion: 1,
	})

	if err := repo.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, err := repo.ListByCapture(ctx, "company_a", "capture_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 artifacts after DeleteAllForCompany, got %d", len(list))
	}
}
