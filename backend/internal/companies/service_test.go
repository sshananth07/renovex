package companies

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

type fakeCompanyRepo struct {
	byID   map[string]Company
	nextID int
}

func newFakeCompanyRepo() *fakeCompanyRepo {
	return &fakeCompanyRepo{byID: map[string]Company{}}
}

func (f *fakeCompanyRepo) Create(_ context.Context, c Company) (Company, error) {
	f.nextID++
	c.ID = string(rune('A' + f.nextID))
	f.byID[c.ID] = c
	return c, nil
}
func (f *fakeCompanyRepo) FindByID(_ context.Context, id string) (Company, error) {
	c, ok := f.byID[id]
	if !ok {
		return Company{}, ErrCompanyNotFound
	}
	return c, nil
}
func (f *fakeCompanyRepo) Delete(_ context.Context, id string) error {
	if _, ok := f.byID[id]; !ok {
		return ErrCompanyNotFound
	}
	delete(f.byID, id)
	return nil
}
func (f *fakeCompanyRepo) FindByName(_ context.Context, name string) (Company, error) {
	for _, c := range f.byID {
		if c.Name == name {
			return c, nil
		}
	}
	return Company{}, ErrCompanyNotFound
}

type fakeMembershipRepo struct {
	byUserID  map[string]CompanyMembership
	byCompany map[string][]CompanyMembership
	nextID    int
}

func newFakeMembershipRepo() *fakeMembershipRepo {
	return &fakeMembershipRepo{byUserID: map[string]CompanyMembership{}, byCompany: map[string][]CompanyMembership{}}
}

