package demoseed

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

type SupplierAccessDriver interface {
	OpenInvitation(ctx context.Context, rawInvitationToken string, openedAt time.Time) (supplieraccess.OpenInvitationResult, error)
	CreateChallenge(ctx context.Context, input supplieraccess.CreateChallengeInput) (supplieraccess.VerificationChallengeResult, error)
	VerifyChallenge(ctx context.Context, input supplieraccess.VerifyChallengeInput) (supplieraccess.VerifyChallengeResult, error)
}

// SupplierOfferDriver is the capability demoseed needs from
// supplieroffers. ListSupplierOfferVersions is checked FIRST on every call
// — the resume signal for "has this Supplier already submitted" is a REAL
// Offer Version existing, never a locally-inferred draft/session state.
type SupplierOfferDriver interface {
	ListSupplierOfferVersions(ctx context.Context, input supplieroffers.SupplierOfferHistoryInput) (supplieroffers.SupplierOfferHistoryPage, error)
	GetSupplierOfferVersion(ctx context.Context, input supplieroffers.SupplierOfferVersionDetailInput) (supplieroffers.SupplierOfferVersionProjection, error)
	CreateOrGetActiveDraft(ctx context.Context, input supplieroffers.SupplierOfferMutationContextInput) (supplieroffers.SupplierOfferDraft, error)
	QuoteDraftLine(ctx context.Context, command supplieroffers.QuoteDraftLineCommand) (supplieroffers.SupplierOfferDraft, error)
	DeclineDraftLine(ctx context.Context, command supplieroffers.DeclineDraftLineCommand) (supplieroffers.SupplierOfferDraft, error)
	SetOfferValidity(ctx context.Context, command supplieroffers.SetOfferValidityCommand) (supplieroffers.SupplierOfferDraft, error)
	SubmitOffer(ctx context.Context, command supplieroffers.SubmitOfferCommand) (supplieroffers.SupplierOfferVersion, error)
}

var demoSupplierClientAddress = netip.MustParseAddr("127.0.0.1")

// createChallengeWithRateLimitBackoff retries once after waiting out the
// identity-scoped verification cooldown (internal/supplieraccess, capped at
// 1 minute) if a rerun of Seed happens to land within it. OpenInvitation
// always mints a fresh exchange, so CreateChallenge's own per-exchange
// idempotency never matches across separate Seed invocations — this is the
// only way to keep "rerun Seed shortly after itself" working without
// weakening the real rate limiter.
func createChallengeWithRateLimitBackoff(ctx context.Context, access SupplierAccessDriver, input supplieraccess.CreateChallengeInput) (supplieraccess.VerificationChallengeResult, error) {
	result, err := access.CreateChallenge(ctx, input)
	var limited *supplieraccess.VerificationRateLimitExceeded
	if err == nil || !errors.As(err, &limited) {
		return result, err
	}
	wait := time.Until(limited.RetryAt)
	if wait <= 0 || wait > 90*time.Second {
		return result, err
	}
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		return result, ctx.Err()
	}
	// The exchange from OpenInvitation is already committed to the first
	// attempt's OperationID (FindChallengeByOperation would otherwise match
	// the original exchange and reject this retry's different one with
	// ErrChallengeOperationConflict), so the retry needs its own distinct ID.
	input.OperationID = input.OperationID + ":retry"
	input.RequestedAt = time.Now().UTC()
	return access.CreateChallenge(ctx, input)
}

