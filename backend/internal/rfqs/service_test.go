package rfqs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// C3-C5 cover RFQ creation, header edits, deletion guarded by outstanding
// claims, line add/remove with claim orchestration and compensation, and the
// lifecycle transitions.

// --- fakes ---

// fakeRFQRepo reproduces the invariants the real repository enforces in its
// conditional filters, not merely storage: the Revision guard and the
// draft/ready state filters. A fake that only stored documents would let a
// service bug pass here and fail in C2's Docker tests.
type fakeRFQRepo struct {
	byID map[string]rfqs.RFQ
	next int

	// replaceLinesCalls records every line write, so a test can assert that a
	// REJECTED operation wrote nothing rather than merely that the values match.
	replaceLinesCalls int

	// failReplaceLines, when set, makes the line append fail — the §7.5
	// compensation window.
	failReplaceLines error
}

func newFakeRepo() *fakeRFQRepo {
	return &fakeRFQRepo{byID: map[string]rfqs.RFQ{}}
}

func (f *fakeRFQRepo) Create(_ context.Context, r rfqs.RFQ) (rfqs.RFQ, error) {
	// Mirror uq_rfqs_company_number.
	for _, existing := range f.byID {
		if existing.CompanyID == r.CompanyID && existing.RFQNumber == r.RFQNumber {
			return rfqs.RFQ{}, rfqs.ErrRFQNumberConflict
		}
	}
	f.next++
	r.ID = "rfq_" + itoa(f.next)
	f.byID[r.ID] = r
	return r, nil
}

func (f *fakeRFQRepo) FindByID(_ context.Context, companyID, id string) (rfqs.RFQ, error) {
	r, ok := f.byID[id]
	if !ok || r.CompanyID != companyID {
		return rfqs.RFQ{}, rfqs.ErrRFQNotFound
	}
	return r, nil
}

func (f *fakeRFQRepo) ListByProject(_ context.Context, companyID, projectID string) ([]rfqs.RFQ, error) {
	var out []rfqs.RFQ
	for _, r := range f.byID {
		if r.CompanyID == companyID && r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRFQRepo) FindByLineRequirementID(_ context.Context, companyID, requirementID string) (rfqs.RFQ, bool, error) {
	for _, r := range f.byID {
		if r.CompanyID != companyID {
			continue
		}
		if _, ok := r.FindLineByRequirementID(requirementID); ok {
			return r, true, nil
		}
	}
	return rfqs.RFQ{}, false, nil
}

func (f *fakeRFQRepo) UpdateDraft(ctx context.Context, companyID, id string,
	expectedRevision int64, updated rfqs.RFQ) (rfqs.RFQ, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return rfqs.RFQ{}, err
	}
	// Mirror the real filter: a ready rfq matches nothing.
	if stored.Status != rfqs.RFQStatusDraft || stored.Revision != expectedRevision {
		return rfqs.RFQ{}, rfqs.ErrRevisionMismatch
	}

	stored.Title = updated.Title
	stored.DeliveryAddress = updated.DeliveryAddress
	stored.RequiredByDate = updated.RequiredByDate
	stored.ResponseDeadline = updated.ResponseDeadline
	stored.SupplierInstructions = updated.SupplierInstructions
	stored.InternalNotes = updated.InternalNotes
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRFQRepo) ReplaceLines(ctx context.Context, companyID, id string,
	expectedRevision int64, lines []rfqs.RFQLine) (rfqs.RFQ, error) {
	f.replaceLinesCalls++
	if f.failReplaceLines != nil {
		return rfqs.RFQ{}, f.failReplaceLines
	}
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return rfqs.RFQ{}, err
	}
	if stored.Status != rfqs.RFQStatusDraft || stored.Revision != expectedRevision {
		return rfqs.RFQ{}, rfqs.ErrRevisionMismatch
	}
	stored.Lines = lines
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()
	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRFQRepo) MarkReady(ctx context.Context, companyID, id string,
	expectedRevision int64, readyAt time.Time) (rfqs.RFQ, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return rfqs.RFQ{}, err
	}
	if stored.Status != rfqs.RFQStatusDraft || stored.Revision != expectedRevision {
		return rfqs.RFQ{}, rfqs.ErrRevisionMismatch
	}
	stored.Status = rfqs.RFQStatusReady
	stored.ReadyAt = &readyAt
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRFQRepo) Reopen(ctx context.Context, companyID, id string,
	expectedRevision int64, reopenedAt time.Time) (rfqs.RFQ, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return rfqs.RFQ{}, err
	}
	if stored.Status != rfqs.RFQStatusReady || stored.Revision != expectedRevision {
		return rfqs.RFQ{}, rfqs.ErrRevisionMismatch
	}
	stored.Status = rfqs.RFQStatusDraft
	stored.ReopenedAt = &reopenedAt
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

