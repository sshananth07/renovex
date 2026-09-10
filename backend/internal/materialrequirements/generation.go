package materialrequirements

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// Contextual skip reasons. These describe an EMITTED row that could not become
// an anchor because of project context. They are reported separately from the
// intrinsic counters, which describe rows internal/costs filtered out before
// this module ever saw them — the two mean different things (design spec §3.7,
// §3.8).
//
// There is deliberately no material_inactive reason: M7 has no
// inactive-Material concept (design spec §0.3 conflict D). Nor is there a
// split_anchor_exists or archived_anchor_exists reason: a terminal anchor is
// classified, never skipped (design spec §3.4).
const (
	SkipReasonWorkItemCancelled = "work_item_cancelled"
	SkipReasonWorkItemNotFound  = "work_item_not_found"
	SkipReasonMaterialNotFound  = "material_not_found"
)

// IneligibleCounts tallies rows internal/costs filtered out itself. This module
// assembles them from the visitor's five primitive returns purely for the HTTP
// response (design spec §3.8).
type IneligibleCounts struct {
	NonMaterialCategory   int
	MissingMaterialID     int
	MissingWorkItemID     int
	MissingOrZeroQuantity int
	BlankUnit             int
}

// SkippedRow is one emitted row rejected during Work Item / Material validation.
type SkippedRow struct {
	MaterialID string
	WorkItemID string
	Reason     string
}

// DiscrepancyRef identifies an anchor whose source moved. AnchorStatus is
// included because update and merge are unavailable on a terminal anchor, so the
// contractor must be able to see that the discrepancy concerns retired or
// superseded demand (design spec §3.4, §5.6).
type DiscrepancyRef struct {
	MaterialRequirementID string
	SyncState             string
	AnchorStatus          string
}

// GenerationResult reports what one generation run did. Created records are
// returned in full; existing anchors return identifiers and sync state rather
// than duplicating every unchanged aggregate (design spec §3.8).
type GenerationResult struct {
	CreatedCount       int
	UnchangedCount     int
	DiscrepancyCount   int
	SourceRemovedCount int
	SkippedCount       int

	Created       []MaterialRequirement
	Discrepancies []DiscrepancyRef
	SourceRemoved []DiscrepancyRef
	Skipped       []SkippedRow

	IntrinsicallyIneligible IneligibleCounts
}

// aggregate is one computed group: the rows contributing to a single
// {project, workItem, material, costItemUnit} key.
type aggregate struct {
	workItemID string
	materialID string
	unit       string
	rows       []SourceRow
	total      decimal.Decimal
}

