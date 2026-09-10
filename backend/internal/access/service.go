package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// --- Service-level sentinels (design spec §15) ---

var (
	// ErrQuotationNotFound is returned when the referenced Quotation does not
	// belong to the caller's company.
	ErrQuotationNotFound = errors.New("access: quotation not found")

	// ErrQuotationNotFinalized is returned when sharing a draft Quotation.
	ErrQuotationNotFinalized = errors.New("access: quotation must be finalized before it can be shared")

	// ErrQuotationExpiredForShare is returned when sharing a Quotation whose
	// ValidUntil has already passed (approved product decision §6.2).
	ErrQuotationExpiredForShare = errors.New("access: quotation has already expired and cannot be shared")

	// ErrQuotationChainAlreadyAccepted is returned when sharing ANY version of
	// a chain that already has an accepted version.
	ErrQuotationChainAlreadyAccepted = errors.New("access: this quotation has already been accepted; no other version may be shared")

	// ErrQuotationSuperseded is returned when sharing a version older than the
	// chain's current one, or when an in-flight decision loses to a newer share.
	ErrQuotationSuperseded = errors.New("access: a newer version of this quotation is now active")

	// ErrActiveGrantAlreadyExistsForGroup is returned when a coordinator claim
	// loses a race. Never retried — the caller should re-read and decide.
	ErrActiveGrantAlreadyExistsForGroup = errors.New("access: another share or rotation is already active for this quotation")

	// ErrGrantSuperseded is returned when rotating a grant that is superseded,
	// already rotated, or does not target the chain's current version.
	ErrGrantSuperseded = errors.New("access: this grant is no longer eligible for rotation")

	// ErrGrantNotActive is returned when editing expiry on a grant that is not
	// active-and-unexpired.
	ErrGrantNotActive = errors.New("access: grant is not active")

	// ErrExternalTokenUnusable is the SINGLE uniform sentinel for every
	// external-token failure mode — malformed, unknown, expired, revoked,
	// rotated, superseded, or orphaned. Deliberately collapsed so the external
	// response leaks no information about which condition failed (§2.3/§16.2).
	ErrExternalTokenUnusable = errors.New("access: this link is no longer active")

	// ErrQuotationExpiredForDecision is returned when a Client submits a
	// decision after the Quotation's commercial ValidUntil has passed.
	ErrQuotationExpiredForDecision = errors.New("access: this quotation has expired and can no longer be acted upon")

	// ErrDecisionAlreadyAccepted is returned for a conflicting decision after
	// a terminal acceptance.
	ErrDecisionAlreadyAccepted = errors.New("access: this quotation has already been accepted")

	// Client input validation (approved product decision §7.5).
	ErrCommentRequired = errors.New("access: a comment is required for this decision")
	ErrFieldTooLong    = errors.New("access: a submitted field exceeds its maximum length")
	ErrInvalidEmail    = errors.New("access: the supplied email address is not valid")
)

// Field limits (approved product decision §7.5).
const (
	MaxClientNameLength  = 200
	MaxClientEmailLength = 320
	MaxCommentLength     = 2000
)

// DefaultGrantValidity is used when a Quotation carries no ValidUntil
// (approved product decision §6.2). Every grant always has a concrete
// expiry — never-expiring grants are not supported.
const DefaultGrantValidity = 30 * 24 * time.Hour

// --- Consumer-defined capability interfaces (ADR 0002) ---

// ShareableQuotationLine is the per-line projection access needs. It carries
// no internal Mongo line ID and no SourceWorkItemIDs — neither field exists
// on this type, so no code path in access can leak them.
type ShareableQuotationLine struct {
	Description string
	Quantity    *decimal.Decimal
	Unit        *string
	UnitPrice   *money.Money
	Amount      money.Money
}

