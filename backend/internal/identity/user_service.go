package identity

import (
	"context"
	"time"
)

// UserService owns user-related capabilities: creation, lookup, and deletion
// (deletion used only for registration/add-member compensation). It has no
// knowledge of companies or auth sessions.
type UserService struct {
	repo UserRepository
}

// NewUserService constructs a UserService backed by repo.
func NewUserService(repo UserRepository) *UserService {
	return &UserService{repo: repo}
}

// CreateUser normalizes email, hashes plaintextPassword, and persists a new User.
func (s *UserService) CreateUser(ctx context.Context, email, plaintextPassword string) (User, error) {
	hash, err := HashPassword(plaintextPassword)
	if err != nil {
		return User{}, err
	}
	return s.repo.Create(ctx, User{
		Email:        NormalizeEmail(email),
		PasswordHash: hash,
		CreatedAt:    time.Now(),
	})
}

// FindUserByEmail normalizes email before lookup.
func (s *UserService) FindUserByEmail(ctx context.Context, email string) (User, error) {
	return s.repo.FindByEmail(ctx, NormalizeEmail(email))
}

// FindUserByID looks up a User by ID.
func (s *UserService) FindUserByID(ctx context.Context, id string) (User, error) {
	return s.repo.FindByID(ctx, id)
}

// DeleteUser removes a User by ID. Used only for compensation flows (registration
// and add-member partial-failure cleanup) — never for a general "delete account"
// feature, which is out of scope for Milestone 1.
func (s *UserService) DeleteUser(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}

// FindOrCreateUser returns an existing User's ID (with an empty tempPassword and
// created=false) if email already has an account, or provisions a brand-new
// User with a cryptographically random temporary password (created=true,
// tempPassword returned once so the caller — companies.Service — can email it).
// The new User's MustChangePassword is set to true. Satisfies
// companies.UserProvisioner structurally.
func (s *UserService) FindOrCreateUser(ctx context.Context, email string) (userID, tempPassword string, created bool, err error) {
	normalized := NormalizeEmail(email)

	existing, err := s.repo.FindByEmail(ctx, normalized)
	if err == nil {
		return existing.ID, "", false, nil
	}
	if err != ErrUserNotFound {
		return "", "", false, err
	}

	generated, err := generateTempPassword()
	if err != nil {
		return "", "", false, err
	}
	hash, err := HashPassword(generated)
	if err != nil {
		return "", "", false, err
	}

	newUser, err := s.repo.Create(ctx, User{
		Email:              normalized,
		PasswordHash:       hash,
		MustChangePassword: true,
		CreatedAt:          time.Now(),
	})
	if err != nil {
		return "", "", false, err
	}

	return newUser.ID, generated, true, nil
}

// DeleteProvisionedUser removes a User by ID. Satisfies companies.UserProvisioner
// structurally. Callers must only invoke this for a User this same call chain
// just created (created=true from FindOrCreateUser) — never for a pre-existing
// User found by FindOrCreateUser.
func (s *UserService) DeleteProvisionedUser(ctx context.Context, userID string) error {
	return s.repo.Delete(ctx, userID)
}
