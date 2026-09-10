package clients

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

type fakeClientRepo struct {
	byID   map[string]Client
	nextID int
}

func newFakeClientRepo() *fakeClientRepo {
	return &fakeClientRepo{byID: map[string]Client{}}
}

func (f *fakeClientRepo) Create(_ context.Context, c Client) (Client, error) {
	f.nextID++
	c.ID = string(rune('a' + f.nextID))
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeClientRepo) FindByID(_ context.Context, companyID, id string) (Client, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return Client{}, ErrClientNotFound
	}
	return c, nil
}

func (f *fakeClientRepo) ListPaginated(_ context.Context, companyID string, req pagination.Request) ([]Client, int, error) {
	var matched []Client
	for _, c := range f.byID {
		if c.CompanyID != companyID {
			continue
		}
		if req.Search != "" {
			needle := strings.ToLower(req.Search)
			if !strings.Contains(strings.ToLower(c.Name), needle) &&
				!strings.Contains(strings.ToLower(c.Email), needle) &&
				!strings.Contains(strings.ToLower(c.Phone), needle) {
				continue
			}
		}
		matched = append(matched, c)
	}
	sort.Slice(matched, func(i, j int) bool {
		var less bool
		switch req.Sort {
		case "name":
			less = matched[i].Name < matched[j].Name
		default:
			less = matched[i].CreatedAt.Before(matched[j].CreatedAt)
		}
		if req.Order == pagination.OrderDesc {
			return !less
		}
		return less
	})
	total := len(matched)
	start := req.Offset()
	if start > total {
		start = total
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	return matched[start:end], total, nil
}

func (f *fakeClientRepo) Update(ctx context.Context, companyID, id string, fn func(*Client)) (Client, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return Client{}, err
	}
	fn(&c)
	f.byID[id] = c
	return c, nil
}

func TestServiceCreateClient(t *testing.T) {
	svc := NewService(newFakeClientRepo())

	c, err := svc.CreateClient(context.Background(), "company_a", "Ahmad", "0123456789", "ahmad@example.com", "123 Street", "123 Street", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.CompanyID != "company_a" {
		t.Fatalf("expected company_a, got %s", c.CompanyID)
	}
	if c.Name != "Ahmad" {
		t.Fatalf("expected Ahmad, got %s", c.Name)
	}
	if c.SchemaVersion != 1 {
		t.Fatalf("expected schemaVersion 1, got %d", c.SchemaVersion)
	}
}

func TestServiceCreateClientRejectsEmptyName(t *testing.T) {
	svc := NewService(newFakeClientRepo())

	_, err := svc.CreateClient(context.Background(), "company_a", "", "", "", "", "", "")
	if err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestServiceGetClientTenantScoped(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetClient(ctx, "company_b", created.ID)
	if err != ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound for cross-tenant get, got %v", err)
	}

	found, err := svc.GetClient(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("expected same client")
	}
}

func TestServiceClientBelongsToCompany(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.ClientBelongsToCompany(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true for correct company")
	}

	belongs, err = svc.ClientBelongsToCompany(ctx, "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for wrong company")
	}

	belongs, err = svc.ClientBelongsToCompany(ctx, "company_a", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if belongs {
		t.Fatal("expected false for nonexistent client")
	}
}

func strPtr(s string) *string { return &s }

func TestServiceUpdateClientChangesName(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Old Name", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{Name: strPtr("New Name")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Name != "New Name" {
		t.Fatalf("expected New Name, got %s", updated.Name)
	}
}

func TestServiceUpdateClientOmittedFieldsPreserved(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "0123456789", "ahmad@example.com", "123 Street", "Billing St", "Some notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only Phone is supplied — every other field must remain unchanged.
	updated, err := svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{Phone: strPtr("0199999999")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Phone != "0199999999" {
		t.Fatalf("Phone = %q, want 0199999999", updated.Phone)
	}
	if updated.Name != "Ahmad" {
		t.Fatalf("Name = %q, want unchanged Ahmad", updated.Name)
	}
	if updated.Email != "ahmad@example.com" {
		t.Fatalf("Email = %q, want unchanged", updated.Email)
	}
	if updated.Address != "123 Street" {
		t.Fatalf("Address = %q, want unchanged", updated.Address)
	}
	if updated.BillingAddress != "Billing St" {
		t.Fatalf("BillingAddress = %q, want unchanged", updated.BillingAddress)
	}
	if updated.Notes != "Some notes" {
		t.Fatalf("Notes = %q, want unchanged", updated.Notes)
	}
}

func TestServiceUpdateClientEmptyStringExplicitlyClearsField(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "", "", "", "", "Some notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{Notes: strPtr("")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Notes != "" {
		t.Fatalf("Notes = %q, want cleared to empty", updated.Notes)
	}
}

