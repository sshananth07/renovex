package ai

// DeltaClassification is the outcome of comparing one freshly-generated
// suggestion against the current authoritative project state and prior
// suggestion history (T1.5 §16-§17).
type DeltaClassification string

const (
	// DeltaNew: no matching current authoritative entity or prior
	// suggestion — genuinely new.
	DeltaNew DeltaClassification = "new"
	// DeltaChanged: matches a current authoritative entity via a
	// trustworthy stable link (AcceptedDomainObjectID or equivalent
	// validated lineage) but proposes a materially different field.
	DeltaChanged DeltaClassification = "changed"
	// DeltaUnchanged: matches a current authoritative entity and proposes
	// nothing different — suppressed from active review, counted only.
	DeltaUnchanged DeltaClassification = "unchanged"
	// DeltaConflict: ambiguous — something that would need CHANGED's
	// stable link but doesn't have a trustworthy one, or otherwise can't
	// be safely auto-classified.
	DeltaConflict DeltaClassification = "conflict"
	// DeltaSuppressed: normalized-matches a suggestion previously rejected
	// under the same brief lineage (SourceBrief) — filtered from active
	// review, but the fresh suggestion is still persisted (audit trail
	// preserved, T1.5 §17 amendment).
	DeltaSuppressed DeltaClassification = "suppressed"
)

// PriorSpaceDecision is the minimal shape of a previously-decided Space
// suggestion the reconciler needs: its data, disposition, the brief it was
// generated against, and (if accepted/modified) which authoritative Space
// it resolved to.
type PriorSpaceDecision struct {
	Data                   SpaceSuggestionData
	Status                 SuggestionStatus
	SourceBrief            string
	AcceptedDomainObjectID string
}

// SpaceDeltaItem is one classified fresh Space suggestion.
type SpaceDeltaItem struct {
	Data           SpaceSuggestionData
	Classification DeltaClassification
	// CurrentAuthoritativeID is set for DeltaChanged/DeltaUnchanged — the
	// real Space this suggestion resolves against.
	CurrentAuthoritativeID string
}

// ClassifySpaceDelta compares fresh (a newly-generated batch's Space
// suggestions, already quality-gated) against currentSpaces (authoritative,
// from DomainGateway) and priorDecisions (this project's suggestion
// history for the Space type) to produce one classification per fresh item
// (T1.5 §16-§17). currentBrief is the brief the fresh batch was generated
// against — used to detect whether a prior rejection's lineage still
// applies.
func ClassifySpaceDelta(fresh []SpaceSuggestionData, currentSpaces []DomainGatewaySpace, priorDecisions []PriorSpaceDecision, currentBrief string) []SpaceDeltaItem {
	currentByKey := make(map[string]DomainGatewaySpace, len(currentSpaces))
	for _, sp := range currentSpaces {
		currentByKey[normalizeSpaceKey(sp.Name, sp.Type)] = sp
	}

	items := make([]SpaceDeltaItem, 0, len(fresh))
	for _, item := range fresh {
		key := normalizeSpaceKey(item.Name, item.SpaceType)

		if current, ok := currentByKey[key]; ok {
			items = append(items, SpaceDeltaItem{Data: item, Classification: DeltaUnchanged, CurrentAuthoritativeID: current.ID})
			continue
		}

		if changed, ok := findChangedSpaceLink(item, currentSpaces, priorDecisions); ok {
			items = append(items, SpaceDeltaItem{Data: item, Classification: DeltaChanged, CurrentAuthoritativeID: changed})
			continue
		}

		if isRejectedUnderSameBrief(item, priorDecisions, currentBrief) {
			items = append(items, SpaceDeltaItem{Data: item, Classification: DeltaSuppressed})
			continue
		}

		items = append(items, SpaceDeltaItem{Data: item, Classification: DeltaNew})
	}
	return items
}

// findChangedSpaceLink looks for a prior suggestion that (a) was
// accepted/modified into a real current authoritative Space
// (AcceptedDomainObjectID resolves to a currentSpaces entry — a trustworthy
// stable link, never fuzzy name-only inference) and (b) is different
// enough from item that presenting it as CHANGED (not just UNCHANGED) is
// warranted. A prior decision whose accepted object no longer exists in
// currentSpaces is not a trustworthy link (the Space may have been
// deleted) and is ignored here — that case surfaces as NEW/CONFLICT
// instead, never guessed at.
func findChangedSpaceLink(item SpaceSuggestionData, currentSpaces []DomainGatewaySpace, priorDecisions []PriorSpaceDecision) (string, bool) {
	currentByID := make(map[string]DomainGatewaySpace, len(currentSpaces))
	for _, sp := range currentSpaces {
		currentByID[sp.ID] = sp
	}
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusAccepted && prior.Status != SuggestionStatusModified {
			continue
		}
		if prior.AcceptedDomainObjectID == "" {
			continue
		}
		current, ok := currentByID[prior.AcceptedDomainObjectID]
		if !ok {
			continue // stable link doesn't resolve to a current entity — not trustworthy
		}
		if prior.Data.SpaceType != item.SpaceType {
			continue // different kind of space entirely — not a "change" of this one
		}
		if normalizeSpaceKey(current.Name, current.Type) == normalizeSpaceKey(item.Name, item.SpaceType) {
			continue // identical to current — that's UNCHANGED, handled by the caller before this is reached
		}
		return current.ID, true
	}
	return "", false
}

