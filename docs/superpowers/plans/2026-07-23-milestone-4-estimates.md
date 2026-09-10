# Milestone 4 — Estimates: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/estimates` — the internal contractor Estimate: a snapshot of M3's `cost_items` ledger with markup/margin pricing, mutable-draft refresh/reprice operations guarded by optimistic concurrency, and an immutable finalize/new-version lifecycle — fully tenant-scoped and wired into the existing single-Huma-API composition root, with one small new capability method on the already-verified M3 `costs` module and three new helpers in `internal/foundation/money`.

**Architecture:** One new module following the exact M1/M2/M3 shape (`model.go` → `repository.go` → `repository_mongo.go` → `service.go` → `handler.go`), plus one new capability method on `costs.Service` (`VisitEstimatedCostItems`) and three new pure functions in `internal/foundation/money` (`AddRateBPS`, `DivideByComplementRateBPS`, `RateBPSFromRatio`). `Estimate` documents are one-per-version; a `Revision int64` field distinct from the business-meaningful `Version int` field guards every draft mutation (`refresh`, pricing recalculation, finalize) against lost updates — finalize freezes Revision rather than incrementing it. Exactly one `draft` may exist per Project at a time, enforced by a MongoDB partial unique index; creating a new `Version` requires the source Estimate to already be `finalized`, enforced explicitly in the service layer before any snapshot work is attempted; `CreateEstimate` itself only ever creates Version 1, rejecting with a named error if the Project already has an Estimate. Version-number allocation uses a bounded-retry strategy implemented in production service code (not merely proven in tests), distinguishing a transient version-number race (retried) from a non-transient one-draft-per-Project collision (rejected immediately, never retried).

**Tech Stack:** Go 1.26.5, chi + Huma v2, mongo-driver v2, shopspring/decimal, MongoDB via Docker Compose, testcontainers-go for integration tests.

**Authority:** `docs/superpowers/specs/2026-07-23-milestone-4-estimates-design.md` (APPROVED, two review rounds) — every task below implements a specific section of that spec; section references are inline. Do not deviate from the approved spec without flagging it back to the user first.

**Working directory for all commands:** `backend/` (per repo convention — `go build`/`go test`/`go vet` from the repo root fail with "directory prefix does not contain main module").

---

## Task ordering rationale

Dependency graph from spec §12: `projects` (unchanged) → `costs` (extended with one new method) → `estimates` (new). Build bottom-up: foundation helpers first (nothing else can be tested without them), then the one `costs` extension, then `estimates` itself layer by layer (model → repo → service → handler), then composition-root wiring, then acceptance tests, then final verification.

- **Task 0**: Add `money.AddRateBPS`, `money.DivideByComplementRateBPS`, `money.RateBPSFromRatio` (spec §18).
- **Task 1**: Extend `costs` with `VisitEstimatedCostItems` (spec §6) — the one required M3 modification.
- **Task 2**: `internal/estimates` domain model (spec §1).
- **Task 3**: `internal/estimates` repository interface + Mongo implementation, including both unique indexes (spec §2, §19, §20).
- **Task 4**: `internal/estimates` pricing/snapshot calculation core — pure functions, no I/O (spec §5-9, §13-15).
- **Task 5**: `internal/estimates.Service` — `CreateEstimate`, `GetEstimate`, `GetLatestEstimate`, `ListEstimatesByProject` (spec §4, §7, §8, §17, §23-25).
- **Task 6**: `internal/estimates.Service` — `RefreshEstimate`, `RecalculatePricing` (spec §16.1-16.2).
- **Task 7**: `internal/estimates.Service` — `FinalizeEstimate`, `CreateNewVersion` (spec §16.3, §17, §21).
- **Task 8**: `internal/estimates` Huma HTTP handlers (spec §23).
- **Task 9**: Composition root wiring (`cmd/api/main.go` + `internal/tenanttest/router.go`).
- **Task 10**: Version-number and Revision concurrency integration tests (spec §21, §27).
- **Task 11**: Full HTTP tenant-isolation and snapshot-immutability acceptance tests (spec §28-30).
- **Task 12**: Final verification sweep.

---

## Task 0: Add `RateBPS` arithmetic helpers to `internal/foundation/money`

**Spec:** §18. Three new pure functions alongside the existing `CalculateLineAmount`/`RoundToMinorUnits` in `rounding.go`. `RateBPS` is already defined (`internal/foundation/money/rate.go`) with `BasisPointsDenominator = 10000` — these are the first arithmetic functions to operate on it.

**IMPORTANT — package style:** `backend/internal/foundation/money/rounding_test.go` is a white-box test file (`package money`, not `package money_test`) — confirmed by inspecting the existing file: its two existing tests (`TestRoundToMinorUnitsHalfUp`, `TestCalculateLineAmountBasicMultiplication`) call `RoundToMinorUnits(...)`/`New(...)`/`CalculateLineAmount(...)` directly, with NO `money.` package-qualifier prefix. The three new tests below MUST follow the exact same style — do not self-import the package or prefix calls with `money.`, matching the M3 plan's own established instruction to inspect and preserve a file's existing package style before adding to it.

**Files:**
- Modify: `backend/internal/foundation/money/rounding.go`
- Test: `backend/internal/foundation/money/rounding_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `backend/internal/foundation/money/rounding_test.go` (note: no `money.` prefix anywhere — this file is `package money`):

```go
func TestAddRateBPS(t *testing.T) {
	cost := New(10000, "MYR") // RM100.00
	result := AddRateBPS(cost, 2000) // 20% markup
	if result.Amount != 12000 || result.Currency != "MYR" {
		t.Fatalf("expected 12000 MYR (20%% markup on RM100), got %+v", result)
	}
}

func TestAddRateBPSZeroRate(t *testing.T) {
	cost := New(10000, "MYR")
	result := AddRateBPS(cost, 0)
	if result.Amount != 10000 {
		t.Fatalf("expected 0%% markup to return the cost unchanged, got %+v", result)
	}
}

func TestDivideByComplementRateBPS(t *testing.T) {
	cost := New(10000, "MYR") // RM100.00
	result := DivideByComplementRateBPS(cost, 2000) // 20% margin
	if result.Amount != 12500 || result.Currency != "MYR" {
		t.Fatalf("expected 12500 MYR (20%% margin on RM100 = RM100/0.8 = RM125), got %+v", result)
	}
}

func TestDivideByComplementRateBPSZeroRate(t *testing.T) {
	cost := New(10000, "MYR")
	result := DivideByComplementRateBPS(cost, 0)
	if result.Amount != 10000 {
		t.Fatalf("expected 0%% margin to return the cost unchanged, got %+v", result)
	}
}

func TestRateBPSFromRatio(t *testing.T) {
	profit := New(2500, "MYR")        // RM25.00 profit
	sellingPrice := New(12500, "MYR") // RM125.00 selling price
	result := RateBPSFromRatio(profit, sellingPrice)
	if result != 2000 {
		t.Fatalf("expected 2000 bps (20%% margin, RM25/RM125), got %d", result)
	}
}

func TestRateBPSFromRatioRounds(t *testing.T) {
	// RM1 profit on RM3 selling price = 33.333...% -> rounds to 3333 bps
	profit := New(100, "MYR")
	sellingPrice := New(300, "MYR")
	result := RateBPSFromRatio(profit, sellingPrice)
	if result != 3333 {
		t.Fatalf("expected 3333 bps (33.33%%, rounded), got %d", result)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/foundation/money/... -run "TestAddRateBPS|TestDivideByComplementRateBPS|TestRateBPSFromRatio" -v`
Expected: FAIL with `undefined: AddRateBPS` (and the other two undefined)

- [ ] **Step 3: Implement the three helpers**

In `backend/internal/foundation/money/rounding.go`, add after `CalculateLineAmount`:

```go
// AddRateBPS computes base × (1 + rate/10000), rounded once via
// RoundToMinorUnits. The markup selling-price formula (M4 design spec §13).
// Callers are responsible for rejecting negative rates before calling —
// this function performs no validation of its own, matching
// CalculateLineAmount's precedent of leaving domain validation to its caller.
func AddRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	rateFraction := decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator))
	amountMajorUnits := baseMajorUnits.Mul(decimal.NewFromInt(1).Add(rateFraction))
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}

// DivideByComplementRateBPS computes base / (1 - rate/10000), rounded once.
// The margin selling-price formula (M4 design spec §13). Callers MUST
// validate 0 <= rate < BasisPointsDenominator before calling — this
// function does not itself guard against rate >= BasisPointsDenominator
// (which would produce a zero or negative divisor); that validation is the
// caller's domain responsibility, matching CalculateLineAmount's precedent.
func DivideByComplementRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	complement := decimal.NewFromInt(1).Sub(decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator)))
	amountMajorUnits := baseMajorUnits.Div(complement)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}

// RateBPSFromRatio computes (numerator/denominator) × 10000, rounded to the
// nearest whole basis point. Used to derive ProjectedGrossMarginBPS from
// ProjectedGrossProfit and ProposedSellingPrice (M4 design spec §15).
// numerator and denominator must share a currency; RateBPSFromRatio does
// not itself validate this (mirrors Money.Add/Subtract's own currency-check
// responsibility living on the caller side when the two values originate
// from the same already-currency-consistent Estimate).
func RateBPSFromRatio(numerator, denominator Money) RateBPS {
	num := decimal.NewFromInt(numerator.Amount)
	den := decimal.NewFromInt(denominator.Amount)
	ratio := num.Div(den).Mul(decimal.NewFromInt(BasisPointsDenominator))
	return RateBPS(ratio.Round(0).IntPart())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/foundation/money/... -v`
Expected: PASS (all money package tests, no regressions)

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction (no `git add`/`git commit` auto-run at any point — committing is always a separate, explicit request), do not stage or commit `backend/internal/foundation/money/rounding.go`/`rounding_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 1: Extend `internal/costs` with `VisitEstimatedCostItems`

**Spec:** §6. The one required M3 modification. Implemented on top of the already-existing `ListCostItemsByProject` — no new repository method needed, matching the spec's own reasoning (mirrors `work.WorkItemBelongsToCompany`'s precedent of building a capability on an existing query).

**Files:**
- Modify: `backend/internal/costs/service.go`
- Modify: `backend/internal/costs/cost_item.go` (package doc comment only)
- Test: `backend/internal/costs/service_test.go`

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/costs/service_test.go`:

```go
func TestServiceVisitEstimatedCostItems(t *testing.T) {
	repo := newFakeCostItemRepository()
	svc := costs.NewService(repo, fakeProjectLookup{belongs: true}, fakeWorkItemLookup{belongs: true}, fakeMaterialLookup{belongs: true})

	ctx := context.Background()
	desc := "Ceramic tiles"
	workItemID := "work_1"
	estimated1 := int64(500000)
	_, err := svc.CreateCostItem(ctx, "company_a", "project_1", &workItemID, costs.CostCategoryMaterial,
		desc, nil, nil, nil, &estimated1, nil, nil, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating cost item with Estimated: %v", err)
	}

	// A CostItem with only Actual set (no Estimated) — must be counted as
	// missing, not visited.
	actual1 := int64(300000)
	_, err = svc.CreateCostItem(ctx, "company_a", "project_1", nil, costs.CostCategoryMiscellaneous,
		"Site cleanup", nil, nil, nil, nil, nil, &actual1, nil, "MYR", nil, time.Now(), "")
	if err != nil {
		t.Fatalf("unexpected error creating cost item with only Actual: %v", err)
	}

	var visited []string
	missingCount, err := svc.VisitEstimatedCostItems(ctx, "company_a", "project_1",
		func(costItemID string, workItemID *string, category, description string, estimated money.Money) error {
			visited = append(visited, description)
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(visited) != 1 || visited[0] != "Ceramic tiles" {
		t.Fatalf("expected exactly one visited CostItem (the one with Estimated set), got %v", visited)
	}
	if missingCount != 1 {
		t.Fatalf("expected missingCount=1 (the Actual-only CostItem), got %d", missingCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/costs/... -run TestServiceVisitEstimatedCostItems -v`
Expected: FAIL with `svc.VisitEstimatedCostItems undefined`

- [ ] **Step 3: Implement `VisitEstimatedCostItems`**

In `backend/internal/costs/service.go`, add after `DeleteProvisionedLabourCost`:

```go
// VisitEstimatedCostItems iterates every CostItem under projectID that has
// a non-nil Estimated value, invoking visit once per CostItem in the order
// returned by ListByProject. Returns the count of CostItems under this
// Project that have NO Estimated value set, so the caller (internal/estimates)
// can apply its own missing-Estimated policy without a second round-trip.
// Satisfies estimates.EstimatedCostSource structurally (M4 design spec §6).
// Uses only primitives and money.Money — no CostItem struct crosses the
// module boundary (ADR 0002).
func (s *Service) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	items, err := s.repo.ListByProject(ctx, companyID, projectID)
	if err != nil {
		return 0, err
	}
	missingCount := 0
	for _, item := range items {
		if item.Estimated == nil {
			missingCount++
			continue
		}
		if err := visit(item.ID, item.WorkItemID, string(item.Category), item.Description, *item.Estimated); err != nil {
			return 0, err
		}
	}
	return missingCount, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/costs/... -v`
Expected: PASS (all costs package tests, no regressions)

- [ ] **Step 5: Update the package doc comment**

In `backend/internal/costs/cost_item.go`, update the package comment (currently ends with "It exposes LabourCostRecorder, consumed by labour.") to also mention the new export:

```go
// costs never imports projects, work, materials, or labour by type. It
// defines ProjectLookup, WorkItemLookup, and MaterialLookup, the three
// capabilities it needs, satisfied structurally by projects.Service,
// work.Service, and materials.Service respectively. It exposes
// LabourCostRecorder, consumed by labour, and VisitEstimatedCostItems,
// consumed by estimates (M4 design spec §6).
package costs
```

- [ ] **Step 6: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/costs/service.go`/`cost_item.go`/`service_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 2: `internal/estimates` domain model

**Spec:** §1. No repository/service/handler yet — just the types.

**Files:**
- Create: `backend/internal/estimates/estimate.go`
- Modify: `backend/internal/estimates/doc.go`

- [ ] **Step 1: Update the package doc comment**

Replace the contents of `backend/internal/estimates/doc.go`:

```go
// Package estimates owns Estimate — the internal contractor's private
// financial calculation (cost snapshot, markup/margin pricing, proposed
// selling price, projected profit) built from the M3 cost_items ledger.
// See phase1.md §20-22 and
// docs/superpowers/specs/2026-07-23-milestone-4-estimates-design.md.
//
// An Estimate is versioned: each Version is its own immutable-once-finalized
// document. Exactly one draft may exist per Project at a time. A draft is a
// genuinely mutable working document — refresh (re-pull cost_items) and
// pricing recalculation both mutate it in place, guarded by a Revision
// optimistic-concurrency counter distinct from the business-meaningful
// Version number. Creating the next Version requires the source Estimate to
// already be finalized.
//
// estimates never imports projects or costs by type. It defines
// ProjectLookup and EstimatedCostSource, the two capabilities it needs,
// satisfied structurally by projects.Service and costs.Service respectively.
package estimates
```

- [ ] **Step 2: Write the domain model**

Create `backend/internal/estimates/estimate.go`:

```go
package estimates

import (
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// EstimateStatus is one of the 2 lifecycle states an Estimate version can be in.
type EstimateStatus string

const (
	EstimateStatusDraft     EstimateStatus = "draft"
	EstimateStatusFinalized EstimateStatus = "finalized"
)

// IsValid reports whether s is one of the 2 defined statuses.
func (s EstimateStatus) IsValid() bool {
	switch s {
	case EstimateStatusDraft, EstimateStatusFinalized:
		return true
	default:
		return false
	}
}

// PricingMode selects which pricing formula PricingRate is interpreted
// under (design spec §13). Markup and margin are never simultaneously
// active — an Estimate has exactly one PricingMode at a time.
type PricingMode string

const (
	PricingModeMarkup PricingMode = "markup"
	PricingModeMargin PricingMode = "margin"
)

// IsValid reports whether m is one of the 2 defined pricing modes.
func (m PricingMode) IsValid() bool {
	switch m {
	case PricingModeMarkup, PricingModeMargin:
		return true
	default:
		return false
	}
}

// Estimate is one immutable-once-finalized version of a Project's internal
// financial calculation. Version is a business-meaningful commercial
// revision counter (only advances via CreateNewVersion); Revision is a
// separate optimistic-concurrency counter guarding in-place draft mutation
// (refresh, pricing recalculation, finalize); SchemaVersion is the document
// schema version (§52). All three are distinct fields with distinct
// meanings (design spec §1).
type Estimate struct {
	ID        string
	CompanyID string
	ProjectID string
	Version   int
	Status    EstimateStatus
	Revision  int64

	Currency string

	Lines                 []EstimateCostLine
	CostSubtotal          money.Money
	ExcludedCostItemCount int

	PricingMode PricingMode
	PricingRate money.RateBPS

	ProposedSellingPrice    money.Money
	ProjectedGrossProfit    money.Money
	ProjectedGrossMarginBPS money.RateBPS

	CreatedAt     time.Time
	RefreshedAt   *time.Time
	FinalizedAt   *time.Time
	SchemaVersion int
}

// EstimateCostLine is one snapshotted cost line, sourced from exactly one
// CostItem at the moment the Estimate was created or last refreshed.
// SourceCostItemID is retained for traceability only — it is never
// dereferenced at read time to fetch a live CostItem.Estimated value; this
// line's own SnapshottedAmount is what the Estimate's totals are computed
// from (design spec §5).
type EstimateCostLine struct {
	SourceCostItemID  string
	WorkItemID        *string
	Category          string
	Description       string
	SnapshottedAmount money.Money
}
```

- [ ] **Step 3: Confirm the module builds**

Run: `cd backend && go build ./internal/estimates/...`
Expected: clean (no tests yet — this task only adds types)

- [ ] **Step 4: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/estimate.go`/`doc.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 3: `internal/estimates` repository interface and Mongo implementation

**Spec:** §2, §19, §20. Both unique indexes (`{companyId, projectId, version}` and the partial `{companyId, projectId}` where `status: draft`) are established here.

**Files:**
- Create: `backend/internal/estimates/repository.go`
- Create: `backend/internal/estimates/repository_mongo.go`
- Test: `backend/internal/estimates/repository_mongo_test.go`

- [ ] **Step 1: Write the repository interface**

Create `backend/internal/estimates/repository.go`:

```go
package estimates

import (
	"context"
	"errors"
)

// ErrEstimateNotFound is returned when an Estimate lookup finds no match —
// including an Estimate that exists but belongs to a different company.
var ErrEstimateNotFound = errors.New("estimates: estimate not found")

// ErrNoEstimatesForProject is returned by FindLatestByProject when a Project
// has no Estimate versions at all.
var ErrNoEstimatesForProject = errors.New("estimates: no estimates exist for this project")

// ErrVersionConflict is returned by Create when the attempted Version
// number collides with the {companyId, projectId, version} unique index —
// i.e. another concurrent writer already claimed that exact version
// number. This is a TRANSIENT condition the caller should resolve by
// re-reading MAX(version) and retrying with a fresh version number
// (design spec §21 steps 1-3/5-6) — distinct from ErrDraftAlreadyExists
// below, which is NOT transient in the same way.
var ErrVersionConflict = errors.New("estimates: version number conflict, retry with a fresh version")

// ErrDraftAlreadyExists is returned by Create when the attempted document
// (status=draft) collides with the {companyId, projectId} PARTIAL unique
// index (design spec §20, §32-3) — i.e. a draft already exists for this
// Project. This is NOT a transient condition to blindly retry: retrying
// "create a new draft" when one already exists will fail again forever.
// The caller's correct response is to surface a 409 pointing at the
// existing draft, not to loop (design spec §21 step 4).
var ErrDraftAlreadyExists = errors.New("estimates: a draft already exists for this project")

// ErrUnclassifiedDuplicateKey is returned by Create when MongoDB reports a
// duplicate-key error that does not match either known unique index by
// name (design spec §20-21, added in the third plan review round). Unlike
// ErrVersionConflict, this is deliberately NOT retried anywhere — an error
// this function cannot positively identify must not be assumed safe to
// retry; it surfaces as an ordinary internal error (500) instead.
var ErrUnclassifiedDuplicateKey = errors.New("estimates: unclassified duplicate-key error")

// EstimateRepository persists Estimates. estimates owns the estimates
// collection exclusively; no other module may query it directly.
type EstimateRepository interface {
	Create(ctx context.Context, e Estimate) (Estimate, error)
	FindByID(ctx context.Context, companyID, id string) (Estimate, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error)
	FindLatestByProject(ctx context.Context, companyID, projectID string) (Estimate, error)
	FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error)

	// ReplaceSnapshot atomically replaces Lines/CostSubtotal/
	// ExcludedCostItemCount and recomputed pricing outputs on a draft,
	// conditioned on {_id, companyId, status: draft, revision: expectedRevision}
	// matching. Returns ErrRevisionMismatch if the condition does not match
	// an existing draft document (design spec §16.2).
	ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error)

	// UpdatePricing atomically replaces PricingMode/PricingRate and
	// recomputed pricing outputs on a draft, same conditional-match
	// semantics as ReplaceSnapshot (design spec §16.1).
	UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error)

	// Finalize atomically transitions a draft to finalized, conditioned on
	// {_id, companyId, status: draft, revision: expectedRevision} matching.
	// Returns ErrRevisionMismatch on a stale expectedRevision against a
	// still-draft document (design spec §16.3).
	Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Estimate, error)
}

// ErrRevisionMismatch is returned by ReplaceSnapshot/UpdatePricing/Finalize
// when the supplied expectedRevision does not match the document's current
// Revision (or the document is no longer a draft) — the caller must re-GET
// and retry with the current Revision (design spec §16.1-16.3).
var ErrRevisionMismatch = errors.New("estimates: revision mismatch or estimate is no longer a draft")
```

- [ ] **Step 2: Fix the missing import**

The `time.Time` reference in `Finalize`'s signature needs `"time"` imported. Update the top of `backend/internal/estimates/repository.go`:

```go
package estimates

import (
	"context"
	"errors"
	"time"
)
```

- [ ] **Step 3: Confirm the package builds**

Run: `cd backend && go build ./internal/estimates/...`
Expected: clean

- [ ] **Step 4: Write the failing repository tests**

Create `backend/internal/estimates/repository_mongo_test.go`:

```go
package estimates_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
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

func sampleEstimate(companyID, projectID string, version int, status estimates.EstimateStatus) estimates.Estimate {
	return estimates.Estimate{
		CompanyID: companyID, ProjectID: projectID, Version: version, Status: status, Revision: 0,
		Currency: "MYR",
		Lines: []estimates.EstimateCostLine{
			{SourceCostItemID: "cost_item_1", Category: "material", Description: "Tiles", SnapshottedAmount: money.New(500000, "MYR")},
		},
		CostSubtotal: money.New(500000, "MYR"),
		PricingMode:  estimates.PricingModeMarkup, PricingRate: 2000,
		ProposedSellingPrice: money.New(600000, "MYR"), ProjectedGrossProfit: money.New(100000, "MYR"),
		ProjectedGrossMarginBPS: 1667,
		CreatedAt:               time.Now(), SchemaVersion: 1,
	}
}

func TestEstimateRepositoryCreateAndFindByID(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_create_find")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
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
	if found.Version != 1 || found.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected Version=1 Status=draft, got %+v", found)
	}
	if len(found.Lines) != 1 || found.Lines[0].Description != "Tiles" {
		t.Fatalf("expected 1 line 'Tiles', got %+v", found.Lines)
	}
	if found.CostSubtotal.Amount != 500000 {
		t.Fatalf("expected CostSubtotal 500000, got %+v", found.CostSubtotal)
	}
}

func TestEstimateRepositoryFindByIDCrossTenant(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_cross_tenant")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != estimates.ErrEstimateNotFound {
		t.Fatalf("expected ErrEstimateNotFound for cross-tenant lookup, got %v", err)
	}
}

func TestEstimateRepositoryListByProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_list_by_project")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_2", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating other project's estimate: %v", err)
	}

	list, err := repo.ListByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error listing: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 estimates for project_1, got %d", len(list))
	}
}

func TestEstimateRepositoryFindMaxVersion(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_max_version")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	maxV, err := repo.FindMaxVersion(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error on empty project: %v", err)
	}
	if maxV != 0 {
		t.Fatalf("expected 0 when no estimates exist, got %d", maxV)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	// Insert v3 before v2 to prove MAX(version) is used, not insertion order.
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 3, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v3: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}

	maxV, err = repo.FindMaxVersion(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if maxV != 3 {
		t.Fatalf("expected max version 3, got %d", maxV)
	}
}

func TestEstimateRepositoryFindLatestByProject(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_find_latest")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	_, err := repo.FindLatestByProject(ctx, "company_a", "project_1")
	if err != estimates.ErrNoEstimatesForProject {
		t.Fatalf("expected ErrNoEstimatesForProject on empty project, got %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}

	latest, err := repo.FindLatestByProject(ctx, "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected latest version 2, got %d", latest.Version)
	}
}

func TestEstimateRepositoryVersionUniqueIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_version_unique")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("unexpected error creating first v1: %v", err)
	}
	_, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusFinalized))
	if err == nil {
		t.Fatal("expected duplicate-key error creating a second version=1 for the same company+project")
	}
	// BLOCKING fix (second plan review, issue #3): Create must translate
	// the raw MongoDB duplicate-key error into the DISTINCT
	// ErrVersionConflict sentinel for a version-number collision — not
	// merely satisfy mongo.IsDuplicateKeyError(err), and not be confused
	// with ErrDraftAlreadyExists (the OTHER unique index's collision,
	// tested separately below). This is what lets the service layer
	// (Task 5/7) and HTTP layer (Task 8) distinguish "retry with a fresh
	// version number" from "a draft already exists, do not retry."
	if err != estimates.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict, got: %v", err)
	}
}

func TestEstimateRepositoryOneDraftPerProjectPartialIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_one_draft")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft)); err != nil {
		t.Fatalf("unexpected error creating first draft: %v", err)
	}
	_, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 2, estimates.EstimateStatusDraft))
	if err == nil {
		t.Fatal("expected duplicate-key error creating a second draft for the same company+project")
	}
	// BLOCKING fix (second plan review, issue #3): this must classify as
	// the DISTINCT ErrDraftAlreadyExists sentinel, never confused with
	// ErrVersionConflict above — a draft collision is NOT safe to
	// blindly retry (design spec §21 step 4), so the two must be
	// distinguishable in production code, not just provable in a
	// standalone test helper.
	if err != estimates.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists, got: %v", err)
	}

	// A second FINALIZED document for the same project must succeed — the
	// partial index only constrains status=draft documents.
	if _, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 3, estimates.EstimateStatusFinalized)); err != nil {
		t.Fatalf("expected a second finalized document to succeed (partial index scoped to draft only), got: %v", err)
	}
}

func TestEstimateRepositoryUpdatePricing(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_update_pricing")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	updated := created
	updated.PricingMode = estimates.PricingModeMargin
	updated.PricingRate = 2500
	updated.ProposedSellingPrice = money.New(666667, "MYR")
	result, err := repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error updating pricing: %v", err)
	}
	if result.Revision != created.Revision+1 {
		t.Fatalf("expected Revision incremented by 1, got %d (was %d)", result.Revision, created.Revision)
	}
	if result.PricingMode != estimates.PricingModeMargin || result.PricingRate != 2500 {
		t.Fatalf("expected updated pricing fields to persist, got %+v", result)
	}

	// Stale expectedRevision must fail.
	_, err = repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch on stale revision, got %v", err)
	}
}

func TestEstimateRepositoryFinalize(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_finalize")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	now := time.Now()
	revisionBeforeFinalize := created.Revision
	result, err := repo.Finalize(ctx, "company_a", created.ID, created.Revision, now)
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}
	if result.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", result.Status)
	}
	if result.FinalizedAt == nil {
		t.Fatal("expected FinalizedAt to be set")
	}
	// BLOCKING invariant from the approved design spec §16.3: finalize
	// FREEZES Revision at its current value — it must NOT increment it.
	// Draft Revision 5 -> Finalize(expectedRevision=5) -> Finalized
	// Revision 5, never Revision 6.
	if result.Revision != revisionBeforeFinalize {
		t.Fatalf("expected Revision to remain %d after finalize (frozen, not incremented), got %d", revisionBeforeFinalize, result.Revision)
	}

	// Stale expectedRevision against a document that's still a draft would
	// fail; here the document is ALREADY finalized, so a second call with
	// the ORIGINAL (correct pre-finalize) revision must also fail per the
	// repository's strict {status: draft, revision: expected} match — the
	// idempotency carve-out (return unchanged if already finalized) is a
	// SERVICE-layer responsibility (Task 7), not the repository's. The
	// repository itself is a strict conditional-match primitive.
	_, err = repo.Finalize(ctx, "company_a", created.ID, created.Revision, now)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch when re-finalizing at the repository layer (status no longer draft), got %v", err)
	}
}

func TestEstimateRepositoryReplaceSnapshotPersistsCurrencyChange(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_replace_snapshot_currency")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}
	if created.Currency != "MYR" {
		t.Fatalf("expected initial Currency=MYR, got %s", created.Currency)
	}

	// Simulate a refresh where the Project's eligible CostItems have all
	// become SGD since the draft was created — a valid sequence per the
	// approved design spec §8 ("on refresh, currency is re-inferred from
	// the current eligible CostItems, the same way").
	updated := created
	updated.Currency = "SGD"
	updated.Lines = []estimates.EstimateCostLine{
		{SourceCostItemID: "cost_item_1", Category: "material", Description: "Tiles", SnapshottedAmount: money.New(700000, "SGD")},
	}
	updated.CostSubtotal = money.New(700000, "SGD")
	updated.ProposedSellingPrice = money.New(840000, "SGD")

	result, err := repo.ReplaceSnapshot(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error replacing snapshot: %v", err)
	}
	// BLOCKING bug this test catches: ReplaceSnapshot must persist the
	// Currency change, not just Lines/CostSubtotal/pricing fields.
	if result.Currency != "SGD" {
		t.Fatalf("expected Currency updated to SGD after refresh, got %s (Currency was NOT included in the $set)", result.Currency)
	}
	if result.CostSubtotal.Currency != "SGD" || result.ProposedSellingPrice.Currency != "SGD" {
		t.Fatalf("expected CostSubtotal/ProposedSellingPrice to be SGD, got %+v / %+v", result.CostSubtotal, result.ProposedSellingPrice)
	}

	// Re-read from a fresh FindByID to prove this was actually persisted,
	// not merely reflected in the in-memory return value.
	refetched, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error re-fetching: %v", err)
	}
	if refetched.Currency != "SGD" {
		t.Fatalf("expected persisted Currency=SGD on re-fetch, got %s", refetched.Currency)
	}
}
```

- [ ] **Step 5: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: FAIL with `undefined: estimates.NewMongoEstimateRepository` (and related undefined symbols)

- [ ] **Step 6: Implement the Mongo repository**

Create `backend/internal/estimates/repository_mongo.go`:

```go
package estimates

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoEstimateRepository is the MongoDB-backed EstimateRepository
// implementation. It owns the "estimates" collection exclusively.
type MongoEstimateRepository struct {
	collection *mongo.Collection
}

// NewMongoEstimateRepository constructs a MongoEstimateRepository against
// db's "estimates" collection.
func NewMongoEstimateRepository(db *mongo.Database) *MongoEstimateRepository {
	return &MongoEstimateRepository{collection: db.Collection("estimates")}
}

// Explicit index names (BLOCKING fix — third plan review round, issue #1):
// EnsureIndexes previously let MongoDB assign default names to all four
// indexes. Mongo's default-naming convention concatenates each key and its
// sort direction, so BOTH the plain companyId+projectId index and the
// partial unique companyId+projectId (draft-only) index would derive the
// SAME default name — companyId_1_projectId_1 — since default naming does
// not account for a partialFilterExpression. Two indexes cannot share one
// name in the same collection, so CreateMany would fail outright at
// EnsureIndexes time, never reaching a point where the ambiguity could
// even surface as a runtime classification bug. Every index now gets an
// explicit, distinct name via SetName, removing the ambiguity at its root
// rather than working around it in classifyCreateError.
const (
	indexNameCompanyProject             = "idx_estimates_company_project"
	indexNameUniqueCompanyProjectVersion = "uq_estimates_company_project_version"
	indexNameUniqueOneDraftPerProject    = "uq_estimates_one_draft_per_project"
)

// EnsureIndexes creates the companyId index, the companyId+projectId
// compound index, the UNIQUE companyId+projectId+version index, and the
// UNIQUE PARTIAL companyId+projectId index (status=draft only) enforcing
// "at most one draft per Project" (design spec §20). All four carry
// explicit, distinct names (see the constants above) — required because
// two of these indexes share the same key pattern and would otherwise
// collide on MongoDB's default auto-generated name.
func (r *MongoEstimateRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueCompanyProjectVersion)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
				SetName(indexNameUniqueOneDraftPerProject)},
	})
	return err
}

type estimateLineDoc struct {
	SourceCostItemID  string      `bson:"sourceCostItemId"`
	WorkItemID        *string     `bson:"workItemId,omitempty"`
	Category          string      `bson:"category"`
	Description       string      `bson:"description"`
	SnapshottedAmount money.Money `bson:"snapshottedAmount"`
}

type estimateDoc struct {
	ID                      bson.ObjectID     `bson:"_id,omitempty"`
	CompanyID               string            `bson:"companyId"`
	ProjectID               string            `bson:"projectId"`
	Version                 int               `bson:"version"`
	Status                  string            `bson:"status"`
	Revision                int64             `bson:"revision"`
	Currency                string            `bson:"currency"`
	Lines                   []estimateLineDoc `bson:"lines"`
	CostSubtotal            money.Money       `bson:"costSubtotal"`
	ExcludedCostItemCount   int               `bson:"excludedCostItemCount"`
	PricingMode             string            `bson:"pricingMode"`
	PricingRate             int64             `bson:"pricingRate"`
	ProposedSellingPrice    money.Money       `bson:"proposedSellingPrice"`
	ProjectedGrossProfit    money.Money       `bson:"projectedGrossProfit"`
	ProjectedGrossMarginBPS int64             `bson:"projectedGrossMarginBps"`
	CreatedAt               time.Time         `bson:"createdAt"`
	RefreshedAt             *time.Time        `bson:"refreshedAt,omitempty"`
	FinalizedAt             *time.Time        `bson:"finalizedAt,omitempty"`
	SchemaVersion           int               `bson:"schemaVersion"`
}

