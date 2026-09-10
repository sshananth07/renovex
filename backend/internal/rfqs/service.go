package rfqs

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Service implements the RFQ workflows and owns ALL claim orchestration:
// sequencing, compensation, the retry matrix and reconciliation. Only this
// package can read its own lines, which is why retry_line lives here and not in
// materialrequirements (design spec §7.1).
type Service struct {
	repo          RFQRepository
	counters      RFQCounterRepository
	projectLookup ProjectLookup
	requirements  MaterialRequirementSource
	issuance      IssuanceStatusSource
	audit         AuditRecorder
}

// NewService constructs a Service from its repositories and consumed
// capabilities.
func NewService(repo RFQRepository, counters RFQCounterRepository, projectLookup ProjectLookup,
	requirements MaterialRequirementSource, issuance IssuanceStatusSource,
	audit AuditRecorder) *Service {
	return &Service{
		repo: repo, counters: counters, projectLookup: projectLookup,
		requirements: requirements, issuance: issuance, audit: audit,
	}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of either public repository interface. Only the real Mongo
// repositories implement it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every RFQ AND its Counter
// document owned by companyID — both collections, one method, per Task
// 1a's multi-collection guidance. Development-tool use only (demoseed
// reset, design spec §6.6). Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	rfqDeleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("rfqs: rfq repository %T does not support DeleteAllForCompany", s.repo)
	}
	if err := rfqDeleter.DeleteAllForCompany(ctx, companyID); err != nil {
		return err
	}

	counterDeleter, ok := s.counters.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("rfqs: counter repository %T does not support DeleteAllForCompany", s.counters)
	}
	return counterDeleter.DeleteAllForCompany(ctx, companyID)
}

// SetIssuanceStatusSource swaps the M8 seam. M7 wires
// NoExternalIssuanceSource; M8 supplies the real adapter with no change to
// service logic (design spec §6.4).
func (s *Service) SetIssuanceStatusSource(src IssuanceStatusSource) { s.issuance = src }

// CreateRFQInput carries the header fields allowed at creation. Requirement IDs
// are deliberately absent: an RFQ is created EMPTY, and lines arrive only
// through the claim-and-append path (design spec §13.2).
type CreateRFQInput struct {
	Title                string
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     *time.Time
	SupplierInstructions string
	InternalNotes        string
}

// UpdateRFQInput is a sparse header edit: a nil field means "leave unchanged".
type UpdateRFQInput struct {
	Title                *string
	DeliveryAddress      *string
	RequiredByDate       **time.Time
	ResponseDeadline     **time.Time
	SupplierInstructions *string
	InternalNotes        *string
}

// CreateRFQ creates an empty draft RFQ with an allocated tenant-scoped number.
func (s *Service) CreateRFQ(ctx context.Context, companyID, actorUserID, projectID string,
	input CreateRFQInput) (RFQ, error) {

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return RFQ{}, err
	}
	if !belongs {
		return RFQ{}, ErrProjectNotFound
	}

	// Atomic by construction — no read-then-write race, no retry loop.
	seq, err := s.counters.NextRFQNumber(ctx, companyID)
	if err != nil {
		return RFQ{}, err
	}

	now := time.Now()
	rfq := RFQ{
		CompanyID: companyID, ProjectID: projectID,
		RFQNumber: FormatRFQNumber(seq),
		Status:    RFQStatusDraft,
		Title:     input.Title,

		DeliveryAddress:      input.DeliveryAddress,
		RequiredByDate:       input.RequiredByDate,
		ResponseDeadline:     input.ResponseDeadline,
		SupplierInstructions: input.SupplierInstructions,
		InternalNotes:        input.InternalNotes,

		CreatedByUserID: actorUserID,
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}

	created, err := s.repo.Create(ctx, rfq)
	if err != nil {
		return RFQ{}, err
	}
	// Audit is best-effort: a failure here never fails the business operation.
	_ = s.audit.RecordRFQCreated(ctx, companyID, created.ProjectID, actorUserID,
		created.ChainID(), created.RFQNumber)
	return created, nil
}

// GetRFQ returns one RFQ, tenant-scoped. This is the contractor route, so the
// aggregate — including InternalNotes — is returned.
func (s *Service) GetRFQ(ctx context.Context, companyID, rfqID string) (RFQ, error) {
	return s.repo.FindByID(ctx, companyID, rfqID)
}

