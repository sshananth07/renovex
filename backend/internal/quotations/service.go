package quotations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ProjectLookup is the capability quotations needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
	// GetProjectClientID returns the bare ClientID string, never a
	// projects.Project struct (ADR 0002) — design spec §18/§22.
	GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error)
}

// WorkItemLookup is the capability quotations needs from work — a
// customer-presentable WorkItem description for line generation, and a
// Company+Project-scoped existence check for contractor-submitted
// SourceWorkItemIDs on a manual line edit (design spec §5.3/§13.1/§22).
type WorkItemLookup interface {
	GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (description string, found bool, err error)
}

// FinalizedEstimateSource is the capability quotations needs from
// estimates. The return shape structurally cannot carry a cost figure,
// category, or description (design spec §4/§22) — eligibility is reported
// via four plain booleans, never a shared error value.
type FinalizedEstimateSource interface {
	VisitQuotationSeeds(
		ctx context.Context,
		companyID, projectID, estimateID string,
		visit func(workItemID *string, allocatedSellingAmount money.Money) error,
	) (
		proposedSellingPrice money.Money,
		currency string,
		found bool,
		projectMatches bool,
		finalized bool,
		allocationEligible bool,
		err error,
	)
}

// Service implements Quotation creation, versioning, draft editing, tax
// configuration, and finalization.
type Service struct {
	repo           QuotationRepository
	counterRepo    QuotationCounterRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	estimateSource FinalizedEstimateSource
}

// NewService constructs a Service backed by repo/counterRepo, consuming
// projectLookup, workItemLookup, and estimateSource to validate parent
// references and generate customer-facing lines.
func NewService(repo QuotationRepository, counterRepo QuotationCounterRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, estimateSource FinalizedEstimateSource) *Service {
	return &Service{repo: repo, counterRepo: counterRepo, projectLookup: projectLookup, workItemLookup: workItemLookup, estimateSource: estimateSource}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of either public repository interface. Only the real Mongo
// repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Quotation AND its Counter
// document owned by companyID — both collections, one method, per Task
// 1a's multi-collection guidance. Development-tool use only (demoseed
// reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	quotationDeleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("quotations: quotation repository %T does not support DeleteAllForCompany", s.repo)
	}
	if err := quotationDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	counterDeleter, ok := s.counterRepo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("quotations: counter repository %T does not support DeleteAllForCompany", s.counterRepo)
	}
	return counterDeleter.DeleteAllForCompany(ctx, companyID)
}

// generatedLine is an intermediate seed collected from VisitQuotationSeeds
// before descriptions are attached and QuotationLines are built.
type generatedLine struct {
	workItemID *string
	amount     money.Money
}

