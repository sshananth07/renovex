package supplieroffers

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func claimSubmissionForReconciliation(t *testing.T, rig *submissionRig,
	operationID string) SupplierOfferDraft {
	t.Helper()
	calculated, err := CalculateSubmission(SubmissionCalculationInput{
		Draft:       rig.readyDraft,
		RFQ:         rig.service.issuedRFQ.(*fakeIssuedRFQSource).snapshot,
		SubmittedAt: rig.input.AccessedAt,
	})
	if err != nil {
		t.Fatalf("calculate submission claim: %v", err)
	}
	chain, found, err := rig.chains.FindChain(context.Background(), "company-1",
		rig.readyDraft.OfferChainID)
	if err != nil || !found {
		t.Fatalf("find offer chain: found=%v err=%v", found, err)
	}
	claimed, err := rig.drafts.ClaimDraftForSubmission(context.Background(),
		DraftSubmissionClaimInput{CompanyID: "company-1", DraftID: rig.readyDraft.ID,
			ExpectedRevision: rig.readyDraft.Revision, SubmissionOperationID: operationID,
			SubmissionBaseRevision:      rig.readyDraft.Revision,
			SubmissionFingerprint:       calculated.Fingerprint,
			SubmissionRecipientIdentity: rig.readyDraft.RecipientIdentity,
			SubmissionInvitationID:      rig.readyDraft.InvitationID,
			SubmissionRFQVersionID:      rig.readyDraft.IssuedRFQVersionID,
			SubmissionAccessGeneration:  4, CandidateOfferVersionID: bson.NewObjectID().Hex(),
			CandidateVersionNumber: chain.LatestSubmittedVersion + 1,
			ClaimedAt:              rig.input.AccessedAt})
	if err != nil {
		t.Fatalf("claim draft for reconciliation: %v", err)
	}
	return claimed
}

func TestReconcileCompletedSubmissionByAuthoritativeTupleIsIdempotent(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	input := ReconcileSupplierOfferInput{CompanyID: "company-1", ActorUserID: "owner-1",
		InvitationID: version.InvitationID, IssuedRFQVersionID: version.IssuedRFQVersionID,
		OperationID: version.SubmissionOperationID}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := rig.service.ReconcileSupplierOffer(context.Background(), input)
		if err != nil {
			t.Fatalf("reconcile attempt %d: %v", attempt+1, err)
		}
		if result.Kind != ReconciliationCompletedSubmission ||
			result.OfferVersionID != version.ID {
			t.Fatalf("result = %+v, want completed submission %s", result, version.ID)
		}
	}
}

