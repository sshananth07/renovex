package composition

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// M8's composition adapters (design spec §2.1, §1A.3).
//
// Two capabilities cross the M7/M8 line in opposite directions:
//
//	rfqissuance needs the M7 ready RFQ snapshot -> ReadyRFQSourceAdapter
//	rfqs needs to know a chain has been issued  -> IssuanceStatusAdapter
//
// Read naively that is a construction cycle. It is not, because rfqs accepts
// its issuance source AFTER construction: the root builds rfqs with
// NoExternalIssuanceSource, builds rfqissuance over it, then calls
// SetIssuanceStatusSource with the adapter below. That setter exists in M7
// precisely for this handoff.
//
// This file and m7adapters.go are the only non-root production files permitted
// to import more than one domain module, enforced by
// TestOnlyCompositionAdaptersImportMultipleDomainModules.

// --- rfqs -> rfqissuance.ReadyRFQSource ---

// readyRFQSource is the narrow slice of rfqs.Service this adapter needs.
// Depending on the method rather than the concrete service keeps the adapter
// unit-testable and states exactly which M7 surface M8 touches.
type readyRFQSource interface {
	GetReadyRFQSnapshot(ctx context.Context, companyID, rfqChainID string) (
		rfqNumber string, projectID string, revision int64, title string,
		deliveryAddress string, requiredBy *time.Time, responseDeadline *time.Time,
		supplierInstructions string, lines []rfqs.RFQLineSnapshot,
		found bool, err error)
}

// ReadyRFQSourceAdapter converts M7's named-return snapshot into the
// rfqissuance-owned struct.
//
// It exists because Go interface satisfaction requires exact types: rfqs.Service
// returns nine positional values including []rfqs.RFQLineSnapshot, while
// rfqissuance declares its own ReadyRFQSnapshot. Without this shim one module
// would have to import the other's types, which ADR 0002 forbids.
type ReadyRFQSourceAdapter struct {
	source readyRFQSource
}

// NewReadyRFQSourceAdapter wraps an rfqs.Service (or any equivalent source).
func NewReadyRFQSourceAdapter(source readyRFQSource) *ReadyRFQSourceAdapter {
	return &ReadyRFQSourceAdapter{source: source}
}

// GetReadyRFQSnapshot returns the allowlisted M7 projection.
//
// The target type has no InternalNotes field and no price field of any kind, so
// a contractor-only note or cost has nowhere to land. Neither this method nor
// the type it returns can leak one.
//
// An error propagates unchanged rather than collapsing to found = false:
// issuance must fail loudly when the source state is unknown, not silently
// behave as though the RFQ were not ready.
func (a *ReadyRFQSourceAdapter) GetReadyRFQSnapshot(ctx context.Context,
	companyID, rfqChainID string) (rfqissuance.ReadyRFQSnapshot, bool, error) {

	rfqNumber, projectID, revision, title, deliveryAddress, requiredBy, responseDeadline,
		supplierInstructions, lines, found, err :=
		a.source.GetReadyRFQSnapshot(ctx, companyID, rfqChainID)
	if err != nil {
		return rfqissuance.ReadyRFQSnapshot{}, false, err
	}
	if !found {
		return rfqissuance.ReadyRFQSnapshot{}, false, nil
	}

	converted := make([]rfqissuance.ReadyRFQLineSnapshot, 0, len(lines))
	for _, l := range lines {
		converted = append(converted, rfqissuance.ReadyRFQLineSnapshot{
			SourceRFQLineID:             l.LineID,
			SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID:                  l.MaterialID,
			MaterialName:                l.MaterialName,
			Specification:               l.Specification,
			QuantityValue:               l.QuantityValue,
			QuantityUnit:                l.QuantityUnit,
			RequiredByDate:              l.RequiredByDate,
			ProcurementNotes:            l.ProcurementNotes,
			SortOrder:                   l.SortOrder,
		})
	}

	return rfqissuance.ReadyRFQSnapshot{
		RFQNumber:            rfqNumber,
		ProjectID:            projectID,
		SourceM7RFQRevision:  revision,
		Title:                title,
		DeliveryAddress:      deliveryAddress,
		RequiredByDate:       requiredBy,
		ResponseDeadline:     responseDeadline,
		SupplierInstructions: supplierInstructions,
		Lines:                converted,
	}, true, nil
}

