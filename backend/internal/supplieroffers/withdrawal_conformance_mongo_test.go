package supplieroffers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var (
	errSimulatedTransactionAbort = errors.New("simulated abort after chain fence")
	errSimulatedCommitUncertain  = errors.New("simulated uncertain commit response")
)

type blockingWithdrawalEligibilityRepository struct {
	delegate *MongoOfferEligibilityRepository
	claimed  chan struct{}
	release  chan struct{}
}

type staleFirstSubmissionChainRepository struct {
	delegate *MongoOfferChainRepository
	stale    SupplierOfferChain
	first    bool
}

func (repository *staleFirstSubmissionChainRepository) FindChain(
	ctx context.Context, companyID, chainID string,
) (SupplierOfferChain, bool, error) {
	if repository.first {
		repository.first = false
		return repository.stale, true, nil
	}
	return repository.delegate.FindChain(ctx, companyID, chainID)
}

func (repository *staleFirstSubmissionChainRepository) AdvanceChainToVersion(
	ctx context.Context, companyID, chainID string, expectedRevision int64,
	versionID string, versionNumber int, updatedAt time.Time,
) (SupplierOfferChain, error) {
	return repository.delegate.AdvanceChainToVersion(ctx, companyID, chainID,
		expectedRevision, versionID, versionNumber, updatedAt)
}

func (repository *blockingWithdrawalEligibilityRepository) InsertEligibility(
	ctx context.Context, eligibility SupplierOfferEligibility,
) error {
	return repository.delegate.InsertEligibility(ctx, eligibility)
}

func (repository *blockingWithdrawalEligibilityRepository) FindEligibility(
	ctx context.Context, companyID, offerVersionID string,
) (SupplierOfferEligibility, bool, error) {
	return repository.delegate.FindEligibility(ctx, companyID, offerVersionID)
}

func (repository *blockingWithdrawalEligibilityRepository) ClaimEligibility(
	ctx context.Context, input EligibilityClaimInput,
) (SupplierOfferEligibility, error) {
	claimed, err := repository.delegate.ClaimEligibility(ctx, input)
	if err != nil {
		return SupplierOfferEligibility{}, err
	}
	close(repository.claimed)
	<-repository.release
	return claimed, nil
}

func (repository *blockingWithdrawalEligibilityRepository) ClaimLatestEligibilityForWithdrawal(
	ctx context.Context, input LatestWithdrawalClaimInput,
) (SupplierOfferEligibility, error) {
	// The delegate returns only after the chain fence and eligibility claim have
	// committed together. Blocking here forces V2 advancement at the actual
	// post-commit boundary rather than approximating the ordering with sleeps.
	claimed, err := repository.delegate.ClaimLatestEligibilityForWithdrawal(ctx, input)
	if err != nil {
		return SupplierOfferEligibility{}, err
	}
	close(repository.claimed)
	<-repository.release
	return claimed, nil
}

func (repository *blockingWithdrawalEligibilityRepository) CompleteEligibilityClaim(
	ctx context.Context, input EligibilityCompletionInput,
) (SupplierOfferEligibility, error) {
	return repository.delegate.CompleteEligibilityClaim(ctx, input)
}

func advanceOfferChainPastVersion(t *testing.T, rig *submissionRig,
	version SupplierOfferVersion) SupplierOfferChain {
	t.Helper()
	chain, found, err := rig.chains.FindChain(context.Background(),
		version.CompanyID, version.OfferChainID)
	if err != nil || !found {
		t.Fatalf("find chain: found=%v err=%v", found, err)
	}
	v2ID := bson.NewObjectID().Hex()
	advanced, err := rig.chains.AdvanceChainToVersion(context.Background(),
		version.CompanyID, version.OfferChainID, chain.Revision, v2ID,
		version.VersionNumber+1, version.SubmittedAt.Add(1))
	if err != nil {
		t.Fatalf("advance chain to V2: %v", err)
	}
	return advanced
}

