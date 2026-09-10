package supplieraccess

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

const (
	verificationRateWindow  = time.Hour
	identityRateCooldown    = time.Minute
	identityRateLimit       = 5
	resendRateCooldown      = time.Minute
	resendRateLimit         = 5
	clientAddressRateLimit  = 20
	maxRateLimitCASAttempts = 64
)

type VerificationRateLimitStore interface {
	CreateState(context.Context, VerificationRateLimitState) (
		VerificationRateLimitState, error)
	FindState(context.Context, VerificationRateLimitScope, string) (
		VerificationRateLimitState, error)
	ReplaceStateCAS(context.Context, VerificationRateLimitState, int64) (
		VerificationRateLimitState, error)
}

// VerificationRateLimitExceeded carries the earliest time this one scope can
// accept a new reservation. Public handlers convert it to bounded Retry-After.
type VerificationRateLimitExceeded struct {
	RetryAt time.Time
}

func (e *VerificationRateLimitExceeded) Error() string {
	return ErrVerificationRateLimited.Error()
}

func (e *VerificationRateLimitExceeded) Unwrap() error {
	return ErrVerificationRateLimited
}

// VerificationRateLimiter owns the prune/evaluate/append CAS loop. Repository
// uniqueness serializes first creation; Revision serializes every later claim.
type VerificationRateLimiter struct {
	states VerificationRateLimitStore
	tokens OpaqueTokenGenerator
}

func NewVerificationRateLimiter(states VerificationRateLimitStore,
	tokens OpaqueTokenGenerator) *VerificationRateLimiter {
	return &VerificationRateLimiter{states: states, tokens: tokens}
}

// Claim reserves one request. Reusing the same reservation ID inside the
// retained window is idempotent and consumes no additional quota.
func (l *VerificationRateLimiter) Claim(ctx context.Context,
	scope VerificationRateLimitScope, scopeKeyHash, reservationID string,
	requestedAt time.Time) error {

	limit, cooldown, err := rateScopePolicy(scope)
	if err != nil || l.states == nil || l.tokens == nil ||
		!canonicalSHA256Hex.MatchString(scopeKeyHash) ||
		reservationID == "" || requestedAt.IsZero() {
		return ErrVerificationRateLimitUnavailable
	}

	for attempt := 0; attempt < maxRateLimitCASAttempts; attempt++ {
		state, findErr := l.states.FindState(ctx, scope, scopeKeyHash)
		if errors.Is(findErr, ErrVerificationRateLimitStateNotFound) {
			stateID, tokenErr := l.tokens.Generate()
			if tokenErr != nil {
				return fmt.Errorf("%w: %v",
					ErrVerificationRateLimitUnavailable, tokenErr)
			}
			_, createErr := l.states.CreateState(ctx, VerificationRateLimitState{
				ID: stateID, Scope: scope, ScopeKeyHash: scopeKeyHash,
				Reservations: []VerificationRateReservation{{
					ReservationID: reservationID,
					RequestedAt:   requestedAt,
				}},
				Revision: 1, UpdatedAt: requestedAt,
				ExpiresAt: requestedAt.Add(verificationRateWindow),
			})
			if createErr == nil {
				return nil
			}
			if errors.Is(createErr, ErrVerificationRateLimitStateAlreadyExists) {
				continue
			}
			return createErr
		}
		if findErr != nil {
			return findErr
		}

		retained := pruneRateReservations(state.Reservations, requestedAt)
		for _, reservation := range retained {
			if reservation.ReservationID == reservationID {
				return nil
			}
		}

		if retryAt, limited := nextRatePermit(retained, limit, cooldown, requestedAt); limited {
			return &VerificationRateLimitExceeded{RetryAt: retryAt}
		}
		retained = append(retained, VerificationRateReservation{
			ReservationID: reservationID,
			RequestedAt:   requestedAt,
		})
		sort.Slice(retained, func(i, j int) bool {
			return retained[i].RequestedAt.Before(retained[j].RequestedAt)
		})
		candidate := state
		candidate.Reservations = retained
		candidate.Revision = state.Revision + 1
		candidate.UpdatedAt = requestedAt
		candidate.ExpiresAt = retained[len(retained)-1].RequestedAt.Add(
			verificationRateWindow)
		if _, replaceErr := l.states.ReplaceStateCAS(
			ctx, candidate, state.Revision); replaceErr == nil {
			return nil
		} else if errors.Is(replaceErr, ErrVerificationRateLimitStateConflict) {
			continue
		} else {
			return replaceErr
		}
	}
	return ErrVerificationRateLimitUnavailable
}

// rateLimitStoreHashDeleter is the unexported capability the underlying
// VerificationRateLimitStore may satisfy — only the real Mongo repository
// does, never the VerificationRateLimitStore interface itself (see
// MongoVerificationRateLimitRepository.DeleteByScopeAndHash's doc comment).
type rateLimitStoreHashDeleter interface {
	DeleteByScopeAndHash(ctx context.Context, scope VerificationRateLimitScope,
		scopeKeyHash string) error
}

// DeleteByScopeAndHash is a development-tool-only passthrough to the
// underlying store's bulk-cleanup capability (see
// supplieraccess.Service.DeleteAllForCompany, which needs this to remove
// rate-limit state this collection cannot otherwise be bulk-deleted by
// companyID, since it has no such field).
func (l *VerificationRateLimiter) DeleteByScopeAndHash(ctx context.Context,
	scope VerificationRateLimitScope, scopeKeyHash string) error {

	deleter, ok := l.states.(rateLimitStoreHashDeleter)
	if !ok {
		return fmt.Errorf("supplieraccess: rate limit store %T does not support DeleteByScopeAndHash", l.states)
	}
	return deleter.DeleteByScopeAndHash(ctx, scope, scopeKeyHash)
}

func rateScopePolicy(scope VerificationRateLimitScope) (
	limit int, cooldown time.Duration, err error) {
	switch scope {
	case VerificationRateScopeIdentity:
		return identityRateLimit, identityRateCooldown, nil
	case VerificationRateScopeClient:
		return clientAddressRateLimit, 0, nil
	case VerificationRateScopeResend:
		return resendRateLimit, resendRateCooldown, nil
	default:
		return 0, 0, ErrVerificationRateLimitUnavailable
	}
}

func pruneRateReservations(reservations []VerificationRateReservation,
	now time.Time) []VerificationRateReservation {
	cutoff := now.Add(-verificationRateWindow)
	retained := make([]VerificationRateReservation, 0, len(reservations))
	for _, reservation := range reservations {
		// At the exact boundary the reservation leaves the rolling window.
		if reservation.RequestedAt.After(cutoff) {
			retained = append(retained, reservation)
		}
	}
	sort.Slice(retained, func(i, j int) bool {
		return retained[i].RequestedAt.Before(retained[j].RequestedAt)
	})
	return retained
}

func nextRatePermit(reservations []VerificationRateReservation,
	limit int, cooldown time.Duration, now time.Time) (time.Time, bool) {

	var retryAt time.Time
	if len(reservations) >= limit {
		retryAt = reservations[0].RequestedAt.Add(verificationRateWindow)
	}
	if cooldown > 0 && len(reservations) > 0 {
		cooldownEnds := reservations[len(reservations)-1].RequestedAt.Add(cooldown)
		if now.Before(cooldownEnds) && cooldownEnds.After(retryAt) {
			retryAt = cooldownEnds
		}
	}
	return retryAt, !retryAt.IsZero() && now.Before(retryAt)
}
