package rfqissuance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// Phase A establishes the service shell and the ONE capability M7 needs back
// from M8 immediately: whether a chain has an issued version (design spec
// §2.1, §1A.3). Issuance, invitations and the rest arrive in later phases.

// fakeIssuedVersionReader answers the issuance-status question with a canned
// result, so these tests exercise ONLY that path — including the failure mode a
// real store would rarely produce on demand.
//
// It embeds the full store fake to satisfy IssuedVersionStore without
// reimplementing creation and lookup, then overrides HasIssuedVersion.
type fakeIssuedVersionReader struct {
	*fakeVersionStore

	issued bool
	err    error

	gotCompanyID  string
	gotRFQChainID string
}

func newFakeIssuedVersionReader(issued bool, err error) *fakeIssuedVersionReader {
	return &fakeIssuedVersionReader{
		fakeVersionStore: newFakeVersionStore(), issued: issued, err: err,
	}
}

func (f *fakeIssuedVersionReader) HasIssuedVersion(_ context.Context,
	companyID, rfqChainID string) (bool, error) {
	f.gotCompanyID = companyID
	f.gotRFQChainID = rfqChainID
	return f.issued, f.err
}

func TestRFQChainHasIssuedVersionReportsTrueOnceIssued(t *testing.T) {
	repo := newFakeIssuedVersionReader(true, nil)
	svc := rfqissuance.NewService(repo)

	issued, err := svc.RFQChainHasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !issued {
		t.Error("expected issued = true for a chain with an issued version")
	}
}

func TestRFQChainHasIssuedVersionReportsFalseBeforeIssuance(t *testing.T) {
	repo := newFakeIssuedVersionReader(false, nil)
	svc := rfqissuance.NewService(repo)

	issued, err := svc.RFQChainHasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("expected issued = false for a chain that has never been issued")
	}
}

// The tenant is part of the question, not an afterthought. Another company's
// issued version must never answer this company's query.
func TestRFQChainHasIssuedVersionScopesTheLookupToTheCompanyAndChain(t *testing.T) {
	repo := newFakeIssuedVersionReader(false, nil)
	svc := rfqissuance.NewService(repo)

	if _, err := svc.RFQChainHasIssuedVersion(context.Background(), "company-1", "chain-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.gotCompanyID != "company-1" {
		t.Errorf("repository received companyID %q, want company-1", repo.gotCompanyID)
	}
	if repo.gotRFQChainID != "chain-1" {
		t.Errorf("repository received rfqChainID %q, want chain-1", repo.gotRFQChainID)
	}
}

// Fail closed. rfqs turns any error here into ErrIssuanceStatusUnavailable and
// refuses to reopen; reporting "not issued" on a failed lookup would let a
// contractor reopen a chain a Supplier is already quoting against.
func TestRFQChainHasIssuedVersionFailsClosedOnRepositoryError(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")
	repo := newFakeIssuedVersionReader(false, sentinel)
	svc := rfqissuance.NewService(repo)

	issued, err := svc.RFQChainHasIssuedVersion(context.Background(), "company-1", "chain-1")

	if err == nil {
		t.Fatal("expected the repository error to propagate, got nil")
	}
	if issued {
		t.Error("a failed lookup must never report issued = true")
	}
}