// The latest-version rule is independent of offer expiry. A Supplier may
// withdraw the latest eligible commercial record even after its validity
// window has closed.
func TestWithdrawLatestExpiredEligibleVersionSucceeds(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	versionID, err := bson.ObjectIDFromHex(version.ID)
	if err != nil {
		t.Fatalf("parse version ID: %v", err)
	}
	// Persist an already-expired immutable fixture while keeping the Supplier
	// session at its normal test time. The production workflow never performs
	// this update; it models a version whose validity elapsed naturally.
	if _, err := rig.db.Collection(collectionSupplierOfferVersions).UpdateOne(
		context.Background(), bson.M{"_id": versionID}, bson.M{"$set": bson.M{
			"offerValidUntil": rig.input.AccessedAt.Add(-time.Minute),
		}}); err != nil {
		t.Fatalf("expire fixture: %v", err)
	}
	command := WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "remove the expired commercial offer", OperationID: "withdraw-expired",
	}
	if _, err := rig.service.WithdrawOffer(context.Background(), command); err != nil {
		t.Fatalf("latest expired withdrawal: %v", err)
	}
}

// This is the required submission-first ordering over real Mongo repositories.
// Once V2 is the authoritative latest pointer, a stale V1 read must not be able
// to acquire the unrelated eligibility CAS or create immutable history.
func TestSubmissionLinearizesBeforeWithdrawalRejectsV1WithoutSideEffects(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	advanceOfferChainPastVersion(t, rig, version)

	_, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "stale withdrawal", OperationID: "withdraw-after-v2",
	})
	if !errors.Is(err, ErrOfferEligibilityConflict) {
		t.Fatalf("withdrawal error = %v, want superseded conflict", err)
	}

	gate, found, findErr := rig.eligibility.FindEligibility(context.Background(),
		version.CompanyID, version.ID)
	if findErr != nil || !found || gate.State != EligibilityEligible {
		t.Fatalf("V1 eligibility = %+v, found=%v err=%v; want untouched eligible",
			gate, found, findErr)
	}
	if _, found, findErr := rig.service.withdrawals.FindWithdrawal(context.Background(),
		version.CompanyID, version.ID); findErr != nil || found {
		t.Fatalf("withdrawal found=%v err=%v; want no immutable withdrawal", found, findErr)
	}
}

// This is the inverse required ordering. The eligibility claim linearizes
// first, so a later V2 advance may supersede V1 without erasing the withdrawal.
func TestWithdrawalClaimLinearizesBeforeSubmissionAndBothComplete(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	blocking := &blockingWithdrawalEligibilityRepository{delegate: rig.eligibility,
		claimed: make(chan struct{}), release: make(chan struct{})}
	rig.service.eligibility = blocking
	type withdrawalResult struct {
		withdrawal SupplierOfferWithdrawal
		err        error
	}
	result := make(chan withdrawalResult, 1)
	go func() {
		withdrawal, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
			Context: rig.input, OfferVersionID: version.ID,
			Reason: "withdraw before replacement", OperationID: "withdraw-before-v2",
		})
		result <- withdrawalResult{withdrawal: withdrawal, err: err}
	}()
	<-blocking.claimed
	advanced := advanceOfferChainPastVersion(t, rig, version)
	if advanced.LatestSubmittedID == nil || *advanced.LatestSubmittedID == version.ID {
		t.Fatalf("chain did not advance: %+v", advanced)
	}

	close(blocking.release)
	completedWithdrawal := <-result
	if completedWithdrawal.err != nil ||
		completedWithdrawal.withdrawal.SupplierOfferVersionID != version.ID {
		t.Fatalf("complete claimed withdrawal: %+v err=%v",
			completedWithdrawal.withdrawal, completedWithdrawal.err)
	}
	completed, found, err := rig.eligibility.FindEligibility(context.Background(),
		version.CompanyID, version.ID)
	if err != nil || !found || completed.State != EligibilityWithdrawn {
		t.Fatalf("completed eligibility = %+v, found=%v err=%v", completed, found, err)
	}
	latestID := *advanced.LatestSubmittedID
	projection := EvaluateOfferVersionState(OfferVersionStateFacts{
		OfferVersionID: version.ID, LatestSubmittedVersionID: latestID,
		EligibilityState: completed.State, EligibilityClaimType: completed.ClaimType,
		OfferValidUntil: version.OfferValidUntil, EvaluatedAt: rig.input.AccessedAt,
	})
	if projection.Status != OfferVersionStatusWithdrawn || !projection.IsSuperseded {
		t.Fatalf("V1 projection = %+v, want withdrawn plus superseded", projection)
	}
}

