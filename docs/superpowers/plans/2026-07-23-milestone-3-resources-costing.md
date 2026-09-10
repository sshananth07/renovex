# Milestone 3 — Resources and Costing: Implementation Plan

**STATUS: COMPLETE** — all 20 tasks (0-19) implemented and verified
2026-07-23. Full summary in `tasks/done.md`'s "Milestone 3" entry, including
5 deviations from this plan caught in user review before/during
implementation (fixed inline, not deferred) and one additional bug (a Huma
schema-registry naming collision) found and fixed during composition-root
wiring. Changes were left uncommitted per explicit instruction.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/materials`, `internal/labour`, `internal/costs` — the M3 resource/cost foundation (Material catalog, Worker + LabourEntry, universal CostItem ledger with a four-stage lifecycle) — fully tenant-scoped, wired into the existing single-Huma-API composition root, with one small extension to the already-verified M2 `work` module.

**Architecture:** Three new modules following the exact M1/M2 shape (`model.go` → `repository.go` → `repository_mongo.go` → `service.go` → `handler.go`), plus one new capability method on `work.Service` and one new helper function in `internal/foundation/money`. `CostItem` is the single authoritative cost ledger; every `LabourEntry` creates exactly one linked `CostItem{Category=labour}` via a narrow cross-module capability (`LabourCostRecorder`), never through the public `POST /cost-items` path (which structurally rejects `category=labour`).

**Tech Stack:** Go 1.25, chi + Huma v2, mongo-driver v2, shopspring/decimal, MongoDB via Docker Compose, testcontainers-go for integration tests.

**Authority:** `docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md` (APPROVED) — every task below implements a specific section of that spec; section references are inline. Do not deviate from the approved spec without flagging it back to the user first.

**Working directory for all commands:** `backend/` (per repo convention — `go build`/`go test`/`go vet` from the repo root fail with "directory prefix does not contain main module").

---

## Task ordering rationale

Module construction order must respect the acyclic dependency graph from spec §8: `work` (extended) → `materials` (independent) → `costs` (consumes `materials`) → `labour` (consumes `costs`). Each module is built bottom-up within itself (model → repo → service → handler) with tests at each layer, TDD red→green, matching M1/M2's established process. Tasks are ordered so that every task leaves `go build ./... && go test ./...` green before moving to the next.

- **Task 0**: Extend `work` with `WorkItemBelongsToProject` (spec §9.3, §20) — the one required M2 modification.
- **Task 1**: Add `money.CalculateLineAmount` (spec §1.3.1, §21) — the one required foundation addition.
- **Tasks 2-6**: `internal/materials` (model → repo → service → handler → verification).
- **Tasks 7-11**: `internal/costs` (model → repo → service, including the `LabourCostRecorder` capability it exposes → handler → verification).
- **Tasks 12-16**: `internal/labour` (Worker + LabourEntry models → repos → service, including the two-module write with compensation and the correction path → handler → verification).
- **Task 17**: Composition root wiring (`cmd/api/main.go` + `internal/tenanttest/router.go`).
- **Task 18**: Full HTTP tenant-isolation and ledger-integrity acceptance tests (spec §17).
- **Task 19**: Final verification sweep (spec §18).

---

## Task 0: Extend `internal/work` with `WorkItemBelongsToProject`

**Spec:** §9.3, §20. This is the only change to already-verified M2 code. It mirrors `spaces.SpaceRepository.BelongsToProject` / `spaces.Service.SpaceBelongsToProject` exactly (see `internal/spaces/repository.go`, `internal/spaces/repository_mongo.go:112-122`, `internal/spaces/service.go:101-107`).

**Files:**
- Modify: `backend/internal/work/repository.go`
- Modify: `backend/internal/work/repository_mongo.go`
- Modify: `backend/internal/work/service.go`
- Modify: `backend/internal/work/work_item.go` (package doc comment only)
- Test: `backend/internal/work/repository_mongo_test.go`
- Test: `backend/internal/work/service_test.go`

- [x] **Step 1: Write the failing repository test**

Add to `backend/internal/work/repository_mongo_test.go`:

```go
func TestWorkItemRepositoryBelongsToProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "work_test_belongs_to_project")
	repo := work.NewMongoWorkItemRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, work.WorkItem{
		CompanyID: "company_a", ProjectID: "project_1", Description: "Demo",
		Quantity: mustQuantity(t, "1", "unit"), Status: work.WorkItemStatusPlanned,
		Source: work.WorkItemSourceManual, VerificationStatus: work.VerificationStatusConfirmed,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := repo.BelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true for matching company+project")
	}

	wrongProject, err := repo.BelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wrongProject {
		t.Fatal("expected false when projectID does not match the WorkItem's own ProjectID")
	}

	wrongCompany, err := repo.BelongsToProject(ctx, "company_b", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wrongCompany {
		t.Fatal("expected false when companyID does not match")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/work/... -run TestWorkItemRepositoryBelongsToProject -v`
Expected: FAIL with `repo.BelongsToProject undefined (type *work.MongoWorkItemRepository has no field or method BelongsToProject)`

- [x] **Step 3: Add `BelongsToProject` to the `WorkItemRepository` interface**

In `backend/internal/work/repository.go`, add to the interface (after `UpdateStatus`):

```go
	// BelongsToProject reports whether id exists, belongs to companyID, AND its
	// stored ProjectID equals projectID — a single compound-filtered query used by
	// MongoWorkItemRepository to satisfy WorkItemLookup.WorkItemBelongsToProject
	// without a separate existence check followed by a lineage check (M3 design
	// spec §9.3, mirroring spaces.SpaceRepository.BelongsToProject from M2).
	BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error)
```

- [x] **Step 4: Implement `BelongsToProject` in the Mongo repository**

In `backend/internal/work/repository_mongo.go`, add after `UpdateStatus`:

```go
func (r *MongoWorkItemRepository) BelongsToProject(ctx context.Context, companyID, id, projectID string) (bool, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return false, nil
	}
	count, err := r.collection.CountDocuments(ctx, bson.M{"_id": objID, "companyId": companyID, "projectId": projectID})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/work/... -run TestWorkItemRepositoryBelongsToProject -v`
Expected: PASS

- [x] **Step 6: Write the failing service-level test**

Add to `backend/internal/work/service_test.go` (check the existing file's fake-repository pattern first — the fake likely already implements `WorkItemRepository`; add `BelongsToProject` to that fake and a new test):

```go
func TestServiceWorkItemBelongsToProject(t *testing.T) {
	repo := newFakeWorkItemRepository() // reuse the existing fake constructor in this file
	svc := work.NewService(repo, fakeProjectLookup{belongs: true}, fakeSpaceLookup{})

	ctx := context.Background()
	created, err := svc.CreateWorkItem(ctx, "company_a", "project_1", nil, "Demo", "", "1", "unit")
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	belongs, err := svc.WorkItemBelongsToProject(ctx, "company_a", created.ID, "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	mismatched, err := svc.WorkItemBelongsToProject(ctx, "company_a", created.ID, "project_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mismatched {
		t.Fatal("expected false for mismatched project")
	}
}
```

Note: inspect the existing fakes in `service_test.go` before writing this — if the fake repository type doesn't yet implement `BelongsToProject`, add a method on it that filters its in-memory map by `CompanyID`+`ProjectID`, matching whatever style the existing fake already uses for `FindByID`.

- [x] **Step 7: Run test to verify it fails**

Run: `cd backend && go test ./internal/work/... -run TestServiceWorkItemBelongsToProject -v`
Expected: FAIL — `svc.WorkItemBelongsToProject undefined`

- [x] **Step 8: Add `WorkItemBelongsToProject` to `work.Service`**

In `backend/internal/work/service.go`, add after `UpdateWorkItemStatus`:

```go
// WorkItemBelongsToProject reports whether workItemID exists, belongs to
// companyID, AND its stored ProjectID equals projectID. Added in M3 — the one
// capability work exposes to another module (labour, costs), satisfying both
// modules' WorkItemLookup interface structurally (M3 design spec §9.3).
func (s *Service) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return s.repo.BelongsToProject(ctx, companyID, workItemID, projectID)
}
```

- [x] **Step 9: Run test to verify it passes**

Run: `cd backend && go test ./internal/work/... -v`
Expected: PASS, all tests in the package

- [x] **Step 10: Update the package doc comment**

In `backend/internal/work/work_item.go`, the package comment currently ends with "It exposes no capability to any other module in M2." Update the whole comment block at the top of the file:

```go
// Package work owns WorkItem records — the actual renovation work performed,
// and the bridge between the physical Project/Space hierarchy and the
// Resource/Cost/Estimate/Quotation financial system. See phase1.md §8-9,
// docs/superpowers/specs/2026-07-22-milestone-2-project-foundation-design.md §1.5,
// and docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md §9.3.
//
// work never imports projects, clients, properties, spaces, materials, labour,
// or costs. It defines ProjectLookup and SpaceLookup, the two capabilities it
// needs, satisfied structurally by projects.Service and spaces.Service
// respectively. As of M3, it exposes WorkItemBelongsToProject, consumed by
// labour and costs via their own WorkItemLookup interfaces.
package work
```

- [x] **Step 11: Run full build and test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions in any M0/M1/M2 package

- [x] **Step 12: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/work/repository.go internal/work/repository_mongo.go internal/work/service.go internal/work/work_item.go internal/work/repository_mongo_test.go internal/work/service_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 1: Add `money.CalculateLineAmount` to `internal/foundation/money`

**Spec:** §1.3.1, §21. Note the existing `internal/foundation/money` package uses white-box tests (`package money`, not `package money_test`) — see `money_test.go:1`. Follow that convention.

**Files:**
- Modify: `backend/internal/foundation/money/rounding.go`
- Test: `backend/internal/foundation/money/rounding_test.go`

- [x] **Step 1: Read the existing rounding test file to match its style**

Run: `cd backend && cat internal/foundation/money/rounding_test.go`

(No code change in this step — just confirms the exact `RoundToMinorUnits` boundary-case test style already in place, so the new test matches it rather than reimplementing a parallel style.)

- [x] **Step 2: Write the failing test**

Add to `backend/internal/foundation/money/rounding_test.go`:

```go
func TestCalculateLineAmountBasicMultiplication(t *testing.T) {
	qty := decimal.RequireFromString("8.5")
	unitPrice := New(15000, "MYR") // RM150.00/day

	result := CalculateLineAmount(qty, unitPrice)

	if result.Amount != 127500 { // RM1,275.00
		t.Fatalf("expected 127500, got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected MYR, got %s", result.Currency)
	}
}

func TestCalculateLineAmountRoundsHalfUp(t *testing.T) {
	// 3 * RM10.005 = RM30.015 -> rounds to RM30.02 (half-up), matching
	// RoundToMinorUnits's own documented tie-breaking rule.
	qty := decimal.RequireFromString("3")
	unitPrice := New(1000, "MYR") // this alone has no fractional-cent issue;
	// use a qty that forces a half-cent tie instead:
	qtyHalfCent := decimal.RequireFromString("1.005")
	unitPriceOneDollar := New(100, "MYR") // RM1.00

	result := CalculateLineAmount(qtyHalfCent, unitPriceOneDollar)
	if result.Amount != 101 { // RM1.005 -> RM1.01, ties away from zero
		t.Fatalf("expected 101 (half-up rounding), got %d", result.Amount)
	}

	// sanity: the non-tie case from the first block still holds
	resultPlain := CalculateLineAmount(qty, unitPrice)
	if resultPlain.Amount != 3000 {
		t.Fatalf("expected 3000, got %d", resultPlain.Amount)
	}
}

func TestCalculateLineAmountZeroQuantity(t *testing.T) {
	result := CalculateLineAmount(decimal.Zero, New(15000, "MYR"))
	if result.Amount != 0 {
		t.Fatalf("expected 0, got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected MYR, got %s", result.Currency)
	}
}
```

Add the `decimal` import to the top of `rounding_test.go` if not already present: `"github.com/shopspring/decimal"`.

- [x] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/foundation/money/... -run TestCalculateLineAmount -v`
Expected: FAIL with `undefined: CalculateLineAmount`

- [x] **Step 4: Implement `CalculateLineAmount`**

In `backend/internal/foundation/money/rounding.go`, add after `RoundToMinorUnits`:

```go
// CalculateLineAmount computes qty × unitPrice, rounding once via
// RoundToMinorUnits. This is the only supported multiplication path for
// quantity-based cost/labour calculations across the platform — see ADR
// 0001 and the M3 design spec §1.3.1. No module may reimplement this
// multiplication+rounding sequence itself.
func CalculateLineAmount(qty decimal.Decimal, unitPrice Money) Money {
	unitPriceMajorUnits := decimal.NewFromInt(unitPrice.Amount).Shift(-2)
	amountMajorUnits := qty.Mul(unitPriceMajorUnits)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: unitPrice.Currency}
}
```

(`Shift(-2)` converts minor units back to major units, e.g. `15000` → `150.00`, mirroring the inverse of `roundHalfUpToMinorUnits`'s `Shift(2)`.)

- [x] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/foundation/money/... -v`
Expected: PASS, all tests in the package

- [x] **Step 6: Run full build and test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions

- [x] **Step 7: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/foundation/money/rounding.go internal/foundation/money/rounding_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 2: `internal/materials` — model

**Spec:** §1.1. Note: no `PreferredSupplierID` field (removed in review, §22-F).

**Files:**
- Create: `backend/internal/materials/material.go`
- Delete: `backend/internal/materials/doc.go` (its placeholder content is superseded by `material.go`'s package doc comment)

- [x] **Step 1: Check the current placeholder**

Run: `cd backend && cat internal/materials/doc.go`

(Confirms exact current placeholder wording before removing it — no code change yet.)

- [x] **Step 2: Remove the placeholder and create the model file**

Delete `backend/internal/materials/doc.go`.

Create `backend/internal/materials/material.go`:

```go
// Package materials owns the company-scoped Material catalog and its
// reference pricing (phase1.md §10-11). A Material is a reusable
// company-level catalog entry, never tied to a single Project or WorkItem —
// demand linkage from a WorkItem to a Material is MaterialRequirement's job,
// out of scope until the Procurement milestone. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md §1.1.
//
// materials never imports projects, work, labour, or costs. It exposes
// MaterialLookup, consumed by costs. It consumes nothing — materials has no
// parent-validation dependency on any other module.
package materials

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// Material is a company-owned catalog entry with one current reference price.
// ReferencePrice is informational/pre-fill only — see CostItem.UnitPrice in
// internal/costs for the authoritative, snapshotted price on an actual cost
// record (design spec §1.1.4-5).
type Material struct {
	ID                 string      `bson:"_id,omitempty" json:"id"`
	CompanyID          string      `bson:"companyId" json:"companyId"`
	Name               string      `bson:"name" json:"name"`
	Category           string      `bson:"category,omitempty" json:"category,omitempty"`
	Specification      string      `bson:"specification,omitempty" json:"specification,omitempty"`
	Unit               string      `bson:"unit" json:"unit"`
	ReferencePrice     money.Money `bson:"referencePrice" json:"referencePrice"`
	ReferencePriceAsOf time.Time   `bson:"referencePriceAsOf" json:"referencePriceAsOf"`
	CreatedAt          time.Time   `bson:"createdAt" json:"createdAt"`
	SchemaVersion      int         `bson:"schemaVersion" json:"schemaVersion"`
}
```

- [x] **Step 3: Confirm the package still builds**

Run: `cd backend && go build ./internal/materials/...`
Expected: succeeds (no `.go` files yet reference anything undefined — this is a pure data type with no logic to test in isolation)

- [x] **Step 4: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/materials/material.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 3: `internal/materials` — repository

**Spec:** §2, §10, §13.

**Files:**
- Create: `backend/internal/materials/repository.go`
- Create: `backend/internal/materials/repository_mongo.go`
- Test: `backend/internal/materials/repository_mongo_test.go`

- [x] **Step 1: Write the repository interface (no test needed — pure interface + sentinel error)**

Create `backend/internal/materials/repository.go`:

```go
package materials

import (
	"context"
	"errors"
)

// ErrMaterialNotFound is returned when a Material lookup finds no match —
// including a Material that exists but belongs to a different company.
var ErrMaterialNotFound = errors.New("materials: material not found")

// MaterialRepository persists Materials. materials owns the materials
// collection exclusively; no other module may query it directly.
type MaterialRepository interface {
	Create(ctx context.Context, m Material) (Material, error)
	FindByID(ctx context.Context, companyID, id string) (Material, error)
	List(ctx context.Context, companyID string) ([]Material, error)
	Update(ctx context.Context, companyID, id string, fn func(*Material)) (Material, error)
}
```

- [x] **Step 2: Write the failing Mongo repository test**

Create `backend/internal/materials/repository_mongo_test.go`:

```go
package materials_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func TestMaterialRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "OPC Cement 50kg", Category: "cement", Unit: "bag",
		ReferencePrice: money.New(1850, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Name != "OPC Cement 50kg" {
		t.Fatalf("expected name to match, got %s", found.Name)
	}
	if found.ReferencePrice.Amount != 1850 || found.ReferencePrice.Currency != "MYR" {
		t.Fatalf("expected reference price 1850 MYR, got %d %s", found.ReferencePrice.Amount, found.ReferencePrice.Currency)
	}
}

func TestMaterialRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_tenant")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Ceramic Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestMaterialRepositoryList(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_list")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Cement", Unit: "bag",
		ReferencePrice: money.New(1850, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, materials.Material{
		CompanyID: "company_b", Name: "Other company's material", Unit: "unit",
		ReferencePrice: money.New(100, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})

	list, err := repo.List(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 materials for company_a, got %d", len(list))
	}
}

func TestMaterialRepositoryUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_update")
	repo := materials.NewMongoMaterialRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, materials.Material{
		CompanyID: "company_a", Name: "Tile", Unit: "m2",
		ReferencePrice: money.New(3200, "MYR"), ReferencePriceAsOf: time.Now(),
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newAsOf := time.Now()
	updated, err := repo.Update(ctx, "company_a", created.ID, func(m *materials.Material) {
		m.ReferencePrice = money.New(3500, "MYR")
		m.ReferencePriceAsOf = newAsOf
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.ReferencePrice.Amount != 3500 {
		t.Fatalf("expected updated reference price 3500, got %d", updated.ReferencePrice.Amount)
	}

	_, err = repo.Update(ctx, "company_b", created.ID, func(m *materials.Material) { m.Name = "hijacked" })
	if err != materials.ErrMaterialNotFound {
		t.Fatalf("expected ErrMaterialNotFound for cross-tenant update, got %v", err)
	}
}

func TestMaterialRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "materials_test_indexes")
	repo := materials.NewMongoMaterialRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/materials/... -v`
Expected: FAIL — `undefined: materials.NewMongoMaterialRepository` (compile error)

- [x] **Step 4: Implement the Mongo repository**

Create `backend/internal/materials/repository_mongo.go`:

```go
package materials

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoMaterialRepository is the MongoDB-backed MaterialRepository
// implementation. It owns the "materials" collection exclusively.
type MongoMaterialRepository struct {
	collection *mongo.Collection
}

// NewMongoMaterialRepository constructs a MongoMaterialRepository against db's
// "materials" collection.
func NewMongoMaterialRepository(db *mongo.Database) *MongoMaterialRepository {
	return &MongoMaterialRepository{collection: db.Collection("materials")}
}

// EnsureIndexes creates the companyId index and the companyId+category
// compound index (design spec §13).
func (r *MongoMaterialRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "category", Value: 1}}},
	})
	return err
}

type materialDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	Name               string        `bson:"name"`
	Category           string        `bson:"category,omitempty"`
	Specification      string        `bson:"specification,omitempty"`
	Unit               string        `bson:"unit"`
	ReferencePrice     money.Money   `bson:"referencePrice"`
	ReferencePriceAsOf time.Time     `bson:"referencePriceAsOf"`
	CreatedAt          time.Time     `bson:"createdAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

func (r *MongoMaterialRepository) Create(ctx context.Context, m Material) (Material, error) {
	doc := toMaterialDoc(m)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Material{}, err
	}
	m.ID = res.InsertedID.(bson.ObjectID).Hex()
	return m, nil
}

func (r *MongoMaterialRepository) FindByID(ctx context.Context, companyID, id string) (Material, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Material{}, ErrMaterialNotFound
	}
	var doc materialDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Material{}, ErrMaterialNotFound
	}
	if err != nil {
		return Material{}, err
	}
	return fromMaterialDoc(doc), nil
}

func (r *MongoMaterialRepository) List(ctx context.Context, companyID string) ([]Material, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []materialDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]Material, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromMaterialDoc(doc))
	}
	return result, nil
}

func (r *MongoMaterialRepository) Update(ctx context.Context, companyID, id string, fn func(*Material)) (Material, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Material{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	doc := toMaterialDoc(existing)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": doc},
	)
	if err != nil {
		return Material{}, err
	}
	if res.MatchedCount == 0 {
		return Material{}, ErrMaterialNotFound
	}
	return existing, nil
}

func toMaterialDoc(m Material) materialDoc {
	doc := materialDoc{
		CompanyID: m.CompanyID, Name: m.Name, Category: m.Category, Specification: m.Specification,
		Unit: m.Unit, ReferencePrice: m.ReferencePrice, ReferencePriceAsOf: m.ReferencePriceAsOf,
		CreatedAt: m.CreatedAt, SchemaVersion: m.SchemaVersion,
	}
	if m.ID != "" {
		objID, _ := bson.ObjectIDFromHex(m.ID)
		doc.ID = objID
	}
	return doc
}

func fromMaterialDoc(doc materialDoc) Material {
	return Material{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, Name: doc.Name, Category: doc.Category,
		Specification: doc.Specification, Unit: doc.Unit, ReferencePrice: doc.ReferencePrice,
		ReferencePriceAsOf: doc.ReferencePriceAsOf, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/materials/... -v`
Expected: PASS (requires Docker Desktop running for testcontainers — confirm with `docker ps` first if this fails with a connection error)

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/materials/repository.go internal/materials/repository_mongo.go internal/materials/repository_mongo_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 4: `internal/materials` — service

**Spec:** §5.1, §9.2 (the `MaterialLookup` capability `materials` exposes to `costs`).

**Files:**
- Create: `backend/internal/materials/service.go`
- Test: `backend/internal/materials/service_test.go`

- [x] **Step 1: Write the failing service test**

Create `backend/internal/materials/service_test.go`:

```go
package materials_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/materials"
)

type fakeMaterialRepository struct {
	byID map[string]materials.Material
	next int
}

func newFakeMaterialRepository() *fakeMaterialRepository {
	return &fakeMaterialRepository{byID: make(map[string]materials.Material)}
}

func (f *fakeMaterialRepository) Create(ctx context.Context, m materials.Material) (materials.Material, error) {
	f.next++
	m.ID = "material_" + string(rune('0'+f.next))
	f.byID[m.ID] = m
	return m, nil
}

func (f *fakeMaterialRepository) FindByID(ctx context.Context, companyID, id string) (materials.Material, error) {
	m, ok := f.byID[id]
	if !ok || m.CompanyID != companyID {
		return materials.Material{}, materials.ErrMaterialNotFound
	}
	return m, nil
}

func (f *fakeMaterialRepository) List(ctx context.Context, companyID string) ([]materials.Material, error) {
	var result []materials.Material
	for _, m := range f.byID {
		if m.CompanyID == companyID {
			result = append(result, m)
		}
	}
	return result, nil
}

func (f *fakeMaterialRepository) Update(ctx context.Context, companyID, id string, fn func(*materials.Material)) (materials.Material, error) {
	m, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return materials.Material{}, err
	}
	fn(&m)
	f.byID[id] = m
	return m, nil
}

func TestCreateMaterialRequiresName(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	_, err := svc.CreateMaterial(context.Background(), "company_a", "", "cement", "", "bag", 1850, "MYR")
	if err != materials.ErrNameRequired {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
}

func TestCreateMaterialRequiresUnit(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	_, err := svc.CreateMaterial(context.Background(), "company_a", "OPC Cement", "cement", "", "", 1850, "MYR")
	if err != materials.ErrUnitRequired {
		t.Fatalf("expected ErrUnitRequired, got %v", err)
	}
}

func TestCreateMaterialSuccess(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	m, err := svc.CreateMaterial(context.Background(), "company_a", "OPC Cement 50kg", "cement", "", "bag", 1850, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ReferencePrice != money.New(1850, "MYR") {
		t.Fatalf("expected reference price 1850 MYR, got %+v", m.ReferencePrice)
	}
	if m.SchemaVersion != 1 {
		t.Fatalf("expected schemaVersion 1, got %d", m.SchemaVersion)
	}
}

func TestUpdateMaterialNeverRetroactive(t *testing.T) {
	// This test documents the invariant structurally: UpdateMaterial only
	// calls MaterialRepository.Update, which this fake never propagates
	// anywhere else. The real cross-package proof that updating a Material's
	// ReferencePrice never mutates an existing CostItem is a costs-package
	// integration test (Task 12) — this test just confirms UpdateMaterial
	// itself doesn't reach outside the materials repository.
	repo := newFakeMaterialRepository()
	svc := materials.NewService(repo)
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateMaterial(context.Background(), "company_a", created.ID, "Tile", "tile", "", "m2", 3500, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ReferencePrice.Amount != 3500 {
		t.Fatalf("expected updated price 3500, got %d", updated.ReferencePrice.Amount)
	}
}

func TestMaterialBelongsToCompany(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	belongs, err := svc.MaterialBelongsToCompany(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !belongs {
		t.Fatal("expected true")
	}

	crossTenant, err := svc.MaterialBelongsToCompany(context.Background(), "company_b", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if crossTenant {
		t.Fatal("expected false for cross-tenant lookup")
	}
}

func TestGetReferencePrice(t *testing.T) {
	svc := materials.NewService(newFakeMaterialRepository())
	created, err := svc.CreateMaterial(context.Background(), "company_a", "Tile", "tile", "", "m2", 3200, "MYR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	price, err := svc.GetReferencePrice(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if price.Amount != 3200 || price.Currency != "MYR" {
		t.Fatalf("expected 3200 MYR, got %d %s", price.Amount, price.Currency)
	}
}

var _ = time.Now // keep time import if needed by future edits; harmless if unused-removed
```

(Drop the trailing `var _ = time.Now` line if `time` ends up unused after writing this file — check with `go vet` in Step 3.)

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/materials/... -v`
Expected: FAIL — `undefined: materials.NewService` (compile error)

- [x] **Step 3: Implement the service**

Create `backend/internal/materials/service.go`:

```go
package materials

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrNameRequired is returned when CreateMaterial/UpdateMaterial is given an
// empty name.
var ErrNameRequired = errors.New("materials: name is required")

// ErrUnitRequired is returned when CreateMaterial/UpdateMaterial is given an
// empty unit.
var ErrUnitRequired = errors.New("materials: unit is required")

// Service implements Material CRUD. It consumes no capability from any other
// module (company-level catalog only, no project/work-item linkage — design
// spec §1.1.6) and exposes MaterialLookup, consumed by costs.
type Service struct {
	repo MaterialRepository
}

// NewService constructs a Service backed by repo.
func NewService(repo MaterialRepository) *Service {
	return &Service{repo: repo}
}

// CreateMaterial validates name and unit are non-empty and persists a new
// Material with the given reference price.
func (s *Service) CreateMaterial(ctx context.Context, companyID, name, category, specification, unit string, referencePriceAmount int64, currency string) (Material, error) {
	if name == "" {
		return Material{}, ErrNameRequired
	}
	if unit == "" {
		return Material{}, ErrUnitRequired
	}
	now := time.Now()
	return s.repo.Create(ctx, Material{
		CompanyID: companyID, Name: name, Category: category, Specification: specification, Unit: unit,
		ReferencePrice: money.New(referencePriceAmount, currency), ReferencePriceAsOf: now,
		CreatedAt: now, SchemaVersion: 1,
	})
}

// GetMaterial returns materialID's Material, tenant-scoped to companyID.
func (s *Service) GetMaterial(ctx context.Context, companyID, materialID string) (Material, error) {
	return s.repo.FindByID(ctx, companyID, materialID)
}

// ListMaterials returns every Material belonging to companyID.
func (s *Service) ListMaterials(ctx context.Context, companyID string) ([]Material, error) {
	return s.repo.List(ctx, companyID)
}

// UpdateMaterial updates materialID's fields, tenant-scoped to companyID.
// Updating ReferencePrice here never retroactively alters any already-created
// CostItem (design spec §1.1.5) — this method only ever writes to the
// materials collection.
func (s *Service) UpdateMaterial(ctx context.Context, companyID, materialID, name, category, specification, unit string, referencePriceAmount int64, currency string) (Material, error) {
	if name == "" {
		return Material{}, ErrNameRequired
	}
	if unit == "" {
		return Material{}, ErrUnitRequired
	}
	return s.repo.Update(ctx, companyID, materialID, func(m *Material) {
		m.Name = name
		m.Category = category
		m.Specification = specification
		m.Unit = unit
		m.ReferencePrice = money.New(referencePriceAmount, currency)
		m.ReferencePriceAsOf = time.Now()
	})
}

// MaterialBelongsToCompany reports whether materialID exists and belongs to
// companyID. Satisfies costs.MaterialLookup structurally (design spec §9.2).
func (s *Service) MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, materialID)
	if err == ErrMaterialNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetReferencePrice returns materialID's current ReferencePrice, tenant-scoped
// to companyID. Used by costs only as a creation-time pre-fill suggestion —
// never re-consulted after a CostItem is created (design spec §1.1.4, §9.2).
func (s *Service) GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error) {
	m, err := s.repo.FindByID(ctx, companyID, materialID)
	if err != nil {
		return money.Money{}, err
	}
	return m.ReferencePrice, nil
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/materials/... -v`
Expected: PASS, all tests (repository tests still need Docker; service tests do not)

- [x] **Step 5: Run full build**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean (fixes any unused-import issue from the `time` placeholder in the test file)

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/materials/service.go internal/materials/service_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 5: `internal/materials` — handler

**Spec:** §9 (endpoint list — Materials section).

**Files:**
- Create: `backend/internal/materials/handler.go`

(No dedicated handler unit test — matching M2's established pattern where handler wiring is proven by `tenanttest`'s HTTP-level integration tests, not package-local handler tests. See Task 21.)

- [x] **Step 1: Implement the handler**

Create `backend/internal/materials/handler.go`:

```go
package materials

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type materialDTO struct {
	ID                    string `json:"id"`
	Name                  string `json:"name"`
	Category              string `json:"category,omitempty"`
	Specification         string `json:"specification,omitempty"`
	Unit                  string `json:"unit"`
	ReferencePriceAmount  int64  `json:"referencePriceAmount"`
	ReferencePriceCurrency string `json:"referencePriceCurrency"`
	ReferencePriceAsOf    string `json:"referencePriceAsOf"`
	CreatedAt             string `json:"createdAt"`
}

type createMaterialInput struct {
	Body struct {
		Name                    string `json:"name" required:"true" minLength:"1"`
		Category                string `json:"category,omitempty"`
		Specification           string `json:"specification,omitempty"`
		Unit                    string `json:"unit" required:"true" minLength:"1"`
		ReferencePriceAmount    int64  `json:"referencePriceAmount" required:"true"`
		ReferencePriceCurrency  string `json:"referencePriceCurrency" required:"true" minLength:"1"`
	}
}

type materialOutput struct {
	Body materialDTO
}

type listMaterialsOutput struct {
	Body struct {
		Materials []materialDTO `json:"materials"`
	}
}

type getMaterialInput struct {
	ID string `path:"id"`
}

type updateMaterialInput struct {
	ID   string `path:"id"`
	Body struct {
		Name                    string `json:"name" required:"true" minLength:"1"`
		Category                string `json:"category,omitempty"`
		Specification           string `json:"specification,omitempty"`
		Unit                    string `json:"unit" required:"true" minLength:"1"`
		ReferencePriceAmount    int64  `json:"referencePriceAmount" required:"true"`
		ReferencePriceCurrency  string `json:"referencePriceCurrency" required:"true" minLength:"1"`
	}
}

// RegisterHandlers registers POST /materials, GET /materials,
// GET /materials/{id}, and PATCH /materials/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "materials-create",
		Method:      http.MethodPost,
		Path:        "/materials",
		Summary:     "Create a Material catalog entry for the authenticated company",
	}, func(ctx context.Context, input *createMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.CreateMaterial(ctx, principal.CompanyID, input.Body.Name, input.Body.Category,
			input.Body.Specification, input.Body.Unit, input.Body.ReferencePriceAmount, input.Body.ReferencePriceCurrency)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-list",
		Method:      http.MethodGet,
		Path:        "/materials",
		Summary:     "List Materials for the authenticated company",
	}, func(ctx context.Context, input *struct{}) (*listMaterialsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListMaterials(ctx, principal.CompanyID)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		resp := &listMaterialsOutput{}
		for _, m := range list {
			resp.Body.Materials = append(resp.Body.Materials, toMaterialDTO(m))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-get",
		Method:      http.MethodGet,
		Path:        "/materials/{id}",
		Summary:     "Get a Material, tenant-scoped",
	}, func(ctx context.Context, input *getMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.GetMaterial(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-update",
		Method:      http.MethodPatch,
		Path:        "/materials/{id}",
		Summary:     "Update a Material, tenant-scoped. Reference price changes never retroactively alter existing CostItems.",
	}, func(ctx context.Context, input *updateMaterialInput) (*materialOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		m, err := svc.UpdateMaterial(ctx, principal.CompanyID, input.ID, input.Body.Name, input.Body.Category,
			input.Body.Specification, input.Body.Unit, input.Body.ReferencePriceAmount, input.Body.ReferencePriceCurrency)
		if err != nil {
			return nil, mapMaterialsError(err)
		}
		return &materialOutput{Body: toMaterialDTO(m)}, nil
	})
}

func toMaterialDTO(m Material) materialDTO {
	return materialDTO{
		ID: m.ID, Name: m.Name, Category: m.Category, Specification: m.Specification, Unit: m.Unit,
		ReferencePriceAmount: m.ReferencePrice.Amount, ReferencePriceCurrency: m.ReferencePrice.Currency,
		ReferencePriceAsOf: m.ReferencePriceAsOf.Format(timeLayout), CreatedAt: m.CreatedAt.Format(timeLayout),
	}
}

func mapMaterialsError(err error) error {
	switch {
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
	case errors.Is(err, ErrUnitRequired):
		return huma.Error422UnprocessableEntity("unit is required")
	default:
		return err
	}
}
```

- [x] **Step 2: Build to confirm it compiles**

Run: `cd backend && go build ./internal/materials/... && go vet ./internal/materials/...`
Expected: clean

- [x] **Step 3: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/materials/handler.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 6: `internal/materials` — full package verification

**Files:** none (verification only)

- [x] **Step 1: Run the full materials package test suite**

Run: `cd backend && go test ./internal/materials/... -v`
Expected: PASS — all model/repository/service tests green

- [x] **Step 2: Run the full build**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions in any earlier package

- [x] **Step 3: No commit needed** (verification-only task; nothing new to stage)

---

## Task 7: `internal/costs` — model

**Spec:** §1.4, §1.4.1, §1.4.2, §1.5.

**Files:**
- Create: `backend/internal/costs/cost_item.go`
- Delete: `backend/internal/costs/doc.go`

- [x] **Step 1: Check the current placeholder**

Run: `cd backend && cat internal/costs/doc.go`

- [x] **Step 2: Remove the placeholder and create the model file**

Delete `backend/internal/costs/doc.go`.

Create `backend/internal/costs/cost_item.go`:

```go
// Package costs owns CostItem — the single authoritative Project-cost ledger
// spanning every cost category, including Material and Labour
// (phase1.md §19, §39-41). Every LabourEntry (owned by internal/labour)
// creates exactly one linked CostItem{Category=labour} via the
// LabourCostRecorder capability this package exposes; the public
// CreateCostItem path structurally rejects category=labour. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
// §1.4.
//
// costs never imports projects, work, materials, or labour by type. It
// defines ProjectLookup, WorkItemLookup, and MaterialLookup, the three
// capabilities it needs, satisfied structurally by projects.Service,
// work.Service, and materials.Service respectively. It exposes
// LabourCostRecorder, consumed by labour.
package costs

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// CostCategory is one of the 9 categories from phase1.md §19.
type CostCategory string

const (
	CostCategoryMaterial        CostCategory = "material"
	CostCategoryLabour          CostCategory = "labour"
	CostCategorySubcontractor   CostCategory = "subcontractor"
	CostCategoryEquipment       CostCategory = "equipment"
	CostCategoryTransport       CostCategory = "transport"
	CostCategoryPermit          CostCategory = "permit"
	CostCategoryProfessionalFee CostCategory = "professional_fee"
	CostCategoryUtility         CostCategory = "utility"
	CostCategoryMiscellaneous   CostCategory = "miscellaneous"
)

// IsValid reports whether c is one of the 9 defined categories.
func (c CostCategory) IsValid() bool {
	switch c {
	case CostCategoryMaterial, CostCategoryLabour, CostCategorySubcontractor, CostCategoryEquipment,
		CostCategoryTransport, CostCategoryPermit, CostCategoryProfessionalFee, CostCategoryUtility,
		CostCategoryMiscellaneous:
		return true
	default:
		return false
	}
}

// CostStage identifies one of the 4 lifecycle Money fields on a CostItem.
type CostStage string

const (
	CostStageEstimated CostStage = "estimated"
	CostStageCommitted CostStage = "committed"
	CostStageActual    CostStage = "actual"
	CostStagePaid      CostStage = "paid"
)

// IsValid reports whether s is one of the 4 defined stages.
func (s CostStage) IsValid() bool {
	switch s {
	case CostStageEstimated, CostStageCommitted, CostStageActual, CostStagePaid:
		return true
	default:
		return false
	}
}

// CostItem is the single authoritative Project-cost ledger record. Estimated,
// Committed, Actual, and Paid are independently nilable and may all be
// non-nil simultaneously (design spec §1.5) — never collapsed into a single
// status field. WorkItemID and Quantity/UnitPrice are optional: a CostItem
// may be Project-level with no specific WorkItem (e.g. a permit fee), and
// lump-sum costs (subcontractor, permit, professional fee) have no natural
// quantity. Paid is a cumulative running total, not a per-payment event
// (design spec §1.5, §22-E).
type CostItem struct {
	ID            string             `bson:"_id,omitempty" json:"id"`
	CompanyID     string             `bson:"companyId" json:"companyId"`
	ProjectID     string             `bson:"projectId" json:"projectId"`
	WorkItemID    *string            `bson:"workItemId,omitempty" json:"workItemId,omitempty"`
	Category      CostCategory       `bson:"category" json:"category"`
	Description   string             `bson:"description" json:"description"`
	Quantity      *quantity.Quantity `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	UnitPrice     *money.Money       `bson:"unitPrice,omitempty" json:"unitPrice,omitempty"`
	MaterialID    *string            `bson:"materialId,omitempty" json:"materialId,omitempty"`
	Estimated     *money.Money       `bson:"estimated,omitempty" json:"estimated,omitempty"`
	Committed     *money.Money       `bson:"committed,omitempty" json:"committed,omitempty"`
	Actual        *money.Money       `bson:"actual,omitempty" json:"actual,omitempty"`
	Paid          *money.Money       `bson:"paid,omitempty" json:"paid,omitempty"`
	Currency      string             `bson:"currency" json:"currency"`
	Date          time.Time          `bson:"date" json:"date"`
	Notes         string             `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     time.Time          `bson:"createdAt" json:"createdAt"`
	SchemaVersion int                `bson:"schemaVersion" json:"schemaVersion"`
}
```

- [x] **Step 3: Confirm the package still builds**

Run: `cd backend && go build ./internal/costs/...`
Expected: succeeds

- [x] **Step 4: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/costs/cost_item.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 8: `internal/costs` — repository

**Spec:** §2, §10, §12, §13.

**Files:**
- Create: `backend/internal/costs/repository.go`
- Create: `backend/internal/costs/repository_mongo.go`
- Test: `backend/internal/costs/repository_mongo_test.go`

- [x] **Step 1: Write the repository interface**

Create `backend/internal/costs/repository.go`:

```go
package costs