func toEstimateDoc(e Estimate) (estimateDoc, error) {
	lines := make([]estimateLineDoc, len(e.Lines))
	for i, l := range e.Lines {
		lines[i] = estimateLineDoc{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	doc := estimateDoc{
		CompanyID: e.CompanyID, ProjectID: e.ProjectID, Version: e.Version, Status: string(e.Status),
		Revision: e.Revision, Currency: e.Currency, Lines: lines, CostSubtotal: e.CostSubtotal,
		ExcludedCostItemCount: e.ExcludedCostItemCount, PricingMode: string(e.PricingMode),
		PricingRate: int64(e.PricingRate), ProposedSellingPrice: e.ProposedSellingPrice,
		ProjectedGrossProfit: e.ProjectedGrossProfit, ProjectedGrossMarginBPS: int64(e.ProjectedGrossMarginBPS),
		CreatedAt: e.CreatedAt, RefreshedAt: e.RefreshedAt, FinalizedAt: e.FinalizedAt, SchemaVersion: e.SchemaVersion,
	}
	if e.ID != "" {
		objID, err := bson.ObjectIDFromHex(e.ID)
		if err != nil {
			return estimateDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromEstimateDoc(doc estimateDoc) Estimate {
	lines := make([]EstimateCostLine, len(doc.Lines))
	for i, l := range doc.Lines {
		lines[i] = EstimateCostLine{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	return Estimate{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, Version: doc.Version,
		Status: EstimateStatus(doc.Status), Revision: doc.Revision, Currency: doc.Currency, Lines: lines,
		CostSubtotal: doc.CostSubtotal, ExcludedCostItemCount: doc.ExcludedCostItemCount,
		PricingMode: PricingMode(doc.PricingMode), PricingRate: money.RateBPS(doc.PricingRate),
		ProposedSellingPrice: doc.ProposedSellingPrice, ProjectedGrossProfit: doc.ProjectedGrossProfit,
		ProjectedGrossMarginBPS: money.RateBPS(doc.ProjectedGrossMarginBPS),
		CreatedAt:               doc.CreatedAt, RefreshedAt: doc.RefreshedAt, FinalizedAt: doc.FinalizedAt,
		SchemaVersion: doc.SchemaVersion,
	}
}

// classifyCreateError translates a raw MongoDB duplicate-key error from
// InsertOne into one of the two named domain sentinels
// (ErrVersionConflict, ErrDraftAlreadyExists) by checking which EXPLICIT
// index name (set via SetName in EnsureIndexes above) appears in the write
// error's message — MongoDB's E11000 duplicate-key error message always
// names the offending index
// (e.g. "E11000 duplicate key error collection: ... index: uq_estimates_company_project_version dup key: ...").
//
// BLOCKING FIX (third plan review round, issue #1): the previous version
// of this function compared against MongoDB's *default* auto-generated
// index names (companyId_1_projectId_1 and
// companyId_1_projectId_1_version_1). Those default names have two fatal
// problems: (a) the plain (non-unique) companyId+projectId index and the
// partial-unique companyId+projectId (draft-only) index derive the exact
// SAME default name — Mongo's default naming does not factor in a
// partialFilterExpression — so EnsureIndexes would have failed outright
// with a name collision at index-creation time, before this classifier
// ever ran; and (b) even setting that aside, "companyId_1_projectId_1" is
// a literal substring of "companyId_1_projectId_1_version_1", so a
// substring check for the draft-index name would ALSO match every
// version-conflict error message, misclassifying every version race as a
// draft collision. Both problems are eliminated at the root by giving all
// four indexes explicit, mutually non-overlapping names in EnsureIndexes
// (indexNameCompanyProject, indexNameUniqueCompanyProjectVersion,
// indexNameUniqueOneDraftPerProject) and comparing against those exact
// constants here — no substring ambiguity is possible between them.
//
// Returns ErrUnclassifiedDuplicateKey (NOT ErrVersionConflict) if a
// duplicate-key error occurs that doesn't match either known index name —
// also a fix from the third review round: an error this function cannot
// positively identify must not be silently assumed safe to retry.
// ErrUnclassifiedDuplicateKey is deliberately NOT retried by
// allocateAndCreate (see below) and surfaces as a 500, the same as any
// other unrecognized internal error — retrying an operation whose failure
// mode is not understood risks looping on a real, non-transient problem
// (e.g. a schema/index misconfiguration) as if it were an ordinary,
// expected race.
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			// strings.Contains (not exact equality) is required here — the
			// index name is embedded inside a longer MongoDB-generated
			// sentence (e.g. "E11000 duplicate key error collection: ...
			// index: uq_estimates_company_project_version dup key: ..."),
			// never the entire we.Message on its own. This is safe against
			// misclassification specifically BECAUSE all three explicit
			// index names (idx_estimates_company_project,
			// uq_estimates_company_project_version,
			// uq_estimates_one_draft_per_project) are mutually
			// non-overlapping strings — none is a substring of another,
			// unlike the previous default-name pair this round fixed.
			switch {
			case strings.Contains(we.Message, indexNameUniqueOneDraftPerProject):
				return ErrDraftAlreadyExists
			case strings.Contains(we.Message, indexNameUniqueCompanyProjectVersion):
				return ErrVersionConflict
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoEstimateRepository) Create(ctx context.Context, e Estimate) (Estimate, error) {
	doc, err := toEstimateDoc(e)
	if err != nil {
		return Estimate{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Estimate{}, classifyCreateError(err)
	}
	e.ID = res.InsertedID.(bson.ObjectID).Hex()
	return e, nil
}

func (r *MongoEstimateRepository) FindByID(ctx context.Context, companyID, id string) (Estimate, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Estimate{}, ErrEstimateNotFound
	}
	var doc estimateDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Estimate{}, ErrEstimateNotFound
	}
	if err != nil {
		return Estimate{}, err
	}
	return fromEstimateDoc(doc), nil
}

func (r *MongoEstimateRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []estimateDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]Estimate, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromEstimateDoc(doc))
	}
	return result, nil
}

func (r *MongoEstimateRepository) FindLatestByProject(ctx context.Context, companyID, projectID string) (Estimate, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}})
	var doc estimateDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "projectId": projectID}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Estimate{}, ErrNoEstimatesForProject
	}
	if err != nil {
		return Estimate{}, err
	}
	return fromEstimateDoc(doc), nil
}

func (r *MongoEstimateRepository) FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}}).SetProjection(bson.M{"version": 1})
	var doc struct {
		Version int `bson:"version"`
	}
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "projectId": projectID}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return doc.Version, nil
}

