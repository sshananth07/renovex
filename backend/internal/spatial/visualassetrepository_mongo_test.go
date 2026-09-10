package spatial_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

func TestMongoVisualAssetVersionRepository_CreateAndFind(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, DisplayName: "Boiler v1",
		Normalization: spatial.VisualAssetNormalization{Pivot: spatial.VisualAssetPivotCenterBottom},
		ContentType:   "model/gltf-binary",
		ByteCount:     1234,
		Checksum:      "abc123",
		StorageKey:    "visual-assets/company_a/boiler-asset/v1/abc123.glb",
		CreatedAt:     time.Now(),
		SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected an ID to be assigned")
	}

	found, err := repo.FindByCompanyAssetVersion(ctx, "company_a", "boiler-asset", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Checksum != "abc123" || found.StorageKey != "visual-assets/company_a/boiler-asset/v1/abc123.glb" {
		t.Fatalf("expected found record to match created, got %+v", found)
	}
}

func TestMongoVisualAssetVersionRepository_PublishV2LeavesV1Unchanged(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()

	_, err := repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-v1", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	_, err = repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 2,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-v2", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}

	v1, err := repo.FindByCompanyAssetVersion(ctx, "company_a", "boiler-asset", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1.Checksum != "checksum-v1" {
		t.Fatalf("expected v1 unchanged after v2 published, got %+v", v1)
	}
}

func TestMongoVisualAssetVersionRepository_DuplicatePublish_IdenticalMetadataAdopts(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	version := spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-v1", ByteCount: 100, CreatedAt: time.Now(), SchemaVersion: 1,
	}
	first, err := repo.Create(ctx, version)
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	second, err := repo.Create(ctx, version)
	if err != nil {
		t.Fatalf("expected idempotent adoption, got error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the SAME existing record adopted, got a different ID")
	}
}

func TestMongoVisualAssetVersionRepository_DuplicatePublish_ConflictingMetadataRejected(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-v1", ByteCount: 100, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	_, err = repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-DIFFERENT", ByteCount: 999, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if !errors.Is(err, spatial.ErrVisualAssetVersionConflict) {
		t.Fatalf("expected ErrVisualAssetVersionConflict, got %v", err)
	}
}

func TestMongoVisualAssetVersionRepository_CrossCompanyIsolation(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()

	_, err := repo.Create(ctx, spatial.VisualAssetVersion{
		CompanyID: "company_a", AssetID: "boiler-asset", Version: 1,
		Format: spatial.VisualAssetFormatGLB, Checksum: "checksum-v1", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.FindByCompanyAssetVersion(ctx, "company_b", "boiler-asset", 1)
	if !errors.Is(err, spatial.ErrVisualAssetVersionNotFound) {
		t.Fatalf("expected ErrVisualAssetVersionNotFound for a different company, got %v", err)
	}
}

func TestMongoVisualAssetVersionRepository_FindByCompanyAssetVersion_NotFound(t *testing.T) {
	db := setupDB(t)
	repo := spatial.NewMongoVisualAssetVersionRepository(db)
	ctx := context.Background()

	_, err := repo.FindByCompanyAssetVersion(ctx, "company_a", "nonexistent", 1)
	if !errors.Is(err, spatial.ErrVisualAssetVersionNotFound) {
		t.Fatalf("expected ErrVisualAssetVersionNotFound, got %v", err)
	}
}
