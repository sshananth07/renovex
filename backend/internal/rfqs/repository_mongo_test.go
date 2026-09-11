package rfqs_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// C2 covers the RFQ repository and the tenant-scoped number counter against a
// real MongoDB (design spec §6.1, §12.2).

// setupDB follows the M6 bootstrap convention: raw mongo.Connect, a uniquely
// named database per test, TerminateContainer in cleanup. A unique DB matters
// because several tests exercise unique indexes.
func setupDB(t *testing.T) *mongo.Database {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("rfqs_test_%d", time.Now().UnixNano()))
}

func newRepo(t *testing.T, db *mongo.Database) *rfqs.MongoRFQRepository {
	t.Helper()
	repo := rfqs.NewMongoRFQRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure indexes: %v", err)
	}
	return repo
}

func newCounterRepo(t *testing.T, db *mongo.Database) *rfqs.MongoRFQCounterRepository {
	t.Helper()
	repo := rfqs.NewMongoRFQCounterRepository(db)
	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("failed to ensure counter indexes: %v", err)
	}
	return repo
}

func qty(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("bad quantity: %v", err)
	}
	return q
}

// draftRFQ builds a header-complete draft with no lines.
func draftRFQ(companyID, projectID, number string) rfqs.RFQ {
	now := time.Now().UTC().Truncate(time.Millisecond)
	requiredBy := now.Add(14 * 24 * time.Hour)
	deadline := now.Add(7 * 24 * time.Hour)
	return rfqs.RFQ{
		CompanyID: companyID, ProjectID: projectID, RFQNumber: number,
		Status: rfqs.RFQStatusDraft,
		Title:  "Cement and aggregate",

		DeliveryAddress:      "12 Site Road, Unit 4",
		RequiredByDate:       &requiredBy,
		ResponseDeadline:     &deadline,
		SupplierInstructions: "deliver before 10am",
		InternalNotes:        "contractor-only: prefer supplier B on price",

		CreatedByUserID: "user_1",
		CreatedAt:       now, UpdatedAt: now, SchemaVersion: 1,
	}
}

func sampleLine(t *testing.T, id, requirementID string, sortOrder int) rfqs.RFQLine {
	t.Helper()
	at := time.Now().UTC().Truncate(time.Millisecond)
	requiredBy := at.Add(10 * 24 * time.Hour)
	return rfqs.RFQLine{
		ID: id, SourceMaterialRequirementID: requirementID,
		MaterialID: "material_1", MaterialName: "Portland Cement",
		Specification:  "OPC 50kg",
		Quantity:       qty(t, "100.5", "bag"),
		RequiredByDate: &requiredBy, ProcurementNotes: "deliver to site gate",
		SnapshotAt: at, SortOrder: sortOrder,
	}
}

// --- Round-trip ---

func TestMongoCreateAndFindRoundTripsEveryField(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	rfq := draftRFQ("company_a", "project_1", "RFQ-000001")
	rfq.Lines = []rfqs.RFQLine{sampleLine(t, "line_1", "mr_1", 0)}

	created, err := repo.Create(ctx, rfq)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("Create did not assign an id")
	}

	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.RFQNumber != "RFQ-000001" || got.Status != rfqs.RFQStatusDraft {
		t.Errorf("header round-trip failed: %+v", got)
	}
	if got.Title != rfq.Title || got.DeliveryAddress != rfq.DeliveryAddress ||
		got.SupplierInstructions != rfq.SupplierInstructions {
		t.Errorf("supplier-visible header fields lost: %+v", got)
	}
	// InternalNotes lives in the AGGREGATE; only the projections omit it.
	if got.InternalNotes != rfq.InternalNotes {
		t.Errorf("InternalNotes = %q, want it preserved in the aggregate", got.InternalNotes)
	}
	if got.RequiredByDate == nil || !got.RequiredByDate.Equal(*rfq.RequiredByDate) {
		t.Errorf("RequiredByDate = %v, want %v", got.RequiredByDate, rfq.RequiredByDate)
	}
	if got.ResponseDeadline == nil || !got.ResponseDeadline.Equal(*rfq.ResponseDeadline) {
		t.Errorf("ResponseDeadline = %v", got.ResponseDeadline)
	}

	if len(got.Lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(got.Lines))
	}
	line := got.Lines[0]
	// Quantities are stored as canonical decimal STRINGS and must round-trip
	// without precision loss (ADR 0001).
	if line.Quantity.Value.String() != "100.5" || line.Quantity.Unit != "bag" {
		t.Errorf("line quantity = %s %s, want 100.5 bag",
			line.Quantity.Value.String(), line.Quantity.Unit)
	}
	if line.SourceMaterialRequirementID != "mr_1" || line.ID != "line_1" {
		t.Errorf("line identity lost: %+v", line)
	}
	if line.RequiredByDate == nil || line.ProcurementNotes != "deliver to site gate" {
		t.Errorf("line snapshot fields lost: %+v", line)
	}
}