// Returning an error after the transactional chain write must abort both
// documents. The hook is inside the session, immediately after the fence.
func TestWithdrawalTransactionAbortRollsBackFenceAndEligibility(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	chainBefore, found, err := rig.chains.FindChain(context.Background(),
		version.CompanyID, version.OfferChainID)
	if err != nil || !found {
		t.Fatalf("find chain before abort: found=%v err=%v", found, err)
	}
	rig.eligibility.transaction.afterChainFence = func(context.Context) error {
		return errSimulatedTransactionAbort
	}

	_, err = rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "abort this transaction", OperationID: "withdraw-abort",
	})
	if !errors.Is(err, errSimulatedTransactionAbort) {
		t.Fatalf("withdrawal error = %v, want simulated abort", err)
	}
	chainAfter, found, err := rig.chains.FindChain(context.Background(),
		version.CompanyID, version.OfferChainID)
	if err != nil || !found || chainAfter.Revision != chainBefore.Revision {
		t.Fatalf("chain after abort = %+v found=%v err=%v, want revision %d",
			chainAfter, found, err, chainBefore.Revision)
	}
	gate, found, err := rig.eligibility.FindEligibility(context.Background(),
		version.CompanyID, version.ID)
	if err != nil || !found || gate.State != EligibilityEligible {
		t.Fatalf("eligibility after abort = %+v found=%v err=%v", gate, found, err)
	}
	if _, found, err := rig.service.withdrawals.FindWithdrawal(context.Background(),
		version.CompanyID, version.ID); err != nil || found {
		t.Fatalf("withdrawal after abort found=%v err=%v", found, err)
	}
}

// The post-commit hook models a lost/uncertain response at the exact boundary:
// Mongo has committed the claim but immutable history has not been inserted.
func TestUncertainCommitAndPostCommitCrashResumeOneStoredClaim(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	first := true
	rig.eligibility.transaction.afterCommit = func(SupplierOfferEligibility) error {
		if first {
			first = false
			return errSimulatedCommitUncertain
		}
		return nil
	}
	command := WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "resume the durable claim", OperationID: "withdraw-uncertain",
	}
	if _, err := rig.service.WithdrawOffer(context.Background(), command); !errors.Is(err, errSimulatedCommitUncertain) {
		t.Fatalf("first response error = %v, want simulated uncertainty", err)
	}
	claimed, found, err := rig.eligibility.FindEligibility(context.Background(),
		version.CompanyID, version.ID)
	if err != nil || !found || claimed.State != EligibilityWithdrawalClaimed {
		t.Fatalf("durable claim = %+v found=%v err=%v", claimed, found, err)
	}

	withdrawal, err := rig.service.WithdrawOffer(context.Background(), command)
	if err != nil {
		t.Fatalf("same-operation recovery: %v", err)
	}
	if withdrawal.ID != claimed.ClaimID || withdrawal.Reason != claimed.WithdrawalReason ||
		claimed.ClaimedAt == nil || !withdrawal.WithdrawnAt.Equal(*claimed.ClaimedAt) {
		t.Fatalf("withdrawal = %+v, claim = %+v", withdrawal, claimed)
	}
	count, err := rig.db.Collection(collectionSupplierOfferWithdrawals).CountDocuments(
		context.Background(), bson.M{"companyId": version.CompanyID,
			"supplierOfferVersionId": version.ID})
	if err != nil || count != 1 {
		t.Fatalf("immutable withdrawal count = %d, err=%v", count, err)
	}
}

