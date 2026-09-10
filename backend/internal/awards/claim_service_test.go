package awards

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"
)

// F4 acquisition protocol (§8E).
//
// Ordering is lexicographic and total, so two concurrent finalisations request
// shared resources in the same sequence and one always wins outright — the
// standard deadlock-avoidance argument.

type fakeLineClaimStore struct {
	claims    map[string]AwardLineClaim
	byLineage map[string]string
	nextID    int

	acquired []string
	released []string
	failOn   string
	err      error
}

func newFakeLineClaimStore() *fakeLineClaimStore {
	return &fakeLineClaimStore{
		claims:    map[string]AwardLineClaim{},
		byLineage: map[string]string{},
	}
}

func (f *fakeLineClaimStore) ClaimLineage(
	_ context.Context, input AwardLineClaimInput,
) (AwardLineClaim, error) {
	if f.err != nil {
		return AwardLineClaim{}, f.err
	}
	if input.StableLineageID == f.failOn {
		return AwardLineClaim{}, ErrRFQLineAwardConflict
	}
	key := input.CompanyID + "|" + input.RFQChainID + "|" + input.StableLineageID
	if id, held := f.byLineage[key]; held {
		existing := f.claims[id]
		if existing.AwardOperationID == input.AwardOperationID &&
			existing.State == LineClaimClaimed {
			return existing, nil
		}
		if existing.State == LineClaimAwarded {
			return AwardLineClaim{}, ErrRFQLineAlreadyAwarded
		}
		return AwardLineClaim{}, ErrRFQLineAwardConflict
	}

	f.nextID++
	claim := AwardLineClaim{
		ID:                  string(rune('a'+f.nextID)) + "-claim",
		CompanyID:           input.CompanyID,
		RFQChainID:          input.RFQChainID,
		StableLineageID:     input.StableLineageID,
		State:               LineClaimClaimed,
		AwardOperationID:    input.AwardOperationID,
		CandidateRevisionID: input.CandidateRevisionID,
		IssuedRFQVersionID:  input.IssuedRFQVersionID,
		Revision:            1,
	}
	f.claims[claim.ID] = claim
	f.byLineage[key] = claim.ID
	f.acquired = append(f.acquired, input.StableLineageID)
	return claim, nil
}

func (f *fakeLineClaimStore) FindClaim(
	_ context.Context, companyID, claimID string,
) (AwardLineClaim, bool, error) {
	claim, ok := f.claims[claimID]
	if !ok || claim.CompanyID != companyID {
		return AwardLineClaim{}, false, nil
	}
	return claim, true, nil
}

func (f *fakeLineClaimStore) FindClaimByLineage(
	_ context.Context, companyID, rfqChainID, lineageID string,
) (AwardLineClaim, bool, error) {
	id, ok := f.byLineage[companyID+"|"+rfqChainID+"|"+lineageID]
	if !ok {
		return AwardLineClaim{}, false, nil
	}
	return f.claims[id], true, nil
}

func (f *fakeLineClaimStore) ListClaimsForOperation(
	_ context.Context, companyID, operationID string,
) ([]AwardLineClaim, error) {
	var claims []AwardLineClaim
	for _, claim := range f.claims {
		if claim.CompanyID == companyID && claim.AwardOperationID == operationID {
			claims = append(claims, claim)
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		return claims[i].StableLineageID < claims[j].StableLineageID
	})
	return claims, nil
}

func (f *fakeLineClaimStore) MarkLineageAwarded(
	_ context.Context, companyID, claimID, operationID, revisionID string,
	expectedRevision int64,
) error {
	claim, ok := f.claims[claimID]
	if !ok || claim.CompanyID != companyID ||
		claim.AwardOperationID != operationID {
		return ErrRFQLineAwardConflict
	}
	claim.State = LineClaimAwarded
	claim.AwardRevisionID = revisionID
	claim.Revision = expectedRevision + 1
	f.claims[claimID] = claim
	return nil
}

func (f *fakeLineClaimStore) ReleaseLineageClaim(
	_ context.Context, companyID, claimID, operationID string,
	expectedRevision int64,
) error {
	claim, ok := f.claims[claimID]
	if !ok || claim.CompanyID != companyID ||
		claim.AwardOperationID != operationID ||
		claim.State != LineClaimClaimed {
		return ErrRFQLineAwardConflict
	}
	f.released = append(f.released, claim.StableLineageID)
	delete(f.claims, claimID)
	delete(f.byLineage,
		companyID+"|"+claim.RFQChainID+"|"+claim.StableLineageID)
	return nil
}

func claimService() (*Service, *fakeLineClaimStore, *fakeOfferEligibilityClaimant) {
	claims := newFakeLineClaimStore()
	eligibility := &fakeOfferEligibilityClaimant{}
	service := NewService(
		WithAwardLineClaimRepository(claims),
		WithOfferEligibilityClaimant(eligibility),
	)
	return service, claims, eligibility
}