// A high-precision quantity must survive the string encoding exactly.
func TestMongoLineQuantityPreservesPrecision(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	rfq := draftRFQ("company_a", "project_1", "RFQ-000001")
	line := sampleLine(t, "line_1", "mr_1", 0)
	line.Quantity = qty(t, "33.7501", "m2")
	rfq.Lines = []rfqs.RFQLine{line}

	created, err := repo.Create(ctx, rfq)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0].Quantity.Value.String() != "33.7501" {
		t.Errorf("quantity = %s, want 33.7501 exactly",
			got.Lines[0].Quantity.Value.String())
	}
}

// --- Tenant scoping ---

func TestMongoFindByIDIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindByID(ctx, "company_b", created.ID); !errors.Is(err, rfqs.ErrRFQNotFound) {
		t.Fatalf("error = %v, want ErrRFQNotFound for a foreign company", err)
	}
}

func TestMongoListByProjectIsTenantAndProjectScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	for _, spec := range []struct{ company, project, number string }{
		{"company_a", "project_1", "RFQ-000001"},
		{"company_a", "project_1", "RFQ-000002"},
		{"company_a", "project_2", "RFQ-000003"},
		{"company_b", "project_1", "RFQ-000001"}, // same number, other tenant
	} {
		if _, err := repo.Create(ctx, draftRFQ(spec.company, spec.project, spec.number)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rfqs, want 2", len(got))
	}
	for _, r := range got {
		if r.CompanyID != "company_a" || r.ProjectID != "project_1" {
			t.Errorf("listing leaked %+v", r)
		}
	}
}

// A malformed id is reported as not-found so the handler maps it to 404 rather
// than 500.
func TestMongoFindByIDRejectsMalformedID(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)

	if _, err := repo.FindByID(context.Background(), "company_a", "not-an-object-id"); !errors.Is(
		err, rfqs.ErrRFQNotFound) {
		t.Fatalf("error = %v, want ErrRFQNotFound", err)
	}
}

// --- Unique number per company (design spec §12.2) ---

func TestMongoRFQNumberIsUniquePerCompany(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001")); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, draftRFQ("company_a", "project_2", "RFQ-000001")); !errors.Is(
		err, rfqs.ErrRFQNumberConflict) {
		t.Fatalf("error = %v, want ErrRFQNumberConflict", err)
	}
	// The index is per company, so another tenant may hold the same number.
	if _, err := repo.Create(ctx, draftRFQ("company_b", "project_1", "RFQ-000001")); err != nil {
		t.Errorf("a different company must be able to reuse a number: %v", err)
	}
}

// --- Counter (design spec §6.1) ---

// FindOneAndUpdate + $inc + upsert is atomic BY CONSTRUCTION: no read-then-write
// window, no retry loop. This is why ErrRFQNumberConflict indicates corruption
// rather than a losable race (design spec §13.5).
func TestMongoNextRFQNumberIncrementsPerCompany(t *testing.T) {
	db := setupDB(t)
	counters := newCounterRepo(t, db)
	ctx := context.Background()

	for want := int64(1); want <= 3; want++ {
		got, err := counters.NextRFQNumber(ctx, "company_a")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("NextRFQNumber() = %d, want %d", got, want)
		}
	}

	// A second company starts its own sequence at 1.
	got, err := counters.NextRFQNumber(ctx, "company_b")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("company_b first number = %d, want 1 — counters are tenant-scoped", got)
	}
}

