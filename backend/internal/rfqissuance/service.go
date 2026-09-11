package rfqissuance

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

// IssuedVersionStore is the immutable-version repository surface.
//
// There is no update method by design: a correction is a NEW version
// (design spec §3.2).
type IssuedVersionStore interface {
	// HasIssuedVersion reports whether companyID's chain has at least one
	// immutable issued version. The company is part of the query, never a
	// post-filter: another tenant's issued version must never answer this
	// tenant's question.
	HasIssuedVersion(ctx context.Context, companyID, rfqChainID string) (bool, error)

	CreateVersion(ctx context.Context, version IssuedRFQVersion) (IssuedRFQVersion, error)
	FindVersion(ctx context.Context, companyID, versionID string) (IssuedRFQVersion, error)
	ListVersions(ctx context.Context, companyID, rfqChainID string) ([]IssuedRFQVersion, error)

	// FindByOperationID resolves the version a previous attempt created under
	// this idempotency key, which is what lets an interrupted caller recover
	// rather than merely fail (design spec §10.1).
	FindByOperationID(ctx context.Context, companyID, operationID string) (
		IssuedRFQVersion, bool, error)
}

// IssuanceChainStore is the serialization point for version creation
// (design spec §3.1, §10.1).
type IssuanceChainStore interface {
	EnsureChain(ctx context.Context, companyID, rfqChainID string) (RFQIssuanceChain, error)
	AdvanceChain(ctx context.Context, companyID, rfqChainID string, expectedRevision int64,
		versionNumber int, issuedVersionID string) (RFQIssuanceChain, error)
	FindChain(ctx context.Context, companyID, rfqChainID string) (RFQIssuanceChain, error)
}

// AmendmentDraftStore holds the ONE mutable document in this module.
type AmendmentDraftStore interface {
	CreateDraft(ctx context.Context, draft RFQAmendmentDraft) (RFQAmendmentDraft, error)
	FindDraft(ctx context.Context, companyID, rfqChainID string) (RFQAmendmentDraft, error)
	UpdateDraft(ctx context.Context, companyID, rfqChainID string, expectedRevision int64,
		updated RFQAmendmentDraft) (RFQAmendmentDraft, error)
	DeleteDraft(ctx context.Context, companyID, rfqChainID string, expectedRevision int64) error
}

// Service is the rfqissuance application service.
type Service struct {
	versions IssuedVersionStore
	chains   IssuanceChainStore
	drafts   AmendmentDraftStore
	readyM7  ReadyRFQSource

	// Invitation collaborators (§5). Optional for the same reason as the M7
	// source: the composition root wires this service before every adapter
	// exists, and RFQChainHasIssuedVersion must work from the version store
	// alone.
	invitations         InvitationStore
	deliveries          DeliveryAttemptStore
	suppliers           SupplierLookup
	keyring             *secrets.InvitationKeyring
	mailer              Mailer
	supplierLinkBaseURL string
	audit               AuditRecorder
	supplierRFQAccess   SupplierRFQAccessAuthorizer

	// offerWorkspace serializes recipient replacement against Supplier draft
	// activity (§5.3A). It is set after construction because supplieroffers
	// already depends on this module, forming a cycle.
	offerWorkspace OfferWorkspaceCoordinator

	// logger records identifiers and outcomes only. A raw token, a derived link
	// and provider error text are never passed to it (§6.1A, §15).
	logger *zerolog.Logger
}

// logEvent returns a log event at info level, or a no-op when no logger is
// wired. Returning a live event unconditionally keeps every call site free of
// nil checks.
func (s *Service) logEvent(msg string) *zerolog.Event {
	if s.logger == nil {
		nop := zerolog.Nop()
		return nop.Info()
	}
	return s.logger.Info().Str("module", "rfqissuance").Str("event", msg)
}

// Option configures optional Service collaborators.
//
// The M7 source and the chain store are options rather than constructor
// arguments because the composition root wires this service BEFORE the M7
// adapter exists — the two form a cycle that is closed by
// rfqs.SetIssuanceStatusSource (design spec §1A.3). Keeping them optional lets
// RFQChainHasIssuedVersion, the only capability M7 needs back, work from the
// version store alone.
type Option func(*Service)

// WithReadyRFQSource supplies the M7 ready-RFQ capability.
func WithReadyRFQSource(source ReadyRFQSource) Option {
	return func(s *Service) { s.readyM7 = source }
}

// WithIssuanceChains supplies the issuance chain store.
func WithIssuanceChains(chains IssuanceChainStore) Option {
	return func(s *Service) { s.chains = chains }
}

// WithAmendmentDrafts supplies the amendment draft store.
func WithAmendmentDrafts(drafts AmendmentDraftStore) Option {
	return func(s *Service) { s.drafts = drafts }
}

