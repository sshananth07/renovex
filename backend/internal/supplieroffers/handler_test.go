package supplieroffers_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// Module errors must map to bounded HTTP statuses. A Supplier learns what to
// fix without learning anything about another tenant's data.
func TestMapSupplierOfferErrorUsesBoundedStatuses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "missing or foreign draft is a tenant-safe not-found",
			err:  supplieroffers.ErrOfferDraftNotFound,
			want: http.StatusNotFound,
		},
		{
			name: "missing or foreign version is a tenant-safe not-found",
			err:  supplieroffers.ErrOfferVersionNotFound,
			want: http.StatusNotFound,
		},
		{
			name: "a state or revision conflict is 409",
			err:  supplieroffers.ErrOfferDraftConflict,
			want: http.StatusConflict,
		},
		{
			name: "an eligibility conflict is 409",
			err:  supplieroffers.ErrOfferEligibilityConflict,
			want: http.StatusConflict,
		},
		{
			name: "an incomplete offer is 422, not a server error",
			err:  supplieroffers.ErrOfferIncomplete,
			want: http.StatusUnprocessableEntity,
		},
		{
			name: "pending review is 422",
			err:  supplieroffers.ErrOfferReviewPending,
			want: http.StatusUnprocessableEntity,
		},
		{
			name: "a closed response window is 422",
			err:  supplieroffers.ErrResponseWindowClosed,
			want: http.StatusUnprocessableEntity,
		},
		{
			name: "unusable or foreign Supplier access is a non-disclosing 404",
			err:  supplieroffers.ErrSupplierOfferAccessInvalid,
			want: http.StatusNotFound,
		},
		{
			name: "an authenticated CSRF refusal remains 403",
			err:  supplieroffers.ErrSupplierOfferCSRFRejected,
			want: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		mapped := supplieroffers.MapSupplierOfferError(tt.err)
		var status huma.StatusError
		if !errors.As(mapped, &status) {
			t.Errorf("%s: mapped error %v is not an HTTP status error", tt.name, mapped)
			continue
		}
		if status.GetStatus() != tt.want {
			t.Errorf("%s: status = %d, want %d", tt.name, status.GetStatus(), tt.want)
		}
	}
}

// An unrecognised error must NOT leak its text: an internal failure message
// could carry infrastructure detail a Supplier should never see.
func TestMapSupplierOfferErrorHidesUnknownFailures(t *testing.T) {
	mapped := supplieroffers.MapSupplierOfferError(
		errors.New("mongo: connection refused to internal-host:27017"))

	var status huma.StatusError
	if !errors.As(mapped, &status) {
		t.Fatalf("mapped error %v is not an HTTP status error", mapped)
	}
	if status.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 for an unknown infrastructure failure",
			status.GetStatus())
	}
	if got := status.Error(); got == "" ||
		containsAny(got, "mongo", "connection refused", "27017") {
		t.Errorf("public message %q leaks infrastructure detail", got)
	}
}

func containsAny(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if len(needle) > 0 && len(haystack) >= len(needle) {
			for index := 0; index+len(needle) <= len(haystack); index++ {
				if haystack[index:index+len(needle)] == needle {
					return true
				}
			}
		}
	}
	return false
}