// generateLines calls estimateSource.VisitQuotationSeeds, maps its four
// eligibility booleans to this package's own sentinels (never propagating
// any value estimates itself defines), and — on success — builds one
// QuotationLine per seed, resolving each generated line's customer-facing
// Description via workItemLookup (never from anything VisitQuotationSeeds
// returns — design spec §4/§5.3).
func (s *Service) generateLines(ctx context.Context, companyID, projectID, estimateID string) ([]QuotationLine, money.Money, string, error) {
	var seeds []generatedLine
	proposedSellingPrice, currency, found, projectMatches, finalized, allocationEligible, err :=
		s.estimateSource.VisitQuotationSeeds(ctx, companyID, projectID, estimateID,
			func(workItemID *string, allocatedSellingAmount money.Money) error {
				seeds = append(seeds, generatedLine{workItemID: workItemID, amount: allocatedSellingAmount})
				return nil
			})
	if err != nil {
		return nil, money.Money{}, "", err
	}
	if !found {
		return nil, money.Money{}, "", ErrEstimateNotFound
	}
	if !projectMatches {
		return nil, money.Money{}, "", ErrEstimateProjectMismatch
	}
	if !finalized {
		return nil, money.Money{}, "", ErrEstimateNotFinalized
	}
	if !allocationEligible {
		return nil, money.Money{}, "", ErrIneligibleCostBasisForQuotation
	}

	// Defense-in-depth boundary validation: estimates' own allocation
	// guarantees (positivity, sum == total) are already enforced inside
	// money.AllocateProportionally, but the privacy-firewall boundary
	// itself must not blindly trust whatever crosses it — verify the
	// seed set independently before any Quotation is persisted.
	if len(seeds) == 0 {
		return nil, money.Money{}, "", ErrNoQuotationSeedsGenerated
	}
	seedSum := money.New(0, currency)
	for _, seed := range seeds {
		if seed.amount.Amount <= 0 {
			return nil, money.Money{}, "", ErrQuotationSeedAmountNotPositive
		}
		if seed.amount.Currency != currency {
			return nil, money.Money{}, "", ErrQuotationSeedCurrencyMismatch
		}
		sum, addErr := seedSum.Add(seed.amount)
		if addErr != nil {
			return nil, money.Money{}, "", ErrQuotationSeedCurrencyMismatch
		}
		seedSum = sum
	}
	if seedSum.Currency != proposedSellingPrice.Currency || seedSum.Amount != proposedSellingPrice.Amount {
		return nil, money.Money{}, "", ErrQuotationSeedTotalMismatch
	}

	lines := make([]QuotationLine, len(seeds))
	for i, seed := range seeds {
		var sourceIDs []string
		description := "General Project Works and Services"
		if seed.workItemID != nil {
			sourceIDs = []string{*seed.workItemID}
			desc, wiFound, wiErr := s.workItemLookup.GetWorkItemDescription(ctx, companyID, projectID, *seed.workItemID)
			if wiErr != nil {
				return nil, money.Money{}, "", wiErr
			}
			if !wiFound {
				return nil, money.Money{}, "", ErrWorkItemNotFound
			}
			description = desc
		} else {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: bson.NewObjectID().Hex(), SourceWorkItemIDs: sourceIDs, Description: description,
			Amount: seed.amount, SortOrder: i,
		}
	}
	return lines, proposedSellingPrice, currency, nil
}

// CreateQuotation creates Version 1 for a fresh commercial quotation chain
// — allocates a new QuotationNumber (design spec §9.3), generates initial
// Lines from the finalized Estimate (§5.3/§6.2), always as a draft.
// Attempted directly at Version=1 with NO retry loop (design spec §27.2,
// mirroring estimates.CreateEstimate exactly — retrying into a
// recalculated higher version number here would silently bypass the
// finalized-source precondition CreateNewVersion exists to enforce).
func (s *Service) CreateQuotation(ctx context.Context, companyID, projectID, estimateID string) (Quotation, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Quotation{}, err
	}
	if !belongs {
		return Quotation{}, ErrProjectNotFound
	}

	clientID, err := s.projectLookup.GetProjectClientID(ctx, companyID, projectID)
	if err != nil {
		return Quotation{}, err
	}

	lines, proposedSellingPrice, currency, err := s.generateLines(ctx, companyID, projectID, estimateID)
	if err != nil {
		return Quotation{}, err
	}

	quotationNumber, err := s.nextQuotationNumber(ctx, companyID)
	if err != nil {
		return Quotation{}, err
	}

	created, err := s.repo.Create(ctx, Quotation{
		CompanyID: companyID, ProjectID: projectID, ClientID: clientID, EstimateID: estimateID,
		QuotationNumber: quotationNumber, Version: 1, Status: QuotationStatusDraft, Revision: 0,
		Currency: currency, Lines: lines,
		Subtotal: proposedSellingPrice, GeneratedSubtotal: proposedSellingPrice,
		TaxMode: TaxModeNone, TaxAmount: money.New(0, currency), Total: proposedSellingPrice,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		// No retry here by design (design spec §27.2) — ErrVersionConflict,
		// ErrDraftAlreadyExists, and ErrUnclassifiedDuplicateKey are all
		// propagated to the caller exactly as classifyCreateError produced
		// them, never collapsed into one another.
		return Quotation{}, err
	}
	return created, nil
}

