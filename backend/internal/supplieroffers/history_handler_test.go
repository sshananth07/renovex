package supplieroffers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

func TestSupplierOfferHistoryAndDetailExposeExactTopLevelAllowlists(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	// HTTP handlers use the real request clock. Keep the otherwise fixed-time
	// session fixture live so this test reaches the history projection boundary.
	rig.service.access.(*fakeOfferAccessAuthorizer).authorized.SessionCookieRenewal.ExpiresAt =
		time.Now().UTC().Add(time.Hour)
	router, api := platformhttp.NewRouter("supplier-offer-history-test", "0.0.0")
	RegisterHandlers(api, rig.service)

	request := func(path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "supplier_session", Value: rig.input.SessionToken})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return body
	}

	history := request("/supplier-offers/invitation-1/versions")
	assertOfferJSONKeys(t, history, "nextCursor", "versions")
	summary := history["versions"].([]any)[0].(map[string]any)
	assertOfferJSONKeys(t, summary, "canWithdraw", "currency", "grandTotal", "id",
		"isSuperseded", "issuedRfqVersionId", "offerValidUntil", "publicStatus",
		"submittedAt", "versionNumber")

	detail := request("/supplier-offers/invitation-1/versions/" + version.ID)
	assertOfferJSONKeys(t, detail, "canWithdraw", "chargeGroups", "currency",
		"deliveryCharge", "deliveryChargeTotal", "fullOfferChargeTotal", "grandTotal",
		"id", "isSuperseded", "issuedRfqVersionId", "lines", "offerValidUntil",
		"publicStatus", "quotedLineSubtotal", "quotedTaxTotal", "submittedAt",
		"supplierNotes", "tax", "versionNumber", "withdrawal")
	if _, leaked := detail["companyId"]; leaked {
		t.Fatal("detail leaked companyId")
	}
}

func TestSupplierOfferDraftReadExposesCompleteCommercialAllowlist(t *testing.T) {
	rig := newSubmissionRig(t)
	rig.service.access.(*fakeOfferAccessAuthorizer).authorized.SessionCookieRenewal.ExpiresAt =
		time.Now().UTC().Add(time.Hour)
	rig.service.issuedRFQ.(*fakeIssuedRFQSource).snapshot.ResponseDeadline =
		time.Now().UTC().Add(time.Hour)

	router, api := platformhttp.NewRouter("supplier-offer-draft-projection-test", "0.0.0")
	RegisterHandlers(api, rig.service)
	req := httptest.NewRequest(http.MethodGet,
		"/supplier-offers/invitation-1/draft", nil)
	req.AddCookie(&http.Cookie{Name: "supplier_session", Value: rig.input.SessionToken})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("draft status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode draft: %v", err)
	}
	assertOfferJSONKeys(t, body, "canEdit", "canSubmit", "chargeGroups", "currency",
		"deliveryCharge", "deliveryChargeReviewRequired", "id", "lines",
		"offerTaxReviewRequired", "offerValidUntil", "revision", "status",
		"supplierNotes", "tax")
	line := body["lines"].([]any)[0].(map[string]any)
	assertOfferJSONKeys(t, line, "brand", "commercialExceptions", "confirmationRequired",
		"id", "leadTime", "lineSubtotalExcludingTax", "lineTax", "productDescription",
		"quotedQuantity", "quotedUnit", "responseStatus", "reviewRequired", "rfqLineId",
		"sku", "supplierLineNotes", "unitPriceExcludingTax")
}

func TestSupplierOfferHistoryReturnsBoundedPendingWithoutClaimType(t *testing.T) {
	rig, version := newWithdrawalRig(t)
	rig.service.access.(*fakeOfferAccessAuthorizer).authorized.SessionCookieRenewal.ExpiresAt =
		time.Now().UTC().Add(time.Hour)
	claimedAt := time.Now().UTC()
	if _, err := rig.eligibility.ClaimEligibility(context.Background(), EligibilityClaimInput{
		CompanyID: version.CompanyID, OfferVersionID: version.ID,
		ExpectedRevision: 1, ClaimType: EligibilityClaimAward,
		OperationID: "award-pending-op", ClaimID: bson.NewObjectID().Hex(), ClaimedAt: claimedAt,
	}); err != nil {
		t.Fatalf("claim award eligibility: %v", err)
	}

	router, api := platformhttp.NewRouter("supplier-offer-pending-test", "0.0.0")
	RegisterHandlers(api, rig.service)
	req := httptest.NewRequest(http.MethodGet, "/supplier-offers/invitation-1/versions", nil)
	req.AddCookie(&http.Cookie{Name: "supplier_session", Value: rig.input.SessionToken})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending status=%d body=%s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "offer_state_pending") ||
		strings.Contains(body, "award_claimed") || strings.Contains(body, "claimType") {
		t.Fatalf("pending response leaked internal claim state: %s", body)
	}
}

func assertOfferJSONKeys(t *testing.T, value map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("keys=%v want=%v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("keys=%v want=%v", got, want)
		}
	}
}