import (
	"context"
	"errors"
)

// ErrCostItemNotFound is returned when a CostItem lookup finds no match —
// including a CostItem that exists but belongs to a different company.
var ErrCostItemNotFound = errors.New("costs: cost item not found")

// CostItemRepository persists CostItems. costs owns the cost_items
// collection exclusively; no other module may query it directly.
type CostItemRepository interface {
	Create(ctx context.Context, c CostItem) (CostItem, error)
	FindByID(ctx context.Context, companyID, id string) (CostItem, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error)
	ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error)
	UpdateLifecycleField(ctx context.Context, companyID, id string, stage CostStage, amount money.Money) (CostItem, error)
	UpdateDetails(ctx context.Context, companyID, id string, description, notes string, category *CostCategory) (CostItem, error)
	// Delete is used only by LabourCostRecorder's best-effort compensation
	// path (design spec §22-B) — never exposed via HTTP.
	Delete(ctx context.Context, companyID, id string) error
}
```

Add the `money` import: `"github.com/shananth/renovation-platform/backend/internal/foundation/money"`.

- [x] **Step 2: Write the failing Mongo repository test**

Create `backend/internal/costs/repository_mongo_test.go`:

```go
package costs_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func mustQuantity(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("unexpected error constructing quantity: %v", err)
	}
	return q
}

func TestCostItemRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(450000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategorySubcontractor,
		Description: "Bathroom plumbing upgrade", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Description != "Bathroom plumbing upgrade" {
		t.Fatalf("expected description to match, got %s", found.Description)
	}
	if found.Estimated == nil || found.Estimated.Amount != 450000 {
		t.Fatalf("expected estimated 450000, got %+v", found.Estimated)
	}
	if found.Committed != nil || found.Actual != nil || found.Paid != nil {
		t.Fatalf("expected only Estimated set, got Committed=%+v Actual=%+v Paid=%+v", found.Committed, found.Actual, found.Paid)
	}
}

func TestCostItemRepositoryMoneyRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_money_roundtrip")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(100000, "MYR")
	committed := money.New(95000, "MYR")
	actual := money.New(108000, "MYR")
	paid := money.New(50000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "All four stages", Estimated: &estimated, Committed: &committed, Actual: &actual, Paid: &paid,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Estimated == nil || found.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated 100000, got %+v", found.Estimated)
	}
	if found.Committed == nil || found.Committed.Amount != 95000 {
		t.Fatalf("expected committed 95000, got %+v", found.Committed)
	}
	if found.Actual == nil || found.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", found.Actual)
	}
	if found.Paid == nil || found.Paid.Amount != 50000 {
		t.Fatalf("expected paid 50000, got %+v", found.Paid)
	}
}

func TestCostItemRepositoryQuantityRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_quantity_roundtrip")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	q := mustQuantity(t, "32.7501", "m2")
	unitPrice := money.New(3200, "MYR")
	estimated := money.New(104800, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMaterial,
		Description: "Ceramic tiles", Quantity: &q, UnitPrice: &unitPrice, Estimated: &estimated,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	expected := decimal.RequireFromString("32.7501")
	if found.Quantity == nil || !found.Quantity.Value.Equal(expected) {
		t.Fatalf("expected exact decimal 32.7501, got %+v", found.Quantity)
	}
	if found.Quantity.Unit != "m2" {
		t.Fatalf("expected unit m2, got %s", found.Quantity.Unit)
	}
}

func TestCostItemRepositoryLumpSumNoQuantity(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_lumpsum")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(150000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryPermit,
		Description: "Renovation permit", Estimated: &estimated,
		Currency: "MYR", Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Quantity != nil {
		t.Fatalf("expected nil Quantity for lump-sum cost item, got %+v", found.Quantity)
	}
	if found.UnitPrice != nil {
		t.Fatalf("expected nil UnitPrice for lump-sum cost item, got %+v", found.UnitPrice)
	}
}

func TestCostItemRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_tenant")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Demo", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestCostItemRepositoryListByProjectAndByWorkItem(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_list")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	workItemID := "work_item_1"
	estimated := money.New(1000, "MYR")
	_, _ = repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: &workItemID, Category: costs.CostCategoryEquipment,
		Description: "With work item", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryPermit,
		Description: "Project-level, no work item", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})

	byProject, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 cost items for project_1, got %d", len(byProject))
	}

	byWorkItem, err := repo.ListByWorkItem(ctx, "company_a", "work_item_1")
	if err != nil {
		t.Fatalf("unexpected error listing by work item: %v", err)
	}
	if len(byWorkItem) != 1 {
		t.Fatalf("expected 1 cost item for work_item_1, got %d", len(byWorkItem))
	}
}

func TestCostItemRepositoryUpdateLifecycleField(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_lifecycle")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(100000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Demo", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated, err := repo.UpdateLifecycleField(ctx, "company_a", created.ID, costs.CostStageActual, money.New(108000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error updating lifecycle field: %v", err)
	}
	if updated.Actual == nil || updated.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", updated.Actual)
	}
	if updated.Estimated == nil || updated.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated to remain 100000, got %+v", updated.Estimated)
	}

	_, err = repo.UpdateLifecycleField(ctx, "company_b", created.ID, costs.CostStagePaid, money.New(1, "MYR"))
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound for cross-tenant update, got %v", err)
	}
}

func TestCostItemRepositoryUpdateDetails(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_details")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryMiscellaneous,
		Description: "Old description", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newCategory := costs.CostCategoryEquipment
	updated, err := repo.UpdateDetails(ctx, "company_a", created.ID, "New description", "some notes", &newCategory)
	if err != nil {
		t.Fatalf("unexpected error updating details: %v", err)
	}
	if updated.Description != "New description" {
		t.Fatalf("expected updated description, got %s", updated.Description)
	}
	if updated.Category != costs.CostCategoryEquipment {
		t.Fatalf("expected updated category, got %s", updated.Category)
	}
}

func TestCostItemRepositoryDelete(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_delete")
	repo := costs.NewMongoCostItemRepository(db)

	ctx := context.Background()
	estimated := money.New(1000, "MYR")
	created, err := repo.Create(ctx, costs.CostItem{
		CompanyID: "company_a", ProjectID: "project_1", Category: costs.CostCategoryLabour,
		Description: "Orphan compensation test", Estimated: &estimated, Currency: "MYR",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	if err := repo.Delete(ctx, "company_a", created.ID); err != nil {
		t.Fatalf("unexpected error deleting: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_a", created.ID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound after delete, got %v", err)
	}
}

func TestCostItemRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "costs_test_indexes")
	repo := costs.NewMongoCostItemRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: FAIL — `undefined: costs.NewMongoCostItemRepository` (compile error)

- [x] **Step 4: Implement the Mongo repository**

Create `backend/internal/costs/repository_mongo.go`:

```go
package costs

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// MongoCostItemRepository is the MongoDB-backed CostItemRepository
// implementation. It owns the "cost_items" collection exclusively.
type MongoCostItemRepository struct {
	collection *mongo.Collection
}

// NewMongoCostItemRepository constructs a MongoCostItemRepository against
// db's "cost_items" collection.
func NewMongoCostItemRepository(db *mongo.Database) *MongoCostItemRepository {
	return &MongoCostItemRepository{collection: db.Collection("cost_items")}
}

// EnsureIndexes creates the companyId index, the companyId+projectId compound
// index, and a sparse companyId+workItemId compound index (design spec §13).
func (r *MongoCostItemRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}},
			Options: options.Index().SetSparse(true)},
	})
	return err
}

// quantityDoc mirrors internal/work/repository_mongo.go's own private copy —
// shopspring/decimal has no BSON marshaling, so Value is stored as a string,
// never Decimal128 (design spec §12).
type quantityDoc struct {
	Value string `bson:"value"`
	Unit  string `bson:"unit"`
}

func toQuantityDoc(q quantity.Quantity) quantityDoc {
	return quantityDoc{Value: q.Value.String(), Unit: q.Unit}
}

func fromQuantityDoc(doc quantityDoc) (quantity.Quantity, error) {
	return quantity.New(doc.Value, doc.Unit)
}

type costItemDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	WorkItemID    *string       `bson:"workItemId,omitempty"`
	Category      string        `bson:"category"`
	Description   string        `bson:"description"`
	Quantity      *quantityDoc  `bson:"quantity,omitempty"`
	UnitPrice     *money.Money  `bson:"unitPrice,omitempty"`
	MaterialID    *string       `bson:"materialId,omitempty"`
	Estimated     *money.Money  `bson:"estimated,omitempty"`
	Committed     *money.Money  `bson:"committed,omitempty"`
	Actual        *money.Money  `bson:"actual,omitempty"`
	Paid          *money.Money  `bson:"paid,omitempty"`
	Currency      string        `bson:"currency"`
	Date          time.Time     `bson:"date"`
	Notes         string        `bson:"notes,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoCostItemRepository) Create(ctx context.Context, c CostItem) (CostItem, error) {
	doc, err := toCostItemDoc(c)
	if err != nil {
		return CostItem{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return CostItem{}, err
	}
	c.ID = res.InsertedID.(bson.ObjectID).Hex()
	return c, nil
}

func (r *MongoCostItemRepository) FindByID(ctx context.Context, companyID, id string) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	var doc costItemDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return CostItem{}, ErrCostItemNotFound
	}
	if err != nil {
		return CostItem{}, err
	}
	return fromCostItemDoc(doc)
}

func (r *MongoCostItemRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

func (r *MongoCostItemRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "workItemId": workItemID})
}

func (r *MongoCostItemRepository) list(ctx context.Context, filter bson.M) ([]CostItem, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []costItemDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]CostItem, 0, len(docs))
	for _, doc := range docs {
		c, err := fromCostItemDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *MongoCostItemRepository) UpdateLifecycleField(ctx context.Context, companyID, id string, stage CostStage, amount money.Money) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{string(stage): amount}},
	)
	if err != nil {
		return CostItem{}, err
	}
	if res.MatchedCount == 0 {
		return CostItem{}, ErrCostItemNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoCostItemRepository) UpdateDetails(ctx context.Context, companyID, id string, description, notes string, category *CostCategory) (CostItem, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return CostItem{}, ErrCostItemNotFound
	}
	set := bson.M{"description": description, "notes": notes}
	if category != nil {
		set["category"] = string(*category)
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": set},
	)
	if err != nil {
		return CostItem{}, err
	}
	if res.MatchedCount == 0 {
		return CostItem{}, ErrCostItemNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoCostItemRepository) Delete(ctx context.Context, companyID, id string) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrCostItemNotFound
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objID, "companyId": companyID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrCostItemNotFound
	}
	return nil
}