// ShareableQuotationSnapshot is owned by access (the consumer) and built by a
// composition-root adapter from quotations.Quotation. It contains exactly the
// fields M6 needs and structurally EXCLUDES EstimateID, GeneratedSubtotal,
// Notes, Revision, SourceWorkItemIDs, internal line IDs, and every cost/
// margin/markup/profit value (design spec §14).
type ShareableQuotationSnapshot struct {
	QuotationID     string
	CompanyID       string
	ProjectID       string
	ClientID        string
	QuotationNumber string
	Version         int
	Status          string
	Currency        string
	Lines           []ShareableQuotationLine
	Subtotal        money.Money
	TaxMode         string
	TaxLabel        string
	TaxRateBPS      money.RateBPS
	TaxAmount       money.Money
	Total           money.Money
	Terms           string
	PaymentSchedule string
	ValidUntil      *time.Time
}

// QuotationSource is satisfied via a composition-root adapter, since the
// return type is owned by access rather than by quotations (design spec §14).
type QuotationSource interface {
	GetFinalizedQuotationForShare(ctx context.Context, companyID, quotationID string) (ShareableQuotationSnapshot, bool, error)
}

// ProjectStatusUpdater carries only primitives, so projects.Service satisfies
// it structurally with no adapter.
type ProjectStatusUpdater interface {
	AdvanceProjectToQuotationSent(ctx context.Context, companyID, projectID string) (changed bool, found bool, err error)
	AdvanceProjectToQuotationApproved(ctx context.Context, companyID, projectID string) (changed bool, found bool, err error)
	GetProjectName(ctx context.Context, companyID, projectID string) (name string, found bool, err error)
}

// DecisionRecorder carries only primitives; satisfied via a thin adapter over
// approvals.Service.
type DecisionRecorder interface {
	RecordDecision(ctx context.Context, companyID, subjectType, subjectID, subjectGroupKey, actorName, actorEmail, comment, status, accessGrantID string) (approvalID string, decidedAt time.Time, isNewDecision bool, err error)
	ReconcileAccepted(ctx context.Context, companyID, subjectType, subjectID, subjectGroupKey, actorName, actorEmail, comment, accessGrantID string) (approvalID string, decidedAt time.Time, err error)
	GetDecision(ctx context.Context, companyID, subjectType, subjectID string) (status, comment, actorName, actorEmail string, decidedAt time.Time, found bool, err error)
}

// CompanyNameLookup is satisfied structurally by companies.Service.
type CompanyNameLookup interface {
	GetCompanyName(ctx context.Context, companyID string) (string, error)
}

// AuditRecorder mirrors audit.AuditRecorder. Declared here as the consumer's
// view so access imports no other domain module; audit.Service satisfies it
// structurally because every parameter is a primitive.
type AuditRecorder interface {
	RecordQuotationSent(ctx context.Context, companyID, projectID, actorUserID, quotationID, quotationNumber string, version int) error
	RecordAccessCreated(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber string, version int) error
	RecordAccessRevoked(ctx context.Context, companyID, projectID, actorUserID, grantID, quotationID, quotationNumber, revokedReason string, version int) error
	RecordClientViewedQuotation(ctx context.Context, companyID, projectID, grantID, quotationID, quotationNumber string, version int) error
	RecordClientDecision(ctx context.Context, companyID, projectID, grantID, approvalID, quotationID, quotationNumber string, version int, decisionStatus, clientName, clientEmail string) error
}

// CleanupLogger receives best-effort cleanup failures. A failed stored-status
// cleanup is never authoritative — the coordinator has already switched — but
// it must be visible for later reconciliation (approved addendum). Neither
// raw token is ever passed here.
type CleanupLogger interface {
	LogCleanupFailure(ctx context.Context, operation, companyID, grantID string, err error)
}

// --- Service ---

// Service implements the M6 coordinator protocols. Every cross-grant
// operation funnels through a single conditional write against the chain's
// AccessGroupState document, which is what makes share/rotate/accept
// mutually exclusive across two collections without a Mongo transaction.
type Service struct {
	grants         AccessGrantRepository
	groups         AccessGroupStateRepository
	quotations     QuotationSource
	projects       ProjectStatusUpdater
	decisions      DecisionRecorder
	companies      CompanyNameLookup
	audit          AuditRecorder
	cleanupLogger  CleanupLogger
	externalAPIURL string
}

