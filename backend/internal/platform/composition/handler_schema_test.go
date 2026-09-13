package composition_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/awards"
	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// A single Huma API owns one schema registry. If two independently owned
// handler packages use the same Go DTO type name for different shapes, the
// second registration panics and the composed server cannot start.
func TestMilestone7HandlersShareOneSchemaRegistry(t *testing.T) {
	_, api := platformhttp.NewRouter("M7 schema composition", "test")
	authedAPI := huma.NewGroup(api)

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("registering all M7 handlers on one Huma API panicked: %v", recovered)
		}
	}()

	materialrequirements.RegisterHandlers(authedAPI, nil)
	rfqs.RegisterHandlers(authedAPI, nil)
	suppliers.RegisterHandlers(authedAPI, nil)
}

func TestSupplierMutationsUseOnlyTheCanonicalCSRFHeader(t *testing.T) {
	_, api := platformhttp.NewRouter("M8 canonical CSRF", "test")
	supplieraccess.RegisterHandlers(api, nil, false, http.SameSiteLaxMode)
	supplieroffers.RegisterHandlers(api, nil)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal OpenAPI: %v", err)
	}
	openAPI := string(document)
	if !strings.Contains(openAPI, `"name":"X-CSRF-Token"`) {
		t.Error("Supplier mutation parameters do not document X-CSRF-Token")
	}
	if strings.Contains(openAPI, `"name":"X-Supplier-CSRF"`) {
		t.Error("legacy X-Supplier-CSRF remains in the Supplier HTTP contract")
	}
}

// The fast M8 composition gate registers every shipped M8 handler on one Huma
// document. Huma's shared registry will panic on incompatible schema names;
// the explicit scan below additionally guards operation identity, route
// ownership, and the external authentication contract.
func TestMilestone8HandlersComposeWithUniqueRoutesAndDocumentedSecurity(t *testing.T) {
	_, api := platformhttp.NewRouter("M8 schema composition", "test")

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("registering all M8 handlers on one Huma API panicked: %v", recovered)
		}
	}()

	// Supplier routes are mounted on the base API because their cookie-backed
	// authorization is enforced by Phase D, not contractor bearer middleware.
	supplieraccess.RegisterHandlers(api, nil, false, http.SameSiteLaxMode)
	supplieroffers.RegisterHandlers(api, nil)

	authedAPI := huma.NewGroup(api)
	authedAPI.UseModifier(func(op *huma.Operation, next func(*huma.Operation)) {
		op.Security = []map[string][]string{{platformhttp.BearerAuthSecurityScheme: {}}}
		next(op)
	})
	rfqissuance.RegisterHandlers(authedAPI, nil)
	awards.RegisterHandlers(authedAPI, nil)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal composed OpenAPI document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			OperationID string                `json:"operationId"`
			Security    []map[string][]string `json:"security"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]json.RawMessage `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode composed OpenAPI document: %v", err)
	}

	for _, scheme := range []string{"bearerAuth", "supplierAccessExchange", "supplierSession", "supplierCSRF"} {
		if _, ok := openAPI.Components.SecuritySchemes[scheme]; !ok {
			t.Errorf("OpenAPI security scheme %q is missing", scheme)
		}
	}

	type operationLocation struct{ method, path string }
	operations := map[string]operationLocation{}
	security := map[string][]map[string][]string{}
	for path, methods := range openAPI.Paths {
		for method, operation := range methods {
			// OpenAPI path-item metadata is not an HTTP operation.
			if operation.OperationID == "" {
				continue
			}
			if previous, duplicate := operations[operation.OperationID]; duplicate {
				t.Errorf("operation ID %q is shared by %s %s and %s %s",
					operation.OperationID, previous.method, previous.path, method, path)
			}
			operations[operation.OperationID] = operationLocation{method: method, path: path}
			security[operation.OperationID] = operation.Security
		}
	}

	assertSecurityRequirement(t, security, "supplier-access-open", map[string]bool{})
	assertSecurityRequirement(t, security, "supplier-access-create-challenge",
		map[string]bool{"supplierAccessExchange": true})
	assertSecurityRequirement(t, security, "supplier-offers-get-draft",
		map[string]bool{"supplierSession": true})
	assertSecurityRequirement(t, security, "supplier-offers-submit",
		map[string]bool{"supplierSession": true, "supplierCSRF": true})
	assertSecurityRequirement(t, security, "awards-get-comparison",
		map[string]bool{"bearerAuth": true})

	// M8.1: the seven previously-unrouted mutations require the same
	// session+CSRF contract as every other Supplier draft mutation.
	for _, id := range []string{
		"supplier-offers-copy-forward",
		"supplier-offers-acknowledge-tax",
		"supplier-offers-acknowledge-charge-group",
		"supplier-offers-acknowledge-delivery-charge",
		"supplier-offers-remove-charge-group",
		"supplier-offers-remove-delivery-charge",
		"supplier-offers-reset-line",
	} {
		assertSecurityRequirement(t, security, id,
			map[string]bool{"supplierSession": true, "supplierCSRF": true})
	}

	// A method/path pair is structurally unique in an OpenAPI paths map. Assert
	// the expected owners too, so a handler silently omitted from composition
	// cannot make this test pass vacuously.
	for _, expected := range []struct{ id, method, path string }{
		{"supplier-access-open", "get", "/supplier-access/open"},
		{"supplier-offers-get-draft", "get", "/supplier-offers/{invitationId}/draft"},
		{"rfq-issuance-get-version", "get", "/rfq-versions/{versionId}"},
		{"awards-get-comparison", "get", "/rfq-chains/{rfqChainId}/issued-versions/{versionId}/comparison"},
		{"supplier-offers-copy-forward", "post", "/supplier-offers/{invitationId}/draft/copy-forward"},
		{"supplier-offers-reset-line", "post", "/supplier-offers/{invitationId}/draft/lines/{lineId}/reset"},
	} {
		location, ok := operations[expected.id]
		if !ok {
			t.Errorf("expected composed operation %q is missing", expected.id)
			continue
		}
		if location.method != expected.method || location.path != expected.path {
			t.Errorf("operation %q registered at %s %s, want %s %s",
				expected.id, location.method, location.path, expected.method, expected.path)
		}
	}
}

func assertSecurityRequirement(t *testing.T, security map[string][]map[string][]string,
	operationID string, expected map[string]bool) {
	t.Helper()
	requirements, ok := security[operationID]
	if !ok {
		t.Fatalf("operation %q is missing", operationID)
	}
	if len(expected) == 0 {
		// OpenAPI's empty Security Requirement Object explicitly permits an
		// anonymous request while remaining visible after JSON omitempty rules.
		if len(requirements) != 1 || len(requirements[0]) != 0 {
			t.Errorf("operation %q security = %#v, want explicit public [{}]", operationID, requirements)
		}
		return
	}
	if len(requirements) != 1 {
		t.Errorf("operation %q security = %#v, want one AND requirement", operationID, requirements)
		return
	}
	for scheme := range expected {
		if _, ok := requirements[0][scheme]; !ok {
			t.Errorf("operation %q does not require %q: %#v", operationID, scheme, requirements)
		}
	}
}
