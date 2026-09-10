package identity

import (
	"context"
	"errors"
)

// UserRepository persists Users. identity owns the users collection exclusively;
// no other module may query it directly.
type UserRepository interface {
	Create(ctx context.Context, u User) (User, error)
	FindByEmail(ctx context.Context, normalizedEmail string) (User, error)
	FindByID(ctx context.Context, id string) (User, error)
	Delete(ctx context.Context, id string) error
}

// ErrUserNotFound is returned by UserRepository lookups that find no match.
var ErrUserNotFound = errors.New("identity: user not found")

// ErrDuplicateEmail is returned by UserRepository.Create when the normalized
// email already exists.
var ErrDuplicateEmail = errors.New("identity: email already registered")
