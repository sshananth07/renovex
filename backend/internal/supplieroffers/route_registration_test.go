package supplieroffers_test

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"net/http"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// Every Supplier Offer route must actually register: a route that fails to
// mount would leave the Supplier unable to respond to an RFQ at all.
func TestSupplierOfferRoutesRegister(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1.0.0"))

	supplieroffers.RegisterHandlers(api, supplieroffers.NewService())

	want := []string{
		"supplier-offers-get-draft",
		"supplier-offers-create-draft",
		"supplier-offers-quote-line",
		"supplier-offers-decline-line",
		// Submission hard-requires OfferValidUntil, so without this route the
		// approved Supplier submission journey is impossible through production
		// HTTP even though SetOfferValidity has existed since Phase E (H1
		// acceptance defect).
		"supplier-offers-set-offer-validity",
		"supplier-offers-submit",
		"supplier-offers-withdraw",
		// M8.1: the seven previously-unrouted Supplier Offer capabilities.
		"supplier-offers-copy-forward",
		"supplier-offers-acknowledge-tax",
		"supplier-offers-acknowledge-charge-group",
		"supplier-offers-acknowledge-delivery-charge",
		"supplier-offers-remove-charge-group",
		"supplier-offers-remove-delivery-charge",
		"supplier-offers-reset-line",
		"supplier-access-offer-get-draft",
		"supplier-access-offer-create-draft",
		"supplier-access-offer-quote-line",
		"supplier-access-offer-decline-line",
		"supplier-access-offer-set-offer-validity",
		"supplier-access-offer-submit",
		"supplier-access-offer-withdraw",
		"supplier-access-offer-copy-forward",
		"supplier-access-offer-acknowledge-tax",
		"supplier-access-offer-acknowledge-charge-group",
		"supplier-access-offer-acknowledge-delivery-charge",
		"supplier-access-offer-remove-charge-group",
		"supplier-access-offer-remove-delivery-charge",
		"supplier-access-offer-reset-line",
		"supplier-access-offer-list-versions",
		"supplier-access-offer-get-version",
	}
	registered := map[string]bool{}
	for _, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{
			item.Get, item.Post, item.Put, item.Patch, item.Delete,
		} {
			if op != nil {
				registered[op.OperationID] = true
			}
		}
	}
	for _, id := range want {
		if !registered[id] {
			t.Errorf("route %q was not registered", id)
		}
	}
}
