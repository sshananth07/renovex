package rfqissuance

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// The immutable issued RFQ version and its lines (design spec §3.2, §3.2A).

// IssuedRFQLine is one line inside one immutable issued version.
//
// Four identifiers with deliberately distinct meanings (§3.2A):
//
//	ID                          the exact record inside THIS version
//	LineageID                   stable logical identity ACROSS versions
//	SourceM7RFQLineID           originating M7 line, when applicable
//	SourceMaterialRequirementID originating Material Requirement, when applicable
//
// The M7 line ID is never reused as ID. Reusing it would conflate V1's and V2's
// records for one logical line, and a Supplier Offer must reference the exact
// line in the exact version it quoted against.
//
// The two Source fields are POINTERS because an M8-native amendment line has no
// M7 provenance at all. Making them plain strings would force an empty string to
// stand in for "no origin", which is a different claim.
type IssuedRFQLine struct {
	ID                          string
	LineageID                   string
	SourceM7RFQLineID           *string
	SourceMaterialRequirementID *string

	MaterialID       string
	MaterialName     string
	Specification    string
	Quantity         quantity.Quantity
	RequiredByDate   *time.Time
	ProcurementNotes string
	SortOrder        int
}

// newIdentity mints a stable opaque identifier, matching the ObjectID-hex
// convention the rest of the codebase uses for pre-generated IDs.
func newIdentity() string { return bson.NewObjectID().Hex() }

func newRFQLineQuantity(value, unit string) (quantity.Quantity, error) {
	qty, err := quantity.New(value, unit)
	if err != nil || qty.Value.Cmp(decimal.Zero) <= 0 ||
		strings.TrimSpace(unit) == "" {
		return quantity.Quantity{}, ErrInvalidQuantity
	}
	return qty, nil
}

// NewIssuedLineFromM7 builds a Version 1 line from the M7 ready snapshot.
//
// Both identities are freshly minted; the M7 identifiers are recorded as
// provenance only (§3.2A).
func NewIssuedLineFromM7(snapshot ReadyRFQLineSnapshot) (IssuedRFQLine, error) {
	qty, err := newRFQLineQuantity(snapshot.QuantityValue, snapshot.QuantityUnit)
	if err != nil {
		return IssuedRFQLine{}, err
	}

	sourceLineID := snapshot.SourceRFQLineID
	sourceRequirementID := snapshot.SourceMaterialRequirementID

	return IssuedRFQLine{
		ID:                          newIdentity(),
		LineageID:                   newIdentity(),
		SourceM7RFQLineID:           &sourceLineID,
		SourceMaterialRequirementID: &sourceRequirementID,
		MaterialID:                  snapshot.MaterialID,
		MaterialName:                snapshot.MaterialName,
		Specification:               snapshot.Specification,
		Quantity:                    qty,
		RequiredByDate:              snapshot.RequiredByDate,
		ProcurementNotes:            snapshot.ProcurementNotes,
		SortOrder:                   snapshot.SortOrder,
	}, nil
}

// M8NativeLineInput carries a line added directly in an amendment draft, with
// no M7 Material Requirement behind it.
type M8NativeLineInput struct {
	MaterialID       string
	MaterialName     string
	Specification    string
	QuantityValue    string
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string
	SortOrder        int
}

