// Package ai owns AIGenerationBatch and AISuggestion — AI output that stays
// separate from authoritative business data until a contractor accepts it
// (M8.5B-A design doc §7-§8). It composes the platform/ai HTTP client (an
// authenticated Go->Python client, not a domain gateway) and, through
// internal/aiintegration's composition adapter, existing Renovex domain
// services. AI suggests, the system calculates, the contractor approves.
package ai

import "time"

// BatchType identifies which of the three AI generation stages a batch is.
type BatchType string

const (
	BatchTypeSpaceSuggestions    BatchType = "space_suggestions"
	BatchTypeWorkItemSuggestions BatchType = "work_item_suggestions"
	BatchTypeResourceSuggestions BatchType = "resource_suggestions"
)

// IsValid reports whether t is one of the three defined batch types.
func (t BatchType) IsValid() bool {
	switch t {
	case BatchTypeSpaceSuggestions, BatchTypeWorkItemSuggestions, BatchTypeResourceSuggestions:
		return true
	default:
		return false
	}
}

// BatchStatus is a generation batch's lifecycle state.
type BatchStatus string

const (
	BatchStatusProcessing BatchStatus = "processing"
	BatchStatusCompleted  BatchStatus = "completed"
	BatchStatusFailed     BatchStatus = "failed"
)

// IsValid reports whether s is one of the three defined batch statuses.
func (s BatchStatus) IsValid() bool {
	switch s {
	case BatchStatusProcessing, BatchStatusCompleted, BatchStatusFailed:
		return true
	default:
		return false
	}
}

// AIGenerationBatch is the durable record of one generation request/response
// cycle (design doc §7). Only suggestions belonging to a completed batch are
// reviewable — a failed generation leaves zero reviewable suggestions
// (design doc §17).
type AIGenerationBatch struct {
	ID        string
	CompanyID string
	ProjectID string

	Type   BatchType
	Status BatchStatus

	OperationID      string
	Provider         string
	Model            string
	PromptVersion    string
	SchemaVersion    int
	InputFingerprint string

	StartedAt   time.Time
	CompletedAt *time.Time
	FailedAt    *time.Time
	ErrorCode   string

	// SourceBrief is the trimmed project scope brief this batch was
	// generated from (generation provenance, not a second authoritative
	// project brief — the Project's ScopeBrief remains authoritative).
	// Used to detect whether a later "Re-run AI setup" click targets an
	// unchanged brief (warn-and-compare) or a materially changed one
	// (generate updates directly) without a second fingerprint call.
	SourceBrief string

	CreatedByUserID string
	CreatedAt       time.Time
}

// SuggestionType identifies what kind of authoritative domain object a
// suggestion would become on acceptance.
type SuggestionType string

const (
	SuggestionTypeSpace             SuggestionType = "space"
	SuggestionTypeWorkItem          SuggestionType = "work_item"
	SuggestionTypeMaterialResource  SuggestionType = "material_resource"
	SuggestionTypeTradeResource     SuggestionType = "trade_resource"
	SuggestionTypeEquipmentResource SuggestionType = "equipment_resource"
)

// IsValid reports whether t is one of the five defined suggestion types.
func (t SuggestionType) IsValid() bool {
	switch t {
	case SuggestionTypeSpace, SuggestionTypeWorkItem,
		SuggestionTypeMaterialResource, SuggestionTypeTradeResource, SuggestionTypeEquipmentResource:
		return true
	default:
		return false
	}
}

// SuggestionStatus is one AISuggestion's review lifecycle state (design doc
// §8.1).
type SuggestionStatus string

const (
	SuggestionStatusPending  SuggestionStatus = "pending"
	SuggestionStatusAccepted SuggestionStatus = "accepted"
	SuggestionStatusModified SuggestionStatus = "modified"
	SuggestionStatusRejected SuggestionStatus = "rejected"
)

