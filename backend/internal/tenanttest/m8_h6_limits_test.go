package tenanttest_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H6 — §19H and operational closure (spec §21.4, §21.5).
//
// The approved input limits, the fixed duplicate-operation concurrency
// baseline, audit/record de-duplication, and the bounded operational failure
// modes. This is an acceptance baseline, NOT a performance test: it sets no
// latency or throughput target and authorizes no optimization.

// The approved duplicate-operation baseline: exactly 20 concurrent requests
// sharing one operation ID converge on ONE business record, and one request
// using a DIFFERENT operation ID does not create a second award.
func TestM8PhaseH6_TwentyConcurrentSameOperationRequestsConverge(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h6a-owner@example.com", "H6A Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h6a")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H6A Supplier", "sales@h6a.example", "h6a")
	offerVersionID, _ := submitOffer(t, supplier, 15000, "h6a")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)

	// Prepare a complete draft, then race the publication itself.
	draft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		owner.accessToken, nil)
	revision := mustNumberField(t, draft, "revision")
	for _, raw := range offer["lines"].([]any) {
		line := raw.(map[string]any)
		selected := doJSON(t, router, http.MethodPut,
			base+"/award-draft/lines/"+line["issuedRfqLineId"].(string)+"/selection",
			owner.accessToken, map[string]any{
				"offerVersionId":   offerVersionID,
				"offerLineId":      line["offerLineId"].(string),
				"expectedRevision": int64(revision),
			})
		if selected.Code != http.StatusOK {
			t.Fatalf("select line: %d: %s", selected.Code, selected.Body.String())
		}
		revision = mustNumberField(t, selected, "revision")
	}

	const concurrentRequests = 20
	responses := make([]*httptest.ResponseRecorder, concurrentRequests)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range responses {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			<-start // release all goroutines together, without sleeping
			responses[slot] = doJSON(t, router, http.MethodPost,
				base+"/award-revisions", owner.accessToken, map[string]any{
					"operationId":  "h6a-shared-operation",
					"changeReason": "Concurrent duplicate submission",
				})
		}(index)
	}
	close(start)
	wait.Wait()

	// Each request either publishes/adopts the authoritative revision, or is
	// told the transition is still in flight with a BOUNDED 409/503 (§21.5).
	// What must never happen is a second revision, an unbounded failure, or a
	// structural panic.
	identities := map[string]int{}
	pending := 0
	for index, response := range responses {
		switch response.Code {
		case http.StatusCreated, http.StatusOK:
			identities[mustField(t, response, "id")]++
		case http.StatusConflict, http.StatusServiceUnavailable:
			pending++
		default:
			t.Fatalf("concurrent request %d = %d, outside the bounded set: %s",
				index, response.Code, response.Body.String())
		}
	}
	if len(identities) != 1 {
		t.Fatalf("concurrent requests produced %d distinct revisions: %v",
			len(identities), identities)
	}
	if identities[""] > 0 {
		t.Fatalf("a successful response carried no revision identity")
	}

	// Once the in-flight publication settles, the SAME operation converges on
	// the already-published revision rather than creating another.
	settled := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h6a-shared-operation",
			"changeReason": "Concurrent duplicate submission",
		})
	if settled.Code != http.StatusCreated && settled.Code != http.StatusOK {
		t.Fatalf("same-operation retry after settling = %d: %s",
			settled.Code, settled.Body.String())
	}
	var published string
	for identity := range identities {
		published = identity
	}
	if got := mustField(t, settled, "id"); got != published {
		t.Fatalf("same-operation retry returned %q, want the published %q",
			got, published)
	}
	t.Logf("bounded in-flight responses: %d of %d", pending, concurrentRequests)

	// A DIFFERENT operation must not publish a second award over the same
	// terminal claims.
	different := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h6a-different-operation",
			"changeReason": "A competing operation",
		})
	if different.Code == http.StatusCreated {
		t.Errorf("a different operation published a second award: %s",
			different.Body.String())
	}

	// Exactly one authoritative record survives the whole storm.
	list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil)
	if got := len(revisionsOf(t, list)); got != 1 {
		t.Fatalf("award revisions = %d after 20 duplicates plus one competitor, want 1",
			got)
	}
}