func TestSameWithdrawalOperationWithChangedReasonConflictsWithoutMutation(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	command := WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "original reason", OperationID: "withdraw-reason",
	}
	first, err := rig.service.WithdrawOffer(context.Background(), command)
	if err != nil {
		t.Fatalf("first withdrawal: %v", err)
	}
	command.Reason = "changed reason"
	if _, err := rig.service.WithdrawOffer(context.Background(), command); !errors.Is(err, ErrOfferEligibilityConflict) {
		t.Fatalf("changed-reason error = %v, want operation conflict", err)
	}
	reloaded, found, err := rig.service.withdrawals.FindWithdrawal(context.Background(),
		version.CompanyID, version.ID)
	if err != nil || !found || reloaded.ID != first.ID || reloaded.Reason != "original reason" {
		t.Fatalf("withdrawal mutated: %+v found=%v err=%v", reloaded, found, err)
	}
}

// Submission may have read the chain immediately before a withdrawal commits
// its fence. The first CAS is therefore stale; recovery must reread and advance
// the already-created V2 rather than insert another immutable version.
func TestFenceInducedStaleSubmissionCASRereadsAndAdvancesWithoutDuplicate(t *testing.T) {
	rig, v1 := newWithdrawalRig(t)
	stale, found, err := rig.chains.FindChain(context.Background(),
		v1.CompanyID, v1.OfferChainID)
	if err != nil || !found {
		t.Fatalf("find stale chain snapshot: found=%v err=%v", found, err)
	}
	if _, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: v1.ID,
		Reason: "fence before V2", OperationID: "withdraw-before-stale-v2",
	}); err != nil {
		t.Fatalf("withdraw V1: %v", err)
	}

	v2 := v1
	v2.ID = bson.NewObjectID().Hex()
	v2.VersionNumber = 2
	v2.SourceDraftID = bson.NewObjectID().Hex()
	v2.SubmissionOperationID = "submit-after-withdrawal-fence"
	v2.SubmittedAt = v1.SubmittedAt.Add(time.Minute)
	if err := rig.versions.InsertVersion(context.Background(), v2); err != nil {
		t.Fatalf("insert reserved V2: %v", err)
	}
	chains := &staleFirstSubmissionChainRepository{
		delegate: rig.chains, stale: stale, first: true,
	}
	if err := advanceSubmissionChainAfterFence(context.Background(), chains, v2); err != nil {
		t.Fatalf("recover fence-stale submission: %v", err)
	}
	advanced, found, err := rig.chains.FindChain(context.Background(),
		v1.CompanyID, v1.OfferChainID)
	if err != nil || !found || advanced.LatestSubmittedID == nil ||
		*advanced.LatestSubmittedID != v2.ID {
		t.Fatalf("advanced chain = %+v found=%v err=%v", advanced, found, err)
	}
	count, err := rig.db.Collection(collectionSupplierOfferVersions).CountDocuments(
		context.Background(), bson.M{"_id": v2.ID})
	if err != nil || count != 1 {
		t.Fatalf("V2 count = %d, err=%v", count, err)
	}
}