func (f *fakeRFQRepo) Delete(ctx context.Context, companyID, id string, expectedRevision int64) error {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return err
	}
	if stored.Revision != expectedRevision {
		return rfqs.ErrRevisionMismatch
	}
	delete(f.byID, id)
	return nil
}

type fakeCounter struct{ n int64 }

func (c *fakeCounter) NextRFQNumber(context.Context, string) (int64, error) {
	c.n++
	return c.n, nil
}

type fakeProjectLookup struct{ projects map[string]string } // projectID -> companyID

func (f fakeProjectLookup) ProjectBelongsToCompany(_ context.Context, companyID, projectID string) (bool, error) {
	return f.projects[projectID] == companyID, nil
}

// fakeRequirementSource stands in for materialrequirements. It reproduces the
// claim semantics that module enforces atomically: exclusivity, the project
// filter, and the exact chain+line release guard.
type fakeRequirementSource struct {
	// requirementID -> the requirement's claim-relevant state
	reqs map[string]*fakeRequirement

	claimCalls   int
	releaseCalls int

	// failClaim / failRelease inject the failures §7.5's compensation matrix
	// describes.
	failClaim   error
	failRelease error
}

type fakeRequirement struct {
	projectID string
	revision  int64
	eligible  bool
	readyOK   bool

	chainID string
	number  string
	lineID  string

	materialID, materialName, specification string
	quantityValue, quantityUnit             string
	procurementNotes                        string
	requiredByDate                          *time.Time
}

func (f *fakeRequirementSource) ClaimForRFQ(_ context.Context, companyID, projectID, requirementID string,
	expectedRevision int64, rfqChainID, rfqNumber, lineID string) (rfqs.ClaimSnapshot, error) {
	f.claimCalls++
	if f.failClaim != nil {
		return rfqs.ClaimSnapshot{}, f.failClaim
	}
	r, ok := f.reqs[requirementID]
	if !ok {
		return rfqs.ClaimSnapshot{}, rfqs.ErrMaterialRequirementNotFound
	}
	// projectId is part of the SAME atomic filter as everything else.
	if r.projectID != projectID {
		return rfqs.ClaimSnapshot{}, rfqs.ErrMaterialRequirementNotFound
	}
	if r.chainID != "" {
		return rfqs.ClaimSnapshot{}, rfqs.ErrMaterialRequirementAlreadyClaimed
	}
	if !r.eligible {
		return rfqs.ClaimSnapshot{}, rfqs.ErrRFQLineNotEligible
	}
	if r.revision != expectedRevision {
		return rfqs.ClaimSnapshot{}, rfqs.ErrRevisionMismatch
	}

	r.chainID, r.number, r.lineID = rfqChainID, rfqNumber, lineID
	r.revision++

	return rfqs.ClaimSnapshot{
		RequirementID: requirementID, Revision: r.revision,
		MaterialID: r.materialID, MaterialName: r.materialName,
		Specification: r.specification,
		QuantityValue: r.quantityValue, QuantityUnit: r.quantityUnit,
		RequiredByDate: r.requiredByDate, ProcurementNotes: r.procurementNotes,
	}, nil
}

func (f *fakeRequirementSource) ReleaseClaim(_ context.Context, companyID, requirementID string,
	expectedRevision int64, rfqChainID, lineID string) error {
	f.releaseCalls++
	if f.failRelease != nil {
		return f.failRelease
	}
	r, ok := f.reqs[requirementID]
	if !ok {
		return rfqs.ErrMaterialRequirementNotFound
	}
	// The EXACT chain and line, plus the revision.
	if r.chainID != rfqChainID || r.lineID != lineID || r.revision != expectedRevision {
		return rfqs.ErrRevisionMismatch
	}
	r.chainID, r.number, r.lineID = "", "", ""
	r.revision++
	return nil
}

func (f *fakeRequirementSource) ReadClaim(_ context.Context, companyID, requirementID string) (
	string, string, string, int64, bool, error) {
	r, ok := f.reqs[requirementID]
	if !ok {
		return "", "", "", 0, false, rfqs.ErrMaterialRequirementNotFound
	}
	if r.chainID == "" {
		return "", "", "", r.revision, false, nil
	}
	return r.chainID, r.number, r.lineID, r.revision, true, nil
}

func (f *fakeRequirementSource) ListClaimsForRFQChain(_ context.Context, companyID, rfqChainID string) (
	[]rfqs.RFQRequirementClaim, error) {
	var out []rfqs.RFQRequirementClaim
	for id, r := range f.reqs {
		if r.chainID == rfqChainID {
			out = append(out, rfqs.RFQRequirementClaim{
				RequirementID: id, RFQChainID: r.chainID, RFQNumber: r.number,
				LineID: r.lineID, Revision: r.revision,
			})
		}
	}
	return out, nil
}

