package projects_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/projects"
)

func defaultProjectListRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", projects.ProjectSortFields, projects.ProjectDefaultSort, projects.ProjectDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

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

func TestProjectRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Name != "Ahmad Residence" {
		t.Fatalf("expected Ahmad Residence, got %s", found.Name)
	}
}

func TestProjectRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_tenant")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != projects.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestProjectRepositoryListPaginatedByClientOnlyReturnsOwnClient(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_list_by_client")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P2", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_2", Name: "P3", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})

	list, total, err := repo.ListPaginated(ctx, "company_a", "client_1", defaultProjectListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 projects for client_1, got %d", len(list))
	}
	for _, p := range list {
		if p.ClientID != "client_1" {
			t.Fatalf("expected only client_1 projects, got %s", p.ClientID)
		}
	}
}

func TestProjectRepositoryListPaginatedWithoutClientFilterIncludesAll(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_list_all_clients")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_2", Name: "P2", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})

	list, total, err := repo.ListPaginated(ctx, "company_a", "", defaultProjectListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("total=%d len(list)=%d, want 2/2", total, len(list))
	}
}

func TestProjectRepositoryCreateDefaultsScopeBriefEmpty(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_scope_brief_default")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ScopeBrief != "" {
		t.Fatalf("expected default ScopeBrief empty, got %q", created.ScopeBrief)
	}
}

func TestProjectRepositoryUpdateScopeBriefRoundTrips(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_scope_brief_roundtrip")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	brief := "Full renovation of a 3-bedroom condominium."
	updated, err := repo.UpdateScopeBrief(ctx, "company_a", created.ID, brief)
	if err != nil {
		t.Fatalf("unexpected error updating scope brief: %v", err)
	}
	if updated.ScopeBrief != brief {
		t.Fatalf("ScopeBrief = %q, want %q", updated.ScopeBrief, brief)
	}
	if updated.Name != "Ahmad Residence" {
		t.Fatalf("updating scope brief must not change Name, got %q", updated.Name)
	}
	if updated.Status != projects.ProjectStatusLead {
		t.Fatalf("updating scope brief must not change Status, got %q", updated.Status)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.ScopeBrief != brief {
		t.Fatalf("persisted ScopeBrief = %q, want %q", found.ScopeBrief, brief)
	}
}

func TestProjectRepositoryUpdateScopeBriefClearingAllowed(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_scope_brief_clear")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if _, err := repo.UpdateScopeBrief(ctx, "company_a", created.ID, "Some brief"); err != nil {
		t.Fatalf("unexpected error setting scope brief: %v", err)
	}

	cleared, err := repo.UpdateScopeBrief(ctx, "company_a", created.ID, "")
	if err != nil {
		t.Fatalf("unexpected error clearing scope brief: %v", err)
	}
	if cleared.ScopeBrief != "" {
		t.Fatalf("expected cleared ScopeBrief, got %q", cleared.ScopeBrief)
	}
}

func TestProjectRepositoryUpdateScopeBriefCrossTenantNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_scope_brief_tenant")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{
		CompanyID: "company_a", ClientID: "client_1", Name: "Ahmad Residence",
		Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.UpdateScopeBrief(ctx, "company_b", created.ID, "Malicious update")
	if err != projects.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant update, got %v", err)
	}
}

func TestProjectRepositoryListPaginatedTenantIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_tenant_isolation")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_b", ClientID: "client_2", Name: "P2", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})

	list, total, err := repo.ListPaginated(ctx, "company_a", "", defaultProjectListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1 (must exclude company_b)", total, len(list))
	}
}

func TestProjectRepositoryListPaginatedSortsByStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_sort_status")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	closedProject, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "Closed", Status: projects.ProjectStatusClosed, CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	leadProject, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "Lead", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "", "status", "asc", projects.ProjectSortFields, projects.ProjectDefaultSort, projects.ProjectDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, _, err := repo.ListPaginated(ctx, "company_a", "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "closed" < "lead" lexicographically, so ascending sort-by-status puts
	// Closed first.
	if len(list) != 2 || list[0].ID != closedProject.ID || list[1].ID != leadProject.ID {
		t.Fatalf("expected [Closed, Lead] order, got %+v", list)
	}
}

func TestProjectRepositoryListPaginatedSearchMatchesName(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_search")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "Bathroom Renovation", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "Kitchen Remodel", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})

	req, err := pagination.ParseRequest(0, 0, "kitchen", "", "", projects.ProjectSortFields, projects.ProjectDefaultSort, projects.ProjectDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", "", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Name != "Kitchen Remodel" {
		t.Fatalf("total=%d list=%+v, want 1 match on Kitchen Remodel", total, list)
	}
}

func TestProjectRepositoryListPaginatedFilterAndCountUseIdenticalPredicate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_filter_count_parity")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	req, err := pagination.ParseRequest(1, 2, "", "", "", projects.ProjectSortFields, projects.ProjectDefaultSort, projects.ProjectDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", "client_1", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (independent of page size)", total)
	}
	if len(list) != 2 {
		t.Fatalf("len(list) = %d, want 2 (bounded by pageSize)", len(list))
	}
}

func TestProjectRepositoryUpdateStatus(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_status")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "P1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.UpdateStatus(ctx, "company_a", created.ID, projects.ProjectStatusInProgress)
	if err != nil {
		t.Fatalf("unexpected error updating status: %v", err)
	}
	if updated.Status != projects.ProjectStatusInProgress {
		t.Fatalf("expected in_progress, got %s", updated.Status)
	}

	_, err = repo.UpdateStatus(ctx, "company_b", created.ID, projects.ProjectStatusClosed)
	if err != projects.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant update, got %v", err)
	}
}

func TestProjectRepositoryUpdateName(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_name")
	repo := projects.NewMongoProjectRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "Old Name", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.UpdateName(ctx, "company_a", created.ID, "New Name")
	if err != nil {
		t.Fatalf("unexpected error updating name: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}
	if updated.ClientID != "client_1" {
		t.Fatalf("expected ClientID unchanged, got %s", updated.ClientID)
	}
	if updated.Status != projects.ProjectStatusLead {
		t.Fatalf("expected Status unchanged, got %s", updated.Status)
	}

	_, err = repo.UpdateName(ctx, "company_b", created.ID, "Hijacked Name")
	if err != projects.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for cross-tenant update, got %v", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's demo-seeding-reset
// capability (spec §6.6): a company-scoped bulk delete, reachable only via
// Service, safe to call repeatedly. Seeds via the repository directly
// (matching every sibling test in this file) since CreateProject would
// otherwise require a real Client for clientLookup.ClientBelongsToCompany.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_deleteall_service")
	repo := projects.NewMongoProjectRepository(db)
	svc := projects.NewService(repo, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, projects.Project{CompanyID: "company_a", ClientID: "client_1", Name: "A1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_a project: %v", err)
	}
	if _, err := repo.Create(ctx, projects.Project{CompanyID: "company_b", ClientID: "client_2", Name: "B1", Status: projects.ProjectStatusLead, CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("create company_b project: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	_, total, err := repo.ListPaginated(ctx, "company_a", "", defaultProjectListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_a: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 remaining company_a projects, got %d", total)
	}

	_, total, err = repo.ListPaginated(ctx, "company_b", "", defaultProjectListRequest(t))
	if err != nil {
		t.Fatalf("ListPaginated company_b: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected company_b's project to be untouched, got %d", total)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "projects_test_deleteall_empty")
	repo := projects.NewMongoProjectRepository(db)
	svc := projects.NewService(repo, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