func TestServiceUpdateClientRejectsEmptyOrWhitespaceOnlyName(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{Name: strPtr("")}); err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired for empty name, got %v", err)
	}
	if _, err := svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{Name: strPtr("   ")}); err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired for whitespace-only name, got %v", err)
	}
}

func TestServiceUpdateClientForeignClientReturnsNotFound(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateClient(ctx, "company_b", created.ID, ClientPatch{Phone: strPtr("0199999999")}); err != ErrClientNotFound {
		t.Fatalf("expected ErrClientNotFound for cross-tenant update, got %v", err)
	}
}

func TestServiceUpdateClientNoPartialPersistenceOnValidationFailure(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()

	created, err := svc.CreateClient(ctx, "company_a", "Ahmad", "0123456789", "", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Phone is valid and would apply, but Name is invalid — the whole update
	// must be rejected, not partially applied.
	_, err = svc.UpdateClient(ctx, "company_a", created.ID, ClientPatch{
		Name: strPtr(""), Phone: strPtr("0199999999"),
	})
	if err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}

	unchanged, err := svc.GetClient(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unchanged.Phone != "0123456789" {
		t.Fatalf("Phone = %q, want unchanged 0123456789 (no partial persistence)", unchanged.Phone)
	}
}

func defaultClientsPaginationRequest(t *testing.T) pagination.Request {
	t.Helper()
	req, err := pagination.ParseRequest(0, 0, "", "", "", ClientSortFields, ClientDefaultSort, ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error building default pagination request: %v", err)
	}
	return req
}

func TestServiceListClientsPaginatedDefaultPage(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := svc.CreateClient(ctx, "company_a", "Client", "", "", "", "", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	items, total, err := svc.ListClientsPaginated(ctx, "company_a", defaultClientsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(items) != 3 {
		t.Errorf("len(items) = %d, want 3", len(items))
	}
}

func TestServiceListClientsPaginatedExplicitPageAndSize(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := svc.CreateClient(ctx, "company_a", "Client", "", "", "", "", ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	req, err := pagination.ParseRequest(2, 2, "", "", "", ClientSortFields, ClientDefaultSort, ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, total, err := svc.ListClientsPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(items))
	}
}

func TestServiceListClientsPaginatedEmptyPageAfterLastPage(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()
	if _, err := svc.CreateClient(ctx, "company_a", "Client", "", "", "", "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(5, 25, "", "", "", ClientSortFields, ClientDefaultSort, ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, total, err := svc.ListClientsPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0 for a page beyond the last", len(items))
	}
}

func TestServiceListClientsPaginatedSearchMatchesNameEmailPhone(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()
	if _, err := svc.CreateClient(ctx, "company_a", "Ahmad Rahman", "0123456789", "ahmad@example.com", "", "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateClient(ctx, "company_a", "Siti Aminah", "0198765432", "siti@example.com", "", "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	req, err := pagination.ParseRequest(0, 0, "ahmad", "", "", ClientSortFields, ClientDefaultSort, ClientDefaultOrder)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items, total, err := svc.ListClientsPaginated(ctx, "company_a", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len(items)=%d, want 1/1", total, len(items))
	}
	if items[0].Name != "Ahmad Rahman" {
		t.Errorf("Name = %q, want Ahmad Rahman", items[0].Name)
	}
}

func TestServiceListClientsPaginatedTenantIsolation(t *testing.T) {
	svc := NewService(newFakeClientRepo())
	ctx := context.Background()
	if _, err := svc.CreateClient(ctx, "company_a", "A Client", "", "", "", "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.CreateClient(ctx, "company_b", "B Client", "", "", "", "", ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	items, total, err := svc.ListClientsPaginated(ctx, "company_a", defaultClientsPaginationRequest(t))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("total=%d len(items)=%d, want 1/1 (must exclude company_b)", total, len(items))
	}
	if items[0].CompanyID != "company_a" {
		t.Errorf("CompanyID = %q, want company_a", items[0].CompanyID)
	}
}

func TestServiceListClientsPaginatedRejectsUnsupportedSort(t *testing.T) {
	if _, err := pagination.ParseRequest(0, 0, "", "phone", "", ClientSortFields, ClientDefaultSort, ClientDefaultOrder); err == nil {
		t.Fatal("expected an error for an unsupported sort field (phone is searchable but not sortable)")
	}
}

var _ = time.Now
