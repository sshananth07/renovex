package access

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// --- External Client-facing DTOs ---

// ClientMoney is the Client-visible money shape.
type ClientMoney struct {
	Amount   int64
	Currency string
}

func toClientMoney(m money.Money) ClientMoney {
	return ClientMoney{Amount: m.Amount, Currency: m.Currency}
}

// ClientQuotationLine is one commercial line as the Client sees it. It has no
// internal line ID and no SourceWorkItemIDs — those fields do not exist on
// this type, so they cannot leak (design spec §5).
type ClientQuotationLine struct {
	Description string
	Quantity    *string
	Unit        *string
	UnitPrice   *ClientMoney
	Amount      ClientMoney
}

// ClientQuotationView is the complete allowlisted external projection.
// Deliberately absent: EstimateID, GeneratedSubtotal, Notes, Revision,
// SourceWorkItemIDs, CompanyID, ProjectID, ClientID, internal Mongo IDs,
// SchemaVersion, CreatedAt, FinalizedAt (design spec §5.1/§5.2).
type ClientQuotationView struct {
	QuotationNumber string
	Version         int
	CompanyName     string
	ProjectName     string
	Currency        string
	Lines           []ClientQuotationLine
	Subtotal        ClientMoney
	TaxMode         string
	TaxLabel        string
	TaxRateBPS      int64
	TaxAmount       ClientMoney
	Total           ClientMoney
	Terms           string
	PaymentSchedule string
	ValidUntil      *time.Time
	IsExpired       bool
	Status          string
	Decision        string
}

// ClientDecisionInput carries one Client decision submission.
type ClientDecisionInput struct {
	Token       string
	Status      string
	ClientName  string
	ClientEmail string
	Comment     string
}

// ClientDecisionResult is the confirmation returned to the Client.
type ClientDecisionResult struct {
	Status    string
	DecidedAt time.Time
}

// resolvedToken bundles everything a live token resolves to.
type resolvedToken struct {
	grant       AccessGrant
	coordinator AccessGroupState
	snapshot    ShareableQuotationSnapshot
}

// resolveLiveToken performs the full five-condition liveness check of design
// spec §2.3. EVERY failure mode returns the same ErrExternalTokenUnusable so
// the external response can never distinguish malformed from unknown from
// revoked from superseded from orphaned.
func (s *Service) resolveLiveToken(ctx context.Context, rawToken string) (resolvedToken, error) {
	if strings.TrimSpace(rawToken) == "" {
		return resolvedToken{}, ErrExternalTokenUnusable
	}

	// Conditions 1-3: the grant exists, is stored-active, and is unexpired.
	grant, err := s.grants.FindByTokenHash(ctx, HashAccessToken(rawToken))
	if err == ErrGrantNotFound {
		return resolvedToken{}, ErrExternalTokenUnusable
	}
	if err != nil {
		return resolvedToken{}, err
	}

	// Conditions 4-5 come from the coordinator, which is the authority on
	// effective access — a stored-active grant it no longer references is
	// dead immediately, without waiting for any cleanup write.
	coordinator, err := s.groups.Find(ctx, grant.CompanyID, grant.ResourceType, grant.ResourceGroupKey)
	if err == ErrGroupStateNotFound {
		return resolvedToken{}, ErrExternalTokenUnusable
	}
	if err != nil {
		return resolvedToken{}, err
	}

	if !IsExternallyLive(grant, coordinator, time.Now()) {
		return resolvedToken{}, ErrExternalTokenUnusable
	}

	// The grant's OWN stored CompanyID scopes every downstream lookup — the
	// Client never supplies a tenant, so one cannot be forged.
	snapshot, found, err := s.quotations.GetFinalizedQuotationForShare(ctx, grant.CompanyID, grant.ResourceID)
	if err != nil {
		return resolvedToken{}, err
	}
	if !found || snapshot.Status != "finalized" {
		// Defense in depth: a draft can never be rendered externally, even if
		// some future path managed to create a grant against one.
		return resolvedToken{}, ErrExternalTokenUnusable
	}

	return resolvedToken{grant: grant, coordinator: coordinator, snapshot: snapshot}, nil
}

// ViewQuotationByToken returns the allowlisted Client projection and records a
// Client Viewed audit event. Every view is recorded separately — no collapsing.
func (s *Service) ViewQuotationByToken(ctx context.Context, rawToken string) (ClientQuotationView, error) {
	resolved, err := s.resolveLiveToken(ctx, rawToken)
	if err != nil {
		return ClientQuotationView{}, err
	}

	view := s.buildClientView(ctx, resolved)

	s.bestEffortAudit(func() error {
		return s.audit.RecordClientViewedQuotation(ctx, resolved.grant.CompanyID,
			resolved.snapshot.ProjectID, resolved.grant.ID, resolved.snapshot.QuotationID,
			resolved.snapshot.QuotationNumber, resolved.snapshot.Version)
	})

	return view, nil
}

