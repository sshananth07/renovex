package awards_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// F2 route contract (§8C). Per Decision A these draft routes are open to owner,
// admin AND employee: a provisional draft claims nothing and has no externally
// visible effect. Owner/admin-only begins at finalisation.

type draftRoutesStore struct {
	chains    map[string]awards.AwardDecisionChain
	drafts    map[string]awards.AwardDraft
	revisions map[string]awards.AwardRevision
	claims    map[string]awards.AwardLineClaim
	outcomes  map[string]awards.AwardOutcome

	deliveries   map[string]awards.AwardOutcomeDelivery
	deliveryByOp map[string]string
	mails        []awards.AwardOutcomeNotification
}

func newDraftRoutesStore() *draftRoutesStore {
	return &draftRoutesStore{
		chains:    map[string]awards.AwardDecisionChain{},
		drafts:    map[string]awards.AwardDraft{},
		revisions: map[string]awards.AwardRevision{},
		claims:    map[string]awards.AwardLineClaim{},
		outcomes:  map[string]awards.AwardOutcome{},

		deliveries:   map[string]awards.AwardOutcomeDelivery{},
		deliveryByOp: map[string]string{},
	}
}

func (s *draftRoutesStore) EnsureAwardChain(
	_ context.Context, companyID, rfqChainID, issuedRFQVersionID string,
) (awards.AwardDecisionChain, error) {
	key := companyID + "|" + issuedRFQVersionID
	if chain, ok := s.chains[key]; ok {
		return chain, nil
	}
	chain := awards.AwardDecisionChain{
		ID: "chain-1", CompanyID: companyID, RFQChainID: rfqChainID,
		IssuedRFQVersionID: issuedRFQVersionID,
		FinalisationState:  awards.FinalisationDraft, Revision: 1,
	}
	s.chains[key] = chain
	return chain, nil
}

func (s *draftRoutesStore) FindChain(
	_ context.Context, companyID, chainID string,
) (awards.AwardDecisionChain, bool, error) {
	for _, chain := range s.chains {
		if chain.ID == chainID && chain.CompanyID == companyID {
			return chain, true, nil
		}
	}
	return awards.AwardDecisionChain{}, false, nil
}

func (s *draftRoutesStore) FindChainByIssuedVersion(
	_ context.Context, companyID, issuedRFQVersionID string,
) (awards.AwardDecisionChain, bool, error) {
	chain, ok := s.chains[companyID+"|"+issuedRFQVersionID]
	return chain, ok, nil
}

func (s *draftRoutesStore) EnsureOpenDraft(
	_ context.Context, candidate awards.AwardDraft,
) (awards.AwardDraft, bool, error) {
	key := candidate.CompanyID + "|" + candidate.AwardChainID
	if draft, ok := s.drafts[key]; ok {
		return draft, false, nil
	}
	draft := candidate
	draft.ID = "draft-1"
	draft.Status = awards.AwardDraftOpen
	draft.Revision = 1
	s.drafts[key] = draft
	return draft, true, nil
}

func (s *draftRoutesStore) FindOpenDraft(
	_ context.Context, companyID, awardChainID string,
) (awards.AwardDraft, bool, error) {
	draft, ok := s.drafts[companyID+"|"+awardChainID]
	return draft, ok, nil
}

func (s *draftRoutesStore) FindDraft(
	_ context.Context, companyID, draftID string,
) (awards.AwardDraft, bool, error) {
	for _, draft := range s.drafts {
		if draft.ID == draftID && draft.CompanyID == companyID {
			return draft, true, nil
		}
	}
	return awards.AwardDraft{}, false, nil
}

func (s *draftRoutesStore) ReplaceDecisions(
	_ context.Context, companyID, draftID string,
	expectedRevision int64, decisions []awards.AwardLineDecisionDraft,
) (awards.AwardDraft, error) {
	for key, draft := range s.drafts {
		if draft.ID != draftID || draft.CompanyID != companyID {
			continue
		}
		if draft.Status != awards.AwardDraftOpen ||
			draft.Revision != expectedRevision {
			return awards.AwardDraft{}, awards.ErrAwardDraftConflict
		}
		draft.LineDecisions = decisions
		draft.Revision++
		s.drafts[key] = draft
		return draft, nil
	}
	return awards.AwardDraft{}, awards.ErrAwardDraftNotFound
}