func acquisitionRequest(lineages, versions []string) ClaimAcquisitionRequest {
	request := ClaimAcquisitionRequest{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		IssuedRFQVersionID: "issued-1",
		OperationID:        "op-1", CandidateRevisionID: "candidate-1",
		AcquiredAt: time.Now().UTC(),
	}
	for _, lineage := range lineages {
		request.Lineages = append(request.Lineages, lineage)
	}
	for _, version := range versions {
		request.OfferVersions = append(request.OfferVersions,
			OfferVersionClaimTarget{OfferVersionID: version, ExpectedRevision: 1})
	}
	return request
}

// Lineages are claimed in ascending order, then offer gates in ascending order.
// Two concurrent finalisations therefore contend in the same sequence, so one
// wins outright rather than deadlocking against the other.
func TestAcquireClaimsUsesDeterministicAscendingOrder(t *testing.T) {
	service, claims, eligibility := claimService()

	if _, err := service.AcquireClaims(context.Background(), acquisitionRequest(
		[]string{"lineage-c", "lineage-a", "lineage-b"},
		[]string{"offer-z", "offer-x", "offer-y"},
	)); err != nil {
		t.Fatalf("AcquireClaims: %v", err)
	}

	wantLineages := []string{"lineage-a", "lineage-b", "lineage-c"}
	if len(claims.acquired) != len(wantLineages) {
		t.Fatalf("acquired %v, want %v", claims.acquired, wantLineages)
	}
	for index, lineage := range wantLineages {
		if claims.acquired[index] != lineage {
			t.Fatalf("acquisition order = %v, want %v",
				claims.acquired, wantLineages)
		}
	}

	wantVersions := []string{"offer-x", "offer-y", "offer-z"}
	for index, version := range wantVersions {
		if eligibility.claimed[index] != version {
			t.Fatalf("gate order = %v, want %v", eligibility.claimed, wantVersions)
		}
	}
}

// On partial failure the operation releases ONLY its own claims, in reverse
// acquisition order, and returns the originating error.
func TestAcquireClaimsReleasesOnlyItsOwnClaimsOnFailure(t *testing.T) {
	service, claims, _ := claimService()
	claims.failOn = "lineage-c"

	_, err := service.AcquireClaims(context.Background(), acquisitionRequest(
		[]string{"lineage-a", "lineage-b", "lineage-c"}, nil))
	if !errors.Is(err, ErrRFQLineAwardConflict) {
		t.Fatalf("err = %v, want the originating ErrRFQLineAwardConflict", err)
	}

	// Reverse acquisition order: b released before a.
	want := []string{"lineage-b", "lineage-a"}
	if len(claims.released) != len(want) {
		t.Fatalf("released %v, want %v", claims.released, want)
	}
	for index, lineage := range want {
		if claims.released[index] != lineage {
			t.Fatalf("release order = %v, want reverse acquisition %v",
				claims.released, want)
		}
	}
}

// A claim owned by ANOTHER operation is never released, even while unwinding.
// Releasing it would hand a competitor's in-flight lineage away.
func TestAcquireClaimsNeverReleasesAForeignClaim(t *testing.T) {
	service, claims, _ := claimService()
	ctx := context.Background()

	// A different operation already holds lineage-b.
	if _, err := claims.ClaimLineage(ctx, AwardLineClaimInput{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		StableLineageID: "lineage-b", AwardOperationID: "other-op",
		CandidateRevisionID: "other-candidate",
	}); err != nil {
		t.Fatalf("seeding foreign claim: %v", err)
	}
	claims.released = nil

	if _, err := service.AcquireClaims(ctx, acquisitionRequest(
		[]string{"lineage-a", "lineage-b"}, nil)); err == nil {
		t.Fatal("acquiring a lineage held by another operation must fail")
	}

	for _, lineage := range claims.released {
		if lineage == "lineage-b" {
			t.Fatal("released a claim owned by another operation")
		}
	}
	// The foreign claim survives untouched.
	if _, found, _ := claims.FindClaimByLineage(
		ctx, "company-1", "rfqchain-1", "lineage-b"); !found {
		t.Fatal("the foreign claim was destroyed")
	}
}

// An already-awarded lineage is terminal: the caller learns it can never
// succeed, rather than receiving a retryable conflict.
func TestAcquireClaimsReportsAnAlreadyAwardedLineageDistinctly(t *testing.T) {
	service, claims, _ := claimService()
	ctx := context.Background()

	claim, err := claims.ClaimLineage(ctx, AwardLineClaimInput{
		CompanyID: "company-1", RFQChainID: "rfqchain-1",
		StableLineageID: "lineage-a", AwardOperationID: "earlier-op",
		CandidateRevisionID: "earlier-candidate",
	})
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}
	if err := claims.MarkLineageAwarded(ctx, "company-1", claim.ID,
		"earlier-op", "revision-1", claim.Revision); err != nil {
		t.Fatalf("MarkLineageAwarded: %v", err)
	}

	if _, err := service.AcquireClaims(ctx, acquisitionRequest(
		[]string{"lineage-a"}, nil)); !errors.Is(
		err, ErrRFQLineAlreadyAwarded) {
		t.Fatalf("err = %v, want ErrRFQLineAlreadyAwarded", err)
	}
}

