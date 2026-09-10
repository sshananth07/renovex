package tenanttest_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H3 — Award branch journeys (spec §21.2, §21.6).
//
// The partial retender-required Award, the locked-baseline monotonic
// correction, notification failure with explicit retry, and frozen Supplier
// outcomes — each through the real router and real MongoDB.
//
// A failed notification must never block the Supplier from viewing or
// acknowledging its outcome, because delivery is not authoritative.

// A partial Award publishes one selected line and one deliberately unawarded
// `retender_required` line. A draft that leaves a line undecided is refused.
func TestM8PhaseH3_PartialAwardWithRetenderRequiredLine(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h3a-owner@example.com", "H3A Contractor")

	rfqChainID, versionID := issueTwoLineRFQ(t, router, owner, "h3a")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H3A Supplier", "sales@h3a.example", "h3a")
	offerVersionID, _ := submitOffer(t, supplier, 14000, "h3a")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	if comparison.Code != http.StatusOK {
		t.Fatalf("comparison: %d: %s", comparison.Code, comparison.Body.String())
	}
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	lines, _ := offer["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("comparison lines = %d, want two RFQ lines", len(lines))
	}

	draft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		owner.accessToken, nil)
	if draft.Code != http.StatusCreated {
		t.Fatalf("create award draft: %d: %s", draft.Code, draft.Body.String())
	}
	revision := mustNumberField(t, draft, "revision")

	// Decide only the FIRST line, leaving the second undecided.
	first := lines[0].(map[string]any)
	selected := doJSON(t, router, http.MethodPut,
		base+"/award-draft/lines/"+first["issuedRfqLineId"].(string)+"/selection",
		owner.accessToken, map[string]any{
			"offerVersionId":   offerVersionID,
			"offerLineId":      first["offerLineId"].(string),
			"expectedRevision": int64(revision),
		})
	if selected.Code != http.StatusOK {
		t.Fatalf("select line: %d: %s", selected.Code, selected.Body.String())
	}
	revision = mustNumberField(t, selected, "revision")

	// An incomplete draft must not publish: every line has to be decided.
	incomplete := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId": "h3a-incomplete", "changeReason": "premature",
		})
	if incomplete.Code != http.StatusUnprocessableEntity {
		t.Fatalf("incomplete finalisation = %d, want 422: %s",
			incomplete.Code, incomplete.Body.String())
	}
	if list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil); len(revisionsOf(t, list)) != 0 {
		t.Fatalf("a refused finalisation published an award revision")
	}

	// Now record the deliberate no-award decision on the second line.
	second := lines[1].(map[string]any)
	unawarded := doJSON(t, router, http.MethodPut,
		base+"/award-draft/lines/"+second["issuedRfqLineId"].(string)+"/unawarded",
		// An enumerated reason carries its own meaning, so a free-text note is
		// permitted only with `other`.
		owner.accessToken, map[string]any{
			"reason":           "retender_required",
			"expectedRevision": int64(revision),
		})
	if unawarded.Code != http.StatusOK {
		t.Fatalf("unaward line: %d: %s", unawarded.Code, unawarded.Body.String())
	}

	finalised := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h3a-finalise",
			"changeReason": "Award one line, retender the other",
		})
	if finalised.Code != http.StatusCreated {
		t.Fatalf("finalise partial award: %d: %s",
			finalised.Code, finalised.Body.String())
	}

	// The published revision carries the awarded line and the reasoned no-award
	// separately, and the award total reflects ONLY the awarded line.
	published := decodeObject(t, finalised.Body.Bytes())
	awarded, _ := published["awardedLines"].([]any)
	unawardedLines, _ := published["unawardedLines"].([]any)
	if len(awarded) != 1 || len(unawardedLines) != 1 {
		t.Fatalf("published lines = %d awarded / %d unawarded, want one of each: %s",
			len(awarded), len(unawardedLines), finalised.Body.String())
	}
	if got := awarded[0].(map[string]any)["issuedRfqLineId"]; got != first["issuedRfqLineId"] {
		t.Errorf("awarded line = %v, want the selected line %v",
			got, first["issuedRfqLineId"])
	}
	retendered := unawardedLines[0].(map[string]any)
	if retendered["reason"] != "retender_required" {
		t.Errorf("unawarded reason = %v, want retender_required", retendered["reason"])
	}
	if got := retendered["issuedRfqLineId"]; got != second["issuedRfqLineId"] {
		t.Errorf("unawarded line = %v, want the undecided line %v",
			got, second["issuedRfqLineId"])
	}

	// A retendered line contributes nothing: the total is the awarded line only.
	total, _ := published["grandAwardTotal"].(map[string]any)
	if total == nil || total["amount"] != float64(1400000) {
		t.Errorf("grand award total = %v, want only the awarded line's subtotal",
			published["grandAwardTotal"])
	}
}

