package supplieroffers_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

// H1 acceptance defect coverage.
//
// Every protected Supplier Offer mutation shares one embedded credential
// struct. Huma resolves `path`, `cookie` and `header` parameters by walking
// exported fields, and it SKIPS an embedded struct whose type is unexported.
// While the shared type was unexported, the session cookie, CSRF header and
// invitation path parameter silently arrived empty, so create-draft, quote,
// decline, set-validity, submit and withdraw were unreachable through the
// composed router even though each one authorized correctly when its service
// was called directly.
//
// The OpenAPI document is the cheapest place to observe this: a parameter that
// cannot be bound is never declared.
func TestSupplierOfferMutationsDeclareTheirCredentialParameters(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "1.0.0"))
	supplieroffers.RegisterHandlers(api, supplieroffers.NewService())

	// Every mutation carries the invitation in its path; without it the route
	// cannot identify which invitation the Supplier is acting for.
	mutations := map[string]string{
		"/supplier-offers/{invitationId}/draft":                           http.MethodPost,
		"/supplier-offers/{invitationId}/draft/offer-validity":            http.MethodPut,
		"/supplier-offers/{invitationId}/submissions":                     http.MethodPost,
		"/supplier-offers/{invitationId}/draft/lines/{lineId}/quote":      http.MethodPut,
		"/supplier-offers/{invitationId}/draft/lines/{lineId}/decline":    http.MethodPut,
		"/supplier-offers/{invitationId}/versions/{versionId}/withdrawal": http.MethodPost,
		// M8.1: the seven previously-unrouted capabilities.
		"/supplier-offers/{invitationId}/draft/copy-forward":                                          http.MethodPost,
		"/supplier-offers/{invitationId}/draft/tax/acknowledge":                                       http.MethodPost,
		"/supplier-offers/{invitationId}/draft/charge-groups/{chargeGroupId}/acknowledge":             http.MethodPost,
		"/supplier-offers/{invitationId}/draft/delivery-charge/acknowledge":                           http.MethodPost,
		"/supplier-offers/{invitationId}/draft/charge-groups/{chargeGroupId}/remove":                  http.MethodPost,
		"/supplier-offers/{invitationId}/draft/delivery-charge/remove":                                http.MethodPost,
		"/supplier-offers/{invitationId}/draft/lines/{lineId}/reset":                                  http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer":                                           http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/validity":                                  http.MethodPut,
		"/supplier-access/invitations/{invitationId}/offer/submissions":                               http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/quote":                      http.MethodPut,
		"/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/decline":                    http.MethodPut,
		"/supplier-access/invitations/{invitationId}/offer/versions/{versionId}/withdrawal":           http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/copy-forward":                              http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/tax/acknowledge":                           http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/charge-groups/{chargeGroupId}/acknowledge": http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/delivery-charge/acknowledge":               http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/charge-groups/{chargeGroupId}/remove":      http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/delivery-charge/remove":                    http.MethodPost,
		"/supplier-access/invitations/{invitationId}/offer/lines/{lineId}/reset":                      http.MethodPost,
	}

	for path, method := range mutations {
		item, ok := api.OpenAPI().Paths[path]
		if !ok {
			t.Errorf("route %s is not registered", path)
			continue
		}
		var operation *huma.Operation
		switch method {
		case http.MethodPost:
			operation = item.Post
		case http.MethodPut:
			operation = item.Put
		}
		if operation == nil {
			t.Errorf("%s %s is not registered", method, path)
			continue
		}

		declared := map[string]bool{}
		for _, parameter := range operation.Parameters {
			declared[parameter.In+":"+parameter.Name] = true
		}
		for _, required := range []string{
			"path:invitationId",
			"cookie:supplier_session",
			"cookie:supplier_csrf",
			"header:X-CSRF-Token",
		} {
			if !declared[required] {
				t.Errorf("%s %s does not bind %s; the credential cannot reach the handler",
					method, path, required)
			}
		}
	}
}

// The shared credential type must stay EXPORTED. Huma skips an embedded struct
// whose type is unexported, which silently delivers empty credentials to every
// mutation; unexporting it again would reintroduce exactly that defect while
// still compiling.
//
// That the bound values then flow through Phase D authorization end to end is
// proven against the real router and MongoDB by the tenanttest H1 journey.
func TestSupplierOfferCredentialsAreBindableByTheFramework(t *testing.T) {
	credentials := reflect.TypeOf(supplieroffers.SupplierOfferCredentials{})

	embedded := reflect.StructOf([]reflect.StructField{{
		Name:      credentials.Name(),
		Type:      credentials,
		Anonymous: true,
	}})
	if field := embedded.Field(0); !field.IsExported() {
		t.Fatalf("embedded %s is unexported, so Huma skips every parameter it declares",
			credentials.Name())
	}

	for _, parameter := range []struct{ field, tag, want string }{
		{"SessionCookie", "cookie", "supplier_session"},
		{"CSRFCookie", "cookie", "supplier_csrf"},
		{"CSRFHeader", "header", "X-CSRF-Token"},
		{"InvitationID", "path", "invitationId"},
	} {
		field, ok := credentials.FieldByName(parameter.field)
		if !ok {
			t.Errorf("credential field %s is missing", parameter.field)
			continue
		}
		if got := field.Tag.Get(parameter.tag); got != parameter.want {
			t.Errorf("%s %s tag = %q, want %q",
				parameter.field, parameter.tag, got, parameter.want)
		}
	}
}
