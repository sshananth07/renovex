package rfqissuance

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

// The invitation lifecycle (design spec §5, §6.1A, §1A.2, §1A.4).
//
// Two invariants run through everything here:
//
//  1. The raw invitation secret is DERIVED, never stored. Only its hash, the
//     access generation and the key version are persisted, which is what lets
//     copy-link reproduce the current link without rotating it.
//  2. Mail is SYNCHRONOUS and happens after the delivery intent is persisted,
//     so a failure leaves a visible retryable record rather than an invitation
//     that never existed.

// InvitationStore is the invitation repository surface.
type InvitationStore interface {
	CreateInvitation(ctx context.Context, invitation SupplierInvitation) (
		SupplierInvitation, error)
	FindInvitation(ctx context.Context, companyID, invitationID string) (
		SupplierInvitation, error)
	FindInvitationByAccessSecretHash(ctx context.Context, accessSecretHash string) (
		SupplierInvitation, error)
	FindInvitationForSupplier(ctx context.Context, companyID, rfqChainID, supplierID string) (
		SupplierInvitation, error)
	ListInvitationsForChain(ctx context.Context, companyID, rfqChainID string) (
		[]SupplierInvitation, error)
	UpdateInvitation(ctx context.Context, companyID, invitationID string,
		expectedRevision int64, updated SupplierInvitation) (SupplierInvitation, error)
	RotateSecret(ctx context.Context, companyID, invitationID string,
		expectedRevision int64, newSecretHash string, keyVersion int) (
		SupplierInvitation, error)
	ReplaceRecipientAtomically(ctx context.Context, input ReplaceRecipientAtomicInput) (
		SupplierInvitation, error)
	ReactivateInvitation(ctx context.Context, companyID, invitationID string,
		command ReactivateInvitationCommand) (SupplierInvitation, bool, error)
	RecordViewed(ctx context.Context, companyID, invitationID string,
		accessGeneration int64, viewedAt time.Time) error
	AdvanceInvitationsToVersion(ctx context.Context, companyID, rfqChainID,
		issuedVersionID string) (int64, error)
}

// DeliveryAttemptStore is the delivery-attempt repository surface.
type DeliveryAttemptStore interface {
	CreateAttempt(ctx context.Context, attempt InvitationDeliveryAttempt) (
		InvitationDeliveryAttempt, error)
	UpdateAttemptStatus(ctx context.Context, companyID, attemptID string,
		status DeliveryStatus, failureCode DeliveryFailureCode, sentAt *time.Time) error
	FindAttemptByOperationID(ctx context.Context, companyID, invitationID,
		operationID string) (InvitationDeliveryAttempt, bool, error)
	ListAttemptsForInvitation(ctx context.Context, companyID, invitationID string) (
		[]InvitationDeliveryAttempt, error)
	ObsoletePendingAttemptsBeforeGeneration(ctx context.Context, companyID,
		invitationID string, currentGeneration int64) (int64, error)
}

// WithInvitations supplies the invitation store.
func WithInvitations(invitations InvitationStore) Option {
	return func(s *Service) { s.invitations = invitations }
}

// WithDeliveryAttempts supplies the delivery-attempt store.
func WithDeliveryAttempts(deliveries DeliveryAttemptStore) Option {
	return func(s *Service) { s.deliveries = deliveries }
}

// WithSupplierLookup supplies the Supplier Directory capability.
func WithSupplierLookup(suppliers SupplierLookup) Option {
	return func(s *Service) { s.suppliers = suppliers }
}

// WithInvitationKeyring supplies the secret-derivation keyring.
func WithInvitationKeyring(keyring *secrets.InvitationKeyring) Option {
	return func(s *Service) { s.keyring = keyring }
}

// WithMailer supplies the synchronous mail sender.
func WithMailer(mailer Mailer) Option {
	return func(s *Service) { s.mailer = mailer }
}

// WithSupplierLinkBaseURL supplies the absolute base URL used to build the
// Supplier-facing link.
func WithSupplierLinkBaseURL(baseURL string) Option {
	return func(s *Service) { s.supplierLinkBaseURL = baseURL }
}

// WithLogger supplies the structured logger used for operational diagnostics.
//
// This module logs identifiers and outcomes ONLY. A raw invitation token, a
// derived link and any provider error text are never passed to it — see
// secret_exposure_test.go, which searches captured log output for the exact
// token the service returned.
func WithLogger(logger *zerolog.Logger) Option {
	return func(s *Service) { s.logger = logger }
}

