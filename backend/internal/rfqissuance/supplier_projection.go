package rfqissuance

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

// SupplierRFQAccessAuthorizer is the only supplieraccess capability the RFQ
// owner consumes. It returns authoritative tenant scope; CompanyID is never
// accepted from the external request.
type SupplierRFQAccessAuthorizer interface {
	AuthorizeSupplierRFQRead(context.Context, SupplierRFQReadAuthorization) (
		AuthorizedSupplierRFQAccess, error)
}

// SupplierRFQReadAuthorization contains only the untrusted values needed to
// verify a Supplier session at the external boundary.
type SupplierRFQReadAuthorization struct {
	SessionToken string
	InvitationID string
	AccessedAt   time.Time
}

// AuthorizedSupplierRFQAccess is the verified identity and invitation scope
// returned by the Phase D session owner.
type AuthorizedSupplierRFQAccess struct {
	CompanyID        string
	SupplierID       string
	InvitationID     string
	AccessGeneration int64
}

// SupplierRFQReadInput carries one invitation-scoped external read.
type SupplierRFQReadInput struct {
	SessionToken string
	InvitationID string
	AccessedAt   time.Time
}

// SupplierRFQHistoryInput adds immutable version-number pagination.
type SupplierRFQHistoryInput struct {
	SupplierRFQReadInput
	PageSize int
	Cursor   int
}

// SupplierRFQVersionInput identifies one immutable version within the
// invitation-scoped history. VersionID is still rechecked against the tenant,
// RFQ chain, and invitation pointer before projection.
type SupplierRFQVersionInput struct {
	SupplierRFQReadInput
	VersionID string
}

// SupplierRFQLine is the explicit Supplier-safe line allowlist. Source IDs and
// internal material identifiers intentionally have no place in this type.
type SupplierRFQLine struct {
	ID               string
	MaterialName     string
	Specification    string
	QuantityValue    string
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string
	SortOrder        int
}

// SupplierRFQVersion is the explicit Supplier-safe immutable RFQ allowlist.
type SupplierRFQVersion struct {
	ID                   string
	RFQNumber            string
	VersionNumber        int
	Currency             string
	Title                string
	IssuedAt             time.Time
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     time.Time
	SupplierInstructions string
	Lines                []SupplierRFQLine
}

// SupplierRFQVersionSummary is deliberately narrower than version detail.
type SupplierRFQVersionSummary struct {
	ID               string
	VersionNumber    int
	IssuedAt         time.Time
	ResponseDeadline time.Time
	IsCurrent        bool
}

type SupplierResponseWindow struct {
	Status     string
	Deadline   time.Time
	CanRespond bool
}

type SupplierInvitationRFQProjection struct {
	InvitationID      string
	Status            string
	ExpiresAt         time.Time
	CurrentRFQVersion SupplierRFQVersion
	ResponseWindow    SupplierResponseWindow
}

type SupplierRFQHistoryPage struct {
	Versions   []SupplierRFQVersionSummary
	NextCursor *int
}

// authorizeSupplierRFQRead rechecks the mutable invitation after session
// authorization. That second authoritative read is what makes access rotation,
// revocation, and expiry fail closed instead of trusting stale session claims.
func (s *Service) authorizeSupplierRFQRead(ctx context.Context, input SupplierRFQReadInput) (
	AuthorizedSupplierRFQAccess, SupplierInvitation, IssuedRFQVersion, error) {
	if s.supplierRFQAccess == nil || s.invitations == nil {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
			ErrInvitationsNotConfigured
	}
	if strings.TrimSpace(input.SessionToken) == "" || strings.TrimSpace(input.InvitationID) == "" ||
		input.AccessedAt.IsZero() {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
			ErrSupplierRFQAccessInvalid
	}

	authorized, err := s.supplierRFQAccess.AuthorizeSupplierRFQRead(ctx,
		SupplierRFQReadAuthorization(input))
	if err != nil {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{}, err
	}
	if authorized.InvitationID != input.InvitationID || authorized.CompanyID == "" ||
		authorized.SupplierID == "" {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
			ErrSupplierRFQAccessInvalid
	}

	invitation, err := s.invitations.FindInvitation(ctx, authorized.CompanyID, input.InvitationID)
	if err != nil {
		if errors.Is(err, ErrInvitationNotFound) {
			return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
				ErrSupplierRFQAccessInvalid
		}
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{}, err
	}
	if invitation.SupplierID != authorized.SupplierID ||
		invitation.AccessGeneration != authorized.AccessGeneration ||
		!invitation.PermitsAccess(input.AccessedAt) {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
			ErrSupplierRFQAccessInvalid
	}

	current, err := s.versions.FindVersion(ctx, authorized.CompanyID,
		invitation.CurrentIssuedRFQVersionID)
	if err != nil {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{}, err
	}
	if current.RFQChainID != invitation.RFQChainID {
		return AuthorizedSupplierRFQAccess{}, SupplierInvitation{}, IssuedRFQVersion{},
			ErrIssuedVersionNotFound
	}
	return authorized, invitation, current, nil
}