// NewM8NativeLine builds an amendment line with no M7 provenance.
//
// MaterialID is required: without a Material Requirement behind it, the Material
// is the only thing tying the line to the catalogue, and a line quoting against
// nothing identifiable would be unresolvable at award time.
func NewM8NativeLine(input M8NativeLineInput) (IssuedRFQLine, error) {
	if strings.TrimSpace(input.MaterialID) == "" {
		return IssuedRFQLine{}, ErrMaterialIDRequired
	}
	qty, err := newRFQLineQuantity(input.QuantityValue, input.QuantityUnit)
	if err != nil {
		return IssuedRFQLine{}, err
	}

	return IssuedRFQLine{
		ID:        newIdentity(),
		LineageID: newIdentity(),
		// Explicitly nil: this line originates in M8, not in an M7 requirement.
		SourceM7RFQLineID:           nil,
		SourceMaterialRequirementID: nil,
		MaterialID:                  input.MaterialID,
		MaterialName:                input.MaterialName,
		Specification:               input.Specification,
		Quantity:                    qty,
		RequiredByDate:              input.RequiredByDate,
		ProcurementNotes:            input.ProcurementNotes,
		SortOrder:                   input.SortOrder,
	}, nil
}

// CloneIssuedLine copies a line forward into the next immutable version.
//
// It takes a NEW per-version ID while preserving lineage and provenance, so the
// line stays traceable without V2's record being mistakable for V1's.
func CloneIssuedLine(line IssuedRFQLine) IssuedRFQLine {
	clone := line
	clone.ID = newIdentity()

	// Copy the pointed-to values so the clone cannot alias the original's
	// provenance and be mutated through it.
	if line.SourceM7RFQLineID != nil {
		v := *line.SourceM7RFQLineID
		clone.SourceM7RFQLineID = &v
	}
	if line.SourceMaterialRequirementID != nil {
		v := *line.SourceMaterialRequirementID
		clone.SourceMaterialRequirementID = &v
	}
	if line.RequiredByDate != nil {
		v := *line.RequiredByDate
		clone.RequiredByDate = &v
	}
	return clone
}

// LinesAreCompatible reports whether current may inherit previous's
// Supplier-entered commercial fields during copy-forward (§3.2A).
//
// M7-backed lines match by Material Requirement, NEVER by Material ID alone:
// two lines for the same Material under different requirements are different
// commercial lines, and cross-matching them would carry a price onto the wrong
// one (approved decision 11).
//
// An M8-native line has no requirement to match on, so lineage is the rule.
// Provenance kinds never match across each other.
func LinesAreCompatible(previous, current IssuedRFQLine) bool {
	previousBacked := previous.SourceMaterialRequirementID != nil
	currentBacked := current.SourceMaterialRequirementID != nil

	if previousBacked != currentBacked {
		return false
	}
	if previousBacked {
		return *previous.SourceMaterialRequirementID == *current.SourceMaterialRequirementID
	}
	return previous.LineageID != "" && previous.LineageID == current.LineageID
}

// IssuedRFQVersion is one immutable issued version.
//
// It is never updated in place. A correction — including a deadline-only
// extension — creates the NEXT version, which is what lets a Supplier keep
// reading the exact version they were invited to.
type IssuedRFQVersion struct {
	ID            string
	CompanyID     string
	ProjectID     string
	RFQChainID    string
	RFQNumber     string
	VersionNumber int
	Currency      string

	Title                string
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     time.Time
	SupplierInstructions string

	Lines []IssuedRFQLine

	SourceM7RFQRevision int64
	SourceFingerprint   string

	// IssuanceOperationID is the caller-supplied idempotency key. It is unique
	// per company, so an interrupted caller that re-presents the same ID
	// recovers the version its first attempt created instead of issuing a
	// second one (design spec §10.1).
	IssuanceOperationID string

	IssuedByUserID string
	IssuedAt       time.Time
	SchemaVersion  int
}

// NewIssuedVersionInput carries everything one immutable version records.
//
// ResponseDeadline is a POINTER here and a concrete time.Time on the version
// itself: the M7 model permits a ready RFQ without a deadline, and this
// constructor is the boundary that refuses to issue one (§4.1A).
type NewIssuedVersionInput struct {
	CompanyID     string
	ProjectID     string
	RFQChainID    string
	RFQNumber     string
	VersionNumber int
	Currency      string

	Title                string
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     *time.Time
	SupplierInstructions string

	Lines []IssuedRFQLine

	SourceM7RFQRevision int64
	SourceFingerprint   string
	IssuanceOperationID string
	IssuedByUserID      string
	IssuedAt            time.Time
}