// invitationsReady reports whether the invitation collaborators are wired.
func (s *Service) invitationsReady() bool {
	return s.invitations != nil && s.deliveries != nil && s.suppliers != nil &&
		s.keyring != nil && s.chains != nil
}

// deriveLink re-derives an invitation's CURRENT raw token and its URL.
//
// It verifies the derived token against the STORED hash and fails closed on a
// mismatch. A mismatch means the invitation's key version is no longer
// configured or its stored hash was written by a different key — returning a
// link that cannot open is worse than refusing, because the contractor would
// send it and only discover the failure through the Supplier.
func (s *Service) deriveLink(companyID string, invitation SupplierInvitation) (
	InvitationLink, error) {

	raw, err := s.keyring.DeriveInvitationSecret(invitation.SecretKeyVersion,
		companyID, invitation.ID, invitation.AccessGeneration)
	if err != nil {
		return InvitationLink{}, err
	}
	if !secrets.VerifyInvitationSecret(raw, invitation.AccessSecretHash) {
		return InvitationLink{}, ErrInvitationSecretUnavailable
	}

	return InvitationLink{
		Token: raw,
		URL: fmt.Sprintf("%s/supplier-access/open?token=%s",
			strings.TrimRight(s.supplierLinkBaseURL, "/"), url.QueryEscape(raw)),
	}, nil
}

// InvitationLink is a raw, single-use-in-transit link.
//
// It is returned to the contractor and never persisted, logged or audited.
type InvitationLink struct {
	Token string
	URL   string
}

// CreateInvitationInput carries a new invitation.
type CreateInvitationInput struct {
	RFQChainID     string
	SupplierID     string
	RecipientName  string
	RecipientEmail string
	ExpiresAt      time.Time
}

// CreateInvitation creates a DRAFT invitation against the chain's current
// issued version (§5.1).
//
// It contacts no one. The secret is derived only after the invitation has an
// ID, because the ID is part of the canonical derivation input.
func (s *Service) CreateInvitation(ctx context.Context, companyID, actorUserID string,
	input CreateInvitationInput) (SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}

	current, err := s.currentIssuedVersion(ctx, companyID, input.RFQChainID)
	if err != nil {
		return SupplierInvitation{}, err
	}

	// Absent, foreign or archived all collapse to one refusal (§5.1).
	existing, err := s.invitations.ListInvitationsForChain(
		ctx, companyID, input.RFQChainID)
	if err != nil {
		return SupplierInvitation{}, err
	}
	if len(existing) >= procurementlimits.MaxInvitationsPerChain {
		return SupplierInvitation{}, ErrInputLimitExceeded
	}

	invitable, err := s.suppliers.SupplierIsInvitable(ctx, companyID, input.SupplierID)
	if err != nil {
		return SupplierInvitation{}, err
	}
	if !invitable {
		return SupplierInvitation{}, ErrSupplierNotInvitable
	}

	invitation, err := NewInvitation(NewInvitationInput{
		CompanyID: companyID, RFQChainID: input.RFQChainID, SupplierID: input.SupplierID,
		CurrentIssuedRFQVersionID: current.ID,
		RecipientName:             input.RecipientName,
		RecipientEmail:            input.RecipientEmail,
		ExpiresAt:                 input.ExpiresAt,
		CreatedByUserID:           actorUserID,
	})
	if err != nil {
		return SupplierInvitation{}, err
	}

	// The SERVICE mints the ID, because the ID is part of the HMAC canonical
	// input: the secret cannot be derived until the ID exists. Letting the
	// repository assign it would make the secret underivable before the insert
	// and force a second write to complete the record.
	invitation.ID = newIdentity()
	invitation.SecretKeyVersion = s.keyring.ActiveVersion()

	// Derive for the INITIAL generation the record will carry. Creation is not
	// a rotation: a new invitation has never had a link invalidated, so it
	// starts at generation 1 and its hash is the hash of generation 1's token.
	raw, err := s.keyring.DeriveInvitationSecret(invitation.SecretKeyVersion,
		companyID, invitation.ID, invitation.AccessGeneration)
	if err != nil {
		return SupplierInvitation{}, err
	}
	invitation.AccessSecretHash = secrets.HashInvitationSecret(raw)

	// ONE write. The invitation lands complete — ID, generation, key version and
	// hash together — so there is no window in which a crash could leave a
	// healthy-looking record whose link can never be reproduced.
	created, err := s.invitations.CreateInvitation(ctx, invitation)
	if err != nil {
		return SupplierInvitation{}, err
	}

	_ = s.audit.RecordSupplierInvitationCreated(ctx, companyID, current.ProjectID,
		actorUserID, input.RFQChainID, current.RFQNumber, created.ID, input.SupplierID)

	return created, nil
}