// A correction may add a previously unawarded line, and both immutable
// revisions remain readable. Removing an awarded line is refused as
// non-monotonic and leaves the baseline untouched.
func TestM8PhaseH3_MonotonicCorrectionSupersedesWithoutMutatingTheBaseline(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h3b-owner@example.com", "H3B Contractor")

	rfqChainID, versionID := issueTwoLineRFQ(t, router, owner, "h3b")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H3B Supplier", "sales@h3b.example", "h3b")
	offerVersionID, _ := submitOffer(t, supplier, 16000, "h3b")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	lines, _ := offer["lines"].([]any)
	first := lines[0].(map[string]any)
	second := lines[1].(map[string]any)

	baselineID := publishAward(t, router, owner, base, offerVersionID,
		[]map[string]any{first}, []map[string]any{second}, "h3b-baseline")

	// A correction that turns the awarded line back into a no-award is
	// commercially non-monotonic: the Supplier has already been told it won.
	// The lineage is PRESENT here, so this isolates reduction rather than the
	// separate "omitted lineage" rule.
	regressive := doJSON(t, router, http.MethodPost,
		base+"/award-revisions/corrections", owner.accessToken, map[string]any{
			"operationId":  "h3b-regressive",
			"changeReason": "attempt to unaward a published line",
			"decisions": []map[string]any{
				{
					"issuedRfqLineId": first["issuedRfqLineId"],
					"stableLineageId": first["stableLineageId"],
					"decision":        "unawarded",
					"unawardedReason": "purchase_deferred",
				},
				{
					"issuedRfqLineId": second["issuedRfqLineId"],
					"stableLineageId": second["stableLineageId"],
					"decision":        "unawarded",
					"unawardedReason": "retender_required",
				},
			},
		})
	if regressive.Code != http.StatusUnprocessableEntity {
		t.Fatalf("regressive correction = %d, want 422: %s",
			regressive.Code, regressive.Body.String())
	}

	// The baseline is untouched by the refused correction.
	if list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil); len(revisionsOf(t, list)) != 1 {
		t.Fatalf("a refused correction changed the published revision history")
	}

	// A correction that ADDS the previously unawarded line is monotonic.
	//
	// The decision set is COMPLETE, not a delta: every baseline-awarded lineage
	// must be re-affirmed with the same Supplier, Offer Version and Offer line,
	// because an omitted lineage reads as a removal (§8G).
	corrected := doJSON(t, router, http.MethodPost,
		base+"/award-revisions/corrections", owner.accessToken, map[string]any{
			"operationId":  "h3b-correct",
			"changeReason": "Budget approved; award the retendered line",
			"decisions": []map[string]any{
				{
					"issuedRfqLineId": first["issuedRfqLineId"],
					"stableLineageId": first["stableLineageId"],
					"decision":        "selected",
					"offerVersionId":  offerVersionID,
					"offerLineId":     first["offerLineId"],
				},
				{
					"issuedRfqLineId": second["issuedRfqLineId"],
					"stableLineageId": second["stableLineageId"],
					"decision":        "selected",
					"offerVersionId":  offerVersionID,
					"offerLineId":     second["offerLineId"],
				},
			},
		})
	if corrected.Code != http.StatusCreated {
		t.Fatalf("monotonic correction: %d: %s",
			corrected.Code, corrected.Body.String())
	}
	correctionID := mustField(t, corrected, "id")
	if correctionID == baselineID {
		t.Fatalf("the correction reused the baseline revision identity")
	}

	// Both immutable revisions remain readable; the baseline is not rewritten.
	list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil)
	if got := len(revisionsOf(t, list)); got != 2 {
		t.Fatalf("award revisions = %d, want baseline plus correction", got)
	}
	baseline := doJSON(t, router, http.MethodGet,
		base+"/award-revisions/"+baselineID, owner.accessToken, nil)
	if baseline.Code != http.StatusOK {
		t.Fatalf("baseline revision is no longer readable: %d: %s",
			baseline.Code, baseline.Body.String())
	}
	if number := mustNumberField(t, baseline, "revisionNumber"); number != 1 {
		t.Errorf("baseline revision number = %v, want an unchanged 1", number)
	}
}

