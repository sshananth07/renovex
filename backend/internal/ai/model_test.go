package ai

import "testing"

func TestBatchTypeIsValid(t *testing.T) {
	valid := []BatchType{BatchTypeSpaceSuggestions, BatchTypeWorkItemSuggestions, BatchTypeResourceSuggestions}
	for _, bt := range valid {
		if !bt.IsValid() {
			t.Fatalf("expected %q to be valid", bt)
		}
	}
	if BatchType("bogus").IsValid() {
		t.Fatal("expected bogus batch type to be invalid")
	}
}

func TestBatchStatusIsValid(t *testing.T) {
	valid := []BatchStatus{BatchStatusProcessing, BatchStatusCompleted, BatchStatusFailed}
	for _, bs := range valid {
		if !bs.IsValid() {
			t.Fatalf("expected %q to be valid", bs)
		}
	}
	if BatchStatus("pending").IsValid() {
		t.Fatal("expected pending to be invalid — not one of the 3 defined statuses")
	}
}

func TestSuggestionTypeIsValid(t *testing.T) {
	valid := []SuggestionType{
		SuggestionTypeSpace, SuggestionTypeWorkItem,
		SuggestionTypeMaterialResource, SuggestionTypeTradeResource, SuggestionTypeEquipmentResource,
	}
	for _, st := range valid {
		if !st.IsValid() {
			t.Fatalf("expected %q to be valid", st)
		}
	}
	if SuggestionType("subcontractor").IsValid() {
		t.Fatal("expected subcontractor to be invalid — deferred from M8.5B-A")
	}
}

func TestSuggestionStatusIsValid(t *testing.T) {
	valid := []SuggestionStatus{
		SuggestionStatusPending, SuggestionStatusAccepted, SuggestionStatusModified, SuggestionStatusRejected,
	}
	for _, ss := range valid {
		if !ss.IsValid() {
			t.Fatalf("expected %q to be valid", ss)
		}
	}
	if SuggestionStatus("archived").IsValid() {
		t.Fatal("expected archived to be invalid — not one of the 4 defined statuses")
	}
}

func TestSuggestionIsTerminal(t *testing.T) {
	cases := []struct {
		status   SuggestionStatus
		terminal bool
	}{
		{SuggestionStatusPending, false},
		{SuggestionStatusAccepted, true},
		{SuggestionStatusModified, true},
		{SuggestionStatusRejected, true},
	}
	for _, c := range cases {
		s := AISuggestion{Status: c.status}
		if s.IsTerminal() != c.terminal {
			t.Fatalf("status %q: IsTerminal() = %v, want %v", c.status, s.IsTerminal(), c.terminal)
		}
	}
}