// GetInvitation reads one invitation, tenant-scoped.
func (s *Service) GetInvitation(ctx context.Context, companyID, invitationID string) (
	SupplierInvitation, error) {
	if s.invitations == nil {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}
	return s.invitations.FindInvitation(ctx, companyID, invitationID)
}

// ListInvitations returns every invitation on a chain, tenant-scoped.
func (s *Service) ListInvitations(ctx context.Context, companyID, rfqChainID string) (
	[]SupplierInvitation, error) {
	if s.invitations == nil {
		return nil, ErrInvitationsNotConfigured
	}
	return s.invitations.ListInvitationsForChain(ctx, companyID, rfqChainID)
}

// CopyInvitationLink returns the CURRENT generation's link without rotating it
// (§1A.4).
//
// This is the capability that made HMAC derivation necessary: a randomly
// generated secret exists only at creation time, so an ordinary copy could only
// mint a new link and silently break the one already sent.
func (s *Service) CopyInvitationLink(ctx context.Context, companyID, actorUserID,
	invitationID string) (InvitationLink, error) {

	if !s.invitationsReady() {
		return InvitationLink{}, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return InvitationLink{}, err
	}
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return InvitationLink{}, ErrInvitationRevoked
	}

	link, err := s.deriveLink(companyID, invitation)
	if err != nil {
		return InvitationLink{}, err
	}

	// Recorded as a copy_link attempt so the contractor can see the link left
	// the platform, even though nothing was emailed. The raw link is NOT part
	// of the record.
	attempt := NewDeliveryAttempt(NewDeliveryAttemptInput{
		CompanyID: companyID, InvitationID: invitation.ID,
		DeliveryOperationID: newIdentity(),
		AccessGeneration:    invitation.AccessGeneration,
		Channel:             DeliveryChannelCopyLink,
		RecipientEmail:      invitation.RecipientEmail,
		IssuedRFQVersionID:  invitation.CurrentIssuedRFQVersionID,
		RequestedByUserID:   actorUserID,
	})
	if _, err := s.deliveries.CreateAttempt(ctx, attempt); err != nil {
		return InvitationLink{}, err
	}

	// The audit records THAT a link was copied, never the link itself (§15).
	_ = s.audit.RecordSupplierInvitationLinkCopied(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID, invitation.AccessGeneration)

	return link, nil
}

// DeriveInvitationLink re-derives the invitation's existing access link for an
// internal, system-initiated purpose (e.g. embedding it in an award outcome
// notification) rather than a contractor-visible action.
//
// Unlike CopyInvitationLink this deliberately:
//   - does NOT rotate the access generation
//   - does NOT issue a new invitation
//   - does NOT write a delivery attempt
//   - does NOT mutate the invitation at all
//
// It still verifies the invitation belongs to companyID, is not revoked, and
// derives strictly from the invitation's current (unchanged) access
// generation and key version — the same validity rules CopyInvitationLink
// enforces, minus the side effects that belong only to a human-visible copy
// action.
func (s *Service) DeriveInvitationLink(ctx context.Context, companyID,
	invitationID string) (InvitationLink, error) {

	if !s.invitationsReady() {
		return InvitationLink{}, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return InvitationLink{}, err
	}
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return InvitationLink{}, ErrInvitationRevoked
	}

	return s.deriveLink(companyID, invitation)
}

// SendInvitationInput carries one send or resend.
type SendInvitationInput struct {
	InvitationID string
	OperationID  string
}

// SendResult reports the outcome of a send.
type SendResult struct {
	AttemptID string
	Status    DeliveryStatus
}