// conditionalUpdate applies set against a document matching
// {_id, companyId, status: draft, revision: expectedRevision}. If
// incrementRevision is true, revision is bumped to expectedRevision+1 as
// part of the same $set (used by ReplaceSnapshot/UpdatePricing, which
// produce a new mutation state that a future writer must be guarded
// against). If incrementRevision is false, revision is left untouched
// (used by Finalize — the approved design spec §16.3 explicitly freezes
// Revision at whatever value it held at the moment finalize succeeds;
// there is no subsequent draft mutation on this now-immutable document for
// a fresh Revision to protect against, so bumping it would be both
// pointless and would contradict the spec's stated invariant that
// Draft Revision N -> Finalize -> Finalized Revision N, unchanged).
func (r *MongoEstimateRepository) conditionalUpdate(ctx context.Context, companyID, id string, expectedRevision int64, set bson.M, incrementRevision bool) (Estimate, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Estimate{}, ErrEstimateNotFound
	}
	if incrementRevision {
		set["revision"] = expectedRevision + 1
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(EstimateStatusDraft), "revision": expectedRevision},
		bson.M{"$set": set},
	)
	if err != nil {
		return Estimate{}, err
	}
	if res.MatchedCount == 0 {
		// Distinguish "doesn't exist / wrong tenant" from "exists but
		// revision/status didn't match" so the service layer can map each
		// to the correct caller-facing error.
		_, findErr := r.FindByID(ctx, companyID, id)
		if findErr == ErrEstimateNotFound {
			return Estimate{}, ErrEstimateNotFound
		}
		return Estimate{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoEstimateRepository) ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error) {
	lines := make([]estimateLineDoc, len(updated.Lines))
	for i, l := range updated.Lines {
		lines[i] = estimateLineDoc{
			SourceCostItemID: l.SourceCostItemID, WorkItemID: l.WorkItemID,
			Category: l.Category, Description: l.Description, SnapshottedAmount: l.SnapshottedAmount,
		}
	}
	set := bson.M{
		"currency": updated.Currency, "lines": lines, "costSubtotal": updated.CostSubtotal,
		"excludedCostItemCount": updated.ExcludedCostItemCount,
		"pricingMode": string(updated.PricingMode), "pricingRate": int64(updated.PricingRate),
		"proposedSellingPrice": updated.ProposedSellingPrice, "projectedGrossProfit": updated.ProjectedGrossProfit,
		"projectedGrossMarginBps": int64(updated.ProjectedGrossMarginBPS), "refreshedAt": updated.RefreshedAt,
	}
	// currency IS included above (bug fixed): RefreshEstimate can re-infer
	// a different Currency than the draft previously held (design spec §8,
	// "on refresh, currency is re-inferred from the current eligible
	// CostItems, the same way") — omitting it here would leave
	// Estimate.Currency permanently stale after a currency-changing refresh
	// even though Lines/CostSubtotal/ProposedSellingPrice all correctly
	// moved to the new currency.
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoEstimateRepository) UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated Estimate) (Estimate, error) {
	set := bson.M{
		"pricingMode": string(updated.PricingMode), "pricingRate": int64(updated.PricingRate),
		"proposedSellingPrice": updated.ProposedSellingPrice, "projectedGrossProfit": updated.ProjectedGrossProfit,
		"projectedGrossMarginBps": int64(updated.ProjectedGrossMarginBPS),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoEstimateRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Estimate, error) {
	set := bson.M{"status": string(EstimateStatusFinalized), "finalizedAt": finalizedAt}
	// incrementRevision=false: finalize freezes Revision at its current
	// value (design spec §16.3) — see conditionalUpdate's doc comment.
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, false)
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 8: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/repository.go`/`repository_mongo.go`/`repository_mongo_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 4: Pricing/snapshot calculation core (pure functions)

**Spec:** §5-9, §13-15. No I/O — these are the formulas the service layer (Tasks 5-7) will call. Isolating them lets the markup/margin/rounding-order tests run without a fake repository or cost source at all.

**Files:**
- Create: `backend/internal/estimates/calculation.go`
- Test: `backend/internal/estimates/calculation_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/estimates/calculation_test.go`:

```go
package estimates_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestCalculatePricingMarkup(t *testing.T) {
	subtotal := money.New(10000, "MYR") // RM100
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 12000 {
		t.Fatalf("expected selling price 12000 (RM100 + 20%% markup), got %+v", result.ProposedSellingPrice)
	}
	if result.ProjectedGrossProfit.Amount != 2000 {
		t.Fatalf("expected profit 2000, got %+v", result.ProjectedGrossProfit)
	}
}

func TestCalculatePricingMargin(t *testing.T) {
	subtotal := money.New(10000, "MYR") // RM100
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 12500 {
		t.Fatalf("expected selling price 12500 (RM100 / 0.8 = RM125, the brief's own worked example), got %+v", result.ProposedSellingPrice)
	}
	if result.ProjectedGrossMarginBPS != 2000 {
		t.Fatalf("expected resulting margin to equal the requested 2000 bps, got %d", result.ProjectedGrossMarginBPS)
	}
}

func TestCalculatePricingMarginRejects100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 10000)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for 100%% margin, got %v", err)
	}
}

func TestCalculatePricingMarginRejectsAbove100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, 10001)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for >100%% margin, got %v", err)
	}
}

func TestCalculatePricingRejectsNegativeRate(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, -1)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for negative markup rate, got %v", err)
	}
	_, err = estimates.CalculatePricing(subtotal, estimates.PricingModeMargin, -1)
	if err != estimates.ErrInvalidPricingRate {
		t.Fatalf("expected ErrInvalidPricingRate for negative margin rate, got %v", err)
	}
}

func TestCalculatePricingRejectsInvalidMode(t *testing.T) {
	// BLOCKING fix (second plan review, issue #5): an invalid/unrecognized
	// PricingMode must be rejected explicitly, not silently fall through
	// to a zero-value selling price that then feeds bogus profit/margin
	// arithmetic.
	subtotal := money.New(10000, "MYR")
	_, err := estimates.CalculatePricing(subtotal, estimates.PricingMode("banana"), 2000)
	if err != estimates.ErrInvalidPricingMode {
		t.Fatalf("expected ErrInvalidPricingMode for an unrecognized mode, got %v", err)
	}
}

func TestCalculatePricingMarkupAllowsAbove100Percent(t *testing.T) {
	subtotal := money.New(10000, "MYR")
	result, err := estimates.CalculatePricing(subtotal, estimates.PricingModeMarkup, 15000) // 150% markup
	if err != nil {
		t.Fatalf("expected 150%% markup to be allowed (no upper bound on markup mode), got error: %v", err)
	}
	if result.ProposedSellingPrice.Amount != 25000 {
		t.Fatalf("expected selling price 25000 (RM100 * 2.5), got %+v", result.ProposedSellingPrice)
	}
}

func TestCalculatePricingRejectsZeroOrNegativeSubtotal(t *testing.T) {
	_, err := estimates.CalculatePricing(money.New(0, "MYR"), estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrCostSubtotalNotPositive {
		t.Fatalf("expected ErrCostSubtotalNotPositive for zero subtotal, got %v", err)
	}
	_, err = estimates.CalculatePricing(money.New(-100, "MYR"), estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrCostSubtotalNotPositive {
		t.Fatalf("expected ErrCostSubtotalNotPositive for negative subtotal, got %v", err)
	}
}

func TestSumLinesNoDoubleCounting(t *testing.T) {
	lines := []estimates.EstimateCostLine{
		{SourceCostItemID: "c1", Category: "material", SnapshottedAmount: money.New(50000, "MYR")},
		{SourceCostItemID: "c2", Category: "labour", SnapshottedAmount: money.New(30000, "MYR")},
	}
	subtotal, err := estimates.SumLines(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if subtotal.Amount != 80000 {
		t.Fatalf("expected 80000 (50000+30000, no double counting), got %+v", subtotal)
	}
}

func TestSumLinesEmptyRejected(t *testing.T) {
	_, err := estimates.SumLines(nil)
	if err != estimates.ErrNoEstimatedCosts {
		t.Fatalf("expected ErrNoEstimatedCosts for zero lines, got %v", err)
	}
}

func TestSumLinesMixedCurrencyRejected(t *testing.T) {
	lines := []estimates.EstimateCostLine{
		{SourceCostItemID: "c1", SnapshottedAmount: money.New(50000, "MYR")},
		{SourceCostItemID: "c2", SnapshottedAmount: money.New(30000, "SGD")},
	}
	_, err := estimates.SumLines(lines)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -run "TestCalculatePricing|TestSumLines" -v`
Expected: FAIL with `undefined: estimates.CalculatePricing` (and related undefined symbols)

- [ ] **Step 3: Implement the calculation core**

Create `backend/internal/estimates/calculation.go`:

```go
package estimates

import (
	"errors"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrNoEstimatedCosts is returned when a Project has zero CostItems with a
// non-nil Estimated value at snapshot-creation or refresh time (design spec
// §7.1) — there is nothing to estimate.
var ErrNoEstimatedCosts = errors.New("estimates: project has no cost items with an estimated amount")

// ErrMixedCurrencyCostItems is returned when the eligible CostItems for a
// Project snapshot span more than one currency (design spec §8).
var ErrMixedCurrencyCostItems = errors.New("estimates: cost items span more than one currency")

// ErrCostSubtotalNotPositive is returned when the summed cost subtotal is
// zero or negative even though at least one eligible line exists (design
// spec §7.2) — prevents undefined margin-mode pricing (division producing
// a zero or negative ProposedSellingPrice).
var ErrCostSubtotalNotPositive = errors.New("estimates: cost subtotal must be positive")

// ErrInvalidPricingRate is returned when PricingRate is negative in either
// mode, or >= 100% (10000 bps) in margin mode (design spec §13).
var ErrInvalidPricingRate = errors.New("estimates: invalid pricing rate for the given pricing mode")

// ErrInvalidPricingMode is returned when mode is not one of the 2 defined
// PricingMode values (design spec §13). BLOCKING fix, second plan review
// issue #5.
var ErrInvalidPricingMode = errors.New("estimates: invalid pricing mode")

// SumLines computes the total CostSubtotal across lines, validating all
// lines share one currency and the result is strictly positive. Returns
// ErrNoEstimatedCosts if lines is empty, ErrMixedCurrencyCostItems if more
// than one currency is present, ErrCostSubtotalNotPositive if the sum is
// <= 0 (design spec §7.1, §7.2, §8).
func SumLines(lines []EstimateCostLine) (money.Money, error) {
	if len(lines) == 0 {
		return money.Money{}, ErrNoEstimatedCosts
	}
	currency := lines[0].SnapshottedAmount.Currency
	total := int64(0)
	for _, l := range lines {
		if l.SnapshottedAmount.Currency != currency {
			return money.Money{}, ErrMixedCurrencyCostItems
		}
		total += l.SnapshottedAmount.Amount
	}
	if total <= 0 {
		return money.Money{}, ErrCostSubtotalNotPositive
	}
	return money.New(total, currency), nil
}

// PricingResult holds the calculated outputs of applying a pricing mode/rate
// to a cost subtotal (design spec §13, §15).
type PricingResult struct {
	ProposedSellingPrice    money.Money
	ProjectedGrossProfit    money.Money
	ProjectedGrossMarginBPS money.RateBPS
}

// CalculatePricing applies markup or margin pricing to costSubtotal,
// producing ProposedSellingPrice, ProjectedGrossProfit, and
// ProjectedGrossMarginBPS in one deterministic rounding order (design spec
// §15). Validates: mode must be one of the 2 defined PricingMode values
// (ErrInvalidPricingMode otherwise — BLOCKING fix, second plan review
// issue #5: without this check, an invalid/typo'd mode such as "banana"
// previously fell through the switch with sellingPrice left at its
// zero-value and silently continued into profit/margin arithmetic against
// a bogus zero selling price); rate must be non-negative in both modes; in
// margin mode, rate must additionally be < 10000 bps (100%). Assumes
// costSubtotal is already validated positive by the caller (SumLines
// enforces this before CalculatePricing is ever invoked in the service
// layer).
func CalculatePricing(costSubtotal money.Money, mode PricingMode, rate money.RateBPS) (PricingResult, error) {
	if !mode.IsValid() {
		return PricingResult{}, ErrInvalidPricingMode
	}
	if rate < 0 {
		return PricingResult{}, ErrInvalidPricingRate
	}
	if mode == PricingModeMargin && rate >= money.BasisPointsDenominator {
		return PricingResult{}, ErrInvalidPricingRate
	}

	var sellingPrice money.Money
	switch mode {
	case PricingModeMarkup:
		sellingPrice = money.AddRateBPS(costSubtotal, rate)
	case PricingModeMargin:
		sellingPrice = money.DivideByComplementRateBPS(costSubtotal, rate)
	}

	profit, err := sellingPrice.Subtract(costSubtotal)
	if err != nil {
		return PricingResult{}, err
	}
	marginBPS := money.RateBPSFromRatio(profit, sellingPrice)

	return PricingResult{
		ProposedSellingPrice:    sellingPrice,
		ProjectedGrossProfit:    profit,
		ProjectedGrossMarginBPS: marginBPS,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/calculation.go`/`calculation_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 5: `Service` — creation and read operations

**Spec:** §4, §7, §8, §17 (steps 3-8, minus the finalized-source precondition which belongs to Task 7's `CreateNewVersion`), §23-25. This task covers `CreateEstimate`, `GetEstimate`, `GetLatestEstimate`, `ListEstimatesByProject`, and the shared `buildSnapshot` helper both `CreateEstimate` and `CreateNewVersion` (Task 7) will use.

**Files:**
- Create: `backend/internal/estimates/service.go`
- Test: `backend/internal/estimates/service_test.go`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/estimates/service_test.go`:

```go
package estimates_test

import (
	"context"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

type fakeEstimateRepository struct {
	byID map[string]estimates.Estimate
	next int
}

func newFakeEstimateRepository() *fakeEstimateRepository {
	return &fakeEstimateRepository{byID: make(map[string]estimates.Estimate)}
}

func (f *fakeEstimateRepository) Create(ctx context.Context, e estimates.Estimate) (estimates.Estimate, error) {
	// Simulate the two real unique indexes (design spec §20) so unit tests
	// can exercise allocateAndCreate's retry behavior and
	// ErrDraftAlreadyExists propagation without a real MongoDB instance —
	// this is what lets TestCreateEstimateVersionConflictRetries and
	// TestCreateNewVersionDraftAlreadyExists (added below) actually prove
	// the PRODUCTION allocateAndCreate helper's behavior, not just a
	// standalone test-only reimplementation (second plan review, issue #3).
	for _, existing := range f.byID {
		if existing.CompanyID != e.CompanyID || existing.ProjectID != e.ProjectID {
			continue
		}
		if existing.Version == e.Version {
			return estimates.Estimate{}, estimates.ErrVersionConflict
		}
		if existing.Status == estimates.EstimateStatusDraft && e.Status == estimates.EstimateStatusDraft {
			return estimates.Estimate{}, estimates.ErrDraftAlreadyExists
		}
	}
	f.next++
	e.ID = "estimate_" + string(rune('0'+f.next))
	f.byID[e.ID] = e
	return e, nil
}

func (f *fakeEstimateRepository) FindByID(ctx context.Context, companyID, id string) (estimates.Estimate, error) {
	e, ok := f.byID[id]
	if !ok || e.CompanyID != companyID {
		return estimates.Estimate{}, estimates.ErrEstimateNotFound
	}
	return e, nil
}

func (f *fakeEstimateRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]estimates.Estimate, error) {
	var result []estimates.Estimate
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeEstimateRepository) FindLatestByProject(ctx context.Context, companyID, projectID string) (estimates.Estimate, error) {
	var latest estimates.Estimate
	found := false
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID && (!found || e.Version > latest.Version) {
			latest = e
			found = true
		}
	}
	if !found {
		return estimates.Estimate{}, estimates.ErrNoEstimatesForProject
	}
	return latest, nil
}

func (f *fakeEstimateRepository) FindMaxVersion(ctx context.Context, companyID, projectID string) (int, error) {
	max := 0
	for _, e := range f.byID {
		if e.CompanyID == companyID && e.ProjectID == projectID && e.Version > max {
			max = e.Version
		}
	}
	return max, nil
}

func (f *fakeEstimateRepository) ReplaceSnapshot(ctx context.Context, companyID, id string, expectedRevision int64, updated estimates.Estimate) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	updated.Revision = expectedRevision + 1
	f.byID[id] = updated
	return updated, nil
}

func (f *fakeEstimateRepository) UpdatePricing(ctx context.Context, companyID, id string, expectedRevision int64, updated estimates.Estimate) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	updated.Revision = expectedRevision + 1
	f.byID[id] = updated
	return updated, nil
}

func (f *fakeEstimateRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (estimates.Estimate, error) {
	existing, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return estimates.Estimate{}, err
	}
	if existing.Status != estimates.EstimateStatusDraft || existing.Revision != expectedRevision {
		return estimates.Estimate{}, estimates.ErrRevisionMismatch
	}
	existing.Status = estimates.EstimateStatusFinalized
	existing.FinalizedAt = &finalizedAt
	f.byID[id] = existing
	return existing, nil
}

type fakeProjectLookup struct{ belongs bool }

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}

type fakeCostSource struct {
	items []fakeCostSourceItem
}

type fakeCostSourceItem struct {
	costItemID  string
	workItemID  *string
	category    string
	description string
	estimated   money.Money
}

func (f fakeCostSource) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	for _, item := range f.items {
		if err := visit(item.costItemID, item.workItemID, item.category, item.description, item.estimated); err != nil {
			return 0, err
		}
	}
	return 0, nil
}

func TestCreateEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", category: "labour", description: "Tiling labour", estimated: money.New(30000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.Version != 1 || e.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected Version=1 Status=draft, got %+v", e)
	}
	if e.CostSubtotal.Amount != 80000 {
		t.Fatalf("expected CostSubtotal 80000 (50000+30000), got %+v", e.CostSubtotal)
	}
	if e.ProposedSellingPrice.Amount != 96000 {
		t.Fatalf("expected selling price 96000 (RM800 + 20%% markup), got %+v", e.ProposedSellingPrice)
	}
	if len(e.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(e.Lines))
	}
}

func TestCreateEstimateProjectNotFound(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: false}, fakeCostSource{})

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateEstimateNoEligibleCostItems(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{items: nil})

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrNoEstimatedCosts {
		t.Fatalf("expected ErrNoEstimatedCosts, got %v", err)
	}
}

func TestCreateEstimateMixedCurrency(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", estimated: money.New(30000, "SGD")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}
}

func TestCreateEstimateDoesNotDoubleCountLabour(t *testing.T) {
	// The universal-ledger invariant from M3: a labour cost enters
	// cost_items exactly once (Category=labour), never separately summed
	// from labour_entries. estimates never touches labour_entries at all —
	// this test proves the Estimate's CostSubtotal exactly equals the sum
	// of what the cost source visits, once each.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "labour_cost_item", category: "labour", description: "Tiler", estimated: money.New(30000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.CostSubtotal.Amount != 30000 {
		t.Fatalf("expected CostSubtotal to equal the single labour CostItem's amount exactly (30000), got %+v", e.CostSubtotal)
	}
}

func TestCreateEstimateExcludedCostItemCount(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := &fakeCostSourceWithMissingCount{
		items:        []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}},
		missingCount: 3,
	}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	e, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e.ExcludedCostItemCount != 3 {
		t.Fatalf("expected ExcludedCostItemCount=3, got %d", e.ExcludedCostItemCount)
	}
}

func TestCreateEstimateRejectsWhenProjectAlreadyHasAnEstimate(t *testing.T) {
	// BLOCKING fix (second plan review, issue #4): POST /estimates must
	// create ONLY Version 1. Calling it again after V1 exists (whether
	// draft or finalized) must NOT silently mint V2 — that is exclusively
	// CreateNewVersion's job, and bypassing it would also bypass
	// CreateNewVersion's "source must be finalized" precondition.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
	}

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject, got %v", err)
	}

	// Also rejected when the existing version is still a draft — the rule
	// is "any version already exists," not "any FINALIZED version exists."
	repo2 := newFakeEstimateRepository()
	svc2 := estimates.NewService(repo2, fakeProjectLookup{belongs: true}, costSource)
	repo2.byID["e2"] = estimates.Estimate{
		ID: "e2", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft,
	}
	_, err = svc2.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject against an existing draft too, got %v", err)
	}
}

func TestCreateEstimateDoesNotRetryOnVersionConflict(t *testing.T) {
	// BLOCKING FIX (third plan review round, issue #2): CreateEstimate must
	// NEVER retry into Version 2+, unlike CreateNewVersion. racyRepo below
	// simulates a concurrent writer winning the Version-1 race ahead of
	// this caller — CreateEstimate must translate that single collision
	// directly into ErrEstimateAlreadyExistsForProject, with exactly ONE
	// Create attempt, never re-reading MAX(version) and trying again at a
	// higher number (which is exactly the race the third-round fix closes;
	// see CreateEstimate's own doc comment for the worked-example race this
	// prevents).
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrVersionConflict}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject (translated from the version-conflict collision, not retried), got %v", err)
	}
	if repo.attempts != 1 {
		t.Fatalf("expected exactly 1 Create attempt (no retry), got %d", repo.attempts)
	}
}

func TestCreateEstimateDoesNotRetryOnDraftAlreadyExists(t *testing.T) {
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrDraftAlreadyExists}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(50000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	_, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != estimates.ErrEstimateAlreadyExistsForProject {
		t.Fatalf("expected ErrEstimateAlreadyExistsForProject (translated from the draft-collision), got %v", err)
	}
	if repo.attempts != 1 {
		t.Fatalf("expected exactly 1 Create attempt (no retry), got %d", repo.attempts)
	}
}

// racyEstimateRepository wraps fakeEstimateRepository, forcing Create to
// return failWith for the first failFirstNAttempts calls regardless of the
// fake's own collision logic — simulating another concurrent writer
// winning a race ahead of this caller. Used by both:
//   - CreateEstimate's tests above, to prove NO retry occurs (third plan
//     review round, issue #2);
//   - CreateNewVersion's TestCreateNewVersionRetriesOnVersionConflict
//     below, to prove the PRODUCTION allocateAndCreate helper DOES retry
//     (second plan review round, issue #3) — the two methods have
//     deliberately different concurrency contracts, and this fake proves
//     both, one no-retry and one retrying, against the same underlying
//     simulated race.
type racyEstimateRepository struct {
	*fakeEstimateRepository
	failFirstNAttempts int
	failWith           error
	attempts           int
}

func (r *racyEstimateRepository) Create(ctx context.Context, e estimates.Estimate) (estimates.Estimate, error) {
	r.attempts++
	if r.attempts <= r.failFirstNAttempts {
		return estimates.Estimate{}, r.failWith
	}
	return r.fakeEstimateRepository.Create(ctx, e)
}

func TestGetLatestEstimate(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1}
	repo.byID["e2"] = estimates.Estimate{ID: "e2", CompanyID: "company_a", ProjectID: "project_1", Version: 2}

	latest, err := svc.GetLatestEstimate(context.Background(), "company_a", "project_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest.Version != 2 {
		t.Fatalf("expected version 2, got %d", latest.Version)
	}
}

type fakeCostSourceWithMissingCount struct {
	items        []fakeCostSourceItem
	missingCount int
}

func (f *fakeCostSourceWithMissingCount) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	for _, item := range f.items {
		if err := visit(item.costItemID, item.workItemID, item.category, item.description, item.estimated); err != nil {
			return 0, err
		}
	}
	return f.missingCount, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: FAIL with `undefined: estimates.NewService` (and related undefined symbols — `ErrProjectNotFound`, `Service`, etc.)

- [ ] **Step 3: Implement `Service` with creation and read operations**

Create `backend/internal/estimates/service.go`:

```go
package estimates

import (
	"context"
	"errors"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ErrProjectNotFound is returned when the given projectID does not belong
// to the caller's company.
var ErrProjectNotFound = errors.New("estimates: project not found")

// ErrEstimateMustBeFinalizedBeforeNewVersion is returned by CreateNewVersion
// when the source Estimate is not yet Status=finalized (design spec §17
// step 2, added in the second review round).
var ErrEstimateMustBeFinalizedBeforeNewVersion = errors.New("estimates: source estimate must be finalized before creating a new version")

// ErrEstimateNotDraft is returned when refresh or pricing recalculation is
// attempted against an Estimate that is not Status=draft.
var ErrEstimateNotDraft = errors.New("estimates: estimate is not a draft")

// ProjectLookup is the capability estimates needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

// EstimatedCostSource is the capability estimates needs from costs (design
// spec §6). Satisfied structurally by costs.Service.
type EstimatedCostSource interface {
	VisitEstimatedCostItems(
		ctx context.Context,
		companyID string,
		projectID string,
		visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
	) (missingEstimatedCount int, err error)
}

// Service implements Estimate creation, versioning, refresh, pricing
// recalculation, and finalization.
type Service struct {
	repo          EstimateRepository
	projectLookup ProjectLookup
	costSource    EstimatedCostSource
}

// NewService constructs a Service backed by repo, consuming projectLookup
// and costSource to validate the Project and build cost snapshots.
func NewService(repo EstimateRepository, projectLookup ProjectLookup, costSource EstimatedCostSource) *Service {
	return &Service{repo: repo, projectLookup: projectLookup, costSource: costSource}
}

// buildSnapshot visits every eligible CostItem under projectID via
// costSource, validates currency/positivity via SumLines, and returns the
// resulting Lines/CostSubtotal/ExcludedCostItemCount. Shared by
// CreateEstimate, CreateNewVersion (Task 7), and RefreshEstimate (Task 6) —
// no snapshot-building logic is duplicated across entry points (design spec
// §25).
func (s *Service) buildSnapshot(ctx context.Context, companyID, projectID string) ([]EstimateCostLine, money.Money, int, error) {
	var lines []EstimateCostLine
	missingCount, err := s.costSource.VisitEstimatedCostItems(ctx, companyID, projectID,
		func(costItemID string, workItemID *string, category, description string, estimated money.Money) error {
			lines = append(lines, EstimateCostLine{
				SourceCostItemID: costItemID, WorkItemID: workItemID,
				Category: category, Description: description, SnapshottedAmount: estimated,
			})
			return nil
		})
	if err != nil {
		return nil, money.Money{}, 0, err
	}

	subtotal, err := SumLines(lines)
	if err != nil {
		return nil, money.Money{}, 0, err
	}

	return lines, subtotal, missingCount, nil
}

// ErrEstimateAlreadyExistsForProject is returned by CreateEstimate when the
// Project already has at least one Estimate version (i.e. MAX(version) > 0).
// CreateEstimate creates ONLY Version 1 for a Project — creating any
// subsequent version is exclusively CreateNewVersion's job, and that path
// requires the source to be finalized (design spec §17 step 2). Without
// this check, POST /estimates could be called repeatedly to silently mint
// V2, V3, ... bypassing the "new version requires a finalized source" rule
// entirely (second plan review, issue #4).
var ErrEstimateAlreadyExistsForProject = errors.New("estimates: an estimate already exists for this project; use CreateNewVersion from a finalized version instead")

const maxVersionAllocationAttempts = 5

// allocateAndCreate implements the bounded-retry version-number allocation
// strategy from design spec §21 steps 1-3/5-6, IN PRODUCTION CODE (not
// merely in a test helper — second plan review, issue #3): read
// MAX(version), attempt Create at MAX+1, and on ErrVersionConflict (another
// concurrent writer claimed that exact version number first) re-read
// MAX(version) and retry, bounded at maxVersionAllocationAttempts. A
// collision on the OTHER unique index (ErrDraftAlreadyExists — a draft
// already exists for this project) is NOT retried here; it is propagated
// immediately, since retrying "create a new draft" when one already exists
// is not a transient condition (design spec §21 step 4).
//
// Used ONLY by CreateNewVersion (third plan review round, issue #2 —
// CreateEstimate deliberately does NOT use this helper, and must never be
// changed to do so: retrying into a higher version number is exactly the
// bypass of "POST /estimates creates ONLY Version 1" that issue #2 fixed.
// The two methods have genuinely different concurrency contracts — this
// helper's retry-into-the-next-version behavior is correct for
// CreateNewVersion and wrong for CreateEstimate). build is called fresh on
// every attempt with the freshly-read version number, so it can close over
// whatever fields CreateNewVersion needs to stamp onto the new document.
func (s *Service) allocateAndCreate(ctx context.Context, companyID, projectID string, build func(version int) Estimate) (Estimate, error) {
	for attempt := 0; attempt < maxVersionAllocationAttempts; attempt++ {
		maxVersion, err := s.repo.FindMaxVersion(ctx, companyID, projectID)
		if err != nil {
			return Estimate{}, err
		}
		created, err := s.repo.Create(ctx, build(maxVersion+1))
		if err == nil {
			return created, nil
		}
		if err == ErrVersionConflict {
			continue // another goroutine won this version number; retry
		}
		return Estimate{}, err // includes ErrDraftAlreadyExists, propagated immediately (not retried)
	}
	return Estimate{}, ErrVersionConflict
}

// CreateEstimate creates Version 1 for a Project (always a draft) — and
// ONLY EVER Version 1, with NO retry loop (BLOCKING FIX, third plan
// review round, issue #2 — corrects the second round's own fix, which was
// still theoretically racy). Validates projectID via ProjectLookup, builds
// the cost snapshot via buildSnapshot, and calculates pricing (design spec
// §4, §17, §23).
//
// Why this does NOT call allocateAndCreate: the second review round's fix
// for "CreateEstimate must only create V1" checked existingMax > 0 up
// front, but then delegated to the SHARED allocateAndCreate helper, which
// recalculates MAX(version)+1 on every retry attempt. That reintroduces
// the exact race the check was meant to close:
//
//	Request A reads MAX(version) = 0 (via the up-front existingMax check)
//	Request B reads MAX(version) = 0 (same up-front check, same instant)
//	A proceeds, creates Version 1, it later gets finalized
//	B's CALL TO allocateAndCreate re-reads MAX(version) — now 1 — and
//	  would attempt Version 2, succeeding via POST /estimates
//
// That is a silent bypass of "POST /estimates creates ONLY Version 1" —
// exactly the invariant this method exists to enforce. The fix is to
// remove retry semantics from CreateEstimate entirely: attempt Version 1
// directly, exactly once. ANY collision on either unique index (a
// concurrent Version-1 creation racing this one, OR an existing later
// version already present) is classified as
// ErrEstimateAlreadyExistsForProject — never retried into Version 2.
// allocateAndCreate (with its MAX+1 retry semantics) remains correct and
// necessary for CreateNewVersion below, where retrying into the NEXT
// version number is exactly the desired behavior — the two methods have
// genuinely different concurrency contracts and must not share one helper.
func (s *Service) CreateEstimate(ctx context.Context, companyID, projectID string, pricingMode PricingMode, pricingRate money.RateBPS) (Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if !belongs {
		return Estimate{}, ErrProjectNotFound
	}

	existingMax, err := s.repo.FindMaxVersion(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if existingMax > 0 {
		return Estimate{}, ErrEstimateAlreadyExistsForProject
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, pricingMode, pricingRate)
	if err != nil {
		return Estimate{}, err
	}

	created, err := s.repo.Create(ctx, Estimate{
		CompanyID: companyID, ProjectID: projectID, Version: 1, Status: EstimateStatusDraft, Revision: 0,
		Currency: subtotal.Currency, Lines: lines, CostSubtotal: subtotal, ExcludedCostItemCount: missingCount,
		PricingMode: pricingMode, PricingRate: pricingRate,
		ProposedSellingPrice: pricing.ProposedSellingPrice, ProjectedGrossProfit: pricing.ProjectedGrossProfit,
		ProjectedGrossMarginBPS: pricing.ProjectedGrossMarginBPS,
		CreatedAt:               time.Now(), SchemaVersion: 1,
	})
	if err == ErrVersionConflict || err == ErrDraftAlreadyExists {
		// Either collision means "an estimate already exists for this
		// project" from CreateEstimate's point of view — a concurrent
		// request won the race to create Version 1 first (ErrVersionConflict
		// on the {companyId,projectId,version} index), or a draft already
		// exists for some other reason (ErrDraftAlreadyExists on the
		// partial index). NEITHER is retried here — retrying would either
		// recompute a higher version number (exactly the bypass this fix
		// closes) or spin uselessly against an existing draft. The correct
		// caller action in both cases is the same: this project already has
		// an Estimate; use CreateNewVersion once it's finalized.
		return Estimate{}, ErrEstimateAlreadyExistsForProject
	}
	if err != nil {
		return Estimate{}, err
	}
	return created, nil
}

// GetEstimate returns estimateID's Estimate, tenant-scoped to companyID.
func (s *Service) GetEstimate(ctx context.Context, companyID, estimateID string) (Estimate, error) {
	return s.repo.FindByID(ctx, companyID, estimateID)
}

// GetLatestEstimate returns the highest-Version Estimate for projectID,
// tenant-scoped, after validating projectID belongs to companyID.
func (s *Service) GetLatestEstimate(ctx context.Context, companyID, projectID string) (Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Estimate{}, err
	}
	if !belongs {
		return Estimate{}, ErrProjectNotFound
	}
	return s.repo.FindLatestByProject(ctx, companyID, projectID)
}

// ListEstimatesByProject validates projectID belongs to companyID before
// listing — a foreign projectID returns ErrProjectNotFound, never an empty
// list (design spec §19.6).
func (s *Service) ListEstimatesByProject(ctx context.Context, companyID, projectID string) ([]Estimate, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/service.go`/`service_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 6: `Service` — `RefreshEstimate` and `RecalculatePricing`

**Spec:** §16.1, §16.2. Both draft-only, both Revision-guarded, both reuse `buildSnapshot`/`CalculatePricing` from Task 5.

**Files:**
- Modify: `backend/internal/estimates/service.go`
- Modify: `backend/internal/estimates/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `backend/internal/estimates/service_test.go`:

```go
func TestRecalculatePricingHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency: "MYR", CostSubtotal: money.New(10000, "MYR"), PricingMode: estimates.PricingModeMarkup, PricingRate: 2000,
	}
	repo.byID["e1"] = draft

	updated, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMargin, 2000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.PricingMode != estimates.PricingModeMargin || updated.PricingRate != 2000 {
		t.Fatalf("expected pricing fields updated, got %+v", updated)
	}
	if updated.ProposedSellingPrice.Amount != 12500 {
		t.Fatalf("expected selling price 12500 (margin recalculated from stored subtotal), got %+v", updated.ProposedSellingPrice)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", updated.Revision)
	}
}

func TestRecalculatePricingNeverTouchesLines(t *testing.T) {
	repo := newFakeEstimateRepository()
	// A cost source that would fail the test if consulted during pricing
	// recalculation — proves RecalculatePricing never reaches into the
	// live cost source.
	costSource := failingCostSource{t: t}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency: "MYR", CostSubtotal: money.New(10000, "MYR"),
		Lines: []estimates.EstimateCostLine{{SourceCostItemID: "c1", SnapshottedAmount: money.New(10000, "MYR")}},
	}
	repo.byID["e1"] = draft

	updated, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Lines) != 1 || updated.Lines[0].SourceCostItemID != "c1" {
		t.Fatalf("expected Lines untouched, got %+v", updated.Lines)
	}
}