// A gate failure unwinds the lineage claims already taken: the operation must
// not keep half a claim set after giving up.
func TestAcquireClaimsUnwindsLineagesWhenAGateFails(t *testing.T) {
	claims := newFakeLineClaimStore()
	eligibility := &fakeOfferEligibilityClaimant{
		err: ErrOfferVersionNotEligible}
	service := NewService(
		WithAwardLineClaimRepository(claims),
		WithOfferEligibilityClaimant(eligibility),
	)

	if _, err := service.AcquireClaims(context.Background(), acquisitionRequest(
		[]string{"lineage-a", "lineage-b"}, []string{"offer-1"},
	)); !errors.Is(err, ErrOfferVersionNotEligible) {
		t.Fatalf("err = %v, want ErrOfferVersionNotEligible", err)
	}

	want := []string{"lineage-b", "lineage-a"}
	if len(claims.released) != len(want) {
		t.Fatalf("released %v, want both lineages unwound %v",
			claims.released, want)
	}
}

// §8E: release is permitted ONLY after verifying no matching Award Revision
// exists. Phase E cannot make that check — it has no award knowledge — so it
// lives here. Assuming otherwise would release claims after an authoritative
// award, the exact failure D2 forbids.
func TestReleaseClaimsRefusesWhenARevisionAlreadyExists(t *testing.T) {
	service, claims, _ := claimService()
	ctx := context.Background()

	acquired, err := service.AcquireClaims(ctx, acquisitionRequest(
		[]string{"lineage-a"}, nil))
	if err != nil {
		t.Fatalf("AcquireClaims: %v", err)
	}
	claims.released = nil

	err = service.ReleaseClaims(ctx, ReleaseClaimsRequest{
		CompanyID: "company-1", OperationID: "op-1",
		Acquired: acquired,
		// A published revision exists: completing forward is the only
		// permitted action.
		AwardRevisionExists: true,
	})
	if !errors.Is(err, ErrAwardRevisionConflict) {
		t.Fatalf("err = %v, want ErrAwardRevisionConflict", err)
	}
	if len(claims.released) != 0 {
		t.Fatalf("released %v after a revision exists; release is prohibited",
			claims.released)
	}
}

// With no revision, an abandoned operation's own claims are released so the
// lineages become available again.
func TestReleaseClaimsReleasesOwnClaimsWhenNoRevisionExists(t *testing.T) {
	service, claims, _ := claimService()
	ctx := context.Background()

	acquired, err := service.AcquireClaims(ctx, acquisitionRequest(
		[]string{"lineage-a", "lineage-b"}, nil))
	if err != nil {
		t.Fatalf("AcquireClaims: %v", err)
	}
	claims.released = nil

	if err := service.ReleaseClaims(ctx, ReleaseClaimsRequest{
		CompanyID: "company-1", OperationID: "op-1",
		Acquired: acquired, AwardRevisionExists: false,
	}); err != nil {
		t.Fatalf("ReleaseClaims: %v", err)
	}
	if len(claims.released) != 2 {
		t.Fatalf("released %v, want both own claims", claims.released)
	}
}

// Completion marks both tiers awarded — the lineage claims and the offer gates.
func TestCompleteClaimsMarksBothTiersAwarded(t *testing.T) {
	service, claims, eligibility := claimService()
	ctx := context.Background()

	acquired, err := service.AcquireClaims(ctx, acquisitionRequest(
		[]string{"lineage-a"}, []string{"offer-1"}))
	if err != nil {
		t.Fatalf("AcquireClaims: %v", err)
	}

	if err := service.CompleteClaims(ctx, CompleteClaimsRequest{
		CompanyID: "company-1", OperationID: "op-1",
		AwardRevisionID: "revision-1", Acquired: acquired,
	}); err != nil {
		t.Fatalf("CompleteClaims: %v", err)
	}

	for _, claim := range claims.claims {
		if claim.State != LineClaimAwarded {
			t.Errorf("lineage %q state = %q, want awarded",
				claim.StableLineageID, claim.State)
		}
	}
	if len(eligibility.completed) != 1 {
		t.Fatalf("completed gates = %v, want one", eligibility.completed)
	}
}

// The service refuses without its repositories rather than silently claiming
// nothing, which would let a finalisation publish with no serialization at all.
func TestAcquireClaimsRequiresItsRepositories(t *testing.T) {
	service := NewService()

	if _, err := service.AcquireClaims(context.Background(),
		acquisitionRequest([]string{"lineage-a"}, nil)); !errors.Is(
		err, ErrAwardsNotConfigured) {
		t.Fatalf("err = %v, want ErrAwardsNotConfigured", err)
	}
}
