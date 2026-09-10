package rfqissuance

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

// The contractor invitation routes (design spec §12.2, §13, §1A.1).
//
// The role split below is the substance of §13, not decoration. Everything on
// the privileged side is externally visible or irreversible: it causes a
// Supplier to be contacted, or makes an already-distributed link stop working.
// Preparing and reading invitations is open to every member because neither
// reaches outside the company.

// PrivilegedInvitationRoles gate the externally visible or irreversible
// invitation actions: send, resend, copy-link, replace recipient, rotate
// secret, revoke, reactivate, change expiry, and reconciliation.
//
// Exported so the handler tests assert the SAME set the handlers apply, rather
// than a duplicated list that could silently drift.
func PrivilegedInvitationRoles() []identity.Role {
	return []identity.Role{identity.RoleOwner, identity.RoleAdmin}
}

// AnyMemberInvitationRoles gate viewing invitations and preparing invitation
// drafts — neither contacts a Supplier nor invalidates an existing link.
func AnyMemberInvitationRoles() []identity.Role {
	return []identity.Role{identity.RoleOwner, identity.RoleAdmin, identity.RoleEmployee}
}

// --- DTOs ---

// invitationDTO is the CONTRACTOR-facing invitation projection.
//
// AccessSecretHash is structurally absent, and so is any raw token: the hash is
// an internal verification value with no client use, and the raw link is
// returned ONLY by the copy-link response.
type invitationDTO struct {
	ID                        string `json:"id"`
	RFQChainID                string `json:"rfqChainId"`
	SupplierID                string `json:"supplierId"`
	CurrentIssuedRFQVersionID string `json:"currentIssuedRfqVersionId"`

	RecipientName  string `json:"recipientName"`
	RecipientEmail string `json:"recipientEmail"`

	Status    string `json:"status"`
	ExpiresAt string `json:"expiresAt"`
	RevokedAt string `json:"revokedAt,omitempty"`

	// AccessGeneration is surfaced so the contractor can see that a rotation or
	// recipient replacement invalidated the previous link.
	AccessGeneration int64 `json:"accessGeneration"`

	FirstViewedAt string `json:"firstViewedAt,omitempty"`
	LastViewedAt  string `json:"lastViewedAt,omitempty"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func toInvitationDTO(i SupplierInvitation) invitationDTO {
	return invitationDTO{
		ID: i.ID, RFQChainID: i.RFQChainID, SupplierID: i.SupplierID,
		CurrentIssuedRFQVersionID: i.CurrentIssuedRFQVersionID,
		RecipientName:             i.RecipientName,
		RecipientEmail:            i.RecipientEmail,
		Status:                    string(i.Status),
		ExpiresAt:                 formatTime(i.ExpiresAt),
		RevokedAt:                 formatOptionalTime(i.RevokedAt),
		AccessGeneration:          i.AccessGeneration,
		FirstViewedAt:             formatOptionalTime(i.FirstViewedAt),
		LastViewedAt:              formatOptionalTime(i.LastViewedAt),
		Revision:                  i.Revision,
		CreatedAt:                 formatTime(i.CreatedAt),
		UpdatedAt:                 formatTime(i.UpdatedAt),
	}
}

// invitationLinkDTO carries the raw link, returned ONCE to the contractor.
//
// This is the only response type in the module that contains a secret. It is
// never persisted, logged or audited — see secret_exposure_test.go.
type invitationLinkDTO struct {
	URL string `json:"url"`
}

type invitationOutput struct {
	Status int
	Body   invitationDTO
}

type invitationListOutput struct {
	Status int
	Body   struct {
		Invitations []invitationDTO `json:"invitations"`
	}
}

type invitationLinkOutput struct {
	Status int
	// Cache-Control and Referrer-Policy are set because the body carries a
	// secret: a cached copy or a leaked referrer would outlive the response.
	CacheControl   string `header:"Cache-Control"`
	ReferrerPolicy string `header:"Referrer-Policy"`
	Body           invitationLinkDTO
}

const (
	secretCacheControl   = "no-store"
	secretReferrerPolicy = "no-referrer"
)

type createInvitationHTTPInput struct {
	RFQChainID string `path:"rfqChainId" maxLength:"128" pattern:"^[!-~]+$"`
	Body       struct {
		SupplierID     string `json:"supplierId" maxLength:"128" pattern:"^[!-~]+$"`
		RecipientName  string `json:"recipientName" maxLength:"200"`
		RecipientEmail string `json:"recipientEmail"`
		ExpiresAt      string `json:"expiresAt"`
	}
}

type invitationPathInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
}

type sendInvitationHTTPInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
	Body         struct {
		OperationID string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type replaceRecipientHTTPInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
	Body         struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		RecipientName    string `json:"recipientName" maxLength:"200"`
		RecipientEmail   string `json:"recipientEmail"`
		// OperationID lets a client safely retry a replacement whose response
		// was lost, instead of minting a second access generation.
		OperationID string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type invitationRevisionInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
	Body         struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
}

type updateExpiryHTTPInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
	Body         struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		ExpiresAt        string `json:"expiresAt"`
	}
}

type reactivateInvitationHTTPInput struct {
	RFQChainID   string `path:"rfqChainId"`
	InvitationID string `path:"invitationId"`
	Body         struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		ExpiresAt        string `json:"expiresAt"`
		OperationID      string `json:"operationId" maxLength:"128" pattern:"^[!-~]+$"`
	}
}

type sendResultOutput struct {
	Status int
	Body   struct {
		AttemptID string `json:"attemptId"`
		Status    string `json:"status"`
	}
}

type reconcileInvitationsOutput struct {
	Status int
	Body   struct {
		AdvancedInvitations int64 `json:"advancedInvitations"`
	}
}

// parseRequiredTime parses an RFC3339 timestamp, mapping a malformed value to
// 422 rather than letting a zero time through as though it were supplied.
func parseRequiredTime(field, value string) (time.Time, error) {
	parsed, err := time.Parse(timeLayout, value)
	if err != nil {
		return time.Time{}, huma.Error422UnprocessableEntity(
			field + " must be an RFC3339 timestamp")
	}
	return parsed, nil
}

// RegisterInvitationHandlers mounts the contractor invitation routes on the
// AUTHENTICATED group.
func RegisterInvitationHandlers(api huma.API, svc *Service) {
	// Creating a DRAFT invitation contacts no one, so every member may do it.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-create-invitation",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations",
		Summary:     "Create a draft Supplier Invitation",
	}, func(ctx context.Context, input *createInvitationHTTPInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, AnyMemberInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		expiresAt, err := parseRequiredTime("expiresAt", input.Body.ExpiresAt)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.CreateInvitation(ctx, principal.CompanyID, principal.UserID,
			CreateInvitationInput{
				RFQChainID:     input.RFQChainID,
				SupplierID:     input.Body.SupplierID,
				RecipientName:  input.Body.RecipientName,
				RecipientEmail: input.Body.RecipientEmail,
				ExpiresAt:      expiresAt,
			})
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{
			Status: http.StatusCreated, Body: toInvitationDTO(invitation),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-list-invitations",
		Method:      http.MethodGet,
		Path:        "/rfq-chains/{rfqChainId}/invitations",
		Summary:     "List Supplier Invitations for an RFQ chain",
	}, func(ctx context.Context, input *chainPathInput) (*invitationListOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, AnyMemberInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		invitations, err := svc.ListInvitations(ctx, principal.CompanyID, input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}

		out := &invitationListOutput{Status: http.StatusOK}
		out.Body.Invitations = make([]invitationDTO, 0, len(invitations))
		for _, invitation := range invitations {
			out.Body.Invitations = append(out.Body.Invitations, toInvitationDTO(invitation))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-get-invitation",
		Method:      http.MethodGet,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}",
		Summary:     "Read one Supplier Invitation",
	}, func(ctx context.Context, input *invitationPathInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, AnyMemberInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.GetInvitation(ctx, principal.CompanyID, input.InvitationID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	// --- privileged: externally visible or irreversible (§13) ---

	registerSend := func(operationID, path, summary string) {
		huma.Register(api, huma.Operation{
			OperationID: operationID,
			Method:      http.MethodPost,
			Path:        path,
			Summary:     summary,
		}, func(ctx context.Context, input *sendInvitationHTTPInput) (*sendResultOutput, error) {
			principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
			if err != nil {
				return nil, err
			}

			result, err := svc.SendInvitation(ctx, principal.CompanyID, principal.UserID,
				SendInvitationInput{
					InvitationID: input.InvitationID,
					OperationID:  input.Body.OperationID,
				})
			if err != nil {
				return nil, MapIssuanceError(err)
			}

			out := &sendResultOutput{Status: http.StatusOK}
			out.Body.AttemptID = result.AttemptID
			out.Body.Status = string(result.Status)
			return out, nil
		})
	}

	// send and resend share one implementation: a resend is simply another
	// delivery attempt on the same stable invitation (§5.2). They are separate
	// routes because the contractor's intent differs and the audit trail should
	// say which was meant.
	registerSend("rfq-issuance-send-invitation",
		"/rfq-chains/{rfqChainId}/invitations/{invitationId}/send",
		"Send a Supplier Invitation by email")
	registerSend("rfq-issuance-resend-invitation",
		"/rfq-chains/{rfqChainId}/invitations/{invitationId}/resend",
		"Resend a Supplier Invitation, reusing the current secure link")

	// POST rather than GET: a GET would place the raw secret in the URL, and
	// therefore in browser history, referrer headers and proxy logs (§1A.4).
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-copy-invitation-link",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/copy-link",
		Summary:     "Return the invitation's current secure link for manual sharing",
	}, func(ctx context.Context, input *invitationPathInput) (*invitationLinkOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		link, err := svc.CopyInvitationLink(ctx, principal.CompanyID, principal.UserID,
			input.InvitationID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationLinkOutput{
			Status:         http.StatusOK,
			CacheControl:   secretCacheControl,
			ReferrerPolicy: secretReferrerPolicy,
			Body:           invitationLinkDTO{URL: link.URL},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-replace-invitation-recipient",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/replace-recipient",
		Summary:     "Replace the recipient, rotating the secure link",
	}, func(ctx context.Context, input *replaceRecipientHTTPInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.ReplaceRecipient(ctx, principal.CompanyID, principal.UserID,
			ReplaceRecipientInput{
				InvitationID:     input.InvitationID,
				ExpectedRevision: input.Body.ExpectedRevision,
				RecipientName:    input.Body.RecipientName,
				RecipientEmail:   input.Body.RecipientEmail,
				OperationID:      input.Body.OperationID,
			})
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-rotate-invitation-secret",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/rotate-secret",
		Summary:     "Rotate the invitation's secure link, invalidating the previous one",
	}, func(ctx context.Context, input *invitationRevisionInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.RotateInvitationSecret(ctx, principal.CompanyID,
			principal.UserID, input.InvitationID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-revoke-invitation",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/revoke",
		Summary:     "Revoke Supplier access immediately, preserving all history",
	}, func(ctx context.Context, input *invitationRevisionInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.RevokeInvitation(ctx, principal.CompanyID, principal.UserID,
			input.InvitationID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	// Reactivation is explicit and privileged because it restores Supplier
	// access. The service creates a new generation; this route can never
	// resurrect the link whose revocation or expiry ended access.
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-reactivate-invitation",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/reactivate",
		Summary:     "Reactivate revoked or expired Supplier access with a new link",
	}, func(ctx context.Context, input *reactivateInvitationHTTPInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		expiresAt, err := parseRequiredTime("expiresAt", input.Body.ExpiresAt)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.ReactivateInvitation(ctx, principal.CompanyID,
			principal.UserID, ReactivateInvitationInput{
				InvitationID:     input.InvitationID,
				ExpectedRevision: input.Body.ExpectedRevision,
				ExpiresAt:        expiresAt,
				OperationID:      input.Body.OperationID,
			})
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	// Extending expiry widens external access, so it is privileged (§5.4).
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-update-invitation-expiry",
		Method:      http.MethodPatch,
		Path:        "/rfq-chains/{rfqChainId}/invitations/{invitationId}/expiry",
		Summary:     "Change when Supplier access expires",
	}, func(ctx context.Context, input *updateExpiryHTTPInput) (*invitationOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		expiresAt, err := parseRequiredTime("expiresAt", input.Body.ExpiresAt)
		if err != nil {
			return nil, err
		}

		invitation, err := svc.UpdateInvitationExpiry(ctx, principal.CompanyID,
			principal.UserID, input.InvitationID, input.Body.ExpectedRevision, expiresAt)
		if err != nil {
			return nil, MapIssuanceError(err)
		}
		return &invitationOutput{Status: http.StatusOK, Body: toInvitationDTO(invitation)}, nil
	})

	// Reconciliation is a state-changing repair, so it is owner/admin (§13).
	huma.Register(api, huma.Operation{
		OperationID: "rfq-issuance-reconcile-invitations",
		Method:      http.MethodPost,
		Path:        "/rfq-chains/{rfqChainId}/invitations/reconcile",
		Summary:     "Advance invitations lagging behind the current issued version",
	}, func(ctx context.Context, input *chainPathInput) (*reconcileInvitationsOutput, error) {
		principal, err := identity.AuthorizedPrincipal(ctx, PrivilegedInvitationRoles()...)
		if err != nil {
			return nil, err
		}

		advanced, err := svc.ReconcileInvitationAdvancement(ctx, principal.CompanyID,
			principal.UserID, input.RFQChainID)
		if err != nil {
			return nil, MapIssuanceError(err)
		}

		out := &reconcileInvitationsOutput{Status: http.StatusOK}
		out.Body.AdvancedInvitations = advanced
		return out, nil
	})
}
