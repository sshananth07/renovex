package rfqissuance_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

func TestSupplierInvitationHTTPProjectionHasExactAllowlistedJSONKeys(t *testing.T) {
	service, _, invitation, _ := supplierProjectionRig(t)
	router, api := platformhttp.NewRouter("supplier-rfq-test", "0.0.0")
	rfqissuance.RegisterSupplierHandlers(api, service)

	req := httptest.NewRequest(http.MethodGet,
		"/supplier-access/invitations/"+invitation.ID, nil)
	req.AddCookie(&http.Cookie{Name: "supplier_session", Value: "session-token"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	assertExactJSONKeys(t, body,
		"currentRfqVersion", "expiresAt", "invitationId", "responseWindow", "status")
	assertExactJSONKeys(t, body["currentRfqVersion"].(map[string]any),
		"currency", "deliveryAddress", "id", "issuedAt", "lines", "requiredByDate",
		"responseDeadline", "rfqNumber", "supplierInstructions", "title", "versionNumber")
	assertExactJSONKeys(t, body["responseWindow"].(map[string]any),
		"canRespond", "deadline", "status")
	line := body["currentRfqVersion"].(map[string]any)["lines"].([]any)[0].(map[string]any)
	assertExactJSONKeys(t, line,
		"id", "materialName", "procurementNotes", "quantityUnit", "quantityValue",
		"requiredByDate", "sortOrder", "specification")

	for _, header := range []string{"Cache-Control", "Pragma", "Referrer-Policy", "X-Content-Type-Options"} {
		if rec.Header().Get(header) == "" {
			t.Errorf("%s header is absent", header)
		}
	}
}

func TestSupplierRFQHistoryAndDetailHaveExactAllowlistedJSONKeys(t *testing.T) {
	service, _, invitation, _ := supplierProjectionRig(t)
	router, api := platformhttp.NewRouter("supplier-rfq-history-test", "0.0.0")
	rfqissuance.RegisterSupplierHandlers(api, service)

	request := func(path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "supplier_session", Value: "session-token"})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return body
	}

	history := request("/supplier-access/invitations/" + invitation.ID + "/rfq-versions")
	assertExactJSONKeys(t, history, "nextCursor", "versions")
	summary := history["versions"].([]any)[0].(map[string]any)
	assertExactJSONKeys(t, summary,
		"id", "isCurrent", "issuedAt", "responseDeadline", "versionNumber")

	detail := request("/supplier-access/invitations/" + invitation.ID +
		"/rfq-versions/" + invitation.CurrentIssuedRFQVersionID)
	assertExactJSONKeys(t, detail,
		"currency", "deliveryAddress", "id", "issuedAt", "lines", "requiredByDate",
		"responseDeadline", "rfqNumber", "supplierInstructions", "title", "versionNumber")
}

func assertExactJSONKeys(t *testing.T, value map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Fatalf("JSON keys = %v, want %v", got, want)
		}
	}
}