// A failed notification is recorded with a bounded failure code, does not undo
// the Award, and does not stop the Supplier viewing or acknowledging its
// outcome. An explicit retry then succeeds.
func TestM8PhaseH3_NotificationFailureDoesNotBlockOutcomeOrAcknowledgement(t *testing.T) {
	router, db, mailer := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h3c-owner@example.com", "H3C Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h3c")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H3C Supplier", "sales@h3c.example", "h3c")
	offerVersionID, _ := submitOffer(t, supplier, 17000, "h3c")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	lines, _ := offer["lines"].([]any)
	revisionID := publishAward(t, router, owner, base, offerVersionID,
		lineSlice(lines), nil, "h3c")

	revisionBase := base + "/award-revisions/" + revisionID
	outcomes := doJSON(t, router, http.MethodGet, revisionBase+"/outcomes",
		owner.accessToken, nil)
	outcomeList, _ := decodeObject(t, outcomes.Body.Bytes())["outcomes"].([]any)
	if len(outcomeList) != 1 {
		t.Fatalf("outcomes = %d, want one: %s",
			len(outcomeList), outcomes.Body.String())
	}
	outcome := outcomeList[0].(map[string]any)
	outcomeID := outcome["id"].(string)

	// The provider is unavailable for the first attempt.
	mailer.failNext(errors.New("smtp: provider unavailable"))
	failed := doJSON(t, router, http.MethodPost,
		revisionBase+"/outcomes/"+outcomeID+"/notifications", owner.accessToken,
		map[string]any{
			"operationId":       "h3c-notify",
			"recipientIdentity": supplier.email,
			"accessGeneration":  1,
		})
	if failed.Code != http.StatusAccepted {
		t.Fatalf("failed notification: %d: %s",
			failed.Code, failed.Body.String())
	}
	failedBody := decodeObject(t, failed.Body.Bytes())
	if failedBody["status"] != "failed" {
		t.Errorf("delivery status = %v, want failed", failedBody["status"])
	}
	if code, _ := failedBody["failureCode"].(string); code == "" {
		t.Errorf("failed delivery carries no bounded failure code: %s",
			failed.Body.String())
	}
	// The failure text must never reach the record.
	if body := failed.Body.String(); strings.Contains(body, "smtp") ||
		strings.Contains(body, "provider unavailable") {
		t.Errorf("provider detail leaked into the delivery record: %s", body)
	}

	// The Award itself is unaffected, and the Supplier can still act.
	if list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil); len(revisionsOf(t, list)) != 1 {
		t.Fatalf("a failed notification changed the published award")
	}
	read := supplier.browser.do(t, http.MethodGet,
		"/supplier-access/outcomes/"+outcomeID+
			"?invitationId="+supplier.invitationID, nil)
	if read.Code != http.StatusOK {
		t.Fatalf("Supplier could not read its outcome after a failed send: %d: %s",
			read.Code, read.Body.String())
	}
	acknowledged := supplier.browser.do(t, http.MethodPost,
		"/supplier-access/outcomes/"+outcomeID+"/acknowledgements",
		map[string]any{
			"invitationId": supplier.invitationID,
			"operationId":  "h3c-ack",
		})
	if acknowledged.Code != http.StatusCreated {
		t.Fatalf("acknowledgement blocked by a failed notification: %d: %s",
			acknowledged.Code, acknowledged.Body.String())
	}

	// An explicit retry is a NEW attempt and now reaches sent.
	deliveryID := failedBody["id"].(string)
	mailer.failNext(nil)
	retried := doJSON(t, router, http.MethodPost,
		base+"/notifications/"+deliveryID+"/retry", owner.accessToken,
		map[string]any{
			"operationId":       "h3c-retry",
			"recipientIdentity": supplier.email,
			"accessGeneration":  1,
		})
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry notification: %d: %s",
			retried.Code, retried.Body.String())
	}
	if status := mustField(t, retried, "status"); status != "sent" {
		t.Errorf("retried delivery status = %q, want sent", status)
	}

	// Both attempts are retained: delivery history is append-only.
	attempts := doJSON(t, router, http.MethodGet, revisionBase+"/notifications",
		owner.accessToken, nil)
	if attempts.Code != http.StatusOK {
		t.Fatalf("list notifications: %d: %s",
			attempts.Code, attempts.Body.String())
	}
	deliveries, _ := decodeObject(t,
		attempts.Body.Bytes())["deliveries"].([]any)
	if len(deliveries) != 2 {
		t.Fatalf("delivery attempts = %d, want the failure and the retry: %s",
			len(deliveries), attempts.Body.String())
	}
}

// --- helpers ---

func lineSlice(lines []any) []map[string]any {
	converted := make([]map[string]any, 0, len(lines))
	for _, raw := range lines {
		converted = append(converted, raw.(map[string]any))
	}
	return converted
}

func revisionsOf(t *testing.T, list *httptest.ResponseRecorder) []any {
	t.Helper()
	revisions, _ := decodeObject(t, list.Body.Bytes())["revisions"].([]any)
	return revisions
}