// resumeSupplierOfferJourney is the exact same journey as
// SubmitSupplierOffer's happy path once a session exists, factored out so
// the operation-conflict recovery path below can share it instead of
// duplicating the quote/decline/submit steps.
func resumeSupplierOfferJourney(
	ctx context.Context,
	offers SupplierOfferDriver,
	invitation rfqissuance.SupplierInvitation,
	lineUnitPricesMinor map[string]int64,
	operationSlug string,
	sessionToken, csrfToken string,
	now time.Time,
) (supplieroffers.SupplierOfferVersionSummary, error) {
	readCtx := supplieroffers.SupplierOfferReadContextInput{
		SessionToken: sessionToken, InvitationID: invitation.ID, AccessedAt: now,
	}
	mutationCtx := supplieroffers.SupplierOfferMutationContextInput{
		SessionToken: sessionToken, InvitationID: invitation.ID,
		CSRFCookie: csrfToken, CSRFHeader: csrfToken,
		AccessedAt: now,
	}

	// THE RESUME CHECK: an Offer Version already existing for this
	// invitation is the authoritative "already submitted" signal — checked
	// BEFORE any draft is touched, since CreateOrGetActiveDraft would
	// otherwise start a brand-new draft against an invitation whose prior
	// draft was already archived by a successful submission. On the very
	// first submission for an invitation no offer chain exists yet at all,
	// which surfaces as ErrOfferChainNotFound rather than an empty page —
	// that is the expected "nothing submitted yet" signal, not a failure.
	history, err := offers.ListSupplierOfferVersions(ctx, supplieroffers.SupplierOfferHistoryInput{
		SupplierOfferReadContextInput: readCtx, PageSize: 1,
	})
	if err != nil && !errors.Is(err, supplieroffers.ErrOfferChainNotFound) {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: check for an existing offer version: %w", err)
	}
	if err == nil && len(history.Versions) > 0 {
		return history.Versions[0], nil
	}

	draft, err := offers.CreateOrGetActiveDraft(ctx, mutationCtx)
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: create/get active offer draft: %w", err)
	}

	for _, line := range draft.Lines {
		unitPrice, ok := lineUnitPricesMinor[line.RFQLineID]
		if !ok {
			// This supplier does not supply the material on this line.
			// SubmitOffer requires every line to be answered (quoted or
			// explicitly declined) — leaving it unanswered is rejected by
			// validateSubmissionGates with ErrOfferIncomplete.
			updated, err := offers.DeclineDraftLine(ctx, supplieroffers.DeclineDraftLineCommand{
				Context: mutationCtx, DraftID: draft.ID, ExpectedRevision: draft.Revision,
				DraftLineID: line.ID, ResponseStatus: supplieroffers.OfferLineNoBid,
			})
			if err != nil {
				return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: decline draft line %s: %w", line.RFQLineID, err)
			}
			draft = updated
			continue
		}
		updated, err := offers.QuoteDraftLine(ctx, supplieroffers.QuoteDraftLineCommand{
			Context: mutationCtx, DraftID: draft.ID, ExpectedRevision: draft.Revision,
			DraftLineID: line.ID, UnitPriceMinor: unitPrice,
		})
		if err != nil {
			return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: quote draft line %s: %w", line.RFQLineID, err)
		}
		draft = updated
	}

	validityDeadline := now.Add(30 * 24 * time.Hour)
	draft, err = offers.SetOfferValidity(ctx, supplieroffers.SetOfferValidityCommand{
		Context: mutationCtx, DraftID: draft.ID, ExpectedRevision: draft.Revision,
		OfferValidUntil: validityDeadline,
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: set offer validity: %w", err)
	}

	version, err := offers.SubmitOffer(ctx, supplieroffers.SubmitOfferCommand{
		Context: mutationCtx, DraftID: draft.ID, ExpectedRevision: draft.Revision,
		OperationID: OperationID(operationSlug, "offer-submit"),
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: submit offer: %w", err)
	}

	return supplieroffers.SupplierOfferVersionSummary{ID: version.ID, VersionNumber: 1}, nil
}

// SubmitSupplierOffer drives ONE Supplier through the real Milestone 8
// journey, resuming correctly if a prior run already submitted an Offer
// Version for this invitation (design spec §3/§4.3): open invitation,
// request/verify an OTP challenge, obtain a session, THEN check whether a
// Version already exists for this invitation's chain BEFORE touching any
// draft. Only when none exists does it create/resume the draft, quote every
// issued RFQ line at the price supplied in lineUnitPricesMinor, set
// validity, and submit.
//
// OpenInvitation always mints a fresh access exchange (by design — each
// "open" is a new visit), so a second call for the SAME invitation and the
// SAME operationSlug (Seed's operationSlug values are fixed per call site,
// matching this package's idempotency convention) legitimately produces a
// challenge-creation request whose deterministic OperationID collides with
// the challenge already recorded against the FIRST exchange.
// supplieraccess.CreateChallenge correctly rejects that as
// ErrChallengeOperationConflict — that anti-replay behavior must not be
// weakened. What makes THIS call legitimate rather than a replay is that it
// owns the very OperationID being contested (it is retrying its own fixed,
// deterministic slug, not guessing someone else's). recoverSessionAfterChallengeConflict
// below uses the conflict error's named existing challenge to recover a
// fresh session for it via VerifyChallenge, which already treats an
// already-consumed challenge idempotently — no new session-bypass, no
// change to what a real (non-demoseed) caller can do.
func SubmitSupplierOffer(
	ctx context.Context,
	access SupplierAccessDriver,
	offers SupplierOfferDriver,
	invitationKeyring *secrets.InvitationKeyring,
	codeKeyring *secrets.SupplierVerificationCodeKeyring,
	invitation rfqissuance.SupplierInvitation,
	lineUnitPricesMinor map[string]int64,
	operationSlug string,
) (supplieroffers.SupplierOfferVersionSummary, error) {
	now := time.Now().UTC()

	rawInvitationToken, err := invitationKeyring.DeriveInvitationSecret(
		invitation.SecretKeyVersion, invitation.CompanyID, invitation.ID, invitation.AccessGeneration)
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: re-derive invitation token: %w", err)
	}

	opened, err := access.OpenInvitation(ctx, rawInvitationToken, now)
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: open invitation: %w", err)
	}

	challengeOperationID := OperationID(operationSlug, "challenge")
	challenge, err := createChallengeWithRateLimitBackoff(ctx, access, supplieraccess.CreateChallengeInput{
		ExchangeToken: opened.ExchangeToken, OperationID: challengeOperationID,
		ClientAddress: demoSupplierClientAddress, RequestedAt: now,
	})
	var conflict *supplieraccess.ChallengeOperationConflictError
	if errors.As(err, &conflict) {
		return recoverSessionAfterChallengeConflict(ctx, access, offers, codeKeyring,
			invitation, lineUnitPricesMinor, operationSlug, challengeOperationID,
			conflict.ExistingChallengeID, now)
	}
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: create verification challenge: %w", err)
	}

	code, err := codeKeyring.DeriveCode(invitation.SecretKeyVersion, secrets.SupplierVerificationCodeContext{
		ChallengeID: challenge.ChallengeID, CompanyID: invitation.CompanyID,
		SupplierID: invitation.SupplierID, InvitationID: invitation.ID,
		AccessGeneration: invitation.AccessGeneration, NormalizedRecipientEmail: invitation.RecipientEmailNormalized,
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: re-derive OTP code: %w", err)
	}

	verified, err := access.VerifyChallenge(ctx, supplieraccess.VerifyChallengeInput{
		ChallengeID: challenge.ChallengeID, Code: code, OperationID: challengeOperationID,
		VerifiedAt: now,
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: verify OTP challenge: %w", err)
	}

	return resumeSupplierOfferJourney(ctx, offers, invitation, lineUnitPricesMinor,
		operationSlug, verified.SessionToken, verified.CSRFToken, now)
}

// recoverSessionAfterChallengeConflict re-derives the OTP code for the
// challenge that already owns challengeOperationID (a pure, deterministic
// HMAC derivation keyed by that challenge's own ID — the same code that was
// valid when the challenge was first created) and verifies it. VerifyChallenge
// already handles an already-consumed challenge idempotently
// (recoverVerificationSession), returning a fresh session for it regardless
// of how long ago it was first verified, so this never mints a second
// challenge and never touches supplieraccess's anti-replay checks.
func recoverSessionAfterChallengeConflict(
	ctx context.Context,
	access SupplierAccessDriver,
	offers SupplierOfferDriver,
	codeKeyring *secrets.SupplierVerificationCodeKeyring,
	invitation rfqissuance.SupplierInvitation,
	lineUnitPricesMinor map[string]int64,
	operationSlug, challengeOperationID, existingChallengeID string,
	now time.Time,
) (supplieroffers.SupplierOfferVersionSummary, error) {
	code, err := codeKeyring.DeriveCode(invitation.SecretKeyVersion, secrets.SupplierVerificationCodeContext{
		ChallengeID: existingChallengeID, CompanyID: invitation.CompanyID,
		SupplierID: invitation.SupplierID, InvitationID: invitation.ID,
		AccessGeneration: invitation.AccessGeneration, NormalizedRecipientEmail: invitation.RecipientEmailNormalized,
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: re-derive OTP code for existing challenge: %w", err)
	}

	verified, err := access.VerifyChallenge(ctx, supplieraccess.VerifyChallengeInput{
		ChallengeID: existingChallengeID, Code: code, OperationID: challengeOperationID,
		VerifiedAt: now,
	})
	if err != nil {
		return supplieroffers.SupplierOfferVersionSummary{}, fmt.Errorf("demoseed: recover session for existing challenge: %w", err)
	}

	return resumeSupplierOfferJourney(ctx, offers, invitation, lineUnitPricesMinor,
		operationSlug, verified.SessionToken, verified.CSRFToken, now)
}
