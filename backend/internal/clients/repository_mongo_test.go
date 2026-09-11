package clients_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
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

func TestClientRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, clients.Client{
		CompanyID: "company_a", Name: "Ahmad", Phone: "0123456789", CreatedAt: time.Now(), SchemaVersion: 1,
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
	if found.Name != "Ahmad" {
		t.Fatalf("expected Ahmad, got %s", found.Name)
	}
}

func TestClientRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_tenant")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Ahmad", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != clients.ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestClientRepositoryFindByIDNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_notfound")
	repo := clients.NewMongoClientRepository(db)

	_, err := repo.FindByID(context.Background(), "company_a", "000000000000000000000000")
	if err != clients.ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound, got %v", err)
	}
}

func defaultListRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

func TestClientRepositoryListPaginatedOnlyReturnsOwnCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_list")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "A1", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "A2", CreatedAt: time.Now(), SchemaVersion: 1})
	_, _ = repo.Create(ctx, clients.Client{CompanyID: "company_b", Name: "B1", CreatedAt: time.Now(), SchemaVersion: 1})

	listA, total, err := repo.ListPaginated(ctx, "company_a", defaultListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (must exclude company_b)", total)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 clients for company_a, got %d", len(listA))
	}
	for _, c := range listA {
		if c.CompanyID != "company_a" {
			t.Fatalf("expected only company_a clients, got %s", c.CompanyID)
		}
	}
}

func TestClientRepositoryListPaginatedOrdersByCreatedAtDescByDefault(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_order_default")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	first, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "First", CreatedAt: base, SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	second, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Second", CreatedAt: base.Add(time.Minute), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	list, _, err := repo.ListPaginated(ctx, "company_a", defaultListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(list))
	}
	if list[0].ID != second.ID || list[1].ID != first.ID {
		t.Fatalf("expected newest-first order [%s, %s], got [%s, %s]", second.ID, first.ID, list[0].ID, list[1].ID)
	}
}

func TestClientRepositoryListPaginatedSortsByNameAscending(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_order_name")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	zebra, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Zebra", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	apple, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Apple", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "", "name", "asc", clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, _, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 || list[0].ID != apple.ID || list[1].ID != zebra.ID {
		t.Fatalf("expected [Apple, Zebra] order, got %+v", list)
	}
}

func TestClientRepositoryListPaginatedStableOrderWhenPrimarySortValuesTie(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_stable_order")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	same := time.Now()
	var ids []string
	for i := 0; i < 5; i++ {
		created, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Same", CreatedAt: same, SchemaVersion: 1})
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
		ids = append(ids, created.ID)
	}

	req := defaultListRequest(t)
	firstRun, _, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	secondRun, _, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(firstRun) != len(secondRun) {
		t.Fatalf("result length changed between identical calls: %d vs %d", len(firstRun), len(secondRun))
	}
	for i := range firstRun {
		if firstRun[i].ID != secondRun[i].ID {
			t.Fatalf("order at index %d differs between identical calls with tied CreatedAt: %s vs %s",
				i, firstRun[i].ID, secondRun[i].ID)
		}
	}
}

func TestClientRepositoryListPaginatedSearchMatchesNameEmailPhone(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_search")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	if _, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Ahmad Rahman", Email: "ahmad@example.com", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Siti Aminah", Phone: "0198765432", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "aminah", "", "", clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len(list)=%d, want 1/1", total, len(list))
	}
	if list[0].Name != "Siti Aminah" {
		t.Fatalf("expected Siti Aminah (matched by name), got %s", list[0].Name)
	}
}

func TestClientRepositoryListPaginatedSearchCannotInjectRegex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_search_regex_safety")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	if _, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Anything", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A raw ".*" would match everything as a regex; as a literal search term
	// it must match nothing, proving metacharacters are escaped rather than
	// interpreted.
	req, err := pagination.ParseRequest(0, 0, ".*", "", "", clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, total, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 matches for the literal string \".*\", got %d — regex metacharacters were not escaped", total)
	}
}

func TestClientRepositoryListPaginatedEmptyPageAfterLastPage(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_beyond_last_page")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	if _, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Only", CreatedAt: time.Now(), SchemaVersion: 1}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(5, 25, "", "", "", clients.ClientSortFields, clients.ClientDefaultSort, clients.ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, total, err := repo.ListPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 items on a page beyond the last, got %d", len(list))
	}
}

func TestClientRepositoryUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_update")
	repo := clients.NewMongoClientRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, clients.Client{CompanyID: "company_a", Name: "Old Name", CreatedAt: time.Now(), SchemaVersion: 1})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(c *clients.Client) {
		c.Name = "New Name"
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}

	_, err = repo.Update(ctx, "company_b", created.ID, func(c *clients.Client) { c.Name = "Hijacked" })
	if err != clients.ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound for cross-tenant update, got %v", err)
	}
}

// TestService_DeleteAllForCompany and TestService_DeleteAllForCompany_EmptyCompanyIsANoOp
// prove Task 1a's demo-seeding-reset capability (spec §6.6): a company-scoped
// bulk delete, reachable only via Service (never a widened public
// ClientRepository interface — see the unexported companyBulkDeleter type
// assertion in service.go), safe to call repeatedly.
func TestService_DeleteAllForCompany(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_deleteall_service")
	repo := clients.NewMongoClientRepository(db)
	svc := clients.NewService(repo)
	ctx := context.Background()

	if _, err := svc.CreateClient(ctx, "company_a", "A1", "", "", "", "", ""); err != nil {
		t.Fatalf("create company_a client: %v", err)
	}
	if _, err := svc.CreateClient(ctx, "company_b", "B1", "", "", "", "", ""); err != nil {
		t.Fatalf("create company_b client: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	_, total, err := svc.ListClientsPaginated(ctx, "company_a", defaultListRequest(t))
	if err != nil {
		t.Fatalf("ListClientsPaginated company_a: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected 0 remaining company_a clients, got %d", total)
	}

	_, total, err = svc.ListClientsPaginated(ctx, "company_b", defaultListRequest(t))
	if err != nil {
		t.Fatalf("ListClientsPaginated company_b: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected company_b's client to be untouched, got %d", total)
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "clients_test_deleteall_empty")
	repo := clients.NewMongoClientRepository(db)
	svc := clients.NewService(repo)
	ctx := context.Background()

	// A company with NO records — proving DeleteAllForCompany is safe to
	// call repeatedly (resumability) rather than erroring on "nothing
	// matched," unlike the single-document Update/Delete-style methods
	// elsewhere.
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
