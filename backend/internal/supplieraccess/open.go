package supplieraccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

const (
	opaqueCredentialBytes  = 32
	opaqueCredentialChars  = 43
	accessExchangeLifetime = 10 * time.Minute
)

// OpenInvitationResult carries the new browser exchange credential only as far
// as the HTTP boundary, where it is placed in an HttpOnly cookie.
type OpenInvitationResult struct {
	ExchangeToken string
	ExpiresAt     time.Time
}

// CryptographicOpaqueTokenGenerator produces a full 256 bits from crypto/rand.
// Raw values are never persisted by supplieraccess repositories.
type CryptographicOpaqueTokenGenerator struct{}

func (CryptographicOpaqueTokenGenerator) Generate() (string, error) {
	random := make([]byte, opaqueCredentialBytes)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("supplieraccess: generate opaque credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// IsCanonicalOpaqueCredential rejects malformed, padded, Unicode, shortened,
// and oversized inputs before any repository or cross-module call.
func IsCanonicalOpaqueCredential(raw string) bool {
	if len(raw) != opaqueCredentialChars {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != opaqueCredentialBytes {
		return false
	}
	return base64.RawURLEncoding.EncodeToString(decoded) == raw
}

// HashAccessExchangeToken is the canonical persistence representation for the
// raw exchange cookie.
func HashAccessExchangeToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// OpenInvitation resolves an opaque invitation token without any caller
// tenant/ID, records the exact generation viewed, and mints a ten-minute
// single-use browser exchange.
func (s *Service) OpenInvitation(ctx context.Context, rawInvitationToken string,
	openedAt time.Time) (OpenInvitationResult, error) {

	if s.invitations == nil || s.views == nil || s.exchanges == nil || s.tokens == nil {
		return OpenInvitationResult{}, ErrSupplierAccessNotConfigured
	}
	if openedAt.IsZero() {
		return OpenInvitationResult{}, ErrSupplierAccessNotConfigured
	}
	if !IsCanonicalOpaqueCredential(rawInvitationToken) {
		return OpenInvitationResult{}, ErrInvalidSupplierCredential
	}

	// Only the hash crosses the module boundary. The resolver derives tenant and
	// invitation identity from the globally indexed current invitation.
	invitationHash := secrets.HashInvitationSecret(rawInvitationToken)
	snapshot, found, err := s.invitations.ResolveInvitationAccess(
		ctx, invitationHash, openedAt)
	if err != nil {
		return OpenInvitationResult{}, err
	}
	if !found {
		return OpenInvitationResult{}, ErrInvalidSupplierCredential
	}

	if err := s.views.RecordInvitationViewed(ctx, snapshot.CompanyID,
		snapshot.InvitationID, snapshot.AccessGeneration, openedAt); err != nil {
		if errors.Is(err, ErrInvitationAccessInvalid) {
			return OpenInvitationResult{}, ErrInvalidSupplierCredential
		}
		return OpenInvitationResult{}, err
	}

	exchangeID, err := s.tokens.Generate()
	if err != nil {
		return OpenInvitationResult{}, err
	}
	rawExchangeToken, err := s.tokens.Generate()
	if err != nil {
		return OpenInvitationResult{}, err
	}
	if !IsCanonicalOpaqueCredential(exchangeID) ||
		!IsCanonicalOpaqueCredential(rawExchangeToken) {
		return OpenInvitationResult{}, fmt.Errorf(
			"supplieraccess: opaque token generator returned a non-canonical value")
	}

	expiresAt := openedAt.Add(accessExchangeLifetime)
	_, err = s.exchanges.CreateExchange(ctx, SupplierAccessExchange{
		ID:                       exchangeID,
		ExchangeTokenHash:        HashAccessExchangeToken(rawExchangeToken),
		CompanyID:                snapshot.CompanyID,
		SupplierID:               snapshot.SupplierID,
		InvitationID:             snapshot.InvitationID,
		AccessGeneration:         snapshot.AccessGeneration,
		NormalizedRecipientEmail: snapshot.NormalizedRecipientEmail,
		CreatedAt:                openedAt,
		ExpiresAt:                expiresAt,
	})
	if err != nil {
		return OpenInvitationResult{}, err
	}
	return OpenInvitationResult{
		ExchangeToken: rawExchangeToken,
		ExpiresAt:     expiresAt,
	}, nil
}