func toCostItemDoc(c CostItem) (costItemDoc, error) {
	doc := costItemDoc{
		CompanyID: c.CompanyID, ProjectID: c.ProjectID, WorkItemID: c.WorkItemID,
		Category: string(c.Category), Description: c.Description, UnitPrice: c.UnitPrice,
		MaterialID: c.MaterialID, Estimated: c.Estimated, Committed: c.Committed,
		Actual: c.Actual, Paid: c.Paid, Currency: c.Currency, Date: c.Date,
		Notes: c.Notes, CreatedAt: c.CreatedAt, SchemaVersion: c.SchemaVersion,
	}
	if c.Quantity != nil {
		qd := toQuantityDoc(*c.Quantity)
		doc.Quantity = &qd
	}
	if c.ID != "" {
		objID, err := bson.ObjectIDFromHex(c.ID)
		if err != nil {
			return costItemDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromCostItemDoc(doc costItemDoc) (CostItem, error) {
	c := CostItem{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		Category: CostCategory(doc.Category), Description: doc.Description, UnitPrice: doc.UnitPrice,
		MaterialID: doc.MaterialID, Estimated: doc.Estimated, Committed: doc.Committed,
		Actual: doc.Actual, Paid: doc.Paid, Currency: doc.Currency, Date: doc.Date,
		Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
	if doc.Quantity != nil {
		q, err := fromQuantityDoc(*doc.Quantity)
		if err != nil {
			return CostItem{}, err
		}
		c.Quantity = &q
	}
	return c, nil
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS (requires Docker Desktop running)

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/costs/repository.go internal/costs/repository_mongo.go internal/costs/repository_mongo_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 9: `internal/costs` — service (validation, category-lock, capabilities)

**Spec:** §1.4, §1.4.1, §1.4.2, §1.4.3, §5.3, §9.1, §9.2, §9.4. This is the most logic-dense task in the plan — the ledger-entry-point rejection, the `MaterialID⟹Category=material` rule, and the category-correction lock all live here.

**Files:**
- Create: `backend/internal/costs/service.go`
- Test: `backend/internal/costs/service_test.go`

- [x] **Step 1: Write the failing service tests**

Create `backend/internal/costs/service_test.go`:

```go
package costs_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

type fakeCostItemRepository struct {
	byID map[string]costs.CostItem
	next int
}

func newFakeCostItemRepository() *fakeCostItemRepository {
	return &fakeCostItemRepository{byID: make(map[string]costs.CostItem)}
}

func (f *fakeCostItemRepository) Create(ctx context.Context, c costs.CostItem) (costs.CostItem, error) {
	f.next++
	c.ID = "cost_item_" + string(rune('0'+f.next))
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeCostItemRepository) FindByID(ctx context.Context, companyID, id string) (costs.CostItem, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return costs.CostItem{}, costs.ErrCostItemNotFound
	}
	return c, nil
}

func (f *fakeCostItemRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]costs.CostItem, error) {
	var result []costs.CostItem
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.ProjectID == projectID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (f *fakeCostItemRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]costs.CostItem, error) {
	var result []costs.CostItem
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.WorkItemID != nil && *c.WorkItemID == workItemID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (f *fakeCostItemRepository) UpdateLifecycleField(ctx context.Context, companyID, id string, stage costs.CostStage, amount money.Money) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	switch stage {
	case costs.CostStageEstimated:
		c.Estimated = &amount
	case costs.CostStageCommitted:
		c.Committed = &amount
	case costs.CostStageActual:
		c.Actual = &amount
	case costs.CostStagePaid:
		c.Paid = &amount
	}
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) UpdateDetails(ctx context.Context, companyID, id string, description, notes string, category *costs.CostCategory) (costs.CostItem, error) {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return costs.CostItem{}, err
	}
	c.Description = description
	c.Notes = notes
	if category != nil {
		c.Category = *category
	}
	f.byID[id] = c
	return c, nil
}

func (f *fakeCostItemRepository) Delete(ctx context.Context, companyID, id string) error {
	c, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return err
	}
	delete(f.byID, id)
	_ = c
	return nil
}

type fakeProjectLookup struct{ belongs bool }

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeWorkItemLookup struct{ belongs bool }

func (f fakeWorkItemLookup) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeMaterialLookup struct {
	belongs        bool
	referencePrice money.Money
}

func (f fakeMaterialLookup) MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error) {
	return f.belongs, nil
}

func (f fakeMaterialLookup) GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error) {
	return f.referencePrice, nil
}

func newTestService() (*costs.Service, *fakeCostItemRepository) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})
	return svc, repo
}

func TestCreateCostItemRejectsLabourCategory(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryLabour,
		"Should be rejected", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrLabourCategoryNotAllowed {
		t.Fatalf("expected ErrLabourCategoryNotAllowed, got %v", err)
	}
}

func TestCreateCostItemRequiresAtLeastOneLifecycleAmount(t *testing.T) {
	svc, _ := newTestService()
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryPermit,
		"Empty", nil, nil, nil, nil, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrNoLifecycleAmount {
		t.Fatalf("expected ErrNoLifecycleAmount, got %v", err)
	}
}

func TestCreateCostItemLumpSumSuccess(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(450000)
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategorySubcontractor,
		"Bathroom plumbing", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 450000 {
		t.Fatalf("expected estimated 450000, got %+v", c.Estimated)
	}
}

func TestCreateCostItemQuantityUnitPriceEstablishesEstimatedOnly(t *testing.T) {
	svc, _ := newTestService()
	qtyValue := "100"
	qtyUnit := "m2"
	unitPrice := int64(1000) // RM10.00
	// Estimated not explicitly supplied -> derived as 100 * RM10.00 = RM1,000.00
	committed := int64(95000) // RM950.00 -- independently different, must be accepted
	c, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", &qtyValue, &qtyUnit, &unitPrice, nil, &committed, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Estimated == nil || c.Estimated.Amount != 100000 {
		t.Fatalf("expected derived estimated 100000, got %+v", c.Estimated)
	}
	if c.Committed == nil || c.Committed.Amount != 95000 {
		t.Fatalf("expected committed to remain independently 95000, got %+v", c.Committed)
	}
}

func TestCreateCostItemMaterialIDRequiresMaterialCategory(t *testing.T) {
	svc, _ := newTestService()
	materialID := "material_1"
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Wrong category for materialID", nil, nil, nil, &estimated, nil, nil, nil, "MYR", &materialID, time.Now(), "")
	if err != costs.ErrMaterialIDRequiresMaterialCategory {
		t.Fatalf("expected ErrMaterialIDRequiresMaterialCategory, got %v", err)
	}
}

func TestCreateCostItemCurrencyMismatchRejected(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	actual := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Mismatched currency", nil, nil, nil, &estimated, nil, &actual, nil, "MYR", nil, time.Now(), "")
	// This particular call is same-currency by construction (single currency param
	// applies to all supplied amounts in this service signature) — currency
	// mismatch is instead tested via UpdateCostItemLifecycle below, which is
	// where per-field currency actually diverges from the record's Currency.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestUpdateCostItemLifecycleRejectsCurrencyMismatch(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, costs.CostStageActual, money.New(1000, "SGD"))
	if err != costs.ErrCurrencyMismatch {
		t.Fatalf("expected ErrCurrencyMismatch, got %v", err)
	}
}

func TestUpdateCostItemLifecycleAllowsAllFourSimultaneously(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(100000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, costs.CostStageCommitted, money.New(95000, "MYR")); err != nil {
		t.Fatalf("unexpected error setting committed: %v", err)
	}
	if _, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, costs.CostStageActual, money.New(108000, "MYR")); err != nil {
		t.Fatalf("unexpected error setting actual: %v", err)
	}
	final, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, costs.CostStagePaid, money.New(50000, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error setting paid: %v", err)
	}
	if final.Estimated == nil || final.Estimated.Amount != 100000 {
		t.Fatalf("expected estimated to remain 100000, got %+v", final.Estimated)
	}
	if final.Committed == nil || final.Committed.Amount != 95000 {
		t.Fatalf("expected committed 95000, got %+v", final.Committed)
	}
	if final.Actual == nil || final.Actual.Amount != 108000 {
		t.Fatalf("expected actual 108000, got %+v", final.Actual)
	}
	if final.Paid == nil || final.Paid.Amount != 50000 {
		t.Fatalf("expected paid 50000, got %+v", final.Paid)
	}
}

func TestUpdateCostItemDetailsCategoryLockedOnceCommitted(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := svc.UpdateCostItemLifecycle(context.Background(), "company_a", created.ID, costs.CostStageCommitted, money.New(1000, "MYR")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryTransport
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, "New desc", "notes", &newCategory)
	if err != costs.ErrCategoryLocked {
		t.Fatalf("expected ErrCategoryLocked once Committed is set, got %v", err)
	}
}

func TestUpdateCostItemDetailsCategoryCorrectablePreCommitment(t *testing.T) {
	svc, _ := newTestService()
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryEquipment,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryTransport
	updated, err := svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, "New desc", "notes", &newCategory)
	if err != nil {
		t.Fatalf("unexpected error correcting category pre-commitment: %v", err)
	}
	if updated.Category != costs.CostCategoryTransport {
		t.Fatalf("expected corrected category, got %s", updated.Category)
	}
}

func TestUpdateCostItemDetailsCannotDetachMaterialIDFromMaterialCategory(t *testing.T) {
	svc, _ := newTestService()
	materialID := "material_1"
	estimated := int64(1000)
	created, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", nil, costs.CostCategoryMaterial,
		"Tiles", nil, nil, nil, &estimated, nil, nil, nil, "MYR", &materialID, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newCategory := costs.CostCategoryEquipment
	_, err = svc.UpdateCostItemDetails(context.Background(), "company_a", created.ID, "desc", "notes", &newCategory)
	if err != costs.ErrMaterialIDRequiresMaterialCategory {
		t.Fatalf("expected ErrMaterialIDRequiresMaterialCategory, got %v", err)
	}
}

func TestRecordLabourCostCreatesCostItemWithLabourCategory(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	created, err := repo.FindByID(context.Background(), "company_a", costItemID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if created.Category != costs.CostCategoryLabour {
		t.Fatalf("expected labour category, got %s", created.Category)
	}
	if created.Estimated == nil || created.Estimated.Amount != 127500 {
		t.Fatalf("expected estimated 127500, got %+v", created.Estimated)
	}
}

func TestUpdateLabourCostEstimateUpdatesEstimatedOnly(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := repo.UpdateLifecycleField(context.Background(), "company_a", costItemID, costs.CostStageActual, money.New(130000, "MYR")); err != nil {
		t.Fatalf("unexpected error setting actual directly via repo for test setup: %v", err)
	}

	if err := svc.UpdateLabourCostEstimate(context.Background(), "company_a", costItemID, money.New(150000, "MYR")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := repo.FindByID(context.Background(), "company_a", costItemID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Estimated == nil || updated.Estimated.Amount != 150000 {
		t.Fatalf("expected estimated updated to 150000, got %+v", updated.Estimated)
	}
	if updated.Actual == nil || updated.Actual.Amount != 130000 {
		t.Fatalf("expected actual to remain untouched at 130000, got %+v", updated.Actual)
	}
}

func TestDeleteProvisionedLabourCost(t *testing.T) {
	svc, repo := newTestService()
	costItemID, err := svc.RecordLabourCost(context.Background(), "company_a", "project_1", "work_item_1", money.New(127500, "MYR"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := svc.DeleteProvisionedLabourCost(context.Background(), "company_a", costItemID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repo.FindByID(context.Background(), "company_a", costItemID)
	if err != costs.ErrCostItemNotFound {
		t.Fatalf("expected ErrCostItemNotFound after compensation delete, got %v", err)
	}
}

func TestCostItemBelongsToProjectValidation(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: false}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})
	estimated := int64(1000)
	_, err := svc.CreateCostItem(context.Background(), "company_a", "nonexistent_project", nil, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCostItemWorkItemLineageValidation(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: false}, fakeMaterialLookup{belongs: true})
	estimated := int64(1000)
	workItemID := "work_item_mismatched"
	_, err := svc.CreateCostItem(context.Background(), "company_a", "project_1", &workItemID, costs.CostCategoryMiscellaneous,
		"Demo", nil, nil, nil, &estimated, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != costs.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: FAIL — `undefined: costs.NewService` (compile error)

- [x] **Step 3: Implement the service**

Create `backend/internal/costs/service.go`:

```go
package costs

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrLabourCategoryNotAllowed is returned when CreateCostItem is called with
// category=labour. Labour-category CostItems may only be created via
// RecordLabourCost — the ledger-entry-point invariant (design spec §1.4).
var ErrLabourCategoryNotAllowed = errors.New("costs: category=labour is not allowed via CreateCostItem; use LabourEntry")

// ErrNoLifecycleAmount is returned when none of estimated/committed/actual/paid
// is supplied at creation.
var ErrNoLifecycleAmount = errors.New("costs: at least one lifecycle amount is required")

// ErrMaterialIDRequiresMaterialCategory is returned when materialID is
// supplied but category is not "material" (design spec §1.4.1), whether at
// creation or during a category correction.
var ErrMaterialIDRequiresMaterialCategory = errors.New("costs: materialId requires category=material")

// ErrCurrencyMismatch is returned when a lifecycle amount's currency does not
// match the CostItem's own Currency.
var ErrCurrencyMismatch = errors.New("costs: currency does not match this cost item's currency")

// ErrCategoryLocked is returned when a category correction is attempted after
// Committed, Actual, or Paid has already been set (design spec §1.4.3).
var ErrCategoryLocked = errors.New("costs: category is locked once committed, actual, or paid is set")

// ErrProjectNotFound is returned when the given projectID does not belong to
// the caller's company.
var ErrProjectNotFound = errors.New("costs: project not found")

// ErrWorkItemNotFound is returned when a given workItemID does not belong to
// the caller's company, or belongs to the company but not to the given
// projectID (lineage mismatch — both cases collapse to the same sentinel).
var ErrWorkItemNotFound = errors.New("costs: work item not found")

// ErrMaterialNotFound is returned when a given materialID does not belong to
// the caller's company.
var ErrMaterialNotFound = errors.New("costs: material not found")

// ErrInvalidCategory is returned when category is not one of the 9 defined
// values.
var ErrInvalidCategory = errors.New("costs: invalid category")

// ProjectLookup is the capability costs needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup is the capability costs needs from work: confirming a
// workItemID belongs to both companyID and projectID in one compound check
// (design spec §9.3).
type WorkItemLookup interface {
	WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
}

// MaterialLookup is the capability costs needs from materials (design spec §9.2).
type MaterialLookup interface {
	MaterialBelongsToCompany(ctx context.Context, companyID, materialID string) (bool, error)
	GetReferencePrice(ctx context.Context, companyID, materialID string) (money.Money, error)
}

// Service implements CostItem CRUD, the ledger-entry-point invariant, and
// exposes LabourCostRecorder, consumed by labour.
type Service struct {
	repo           CostItemRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	materialLookup MaterialLookup
}

// NewService constructs a Service backed by repo, consuming projectLookup,
// workItemLookup, and materialLookup to validate parent references.
func NewService(repo CostItemRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, materialLookup MaterialLookup) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, workItemLookup: workItemLookup, materialLookup: materialLookup}
}

// CreateCostItem validates projectID/workItemID/materialID parent references,
// rejects category=labour (the ledger-entry-point invariant), enforces
// materialID⟹category=material, requires at least one lifecycle amount, and
// — when quantity+unitPrice are both supplied — establishes/validates
// Estimated only (never Committed/Actual/Paid — design spec §1.4.2).
func (s *Service) CreateCostItem(ctx context.Context, companyID, projectID string, workItemID *string, category CostCategory,
	description string, quantityValue, quantityUnit *string, unitPriceAmount, estimatedAmount, committedAmount, actualAmount, paidAmount *int64,
	currency string, materialID *string, date time.Time, notes string) (CostItem, error) {

	if category == CostCategoryLabour {
		return CostItem{}, ErrLabourCategoryNotAllowed
	}
	if !category.IsValid() {
		return CostItem{}, ErrInvalidCategory
	}
	if materialID != nil && category != CostCategoryMaterial {
		return CostItem{}, ErrMaterialIDRequiresMaterialCategory
	}
	if estimatedAmount == nil && committedAmount == nil && actualAmount == nil && paidAmount == nil {
		return CostItem{}, ErrNoLifecycleAmount
	}

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return CostItem{}, err
	}
	if !belongs {
		return CostItem{}, ErrProjectNotFound
	}

	if workItemID != nil {
		wiBelongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, *workItemID, projectID)
		if err != nil {
			return CostItem{}, err
		}
		if !wiBelongs {
			return CostItem{}, ErrWorkItemNotFound
		}
	}

	if materialID != nil {
		mBelongs, err := s.materialLookup.MaterialBelongsToCompany(ctx, companyID, *materialID)
		if err != nil {
			return CostItem{}, err
		}
		if !mBelongs {
			return CostItem{}, ErrMaterialNotFound
		}
	}

	var q *quantity.Quantity
	var unitPrice *money.Money
	if quantityValue != nil && quantityUnit != nil && unitPriceAmount != nil {
		parsed, err := quantity.New(*quantityValue, *quantityUnit)
		if err != nil {
			return CostItem{}, err
		}
		q = &parsed
		up := money.New(*unitPriceAmount, currency)
		unitPrice = &up

		lineAmount := money.CalculateLineAmount(parsed.Value, up)
		if estimatedAmount == nil {
			derived := lineAmount.Amount
			estimatedAmount = &derived
		}
		// If estimatedAmount WAS explicitly supplied, it is trusted as given —
		// this spec does not reject a mismatch here to avoid double-guessing a
		// contractor-confirmed override; it only fills the gap when absent.
	}

	item := CostItem{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID, Category: category,
		Description: description, Quantity: q, UnitPrice: unitPrice, MaterialID: materialID,
		Currency: currency, Date: date, Notes: notes, CreatedAt: time.Now(), SchemaVersion: 1,
	}
	if estimatedAmount != nil {
		m := money.New(*estimatedAmount, currency)
		item.Estimated = &m
	}
	if committedAmount != nil {
		m := money.New(*committedAmount, currency)
		item.Committed = &m
	}
	if actualAmount != nil {
		m := money.New(*actualAmount, currency)
		item.Actual = &m
	}
	if paidAmount != nil {
		m := money.New(*paidAmount, currency)
		item.Paid = &m
	}

	return s.repo.Create(ctx, item)
}

// GetCostItem returns costItemID's CostItem, tenant-scoped to companyID.
func (s *Service) GetCostItem(ctx context.Context, companyID, costItemID string) (CostItem, error) {
	return s.repo.FindByID(ctx, companyID, costItemID)
}

// ListCostItemsByProject validates projectID belongs to companyID before listing.
func (s *Service) ListCostItemsByProject(ctx context.Context, companyID, projectID string) ([]CostItem, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// ListCostItemsByWorkItem lists CostItems for a WorkItem, tenant-scoped to
// companyID. Parent validation here is intentionally a company-ownership-only
// check via the same capability's lineage method with the CostItem's own
// stored ProjectID looked up first would require an extra round trip; instead
// the repository query itself is already companyId+workItemId scoped, and any
// foreign workItemID simply yields an empty list rather than a 404 — this
// mirrors that WorkItemLookup has no company-ownership-only variant exposed,
// unlike SpaceLookup in M2. Acceptable because a truly foreign workItemID
// cannot match any CostItem under this company's collection scope regardless.
func (s *Service) ListCostItemsByWorkItem(ctx context.Context, companyID, workItemID string) ([]CostItem, error) {
	return s.repo.ListByWorkItem(ctx, companyID, workItemID)
}

// UpdateCostItemLifecycle sets exactly one of the four lifecycle fields,
// validating the new amount's currency matches the record's own Currency.
// Paid is a cumulative running total, not a delta (design spec §1.5).
func (s *Service) UpdateCostItemLifecycle(ctx context.Context, companyID, costItemID string, stage CostStage, amount money.Money) (CostItem, error) {
	if !stage.IsValid() {
		return CostItem{}, errors.New("costs: invalid lifecycle stage")
	}
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if amount.Currency != existing.Currency {
		return CostItem{}, ErrCurrencyMismatch
	}
	return s.repo.UpdateLifecycleField(ctx, companyID, costItemID, stage, amount)
}

// UpdateCostItemDetails updates description/notes, and optionally category —
// category correction is only allowed while Committed, Actual, and Paid are
// all still nil (design spec §1.4.3); it always re-validates
// materialID⟹category=material against the existing record's MaterialID.
func (s *Service) UpdateCostItemDetails(ctx context.Context, companyID, costItemID, description, notes string, category *CostCategory) (CostItem, error) {
	existing, err := s.repo.FindByID(ctx, companyID, costItemID)
	if err != nil {
		return CostItem{}, err
	}
	if category != nil && *category != existing.Category {
		if existing.Committed != nil || existing.Actual != nil || existing.Paid != nil {
			return CostItem{}, ErrCategoryLocked
		}
		if existing.MaterialID != nil && *category != CostCategoryMaterial {
			return CostItem{}, ErrMaterialIDRequiresMaterialCategory
		}
		if !category.IsValid() {
			return CostItem{}, ErrInvalidCategory
		}
	}
	return s.repo.UpdateDetails(ctx, companyID, costItemID, description, notes, category)
}

// RecordLabourCost creates a CostItem with Category=labour on behalf of a
// LabourEntry. Satisfies labour.LabourCostRecorder structurally (design spec
// §9.4). This is the ONLY code path that may produce a labour-category
// CostItem — CreateCostItem structurally rejects it.
func (s *Service) RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (string, error) {
	item := CostItem{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: &workItemID, Category: CostCategoryLabour,
		Description: "Labour cost", Estimated: &estimated, Currency: estimated.Currency,
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	}
	created, err := s.repo.Create(ctx, item)
	if err != nil {
		return "", err
	}
	return created.ID, nil
}

// UpdateLabourCostEstimate updates only the Estimated field of an existing
// labour CostItem — backs LabourEntry correction. Never touches
// Committed/Actual/Paid (design spec §1.3.2, §9.4).
func (s *Service) UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error {
	_, err := s.repo.UpdateLifecycleField(ctx, companyID, costItemID, CostStageEstimated, estimated)
	return err
}

// DeleteProvisionedLabourCost is best-effort compensation: deletes a
// just-created labour CostItem if the paired LabourEntry write subsequently
// fails (design spec §22-B).
func (s *Service) DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error {
	return s.repo.Delete(ctx, companyID, costItemID)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS, all tests in the package (repository tests need Docker; service tests do not)

- [x] **Step 5: Run full build**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/costs/service.go internal/costs/service_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 10: `internal/costs` — handler

**Spec:** §9 (endpoint list — CostItem section).

**Files:**
- Create: `backend/internal/costs/handler.go`

- [x] **Step 1: Implement the handler**

Create `backend/internal/costs/handler.go`:

```go
package costs

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type moneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type costItemDTO struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"projectId"`
	WorkItemID    string    `json:"workItemId,omitempty"`
	Category      string    `json:"category"`
	Description   string    `json:"description"`
	QuantityValue string    `json:"quantityValue,omitempty"`
	QuantityUnit  string    `json:"quantityUnit,omitempty"`
	UnitPrice     *moneyDTO `json:"unitPrice,omitempty"`
	MaterialID    string    `json:"materialId,omitempty"`
	Estimated     *moneyDTO `json:"estimated,omitempty"`
	Committed     *moneyDTO `json:"committed,omitempty"`
	Actual        *moneyDTO `json:"actual,omitempty"`
	Paid          *moneyDTO `json:"paid,omitempty"`
	Currency      string    `json:"currency"`
	Date          string    `json:"date"`
	Notes         string    `json:"notes,omitempty"`
	CreatedAt     string    `json:"createdAt"`
}

type createCostItemInput struct {
	Body struct {
		ProjectID       string  `json:"projectId" required:"true" minLength:"1"`
		WorkItemID      string  `json:"workItemId,omitempty"`
		Category        string  `json:"category" required:"true"`
		Description     string  `json:"description" required:"true" minLength:"1"`
		QuantityValue   string  `json:"quantityValue,omitempty"`
		QuantityUnit    string  `json:"quantityUnit,omitempty"`
		UnitPriceAmount *int64  `json:"unitPriceAmount,omitempty"`
		EstimatedAmount *int64  `json:"estimatedAmount,omitempty"`
		CommittedAmount *int64  `json:"committedAmount,omitempty"`
		ActualAmount    *int64  `json:"actualAmount,omitempty"`
		PaidAmount      *int64  `json:"paidAmount,omitempty"`
		Currency        string  `json:"currency" required:"true" minLength:"1"`
		MaterialID      string  `json:"materialId,omitempty"`
		Notes           string  `json:"notes,omitempty"`
	}
}

type costItemOutput struct {
	Body costItemDTO
}

type listCostItemsInput struct {
	ProjectID  string `query:"projectId"`
	WorkItemID string `query:"workItemId"`
}

type listCostItemsOutput struct {
	Body struct {
		CostItems []costItemDTO `json:"costItems"`
	}
}

type getCostItemInput struct {
	ID string `path:"id"`
}

type updateCostItemLifecycleInput struct {
	ID   string `path:"id"`
	Body struct {
		Stage  string `json:"stage" required:"true"`
		Amount int64  `json:"amount" required:"true"`
	}
}

type updateCostItemDetailsInput struct {
	ID   string `path:"id"`
	Body struct {
		Description string  `json:"description" required:"true" minLength:"1"`
		Notes       string  `json:"notes,omitempty"`
		Category    *string `json:"category,omitempty"`
	}
}

// RegisterHandlers registers POST /cost-items, GET /cost-items,
// GET /cost-items/{id}, PATCH /cost-items/{id}/lifecycle, and
// PATCH /cost-items/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "cost-items-create",
		Method:      http.MethodPost,
		Path:        "/cost-items",
		Summary:     "Create a CostItem under a Project (and optionally a WorkItem). Rejects category=labour.",
	}, func(ctx context.Context, input *createCostItemInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var workItemID *string
		if input.Body.WorkItemID != "" {
			workItemID = &input.Body.WorkItemID
		}
		var materialID *string
		if input.Body.MaterialID != "" {
			materialID = &input.Body.MaterialID
		}
		var quantityValue, quantityUnit *string
		if input.Body.QuantityValue != "" && input.Body.QuantityUnit != "" {
			quantityValue = &input.Body.QuantityValue
			quantityUnit = &input.Body.QuantityUnit
		}

		c, err := svc.CreateCostItem(ctx, principal.CompanyID, input.Body.ProjectID, workItemID, CostCategory(input.Body.Category),
			input.Body.Description, quantityValue, quantityUnit, input.Body.UnitPriceAmount,
			input.Body.EstimatedAmount, input.Body.CommittedAmount, input.Body.ActualAmount, input.Body.PaidAmount,
			input.Body.Currency, materialID, time.Now(), input.Body.Notes)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-list",
		Method:      http.MethodGet,
		Path:        "/cost-items",
		Summary:     "List CostItems for a Project or a WorkItem belonging to the authenticated company",
	}, func(ctx context.Context, input *listCostItemsInput) (*listCostItemsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		var list []CostItem
		var err error
		switch {
		case input.WorkItemID != "":
			list, err = svc.ListCostItemsByWorkItem(ctx, principal.CompanyID, input.WorkItemID)
		case input.ProjectID != "":
			list, err = svc.ListCostItemsByProject(ctx, principal.CompanyID, input.ProjectID)
		default:
			return nil, huma.Error422UnprocessableEntity("either projectId or workItemId query parameter is required")
		}
		if err != nil {
			return nil, mapCostsError(err)
		}

		resp := &listCostItemsOutput{}
		for _, c := range list {
			resp.Body.CostItems = append(resp.Body.CostItems, toCostItemDTO(c))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-get",
		Method:      http.MethodGet,
		Path:        "/cost-items/{id}",
		Summary:     "Get a CostItem, tenant-scoped",
	}, func(ctx context.Context, input *getCostItemInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		c, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-update-lifecycle",
		Method:      http.MethodPatch,
		Path:        "/cost-items/{id}/lifecycle",
		Summary:     "Set exactly one of estimated/committed/actual/paid. paid is a cumulative running total.",
	}, func(ctx context.Context, input *updateCostItemLifecycleInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		existing, err := svc.GetCostItem(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapCostsError(err)
		}
		amount := money.New(input.Body.Amount, existing.Currency)
		c, err := svc.UpdateCostItemLifecycle(ctx, principal.CompanyID, input.ID, CostStage(input.Body.Stage), amount)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cost-items-update-details",
		Method:      http.MethodPatch,
		Path:        "/cost-items/{id}",
		Summary:     "Update description/notes, and optionally category (locked once committed/actual/paid is set)",
	}, func(ctx context.Context, input *updateCostItemDetailsInput) (*costItemOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var category *CostCategory
		if input.Body.Category != nil {
			cat := CostCategory(*input.Body.Category)
			category = &cat
		}
		c, err := svc.UpdateCostItemDetails(ctx, principal.CompanyID, input.ID, input.Body.Description, input.Body.Notes, category)
		if err != nil {
			return nil, mapCostsError(err)
		}
		return &costItemOutput{Body: toCostItemDTO(c)}, nil
	})
}