// Concurrent allocation must never hand two callers the same number.
func TestMongoNextRFQNumberIsAtomicUnderConcurrency(t *testing.T) {
	db := setupDB(t)
	counters := newCounterRepo(t, db)
	ctx := context.Background()

	const callers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[int64]int{}
	var errs []error

	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			n, err := counters.NextRFQNumber(ctx, "company_a")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			seen[n]++
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("errors allocating numbers: %v", errs)
	}
	if len(seen) != callers {
		t.Fatalf("got %d distinct numbers from %d callers — the counter is not atomic",
			len(seen), callers)
	}
	for n, count := range seen {
		if count != 1 {
			t.Errorf("number %d was issued %d times", n, count)
		}
	}
}

func TestFormatRFQNumberIsSixDigitPadded(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{
		{1, "RFQ-000001"},
		{124, "RFQ-000124"},
		{999999, "RFQ-999999"},
		{1000000, "RFQ-1000000"},
	} {
		if got := rfqs.FormatRFQNumber(tc.n); got != tc.want {
			t.Errorf("FormatRFQNumber(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// --- Conditional updates (design spec §10) ---

func TestMongoUpdateDraftIsRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}

	updated := created
	updated.Title = "Revised title"
	updated.DeliveryAddress = "99 New Road"

	got, err := repo.UpdateDraft(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Revised title" || got.DeliveryAddress != "99 New Road" {
		t.Errorf("update did not apply: %+v", got)
	}
	if got.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want %d", got.Revision, created.Revision+1)
	}

	// The same expectation now fails.
	if _, err := repo.UpdateDraft(ctx, "company_a", created.ID, created.Revision,
		updated); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// A ready RFQ rejects header edits at the FILTER, so §6.4's draft-only rule is
// enforced by the write rather than only by the service.
func TestMongoUpdateDraftRejectsAReadyRFQ(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}
	created.Lines = []rfqs.RFQLine{sampleLine(t, "line_1", "mr_1", 0)}
	withLine, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, created.Lines)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := repo.MarkReady(ctx, "company_a", created.ID, withLine.Revision, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	updated := ready
	updated.Title = "should not apply"
	if _, err := repo.UpdateDraft(ctx, "company_a", created.ID, ready.Revision,
		updated); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch for a ready rfq", err)
	}

	unchanged, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Title == "should not apply" {
		t.Error("a ready rfq accepted a header edit")
	}
}

// --- Lifecycle: BOTH transitions increment Revision (design spec §6.4) ---

// The ABA hazard: an RFQ cycles draft -> ready -> draft. If the revision froze,
// a stale pre-ready client's conditional update would still match and mutate an
// RFQ whose scope was reviewed and locked in between. Deliberately unlike
// quotations.Finalize, which never returns to draft.
func TestMongoMarkReadyAndReopenBothIncrementRevision(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}
	withLine, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision,
		[]rfqs.RFQLine{sampleLine(t, "line_1", "mr_1", 0)})
	if err != nil {
		t.Fatal(err)
	}

	readyAt := time.Now().UTC().Truncate(time.Millisecond)
	ready, err := repo.MarkReady(ctx, "company_a", created.ID, withLine.Revision, readyAt)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != rfqs.RFQStatusReady {
		t.Errorf("Status = %q, want ready", ready.Status)
	}
	if ready.ReadyAt == nil || !ready.ReadyAt.Equal(readyAt) {
		t.Errorf("ReadyAt = %v, want %v", ready.ReadyAt, readyAt)
	}
	if ready.Revision != withLine.Revision+1 {
		t.Fatalf("mark-ready Revision = %d, want %d — the ABA hazard requires an increment",
			ready.Revision, withLine.Revision+1)
	}

	reopenedAt := readyAt.Add(time.Minute)
	reopened, err := repo.Reopen(ctx, "company_a", created.ID, ready.Revision, reopenedAt)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status != rfqs.RFQStatusDraft {
		t.Errorf("Status = %q, want draft", reopened.Status)
	}
	if reopened.ReopenedAt == nil || !reopened.ReopenedAt.Equal(reopenedAt) {
		t.Errorf("ReopenedAt = %v", reopened.ReopenedAt)
	}
	if reopened.Revision != ready.Revision+1 {
		t.Fatalf("reopen Revision = %d, want %d", reopened.Revision, ready.Revision+1)
	}

	// The stale pre-ready client is now correctly rejected.
	if _, err := repo.MarkReady(ctx, "company_a", created.ID, withLine.Revision,
		time.Now()); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch — the ABA hazard is not closed", err)
	}
}