// NewService constructs a Service. externalAPIBaseURL is used to build the
// development/integration API URL returned alongside a freshly minted token —
// it is explicitly NOT a finished Client portal link (design spec §4.3).
func NewService(
	grants AccessGrantRepository,
	groups AccessGroupStateRepository,
	quotationSource QuotationSource,
	projectStatus ProjectStatusUpdater,
	decisions DecisionRecorder,
	companies CompanyNameLookup,
	auditRecorder AuditRecorder,
	cleanupLogger CleanupLogger,
	externalAPIBaseURL string,
) *Service {
	return &Service{
		grants: grants, groups: groups, quotations: quotationSource,
		projects: projectStatus, decisions: decisions, companies: companies,
		audit: auditRecorder, cleanupLogger: cleanupLogger,
		externalAPIURL: strings.TrimRight(externalAPIBaseURL, "/"),
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of either public repository interface. Only the real Mongo
// repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every AccessGrant AND
// AccessGroupState owned by companyID — both collections, one method, per
// Task 1a's multi-collection guidance. Development-tool use only (demoseed
// reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	grantDeleter, ok := s.grants.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("access: grant repository %T does not support DeleteAllForCompany", s.grants)
	}
	if err := grantDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	groupDeleter, ok := s.groups.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("access: group state repository %T does not support DeleteAllForCompany", s.groups)
	}
	return groupDeleter.DeleteAllForCompany(ctx, companyID)
}

// GrantView is the contractor-facing projection of a grant, combining the
// grant's own stored state with the coordinator's authoritative view.
type GrantView struct {
	GrantID         string
	QuotationID     string
	StoredStatus    string
	IsExpired       bool
	EffectiveStatus string
	ExpiresAt       time.Time
	RevokedReason   string
	Revision        int64
	CreatedAt       time.Time

	// RawToken and URL are populated ONLY when this view describes a grant
	// whose token was just minted by the current request (design spec §6.6).
	RawToken string
	URL      string

	// Decision is the current Client decision for this Quotation version.
	DecisionStatus     string
	DecisionComment    string
	DecisionClientName string
	DecisionDecidedAt  time.Time
	HasDecision        bool
}

// externalURLFor builds the development/integration API URL for a raw token.
func (s *Service) externalURLFor(rawToken string) string {
	return s.externalAPIURL + "/client/quotations/" + rawToken
}