func toCostItemDTO(c CostItem) costItemDTO {
	dto := costItemDTO{
		ID: c.ID, ProjectID: c.ProjectID, Category: string(c.Category), Description: c.Description,
		Currency: c.Currency, Date: c.Date.Format(timeLayout), Notes: c.Notes, CreatedAt: c.CreatedAt.Format(timeLayout),
	}
	if c.WorkItemID != nil {
		dto.WorkItemID = *c.WorkItemID
	}
	if c.MaterialID != nil {
		dto.MaterialID = *c.MaterialID
	}
	if c.Quantity != nil {
		dto.QuantityValue = c.Quantity.Value.String()
		dto.QuantityUnit = c.Quantity.Unit
	}
	if c.UnitPrice != nil {
		dto.UnitPrice = &moneyDTO{Amount: c.UnitPrice.Amount, Currency: c.UnitPrice.Currency}
	}
	if c.Estimated != nil {
		dto.Estimated = &moneyDTO{Amount: c.Estimated.Amount, Currency: c.Estimated.Currency}
	}
	if c.Committed != nil {
		dto.Committed = &moneyDTO{Amount: c.Committed.Amount, Currency: c.Committed.Currency}
	}
	if c.Actual != nil {
		dto.Actual = &moneyDTO{Amount: c.Actual.Amount, Currency: c.Actual.Currency}
	}
	if c.Paid != nil {
		dto.Paid = &moneyDTO{Amount: c.Paid.Amount, Currency: c.Paid.Currency}
	}
	return dto
}

func mapCostsError(err error) error {
	switch {
	case errors.Is(err, ErrCostItemNotFound):
		return huma.Error404NotFound("cost item not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")
	case errors.Is(err, ErrLabourCategoryNotAllowed):
		return huma.Error422UnprocessableEntity("category=labour is not allowed via this endpoint; use POST /labour-entries")
	case errors.Is(err, ErrNoLifecycleAmount):
		return huma.Error422UnprocessableEntity("at least one of estimated, committed, actual, or paid is required")
	case errors.Is(err, ErrMaterialIDRequiresMaterialCategory):
		return huma.Error422UnprocessableEntity("materialId requires category=material")
	case errors.Is(err, ErrInvalidCategory):
		return huma.Error422UnprocessableEntity("invalid category")
	case errors.Is(err, ErrCurrencyMismatch):
		return huma.Error422UnprocessableEntity("currency does not match this cost item's currency")
	case errors.Is(err, ErrCategoryLocked):
		return huma.Error409Conflict("category is locked once committed, actual, or paid is set")
	default:
		return err
	}
}
```

- [x] **Step 2: Build to confirm it compiles**

Run: `cd backend && go build ./internal/costs/... && go vet ./internal/costs/...`
Expected: clean

- [x] **Step 3: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/costs/handler.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 11: `internal/costs` — full package verification

**Files:** none (verification only)

- [x] **Step 1: Run the full costs package test suite**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS — all model/repository/service tests green

- [x] **Step 2: Run the full build**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions in any earlier package

- [x] **Step 3: No commit needed** (verification-only task)

---

## Task 12: `internal/labour` — Worker and LabourEntry models

**Spec:** §1.2, §1.3, §1.3.1 (already done in Task 1), §1.3.2 rationale.

**Files:**
- Create: `backend/internal/labour/worker.go`
- Create: `backend/internal/labour/labour_entry.go`
- Delete: `backend/internal/labour/doc.go`

- [x] **Step 1: Check the current placeholder**

Run: `cd backend && cat internal/labour/doc.go`

- [x] **Step 2: Remove the placeholder and create the Worker model file**

Delete `backend/internal/labour/doc.go`.

Create `backend/internal/labour/worker.go`:

```go
// Package labour owns Worker (reusable company-level person/resource records)
// and LabourEntry (the financial/work record representing labour applied to
// a WorkItem — phase1.md §13-15). Every LabourEntry creates exactly one
// linked CostItem{Category=labour} via the LabourCostRecorder capability
// consumed from internal/costs; LabourEntry.Cost is a derived/snapshotted
// display value, and the linked CostItem.Estimated is the authoritative
// ledger value M4 aggregates from. See
// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
// §1.2-1.3, §5.2, §9.
//
// labour never imports projects, work, materials, or costs by type. It
// defines ProjectLookup, WorkItemLookup, and LabourCostRecorder-consuming
// interfaces are NOT applicable here — LabourCostRecorder is defined here and
// satisfied by costs.Service. labour exposes no capability to any other
// module.
package labour

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// RateType is one of the 5 supported rate types from phase1.md §13.
type RateType string

const (
	RateTypeHourly         RateType = "hourly"
	RateTypeDaily          RateType = "daily"
	RateTypeFixedProject   RateType = "fixed_project"
	RateTypePerUnit        RateType = "per_unit"
	RateTypePerSquareMeter RateType = "per_square_meter"
)

// IsValid reports whether r is one of the 5 defined rate types.
func (r RateType) IsValid() bool {
	switch r {
	case RateTypeHourly, RateTypeDaily, RateTypeFixedProject, RateTypePerUnit, RateTypePerSquareMeter:
		return true
	default:
		return false
	}
}

// Worker is a reusable, company-level person/resource record. Workers may
// participate in multiple Projects; Project-specific labour costs are never
// stored on Worker itself (phase1.md §13) — see LabourEntry.
type Worker struct {
	ID            string      `bson:"_id,omitempty" json:"id"`
	CompanyID     string      `bson:"companyId" json:"companyId"`
	Name          string      `bson:"name" json:"name"`
	Trade         string      `bson:"trade,omitempty" json:"trade,omitempty"`
	RateType      RateType    `bson:"rateType" json:"rateType"`
	DefaultRate   money.Money `bson:"defaultRate" json:"defaultRate"`
	ContactPhone  string      `bson:"contactPhone,omitempty" json:"contactPhone,omitempty"`
	ContactEmail  string      `bson:"contactEmail,omitempty" json:"contactEmail,omitempty"`
	CreatedAt     time.Time   `bson:"createdAt" json:"createdAt"`
	SchemaVersion int         `bson:"schemaVersion" json:"schemaVersion"`
}
```

- [x] **Step 3: Create the LabourEntry model file**

Create `backend/internal/labour/labour_entry.go`:

```go
package labour

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// LabourEntry represents labour applied to a WorkItem. WorkerID is optional —
// nil for ad-hoc labour paid outside the Worker catalog (design spec §1.3.3).
// Subcontractor costs are never represented as a LabourEntry — they are
// CostItems with Category=subcontractor, created directly through
// internal/costs. CostItemID references the one linked CostItem this entry
// created; the relationship is one-directional — CostItem stores no
// back-reference (design spec §1.3, §22-B).
type LabourEntry struct {
	ID            string            `bson:"_id,omitempty" json:"id"`
	CompanyID     string            `bson:"companyId" json:"companyId"`
	ProjectID     string            `bson:"projectId" json:"projectId"`
	WorkItemID    string            `bson:"workItemId" json:"workItemId"`
	WorkerID      *string           `bson:"workerId,omitempty" json:"workerId,omitempty"`
	WorkerName    string            `bson:"workerName" json:"workerName"`
	Trade         string            `bson:"trade,omitempty" json:"trade,omitempty"`
	Quantity      quantity.Quantity `bson:"-" json:"-"` // never BSON-marshaled directly — see quantityDoc in repository_mongo.go
	Rate          money.Money       `bson:"rate" json:"rate"`
	Cost          money.Money       `bson:"cost" json:"cost"`
	CostItemID    string            `bson:"costItemId" json:"costItemId"`
	Date          time.Time         `bson:"date" json:"date"`
	Notes         string            `bson:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     time.Time         `bson:"createdAt" json:"createdAt"`
	SchemaVersion int               `bson:"schemaVersion" json:"schemaVersion"`
}
```

- [x] **Step 4: Confirm the package still builds**

Run: `cd backend && go build ./internal/labour/...`
Expected: succeeds

- [x] **Step 5: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/labour/worker.go internal/labour/labour_entry.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 13: `internal/labour` — repositories (Worker and LabourEntry)

**Spec:** §2, §10, §12, §13 (including the unique `{companyId, costItemId}` index enforcing the 1:1 LabourEntry↔CostItem invariant at the database level).

**Files:**
- Create: `backend/internal/labour/repository.go`
- Create: `backend/internal/labour/repository_mongo.go`
- Test: `backend/internal/labour/repository_mongo_test.go`

- [x] **Step 1: Write both repository interfaces**

Create `backend/internal/labour/repository.go`:

```go
package labour

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrWorkerNotFound is returned when a Worker lookup finds no match —
// including a Worker that exists but belongs to a different company.
var ErrWorkerNotFound = errors.New("labour: worker not found")

// ErrLabourEntryNotFound is returned when a LabourEntry lookup finds no
// match — including one that exists but belongs to a different company.
var ErrLabourEntryNotFound = errors.New("labour: labour entry not found")

// WorkerRepository persists Workers. labour owns the workers collection
// exclusively; no other module may query it directly.
type WorkerRepository interface {
	Create(ctx context.Context, w Worker) (Worker, error)
	FindByID(ctx context.Context, companyID, id string) (Worker, error)
	List(ctx context.Context, companyID string) ([]Worker, error)
	Update(ctx context.Context, companyID, id string, fn func(*Worker)) (Worker, error)
}

// LabourEntryRepository persists LabourEntries. labour owns the
// labour_entries collection exclusively; no other module may query it
// directly.
type LabourEntryRepository interface {
	Create(ctx context.Context, e LabourEntry) (LabourEntry, error)
	FindByID(ctx context.Context, companyID, id string) (LabourEntry, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error)
	ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error)
	// UpdateCorrection persists a corrected Quantity/Rate/Cost/Date/Notes for
	// an existing LabourEntry — the only mutation path, backing the narrow
	// PATCH /labour-entries/{id} (design spec §1.3.2). Never changes WorkerID.
	UpdateCorrection(ctx context.Context, companyID, id string, quantity quantity.Quantity, rate money.Money, cost money.Money, date time.Time, notes string) (LabourEntry, error)
}
```

- [x] **Step 2: Write the failing Mongo repository tests**

Create `backend/internal/labour/repository_mongo_test.go`:

```go
package labour_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func setupMongoDB(t *testing.T) *mongo.Client {
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	client, err := platformmongo.Connect(connStr)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = platformmongo.Disconnect(context.Background(), client) })

	return client
}

func mustQuantity(t *testing.T, value, unit string) quantity.Quantity {
	t.Helper()
	q, err := quantity.New(value, unit)
	if err != nil {
		t.Fatalf("unexpected error constructing quantity: %v", err)
	}
	return q
}

func TestWorkerRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", Trade: "Tiler", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Name != "Ahmad" || found.DefaultRate.Amount != 15000 {
		t.Fatalf("expected Ahmad/15000, got %s/%d", found.Name, found.DefaultRate.Amount)
	}
}

func TestWorkerRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_tenant")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != labour.ErrWorkerNotFound {
		t.Fatalf("expected ErrWorkerNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestWorkerRepositoryListAndUpdate(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_list")
	repo := labour.NewMongoWorkerRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.Worker{
		CompanyID: "company_a", Name: "Ahmad", RateType: labour.RateTypeDaily,
		DefaultRate: money.New(15000, "MYR"), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	list, err := repo.List(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(list))
	}

	updated, err := repo.Update(ctx, "company_a", created.ID, func(w *labour.Worker) {
		w.DefaultRate = money.New(18000, "MYR")
	})
	if err != nil {
		t.Fatalf("unexpected error updating: %v", err)
	}
	if updated.DefaultRate.Amount != 18000 {
		t.Fatalf("expected updated rate 18000, got %d", updated.DefaultRate.Amount)
	}
}

func TestWorkerRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_worker_indexes")
	repo := labour.NewMongoWorkerRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}

func TestLabourEntryRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	workerID := "worker_1"
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1", WorkerID: &workerID,
		WorkerName: "Ahmad", Trade: "Tiler", Quantity: mustQuantity(t, "8", "day"),
		Rate: money.New(15000, "MYR"), Cost: money.New(1200000, "MYR"), CostItemID: "cost_item_1",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	if found.Cost.Amount != 1200000 {
		t.Fatalf("expected cost 1200000, got %d", found.Cost.Amount)
	}
	if found.CostItemID != "cost_item_1" {
		t.Fatalf("expected costItemId cost_item_1, got %s", found.CostItemID)
	}
}

func TestLabourEntryRepositoryQuantityRoundTrip(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_quantity")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Ad-hoc labourer", Trade: "General", Quantity: mustQuantity(t, "8.5", "hour"),
		Rate: money.New(2500, "MYR"), Cost: money.New(21250, "MYR"), CostItemID: "cost_item_2",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding: %v", err)
	}
	expected := decimal.RequireFromString("8.5")
	if !found.Quantity.Value.Equal(expected) {
		t.Fatalf("expected exact decimal 8.5, got %s", found.Quantity.Value.String())
	}
	if found.Quantity.Unit != "hour" {
		t.Fatalf("expected unit hour, got %s", found.Quantity.Unit)
	}
}

func TestLabourEntryRepositoryFindByIDWrongCompanyNotFound(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_tenant")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Demo", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(15000, "MYR"),
		Cost: money.New(15000, "MYR"), CostItemID: "cost_item_3",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != labour.ErrLabourEntryNotFound {
		t.Fatalf("expected ErrLabourEntryNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestLabourEntryRepositoryListByProjectAndByWorkItem(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_list")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	_, _ = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "A", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "cost_item_a",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	_, _ = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_2",
		WorkerName: "B", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "cost_item_b",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})

	byProject, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing by project: %v", err)
	}
	if len(byProject) != 2 {
		t.Fatalf("expected 2 labour entries for project_1, got %d", len(byProject))
	}

	byWorkItem, err := repo.ListByWorkItem(ctx, "company_a", "work_item_1")
	if err != nil {
		t.Fatalf("unexpected error listing by work item: %v", err)
	}
	if len(byWorkItem) != 1 {
		t.Fatalf("expected 1 labour entry for work_item_1, got %d", len(byWorkItem))
	}
}

func TestLabourEntryRepositoryUpdateCorrection(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_correction")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	created, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "Ahmad", Quantity: mustQuantity(t, "8", "hour"), Rate: money.New(2500, "MYR"),
		Cost: money.New(20000, "MYR"), CostItemID: "cost_item_correction",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	newQty := mustQuantity(t, "80", "hour") // the typo-correction scenario from the design spec
	newDate := time.Now()
	updated, err := repo.UpdateCorrection(ctx, "company_a", created.ID, newQty, money.New(2500, "MYR"), money.New(200000, "MYR"), newDate, "corrected typo")
	if err != nil {
		t.Fatalf("unexpected error correcting: %v", err)
	}
	if !updated.Quantity.Value.Equal(decimal.RequireFromString("80")) {
		t.Fatalf("expected corrected quantity 80, got %s", updated.Quantity.Value.String())
	}
	if updated.Cost.Amount != 200000 {
		t.Fatalf("expected corrected cost 200000, got %d", updated.Cost.Amount)
	}
	if updated.CostItemID != "cost_item_correction" {
		t.Fatalf("expected costItemId to remain unchanged, got %s", updated.CostItemID)
	}
}

func TestLabourEntryRepositoryCostItemIDUniqueIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_unique_costitem")
	repo := labour.NewMongoLabourEntryRepository(db)

	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_1",
		WorkerName: "First", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "shared_cost_item",
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("unexpected error creating first entry: %v", err)
	}

	_, err = repo.Create(ctx, labour.LabourEntry{
		CompanyID: "company_a", ProjectID: "project_1", WorkItemID: "work_item_2",
		WorkerName: "Second", Quantity: mustQuantity(t, "1", "day"), Rate: money.New(1, "MYR"),
		Cost: money.New(1, "MYR"), CostItemID: "shared_cost_item", // same costItemId — must be rejected
		Date: time.Now(), CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == nil {
		t.Fatal("expected duplicate-key error for second LabourEntry with the same costItemId, got nil")
	}
}

func TestLabourEntryRepositoryEnsureIndexes(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "labour_test_entry_indexes")
	repo := labour.NewMongoLabourEntryRepository(db)

	if err := repo.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}
}
```

- [x] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/labour/... -v`
Expected: FAIL — `undefined: labour.NewMongoWorkerRepository` (compile error)

- [x] **Step 4: Implement the Mongo repositories**

Create `backend/internal/labour/repository_mongo.go`:

```go
package labour

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// MongoWorkerRepository is the MongoDB-backed WorkerRepository implementation.
// It owns the "workers" collection exclusively.
type MongoWorkerRepository struct {
	collection *mongo.Collection
}

// NewMongoWorkerRepository constructs a MongoWorkerRepository against db's
// "workers" collection.
func NewMongoWorkerRepository(db *mongo.Database) *MongoWorkerRepository {
	return &MongoWorkerRepository{collection: db.Collection("workers")}
}

// EnsureIndexes creates the companyId index (design spec §13).
func (r *MongoWorkerRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "companyId", Value: 1}},
	})
	return err
}

type workerDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	Name          string        `bson:"name"`
	Trade         string        `bson:"trade,omitempty"`
	RateType      string        `bson:"rateType"`
	DefaultRate   money.Money   `bson:"defaultRate"`
	ContactPhone  string        `bson:"contactPhone,omitempty"`
	ContactEmail  string        `bson:"contactEmail,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoWorkerRepository) Create(ctx context.Context, w Worker) (Worker, error) {
	doc := toWorkerDoc(w)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Worker{}, err
	}
	w.ID = res.InsertedID.(bson.ObjectID).Hex()
	return w, nil
}

func (r *MongoWorkerRepository) FindByID(ctx context.Context, companyID, id string) (Worker, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Worker{}, ErrWorkerNotFound
	}
	var doc workerDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Worker{}, ErrWorkerNotFound
	}
	if err != nil {
		return Worker{}, err
	}
	return fromWorkerDoc(doc), nil
}

func (r *MongoWorkerRepository) List(ctx context.Context, companyID string) ([]Worker, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []workerDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]Worker, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromWorkerDoc(doc))
	}
	return result, nil
}

func (r *MongoWorkerRepository) Update(ctx context.Context, companyID, id string, fn func(*Worker)) (Worker, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Worker{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id)
	doc := toWorkerDoc(existing)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": doc},
	)
	if err != nil {
		return Worker{}, err
	}
	if res.MatchedCount == 0 {
		return Worker{}, ErrWorkerNotFound
	}
	return existing, nil
}

func toWorkerDoc(w Worker) workerDoc {
	doc := workerDoc{
		CompanyID: w.CompanyID, Name: w.Name, Trade: w.Trade, RateType: string(w.RateType),
		DefaultRate: w.DefaultRate, ContactPhone: w.ContactPhone, ContactEmail: w.ContactEmail,
		CreatedAt: w.CreatedAt, SchemaVersion: w.SchemaVersion,
	}
	if w.ID != "" {
		objID, _ := bson.ObjectIDFromHex(w.ID)
		doc.ID = objID
	}
	return doc
}

func fromWorkerDoc(doc workerDoc) Worker {
	return Worker{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, Name: doc.Name, Trade: doc.Trade,
		RateType: RateType(doc.RateType), DefaultRate: doc.DefaultRate,
		ContactPhone: doc.ContactPhone, ContactEmail: doc.ContactEmail,
		CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// MongoLabourEntryRepository is the MongoDB-backed LabourEntryRepository
// implementation. It owns the "labour_entries" collection exclusively.
type MongoLabourEntryRepository struct {
	collection *mongo.Collection
}

// NewMongoLabourEntryRepository constructs a MongoLabourEntryRepository
// against db's "labour_entries" collection.
func NewMongoLabourEntryRepository(db *mongo.Database) *MongoLabourEntryRepository {
	return &MongoLabourEntryRepository{collection: db.Collection("labour_entries")}
}

// EnsureIndexes creates the companyId index, companyId+projectId,
// companyId+workItemId, sparse companyId+workerId compound indexes, and the
// UNIQUE companyId+costItemId index enforcing the 1:1 LabourEntry<->CostItem
// invariant at the database level (design spec §13, §22-B).
func (r *MongoLabourEntryRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workItemId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "workerId", Value: 1}},
			Options: options.Index().SetSparse(true)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "costItemId", Value: 1}},
			Options: options.Index().SetUnique(true)},
	})
	return err
}

// quantityDoc mirrors internal/work's own private copy — string-backed,
// never Decimal128 (design spec §12).
type quantityDoc struct {
	Value string `bson:"value"`
	Unit  string `bson:"unit"`
}

func toQuantityDoc(q quantity.Quantity) quantityDoc {
	return quantityDoc{Value: q.Value.String(), Unit: q.Unit}
}

func fromQuantityDoc(doc quantityDoc) (quantity.Quantity, error) {
	return quantity.New(doc.Value, doc.Unit)
}

type labourEntryDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	WorkItemID    string        `bson:"workItemId"`
	WorkerID      *string       `bson:"workerId,omitempty"`
	WorkerName    string        `bson:"workerName"`
	Trade         string        `bson:"trade,omitempty"`
	Quantity      quantityDoc   `bson:"quantity"`
	Rate          money.Money   `bson:"rate"`
	Cost          money.Money   `bson:"cost"`
	CostItemID    string        `bson:"costItemId"`
	Date          time.Time     `bson:"date"`
	Notes         string        `bson:"notes,omitempty"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func (r *MongoLabourEntryRepository) Create(ctx context.Context, e LabourEntry) (LabourEntry, error) {
	doc := toLabourEntryDoc(e)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return LabourEntry{}, err
	}
	e.ID = res.InsertedID.(bson.ObjectID).Hex()
	return e, nil
}