// MarkReady requires draft; Reopen requires ready. Each is enforced by its own
// filter rather than only by the service.
func TestMongoLifecycleTransitionsRequireTheCorrectSourceStatus(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}

	// Reopen on a DRAFT matches nothing.
	if _, err := repo.Reopen(ctx, "company_a", created.ID, created.Revision,
		time.Now()); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch reopening a draft", err)
	}

	withLine, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision,
		[]rfqs.RFQLine{sampleLine(t, "line_1", "mr_1", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := repo.MarkReady(ctx, "company_a", created.ID, withLine.Revision, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Mark-ready on a READY rfq matches nothing.
	if _, err := repo.MarkReady(ctx, "company_a", created.ID, ready.Revision,
		time.Now()); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch marking a ready rfq ready", err)
	}
}

// --- Lines ---

func TestMongoReplaceLinesIsDraftOnlyAndRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}

	lines := []rfqs.RFQLine{
		sampleLine(t, "line_1", "mr_1", 0),
		sampleLine(t, "line_2", "mr_2", 1),
	}
	got, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, lines)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(got.Lines))
	}
	if got.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want an increment", got.Revision)
	}

	// A stale revision applies nothing.
	if _, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision,
		nil); !errors.Is(err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
	stillTwo, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stillTwo.Lines) != 2 {
		t.Error("a rejected ReplaceLines mutated the lines")
	}
}

// The multikey index serves §7.3/§7.5 line lookup: finding which RFQ chain
// holds a requirement, without scanning.
func TestMongoFindByLineRequirementID(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision,
		[]rfqs.RFQLine{sampleLine(t, "line_1", "mr_1", 0)}); err != nil {
		t.Fatal(err)
	}

	got, found, err := repo.FindByLineRequirementID(ctx, "company_a", "mr_1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.ID != created.ID {
		t.Errorf("FindByLineRequirementID = %+v/%v, want the created rfq", got, found)
	}

	if _, found, err := repo.FindByLineRequirementID(ctx, "company_a", "mr_zzz"); err != nil || found {
		t.Errorf("a missing requirement reported found=%v err=%v", found, err)
	}
	// Tenant scoping.
	if _, found, err := repo.FindByLineRequirementID(ctx, "company_b", "mr_1"); err != nil || found {
		t.Errorf("a foreign company found the line: found=%v err=%v", found, err)
	}
}

// --- Deletion ---

func TestMongoDeleteIsDraftOnlyAndRevisionGuarded(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Delete(ctx, "company_a", created.ID, created.Revision+5); !errors.Is(
		err, rfqs.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); err != nil {
		t.Fatal("a rejected delete removed the document")
	}

	if err := repo.Delete(ctx, "company_a", created.ID, created.Revision); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); !errors.Is(err, rfqs.ErrRFQNotFound) {
		t.Errorf("error = %v, want ErrRFQNotFound after deletion", err)
	}
}

func TestMongoDeleteIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, "company_b", created.ID, created.Revision); !errors.Is(
		err, rfqs.ErrRFQNotFound) {
		t.Fatalf("error = %v, want ErrRFQNotFound", err)
	}
	if _, err := repo.FindByID(ctx, "company_a", created.ID); err != nil {
		t.Error("a foreign-company delete removed the document")
	}
}

// --- Indexes (design spec §12.2) ---