func (f *fakeMembershipRepo) Create(_ context.Context, m CompanyMembership) (CompanyMembership, error) {
	if _, exists := f.byUserID[m.UserID]; exists {
		return CompanyMembership{}, ErrUserAlreadyHasMembership
	}
	f.nextID++
	m.ID = string(rune('a' + f.nextID))
	f.byUserID[m.UserID] = m
	f.byCompany[m.CompanyID] = append(f.byCompany[m.CompanyID], m)
	return m, nil
}
func (f *fakeMembershipRepo) FindByUserID(_ context.Context, userID string) (CompanyMembership, error) {
	m, ok := f.byUserID[userID]
	if !ok {
		return CompanyMembership{}, ErrMembershipNotFound
	}
	return m, nil
}
func (f *fakeMembershipRepo) ListPaginated(_ context.Context, companyID string, req pagination.Request) ([]CompanyMembership, int, error) {
	matched := make([]CompanyMembership, 0, len(f.byCompany[companyID]))
	for _, m := range f.byCompany[companyID] {
		if req.Search != "" {
			needle := strings.ToLower(req.Search)
			if !strings.Contains(strings.ToLower(m.UserID), needle) &&
				!strings.Contains(strings.ToLower(string(m.Role)), needle) {
				continue
			}
		}
		matched = append(matched, m)
	}
	sort.Slice(matched, func(i, j int) bool {
		var less bool
		switch req.Sort {
		case "role":
			less = matched[i].Role < matched[j].Role
		case "userId":
			less = matched[i].UserID < matched[j].UserID
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
func (f *fakeMembershipRepo) DeleteByUserID(_ context.Context, userID string) error {
	m, ok := f.byUserID[userID]
	if !ok {
		return ErrMembershipNotFound
	}
	delete(f.byUserID, userID)
	list := f.byCompany[m.CompanyID]
	for i, existing := range list {
		if existing.UserID == userID {
			f.byCompany[m.CompanyID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	return nil
}

type fakeUserProvisioner struct {
	users        map[string]string // email -> userID
	deletedUsers map[string]bool
}

func newFakeUserProvisioner() *fakeUserProvisioner {
	return &fakeUserProvisioner{users: map[string]string{}, deletedUsers: map[string]bool{}}
}

func (f *fakeUserProvisioner) FindOrCreateUser(_ context.Context, email string) (userID, tempPassword string, created bool, err error) {
	if id, ok := f.users[email]; ok {
		return id, "", false, nil
	}
	newID := "user_" + email
	f.users[email] = newID
	return newID, "generated-temp-password", true, nil
}

func (f *fakeUserProvisioner) DeleteProvisionedUser(_ context.Context, userID string) error {
	f.deletedUsers[userID] = true
	return nil
}

type fakeMailer struct {
	sentTo []string
}

func (f *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	f.sentTo = append(f.sentTo, msg.To)
	return nil
}

func TestServiceCreateOwnerCompany(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})

	companyID, err := svc.CreateOwnerCompany(context.Background(), "user_1", "Acme Renovations")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if companyID == "" {
		t.Fatal("expected a company ID")
	}

	membership, err := svc.membershipRepo.FindByUserID(context.Background(), "user_1")
	if err != nil {
		t.Fatalf("unexpected error finding membership: %v", err)
	}
	if membership.Role != RoleOwner {
		t.Fatalf("expected RoleOwner, got %s", membership.Role)
	}
}

func TestServiceFindCompanyByName(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "user_1", "Acme Renovations")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found, err := svc.FindCompanyByName(ctx, "Acme Renovations")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.ID != companyID {
		t.Fatalf("expected ID %s, got %s", companyID, found.ID)
	}

	if _, err := svc.FindCompanyByName(ctx, "Nonexistent"); err != ErrCompanyNotFound {
		t.Fatalf("expected ErrCompanyNotFound, got %v", err)
	}
}

func TestServiceDeleteProvisionedCompanyRemovesMembershipAndCompany(t *testing.T) {
	companyRepo := newFakeCompanyRepo()
	memberRepo := newFakeMembershipRepo()
	svc := NewService(companyRepo, memberRepo, newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "user_1", "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.DeleteProvisionedCompany(ctx, companyID, "user_1"); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	if _, err := memberRepo.FindByUserID(ctx, "user_1"); err != ErrMembershipNotFound {
		t.Fatalf("expected membership deleted, got %v", err)
	}
	if _, err := companyRepo.FindByID(ctx, companyID); err != ErrCompanyNotFound {
		t.Fatalf("expected company deleted, got %v", err)
	}
}

func TestServiceFindMembershipByUserID(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "user_1", "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotCompanyID, gotRole, err := svc.FindMembershipByUserID(ctx, "user_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCompanyID != companyID {
		t.Fatalf("expected companyID %s, got %s", companyID, gotCompanyID)
	}
	if gotRole != string(RoleOwner) {
		t.Fatalf("expected role string %q, got %q", RoleOwner, gotRole)
	}
}

func TestServiceAddMemberOwnerCanAdd(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "owner_1", "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	principal := Principal{UserID: "owner_1", CompanyID: companyID, Role: string(RoleOwner)}

	err = svc.AddMember(ctx, principal, "employee@example.com", RoleEmployee)
	if err != nil {
		t.Fatalf("unexpected error adding member: %v", err)
	}
}

func TestServiceAddMemberEmployeeForbidden(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "owner_1", "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	principal := Principal{UserID: "employee_1", CompanyID: companyID, Role: string(RoleEmployee)}

	err = svc.AddMember(ctx, principal, "new@example.com", RoleEmployee)
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestServiceAddMemberRejectsOwnerRole(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})
	ctx := context.Background()

	companyID, err := svc.CreateOwnerCompany(ctx, "owner_1", "Acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	principal := Principal{UserID: "owner_1", CompanyID: companyID, Role: string(RoleOwner)}

	err = svc.AddMember(ctx, principal, "new@example.com", RoleOwner)
	if err != ErrInvalidRole {
		t.Fatalf("expected ErrInvalidRole, got %v", err)
	}
}

func TestServiceAddMemberRejectsUserWithExistingMembership(t *testing.T) {
	provisioner := newFakeUserProvisioner()
	memberRepo := newFakeMembershipRepo()
	svc := NewService(newFakeCompanyRepo(), memberRepo, provisioner, &fakeMailer{})
	ctx := context.Background()

	companyAID, err := svc.CreateOwnerCompany(ctx, "owner_A", "Company A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	principalA := Principal{UserID: "owner_A", CompanyID: companyAID, Role: string(RoleOwner)}

	// existing_user already has a membership at Company A (owner_A does).
	// Simulate a second company trying to add owner_A.
	companyBID, err := svc.CreateOwnerCompany(ctx, "owner_B", "Company B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	principalB := Principal{UserID: "owner_B", CompanyID: companyBID, Role: string(RoleOwner)}
	_ = principalA

	provisioner.users["owner_A@example.com"] = "owner_A"
	err = svc.AddMember(ctx, principalB, "owner_A@example.com", RoleEmployee)
	if err != ErrUserAlreadyHasMembership {
		t.Fatalf("expected ErrUserAlreadyHasMembership, got %v", err)
	}
}

// --- Milestone 6: narrow company-name capability ---

func TestGetCompanyNameReturnsBareName(t *testing.T) {
	companyRepo := newFakeCompanyRepo()
	svc := NewService(companyRepo, newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})

	created, err := companyRepo.Create(context.Background(), Company{Name: "Renovate Sdn Bhd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	name, err := svc.GetCompanyName(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "Renovate Sdn Bhd" {
		t.Fatalf("expected the bare company name, got %q", name)
	}
}

func TestGetCompanyNameUnknownCompany(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})

	if _, err := svc.GetCompanyName(context.Background(), "missing"); err == nil {
		t.Fatal("expected an error for an unknown company")
	}
}

// --- GET /auth/me: narrow current-membership capability ---

func TestLookupCurrentMembershipReturnsCompanyIDNameAndRole(t *testing.T) {
	companyRepo := newFakeCompanyRepo()
	membershipRepo := newFakeMembershipRepo()
	svc := NewService(companyRepo, membershipRepo, newFakeUserProvisioner(), &fakeMailer{})

	company, err := companyRepo.Create(context.Background(), Company{Name: "Acme Renovations"})
	if err != nil {
		t.Fatalf("seed company: %v", err)
	}
	if _, err := membershipRepo.Create(context.Background(), CompanyMembership{
		UserID: "user-1", CompanyID: company.ID, Role: RoleOwner,
	}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	companyID, companyName, role, err := svc.LookupCurrentMembership(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if companyID != company.ID {
		t.Errorf("companyID = %q, want %q", companyID, company.ID)
	}
	if companyName != "Acme Renovations" {
		t.Errorf("companyName = %q", companyName)
	}
	if role != string(RoleOwner) {
		t.Errorf("role = %q, want %q", role, RoleOwner)
	}
}

func TestLookupCurrentMembershipErrorsWhenUserHasNoMembership(t *testing.T) {
	svc := NewService(newFakeCompanyRepo(), newFakeMembershipRepo(), newFakeUserProvisioner(), &fakeMailer{})

	if _, _, _, err := svc.LookupCurrentMembership(context.Background(), "no-such-user"); err == nil {
		t.Fatal("expected an error when the user has no membership")
	}
}

func TestLookupCurrentMembershipErrorsWhenCompanyMissingDespiteMembership(t *testing.T) {
	// Defensive case: a Membership referencing a Company that no longer
	// exists (should not happen given companies.Service's own compensation
	// logic, but LookupCurrentMembership must not silently fabricate a name).
	membershipRepo := newFakeMembershipRepo()
	svc := NewService(newFakeCompanyRepo(), membershipRepo, newFakeUserProvisioner(), &fakeMailer{})

	if _, err := membershipRepo.Create(context.Background(), CompanyMembership{
		UserID: "user-1", CompanyID: "orphaned-company", Role: RoleOwner,
	}); err != nil {
		t.Fatalf("seed membership: %v", err)
	}

	if _, _, _, err := svc.LookupCurrentMembership(context.Background(), "user-1"); err == nil {
		t.Fatal("expected an error when the membership's company cannot be found")
	}
}