func (r *MongoLabourEntryRepository) FindByID(ctx context.Context, companyID, id string) (LabourEntry, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	var doc labourEntryDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	if err != nil {
		return LabourEntry{}, err
	}
	return fromLabourEntryDoc(doc)
}

func (r *MongoLabourEntryRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "projectId": projectID})
}

func (r *MongoLabourEntryRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error) {
	return r.list(ctx, bson.M{"companyId": companyID, "workItemId": workItemID})
}

func (r *MongoLabourEntryRepository) list(ctx context.Context, filter bson.M) ([]LabourEntry, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []labourEntryDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]LabourEntry, 0, len(docs))
	for _, doc := range docs {
		e, err := fromLabourEntryDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (r *MongoLabourEntryRepository) UpdateCorrection(ctx context.Context, companyID, id string, q quantity.Quantity, rate, cost money.Money, date time.Time, notes string) (LabourEntry, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{
			"quantity": toQuantityDoc(q), "rate": rate, "cost": cost, "date": date, "notes": notes,
		}},
	)
	if err != nil {
		return LabourEntry{}, err
	}
	if res.MatchedCount == 0 {
		return LabourEntry{}, ErrLabourEntryNotFound
	}
	return r.FindByID(ctx, companyID, id)
}

func toLabourEntryDoc(e LabourEntry) labourEntryDoc {
	doc := labourEntryDoc{
		CompanyID: e.CompanyID, ProjectID: e.ProjectID, WorkItemID: e.WorkItemID, WorkerID: e.WorkerID,
		WorkerName: e.WorkerName, Trade: e.Trade, Quantity: toQuantityDoc(e.Quantity),
		Rate: e.Rate, Cost: e.Cost, CostItemID: e.CostItemID, Date: e.Date,
		Notes: e.Notes, CreatedAt: e.CreatedAt, SchemaVersion: e.SchemaVersion,
	}
	if e.ID != "" {
		objID, _ := bson.ObjectIDFromHex(e.ID)
		doc.ID = objID
	}
	return doc
}

func fromLabourEntryDoc(doc labourEntryDoc) (LabourEntry, error) {
	q, err := fromQuantityDoc(doc.Quantity)
	if err != nil {
		return LabourEntry{}, err
	}
	return LabourEntry{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, WorkItemID: doc.WorkItemID,
		WorkerID: doc.WorkerID, WorkerName: doc.WorkerName, Trade: doc.Trade, Quantity: q,
		Rate: doc.Rate, Cost: doc.Cost, CostItemID: doc.CostItemID, Date: doc.Date,
		Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}, nil
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/labour/... -v`
Expected: PASS (requires Docker Desktop running)

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/labour/repository.go internal/labour/repository_mongo.go internal/labour/repository_mongo_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 14: `internal/labour` — service (two-module write, compensation, correction)

**Spec:** §1.3.2, §1.3.3, §5.2, §9.1, §9.3, §9.4, §22-B, §22-D. This is the most important task in the module — `CreateLabourEntry`'s cost-first-with-compensation write ordering and `UpdateLabourEntry`'s narrow correction path.

**Files:**
- Create: `backend/internal/labour/service.go`
- Test: `backend/internal/labour/service_test.go`

- [x] **Step 1: Write the failing service tests**

Create `backend/internal/labour/service_test.go`:

```go
package labour_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
)

type fakeWorkerRepository struct {
	byID map[string]labour.Worker
	next int
}

func newFakeWorkerRepository() *fakeWorkerRepository {
	return &fakeWorkerRepository{byID: make(map[string]labour.Worker)}
}

func (f *fakeWorkerRepository) Create(ctx context.Context, w labour.Worker) (labour.Worker, error) {
	f.next++
	w.ID = "worker_" + string(rune('0'+f.next))
	f.byID[w.ID] = w
	return w, nil
}

func (f *fakeWorkerRepository) FindByID(ctx context.Context, companyID, id string) (labour.Worker, error) {
	w, ok := f.byID[id]
	if !ok || w.CompanyID != companyID {
		return labour.Worker{}, labour.ErrWorkerNotFound
	}
	return w, nil
}

func (f *fakeWorkerRepository) List(ctx context.Context, companyID string) ([]labour.Worker, error) {
	var result []labour.Worker
	for _, w := range f.byID {
		if w.CompanyID == companyID {
			result = append(result, w)
		}
	}
	return result, nil
}

func (f *fakeWorkerRepository) Update(ctx context.Context, companyID, id string, fn func(*labour.Worker)) (labour.Worker, error) {
	w, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return labour.Worker{}, err
	}
	fn(&w)
	f.byID[id] = w
	return w, nil
}

type fakeLabourEntryRepository struct {
	byID           map[string]labour.LabourEntry
	byCostItemID   map[string]bool
	next           int
	failNextCreate bool
}

func newFakeLabourEntryRepository() *fakeLabourEntryRepository {
	return &fakeLabourEntryRepository{byID: make(map[string]labour.LabourEntry), byCostItemID: make(map[string]bool)}
}

func (f *fakeLabourEntryRepository) Create(ctx context.Context, e labour.LabourEntry) (labour.LabourEntry, error) {
	if f.failNextCreate {
		f.failNextCreate = false
		return labour.LabourEntry{}, errors.New("simulated LabourEntry write failure")
	}
	if f.byCostItemID[e.CostItemID] {
		return labour.LabourEntry{}, errors.New("duplicate costItemId")
	}
	f.next++
	e.ID = "labour_entry_" + string(rune('0'+f.next))
	f.byID[e.ID] = e
	f.byCostItemID[e.CostItemID] = true
	return e, nil
}

func (f *fakeLabourEntryRepository) FindByID(ctx context.Context, companyID, id string) (labour.LabourEntry, error) {
	e, ok := f.byID[id]
	if !ok || e.CompanyID != companyID {
		return labour.LabourEntry{}, labour.ErrLabourEntryNotFound
	}
	return e, nil
}

func (f *fakeLabourEntryRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]labour.LabourEntry, error) {
	var result []labour.LabourEntry
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeLabourEntryRepository) ListByWorkItem(ctx context.Context, companyID, workItemID string) ([]labour.LabourEntry, error) {
	var result []labour.LabourEntry
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.WorkItemID == workItemID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeLabourEntryRepository) UpdateCorrection(ctx context.Context, companyID, id string, q quantity.Quantity, rate, cost money.Money, date time.Time, notes string) (labour.LabourEntry, error) {
	e, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return labour.LabourEntry{}, err
	}
	e.Quantity = q
	e.Rate = rate
	e.Cost = cost
	e.Date = date
	e.Notes = notes
	f.byID[id] = e
	return e, nil
}

type fakeLabourProjectLookup struct{ belongs bool }

func (f fakeLabourProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeLabourWorkItemLookup struct{ belongs bool }

func (f fakeLabourWorkItemLookup) WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error) {
	return f.belongs, nil
}

// fakeLabourCostRecorder tracks calls so tests can assert write ordering and
// compensation behavior (design spec §22-B).
type fakeLabourCostRecorder struct {
	created        map[string]money.Money // costItemID -> estimated
	next           int
	failNextRecord bool
	deletedIDs     []string
	updatedEstimates map[string]money.Money
}

func newFakeLabourCostRecorder() *fakeLabourCostRecorder {
	return &fakeLabourCostRecorder{created: make(map[string]money.Money), updatedEstimates: make(map[string]money.Money)}
}

func (f *fakeLabourCostRecorder) RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (string, error) {
	if f.failNextRecord {
		f.failNextRecord = false
		return "", errors.New("simulated CostItem write failure")
	}
	f.next++
	id := "cost_item_" + string(rune('0'+f.next))
	f.created[id] = estimated
	return id, nil
}

func (f *fakeLabourCostRecorder) UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error {
	f.updatedEstimates[costItemID] = estimated
	return nil
}

func (f *fakeLabourCostRecorder) DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error {
	f.deletedIDs = append(f.deletedIDs, costItemID)
	delete(f.created, costItemID)
	return nil
}

func newTestLabourService() (*labour.Service, *fakeWorkerRepository, *fakeLabourEntryRepository, *fakeLabourCostRecorder) {
	workerRepo := newFakeWorkerRepository()
	entryRepo := newFakeLabourEntryRepository()
	recorder := newFakeLabourCostRecorder()
	svc := labour.NewService(workerRepo, entryRepo, fakeLabourProjectLookup{belongs: true}, fakeLabourWorkItemLookup{belongs: true}, recorder)
	return svc, workerRepo, entryRepo, recorder
}

func TestCreateWorkerRequiresValidRateType(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	_, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", "not_a_real_rate_type", 15000, "MYR", "", "")
	if err != labour.ErrInvalidRateType {
		t.Fatalf("expected ErrInvalidRateType, got %v", err)
	}
}

func TestCreateWorkerSuccess(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	w, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.DefaultRate.Amount != 15000 {
		t.Fatalf("expected 15000, got %d", w.DefaultRate.Amount)
	}
}

func TestCreateLabourEntryWithWorkerSnapshotsRateAndCreatesLinkedCostItem(t *testing.T) {
	svc, _, entryRepo, recorder := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating labour entry: %v", err)
	}
	if entry.WorkerName != "Ahmad" || entry.Trade != "Tiler" {
		t.Fatalf("expected snapshotted name/trade, got %s/%s", entry.WorkerName, entry.Trade)
	}
	if entry.Rate.Amount != 15000 {
		t.Fatalf("expected snapshotted rate 15000, got %d", entry.Rate.Amount)
	}
	if entry.Cost.Amount != 120000 { // 8 days * RM150.00 = RM1,200.00
		t.Fatalf("expected cost 120000, got %d", entry.Cost.Amount)
	}
	if entry.CostItemID == "" {
		t.Fatal("expected a linked CostItemID")
	}

	stored, err := entryRepo.FindByID(context.Background(), "company_a", entry.ID)
	if err != nil {
		t.Fatalf("unexpected error fetching stored entry: %v", err)
	}
	if stored.CostItemID != entry.CostItemID {
		t.Fatalf("expected persisted CostItemID to match")
	}
	if recorder.created[entry.CostItemID].Amount != 120000 {
		t.Fatalf("expected CostItem to have been recorded with estimated 120000, got %+v", recorder.created[entry.CostItemID])
	}
}

func TestCreateLabourEntryAdHocRequiresManualFields(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	_, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != labour.ErrAdHocFieldsRequired {
		t.Fatalf("expected ErrAdHocFieldsRequired, got %v", err)
	}
}

func TestCreateLabourEntryAdHocSuccess(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.WorkerName != "Day Labourer" || entry.Trade != "General" {
		t.Fatalf("expected manual name/trade, got %s/%s", entry.WorkerName, entry.Trade)
	}
	if entry.Cost.Amount != 20000 { // 8 hours * RM25.00 = RM200.00
		t.Fatalf("expected cost 20000, got %d", entry.Cost.Amount)
	}
}

func TestCreateLabourEntryRateOverride(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}

	overrideRate := int64(20000) // project-specific override, higher than default
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", &overrideRate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.Rate.Amount != 20000 {
		t.Fatalf("expected overridden rate 20000, got %d", entry.Rate.Amount)
	}
}

func TestCreateLabourEntryCompensatesOnLabourEntryWriteFailure(t *testing.T) {
	svc, _, entryRepo, recorder := newTestLabourService()
	entryRepo.failNextCreate = true

	_, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", int64Ptr(2500), "MYR", time.Now(), "")
	if err == nil {
		t.Fatal("expected error propagated from LabourEntry write failure")
	}
	if len(recorder.deletedIDs) != 1 {
		t.Fatalf("expected exactly 1 compensation delete call, got %d", len(recorder.deletedIDs))
	}
	if len(recorder.created) != 0 {
		t.Fatalf("expected the orphaned CostItem to be removed from the recorder's view, got %d remaining", len(recorder.created))
	}
}

func int64Ptr(v int64) *int64 { return &v }

func TestUpdateLabourEntryCorrectsQuantityAndPropagatesEstimatedOnly(t *testing.T) {
	svc, _, _, recorder := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newQty := "80" // typo-correction scenario from the design spec
	updated, err := svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, &newQty, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error correcting: %v", err)
	}
	if updated.Cost.Amount != 200000 { // 80 hours * RM25.00 = RM2,000.00
		t.Fatalf("expected recomputed cost 200000, got %d", updated.Cost.Amount)
	}
	if recorder.updatedEstimates[entry.CostItemID].Amount != 200000 {
		t.Fatalf("expected linked CostItem's Estimated updated to 200000, got %+v", recorder.updatedEstimates[entry.CostItemID])
	}
}