// IssuedRFQVersionSchemaVersion is the current persisted shape.
const IssuedRFQVersionSchemaVersion = 1

// NewIssuedVersion validates and builds an immutable version.
//
// A missing response deadline is refused rather than defaulted: the deadline is
// the commercial window a Supplier responds within, and the platform has no
// basis to invent one (§4.1A).
func NewIssuedVersion(input NewIssuedVersionInput) (IssuedRFQVersion, error) {
	if input.ResponseDeadline == nil {
		return IssuedRFQVersion{}, ErrResponseDeadlineRequired
	}

	issuedAt := input.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	if err := procurementlimits.ValidateCount(len(input.Lines),
		procurementlimits.MaxLines); err != nil {
		return IssuedRFQVersion{}, ErrInputLimitExceeded
	}
	if _, err := procurementlimits.TrimText(input.Title,
		procurementlimits.MaxDisplayTextRunes); err != nil {
		return IssuedRFQVersion{}, ErrInputLimitExceeded
	}
	if _, err := procurementlimits.TrimText(input.DeliveryAddress,
		procurementlimits.MaxNoteRunes); err != nil {
		return IssuedRFQVersion{}, ErrInputLimitExceeded
	}
	if _, err := procurementlimits.TrimText(input.SupplierInstructions,
		procurementlimits.MaxLongTextRunes); err != nil {
		return IssuedRFQVersion{}, ErrInputLimitExceeded
	}
	if err := procurementlimits.ValidateResponseDeadline(
		issuedAt, *input.ResponseDeadline); err != nil {
		return IssuedRFQVersion{}, ErrInvalidBusinessDate
	}
	if err := procurementlimits.ValidateRequiredByDate(
		*input.ResponseDeadline, input.RequiredByDate); err != nil {
		return IssuedRFQVersion{}, ErrInvalidBusinessDate
	}
	for _, line := range input.Lines {
		if _, err := procurementlimits.TrimText(line.MaterialName,
			procurementlimits.MaxDisplayTextRunes); err != nil {
			return IssuedRFQVersion{}, ErrInputLimitExceeded
		}
		if _, err := procurementlimits.TrimText(line.Specification,
			procurementlimits.MaxLongTextRunes); err != nil {
			return IssuedRFQVersion{}, ErrInputLimitExceeded
		}
		if _, err := procurementlimits.TrimText(line.ProcurementNotes,
			procurementlimits.MaxLongTextRunes); err != nil {
			return IssuedRFQVersion{}, ErrInputLimitExceeded
		}
		if err := procurementlimits.ValidateRequiredByDate(
			*input.ResponseDeadline, line.RequiredByDate); err != nil {
			return IssuedRFQVersion{}, ErrInvalidBusinessDate
		}
	}

	return IssuedRFQVersion{
		CompanyID:            input.CompanyID,
		ProjectID:            input.ProjectID,
		RFQChainID:           input.RFQChainID,
		RFQNumber:            input.RFQNumber,
		VersionNumber:        input.VersionNumber,
		Currency:             input.Currency,
		Title:                input.Title,
		DeliveryAddress:      input.DeliveryAddress,
		RequiredByDate:       input.RequiredByDate,
		ResponseDeadline:     *input.ResponseDeadline,
		SupplierInstructions: input.SupplierInstructions,
		Lines:                input.Lines,
		SourceM7RFQRevision:  input.SourceM7RFQRevision,
		SourceFingerprint:    input.SourceFingerprint,
		IssuanceOperationID:  input.IssuanceOperationID,
		IssuedByUserID:       input.IssuedByUserID,
		IssuedAt:             issuedAt,
		SchemaVersion:        IssuedRFQVersionSchemaVersion,
	}, nil
}
