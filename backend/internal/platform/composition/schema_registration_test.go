package composition_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
)

// TestRegisterAllForSchemaComposesWithoutPanicking is the schema-only
// analogue of TestMilestone8HandlersComposeWithUniqueRoutesAndDocumentedSecurity
// in handler_schema_test.go, extended to the full production route
// inventory: every handler registers with nil services (schema construction
// never dereferences svc), so this proves the whole API composes on one
// Huma document with no MongoDB, no SMTP, and no server listener.
func TestRegisterAllForSchemaComposesWithoutPanicking(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-composition-test", "test")

	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("RegisterAllForSchema panicked: %v", recovered)
		}
	}()

	composition.RegisterAllForSchema(api)
}

func TestRegisterAllForSchemaProducesDeterministicOutput(t *testing.T) {
	_, api1 := platformhttp.NewRouter("schema-determinism-test", "test")
	composition.RegisterAllForSchema(api1)
	doc1, err := json.Marshal(api1.OpenAPI())
	if err != nil {
		t.Fatalf("marshal first document: %v", err)
	}

	_, api2 := platformhttp.NewRouter("schema-determinism-test", "test")
	composition.RegisterAllForSchema(api2)
	doc2, err := json.Marshal(api2.OpenAPI())
	if err != nil {
		t.Fatalf("marshal second document: %v", err)
	}

	if string(doc1) != string(doc2) {
		t.Fatal("two independent RegisterAllForSchema runs produced different OpenAPI documents")
	}
}

func TestRegisterAllForSchemaHasUniqueOperationIDs(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-unique-ops-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	seen := map[string]bool{}
	for path, methods := range openAPI.Paths {
		for method, op := range methods {
			if op.OperationID == "" {
				continue
			}
			if seen[op.OperationID] {
				t.Errorf("duplicate operationId %q (seen again at %s %s)", op.OperationID, method, path)
			}
			seen[op.OperationID] = true
		}
	}
	if len(seen) == 0 {
		t.Fatal("expected at least one registered operation")
	}
}

// TestRegisterAllForSchemaIncludesKeyF1Routes spot-checks that core M0-M2
// routes the frontend design depends on are present in the schema-only
// composition — a regression here would mean the OpenAPI export silently
// dropped routes the real server still serves.
func TestRegisterAllForSchemaIncludesKeyF1Routes(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-f1-routes-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	type expectation struct{ path, method, operationID string }
	for _, want := range []expectation{
		{"/auth/register", "post", "auth-register"},
		{"/auth/me", "get", "auth-me"},
		{"/clients", "get", "clients-list"},
		{"/clients/{id}", "patch", "clients-update"},
		{"/projects", "get", "projects-list"},
		{"/projects/{projectId}", "patch", "projects-update-name"},
		{"/properties", "get", "properties-list-by-project"},
		{"/spaces", "get", "spaces-list-by-project"},
		{"/work-items", "get", "work-items-list"},
		{"/work-items/{workItemId}", "patch", "work-items-update"},
		{"/companies/me", "get", "companies-get-me"},
		{"/companies/me/members", "get", "companies-get-me-members"},
	} {
		operation, ok := openAPI.Paths[want.path][want.method]
		if !ok || operation.OperationID != want.operationID {
			t.Errorf("%s %s = %#v, want operationId %q", want.method, want.path, operation, want.operationID)
		}
	}
}

// TestRegisterAllForSchemaClientPatchFieldsAreOptional proves PATCH
// /clients/{id}'s request body schema marks none of its fields required —
// the genuine partial-update contract (Checkpoint 9) must be visible in the
// OpenAPI document itself, not just enforced at runtime.
func TestRegisterAllForSchemaClientPatchFieldsAreOptional(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-client-patch-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Required []string `json:"required"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	patch, ok := openAPI.Paths["/clients/{id}"]["patch"]
	if !ok {
		t.Fatal("PATCH /clients/{id} not found in document")
	}
	body, ok := patch.RequestBody.Content["application/json"]
	if !ok {
		t.Fatal("PATCH /clients/{id} has no application/json request body")
	}
	if len(body.Schema.Required) != 0 {
		t.Errorf("PATCH /clients/{id} request body required = %v, want none (genuine partial update)", body.Schema.Required)
	}
}

// anyStringOrArray decodes an OpenAPI "type" keyword, which Huma emits
// either as a bare string ("string") for OpenAPI 3.0-style output or as an
// array (["string","null"]) for OpenAPI 3.1's native union syntax — this
// codebase's NewRouter uses huma.DefaultConfig, which targets 3.1.
type anyStringOrArray []string

func (a *anyStringOrArray) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = []string{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(data, &multi); err != nil {
		return err
	}
	*a = multi
	return nil
}

func (a anyStringOrArray) contains(want string) bool {
	for _, t := range a {
		if t == want {
			return true
		}
	}
	return false
}

// TestRegisterAllForSchemaWorkItemSpaceIDIsNullableString proves PATCH
// /work-items/{workItemId}'s spaceId field is documented as a plain
// nullable string in the OpenAPI schema (OpenAPI 3.1's type: [string, null]
// union) — optional.NullableString's SchemaProvider implementation, not the
// {Present,Value} Go struct shape.
func TestRegisterAllForSchemaWorkItemSpaceIDIsNullableString(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-workitem-patch-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]struct {
					Type anyStringOrArray `json:"type"`
				} `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	patch, ok := openAPI.Paths["/work-items/{workItemId}"]["patch"]
	if !ok {
		t.Fatal("PATCH /work-items/{workItemId} not found in document")
	}
	body, ok := patch.RequestBody.Content["application/json"]
	if !ok {
		t.Fatal("PATCH /work-items/{workItemId} has no application/json request body")
	}
	const prefix = "#/components/schemas/"
	schemaName, ok := strings.CutPrefix(body.Schema.Ref, prefix)
	if !ok {
		t.Fatalf("request body schema ref %q is not a components/schemas reference", body.Schema.Ref)
	}
	schema, ok := openAPI.Components.Schemas[schemaName]
	if !ok {
		t.Fatalf("component schema %q not found", schemaName)
	}
	spaceID, ok := schema.Properties["spaceId"]
	if !ok {
		t.Fatal("spaceId property missing from PATCH /work-items/{workItemId} schema")
	}
	if !spaceID.Type.contains("string") {
		t.Errorf("spaceId.type = %v, want it to contain \"string\"", spaceID.Type)
	}
	if !spaceID.Type.contains("null") {
		t.Errorf("spaceId.type = %v, want it to contain \"null\"", spaceID.Type)
	}
}

