package rfqissuance

import "time"

// RFQAmendmentDraft is the ONE mutable document in this module
// (design spec §3.3, §4.2).
//
// Everything else here is immutable by construction. The draft exists precisely
// so that preparing the next version never touches the current one: a Supplier
// invited to Version N keeps reading exactly Version N while the contractor
// edits the draft that will become N+1.
//
// It CLONES the base version rather than referencing it. A reference would make
// the draft's content shift underneath the editor if the base ever changed —
// and would make "what will Version 2 say?" unanswerable until issuance.
type RFQAmendmentDraft struct {
	ID         string
	CompanyID  string
	RFQChainID string

	// BaseIssuedVersionID and BaseVersionNumber record what this draft was
	// cloned FROM. Issuance re-checks that the base is still the chain's
	// current version: if another amendment landed meanwhile, this draft was
	// built on a superseded base and its edits may contradict it (§4.2 step 2).
	BaseIssuedVersionID string
	BaseVersionNumber   int

	// Currency is cloned and not editable. The contractor sets one immutable
	// currency at first issuance; changing it mid-chain would invalidate every
	// offer already submitted against the earlier version.
	Currency string

	Title                string
	DeliveryAddress      string
	RequiredByDate       *time.Time
	ResponseDeadline     *time.Time
	SupplierInstructions string

	Lines []IssuedRFQLine

	Revision        int64
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

// RFQAmendmentDraftSchemaVersion is the current persisted shape.
const RFQAmendmentDraftSchemaVersion = 1

// AmendmentDraftPatch is a sparse edit. A nil field means "leave unchanged",
// which is what lets a deadline-only extension avoid restating the whole draft.
//
// Currency is deliberately absent: it is immutable for the life of the chain.
type AmendmentDraftPatch struct {
	Title                *string
	DeliveryAddress      *string
	RequiredByDate       **time.Time
	ResponseDeadline     *time.Time
	SupplierInstructions *string
	Lines                *[]AmendmentDraftLinePatch
}

// AmendmentDraftLinePatch is one line in a complete draft-line replacement.
//
// ID is empty for a new M8-native line. For an existing line it is only a
// lookup key: callers never submit LineageID or M7 provenance, because those
// identities are owned by the server and copied from the current draft.
type AmendmentDraftLinePatch struct {
	ID               string
	MaterialID       string
	MaterialName     string
	Specification    string
	QuantityValue    string
	QuantityUnit     string
	RequiredByDate   *time.Time
	ProcurementNotes string
	SortOrder        int
}

func replaceAmendmentLines(existing []IssuedRFQLine,
	patches []AmendmentDraftLinePatch) ([]IssuedRFQLine, error) {

	byID := make(map[string]IssuedRFQLine, len(existing))
	for _, line := range existing {
		byID[line.ID] = line
	}

	seen := make(map[string]struct{}, len(patches))
	replaced := make([]IssuedRFQLine, 0, len(patches))
	for _, patch := range patches {
		if patch.ID == "" {
			line, err := NewM8NativeLine(M8NativeLineInput{
				MaterialID: patch.MaterialID, MaterialName: patch.MaterialName,
				Specification: patch.Specification,
				QuantityValue: patch.QuantityValue, QuantityUnit: patch.QuantityUnit,
				RequiredByDate:   patch.RequiredByDate,
				ProcurementNotes: patch.ProcurementNotes, SortOrder: patch.SortOrder,
			})
			if err != nil {
				return nil, err
			}
			replaced = append(replaced, line)
			continue
		}

		line, found := byID[patch.ID]
		if !found {
			return nil, ErrAmendmentLineNotFound
		}
		if _, duplicate := seen[patch.ID]; duplicate {
			return nil, ErrDuplicateAmendmentLine
		}
		seen[patch.ID] = struct{}{}

		qty, err := newRFQLineQuantity(patch.QuantityValue, patch.QuantityUnit)
		if err != nil {
			return nil, err
		}
		// Only supplier-visible content is replaced. ID, LineageID and both M7
		// source identifiers remain exactly as stored on the current draft.
		line.MaterialID = patch.MaterialID
		line.MaterialName = patch.MaterialName
		line.Specification = patch.Specification
		line.Quantity = qty
		line.RequiredByDate = patch.RequiredByDate
		line.ProcurementNotes = patch.ProcurementNotes
		line.SortOrder = patch.SortOrder
		replaced = append(replaced, line)
	}
	return replaced, nil
}

// applyTo returns draft with the patch's present fields applied.
func (p AmendmentDraftPatch) applyTo(draft RFQAmendmentDraft) (RFQAmendmentDraft, error) {
	if p.Title != nil {
		draft.Title = *p.Title
	}
	if p.DeliveryAddress != nil {
		draft.DeliveryAddress = *p.DeliveryAddress
	}
	if p.RequiredByDate != nil {
		draft.RequiredByDate = *p.RequiredByDate
	}
	if p.ResponseDeadline != nil {
		deadline := *p.ResponseDeadline
		draft.ResponseDeadline = &deadline
	}
	if p.SupplierInstructions != nil {
		draft.SupplierInstructions = *p.SupplierInstructions
	}
	if p.Lines != nil {
		lines, err := replaceAmendmentLines(draft.Lines, *p.Lines)
		if err != nil {
			return RFQAmendmentDraft{}, err
		}
		draft.Lines = lines
	}
	draft.UpdatedAt = time.Now()
	return draft, nil
}

// newAmendmentDraftFrom clones an immutable version into an editable draft.
//
// Lines are cloned through CloneIssuedLine, so each takes a NEW per-version ID
// while keeping its lineage and M7 provenance (§3.2A). Without the new IDs,
// V2's line records would be indistinguishable from V1's, and an offer could
// not say which version's line it quoted against.
func newAmendmentDraftFrom(version IssuedRFQVersion, actorUserID string) RFQAmendmentDraft {
	lines := make([]IssuedRFQLine, 0, len(version.Lines))
	for _, l := range version.Lines {
		lines = append(lines, CloneIssuedLine(l))
	}

	deadline := version.ResponseDeadline
	now := time.Now()

	return RFQAmendmentDraft{
		CompanyID:            version.CompanyID,
		RFQChainID:           version.RFQChainID,
		BaseIssuedVersionID:  version.ID,
		BaseVersionNumber:    version.VersionNumber,
		Currency:             version.Currency,
		Title:                version.Title,
		DeliveryAddress:      version.DeliveryAddress,
		RequiredByDate:       version.RequiredByDate,
		ResponseDeadline:     &deadline,
		SupplierInstructions: version.SupplierInstructions,
		Lines:                lines,
		CreatedByUserID:      actorUserID,
		CreatedAt:            now,
		UpdatedAt:            now,
		SchemaVersion:        RFQAmendmentDraftSchemaVersion,
	}
}
