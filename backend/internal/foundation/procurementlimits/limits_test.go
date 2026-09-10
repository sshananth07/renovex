package procurementlimits_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

func TestResourceIDAcceptsOneTo128ASCIIBytesOnly(t *testing.T) {
	for _, value := range []string{"a", strings.Repeat("x", 128)} {
		if err := procurementlimits.ValidateID(value); err != nil {
			t.Errorf("ValidateID(%d ASCII bytes) = %v", len(value), err)
		}
	}
	for _, value := range []string{"", strings.Repeat("x", 129), "invitation-λ", "line\n2"} {
		if err := procurementlimits.ValidateID(value); err == nil {
			t.Errorf("ValidateID(%q) succeeded", value)
		}
	}
}

func TestTextLimitsCountTrimmedUnicodeCodePoints(t *testing.T) {
	if got, err := procurementlimits.TrimText("  "+strings.Repeat("界", 200)+"  ", 200); err != nil ||
		got != strings.Repeat("界", 200) {
		t.Fatalf("200-rune text = %q, %v", got, err)
	}
	if _, err := procurementlimits.TrimText(strings.Repeat("界", 201), 200); err == nil {
		t.Fatal("201-rune text succeeded")
	}
}

func TestUniqueIDsAndCollectionBounds(t *testing.T) {
	ids := make([]string, procurementlimits.MaxIDsPerRequest)
	for index := range ids {
		ids[index] = "id-" + strconv.Itoa(index)
	}
	if err := procurementlimits.ValidateUniqueIDs(ids); err != nil {
		t.Fatalf("100 unique IDs = %v", err)
	}
	if err := procurementlimits.ValidateUniqueIDs(append(ids, "extra")); err == nil {
		t.Fatal("101 IDs succeeded")
	}
	if err := procurementlimits.ValidateUniqueIDs([]string{"same", "same"}); err == nil {
		t.Fatal("duplicate IDs succeeded")
	}
}

func TestBusinessDateHorizonsAtAndBeyondBoundaries(t *testing.T) {
	base := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	if err := procurementlimits.ValidateResponseDeadline(base, base.Add(180*24*time.Hour)); err != nil {
		t.Fatalf("180-day response deadline = %v", err)
	}
	if err := procurementlimits.ValidateResponseDeadline(base, base.Add(180*24*time.Hour+time.Nanosecond)); err == nil {
		t.Fatal("response deadline beyond 180 days succeeded")
	}
	if err := procurementlimits.ValidateOfferValidity(base, base.AddDate(1, 0, 1)); err != nil {
		t.Fatalf("366-day offer validity = %v", err)
	}
	if err := procurementlimits.ValidateOfferValidity(base, base.AddDate(1, 0, 1).Add(time.Nanosecond)); err == nil {
		t.Fatal("offer validity beyond 366 days succeeded")
	}
}

func TestPageAndBatchBounds(t *testing.T) {
	for _, tc := range []struct{ supplied, want int }{{0, 25}, {1, 1}, {100, 100}} {
		got, err := procurementlimits.PageSize(tc.supplied)
		if err != nil || got != tc.want {
			t.Errorf("PageSize(%d) = %d, %v; want %d", tc.supplied, got, err, tc.want)
		}
	}
	if _, err := procurementlimits.PageSize(101); err == nil {
		t.Fatal("page size 101 succeeded")
	}
	if _, err := procurementlimits.BatchSize(101); err == nil {
		t.Fatal("batch size 101 succeeded")
	}
}