// ListRFQsByProject validates the project belongs to the company before
// listing, so a foreign projectID yields 404 rather than an empty list.
func (s *Service) ListRFQsByProject(ctx context.Context, companyID, projectID string) ([]RFQ, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// UpdateRFQ applies a sparse header edit under a Revision guard. Draft-only:
// the repository filter enforces it, so a concurrent mark-ready cannot be raced.
func (s *Service) UpdateRFQ(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64, input UpdateRFQInput) (RFQ, error) {

	existing, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if existing.Status != RFQStatusDraft {
		return RFQ{}, ErrRFQNotDraft
	}

	updated := existing
	if input.Title != nil {
		updated.Title = *input.Title
	}
	if input.DeliveryAddress != nil {
		updated.DeliveryAddress = *input.DeliveryAddress
	}
	if input.RequiredByDate != nil {
		updated.RequiredByDate = *input.RequiredByDate
	}
	if input.ResponseDeadline != nil {
		updated.ResponseDeadline = *input.ResponseDeadline
	}
	if input.SupplierInstructions != nil {
		updated.SupplierInstructions = *input.SupplierInstructions
	}
	if input.InternalNotes != nil {
		updated.InternalNotes = *input.InternalNotes
	}

	saved, err := s.repo.UpdateDraft(ctx, companyID, rfqID, expectedRevision, updated)
	if err != nil {
		return RFQ{}, err
	}
	_ = s.audit.RecordRFQUpdated(ctx, companyID, saved.ProjectID, actorUserID, saved.ChainID(), saved.RFQNumber)
	return saved, nil
}

// DeleteRFQ removes an empty draft RFQ that holds NO outstanding claims
// (design spec §7.7).
//
// The third condition is not redundant: an orphaned claim is precisely a claim
// with no line, so an RFQ can be line-empty while still holding a requirement
// hostage. Deleting it would leave ActiveRFQChainID naming a chain that no
// longer exists, with no RFQ left to reconcile through — an unrecoverable state
// reachable by a single delete.
func (s *Service) DeleteRFQ(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64) error {

	existing, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return err
	}
	if existing.Status != RFQStatusDraft {
		return ErrRFQNotDraft
	}
	if len(existing.Lines) != 0 {
		return ErrRFQNotEmpty
	}

	claims, err := s.requirements.ListClaimsForRFQChain(ctx, companyID, existing.ChainID())
	if err != nil {
		return err
	}
	if len(claims) != 0 {
		return ErrRFQHasOutstandingClaims
	}

	if err := s.repo.Delete(ctx, companyID, rfqID, expectedRevision); err != nil {
		return err
	}
	_ = s.audit.RecordRFQDeleted(ctx, companyID, existing.ProjectID, actorUserID,
		existing.ChainID(), existing.RFQNumber)
	return nil
}

// --- Lifecycle (design spec §6.4) ---

// MarkReady transitions draft -> ready after validating the aggregate and every
// claimed line.
//
// Per-line validation uses ClaimedRequirementIsReadyForRFQ, NOT the unclaimed
// §8.5 predicate: that predicate requires no claim, which is false for every
// requirement already on a line, so applying it here would make every non-empty
// RFQ impossible to mark ready.
//
// This is a POINT-IN-TIME validation, not a standing guarantee. M7 has no
// mechanism that revisits it, and deliberately no automatic ready -> draft
// demotion: an automatic demotion would let a background source change retract
// a scope the contractor had reviewed and locked (design spec §6.5).
func (s *Service) MarkReady(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64) (RFQ, error) {

	existing, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if err := existing.ReadinessErr(); err != nil {
		return RFQ{}, err
	}

	for _, line := range existing.Lines {
		ready, err := s.requirements.ClaimedRequirementIsReadyForRFQ(ctx, companyID,
			line.SourceMaterialRequirementID, existing.ChainID(), line.ID)
		if err != nil {
			return RFQ{}, err
		}
		if !ready {
			return RFQ{}, ErrRFQLineNotEligible
		}
	}

	saved, err := s.repo.MarkReady(ctx, companyID, rfqID, expectedRevision, time.Now())
	if err != nil {
		return RFQ{}, err
	}
	_ = s.audit.RecordRFQMarkedReady(ctx, companyID, saved.ProjectID, actorUserID,
		saved.ChainID(), saved.RFQNumber, len(saved.Lines))
	return saved, nil
}