// comparisonOfferFor returns the comparison entry for one submitted offer.
func comparisonOfferFor(
	t *testing.T,
	comparison *httptest.ResponseRecorder,
	offerVersionID string,
) map[string]any {
	t.Helper()

	offers, _ := decodeObject(t, comparison.Body.Bytes())["offers"].([]any)
	for _, raw := range offers {
		offer := raw.(map[string]any)
		if offer["offerVersionId"] == offerVersionID {
			return offer
		}
	}
	t.Fatalf("offer %q absent from comparison: %s",
		offerVersionID, comparison.Body.String())
	return nil
}

// publishAward selects every line in selected, records a retender-required
// no-award for every line in unawarded, and finalises.
func publishAward(
	t *testing.T,
	router http.Handler,
	owner testCompany,
	base string,
	offerVersionID string,
	selected []map[string]any,
	unawarded []map[string]any,
	suffix string,
) string {
	t.Helper()

	draft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		owner.accessToken, nil)
	if draft.Code != http.StatusCreated {
		t.Fatalf("create award draft: %d: %s", draft.Code, draft.Body.String())
	}
	revision := mustNumberField(t, draft, "revision")

	for _, line := range selected {
		response := doJSON(t, router, http.MethodPut,
			base+"/award-draft/lines/"+line["issuedRfqLineId"].(string)+"/selection",
			owner.accessToken, map[string]any{
				"offerVersionId":   offerVersionID,
				"offerLineId":      line["offerLineId"].(string),
				"expectedRevision": int64(revision),
			})
		if response.Code != http.StatusOK {
			t.Fatalf("select line: %d: %s", response.Code, response.Body.String())
		}
		revision = mustNumberField(t, response, "revision")
	}
	for _, line := range unawarded {
		response := doJSON(t, router, http.MethodPut,
			base+"/award-draft/lines/"+line["issuedRfqLineId"].(string)+"/unawarded",
			owner.accessToken, map[string]any{
				"reason":           "retender_required",
				"expectedRevision": int64(revision),
			})
		if response.Code != http.StatusOK {
			t.Fatalf("unaward line: %d: %s", response.Code, response.Body.String())
		}
		revision = mustNumberField(t, response, "revision")
	}

	finalised := doJSON(t, router, http.MethodPost, base+"/award-revisions",
		owner.accessToken, map[string]any{
			"operationId":  "h3-finalise-" + suffix,
			"changeReason": "Acceptance award " + suffix,
		})
	if finalised.Code != http.StatusCreated {
		t.Fatalf("finalise award: %d: %s",
			finalised.Code, finalised.Body.String())
	}
	return mustField(t, finalised, "id")
}

// issueTwoLineRFQ builds and issues an RFQ carrying TWO immutable lines, so a
// partial award and a monotonic correction are both expressible.
func issueTwoLineRFQ(
	t *testing.T,
	router http.Handler,
	owner testCompany,
	suffix string,
) (rfqChainID, issuedVersionID string) {
	t.Helper()

	projectID, workItemID, materialID := buildProcurementProject(t, router, owner)
	rfqID, _, _, revision := createRFQWithLine(
		t, router, owner, projectID, workItemID, materialID)

	// A second reviewed requirement becomes the second RFQ line.
	secondRequirementID, secondRequirementRevision := reviewedRequirement(
		t, router, owner, projectID, workItemID, materialID)
	added := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/lines",
		owner.accessToken, map[string]any{
			"materialRequirementId":       secondRequirementID,
			"expectedRequirementRevision": secondRequirementRevision,
			"expectedRfqRevision":         revision,
		})
	if added.Code != http.StatusOK {
		t.Fatalf("add second RFQ line: %d: %s", added.Code, added.Body.String())
	}

	deadline := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	patched := doJSON(t, router, http.MethodPatch, "/rfqs/"+rfqID,
		owner.accessToken, map[string]any{
			"expectedRevision":     mustNumberField(t, added, "revision"),
			"title":                "Two-line acceptance " + suffix,
			"requiredByDate":       deadline.Add(7 * 24 * time.Hour).Format(time.RFC3339),
			"responseDeadline":     deadline.Format(time.RFC3339),
			"supplierInstructions": "Quote delivered price",
		})
	if patched.Code != http.StatusOK {
		t.Fatalf("patch RFQ: %d: %s", patched.Code, patched.Body.String())
	}
	ready := doJSON(t, router, http.MethodPost, "/rfqs/"+rfqID+"/ready",
		owner.accessToken, map[string]any{
			"expectedRevision": mustNumberField(t, patched, "revision"),
		})
	if ready.Code != http.StatusOK {
		t.Fatalf("ready RFQ: %d: %s", ready.Code, ready.Body.String())
	}

	issued := doJSON(t, router, http.MethodPost, "/rfq-chains/"+rfqID+"/issue",
		owner.accessToken, map[string]any{
			"currency": "MYR", "operationId": "h3-issue-" + suffix,
		})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue Version 1: %d: %s", issued.Code, issued.Body.String())
	}
	return rfqID, mustField(t, issued, "id")
}
