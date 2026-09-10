package secrets

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
)

const (
	supplierRateIdentityDomain        = "supplier-rate-limit-identity-v1"
	supplierRateClientAddressDomain   = "supplier-rate-limit-client-address-v1"
	supplierRateChallengeResendDomain = "supplier-rate-limit-challenge-resend-v1"
)

// SupplierRateLimitFingerprinter turns sensitive rate-scope inputs into keyed,
// irreversible identifiers. The raw recipient and client address therefore
// never need to enter rate-limit persistence.
type SupplierRateLimitFingerprinter struct {
	key []byte
}

// NewSupplierRateLimitFingerprinter validates the dedicated, non-versioned
// Phase 1 key. Operational rotation intentionally resets defense-in-depth rate
// state, as recorded in Revision 11.
func NewSupplierRateLimitFingerprinter(
	encodedKey string) (*SupplierRateLimitFingerprinter, error) {

	key, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, fmt.Errorf(
			"secrets: supplier rate-limit fingerprint key is not valid base64: %w", err)
	}
	if len(key) < MinInvitationKeyBytes {
		return nil, fmt.Errorf(
			"secrets: supplier rate-limit fingerprint key decodes to %d bytes; at least %d "+
				"random bytes are required", len(key), MinInvitationKeyBytes)
	}
	return &SupplierRateLimitFingerprinter{key: key}, nil
}

// FingerprintIdentity derives the per-invitation-generation rate scope.
func (f *SupplierRateLimitFingerprinter) FingerprintIdentity(
	companyID, supplierID, normalizedRecipientEmail, invitationID string,
	accessGeneration int64) (string, error) {

	if companyID == "" || supplierID == "" || normalizedRecipientEmail == "" ||
		invitationID == "" || accessGeneration < 1 {
		return "", fmt.Errorf("secrets: complete supplier rate-limit identity is required")
	}
	mac := hmac.New(sha256.New, f.key)
	writeLengthPrefixed(mac, []byte(supplierRateIdentityDomain))
	writeLengthPrefixed(mac, []byte(companyID))
	writeLengthPrefixed(mac, []byte(supplierID))
	writeLengthPrefixed(mac, []byte(normalizedRecipientEmail))
	writeLengthPrefixed(mac, []byte(invitationID))
	writeLengthPrefixedUint64(mac, uint64(accessGeneration))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// FingerprintClientAddress hashes canonical address bytes. Unmapping
// IPv4-mapped IPv6 prevents one client from receiving two quotas merely by
// spelling the same network address differently.
func (f *SupplierRateLimitFingerprinter) FingerprintClientAddress(
	address netip.Addr) (string, error) {

	if !address.IsValid() {
		return "", fmt.Errorf("secrets: valid supplier client address is required")
	}
	canonical := address.Unmap()
	mac := hmac.New(sha256.New, f.key)
	writeLengthPrefixed(mac, []byte(supplierRateClientAddressDomain))
	writeLengthPrefixed(mac, canonical.AsSlice())
	return hex.EncodeToString(mac.Sum(nil)), nil
}

// FingerprintChallengeResend derives the separate per-challenge email-resend
// quota approved for Revision 12's resend route. It cannot collide
// semantically with challenge creation or client-address scopes.
func (f *SupplierRateLimitFingerprinter) FingerprintChallengeResend(
	companyID, challengeID string) (string, error) {

	if companyID == "" || challengeID == "" {
		return "", fmt.Errorf(
			"secrets: complete supplier challenge resend identity is required")
	}
	mac := hmac.New(sha256.New, f.key)
	writeLengthPrefixed(mac, []byte(supplierRateChallengeResendDomain))
	writeLengthPrefixed(mac, []byte(companyID))
	writeLengthPrefixed(mac, []byte(challengeID))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