// GenerateFromCostItems creates missing Material Requirement anchors and
// converges the detection state of existing ones.
//
// It processes the UNION of existing cost_item anchors and currently computed
// aggregation keys. Iterating computed keys alone could never discover
// source_removed, because a key with zero current CostItems is never computed
// (design spec §3.4).
//
// It never overwrites a contractor-controlled field. For an existing anchor it
// writes only SourceSyncState and SourceCheckedAt, leaving the accepted snapshot
// intact so a later merge can compute proposed − accepted (design spec §5.8).
func (s *Service) GenerateFromCostItems(ctx context.Context, companyID, actorUserID, projectID string) (GenerationResult, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}
	if !belongs {
		return GenerationResult{}, ErrProjectNotFound
	}

	result := GenerationResult{}

	// --- 1. Collect eligible rows, grouped by aggregation key. ---
	computed := map[string]*aggregate{}
	nonMaterial, missingMaterial, missingWorkItem, missingQty, blankUnit, err :=
		s.costSource.VisitEligibleMaterialCostItems(ctx, companyID, projectID,
			func(costItemID, workItemID, materialID string, qty decimal.Decimal, unit string) error {
				key := ComputeSourceAggregationKey(companyID, projectID,
					WorkItemRef(workItemID), materialID, unit)
				agg, ok := computed[key]
				if !ok {
					agg = &aggregate{
						workItemID: workItemID, materialID: materialID, unit: unit,
						total: decimal.Zero,
					}
					computed[key] = agg
				}
				agg.rows = append(agg.rows, SourceRow{CostItemID: costItemID, Quantity: qty})
				// Exact decimal addition — never a float, never rounded (ADR 0001).
				agg.total = agg.total.Add(qty)
				return nil
			})
	if err != nil {
		return GenerationResult{}, err
	}
	result.IntrinsicallyIneligible = IneligibleCounts{
		NonMaterialCategory: nonMaterial, MissingMaterialID: missingMaterial,
		MissingWorkItemID: missingWorkItem, MissingOrZeroQuantity: missingQty,
		BlankUnit: blankUnit,
	}

	// --- 2. Load EVERY existing cost_item anchor, including terminal ones. ---
	// Their presence is what prevents a split or archived anchor from being
	// silently recreated (design spec §3.4).
	existingAnchors, err := s.repo.ListGeneratedAnchorsByProject(ctx, companyID, projectID)
	if err != nil {
		return GenerationResult{}, err
	}
	anchorsByKey := map[string]MaterialRequirement{}
	for _, a := range existingAnchors {
		if a.SourceAggregationKey != "" {
			anchorsByKey[a.SourceAggregationKey] = a
		}
	}

	// --- 3. Walk the union of both key sets, in deterministic order. ---
	for _, key := range unionKeys(computed, anchorsByKey) {
		agg, hasComputed := computed[key]
		anchor, hasAnchor := anchorsByKey[key]

		switch {
		case hasComputed && !hasAnchor:
			created, skipped, err := s.createAnchor(ctx, companyID, projectID, key, agg)
			if err != nil {
				return GenerationResult{}, err
			}
			switch {
			case skipped != nil:
				result.Skipped = append(result.Skipped, *skipped)
				result.SkippedCount++
			case created != nil:
				result.Created = append(result.Created, *created)
				result.CreatedCount++
			default:
				// A concurrent winner already created this anchor; reload and
				// report it as unchanged rather than failing the run
				// (design spec §3.7).
				result.UnchangedCount++
			}

		case hasAnchor:
			// Both cases share one path: compare the CURRENT aggregate — which may
			// be empty — against the anchor's ACCEPTED fingerprint. Using the same
			// comparison for populated and empty aggregates is what stops a
			// resolved source removal from warning forever (design spec §5.3).
			var rows []SourceRow
			if hasComputed {
				rows = agg.rows
			}
			state := s.classifySyncState(anchor, projectID, rows)

			if state != anchor.SourceSyncState {
				if _, err := s.repo.UpdateSyncState(ctx, companyID, anchor.ID,
					anchor.Revision, state, time.Now()); err != nil {
					return GenerationResult{}, err
				}
			}

			ref := DiscrepancyRef{
				MaterialRequirementID: anchor.ID,
				SyncState:             string(state),
				AnchorStatus:          string(anchor.Status),
			}
			switch state {
			case SourceSyncStateChangeDetected:
				result.Discrepancies = append(result.Discrepancies, ref)
				result.DiscrepancyCount++
			case SourceSyncStateSourceRemoved:
				result.SourceRemoved = append(result.SourceRemoved, ref)
				result.SourceRemovedCount++
			default:
				result.UnchangedCount++
			}
		}
	}

	_ = s.audit.RecordMaterialRequirementsGenerated(ctx, companyID, projectID, actorUserID,
		result.CreatedCount, result.UnchangedCount, result.DiscrepancyCount,
		result.SourceRemovedCount, result.SkippedCount)
	return result, nil
}

// classifySyncState compares the current aggregate against the anchor's
// ACCEPTED fingerprint (design spec §5.3):
//
//	fingerprints equal                     -> clean
//	differ, and there are current rows      -> change_detected
//	differ, and there are no current rows   -> source_removed
//
// The empty aggregate has a well-defined fingerprint, so a requirement whose
// removal was accepted via keep_current compares clean on every later run
// instead of warning forever.
func (s *Service) classifySyncState(anchor MaterialRequirement, projectID string, rows []SourceRow) SourceSyncState {
	current := ComputeSourceFingerprint(projectID, WorkItemRefFromPointer(anchor.WorkItemID),
		anchor.MaterialID, anchor.SourceAggregationUnit, rows)
	if current == anchor.SourceFingerprint {
		return SourceSyncStateClean
	}
	if len(rows) == 0 {
		return SourceSyncStateSourceRemoved
	}
	return SourceSyncStateChangeDetected
}