const quotationNumberDigits = 6

// nextQuotationNumber allocates the next per-Company sequence number via
// the atomic counter and formats it as "QT-000001" (design spec §9.2).
func (s *Service) nextQuotationNumber(ctx context.Context, companyID string) (string, error) {
	next, err := s.counterRepo.NextQuotationNumber(ctx, companyID)
	if err != nil {
		return "", ErrQuotationNumberAllocationFailed
	}
	digits := decimal.NewFromInt(next).StringFixed(0)
	for len(digits) < quotationNumberDigits {
		digits = "0" + digits
	}
	return "QT-" + digits, nil
}

// GetQuotation returns quotationID's Quotation, tenant-scoped to companyID.
func (s *Service) GetQuotation(ctx context.Context, companyID, quotationID string) (Quotation, error) {
	return s.repo.FindByID(ctx, companyID, quotationID)
}

// ListQuotationsByProject validates projectID belongs to companyID before
// listing — a foreign projectID returns ErrProjectNotFound, never an empty
// list.
func (s *Service) ListQuotationsByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// validateLine applies design spec §7.1/§7.2/§8's rules to one submitted
// line: Quantity/Unit/UnitPrice all-or-nothing, positivity, product-
// matches-Amount, currency match against the Quotation's own currency,
// and Amount > 0.
func validateLine(line QuotationLine, quotationCurrency string) error {
	quantitySupplied := line.Quantity != nil || line.Unit != nil || line.UnitPrice != nil
	quantityComplete := line.Quantity != nil && line.Unit != nil && line.UnitPrice != nil
	if quantitySupplied && !quantityComplete {
		return ErrIncompleteLinePricing
	}
	if quantityComplete {
		if !line.Quantity.IsPositive() {
			return ErrInvalidLineQuantity
		}
		if strings.TrimSpace(*line.Unit) == "" {
			return ErrInvalidLineUnit
		}
		if line.UnitPrice.Amount <= 0 {
			return ErrInvalidLineUnitPrice
		}
		if line.UnitPrice.Currency != quotationCurrency {
			return ErrLineCurrencyMismatch
		}
		expected := money.CalculateLineAmount(*line.Quantity, *line.UnitPrice)
		if expected.Amount != line.Amount.Amount {
			return ErrLineAmountMismatch
		}
	}
	if line.Amount.Currency != quotationCurrency {
		return ErrLineCurrencyMismatch
	}
	if line.Amount.Amount <= 0 {
		return ErrLineAmountNotPositive
	}
	if strings.TrimSpace(line.Description) == "" {
		return ErrBlankLineDescription
	}
	return nil
}