// WithAuditRecorder supplies the module's primitive-only audit capability.
func WithAuditRecorder(audit AuditRecorder) Option {
	return func(s *Service) { s.audit = audit }
}

// WithSupplierRFQAccessAuthorizer supplies the narrow Phase D capability that
// turns a Supplier session into tenant and invitation scope. Keeping that
// verification behind an interface prevents this owner module from importing
// supplieraccess or learning how its cookies are encoded.
func WithSupplierRFQAccessAuthorizer(authorizer SupplierRFQAccessAuthorizer) Option {
	return func(s *Service) { s.supplierRFQAccess = authorizer }
}

// SetSupplierRFQAccessAuthorizer closes the invitation/session composition
// cycle after both services exist. supplieraccess already validates invitations
// through rfqissuance, while these reads validate sessions through
// supplieraccess, so constructor-only wiring would create a package cycle.
func (s *Service) SetSupplierRFQAccessAuthorizer(authorizer SupplierRFQAccessAuthorizer) {
	s.supplierRFQAccess = authorizer
}

// SetOfferWorkspaceCoordinator closes the M8 issuance/offers cycle.
//
// supplieroffers already consumes rfqissuance through IssuedRFQSource, so this
// capability cannot be a constructor argument without creating an unresolvable
// wiring cycle. It follows the same setter pattern as
// rfqs.SetIssuanceStatusSource (Decision C).
func (s *Service) SetOfferWorkspaceCoordinator(coordinator OfferWorkspaceCoordinator) {
	s.offerWorkspace = coordinator
}

// NewService constructs the service over its repositories.
func NewService(versions IssuedVersionStore, opts ...Option) *Service {
	svc := &Service{versions: versions, audit: noOpAuditRecorder{}}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of any of the five public repository interfaces. Only the real
// Mongo repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every IssuedRFQVersion,
// IssuanceChain, RFQAmendmentDraft, SupplierInvitation, AND
// DeliveryAttempt owned by companyID — all five collections, one method,
// per Task 1a's multi-collection guidance. invitations/deliveries are
// skipped (not an error) if this Service was constructed without them
// (WithInvitations/WithDeliveryAttempts are optional — see the Service
// struct's own doc comment), since demoseed always constructs the full
// composition root and this only matters for narrower test construction.
// Development-tool use only (demoseed reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	versionDeleter, ok := s.versions.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("rfqissuance: version repository %T does not support DeleteAllForCompany", s.versions)
	}
	if err := versionDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	if s.chains != nil {
		chainDeleter, ok := s.chains.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("rfqissuance: chain repository %T does not support DeleteAllForCompany", s.chains)
		}
		if err := chainDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	if s.drafts != nil {
		draftDeleter, ok := s.drafts.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("rfqissuance: draft repository %T does not support DeleteAllForCompany", s.drafts)
		}
		if err := draftDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	if s.invitations != nil {
		invitationDeleter, ok := s.invitations.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("rfqissuance: invitation repository %T does not support DeleteAllForCompany", s.invitations)
		}
		if err := invitationDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	if s.deliveries != nil {
		deliveryDeleter, ok := s.deliveries.(companyBulkDeleter)
		if !ok {
			return fmt.Errorf("rfqissuance: delivery repository %T does not support DeleteAllForCompany", s.deliveries)
		}
		if err := deliveryDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
			return err
		}
	}

	return nil
}

// RFQChainHasIssuedVersion reports whether a chain has been issued at least
// once. It is the fact rfqs consumes through its own IssuanceStatusSource
// interface, wired by the composition adapter and setter.
//
// The error is returned rather than absorbed into a false result. rfqs maps any
// error to ErrIssuanceStatusUnavailable and refuses to reopen; reporting a
// failed lookup as "not issued" would let a contractor reopen the M7 draft
// underneath a Supplier who is already quoting against the issued version.
func (s *Service) RFQChainHasIssuedVersion(ctx context.Context,
	companyID, rfqChainID string) (bool, error) {

	issued, err := s.versions.HasIssuedVersion(ctx, companyID, rfqChainID)
	if err != nil {
		return false, err
	}
	return issued, nil
}

// IssueVersionInput carries one issuance request.
//
// OperationID is the caller-supplied idempotency key. Without it a retry after
// an uncertain response is indistinguishable from a deliberate second issuance,
// and the Supplier would receive two versions where the contractor intended one.
type IssueVersionInput struct {
	RFQChainID  string
	Currency    string
	OperationID string
}

