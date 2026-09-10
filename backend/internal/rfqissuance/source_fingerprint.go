package rfqissuance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// fingerprintReadyRFQSnapshot identifies the exact allowlisted M7 source that
// became an immutable issued version.
//
// The canonical structs are deliberately declared here instead of hashing a
// persisted document or an API DTO. Persisted and transport shapes may gain
// contractor-facing metadata over time; allowing that metadata into this hash
// would make the same supplier-visible source acquire a different identity.
// Conversely, every supplier-visible field is present below, so changing the
// commercial request necessarily changes the fingerprint.
func fingerprintReadyRFQSnapshot(snapshot ReadyRFQSnapshot) string {
	type canonicalLine struct {
		SourceRFQLineID             string
		SourceMaterialRequirementID string
		MaterialID                  string
		MaterialName                string
		Specification               string
		QuantityValue               string
		QuantityUnit                string
		RequiredByDate              string
		ProcurementNotes            string
		SortOrder                   int
	}
	type canonicalSnapshot struct {
		RFQNumber            string
		ProjectID            string
		SourceM7RFQRevision  int64
		Title                string
		DeliveryAddress      string
		RequiredByDate       string
		ResponseDeadline     string
		SupplierInstructions string
		Lines                []canonicalLine
	}

	lines := make([]canonicalLine, 0, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lines = append(lines, canonicalLine{
			SourceRFQLineID:             line.SourceRFQLineID,
			SourceMaterialRequirementID: line.SourceMaterialRequirementID,
			MaterialID:                  line.MaterialID,
			MaterialName:                line.MaterialName,
			Specification:               line.Specification,
			QuantityValue:               line.QuantityValue,
			QuantityUnit:                line.QuantityUnit,
			RequiredByDate:              canonicalTime(line.RequiredByDate),
			ProcurementNotes:            line.ProcurementNotes,
			SortOrder:                   line.SortOrder,
		})
	}

	payload, err := json.Marshal(canonicalSnapshot{
		RFQNumber:            snapshot.RFQNumber,
		ProjectID:            snapshot.ProjectID,
		SourceM7RFQRevision:  snapshot.SourceM7RFQRevision,
		Title:                snapshot.Title,
		DeliveryAddress:      snapshot.DeliveryAddress,
		RequiredByDate:       canonicalTime(snapshot.RequiredByDate),
		ResponseDeadline:     canonicalTime(snapshot.ResponseDeadline),
		SupplierInstructions: snapshot.SupplierInstructions,
		Lines:                lines,
	})
	if err != nil {
		// The canonical shape contains only JSON-infallible primitives. Keep
		// this guard anyway: silently hashing an empty payload would collapse
		// every issuance onto the same identity if that assumption ever changes.
		panic("rfqissuance: canonical source fingerprint encoding failed: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// fingerprintAmendmentDraft binds Version N+1 to the exact mutable draft
// revision that produced it. Draft ID, base identity, revision, line identities,
// and every commercial field participate; timestamps and editor identity do
// not, because they are metadata rather than Supplier-visible content.
func fingerprintAmendmentDraft(draft RFQAmendmentDraft) string {
	type canonicalLine struct {
		ID                          string
		LineageID                   string
		SourceM7RFQLineID           string
		SourceMaterialRequirementID string
		MaterialID                  string
		MaterialName                string
		Specification               string
		QuantityValue               string
		QuantityUnit                string
		RequiredByDate              string
		ProcurementNotes            string
		SortOrder                   int
	}
	type canonicalDraft struct {
		ID                   string
		RFQChainID           string
		BaseIssuedVersionID  string
		BaseVersionNumber    int
		Revision             int64
		Currency             string
		Title                string
		DeliveryAddress      string
		RequiredByDate       string
		ResponseDeadline     string
		SupplierInstructions string
		Lines                []canonicalLine
	}

	lines := make([]canonicalLine, 0, len(draft.Lines))
	for _, line := range draft.Lines {
		lines = append(lines, canonicalLine{
			ID:                          line.ID,
			LineageID:                   line.LineageID,
			SourceM7RFQLineID:           pointerString(line.SourceM7RFQLineID),
			SourceMaterialRequirementID: pointerString(line.SourceMaterialRequirementID),
			MaterialID:                  line.MaterialID,
			MaterialName:                line.MaterialName,
			Specification:               line.Specification,
			QuantityValue:               line.Quantity.Value.String(),
			QuantityUnit:                line.Quantity.Unit,
			RequiredByDate:              canonicalTime(line.RequiredByDate),
			ProcurementNotes:            line.ProcurementNotes,
			SortOrder:                   line.SortOrder,
		})
	}

	payload, err := json.Marshal(canonicalDraft{
		ID:                   draft.ID,
		RFQChainID:           draft.RFQChainID,
		BaseIssuedVersionID:  draft.BaseIssuedVersionID,
		BaseVersionNumber:    draft.BaseVersionNumber,
		Revision:             draft.Revision,
		Currency:             draft.Currency,
		Title:                draft.Title,
		DeliveryAddress:      draft.DeliveryAddress,
		RequiredByDate:       canonicalTime(draft.RequiredByDate),
		ResponseDeadline:     canonicalTime(draft.ResponseDeadline),
		SupplierInstructions: draft.SupplierInstructions,
		Lines:                lines,
	})
	if err != nil {
		panic("rfqissuance: canonical amendment fingerprint encoding failed: " + err.Error())
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// canonicalTime represents instants identically regardless of their original
// location object. Empty optional dates remain distinct from the zero instant.
func canonicalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
