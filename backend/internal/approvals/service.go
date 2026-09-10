package approvals

import (
	"context"
	"fmt"
	"time"
)

// Service implements the subject-local Approval transition rules (design
// spec §7.4). It is deliberately unaware of Quotation chains, versions, and
// access grants — chain-level coordination lives in internal/access.
type Service struct {
	repo ApprovalRepository
}

// NewService constructs a Service backed by repo.
func NewService(repo ApprovalRepository) *Service {
	return &Service{repo: repo}
}

// companyBulkDeleter is a private, unexported capability — deliberately NOT
// part of the public ApprovalRepository interface. Only the real Mongo
// repository implements it.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Approval owned by
// companyID. Development-tool use only (demoseed reset, design spec §6.6).
// Idempotent.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("approvals: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// RecordDecisionInput carries everything needed to record one decision.
// Grouped into a struct rather than a long parameter list because the
// capability interface access consumes passes all of these together.
type RecordDecisionInput struct {
	CompanyID       string
	SubjectType     string
	SubjectID       string
	SubjectGroupKey string
	ActorName       string
	ActorEmail      string
	Comment         string
	Status          ApprovalStatus
	AccessGrantID   string
}

// RecordDecision applies the transition rules of design spec §7.4:
//
//  1. No existing document        -> insert at Revision 0
//  2. Current non-terminal        -> Revision-guarded conditional update
//  3. Current accepted + accept   -> idempotent, NO write at all
//  4. Current accepted + other    -> ErrDecisionAlreadyAccepted
//
// It reports isNewDecision=true only when it created the very first
// Approval document for the subject.
func (s *Service) RecordDecision(ctx context.Context, in RecordDecisionInput) (Approval, bool, error) {
	if !in.Status.IsValid() {
		return Approval{}, false, ErrInvalidApprovalStatus
	}

	existing, err := s.repo.FindBySubject(ctx, in.CompanyID, in.SubjectType, in.SubjectID)
	if err == ErrApprovalNotFound {
		created, createErr := s.repo.Create(ctx, s.buildApproval(in))
		if createErr == ErrApprovalAlreadyExists {
			// Lost the first-decision race — re-read once and apply the
			// transition rules against the winner's document instead.
			existing, err = s.repo.FindBySubject(ctx, in.CompanyID, in.SubjectType, in.SubjectID)
			if err != nil {
				return Approval{}, false, err
			}
			updated, updateErr := s.applyTransition(ctx, in, existing)
			return updated, false, updateErr
		}
		if createErr != nil {
			return Approval{}, false, createErr
		}
		return created, true, nil
	}
	if err != nil {
		return Approval{}, false, err
	}

	updated, err := s.applyTransition(ctx, in, existing)
	return updated, false, err
}

// applyTransition enforces rules 2-4 against an existing Approval.
func (s *Service) applyTransition(ctx context.Context, in RecordDecisionInput, existing Approval) (Approval, error) {
	if existing.Status.IsTerminal() {
		if in.Status == ApprovalStatusAccepted {
			// Repeat accept: idempotent, no write attempted at all.
			return existing, nil
		}
		return Approval{}, ErrDecisionAlreadyAccepted
	}

	updated := s.buildApproval(in)
	updated.ID = existing.ID
	return s.repo.UpdateDecision(ctx, in.CompanyID, in.SubjectType, in.SubjectID, existing.Revision, updated)
}

// ReconcileAccepted brings the Approval record into line with an acceptance
// the coordinator has ALREADY recorded as authoritative (design spec §7.2:
// "if the coordinator claim succeeds but the Approval write fails... a
// repeated accept must reconcile the Approval record before returning
// success"). Unlike RecordDecision it never refuses on an existing
// non-terminal state — the commercial decision is already final upstream, so
// this only ever moves the record forward to accepted.
func (s *Service) ReconcileAccepted(ctx context.Context, in RecordDecisionInput) (Approval, error) {
	in.Status = ApprovalStatusAccepted

	existing, err := s.repo.FindBySubject(ctx, in.CompanyID, in.SubjectType, in.SubjectID)
	if err == ErrApprovalNotFound {
		created, createErr := s.repo.Create(ctx, s.buildApproval(in))
		if createErr == ErrApprovalAlreadyExists {
			existing, err = s.repo.FindBySubject(ctx, in.CompanyID, in.SubjectType, in.SubjectID)
			if err != nil {
				return Approval{}, err
			}
			return s.forceAccepted(ctx, in, existing)
		}
		return created, createErr
	}
	if err != nil {
		return Approval{}, err
	}
	return s.forceAccepted(ctx, in, existing)
}

func (s *Service) forceAccepted(ctx context.Context, in RecordDecisionInput, existing Approval) (Approval, error) {
	if existing.Status == ApprovalStatusAccepted {
		return existing, nil
	}
	updated := s.buildApproval(in)
	updated.ID = existing.ID
	return s.repo.UpdateDecision(ctx, in.CompanyID, in.SubjectType, in.SubjectID, existing.Revision, updated)
}

// GetDecision returns the current decision for a subject, tenant-scoped.
func (s *Service) GetDecision(ctx context.Context, companyID, subjectType, subjectID string) (Approval, bool, error) {
	a, err := s.repo.FindBySubject(ctx, companyID, subjectType, subjectID)
	if err == ErrApprovalNotFound {
		return Approval{}, false, nil
	}
	if err != nil {
		return Approval{}, false, err
	}
	return a, true, nil
}

func (s *Service) buildApproval(in RecordDecisionInput) Approval {
	return Approval{
		CompanyID: in.CompanyID, SubjectType: in.SubjectType,
		SubjectID: in.SubjectID, SubjectGroupKey: in.SubjectGroupKey,
		ActorType: ActorTypeClient, ActorName: in.ActorName, ActorEmail: in.ActorEmail,
		Status: in.Status, Comment: in.Comment, AccessGrantID: in.AccessGrantID,
		Revision: 0, DecidedAt: time.Now(), SchemaVersion: 1,
	}
}
