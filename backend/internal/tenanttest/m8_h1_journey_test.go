package tenanttest_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// The complete promised business result (§21.2 H1): two invited Suppliers
// respond, the contractor compares and awards, both Suppliers are explicitly
// notified, and each reads and acknowledges only its own outcome.
func TestM8PhaseH1_GoldenComposedJourney(t *testing.T) {
	router, db := setupRouterWithDatabase(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h1-owner@example.com", "H1 Contractor")

	// --- Contractor: M7 RFQ through M8 Version 1 ---
	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h1")

	// --- Two Suppliers complete the real Phase D access flow ---
	winner := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H1 Winning Supplier", "sales@h1-winner.example", "winner")
	loser := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H1 Losing Supplier", "sales@h1-loser.example", "loser")

	// --- Each Supplier submits an immutable Offer Version ---
	winnerVersionID, winnerTotal := submitOffer(t, winner, 15000, "winner")
	loserVersionID, loserTotal := submitOffer(t, loser, 19000, "loser")
	if winnerVersionID == loserVersionID {
		t.Fatalf("both Suppliers submitted the same offer version %q", winnerVersionID)
	}
	if winnerTotal <= 0 || loserTotal <= winnerTotal {
		t.Fatalf("offer totals = winner %d, loser %d; want positive and ordered",
			winnerTotal, loserTotal)
	}

	// --- Contractor comparison sees both offers, and ranks neither ---
	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	if comparison.Code != http.StatusOK {
		t.Fatalf("comparison: %d: %s", comparison.Code, comparison.Body.String())
	}
	comparisonBody := decodeObject(t, comparison.Body.Bytes())
	if authoritative, _ := comparisonBody["authoritative"].(bool); authoritative {
		t.Errorf("comparison claims to be authoritative")
	}
	offers, _ := comparisonBody["offers"].([]any)
	if len(offers) != 2 {
		t.Fatalf("comparison offers = %d, want both Suppliers: %s",
			len(offers), comparison.Body.String())
	}

	var winningOffer map[string]any
	for _, raw := range offers {
		offer := raw.(map[string]any)
		if offer["offerVersionId"] == winnerVersionID {
			winningOffer = offer
		}
	}
	if winningOffer == nil {
		t.Fatalf("winning offer %q absent from comparison", winnerVersionID)
	}

	// --- Award draft, selection and publication ---
	draft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		owner.accessToken, nil)
	if draft.Code != http.StatusCreated {
		t.Fatalf("create award draft: %d: %s", draft.Code, draft.Body.String())
	}
	draftRevision := mustNumberField(t, draft, "revision")

	for _, raw := range winningOffer["lines"].([]any) {
		line := raw.(map[string]any)
		selected := doJSON(t, router, http.MethodPut,
			base+"/award-draft/lines/"+line["issuedRfqLineId"].(string)+"/selection",
			owner.accessToken, map[string]any{
				"offerVersionId":   winnerVersionID,
				"offerLineId":      line["offerLineId"].(string),
				"expectedRevision": int64(draftRevision),
			})
		if selected.Code != http.StatusOK {
			t.Fatalf("select award line: %d: %s",
				selected.Code, selected.Body.String())
		}
		draftRevision = mustNumberField(t, selected, "revision")
	}

	finalised := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h1-finalise",
			"changeReason": "Lowest complete quotation",
		})
	if finalised.Code != http.StatusCreated {
		t.Fatalf("finalise award: %d: %s", finalised.Code, finalised.Body.String())
	}
	revisionID := mustField(t, finalised, "id")

	// A repeated finalisation with the same operation converges rather than
	// publishing a second authoritative revision.
	repeat := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h1-finalise",
			"changeReason": "Lowest complete quotation",
		})
	if repeat.Code != http.StatusCreated && repeat.Code != http.StatusOK {
		t.Fatalf("repeated finalisation: %d: %s", repeat.Code, repeat.Body.String())
	}
	if got := mustField(t, repeat, "id"); got != revisionID {
		t.Fatalf("repeated finalisation published a second revision %q, want %q",
			got, revisionID)
	}

	revisionBase := base + "/award-revisions/" + revisionID

	// --- Outcomes: one per participating Supplier ---
	outcomes := doJSON(t, router, http.MethodGet, revisionBase+"/outcomes",
		owner.accessToken, nil)
	if outcomes.Code != http.StatusOK {
		t.Fatalf("list outcomes: %d: %s", outcomes.Code, outcomes.Body.String())
	}
	outcomeList, _ := decodeObject(t, outcomes.Body.Bytes())["outcomes"].([]any)
	if len(outcomeList) != 2 {
		t.Fatalf("outcomes = %d, want one per participating Supplier: %s",
			len(outcomeList), outcomes.Body.String())
	}

	outcomeBySupplier := map[string]map[string]any{}
	for _, raw := range outcomeList {
		outcome := raw.(map[string]any)
		outcomeBySupplier[outcome["supplierId"].(string)] = outcome
	}
	winnerOutcome := outcomeBySupplier[winner.supplierID]
	loserOutcome := outcomeBySupplier[loser.supplierID]
	if winnerOutcome == nil || loserOutcome == nil {
		t.Fatalf("outcomes missing a Supplier: %s", outcomes.Body.String())
	}
	if winnerOutcome["result"] != "selected" {
		t.Errorf("winner result = %v, want selected", winnerOutcome["result"])
	}
	if loserOutcome["result"] != "unsuccessful" {
		t.Errorf("loser result = %v, want unsuccessful", loserOutcome["result"])
	}

	// --- Explicit notifications must reach sent ---
	for name, pair := range map[string]struct {
		outcome  map[string]any
		supplier verifiedSupplier
	}{
		"winner": {winnerOutcome, winner},
		"loser":  {loserOutcome, loser},
	} {
		sent := doJSON(t, router, http.MethodPost,
			revisionBase+"/outcomes/"+pair.outcome["id"].(string)+"/notifications",
			owner.accessToken, map[string]any{
				"operationId":       "h1-notify-" + name,
				"recipientIdentity": pair.supplier.email,
				"accessGeneration":  1,
			})
		if sent.Code != http.StatusAccepted {
			t.Fatalf("%s notification: %d: %s", name, sent.Code, sent.Body.String())
		}
		if status := mustField(t, sent, "status"); status != "sent" {
			t.Errorf("%s notification status = %q, want sent", name, status)
		}
	}

	// --- Each Supplier reads ONLY its own outcome, then acknowledges ---
	for name, pair := range map[string]struct {
		own    map[string]any
		other  map[string]any
		client verifiedSupplier
	}{
		"winner": {winnerOutcome, loserOutcome, winner},
		"loser":  {loserOutcome, winnerOutcome, loser},
	} {
		ownID := pair.own["id"].(string)
		read := pair.client.browser.do(t, http.MethodGet,
			"/supplier-access/outcomes/"+ownID+
				"?invitationId="+pair.client.invitationID, nil)
		if read.Code != http.StatusOK {
			t.Fatalf("%s reading own outcome: %d: %s",
				name, read.Code, read.Body.String())
		}
		for _, forbidden := range []string{
			"competitor", "offerVersionId", "operationId",
		} {
			if strings.Contains(read.Body.String(), forbidden) {
				t.Errorf("%s outcome exposed %q: %s",
					name, forbidden, read.Body.String())
			}
		}

		// The other Supplier's outcome must be a neutral not-found.
		foreign := pair.client.browser.do(t, http.MethodGet,
			"/supplier-access/outcomes/"+pair.other["id"].(string)+
				"?invitationId="+pair.client.invitationID, nil)
		if foreign.Code != http.StatusNotFound {
			t.Fatalf("%s read another Supplier's outcome: %d: %s",
				name, foreign.Code, foreign.Body.String())
		}

		acknowledged := pair.client.browser.do(t, http.MethodPost,
			"/supplier-access/outcomes/"+ownID+"/acknowledgements",
			map[string]any{
				"invitationId": pair.client.invitationID,
				"operationId":  "h1-ack-" + name,
			})
		if acknowledged.Code != http.StatusCreated {
			t.Fatalf("%s acknowledgement: %d: %s",
				name, acknowledged.Code, acknowledged.Body.String())
		}

		// A repeated receipt is idempotent: the first receipt is never replaced.
		again := pair.client.browser.do(t, http.MethodPost,
			"/supplier-access/outcomes/"+ownID+"/acknowledgements",
			map[string]any{
				"invitationId": pair.client.invitationID,
				"operationId":  "h1-ack-" + name,
			})
		if again.Code != http.StatusOK && again.Code != http.StatusCreated {
			t.Fatalf("%s repeated acknowledgement: %d: %s",
				name, again.Code, again.Body.String())
		}
	}

	// --- Persisted result, asserted through the owning route ---
	revisions := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil)
	if revisions.Code != http.StatusOK {
		t.Fatalf("list revisions: %d: %s", revisions.Code, revisions.Body.String())
	}
	revisionList, _ := decodeObject(t, revisions.Body.Bytes())["revisions"].([]any)
	if len(revisionList) != 1 {
		t.Fatalf("award revisions = %d, want exactly one authoritative revision",
			len(revisionList))
	}
}