// IsValid reports whether s is one of the four defined suggestion statuses.
func (s SuggestionStatus) IsValid() bool {
	switch s {
	case SuggestionStatusPending, SuggestionStatusAccepted, SuggestionStatusModified, SuggestionStatusRejected:
		return true
	default:
		return false
	}
}

// AISuggestion is one non-authoritative AI-generated proposal, separate from
// business data until a contractor accepts it (design doc §8). SuggestedData
// is a discriminated, typed payload keyed by Type — never an arbitrary
// unvalidated map at service boundaries, even though BSON may persist it as
// a discriminated document. Only a short contractor-facing Rationale is
// stored; hidden provider reasoning/chain-of-thought is never requested or
// stored (design doc §8, "AI Governance Metadata").
type AISuggestion struct {
	ID        string
	CompanyID string
	ProjectID string
	BatchID   string

	Type   SuggestionType
	Status SuggestionStatus

	Confidence *float64
	Rationale  string

	SuggestedData SuggestedData

	InputFingerprint string

	AcceptedDomainObjectID string
	AcceptedAt             *time.Time
	AcceptedByUserID       string

	RejectedAt       *time.Time
	RejectedByUserID string

	Revision  int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsTerminal reports whether the suggestion is in a terminal, no-longer-
// pending state (design doc §8.1: accepted/modified/rejected all sit below
// the single "authoritative object" terminal node in the review lifecycle).
func (s AISuggestion) IsTerminal() bool {
	return s.Status == SuggestionStatusAccepted || s.Status == SuggestionStatusModified || s.Status == SuggestionStatusRejected
}

// SuggestedData is the discriminated AI-domain payload for one suggestion.
// Exactly one field is populated, matching the suggestion's Type — Go's type
// system cannot express a true sum type here, so callers switch on Type and
// trust the corresponding field is set (enforced by the service layer that
// constructs these, never by public deserialization).
type SuggestedData struct {
	Space             *SpaceSuggestionData
	WorkItem          *WorkItemSuggestionData
	MaterialResource  *ResourceSuggestionData
	TradeResource     *ResourceSuggestionData
	EquipmentResource *ResourceSuggestionData
}

// EvidenceType classifies how strongly a suggestion is grounded in the
// project brief (T1.5 quality hardening). Reused across Space suggestions
// and (via WorkItemSuggestionData.ScopeOrigin's existing three-way split)
// conceptually mirrored for Work Items — Space suggestions had no such
// field before T1.5, which is the root cause of explicit Spaces silently
// disappearing.
type EvidenceType string

const (
	// EvidenceExplicit means the brief directly names or requests this —
	// Go independently verifies (bounded fuzzy grounding, see
	// evidence.go) that a claimed-explicit item's SourceExcerpt actually
	// appears in the brief before trusting this classification; an
	// unverifiable claim is downgraded before persistence.
	EvidenceExplicit EvidenceType = "explicit"
	// EvidenceDerived means this is inferred support for explicit scope.
	EvidenceDerived EvidenceType = "derived"
	// EvidencePossibleMissing means this is a plausible addition requiring
	// user judgment — never phrased as a confirmed omission.
	EvidencePossibleMissing EvidenceType = "possible_missing"
)

// IsValid reports whether e is one of the three defined evidence types.
func (e EvidenceType) IsValid() bool {
	switch e {
	case EvidenceExplicit, EvidenceDerived, EvidencePossibleMissing:
		return true
	default:
		return false
	}
}

// SpaceSuggestionData mirrors the Python service's SpaceSuggestion schema —
// name/spaceType/rationale/confidence plus evidence grounding (design doc
// §9.2 as hardened by T1.5). No dimensions/quantities/costs. JSON tags are
// required here — this struct is marshaled directly as the public
// suggestedData response body (toSuggestedDataJSON), not through a DTO with
// its own tags, so the default Go field-name casing would otherwise leak
// into the API contract.
type SpaceSuggestionData struct {
	Name      string `json:"name"`
	SpaceType string `json:"spaceType"`

	// EvidenceType classifies how this Space is grounded in the brief.
	EvidenceType EvidenceType `json:"evidenceType"`
	// SourceExcerpt is an optional short brief excerpt supporting
	// EvidenceType — evidence for humans/debugging only. It is never
	// trusted at face value: an EvidenceExplicit claim is independently
	// grounding-checked against the actual brief text (evidence.go)
	// before persistence, and downgraded if it doesn't hold up.
	SourceExcerpt string `json:"sourceExcerpt,omitempty"`
}

// ScopeLevel is Space-scoped or project-level/unassigned Work.
type ScopeLevel string

const (
	ScopeLevelSpace   ScopeLevel = "space"
	ScopeLevelProject ScopeLevel = "project"
)

// ScopeOrigin classifies why a Work Item suggestion exists (design doc
// §10.5). All three remain equally non-authoritative until reviewed.
type ScopeOrigin string

const (
	ScopeOriginExplicit        ScopeOrigin = "explicit_scope"
	ScopeOriginSupporting      ScopeOrigin = "supporting_scope"
	ScopeOriginPossibleMissing ScopeOrigin = "possible_missing_scope"
)

// MaterialSpecificity classifies whether a Work Item's description
// establishes a specific material/product (T1.5 §6-§7 cascade prevention).
// This is a hard, one-directional invariant: only the AI's own classification
// of the ORIGINAL work item description (independently grounding-checked by
// Go exactly like EvidenceExplicit) or an explicit later user confirmation
// may set this to MaterialExplicit. No downstream generation stage
// (Resources reading a Work Item, or any future stage reading a Resource)
// may ever promote MaterialUnspecified/MaterialInferred to MaterialExplicit
// merely because it consumed the prior stage's output — that is exactly the
// inference-cascading bug this field exists to prevent.
type MaterialSpecificity string

const (
	MaterialExplicit    MaterialSpecificity = "explicit"
	MaterialInferred    MaterialSpecificity = "inferred"
	MaterialUnspecified MaterialSpecificity = "unspecified"
)

// IsValid reports whether m is one of the three defined specificity levels.
func (m MaterialSpecificity) IsValid() bool {
	switch m {
	case MaterialExplicit, MaterialInferred, MaterialUnspecified:
		return true
	default:
		return false
	}
}

// WorkItemSuggestionData mirrors the Python service's WorkItemSuggestion
// schema. It deliberately carries no quantity/unit/cost/price field — the
// contractor supplies quantity/unit at acceptance time (design doc §10.6).
type WorkItemSuggestionData struct {
	Description string      `json:"description"`
	WorkType    string      `json:"workType"`
	ScopeLevel  ScopeLevel  `json:"scopeLevel"`
	SpaceID     *string     `json:"spaceId,omitempty"`
	ScopeOrigin ScopeOrigin `json:"scopeOrigin"`

	// SourceExcerpt is an optional short brief excerpt supporting
	// ScopeOrigin=explicit_scope — same grounding-check treatment as
	// SpaceSuggestionData.SourceExcerpt.
	SourceExcerpt string `json:"sourceExcerpt,omitempty"`
	// MaterialSpecificity classifies whether this Work Item establishes a
	// specific material/product. Resource generation (SuggestResources)
	// reads this to decide whether product-specific resources may be
	// proposed for this Work Item at all.
	MaterialSpecificity MaterialSpecificity `json:"materialSpecificity"`
}

// ResourceSuggestionData mirrors the Python service's ResourceSuggestion
// schema for one of material/trade/equipment. CandidateMaterialID is
// advisory only and set only for material suggestions (design doc §12.3).
type ResourceSuggestionData struct {
	WorkItemID          string  `json:"workItemId"`
	Name                string  `json:"name"`
	CandidateMaterialID *string `json:"candidateMaterialId,omitempty"`
}
