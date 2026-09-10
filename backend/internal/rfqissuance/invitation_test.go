package rfqissuance_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func TestNewInvitationEnforcesDisplayAndExpiryLimits(t *testing.T) {
	input := rfqissuance.NewInvitationInput{
		CompanyID: "company-1", RFQChainID: "chain-1", SupplierID: "supplier-1",
		CurrentIssuedRFQVersionID: "version-1", RecipientName: strings.Repeat("界", 201),
		RecipientEmail: "supplier@example.test", ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		CreatedByUserID: "user-1",
	}
	if _, err := rfqissuance.NewInvitation(input); !errors.Is(err, rfqissuance.ErrInputLimitExceeded) {
		t.Errorf("201-rune recipient name error = %v, want ErrInputLimitExceeded", err)
	}
	input.RecipientName = "Supplier"
	input.ExpiresAt = time.Now().Add(180*24*time.Hour + time.Minute)
	if _, err := rfqissuance.NewInvitation(input); !errors.Is(err, rfqissuance.ErrInvalidBusinessDate) {
		t.Errorf("expiry beyond 180 days error = %v, want ErrInvalidBusinessDate", err)
	}
}

// The stable Supplier Invitation (design spec §3.4, §5).
//
// "Stable" is the whole design: ONE invitation exists per
// Company + RFQChain + Supplier for the life of the chain. It advances its
// pointer to each newer issued version rather than being replaced, so a
// Supplier's link keeps working across amendments and their offer history stays
// attached to one identity.

func TestNewInvitationNormalisesTheRecipientEmail(t *testing.T) {
	invitation, err := rfqissuance.NewInvitation(rfqissuance.NewInvitationInput{
		CompanyID: "company-1", RFQChainID: "chain-1", SupplierID: "supplier-1",
		CurrentIssuedRFQVersionID: "version-1",
		RecipientName:             "  Aisha Rahman  ",
		RecipientEmail:            "  Sales@Supplier.COM  ",
		ExpiresAt:                 time.Now().Add(30 * 24 * time.Hour),
		CreatedByUserID:           "user-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The normalised form is the IDENTITY key: Supplier access matches on it,
	// so "Sales@Supplier.COM" and "sales@supplier.com" must be one recipient.
	if invitation.RecipientEmailNormalized != "sales@supplier.com" {
		t.Errorf("RecipientEmailNormalized = %q, want sales@supplier.com",
			invitation.RecipientEmailNormalized)
	}
	// The display form keeps what the contractor typed, minus surrounding space.
	if invitation.RecipientEmail != "Sales@Supplier.COM" {
		t.Errorf("RecipientEmail = %q, want the trimmed original", invitation.RecipientEmail)
	}
	if invitation.RecipientName != "Aisha Rahman" {
		t.Errorf("RecipientName = %q, want the trimmed name", invitation.RecipientName)
	}
}

// A new invitation starts as a DRAFT: creation never contacts the Supplier
// (§5.1). Sending is a separate explicit action.
func TestNewInvitationStartsAsADraftAtGenerationOne(t *testing.T) {
	invitation, err := rfqissuance.NewInvitation(validInvitationInput())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if invitation.Status != rfqissuance.InvitationStatusDraft {
		t.Errorf("Status = %q, want draft: creation must not contact the Supplier (§5.1)",
			invitation.Status)
	}
	if invitation.AccessGeneration != 1 {
		t.Errorf("AccessGeneration = %d, want 1", invitation.AccessGeneration)
	}
	if invitation.RevokedAt != nil {
		t.Error("a new invitation must not be revoked")
	}
	if invitation.FirstViewedAt != nil || invitation.LastViewedAt != nil {
		t.Error("a new invitation has never been viewed")
	}
}

func validInvitationInput() rfqissuance.NewInvitationInput {
	return rfqissuance.NewInvitationInput{
		CompanyID: "company-1", RFQChainID: "chain-1", SupplierID: "supplier-1",
		CurrentIssuedRFQVersionID: "version-1",
		RecipientName:             "Aisha Rahman",
		RecipientEmail:            "sales@supplier.com",
		ExpiresAt:                 time.Now().Add(30 * 24 * time.Hour),
		CreatedByUserID:           "user-1",
	}
}

func TestNewInvitationRejectsAnInvalidRecipientEmail(t *testing.T) {
	for _, email := range []string{"", "   ", "not-an-email", "@supplier.com", "a@"} {
		t.Run(email, func(t *testing.T) {
			input := validInvitationInput()
			input.RecipientEmail = email

			if _, err := rfqissuance.NewInvitation(input); err !=
				rfqissuance.ErrInvalidRecipientEmail {
				t.Errorf("error = %v, want ErrInvalidRecipientEmail", err)
			}
		})
	}
}

func TestNewInvitationRequiresARecipientName(t *testing.T) {
	input := validInvitationInput()
	input.RecipientName = "   "

	if _, err := rfqissuance.NewInvitation(input); err !=
		rfqissuance.ErrRecipientNameRequired {
		t.Errorf("error = %v, want ErrRecipientNameRequired", err)
	}
}

// An expiry in the past would create an invitation that is dead on arrival
// (§5.1).
func TestNewInvitationRequiresAFutureExpiry(t *testing.T) {
	input := validInvitationInput()
	input.ExpiresAt = time.Now().Add(-time.Hour)

	if _, err := rfqissuance.NewInvitation(input); err !=
		rfqissuance.ErrInvitationExpiryNotInFuture {
		t.Errorf("error = %v, want ErrInvitationExpiryNotInFuture", err)
	}
}

// --- access predicate (§5.4, §6.3) ---
//
// Expiry and revocation are SEPARATE controls. Access requires both to pass.

func TestInvitationAccessRequiresActiveUnexpiredAndUnrevoked(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)
	revoked := now.Add(-time.Hour)

	cases := []struct {
		name       string
		status     rfqissuance.InvitationStatus
		expiresAt  time.Time
		revokedAt  *time.Time
		wantAccess bool
	}{
		{"active and unexpired", rfqissuance.InvitationStatusActive, future, nil, true},
		{"still a draft", rfqissuance.InvitationStatusDraft, future, nil, false},
		{"expired by time", rfqissuance.InvitationStatusActive, past, nil, false},
		{"revoked", rfqissuance.InvitationStatusRevoked, future, &revoked, false},
		// Revocation blocks IMMEDIATELY, even if the stored status still says
		// active — the timestamp is the authority, not a status that may lag.
		{"revoked timestamp with stale status", rfqissuance.InvitationStatusActive,
			future, &revoked, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invitation := rfqissuance.SupplierInvitation{
				Status: tc.status, ExpiresAt: tc.expiresAt, RevokedAt: tc.revokedAt,
			}

			if got := invitation.PermitsAccess(now); got != tc.wantAccess {
				t.Errorf("PermitsAccess = %v, want %v", got, tc.wantAccess)
			}
		})
	}
}