func TestRecalculatePricingRejectsFinalized(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 3,
		CostSubtotal: money.New(10000, "MYR"),
	}

	_, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 3)
	if err != estimates.ErrEstimateNotDraft {
		t.Fatalf("expected ErrEstimateNotDraft, got %v", err)
	}
}

func TestRecalculatePricingStaleRevisionRejected(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 5,
		CostSubtotal: money.New(10000, "MYR"),
	}

	_, err := svc.RecalculatePricing(context.Background(), "company_a", "e1", estimates.PricingModeMarkup, 1000, 4)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

func TestRefreshEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", category: "material", description: "Tiles", estimated: money.New(120000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	draft := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft, Revision: 0,
		Currency: "MYR",
		Lines: []estimates.EstimateCostLine{{SourceCostItemID: "c1", SnapshottedAmount: money.New(50000, "MYR")}},
		CostSubtotal: money.New(50000, "MYR"), PricingMode: estimates.PricingModeMarkup, PricingRate: 2000,
	}
	repo.byID["e1"] = draft

	updated, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.CostSubtotal.Amount != 120000 {
		t.Fatalf("expected CostSubtotal updated to 120000, got %+v", updated.CostSubtotal)
	}
	if updated.Version != 1 {
		t.Fatalf("expected Version unchanged at 1, got %d", updated.Version)
	}
	if updated.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", updated.Revision)
	}
	if updated.RefreshedAt == nil {
		t.Fatal("expected RefreshedAt to be set")
	}
	// Pricing must be recomputed against the NEW subtotal, using the
	// EXISTING PricingMode/PricingRate (20% markup carried forward).
	if updated.ProposedSellingPrice.Amount != 144000 {
		t.Fatalf("expected selling price recomputed as 144000 (RM1200 + 20%%), got %+v", updated.ProposedSellingPrice)
	}
}

func TestRefreshEstimateFailureLeavesDraftUntouched(t *testing.T) {
	repo := newFakeEstimateRepository()
	// The refreshed cost_items are now mixed-currency — refresh must fail
	// and leave the existing snapshot completely untouched.
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(50000, "MYR")},
		{costItemID: "c2", estimated: money.New(30000, "SGD")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	original := estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Status: estimates.EstimateStatusDraft, Revision: 0,
		Lines: []estimates.EstimateCostLine{{SourceCostItemID: "old", SnapshottedAmount: money.New(1000, "MYR")}},
		CostSubtotal: money.New(1000, "MYR"),
	}
	repo.byID["e1"] = original

	_, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 0)
	if err != estimates.ErrMixedCurrencyCostItems {
		t.Fatalf("expected ErrMixedCurrencyCostItems, got %v", err)
	}

	unchanged := repo.byID["e1"]
	if unchanged.CostSubtotal.Amount != 1000 || unchanged.Revision != 0 {
		t.Fatalf("expected draft completely untouched after failed refresh, got %+v", unchanged)
	}
}

func TestRefreshEstimateRejectsFinalized(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 2}

	_, err := svc.RefreshEstimate(context.Background(), "company_a", "e1", 2)
	if err != estimates.ErrEstimateNotDraft {
		t.Fatalf("expected ErrEstimateNotDraft, got %v", err)
	}
}

type failingCostSource struct{ t *testing.T }

func (f failingCostSource) VisitEstimatedCostItems(ctx context.Context, companyID, projectID string,
	visit func(costItemID string, workItemID *string, category string, description string, estimated money.Money) error,
) (int, error) {
	f.t.Helper()
	f.t.Fatal("cost source must not be consulted during pricing recalculation")
	return 0, nil
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -run "TestRecalculatePricing|TestRefreshEstimate" -v`
Expected: FAIL with `undefined: (*estimates.Service).RecalculatePricing` (and `RefreshEstimate`)

- [ ] **Step 3: Implement `RecalculatePricing` and `RefreshEstimate`**

In `backend/internal/estimates/service.go`, add after `ListEstimatesByProject`:

```go
// RecalculatePricing recomputes pricing fields from the ALREADY-STORED
// CostSubtotal — never re-touches cost_items. Draft-only; rejects
// (ErrEstimateNotDraft) if Status != draft. Revision-guarded: expectedRevision
// must match the document's current Revision or ErrRevisionMismatch is
// returned (design spec §16.1).
func (s *Service) RecalculatePricing(ctx context.Context, companyID, estimateID string, pricingMode PricingMode, pricingRate money.RateBPS, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status != EstimateStatusDraft {
		return Estimate{}, ErrEstimateNotDraft
	}

	pricing, err := CalculatePricing(existing.CostSubtotal, pricingMode, pricingRate)
	if err != nil {
		return Estimate{}, err
	}

	updated := existing
	updated.PricingMode = pricingMode
	updated.PricingRate = pricingRate
	updated.ProposedSellingPrice = pricing.ProposedSellingPrice
	updated.ProjectedGrossProfit = pricing.ProjectedGrossProfit
	updated.ProjectedGrossMarginBPS = pricing.ProjectedGrossMarginBPS

	return s.repo.UpdatePricing(ctx, companyID, estimateID, expectedRevision, updated)
}

// RefreshEstimate re-pulls current cost_items into this draft, in place,
// keeping the same Version. Recomputes pricing against the new
// CostSubtotal using the currently-stored PricingMode/PricingRate. Draft-only;
// rejects (ErrEstimateNotDraft) if Status != draft. Revision-guarded, same
// contract as RecalculatePricing. On any snapshot-building failure (no
// eligible cost items, mixed currency, non-positive subtotal), the existing
// draft is left completely untouched — no partial update (design spec
// §16.2).
func (s *Service) RefreshEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status != EstimateStatusDraft {
		return Estimate{}, ErrEstimateNotDraft
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, existing.ProjectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, existing.PricingMode, existing.PricingRate)
	if err != nil {
		return Estimate{}, err
	}

	now := time.Now()
	updated := existing
	updated.Currency = subtotal.Currency
	updated.Lines = lines
	updated.CostSubtotal = subtotal
	updated.ExcludedCostItemCount = missingCount
	updated.ProposedSellingPrice = pricing.ProposedSellingPrice
	updated.ProjectedGrossProfit = pricing.ProjectedGrossProfit
	updated.ProjectedGrossMarginBPS = pricing.ProjectedGrossMarginBPS
	updated.RefreshedAt = &now

	return s.repo.ReplaceSnapshot(ctx, companyID, estimateID, expectedRevision, updated)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/service.go`/`service_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 7: `Service` — `FinalizeEstimate` and `CreateNewVersion`

**Spec:** §16.3, §17, §21. `FinalizeEstimate` is Revision-guarded with an idempotency carve-out (per the second review round). `CreateNewVersion` requires the source to already be finalized, checked explicitly before any snapshot work (per the second review round).

**Files:**
- Modify: `backend/internal/estimates/service.go`
- Modify: `backend/internal/estimates/service_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `backend/internal/estimates/service_test.go`:

```go
func TestFinalizeEstimateHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 4}

	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", updated.Status)
	}
	if updated.FinalizedAt == nil {
		t.Fatal("expected FinalizedAt set")
	}
	// BLOCKING invariant (design spec §16.3, second plan review issue #1):
	// finalize FREEZES Revision — Draft Revision 4 -> Finalize -> Finalized
	// Revision 4, never Revision 5.
	if updated.Revision != 4 {
		t.Fatalf("expected Revision to remain 4 after finalize (frozen, not incremented), got %d", updated.Revision)
	}
}

func TestFinalizeEstimateStaleReviewScenario(t *testing.T) {
	// The exact worked example from the second review round: contractor A
	// reads Revision 4, another mutation advances to Revision 5, A tries to
	// finalize at the stale Revision 4 -> rejected; A re-reads, retries at
	// Revision 5 -> succeeds.
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusDraft, Revision: 5}

	_, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 4)
	if err != estimates.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch for stale expectedRevision=4, got %v", err)
	}
	if repo.byID["e1"].Status != estimates.EstimateStatusDraft {
		t.Fatal("expected estimate to remain draft after rejected finalize attempt")
	}

	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 5)
	if err != nil {
		t.Fatalf("expected success with the current revision 5, got error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized {
		t.Fatalf("expected Status=finalized, got %s", updated.Status)
	}
}

func TestFinalizeEstimateIdempotentOnRetry(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	now := time.Now()
	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", Status: estimates.EstimateStatusFinalized, Revision: 7, FinalizedAt: &now,
	}

	// A retry supplies an arbitrary/stale expectedRevision — must still
	// succeed and return the unchanged Estimate, since it is already
	// finalized (idempotency carve-out, design spec §16.3 step 2).
	updated, err := svc.FinalizeEstimate(context.Background(), "company_a", "e1", 0)
	if err != nil {
		t.Fatalf("expected idempotent success on an already-finalized estimate regardless of expectedRevision, got error: %v", err)
	}
	if updated.Status != estimates.EstimateStatusFinalized || updated.Revision != 7 {
		t.Fatalf("expected unchanged finalized estimate returned, got %+v", updated)
	}
}

func TestCreateNewVersionRequiresFinalizedSource(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	repo.byID["e1"] = estimates.Estimate{ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusDraft}

	_, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != estimates.ErrEstimateMustBeFinalizedBeforeNewVersion {
		t.Fatalf("expected ErrEstimateMustBeFinalizedBeforeNewVersion, got %v", err)
	}
}

func TestCreateNewVersionHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", estimated: money.New(150000, "MYR")}, // cost has changed since v1
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}

	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version=2, got %d", v2.Version)
	}
	if v2.Status != estimates.EstimateStatusDraft {
		t.Fatalf("expected new version to start as draft, got %s", v2.Status)
	}
	if v2.Revision != 0 {
		t.Fatalf("expected new version to start at Revision=0, got %d", v2.Revision)
	}
	if v2.CostSubtotal.Amount != 150000 {
		t.Fatalf("expected fresh snapshot reflecting the updated cost (150000), got %+v", v2.CostSubtotal)
	}
	// Pricing defaults carried forward from source (15% markup) since no
	// override was supplied.
	if v2.PricingMode != estimates.PricingModeMarkup || v2.PricingRate != 1500 {
		t.Fatalf("expected pricing defaults carried forward from source, got %+v", v2)
	}
}

