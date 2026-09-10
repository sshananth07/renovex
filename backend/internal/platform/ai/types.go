package ai

// These types mirror the Python ai-service's request/response schemas
// exactly (M8.5B-A design doc §9-§13). They are internal to platform/ai —
// never browser DTOs, never part of the public Huma/OpenAPI surface.

type ProjectContext struct {
	ID         string `json:"id"`
	ScopeBrief string `json:"scopeBrief"`
}

type ExistingSpace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type SpaceSuggestionRequest struct {
	OperationID    string          `json:"operationId"`
	Project        ProjectContext  `json:"project"`
	ExistingSpaces []ExistingSpace `json:"existingSpaces"`
	// RepairInstruction, when non-empty, asks the provider to repair
	// specific coverage omissions from a prior generation in this same
	// request/response cycle — never a second independent generation. Set
	// only for the single bounded repair call the quality gate may issue
	// (T1.5 §9); empty on every normal generation call.
	RepairInstruction string `json:"repairInstruction,omitempty"`
}

type SpaceSuggestion struct {
	Name          string   `json:"name"`
	SpaceType     string   `json:"spaceType"`
	Rationale     string   `json:"rationale"`
	Confidence    *float64 `json:"confidence,omitempty"`
	EvidenceType  string   `json:"evidenceType"`
	SourceExcerpt string   `json:"sourceExcerpt,omitempty"`
}

type SpaceSuggestionResponse struct {
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	PromptVersion string            `json:"promptVersion"`
	SchemaVersion int               `json:"schemaVersion"`
	Suggestions   []SpaceSuggestion `json:"suggestions"`
}

type SpaceContext struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type ExistingWorkItem struct {
	ID          string `json:"id"`
	SpaceID     string `json:"spaceId,omitempty"`
	Description string `json:"description"`
	WorkType    string `json:"workType,omitempty"`
}

type WorkItemSuggestionRequest struct {
	OperationID       string             `json:"operationId"`
	ProjectBrief      string             `json:"projectBrief"`
	Spaces            []SpaceContext     `json:"spaces"`
	ExistingWorkItems []ExistingWorkItem `json:"existingWorkItems"`
}

type WorkItemSuggestion struct {
	Description         string   `json:"description"`
	WorkType            string   `json:"workType"`
	ScopeLevel          string   `json:"scopeLevel"`
	SpaceID             *string  `json:"spaceId"`
	ScopeOrigin         string   `json:"scopeOrigin"`
	Rationale           string   `json:"rationale"`
	Confidence          *float64 `json:"confidence,omitempty"`
	SourceExcerpt       string   `json:"sourceExcerpt,omitempty"`
	MaterialSpecificity string   `json:"materialSpecificity"`
}

type WorkItemSuggestionResponse struct {
	Provider      string               `json:"provider"`
	Model         string               `json:"model"`
	PromptVersion string               `json:"promptVersion"`
	SchemaVersion int                  `json:"schemaVersion"`
	Suggestions   []WorkItemSuggestion `json:"suggestions"`
}

type WorkItemContext struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type MaterialCandidate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExistingRequirement struct {
	WorkItemID   string `json:"workItemId"`
	ResourceType string `json:"resourceType"`
	Name         string `json:"name"`
}

type ResourceSuggestionRequest struct {
	OperationID          string                `json:"operationId"`
	WorkItems            []WorkItemContext     `json:"workItems"`
	MaterialCandidates   []MaterialCandidate   `json:"materialCandidates"`
	ExistingRequirements []ExistingRequirement `json:"existingRequirements"`
}

type ResourceSuggestion struct {
	ResourceType        string   `json:"resourceType"`
	WorkItemID          string   `json:"workItemId"`
	Name                string   `json:"name"`
	CandidateMaterialID *string  `json:"candidateMaterialId"`
	Rationale           string   `json:"rationale"`
	Confidence          *float64 `json:"confidence,omitempty"`
}

type ResourceSuggestionResponse struct {
	Provider      string               `json:"provider"`
	Model         string               `json:"model"`
	PromptVersion string               `json:"promptVersion"`
	SchemaVersion int                  `json:"schemaVersion"`
	Suggestions   []ResourceSuggestion `json:"suggestions"`
}