// createAnchor validates project context and creates one draft anchor.
//
// It returns (created, nil, nil) on success, (nil, skipped, nil) when project
// context rejects the row, and (nil, nil, nil) when a concurrent winner already
// holds the key — which the caller reports as unchanged.
func (s *Service) createAnchor(ctx context.Context, companyID, projectID, aggregationKey string,
	agg *aggregate) (*MaterialRequirement, *SkippedRow, error) {

	cancelled, found, err := s.workItemLookup.WorkItemProcurementContext(ctx, companyID, agg.workItemID, projectID)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, &SkippedRow{MaterialID: agg.materialID, WorkItemID: agg.workItemID,
			Reason: SkipReasonWorkItemNotFound}, nil
	}
	if cancelled {
		// Cancelled scope must not produce procurement demand, and this is
		// reported distinctly from "no such work item" (design spec §3.7).
		return nil, &SkippedRow{MaterialID: agg.materialID, WorkItemID: agg.workItemID,
			Reason: SkipReasonWorkItemCancelled}, nil
	}

	name, catalogUnit, catalogSpec, materialFound, err := s.materialLookup.GetMaterialReference(ctx, companyID, agg.materialID)
	if err != nil {
		return nil, nil, err
	}
	if !materialFound {
		return nil, &SkippedRow{MaterialID: agg.materialID, WorkItemID: agg.workItemID,
			Reason: SkipReasonMaterialNotFound}, nil
	}

	sort.Slice(agg.rows, func(i, j int) bool { return agg.rows[i].CostItemID < agg.rows[j].CostItemID })
	costItemIDs := make([]string, 0, len(agg.rows))
	for _, r := range agg.rows {
		costItemIDs = append(costItemIDs, r.CostItemID)
	}

	sourceQty := quantity.Quantity{Value: agg.total, Unit: agg.unit}
	fingerprint := ComputeSourceFingerprint(projectID, WorkItemRef(agg.workItemID),
		agg.materialID, agg.unit, agg.rows)
	now := time.Now()
	workItemID := agg.workItemID

	req := MaterialRequirement{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: &workItemID,
		MaterialID: agg.materialID, MaterialName: name, Specification: catalogSpec,
		// The CostItem unit is preserved verbatim; no conversion is ever
		// attempted (design spec §3.5).
		RequiredQuantity: sourceQty,
		CatalogUnit:      catalogUnit,
		UnitMismatch:     UnitsMismatch(agg.unit, catalogUnit),
		// Review is the confirmation gate, so a generated anchor starts as draft
		// and is not RFQ-eligible (design spec §3.6).
		Status:                RequirementStatusDraft,
		SourceType:            SourceTypeCostItem,
		SourceAggregationUnit: agg.unit,
		SourceAggregationKey:  aggregationKey,
		SourceCostItemIDs:     costItemIDs,
		SourceQuantity:        &sourceQty,
		SourceFingerprint:     fingerprint,
		SourceSyncedAt:        &now,
		SourceSyncState:       SourceSyncStateClean,
		SourceCheckedAt:       &now,
		CreatedAt:             now,
		UpdatedAt:             now,
		SchemaVersion:         1,
	}

	created, err := s.repo.Create(ctx, req)
	if errors.Is(err, ErrSourceAggregationKeyExists) {
		// A concurrent run won this key. The unique index is what makes
		// generation race-safe; the loser reports the winner as unchanged rather
		// than failing the whole command (design spec §3.7).
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &created, nil, nil
}

// unionKeys returns every key present in either map, sorted, so a generation run
// processes keys in a deterministic order and its result is reproducible.
func unionKeys(computed map[string]*aggregate, anchors map[string]MaterialRequirement) []string {
	seen := make(map[string]struct{}, len(computed)+len(anchors))
	keys := make([]string, 0, len(computed)+len(anchors))
	for k := range computed {
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	for k := range anchors {
		if _, ok := seen[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