// ShareQuotation implements the share decision table of design spec §6.1,
// with the candidate-insert-then-coordinator-claim write order of §2.4.
func (s *Service) ShareQuotation(ctx context.Context, companyID, quotationID, actorUserID string) (GrantView, bool, error) {
	snapshot, found, err := s.quotations.GetFinalizedQuotationForShare(ctx, companyID, quotationID)
	if err != nil {
		return GrantView{}, false, err
	}
	if !found {
		return GrantView{}, false, ErrQuotationNotFound
	}
	if snapshot.Status != "finalized" {
		return GrantView{}, false, ErrQuotationNotFinalized
	}

	now := time.Now()
	expiresAt, err := grantExpiryFor(snapshot, now)
	if err != nil {
		return GrantView{}, false, err
	}

	groupKey := ResourceGroupKeyForQuotation(snapshot.QuotationNumber)
	coordinator, err := s.groups.FindOrCreate(ctx, companyID, ResourceTypeQuotation, groupKey, quotationID, now)
	if err != nil {
		return GrantView{}, false, err
	}

	// Accepted chains are terminal for sharing — any version, including the
	// accepted one (design spec §9.1).
	if coordinator.AcceptedResourceID != nil {
		return GrantView{}, false, ErrQuotationChainAlreadyAccepted
	}

	// Same version already active -> idempotent 200 with no url/token.
	if coordinator.CurrentResourceID == quotationID && coordinator.ActiveGrantID != nil {
		existing, findErr := s.grants.FindByID(ctx, companyID, *coordinator.ActiveGrantID)
		if findErr != nil && findErr != ErrGrantNotFound {
			return GrantView{}, false, findErr
		}
		if findErr == nil && existing.Status == AccessGrantStatusActive {
			// Retry the Project projection on every repeat share so a prior
			// best-effort failure reconciles (design spec §8.3).
			s.advanceProjectToSent(ctx, companyID, snapshot.ProjectID)
			return s.buildGrantView(ctx, existing, coordinator, "", ""), false, nil
		}
		// The coordinator references a dead grant — self-heal by falling
		// through to create a replacement (design spec §6.1).
	}

	// A version older than (or equal to) the chain's current one can never be
	// re-shared once the chain has moved on. Version numbers within one
	// QuotationNumber chain are monotonically increasing (guaranteed by M5's
	// CreateNewVersion), so comparing the requested version against the
	// chain's current version is the correct staleness test.
	if coordinator.CurrentResourceID != quotationID {
		currentSnapshot, currentFound, curErr := s.quotations.GetFinalizedQuotationForShare(ctx, companyID, coordinator.CurrentResourceID)
		if curErr != nil {
			return GrantView{}, false, curErr
		}
		if currentFound && snapshot.Version <= currentSnapshot.Version {
			return GrantView{}, false, ErrQuotationSuperseded
		}
	}

	previousActiveGrantID := coordinator.ActiveGrantID
	isSupersession := coordinator.CurrentResourceID != quotationID && previousActiveGrantID != nil
	isFirstShareOfChain := previousActiveGrantID == nil && coordinator.CurrentResourceID == quotationID

	view, err := s.mintAndClaim(ctx, mintRequest{
		companyID: companyID, snapshot: snapshot, coordinator: coordinator,
		expectedActiveGrantID: previousActiveGrantID,
		expectedCurrentID:     coordinator.CurrentResourceID,
		newCurrentID:          quotationID,
		requireAcceptedNil:    true,
		expiresAt:             expiresAt,
		actorUserID:           actorUserID,
	})
	if err != nil {
		return GrantView{}, false, err
	}

	// Post-claim, in order (design spec §2.4 step 4).
	if isSupersession && previousActiveGrantID != nil {
		s.bestEffortRevoke(ctx, companyID, *previousActiveGrantID, RevokedReasonSuperseded,
			snapshot, actorUserID)
	}
	if isFirstShareOfChain || isSupersession {
		s.bestEffortAudit(func() error {
			return s.audit.RecordQuotationSent(ctx, companyID, snapshot.ProjectID, actorUserID,
				quotationID, snapshot.QuotationNumber, snapshot.Version)
		})
	}
	s.bestEffortAudit(func() error {
		return s.audit.RecordAccessCreated(ctx, companyID, snapshot.ProjectID, actorUserID,
			view.GrantID, quotationID, snapshot.QuotationNumber, snapshot.Version)
	})
	s.advanceProjectToSent(ctx, companyID, snapshot.ProjectID)

	return view, true, nil
}

type mintRequest struct {
	companyID             string
	snapshot              ShareableQuotationSnapshot
	coordinator           AccessGroupState
	expectedActiveGrantID *string
	expectedCurrentID     string
	newCurrentID          string
	requireAcceptedNil    bool
	expiresAt             time.Time
	actorUserID           string
}

// mintAndClaim performs design spec §2.4 steps 1-3: generate the token,
// insert the candidate grant, then conditionally claim it on the coordinator.
// The raw token is returned ONLY when the claim succeeds — a losing candidate
// is left as an undisclosed orphan and its token never leaves this function.
func (s *Service) mintAndClaim(ctx context.Context, req mintRequest) (GrantView, error) {
	rawToken, err := GenerateAccessToken()
	if err != nil {
		return GrantView{}, err
	}

	candidate := AccessGrant{
		CompanyID: req.companyID, ProjectID: req.snapshot.ProjectID,
		ResourceType: ResourceTypeQuotation, ResourceID: req.newCurrentID,
		ResourceGroupKey: ResourceGroupKeyForQuotation(req.snapshot.QuotationNumber),
		QuotationNumber:  req.snapshot.QuotationNumber,
		GranteeType:      GranteeTypeClient, GranteeID: req.snapshot.ClientID,
		Permissions: ClientQuotationPermissions,
		TokenHash:   HashAccessToken(rawToken),
		Status:      AccessGrantStatusActive, Revision: 0,
		CreatedByUserID: req.actorUserID, CreatedAt: time.Now(),
		ExpiresAt: req.expiresAt, SchemaVersion: 1,
	}

	created, err := s.grants.Create(ctx, candidate)
	if err != nil {
		return GrantView{}, err
	}

	claimed, err := s.groups.ClaimActiveGrant(ctx,
		req.companyID, ResourceTypeQuotation, candidate.ResourceGroupKey,
		req.coordinator.Revision, req.expectedCurrentID, req.expectedActiveGrantID,
		req.newCurrentID, created.ID, req.requireAcceptedNil)
	if err == ErrGroupStateRevisionMismatch {
		// Lost the race. The candidate stays an undisclosed orphan: it is
		// never referenced by the coordinator, so it can never pass the
		// external liveness check (§2.3 conditions 4-5).
		return GrantView{}, ErrActiveGrantAlreadyExistsForGroup
	}
	if err != nil {
		return GrantView{}, err
	}

	view := s.buildGrantView(ctx, created, claimed, rawToken, s.externalURLFor(rawToken))
	return view, nil
}

