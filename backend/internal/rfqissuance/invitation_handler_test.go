package rfqissuance_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// The invitation HTTP surface and its role boundary (design spec §12.2, §13,
// §1A.1).
//
// §13 splits the invitation routes:
//
//	every member  — view invitations, create a draft invitation
//	owner/admin   — send, resend, copy-link, replace recipient, rotate, revoke
//
// The split is not cosmetic: everything on the owner/admin side is externally
// visible or irreversible. An employee preparing a draft invitation cannot
// cause a Supplier to be contacted or an existing link to stop working.

// operationIDs returns every OperationID present in the generated OpenAPI
// document, which is the observable route contract.
func operationIDs(t *testing.T) map[string]bool {
	t.Helper()

	router, api := platformhttp.NewRouter("rfqissuance-invitation-test", "0.0.0")
	rfqissuance.RegisterHandlers(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d, want 200", rec.Code)
	}

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	found := map[string]bool{}
	for _, methods := range spec.Paths {
		for _, operation := range methods {
			if operation.OperationID != "" {
				found[operation.OperationID] = true
			}
		}
	}
	return found
}

// Every §12.2 invitation capability must be reachable. A service can be
// perfectly wired and still leave the contractor unable to invite anyone.
func TestRegisterHandlersExposesEveryInvitationRoute(t *testing.T) {
	found := operationIDs(t)

	required := []string{
		"rfq-issuance-create-invitation",
		"rfq-issuance-list-invitations",
		"rfq-issuance-get-invitation",
		"rfq-issuance-send-invitation",
		"rfq-issuance-resend-invitation",
		"rfq-issuance-copy-invitation-link",
		"rfq-issuance-replace-invitation-recipient",
		"rfq-issuance-rotate-invitation-secret",
		"rfq-issuance-revoke-invitation",
		"rfq-issuance-reactivate-invitation",
		"rfq-issuance-update-invitation-expiry",
		"rfq-issuance-reconcile-invitations",
	}

	for _, operationID := range required {
		if !found[operationID] {
			t.Errorf("OpenAPI operation %q is missing; the capability is unreachable",
				operationID)
		}
	}
}

// The copy-link route must be POST, never GET.
//
// A GET would put the raw secret in a URL, and therefore in browser history,
// referrer headers and any proxy access log — defeating the derivation design
// entirely (§1A.4).
func TestCopyLinkRouteIsPostNotGet(t *testing.T) {
	router, api := platformhttp.NewRouter("rfqissuance-invitation-test", "0.0.0")
	rfqissuance.RegisterHandlers(api, nil)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode OpenAPI: %v", err)
	}

	for path, methods := range spec.Paths {
		for method, operation := range methods {
			if operation.OperationID != "rfq-issuance-copy-invitation-link" {
				continue
			}
			if method != "post" {
				t.Errorf("copy-link is exposed as %s %s. It must be POST: a GET would put "+
					"the raw secret into browser history, referrer headers and proxy "+
					"logs (§1A.4)", method, path)
			}
		}
	}
}

// --- role boundary (§13) ---

// The privileged invitation actions must be owner/admin only.
func TestPrivilegedInvitationActionsRefuseAnEmployee(t *testing.T) {
	privileged := []string{
		"send an invitation",
		"resend an invitation",
		"copy an invitation link",
		"replace an invitation recipient",
		"rotate an invitation secret",
		"revoke an invitation",
		"reactivate an invitation",
		"reconcile invitations",
	}

	for _, action := range privileged {
		t.Run(action, func(t *testing.T) {
			for _, role := range []string{"owner", "admin"} {
				if err := roleCheckFor(role, privilegedInvitationRoles()); err != nil {
					t.Errorf("%s must be permitted to %s, got %v", role, action, err)
				}
			}
			if err := roleCheckFor("employee", privilegedInvitationRoles()); err == nil {
				t.Errorf("an employee must NOT be permitted to %s: it is externally "+
					"visible or irreversible (§13)", action)
			}
		})
	}
}

// Viewing and preparing an invitation draft is open to every member (§13).
func TestViewAndDraftInvitationActionsPermitEveryRole(t *testing.T) {
	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			if err := roleCheckFor(role, anyMemberInvitationRoles()); err != nil {
				t.Errorf("%s must be permitted to view and prepare invitations, got %v",
					role, err)
			}
		})
	}
}

// roleCheckFor exercises the SAME role sets the handlers use, so the test
// cannot drift from production by asserting a duplicated list.
func roleCheckFor(role string, allowed []identity.Role) error {
	return identity.RequireCompanyRole(
		identity.Principal{UserID: "u1", CompanyID: "c1", Role: role}, allowed...)
}

func privilegedInvitationRoles() []identity.Role {
	return rfqissuance.PrivilegedInvitationRoles()
}

func anyMemberInvitationRoles() []identity.Role {
	return rfqissuance.AnyMemberInvitationRoles()
}