// Concurrent duplicate acknowledgements create exactly one receipt, and the
// first receipt is never overwritten.
func TestM8PhaseH6_ConcurrentAcknowledgementsCreateOneReceipt(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h6b-owner@example.com", "H6B Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h6b")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H6B Supplier", "sales@h6b.example", "h6b")
	offerVersionID, _ := submitOffer(t, supplier, 15000, "h6b")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	revisionID := publishAward(t, router, owner, base, offerVersionID,
		lineSlice(offer["lines"].([]any)), nil, "h6b")

	outcomes := doJSON(t, router, http.MethodGet,
		base+"/award-revisions/"+revisionID+"/outcomes", owner.accessToken, nil)
	list, _ := decodeObject(t, outcomes.Body.Bytes())["outcomes"].([]any)
	outcomeID := list[0].(map[string]any)["id"].(string)

	const concurrentRequests = 20
	codes := make([]int, concurrentRequests)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index := range codes {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			<-start
			ackBody := map[string]any{
				"invitationId": supplier.invitationID,
				"operationId":  "h6b-shared-ack",
			}
			response := supplier.browser.do(t, http.MethodPost,
				"/supplier-access/outcomes/"+outcomeID+"/acknowledgements",
				ackBody)
			// The Phase D sliding-session renewal (supplieraccess, unrelated to
			// this acknowledgement's own idempotency logic) is a plain
			// optimistic CAS with a bounded retry count and no backoff — the
			// SAME renewSupplierSession path exercised by every protected
			// Supplier request, including copy-forward
			// (m81_copyforward_journey_test.go). Twenty literally-simultaneous
			// requests against the SAME session document can occasionally
			// exhaust that bounded budget and return a 503 entirely BEFORE
			// this handler's acknowledgement logic ever runs. That condition
			// is transient by construction, so — exactly like a real client,
			// and exactly like the identical fix already applied to the
			// copy-forward concurrency journey — retry the identical request
			// once on a 503 rather than treating an unrelated session-layer
			// contention limit as an acknowledgement correctness failure.
			if response.Code == http.StatusServiceUnavailable {
				response = supplier.browser.do(t, http.MethodPost,
					"/supplier-access/outcomes/"+outcomeID+"/acknowledgements",
					ackBody)
			}
			codes[slot] = response.Code
		}(index)
	}
	close(start)
	wait.Wait()

	// Exactly one request may report a creation; the rest observe the existing
	// receipt. None may fail.
	created := 0
	for index, code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
		default:
			t.Fatalf("concurrent acknowledgement %d = %d", index, code)
		}
	}
	if created != 1 {
		t.Errorf("acknowledgement creations = %d, want exactly 1", created)
	}
}

// Requests beyond the approved input limits are refused at the boundary with a
// bounded status, and no partial domain write occurs.
func TestM8PhaseH6_ApprovedInputLimitsAreEnforced(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h6c-owner@example.com", "H6C Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h6c")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H6C Supplier", "sales@h6c.example", "h6c")

	created := supplier.browser.do(t, http.MethodPost,
		"/supplier-offers/"+supplier.invitationID+"/draft", nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create draft: %d: %s", created.Code, created.Body.String())
	}
	draft := decodeObject(t, created.Body.Bytes())
	draftID := draft["id"].(string)
	revision := int64(draft["revision"].(float64))
	lineID := draft["lines"].([]any)[0].(map[string]any)["id"].(string)

	overLimit := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{
			name: "over-long brand",
			body: map[string]any{
				"draftId": draftID, "expectedRevision": revision,
				"unitPriceMinor": 1000,
				"brand": strings.Repeat("x",
					procurementlimits.MaxDisplayTextRunes+1),
			},
		},
		{
			name: "over-long supplier notes",
			body: map[string]any{
				"draftId": draftID, "expectedRevision": revision,
				"unitPriceMinor": 1000,
				"supplierLineNotes": strings.Repeat("y",
					procurementlimits.MaxLongTextRunes+1),
			},
		},
		{
			name: "over-long draft id",
			body: map[string]any{
				"draftId":          strings.Repeat("d", procurementlimits.MaxIDBytes+1),
				"expectedRevision": revision, "unitPriceMinor": 1000,
			},
		},
	}
	for _, testCase := range overLimit {
		response := supplier.browser.do(t, http.MethodPut,
			"/supplier-offers/"+supplier.invitationID+"/draft/lines/"+lineID+"/quote",
			testCase.body)
		if response.Code != http.StatusUnprocessableEntity &&
			response.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("%s = %d, want a bounded 422/413: %s",
				testCase.name, response.Code, response.Body.String())
		}
	}

	// No partial write: the draft is untouched and its line unanswered.
	after := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/draft", nil)
	if after.Code != http.StatusOK {
		t.Fatalf("read draft: %d: %s", after.Code, after.Body.String())
	}
	state := decodeObject(t, after.Body.Bytes())
	if got := int64(state["revision"].(float64)); got != revision {
		t.Errorf("draft revision = %d after refused writes, want an unchanged %d",
			got, revision)
	}
	line := state["lines"].([]any)[0].(map[string]any)
	if line["responseStatus"] != "unanswered" {
		t.Errorf("line status = %v after refused writes, want unanswered",
			line["responseStatus"])
	}
}

