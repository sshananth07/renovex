package companies_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// TestCompanyRepository_FindByName proves Task 1a Step 7's addition, needed
// by demoseed's positive-identification checks (design spec §6.2).
func TestCompanyRepository_FindByName(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_findbyname")
	repo := companies.NewMongoCompanyRepository(db)
	ctx := context.Background()

	created, err := repo.Create(ctx, companies.Company{
		Name: "Acme Renovations", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	found, err := repo.FindByName(ctx, "Acme Renovations")
	if err != nil {
		t.Fatalf("FindByName: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected to find the same company, got ID %s want %s", found.ID, created.ID)
	}

	if _, err := repo.FindByName(ctx, "Nonexistent Company"); err != companies.ErrCompanyNotFound {
		t.Fatalf("expected ErrCompanyNotFound for a nonexistent name, got %v", err)
	}
}
