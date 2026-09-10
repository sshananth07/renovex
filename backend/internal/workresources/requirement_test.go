package workresources

import "testing"

func validRequirement() WorkResourceRequirement {
	suggestionID := "suggestion_1"
	return WorkResourceRequirement{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_1",
		ResourceType: ResourceTypeTrade, Name: "Tiler",
		Status: StatusConfirmed, Source: SourceAISuggestion,
		SourceSuggestionID: &suggestionID, CreatedByUserID: "user_1",
		SchemaVersion: 1,
	}
}

func TestValidateRequiresCompanyID(t *testing.T) {
	r := validRequirement()
	r.CompanyID = ""
	if err := r.Validate(); err != ErrCompanyIDRequired {
		t.Fatalf("expected ErrCompanyIDRequired, got %v", err)
	}
}

func TestValidateRequiresProjectID(t *testing.T) {
	r := validRequirement()
	r.ProjectID = ""
	if err := r.Validate(); err != ErrProjectIDRequired {
		t.Fatalf("expected ErrProjectIDRequired, got %v", err)
	}
}

func TestValidateRequiresWorkItemID(t *testing.T) {
	r := validRequirement()
	r.WorkItemID = ""
	if err := r.Validate(); err != ErrWorkItemIDRequired {
		t.Fatalf("expected ErrWorkItemIDRequired, got %v", err)
	}
}

func TestValidateRequiresName(t *testing.T) {
	r := validRequirement()
	r.Name = ""
	if err := r.Validate(); err != ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestValidateRejectsInvalidResourceType(t *testing.T) {
	r := validRequirement()
	r.ResourceType = ResourceType("subcontractor")
	if err := r.Validate(); err != ErrInvalidResourceType {
		t.Fatalf("expected ErrInvalidResourceType, got %v", err)
	}
}

func TestValidateMaterialRequiresMaterialID(t *testing.T) {
	r := validRequirement()
	r.ResourceType = ResourceTypeMaterial
	r.MaterialID = nil
	if err := r.Validate(); err != ErrMaterialIDRequired {
		t.Fatalf("expected ErrMaterialIDRequired for material without MaterialID, got %v", err)
	}
}

func TestValidateMaterialAcceptsMaterialID(t *testing.T) {
	r := validRequirement()
	materialID := "material_1"
	r.ResourceType = ResourceTypeMaterial
	r.MaterialID = &materialID
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateTradeRejectsMaterialID(t *testing.T) {
	r := validRequirement()
	materialID := "material_1"
	r.ResourceType = ResourceTypeTrade
	r.MaterialID = &materialID
	if err := r.Validate(); err != ErrMaterialIDMustBeNil {
		t.Fatalf("expected ErrMaterialIDMustBeNil for trade with MaterialID set, got %v", err)
	}
}

func TestValidateEquipmentRejectsMaterialID(t *testing.T) {
	r := validRequirement()
	materialID := "material_1"
	r.ResourceType = ResourceTypeEquipment
	r.MaterialID = &materialID
	if err := r.Validate(); err != ErrMaterialIDMustBeNil {
		t.Fatalf("expected ErrMaterialIDMustBeNil for equipment with MaterialID set, got %v", err)
	}
}

func TestValidateRejectsInvalidStatus(t *testing.T) {
	r := validRequirement()
	r.Status = Status("draft")
	if err := r.Validate(); err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestValidateRejectsInvalidSource(t *testing.T) {
	r := validRequirement()
	r.Source = Source("imported")
	if err := r.Validate(); err != ErrInvalidSource {
		t.Fatalf("expected ErrInvalidSource, got %v", err)
	}
}

func TestValidateAISourceRequiresSourceSuggestionID(t *testing.T) {
	r := validRequirement()
	r.Source = SourceAISuggestion
	r.SourceSuggestionID = nil
	if err := r.Validate(); err != ErrSourceSuggestionIDRequired {
		t.Fatalf("expected ErrSourceSuggestionIDRequired for ai_suggestion source without id, got %v", err)
	}
}

func TestValidateManualSourceAllowsNilSourceSuggestionID(t *testing.T) {
	r := validRequirement()
	r.Source = SourceManual
	r.SourceSuggestionID = nil
	if err := r.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