// readySnapshotForIssuance centralises the fail-closed M7 read. Both a new
// issuance and an idempotent retry use it: a retry cannot claim equivalence
// unless the current ready source still has the exact persisted identity.
func (s *Service) readySnapshotForIssuance(ctx context.Context,
	companyID, rfqChainID string) (ReadyRFQSnapshot, error) {

	// companyID is the AUTHENTICATED tenant, never a caller-supplied value.
	snapshot, found, err := s.readyM7.GetReadyRFQSnapshot(ctx, companyID, rfqChainID)
	if err != nil {
		// Propagated, not collapsed into "not ready": a lookup failure must not
		// silently refuse an RFQ that is in fact issuable.
		return ReadyRFQSnapshot{}, err
	}
	if !found {
		return ReadyRFQSnapshot{}, ErrRFQNotReady
	}
	if snapshot.ResponseDeadline == nil {
		// §4.1A: the platform must not invent a commercial deadline.
		return ReadyRFQSnapshot{}, ErrResponseDeadlineRequired
	}
	return snapshot, nil
}

// initialIssuanceMatches verifies that an operation-ID hit is the exact logical
// request being retried. The company match is guaranteed by the repository
// query; every remaining identity component is checked here.
func initialIssuanceMatches(existing IssuedRFQVersion, input IssueVersionInput,
	snapshot ReadyRFQSnapshot) bool {

	return existing.RFQChainID == input.RFQChainID &&
		existing.VersionNumber == 1 &&
		existing.Currency == input.Currency &&
		existing.SourceM7RFQRevision == snapshot.SourceM7RFQRevision &&
		existing.SourceFingerprint == fingerprintReadyRFQSnapshot(snapshot)
}