// RotateGrant implements the three-branch rotation of design spec §2.5.
func (s *Service) RotateGrant(ctx context.Context, companyID, grantID, actorUserID string, expectedRevision int64) (GrantView, error) {
	grant, err := s.grants.FindByID(ctx, companyID, grantID)
	if err != nil {
		return GrantView{}, err
	}

	coordinator, err := s.groups.Find(ctx, companyID, grant.ResourceType, grant.ResourceGroupKey)
	if err != nil {
		return GrantView{}, err
	}

	// Branch 3: superseded/rotated grants, and any grant not targeting the
	// chain's current version, are never rotatable.
	if grant.Status == AccessGrantStatusRevoked &&
		(grant.RevokedReason == RevokedReasonSuperseded || grant.RevokedReason == RevokedReasonRotated) {
		return GrantView{}, ErrGrantSuperseded
	}
	if coordinator.CurrentResourceID != grant.ResourceID {
		return GrantView{}, ErrGrantSuperseded
	}

	snapshot, found, err := s.quotations.GetFinalizedQuotationForShare(ctx, companyID, grant.ResourceID)
	if err != nil {
		return GrantView{}, err
	}
	if !found {
		return GrantView{}, ErrQuotationNotFound
	}

	// Rotation may proceed on an accepted chain ONLY for the accepted
	// version's own grant (design spec §9.1) — expressed to the coordinator
	// as requireAcceptedNil=false, which also permits acceptedResourceId==nil.
	requireAcceptedNil := coordinator.AcceptedResourceID == nil

	switch {
	case grant.Status == AccessGrantStatusActive:
		// Branch 1: the caller's expectedRevision must match this grant.
		if grant.Revision != expectedRevision {
			return GrantView{}, ErrGrantRevisionMismatch
		}
	case grant.Status == AccessGrantStatusRevoked && grant.RevokedReason == RevokedReasonManual:
		// Branch 2: re-share semantics. The source grant stays revoked; the
		// coordinator must currently have no active grant.
		if coordinator.ActiveGrantID != nil {
			return GrantView{}, ErrActiveGrantAlreadyExistsForGroup
		}
	default:
		return GrantView{}, ErrGrantSuperseded
	}

	now := time.Now()
	expiresAt, err := grantExpiryForRotation(snapshot, now)
	if err != nil {
		return GrantView{}, err
	}

	view, err := s.mintAndClaim(ctx, mintRequest{
		companyID: companyID, snapshot: snapshot, coordinator: coordinator,
		expectedActiveGrantID: coordinator.ActiveGrantID,
		expectedCurrentID:     coordinator.CurrentResourceID,
		newCurrentID:          grant.ResourceID,
		requireAcceptedNil:    requireAcceptedNil,
		expiresAt:             expiresAt,
		actorUserID:           actorUserID,
	})
	if err != nil {
		return GrantView{}, err
	}

	// Post-claim cleanup: only Branch 1 has a live predecessor to revoke.
	if grant.Status == AccessGrantStatusActive {
		s.bestEffortRevoke(ctx, companyID, grant.ID, RevokedReasonRotated, snapshot, actorUserID)
	}
	s.bestEffortAudit(func() error {
		return s.audit.RecordAccessCreated(ctx, companyID, snapshot.ProjectID, actorUserID,
			view.GrantID, grant.ResourceID, snapshot.QuotationNumber, snapshot.Version)
	})

	return view, nil
}