// SendInvitation persists a delivery intent, then sends the mail synchronously
// (§5.2, §1A.2).
//
// The order is the recovery model:
//
//	persist pending → send → mark sent or failed
//
// A crash after the first step leaves a `pending` row reconciliation can find.
// A send failure leaves a `failed` row the contractor can retry. Neither ever
// rolls back the invitation, because the Supplier relationship the contractor
// created is real whether or not the email got through.
func (s *Service) SendInvitation(ctx context.Context, companyID, actorUserID string,
	input SendInvitationInput) (SendResult, error) {

	if !s.invitationsReady() || s.mailer == nil {
		return SendResult{}, ErrInvitationsNotConfigured
	}
	if procurementlimits.ValidateID(strings.TrimSpace(input.OperationID)) != nil {
		return SendResult{}, ErrOperationIDRequired
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, input.InvitationID)
	if err != nil {
		return SendResult{}, err
	}
	// Revocation is terminal: a resend must never reactivate access (§5.4).
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return SendResult{}, ErrInvitationRevoked
	}

	// Idempotency: a retry must not deliver the Supplier a second copy (§10.2).
	if existing, found, err := s.deliveries.FindAttemptByOperationID(ctx, companyID,
		invitation.ID, input.OperationID); err != nil {
		return SendResult{}, err
	} else if found {
		return SendResult{AttemptID: existing.ID, Status: existing.Status}, nil
	}

	link, err := s.deriveLink(companyID, invitation)
	if err != nil {
		return SendResult{}, err
	}

	// Step 1: persist the intent BEFORE sending.
	attempt, err := s.deliveries.CreateAttempt(ctx, NewDeliveryAttempt(
		NewDeliveryAttemptInput{
			CompanyID: companyID, InvitationID: invitation.ID,
			DeliveryOperationID: input.OperationID,
			AccessGeneration:    invitation.AccessGeneration,
			Channel:             DeliveryChannelEmail,
			RecipientEmail:      invitation.RecipientEmail,
			IssuedRFQVersionID:  invitation.CurrentIssuedRFQVersionID,
			RequestedByUserID:   actorUserID,
		}))
	if err != nil {
		return SendResult{}, err
	}

	// Step 2: send synchronously.
	sendErr := s.mailer.Send(ctx, mail.Message{
		To:      invitation.RecipientEmail,
		Subject: "Request for Quotation",
		Body:    buildInvitationBody(invitation, link),
	})

	// Step 3: record the outcome. A bounded code only — provider text never
	// reaches the database (§1A.2).
	if sendErr != nil {
		_ = s.deliveries.UpdateAttemptStatus(ctx, companyID, attempt.ID,
			DeliveryStatusFailed, ClassifyDeliveryFailure(sendErr), nil)
		_ = s.audit.RecordSupplierInvitationDeliveryFailed(ctx, companyID, actorUserID,
			invitation.RFQChainID, invitation.ID, attempt.ID,
			string(DeliveryFailureCodeSendFailed))
		return SendResult{AttemptID: attempt.ID, Status: DeliveryStatusFailed},
			ErrMailDeliveryFailed
	}

	sentAt := time.Now()
	if err := s.deliveries.UpdateAttemptStatus(ctx, companyID, attempt.ID,
		DeliveryStatusSent, "", &sentAt); err != nil {
		return SendResult{}, err
	}

	// An explicit send is what activates the invitation (§5).
	if invitation.Status == InvitationStatusDraft {
		activated := invitation
		activated.Status = InvitationStatusActive
		if _, err := s.invitations.UpdateInvitation(ctx, companyID, invitation.ID,
			invitation.Revision, activated); err != nil {
			// The mail is already out; failing here would misreport a delivered
			// invitation as failed. Reconciliation repairs the status.
			_ = err
		}
	}

	_ = s.audit.RecordSupplierInvitationSent(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID, attempt.ID, string(DeliveryChannelEmail))

	return SendResult{AttemptID: attempt.ID, Status: DeliveryStatusSent}, nil
}

// buildInvitationBody renders the Supplier email.
//
// The link is the ONLY secret-bearing content, and it exists solely in this
// in-flight message — never in a log line, an audit record or the database.
func buildInvitationBody(invitation SupplierInvitation, link InvitationLink) string {
	return fmt.Sprintf(
		"Hello %s,\n\n"+
			"You have been invited to quote on a Request for Quotation.\n\n"+
			"Open your secure link:\n%s\n\n"+
			"This link expires on %s.\n",
		invitation.RecipientName, link.URL,
		invitation.ExpiresAt.Format("2 January 2006"))
}

