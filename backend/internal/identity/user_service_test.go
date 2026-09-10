package identity

import (
	"context"
	"testing"
)

type fakeUserRepository struct {
	byID    map[string]User
	byEmail map[string]User
	nextID  int
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{byID: map[string]User{}, byEmail: map[string]User{}}
}

func (f *fakeUserRepository) Create(_ context.Context, u User) (User, error) {
	if _, exists := f.byEmail[u.Email]; exists {
		return User{}, ErrDuplicateEmail
	}
	f.nextID++
	u.ID = string(rune('a' + f.nextID))
	f.byID[u.ID] = u
	f.byEmail[u.Email] = u
	return u, nil
}

func (f *fakeUserRepository) FindByEmail(_ context.Context, email string) (User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserRepository) FindByID(_ context.Context, id string) (User, error) {
	u, ok := f.byID[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func (f *fakeUserRepository) Delete(_ context.Context, id string) error {
	u, ok := f.byID[id]
	if !ok {
		return ErrUserNotFound
	}
	delete(f.byID, id)
	delete(f.byEmail, u.Email)
	return nil
}

func TestUserServiceCreateUserNormalizesEmailAndHashesPassword(t *testing.T) {
	svc := NewUserService(newFakeUserRepository())

	u, err := svc.CreateUser(context.Background(), "John@Example.com", "s3cret-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.Email != "john@example.com" {
		t.Fatalf("expected normalized email, got %s", u.Email)
	}
	if u.PasswordHash == "s3cret-password" {
		t.Fatal("password must be hashed, not stored plaintext")
	}
	if err := VerifyPassword(u.PasswordHash, "s3cret-password"); err != nil {
		t.Fatalf("expected stored hash to verify against original password: %v", err)
	}
}

func TestUserServiceFindUserByEmailNormalizes(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo)
	ctx := context.Background()

	_, err := svc.CreateUser(ctx, "Case@Example.com", "password123")
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := svc.FindUserByEmail(ctx, "case@EXAMPLE.com")
	if err != nil {
		t.Fatalf("expected to find user with differently-cased email: %v", err)
	}
	if found.Email != "case@example.com" {
		t.Fatalf("expected normalized stored email, got %s", found.Email)
	}
}

func TestUserServiceDeleteUser(t *testing.T) {
	repo := newFakeUserRepository()
	svc := NewUserService(repo)
	ctx := context.Background()

	u, err := svc.CreateUser(ctx, "todelete@example.com", "password123")
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := svc.DeleteUser(ctx, u.ID); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	_, err = svc.FindUserByID(ctx, u.ID)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}
}

func TestUserServiceFindOrCreateUserCreatesNewUserWithTempPassword(t *testing.T) {
	svc := NewUserService(newFakeUserRepository())
	ctx := context.Background()

	userID, tempPassword, created, err := svc.FindOrCreateUser(ctx, "newmember@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for a brand-new email")
	}
	if userID == "" {
		t.Fatal("expected non-empty userID")
	}
	if tempPassword == "" {
		t.Fatal("expected a non-empty generated temp password")
	}

	u, err := svc.FindUserByID(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error finding created user: %v", err)
	}
	if !u.MustChangePassword {
		t.Fatal("expected MustChangePassword=true for a temp-password-provisioned user")
	}
	if VerifyPassword(u.PasswordHash, tempPassword) != nil {
		t.Fatal("expected stored hash to verify against the returned temp password")
	}
}

func TestUserServiceFindOrCreateUserReturnsExistingUser(t *testing.T) {
	svc := NewUserService(newFakeUserRepository())
	ctx := context.Background()

	existing, err := svc.CreateUser(ctx, "existing@example.com", "realpassword")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	userID, tempPassword, created, err := svc.FindOrCreateUser(ctx, "existing@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Fatal("expected created=false for an existing email")
	}
	if userID != existing.ID {
		t.Fatalf("expected existing user's ID %s, got %s", existing.ID, userID)
	}
	if tempPassword != "" {
		t.Fatal("expected empty tempPassword when no new user was created")
	}
}

func TestUserServiceDeleteProvisionedUser(t *testing.T) {
	svc := NewUserService(newFakeUserRepository())
	ctx := context.Background()

	userID, _, _, err := svc.FindOrCreateUser(ctx, "toprovision@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.DeleteProvisionedUser(ctx, userID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.FindUserByID(ctx, userID)
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound after delete, got %v", err)
	}
}