// --- rfqissuance -> rfqs.IssuanceStatusSource ---

// issuanceStatusSource is the narrow slice of rfqissuance.Service this adapter
// needs. The method is named differently from rfqs.IssuanceStatusSource's
// deliberately: the two sides describe the same fact in their own vocabulary,
// and this adapter is what joins them.
type issuanceStatusSource interface {
	RFQChainHasIssuedVersion(ctx context.Context, companyID, rfqChainID string) (bool, error)
}

// IssuanceStatusAdapter satisfies rfqs.IssuanceStatusSource from M8's issuance
// records, replacing M7's rfqs.NoExternalIssuanceSource once M8 is wired.
//
// This is what makes the M7 reopen rule real: once a chain has an issued
// version, a Supplier may already be quoting against it, so the contractor may
// no longer reopen the M7 draft underneath them.
type IssuanceStatusAdapter struct {
	source issuanceStatusSource
}

// NewIssuanceStatusAdapter wraps an rfqissuance.Service.
func NewIssuanceStatusAdapter(source issuanceStatusSource) *IssuanceStatusAdapter {
	return &IssuanceStatusAdapter{source: source}
}

// RFQChainIssued reports whether the chain has at least one issued version.
//
// The error is returned rather than swallowed. rfqs maps any error here to
// ErrIssuanceStatusUnavailable and refuses to reopen — the fail-closed
// behaviour of design spec §2.1, which depends entirely on this method not
// reporting a lookup failure as "not issued".
func (a *IssuanceStatusAdapter) RFQChainIssued(ctx context.Context,
	companyID, rfqChainID string) (bool, error) {

	issued, err := a.source.RFQChainHasIssuedVersion(ctx, companyID, rfqChainID)
	if err != nil {
		return false, err
	}
	return issued, nil
}

// --- suppliers -> rfqissuance.SupplierLookup ---

// supplierSource is the narrow slice of suppliers.Service this adapter needs.
type supplierSource interface {
	GetSupplier(ctx context.Context, companyID, supplierID string) (suppliers.Supplier, error)
}

// SupplierInvitabilityAdapter collapses a Supplier Directory entry into the ONE
// boolean rfqissuance consumes (design spec §5.1).
//
// Two reasons it returns a boolean rather than the Supplier:
//
//   - ADR 0002: rfqissuance must not name a suppliers type, and it has no
//     legitimate use for any Supplier field beyond "may I invite this one".
//   - Non-disclosure: absent, another company's, and archived all collapse to
//     false, so a caller enumerating supplier IDs cannot learn which applies.
type SupplierInvitabilityAdapter struct {
	suppliers supplierSource
}

// NewSupplierInvitabilityAdapter wraps a suppliers.Service.
func NewSupplierInvitabilityAdapter(source supplierSource) *SupplierInvitabilityAdapter {
	return &SupplierInvitabilityAdapter{suppliers: source}
}

