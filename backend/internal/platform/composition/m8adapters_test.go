package composition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// M8's composition adapters (design spec §2.1, §1A.3).
//
// Two adapters resolve what would otherwise be a construction cycle:
//
//	rfqissuance needs the M7 ready RFQ snapshot  -> ReadyRFQSourceAdapter
//	rfqs needs M8's issuance status              -> IssuanceStatusAdapter
//
// Neither domain module imports the other. The cycle is broken by constructing
// rfqs with NoExternalIssuanceSource first, then calling
// SetIssuanceStatusSource with the adapter once rfqissuance exists.

// --- ReadyRFQSourceAdapter ---

// fakeReadySource stands in for rfqs.Service. The adapter's job is conversion
// and sentinel translation, so a fake proves the mapping without a database.
type fakeReadySource struct {
	rfqNumber            string
	projectID            string
	revision             int64
	title                string
	deliveryAddress      string
	requiredBy           *time.Time
	responseDeadline     *time.Time
	supplierInstructions string
	lines                []rfqs.RFQLineSnapshot
	found                bool
	err                  error
}

func (f *fakeReadySource) GetReadyRFQSnapshot(context.Context, string, string) (
	string, string, int64, string, string, *time.Time, *time.Time, string,
	[]rfqs.RFQLineSnapshot, bool, error) {
	return f.rfqNumber, f.projectID, f.revision, f.title, f.deliveryAddress, f.requiredBy,
		f.responseDeadline, f.supplierInstructions, f.lines, f.found, f.err
}

func TestReadyRFQSourceAdapterConvertsEveryField(t *testing.T) {
	requiredBy := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)

	source := &fakeReadySource{
		rfqNumber: "RFQ-000123", projectID: "project-1", revision: 7,
		title:           "Cement and aggregate",
		deliveryAddress: "12 Jalan Satu", requiredBy: &requiredBy,
		responseDeadline: &deadline, supplierInstructions: "deliver to site office",
		lines: []rfqs.RFQLineSnapshot{{
			LineID: "line-1", SourceMaterialRequirementID: "mr-1", MaterialID: "material-1",
			MaterialName: "Cement", Specification: "OPC 50kg",
			QuantityValue: "12.5", QuantityUnit: "bag", RequiredByDate: &requiredBy,
			ProcurementNotes: "stack under cover", SortOrder: 0,
		}},
		found: true,
	}

	adapter := composition.NewReadyRFQSourceAdapter(source)

	snapshot, found, err := adapter.GetReadyRFQSnapshot(context.Background(), "company-1", "chain-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected the ready RFQ to be found")
	}

	if snapshot.RFQNumber != "RFQ-000123" {
		t.Errorf("RFQNumber = %q, want RFQ-000123", snapshot.RFQNumber)
	}
	if snapshot.ProjectID != "project-1" {
		t.Errorf("ProjectID = %q, want project-1", snapshot.ProjectID)
	}
	if snapshot.SourceM7RFQRevision != 7 {
		t.Errorf("SourceM7RFQRevision = %d, want 7", snapshot.SourceM7RFQRevision)
	}
	if snapshot.Title != "Cement and aggregate" {
		t.Errorf("Title = %q, want Cement and aggregate", snapshot.Title)
	}
	if snapshot.DeliveryAddress != "12 Jalan Satu" {
		t.Errorf("DeliveryAddress = %q, want 12 Jalan Satu", snapshot.DeliveryAddress)
	}
	if snapshot.SupplierInstructions != "deliver to site office" {
		t.Errorf("SupplierInstructions = %q, want deliver to site office",
			snapshot.SupplierInstructions)
	}
	if snapshot.RequiredByDate == nil || !snapshot.RequiredByDate.Equal(requiredBy) {
		t.Errorf("RequiredByDate = %v, want %v", snapshot.RequiredByDate, requiredBy)
	}
	if snapshot.ResponseDeadline == nil || !snapshot.ResponseDeadline.Equal(deadline) {
		t.Errorf("ResponseDeadline = %v, want %v", snapshot.ResponseDeadline, deadline)
	}

	if len(snapshot.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(snapshot.Lines))
	}
	line := snapshot.Lines[0]
	if line.SourceRFQLineID != "line-1" {
		t.Errorf("SourceRFQLineID = %q, want line-1", line.SourceRFQLineID)
	}
	// Provenance must survive conversion: copy-forward matches on the Material
	// Requirement, and award traceability references it (design spec §3.2A).
	if line.SourceMaterialRequirementID != "mr-1" {
		t.Errorf("SourceMaterialRequirementID = %q, want mr-1", line.SourceMaterialRequirementID)
	}
	if line.MaterialID != "material-1" {
		t.Errorf("MaterialID = %q, want material-1", line.MaterialID)
	}
	if line.MaterialName != "Cement" {
		t.Errorf("MaterialName = %q, want Cement", line.MaterialName)
	}
	if line.Specification != "OPC 50kg" {
		t.Errorf("Specification = %q, want OPC 50kg", line.Specification)
	}
	if line.QuantityValue != "12.5" {
		t.Errorf("QuantityValue = %q, want 12.5", line.QuantityValue)
	}
	if line.QuantityUnit != "bag" {
		t.Errorf("QuantityUnit = %q, want bag", line.QuantityUnit)
	}
	if line.ProcurementNotes != "stack under cover" {
		t.Errorf("ProcurementNotes = %q, want stack under cover", line.ProcurementNotes)
	}
}