// Expiry is evaluated against the CURRENT time on every check, never against a
// persisted flag that could go stale (§3.4).
func TestInvitationExpiryIsEvaluatedAgainstCurrentTime(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	invitation := rfqissuance.SupplierInvitation{
		Status: rfqissuance.InvitationStatusActive, ExpiresAt: expiry,
	}

	if !invitation.PermitsAccess(expiry.Add(-time.Minute)) {
		t.Error("access must be permitted before the expiry instant")
	}
	if invitation.PermitsAccess(expiry.Add(time.Minute)) {
		t.Error("access must be refused after the expiry instant")
	}
}

// --- email normalisation ---

func TestNormalizeRecipientEmailIsCaseAndSpaceInsensitive(t *testing.T) {
	variants := []string{
		"sales@supplier.com",
		"SALES@SUPPLIER.COM",
		"  Sales@Supplier.Com  ",
	}

	first, err := rfqissuance.NormalizeRecipientEmail(variants[0])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, variant := range variants[1:] {
		got, err := rfqissuance.NormalizeRecipientEmail(variant)
		if err != nil {
			t.Fatalf("normalising %q: %v", variant, err)
		}
		if got != first {
			t.Errorf("%q normalised to %q, want %q. The normalised form is the identity "+
				"key a Supplier session matches on", variant, got, first)
		}
	}
}

func TestNormalizeRecipientEmailRejectsMalformedInput(t *testing.T) {
	for _, email := range []string{"", "nope", strings.Repeat("a", 300) + "@x.com"} {
		if _, err := rfqissuance.NormalizeRecipientEmail(email); err == nil {
			t.Errorf("expected %q to be rejected", email)
		}
	}
}