// SupplierIsInvitable reports whether this company may invite this Supplier.
//
// A not-found result is `false`, not an error — it is one of the three ordinary
// refusals. Any OTHER error propagates: treating an infrastructure failure as
// "not invitable" would refuse a valid Supplier whenever the database hiccupped,
// and the contractor could not tell that apart from a genuine refusal.
func (a *SupplierInvitabilityAdapter) SupplierIsInvitable(ctx context.Context,
	companyID, supplierID string) (bool, error) {

	supplier, err := a.suppliers.GetSupplier(ctx, companyID, supplierID)
	if errors.Is(err, suppliers.ErrSupplierNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// GetSupplier is already tenant-scoped, so a foreign Supplier surfaces as
	// not-found above. Active is the remaining gate: a retired Supplier keeps
	// its history but must not receive new RFQs.
	return supplier.Active, nil
}

// --- rfqissuance -> supplieraccess invitation capabilities ---

// invitationAccessSource is the exact primitive/projection surface exposed by
// rfqissuance.Service. The adapter exists because each domain owns its return
// type and neither is permitted to import the other.
type invitationAccessSource interface {
	ResolveInvitationAccessByHash(ctx context.Context, accessSecretHash string,
		accessedAt time.Time) (rfqissuance.InvitationAccessProjection, bool, error)
	ResolveInvitationAccessByIdentity(ctx context.Context, companyID, invitationID string,
		accessedAt time.Time) (rfqissuance.InvitationAccessProjection, bool, error)
	RecordInvitationViewed(ctx context.Context, companyID, invitationID string,
		accessGeneration int64, viewedAt time.Time) error
}

// ValidateInvitationAccess re-reads the current owner-side invitation after an
// exchange or session binding supplies its captured identity.
func (a *InvitationAccessAdapter) ValidateInvitationAccess(ctx context.Context,
	companyID, invitationID string, accessedAt time.Time) (
	supplieraccess.InvitationAccessSnapshot, bool, error) {

	resolved, found, err := a.source.ResolveInvitationAccessByIdentity(
		ctx, companyID, invitationID, accessedAt)
	if err != nil {
		return supplieraccess.InvitationAccessSnapshot{}, false, err
	}
	if !found {
		return supplieraccess.InvitationAccessSnapshot{}, false, nil
	}
	return supplieraccess.InvitationAccessSnapshot{
		CompanyID:                 resolved.CompanyID,
		SupplierID:                resolved.SupplierID,
		InvitationID:              resolved.InvitationID,
		NormalizedRecipientEmail:  resolved.NormalizedRecipientEmail,
		AccessGeneration:          resolved.AccessGeneration,
		CurrentIssuedRFQVersionID: resolved.CurrentIssuedRFQVersionID,
	}, true, nil
}

// InvitationAccessAdapter maps rfqissuance's owner-side projection into the
// consumer-owned supplieraccess capability without exposing a repository.
type InvitationAccessAdapter struct {
	source invitationAccessSource
}

// NewInvitationAccessAdapter wraps rfqissuance.Service.
func NewInvitationAccessAdapter(source invitationAccessSource) *InvitationAccessAdapter {
	return &InvitationAccessAdapter{source: source}
}

// ResolveInvitationAccess preserves the neutral found result and propagates
// infrastructure errors so supplieraccess can keep invalid credentials distinct
// from bounded operational failures.
func (a *InvitationAccessAdapter) ResolveInvitationAccess(ctx context.Context,
	accessSecretHash string, accessedAt time.Time) (
	supplieraccess.InvitationAccessSnapshot, bool, error) {

	resolved, found, err := a.source.ResolveInvitationAccessByHash(
		ctx, accessSecretHash, accessedAt)
	if err != nil {
		return supplieraccess.InvitationAccessSnapshot{}, false, err
	}
	if !found {
		return supplieraccess.InvitationAccessSnapshot{}, false, nil
	}
	return supplieraccess.InvitationAccessSnapshot{
		CompanyID:                 resolved.CompanyID,
		SupplierID:                resolved.SupplierID,
		InvitationID:              resolved.InvitationID,
		NormalizedRecipientEmail:  resolved.NormalizedRecipientEmail,
		AccessGeneration:          resolved.AccessGeneration,
		CurrentIssuedRFQVersionID: resolved.CurrentIssuedRFQVersionID,
	}, true, nil
}

// RecordInvitationViewed forwards only primitive identity and generation data.
func (a *InvitationAccessAdapter) RecordInvitationViewed(ctx context.Context,
	companyID, invitationID string, accessGeneration int64, viewedAt time.Time) error {

	err := a.source.RecordInvitationViewed(
		ctx, companyID, invitationID, accessGeneration, viewedAt)
	if errors.Is(err, rfqissuance.ErrInvitationNotFound) {
		// The invitation existed during resolution but no longer matches the
		// validated generation. supplieraccess owns the public meaning of that
		// race and collapses it with every other invalid credential state.
		return supplieraccess.ErrInvitationAccessInvalid
	}
	return err
}
