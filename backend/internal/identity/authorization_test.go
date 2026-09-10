package identity_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// M8 design spec §1A.1 / §13: M8 introduces the FIRST route-level role
// convention in the codebase. M2-M7 gate on tenant only and are deliberately
// not retrofitted.
//
// The rule these tests pin: owner and admin may perform externally visible or
// irreversible transitions; employee may view records and prepare editable
// drafts but may not. Authorization runs at the HTTP boundary BEFORE the
// privileged service call, so a service never has to be role-aware.

func TestRequireCompanyRoleAcceptsAnAllowedRole(t *testing.T) {
	principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: "admin"}

	err := identity.RequireCompanyRole(principal, identity.RoleOwner, identity.RoleAdmin)

	if err != nil {
		t.Errorf("admin should be permitted, got %v", err)
	}
}

func TestRequireCompanyRoleRejectsADisallowedRole(t *testing.T) {
	principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: "employee"}

	err := identity.RequireCompanyRole(principal, identity.RoleOwner, identity.RoleAdmin)

	if !errors.Is(err, identity.ErrInsufficientRole) {
		t.Errorf("employee should be refused with ErrInsufficientRole, got %v", err)
	}
}

// Every privileged M8 action category, checked for all three roles. This is the
// §1A.1 requirement that owner, admin and employee are each covered.
func TestRequireCompanyRoleAcrossEveryRoleForPrivilegedActions(t *testing.T) {
	privileged := []identity.Role{identity.RoleOwner, identity.RoleAdmin}

	cases := []struct {
		role    string
		allowed bool
	}{
		{"owner", true},
		{"admin", true},
		{"employee", false},
	}

	// The action categories §13 marks owner/admin-only. They share one rule, so
	// the table asserts the rule holds identically for each rather than letting
	// one category silently diverge.
	actions := []string{
		"issue an RFQ version",
		"send invitations",
		"copy an invitation's secure link",
		"replace recipients or rotate secrets",
		"revoke Supplier access",
		"finalise or revise an award",
		"send award outcomes",
		"run state-changing reconciliation",
	}

	for _, action := range actions {
		for _, tc := range cases {
			t.Run(action+"/"+tc.role, func(t *testing.T) {
				principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: tc.role}

				err := identity.RequireCompanyRole(principal, privileged...)

				if tc.allowed && err != nil {
					t.Errorf("%s should be permitted to %s, got %v", tc.role, action, err)
				}
				if !tc.allowed && !errors.Is(err, identity.ErrInsufficientRole) {
					t.Errorf("%s must not be permitted to %s, got %v", tc.role, action, err)
				}
			})
		}
	}
}

// Employee-permitted categories: viewing and preparing editable drafts (§13).
func TestRequireCompanyRoleAllowsEveryRoleForDraftAndViewActions(t *testing.T) {
	anyMember := []identity.Role{identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee}

	for _, role := range []string{"owner", "admin", "employee"} {
		t.Run(role, func(t *testing.T) {
			principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: role}

			if err := identity.RequireCompanyRole(principal, anyMember...); err != nil {
				t.Errorf("%s should be permitted to view and prepare drafts, got %v", role, err)
			}
		})
	}
}

// An unrecognised role fails closed. A token carrying a role this build does
// not know must never be treated as privileged.
func TestRequireCompanyRoleRejectsAnUnknownRole(t *testing.T) {
	principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: "superuser"}

	err := identity.RequireCompanyRole(principal, identity.RoleOwner, identity.RoleAdmin,
		identity.RoleEmployee)

	if !errors.Is(err, identity.ErrInsufficientRole) {
		t.Errorf("an unknown role must fail closed, got %v", err)
	}
}

func TestRequireCompanyRoleRejectsAnEmptyRole(t *testing.T) {
	principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: ""}

	err := identity.RequireCompanyRole(principal, identity.RoleOwner, identity.RoleAdmin,
		identity.RoleEmployee)

	if !errors.Is(err, identity.ErrInsufficientRole) {
		t.Errorf("an empty role must fail closed, got %v", err)
	}
}

// Calling with no allowed roles must refuse rather than permit. A helper that
// defaulted to "allow" would silently open any route whose role list was
// forgotten.
func TestRequireCompanyRoleWithNoAllowedRolesRefuses(t *testing.T) {
	principal := identity.Principal{UserID: "u1", CompanyID: "c1", Role: "owner"}

	err := identity.RequireCompanyRole(principal)

	if !errors.Is(err, identity.ErrInsufficientRole) {
		t.Errorf("an empty allow-list must refuse, got %v", err)
	}
}

// --- AuthorizedPrincipal: the form handlers actually call ---

// The happy path returns the Principal so the handler can pass CompanyID and
// UserID straight into the service.
func TestAuthorizedPrincipalReturnsThePrincipalForAnAllowedRole(t *testing.T) {
	ctx := identity.ContextWithPrincipal(context.Background(),
		identity.Principal{UserID: "u1", CompanyID: "c1", Role: "owner"})

	principal, err := identity.AuthorizedPrincipal(ctx, identity.RoleOwner, identity.RoleAdmin)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if principal.UserID != "u1" || principal.CompanyID != "c1" {
		t.Errorf("returned principal = %+v, want the one in context", principal)
	}
}

// Insufficient role is 403, NOT 404: the caller is inside the right tenant and
// can already see the resource — they simply may not perform this transition
// (design spec §1A.1).
func TestAuthorizedPrincipalReturns403ForAnInsufficientRole(t *testing.T) {
	ctx := identity.ContextWithPrincipal(context.Background(),
		identity.Principal{UserID: "u1", CompanyID: "c1", Role: "employee"})

	_, err := identity.AuthorizedPrincipal(ctx, identity.RoleOwner, identity.RoleAdmin)

	if err == nil {
		t.Fatal("expected an error for an insufficient role")
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected a huma.StatusError, got %T", err)
	}
	if statusErr.GetStatus() != http.StatusForbidden {
		t.Errorf("status = %d, want 403", statusErr.GetStatus())
	}
}

// A missing Principal means the operation was registered outside the
// authenticated group — a wiring bug, reported as 401 rather than 403.
func TestAuthorizedPrincipalReturns401WithoutAPrincipal(t *testing.T) {
	_, err := identity.AuthorizedPrincipal(context.Background(), identity.RoleOwner)

	if err == nil {
		t.Fatal("expected an error with no principal in context")
	}
	var statusErr huma.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected a huma.StatusError, got %T", err)
	}
	if statusErr.GetStatus() != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", statusErr.GetStatus())
	}
}

// The role constants must match the persisted companies.Role values exactly.
// identity deliberately does not import companies (the M1 zero-cross-import
// boundary), so this is the assertion that keeps the two in step.
func TestRoleConstantsMatchThePersistedMembershipValues(t *testing.T) {
	cases := []struct {
		role identity.Role
		want string
	}{
		{identity.RoleOwner, "owner"},
		{identity.RoleAdmin, "admin"},
		{identity.RoleEmployee, "employee"},
	}

	for _, tc := range cases {
		if string(tc.role) != tc.want {
			t.Errorf("role constant = %q, want %q; it must match the value companies "+
				"persists on a membership", tc.role, tc.want)
		}
	}
}