// RevokeGrant conditionally revokes one grant, then best-effort clears the
// coordinator's pointer (design spec §2.6).
func (s *Service) RevokeGrant(ctx context.Context, companyID, grantID, actorUserID string, expectedRevision int64) (GrantView, error) {
	grant, err := s.grants.FindByID(ctx, companyID, grantID)
	if err != nil {
		return GrantView{}, err
	}

	revoked, err := s.grants.Revoke(ctx, companyID, grantID, expectedRevision, RevokedReasonManual, time.Now())
	if err != nil {
		return GrantView{}, err
	}

	// Best-effort: clear the coordinator pointer if it still references this
	// grant. A failure here leaves the grant correctly revoked (authoritative)
	// and the coordinator self-heals on the next share.
	if _, clearErr := s.groups.ClearActiveGrant(ctx, companyID, grant.ResourceType, grant.ResourceGroupKey, grantID); clearErr != nil {
		if clearErr != ErrGroupStateRevisionMismatch {
			s.logCleanupFailure(ctx, "clear_active_grant", companyID, grantID, clearErr)
		}
	}

	snapshot, found, snapErr := s.quotations.GetFinalizedQuotationForShare(ctx, companyID, grant.ResourceID)
	if snapErr == nil && found {
		s.bestEffortAudit(func() error {
			return s.audit.RecordAccessRevoked(ctx, companyID, grant.ProjectID, actorUserID,
				grantID, grant.ResourceID, snapshot.QuotationNumber, RevokedReasonManual, snapshot.Version)
		})
	}

	coordinator, _ := s.groups.Find(ctx, companyID, grant.ResourceType, grant.ResourceGroupKey)
	return s.buildGrantView(ctx, revoked, coordinator, "", ""), nil
}

// UpdateGrantExpiry conditionally replaces ExpiresAt on an active, unexpired
// grant. An already-expired grant cannot be edited in place — it must be
// replaced via rotation (design spec §10).
func (s *Service) UpdateGrantExpiry(ctx context.Context, companyID, grantID string, expectedRevision int64, expiresAt time.Time) (GrantView, error) {
	grant, err := s.grants.FindByID(ctx, companyID, grantID)
	if err != nil {
		return GrantView{}, err
	}

	coordinator, err := s.groups.Find(ctx, companyID, grant.ResourceType, grant.ResourceGroupKey)
	if err != nil {
		return GrantView{}, err
	}

	now := time.Now()
	if grant.Status != AccessGrantStatusActive || !now.Before(grant.ExpiresAt) {
		return GrantView{}, ErrGrantNotActive
	}
	if !IsExternallyLive(grant, coordinator, now) {
		// Orphaned or superseded grants are not editable either.
		return GrantView{}, ErrGrantNotActive
	}

	updated, err := s.grants.UpdateExpiry(ctx, companyID, grantID, expectedRevision, expiresAt)
	if err != nil {
		return GrantView{}, err
	}
	return s.buildGrantView(ctx, updated, coordinator, "", ""), nil
}

// GetShareStatus returns the current/most-recent grant for a Quotation plus
// its Client decision, for contractor visibility.
func (s *Service) GetShareStatus(ctx context.Context, companyID, quotationID string) (GrantView, error) {
	snapshot, found, err := s.quotations.GetFinalizedQuotationForShare(ctx, companyID, quotationID)
	if err != nil {
		return GrantView{}, err
	}
	if !found {
		return GrantView{}, ErrQuotationNotFound
	}

	grant, err := s.grants.FindLatestByResource(ctx, companyID, ResourceTypeQuotation, quotationID)
	if err != nil {
		return GrantView{}, err
	}

	coordinator, err := s.groups.Find(ctx, companyID, ResourceTypeQuotation,
		ResourceGroupKeyForQuotation(snapshot.QuotationNumber))
	if err != nil {
		return GrantView{}, err
	}

	return s.buildGrantView(ctx, grant, coordinator, "", ""), nil
}