// ReadClaimSnapshot mirrors the EXACT-claim filter materialrequirements
// enforces: company, requirement, the exact chain AND the exact line. It
// deliberately does not re-check eligibility (design spec §1.3).
func (f *fakeRequirementSource) ReadClaimSnapshot(_ context.Context, companyID, requirementID,
	rfqChainID, lineID string) (rfqs.ClaimSnapshot, error) {
	r, ok := f.reqs[requirementID]
	if !ok {
		return rfqs.ClaimSnapshot{}, rfqs.ErrMaterialRequirementNotFound
	}
	if r.chainID != rfqChainID || r.lineID != lineID {
		return rfqs.ClaimSnapshot{}, rfqs.ErrMaterialRequirementNotFound
	}
	return rfqs.ClaimSnapshot{
		RequirementID: requirementID, Revision: r.revision,
		MaterialID: r.materialID, MaterialName: r.materialName,
		Specification: r.specification,
		QuantityValue: r.quantityValue, QuantityUnit: r.quantityUnit,
		RequiredByDate: r.requiredByDate, ProcurementNotes: r.procurementNotes,
	}, nil
}

func (f *fakeRequirementSource) ClaimedRequirementIsReadyForRFQ(_ context.Context,
	companyID, requirementID, rfqChainID, lineID string) (bool, error) {
	r, ok := f.reqs[requirementID]
	if !ok {
		return false, nil
	}
	// Requires the claim to name THIS chain and THIS line.
	if r.chainID != rfqChainID || r.lineID != lineID {
		return false, nil
	}
	return r.readyOK, nil
}

type recordedAudit struct {
	created, updated, lineAdded, lineRemoved int
	markedReady, reopened, deleted           int
	reconciled                               int
	lastReconcileAction                      string
}

type fakeAudit struct{ rec *recordedAudit }

func (f fakeAudit) RecordRFQCreated(context.Context, string, string, string, string, string) error {
	f.rec.created++
	return nil
}
func (f fakeAudit) RecordRFQUpdated(context.Context, string, string, string, string, string) error {
	f.rec.updated++
	return nil
}
func (f fakeAudit) RecordRFQLineAdded(context.Context, string, string, string, string, string, string, string) error {
	f.rec.lineAdded++
	return nil
}
func (f fakeAudit) RecordRFQLineRemoved(context.Context, string, string, string, string, string, string, string) error {
	f.rec.lineRemoved++
	return nil
}
func (f fakeAudit) RecordRFQMarkedReady(_ context.Context, _, _, _, _, _ string, _ int) error {
	f.rec.markedReady++
	return nil
}
func (f fakeAudit) RecordRFQReopened(context.Context, string, string, string, string, string) error {
	f.rec.reopened++
	return nil
}
func (f fakeAudit) RecordRFQDeleted(context.Context, string, string, string, string, string) error {
	f.rec.deleted++
	return nil
}
func (f fakeAudit) RecordRFQClaimReconciled(_ context.Context, _, _, _, _, _, action string) error {
	f.rec.reconciled++
	f.rec.lastReconcileAction = action
	return nil
}

// newService wires a service where project_1 belongs to company_a and mr_1/mr_2
// are eligible, unclaimed requirements in project_1.
func newService(t *testing.T) (*rfqs.Service, *fakeRFQRepo, *fakeRequirementSource, *recordedAudit) {
	t.Helper()
	repo := newFakeRepo()
	rec := &recordedAudit{}
	source := &fakeRequirementSource{reqs: map[string]*fakeRequirement{
		"mr_1": {
			projectID: "project_1", eligible: true, readyOK: true,
			materialID: "material_1", materialName: "Portland Cement",
			specification: "OPC 50kg", quantityValue: "100", quantityUnit: "bag",
			procurementNotes: "deliver to site gate",
		},
		"mr_2": {
			projectID: "project_1", eligible: true, readyOK: true,
			materialID: "material_2", materialName: "River Sand",
			specification: "washed", quantityValue: "12.5", quantityUnit: "m3",
		},
		// Belongs to another project of the SAME company.
		"mr_other_project": {
			projectID: "project_2", eligible: true, readyOK: true,
			materialID: "material_1", materialName: "Portland Cement",
			quantityValue: "5", quantityUnit: "bag",
		},
	}}

	svc := rfqs.NewService(repo, &fakeCounter{},
		fakeProjectLookup{projects: map[string]string{
			"project_1": "company_a", "project_2": "company_a",
		}},
		source, rfqs.NoExternalIssuanceSource{}, fakeAudit{rec: rec})
	return svc, repo, source, rec
}