func TestCreateNewVersionPricingOverride(t *testing.T) {
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(100000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}

	newMode := estimates.PricingModeMargin
	newRate := money.RateBPS(2500)
	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", &newMode, &newRate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.PricingMode != estimates.PricingModeMargin || v2.PricingRate != 2500 {
		t.Fatalf("expected overridden pricing to take effect, got %+v", v2)
	}
}

func TestCreateNewVersionRetriesOnVersionConflict(t *testing.T) {
	// Unlike CreateEstimate (which must NEVER retry — see
	// TestCreateEstimateDoesNotRetryOnVersionConflict in Task 5),
	// CreateNewVersion legitimately uses allocateAndCreate's bounded-retry
	// MAX(version)+1 strategy, since retrying into the next version number
	// IS the correct behavior here — a concurrent request creating some
	// OTHER project's next version, or racing this exact call, should not
	// cause a spurious failure when a fresh version number is available on
	// retry. Reuses racyEstimateRepository from Task 5 to simulate exactly
	// 1 version-conflict collision before succeeding.
	repo := &racyEstimateRepository{fakeEstimateRepository: newFakeEstimateRepository(), failFirstNAttempts: 1, failWith: estimates.ErrVersionConflict}
	costSource := fakeCostSource{items: []fakeCostSourceItem{{costItemID: "c1", estimated: money.New(100000, "MYR")}}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	repo.byID["e1"] = estimates.Estimate{
		ID: "e1", CompanyID: "company_a", ProjectID: "project_1", Version: 1, Status: estimates.EstimateStatusFinalized,
		PricingMode: estimates.PricingModeMarkup, PricingRate: 1500,
	}
	// The fake's own next-ID counter starts fresh; account for the seeded
	// "e1" above not going through repo.Create, so racyEstimateRepository's
	// attempt counter only reflects CreateNewVersion's own Create calls.

	v2, err := svc.CreateNewVersion(context.Background(), "company_a", "e1", nil, nil)
	if err != nil {
		t.Fatalf("expected CreateNewVersion to succeed after retrying past one simulated version conflict, got error: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version=2, got %d", v2.Version)
	}
	if repo.attempts != 2 {
		t.Fatalf("expected exactly 2 Create attempts (1 simulated conflict + 1 success), got %d", repo.attempts)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -run "TestFinalizeEstimate|TestCreateNewVersion" -v`
Expected: FAIL with `undefined: (*estimates.Service).FinalizeEstimate` (and `CreateNewVersion`)

- [ ] **Step 3: Implement `FinalizeEstimate` and `CreateNewVersion`**

In `backend/internal/estimates/service.go`, add after `RefreshEstimate`:

```go
// FinalizeEstimate transitions a draft to finalized, one-directional,
// locking every field. Revision-guarded like refresh/pricing recalculation
// (added in the second review round — finalize is the single most
// consequential draft mutation and needs this guard more than any other):
// a stale expectedRevision against a still-draft Estimate is rejected
// (ErrRevisionMismatch), leaving the document unchanged. Idempotent on
// retry: if the Estimate is ALREADY finalized, returns it unchanged
// regardless of the supplied expectedRevision, since a retry cannot know
// the frozen post-finalize Revision value (design spec §16.3).
func (s *Service) FinalizeEstimate(ctx context.Context, companyID, estimateID string, expectedRevision int64) (Estimate, error) {
	existing, err := s.repo.FindByID(ctx, companyID, estimateID)
	if err != nil {
		return Estimate{}, err
	}
	if existing.Status == EstimateStatusFinalized {
		return existing, nil
	}

	return s.repo.Finalize(ctx, companyID, estimateID, expectedRevision, time.Now())
}

// CreateNewVersion creates the next Version as a new draft from
// sourceEstimateID's Project. REQUIRES the source Estimate to already be
// Status=finalized (design spec §17 step 2, added in the second review
// round) — rejected immediately with
// ErrEstimateMustBeFinalizedBeforeNewVersion if the source is still a
// draft, before any snapshot-building or version-number allocation is
// attempted. pricingMode/pricingRate default to the source Estimate's own
// values if nil (carrying forward the contractor's last pricing choice).
func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceEstimateID string, pricingMode *PricingMode, pricingRate *money.RateBPS) (Estimate, error) {
	source, err := s.repo.FindByID(ctx, companyID, sourceEstimateID)
	if err != nil {
		return Estimate{}, err
	}
	if source.Status != EstimateStatusFinalized {
		return Estimate{}, ErrEstimateMustBeFinalizedBeforeNewVersion
	}

	mode := source.PricingMode
	if pricingMode != nil {
		mode = *pricingMode
	}
	rate := source.PricingRate
	if pricingRate != nil {
		rate = *pricingRate
	}

	lines, subtotal, missingCount, err := s.buildSnapshot(ctx, companyID, source.ProjectID)
	if err != nil {
		return Estimate{}, err
	}

	pricing, err := CalculatePricing(subtotal, mode, rate)
	if err != nil {
		return Estimate{}, err
	}

	// Uses the SAME production allocateAndCreate helper CreateEstimate
	// uses (Task 5) — the bounded-retry version-number allocation strategy
	// (design spec §21) must live in one place, not be duplicated here as
	// a second copy that could drift from the first (second plan review,
	// issue #3).
	return s.allocateAndCreate(ctx, companyID, source.ProjectID, func(version int) Estimate {
		return Estimate{
			CompanyID: companyID, ProjectID: source.ProjectID, Version: version, Status: EstimateStatusDraft, Revision: 0,
			Currency: subtotal.Currency, Lines: lines, CostSubtotal: subtotal, ExcludedCostItemCount: missingCount,
			PricingMode: mode, PricingRate: rate,
			ProposedSellingPrice: pricing.ProposedSellingPrice, ProjectedGrossProfit: pricing.ProjectedGrossProfit,
			ProjectedGrossMarginBPS: pricing.ProjectedGrossMarginBPS,
			CreatedAt:               time.Now(), SchemaVersion: 1,
		}
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS (full `estimates` package test suite green)

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/service.go`/`service_test.go`. Leave them as plain working-tree modifications and move to the next task.

---

## Task 8: Huma HTTP handlers

**Spec:** §23. 8 endpoints total: `POST /estimates`, `GET /estimates`, `GET /estimates/latest`, `GET /estimates/{id}`, `POST /estimates/{id}/refresh`, `PATCH /estimates/{id}/pricing`, `POST /estimates/{id}/versions`, `POST /estimates/{id}/finalize`. Package-qualified DTO names (`estimateMoneyDTO`, not `moneyDTO`) per the standing Huma schema-registry collision lesson from M3.

**Files:**
- Create: `backend/internal/estimates/handler.go`

- [ ] **Step 1: Write the handler**

Create `backend/internal/estimates/handler.go`:

```go
package estimates

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type estimateMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type estimateLineDTO struct {
	SourceCostItemID  string           `json:"sourceCostItemId"`
	WorkItemID        string           `json:"workItemId,omitempty"`
	Category          string           `json:"category"`
	Description       string           `json:"description"`
	SnapshottedAmount estimateMoneyDTO `json:"snapshottedAmount"`
}

type estimateDTO struct {
	ID                      string            `json:"id"`
	ProjectID               string            `json:"projectId"`
	Version                 int               `json:"version"`
	Status                  string            `json:"status"`
	Revision                int64             `json:"revision"`
	Currency                string            `json:"currency"`
	Lines                   []estimateLineDTO `json:"lines"`
	CostSubtotal            estimateMoneyDTO  `json:"costSubtotal"`
	ExcludedCostItemCount   int               `json:"excludedCostItemCount"`
	PricingMode             string            `json:"pricingMode"`
	PricingRate             int64             `json:"pricingRate"`
	ProposedSellingPrice    estimateMoneyDTO  `json:"proposedSellingPrice"`
	ProjectedGrossProfit    estimateMoneyDTO  `json:"projectedGrossProfit"`
	ProjectedGrossMarginBPS int64             `json:"projectedGrossMarginBps"`
	CreatedAt               string            `json:"createdAt"`
	RefreshedAt             string            `json:"refreshedAt,omitempty"`
	FinalizedAt             string            `json:"finalizedAt,omitempty"`
}

type createEstimateInput struct {
	Body struct {
		ProjectID   string `json:"projectId" required:"true" minLength:"1"`
		PricingMode string `json:"pricingMode" required:"true"`
		PricingRate int64  `json:"pricingRate" required:"true"`
	}
}

type estimateOutput struct {
	Body estimateDTO
}

type listEstimatesInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type listEstimatesOutput struct {
	Body struct {
		Estimates []estimateDTO `json:"estimates"`
	}
}

type latestEstimateInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type getEstimateInput struct {
	ID string `path:"id"`
}

type refreshEstimateInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type recalculatePricingInput struct {
	ID   string `path:"id"`
	Body struct {
		PricingMode      string `json:"pricingMode" required:"true"`
		PricingRate      int64  `json:"pricingRate" required:"true"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type createNewVersionInput struct {
	ID   string `path:"id"`
	Body struct {
		PricingMode *string `json:"pricingMode,omitempty"`
		PricingRate *int64  `json:"pricingRate,omitempty"`
	}
}

type finalizeEstimateInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

// RegisterHandlers registers POST /estimates, GET /estimates,
// GET /estimates/latest, GET /estimates/{id}, POST /estimates/{id}/refresh,
// PATCH /estimates/{id}/pricing, POST /estimates/{id}/versions, and
// POST /estimates/{id}/finalize on api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "estimates-create",
		Method:      http.MethodPost,
		Path:        "/estimates",
		Summary:     "Create Version 1 for a Project, always as a draft",
	}, func(ctx context.Context, input *createEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.CreateEstimate(ctx, principal.CompanyID, input.Body.ProjectID,
			PricingMode(input.Body.PricingMode), money.RateBPS(input.Body.PricingRate))
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-list",
		Method:      http.MethodGet,
		Path:        "/estimates",
		Summary:     "List all Estimate versions for a Project",
	}, func(ctx context.Context, input *listEstimatesInput) (*listEstimatesOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListEstimatesByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		resp := &listEstimatesOutput{}
		for _, e := range list {
			resp.Body.Estimates = append(resp.Body.Estimates, toEstimateDTO(e))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-latest",
		Method:      http.MethodGet,
		Path:        "/estimates/latest",
		Summary:     "Get the single highest-Version Estimate for a Project",
	}, func(ctx context.Context, input *latestEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetLatestEstimate(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-get",
		Method:      http.MethodGet,
		Path:        "/estimates/{id}",
		Summary:     "Get one specific Estimate version, tenant-scoped",
	}, func(ctx context.Context, input *getEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.GetEstimate(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-refresh",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/refresh",
		Summary:     "Re-pull current cost_items into this draft, in place, same Version",
	}, func(ctx context.Context, input *refreshEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.RefreshEstimate(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-recalculate-pricing",
		Method:      http.MethodPatch,
		Path:        "/estimates/{id}/pricing",
		Summary:     "Recalculate pricing in place from the stored snapshot — draft only",
	}, func(ctx context.Context, input *recalculatePricingInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.RecalculatePricing(ctx, principal.CompanyID, input.ID,
			PricingMode(input.Body.PricingMode), money.RateBPS(input.Body.PricingRate), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-create-version",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/versions",
		Summary:     "Create the next Version as a new draft — source must already be finalized",
	}, func(ctx context.Context, input *createNewVersionInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var mode *PricingMode
		if input.Body.PricingMode != nil {
			m := PricingMode(*input.Body.PricingMode)
			mode = &m
		}
		var rate *money.RateBPS
		if input.Body.PricingRate != nil {
			r := money.RateBPS(*input.Body.PricingRate)
			rate = &r
		}
		e, err := svc.CreateNewVersion(ctx, principal.CompanyID, input.ID, mode, rate)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "estimates-finalize",
		Method:      http.MethodPost,
		Path:        "/estimates/{id}/finalize",
		Summary:     "draft -> finalized, one-directional, locks every field. Idempotent on retry.",
	}, func(ctx context.Context, input *finalizeEstimateInput) (*estimateOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		e, err := svc.FinalizeEstimate(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapEstimatesError(err)
		}
		return &estimateOutput{Body: toEstimateDTO(e)}, nil
	})
}

func toEstimateDTO(e Estimate) estimateDTO {
	dto := estimateDTO{
		ID: e.ID, ProjectID: e.ProjectID, Version: e.Version, Status: string(e.Status), Revision: e.Revision,
		Currency: e.Currency, CostSubtotal: estimateMoneyDTO{Amount: e.CostSubtotal.Amount, Currency: e.CostSubtotal.Currency},
		ExcludedCostItemCount: e.ExcludedCostItemCount, PricingMode: string(e.PricingMode), PricingRate: int64(e.PricingRate),
		ProposedSellingPrice: estimateMoneyDTO{Amount: e.ProposedSellingPrice.Amount, Currency: e.ProposedSellingPrice.Currency},
		ProjectedGrossProfit: estimateMoneyDTO{Amount: e.ProjectedGrossProfit.Amount, Currency: e.ProjectedGrossProfit.Currency},
		ProjectedGrossMarginBPS: int64(e.ProjectedGrossMarginBPS),
		CreatedAt:               e.CreatedAt.Format(timeLayout),
	}
	for _, l := range e.Lines {
		lineDTO := estimateLineDTO{
			SourceCostItemID: l.SourceCostItemID, Category: l.Category, Description: l.Description,
			SnapshottedAmount: estimateMoneyDTO{Amount: l.SnapshottedAmount.Amount, Currency: l.SnapshottedAmount.Currency},
		}
		if l.WorkItemID != nil {
			lineDTO.WorkItemID = *l.WorkItemID
		}
		dto.Lines = append(dto.Lines, lineDTO)
	}
	if e.RefreshedAt != nil {
		dto.RefreshedAt = e.RefreshedAt.Format(timeLayout)
	}
	if e.FinalizedAt != nil {
		dto.FinalizedAt = e.FinalizedAt.Format(timeLayout)
	}
	return dto
}

func mapEstimatesError(err error) error {
	switch {
	case errors.Is(err, ErrEstimateNotFound):
		return huma.Error404NotFound("estimate not found")
	case errors.Is(err, ErrNoEstimatesForProject):
		return huma.Error404NotFound("no estimates exist for this project")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrNoEstimatedCosts):
		return huma.Error422UnprocessableEntity("project has no cost items with an estimated amount")
	case errors.Is(err, ErrMixedCurrencyCostItems):
		return huma.Error422UnprocessableEntity("cost items span more than one currency")
	case errors.Is(err, ErrCostSubtotalNotPositive):
		return huma.Error422UnprocessableEntity("cost subtotal must be positive")
	case errors.Is(err, ErrInvalidPricingRate):
		return huma.Error422UnprocessableEntity("invalid pricing rate for the given pricing mode")
	case errors.Is(err, ErrInvalidPricingMode):
		return huma.Error422UnprocessableEntity("invalid pricing mode: must be 'markup' or 'margin'")
	case errors.Is(err, ErrEstimateNotDraft):
		return huma.Error409Conflict("estimate is not a draft")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("revision mismatch: the estimate has changed since it was last read")
	case errors.Is(err, ErrEstimateMustBeFinalizedBeforeNewVersion):
		return huma.Error409Conflict("source estimate must be finalized before creating a new version")
	case errors.Is(err, ErrEstimateAlreadyExistsForProject):
		return huma.Error409Conflict("an estimate already exists for this project; use POST /estimates/{id}/versions from a finalized version instead")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("version allocation conflict; please retry")
	case errors.Is(err, ErrDraftAlreadyExists):
		return huma.Error409Conflict("a draft already exists for this project")
	case errors.Is(err, ErrUnclassifiedDuplicateKey):
		// Deliberately mapped to 500, not 409 (third plan review round,
		// issue #1): an unclassified duplicate-key error is, by
		// definition, one this code could not positively identify as
		// either known safe-to-surface case — treating it as an ordinary
		// caller-actionable 409 would imply a confidence the code doesn't
		// actually have. Falls through to the same internal-error handling
		// any other unrecognized error gets.
		return err
	default:
		return err
	}
}
```

- [ ] **Step 2: Confirm the package builds**

Run: `cd backend && go build ./internal/estimates/...`
Expected: clean

- [ ] **Step 3: Run the full package test suite (no regressions)**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 4: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/handler.go`. Leave it as a plain working-tree modification and move to the next task.

---

## Task 9: Composition root wiring

**Spec:** §12. `estimates` depends on already-constructed `projects` and `costs`; append after the existing M3 block.

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/tenanttest/router.go`

- [ ] **Step 1: Update `cmd/api/main.go` imports**

In `backend/cmd/api/main.go`, add one new import to the existing import block (alphabetically among the existing `internal/` imports, after `costs` and before `identity`):

```go
	"github.com/shananth/renovation-platform/backend/internal/estimates"
```

- [ ] **Step 2: Add repository construction**

After the existing M3 repository block (`labourEntryRepo := labour.NewMongoLabourEntryRepository(db)`), add:

```go
	// --- Repositories (Milestone 4: estimates) ---
	estimateRepo := estimates.NewMongoEstimateRepository(db)
```

- [ ] **Step 3: Add index creation call**

After the existing `if err := labourEntryRepo.EnsureIndexes(indexCtx); err != nil { ... }` block, add:

```go
	if err := estimateRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure estimates indexes")
	}
```

- [ ] **Step 4: Add service construction**

After the existing M3 wiring block (`labourService := labour.NewService(...)`), add:

```go
	// --- Composition root: Milestone 4 wiring. estimates consumes
	// ProjectLookup (from projectsService, unchanged) and
	// EstimatedCostSource (from costsService's new VisitEstimatedCostItems
	// method) — matching
	// docs/superpowers/specs/2026-07-23-milestone-4-estimates-design.md §12.
	estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)
```

- [ ] **Step 5: Register HTTP handlers**

After the existing `labour.RegisterHandlers(authedAPI, labourService)` line, add:

```go
	estimates.RegisterHandlers(authedAPI, estimatesService)
```

- [ ] **Step 6: Run build to confirm `cmd/api` compiles**

Run: `cd backend && go build ./cmd/api/...`
Expected: clean

- [ ] **Step 7: Mirror the same changes in `internal/tenanttest/router.go`**

In `backend/internal/tenanttest/router.go`, add the import:

```go
	"github.com/shananth/renovation-platform/backend/internal/estimates"
```

Add repository construction after `labourEntryRepo := labour.NewMongoLabourEntryRepository(db)`:

```go
	estimateRepo := estimates.NewMongoEstimateRepository(db)
```

Extend the `EnsureIndexes` loop slice to include the new repository:

```go
	for _, ensure := range []func(context.Context) error{
		userRepo.EnsureIndexes, sessionRepo.EnsureIndexes, membershipRepo.EnsureIndexes,
		clientRepo.EnsureIndexes, projectRepo.EnsureIndexes, propertyRepo.EnsureIndexes,
		spaceRepo.EnsureIndexes, workItemRepo.EnsureIndexes,
		materialRepo.EnsureIndexes, costItemRepo.EnsureIndexes, workerRepo.EnsureIndexes, labourEntryRepo.EnsureIndexes,
		estimateRepo.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			return nil, err
		}
	}
```

Add service construction after `labourService := labour.NewService(...)`:

```go
	estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)
```

Add handler registration after `labour.RegisterHandlers(authedAPI, labourService)`:

```go
	estimates.RegisterHandlers(authedAPI, estimatesService)
```

- [ ] **Step 8: Run full build and test suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: all pass, no regressions across any M0/M1/M2/M3/M4 package

- [ ] **Step 9: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/cmd/api/main.go`/`backend/internal/tenanttest/router.go`. Leave them as plain working-tree modifications — committing is always a separate, explicit future request, never an automatic part of finishing a task.

---

## Task 10: Version-number and Revision concurrency integration tests

**Spec:** §21, §27. Proves the bounded-retry version-number allocation strategy and the Revision-guarded concurrent-mutation behavior against a real MongoDB instance.

**Files:**
- Modify: `backend/internal/estimates/repository_mongo_test.go`

- [ ] **Step 1: Write the concurrency tests**

Add to `backend/internal/estimates/repository_mongo_test.go`:

```go
func TestEstimateRepositoryConcurrentVersionCreation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_concurrent_version")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	// Seed N already-FINALIZED "source" estimates under the same project,
	// each at a distinct starting version, so N concurrent "create the next
	// version" attempts can proceed without tripping the one-draft-per-
	// project partial index against each other (that invariant is tested
	// separately in TestEstimateRepositoryOneDraftPerProjectPartialIndex).
	// This isolates proving the {companyId, projectId, version} unique
	// index's bounded-retry race-recovery behavior specifically.
	const n = 5
	for i := 1; i <= n; i++ {
		e := sampleEstimate("company_a", "project_1", i, estimates.EstimateStatusFinalized)
		if _, err := repo.Create(ctx, e); err != nil {
			t.Fatalf("unexpected error seeding version %d: %v", i, err)
		}
	}

	results := make(chan int, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			version, err := createNextVersionWithRetry(ctx, repo, "company_a", "project_1")
			if err != nil {
				errs <- err
				return
			}
			results <- version
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error in concurrent version creation: %v", err)
	}

	seen := make(map[int]bool)
	for v := range results {
		if seen[v] {
			t.Fatalf("duplicate version number %d produced by concurrent creation", v)
		}
		seen[v] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique new version numbers, got %d", n, len(seen))
	}
	for v := n + 1; v <= 2*n; v++ {
		if !seen[v] {
			t.Fatalf("expected version %d to have been created, got set %v", v, seen)
		}
	}
}

// createNextVersionWithRetry implements the bounded-retry strategy from
// design spec §21 steps 1-3/5-6: read MAX(version), attempt insert at
// MAX+1, retry on a version-number duplicate-key race, bounded at 5
// attempts.
func createNextVersionWithRetry(ctx context.Context, repo *estimates.MongoEstimateRepository, companyID, projectID string) (int, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		maxVersion, err := repo.FindMaxVersion(ctx, companyID, projectID)
		if err != nil {
			return 0, err
		}
		nextVersion := maxVersion + 1
		e := sampleEstimate(companyID, projectID, nextVersion, estimates.EstimateStatusFinalized)
		_, err = repo.Create(ctx, e)
		if err == nil {
			return nextVersion, nil
		}
		if mongo.IsDuplicateKeyError(err) {
			continue // another goroutine won this version number; retry
		}
		return 0, err
	}
	return 0, errors.New("exhausted retry attempts allocating a version number")
}

func TestEstimateRepositoryConcurrentDraftMutation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "estimates_test_concurrent_draft_mutation")
	repo := estimates.NewMongoEstimateRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	created, err := repo.Create(ctx, sampleEstimate("company_a", "project_1", 1, estimates.EstimateStatusDraft))
	if err != nil {
		t.Fatalf("unexpected error creating: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		updated := created
		updated.CostSubtotal = money.New(999999, "MYR")
		_, err := repo.ReplaceSnapshot(ctx, "company_a", created.ID, created.Revision, updated)
		results <- err
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		updated := created
		updated.PricingRate = 3000
		_, err := repo.UpdatePricing(ctx, "company_a", created.ID, created.Revision, updated)
		results <- err
	}()

	wg.Wait()
	close(results)

	successCount := 0
	conflictCount := 0
	for err := range results {
		switch err {
		case nil:
			successCount++
		case estimates.ErrRevisionMismatch:
			conflictCount++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successCount != 1 || conflictCount != 1 {
		t.Fatalf("expected exactly one success and one revision-mismatch conflict, got %d successes and %d conflicts", successCount, conflictCount)
	}
}
```

- [ ] **Step 2: Add the missing imports**

Update the import block at the top of `backend/internal/estimates/repository_mongo_test.go` to include `errors` and `sync`:

```go
import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/estimates"
	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)
```

- [ ] **Step 3: Run the new tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -run "TestEstimateRepositoryConcurrent" -v`
Expected: PASS (both concurrency tests). If `TestEstimateRepositoryConcurrentVersionCreation` flakes with a duplicate version, re-check that `createNextVersionWithRetry`'s retry loop is actually being exercised (add a t.Logf on retry) — 5 attempts should comfortably cover 5 concurrent goroutines contending for the version-number race in practice, but this is exactly the invariant this test exists to prove.

- [ ] **Step 4: Run the full package test suite**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS

- [ ] **Step 5: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/estimates/repository_mongo_test.go`. Leave it as a plain working-tree modification and move to the next task.

---

## Task 11: Full HTTP tenant-isolation and snapshot-immutability acceptance tests

**Spec:** §28-30. Reuses `internal/tenanttest`'s existing helpers (`setupRouter`/`BuildRouter`, `registerCompany`, `doJSON`, `mustField`) from `internal/tenanttest/tenant_isolation_test.go` — do not redefine them, add new test functions to the same file. Check that file's exact helper names/signatures before writing (they may differ slightly from what's assumed below — match what's actually there).

**Files:**
- Modify: `backend/internal/tenanttest/tenant_isolation_test.go`

- [ ] **Step 1: Read the existing acceptance-test file's helpers first**

Before writing new tests, read `backend/internal/tenanttest/tenant_isolation_test.go` in full to confirm the exact signatures of `setupRouter`/`BuildRouter` invocation, `registerCompany`, `doJSON`, `mustField`, and any existing helper that creates a full Client→Project→WorkItem→Material→Worker→LabourEntry→CostItem chain (the M3 acceptance tests almost certainly already have one reusable for this — e.g. `buildFullHierarchyForCompanyA` per the M3 plan's own Task 18 comment). Use whatever that helper is called and returns — do not reinvent project/cost-item creation from scratch in the new tests below.

- [ ] **Step 2: Write the tenant-isolation acceptance tests**

Add to `backend/internal/tenanttest/tenant_isolation_test.go` (adapt helper calls to match what Step 1 found — the shape below assumes helpers consistent with the M3 file's documented pattern):

```go
func TestEstimatesTenantIsolation(t *testing.T) {
	router := setupRouter(t)

	tokenA, companyA := registerCompany(t, router, "estimates-a@example.com", "Estimates Co A")
	tokenB, _ := registerCompany(t, router, "estimates-b@example.com", "Estimates Co B")
	_ = companyA

	projectA, costItemIDA := buildProjectWithEstimatedCostItem(t, router, tokenA)

	createBody := map[string]any{"projectId": projectA, "pricingMode": "markup", "pricingRate": 2000}
	estimateResp := doJSON(t, router, http.MethodPost, "/estimates", tokenA, createBody)
	if estimateResp.Code != http.StatusOK && estimateResp.Code != http.StatusCreated {
		t.Fatalf("expected estimate creation to succeed, got %d: %s", estimateResp.Code, estimateResp.Body.String())
	}
	estimateID := mustField(t, estimateResp, "id")
	_ = costItemIDA

	// B cannot GET A's estimate.
	getResp := doJSON(t, router, http.MethodGet, "/estimates/"+estimateID, tokenB, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant GET, got %d", getResp.Code)
	}

	// B cannot GET latest for A's project.
	latestResp := doJSON(t, router, http.MethodGet, "/estimates/latest?projectId="+projectA, tokenB, nil)
	if latestResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant latest lookup, got %d", latestResp.Code)
	}

	// B cannot list A's project's estimates.
	listResp := doJSON(t, router, http.MethodGet, "/estimates?projectId="+projectA, tokenB, nil)
	if listResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant list, not an empty array, got %d: %s", listResp.Code, listResp.Body.String())
	}

	// B cannot refresh/reprice/finalize/version A's estimate.
	refreshResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/refresh", tokenB,
		map[string]any{"expectedRevision": 0})
	if refreshResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant refresh, got %d", refreshResp.Code)
	}

	pricingResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", tokenB,
		map[string]any{"pricingMode": "margin", "pricingRate": 1000, "expectedRevision": 0})
	if pricingResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant pricing update, got %d", pricingResp.Code)
	}

	finalizeRespB := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", tokenB,
		map[string]any{"expectedRevision": 0})
	if finalizeRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant finalize, got %d", finalizeRespB.Code)
	}

	versionRespB := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/versions", tokenB, map[string]any{})
	if versionRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant new-version creation, got %d", versionRespB.Code)
	}

	// B cannot create an estimate under A's project.
	createRespB := doJSON(t, router, http.MethodPost, "/estimates", tokenB, createBody)
	if createRespB.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant estimate creation, got %d", createRespB.Code)
	}
}