// ReplaceRecipientInput carries a recipient replacement.
type ReplaceRecipientInput struct {
	InvitationID     string
	ExpectedRevision int64
	RecipientName    string
	RecipientEmail   string
	// OperationID identifies the whole logical replacement so a retry after an
	// infrastructure failure converges instead of minting a second generation.
	OperationID string
}

// ReplaceRecipient snapshots a new recipient and rotates the secret (§5.3).
//
// Rotation is the whole point: the previous recipient's link must stop working
// immediately. Submitted offer versions and their original attribution are
// untouched — the person changed, the history did not.
func (s *Service) ReplaceRecipient(ctx context.Context, companyID, actorUserID string,
	input ReplaceRecipientInput) (SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, input.InvitationID)
	if err != nil {
		return SupplierInvitation{}, err
	}
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return SupplierInvitation{}, ErrInvitationRevoked
	}
	// The revision check is deliberately NOT a service-level rejection here.
	// After a completed replacement the revision has advanced, so pre-checking
	// it would reject the same-operation retry that §5.3A recovery depends on.
	// The authoritative write below is revision-guarded AND operation-ID
	// idempotent, so it converges on a completed replacement and still rejects
	// a genuinely stale one.
	if input.OperationID == "" && invitation.Revision != input.ExpectedRevision {
		return SupplierInvitation{}, ErrRevisionMismatch
	}

	name := strings.TrimSpace(input.RecipientName)
	if name == "" {
		return SupplierInvitation{}, ErrRecipientNameRequired
	}
	normalized, err := NormalizeRecipientEmail(input.RecipientEmail)
	if err != nil {
		return SupplierInvitation{}, err
	}

	// Step one of §5.3A: claim the Supplier's offer workspace BEFORE the
	// authoritative write. The claim keeps holding the unfinished-draft
	// uniqueness slot, so the previous recipient cannot edit, submit or open a
	// new draft during the cross-collection window — and a crash before the
	// invitation write leaves them locked out rather than authoritative again.
	if s.offerWorkspace != nil {
		if err := s.offerWorkspace.PrepareRecipientReplacement(ctx,
			RecipientReplacementPreparation{
				CompanyID:                  companyID,
				InvitationID:               invitation.ID,
				IssuedRFQVersionID:         invitation.CurrentIssuedRFQVersionID,
				PreviousRecipientIdentity:  invitation.RecipientEmailNormalized,
				CandidateRecipientIdentity: normalized,
				ReplacementOperationID:     input.OperationID,
			}); err != nil {
			return SupplierInvitation{}, err
		}
	}

	// The rotation is what invalidates the previous recipient's link. It uses
	// the ACTIVE key version, so a replacement naturally adopts a newer key.
	activeVersion := s.keyring.ActiveVersion()
	raw, err := s.keyring.DeriveInvitationSecret(activeVersion, companyID, invitation.ID,
		invitation.AccessGeneration+1)
	if err != nil {
		return SupplierInvitation{}, err
	}

	// One atomic write (§5.3A). The recipient snapshot and the rotation must
	// move together: applying the snapshot first and rotating second leaves a
	// crash window where the recipient has changed while the previous
	// recipient's link still resolves.
	rotated, err := s.invitations.ReplaceRecipientAtomically(ctx,
		ReplaceRecipientAtomicInput{
			CompanyID:                 companyID,
			InvitationID:              invitation.ID,
			ExpectedRevision:          invitation.Revision,
			PreviousRecipientIdentity: invitation.RecipientEmailNormalized,
			RecipientName:             name,
			RecipientEmail:            strings.TrimSpace(input.RecipientEmail),
			RecipientEmailNormalized:  normalized,
			NewSecretHash:             secrets.HashInvitationSecret(raw),
			SecretKeyVersion:          activeVersion,
			ReplacementOperationID:    input.OperationID,
			ReplacedAt:                time.Now().UTC(),
		})
	if err != nil {
		return SupplierInvitation{}, err
	}

	// Step three of §5.3A: the authoritative write is confirmed, so the held
	// claim becomes archived. A failure here is NOT rolled back — the
	// replacement already stands, and aborting would hand the workspace back to
	// a recipient the contractor replaced. The claim simply stays held until a
	// same-operation retry or reconciliation finishes it.
	if s.offerWorkspace != nil {
		if err := s.offerWorkspace.CompleteRecipientReplacement(ctx,
			RecipientReplacementCompletion{
				CompanyID:                    companyID,
				InvitationID:                 invitation.ID,
				IssuedRFQVersionID:           invitation.CurrentIssuedRFQVersionID,
				PreviousRecipientIdentity:    invitation.RecipientEmailNormalized,
				ReplacementRecipientIdentity: normalized,
				ReplacementOperationID:       input.OperationID,
			}); err != nil {
			return SupplierInvitation{}, ErrRecipientReplacementPending
		}
	}

	_ = s.audit.RecordSupplierInvitationRecipientReplaced(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID, rotated.AccessGeneration)

	// Only older PENDING attempts move. A delivery still queued for the
	// replaced recipient would carry a link the replacement just invalidated;
	// sent and failed rows stay as append-only historical facts.
	if _, err := s.deliveries.ObsoletePendingAttemptsBeforeGeneration(ctx, companyID,
		rotated.ID, rotated.AccessGeneration); err != nil {
		// The invitation write is already authoritative, so an operation-ID
		// retry resolves the same generation and re-drives this idempotent
		// cleanup rather than rotating again.
		return SupplierInvitation{}, err
	}

	return rotated, nil
}