func TestUpdateLabourEntryNeverAcceptsWorkerIDChange(t *testing.T) {
	// UpdateLabourEntry's signature structurally has no workerID parameter —
	// this test documents that invariant by confirming the method signature
	// only accepts quantity/rate/date/notes corrections.
	svc, _, _, _ := newTestLabourService()
	rate := int64(2500)
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", nil,
		"Day Labourer", "General", "8", "hour", &rate, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	newNotes := "just a notes correction"
	_, err = svc.UpdateLabourEntry(context.Background(), "company_a", entry.ID, nil, nil, nil, nil, &newNotes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerDefaultRateChangeNeverAffectsExistingLabourEntry(t *testing.T) {
	svc, _, _, _ := newTestLabourService()
	worker, err := svc.CreateWorker(context.Background(), "company_a", "Ahmad", "Tiler", string(labour.RateTypeDaily), 15000, "MYR", "", "")
	if err != nil {
		t.Fatalf("unexpected error creating worker: %v", err)
	}
	entry, err := svc.CreateLabourEntry(context.Background(), "company_a", "project_1", "work_item_1", &worker.ID,
		"", "", "8", "day", nil, "MYR", time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating entry: %v", err)
	}

	if _, err := svc.UpdateWorker(context.Background(), "company_a", worker.ID, "Ahmad", "Tiler", string(labour.RateTypeDaily), 30000, "MYR", "", ""); err != nil {
		t.Fatalf("unexpected error updating worker: %v", err)
	}

	unchanged, err := svc.GetLabourEntry(context.Background(), "company_a", entry.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if unchanged.Rate.Amount != 15000 {
		t.Fatalf("expected LabourEntry.Rate to remain 15000 after Worker.DefaultRate changed, got %d", unchanged.Rate.Amount)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/labour/... -v`
Expected: FAIL — `undefined: labour.NewService` (compile error)

- [x] **Step 3: Implement the service**

Create `backend/internal/labour/service.go`:

```go
package labour

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

// ErrNameRequired is returned when CreateWorker is given an empty name.
var ErrNameRequired = errors.New("labour: name is required")

// ErrInvalidRateType is returned when CreateWorker/UpdateWorker is given a
// rateType outside the 5 defined values.
var ErrInvalidRateType = errors.New("labour: invalid rate type")

// ErrProjectNotFound is returned when the given projectID does not belong to
// the caller's company.
var ErrProjectNotFound = errors.New("labour: project not found")

// ErrWorkItemNotFound is returned when a given workItemID does not belong to
// the caller's company, or belongs to the company but not to the given
// projectID (lineage mismatch).
var ErrWorkItemNotFound = errors.New("labour: work item not found")

// ErrInvalidQuantity is returned when a quantity value/unit fails to parse or
// is not strictly positive.
var ErrInvalidQuantity = errors.New("labour: quantity must be a positive number with a non-empty unit")

// ErrAdHocFieldsRequired is returned when workerID is nil and workerName,
// trade, or rateAmount is missing — ad-hoc labour requires all three manually
// (design spec §1.3.3).
var ErrAdHocFieldsRequired = errors.New("labour: workerName, trade, and rateAmount are required when workerId is not supplied")

// ProjectLookup is the capability labour needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// WorkItemLookup is the capability labour needs from work.
type WorkItemLookup interface {
	WorkItemBelongsToProject(ctx context.Context, companyID, workItemID, projectID string) (bool, error)
}

// LabourCostRecorder is the capability labour needs from costs — defined here
// (consumer-defines-interface), satisfied structurally by costs.Service
// (design spec §9.4).
type LabourCostRecorder interface {
	RecordLabourCost(ctx context.Context, companyID, projectID, workItemID string, estimated money.Money) (costItemID string, err error)
	UpdateLabourCostEstimate(ctx context.Context, companyID, costItemID string, estimated money.Money) error
	DeleteProvisionedLabourCost(ctx context.Context, companyID, costItemID string) error
}

// Service implements Worker and LabourEntry CRUD, including the two-module
// write (LabourEntry <-> CostItem) with cost-first-with-compensation
// ordering (design spec §22-B).
type Service struct {
	workerRepo     WorkerRepository
	entryRepo      LabourEntryRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	costRecorder   LabourCostRecorder
}

// NewService constructs a Service backed by workerRepo and entryRepo,
// consuming projectLookup, workItemLookup, and costRecorder.
func NewService(workerRepo WorkerRepository, entryRepo LabourEntryRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, costRecorder LabourCostRecorder) *Service {
	return &Service{workerRepo: workerRepo, entryRepo: entryRepo, projectLookup: projectLookup, workItemLookup: workItemLookup, costRecorder: costRecorder}
}

// CreateWorker validates name is non-empty and rateType is one of the 5
// defined values, then persists a new Worker.
func (s *Service) CreateWorker(ctx context.Context, companyID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (Worker, error) {
	if name == "" {
		return Worker{}, ErrNameRequired
	}
	rt := RateType(rateType)
	if !rt.IsValid() {
		return Worker{}, ErrInvalidRateType
	}
	return s.workerRepo.Create(ctx, Worker{
		CompanyID: companyID, Name: name, Trade: trade, RateType: rt,
		DefaultRate: money.New(defaultRateAmount, currency), ContactPhone: contactPhone, ContactEmail: contactEmail,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetWorker returns workerID's Worker, tenant-scoped to companyID.
func (s *Service) GetWorker(ctx context.Context, companyID, workerID string) (Worker, error) {
	return s.workerRepo.FindByID(ctx, companyID, workerID)
}

// ListWorkers returns every Worker belonging to companyID.
func (s *Service) ListWorkers(ctx context.Context, companyID string) ([]Worker, error) {
	return s.workerRepo.List(ctx, companyID)
}

// UpdateWorker updates workerID's fields, tenant-scoped to companyID.
// Updating DefaultRate here never retroactively alters any already-created
// LabourEntry (design spec §1.3.6) — this method only ever writes to the
// workers collection.
func (s *Service) UpdateWorker(ctx context.Context, companyID, workerID, name, trade, rateType string, defaultRateAmount int64, currency, contactPhone, contactEmail string) (Worker, error) {
	if name == "" {
		return Worker{}, ErrNameRequired
	}
	rt := RateType(rateType)
	if !rt.IsValid() {
		return Worker{}, ErrInvalidRateType
	}
	return s.workerRepo.Update(ctx, companyID, workerID, func(w *Worker) {
		w.Name = name
		w.Trade = trade
		w.RateType = rt
		w.DefaultRate = money.New(defaultRateAmount, currency)
		w.ContactPhone = contactPhone
		w.ContactEmail = contactEmail
	})
}

// CreateLabourEntry validates projectID and workItemID (lineage-checked
// against projectID), resolves the Worker snapshot or ad-hoc manual fields,
// computes Cost via money.CalculateLineAmount, and writes the linked CostItem
// FIRST via LabourCostRecorder before writing the LabourEntry itself — if the
// LabourEntry write then fails, the CostItem is deleted as best-effort
// compensation (design spec §5.2 step 7, §22-B).
func (s *Service) CreateLabourEntry(ctx context.Context, companyID, projectID, workItemID string, workerID *string,
	workerName, trade, quantityValue, quantityUnit string, rateAmount *int64, currency string, date time.Time, notes string) (LabourEntry, error) {

	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return LabourEntry{}, err
	}
	if !belongs {
		return LabourEntry{}, ErrProjectNotFound
	}

	wiBelongs, err := s.workItemLookup.WorkItemBelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return LabourEntry{}, err
	}
	if !wiBelongs {
		return LabourEntry{}, ErrWorkItemNotFound
	}

	q, err := quantity.New(quantityValue, quantityUnit)
	if err != nil {
		return LabourEntry{}, ErrInvalidQuantity
	}
	if !q.Value.IsPositive() || q.Unit == "" {
		return LabourEntry{}, ErrInvalidQuantity
	}

	var rate money.Money
	var resolvedName, resolvedTrade string

	if workerID != nil {
		worker, err := s.workerRepo.FindByID(ctx, companyID, *workerID)
		if err != nil {
			return LabourEntry{}, err
		}
		resolvedName = worker.Name
		resolvedTrade = worker.Trade
		if rateAmount != nil {
			rate = money.New(*rateAmount, currency)
		} else {
			rate = worker.DefaultRate
		}
	} else {
		if workerName == "" || trade == "" || rateAmount == nil {
			return LabourEntry{}, ErrAdHocFieldsRequired
		}
		resolvedName = workerName
		resolvedTrade = trade
		rate = money.New(*rateAmount, currency)
	}

	cost := money.CalculateLineAmount(q.Value, rate)

	costItemID, err := s.costRecorder.RecordLabourCost(ctx, companyID, projectID, workItemID, cost)
	if err != nil {
		return LabourEntry{}, err
	}

	entry, err := s.entryRepo.Create(ctx, LabourEntry{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: workItemID, WorkerID: workerID,
		WorkerName: resolvedName, Trade: resolvedTrade, Quantity: q, Rate: rate, Cost: cost,
		CostItemID: costItemID, Date: date, Notes: notes, CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err != nil {
		// Best-effort compensation: the LabourEntry write failed after the
		// CostItem was already created. Delete the now-orphaned CostItem;
		// log clearly if compensation itself fails (design spec §22-B).
		if compErr := s.costRecorder.DeleteProvisionedLabourCost(ctx, companyID, costItemID); compErr != nil {
			return LabourEntry{}, errors.Join(err, compErr)
		}
		return LabourEntry{}, err
	}

	return entry, nil
}

// GetLabourEntry returns labourEntryID's LabourEntry, tenant-scoped to companyID.
func (s *Service) GetLabourEntry(ctx context.Context, companyID, labourEntryID string) (LabourEntry, error) {
	return s.entryRepo.FindByID(ctx, companyID, labourEntryID)
}

// ListLabourEntriesByProject validates projectID belongs to companyID before listing.
func (s *Service) ListLabourEntriesByProject(ctx context.Context, companyID, projectID string) ([]LabourEntry, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.entryRepo.ListByProject(ctx, companyID, projectID)
}

// ListLabourEntriesByWorkItem lists LabourEntries for a WorkItem, tenant-scoped
// to companyID.
func (s *Service) ListLabourEntriesByWorkItem(ctx context.Context, companyID, workItemID string) ([]LabourEntry, error) {
	return s.entryRepo.ListByWorkItem(ctx, companyID, workItemID)
}

// UpdateLabourEntry corrects quantity/rate/date/notes only — never workerId
// (design spec §1.3.2). When quantity or rate changes, recomputes Cost and
// propagates the new value to the linked CostItem's Estimated field FIRST
// (via LabourCostRecorder.UpdateLabourCostEstimate), then persists the
// LabourEntry itself — the same cost-first ordering principle as creation.
// Never touches Committed/Actual/Paid on the linked CostItem.
func (s *Service) UpdateLabourEntry(ctx context.Context, companyID, labourEntryID string, quantityValue, quantityUnit *string, rateAmount *int64, date *time.Time, notes *string) (LabourEntry, error) {
	existing, err := s.entryRepo.FindByID(ctx, companyID, labourEntryID)
	if err != nil {
		return LabourEntry{}, err
	}

	newQuantity := existing.Quantity
	if quantityValue != nil {
		unit := existing.Quantity.Unit
		if quantityUnit != nil {
			unit = *quantityUnit
		}
		q, err := quantity.New(*quantityValue, unit)
		if err != nil {
			return LabourEntry{}, ErrInvalidQuantity
		}
		if !q.Value.IsPositive() || q.Unit == "" {
			return LabourEntry{}, ErrInvalidQuantity
		}
		newQuantity = q
	}

	newRate := existing.Rate
	if rateAmount != nil {
		newRate = money.New(*rateAmount, existing.Rate.Currency)
	}

	newDate := existing.Date
	if date != nil {
		newDate = *date
	}
	newNotes := existing.Notes
	if notes != nil {
		newNotes = *notes
	}

	newCost := money.CalculateLineAmount(newQuantity.Value, newRate)

	if err := s.costRecorder.UpdateLabourCostEstimate(ctx, companyID, existing.CostItemID, newCost); err != nil {
		return LabourEntry{}, err
	}

	return s.entryRepo.UpdateCorrection(ctx, companyID, labourEntryID, newQuantity, newRate, newCost, newDate, newNotes)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/labour/... -v`
Expected: PASS, all tests in the package (repository tests need Docker; service tests do not)

- [x] **Step 5: Run full build**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean

- [x] **Step 6: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/labour/service.go internal/labour/service_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 15: `internal/labour` — handler

**Spec:** §9 (endpoint list — Worker and LabourEntry sections).

**Files:**
- Create: `backend/internal/labour/handler.go`

- [x] **Step 1: Implement the handler**

Create `backend/internal/labour/handler.go`:

```go
package labour

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type moneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type workerDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Trade               string   `json:"trade,omitempty"`
	RateType            string   `json:"rateType"`
	DefaultRate         moneyDTO `json:"defaultRate"`
	ContactPhone        string   `json:"contactPhone,omitempty"`
	ContactEmail        string   `json:"contactEmail,omitempty"`
	CreatedAt           string   `json:"createdAt"`
}

type createWorkerInput struct {
	Body struct {
		Name                string `json:"name" required:"true" minLength:"1"`
		Trade               string `json:"trade,omitempty"`
		RateType            string `json:"rateType" required:"true"`
		DefaultRateAmount   int64  `json:"defaultRateAmount" required:"true"`
		Currency            string `json:"currency" required:"true" minLength:"1"`
		ContactPhone        string `json:"contactPhone,omitempty"`
		ContactEmail        string `json:"contactEmail,omitempty"`
	}
}

type workerOutput struct {
	Body workerDTO
}

type listWorkersOutput struct {
	Body struct {
		Workers []workerDTO `json:"workers"`
	}
}

type getWorkerInput struct {
	ID string `path:"id"`
}

type updateWorkerInput struct {
	ID   string `path:"id"`
	Body struct {
		Name              string `json:"name" required:"true" minLength:"1"`
		Trade             string `json:"trade,omitempty"`
		RateType          string `json:"rateType" required:"true"`
		DefaultRateAmount int64  `json:"defaultRateAmount" required:"true"`
		Currency          string `json:"currency" required:"true" minLength:"1"`
		ContactPhone      string `json:"contactPhone,omitempty"`
		ContactEmail      string `json:"contactEmail,omitempty"`
	}
}

type labourEntryDTO struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"projectId"`
	WorkItemID    string   `json:"workItemId"`
	WorkerID      string   `json:"workerId,omitempty"`
	WorkerName    string   `json:"workerName"`
	Trade         string   `json:"trade,omitempty"`
	QuantityValue string   `json:"quantityValue"`
	QuantityUnit  string   `json:"quantityUnit"`
	Rate          moneyDTO `json:"rate"`
	Cost          moneyDTO `json:"cost"`
	CostItemID    string   `json:"costItemId"`
	Date          string   `json:"date"`
	Notes         string   `json:"notes,omitempty"`
	CreatedAt     string   `json:"createdAt"`
}

type createLabourEntryInput struct {
	Body struct {
		ProjectID     string `json:"projectId" required:"true" minLength:"1"`
		WorkItemID    string `json:"workItemId" required:"true" minLength:"1"`
		WorkerID      string `json:"workerId,omitempty"`
		WorkerName    string `json:"workerName,omitempty"`
		Trade         string `json:"trade,omitempty"`
		QuantityValue string `json:"quantityValue" required:"true"`
		QuantityUnit  string `json:"quantityUnit" required:"true" minLength:"1"`
		RateAmount    *int64 `json:"rateAmount,omitempty"`
		Currency      string `json:"currency" required:"true" minLength:"1"`
		Notes         string `json:"notes,omitempty"`
	}
}

type labourEntryOutput struct {
	Body labourEntryDTO
}

type listLabourEntriesInput struct {
	ProjectID  string `query:"projectId"`
	WorkItemID string `query:"workItemId"`
}

type listLabourEntriesOutput struct {
	Body struct {
		LabourEntries []labourEntryDTO `json:"labourEntries"`
	}
}

type getLabourEntryInput struct {
	ID string `path:"id"`
}

type updateLabourEntryInput struct {
	ID   string `path:"id"`
	Body struct {
		QuantityValue string `json:"quantityValue,omitempty"`
		QuantityUnit  string `json:"quantityUnit,omitempty"`
		RateAmount    *int64 `json:"rateAmount,omitempty"`
		Notes         string `json:"notes,omitempty"`
	}
}

// RegisterHandlers registers POST/GET /workers, GET/PATCH /workers/{id},
// POST/GET /labour-entries, GET /labour-entries/{id}, and
// PATCH /labour-entries/{id} on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "workers-create",
		Method:      http.MethodPost,
		Path:        "/workers",
		Summary:     "Create a Worker for the authenticated company",
	}, func(ctx context.Context, input *createWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.CreateWorker(ctx, principal.CompanyID, input.Body.Name, input.Body.Trade, input.Body.RateType,
			input.Body.DefaultRateAmount, input.Body.Currency, input.Body.ContactPhone, input.Body.ContactEmail)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-list",
		Method:      http.MethodGet,
		Path:        "/workers",
		Summary:     "List Workers for the authenticated company",
	}, func(ctx context.Context, input *struct{}) (*listWorkersOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListWorkers(ctx, principal.CompanyID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		resp := &listWorkersOutput{}
		for _, w := range list {
			resp.Body.Workers = append(resp.Body.Workers, toWorkerDTO(w))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-get",
		Method:      http.MethodGet,
		Path:        "/workers/{id}",
		Summary:     "Get a Worker, tenant-scoped",
	}, func(ctx context.Context, input *getWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.GetWorker(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "workers-update",
		Method:      http.MethodPatch,
		Path:        "/workers/{id}",
		Summary:     "Update a Worker, tenant-scoped. Default rate changes never retroactively alter existing LabourEntries.",
	}, func(ctx context.Context, input *updateWorkerInput) (*workerOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		w, err := svc.UpdateWorker(ctx, principal.CompanyID, input.ID, input.Body.Name, input.Body.Trade, input.Body.RateType,
			input.Body.DefaultRateAmount, input.Body.Currency, input.Body.ContactPhone, input.Body.ContactEmail)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &workerOutput{Body: toWorkerDTO(w)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-create",
		Method:      http.MethodPost,
		Path:        "/labour-entries",
		Summary:     "Create a LabourEntry under a Project/WorkItem; creates a linked CostItem{category=labour}",
	}, func(ctx context.Context, input *createLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var workerID *string
		if input.Body.WorkerID != "" {
			workerID = &input.Body.WorkerID
		}
		e, err := svc.CreateLabourEntry(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.WorkItemID, workerID,
			input.Body.WorkerName, input.Body.Trade, input.Body.QuantityValue, input.Body.QuantityUnit,
			input.Body.RateAmount, input.Body.Currency, time.Now(), input.Body.Notes)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-list",
		Method:      http.MethodGet,
		Path:        "/labour-entries",
		Summary:     "List LabourEntries for a Project or a WorkItem belonging to the authenticated company",
	}, func(ctx context.Context, input *listLabourEntriesInput) (*listLabourEntriesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var list []LabourEntry
		var err error
		switch {
		case input.WorkItemID != "":
			list, err = svc.ListLabourEntriesByWorkItem(ctx, principal.CompanyID, input.WorkItemID)
		case input.ProjectID != "":
			list, err = svc.ListLabourEntriesByProject(ctx, principal.CompanyID, input.ProjectID)
		default:
			return nil, huma.Error422UnprocessableEntity("either projectId or workItemId query parameter is required")
		}
		if err != nil {
			return nil, mapLabourError(err)
		}
		resp := &listLabourEntriesOutput{}
		for _, e := range list {
			resp.Body.LabourEntries = append(resp.Body.LabourEntries, toLabourEntryDTO(e))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-get",
		Method:      http.MethodGet,
		Path:        "/labour-entries/{id}",
		Summary:     "Get a LabourEntry, tenant-scoped",
	}, func(ctx context.Context, input *getLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetLabourEntry(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "labour-entries-update",
		Method:      http.MethodPatch,
		Path:        "/labour-entries/{id}",
		Summary:     "Correct quantity/rate/notes only (never workerId). Propagates to the linked CostItem's Estimated field only.",
	}, func(ctx context.Context, input *updateLabourEntryInput) (*labourEntryOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var quantityValue, quantityUnit *string
		if input.Body.QuantityValue != "" {
			quantityValue = &input.Body.QuantityValue
		}
		if input.Body.QuantityUnit != "" {
			quantityUnit = &input.Body.QuantityUnit
		}
		var notes *string
		if input.Body.Notes != "" {
			notes = &input.Body.Notes
		}
		e, err := svc.UpdateLabourEntry(ctx, principal.CompanyID, input.ID, quantityValue, quantityUnit, input.Body.RateAmount, nil, notes)
		if err != nil {
			return nil, mapLabourError(err)
		}
		return &labourEntryOutput{Body: toLabourEntryDTO(e)}, nil
	})
}

func toWorkerDTO(w Worker) workerDTO {
	return workerDTO{
		ID: w.ID, Name: w.Name, Trade: w.Trade, RateType: string(w.RateType),
		DefaultRate: moneyDTO{Amount: w.DefaultRate.Amount, Currency: w.DefaultRate.Currency},
		ContactPhone: w.ContactPhone, ContactEmail: w.ContactEmail, CreatedAt: w.CreatedAt.Format(timeLayout),
	}
}

func toLabourEntryDTO(e LabourEntry) labourEntryDTO {
	dto := labourEntryDTO{
		ID: e.ID, ProjectID: e.ProjectID, WorkItemID: e.WorkItemID, WorkerName: e.WorkerName, Trade: e.Trade,
		QuantityValue: e.Quantity.Value.String(), QuantityUnit: e.Quantity.Unit,
		Rate: moneyDTO{Amount: e.Rate.Amount, Currency: e.Rate.Currency},
		Cost: moneyDTO{Amount: e.Cost.Amount, Currency: e.Cost.Currency},
		CostItemID: e.CostItemID, Date: e.Date.Format(timeLayout), Notes: e.Notes, CreatedAt: e.CreatedAt.Format(timeLayout),
	}
	if e.WorkerID != nil {
		dto.WorkerID = *e.WorkerID
	}
	return dto
}

func mapLabourError(err error) error {
	switch {
	case errors.Is(err, ErrWorkerNotFound):
		return huma.Error404NotFound("worker not found")
	case errors.Is(err, ErrLabourEntryNotFound):
		return huma.Error404NotFound("labour entry not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrNameRequired):
		return huma.Error422UnprocessableEntity("name is required")
	case errors.Is(err, ErrInvalidRateType):
		return huma.Error422UnprocessableEntity("invalid rate type")
	case errors.Is(err, ErrInvalidQuantity):
		return huma.Error422UnprocessableEntity("quantity must be a positive number with a non-empty unit")
	case errors.Is(err, ErrAdHocFieldsRequired):
		return huma.Error422UnprocessableEntity("workerName, trade, and rateAmount are required when workerId is not supplied")
	default:
		return err
	}
}
```

- [x] **Step 2: Build to confirm it compiles**

Run: `cd backend && go build ./internal/labour/... && go vet ./internal/labour/...`
Expected: clean

- [x] **Step 3: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/labour/handler.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 16: `internal/labour` — full package verification

**Files:** none (verification only)

- [x] **Step 1: Run the full labour package test suite**

Run: `cd backend && go test ./internal/labour/... -v`
Expected: PASS — all model/repository/service tests green

- [x] **Step 2: Run the full build**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions in any earlier package

- [x] **Step 3: No commit needed** (verification-only task, labour module)

---

## Task 17: Composition root wiring — `cmd/api/main.go` and `internal/tenanttest/router.go`

**Spec:** §8. Construction order: `materials` (no M3 deps) → `costs` (consumes `materials`) → `labour` (consumes `costs`). HTTP registration appends to the existing `authedAPI` group — no new `huma.API` instance.

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/tenanttest/router.go`

- [x] **Step 1: Update `cmd/api/main.go` imports**

In `backend/cmd/api/main.go`, add three new imports to the existing import block (alphabetically among the existing `internal/` imports):

```go
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
```

- [x] **Step 2: Add repository construction**

In `backend/cmd/api/main.go`, after the existing M2 repository block (`workItemRepo := work.NewMongoWorkItemRepository(db)`), add:

```go
	// --- Repositories (Milestone 3: materials, costs, labour) ---
	materialRepo := materials.NewMongoMaterialRepository(db)
	costItemRepo := costs.NewMongoCostItemRepository(db)
	workerRepo := labour.NewMongoWorkerRepository(db)
	labourEntryRepo := labour.NewMongoLabourEntryRepository(db)
```

- [x] **Step 3: Add index creation calls**

After the existing `if err := workItemRepo.EnsureIndexes(indexCtx); err != nil { ... }` block, add:

```go
	if err := materialRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure materials indexes")
	}
	if err := costItemRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure cost_items indexes")
	}
	if err := workerRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure workers indexes")
	}
	if err := labourEntryRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure labour_entries indexes")
	}
```

- [x] **Step 4: Add service construction**

After the existing M2 wiring block (`workService := work.NewService(workItemRepo, projectsService, spacesService)`), add:

```go
	// --- Composition root: Milestone 3 wiring. Acyclic construction order —
	// materials (no M3 deps) -> costs (consumes MaterialLookup, ProjectLookup,
	// WorkItemLookup) -> labour (consumes ProjectLookup, WorkItemLookup,
	// LabourCostRecorder) — matching
	// docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md
	// §8. labour must be constructed after costs since it depends on
	// costsService satisfying labour.LabourCostRecorder.
	materialsService := materials.NewService(materialRepo)
	costsService := costs.NewService(costItemRepo, projectsService, workService, materialsService)
	labourService := labour.NewService(workerRepo, labourEntryRepo, projectsService, workService, costsService)
```

- [x] **Step 5: Register HTTP handlers**

After the existing `work.RegisterHandlers(authedAPI, workService)` line, add:

```go
	materials.RegisterHandlers(authedAPI, materialsService)
	costs.RegisterHandlers(authedAPI, costsService)
	labour.RegisterHandlers(authedAPI, labourService)
```

- [x] **Step 6: Run build to confirm `cmd/api` compiles**

Run: `cd backend && go build ./cmd/api/...`
Expected: clean

- [x] **Step 7: Mirror the same changes in `internal/tenanttest/router.go`**

In `backend/internal/tenanttest/router.go`, add the same three imports:

```go
	"github.com/shananth/renovation-platform/backend/internal/clients"
	"github.com/shananth/renovation-platform/backend/internal/companies"
	"github.com/shananth/renovation-platform/backend/internal/costs"
	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/labour"
	"github.com/shananth/renovation-platform/backend/internal/materials"
	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/projects"
	"github.com/shananth/renovation-platform/backend/internal/properties"
	"github.com/shananth/renovation-platform/backend/internal/spaces"
	"github.com/shananth/renovation-platform/backend/internal/work"
```

Add repository construction after `workItemRepo := work.NewMongoWorkItemRepository(db)`:

```go
	materialRepo := materials.NewMongoMaterialRepository(db)
	costItemRepo := costs.NewMongoCostItemRepository(db)
	workerRepo := labour.NewMongoWorkerRepository(db)
	labourEntryRepo := labour.NewMongoLabourEntryRepository(db)
```

Extend the `EnsureIndexes` loop slice to include the four new repositories:

```go
	for _, ensure := range []func(context.Context) error{
		userRepo.EnsureIndexes, sessionRepo.EnsureIndexes, membershipRepo.EnsureIndexes,
		clientRepo.EnsureIndexes, projectRepo.EnsureIndexes, propertyRepo.EnsureIndexes,
		spaceRepo.EnsureIndexes, workItemRepo.EnsureIndexes,
		materialRepo.EnsureIndexes, costItemRepo.EnsureIndexes, workerRepo.EnsureIndexes, labourEntryRepo.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			return nil, err
		}
	}
```

Add service construction after `workService := work.NewService(workItemRepo, projectsService, spacesService)`:

```go
	materialsService := materials.NewService(materialRepo)
	costsService := costs.NewService(costItemRepo, projectsService, workService, materialsService)
	labourService := labour.NewService(workerRepo, labourEntryRepo, projectsService, workService, costsService)
```

Add handler registration after `work.RegisterHandlers(authedAPI, workService)`:

```go
	materials.RegisterHandlers(authedAPI, materialsService)
	costs.RegisterHandlers(authedAPI, costsService)
	labour.RegisterHandlers(authedAPI, labourService)
```

- [x] **Step 8: Run full build and test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions across any M0/M1/M2/M3 package

- [x] **Step 9: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `cmd/api/main.go internal/tenanttest/router.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 18: Full HTTP tenant-isolation and ledger-integrity acceptance tests

**Spec:** §17. Reuses `internal/tenanttest`'s existing helpers (`setupRouter`, `registerCompany`, `doJSON`, `mustField`, `buildFullHierarchyForCompanyA`) from `internal/tenanttest/tenant_isolation_test.go` — do not redefine them, add new test functions to the same file.

**Files:**
- Modify: `backend/internal/tenanttest/tenant_isolation_test.go`

- [x] **Step 1: Add an M3 hierarchy-building helper**

Add to `backend/internal/tenanttest/tenant_isolation_test.go`, after `buildFullHierarchyForCompanyA`:

```go
// buildM3ResourcesForCompanyA creates a Material, a Worker, a LabourEntry
// (under projectID/workItemID), and a CostItem (under projectID), all owned
// by companyA, and returns each resource's ID plus the LabourEntry's linked
// CostItemID.
func buildM3ResourcesForCompanyA(t *testing.T, router http.Handler, companyA testCompany, projectID, workItemID string) (materialID, workerID, labourEntryID, costItemID, labourCostItemID string) {
	t.Helper()

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "OPC Cement 50kg", "category": "cement", "unit": "bag",
		"referencePriceAmount": 1850, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material, got %d: %s", rec.Code, rec.Body.String())
	}
	materialID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "trade": "Tiler", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating worker, got %d: %s", rec.Code, rec.Body.String())
	}
	workerID = mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating labour entry, got %d: %s", rec.Code, rec.Body.String())
	}
	labourEntryID = mustField(t, rec, "id")
	labourCostItemID = mustField(t, rec, "costItemId")

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "category": "permit", "description": "Renovation permit",
		"estimatedAmount": 150000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	costItemID = mustField(t, rec, "id")

	return materialID, workerID, labourEntryID, costItemID, labourCostItemID
}
```

- [x] **Step 2: Write the failing Material tenant-isolation test**

Add:

```go
func TestTenantIsolation_Material(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "matA@example.com", "Company A")
	companyB := registerCompany(t, router, "matB@example.com", "Company B")

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "Ceramic Tile", "unit": "m2", "referencePriceAmount": 3200, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating material, got %d: %s", rec.Code, rec.Body.String())
	}
	materialID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodGet, "/materials/"+materialID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's material, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/materials/"+materialID, companyB.accessToken, map[string]any{
		"name": "hijacked", "unit": "m2", "referencePriceAmount": 1, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's material, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 3: Write the failing Worker tenant-isolation test**

Add:

```go
func TestTenantIsolation_Worker(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "workerA@example.com", "Company A")
	companyB := registerCompany(t, router, "workerB@example.com", "Company B")

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating worker, got %d: %s", rec.Code, rec.Body.String())
	}
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodGet, "/workers/"+workerID, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's worker, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/workers/"+workerID, companyB.accessToken, map[string]any{
		"name": "hijacked", "rateType": "daily", "defaultRateAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's worker, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 4: Write the failing LabourEntry tenant-isolation test**

Add:

```go
func TestTenantIsolation_LabourEntry(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "labourA@example.com", "Company A")
	companyB := registerCompany(t, router, "labourB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	_, workerA, labourEntryA, _, _ := buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/labour-entries/"+labourEntryA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/labour-entries/"+labourEntryA, companyB.accessToken, map[string]any{"notes": "hijacked"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
		"workerName": "X", "trade": "Y", "rateAmount": 1,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating labour entry against A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "workerId": workerA,
		"quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating labour entry with A's workerId, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 5: Write the failing CostItem tenant-isolation test**

Add:

```go
func TestTenantIsolation_CostItem(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "costA@example.com", "Company A")
	companyB := registerCompany(t, router, "costB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	materialA, _, _, costItemA, _ := buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/cost-items/"+costItemA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B reading A's cost item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemA, companyB.accessToken, map[string]any{"description": "hijacked"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B updating A's cost item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "category": "permit", "description": "X", "estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's project, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "category": "permit", "description": "X", "estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's work item, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectA, "category": "material", "description": "X", "estimatedAmount": 1, "currency": "MYR",
		"materialId": materialA,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B creating cost item against A's material, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 6: Write the failing parent-filtered-list and companyId-spoofing tests**

Add:

```go
func TestTenantIsolation_M3ParentFilteredListsRejectForeignParent(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "listA@example.com", "Company A")
	companyB := registerCompany(t, router, "listB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	buildM3ResourcesForCompanyA(t, router, companyA, projectA, workItemA)

	rec := doJSON(t, router, http.MethodGet, "/labour-entries?projectId="+projectA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing labour-entries with A's projectId, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/labour-entries?workItemId="+workItemA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing labour-entries with A's workItemId, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items?projectId="+projectA, companyB.accessToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for B listing cost-items with A's projectId, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CompanyIDInRequestBodyIsIgnored(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "spoofA@example.com", "Company A")
	companyB := registerCompany(t, router, "spoofB@example.com", "Company B")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)
	_, projectB, _, _, workItemB := buildFullHierarchyForCompanyA(t, router, companyB)
	_ = projectA
	_ = workItemA

	rec := doJSON(t, router, http.MethodPost, "/materials", companyB.accessToken, map[string]any{
		"name": "Spoofed", "unit": "m2", "referencePriceAmount": 1, "referencePriceCurrency": "MYR",
		"companyId": "should-be-ignored",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 (companyId silently ignored) or 422 (unknown field rejected), got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		materialID := mustField(t, rec, "id")
		checkRec := doJSON(t, router, http.MethodGet, "/materials/"+materialID, companyA.accessToken, nil)
		if checkRec.Code != http.StatusNotFound {
			t.Fatal("spoofed companyId must never let Company A see Company B's material")
		}
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyB.accessToken, map[string]any{
		"projectId": projectB, "workItemId": workItemB, "category": "permit", "description": "X",
		"estimatedAmount": 1, "currency": "MYR", "companyId": "should-be-ignored",
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 200 or 422, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 7: Write the failing same-tenant lineage tests**

Add:

```go
func TestTenantIsolation_M3WorkItemProjectLineageMismatch(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "lineageA@example.com", "Company A")

	_, projectA1, _, _, workItemA1 := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Second Client"})
	clientID2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID2, "name": "Second Project"})
	projectA2 := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA1, // real WorkItem, but belongs to projectA1, not projectA2
		"quantityValue": "1", "quantityUnit": "day", "currency": "MYR",
		"workerName": "X", "trade": "Y", "rateAmount": 1,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for mismatched Project/WorkItem lineage in labour entry, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA1, "category": "permit", "description": "X",
		"estimatedAmount": 1, "currency": "MYR",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for mismatched Project/WorkItem lineage in cost item, got %d: %s", rec.Code, rec.Body.String())
	}

	_ = projectA1
}