func (s *draftRoutesStore) ArchiveDraft(
	_ context.Context, companyID, draftID string, expectedRevision int64,
) error {
	for key, draft := range s.drafts {
		if draft.ID != draftID || draft.CompanyID != companyID {
			continue
		}
		if draft.Status != awards.AwardDraftOpen ||
			draft.Revision != expectedRevision {
			return awards.ErrAwardDraftConflict
		}
		delete(s.drafts, key)
		return nil
	}
	return awards.ErrAwardDraftNotFound
}

func draftRouter(t *testing.T, role, companyID string) (
	http.Handler, *draftRoutesStore) {
	t.Helper()
	store := newDraftRoutesStore()

	issued := &stubComparisonSource{
		issued: awards.IssuedRFQSnapshot{
			ID: "issued-1", CompanyID: "company-1", RFQChainID: "rfqchain-1",
			Currency: "MYR",
			Lines: []awards.IssuedRFQLineSnapshot{
				{ID: "line-1", LineageID: "lineage-1"},
				{ID: "line-2", LineageID: "lineage-2"},
			},
		},
		found: true,
	}
	// Every Phase F repository is wired, so a route failing here reports a real
	// authorization or isolation result rather than a missing dependency.
	service := awards.NewService(
		awards.WithIssuedRFQSource(issued),
		awards.WithOfferVersionSource(issued),
		awards.WithAwardChainRepository(store),
		awards.WithAwardDraftRepository(store),
		awards.WithAwardRevisionRepository(store),
		awards.WithAwardLineClaimRepository(store),
		awards.WithOfferEligibilityClaimant(store),
		awards.WithAwardOutcomeRepository(store),
		awards.WithAwardDeliveryRepository(store),
		awards.WithAwardNotificationMailer(store),
		awards.WithInvitationLinkSource(store),
	)

	router, api := platformhttp.NewRouter("awards-test", "0.0.0")
	api.UseMiddleware(func(hctx huma.Context, next func(huma.Context)) {
		ctx := identity.ContextWithPrincipal(hctx.Context(), identity.Principal{
			UserID: "user-1", CompanyID: companyID, Role: role,
		})
		next(huma.WithContext(hctx, ctx))
	})
	awards.RegisterHandlers(api, service)
	return router, store
}