func (s *Service) buildClientView(ctx context.Context, r resolvedToken) ClientQuotationView {
	now := time.Now()

	companyName, err := s.companies.GetCompanyName(ctx, r.grant.CompanyID)
	if err != nil {
		companyName = ""
	}
	projectName, found, err := s.projects.GetProjectName(ctx, r.grant.CompanyID, r.snapshot.ProjectID)
	if err != nil || !found {
		projectName = ""
	}

	lines := make([]ClientQuotationLine, len(r.snapshot.Lines))
	for i, l := range r.snapshot.Lines {
		line := ClientQuotationLine{Description: l.Description, Amount: toClientMoney(l.Amount)}
		if l.Quantity != nil {
			// Rendered as a string, never a float (ADR 0001).
			q := l.Quantity.String()
			line.Quantity = &q
		}
		if l.Unit != nil {
			u := *l.Unit
			line.Unit = &u
		}
		if l.UnitPrice != nil {
			up := toClientMoney(*l.UnitPrice)
			line.UnitPrice = &up
		}
		lines[i] = line
	}

	view := ClientQuotationView{
		QuotationNumber: r.snapshot.QuotationNumber, Version: r.snapshot.Version,
		CompanyName: companyName, ProjectName: projectName,
		Currency: r.snapshot.Currency, Lines: lines,
		Subtotal: toClientMoney(r.snapshot.Subtotal),
		TaxMode:  r.snapshot.TaxMode, TaxLabel: r.snapshot.TaxLabel,
		TaxRateBPS: int64(r.snapshot.TaxRateBPS),
		TaxAmount:  toClientMoney(r.snapshot.TaxAmount),
		Total:      toClientMoney(r.snapshot.Total),
		Terms:      r.snapshot.Terms, PaymentSchedule: r.snapshot.PaymentSchedule,
		ValidUntil: r.snapshot.ValidUntil,
		IsExpired:  quotationIsCommerciallyExpired(r.snapshot, now),
		Status:     r.snapshot.Status,
	}

	if status, _, _, _, _, found, err := s.decisions.GetDecision(ctx, r.grant.CompanyID,
		"quotation", r.snapshot.QuotationID); err == nil && found {
		view.Decision = status
	}
	return view
}

func quotationIsCommerciallyExpired(snapshot ShareableQuotationSnapshot, now time.Time) bool {
	return snapshot.ValidUntil != nil && !now.Before(*snapshot.ValidUntil)
}

// SubmitClientDecision applies the acceptance write order of design spec §7.2
// and the non-terminal fence of §7.3.
//
// For acceptance the coordinator is claimed BEFORE any Approval write, which
// is why no backward compensation exists anywhere in this method: either the
// claim wins (and the Approval follows, reconciled later if it fails), or the
// claim loses (and nothing about the decision is ever written).
func (s *Service) SubmitClientDecision(ctx context.Context, in ClientDecisionInput) (ClientDecisionResult, error) {
	if err := validateClientInput(in); err != nil {
		return ClientDecisionResult{}, err
	}

	resolved, err := s.resolveLiveToken(ctx, in.Token)
	if err != nil {
		return ClientDecisionResult{}, err
	}

	// Commercial validity gates every decision, even through a live grant.
	if quotationIsCommerciallyExpired(resolved.snapshot, time.Now()) {
		return ClientDecisionResult{}, ErrQuotationExpiredForDecision
	}

	if in.Status == "accepted" {
		return s.acceptQuotation(ctx, resolved, in)
	}
	return s.recordNonTerminalDecision(ctx, resolved, in)
}