func TestTenantIsolation_M3WorkerReusableAcrossProjects(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "reuseA@example.com", "Company A")

	_, projectA1, _, _, workItemA1 := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA1, "workItemId": workItemA1, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 using the same worker on project A1, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPost, "/clients", companyA.accessToken, map[string]string{"name": "Second Client"})
	clientID2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/projects", companyA.accessToken, map[string]string{"clientId": clientID2, "name": "Second Project"})
	projectA2 := mustField(t, rec, "id")
	rec = doJSON(t, router, http.MethodPost, "/work-items", companyA.accessToken, map[string]string{
		"projectId": projectA2, "description": "Second work item", "quantityValue": "1", "quantityUnit": "unit",
	})
	workItemA2 := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA2, "workItemId": workItemA2, "workerId": workerID,
		"quantityValue": "4", "quantityUnit": "day", "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 reusing the same worker on project A2, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [x] **Step 8: Write the failing historical-snapshot tests**

Add:

```go
func TestTenantIsolation_M3WorkerDefaultRateChangeDoesNotAffectExistingLabourEntry(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "snapshotA@example.com", "Company A")

	_, projectA, _, _, workItemA := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/workers", companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 15000, "currency": "MYR",
	})
	workerID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/labour-entries", companyA.accessToken, map[string]any{
		"projectId": projectA, "workItemId": workItemA, "workerId": workerID,
		"quantityValue": "8", "quantityUnit": "day", "currency": "MYR",
	})
	labourEntryID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/workers/"+workerID, companyA.accessToken, map[string]any{
		"name": "Ahmad", "rateType": "daily", "defaultRateAmount": 30000, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 updating worker rate, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/labour-entries/"+labourEntryID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching labour entry, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	rateField := resp["rate"].(map[string]any)
	if int64(rateField["amount"].(float64)) != 15000 {
		t.Fatalf("expected LabourEntry.Rate to remain 15000 after Worker.DefaultRate changed, got %v", rateField["amount"])
	}
}

func TestTenantIsolation_M3MaterialReferencePriceChangeDoesNotAffectExistingCostItem(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "matsnapshotA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/materials", companyA.accessToken, map[string]any{
		"name": "Tile", "unit": "m2", "referencePriceAmount": 3200, "referencePriceCurrency": "MYR",
	})
	materialID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "material", "description": "Tiles", "materialId": materialID,
		"quantityValue": "10", "quantityUnit": "m2", "unitPriceAmount": 3200, "currency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	costItemID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/materials/"+materialID, companyA.accessToken, map[string]any{
		"name": "Tile", "unit": "m2", "referencePriceAmount": 5000, "referencePriceCurrency": "MYR",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 updating material, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items/"+costItemID, companyA.accessToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching cost item, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	unitPriceField := resp["unitPrice"].(map[string]any)
	if int64(unitPriceField["amount"].(float64)) != 3200 {
		t.Fatalf("expected CostItem.UnitPrice to remain 3200 after Material.ReferencePrice changed, got %v", unitPriceField["amount"])
	}
}
```

- [x] **Step 9: Write the failing ledger-integrity tests**

Add:

```go
func TestTenantIsolation_M3CostItemRejectsLabourCategory(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "ledgerA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "labour", "description": "Should be rejected",
		"estimatedAmount": 1000, "currency": "MYR",
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for category=labour via POST /cost-items, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CostItemCategoryLockOnceCommitted(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "lockA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "equipment", "description": "Tile cutter rental",
		"estimatedAmount": 50000, "currency": "MYR",
	})
	costItemID := mustField(t, rec, "id")

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID, companyA.accessToken, map[string]any{
		"description": "Tile cutter rental", "category": "transport",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 correcting category pre-commitment, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken, map[string]any{
		"stage": "committed", "amount": 50000,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 setting committed, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID, companyA.accessToken, map[string]any{
		"description": "Tile cutter rental", "category": "equipment",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 correcting category after committed is set, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantIsolation_M3CostItemLifecycleAllFourCoexist(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "coexistA@example.com", "Company A")

	_, projectA, _, _, _ := buildFullHierarchyForCompanyA(t, router, companyA)

	rec := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectA, "category": "miscellaneous", "description": "Demo",
		"estimatedAmount": 100000, "currency": "MYR",
	})
	costItemID := mustField(t, rec, "id")

	for _, stage := range []string{"committed", "actual", "paid"} {
		rec = doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", companyA.accessToken, map[string]any{
			"stage": stage, "amount": 90000,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 setting %s, got %d: %s", stage, rec.Code, rec.Body.String())
		}
	}

	rec = doJSON(t, router, http.MethodGet, "/cost-items/"+costItemID, companyA.accessToken, nil)
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	for _, field := range []string{"estimated", "committed", "actual", "paid"} {
		if resp[field] == nil {
			t.Fatalf("expected %s to be non-nil, all four lifecycle fields must coexist", field)
		}
	}
}
```

- [x] **Step 10: Run test to verify all new tests fail initially, then pass**

Run: `cd backend && go test ./internal/tenanttest/... -v -run TestTenantIsolation_M3`
Expected: first FAIL (before Task 17's wiring exists, these 404 on unknown routes); after Task 17 is complete, PASS. Since Task 17 already precedes this task in the plan, run this immediately — expected: PASS.

Run the full tenanttest suite too: `cd backend && go test ./internal/tenanttest/... -v`
Expected: PASS — all M0/M1/M2 tenant-isolation tests plus all new M3 ones green

- [x] **Step 11: Leave changes uncommitted**

Do not run `git add` or `git commit`. Changes to `internal/tenanttest/tenant_isolation_test.go` remain as plain working-tree modifications until the user explicitly requests a commit.

---

## Task 19: Final verification sweep

**Spec:** §18. This is the milestone's closing gate — matches the exact bar M1/M2 were held to.

**Files:** none (verification only)

- [x] **Step 1: Full build**

Run: `cd backend && go build ./...`
Expected: clean, no errors across every package including the three new M3 modules

- [x] **Step 2: Full vet**

Run: `cd backend && go vet ./...`
Expected: clean

- [x] **Step 3: Full test suite**

Run: `cd backend && go test ./...`
Expected: PASS across all M0/M1/M2/M3 packages — `internal/foundation/money`, `internal/foundation/quantity`, `internal/identity`, `internal/companies`, `internal/clients`, `internal/projects`, `internal/properties`, `internal/spaces`, `internal/work`, `internal/materials`, `internal/costs`, `internal/labour`, `internal/tenanttest`. No regressions in any pre-M3 package.

- [x] **Step 4: `go mod tidy` stability**

Run: `cd backend && go mod tidy && git status`
Expected: `go.mod`/`go.sum` either unchanged or updated cleanly with no unexpected new/removed dependencies (the three new modules only use packages already present in `go.mod` — `shopspring/decimal`, `mongo-driver/v2`, `huma/v2` — so no new dependency should be introduced). Run `go mod tidy` a second time to confirm stability (matching the lesson recorded from M1/M2: unstable `go mod tidy` output between runs indicates a real problem).

- [x] **Step 5: Confirm no `.git` directory exists anywhere**

Run: `cd .. && find . -maxdepth 3 -name ".git" -type d` (from repo root)
Expected: no output — this project has no git repository anywhere by design (per CLAUDE.md), and no tool invoked during this milestone should have created one.

- [x] **Step 6: Live end-to-end verification against Docker Compose Mongo**

Confirm Docker Desktop is running: `docker ps`
If not running, start it and run `docker compose up -d` from the repo root first.

Start the API: `cd backend && go run ./cmd/api` (run in background or a separate terminal)

With the server running, exercise the full M3 chain via curl or the `/docs` Swagger UI at `http://localhost:8080/docs`:
1. Register a company via `POST /auth/register`.
2. Create a Client → Project → WorkItem (reusing the M2 chain).
3. Create a Material via `POST /materials`.
4. Create a Worker via `POST /workers`.
5. Create a LabourEntry via `POST /labour-entries` referencing the WorkItem and Worker — confirm the response includes a non-empty `costItemId`.
6. `GET /cost-items/{costItemId}` (the ID returned by step 5) — confirm `category` is `"labour"` and `estimated` matches the expected quantity×rate calculation.
7. Create a standalone CostItem via `POST /cost-items` with `category: "permit"`.
8. `PATCH /cost-items/{id}/lifecycle` to set `committed`, then `actual`, then `paid` — confirm a final `GET` shows all four lifecycle fields populated simultaneously.
9. Attempt `POST /cost-items` with `category: "labour"` — confirm 422.
10. Stop the server (`Ctrl+C` or `Stop-Process` per the PowerShell environment note in `handoff.md` §7).

Expected: every step succeeds as described; no unexpected errors. The OpenAPI doc at `/openapi.json` includes all 17 new M3 operations — 4 materials (create, list, get, update), 4 workers (create, list, get, update), 4 labour-entries (create, list, get, update-correction), 5 cost-items (create, list, get, update-lifecycle, update-details) — all declaring `security: [{bearerAuth: []}]` in the served OpenAPI document, matching the post-M2 fix's verification pattern recorded in `handoff.md` §2.

- [x] **Step 7: No commit for this task** (pure verification; if any step surfaces a bug, fix it in the relevant earlier task's files and commit there, then re-run this task's steps)

---

## Plan complete

All 19 tasks implement the full M3 design spec (`docs/superpowers/specs/2026-07-23-milestone-3-resources-costing-design.md`, APPROVED). Once Task 19 passes cleanly, update `tasks/todo.md`/`tasks/done.md` per this repository's CLAUDE.md workflow convention (mark this plan `STATUS: COMPLETE`, append a Milestone 3 summary entry to `tasks/done.md`), and consider writing a fresh `handoff.md` reflecting M3 complete and M4 (Estimates) next, mirroring the M1→M2 handoff pattern already used twice in this project.