func do(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const draftBase = "/rfq-chains/rfqchain-1/issued-versions/issued-1/award-draft"

// A provisional draft has no externally visible effect, so all three roles may
// create, read, edit and discard it (Decision A §1A.1, §8C).
func TestAwardDraftRoutesAreOpenToOwnerAdminAndEmployee(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			router, _ := draftRouter(t, role, "company-1")

			if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusCreated {
				t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
			}
			if rec := do(t, router, http.MethodGet, draftBase, nil); rec.Code != http.StatusOK {
				t.Fatalf("read status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			rec := do(t, router, http.MethodPut, draftBase+"/lines/line-1/selection",
				map[string]any{
					"offerVersionId": "offer-1", "offerLineId": "ol-1",
					"expectedRevision": 1,
				})
			if rec.Code != http.StatusOK {
				t.Fatalf("select status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			rec = do(t, router, http.MethodPut, draftBase+"/lines/line-2/unawarded",
				map[string]any{
					"reason": "scope_cancelled", "expectedRevision": 2,
				})
			if rec.Code != http.StatusOK {
				t.Fatalf("unaward status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			rec = do(t, router, http.MethodDelete, draftBase+"?expectedRevision=3", nil)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("discard status = %d, want 204; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A foreign tenant gets 404, never 403: a 403 would confirm the issued version
// exists in some other company.
func TestAwardDraftRoutesReturn404ForForeignTenant(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-2")

	if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("create status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(t, router, http.MethodGet, draftBase, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("read status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// An unbounded unawarded reason must be refused at the boundary, so an
// unrecognised string can never reach a published revision.
func TestUnawardRouteRefusesAnUnboundedReason(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-1")
	if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}

	rec := do(t, router, http.MethodPut, draftBase+"/lines/line-1/unawarded",
		map[string]any{"reason": "because_i_said_so", "expectedRevision": 1})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

// A stale expected revision is a 409 the caller can retry against fresh state,
// not a 422: nothing about the content was wrong.
func TestSelectionRouteReturns409ForAStaleRevision(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-1")
	if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}

	rec := do(t, router, http.MethodPut, draftBase+"/lines/line-1/selection",
		map[string]any{
			"offerVersionId": "offer-1", "offerLineId": "ol-1",
			"expectedRevision": 99,
		})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

// The response must expose the revision the next edit has to present, or a
// client cannot participate in the CAS protocol at all.
func TestAwardDraftResponseCarriesItsRevisionAndDecisions(t *testing.T) {
	router, _ := draftRouter(t, "owner", "company-1")
	if rec := do(t, router, http.MethodPost, draftBase, nil); rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", rec.Code)
	}
	rec := do(t, router, http.MethodPut, draftBase+"/lines/line-1/selection",
		map[string]any{
			"offerVersionId": "offer-1", "offerLineId": "ol-1",
			"expectedRevision": 1,
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("select status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		Revision  int64  `json:"revision"`
		Decisions []struct {
			IssuedRFQLineID string `json:"issuedRfqLineId"`
			StableLineageID string `json:"stableLineageId"`
			Decision        string `json:"decision"`
			OfferVersionID  string `json:"offerVersionId"`
		} `json:"lineDecisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode draft: %v; body=%s", err, rec.Body.String())
	}
	if body.Revision != 2 {
		t.Errorf("revision = %d, want 2", body.Revision)
	}
	if len(body.Decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(body.Decisions))
	}
	if body.Decisions[0].StableLineageID != "lineage-1" {
		t.Errorf("lineage = %q, want the issued version's lineage",
			body.Decisions[0].StableLineageID)
	}
}

// The route store also plays the revision, line-claim and eligibility roles, so
// a route test exercises the whole composed surface rather than failing on a
// missing dependency.
func (s *draftRoutesStore) InsertRevision(
	_ context.Context, candidate awards.AwardRevision,
) (awards.AwardRevision, error) {
	for _, existing := range s.revisions {
		if existing.CompanyID == candidate.CompanyID &&
			existing.FinalisationOperationID == candidate.FinalisationOperationID {
			if existing.SelectionFingerprint == candidate.SelectionFingerprint {
				return existing, nil
			}
			return awards.AwardRevision{}, awards.ErrAwardRevisionConflict
		}
	}
	candidate.ID = fmt.Sprintf("revision-%d", len(s.revisions)+1)
	s.revisions[candidate.ID] = candidate
	return candidate, nil
}

func (s *draftRoutesStore) FindRevision(
	_ context.Context, companyID, revisionID string,
) (awards.AwardRevision, bool, error) {
	revision, ok := s.revisions[revisionID]
	if !ok || revision.CompanyID != companyID {
		return awards.AwardRevision{}, false, nil
	}
	return revision, true, nil
}

func (s *draftRoutesStore) FindRevisionByOperation(
	_ context.Context, companyID, operationID string,
) (awards.AwardRevision, bool, error) {
	for _, revision := range s.revisions {
		if revision.CompanyID == companyID &&
			revision.FinalisationOperationID == operationID {
			return revision, true, nil
		}
	}
	return awards.AwardRevision{}, false, nil
}

func (s *draftRoutesStore) FindLatestRevision(
	_ context.Context, companyID, chainID string,
) (awards.AwardRevision, bool, error) {
	var latest awards.AwardRevision
	found := false
	for _, revision := range s.revisions {
		if revision.CompanyID != companyID || revision.AwardChainID != chainID {
			continue
		}
		if !found || revision.RevisionNumber > latest.RevisionNumber {
			latest, found = revision, true
		}
	}
	return latest, found, nil
}

func (s *draftRoutesStore) ListRevisions(
	_ context.Context, companyID, chainID string,
) ([]awards.AwardRevision, error) {
	revisions := []awards.AwardRevision{}
	for _, revision := range s.revisions {
		if revision.CompanyID == companyID && revision.AwardChainID == chainID {
			revisions = append(revisions, revision)
		}
	}
	return revisions, nil
}

func (s *draftRoutesStore) ClaimFinalisation(
	_ context.Context, input awards.FinalisationClaimInput,
) (awards.AwardDecisionChain, error) {
	for key, chain := range s.chains {
		if chain.ID != input.AwardChainID || chain.CompanyID != input.CompanyID {
			continue
		}
		if chain.FinalisationState != awards.FinalisationDraft {
			return awards.AwardDecisionChain{}, awards.ErrAwardRevisionConflict
		}
		chain.FinalisationState = awards.FinalisationFinalising
		chain.FinalisingOperationID = input.OperationID
		chain.FinalisingRevisionID = input.CandidateRevisionID
		chain.Revision++
		s.chains[key] = chain
		return chain, nil
	}
	return awards.AwardDecisionChain{}, awards.ErrAwardChainNotFound
}

func (s *draftRoutesStore) PublishRevision(
	_ context.Context, input awards.PublishRevisionInput,
) (awards.AwardDecisionChain, error) {
	for key, chain := range s.chains {
		if chain.ID != input.AwardChainID || chain.CompanyID != input.CompanyID {
			continue
		}
		revisionID := input.AwardRevisionID
		chain.FinalisationState = awards.FinalisationPublished
		chain.CurrentAwardRevisionID = &revisionID
		chain.LatestRevisionNumber = input.RevisionNumber
		chain.FinalisingOperationID = ""
		chain.FinalisingRevisionID = ""
		chain.Revision++
		s.chains[key] = chain
		return chain, nil
	}
	return awards.AwardDecisionChain{}, awards.ErrAwardChainNotFound
}

func (s *draftRoutesStore) AbandonFinalisation(
	_ context.Context, companyID, chainID, operationID string, _ int64,
) error {
	for key, chain := range s.chains {
		if chain.ID != chainID || chain.CompanyID != companyID {
			continue
		}
		chain.FinalisationState = awards.FinalisationDraft
		chain.FinalisingOperationID = ""
		chain.FinalisingRevisionID = ""
		chain.Revision++
		s.chains[key] = chain
		return nil
	}
	return awards.ErrAwardChainNotFound
}

func (s *draftRoutesStore) ClaimLineage(
	_ context.Context, input awards.AwardLineClaimInput,
) (awards.AwardLineClaim, error) {
	key := input.CompanyID + "|" + input.RFQChainID + "|" + input.StableLineageID
	if existing, held := s.claims[key]; held {
		if existing.AwardOperationID == input.AwardOperationID {
			return existing, nil
		}
		return awards.AwardLineClaim{}, awards.ErrRFQLineAwardConflict
	}
	claim := awards.AwardLineClaim{
		ID: key, CompanyID: input.CompanyID, RFQChainID: input.RFQChainID,
		StableLineageID: input.StableLineageID, State: awards.LineClaimClaimed,
		AwardOperationID: input.AwardOperationID, Revision: 1,
	}
	s.claims[key] = claim
	return claim, nil
}

func (s *draftRoutesStore) FindClaim(
	_ context.Context, companyID, claimID string,
) (awards.AwardLineClaim, bool, error) {
	claim, ok := s.claims[claimID]
	return claim, ok && claim.CompanyID == companyID, nil
}

func (s *draftRoutesStore) FindClaimByLineage(
	_ context.Context, companyID, rfqChainID, lineageID string,
) (awards.AwardLineClaim, bool, error) {
	claim, ok := s.claims[companyID+"|"+rfqChainID+"|"+lineageID]
	return claim, ok, nil
}

func (s *draftRoutesStore) ListClaimsForOperation(
	_ context.Context, companyID, operationID string,
) ([]awards.AwardLineClaim, error) {
	var claims []awards.AwardLineClaim
	for _, claim := range s.claims {
		if claim.CompanyID == companyID && claim.AwardOperationID == operationID {
			claims = append(claims, claim)
		}
	}
	return claims, nil
}

func (s *draftRoutesStore) MarkLineageAwarded(
	_ context.Context, companyID, claimID, operationID, revisionID string, _ int64,
) error {
	claim, ok := s.claims[claimID]
	if !ok || claim.CompanyID != companyID {
		return awards.ErrRFQLineAwardConflict
	}
	claim.State = awards.LineClaimAwarded
	claim.AwardRevisionID = revisionID
	s.claims[claimID] = claim
	return nil
}

func (s *draftRoutesStore) ReleaseLineageClaim(
	_ context.Context, companyID, claimID, operationID string, _ int64,
) error {
	delete(s.claims, claimID)
	return nil
}

func (s *draftRoutesStore) ClaimOfferForAward(
	_ context.Context, request awards.OfferEligibilityClaimRequest,
) (awards.OfferEligibilitySnapshot, error) {
	return awards.OfferEligibilitySnapshot{
		OfferVersionID: request.OfferVersionID,
		State:          awards.OfferEligibilityAwardClaimed,
		OperationID:    request.OperationID,
		Revision:       request.ExpectedRevision + 1,
	}, nil
}

func (s *draftRoutesStore) CompleteOfferAward(
	_ context.Context, request awards.OfferEligibilityCompletionRequest,
) (awards.OfferEligibilitySnapshot, error) {
	return awards.OfferEligibilitySnapshot{
		OfferVersionID: request.OfferVersionID,
		State:          awards.OfferEligibilityAwarded,
	}, nil
}

func (s *draftRoutesStore) ReleaseOfferAwardClaim(
	_ context.Context, request awards.OfferEligibilityReleaseRequest,
) (awards.OfferEligibilitySnapshot, error) {
	return awards.OfferEligibilitySnapshot{
		OfferVersionID: request.OfferVersionID,
		State:          awards.OfferEligibilityEligible,
	}, nil
}

func (s *draftRoutesStore) GetOfferEligibility(
	_ context.Context, _, offerVersionID string,
) (awards.OfferEligibilitySnapshot, bool, error) {
	return awards.OfferEligibilitySnapshot{
		OfferVersionID: offerVersionID,
		State:          awards.OfferEligibilityEligible,
	}, true, nil
}

// The route store also plays the outcome repository role.
func (s *draftRoutesStore) EnsureOutcome(
	_ context.Context, candidate awards.AwardOutcome,
) (awards.AwardOutcome, bool, error) {
	key := candidate.CompanyID + "|" + candidate.AwardRevisionID + "|" +
		candidate.SupplierID
	if existing, ok := s.outcomes[key]; ok {
		return existing, false, nil
	}
	candidate.ID = fmt.Sprintf("outcome-%d", len(s.outcomes)+1)
	s.outcomes[key] = candidate
	return candidate, true, nil
}

func (s *draftRoutesStore) FindOutcome(
	_ context.Context, companyID, outcomeID string,
) (awards.AwardOutcome, bool, error) {
	for _, outcome := range s.outcomes {
		if outcome.ID == outcomeID && outcome.CompanyID == companyID {
			return outcome, true, nil
		}
	}
	return awards.AwardOutcome{}, false, nil
}

func (s *draftRoutesStore) FindOutcomeForSupplier(
	_ context.Context, companyID, outcomeID, supplierID, invitationID string,
) (awards.AwardOutcome, bool, error) {
	for _, outcome := range s.outcomes {
		if outcome.ID == outcomeID && outcome.CompanyID == companyID &&
			outcome.SupplierID == supplierID &&
			outcome.InvitationID == invitationID {
			return outcome, true, nil
		}
	}
	return awards.AwardOutcome{}, false, nil
}

func (s *draftRoutesStore) ListOutcomes(
	_ context.Context, companyID, awardRevisionID string,
) ([]awards.AwardOutcome, error) {
	outcomes := []awards.AwardOutcome{}
	for _, outcome := range s.outcomes {
		if outcome.CompanyID == companyID &&
			outcome.AwardRevisionID == awardRevisionID {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes, nil
}

// seedOutcome plants a published revision and one selected outcome, which is
// the state every outcome-route assertion needs.
func seedOutcome(t *testing.T, store *draftRoutesStore) {
	t.Helper()

	store.revisions["revision-1"] = awards.AwardRevision{
		ID: "revision-1", CompanyID: "company-1", AwardChainID: "chain-1",
		RFQChainID: "rfqchain-1", IssuedRFQVersionID: "issued-1",
		RevisionNumber: 1, FinalisationOperationID: "op-1",
		SelectionFingerprint: "fingerprint-1",
		GrandAwardTotal:      money.New(18_100, "MYR"),
	}
	store.outcomes["company-1|revision-1|supplier-a"] = awards.AwardOutcome{
		ID: "outcome-1", CompanyID: "company-1", AwardChainID: "chain-1",
		AwardRevisionID: "revision-1", RFQChainID: "rfqchain-1",
		IssuedRFQVersionID: "issued-1",
		SupplierID:         "supplier-a", InvitationID: "invitation-a",
		Result: awards.OutcomeSelected,
		Projection: awards.OutcomeProjection{
			Result: awards.OutcomeSelected,
			AwardedLines: []awards.OutcomeAwardedLine{{
				IssuedRFQLineID: "line-1", MaterialName: "Tile",
				UnitPrice:     money.New(1_000, "MYR"),
				LineSubtotal:  money.New(10_000, "MYR"),
				LineTaxAmount: money.New(600, "MYR"),
			}},
			LineSubtotal:   money.New(10_000, "MYR"),
			TaxTotal:       money.New(600, "MYR"),
			ChargeTotal:    money.New(0, "MYR"),
			DeliveryCharge: money.New(3_000, "MYR"),
			AwardTotal:     money.New(13_600, "MYR"),
		},
	}
}

// The route store also plays the delivery repository and the mailer, so a
// notification route test exercises the real send ordering.
func (s *draftRoutesStore) EnsureDeliveryIntent(
	_ context.Context, candidate awards.AwardOutcomeDelivery,
) (awards.AwardOutcomeDelivery, bool, error) {
	if err := candidate.Validate(); err != nil {
		return awards.AwardOutcomeDelivery{}, false, err
	}
	key := candidate.CompanyID + "|" + candidate.DeliveryOperationID
	if id, ok := s.deliveryByOp[key]; ok {
		return s.deliveries[id], false, nil
	}
	candidate.ID = fmt.Sprintf("delivery-%d", len(s.deliveries)+1)
	s.deliveries[candidate.ID] = candidate
	s.deliveryByOp[key] = candidate.ID
	return candidate, true, nil
}

func (s *draftRoutesStore) FindDelivery(
	_ context.Context, companyID, deliveryID string,
) (awards.AwardOutcomeDelivery, bool, error) {
	delivery, ok := s.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID {
		return awards.AwardOutcomeDelivery{}, false, nil
	}
	return delivery, true, nil
}

func (s *draftRoutesStore) FindDeliveryByOperation(
	_ context.Context, companyID, operationID string,
) (awards.AwardOutcomeDelivery, bool, error) {
	id, ok := s.deliveryByOp[companyID+"|"+operationID]
	if !ok {
		return awards.AwardOutcomeDelivery{}, false, nil
	}
	return s.deliveries[id], true, nil
}

func (s *draftRoutesStore) MarkDeliverySent(
	_ context.Context, companyID, deliveryID string, sentAt time.Time,
) error {
	delivery, ok := s.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID ||
		delivery.Status != awards.DeliveryPending {
		return awards.ErrAwardDeliveryNotFound
	}
	delivery.Status = awards.DeliverySent
	delivery.SentAt = &sentAt
	s.deliveries[deliveryID] = delivery
	return nil
}

func (s *draftRoutesStore) MarkDeliveryFailed(
	_ context.Context, companyID, deliveryID string,
	code awards.DeliveryFailureCode,
) error {
	delivery, ok := s.deliveries[deliveryID]
	if !ok || delivery.CompanyID != companyID ||
		delivery.Status != awards.DeliveryPending {
		return awards.ErrAwardDeliveryNotFound
	}
	delivery.Status = awards.DeliveryFailed
	delivery.FailureCode = code
	s.deliveries[deliveryID] = delivery
	return nil
}

func (s *draftRoutesStore) ObsoletePendingDeliveries(
	_ context.Context, companyID, revisionID string,
) ([]awards.AwardOutcomeDelivery, error) {
	var affected []awards.AwardOutcomeDelivery
	for id, delivery := range s.deliveries {
		if delivery.CompanyID != companyID ||
			delivery.AwardRevisionID != revisionID ||
			delivery.Status != awards.DeliveryPending {
			continue
		}
		delivery.Status = awards.DeliveryObsolete
		s.deliveries[id] = delivery
		affected = append(affected, delivery)
	}
	return affected, nil
}

func (s *draftRoutesStore) ListDeliveries(
	_ context.Context, companyID, revisionID string,
) ([]awards.AwardOutcomeDelivery, error) {
	deliveries := []awards.AwardOutcomeDelivery{}
	for _, delivery := range s.deliveries {
		if delivery.CompanyID == companyID &&
			delivery.AwardRevisionID == revisionID {
			deliveries = append(deliveries, delivery)
		}
	}
	return deliveries, nil
}

func (s *draftRoutesStore) SendAwardOutcomeNotification(
	_ context.Context, notification awards.AwardOutcomeNotification,
	_ awards.OutcomeProjection,
) error {
	s.mails = append(s.mails, notification)
	return nil
}

// DeriveInvitationLink stubs the re-derived Supplier Access link for route
// tests that only care that a notification can be sent end-to-end, not what
// the link itself is.
func (s *draftRoutesStore) DeriveInvitationLink(
	_ context.Context, _, invitationID string,
) (awards.InvitationLink, error) {
	return awards.InvitationLink{
		Token: "test-token",
		URL:   "https://app.example.test/supplier-access/open?token=test-token",
	}, nil
}