// A draft or absent RFQ is reported as not-found, never as an error. That is
// what lets rfqissuance answer "not ready" without learning why.
func TestReadyRFQSourceAdapterReportsNotFound(t *testing.T) {
	adapter := composition.NewReadyRFQSourceAdapter(&fakeReadySource{found: false})

	_, found, err := adapter.GetReadyRFQSnapshot(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found = false for a non-ready RFQ")
	}
}

// An infrastructure failure must propagate unchanged. Laundering it into
// "not ready" would let issuance proceed against an unknown source state.
func TestReadyRFQSourceAdapterPropagatesErrors(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")
	adapter := composition.NewReadyRFQSourceAdapter(&fakeReadySource{err: sentinel})

	_, _, err := adapter.GetReadyRFQSnapshot(context.Background(), "company-1", "chain-1")

	if !errors.Is(err, sentinel) {
		t.Errorf("expected the underlying error to propagate, got %v", err)
	}
}

// --- IssuanceStatusAdapter ---

type fakeIssuanceStatus struct {
	issued bool
	err    error
}

func (f *fakeIssuanceStatus) RFQChainHasIssuedVersion(context.Context, string, string) (bool, error) {
	return f.issued, f.err
}

func TestIssuanceStatusAdapterReportsIssued(t *testing.T) {
	adapter := composition.NewIssuanceStatusAdapter(&fakeIssuanceStatus{issued: true})

	issued, err := adapter.RFQChainIssued(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !issued {
		t.Error("expected issued = true")
	}
}

func TestIssuanceStatusAdapterReportsNotIssued(t *testing.T) {
	adapter := composition.NewIssuanceStatusAdapter(&fakeIssuanceStatus{issued: false})

	issued, err := adapter.RFQChainIssued(context.Background(), "company-1", "chain-1")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issued {
		t.Error("expected issued = false")
	}
}

// Fail closed: an error must NOT be reported as "not issued". rfqs turns a
// lookup failure into ErrIssuanceStatusUnavailable and refuses to reopen, which
// only works if the adapter propagates the error rather than swallowing it
// (design spec §2.1).
func TestIssuanceStatusAdapterFailsClosedOnError(t *testing.T) {
	sentinel := errors.New("mongo is unreachable")
	adapter := composition.NewIssuanceStatusAdapter(&fakeIssuanceStatus{issued: false, err: sentinel})

	issued, err := adapter.RFQChainIssued(context.Background(), "company-1", "chain-1")

	if err == nil {
		t.Fatal("expected the error to propagate so rfqs can fail closed, got nil")
	}
	if issued {
		t.Error("a failed lookup must never report issued = true")
	}
}

// The adapters must satisfy the interfaces their consumers declare. If either
// drifts, this stops compiling — which is the point.
func TestAdaptersSatisfyConsumerInterfaces(t *testing.T) {
	var _ rfqissuance.ReadyRFQSource = composition.NewReadyRFQSourceAdapter(&fakeReadySource{})
	var _ rfqs.IssuanceStatusSource = composition.NewIssuanceStatusAdapter(&fakeIssuanceStatus{})
}
