package costs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func mustCreateCostItem(t *testing.T, svc *costs.Service, category costs.CostCategory) costs.CostItem {
	t.Helper()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, category,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating cost item: %v", err)
	}
	return created
}

func TestRecordCostItemActualSetsTheFirstValueWithNoCorrectionRecord(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)

	updated, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision,
		money.New(85000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error recording the first actual: %v", err)
	}
	if updated.Actual == nil || updated.Actual.Amount != 85000 {
		t.Fatalf("Actual = %v, want 85000", updated.Actual)
	}
	if len(updated.ActualCorrections) != 0 {
		t.Fatalf("recording the FIRST actual must not create a correction record, got %d", len(updated.ActualCorrections))
	}
}

func TestRecordCostItemActualRejectsWhenActualAlreadySet(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)

	first, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, first.Revision, money.New(90000, "MYR"))
	if !errors.Is(err, costs.ErrActualAlreadyRecorded) {
		t.Fatalf("error = %v, want ErrActualAlreadyRecorded — a second attempt must use Correct, not Record", err)
	}
}

func TestCorrectCostItemActualPreservesThePreviousValue(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	corrected, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "Original entry was a data-entry error — decimal point")
	if err != nil {
		t.Fatalf("unexpected error correcting actual: %v", err)
	}
	if corrected.Actual == nil || corrected.Actual.Amount != 8500 {
		t.Fatalf("Actual after correction = %v, want 8500", corrected.Actual)
	}
	if len(corrected.ActualCorrections) != 1 {
		t.Fatalf("expected exactly 1 correction record, got %d", len(corrected.ActualCorrections))
	}
	if corrected.ActualCorrections[0].PreviousAmount.Amount != 85000 || corrected.ActualCorrections[0].NewAmount.Amount != 8500 {
		t.Errorf("correction record = %+v, want previous=85000 new=8500", corrected.ActualCorrections[0])
	}
	if corrected.ActualCorrections[0].Reason != "Original entry was a data-entry error — decimal point" {
		t.Errorf("Reason = %q", corrected.ActualCorrections[0].Reason)
	}
	if corrected.ActualCorrections[0].CorrectedByUser != "user_1" {
		t.Errorf("CorrectedByUser = %q, want user_1", corrected.ActualCorrections[0].CorrectedByUser)
	}
}

func TestCorrectCostItemActualAppendsRatherThanReplacesEarlierCorrections(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(850000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}
	afterFirstCorrection, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(85000, "MYR"), "First correction")
	if err != nil {
		t.Fatal(err)
	}
	afterSecondCorrection, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterFirstCorrection.Revision,
		money.New(8500, "MYR"), "Second correction")
	if err != nil {
		t.Fatal(err)
	}
	if len(afterSecondCorrection.ActualCorrections) != 2 {
		t.Fatalf("expected 2 correction records after 2 corrections, got %d", len(afterSecondCorrection.ActualCorrections))
	}
	if afterSecondCorrection.ActualCorrections[0].PreviousAmount.Amount != 850000 || afterSecondCorrection.ActualCorrections[0].NewAmount.Amount != 85000 {
		t.Errorf("first correction record = %+v", afterSecondCorrection.ActualCorrections[0])
	}
	if afterSecondCorrection.ActualCorrections[1].PreviousAmount.Amount != 85000 || afterSecondCorrection.ActualCorrections[1].NewAmount.Amount != 8500 {
		t.Errorf("second correction record = %+v", afterSecondCorrection.ActualCorrections[1])
	}
}

func TestCorrectCostItemActualRejectsWhenActualNotYetSet(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)

	_, err := svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrNoActualToCorrect) {
		t.Fatalf("error = %v, want ErrNoActualToCorrect — a first entry must use Record, not Correct", err)
	}
}

func TestCorrectCostItemActualRequiresAReason(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "")
	if !errors.Is(err, costs.ErrCorrectionReasonRequired) {
		t.Fatalf("error = %v, want ErrCorrectionReasonRequired", err)
	}
}

func TestCorrectCostItemActualRejectsStaleRevision(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_a", "user_1", created.ID, afterRecord.Revision+1,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

func TestCorrectCostItemActualIsTenantScoped(t *testing.T) {
	svc, _ := newTestService()
	created := mustCreateCostItem(t, svc, costs.CostCategoryMaterial)
	afterRecord, err := svc.RecordCostItemActual(context.Background(), "company_a", "user_1", created.ID, created.Revision, money.New(85000, "MYR"))
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.CorrectCostItemActual(context.Background(), "company_b", "user_1", created.ID, afterRecord.Revision,
		money.New(8500, "MYR"), "some reason")
	if !errors.Is(err, costs.ErrCostItemNotFound) {
		t.Fatalf("error = %v, want ErrCostItemNotFound", err)
	}
}