// GetSupplierInvitationRFQ returns the current invitation and RFQ projection;
// it never exposes internal tenancy, project, provenance, or operation fields.
func (s *Service) GetSupplierInvitationRFQ(ctx context.Context, input SupplierRFQReadInput) (
	SupplierInvitationRFQProjection, error) {
	_, invitation, current, err := s.authorizeSupplierRFQRead(ctx, input)
	if err != nil {
		return SupplierInvitationRFQProjection{}, err
	}
	canRespond := input.AccessedAt.Before(current.ResponseDeadline)
	windowStatus := "closed"
	if canRespond {
		windowStatus = "open"
	}
	return SupplierInvitationRFQProjection{
		InvitationID:      invitation.ID,
		Status:            string(invitation.Status),
		ExpiresAt:         invitation.ExpiresAt,
		CurrentRFQVersion: projectSupplierRFQVersion(current),
		ResponseWindow: SupplierResponseWindow{
			Status: windowStatus, Deadline: current.ResponseDeadline, CanRespond: canRespond,
		},
	}, nil
}

// ListSupplierRFQVersions pages only versions at or behind the invitation's
// current pointer. A concurrent future issuance can therefore never leak into
// a read authorized against the earlier invitation snapshot.
func (s *Service) ListSupplierRFQVersions(ctx context.Context, input SupplierRFQHistoryInput) (
	SupplierRFQHistoryPage, error) {
	authorized, invitation, current, err := s.authorizeSupplierRFQRead(ctx,
		input.SupplierRFQReadInput)
	if err != nil {
		return SupplierRFQHistoryPage{}, err
	}
	pageSize, err := procurementlimits.PageSize(input.PageSize)
	if err != nil || input.Cursor < 0 || input.Cursor > current.VersionNumber {
		return SupplierRFQHistoryPage{}, ErrInputLimitExceeded
	}
	versions, err := s.versions.ListVersions(ctx, authorized.CompanyID, invitation.RFQChainID)
	if err != nil {
		return SupplierRFQHistoryPage{}, err
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })

	eligible := make([]IssuedRFQVersion, 0, len(versions))
	for _, version := range versions {
		if version.VersionNumber > current.VersionNumber ||
			(input.Cursor > 0 && version.VersionNumber >= input.Cursor) {
			continue
		}
		eligible = append(eligible, version)
	}
	page := SupplierRFQHistoryPage{Versions: make([]SupplierRFQVersionSummary, 0, pageSize)}
	for index, version := range eligible {
		if index == pageSize {
			cursor := page.Versions[len(page.Versions)-1].VersionNumber
			page.NextCursor = &cursor
			break
		}
		page.Versions = append(page.Versions, SupplierRFQVersionSummary{
			ID: version.ID, VersionNumber: version.VersionNumber, IssuedAt: version.IssuedAt,
			ResponseDeadline: version.ResponseDeadline,
			IsCurrent:        version.ID == invitation.CurrentIssuedRFQVersionID,
		})
	}
	return page, nil
}

// GetSupplierRFQVersion returns one exact historical version only when it is a
// member of the invitation's chain and no newer than its current pointer.
func (s *Service) GetSupplierRFQVersion(ctx context.Context, input SupplierRFQVersionInput) (
	SupplierRFQVersion, error) {
	authorized, invitation, current, err := s.authorizeSupplierRFQRead(ctx,
		input.SupplierRFQReadInput)
	if err != nil {
		return SupplierRFQVersion{}, err
	}
	if strings.TrimSpace(input.VersionID) == "" {
		return SupplierRFQVersion{}, ErrSupplierRFQAccessInvalid
	}
	version, err := s.versions.FindVersion(ctx, authorized.CompanyID, input.VersionID)
	if err != nil {
		if errors.Is(err, ErrIssuedVersionNotFound) {
			return SupplierRFQVersion{}, ErrSupplierRFQAccessInvalid
		}
		return SupplierRFQVersion{}, err
	}
	if version.RFQChainID != invitation.RFQChainID ||
		version.VersionNumber > current.VersionNumber {
		return SupplierRFQVersion{}, ErrSupplierRFQAccessInvalid
	}
	return projectSupplierRFQVersion(version), nil
}

func projectSupplierRFQVersion(version IssuedRFQVersion) SupplierRFQVersion {
	lines := make([]SupplierRFQLine, 0, len(version.Lines))
	for _, line := range version.Lines {
		lines = append(lines, SupplierRFQLine{
			ID: line.ID, MaterialName: line.MaterialName, Specification: line.Specification,
			QuantityValue: line.Quantity.Value.String(), QuantityUnit: line.Quantity.Unit,
			RequiredByDate: line.RequiredByDate, ProcurementNotes: line.ProcurementNotes,
			SortOrder: line.SortOrder,
		})
	}
	return SupplierRFQVersion{
		ID: version.ID, RFQNumber: version.RFQNumber, VersionNumber: version.VersionNumber,
		Currency: version.Currency, Title: version.Title, IssuedAt: version.IssuedAt,
		DeliveryAddress: version.DeliveryAddress, RequiredByDate: version.RequiredByDate,
		ResponseDeadline:     version.ResponseDeadline,
		SupplierInstructions: version.SupplierInstructions, Lines: lines,
	}
}