func TestEstimateSnapshotImmutabilityAndLifecycle(t *testing.T) {
	router := setupRouter(t)
	token, _ := registerCompany(t, router, "estimates-lifecycle@example.com", "Estimates Lifecycle Co")

	projectID, costItemID := buildProjectWithEstimatedCostItem(t, router, token)

	// Step 1-4: create V1, record its snapshot.
	createResp := doJSON(t, router, http.MethodPost, "/estimates", token,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	v1ID := mustField(t, createResp, "id")
	v1CostSubtotalBefore := mustField(t, createResp, "costSubtotal.amount")

	// Step 5: change the underlying CostItem's Estimated.
	lifecycleResp := doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", token,
		map[string]any{"stage": "estimated", "amount": 999999})
	if lifecycleResp.Code != http.StatusOK {
		t.Fatalf("expected cost item lifecycle update to succeed, got %d: %s", lifecycleResp.Code, lifecycleResp.Body.String())
	}

	// Step 6-7: GET V1 -> unchanged.
	getV1Resp := doJSON(t, router, http.MethodGet, "/estimates/"+v1ID, token, nil)
	if mustField(t, getV1Resp, "costSubtotal.amount") != v1CostSubtotalBefore {
		t.Fatalf("expected V1's CostSubtotal unchanged by a mere GET after the CostItem change")
	}

	// Step 8-9: refresh V1 -> picks up the change, same Version, Revision incremented.
	refreshResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/refresh", token,
		map[string]any{"expectedRevision": 0})
	if refreshResp.Code != http.StatusOK {
		t.Fatalf("expected refresh to succeed, got %d: %s", refreshResp.Code, refreshResp.Body.String())
	}
	if mustField(t, refreshResp, "version") != float64(1) {
		t.Fatalf("expected Version to remain 1 after refresh")
	}
	if mustField(t, refreshResp, "revision") != float64(1) {
		t.Fatalf("expected Revision incremented to 1 after refresh")
	}
	if mustField(t, refreshResp, "costSubtotal.amount") == v1CostSubtotalBefore {
		t.Fatal("expected CostSubtotal to reflect the updated CostItem.Estimated after refresh")
	}

	// Step 10: finalize V1 at the post-refresh revision.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/finalize", token,
		map[string]any{"expectedRevision": 1})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	if mustField(t, finalizeResp, "status") != "finalized" {
		t.Fatal("expected status=finalized")
	}

	// Step 11-12: change the CostItem again, attempt refresh on the now-finalized V1 -> rejected.
	doJSON(t, router, http.MethodPatch, "/cost-items/"+costItemID+"/lifecycle", token,
		map[string]any{"stage": "estimated", "amount": 555555})
	refreshFinalizedResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/refresh", token,
		map[string]any{"expectedRevision": 1})
	if refreshFinalizedResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 refreshing a finalized estimate, got %d", refreshFinalizedResp.Code)
	}

	// Step 14: create V2 -> succeeds since V1 is finalized, reflects the latest cost change.
	versionResp := doJSON(t, router, http.MethodPost, "/estimates/"+v1ID+"/versions", token, map[string]any{})
	if versionResp.Code != http.StatusOK && versionResp.Code != http.StatusCreated {
		t.Fatalf("expected new-version creation to succeed from a finalized source, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
	v2ID := mustField(t, versionResp, "id")
	if mustField(t, versionResp, "version") != float64(2) {
		t.Fatal("expected Version=2")
	}
	if mustField(t, versionResp, "revision") != float64(0) {
		t.Fatal("expected new version to start at Revision=0")
	}

	// Step 16: V1 remains unchanged.
	getV1AgainResp := doJSON(t, router, http.MethodGet, "/estimates/"+v1ID, token, nil)
	if mustField(t, getV1AgainResp, "status") != "finalized" {
		t.Fatal("expected V1 to remain finalized")
	}

	// Step 17: attempting a new version from V2 (still a draft) is rejected.
	versionFromDraftResp := doJSON(t, router, http.MethodPost, "/estimates/"+v2ID+"/versions", token, map[string]any{})
	if versionFromDraftResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 creating a new version from a draft source, got %d", versionFromDraftResp.Code)
	}
}

func TestEstimateFinalizeStaleRevisionRejected(t *testing.T) {
	router := setupRouter(t)
	token, _ := registerCompany(t, router, "estimates-stale-revision@example.com", "Estimates Stale Revision Co")

	projectID, _ := buildProjectWithEstimatedCostItem(t, router, token)

	createResp := doJSON(t, router, http.MethodPost, "/estimates", token,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	estimateID := mustField(t, createResp, "id")

	// Advance the draft's Revision via a pricing recalculation.
	repriceResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", token,
		map[string]any{"pricingMode": "margin", "pricingRate": 1000, "expectedRevision": 0})
	if repriceResp.Code != http.StatusOK {
		t.Fatalf("expected pricing recalculation to succeed, got %d: %s", repriceResp.Code, repriceResp.Body.String())
	}

	// Attempt to finalize at the STALE revision 0 -- must be rejected.
	staleFinalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", token,
		map[string]any{"expectedRevision": 0})
	if staleFinalizeResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 finalizing at a stale revision, got %d: %s", staleFinalizeResp.Code, staleFinalizeResp.Body.String())
	}

	// Retry at the CURRENT revision (1) -- succeeds.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", token,
		map[string]any{"expectedRevision": 1})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed at the current revision, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	// Retry finalize again (idempotent) -- succeeds regardless of the
	// expectedRevision supplied.
	retryFinalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", token,
		map[string]any{"expectedRevision": 999})
	if retryFinalizeResp.Code != http.StatusOK {
		t.Fatalf("expected idempotent finalize retry to succeed regardless of expectedRevision, got %d", retryFinalizeResp.Code)
	}
}

