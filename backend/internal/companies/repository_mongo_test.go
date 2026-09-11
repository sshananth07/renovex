package companies_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func defaultMemberListRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", companies.MemberSortFields, companies.MemberDefaultSort, companies.MemberDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default request: %v", err)
	}
	return req
}

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

func TestCompanyRepositoryCreateFindDelete(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test")
	repo := companies.NewMongoCompanyRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, companies.Company{Name: "Acme Renovations", CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Name != "Acme Renovations" {
		t.Fatalf("expected Acme Renovations, got %s", found.Name)
	}

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}
	_, err = repo.FindByID(ctx, created.ID)
	if err != companies.ErrCompanyNotFound {
		t.Fatalf("expected ErrCompanyNotFound after delete, got %v", err)
	}
}

func TestMembershipRepositoryCreateEnforcesOneMembershipPerUser(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_membership")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	if err := memberRepo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	companyA, _ := companyRepo.Create(ctx, companies.Company{Name: "Company A", CreatedAt: time.Now()})
	companyB, _ := companyRepo.Create(ctx, companies.Company{Name: "Company B", CreatedAt: time.Now()})

	_, err := memberRepo.Create(ctx, companies.CompanyMembership{
		UserID: "user_1", CompanyID: companyA.ID, Role: companies.RoleOwner, CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error creating first membership: %v", err)
	}

	_, err = memberRepo.Create(ctx, companies.CompanyMembership{
		UserID: "user_1", CompanyID: companyB.ID, Role: companies.RoleEmployee, CreatedAt: time.Now(),
	})
	if err != companies.ErrUserAlreadyHasMembership {
		t.Fatalf("expected ErrUserAlreadyHasMembership, got %v", err)
	}
}

func TestMembershipRepositoryListPaginatedTenantIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_list")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	companyA, _ := companyRepo.Create(ctx, companies.Company{Name: "Company A", CreatedAt: time.Now()})
	companyB, _ := companyRepo.Create(ctx, companies.Company{Name: "Company B", CreatedAt: time.Now()})

	_, _ = memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u1", CompanyID: companyA.ID, Role: companies.RoleOwner, CreatedAt: time.Now()})
	_, _ = memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u2", CompanyID: companyA.ID, Role: companies.RoleEmployee, CreatedAt: time.Now()})
	_, _ = memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u3", CompanyID: companyB.ID, Role: companies.RoleOwner, CreatedAt: time.Now()})

	membersA, total, err := memberRepo.ListPaginated(ctx, companyA.ID, defaultMemberListRequest(t))
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (must exclude Company B)", total)
	}
	if len(membersA) != 2 {
		t.Fatalf("expected 2 members for Company A, got %d", len(membersA))
	}
	for _, m := range membersA {
		if m.CompanyID != companyA.ID {
			t.Fatalf("expected only Company A members, got membership for %s", m.CompanyID)
		}
	}
}

func TestMembershipRepositoryListPaginatedSortsByRole(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_sort_role")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	company, _ := companyRepo.Create(ctx, companies.Company{Name: "Company", CreatedAt: time.Now()})
	owner, err := memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u_owner", CompanyID: company.ID, Role: companies.RoleOwner, CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	employee, err := memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u_employee", CompanyID: company.ID, Role: companies.RoleEmployee, CreatedAt: time.Now()})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "", "role", "asc", companies.MemberSortFields, companies.MemberDefaultSort, companies.MemberDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	members, _, err := memberRepo.ListPaginated(ctx, company.ID, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "employee" < "owner" lexicographically.
	if len(members) != 2 || members[0].ID != employee.ID || members[1].ID != owner.ID {
		t.Fatalf("expected [employee, owner] order, got %+v", members)
	}
}

func TestMembershipRepositoryListPaginatedSearchMatchesUserIDAndRole(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_search")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	company, _ := companyRepo.Create(ctx, companies.Company{Name: "Company", CreatedAt: time.Now()})
	if _, err := memberRepo.Create(ctx, companies.CompanyMembership{UserID: "unique-user-id-123", CompanyID: company.ID, Role: companies.RoleOwner, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := memberRepo.Create(ctx, companies.CompanyMembership{UserID: "other-user", CompanyID: company.ID, Role: companies.RoleEmployee, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "unique-user-id", "", "", companies.MemberSortFields, companies.MemberDefaultSort, companies.MemberDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	members, total, err := memberRepo.ListPaginated(ctx, company.ID, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(members) != 1 || members[0].UserID != "unique-user-id-123" {
		t.Fatalf("total=%d members=%+v, want 1 match on unique-user-id-123", total, members)
	}
}

func TestMembershipRepositoryListPaginatedEmptyPageAfterLastPage(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_beyond_last_page")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	company, _ := companyRepo.Create(ctx, companies.Company{Name: "Company", CreatedAt: time.Now()})
	if _, err := memberRepo.Create(ctx, companies.CompanyMembership{UserID: "only-member", CompanyID: company.ID, Role: companies.RoleOwner, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(5, 25, "", "", "", companies.MemberSortFields, companies.MemberDefaultSort, companies.MemberDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	members, total, err := memberRepo.ListPaginated(ctx, company.ID, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1", total)
	}
	if len(members) != 0 {
		t.Fatalf("expected 0 members on a page beyond the last, got %d", len(members))
	}
}

func TestMembershipRepositoryDeleteByUserID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "companies_test_delete")
	companyRepo := companies.NewMongoCompanyRepository(db)
	memberRepo := companies.NewMongoMembershipRepository(db)

	ctx := context.Background()
	company, _ := companyRepo.Create(ctx, companies.Company{Name: "Company A", CreatedAt: time.Now()})
	_, _ = memberRepo.Create(ctx, companies.CompanyMembership{UserID: "u1", CompanyID: company.ID, Role: companies.RoleOwner, CreatedAt: time.Now()})

	if err := memberRepo.DeleteByUserID(ctx, "u1"); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	_, err := memberRepo.FindByUserID(ctx, "u1")
	if err != companies.ErrMembershipNotFound {
		t.Fatalf("expected ErrMembershipNotFound after delete, got %v", err)
	}
}