func (s *Service) acceptQuotation(ctx context.Context, r resolvedToken, in ClientDecisionInput) (ClientDecisionResult, error) {
	groupKey := r.grant.ResourceGroupKey

	// Step 2: claim the coordinator FIRST.
	claimed, err := s.groups.ClaimAcceptance(ctx, r.grant.CompanyID, ResourceTypeQuotation,
		groupKey, r.coordinator.Revision, r.snapshot.QuotationID, r.grant.ID)
	if err == ErrGroupStateRevisionMismatch {
		// The claim lost. Re-read to distinguish the two legitimate cases.
		current, findErr := s.groups.Find(ctx, r.grant.CompanyID, ResourceTypeQuotation, groupKey)
		if findErr != nil {
			return ClientDecisionResult{}, findErr
		}

		if current.AcceptedResourceID != nil && *current.AcceptedResourceID == r.snapshot.QuotationID {
			// Already accepted — idempotent. Reconcile the Approval record in
			// case a previous attempt's write failed after its claim landed.
			approvalID, decidedAt, reconcileErr := s.decisions.ReconcileAccepted(ctx,
				r.grant.CompanyID, "quotation", r.snapshot.QuotationID, groupKey,
				in.ClientName, in.ClientEmail, in.Comment, r.grant.ID)
			if reconcileErr != nil {
				return ClientDecisionResult{}, reconcileErr
			}
			_ = approvalID
			// Retry the projection so a prior best-effort failure heals.
			s.advanceProjectToApproved(ctx, r.grant.CompanyID, r.snapshot.ProjectID)
			return ClientDecisionResult{Status: "accepted", DecidedAt: decidedAt}, nil
		}

		// A newer-version share won the race. NO accepted Approval state is
		// written — there is nothing to compensate.
		return ClientDecisionResult{}, ErrQuotationSuperseded
	}
	if err != nil {
		return ClientDecisionResult{}, err
	}
	_ = claimed

	// Step 3: the Approval write. If this fails the coordinator's
	// AcceptedResourceID remains authoritative and a repeat accept reconciles.
	approvalID, decidedAt, _, err := s.decisions.RecordDecision(ctx,
		r.grant.CompanyID, "quotation", r.snapshot.QuotationID, groupKey,
		in.ClientName, in.ClientEmail, in.Comment, "accepted", r.grant.ID)
	if err != nil {
		return ClientDecisionResult{}, err
	}

	// Step 4: monotonic project projection (best-effort).
	s.advanceProjectToApproved(ctx, r.grant.CompanyID, r.snapshot.ProjectID)

	// Step 5: audit.
	s.bestEffortAudit(func() error {
		return s.audit.RecordClientDecision(ctx, r.grant.CompanyID, r.snapshot.ProjectID,
			r.grant.ID, approvalID, r.snapshot.QuotationID, r.snapshot.QuotationNumber,
			r.snapshot.Version, "accepted", in.ClientName, in.ClientEmail)
	})

	return ClientDecisionResult{Status: "accepted", DecidedAt: decidedAt}, nil
}

// recordNonTerminalDecision fences reject/request-changes against concurrent
// supersession and acceptance before writing anything (design spec §7.3).
func (s *Service) recordNonTerminalDecision(ctx context.Context, r resolvedToken, in ClientDecisionInput) (ClientDecisionResult, error) {
	groupKey := r.grant.ResourceGroupKey

	_, err := s.groups.FenceNonTerminalDecision(ctx, r.grant.CompanyID, ResourceTypeQuotation,
		groupKey, r.coordinator.Revision, r.snapshot.QuotationID, r.grant.ID)
	if err == ErrGroupStateRevisionMismatch {
		current, findErr := s.groups.Find(ctx, r.grant.CompanyID, ResourceTypeQuotation, groupKey)
		if findErr != nil {
			return ClientDecisionResult{}, findErr
		}
		if current.AcceptedResourceID != nil {
			return ClientDecisionResult{}, ErrDecisionAlreadyAccepted
		}
		return ClientDecisionResult{}, ErrQuotationSuperseded
	}
	if err != nil {
		return ClientDecisionResult{}, err
	}

	approvalID, decidedAt, _, err := s.decisions.RecordDecision(ctx,
		r.grant.CompanyID, "quotation", r.snapshot.QuotationID, groupKey,
		in.ClientName, in.ClientEmail, in.Comment, in.Status, r.grant.ID)
	if err != nil {
		return ClientDecisionResult{}, err
	}

	// A non-terminal decision never changes Project.Status.
	s.bestEffortAudit(func() error {
		return s.audit.RecordClientDecision(ctx, r.grant.CompanyID, r.snapshot.ProjectID,
			r.grant.ID, approvalID, r.snapshot.QuotationID, r.snapshot.QuotationNumber,
			r.snapshot.Version, in.Status, in.ClientName, in.ClientEmail)
	})

	return ClientDecisionResult{Status: in.Status, DecidedAt: decidedAt}, nil
}

func (s *Service) advanceProjectToApproved(ctx context.Context, companyID, projectID string) {
	if _, _, err := s.projects.AdvanceProjectToQuotationApproved(ctx, companyID, projectID); err != nil {
		s.logCleanupFailure(ctx, "advance_project_to_quotation_approved", companyID, "", err)
	}
}

// validateClientInput applies the approved field rules of design spec §7.5.
func validateClientInput(in ClientDecisionInput) error {
	if len(in.ClientName) > MaxClientNameLength ||
		len(in.ClientEmail) > MaxClientEmailLength ||
		len(in.Comment) > MaxCommentLength {
		return ErrFieldTooLong
	}
	if in.ClientEmail != "" {
		if _, err := mail.ParseAddress(in.ClientEmail); err != nil {
			return ErrInvalidEmail
		}
	}
	// A comment is required for reject and request-changes, optional on accept.
	if in.Status != "accepted" && strings.TrimSpace(in.Comment) == "" {
		return ErrCommentRequired
	}
	return nil
}
