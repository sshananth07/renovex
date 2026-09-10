package supplieroffers

import "testing"

func TestDraftStatusAllowsMutationOnlyWhileActive(t *testing.T) {
	tests := []struct {
		status DraftStatus
		want   bool
	}{
		{status: DraftActive, want: true},
		{status: DraftSubmitting, want: false},
		{status: DraftArchived, want: false},
		{status: DraftStatus("unknown"), want: false},
	}

	for _, tt := range tests {
		if got := tt.status.AllowsMutation(); got != tt.want {
			t.Fatalf("%q AllowsMutation = %v, want %v", tt.status, got, tt.want)
		}
	}
}