func TestSupplierOfferReconciliationRouteIsOwnerAdminOnly(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	router, api := platformhttp.NewRouter("supplier-offer-reconciliation-test", "0.0.0")
	RegisterReconciliationHandlers(api, rig.service)
	body := []byte(`{"invitationId":"invitation-1","issuedRfqVersionId":"issued-version-2","operationId":"op-submit-1"}`)

	request := func(role string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/supplier-offer-reconciliations",
			bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
			UserID: "user-1", CompanyID: version.CompanyID, Role: role,
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	if rec := request(string(identity.RoleEmployee)); rec.Code != http.StatusForbidden {
		t.Fatalf("employee status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := request(string(identity.RoleOwner)); rec.Code != http.StatusOK {
		t.Fatalf("owner status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSupplierOfferReconciliationHTTPUsesBounded404And503(t *testing.T) {
	request := func(service *Service, body string) *httptest.ResponseRecorder {
		router, api := platformhttp.NewRouter("supplier-offer-reconciliation-errors", "0.0.0")
		RegisterReconciliationHandlers(api, service)
		req := httptest.NewRequest(http.MethodPost, "/supplier-offer-reconciliations",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
			UserID: "owner-1", CompanyID: "company-1", Role: string(identity.RoleOwner),
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rig, _ := newWithdrawalRig(t)
	missing := request(rig.service,
		`{"invitationId":"missing-invitation","issuedRfqVersionId":"missing-version","operationId":"missing-operation"}`)
	if missing.Code != http.StatusNotFound || bytes.Contains(missing.Body.Bytes(), []byte("company-1")) {
		t.Fatalf("missing tuple status=%d body=%s", missing.Code, missing.Body.String())
	}
	unresolved := request(NewService(),
		`{"invitationId":"invitation-1","issuedRfqVersionId":"issued-version-2","operationId":"operation-1"}`)
	if unresolved.Code != http.StatusServiceUnavailable || unresolved.Body.Len() > 4096 ||
		bytes.Contains(unresolved.Body.Bytes(), []byte("required capabilities")) {
		t.Fatalf("unresolved status=%d bytes=%d body=%s", unresolved.Code,
			unresolved.Body.Len(), unresolved.Body.String())
	}
}

func TestReconcileSupplierOfferTupleIsTenantScopedAndNonDisclosing(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	_, err := rig.service.ReconcileSupplierOffer(context.Background(),
		ReconcileSupplierOfferInput{CompanyID: "company-other", ActorUserID: "owner-2",
			InvitationID: version.InvitationID, IssuedRFQVersionID: version.IssuedRFQVersionID,
			OperationID: version.SubmissionOperationID})
	if !errors.Is(err, ErrOfferChainNotFound) {
		t.Fatalf("foreign tuple error = %v, want non-disclosing chain not found", err)
	}
}

func TestReconcileCompletesEarliestPreVersionSubmissionCrash(t *testing.T) {
	rig := newSubmissionRig(t)
	claimed := claimSubmissionForReconciliation(t, rig, "reconcile-submit-op")
	result, err := rig.service.ReconcileSupplierOffer(context.Background(),
		ReconcileSupplierOfferInput{CompanyID: claimed.CompanyID, ActorUserID: "owner-1",
			InvitationID: claimed.InvitationID, IssuedRFQVersionID: claimed.IssuedRFQVersionID,
			OperationID: claimed.SubmissionOperationID})
	if err != nil {
		t.Fatalf("reconcile pre-version crash: %v", err)
	}
	if result.OfferVersionID != claimed.CandidateOfferVersionID {
		t.Fatalf("result=%+v, want reserved candidate %s", result, claimed.CandidateOfferVersionID)
	}
	if _, found, err := rig.versions.FindVersion(context.Background(), claimed.CompanyID,
		claimed.CandidateOfferVersionID); err != nil || !found {
		t.Fatalf("immutable version found=%v err=%v", found, err)
	}
}

func TestConcurrentReconciliationOfOneSubmissionCreatesOneVersion(t *testing.T) {
	rig := newSubmissionRig(t)
	claimed := claimSubmissionForReconciliation(t, rig, "reconcile-race-op")
	input := ReconcileSupplierOfferInput{CompanyID: claimed.CompanyID, ActorUserID: "owner-1",
		InvitationID: claimed.InvitationID, IssuedRFQVersionID: claimed.IssuedRFQVersionID,
		OperationID: claimed.SubmissionOperationID}
	start := make(chan struct{})
	results := make([]SupplierOfferReconciliationResult, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for index := range results {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			results[index], errs[index] = rig.service.ReconcileSupplierOffer(context.Background(), input)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, err := range errs {
		if err != nil || results[index].OfferVersionID != claimed.CandidateOfferVersionID {
			t.Fatalf("reconcile %d result=%+v err=%v", index, results[index], err)
		}
	}
	versions, err := rig.versions.ListVersionsForChain(context.Background(),
		claimed.CompanyID, claimed.OfferChainID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%d err=%v, want exactly one", len(versions), err)
	}
}

func TestDifferentLiveSubmissionOperationConflictsWithoutCompletion(t *testing.T) {
	rig := newSubmissionRig(t)
	claimed := claimSubmissionForReconciliation(t, rig, "owning-submit-op")
	_, err := rig.service.ReconcileSupplierOffer(context.Background(),
		ReconcileSupplierOfferInput{CompanyID: claimed.CompanyID, ActorUserID: "owner-1",
			InvitationID: claimed.InvitationID, IssuedRFQVersionID: claimed.IssuedRFQVersionID,
			OperationID: "different-submit-op"})
	if !errors.Is(err, ErrOfferDraftConflict) {
		t.Fatalf("different operation error=%v, want conflict", err)
	}
	if _, found, findErr := rig.versions.FindVersion(context.Background(), claimed.CompanyID,
		claimed.CandidateOfferVersionID); findErr != nil || found {
		t.Fatalf("losing operation version found=%v err=%v", found, findErr)
	}
}

func TestReconcileCompletesDurableWithdrawalClaim(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	claimed, err := rig.eligibility.ClaimLatestEligibilityForWithdrawal(context.Background(),
		LatestWithdrawalClaimInput{CompanyID: version.CompanyID, OfferChainID: version.OfferChainID,
			OfferVersionID: version.ID, OperationID: "reconcile-withdraw-op",
			ClaimID: bson.NewObjectID().Hex(), WithdrawalReason: "recover this withdrawal",
			ClaimedAt: rig.input.AccessedAt})
	if err != nil {
		t.Fatalf("create durable withdrawal claim: %v", err)
	}
	result, err := rig.service.ReconcileSupplierOffer(context.Background(),
		ReconcileSupplierOfferInput{CompanyID: version.CompanyID, ActorUserID: "owner-1",
			InvitationID: version.InvitationID, IssuedRFQVersionID: version.IssuedRFQVersionID,
			OperationID: claimed.OperationID})
	if err != nil {
		t.Fatalf("reconcile withdrawal: %v", err)
	}
	if result.WithdrawalID != claimed.ClaimID || result.Kind != ReconciliationCompletedWithdrawal {
		t.Fatalf("result=%+v, want withdrawal claim %s", result, claimed.ClaimID)
	}
}