// A mail provider failure leaves the domain action and its failed intent
// persisted with a bounded code, and never echoes provider text.
func TestM8PhaseH6_MailFailureIsBoundedAndPreservesTheDomainAction(t *testing.T) {
	router, db, mailer := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h6d-owner@example.com", "H6D Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h6d")
	supplier := createSupplier(t, router, owner, "H6D Supplier")
	_ = challenges

	invitation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations", owner.accessToken,
		map[string]any{
			"supplierId":     mustField(t, supplier, "id"),
			"recipientName":  "H6D Sales",
			"recipientEmail": "sales@h6d.example",
			"expiresAt":      "2026-12-31T00:00:00Z",
		})
	if invitation.Code != http.StatusCreated {
		t.Fatalf("create invitation: %d: %s",
			invitation.Code, invitation.Body.String())
	}
	invitationID := mustField(t, invitation, "id")

	mailer.failNext(errors.New("smtp: relay refused at internal-host:1025"))
	failed := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+invitationID+"/send",
		owner.accessToken, map[string]any{"operationId": "h6d-send"})
	if failed.Code != http.StatusServiceUnavailable {
		t.Fatalf("send with a failing provider = %d, want a bounded 503: %s",
			failed.Code, failed.Body.String())
	}
	if body := failed.Body.String(); strings.Contains(body, "internal-host") ||
		strings.Contains(body, "relay refused") {
		t.Errorf("provider detail leaked into the response: %s", body)
	}

	// The invitation itself survives, unactivated, and a later successful send
	// still works: the failure is recoverable, not terminal.
	mailer.failNext(nil)
	recovered := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+invitationID+"/send",
		owner.accessToken, map[string]any{"operationId": "h6d-send-retry"})
	if recovered.Code != http.StatusOK {
		t.Fatalf("send after provider recovery = %d: %s",
			recovered.Code, recovered.Body.String())
	}
	if status := mustField(t, recovered, "status"); status != "sent" {
		t.Errorf("recovered delivery status = %q, want sent", status)
	}
}

// Paged reads honour the approved page bound and stay stable across requests.
func TestM8PhaseH6_PaginationHonoursTheApprovedBound(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h6e-owner@example.com", "H6E Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h6e")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H6E Supplier", "sales@h6e.example", "h6e")
	submitOffer(t, supplier, 15000, "h6e")

	// A page size beyond the approved maximum is refused at the boundary
	// rather than silently returning an unbounded page.
	tooLarge := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions?pageSize="+
			itoa(procurementlimits.MaxPageSize+1), nil)
	if tooLarge.Code != http.StatusUnprocessableEntity {
		t.Errorf("page size above the bound = %d, want 422: %s",
			tooLarge.Code, tooLarge.Body.String())
	}

	// The approved maximum is accepted and the page is stable across reads.
	first := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions?pageSize="+
			itoa(procurementlimits.MaxPageSize), nil)
	if first.Code != http.StatusOK {
		t.Fatalf("page at the approved bound = %d: %s",
			first.Code, first.Body.String())
	}
	second := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions?pageSize="+
			itoa(procurementlimits.MaxPageSize), nil)
	if first.Body.String() != second.Body.String() {
		t.Errorf("repeated identical page reads differ:\n%s\n%s",
			first.Body.String(), second.Body.String())
	}
}