// RotateInvitationSecret rotates the link without changing the recipient (§5.4).
func (s *Service) RotateInvitationSecret(ctx context.Context, companyID, actorUserID,
	invitationID string, expectedRevision int64) (SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return SupplierInvitation{}, err
	}
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return SupplierInvitation{}, ErrInvitationRevoked
	}

	activeVersion := s.keyring.ActiveVersion()
	raw, err := s.keyring.DeriveInvitationSecret(activeVersion, companyID, invitation.ID,
		invitation.AccessGeneration+1)
	if err != nil {
		return SupplierInvitation{}, err
	}

	rotated, err := s.invitations.RotateSecret(ctx, companyID, invitation.ID,
		expectedRevision, secrets.HashInvitationSecret(raw), activeVersion)
	if err != nil {
		return SupplierInvitation{}, err
	}

	_ = s.audit.RecordSupplierInvitationSecretRotated(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID, rotated.AccessGeneration)

	return rotated, nil
}

// RevokeInvitation blocks access immediately while preserving history (§5.4).
//
// Revocation is terminal for the CURRENT ACCESS GENERATION. The stable
// invitation may later be explicitly reactivated, but only by establishing a
// new generation and secret, so a leaked link can never be brought back to
// life.
func (s *Service) RevokeInvitation(ctx context.Context, companyID, actorUserID,
	invitationID string, expectedRevision int64) (SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return SupplierInvitation{}, err
	}

	revokedAt := time.Now()
	updated := invitation
	updated.Status = InvitationStatusRevoked
	updated.RevokedAt = &revokedAt

	saved, err := s.invitations.UpdateInvitation(ctx, companyID, invitationID,
		expectedRevision, updated)
	if err != nil {
		return SupplierInvitation{}, err
	}

	_ = s.audit.RecordSupplierInvitationRevoked(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID)

	return saved, nil
}

// ReactivateInvitationInput carries the explicit reactivation command.
//
// OperationID is distinct from the optimistic revision: the revision chooses
// the state the caller intends to replace, while the operation ID lets an
// uncertain retry resolve the write that already won.
type ReactivateInvitationInput struct {
	InvitationID     string
	ExpectedRevision int64
	ExpiresAt        time.Time
	OperationID      string
}

// ReactivateInvitationCommand is the complete conditional write supplied to
// the invitation repository.
//
// All values needed for the new access generation are selected before the
// write. The repository commits them together so no observer can see an active
// invitation paired with an old generation, hash, key, or RFQ version.
type ReactivateInvitationCommand struct {
	ExpectedRevision          int64
	ExpiresAt                 time.Time
	OperationID               string
	CurrentIssuedRFQVersionID string
	NewAccessSecretHash       string
	SecretKeyVersion          int
	Now                       time.Time
}