func TestTransactionalWithdrawalVersusAwardStillHasExactlyOneWinner(t *testing.T) {
	t.Run("award commits while withdrawal transaction is before eligibility CAS", func(t *testing.T) {
		rig, version := newWithdrawalRig(t)
		reached := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		rig.eligibility.transaction.afterChainFence = func(context.Context) error {
			once.Do(func() {
				close(reached)
				<-release
			})
			return nil
		}
		withdrawResult := make(chan error, 1)
		go func() {
			_, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
				Context: rig.input, OfferVersionID: version.ID,
				Reason: "compete with award", OperationID: "withdraw-versus-award",
			})
			withdrawResult <- err
		}()
		<-reached
		gate, found, err := rig.eligibility.FindEligibility(context.Background(),
			version.CompanyID, version.ID)
		if err != nil || !found {
			t.Fatalf("find eligibility: found=%v err=%v", found, err)
		}
		_, awardErr := rig.eligibility.ClaimEligibility(context.Background(),
			EligibilityClaimInput{
				CompanyID: version.CompanyID, OfferVersionID: version.ID,
				ExpectedRevision: gate.Revision, ClaimType: EligibilityClaimAward,
				OperationID: "award-operation", ClaimID: "award-revision-1",
				ClaimedAt: rig.input.AccessedAt,
			})
		if awardErr != nil {
			t.Fatalf("award claim: %v", awardErr)
		}
		close(release)
		if err := <-withdrawResult; !errors.Is(err, ErrOfferEligibilityConflict) {
			t.Fatalf("withdrawal error = %v, want one-winner conflict", err)
		}
		final, found, err := rig.eligibility.FindEligibility(context.Background(),
			version.CompanyID, version.ID)
		if err != nil || !found || final.State != EligibilityAwardClaimed {
			t.Fatalf("final eligibility = %+v found=%v err=%v", final, found, err)
		}
	})

	t.Run("withdrawal commits before award", func(t *testing.T) {
		rig, version := newWithdrawalRig(t)
		if _, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
			Context: rig.input, OfferVersionID: version.ID,
			Reason: "withdraw before award", OperationID: "withdraw-wins-award",
		}); err != nil {
			t.Fatalf("withdrawal: %v", err)
		}
		gate, found, err := rig.eligibility.FindEligibility(context.Background(),
			version.CompanyID, version.ID)
		if err != nil || !found {
			t.Fatalf("find eligibility: found=%v err=%v", found, err)
		}
		_, err = rig.eligibility.ClaimEligibility(context.Background(), EligibilityClaimInput{
			CompanyID: version.CompanyID, OfferVersionID: version.ID,
			ExpectedRevision: gate.Revision, ClaimType: EligibilityClaimAward,
			OperationID: "late-award", ClaimID: "award-revision-late",
			ClaimedAt: rig.input.AccessedAt,
		})
		if !errors.Is(err, ErrOfferEligibilityConflict) {
			t.Fatalf("late award error = %v, want conflict", err)
		}
	})
}

func TestCanWithdrawProjectionAndMutationShareTheLatestEligibleRule(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	read := SupplierOfferReadContextInput{
		SessionToken: rig.input.SessionToken, InvitationID: rig.input.InvitationID,
		AccessedAt: rig.input.AccessedAt,
	}
	before, err := rig.service.GetSupplierOfferVersion(context.Background(),
		SupplierOfferVersionDetailInput{
			SupplierOfferReadContextInput: read, OfferVersionID: version.ID,
		})
	if err != nil || !before.CanWithdraw || before.PublicStatus != OfferVersionStatusEligible {
		t.Fatalf("before withdrawal = %+v err=%v", before, err)
	}
	if _, err := rig.service.WithdrawOffer(context.Background(), WithdrawOfferCommand{
		Context: rig.input, OfferVersionID: version.ID,
		Reason: "projection agreement", OperationID: "withdraw-projection-agreement",
	}); err != nil {
		t.Fatalf("withdraw latest eligible: %v", err)
	}
	advanced := advanceOfferChainPastVersion(t, rig, version)
	after, err := rig.service.GetSupplierOfferVersion(context.Background(),
		SupplierOfferVersionDetailInput{
			SupplierOfferReadContextInput: read, OfferVersionID: version.ID,
		})
	if err != nil || after.CanWithdraw || !after.IsSuperseded ||
		after.PublicStatus != OfferVersionStatusWithdrawn || after.Withdrawal == nil {
		t.Fatalf("after withdrawal/V2 = %+v err=%v chain=%+v", after, err, advanced)
	}
}