// ReplaceLines validates and replaces the entire Lines array on a draft
// Quotation, per design spec §13.1's full validation sequence — applied to
// the WHOLE submitted array before ANY line is accepted. Recomputes
// Subtotal/TaxAmount/Total server-side. NEVER touches GeneratedSubtotal.
// Draft-only; Revision-guarded.
func (s *Service) ReplaceLines(ctx context.Context, companyID, quotationID string, lines []QuotationLine, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}
	if len(lines) == 0 {
		return Quotation{}, ErrEmptyLines
	}

	seenLineIDs := map[string]bool{}
	existingLineIDs := map[string]bool{}
	for _, l := range existing.Lines {
		existingLineIDs[l.ID] = true
	}

	assigned := make([]QuotationLine, len(lines))
	for i, line := range lines {
		// A submitted line with no ID is a new line — the server generates
		// a fresh one (design spec §13.1 step 5). An existing ID must
		// belong to THIS Quotation and must not repeat within the
		// submitted array.
		if line.ID == "" {
			line.ID = bson.NewObjectID().Hex()
		} else {
			if !existingLineIDs[line.ID] {
				return Quotation{}, ErrUnknownLineID
			}
			if seenLineIDs[line.ID] {
				return Quotation{}, ErrDuplicateLineID
			}
			seenLineIDs[line.ID] = true
		}

		seenWorkItemIDs := map[string]bool{}
		for _, wid := range line.SourceWorkItemIDs {
			if seenWorkItemIDs[wid] {
				return Quotation{}, ErrDuplicateWorkItemIDInLine
			}
			seenWorkItemIDs[wid] = true
			_, found, err := s.workItemLookup.GetWorkItemDescription(ctx, companyID, existing.ProjectID, wid)
			if err != nil {
				return Quotation{}, err
			}
			if !found {
				return Quotation{}, ErrWorkItemNotFound
			}
		}

		if err := validateLine(line, existing.Currency); err != nil {
			return Quotation{}, err
		}

		trimmed := line
		trimmed.Description = strings.TrimSpace(line.Description)
		assigned[i] = trimmed
	}

	subtotal := money.New(0, existing.Currency)
	for _, l := range assigned {
		sum, addErr := subtotal.Add(l.Amount)
		if addErr != nil {
			return Quotation{}, ErrLineCurrencyMismatch
		}
		subtotal = sum
	}

	var taxAmount money.Money
	if existing.TaxMode == TaxModePercentage {
		taxAmount = money.ApplyRateBPS(subtotal, existing.TaxRateBPS)
	} else {
		taxAmount = money.New(0, existing.Currency)
	}
	total, err := subtotal.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	updated := existing
	updated.Lines = assigned
	updated.Subtotal = subtotal
	updated.TaxAmount = taxAmount
	updated.Total = total

	return s.repo.ReplaceLines(ctx, companyID, quotationID, expectedRevision, updated)
}

// UpdateTerms updates Terms/PaymentSchedule/Notes/ValidUntil on a draft
// Quotation. Draft-only; Revision-guarded.
func (s *Service) UpdateTerms(ctx context.Context, companyID, quotationID, terms, paymentSchedule, notes string, validUntil *time.Time, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}

	updated := existing
	updated.Terms = terms
	updated.PaymentSchedule = paymentSchedule
	updated.Notes = notes
	updated.ValidUntil = validUntil

	return s.repo.ReplaceTerms(ctx, companyID, quotationID, expectedRevision, updated)
}

// UpdateTax validates and updates TaxMode/TaxLabel/TaxRateBPS on a draft
// Quotation per design spec §15.1's exact input-combination rules,
// recomputing TaxAmount/Total from the currently-stored Subtotal. Draft-
// only; Revision-guarded. A rejected call leaves the draft's stored tax
// fields completely untouched.
func (s *Service) UpdateTax(ctx context.Context, companyID, quotationID string, taxMode TaxMode, taxLabel string, taxRateBPS money.RateBPS, expectedRevision int64) (Quotation, error) {
	if !taxMode.IsValid() {
		return Quotation{}, ErrInvalidTaxMode
	}

	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}

	var taxAmount money.Money
	switch taxMode {
	case TaxModeNone:
		if taxLabel != "" {
			return Quotation{}, ErrInvalidTaxLabel
		}
		if taxRateBPS != 0 {
			return Quotation{}, ErrInvalidTaxRate
		}
		taxAmount = money.New(0, existing.Currency)
	case TaxModePercentage:
		if strings.TrimSpace(taxLabel) == "" {
			return Quotation{}, ErrInvalidTaxLabel
		}
		if taxRateBPS <= 0 || taxRateBPS > money.BasisPointsDenominator {
			return Quotation{}, ErrInvalidTaxRate
		}
		taxAmount = money.ApplyRateBPS(existing.Subtotal, taxRateBPS)
	}

	total, err := existing.Subtotal.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	updated := existing
	updated.TaxMode = taxMode
	updated.TaxLabel = taxLabel
	updated.TaxRateBPS = taxRateBPS
	updated.TaxAmount = taxAmount
	updated.Total = total

	return s.repo.ReplaceTax(ctx, companyID, quotationID, expectedRevision, updated)
}