// TestRegisterAllForSchemaListRoutesExposeCanonicalPaginationInputs proves
// every F1 list endpoint accepts the shared page/pageSize/search/sort/order
// query parameters, per Checkpoint 7's canonical pagination contract.
func TestRegisterAllForSchemaListRoutesExposeCanonicalPaginationInputs(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-pagination-params-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	paginatedRoutes := map[string]string{
		"/clients":              "get",
		"/projects":             "get",
		"/companies/me/members": "get",
		"/spaces":               "get",
		"/work-items":           "get",
	}
	for path, method := range paginatedRoutes {
		operation, ok := openAPI.Paths[path][method]
		if !ok {
			t.Errorf("%s %s not found in document", method, path)
			continue
		}
		found := map[string]bool{}
		for _, p := range operation.Parameters {
			if p.In == "query" {
				found[p.Name] = true
			}
		}
		for _, want := range []string{"page", "pageSize", "search", "sort", "order"} {
			if !found[want] {
				t.Errorf("%s %s missing query parameter %q", method, path, want)
			}
		}
	}
}

// TestRegisterAllForSchemaListResponsesUseCanonicalEnvelope proves every F1
// list endpoint's 200 response schema has items/page/pageSize/total
// properties — the canonical pagination.Response[T] shape, not the old bare
// array envelope.
func TestRegisterAllForSchemaListResponsesUseCanonicalEnvelope(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-pagination-envelope-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			Responses map[string]struct {
				Content map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					} `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	paginatedRoutes := map[string]string{
		"/clients":              "get",
		"/projects":             "get",
		"/companies/me/members": "get",
		"/spaces":               "get",
		"/work-items":           "get",
	}
	const prefix = "#/components/schemas/"
	for path, method := range paginatedRoutes {
		operation, ok := openAPI.Paths[path][method]
		if !ok {
			t.Errorf("%s %s not found in document", method, path)
			continue
		}
		response, ok := operation.Responses["200"]
		if !ok {
			t.Errorf("%s %s has no 200 response", method, path)
			continue
		}
		body, ok := response.Content["application/json"]
		if !ok {
			t.Errorf("%s %s 200 response has no application/json content", method, path)
			continue
		}
		schemaName, ok := strings.CutPrefix(body.Schema.Ref, prefix)
		if !ok {
			t.Errorf("%s %s response schema ref %q is not a components/schemas reference", method, path, body.Schema.Ref)
			continue
		}
		schema, ok := openAPI.Components.Schemas[schemaName]
		if !ok {
			t.Errorf("%s %s component schema %q not found", method, path, schemaName)
			continue
		}
		for _, want := range []string{"items", "page", "pageSize", "total"} {
			if _, ok := schema.Properties[want]; !ok {
				t.Errorf("%s %s response schema %q missing property %q", method, path, schemaName, want)
			}
		}
	}
}

// TestRegisterAllForSchemaRefreshTokenRemainsExplicitCookieParameter proves
// /auth/refresh and /auth/logout still document refresh_token as an
// explicit cookie parameter (the F0A finding that it is not a named
// security scheme, so a generated client must be told to send credentials).
func TestRegisterAllForSchemaRefreshTokenRemainsExplicitCookieParameter(t *testing.T) {
	_, api := platformhttp.NewRouter("schema-refresh-cookie-test", "test")
	composition.RegisterAllForSchema(api)

	document, err := json.Marshal(api.OpenAPI())
	if err != nil {
		t.Fatalf("marshal document: %v", err)
	}
	var openAPI struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string `json:"name"`
				In   string `json:"in"`
			} `json:"parameters"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(document, &openAPI); err != nil {
		t.Fatalf("decode document: %v", err)
	}

	for path, method := range map[string]string{"/auth/refresh": "post", "/auth/logout": "post"} {
		operation, ok := openAPI.Paths[path][method]
		if !ok {
			t.Fatalf("%s %s not found in document", method, path)
		}
		found := false
		for _, p := range operation.Parameters {
			if p.In == "cookie" && p.Name == "refresh_token" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s %s does not document refresh_token as a cookie parameter", method, path)
		}
	}
}