// --- helpers ---

// grantExpiryFor implements the approved initial-expiry policy (§6.2).
func grantExpiryFor(snapshot ShareableQuotationSnapshot, now time.Time) (time.Time, error) {
	if snapshot.ValidUntil == nil {
		return now.Add(DefaultGrantValidity), nil
	}
	if !now.Before(*snapshot.ValidUntil) {
		return time.Time{}, ErrQuotationExpiredForShare
	}
	return *snapshot.ValidUntil, nil
}

// grantExpiryForRotation mirrors the share policy but permits rotating a
// commercially expired Quotation for continued read-only access (§2.1) —
// in that case the grant gets the default validity window instead.
func grantExpiryForRotation(snapshot ShareableQuotationSnapshot, now time.Time) (time.Time, error) {
	if snapshot.ValidUntil == nil || !now.Before(*snapshot.ValidUntil) {
		return now.Add(DefaultGrantValidity), nil
	}
	return *snapshot.ValidUntil, nil
}

func (s *Service) advanceProjectToSent(ctx context.Context, companyID, projectID string) {
	if _, _, err := s.projects.AdvanceProjectToQuotationSent(ctx, companyID, projectID); err != nil {
		s.logCleanupFailure(ctx, "advance_project_to_quotation_sent", companyID, "", err)
	}
}

func (s *Service) bestEffortRevoke(ctx context.Context, companyID, grantID, reason string, snapshot ShareableQuotationSnapshot, actorUserID string) {
	if _, err := s.grants.RevokeBestEffort(ctx, companyID, grantID, reason, time.Now()); err != nil {
		if err != ErrGrantRevisionMismatch {
			s.logCleanupFailure(ctx, "revoke_previous_grant", companyID, grantID, err)
		}
		// Not authoritative — the coordinator has already switched, so the
		// old grant is externally dead regardless (approved addendum).
	}
	// The logical transition is recorded whether or not the stored-status
	// cleanup landed, because the coordinator claim is what changed access.
	// A supersession has no directly-acting human, so actorUserID is passed
	// through and audit maps an empty value to a system actor.
	auditActor := actorUserID
	if reason == RevokedReasonSuperseded {
		auditActor = ""
	}
	s.bestEffortAudit(func() error {
		return s.audit.RecordAccessRevoked(ctx, companyID, snapshot.ProjectID, auditActor,
			grantID, snapshot.QuotationID, snapshot.QuotationNumber, reason, snapshot.Version)
	})
}

func (s *Service) bestEffortAudit(write func() error) {
	// Audit failures never fail or delay the business operation (phase1.md §50).
	_ = write()
}

func (s *Service) logCleanupFailure(ctx context.Context, operation, companyID, grantID string, err error) {
	if s.cleanupLogger != nil {
		s.cleanupLogger.LogCleanupFailure(ctx, operation, companyID, grantID, err)
	}
}

func (s *Service) buildGrantView(ctx context.Context, grant AccessGrant, coordinator AccessGroupState, rawToken, url string) GrantView {
	now := time.Now()
	view := GrantView{
		GrantID: grant.ID, QuotationID: grant.ResourceID,
		StoredStatus:    string(grant.Status),
		IsExpired:       !now.Before(grant.ExpiresAt),
		EffectiveStatus: EffectiveStatus(grant, coordinator, now),
		ExpiresAt:       grant.ExpiresAt, RevokedReason: grant.RevokedReason,
		Revision: grant.Revision, CreatedAt: grant.CreatedAt,
		RawToken: rawToken, URL: url,
	}

	status, comment, actorName, _, decidedAt, found, err := s.decisions.GetDecision(
		ctx, grant.CompanyID, "quotation", grant.ResourceID)
	if err == nil && found {
		view.HasDecision = true
		view.DecisionStatus = status
		view.DecisionComment = comment
		view.DecisionClientName = actorName
		view.DecisionDecidedAt = decidedAt
	}
	return view
}
