package rfqs

import (
	"context"
	"time"
)

// The capabilities rfqs CONSUMES (design spec §1.3).
//
// Every method takes primitives and returns primitives or rfqs-owned types, so
// the providers satisfy these interfaces STRUCTURALLY without either side
// importing the other. rfqs imports no materialrequirements type at all
// (ADR 0002); the composition root supplies a single adapter (§1.4.1).

// ProjectLookup confirms a Project belongs to the authenticated company.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// ClaimSnapshot is rfqs-owned: the allowlisted projection rfqs snapshots into a
// line. It carries no cost, no margin and no InternalNotes (design spec §1.3).
type ClaimSnapshot struct {
	RequirementID    string
	Revision         int64
	MaterialID       string
	MaterialName     string
	Specification    string
	QuantityValue    string // canonical decimal string; never a float
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string
}

// RFQRequirementClaim is rfqs-owned: one requirement's claim on this chain.
//
// Revision is carried so a release can be issued under its guard without a
// second read per requirement.
type RFQRequirementClaim struct {
	RequirementID string
	RFQChainID    string
	RFQNumber     string
	LineID        string
	Revision      int64
}

// MaterialRequirementSource is the ONLY way rfqs reaches a requirement.
//
// materialrequirements owns the claim FIELDS and these conditional writes;
// rfqs owns all ORCHESTRATION. That split is why retry_line lives here and not
// there: only rfqs can read its own lines (design spec §7.1).
type MaterialRequirementSource interface {
	// ClaimForRFQ performs the Revision-guarded conditional claim and returns
	// the snapshot taken from the SAME document the claim validated
	// (design spec §7.2 steps 3-4).
	//
	// projectID is the RFQ's own ProjectID and is enforced INSIDE the atomic
	// claim filter. Without it, a reviewed requirement belonging to a different
	// Project of the same company could be pulled into this RFQ.
	ClaimForRFQ(ctx context.Context, companyID, projectID, requirementID string,
		expectedRevision int64, rfqChainID, rfqNumber, lineID string) (ClaimSnapshot, error)

	// ReleaseClaim clears a claim, conditional on companyID + requirementID +
	// the exact chain ID + the exact line ID + expectedRevision.
	ReleaseClaim(ctx context.Context, companyID, requirementID string, expectedRevision int64,
		rfqChainID, lineID string) error

	// ReadClaim reports one requirement's claim state by requirement ID.
	ReadClaim(ctx context.Context, companyID, requirementID string) (
		rfqChainID string, rfqNumber string, lineID string, revision int64,
		found bool, err error)

	// ListClaimsForRFQChain enumerates every requirement currently claiming
	// this chain WITHOUT the caller needing requirement IDs in advance. This is
	// what makes an orphaned claim discoverable: ReadClaim can only confirm a
	// claim the caller already suspects (design spec §7.5).
	ListClaimsForRFQChain(ctx context.Context, companyID, rfqChainID string) (
		[]RFQRequirementClaim, error)

	// ClaimedRequirementIsReadyForRFQ validates an ALREADY-CLAIMED requirement
	// at mark-ready time. It deliberately does NOT apply the unclaimed §8.5
	// predicate, whose activeRfqChainId == nil term is false for every
	// requirement already on a line — applying it here would make every
	// non-empty RFQ impossible to mark ready (design spec §6.4, §8.5).
	ClaimedRequirementIsReadyForRFQ(ctx context.Context, companyID, requirementID,
		rfqChainID, lineID string) (bool, error)

	// ReadClaimSnapshot returns the allowlisted projection for a requirement
	// THIS chain already holds under THIS line, so an interrupted claim-first
	// add can be repaired (§7.3) and retry_line can rebuild a missing line
	// (§7.5).
	//
	// It is scoped to the EXACT existing claim. The underlying read requires
	// all of companyId, requirementId, activeRfqChainId == rfqChainID and
	// activeRfqLineId == lineID, so rfqs can never obtain a snapshot from an
	// unclaimed requirement, one claimed by another chain, one claimed under
	// another line, or another tenant. Repair is the only purpose; this is not
	// a general requirement read.
	//
	// It deliberately does NOT re-run RFQ eligibility. The claim was eligible
	// when it was created, and while it exists every supplier-visible
	// contractor field is frozen (§2.3) — only detection state may move (§5.8).
	// Requiring current eligibility would make an interrupted line impossible
	// to repair after an ordinary source change, even though the snapshot it
	// would rebuild is still exactly the one the claim validated.
	//
	// The ordinary add path never needs this: ClaimForRFQ already returns the
	// snapshot. A repair cannot re-claim an already-claimed requirement, so
	// without this capability the only alternatives would be fabricating
	// supplier-visible content or leaving the orphan permanently unrepairable.
	ReadClaimSnapshot(ctx context.Context, companyID, requirementID,
		rfqChainID, lineID string) (ClaimSnapshot, error)
}

// AuditRecorder is the consumer-owned audit contract for this module. There is
// no shared cross-module audit interface (design spec §1.5).
// The signatures mirror the A4 audit contract exactly, so audit.Service
// satisfies this STRUCTURALLY with no adapter — every parameter is a primitive
// (design spec §1.5).
type AuditRecorder interface {
	RecordRFQCreated(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber string) error
	RecordRFQUpdated(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber string) error
	RecordRFQLineAdded(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber, requirementID, lineID string) error
	RecordRFQLineRemoved(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber, requirementID, lineID string) error
	RecordRFQMarkedReady(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber string, lineCount int) error
	RecordRFQReopened(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber string) error
	RecordRFQDeleted(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, rfqNumber string) error
	RecordRFQClaimReconciled(ctx context.Context, companyID, projectID, actorUserID,
		rfqChainID, requirementID, action string) error
}

// IssuanceStatusSource is the M8 seam (design spec §1.3, §6.4).
//
// Keyed on rfqChainID, never RFQNumber: the number is a tenant-scoped display
// identifier, while the stable aggregate ID is the cross-module identity M8
// uses.
type IssuanceStatusSource interface {
	RFQChainIssued(ctx context.Context, companyID, rfqChainID string) (bool, error)
}

// NoExternalIssuanceSource is what M7 wires: nothing is ever issued, so reopen
// always succeeds. M8 swaps in the real adapter with NO change to service
// logic (design spec §6.4).
type NoExternalIssuanceSource struct{}

// RFQChainIssued always reports false — M7 issues nothing.
func (NoExternalIssuanceSource) RFQChainIssued(context.Context, string, string) (bool, error) {
	return false, nil
}

// RFQLineSnapshot is one of the three read-only capabilities M8 consumes
// (design spec §9; M8 design spec §2.1A).
//
// InternalNotes is structurally absent, so it cannot leak to a supplier-facing
// consumer even by mistake — allowlist-by-signature, the same discipline as
// audit.AuditRecorder. There is no price field, so an indicative offering price
// has no destination.
//
// SourceMaterialRequirementID and MaterialID are IDENTIFIERS, not commercial
// data. M8 persists them on every immutable issued line: copy-forward matches
// compatible lines by SourceMaterialRequirementID rather than Material ID alone
// (M8 approved decision 11), and award traceability references the originating
// requirement. Without them here, M8 would have to weaken that matching rule or
// reach into materialrequirements directly, which ADR 0002 forbids.
type RFQLineSnapshot struct {
	LineID                      string
	SourceMaterialRequirementID string
	MaterialID                  string
	MaterialName                string
	Specification               string
	QuantityValue               string // canonical decimal string
	QuantityUnit                string
	RequiredByDate              *time.Time
	ProcurementNotes            string
	SortOrder                   int
}
