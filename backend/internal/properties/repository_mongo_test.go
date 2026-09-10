package properties_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/properties"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func TestPropertyRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, properties.Property{
		CompanyID: "company_a", ProjectID: "project_1", Address: "123 Street",
		PropertyType: "residential", CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Address != "123 Street" {
		t.Fatalf("expected 123 Street, got %s", found.Address)
	}
}

func TestPropertyRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_tenant")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "123 Street", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != properties.ErrPropertyNotFound {
		t.Fatalf("expected ErrPropertyNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestPropertyRepositoryCreateSecondForSameProjectRejected(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_unique")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "First", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error on first create: %v", err)
	}

	_, err = repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "Second", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != properties.ErrProjectAlreadyHasProperty {
		t.Fatalf("expected ErrProjectAlreadyHasProperty, got %v", err)
	}
}

func TestPropertyRepositorySameProjectIDDifferentCompanyAllowed(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_unique_cross_company")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	// Same projectID value under two different companies must not collide — the
	// unique index is {companyId, projectId} compound, not projectId alone.
	_, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_shared_id", Address: "A", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating for company_a: %v", err)
	}
	_, err = repo.Create(ctx, properties.Property{CompanyID: "company_b", ProjectID: "project_shared_id", Address: "B", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating for company_b (should not collide with company_a): %v", err)
	}
}

func TestPropertyRepositoryListByProjectValidatesOwnProjectOnly(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_list")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	_, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "A", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	list, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 property, got %d", len(list))
	}

	listWrongCompany, err := repo.ListByProject(ctx, "company_b", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(listWrongCompany) != 0 {
		t.Fatalf("expected 0 properties for company_b, got %d", len(listWrongCompany))
	}
}

func TestPropertyRepositoryUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_update")
	repo := properties.NewMongoPropertyRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "Old Address", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(p *properties.Property) {
		p.Address = "New Address"
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.Address != "New Address" {
		t.Fatalf("expected New Address, got %s", updated.Address)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6). Seeds via the repository directly (matching
// every sibling test in this file) since CreateProperty would otherwise
// require a real Project for projectLookup.ProjectBelongsToCompany.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_deleteall_service")
	repo := properties.NewMongoPropertyRepository(db)
	svc := properties.NewService(repo, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, properties.Property{CompanyID: "company_a", ProjectID: "project_1", Address: "A1", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_a property: %v", err)
	}
	if _, err := repo.Create(ctx, properties.Property{CompanyID: "company_b", ProjectID: "project_2", Address: "B1", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_b property: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a properties, got %d", len(listA))
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's property to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "properties_test_deleteall_empty")
	repo := properties.NewMongoPropertyRepository(db)
	svc := properties.NewService(repo, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