// Reopen transitions ready -> draft, gated by the M8 issuance seam.
//
// It FAILS CLOSED: on an issued chain the RFQ remains ready (409), and when the
// seam errors the RFQ also remains ready (503) rather than being reopened on an
// unverified assumption. Claims survive the transition (design spec §6.4).
func (s *Service) Reopen(ctx context.Context, companyID, actorUserID, rfqID string,
	expectedRevision int64) (RFQ, error) {

	existing, err := s.repo.FindByID(ctx, companyID, rfqID)
	if err != nil {
		return RFQ{}, err
	}
	if existing.Status != RFQStatusReady {
		return RFQ{}, ErrRFQNotReady
	}

	issued, err := s.issuance.RFQChainIssued(ctx, companyID, existing.ChainID())
	if err != nil {
		return RFQ{}, ErrIssuanceStatusUnavailable
	}
	if issued {
		return RFQ{}, ErrRFQAlreadyIssued
	}

	saved, err := s.repo.Reopen(ctx, companyID, rfqID, expectedRevision, time.Now())
	if err != nil {
		return RFQ{}, err
	}
	_ = s.audit.RecordRFQReopened(ctx, companyID, saved.ProjectID, actorUserID, saved.ChainID(), saved.RFQNumber)
	return saved, nil
}

// --- M8 handoff capabilities (design spec §9) ---

// GetReadyRFQSnapshot exposes a ready RFQ to M8.
//
// InternalNotes is structurally absent from this signature, so it cannot leak
// to a supplier-facing consumer even by mistake. A draft RFQ returns
// found = false: only ready RFQs are handoff-visible.
//
// revision, title and the line identifiers were added by M8 design spec §2.1A:
// an immutable issued version records exactly which ready RFQ revision it
// snapshots, and every issued line persists its originating Material
// Requirement so copy-forward can match on it (M8 approved decision 11). These
// are identifiers, provenance, or supplier-visible content — no cost, margin,
// price or contractor-private note is exposed.
//
// ResponseDeadline stays optional HERE. M7 permits a ready RFQ without one;
// M8's issuance boundary is what refuses to issue it (M8 design spec §4.1A).
// Tightening it in M7 would retroactively invalidate existing ready RFQs.
func (s *Service) GetReadyRFQSnapshot(ctx context.Context, companyID, rfqChainID string) (
	rfqNumber string, projectID string, revision int64, title string,
	deliveryAddress string, requiredBy *time.Time, responseDeadline *time.Time,
	supplierInstructions string, lines []RFQLineSnapshot,
	found bool, err error) {

	rfq, err := s.repo.FindByID(ctx, companyID, rfqChainID)
	if errors.Is(err, ErrRFQNotFound) {
		return "", "", 0, "", "", nil, nil, "", nil, false, nil
	}
	if err != nil {
		return "", "", 0, "", "", nil, nil, "", nil, false, err
	}
	if rfq.Status != RFQStatusReady {
		return "", "", 0, "", "", nil, nil, "", nil, false, nil
	}

	ordered := append([]RFQLine(nil), rfq.Lines...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].SortOrder < ordered[j].SortOrder })

	snapshots := make([]RFQLineSnapshot, 0, len(ordered))
	for _, l := range ordered {
		snapshots = append(snapshots, RFQLineSnapshot{
			LineID:                      l.ID,
			SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID:                  l.MaterialID,
			MaterialName:                l.MaterialName,
			Specification:               l.Specification,
			QuantityValue:               l.Quantity.Value.String(),
			QuantityUnit:                l.Quantity.Unit,
			RequiredByDate:              l.RequiredByDate,
			ProcurementNotes:            l.ProcurementNotes,
			SortOrder:                   l.SortOrder,
		})
	}

	return rfq.RFQNumber, rfq.ProjectID, rfq.Revision, rfq.Title, rfq.DeliveryAddress,
		rfq.RequiredByDate, rfq.ResponseDeadline, rfq.SupplierInstructions, snapshots, true, nil
}

// RFQChainIsReady reports whether a chain is in ready status.
func (s *Service) RFQChainIsReady(ctx context.Context, companyID, rfqChainID string) (bool, error) {
	rfq, err := s.repo.FindByID(ctx, companyID, rfqChainID)
	if errors.Is(err, ErrRFQNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rfq.Status == RFQStatusReady, nil
}

// ReopenPermitted answers the question Reopen enforces, without mutating.
func (s *Service) ReopenPermitted(ctx context.Context, companyID, rfqChainID string) (bool, error) {
	issued, err := s.issuance.RFQChainIssued(ctx, companyID, rfqChainID)
	if err != nil {
		return false, ErrIssuanceStatusUnavailable
	}
	return !issued, nil
}

// newLineID pre-generates a stable RFQ line identifier (§7.2 step 2). The line
// ID must exist BEFORE the claim, because the claim stores it — that is what
// makes an uncertain add-line retry deterministic.
func newLineID() string { return bson.NewObjectID().Hex() }