// isRejectedUnderSameBrief reports whether item normalized-matches a
// previously REJECTED suggestion generated against the same SourceBrief —
// suppress-from-review-but-still-persist territory (T1.5 §17).
func isRejectedUnderSameBrief(item SpaceSuggestionData, priorDecisions []PriorSpaceDecision, currentBrief string) bool {
	normalizedCurrentBrief := normalizeMatchText(currentBrief)
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusRejected {
			continue
		}
		if normalizeMatchText(prior.SourceBrief) != normalizedCurrentBrief {
			continue // brief changed since rejection — evidence may differ, allow it to resurface
		}
		if normalizeSpaceKey(prior.Data.Name, prior.Data.SpaceType) == normalizeSpaceKey(item.Name, item.SpaceType) {
			return true
		}
	}
	return false
}

func normalizeWorkItemKey(description string, spaceID *string) string {
	sid := ""
	if spaceID != nil {
		sid = *spaceID
	}
	return normalizeMatchText(description) + "|" + sid
}

// PriorWorkItemDecision mirrors PriorSpaceDecision for Work Item
// suggestions (T1.5B closure).
type PriorWorkItemDecision struct {
	Data                   WorkItemSuggestionData
	Status                 SuggestionStatus
	SourceBrief            string
	AcceptedDomainObjectID string
}

// WorkItemDeltaItem is one classified fresh Work Item suggestion.
type WorkItemDeltaItem struct {
	Data                   WorkItemSuggestionData
	Classification         DeltaClassification
	CurrentAuthoritativeID string
}

// ClassifyWorkItemDelta mirrors ClassifySpaceDelta for Work Items — same
// invariants: CHANGED requires a trustworthy AcceptedDomainObjectID link
// resolving to a currently-existing Work Item, never fuzzy description
// matching; UNCHANGED collapses; SUPPRESSED stays persisted but hidden from
// active review for an equivalent prior rejection under the same brief.
func ClassifyWorkItemDelta(fresh []WorkItemSuggestionData, currentWorkItems []DomainGatewayWorkItem, priorDecisions []PriorWorkItemDecision, currentBrief string) []WorkItemDeltaItem {
	currentByKey := make(map[string]DomainGatewayWorkItem, len(currentWorkItems))
	for _, w := range currentWorkItems {
		var spaceID *string
		if w.SpaceID != "" {
			spaceID = &w.SpaceID
		}
		currentByKey[normalizeWorkItemKey(w.Description, spaceID)] = w
	}

	items := make([]WorkItemDeltaItem, 0, len(fresh))
	for _, item := range fresh {
		key := normalizeWorkItemKey(item.Description, item.SpaceID)

		if current, ok := currentByKey[key]; ok {
			items = append(items, WorkItemDeltaItem{Data: item, Classification: DeltaUnchanged, CurrentAuthoritativeID: current.ID})
			continue
		}

		if changed, ok := findChangedWorkItemLink(item, currentWorkItems, priorDecisions); ok {
			items = append(items, WorkItemDeltaItem{Data: item, Classification: DeltaChanged, CurrentAuthoritativeID: changed})
			continue
		}

		if isWorkItemRejectedUnderSameBrief(item, priorDecisions, currentBrief) {
			items = append(items, WorkItemDeltaItem{Data: item, Classification: DeltaSuppressed})
			continue
		}

		items = append(items, WorkItemDeltaItem{Data: item, Classification: DeltaNew})
	}
	return items
}

func findChangedWorkItemLink(item WorkItemSuggestionData, currentWorkItems []DomainGatewayWorkItem, priorDecisions []PriorWorkItemDecision) (string, bool) {
	currentByID := make(map[string]DomainGatewayWorkItem, len(currentWorkItems))
	for _, w := range currentWorkItems {
		currentByID[w.ID] = w
	}
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusAccepted && prior.Status != SuggestionStatusModified {
			continue
		}
		if prior.AcceptedDomainObjectID == "" {
			continue
		}
		current, ok := currentByID[prior.AcceptedDomainObjectID]
		if !ok {
			continue // stable link doesn't resolve to a current entity — not trustworthy
		}
		var currentSpaceID *string
		if current.SpaceID != "" {
			currentSpaceID = &current.SpaceID
		}
		if normalizeWorkItemKey(current.Description, currentSpaceID) == normalizeWorkItemKey(item.Description, item.SpaceID) {
			continue // identical to current — that's UNCHANGED, handled by the caller before this is reached
		}
		return current.ID, true
	}
	return "", false
}

func isWorkItemRejectedUnderSameBrief(item WorkItemSuggestionData, priorDecisions []PriorWorkItemDecision, currentBrief string) bool {
	normalizedCurrentBrief := normalizeMatchText(currentBrief)
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusRejected {
			continue
		}
		if normalizeMatchText(prior.SourceBrief) != normalizedCurrentBrief {
			continue
		}
		if normalizeWorkItemKey(prior.Data.Description, prior.Data.SpaceID) == normalizeWorkItemKey(item.Description, item.SpaceID) {
			return true
		}
	}
	return false
}