// FinalizeQuotation transitions a draft to finalized, one-directional,
// locking every field. Revision-guarded. Idempotent on retry: if already
// finalized, returns it unchanged regardless of the supplied
// expectedRevision (design spec §12).
func (s *Service) FinalizeQuotation(ctx context.Context, companyID, quotationID string, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status == QuotationStatusFinalized {
		return existing, nil
	}
	return s.repo.Finalize(ctx, companyID, quotationID, expectedRevision, time.Now())
}

// CreateNewVersion creates the next Version as a new draft from
// sourceQuotationID's chain, referencing a NEW explicit estimateID
// (design spec §11 — never silently re-resolved to "latest Estimate").
// REQUIRES the source Quotation to already be Status=finalized. Lines and
// GeneratedSubtotal are FRESHLY generated from the new Estimate — never
// copied forward from the source's possibly-hand-edited snapshot (Review
// Decision 4). Terms/PaymentSchedule/ValidUntil/Notes/tax configuration
// ARE copied forward unchanged.
func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceQuotationID, estimateID string) (Quotation, error) {
	source, err := s.repo.FindByID(ctx, companyID, sourceQuotationID)
	if err != nil {
		return Quotation{}, err
	}
	if source.Status != QuotationStatusFinalized {
		return Quotation{}, ErrQuotationMustBeFinalizedBeforeNewVersion
	}

	lines, proposedSellingPrice, currency, err := s.generateLines(ctx, companyID, source.ProjectID, estimateID)
	if err != nil {
		return Quotation{}, err
	}
	if currency != source.Currency {
		return Quotation{}, ErrQuotationCurrencyMismatch
	}

	taxAmount := money.New(0, currency)
	if source.TaxMode == TaxModePercentage {
		taxAmount = money.ApplyRateBPS(proposedSellingPrice, source.TaxRateBPS)
	}
	total, err := proposedSellingPrice.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	build := func(version int) Quotation {
		return Quotation{
			CompanyID: companyID, ProjectID: source.ProjectID, ClientID: source.ClientID, EstimateID: estimateID,
			QuotationNumber: source.QuotationNumber, Version: version, Status: QuotationStatusDraft, Revision: 0,
			Currency: currency, Lines: lines,
			Subtotal: proposedSellingPrice, GeneratedSubtotal: proposedSellingPrice,
			TaxMode: source.TaxMode, TaxLabel: source.TaxLabel, TaxRateBPS: source.TaxRateBPS,
			TaxAmount: taxAmount, Total: total,
			Terms: source.Terms, PaymentSchedule: source.PaymentSchedule, ValidUntil: source.ValidUntil, Notes: source.Notes,
			CreatedAt: time.Now(), SchemaVersion: 1,
		}
	}

	return s.allocateAndCreateVersion(ctx, companyID, source.QuotationNumber, build)
}

const maxVersionAllocationAttempts = 5

// allocateAndCreateVersion implements the bounded-retry version-number
// allocation strategy from design spec §27.3 steps 2-6: read MAX(version),
// attempt Create at MAX+1, and on ErrVersionConflict re-read MAX(version)
// and retry, bounded at maxVersionAllocationAttempts. A collision on
// ErrDraftAlreadyExists is NOT retried — propagated immediately. Used
// ONLY by CreateNewVersion — CreateQuotation deliberately does NOT use
// this helper (design spec §27.2, mirroring estimates.allocateAndCreate's
// own identical rationale).
func (s *Service) allocateAndCreateVersion(ctx context.Context, companyID, quotationNumber string, build func(version int) Quotation) (Quotation, error) {
	for attempt := 0; attempt < maxVersionAllocationAttempts; attempt++ {
		maxVersion, err := s.repo.FindMaxVersion(ctx, companyID, quotationNumber)
		if err != nil {
			return Quotation{}, err
		}
		created, err := s.repo.Create(ctx, build(maxVersion+1))
		if err == nil {
			return created, nil
		}
		if err == ErrVersionConflict {
			continue
		}
		return Quotation{}, err
	}
	return Quotation{}, ErrVersionConflict
}