func createDraft(t *testing.T, svc *rfqs.Service) rfqs.RFQ {
	t.Helper()
	got, err := svc.CreateRFQ(context.Background(), "company_a", "user_1", "project_1",
		rfqs.CreateRFQInput{Title: "Cement and aggregate", DeliveryAddress: "12 Site Road"})
	if err != nil {
		t.Fatalf("unexpected error creating rfq: %v", err)
	}
	return got
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// --- C3: creation ---

// An RFQ is created EMPTY: header fields are allowed, requirement IDs are not.
// Lines arrive only through the claim-and-append path (design spec §13.2).
func TestCreateRFQStartsEmptyAndDraft(t *testing.T) {
	svc, _, _, rec := newService(t)

	got := createDraft(t, svc)

	if got.Status != rfqs.RFQStatusDraft {
		t.Errorf("Status = %q, want draft", got.Status)
	}
	if len(got.Lines) != 0 {
		t.Errorf("a new rfq has %d lines, want 0 — lines arrive only via claim-and-append",
			len(got.Lines))
	}
	if got.RFQNumber != "RFQ-000001" {
		t.Errorf("RFQNumber = %q, want RFQ-000001", got.RFQNumber)
	}
	if got.ChainID() != got.ID {
		t.Error("the chain id must be the aggregate id")
	}
	if got.ProjectID != "project_1" || got.CreatedByUserID != "user_1" {
		t.Errorf("provenance lost: %+v", got)
	}
	if rec.created != 1 {
		t.Errorf("audit created = %d, want 1", rec.created)
	}
}

func TestCreateRFQAllocatesSequentialNumbers(t *testing.T) {
	svc, _, _, _ := newService(t)

	first := createDraft(t, svc)
	second := createDraft(t, svc)

	if first.RFQNumber == second.RFQNumber {
		t.Fatalf("both rfqs got %q", first.RFQNumber)
	}
	if second.RFQNumber != "RFQ-000002" {
		t.Errorf("second number = %q, want RFQ-000002", second.RFQNumber)
	}
}

func TestCreateRFQRejectsAForeignProject(t *testing.T) {
	svc, _, _, _ := newService(t)

	if _, err := svc.CreateRFQ(context.Background(), "company_a", "user_1", "project_zzz",
		rfqs.CreateRFQInput{}); !errors.Is(err, rfqs.ErrProjectNotFound) {
		t.Fatalf("error = %v, want ErrProjectNotFound", err)
	}
}

// --- C3: header edits ---

func TestUpdateRFQAppliesHeaderFieldsUnderRevisionGuard(t *testing.T) {
	svc, _, _, rec := newService(t)
	created := createDraft(t, svc)
	ctx := context.Background()

	notes := "contractor-only note"
	got, err := svc.UpdateRFQ(ctx, "company_a", "user_1", created.ID, created.Revision,
		rfqs.UpdateRFQInput{
			Title:                strPtr("Revised"),
			DeliveryAddress:      strPtr("99 New Road"),
			SupplierInstructions: strPtr("before 10am"),
			InternalNotes:        &notes,
		})
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if got.Title != "Revised" || got.DeliveryAddress != "99 New Road" {
		t.Errorf("header not applied: %+v", got)
	}
	if got.InternalNotes != notes {
		t.Errorf("InternalNotes = %q, want it stored on the aggregate", got.InternalNotes)
	}
	if rec.updated != 1 {
		t.Errorf("audit updated = %d, want 1", rec.updated)
	}

	// The same expectation now fails.
	if _, err := svc.UpdateRFQ(ctx, "company_a", "user_1", created.ID, created.Revision,
		rfqs.UpdateRFQInput{Title: strPtr("again")}); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// A sparse edit leaves untouched fields alone.
func TestUpdateRFQIsSparse(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	got, err := svc.UpdateRFQ(context.Background(), "company_a", "user_1", created.ID,
		created.Revision, rfqs.UpdateRFQInput{SupplierInstructions: strPtr("before 10am")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != created.Title {
		t.Errorf("Title = %q, want %q unchanged", got.Title, created.Title)
	}
	if got.DeliveryAddress != created.DeliveryAddress {
		t.Errorf("DeliveryAddress = %q, want unchanged", got.DeliveryAddress)
	}
}

func TestUpdateRFQIsTenantScoped(t *testing.T) {
	svc, _, _, _ := newService(t)
	created := createDraft(t, svc)

	if _, err := svc.UpdateRFQ(context.Background(), "company_b", "user_1", created.ID,
		created.Revision, rfqs.UpdateRFQInput{Title: strPtr("x")}); !errors.Is(err, rfqs.ErrRFQNotFound) {
		t.Fatalf("error = %v, want ErrRFQNotFound", err)
	}
}

func strPtr(s string) *string { return &s }