func TestEstimateMixedCurrencyRejected(t *testing.T) {
	router := setupRouter(t)
	token, _ := registerCompany(t, router, "estimates-mixed-currency@example.com", "Estimates Mixed Currency Co")

	projectID := buildProjectOnly(t, router, token)

	createCostItemMYR := map[string]any{
		"projectId": projectID, "category": "material", "description": "MYR cost",
		"estimatedAmount": 50000, "currency": "MYR",
	}
	doJSON(t, router, http.MethodPost, "/cost-items", token, createCostItemMYR)

	createCostItemSGD := map[string]any{
		"projectId": projectID, "category": "material", "description": "SGD cost",
		"estimatedAmount": 30000, "currency": "SGD",
	}
	doJSON(t, router, http.MethodPost, "/cost-items", token, createCostItemSGD)

	createEstimateResp := doJSON(t, router, http.MethodPost, "/estimates", token,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if createEstimateResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for mixed-currency estimate creation, got %d: %s", createEstimateResp.Code, createEstimateResp.Body.String())
	}
}

func TestEstimateInvalidPricingModeRejectedEverywhere(t *testing.T) {
	// BLOCKING fix (second plan review, issue #5): an invalid pricingMode
	// string must be rejected with 422 through EVERY endpoint that accepts
	// one — POST /estimates, PATCH /estimates/{id}/pricing, and the
	// optional override on POST /estimates/{id}/versions — not just proven
	// at the pure-function level (Task 4).
	router := setupRouter(t)
	token, _ := registerCompany(t, router, "estimates-invalid-mode@example.com", "Estimates Invalid Mode Co")

	projectID, _ := buildProjectWithEstimatedCostItem(t, router, token)

	// 1. POST /estimates with an invalid pricingMode.
	createResp := doJSON(t, router, http.MethodPost, "/estimates", token,
		map[string]any{"projectId": projectID, "pricingMode": "banana", "pricingRate": 2000})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode on create, got %d: %s", createResp.Code, createResp.Body.String())
	}

	// Create a valid estimate to exercise the other two endpoints against.
	validCreateResp := doJSON(t, router, http.MethodPost, "/estimates", token,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if validCreateResp.Code != http.StatusOK && validCreateResp.Code != http.StatusCreated {
		t.Fatalf("expected valid estimate creation to succeed, got %d: %s", validCreateResp.Code, validCreateResp.Body.String())
	}
	estimateID := mustField(t, validCreateResp, "id")

	// 2. PATCH /estimates/{id}/pricing with an invalid pricingMode.
	pricingResp := doJSON(t, router, http.MethodPatch, "/estimates/"+estimateID+"/pricing", token,
		map[string]any{"pricingMode": "banana", "pricingRate": 2000, "expectedRevision": 0})
	if pricingResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode on pricing recalculation, got %d: %s", pricingResp.Code, pricingResp.Body.String())
	}

	// 3. POST /estimates/{id}/versions with an invalid pricingMode override
	// — first finalize, since CreateNewVersion requires a finalized source.
	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/finalize", token,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected finalize to succeed, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	invalidModeStr := "banana"
	versionResp := doJSON(t, router, http.MethodPost, "/estimates/"+estimateID+"/versions", token,
		map[string]any{"pricingMode": &invalidModeStr})
	if versionResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid pricingMode override on new-version creation, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
}
```

- [ ] **Step 3: Add the two shared test helpers this file needs**

If `buildProjectWithEstimatedCostItem` and `buildProjectOnly` don't already exist in `backend/internal/tenanttest/tenant_isolation_test.go` (check first — an M3-era helper like `buildFullHierarchyForCompanyA` may already cover most of this and can be adapted/reused instead of adding new ones), add:

```go
// buildProjectWithEstimatedCostItem creates a Client -> Project -> CostItem
// (with an Estimated amount) chain for token's company, returning the
// Project ID and the CostItem ID.
func buildProjectWithEstimatedCostItem(t *testing.T, router http.Handler, token string) (projectID, costItemID string) {
	t.Helper()
	projectID = buildProjectOnly(t, router, token)

	createCostItemResp := doJSON(t, router, http.MethodPost, "/cost-items", token, map[string]any{
		"projectId": projectID, "category": "material", "description": "Ceramic tiles",
		"estimatedAmount": 500000, "currency": "MYR",
	})
	if createCostItemResp.Code != http.StatusOK && createCostItemResp.Code != http.StatusCreated {
		t.Fatalf("expected cost item creation to succeed, got %d: %s", createCostItemResp.Code, createCostItemResp.Body.String())
	}
	costItemID = mustField(t, createCostItemResp, "id")
	return projectID, costItemID
}

// buildProjectOnly creates a Client -> Project chain for token's company,
// returning the Project ID.
func buildProjectOnly(t *testing.T, router http.Handler, token string) string {
	t.Helper()
	createClientResp := doJSON(t, router, http.MethodPost, "/clients", token, map[string]any{"name": "Estimates Test Client"})
	if createClientResp.Code != http.StatusOK && createClientResp.Code != http.StatusCreated {
		t.Fatalf("expected client creation to succeed, got %d: %s", createClientResp.Code, createClientResp.Body.String())
	}
	clientID := mustField(t, createClientResp, "id")

	createProjectResp := doJSON(t, router, http.MethodPost, "/projects", token, map[string]any{"clientId": clientID, "name": "Estimates Test Project"})
	if createProjectResp.Code != http.StatusOK && createProjectResp.Code != http.StatusCreated {
		t.Fatalf("expected project creation to succeed, got %d: %s", createProjectResp.Code, createProjectResp.Body.String())
	}
	return mustField(t, createProjectResp, "id")
}
```

**Important:** before adding these, actually check whether equivalent helpers already exist in the file with different names/signatures (the M3 plan's Task 18 references `buildFullHierarchyForCompanyA` — if that or something similar already builds a Client→Project chain, reuse it directly rather than adding a duplicate). Adjust the new tests above to call whatever the real existing helper is named.

- [ ] **Step 4: Run the new tests to verify they fail first (if not already reusing working helpers), then pass**

Run: `cd backend && go test ./internal/tenanttest/... -run "TestEstimates|TestEstimate" -v`
Expected: PASS once handlers (Task 8) and wiring (Task 9) are in place — if this task is executed before Task 9's wiring, these will fail with 404s on every route; confirm Task 9 is complete first.

- [ ] **Step 5: Run the full acceptance suite (no regressions)**

Run: `cd backend && go test ./internal/tenanttest/... -v`
Expected: PASS — all M0-M3 tenant-isolation tests plus the new M4 ones.

- [ ] **Step 6: Leave changes uncommitted**

Per standing project instruction, do not stage or commit `backend/internal/tenanttest/tenant_isolation_test.go`. Leave it as a plain working-tree modification and move to the next task.

---

## Task 12: Final verification sweep

**Spec:** §29 (project-wide TDD/verification requirements). Confirms the entire backend — M0 through M4 — is green with no regressions.

**Files:** none (verification only)

- [ ] **Step 1: Full build**

Run: `cd backend && go build ./...`
Expected: clean, exit 0

- [ ] **Step 2: Vet**

Run: `cd backend && go vet ./...`
Expected: clean, exit 0

- [ ] **Step 3: Full test suite**

Run: `cd backend && go test ./...`
Expected: all packages PASS, no regressions in any M0/M1/M2/M3 package

- [ ] **Step 4: `go mod tidy` stability check**

This project has no `.git` directory, so `git diff go.sum` is not available — use a before/after hash comparison instead (matching the M3 plan's own established "2 identical runs" verification, adapted to a hash rather than a git diff):

Run (PowerShell, from `backend/`):
```powershell
$before = (Get-FileHash go.sum -Algorithm SHA256).Hash
go mod tidy
$after = (Get-FileHash go.sum -Algorithm SHA256).Hash
if ($before -eq $after) { "STABLE: go.sum unchanged" } else { "CHANGED: go.sum hash differs before/after go mod tidy" }
```
Expected: `STABLE: go.sum unchanged` — no new dependency is expected, since Task 0-11 only use packages already imported elsewhere in the module (mongo-driver, shopspring/decimal, huma, testcontainers-go — all already dependencies before this milestone).

- [ ] **Step 5: Confirm no `.git` directory exists anywhere**

Run: `find /c/Users/shananth/mvp -maxdepth 2 -name ".git"`
Expected: no output (this project has no `.git` directory by design — do not create one)

- [ ] **Step 6: Live E2E smoke test (optional but recommended, matching M3's precedent)**

Start Docker Compose if not already running, run `go run ./cmd/api` from `backend/`, and manually exercise via curl or the served `/docs`:
1. Register a company, get a bearer token.
2. Create a Client → Project.
3. Create a CostItem with an Estimated amount.
4. `POST /estimates` — confirm a Version 1 draft is returned with the correct CostSubtotal/ProposedSellingPrice.
5. `PATCH /estimates/{id}/pricing` — confirm Revision increments and pricing recalculates.
6. `POST /estimates/{id}/refresh` — confirm it succeeds (no cost change yet, so Lines are unchanged but Revision still increments).
7. `POST /estimates/{id}/finalize` — confirm Status becomes finalized.
8. `POST /estimates/{id}/versions` — confirm Version 2 is created.
9. Confirm `/openapi.json` lists all 8 new M4 operations, each declaring `bearerAuth` security.
10. Stop the server cleanly afterward (per the standing environment-hygiene note in `handoff.md`).

- [ ] **Step 7: Update `tasks/done.md`**

Append a "Milestone 4" entry to `backend/../tasks/done.md` (repo root `tasks/done.md`) summarizing: modules built, design spec/plan locations, test counts, verification results, and any deviations from this plan discovered during implementation — matching the exact format of the existing "Milestone 3" entry.

- [ ] **Step 8: Do not commit**

Per standing project instruction (see memory: no `git add`/`git commit` steps run automatically), leave all Milestone 4 changes as uncommitted working-tree modifications. Committing is a separate, explicit future request.

---

## Self-review notes (spec coverage check)

- §1 (domain model) → Task 2. §2 (collection ownership) → Task 3. §3 (source of truth) → Tasks 5-7 collectively. §4 (versioning/numbering) → Tasks 5, 7, 10. §5 (snapshot granularity) → Task 2/4. §6 (cost source/capability) → Task 1. §7/§7.1/§7.2 (missing-estimated, zero-subtotal) → Task 4. §8 (mixed currency, including re-inference and persistence on refresh) → Task 4/6/10. §9 (persisted vs derived) → Task 2/3 (all persisted fields, including `Currency` on refresh). §10 (lifecycle, incl. Revision-frozen-not-incremented on finalize) → Task 3/7. §11 (operation summary) → Tasks 6-7. §12 (dependency graph) → Task 9. §13 (markup/margin, incl. invalid-mode rejection) → Task 4. §14 (contingency removed) → confirmed absent from Task 2's model (no Contingency fields anywhere in this plan). §15 (rounding order) → Task 4. §16.1-16.3 (refresh/pricing/finalize, incl. Revision-freeze on finalize) → Tasks 3, 6-7. §17/§17.1 (new version REQUIRES finalized source, no retry into V2+ from `CreateEstimate`, manual override) → Task 5 (`ErrEstimateAlreadyExistsForProject`, no-retry proven directly) and Task 7 (`ErrEstimateMustBeFinalizedBeforeNewVersion`, retry via `allocateAndCreate` proven directly); manual override requires no new estimates code per the spec's own resolution — confirmed no task attempts to build a line-override endpoint. §18 (money helpers, 3 not 2) → Task 0. §19 (tenant ownership) → Tasks 5-8 throughout. §20 (Mongo representation/indexes, incl. explicit distinct index names and index-name-based error classification) → Task 3. §21 (concurrency: version-number bounded retry restricted to `CreateNewVersion` only, draft-collision distinction, unclassified-duplicate-key non-retry, all in PRODUCTION code) → Tasks 3, 5, 7, 10. §22 (historical integrity) → proven by Task 11's acceptance test. §23 (endpoints — 8 total) → Task 8. §24 (parent validation) → Task 5/8. §25 (service responsibilities) → Tasks 5-7. §26 (unit tests, incl. invalid-pricing-mode, no-retry-in-CreateEstimate, and retry-in-CreateNewVersion tests) → Tasks 4-7's test steps. §27 (integration tests, incl. currency-persists-on-refresh, finalize-does-not-increment-revision, and distinct-named-index collisions) → Tasks 3, 10. §28 (tenant isolation) → Task 11. §29 (snapshot immutability) → Task 11. §30 (mixed currency acceptance) → Task 11. §31 (M5 boundary) → no task; explicitly out of scope, nothing to build. §32 (review log) → informational, no corresponding task needed.
- Placeholder scan: no "TBD"/"TODO" strings; every step has literal, complete code. No `git add`/`git commit` steps remain anywhere in the plan (removed per the second review round) — every "Commit" step is "Leave changes uncommitted," consistent with standing project instruction and applied uniformly across all 12 tasks, not just Task 0.
- Type consistency: `EstimatedCostSource.VisitEstimatedCostItems` signature matches exactly between Task 1 (costs.Service), Task 5 (estimates.Service field), and all fakes across Tasks 5-7. `PricingMode`/`money.RateBPS` used consistently as parameter types across Tasks 4-8. `ErrEstimateNotDraft` vs `ErrRevisionMismatch` vs `ErrEstimateMustBeFinalizedBeforeNewVersion` vs `ErrEstimateAlreadyExistsForProject` vs `ErrVersionConflict` vs `ErrDraftAlreadyExists` vs `ErrUnclassifiedDuplicateKey` vs `ErrInvalidPricingMode` are eight distinct, consistently-named sentinels used identically in the repository (Task 3), service (Tasks 5-7), handler error mapping (Task 8), and acceptance tests (Task 11). `conditionalUpdate`'s `incrementRevision bool` parameter is used consistently: `true` from `ReplaceSnapshot`/`UpdatePricing` (Task 3), `false` from `Finalize` (Task 3) — matching the approved design spec §16.3's explicit "finalize freezes Revision" invariant, proven by a dedicated repository test (`TestEstimateRepositoryFinalize`, Task 3) and a service-level test (`TestFinalizeEstimateHappyPath`'s revision assertions, Task 7). `indexNameCompanyProject`/`indexNameUniqueCompanyProjectVersion`/`indexNameUniqueOneDraftPerProject` are three mutually non-overlapping explicit index-name constants (Task 3), each set via `SetName` at index-creation time and compared exactly (not by substring against each other) in `classifyCreateError` — eliminating both the default-name collision and the substring-misclassification bug the third review round found.

### Third review round — corrections applied (post second-round approval)

A third review pass, against the corrected plan itself (after the second
round's fixes were already applied), found and fixed 2 further real bugs
before implementation began:

1. **Mongo index name collision + duplicate-key misclassification.** The
   plain (non-unique) `{companyId, projectId}` index and the partial
   unique `{companyId, projectId}` (draft-only) index would have derived
   the SAME MongoDB default auto-generated name
   (`companyId_1_projectId_1`) — since default naming does not account for
   a `partialFilterExpression` — meaning `EnsureIndexes` would fail
   outright at index-creation time with a name collision, before the
   application ever ran. Separately, even the *intended* classification
   logic was broken: `companyId_1_projectId_1` (the draft-index name) is a
   literal substring of `companyId_1_projectId_1_version_1` (the
   version-index name), so a substring check would have matched every
   version-conflict error against the draft-index case first,
   misclassifying every `ErrVersionConflict` as `ErrDraftAlreadyExists`.
   Fixed by giving all four indexes explicit, mutually non-overlapping
   names via `SetName` (`idx_estimates_company_project`,
   `uq_estimates_company_project_version`,
   `uq_estimates_one_draft_per_project`) and comparing `classifyCreateError`
   against those exact constants — no substring relationship exists
   between any two of them. Also added `ErrUnclassifiedDuplicateKey`: a
   duplicate-key error that doesn't match either known index name is no
   longer defaulted to the retriable `ErrVersionConflict` (which risked
   silently retrying a genuinely unknown failure mode) — it is now a
   distinct, explicitly non-retried sentinel, mapped to a 500 at the HTTP
   layer like any other unrecognized internal error. See Task 3.

2. **`CreateEstimate` could still theoretically create Version 2+ under a
   race, even after the second round's fix.** The second round added an
   up-front `existingMax > 0` check, but then delegated the actual write
   to the shared `allocateAndCreate` helper — which recalculates
   `MAX(version)+1` on every retry attempt. Two concurrent requests could
   both pass the up-front check at `MAX(version) = 0`, one creates and
   finalizes Version 1, and the other's retry-driven re-read inside
   `allocateAndCreate` then sees `MAX(version) = 1` and succeeds in
   creating Version 2 — a silent bypass of "`POST /estimates` creates
   ONLY Version 1," and, transitively, of `CreateNewVersion`'s
   finalized-source precondition (since a stray Version 2 could exist
   without ever going through it). Fixed by removing `allocateAndCreate`
   entirely from `CreateEstimate`'s call path: it now attempts
   `Version: 1` directly, exactly once, and translates ANY collision
   (`ErrVersionConflict` OR `ErrDraftAlreadyExists`) into
   `ErrEstimateAlreadyExistsForProject` with no retry. `allocateAndCreate`
   remains — correctly — the sole allocation strategy for
   `CreateNewVersion`, where retrying into the next version number is the
   intended behavior. New tests
   (`TestCreateEstimateDoesNotRetryOnVersionConflict`,
   `TestCreateEstimateDoesNotRetryOnDraftAlreadyExists`) prove
   `CreateEstimate` makes exactly one `Create` attempt regardless of which
   collision occurs; a new test
   (`TestCreateNewVersionRetriesOnVersionConflict`) proves
   `CreateNewVersion` still correctly retries via the shared helper. See
   Tasks 5 and 7.