// ReactivateInvitation replaces revoked or expired access with one new access
// generation.
//
// The authoritative invitation write happens before delivery cleanup and
// audit. A retry under the same operation ID resolves that write without a
// second increment and safely re-drives the idempotent cleanup if the first
// process stopped between the two stores.
func (s *Service) ReactivateInvitation(ctx context.Context, companyID, actorUserID string,
	input ReactivateInvitationInput) (SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}
	if procurementlimits.ValidateID(strings.TrimSpace(input.OperationID)) != nil {
		return SupplierInvitation{}, ErrOperationIDRequired
	}

	now := time.Now()
	if !input.ExpiresAt.After(now) {
		return SupplierInvitation{}, ErrInvitationExpiryNotInFuture
	}
	if err := procurementlimits.ValidateInvitationExpiry(now,
		input.ExpiresAt); err != nil {
		return SupplierInvitation{}, ErrInvalidBusinessDate
	}

	// This tenant-scoped read supplies the stable chain identity and candidate
	// generation. It is not the concurrency fence; the repository's company +
	// invitation + expected revision filter is authoritative.
	invitation, err := s.invitations.FindInvitation(ctx, companyID, input.InvitationID)
	if err != nil {
		return SupplierInvitation{}, err
	}

	latest, err := s.currentIssuedVersion(ctx, companyID, invitation.RFQChainID)
	if err != nil {
		return SupplierInvitation{}, err
	}

	activeKeyVersion := s.keyring.ActiveVersion()
	raw, err := s.keyring.DeriveInvitationSecret(activeKeyVersion, companyID,
		invitation.ID, invitation.AccessGeneration+1)
	if err != nil {
		return SupplierInvitation{}, err
	}

	reactivated, applied, err := s.invitations.ReactivateInvitation(ctx, companyID,
		invitation.ID, ReactivateInvitationCommand{
			ExpectedRevision:          input.ExpectedRevision,
			ExpiresAt:                 input.ExpiresAt,
			OperationID:               strings.TrimSpace(input.OperationID),
			CurrentIssuedRFQVersionID: latest.ID,
			NewAccessSecretHash:       secrets.HashInvitationSecret(raw),
			SecretKeyVersion:          activeKeyVersion,
			Now:                       now,
		})
	if err != nil {
		return SupplierInvitation{}, err
	}

	// Audit only the winning write. An idempotent retry still performs delivery
	// cleanup below, but must not manufacture a second reactivation event.
	if applied {
		_ = s.audit.RecordSupplierInvitationReactivated(ctx, companyID, actorUserID,
			reactivated.RFQChainID, reactivated.ID,
			reactivated.CurrentIssuedRFQVersionID, reactivated.AccessGeneration)
	}

	// Only stuck PENDING attempts move. Sent and failed rows are append-only
	// historical facts, and reactivation itself creates no delivery record.
	if _, err := s.deliveries.ObsoletePendingAttemptsBeforeGeneration(ctx, companyID,
		reactivated.ID, reactivated.AccessGeneration); err != nil {
		// The invitation write is already authoritative. Returning the error
		// prompts an operation-ID retry, which resolves the same generation and
		// re-drives this idempotent cleanup instead of rotating again.
		return SupplierInvitation{}, err
	}

	return reactivated, nil
}

// UpdateInvitationExpiry extends or shortens the access window (§5.4).
func (s *Service) UpdateInvitationExpiry(ctx context.Context, companyID, actorUserID,
	invitationID string, expectedRevision int64, expiresAt time.Time) (
	SupplierInvitation, error) {

	if !s.invitationsReady() {
		return SupplierInvitation{}, ErrInvitationsNotConfigured
	}
	now := time.Now()
	if !expiresAt.After(now) {
		return SupplierInvitation{}, ErrInvitationExpiryNotInFuture
	}
	if err := procurementlimits.ValidateInvitationExpiry(now, expiresAt); err != nil {
		return SupplierInvitation{}, ErrInvalidBusinessDate
	}

	invitation, err := s.invitations.FindInvitation(ctx, companyID, invitationID)
	if err != nil {
		return SupplierInvitation{}, err
	}
	if invitation.RevokedAt != nil || invitation.Status == InvitationStatusRevoked {
		return SupplierInvitation{}, ErrInvitationRevoked
	}

	updated := invitation
	updated.ExpiresAt = expiresAt

	saved, err := s.invitations.UpdateInvitation(ctx, companyID, invitationID,
		expectedRevision, updated)
	if err != nil {
		return SupplierInvitation{}, err
	}

	_ = s.audit.RecordSupplierInvitationExpiryChanged(ctx, companyID, actorUserID,
		invitation.RFQChainID, invitation.ID)

	return saved, nil
}