func normalizeResourceKey(workItemID, resourceType, name string) string {
	return workItemID + "|" + resourceType + "|" + normalizeMatchText(name)
}

// PriorResourceDecision mirrors PriorSpaceDecision for Resource
// suggestions (T1.5B closure).
type PriorResourceDecision struct {
	WorkItemID             string
	ResourceType           string
	Name                   string
	Status                 SuggestionStatus
	SourceBrief            string
	AcceptedDomainObjectID string
}

// ResourceDeltaItem is one classified fresh Resource suggestion.
type ResourceDeltaItem struct {
	WorkItemID             string
	ResourceType           string
	Name                   string
	Classification         DeltaClassification
	CurrentAuthoritativeID string
}

// ClassifyResourceDelta mirrors ClassifySpaceDelta/ClassifyWorkItemDelta
// for Resources, EXCEPT it never returns DeltaChanged: workresources has no
// update-in-place domain mutation (only Create/Find/List — see
// workresources.Service), so presenting a "Use suggestion" action here
// would either be false (nothing to route it through) or would require
// creating a replacement requirement as a substitute for update, which is
// exactly the silent-duplicate/replace behavior T1.5 forbids. A fresh
// suggestion that would otherwise qualify as CHANGED (trustworthy lineage
// to a current authoritative requirement, but different name) is
// classified CONFLICT instead — surfaced for a human decision through
// whatever means, never auto-resolved. A future workresources update
// capability can upgrade this to true CHANGED without altering this
// classification model.
func ClassifyResourceDelta(fresh []ResourceDeltaItem, currentRequirements []DomainGatewayRequirement, priorDecisions []PriorResourceDecision, currentBrief string) []ResourceDeltaItem {
	currentByKey := make(map[string]DomainGatewayRequirement, len(currentRequirements))
	currentByID := make(map[string]DomainGatewayRequirement, len(currentRequirements))
	for _, r := range currentRequirements {
		currentByKey[normalizeResourceKey(r.WorkItemID, r.ResourceType, r.Name)] = r
		currentByID[r.ID] = r
	}

	items := make([]ResourceDeltaItem, 0, len(fresh))
	for _, item := range fresh {
		key := normalizeResourceKey(item.WorkItemID, item.ResourceType, item.Name)

		if current, ok := currentByKey[key]; ok {
			items = append(items, ResourceDeltaItem{
				WorkItemID: item.WorkItemID, ResourceType: item.ResourceType, Name: item.Name,
				Classification: DeltaUnchanged, CurrentAuthoritativeID: current.ID,
			})
			continue
		}

		if conflictID, ok := findConflictingResourceLink(item, currentByID, priorDecisions); ok {
			items = append(items, ResourceDeltaItem{
				WorkItemID: item.WorkItemID, ResourceType: item.ResourceType, Name: item.Name,
				Classification: DeltaConflict, CurrentAuthoritativeID: conflictID,
			})
			continue
		}

		if isResourceRejectedUnderSameBrief(item, priorDecisions, currentBrief) {
			items = append(items, ResourceDeltaItem{
				WorkItemID: item.WorkItemID, ResourceType: item.ResourceType, Name: item.Name,
				Classification: DeltaSuppressed,
			})
			continue
		}

		items = append(items, ResourceDeltaItem{
			WorkItemID: item.WorkItemID, ResourceType: item.ResourceType, Name: item.Name,
			Classification: DeltaNew,
		})
	}
	return items
}

func findConflictingResourceLink(item ResourceDeltaItem, currentByID map[string]DomainGatewayRequirement, priorDecisions []PriorResourceDecision) (string, bool) {
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusAccepted && prior.Status != SuggestionStatusModified {
			continue
		}
		if prior.AcceptedDomainObjectID == "" {
			continue
		}
		current, ok := currentByID[prior.AcceptedDomainObjectID]
		if !ok {
			continue
		}
		if prior.WorkItemID != item.WorkItemID || prior.ResourceType != item.ResourceType {
			continue
		}
		if normalizeResourceKey(current.WorkItemID, current.ResourceType, current.Name) == normalizeResourceKey(item.WorkItemID, item.ResourceType, item.Name) {
			continue // identical to current — UNCHANGED, handled before this is reached
		}
		return current.ID, true
	}
	return "", false
}

func isResourceRejectedUnderSameBrief(item ResourceDeltaItem, priorDecisions []PriorResourceDecision, currentBrief string) bool {
	normalizedCurrentBrief := normalizeMatchText(currentBrief)
	for _, prior := range priorDecisions {
		if prior.Status != SuggestionStatusRejected {
			continue
		}
		if normalizeMatchText(prior.SourceBrief) != normalizedCurrentBrief {
			continue
		}
		if normalizeResourceKey(prior.WorkItemID, prior.ResourceType, prior.Name) == normalizeResourceKey(item.WorkItemID, item.ResourceType, item.Name) {
			return true
		}
	}
	return false
}