// IssueVersion creates the next immutable issued version from the M7 ready RFQ
// (design spec §4.1, §10.1).
//
// The write order is the recovery model:
//
//	resolve idempotency → validate → read chain candidate
//	→ create immutable version → advance chain pointer
//
// Validation happens BEFORE anything is written, so a refusal — a missing
// deadline, an absent currency, a non-ready RFQ — leaves no chain and no
// version behind.
//
// The candidate number is chain.LatestIssuedVersion + 1, but allocation becomes
// authoritative only when the immutable version insert wins the named
// company+chain+version unique index. Two concurrent callers therefore contend
// for the SAME number and exactly one wins. Incrementing a counter before the
// insert would hand them different numbers and let both publish the same M7
// snapshot as Versions 1 and 2.
//
// If the process dies after the version is created but before the pointer
// advances, the version is the authoritative record and reconciliation
// completes the pointer. The reverse order would leave a pointer addressing a
// version that does not exist.
func (s *Service) IssueVersion(ctx context.Context, companyID, actorUserID string,
	input IssueVersionInput) (IssuedRFQVersion, error) {

	if procurementlimits.ValidateID(strings.TrimSpace(input.OperationID)) != nil {
		return IssuedRFQVersion{}, ErrOperationIDRequired
	}
	// Currency is part of idempotent commercial identity, so normalise it once
	// before either comparing an existing operation or persisting a new one.
	input.Currency = strings.ToUpper(strings.TrimSpace(input.Currency))
	if input.Currency == "" {
		return IssuedRFQVersion{}, ErrCurrencyRequired
	}
	if s.readyM7 == nil || s.chains == nil {
		return IssuedRFQVersion{}, ErrIssuanceNotConfigured
	}

	// Idempotency first: a retry must resolve to its original result rather
	// than re-running the flow and colliding on the version number.
	if existing, found, err := s.versions.FindByOperationID(ctx, companyID,
		input.OperationID); err != nil {
		return IssuedRFQVersion{}, err
	} else if found {
		// The company-scoped unique key proves only that this operation ID was
		// used somewhere. It is a valid retry only when it names this exact
		// initial issuance; returning another chain's version would turn a
		// conflict into a false success.
		if existing.RFQChainID != input.RFQChainID ||
			existing.VersionNumber != 1 ||
			existing.Currency != input.Currency {
			return IssuedRFQVersion{}, ErrOperationAlreadyUsed
		}
		snapshot, sourceErr := s.readySnapshotForIssuance(ctx, companyID, input.RFQChainID)
		if sourceErr != nil {
			return IssuedRFQVersion{}, sourceErr
		}
		if !initialIssuanceMatches(existing, input, snapshot) {
			return IssuedRFQVersion{}, ErrOperationAlreadyUsed
		}
		// The first attempt may have inserted the immutable version and died
		// before advancing the chain. Identity has been verified above, so this
		// retry may safely complete that known transition. The version remains
		// the authoritative result even if the repair itself is temporarily
		// unavailable.
		_, _ = s.ReconcileIssuanceChain(
			ctx, companyID, actorUserID, input.RFQChainID)
		return existing, nil
	}

	// Phase 1 has one supported commercial currency. This validation stays
	// after operation lookup so reusing a consumed idempotency key for a
	// different currency remains an identity conflict, not a new request.
	if input.Currency != "MYR" {
		return IssuedRFQVersion{}, ErrInvalidCurrency
	}

	snapshot, err := s.readySnapshotForIssuance(ctx, companyID, input.RFQChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}

	lines := make([]IssuedRFQLine, 0, len(snapshot.Lines))
	for _, l := range snapshot.Lines {
		line, err := NewIssuedLineFromM7(l)
		if err != nil {
			return IssuedRFQVersion{}, err
		}
		lines = append(lines, line)
	}

	chain, err := s.chains.EnsureChain(ctx, companyID, input.RFQChainID)
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	if chain.LatestIssuedVersion != 0 || chain.CurrentIssuedVersionID != nil {
		// Version 1 is the only version sourced directly from M7. Every later
		// version must come from a reviewed amendment draft — UNLESS this is
		// actually a concurrent retry of the SAME operation racing itself:
		// two callers can both observe chain.LatestIssuedVersion == 0 here
		// (the chain pointer only advances AFTER the version insert, per
		// this function's own write-order comment above), so the loser must
		// not be told "version already exists" as a hard domain conflict —
		// it must resolve via the identical idempotency recovery the real
		// CreateVersion collision path below already uses, matching that
		// path's own established contract ("resolve the winner by operation
		// ID... a different operation still receives the conflict").
		existing, found, findErr := s.versions.FindByOperationID(ctx, companyID, input.OperationID)
		if findErr != nil {
			return IssuedRFQVersion{}, findErr
		}
		if found {
			snapshot, sourceErr := s.readySnapshotForIssuance(ctx, companyID, input.RFQChainID)
			if sourceErr != nil {
				return IssuedRFQVersion{}, sourceErr
			}
			if !initialIssuanceMatches(existing, input, snapshot) {
				return IssuedRFQVersion{}, ErrOperationAlreadyUsed
			}
			_, _ = s.ReconcileIssuanceChain(ctx, companyID, actorUserID, input.RFQChainID)
			return existing, nil
		}
		return IssuedRFQVersion{}, ErrVersionAlreadyExists
	}

	version, err := NewIssuedVersion(NewIssuedVersionInput{
		CompanyID: companyID, ProjectID: snapshot.ProjectID,
		RFQChainID: input.RFQChainID, RFQNumber: snapshot.RFQNumber,
		VersionNumber: chain.LatestIssuedVersion + 1, Currency: input.Currency,
		Title: snapshot.Title, DeliveryAddress: snapshot.DeliveryAddress,
		RequiredByDate:       snapshot.RequiredByDate,
		ResponseDeadline:     snapshot.ResponseDeadline,
		SupplierInstructions: snapshot.SupplierInstructions,
		Lines:                lines,
		// The source revision is provenance, not an optimistic lock. It tells
		// reconciliation and audit exactly which ready M7 snapshot became this
		// immutable external record.
		SourceM7RFQRevision: snapshot.SourceM7RFQRevision,
		SourceFingerprint:   fingerprintReadyRFQSnapshot(snapshot),
		IssuanceOperationID: input.OperationID,
		IssuedByUserID:      actorUserID,
		IssuedAt:            time.Now(),
	})
	if err != nil {
		return IssuedRFQVersion{}, err
	}

	created, err := s.versions.CreateVersion(ctx, version)
	if err != nil {
		// Two concurrent retries may both miss FindByOperationID before either
		// inserts. Mongo chooses one winner. Once the loser observes the unique
		// collision, resolve the winner by operation ID and return it only after
		// the same exact identity check; a different operation that merely lost
		// the version-number race still receives the conflict.
		if errors.Is(err, ErrVersionAlreadyExists) ||
			errors.Is(err, ErrOperationAlreadyUsed) {
			existing, found, findErr := s.versions.FindByOperationID(
				ctx, companyID, input.OperationID)
			if findErr != nil {
				return IssuedRFQVersion{}, findErr
			}
			if found {
				if !initialIssuanceMatches(existing, input, snapshot) {
					return IssuedRFQVersion{}, ErrOperationAlreadyUsed
				}
				_, _ = s.ReconcileIssuanceChain(
					ctx, companyID, actorUserID, input.RFQChainID)
				return existing, nil
			}
		}
		return IssuedRFQVersion{}, err
	}

	// The immutable record already exists, so audit is best-effort and cannot
	// roll back or disguise the successful domain transition.
	_ = s.audit.RecordRFQVersionIssued(
		ctx, companyID, created.ProjectID, actorUserID,
		created.RFQChainID, created.RFQNumber, created.ID, created.VersionNumber)

	// A failed pointer advance is a RECONCILABLE GAP, never a reason to report
	// failure: the immutable version already exists, and a Supplier may already
	// be able to reach it. Deleting or hiding it would be the actual data loss.
	// Reconciliation completes the pointer later (design spec §10.1, §10.7).
	_, _ = s.chains.AdvanceChain(ctx, companyID, input.RFQChainID, chain.Revision,
		created.VersionNumber, created.ID)

	return created, nil
}