func TestMongoEnsureIndexesCreatesTheNamedIndexes(t *testing.T) {
	db := setupDB(t)
	newRepo(t, db)
	newCounterRepo(t, db)
	ctx := context.Background()

	for _, spec := range []struct {
		collection string
		want       []string
	}{
		{"rfqs", []string{
			"idx_rfqs_company",
			"idx_rfqs_company_project",
			"idx_rfqs_company_project_status",
			"uq_rfqs_company_number",
			"idx_rfqs_company_line_requirement",
		}},
		{"rfq_counters", []string{"uq_rfq_counters_company"}},
	} {
		cursor, err := db.Collection(spec.collection).Indexes().List(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var idx []struct {
			Name string `bson:"name"`
		}
		if err := cursor.All(ctx, &idx); err != nil {
			t.Fatal(err)
		}
		present := map[string]bool{}
		for _, i := range idx {
			present[i.Name] = true
		}
		for _, want := range spec.want {
			if !present[want] {
				t.Errorf("%s is missing index %q", spec.collection, want)
			}
		}
	}
}

// EnsureIndexes must be safe to run repeatedly — the composition root calls it
// on every boot.
func TestMongoEnsureIndexesIsIdempotent(t *testing.T) {
	db := setupDB(t)
	repo := rfqs.NewMongoRFQRepository(db)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := repo.EnsureIndexes(ctx); err != nil {
			t.Fatalf("EnsureIndexes run %d failed: %v", i+1, err)
		}
	}
}

// The unique number index must be enforced by MongoDB, not merely declared.
func TestMongoUniqueNumberIndexIsEnforcedAtTheDatabase(t *testing.T) {
	db := setupDB(t)
	newRepo(t, db)
	ctx := context.Background()

	doc := bson.M{
		"companyId": "company_a", "projectId": "project_1", "rfqNumber": "RFQ-000001",
		"status": string(rfqs.RFQStatusDraft), "revision": int64(0), "schemaVersion": 1,
		"createdAt": time.Now(), "updatedAt": time.Now(),
	}
	if _, err := db.Collection("rfqs").InsertOne(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection("rfqs").InsertOne(ctx, doc); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("error = %v, want a duplicate-key error from the database itself", err)
	}
}

// TestService_DeleteAllForCompany proves Task 1a's multi-collection
// guidance (spec §6.6): ONE Service.DeleteAllForCompany call removes BOTH
// RFQ and RFQCounter records for companyID. Seeds via both repositories
// directly (matching every sibling test in this file).
func TestService_DeleteAllForCompany(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	counterRepo := newCounterRepo(t, db)
	svc := rfqs.NewService(repo, counterRepo, nil, nil, nil, nil)
	ctx := context.Background()

	if _, err := repo.Create(ctx, draftRFQ("company_a", "project_1", "RFQ-000001")); err != nil {
		t.Fatalf("create company_a rfq: %v", err)
	}
	if _, err := repo.Create(ctx, draftRFQ("company_b", "project_2", "RFQ-000001")); err != nil {
		t.Fatalf("create company_b rfq: %v", err)
	}
	if _, err := counterRepo.NextRFQNumber(ctx, "company_a"); err != nil {
		t.Fatalf("allocate company_a counter: %v", err)
	}
	if _, err := counterRepo.NextRFQNumber(ctx, "company_b"); err != nil {
		t.Fatalf("allocate company_b counter: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("DeleteAllForCompany: %v", err)
	}

	listA, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("ListByProject company_a: %v", err)
	}
	if len(listA) != 0 {
		t.Fatalf("expected 0 remaining company_a rfqs, got %d", len(listA))
	}
	numA, err := counterRepo.NextRFQNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("NextRFQNumber company_a after delete: %v", err)
	}
	if numA != 1 {
		t.Fatalf("expected company_a's counter to restart at 1 after DeleteAllForCompany, got %d", numA)
	}

	listB, err := repo.ListByProject(ctx, "company_b", "project_2")
	if err != nil {
		t.Fatalf("ListByProject company_b: %v", err)
	}
	if len(listB) != 1 {
		t.Fatalf("expected company_b's rfq to be untouched, got %d", len(listB))
	}
}

func TestService_DeleteAllForCompany_EmptyCompanyIsANoOp(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	counterRepo := newCounterRepo(t, db)
	svc := rfqs.NewService(repo, counterRepo, nil, nil, nil, nil)
	ctx := context.Background()

	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany on an empty company should succeed, got: %v", err)
	}
	if err := svc.DeleteAllForCompany(ctx, "company_never_existed"); err != nil {
		t.Fatalf("DeleteAllForCompany called TWICE on an empty company should still succeed, got: %v", err)
	}
}
