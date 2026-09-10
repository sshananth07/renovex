package identity

import (
	"context"
	"errors"
	"time"
)

// ErrSessionNotFound is returned when an AuthSession lookup finds no match
// (including one whose hash was already rotated away).
var ErrSessionNotFound = errors.New("identity: auth session not found")

// AuthSessionRepository persists AuthSessions.
type AuthSessionRepository interface {
	Create(ctx context.Context, s AuthSession) (AuthSession, error)
	FindActiveByHash(ctx context.Context, refreshTokenHash string) (AuthSession, error)
	RotateHash(ctx context.Context, sessionID, newHash string, lastUsedAt time.Time) error
	Revoke(ctx context.Context, sessionID string, revokedAt time.Time) error

	// DeleteAllForUser permanently removes every AuthSession owned by userID.
	// Never errors when zero documents match. Development-tool use only
	// (demoseed reset, design spec §6.6) — AuthSession is scoped by UserID,
	// not CompanyID, so this is deliberately named and shaped differently
	// from the company-scoped DeleteAllForCompany pattern used elsewhere.
	DeleteAllForUser(ctx context.Context, userID string) error
}
