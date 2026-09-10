# Milestone 5 — Quotations Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **This project uses INLINE execution, not subagents** (standing instruction, confirmed for M2/M3/M4, applies here too — see `tasks/lessons.md` / `handoff.md`). Execute task-by-task in this session, red-then-green, per step.

**Goal:** Implement `internal/quotations` — the internal-authenticated, customer-facing commercial Quotation generated from a finalized Estimate — per the APPROVED design spec `docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md` (3 review rounds, all decisions resolved).

**Architecture:** New `internal/quotations` module (model/repository/service/handler, mirroring M4's `internal/estimates` file shape exactly) plus one new method each on `internal/estimates` (`VisitQuotationSeeds` — performs cost-grouping and proportional allocation *inside* `estimates`, never in `quotations`, per the design's privacy-firewall decision), `internal/projects` (`GetProjectClientID`), and `internal/work` (`GetWorkItemDescription`), plus two new pure helpers in `internal/foundation/money` (`AllocateProportionally`, `ApplyRateBPS`). Dependency graph: `projects`/`work`/`costs` (unchanged) → `estimates` (extended) → `quotations` (new) — acyclic, matching ADR 0002.

**Tech Stack:** Go 1.25, chi + Huma v2, mongo-driver v2, shopspring/decimal, Testcontainers-Go. No new external dependencies.

## Global Constraints

- **Money/quantities are never floats.** Money is `int64` minor units + currency; quantities use `shopspring/decimal`. All rounding goes through `internal/foundation/money`'s centralized helpers only — never reimplement rounding in `quotations` or `estimates`. (ADR 0001, project CLAUDE.md)
- **`internal/quotations` must never import `internal/estimates`, `internal/work`, `internal/projects`, or `internal/clients` by type** (ADR 0002). It defines its own narrow capability interfaces (consumer-defines-interface), satisfied structurally.
- **No cost figure, category, or internal margin/profit value may ever cross from `internal/estimates` into `internal/quotations`** — enforced by capability-interface return-shape, not by convention (design spec §4). `VisitQuotationSeeds`'s visit callback signature is `func(workItemID *string, allocatedSellingAmount money.Money) error` — nothing else.
- **All M5 endpoints are internal authenticated contractor APIs**, registered on the existing single `authedAPI` `huma.Group` in `cmd/api/main.go` / `internal/tenanttest/router.go` — never construct a second `huma.API`/`humachi.New(...)`.
- **Every MongoDB index must carry an explicit, mutually non-overlapping `SetName`** — no relying on MongoDB's default auto-generated index names (established M4 lesson).
- **Error-sentinel style**: package-level `var ErrXxx = errors.New(...)`. Service/repository/unit tests compare via plain `==`/`!=` against exported sentinels (M4 convention, confirmed — NOT `errors.Is` at that layer). Handler-layer `mapQuotationsError` uses `errors.Is` (M4 convention). No testify anywhere in this codebase — plain stdlib `t.Fatalf`.
- **No git commands** — this project has no `.git` directory; never run `git init`/`add`/`commit`. Leave changes uncommitted throughout (standing instruction).
- **Never mark a task done without running its tests and observing the actual pass/fail output.**
- **This is a large multi-task plan. After every 2-3 tasks, stop and report progress** (tests green, files created) before continuing, so the human can course-correct early rather than after the whole plan is built.

---

## File Structure

```
backend/internal/foundation/money/
  rounding.go                    MODIFY — add AllocateProportionally, ApplyRateBPS, 4 new sentinel errors
  rounding_test.go                MODIFY — add tests for both new helpers

backend/internal/projects/
  service.go                      MODIFY — add GetProjectClientID

backend/internal/work/
  service.go                      MODIFY — add GetWorkItemDescription
  work_item.go                    (no change — Description field already exists)

backend/internal/estimates/
  service.go                      MODIFY — add VisitQuotationSeeds (calls money.AllocateProportionally internally)
  service_test.go                 MODIFY — add VisitQuotationSeeds unit tests with fakes

backend/internal/quotations/
  doc.go                          MODIFY — replace M0 placeholder doc comment
  quotation.go                    CREATE — Quotation, QuotationLine, QuotationStatus, TaxMode domain structs
  repository.go                   CREATE — QuotationRepository interface + sentinel errors
  repository_mongo.go             CREATE — Mongo impl, quotations + quotation_counters collections, indexes
  repository_mongo_test.go        CREATE — Testcontainers integration tests
  service.go                      CREATE — Service, capability interfaces (ProjectLookup, WorkItemLookup, FinalizedEstimateSource), business logic
  service_test.go                 CREATE — unit tests against fakes
  handler.go                      CREATE — Huma HTTP handlers, DTOs, error mapping
  allocation_test.go               (allocation logic lives in estimates, not here — see estimates/service_test.go)

backend/internal/tenanttest/
  router.go                       MODIFY — wire quotations module into the composition root
  tenant_isolation_test.go        MODIFY — add M5 acceptance tests

backend/cmd/api/
  main.go                         MODIFY — wire quotations module into the composition root (mirrors tenanttest/router.go)
```

---

## Task 0: `money.AllocateProportionally` — largest-remainder proportional allocation

**Files:**
- Modify: `backend/internal/foundation/money/rounding.go`
- Test: `backend/internal/foundation/money/rounding_test.go`

**Interfaces:**
- Produces (used by Task 6, `estimates.VisitQuotationSeeds`):
  ```go
  var ErrNonPositiveAllocationTotal = errors.New("money: allocation total must be strictly positive")
  var ErrNoAllocationWeights = errors.New("money: at least one weight is required")
  var ErrInvalidAllocationWeight = errors.New("money: every weight must be strictly positive")
  var ErrZeroAllocationShare = errors.New("money: allocation would produce a zero share for at least one weight")

  func AllocateProportionally(total Money, weights []int64) ([]Money, error)
  ```

Design spec reference: §6.2, §19 (all validation rules and rationale below are taken verbatim from there).

- [ ] **Step 1: Write the failing tests**

Read the existing file first to confirm current contents and exact style to match:

Read `backend/internal/foundation/money/rounding_test.go` in full (already read this session — package `money`, white-box, plain `t.Fatalf` assertions, `decimal.NewFromString` for parsing).

Add to `backend/internal/foundation/money/rounding_test.go` (append at end of file):

```go
func TestAllocateProportionallyEqualWeights(t *testing.T) {
	total := New(300, "MYR")
	shares, err := AllocateProportionally(total, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shares) != 3 {
		t.Fatalf("expected 3 shares, got %d", len(shares))
	}
	sum := int64(0)
	for _, s := range shares {
		if s.Currency != "MYR" {
			t.Fatalf("expected every share to carry total's currency MYR, got %s", s.Currency)
		}
		if s.Amount <= 0 {
			t.Fatalf("expected every share to be strictly positive, got %d", s.Amount)
		}
		sum += s.Amount
	}
	if sum != 300 {
		t.Fatalf("expected shares to sum to exactly 300, got %d", sum)
	}
}

func TestAllocateProportionallySkewedWeights(t *testing.T) {
	// A RM625.00 total split 1000:1 (WorkItem A) : 200 (WorkItem B) —
	// mirrors design spec §6.2's worked example shape.
	total := New(62500, "MYR")
	shares, err := AllocateProportionally(total, []int64{1000, 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := int64(0)
	for _, s := range shares {
		sum += s.Amount
	}
	if sum != 62500 {
		t.Fatalf("expected shares to sum to exactly 62500, got %d", sum)
	}
	// Larger weight must receive the larger share.
	if shares[0].Amount <= shares[1].Amount {
		t.Fatalf("expected shares[0] (weight 1000) > shares[1] (weight 200), got %d and %d", shares[0].Amount, shares[1].Amount)
	}
}

func TestAllocateProportionallySingleWeight(t *testing.T) {
	total := New(999, "MYR")
	shares, err := AllocateProportionally(total, []int64{1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shares) != 1 || shares[0].Amount != 999 {
		t.Fatalf("expected the single weight to receive the entire total, got %+v", shares)
	}
}

func TestAllocateProportionallyResidualNotEvenlyDivisible(t *testing.T) {
	// 100 minor units across 3 equal weights -> 33/33/33 truncated, 1 unit
	// residual distributed by largest-remainder to exactly one share.
	total := New(100, "MYR")
	shares, err := AllocateProportionally(total, []int64{1, 1, 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sum := int64(0)
	for _, s := range shares {
		sum += s.Amount
	}
	if sum != 100 {
		t.Fatalf("expected shares to sum to exactly 100 (no residual dropped or duplicated), got %d", sum)
	}
}

func TestAllocateProportionallyDeterministicTieBreak(t *testing.T) {
	// Running the same inputs twice must produce identical outputs — no
	// dependency on map iteration order (design spec §6.2 Step 3).
	total := New(100, "MYR")
	weights := []int64{1, 1, 1}
	first, err := AllocateProportionally(total, weights)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := AllocateProportionally(total, weights)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := range first {
		if first[i].Amount != second[i].Amount {
			t.Fatalf("expected deterministic output across repeated calls, got %v then %v", first, second)
		}
	}
}

func TestAllocateProportionallyRejectsNonPositiveTotal(t *testing.T) {
	_, err := AllocateProportionally(New(0, "MYR"), []int64{1, 1})
	if err != ErrNonPositiveAllocationTotal {
		t.Fatalf("expected ErrNonPositiveAllocationTotal for zero total, got %v", err)
	}
	_, err = AllocateProportionally(New(-100, "MYR"), []int64{1, 1})
	if err != ErrNonPositiveAllocationTotal {
		t.Fatalf("expected ErrNonPositiveAllocationTotal for negative total, got %v", err)
	}
}

func TestAllocateProportionallyRejectsEmptyWeights(t *testing.T) {
	_, err := AllocateProportionally(New(100, "MYR"), nil)
	if err != ErrNoAllocationWeights {
		t.Fatalf("expected ErrNoAllocationWeights for nil weights, got %v", err)
	}
	_, err = AllocateProportionally(New(100, "MYR"), []int64{})
	if err != ErrNoAllocationWeights {
		t.Fatalf("expected ErrNoAllocationWeights for empty weights, got %v", err)
	}
}

func TestAllocateProportionallyRejectsNonPositiveWeight(t *testing.T) {
	// A positive overall total does NOT imply every individual weight is
	// positive — design spec §6.2's corrected worked example
	// (WorkItem A: RM1,000, WorkItem B: -RM200, CostSubtotal: RM800).
	_, err := AllocateProportionally(New(800, "MYR"), []int64{1000, -200})
	if err != ErrInvalidAllocationWeight {
		t.Fatalf("expected ErrInvalidAllocationWeight for a negative weight, got %v", err)
	}
	// A zero weight among otherwise-positive weights must also be rejected
	// — the original design draft's own test matrix left this undefined;
	// the design spec's 2nd review round resolved it as rejection.
	_, err = AllocateProportionally(New(100, "MYR"), []int64{1, 0, 1})
	if err != ErrInvalidAllocationWeight {
		t.Fatalf("expected ErrInvalidAllocationWeight for a zero weight, got %v", err)
	}
}

func TestAllocateProportionallyRejectsZeroShare(t *testing.T) {
	// 10 equal weights against a 3-minor-unit total: at most 3 shares can
	// receive 1 minor unit each; the other 7 truncate to zero and can
	// never win a residual unit. The whole call must fail, not return a
	// result set containing a zero share (design spec §6.2 post-allocation
	// validation, §19 ErrZeroAllocationShare).
	weights := make([]int64, 10)
	for i := range weights {
		weights[i] = 1
	}
	_, err := AllocateProportionally(New(3, "MYR"), weights)
	if err != ErrZeroAllocationShare {
		t.Fatalf("expected ErrZeroAllocationShare when more weights than minor units exist, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/foundation/money/... -run TestAllocateProportionally -v`
Expected: FAIL with `undefined: AllocateProportionally` (and undefined sentinel errors) — compile error, confirming the function doesn't exist yet.

- [ ] **Step 3: Write the implementation**

Read `backend/internal/foundation/money/rounding.go` in full first (already read this session) to match its exact style (doc-comment format, `decimal` usage, `RoundToMinorUnits` delegation).

Append to `backend/internal/foundation/money/rounding.go`:

```go
import (
	"errors"

	"github.com/shopspring/decimal"
)
```

(Note: `rounding.go` currently imports only `"github.com/shopspring/decimal"` with no `errors` — add `"errors"` to the import block.)

```go
// ErrNonPositiveAllocationTotal is returned by AllocateProportionally when
// total.Amount <= 0.
var ErrNonPositiveAllocationTotal = errors.New("money: allocation total must be strictly positive")

// ErrNoAllocationWeights is returned by AllocateProportionally when weights
// is empty.
var ErrNoAllocationWeights = errors.New("money: at least one weight is required")

// ErrInvalidAllocationWeight is returned by AllocateProportionally when any
// individual weight is <= 0. A positive sum of weights does NOT imply every
// individual weight is positive (M5 design spec §6.2) — callers must not
// pass non-positive weights; this function rejects them explicitly rather
// than silently dropping or clamping them.
var ErrInvalidAllocationWeight = errors.New("money: every weight must be strictly positive")

// ErrZeroAllocationShare is returned by AllocateProportionally when the
// largest-remainder algorithm would produce a zero-minor-unit share for at
// least one weight even after residual distribution — possible when
// len(weights) is large relative to total's minor-unit count, or one
// weight is a vanishingly small fraction of the sum of weights (M5 design
// spec §6.2). The entire call fails; no partial/best-effort result with a
// zero share is ever returned.
var ErrZeroAllocationShare = errors.New("money: allocation would produce a zero share for at least one weight")

// AllocateProportionally splits total across len(weights) shares,
// proportional to each weight, using the largest-remainder method so the
// shares sum to EXACTLY total (never more, never less) — no residual is
// ever silently dropped or double-counted. total's Currency is copied onto
// every returned share. Ties in the remainder-ranking step are broken by
// weights' input index (deterministic, reproducible).
//
// Validates: total.Amount > 0, len(weights) > 0, every weights[i] > 0, and
// every resulting share > 0 — returning ErrNonPositiveAllocationTotal /
// ErrNoAllocationWeights / ErrInvalidAllocationWeight /
// ErrZeroAllocationShare respectively (M5 design spec §6.2/§19).
func AllocateProportionally(total Money, weights []int64) ([]Money, error) {
	if total.Amount <= 0 {
		return nil, ErrNonPositiveAllocationTotal
	}
	if len(weights) == 0 {
		return nil, ErrNoAllocationWeights
	}

	sumWeights := int64(0)
	for _, w := range weights {
		if w <= 0 {
			return nil, ErrInvalidAllocationWeight
		}
		sumWeights += w
	}

	totalMajorUnits := decimal.NewFromInt(total.Amount)
	sumWeightsDecimal := decimal.NewFromInt(sumWeights)

	rawShares := make([]decimal.Decimal, len(weights))
	truncatedShares := make([]int64, len(weights))
	remainders := make([]decimal.Decimal, len(weights))
	truncatedSum := int64(0)

	for i, w := range weights {
		raw := totalMajorUnits.Mul(decimal.NewFromInt(w)).Div(sumWeightsDecimal)
		rawShares[i] = raw
		truncated := raw.Floor()
		truncatedShares[i] = truncated.IntPart()
		remainders[i] = raw.Sub(truncated)
		truncatedSum += truncatedShares[i]
	}

	residual := total.Amount - truncatedSum

	// Distribute the residual one minor unit at a time to the shares with
	// the largest fractional remainder, breaking ties by input index.
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	// Simple stable selection sort by descending remainder, ascending
	// index on ties — len(weights) is always small (bounded by a single
	// Project's WorkItem count), so O(n^2) is not a concern here.
	for i := 0; i < len(order); i++ {
		maxIdx := i
		for j := i + 1; j < len(order); j++ {
			if remainders[order[j]].GreaterThan(remainders[order[maxIdx]]) {
				maxIdx = j
			}
		}
		order[i], order[maxIdx] = order[maxIdx], order[i]
	}

	finalShares := make([]int64, len(weights))
	copy(finalShares, truncatedShares)
	for i := int64(0); i < residual; i++ {
		finalShares[order[i]]++
	}

	result := make([]Money, len(weights))
	for i, amount := range finalShares {
		if amount <= 0 {
			return nil, ErrZeroAllocationShare
		}
		result[i] = Money{Amount: amount, Currency: total.Currency}
	}

	return result, nil
}

// ApplyRateBPS computes base × rate/10000, rounded once via
// RoundToMinorUnits — used for tax-amount calculation (M5 design spec §7).
// Distinct from AddRateBPS (which returns base PLUS the rate-computed
// delta, the markup formula): ApplyRateBPS returns ONLY the delta itself.
func ApplyRateBPS(base Money, rate RateBPS) Money {
	baseMajorUnits := decimal.NewFromInt(base.Amount).Shift(-2)
	rateFraction := decimal.NewFromInt(int64(rate)).Div(decimal.NewFromInt(BasisPointsDenominator))
	amountMajorUnits := baseMajorUnits.Mul(rateFraction)
	return Money{Amount: RoundToMinorUnits(amountMajorUnits), Currency: base.Currency}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/foundation/money/... -v`
Expected: PASS — all existing money tests plus all new `TestAllocateProportionally*` tests green.

- [ ] **Step 5: Add `ApplyRateBPS` unit test**

Append to `backend/internal/foundation/money/rounding_test.go`:

```go
func TestApplyRateBPS(t *testing.T) {
	base := New(10000, "MYR") // RM100
	result := ApplyRateBPS(base, 600)  // 6%
	if result.Amount != 600 {
		t.Fatalf("expected 600 (6%% of 10000), got %d", result.Amount)
	}
	if result.Currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", result.Currency)
	}
}

func TestApplyRateBPSMatchesAddRateBPSDelta(t *testing.T) {
	base := New(62500, "MYR")
	rate := RateBPS(600)
	delta := ApplyRateBPS(base, rate)
	marked := AddRateBPS(base, rate)
	expectedDelta := marked.Amount - base.Amount
	if delta.Amount != expectedDelta {
		t.Fatalf("expected ApplyRateBPS delta (%d) to equal AddRateBPS(base,rate) - base (%d)", delta.Amount, expectedDelta)
	}
}

func TestApplyRateBPSZeroRate(t *testing.T) {
	base := New(10000, "MYR")
	result := ApplyRateBPS(base, 0)
	if result.Amount != 0 {
		t.Fatalf("expected 0 for a zero rate, got %d", result.Amount)
	}
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && go test ./internal/foundation/money/... -v`
Expected: PASS — full package green.

- [ ] **Step 7: Leave changes uncommitted**

No `git add`/`git commit` — this project has no `.git` directory (standing instruction). Confirm the working tree state is otherwise clean by running `go vet ./internal/foundation/money/...` and expecting no output.

---

## Task 1: `projects.Service.GetProjectClientID`

**Files:**
- Modify: `backend/internal/projects/service.go`
- Test: `backend/internal/projects/service_test.go`

**Interfaces:**
- Consumes: `s.repo.FindByID(ctx, companyID, projectID) (Project, error)` — already exists (`ProjectRepository` interface, unchanged).
- Produces (used by Task 8, `quotations.ProjectLookup`):
  ```go
  func (s *Service) GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error)
  ```

Design spec reference: §18, §22.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/projects/service_test.go` (package `projects`, white-box, matching the file's existing style):

```go
func TestServiceGetProjectClientID(t *testing.T) {
	repo := newFakeProjectRepo()
	svc := NewService(repo, fakeClientLookup{belongs: true})

	created, err := svc.CreateProject(context.Background(), "company_a", "client_1", "Bathroom Reno")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	clientID, err := svc.GetProjectClientID(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clientID != "client_1" {
		t.Fatalf("expected clientID client_1, got %s", clientID)
	}
}

func TestServiceGetProjectClientIDNotFound(t *testing.T) {
	repo := newFakeProjectRepo()
	svc := NewService(repo, fakeClientLookup{belongs: true})

	_, err := svc.GetProjectClientID(context.Background(), "company_a", "nonexistent")
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestServiceGetProjectClientIDCrossTenant(t *testing.T) {
	repo := newFakeProjectRepo()
	svc := NewService(repo, fakeClientLookup{belongs: true})

	created, err := svc.CreateProject(context.Background(), "company_a", "client_1", "Bathroom Reno")
	if err != nil {
		t.Fatalf("unexpected error creating project: %v", err)
	}

	_, err = svc.GetProjectClientID(context.Background(), "company_b", created.ID)
	if err != ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound for a cross-tenant lookup, got %v", err)
	}
}
```

Check `backend/internal/projects/service_test.go` for the existing `fakeClientLookup` fake — if a fake satisfying `ClientLookup` under a different name already exists in the file, reuse that name instead of `fakeClientLookup` (read the file's full contents before writing this step to confirm the exact existing fake name and adjust these three tests accordingly before running them).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/projects/... -run TestServiceGetProjectClientID -v`
Expected: FAIL with `svc.GetProjectClientID undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `backend/internal/projects/service.go`, immediately after the existing `ProjectBelongsToCompany` method:

```go
// GetProjectClientID returns projectID's ClientID, tenant-scoped to
// companyID. Satisfies quotations.ProjectLookup structurally (M5 design
// spec §18/§22) — returns the bare ClientID string, never a Project
// struct (ADR 0002).
func (s *Service) GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error) {
	p, err := s.repo.FindByID(ctx, companyID, projectID)
	if err != nil {
		return "", err
	}
	return p.ClientID, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/projects/... -v`
Expected: PASS — full package green, no regressions.

- [ ] **Step 5: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 2: `work.Service.GetWorkItemDescription`

**Files:**
- Modify: `backend/internal/work/service.go`
- Test: `backend/internal/work/service_test.go`

**Interfaces:**
- Consumes: `s.repo.FindByID(ctx, companyID, id) (WorkItem, error)` — already exists.
- Produces (used by Task 8, `quotations.WorkItemLookup`):
  ```go
  func (s *Service) GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (description string, found bool, err error)
  ```

Design spec reference: §5.3, §13.1, §22 — note the signature takes **both** `companyID` and `projectID` (not company-only), and returns `found bool` separately from `err` (the design's corrected 2nd-review-round contract, so `quotations`, not `work`, decides what caller-facing error a "not found" maps to).

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/work/service_test.go` (package `work`, white-box, matching the file's existing style). First check what fakes for `ProjectLookup`/`SpaceLookup` already exist in the file (read it in full) and reuse their names — the sketch below assumes fakes named `fakeProjectLookup{belongs: true}` and `fakeSpaceLookup{belongs: true}` exist; adjust to match whatever is actually there:

```go
func TestServiceGetWorkItemDescription(t *testing.T) {
	repo := newFakeWorkItemRepo()
	svc := NewService(repo, fakeProjectLookup{belongs: true}, fakeSpaceLookup{belongs: true})

	created, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil,
		"Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	desc, found, err := svc.GetWorkItemDescription(context.Background(), "company_a", "project_1", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if desc != "Install ceramic floor tiles" {
		t.Fatalf("expected description 'Install ceramic floor tiles', got %q", desc)
	}
}

func TestServiceGetWorkItemDescriptionNotFound(t *testing.T) {
	repo := newFakeWorkItemRepo()
	svc := NewService(repo, fakeProjectLookup{belongs: true}, fakeSpaceLookup{belongs: true})

	_, found, err := svc.GetWorkItemDescription(context.Background(), "company_a", "project_1", "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a nonexistent work item")
	}
}

func TestServiceGetWorkItemDescriptionCrossProject(t *testing.T) {
	// A WorkItem belonging to the same Company but a DIFFERENT Project must
	// report found=false — company-only scoping is insufficient (design
	// spec §22's corrected WorkItemLookup rationale).
	repo := newFakeWorkItemRepo()
	svc := NewService(repo, fakeProjectLookup{belongs: true}, fakeSpaceLookup{belongs: true})

	created, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil,
		"Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	_, found, err := svc.GetWorkItemDescription(context.Background(), "company_a", "project_2", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a work item belonging to a different project")
	}
}

func TestServiceGetWorkItemDescriptionCrossTenant(t *testing.T) {
	repo := newFakeWorkItemRepo()
	svc := NewService(repo, fakeProjectLookup{belongs: true}, fakeSpaceLookup{belongs: true})

	created, err := svc.CreateWorkItem(context.Background(), "company_a", "project_1", nil,
		"Install ceramic floor tiles", "", "30", "m2")
	if err != nil {
		t.Fatalf("unexpected error creating work item: %v", err)
	}

	_, found, err := svc.GetWorkItemDescription(context.Background(), "company_b", "project_1", created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a cross-tenant lookup")
	}
}
```

Before running, read `backend/internal/work/service.go`'s `CreateWorkItem` signature in full (already read this session — takes `companyID, projectID string, spaceID *string, description, workType, quantityValue, quantityUnit string`) and adjust the test calls above to match exactly if there's any discrepancy.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/work/... -run TestServiceGetWorkItemDescription -v`
Expected: FAIL with `svc.GetWorkItemDescription undefined`.

- [ ] **Step 3: Write minimal implementation**

Add to `backend/internal/work/service.go`, near the existing `WorkItemBelongsToProject`/`WorkItemBelongsToCompany` methods:

```go
// GetWorkItemDescription returns workItemID's Description, requiring it to
// belong to BOTH companyID and projectID (a company-only check is
// insufficient here — quotations.WorkItemLookup uses this to validate
// contractor-submitted WorkItem IDs that may reference any WorkItem under
// the Company, not just ones the system itself generated; a cross-project
// reference under the same Company must still be rejected, M5 design spec
// §13.1/§22). found is a separate return value, not folded into err — the
// caller (quotations), not work, decides what caller-facing error a
// "not found" maps to.
func (s *Service) GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (string, bool, error) {
	belongs, err := s.repo.BelongsToProject(ctx, companyID, workItemID, projectID)
	if err != nil {
		return "", false, err
	}
	if !belongs {
		return "", false, nil
	}
	wi, err := s.repo.FindByID(ctx, companyID, workItemID)
	if err != nil {
		return "", false, err
	}
	return wi.Description, true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/work/... -v`
Expected: PASS — full package green, no regressions.

- [ ] **Step 5: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 3: `quotations.Quotation`/`QuotationLine` domain model

**Files:**
- Modify: `backend/internal/quotations/doc.go`
- Create: `backend/internal/quotations/quotation.go`
- Test: `backend/internal/quotations/quotation_test.go`

**Interfaces:**
- Produces (used by every later task in this module):
  ```go
  type QuotationStatus string
  const (
      QuotationStatusDraft     QuotationStatus = "draft"
      QuotationStatusFinalized QuotationStatus = "finalized"
  )
  func (s QuotationStatus) IsValid() bool

  type TaxMode string
  const (
      TaxModeNone       TaxMode = "none"
      TaxModePercentage TaxMode = "percentage"
  )
  func (m TaxMode) IsValid() bool

  type Quotation struct { ... }     // see design spec §1, exact field list below
  type QuotationLine struct { ... } // see design spec §1, exact field list below
  ```

Design spec reference: §1 (verbatim struct definitions), §9.1 (Version/Revision/SchemaVersion distinction).

- [ ] **Step 1: Replace the M0 placeholder doc comment**

Read `backend/internal/quotations/doc.go` (currently the M0 scaffold placeholder — confirmed contents: `// Package quotations owns the customer-facing Quotation: versioned, snapshotted selling-price documents generated from an Estimate, plus Client acceptance/rejection/change-request handling. See phase1.md §23-31. Implementation begins in Milestone 5.` followed by `package quotations`).

Replace `backend/internal/quotations/doc.go` entirely with:

```go
// Package quotations owns the customer-facing Quotation: a versioned,
// snapshotted selling-price document generated from a finalized Estimate.
// See phase1.md §23-31 and
// docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md.
//
// quotations never imports internal/estimates, internal/work,
// internal/projects, or internal/clients by type (ADR 0002). It defines
// ProjectLookup, WorkItemLookup, and FinalizedEstimateSource — the three
// capabilities it needs from those modules — satisfied structurally.
//
// Client-facing secure links, Access Grants, and Client accept/reject
// actions are Milestone 6, not built here — see design spec §24.
package quotations

- [ ] **Step 2: Create the domain model**

Create `backend/internal/quotations/quotation.go`:

```go
package quotations

import (
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// QuotationStatus is one of the 2 lifecycle states a Quotation version can
// be in (design spec §12). No sent/approved/rejected states in M5 — those
// are M6's Client-facing states.
type QuotationStatus string

const (
	QuotationStatusDraft     QuotationStatus = "draft"
	QuotationStatusFinalized QuotationStatus = "finalized"
)

// IsValid reports whether s is one of the 2 defined statuses.
func (s QuotationStatus) IsValid() bool {
	switch s {
	case QuotationStatusDraft, QuotationStatusFinalized:
		return true
	default:
		return false
	}
}

// TaxMode selects whether a Quotation has an active percentage tax
// configured (design spec §15). TaxMode=none is the default for every
// newly-generated Quotation — the system never infers taxability.
type TaxMode string

const (
	TaxModeNone       TaxMode = "none"
	TaxModePercentage TaxMode = "percentage"
)

// IsValid reports whether m is one of the 2 defined tax modes.
func (m TaxMode) IsValid() bool {
	switch m {
	case TaxModeNone, TaxModePercentage:
		return true
	default:
		return false
	}
}

// Quotation is one immutable-once-finalized version of a customer-facing
// commercial document, generated from a specific finalized Estimate.
// QuotationNumber (e.g. "QT-000124") is stable across ALL versions of one
// commercial quotation chain; Version is a business-meaningful commercial
// revision counter (only advances via CreateNewVersion); Revision is a
// separate optimistic-concurrency counter guarding in-place draft mutation;
// SchemaVersion is the document schema version. All four are distinct
// fields with distinct meanings (design spec §9.1) — never conflate any
// two.
type Quotation struct {
	ID              string
	CompanyID       string
	ProjectID       string
	ClientID        string // copied from Project at creation time, never live-resolved — design spec §18
	EstimateID      string // the SPECIFIC finalized Estimate this version was built from — design spec §3
	QuotationNumber string
	Version         int
	Status          QuotationStatus
	Revision        int64

	Currency string // == the source Estimate's Currency, always — design spec §8

	Lines             []QuotationLine
	Subtotal          money.Money // LIVE — recomputed on every line edit, design spec §6.3
	GeneratedSubtotal money.Money // FROZEN at generation time — design spec §1/§6.3, Review Decision 3

	TaxMode    TaxMode
	TaxLabel   string
	TaxRateBPS money.RateBPS
	TaxAmount  money.Money

	Total money.Money // Subtotal + TaxAmount

	Terms           string
	PaymentSchedule string
	ValidUntil      *time.Time

	Notes string // internal-authoring notes, contractor-only — NEVER client-facing, design spec §4/§24

	CreatedAt     time.Time
	FinalizedAt   *time.Time
	SchemaVersion int
}

// QuotationLine is one customer-facing commercial line. It NEVER carries
// any internal cost/margin figure — design spec §4/§5. SourceWorkItemIDs is
// traceability metadata only: empty for a fully contractor-authored line,
// one entry for an unmerged generated line, multiple entries when the
// contractor has merged several generated lines into one grouped/
// fixed-package line. The SAME WorkItemID may appear on more than one line
// simultaneously (a split) — this is not a partition or an ownership claim
// (design spec §1/§5.3).
type QuotationLine struct {
	ID                string
	SourceWorkItemIDs []string
	Description       string
	Quantity          *decimal.Decimal // optional — only meaningful with Unit
	Unit              *string          // optional
	UnitPrice         *money.Money     // optional — only meaningful with Quantity/Unit
	Amount            money.Money      // ALWAYS present, ALWAYS strictly positive — design spec §7.2
	SortOrder         int
}
```

- [ ] **Step 3: Write a smoke test for the two `IsValid()` methods**

Create `backend/internal/quotations/quotation_test.go`:

```go
package quotations_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

func TestQuotationStatusIsValid(t *testing.T) {
	valid := []quotations.QuotationStatus{quotations.QuotationStatusDraft, quotations.QuotationStatusFinalized}
	for _, s := range valid {
		if !s.IsValid() {
			t.Fatalf("expected %q to be valid", s)
		}
	}
	if quotations.QuotationStatus("sent").IsValid() {
		t.Fatal("expected an M6 status like 'sent' to be invalid in M5")
	}
	if quotations.QuotationStatus("").IsValid() {
		t.Fatal("expected empty string to be invalid")
	}
}

func TestTaxModeIsValid(t *testing.T) {
	valid := []quotations.TaxMode{quotations.TaxModeNone, quotations.TaxModePercentage}
	for _, m := range valid {
		if !m.IsValid() {
			t.Fatalf("expected %q to be valid", m)
		}
	}
	if quotations.TaxMode("fixed").IsValid() {
		t.Fatal("expected an undefined tax mode to be invalid")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go build ./internal/quotations/... && go test ./internal/quotations/... -v`
Expected: PASS. If `go build` fails, fix compile errors in `quotation.go` before proceeding (e.g. confirm `money.RateBPS`/`money.Money` are the correct exported type names from `internal/foundation/money` — already confirmed this session).

- [ ] **Step 5: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 4: `quotations.QuotationRepository` interface + sentinel errors

**Files:**
- Create: `backend/internal/quotations/repository.go`

**Interfaces:**
- Produces (used by Task 5's Mongo implementation, Task 8's Service, and every later test): a `QuotationRepository` interface with 8 methods (`Create`, `FindByID`, `ListByProject`, `FindMaxVersion`, `ReplaceLines`, `ReplaceTerms`, `ReplaceTax`, `Finalize`) and a `QuotationCounterRepository` interface with 1 method (`NextQuotationNumber`) — exact signatures in Step 1's code block below.

This task has no standalone test — the interface's correctness is proven by Task 5's Mongo implementation compiling against it and Task 7's integration tests exercising every method against real MongoDB (matching M4's own `estimates/repository.go`, which has no dedicated test file).

Design spec reference: §20 (every sentinel below, verbatim), §26 (index names referenced in comments), §27 (concurrency semantics referenced in comments).

- [ ] **Step 1: Write the interface and sentinels**

Create `backend/internal/quotations/repository.go` with the following content. Package `quotations`, imports `context`, `errors`, `time`.

Sentinel errors (one `var ErrXxx = errors.New("quotations: ...")` per line, in this order — see the design spec §20 code block for the exact wording of each, copied verbatim):

1. `ErrQuotationNotFound` — "quotations: quotation not found"
2. `ErrProjectNotFound` — "quotations: project not found"
3. `ErrEstimateNotFound` — "quotations: estimate not found"
4. `ErrEstimateNotFinalized` — "quotations: source estimate must be finalized"
5. `ErrEstimateProjectMismatch` — "quotations: estimate does not belong to this project"
6. `ErrIneligibleCostBasisForQuotation` — "quotations: estimate's cost basis cannot be allocated into a quotation (a workitem group is non-positive or would receive a zero share)"
7. `ErrQuotationCurrencyMismatch` — "quotations: referenced estimate's currency does not match this quotation chain"
8. `ErrLineCurrencyMismatch` — "quotations: line amount/unitPrice currency does not match this quotation's currency"
9. `ErrWorkItemNotFound` — "quotations: work item not found"
10. `ErrDuplicateWorkItemIDInLine` — "quotations: sourceWorkItemIds must not contain the same work item twice"
11. `ErrUnknownLineID` — "quotations: line id does not belong to this quotation"
12. `ErrDuplicateLineID` — "quotations: duplicate line id in submitted lines"
13. `ErrBlankLineDescription` — "quotations: line description must not be blank"
14. `ErrQuotationNotDraft` — "quotations: quotation is not a draft"
15. `ErrQuotationMustBeFinalizedBeforeNewVersion` — "quotations: source quotation must be finalized before creating a new version"
16. `ErrIncompleteLinePricing` — "quotations: quantity, unit, and unitPrice must all be supplied together or not at all"
17. `ErrInvalidLineQuantity` — "quotations: quantity must be strictly positive"
18. `ErrInvalidLineUnitPrice` — "quotations: unitPrice must be strictly positive" (strictly positive, not merely non-negative — a zero UnitPrice would force Amount==0, unconditionally rejected)
19. `ErrLineAmountMismatch` — "quotations: line amount does not match quantity x unitPrice"
20. `ErrLineAmountNotPositive` — "quotations: line amount must be strictly positive"
21. `ErrEmptyLines` — "quotations: at least one line is required"
22. `ErrInvalidTaxMode` — "quotations: invalid tax mode"
23. `ErrInvalidTaxRate` — "quotations: invalid tax rate for the given tax mode"
24. `ErrInvalidTaxLabel` — "quotations: invalid tax label for the given tax mode"
25. `ErrRevisionMismatch` — "quotations: revision mismatch or quotation is no longer a draft"
26. `ErrVersionConflict` — "quotations: version number conflict, retry with a fresh version" (transient; also returned when the bounded retry loop is exhausted, per §27.5 — not a distinct sentinel)
27. `ErrDraftAlreadyExists` — "quotations: a draft already exists for this quotation chain" (not transient, never retried)
28. `ErrUnclassifiedDuplicateKey` — "quotations: unclassified duplicate-key error" (never retried, surfaces as 500)
29. `ErrQuotationNumberAllocationFailed` — "quotations: failed to allocate a quotation number" (ONLY a genuine counter-infrastructure failure — never version-retry exhaustion or an unclassified collision)

Give each sentinel a one-line doc comment naming the design-spec section it comes from, following the exact `// ErrXxx is returned when ...` phrasing style used throughout `internal/estimates/repository.go` and `internal/estimates/service.go` (already read in full this session — mirror that comment style precisely, one sentence stating the trigger condition, a second sentence only when there is a non-obvious retry/transience rule attached, as seen for `ErrVersionConflict`/`ErrDraftAlreadyExists`/`ErrUnclassifiedDuplicateKey` in `estimates/repository.go`).

Then the two interfaces:

```go
// QuotationRepository persists Quotations. quotations owns the quotations
// collection exclusively; no other module may query it directly.
type QuotationRepository interface {
	Create(ctx context.Context, q Quotation) (Quotation, error)
	FindByID(ctx context.Context, companyID, id string) (Quotation, error)
	ListByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error)
	FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error)
	ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)
	ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)
	ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error)
	Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Quotation, error)
}

// QuotationCounterRepository allocates monotonically increasing
// QuotationNumbers per Company via an atomic FindOneAndUpdate/$inc against
// the quotation_counters collection (design spec §9.3/§27.1). Counter gaps
// are acceptable and are never repaired (design spec §2).
type QuotationCounterRepository interface {
	NextQuotationNumber(ctx context.Context, companyID string) (int64, error)
}
```

`FindMaxVersion` is keyed by `{companyID, quotationNumber}` (NOT `projectID` — design spec §9.2/§10: the version sequence is scoped to the QuotationNumber chain, since one Project could in principle host more than one chain). `ReplaceLines`/`ReplaceTerms`/`ReplaceTax` each touch only their own named subset of fields — mirroring M4's `ReplaceSnapshot`/`UpdatePricing` split exactly (never one generic `Update` method). `ReplaceLines` must never write `GeneratedSubtotal` (design spec §6.3/§28 — proven later by a repository-layer unit test in Task 7).

- [ ] **Step 2: Confirm the package builds**

Run: `cd backend && go build ./internal/quotations/...`
Expected: exits 0, no output.

- [ ] **Step 3: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 5: `MongoQuotationRepository` — MongoDB implementation, indexes, error classification

**Files:**
- Create: `backend/internal/quotations/repository_mongo.go`

**Interfaces:**
- Consumes: `QuotationRepository`, `QuotationCounterRepository` (Task 4), `Quotation`/`QuotationLine`/`QuotationStatus`/`TaxMode` (Task 3), `money.Money`/`money.RateBPS` (existing).
- Produces (used by Task 7's integration tests, Task 8's Service, Task 11's composition root):
  ```go
  type MongoQuotationRepository struct { ... }
  func NewMongoQuotationRepository(db *mongo.Database) *MongoQuotationRepository
  func (r *MongoQuotationRepository) EnsureIndexes(ctx context.Context) error
  // + all 8 QuotationRepository methods

  type MongoQuotationCounterRepository struct { ... }
  func NewMongoQuotationCounterRepository(db *mongo.Database) *MongoQuotationCounterRepository
  func (r *MongoQuotationCounterRepository) EnsureIndexes(ctx context.Context) error
  func (r *MongoQuotationCounterRepository) NextQuotationNumber(ctx context.Context, companyID string) (int64, error)
  ```

No unit test in this task — proven by Task 7's Testcontainers integration tests (matching M4's `estimates/repository_mongo.go`, which has no white-box unit test of its own either).

Design spec reference: §26 (indexes, verbatim key patterns and names), §27.1-27.3 (concurrency/classification logic), §2 (`quotation_counters` collection).

- [ ] **Step 1: Write the Mongo repository**

Read `backend/internal/estimates/repository_mongo.go` in full first (already read this session) — this task mirrors its structure exactly: `estimateLineDoc`/`estimateDoc` BSON structs, `toEstimateDoc`/`fromEstimateDoc` mapping functions, `classifyCreateError` using `strings.Contains` against explicit index-name constants, `conditionalUpdate` helper with an `incrementRevision bool` parameter.

Create `backend/internal/quotations/repository_mongo.go`:

```go
package quotations

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MongoQuotationRepository is the MongoDB-backed QuotationRepository
// implementation. It owns the "quotations" collection exclusively.
type MongoQuotationRepository struct {
	collection *mongo.Collection
}

// NewMongoQuotationRepository constructs a MongoQuotationRepository
// against db's "quotations" collection.
func NewMongoQuotationRepository(db *mongo.Database) *MongoQuotationRepository {
	return &MongoQuotationRepository{collection: db.Collection("quotations")}
}

// Explicit index names — EVERY index carries one, with no exceptions,
// including the plain {companyId} index (design spec §26, 2nd review
// round correction). uq_quotations_company_number_version and
// uq_quotations_one_draft_per_number both key on
// {companyId, quotationNumber} (one plain-unique, one partial-unique
// scoped to status=draft) and would otherwise collide on MongoDB's
// default auto-generated name.
const (
	indexNameQuotationsCompany                = "idx_quotations_company"
	indexNameQuotationsCompanyProject         = "idx_quotations_company_project"
	indexNameUniqueQuotationsCompanyNumberVersion = "uq_quotations_company_number_version"
	indexNameUniqueQuotationsOneDraftPerNumber    = "uq_quotations_one_draft_per_number"
)

// EnsureIndexes creates the companyId index, the companyId+projectId
// compound index, the UNIQUE companyId+quotationNumber+version index, and
// the UNIQUE PARTIAL companyId+quotationNumber index (status=draft only)
// enforcing "at most one draft per QuotationNumber chain" (design spec
// §26).
func (r *MongoQuotationRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameQuotationsCompany)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameQuotationsCompanyProject)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueQuotationsCompanyNumberVersion)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "quotationNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"status": "draft"}).
				SetName(indexNameUniqueQuotationsOneDraftPerNumber)},
	})
	return err
}

type quotationLineDoc struct {
	ID                string      `bson:"id"`
	SourceWorkItemIDs []string    `bson:"sourceWorkItemIds"`
	Description       string      `bson:"description"`
	Quantity          *string     `bson:"quantity,omitempty"`
	Unit              *string     `bson:"unit,omitempty"`
	UnitPrice         *money.Money `bson:"unitPrice,omitempty"`
	Amount            money.Money  `bson:"amount"`
	SortOrder         int          `bson:"sortOrder"`
}

type quotationDoc struct {
	ID                bson.ObjectID       `bson:"_id,omitempty"`
	CompanyID         string              `bson:"companyId"`
	ProjectID         string              `bson:"projectId"`
	ClientID          string              `bson:"clientId"`
	EstimateID        string              `bson:"estimateId"`
	QuotationNumber   string              `bson:"quotationNumber"`
	Version           int                 `bson:"version"`
	Status            string              `bson:"status"`
	Revision          int64               `bson:"revision"`
	Currency          string              `bson:"currency"`
	Lines             []quotationLineDoc  `bson:"lines"`
	Subtotal          money.Money         `bson:"subtotal"`
	GeneratedSubtotal money.Money         `bson:"generatedSubtotal"`
	TaxMode           string              `bson:"taxMode"`
	TaxLabel          string              `bson:"taxLabel"`
	TaxRateBPS        int64               `bson:"taxRateBps"`
	TaxAmount         money.Money         `bson:"taxAmount"`
	Total             money.Money         `bson:"total"`
	Terms             string              `bson:"terms"`
	PaymentSchedule   string              `bson:"paymentSchedule"`
	ValidUntil        *time.Time          `bson:"validUntil,omitempty"`
	Notes             string              `bson:"notes"`
	CreatedAt         time.Time           `bson:"createdAt"`
	FinalizedAt       *time.Time          `bson:"finalizedAt,omitempty"`
	SchemaVersion     int                 `bson:"schemaVersion"`
}

func toQuotationLineDocs(lines []QuotationLine) []quotationLineDoc {
	docs := make([]quotationLineDoc, len(lines))
	for i, l := range lines {
		var quantityStr *string
		if l.Quantity != nil {
			s := l.Quantity.String()
			quantityStr = &s
		}
		sourceIDs := l.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		docs[i] = quotationLineDoc{
			ID: l.ID, SourceWorkItemIDs: sourceIDs, Description: l.Description,
			Quantity: quantityStr, Unit: l.Unit, UnitPrice: l.UnitPrice,
			Amount: l.Amount, SortOrder: l.SortOrder,
		}
	}
	return docs
}

func fromQuotationLineDocs(docs []quotationLineDoc) ([]QuotationLine, error) {
	lines := make([]QuotationLine, len(docs))
	for i, d := range docs {
		var qty *decimal.Decimal
		if d.Quantity != nil {
			parsed, err := decimal.NewFromString(*d.Quantity)
			if err != nil {
				return nil, err
			}
			qty = &parsed
		}
		sourceIDs := d.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: d.ID, SourceWorkItemIDs: sourceIDs, Description: d.Description,
			Quantity: qty, Unit: d.Unit, UnitPrice: d.UnitPrice,
			Amount: d.Amount, SortOrder: d.SortOrder,
		}
	}
	return lines, nil
}

func toQuotationDoc(q Quotation) (quotationDoc, error) {
	doc := quotationDoc{
		CompanyID: q.CompanyID, ProjectID: q.ProjectID, ClientID: q.ClientID, EstimateID: q.EstimateID,
		QuotationNumber: q.QuotationNumber, Version: q.Version, Status: string(q.Status), Revision: q.Revision,
		Currency: q.Currency, Lines: toQuotationLineDocs(q.Lines),
		Subtotal: q.Subtotal, GeneratedSubtotal: q.GeneratedSubtotal,
		TaxMode: string(q.TaxMode), TaxLabel: q.TaxLabel, TaxRateBPS: int64(q.TaxRateBPS),
		TaxAmount: q.TaxAmount, Total: q.Total,
		Terms: q.Terms, PaymentSchedule: q.PaymentSchedule, ValidUntil: q.ValidUntil, Notes: q.Notes,
		CreatedAt: q.CreatedAt, FinalizedAt: q.FinalizedAt, SchemaVersion: q.SchemaVersion,
	}
	if q.ID != "" {
		objID, err := bson.ObjectIDFromHex(q.ID)
		if err != nil {
			return quotationDoc{}, err
		}
		doc.ID = objID
	}
	return doc, nil
}

func fromQuotationDoc(doc quotationDoc) (Quotation, error) {
	lines, err := fromQuotationLineDocs(doc.Lines)
	if err != nil {
		return Quotation{}, err
	}
	return Quotation{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, ClientID: doc.ClientID,
		EstimateID: doc.EstimateID, QuotationNumber: doc.QuotationNumber, Version: doc.Version,
		Status: QuotationStatus(doc.Status), Revision: doc.Revision, Currency: doc.Currency, Lines: lines,
		Subtotal: doc.Subtotal, GeneratedSubtotal: doc.GeneratedSubtotal,
		TaxMode: TaxMode(doc.TaxMode), TaxLabel: doc.TaxLabel, TaxRateBPS: money.RateBPS(doc.TaxRateBPS),
		TaxAmount: doc.TaxAmount, Total: doc.Total,
		Terms: doc.Terms, PaymentSchedule: doc.PaymentSchedule, ValidUntil: doc.ValidUntil, Notes: doc.Notes,
		CreatedAt: doc.CreatedAt, FinalizedAt: doc.FinalizedAt, SchemaVersion: doc.SchemaVersion,
	}, nil
}

// classifyCreateError translates a raw MongoDB duplicate-key error from
// InsertOne into one of the two named domain sentinels (ErrVersionConflict,
// ErrDraftAlreadyExists) by checking which EXPLICIT index name appears in
// the write error's message. Returns ErrUnclassifiedDuplicateKey (NOT
// ErrVersionConflict) if a duplicate-key error occurs that doesn't match
// either known index name — an error this function cannot positively
// identify must not be silently assumed safe to retry (design spec §27.3
// step 6, mirroring estimates.classifyCreateError exactly).
func classifyCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			switch {
			case strings.Contains(we.Message, indexNameUniqueQuotationsOneDraftPerNumber):
				return ErrDraftAlreadyExists
			case strings.Contains(we.Message, indexNameUniqueQuotationsCompanyNumberVersion):
				return ErrVersionConflict
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoQuotationRepository) Create(ctx context.Context, q Quotation) (Quotation, error) {
	doc, err := toQuotationDoc(q)
	if err != nil {
		return Quotation{}, err
	}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Quotation{}, classifyCreateError(err)
	}
	q.ID = res.InsertedID.(bson.ObjectID).Hex()
	return q, nil
}

func (r *MongoQuotationRepository) FindByID(ctx context.Context, companyID, id string) (Quotation, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Quotation{}, ErrQuotationNotFound
	}
	var doc quotationDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Quotation{}, ErrQuotationNotFound
	}
	if err != nil {
		return Quotation{}, err
	}
	return fromQuotationDoc(doc)
}

func (r *MongoQuotationRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "projectId": projectID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []quotationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]Quotation, 0, len(docs))
	for _, doc := range docs {
		q, err := fromQuotationDoc(doc)
		if err != nil {
			return nil, err
		}
		result = append(result, q)
	}
	return result, nil
}

func (r *MongoQuotationRepository) FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error) {
	opts := options.FindOne().SetSort(bson.D{{Key: "version", Value: -1}}).SetProjection(bson.M{"version": 1})
	var doc struct {
		Version int `bson:"version"`
	}
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "quotationNumber": quotationNumber}, opts).Decode(&doc)
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
// part of the same $set. If false (used by Finalize), revision is left
// untouched — frozen at whatever value it held at the moment finalize
// succeeds (design spec §12/§27.4, mirroring
// estimates.conditionalUpdate exactly).
func (r *MongoQuotationRepository) conditionalUpdate(ctx context.Context, companyID, id string, expectedRevision int64, set bson.M, incrementRevision bool) (Quotation, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Quotation{}, ErrQuotationNotFound
	}
	if incrementRevision {
		set["revision"] = expectedRevision + 1
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(QuotationStatusDraft), "revision": expectedRevision},
		bson.M{"$set": set},
	)
	if err != nil {
		return Quotation{}, err
	}
	if res.MatchedCount == 0 {
		_, findErr := r.FindByID(ctx, companyID, id)
		if findErr == ErrQuotationNotFound {
			return Quotation{}, ErrQuotationNotFound
		}
		return Quotation{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

func (r *MongoQuotationRepository) ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"lines": toQuotationLineDocs(updated.Lines), "subtotal": updated.Subtotal,
		"taxAmount": updated.TaxAmount, "total": updated.Total,
	}
	// GeneratedSubtotal is deliberately absent from this $set — it is
	// frozen at generation time and must never be touched by a line edit
	// (design spec §6.3, Review Decision 3).
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"terms": updated.Terms, "paymentSchedule": updated.PaymentSchedule,
		"notes": updated.Notes, "validUntil": updated.ValidUntil,
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated Quotation) (Quotation, error) {
	set := bson.M{
		"taxMode": string(updated.TaxMode), "taxLabel": updated.TaxLabel, "taxRateBps": int64(updated.TaxRateBPS),
		"taxAmount": updated.TaxAmount, "total": updated.Total,
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, true)
}

func (r *MongoQuotationRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (Quotation, error) {
	set := bson.M{"status": string(QuotationStatusFinalized), "finalizedAt": finalizedAt}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set, false)
}

// MongoQuotationCounterRepository is the MongoDB-backed
// QuotationCounterRepository implementation. It owns the
// "quotation_counters" collection exclusively (design spec §2).
type MongoQuotationCounterRepository struct {
	collection *mongo.Collection
}

// NewMongoQuotationCounterRepository constructs a
// MongoQuotationCounterRepository against db's "quotation_counters"
// collection.
func NewMongoQuotationCounterRepository(db *mongo.Database) *MongoQuotationCounterRepository {
	return &MongoQuotationCounterRepository{collection: db.Collection("quotation_counters")}
}

const indexNameUniqueQuotationCountersCompany = "uq_quotation_counters_company"

// EnsureIndexes creates the UNIQUE companyId index enforcing one counter
// document per Company (design spec §26).
func (r *MongoQuotationCounterRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueQuotationCountersCompany)},
	})
	return err
}

// NextQuotationNumber atomically increments and returns companyID's next
// sequence number via FindOneAndUpdate/$inc with upsert:true — atomic by
// construction, no read-then-write race window, no retry loop needed
// (design spec §9.3/§27.1).
func (r *MongoQuotationCounterRepository) NextQuotationNumber(ctx context.Context, companyID string) (int64, error) {
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc struct {
		NextNumber int64 `bson:"nextNumber"`
	}
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{"companyId": companyID},
		bson.M{"$inc": bson.M{"nextNumber": int64(1)}},
		opts,
	).Decode(&doc)
	if err != nil {
		return 0, err
	}
	return doc.NextNumber, nil
}
```

- [ ] **Step 2: Confirm the package builds**

Run: `cd backend && go build ./internal/quotations/...`
Expected: exits 0, no output. If there are import-formatting errors (gofmt alignment in the `const` block), run `gofmt -w backend/internal/quotations/repository_mongo.go` and rebuild.

- [ ] **Step 3: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 6: `estimates.Service.VisitQuotationSeeds` — cost-grouping + allocation, entirely inside `estimates`

**Files:**
- Modify: `backend/internal/estimates/service.go`
- Test: `backend/internal/estimates/service_test.go`

**Interfaces:**
- Consumes: `s.repo.FindByID(ctx, companyID, estimateID) (Estimate, error)` (existing), `money.AllocateProportionally(total money.Money, weights []int64) ([]money.Money, error)` (Task 0).
- Produces (used by Task 8's `quotations.Service` via the `FinalizedEstimateSource` capability interface it will define locally):
  ```go
  func (s *Service) VisitQuotationSeeds(
      ctx context.Context,
      companyID, projectID, estimateID string,
      visit func(workItemID *string, allocatedSellingAmount money.Money) error,
  ) (
      proposedSellingPrice money.Money,
      currency string,
      found bool,
      projectMatches bool,
      finalized bool,
      allocationEligible bool,
      err error,
  )
  ```

**This is the single most important method in the entire M5 build — it is the privacy firewall.** No cost figure, category, or description may ever be returned by it or reachable from its return values. Re-read design spec §4 and §6.2 in full before writing this method if any doubt remains about what may cross the boundary.

Design spec reference: §4 (privacy firewall), §5.4 (WorkItem-less grouping + default description — wait, description does NOT cross this boundary, only the grouping does), §6.2 (allocation algorithm + corrected preconditions), §22 (exact signature, verbatim).

- [ ] **Step 1: Write the failing tests**

Read `backend/internal/estimates/service_test.go` in full first (already read/summarized this session — package `estimates_test`, black-box, fake repository/ProjectLookup/EstimatedCostSource pattern, plain `==`/`!=` sentinel comparison, no testify).

Append to `backend/internal/estimates/service_test.go`:

```go
func TestVisitQuotationSeedsHappyPath(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	workItem1 := "work_1"
	workItem2 := "work_2"
	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	_ = created
	_ = err
	// NOTE: CreateEstimate needs a costSource with items to produce a
	// non-empty snapshot — construct the Service with a costSource that
	// returns two WorkItem-grouped lines instead of the empty default
	// above before running this test; see the corrected setup below.

	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", description: "Tiles", estimated: money.New(100000, "MYR")},
		{costItemID: "c2", workItemID: &workItem2, category: "labour", description: "Install", estimated: money.New(50000, "MYR")},
	}}
	svc = estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)
	finalized, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err = svc.FinalizeEstimate(context.Background(), "company_a", finalized.ID, finalized.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	var seeds []struct {
		workItemID *string
		amount     money.Money
	}
	proposedSellingPrice, currency, found, projectMatches, isFinalized, allocationEligible, err :=
		svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", finalized.ID,
			func(workItemID *string, allocatedSellingAmount money.Money) error {
				seeds = append(seeds, struct {
					workItemID *string
					amount     money.Money
				}{workItemID, allocatedSellingAmount})
				return nil
			})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches || !isFinalized || !allocationEligible {
		t.Fatalf("expected all four eligibility booleans true, got found=%v projectMatches=%v finalized=%v allocationEligible=%v",
			found, projectMatches, isFinalized, allocationEligible)
	}
	if currency != "MYR" {
		t.Fatalf("expected currency MYR, got %s", currency)
	}
	if proposedSellingPrice.Amount != finalized.ProposedSellingPrice.Amount {
		t.Fatalf("expected proposedSellingPrice to match the Estimate's own ProposedSellingPrice %d, got %d",
			finalized.ProposedSellingPrice.Amount, proposedSellingPrice.Amount)
	}
	if len(seeds) != 2 {
		t.Fatalf("expected 2 seeds (one per WorkItem), got %d", len(seeds))
	}
	sum := int64(0)
	for _, s := range seeds {
		if s.amount.Amount <= 0 {
			t.Fatalf("expected every allocated seed amount to be strictly positive, got %d", s.amount.Amount)
		}
		sum += s.amount.Amount
	}
	if sum != proposedSellingPrice.Amount {
		t.Fatalf("expected seed amounts to sum exactly to proposedSellingPrice (%d), got %d", proposedSellingPrice.Amount, sum)
	}
}

func TestVisitQuotationSeedsNotFound(t *testing.T) {
	repo := newFakeEstimateRepository()
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, fakeCostSource{})

	_, _, found, _, _, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", "nonexistent", func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected found=false for a nonexistent estimate")
	}
}

func TestVisitQuotationSeedsProjectMismatch(t *testing.T) {
	workItem1 := "work_1"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", estimated: money.New(100000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	_, _, found, projectMatches, _, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_OTHER", finalized.ID, func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true (the estimate does exist)")
	}
	if projectMatches {
		t.Fatal("expected projectMatches=false for a mismatched project")
	}
}

func TestVisitQuotationSeedsNotFinalized(t *testing.T) {
	workItem1 := "work_1"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItem1, category: "material", estimated: money.New(100000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}

	_, _, found, projectMatches, isFinalized, _, err := svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", created.ID, func(*string, money.Money) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches {
		t.Fatalf("expected found=true, projectMatches=true, got found=%v projectMatches=%v", found, projectMatches)
	}
	if isFinalized {
		t.Fatal("expected finalized=false for a still-draft estimate")
	}
}

func TestVisitQuotationSeedsIneligibleCostBasis(t *testing.T) {
	// A WorkItem group whose summed SnapshottedAmount is non-positive
	// causes allocationEligible=false, even though the Project-wide
	// CostSubtotal is itself positive (design spec §6.2's corrected
	// worked example: WorkItem A RM1,000, WorkItem B -RM200, subtotal
	// RM800).
	workItemA := "work_a"
	workItemB := "work_b"
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: &workItemA, category: "material", estimated: money.New(100000, "MYR")},
		{costItemID: "c2", workItemID: &workItemB, category: "material", estimated: money.New(-20000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	visitorCalled := false
	_, _, found, projectMatches, isFinalized, allocationEligible, err := svc.VisitQuotationSeeds(
		context.Background(), "company_a", "project_1", finalized.ID,
		func(*string, money.Money) error { visitorCalled = true; return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found || !projectMatches || !isFinalized {
		t.Fatalf("expected found/projectMatches/finalized all true, got %v %v %v", found, projectMatches, isFinalized)
	}
	if allocationEligible {
		t.Fatal("expected allocationEligible=false for a non-positive WorkItem cost-group weight")
	}
	if visitorCalled {
		t.Fatal("expected the visit callback to never be invoked when allocationEligible=false")
	}
}

func TestVisitQuotationSeedsWorkItemLessGroup(t *testing.T) {
	// A CostItem with no WorkItemID forms its own group (design spec §5.4)
	// — represented by workItemID=nil in the visit callback.
	repo := newFakeEstimateRepository()
	costSource := fakeCostSource{items: []fakeCostSourceItem{
		{costItemID: "c1", workItemID: nil, category: "permit", estimated: money.New(50000, "MYR")},
	}}
	svc := estimates.NewService(repo, fakeProjectLookup{belongs: true}, costSource)

	created, err := svc.CreateEstimate(context.Background(), "company_a", "project_1", estimates.PricingModeMarkup, 2000)
	if err != nil {
		t.Fatalf("unexpected error creating estimate: %v", err)
	}
	finalized, err := svc.FinalizeEstimate(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing estimate: %v", err)
	}

	var sawNilWorkItem bool
	_, _, _, _, _, _, err = svc.VisitQuotationSeeds(context.Background(), "company_a", "project_1", finalized.ID,
		func(workItemID *string, allocatedSellingAmount money.Money) error {
			if workItemID == nil {
				sawNilWorkItem = true
			}
			return nil
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sawNilWorkItem {
		t.Fatal("expected exactly one seed with workItemID=nil for the WorkItem-less CostItem")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/estimates/... -run TestVisitQuotationSeeds -v`
Expected: FAIL with `svc.VisitQuotationSeeds undefined`.

- [ ] **Step 3: Write the implementation**

Add to `backend/internal/estimates/service.go`, after the existing `CreateNewVersion` method (the last method in the file):

```go
// VisitQuotationSeeds validates that estimateID belongs to companyID AND
// projectID AND is Status=finalized, then internally groups this
// Estimate's Lines by WorkItemID, computes each group's share of
// CostSubtotal, and allocates ProposedSellingPrice across groups
// proportionally with largest-remainder rounding (design spec §6.2).
// Invokes visit once per resulting group — workItemID is nil for the one
// group covering CostItems with no WorkItem, if any exist (design spec
// §5.4).
//
// Eligibility is reported via four plain booleans (found, projectMatches,
// finalized, allocationEligible), never via a shared error value — a
// quotations-defined sentinel cannot be referenced here without an import
// cycle, since quotations already imports estimates for this capability
// (design spec §22). All four booleans are populated only when err==nil;
// visit is invoked ONLY when all four are true.
//
// NEITHER SnapshottedAmount NOR CostSubtotal NOR any intermediate
// cost-weight value crosses this method's return values or the visit
// callback's parameters at any point (design spec §4) — the callback
// signature has no parameter capable of holding one.
func (s *Service) VisitQuotationSeeds(
	ctx context.Context,
	companyID, projectID, estimateID string,
	visit func(workItemID *string, allocatedSellingAmount money.Money) error,
) (
	proposedSellingPrice money.Money,
	currency string,
	found bool,
	projectMatches bool,
	finalized bool,
	allocationEligible bool,
	err error,
) {
	est, findErr := s.repo.FindByID(ctx, companyID, estimateID)
	if findErr == ErrEstimateNotFound {
		return money.Money{}, "", false, false, false, false, nil
	}
	if findErr != nil {
		return money.Money{}, "", false, false, false, false, findErr
	}
	if est.ProjectID != projectID {
		return money.Money{}, "", true, false, false, false, nil
	}
	if est.Status != EstimateStatusFinalized {
		return money.Money{}, "", true, true, false, false, nil
	}

	// Group Lines by WorkItemID. A nil WorkItemID forms its own group
	// (design spec §5.4) — represented by the map key "" internally, but
	// tracked separately so its zero-value string key is never confused
	// with a real (empty-string) WorkItemID, which cannot occur since
	// WorkItemID is always either nil or a real Mongo ObjectID hex string.
	type group struct {
		workItemID *string
		weight     int64
	}
	order := []string{}
	groups := map[string]*group{}
	hasWorkItemLessGroup := false
	for _, line := range est.Lines {
		key := "__none__"
		var wid *string
		if line.WorkItemID != nil {
			key = *line.WorkItemID
			w := *line.WorkItemID
			wid = &w
		} else {
			hasWorkItemLessGroup = true
		}
		g, exists := groups[key]
		if !exists {
			g = &group{workItemID: wid, weight: 0}
			groups[key] = g
			order = append(order, key)
		}
		g.weight += line.SnapshottedAmount.Amount
	}
	_ = hasWorkItemLessGroup

	weights := make([]int64, len(order))
	for i, key := range order {
		w := groups[key].weight
		if w <= 0 {
			// A non-positive WorkItem cost-group weight — report via
			// allocationEligible=false, never a returned error (design
			// spec §6.2).
			return money.Money{}, "", true, true, true, false, nil
		}
		weights[i] = w
	}

	shares, allocErr := money.AllocateProportionally(est.ProposedSellingPrice, weights)
	if allocErr != nil {
		// Every documented AllocateProportionally error (§19) represents
		// an ineligible cost basis in this context — report via
		// allocationEligible=false, never propagate money's own error
		// value (which would itself be a cross-package leak of a
		// different kind).
		return money.Money{}, "", true, true, true, false, nil
	}

	for i, key := range order {
		if visitErr := visit(groups[key].workItemID, shares[i]); visitErr != nil {
			return money.Money{}, "", false, false, false, false, visitErr
		}
	}

	return est.ProposedSellingPrice, est.Currency, true, true, true, true, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/estimates/... -v`
Expected: PASS — full `estimates` package green, including all new `TestVisitQuotationSeeds*` tests and every pre-existing M4 test (no regression).

- [ ] **Step 5: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 7: `quotations` repository Testcontainers integration tests

**Files:**
- Create: `backend/internal/quotations/repository_mongo_test.go`

**Interfaces:**
- Consumes: `MongoQuotationRepository`, `MongoQuotationCounterRepository` (Task 5), everything from Tasks 3-4.

No new production code in this task — pure verification against real MongoDB.

Design spec reference: §26 (indexes), §27.1-27.3 (concurrency), §28 (test matrix — repository-layer bullets).

- [ ] **Step 1: Write the integration tests**

Read `backend/internal/estimates/repository_mongo_test.go` in full first (already read/summarized this session) — this task mirrors its `setupMongoDB` helper, per-test unique database naming, and concurrent-race test structure exactly.

Create `backend/internal/quotations/repository_mongo_test.go`:

```go
package quotations_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
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

func sampleQuotation(companyID, projectID, quotationNumber string, version int, status quotations.QuotationStatus) quotations.Quotation {
	return quotations.Quotation{
		CompanyID: companyID, ProjectID: projectID, ClientID: "client_1", EstimateID: "estimate_1",
		QuotationNumber: quotationNumber, Version: version, Status: status, Revision: 0,
		Currency: "MYR",
		Lines: []quotations.QuotationLine{
			{ID: "line_1", SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles", Amount: money.New(620000, "MYR"), SortOrder: 0},
		},
		Subtotal: money.New(620000, "MYR"), GeneratedSubtotal: money.New(620000, "MYR"),
		TaxMode: quotations.TaxModeNone,
		Total:   money.New(620000, "MYR"),
		CreatedAt: time.Now(), SchemaVersion: 1,
	}
}

func TestQuotationRepositoryCreateAndFind(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_create_find")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected a generated ID")
	}

	found, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error finding quotation: %v", err)
	}
	if found.QuotationNumber != "QT-000001" {
		t.Fatalf("expected QuotationNumber QT-000001, got %s", found.QuotationNumber)
	}
	if len(found.Lines) != 1 || found.Lines[0].SourceWorkItemIDs[0] != "work_1" {
		t.Fatalf("expected round-tripped SourceWorkItemIDs, got %+v", found.Lines)
	}
}

func TestQuotationRepositoryFindByIDCrossTenant(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_cross_tenant")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	_, err = repo.FindByID(ctx, "company_b", created.ID)
	if err != quotations.ErrQuotationNotFound {
		t.Fatalf("expected ErrQuotationNotFound for a cross-tenant lookup, got %v", err)
	}
}

func TestQuotationRepositoryUniqueVersionIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_unique_version")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	v1 := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusFinalized)
	if _, err := repo.Create(ctx, v1); err != nil {
		t.Fatalf("unexpected error creating v1: %v", err)
	}

	// A second Version=1 attempt for the same {companyId, quotationNumber}
	// collides on uq_quotations_company_number_version.
	dup := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusFinalized)
	_, err := repo.Create(ctx, dup)
	if err != quotations.ErrVersionConflict {
		t.Fatalf("expected ErrVersionConflict for a duplicate version number, got %v", err)
	}
}

func TestQuotationRepositoryOneDraftPerNumberPartialIndex(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_one_draft")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	draft1 := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	if _, err := repo.Create(ctx, draft1); err != nil {
		t.Fatalf("unexpected error creating first draft: %v", err)
	}

	// A second draft for the SAME quotationNumber (different Version, so it
	// would NOT collide on the version-unique index) must still collide on
	// the partial one-draft-per-number index.
	draft2 := sampleQuotation("company_a", "project_1", "QT-000001", 2, quotations.QuotationStatusDraft)
	_, err := repo.Create(ctx, draft2)
	if err != quotations.ErrDraftAlreadyExists {
		t.Fatalf("expected ErrDraftAlreadyExists for a second draft in the same chain, got %v", err)
	}

	// A second FINALIZED version for the same chain must NOT be blocked by
	// the partial index (it only constrains status=draft documents).
	finalizedV2 := sampleQuotation("company_a", "project_1", "QT-000001", 2, quotations.QuotationStatusFinalized)
	if _, err := repo.Create(ctx, finalizedV2); err != nil {
		t.Fatalf("expected a second FINALIZED version to be allowed, got error: %v", err)
	}
}

func TestQuotationRepositoryReplaceLinesRevisionGuard(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_replace_lines_revision")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Subtotal = money.New(700000, "MYR")
	updated.Total = money.New(700000, "MYR")
	_, err = repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error on a valid revision: %v", err)
	}

	// A stale (already-consumed) revision must be rejected.
	_, err = repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch on a stale revision, got %v", err)
	}
}

func TestQuotationRepositoryReplaceLinesNeverTouchesGeneratedSubtotal(t *testing.T) {
	// Direct proof of design spec §6.3/§28: GeneratedSubtotal must be
	// absent from ReplaceLines' own $set document — asserted here by
	// reading the document back and confirming it still matches the
	// ORIGINAL GeneratedSubtotal even though Subtotal (a DIFFERENT field)
	// was changed in the same call.
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_generated_subtotal_frozen")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	q.GeneratedSubtotal = money.New(620000, "MYR")
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Subtotal = money.New(500000, "MYR") // deliberately diverging from GeneratedSubtotal
	updated.GeneratedSubtotal = money.New(999999, "MYR") // even if the caller (buggily) sets this, ReplaceLines must ignore it
	replaced, err := repo.ReplaceLines(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if replaced.GeneratedSubtotal.Amount != 620000 {
		t.Fatalf("expected GeneratedSubtotal to remain frozen at 620000, got %d", replaced.GeneratedSubtotal.Amount)
	}
	if replaced.Subtotal.Amount != 500000 {
		t.Fatalf("expected Subtotal to have been updated to 500000, got %d", replaced.Subtotal.Amount)
	}
}

func TestQuotationRepositoryFinalizeFreezesRevision(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_finalize_revision")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	q := sampleQuotation("company_a", "project_1", "QT-000001", 1, quotations.QuotationStatusDraft)
	created, err := repo.Create(ctx, q)
	if err != nil {
		t.Fatalf("unexpected error creating quotation: %v", err)
	}

	updated := created
	updated.Terms = "50% deposit"
	afterTermsUpdate, err := repo.ReplaceTerms(ctx, "company_a", created.ID, created.Revision, updated)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if afterTermsUpdate.Revision != 1 {
		t.Fatalf("expected Revision incremented to 1, got %d", afterTermsUpdate.Revision)
	}

	finalized, err := repo.Finalize(ctx, "company_a", created.ID, afterTermsUpdate.Revision, time.Now())
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}
	if finalized.Revision != 1 {
		t.Fatalf("expected Revision to remain frozen at 1 after finalize, got %d", finalized.Revision)
	}
	if finalized.Status != quotations.QuotationStatusFinalized {
		t.Fatal("expected status=finalized")
	}
}

func TestQuotationRepositoryConcurrentVersionCreation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_concurrent_version")
	repo := quotations.NewMongoQuotationRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	const n = 5
	for i := 1; i <= n; i++ {
		q := sampleQuotation("company_a", "project_1", "QT-000001", i, quotations.QuotationStatusFinalized)
		if _, err := repo.Create(ctx, q); err != nil {
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
			version, err := createNextQuotationVersionWithRetry(ctx, repo, "company_a", "project_1", "QT-000001")
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
}

// createNextQuotationVersionWithRetry implements the bounded-retry
// strategy from design spec §27.3 steps 2-4: read MAX(version), attempt
// insert at MAX+1, retry on a version-number duplicate-key race.
func createNextQuotationVersionWithRetry(ctx context.Context, repo *quotations.MongoQuotationRepository, companyID, projectID, quotationNumber string) (int, error) {
	const maxAttempts = 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		maxVersion, err := repo.FindMaxVersion(ctx, companyID, quotationNumber)
		if err != nil {
			return 0, err
		}
		nextVersion := maxVersion + 1
		q := sampleQuotation(companyID, projectID, quotationNumber, nextVersion, quotations.QuotationStatusFinalized)
		_, err = repo.Create(ctx, q)
		if err == nil {
			return nextVersion, nil
		}
		if err == quotations.ErrVersionConflict {
			continue
		}
		return 0, err
	}
	return 0, quotations.ErrVersionConflict
}

func TestQuotationCounterRepositoryConcurrentAllocation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_counter_concurrent")
	repo := quotations.NewMongoQuotationCounterRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	const n = 10
	results := make(chan int64, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			num, err := repo.NextQuotationNumber(ctx, "company_a")
			if err != nil {
				errs <- err
				return
			}
			results <- num
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error allocating a quotation number: %v", err)
	}

	seen := make(map[int64]bool)
	for num := range results {
		if seen[num] {
			t.Fatalf("duplicate quotation number %d produced by concurrent allocation", num)
		}
		seen[num] = true
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique quotation numbers, got %d", n, len(seen))
	}
}

func TestQuotationCounterRepositoryPerCompanyIsolation(t *testing.T) {
	client := setupMongoDB(t)
	db := platformmongo.Database(client, "quotations_test_counter_isolation")
	repo := quotations.NewMongoQuotationCounterRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("unexpected error ensuring indexes: %v", err)
	}

	numA1, err := repo.NextQuotationNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	numB1, err := repo.NextQuotationNumber(ctx, "company_b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if numA1 != 1 || numB1 != 1 {
		t.Fatalf("expected both companies' first allocation to be 1 (independent per-company counters), got %d and %d", numA1, numB1)
	}
	numA2, err := repo.NextQuotationNumber(ctx, "company_a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if numA2 != 2 {
		t.Fatalf("expected company_a's second allocation to be 2, got %d", numA2)
	}
}
```

- [ ] **Step 2: Run the tests**

Run: `cd backend && go test ./internal/quotations/... -v`
Expected: PASS — all tests in this file green against real Testcontainers MongoDB. If Docker throws a transient "rootless Docker is not supported on Windows" error, re-run the same command once (a known, previously-documented flake in this environment, not a real defect — see `tasks/lessons.md`/prior milestone handoffs).

- [ ] **Step 3: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 8: `quotations.Service` — capability interfaces + business logic

**Files:**
- Create: `backend/internal/quotations/service.go`

**Interfaces:**
- Consumes: `QuotationRepository`, `QuotationCounterRepository` (Task 4/5), `Quotation`/`QuotationLine` (Task 3), `money.CalculateLineAmount` (existing M3 helper).
- Produces (used by Task 9's tests, Task 10's handlers, Task 11's composition root):
  ```go
  type ProjectLookup interface {
      ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
      GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error)
  }
  type WorkItemLookup interface {
      GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (description string, found bool, err error)
  }
  type FinalizedEstimateSource interface {
      VisitQuotationSeeds(ctx context.Context, companyID, projectID, estimateID string,
          visit func(workItemID *string, allocatedSellingAmount money.Money) error,
      ) (proposedSellingPrice money.Money, currency string, found, projectMatches, finalized, allocationEligible bool, err error)
  }

  type Service struct { ... }
  func NewService(repo QuotationRepository, counterRepo QuotationCounterRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, estimateSource FinalizedEstimateSource) *Service

  func (s *Service) CreateQuotation(ctx context.Context, companyID, projectID, estimateID string) (Quotation, error)
  func (s *Service) GetQuotation(ctx context.Context, companyID, quotationID string) (Quotation, error)
  func (s *Service) ListQuotationsByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error)
  func (s *Service) ReplaceLines(ctx context.Context, companyID, quotationID string, lines []QuotationLine, expectedRevision int64) (Quotation, error)
  func (s *Service) UpdateTerms(ctx context.Context, companyID, quotationID, terms, paymentSchedule, notes string, validUntil *time.Time, expectedRevision int64) (Quotation, error)
  func (s *Service) UpdateTax(ctx context.Context, companyID, quotationID string, taxMode TaxMode, taxLabel string, taxRateBPS money.RateBPS, expectedRevision int64) (Quotation, error)
  func (s *Service) FinalizeQuotation(ctx context.Context, companyID, quotationID string, expectedRevision int64) (Quotation, error)
  func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceQuotationID, estimateID string) (Quotation, error)
  ```

This is the largest single task in the plan — the full business-logic layer. Write it in one pass but run `go build` after writing to catch compile errors before moving to Task 9's tests.

Design spec reference: §3 (Estimate boundary), §5.3/§5.4 (line generation), §6.2/§6.3 (allocation consumption, GeneratedSubtotal freeze), §7.1/§7.2 (line validation), §8 (currency validation), §9.2/§9.3 (numbering), §11 (new-version semantics), §12 (lifecycle), §13.1 (PUT /lines validation), §15.1 (tax validation), §18 (ClientID), §22 (capability interfaces, verbatim signatures), §27 (concurrency).

- [ ] **Step 1: Write the service**

Read `backend/internal/estimates/service.go` in full first (already read this session) — mirror its `buildSnapshot`/`allocateAndCreate` two-tier helper pattern, its `CreateEstimate` (no-retry) vs `CreateNewVersion` (bounded-retry) split, and its `RecalculatePricing`/`RefreshEstimate`/`FinalizeEstimate` Revision-guard pattern exactly, adapted to Quotation's own field names.

Create `backend/internal/quotations/service.go`:

```go
package quotations

import (
	"context"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// ProjectLookup is the capability quotations needs from projects.
type ProjectLookup interface {
	ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
	// GetProjectClientID returns the bare ClientID string, never a
	// projects.Project struct (ADR 0002) — design spec §18/§22.
	GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error)
}

// WorkItemLookup is the capability quotations needs from work — a
// customer-presentable WorkItem description for line generation, and a
// Company+Project-scoped existence check for contractor-submitted
// SourceWorkItemIDs on a manual line edit (design spec §5.3/§13.1/§22).
type WorkItemLookup interface {
	GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (description string, found bool, err error)
}

// FinalizedEstimateSource is the capability quotations needs from
// estimates. The return shape structurally cannot carry a cost figure,
// category, or description (design spec §4/§22) — eligibility is reported
// via four plain booleans, never a shared error value.
type FinalizedEstimateSource interface {
	VisitQuotationSeeds(
		ctx context.Context,
		companyID, projectID, estimateID string,
		visit func(workItemID *string, allocatedSellingAmount money.Money) error,
	) (
		proposedSellingPrice money.Money,
		currency string,
		found bool,
		projectMatches bool,
		finalized bool,
		allocationEligible bool,
		err error,
	)
}

// Service implements Quotation creation, versioning, draft editing, tax
// configuration, and finalization.
type Service struct {
	repo           QuotationRepository
	counterRepo    QuotationCounterRepository
	projectLookup  ProjectLookup
	workItemLookup WorkItemLookup
	estimateSource FinalizedEstimateSource
}

// NewService constructs a Service backed by repo/counterRepo, consuming
// projectLookup, workItemLookup, and estimateSource to validate parent
// references and generate customer-facing lines.
func NewService(repo QuotationRepository, counterRepo QuotationCounterRepository, projectLookup ProjectLookup, workItemLookup WorkItemLookup, estimateSource FinalizedEstimateSource) *Service {
	return &Service{repo: repo, counterRepo: counterRepo, projectLookup: projectLookup, workItemLookup: workItemLookup, estimateSource: estimateSource}
}

// generatedLine is an intermediate seed collected from VisitQuotationSeeds
// before descriptions are attached and QuotationLines are built.
type generatedLine struct {
	workItemID *string
	amount     money.Money
}

// generateLines calls estimateSource.VisitQuotationSeeds, maps its four
// eligibility booleans to this package's own sentinels (never propagating
// any value estimates itself defines), and — on success — builds one
// QuotationLine per seed, resolving each generated line's customer-facing
// Description via workItemLookup (never from anything VisitQuotationSeeds
// returns — design spec §4/§5.3).
func (s *Service) generateLines(ctx context.Context, companyID, projectID, estimateID string) ([]QuotationLine, money.Money, string, error) {
	var seeds []generatedLine
	proposedSellingPrice, currency, found, projectMatches, finalized, allocationEligible, err :=
		s.estimateSource.VisitQuotationSeeds(ctx, companyID, projectID, estimateID,
			func(workItemID *string, allocatedSellingAmount money.Money) error {
				seeds = append(seeds, generatedLine{workItemID: workItemID, amount: allocatedSellingAmount})
				return nil
			})
	if err != nil {
		return nil, money.Money{}, "", err
	}
	if !found {
		return nil, money.Money{}, "", ErrEstimateNotFound
	}
	if !projectMatches {
		return nil, money.Money{}, "", ErrEstimateProjectMismatch
	}
	if !finalized {
		return nil, money.Money{}, "", ErrEstimateNotFinalized
	}
	if !allocationEligible {
		return nil, money.Money{}, "", ErrIneligibleCostBasisForQuotation
	}

	lines := make([]QuotationLine, len(seeds))
	for i, seed := range seeds {
		var sourceIDs []string
		description := "General Project Works and Services"
		if seed.workItemID != nil {
			sourceIDs = []string{*seed.workItemID}
			desc, wiFound, wiErr := s.workItemLookup.GetWorkItemDescription(ctx, companyID, projectID, *seed.workItemID)
			if wiErr != nil {
				return nil, money.Money{}, "", wiErr
			}
			if wiFound {
				description = desc
			}
		} else {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: "", SourceWorkItemIDs: sourceIDs, Description: description,
			Amount: seed.amount, SortOrder: i,
		}
	}
	return lines, proposedSellingPrice, currency, nil
}

// CreateQuotation creates Version 1 for a fresh commercial quotation chain
// — allocates a new QuotationNumber (design spec §9.3), generates initial
// Lines from the finalized Estimate (§5.3/§6.2), always as a draft.
// Attempted directly at Version=1 with NO retry loop (design spec §27.2,
// mirroring estimates.CreateEstimate exactly — retrying into a
// recalculated higher version number here would silently bypass the
// finalized-source precondition CreateNewVersion exists to enforce).
func (s *Service) CreateQuotation(ctx context.Context, companyID, projectID, estimateID string) (Quotation, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return Quotation{}, err
	}
	if !belongs {
		return Quotation{}, ErrProjectNotFound
	}

	clientID, err := s.projectLookup.GetProjectClientID(ctx, companyID, projectID)
	if err != nil {
		return Quotation{}, err
	}

	lines, proposedSellingPrice, currency, err := s.generateLines(ctx, companyID, projectID, estimateID)
	if err != nil {
		return Quotation{}, err
	}

	quotationNumber, err := s.nextQuotationNumber(ctx, companyID)
	if err != nil {
		return Quotation{}, err
	}

	created, err := s.repo.Create(ctx, Quotation{
		CompanyID: companyID, ProjectID: projectID, ClientID: clientID, EstimateID: estimateID,
		QuotationNumber: quotationNumber, Version: 1, Status: QuotationStatusDraft, Revision: 0,
		Currency: currency, Lines: lines,
		Subtotal: proposedSellingPrice, GeneratedSubtotal: proposedSellingPrice,
		TaxMode: TaxModeNone, TaxAmount: money.New(0, currency), Total: proposedSellingPrice,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
	if err == ErrVersionConflict || err == ErrDraftAlreadyExists {
		return Quotation{}, ErrVersionConflict
	}
	if err != nil {
		return Quotation{}, err
	}
	return created, nil
}

const quotationNumberDigits = 6

// nextQuotationNumber allocates the next per-Company sequence number via
// the atomic counter and formats it as "QT-000001" (design spec §9.2).
func (s *Service) nextQuotationNumber(ctx context.Context, companyID string) (string, error) {
	next, err := s.counterRepo.NextQuotationNumber(ctx, companyID)
	if err != nil {
		return "", ErrQuotationNumberAllocationFailed
	}
	digits := decimal.NewFromInt(next).StringFixed(0)
	for len(digits) < quotationNumberDigits {
		digits = "0" + digits
	}
	return "QT-" + digits, nil
}

// GetQuotation returns quotationID's Quotation, tenant-scoped to companyID.
func (s *Service) GetQuotation(ctx context.Context, companyID, quotationID string) (Quotation, error) {
	return s.repo.FindByID(ctx, companyID, quotationID)
}

// ListQuotationsByProject validates projectID belongs to companyID before
// listing — a foreign projectID returns ErrProjectNotFound, never an empty
// list.
func (s *Service) ListQuotationsByProject(ctx context.Context, companyID, projectID string) ([]Quotation, error) {
	belongs, err := s.projectLookup.ProjectBelongsToCompany(ctx, companyID, projectID)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, ErrProjectNotFound
	}
	return s.repo.ListByProject(ctx, companyID, projectID)
}

// validateLine applies design spec §7.1/§7.2/§8's rules to one submitted
// line: Quantity/Unit/UnitPrice all-or-nothing, positivity, product-
// matches-Amount, currency match against the Quotation's own currency,
// and Amount > 0.
func validateLine(line QuotationLine, quotationCurrency string) error {
	quantitySupplied := line.Quantity != nil || line.Unit != nil || line.UnitPrice != nil
	quantityComplete := line.Quantity != nil && line.Unit != nil && line.UnitPrice != nil
	if quantitySupplied && !quantityComplete {
		return ErrIncompleteLinePricing
	}
	if quantityComplete {
		if !line.Quantity.IsPositive() {
			return ErrInvalidLineQuantity
		}
		if line.UnitPrice.Amount <= 0 {
			return ErrInvalidLineUnitPrice
		}
		if line.UnitPrice.Currency != quotationCurrency {
			return ErrLineCurrencyMismatch
		}
		expected := money.CalculateLineAmount(*line.Quantity, *line.UnitPrice)
		if expected.Amount != line.Amount.Amount {
			return ErrLineAmountMismatch
		}
	}
	if line.Amount.Currency != quotationCurrency {
		return ErrLineCurrencyMismatch
	}
	if line.Amount.Amount <= 0 {
		return ErrLineAmountNotPositive
	}
	if strings.TrimSpace(line.Description) == "" {
		return ErrBlankLineDescription
	}
	return nil
}

// ReplaceLines validates and replaces the entire Lines array on a draft
// Quotation, per design spec §13.1's full validation sequence — applied to
// the WHOLE submitted array before ANY line is accepted. Recomputes
// Subtotal/TaxAmount/Total server-side. NEVER touches GeneratedSubtotal.
// Draft-only; Revision-guarded.
func (s *Service) ReplaceLines(ctx context.Context, companyID, quotationID string, lines []QuotationLine, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}
	if len(lines) == 0 {
		return Quotation{}, ErrEmptyLines
	}

	seenLineIDs := map[string]bool{}
	existingLineIDs := map[string]bool{}
	for _, l := range existing.Lines {
		existingLineIDs[l.ID] = true
	}

	assigned := make([]QuotationLine, len(lines))
	for i, line := range lines {
		if line.ID != "" {
			if !existingLineIDs[line.ID] {
				return Quotation{}, ErrUnknownLineID
			}
			if seenLineIDs[line.ID] {
				return Quotation{}, ErrDuplicateLineID
			}
			seenLineIDs[line.ID] = true
		}

		seenWorkItemIDs := map[string]bool{}
		for _, wid := range line.SourceWorkItemIDs {
			if seenWorkItemIDs[wid] {
				return Quotation{}, ErrDuplicateWorkItemIDInLine
			}
			seenWorkItemIDs[wid] = true
			_, found, err := s.workItemLookup.GetWorkItemDescription(ctx, companyID, existing.ProjectID, wid)
			if err != nil {
				return Quotation{}, err
			}
			if !found {
				return Quotation{}, ErrWorkItemNotFound
			}
		}

		if err := validateLine(line, existing.Currency); err != nil {
			return Quotation{}, err
		}

		trimmed := line
		trimmed.Description = strings.TrimSpace(line.Description)
		assigned[i] = trimmed
	}

	subtotal := money.New(0, existing.Currency)
	for _, l := range assigned {
		sum, addErr := subtotal.Add(l.Amount)
		if addErr != nil {
			return Quotation{}, ErrLineCurrencyMismatch
		}
		subtotal = sum
	}

	taxAmount := existing.TaxAmount
	if existing.TaxMode == TaxModePercentage {
		taxAmount = money.ApplyRateBPS(subtotal, existing.TaxRateBPS)
	} else {
		taxAmount = money.New(0, existing.Currency)
	}
	total, err := subtotal.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	updated := existing
	updated.Lines = assigned
	updated.Subtotal = subtotal
	updated.TaxAmount = taxAmount
	updated.Total = total

	return s.repo.ReplaceLines(ctx, companyID, quotationID, expectedRevision, updated)
}

// UpdateTerms updates Terms/PaymentSchedule/Notes/ValidUntil on a draft
// Quotation. Draft-only; Revision-guarded.
func (s *Service) UpdateTerms(ctx context.Context, companyID, quotationID, terms, paymentSchedule, notes string, validUntil *time.Time, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}

	updated := existing
	updated.Terms = terms
	updated.PaymentSchedule = paymentSchedule
	updated.Notes = notes
	updated.ValidUntil = validUntil

	return s.repo.ReplaceTerms(ctx, companyID, quotationID, expectedRevision, updated)
}

// UpdateTax validates and updates TaxMode/TaxLabel/TaxRateBPS on a draft
// Quotation per design spec §15.1's exact input-combination rules,
// recomputing TaxAmount/Total from the currently-stored Subtotal. Draft-
// only; Revision-guarded. A rejected call leaves the draft's stored tax
// fields completely untouched.
func (s *Service) UpdateTax(ctx context.Context, companyID, quotationID string, taxMode TaxMode, taxLabel string, taxRateBPS money.RateBPS, expectedRevision int64) (Quotation, error) {
	if !taxMode.IsValid() {
		return Quotation{}, ErrInvalidTaxMode
	}

	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status != QuotationStatusDraft {
		return Quotation{}, ErrQuotationNotDraft
	}

	var taxAmount money.Money
	switch taxMode {
	case TaxModeNone:
		if taxLabel != "" {
			return Quotation{}, ErrInvalidTaxLabel
		}
		if taxRateBPS != 0 {
			return Quotation{}, ErrInvalidTaxRate
		}
		taxAmount = money.New(0, existing.Currency)
	case TaxModePercentage:
		if strings.TrimSpace(taxLabel) == "" {
			return Quotation{}, ErrInvalidTaxLabel
		}
		if taxRateBPS <= 0 || taxRateBPS > money.BasisPointsDenominator {
			return Quotation{}, ErrInvalidTaxRate
		}
		taxAmount = money.ApplyRateBPS(existing.Subtotal, taxRateBPS)
	}

	total, err := existing.Subtotal.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	updated := existing
	updated.TaxMode = taxMode
	updated.TaxLabel = taxLabel
	updated.TaxRateBPS = taxRateBPS
	updated.TaxAmount = taxAmount
	updated.Total = total

	return s.repo.ReplaceTax(ctx, companyID, quotationID, expectedRevision, updated)
}

// FinalizeQuotation transitions a draft to finalized, one-directional,
// locking every field. Revision-guarded. Idempotent on retry: if already
// finalized, returns it unchanged regardless of the supplied
// expectedRevision (design spec §12).
func (s *Service) FinalizeQuotation(ctx context.Context, companyID, quotationID string, expectedRevision int64) (Quotation, error) {
	existing, err := s.repo.FindByID(ctx, companyID, quotationID)
	if err != nil {
		return Quotation{}, err
	}
	if existing.Status == QuotationStatusFinalized {
		return existing, nil
	}
	return s.repo.Finalize(ctx, companyID, quotationID, expectedRevision, time.Now())
}

// CreateNewVersion creates the next Version as a new draft from
// sourceQuotationID's chain, referencing a NEW explicit estimateID
// (design spec §11 — never silently re-resolved to "latest Estimate").
// REQUIRES the source Quotation to already be Status=finalized. Lines and
// GeneratedSubtotal are FRESHLY generated from the new Estimate — never
// copied forward from the source's possibly-hand-edited snapshot (Review
// Decision 4). Terms/PaymentSchedule/ValidUntil/Notes/tax configuration
// ARE copied forward unchanged.
func (s *Service) CreateNewVersion(ctx context.Context, companyID, sourceQuotationID, estimateID string) (Quotation, error) {
	source, err := s.repo.FindByID(ctx, companyID, sourceQuotationID)
	if err != nil {
		return Quotation{}, err
	}
	if source.Status != QuotationStatusFinalized {
		return Quotation{}, ErrQuotationMustBeFinalizedBeforeNewVersion
	}

	lines, proposedSellingPrice, currency, err := s.generateLines(ctx, companyID, source.ProjectID, estimateID)
	if err != nil {
		return Quotation{}, err
	}
	if currency != source.Currency {
		return Quotation{}, ErrQuotationCurrencyMismatch
	}

	taxAmount := money.New(0, currency)
	if source.TaxMode == TaxModePercentage {
		taxAmount = money.ApplyRateBPS(proposedSellingPrice, source.TaxRateBPS)
	}
	total, err := proposedSellingPrice.Add(taxAmount)
	if err != nil {
		return Quotation{}, err
	}

	build := func(version int) Quotation {
		return Quotation{
			CompanyID: companyID, ProjectID: source.ProjectID, ClientID: source.ClientID, EstimateID: estimateID,
			QuotationNumber: source.QuotationNumber, Version: version, Status: QuotationStatusDraft, Revision: 0,
			Currency: currency, Lines: lines,
			Subtotal: proposedSellingPrice, GeneratedSubtotal: proposedSellingPrice,
			TaxMode: source.TaxMode, TaxLabel: source.TaxLabel, TaxRateBPS: source.TaxRateBPS,
			TaxAmount: taxAmount, Total: total,
			Terms: source.Terms, PaymentSchedule: source.PaymentSchedule, ValidUntil: source.ValidUntil, Notes: source.Notes,
			CreatedAt: time.Now(), SchemaVersion: 1,
		}
	}

	return s.allocateAndCreateVersion(ctx, companyID, source.QuotationNumber, build)
}

const maxVersionAllocationAttempts = 5

// allocateAndCreateVersion implements the bounded-retry version-number
// allocation strategy from design spec §27.3 steps 2-6: read MAX(version),
// attempt Create at MAX+1, and on ErrVersionConflict re-read MAX(version)
// and retry, bounded at maxVersionAllocationAttempts. A collision on
// ErrDraftAlreadyExists is NOT retried — propagated immediately. Used
// ONLY by CreateNewVersion — CreateQuotation deliberately does NOT use
// this helper (design spec §27.2, mirroring estimates.allocateAndCreate's
// own identical rationale).
func (s *Service) allocateAndCreateVersion(ctx context.Context, companyID, quotationNumber string, build func(version int) Quotation) (Quotation, error) {
	for attempt := 0; attempt < maxVersionAllocationAttempts; attempt++ {
		maxVersion, err := s.repo.FindMaxVersion(ctx, companyID, quotationNumber)
		if err != nil {
			return Quotation{}, err
		}
		created, err := s.repo.Create(ctx, build(maxVersion+1))
		if err == nil {
			return created, nil
		}
		if err == ErrVersionConflict {
			continue
		}
		return Quotation{}, err
	}
	return Quotation{}, ErrVersionConflict
}
```

- [ ] **Step 2: Confirm the package builds**

Run: `cd backend && go build ./internal/quotations/...`
Expected: exits 0, no output. Fix any compile errors before proceeding to Task 9 — common issues to check: `money.Money.Add` returns `(Money, error)` (confirmed this session), `money.CalculateLineAmount(qty decimal.Decimal, unitPrice Money) Money` takes value types not pointers (confirmed this session — deref `*line.Quantity`/`*line.UnitPrice` when calling it), `money.BasisPointsDenominator` is an untyped int constant `10000` (confirmed this session).

- [ ] **Step 3: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 9: `quotations.Service` unit tests against fakes

**Files:**
- Create: `backend/internal/quotations/service_test.go`

**Interfaces:**
- Consumes: everything from Task 8, plus in-memory fakes defined in this file (mirroring `estimates/service_test.go`'s `fakeEstimateRepository`/`fakeProjectLookup`/`fakeCostSource` pattern exactly).

No production code changes in this task — pure test coverage against fakes, no MongoDB/Testcontainers dependency (fast, run on every `go test ./...`).

Design spec reference: §28 (unit-test matrix, verbatim — this task implements every bullet under "Unit tests").

- [ ] **Step 1: Write the fakes and tests**

Read `backend/internal/estimates/service_test.go` in full first (already read/summarized this session) — reuse its exact `fakeEstimateRepository`-style in-memory map-backed fake pattern, `fakeProjectLookup{belongs: true}`-style single-field capability fakes, and plain `==`/`!=` sentinel-error assertions (no testify, no `errors.Is` at this layer).

Create `backend/internal/quotations/service_test.go`:

```go
package quotations_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/quotations"
)

// --- fakes ---

type fakeQuotationRepository struct {
	byID map[string]quotations.Quotation
	next int
}

func newFakeQuotationRepository() *fakeQuotationRepository {
	return &fakeQuotationRepository{byID: make(map[string]quotations.Quotation)}
}

func (f *fakeQuotationRepository) Create(ctx context.Context, q quotations.Quotation) (quotations.Quotation, error) {
	for _, existing := range f.byID {
		if existing.CompanyID != q.CompanyID || existing.QuotationNumber != q.QuotationNumber {
			continue
		}
		if existing.Version == q.Version {
			return quotations.Quotation{}, quotations.ErrVersionConflict
		}
		if existing.Status == quotations.QuotationStatusDraft && q.Status == quotations.QuotationStatusDraft {
			return quotations.Quotation{}, quotations.ErrDraftAlreadyExists
		}
	}
	f.next++
	q.ID = "quotation_" + string(rune('0'+f.next))
	f.byID[q.ID] = q
	return q, nil
}

func (f *fakeQuotationRepository) FindByID(ctx context.Context, companyID, id string) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	return q, nil
}

func (f *fakeQuotationRepository) ListByProject(ctx context.Context, companyID, projectID string) ([]quotations.Quotation, error) {
	var result []quotations.Quotation
	for _, q := range f.byID {
		if q.CompanyID == companyID && q.ProjectID == projectID {
			result = append(result, q)
		}
	}
	return result, nil
}

func (f *fakeQuotationRepository) FindMaxVersion(ctx context.Context, companyID, quotationNumber string) (int, error) {
	max := 0
	for _, q := range f.byID {
		if q.CompanyID == companyID && q.QuotationNumber == quotationNumber && q.Version > max {
			max = q.Version
		}
	}
	return max, nil
}

func (f *fakeQuotationRepository) replaceConditional(companyID, id string, expectedRevision int64, apply func(*quotations.Quotation)) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	if q.Status != quotations.QuotationStatusDraft || q.Revision != expectedRevision {
		return quotations.Quotation{}, quotations.ErrRevisionMismatch
	}
	apply(&q)
	q.Revision++
	f.byID[id] = q
	return q, nil
}

func (f *fakeQuotationRepository) ReplaceLines(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.Lines = updated.Lines
		q.Subtotal = updated.Subtotal
		q.TaxAmount = updated.TaxAmount
		q.Total = updated.Total
		// GeneratedSubtotal deliberately untouched.
	})
}

func (f *fakeQuotationRepository) ReplaceTerms(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.Terms = updated.Terms
		q.PaymentSchedule = updated.PaymentSchedule
		q.Notes = updated.Notes
		q.ValidUntil = updated.ValidUntil
	})
}

func (f *fakeQuotationRepository) ReplaceTax(ctx context.Context, companyID, id string, expectedRevision int64, updated quotations.Quotation) (quotations.Quotation, error) {
	return f.replaceConditional(companyID, id, expectedRevision, func(q *quotations.Quotation) {
		q.TaxMode = updated.TaxMode
		q.TaxLabel = updated.TaxLabel
		q.TaxRateBPS = updated.TaxRateBPS
		q.TaxAmount = updated.TaxAmount
		q.Total = updated.Total
	})
}

func (f *fakeQuotationRepository) Finalize(ctx context.Context, companyID, id string, expectedRevision int64, finalizedAt time.Time) (quotations.Quotation, error) {
	q, ok := f.byID[id]
	if !ok || q.CompanyID != companyID {
		return quotations.Quotation{}, quotations.ErrQuotationNotFound
	}
	if q.Status != quotations.QuotationStatusDraft || q.Revision != expectedRevision {
		return quotations.Quotation{}, quotations.ErrRevisionMismatch
	}
	q.Status = quotations.QuotationStatusFinalized
	q.FinalizedAt = &finalizedAt
	f.byID[id] = q
	return q, nil
}

type fakeQuotationCounterRepository struct {
	counts map[string]int64
}

func newFakeQuotationCounterRepository() *fakeQuotationCounterRepository {
	return &fakeQuotationCounterRepository{counts: map[string]int64{}}
}

func (f *fakeQuotationCounterRepository) NextQuotationNumber(ctx context.Context, companyID string) (int64, error) {
	f.counts[companyID]++
	return f.counts[companyID], nil
}

type fakeProjectLookup struct {
	belongs  bool
	clientID string
}

func (f fakeProjectLookup) ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error) {
	return f.belongs, nil
}
func (f fakeProjectLookup) GetProjectClientID(ctx context.Context, companyID, projectID string) (string, error) {
	if !f.belongs {
		return "", quotations.ErrProjectNotFound
	}
	return f.clientID, nil
}

type fakeWorkItemLookup struct {
	descriptions map[string]string // workItemID -> description; absent key means not found
}

func (f fakeWorkItemLookup) GetWorkItemDescription(ctx context.Context, companyID, projectID, workItemID string) (string, bool, error) {
	desc, ok := f.descriptions[workItemID]
	return desc, ok, nil
}

// fakeEstimateSource simulates estimates.Service.VisitQuotationSeeds
// against a fixed, pre-configured eligibility/seed script — never
// carrying any cost figure, matching the real method's return contract
// exactly.
type fakeEstimateSource struct {
	found               bool
	projectMatches      bool
	finalized           bool
	allocationEligible  bool
	proposedSellingPrice money.Money
	currency            string
	seeds               []struct {
		workItemID *string
		amount     money.Money
	}
}

func (f fakeEstimateSource) VisitQuotationSeeds(ctx context.Context, companyID, projectID, estimateID string,
	visit func(workItemID *string, allocatedSellingAmount money.Money) error,
) (money.Money, string, bool, bool, bool, bool, error) {
	if !f.found || !f.projectMatches || !f.finalized || !f.allocationEligible {
		return money.Money{}, "", f.found, f.projectMatches, f.finalized, f.allocationEligible, nil
	}
	for _, s := range f.seeds {
		if err := visit(s.workItemID, s.amount); err != nil {
			return money.Money{}, "", false, false, false, false, err
		}
	}
	return f.proposedSellingPrice, f.currency, true, true, true, true, nil
}

func workItemIDPtr(s string) *string { return &s }

func eligibleEstimateSource() fakeEstimateSource {
	return fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(62500, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(50000, "MYR")},
			{workItemID: workItemIDPtr("work_2"), amount: money.New(12500, "MYR")},
		},
	}
}

func defaultWorkItemLookup() fakeWorkItemLookup {
	return fakeWorkItemLookup{descriptions: map[string]string{
		"work_1": "Wall Tiles", "work_2": "Floor Tiles",
	}}
}

// --- CreateQuotation ---

func TestCreateQuotationHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	q, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q.QuotationNumber != "QT-000001" {
		t.Fatalf("expected QT-000001, got %s", q.QuotationNumber)
	}
	if q.Version != 1 {
		t.Fatalf("expected Version 1, got %d", q.Version)
	}
	if q.Status != quotations.QuotationStatusDraft {
		t.Fatal("expected draft status")
	}
	if q.ClientID != "client_1" {
		t.Fatalf("expected ClientID client_1, got %s", q.ClientID)
	}
	if len(q.Lines) != 2 {
		t.Fatalf("expected 2 generated lines, got %d", len(q.Lines))
	}
	if q.Lines[0].Description != "Wall Tiles" || q.Lines[1].Description != "Floor Tiles" {
		t.Fatalf("expected descriptions resolved via WorkItemLookup, got %+v", q.Lines)
	}
	sum := int64(0)
	for _, l := range q.Lines {
		sum += l.Amount.Amount
	}
	if sum != q.Subtotal.Amount || sum != q.GeneratedSubtotal.Amount || sum != 62500 {
		t.Fatalf("expected Subtotal == GeneratedSubtotal == sum(lines) == 62500, got sum=%d subtotal=%d generatedSubtotal=%d",
			sum, q.Subtotal.Amount, q.GeneratedSubtotal.Amount)
	}
	if q.TaxMode != quotations.TaxModeNone {
		t.Fatal("expected TaxMode=none by default")
	}
	if q.Total.Amount != q.Subtotal.Amount {
		t.Fatal("expected Total == Subtotal when TaxMode=none")
	}
}

func TestCreateQuotationProjectNotFound(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: false}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

func TestCreateQuotationEstimateNotFound(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateNotFound {
		t.Fatalf("expected ErrEstimateNotFound, got %v", err)
	}
}

func TestCreateQuotationEstimateProjectMismatch(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateProjectMismatch {
		t.Fatalf("expected ErrEstimateProjectMismatch, got %v", err)
	}
}

func TestCreateQuotationEstimateNotFinalized(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: true, finalized: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrEstimateNotFinalized {
		t.Fatalf("expected ErrEstimateNotFinalized, got %v", err)
	}
}

func TestCreateQuotationIneligibleCostBasis(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{found: true, projectMatches: true, finalized: true, allocationEligible: false}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	_, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != quotations.ErrIneligibleCostBasisForQuotation {
		t.Fatalf("expected ErrIneligibleCostBasisForQuotation, got %v", err)
	}
}

func TestCreateQuotationWorkItemLessLineDefaultDescription(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(50000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: nil, amount: money.New(50000, "MYR")},
		},
	}
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	q, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(q.Lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(q.Lines))
	}
	if q.Lines[0].Description != "General Project Works and Services" {
		t.Fatalf("expected default WorkItem-less description, got %q", q.Lines[0].Description)
	}
	if len(q.Lines[0].SourceWorkItemIDs) != 0 {
		t.Fatalf("expected empty SourceWorkItemIDs for a WorkItem-less line, got %v", q.Lines[0].SourceWorkItemIDs)
	}
}

func TestCreateQuotationSequentialNumbering(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	first, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := svc.CreateQuotation(context.Background(), "company_a", "project_2", "estimate_2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.QuotationNumber != "QT-000001" || second.QuotationNumber != "QT-000002" {
		t.Fatalf("expected sequential numbering, got %s then %s", first.QuotationNumber, second.QuotationNumber)
	}
}

// --- ReplaceLines ---

func decimalPtr(s string) *decimal.Decimal {
	d := decimal.RequireFromString(s)
	return &d
}
func stringPtr(s string) *string { return &s }

func TestReplaceLinesHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_2"}, Description: "Combined Tiling Works", Amount: money.New(60000, "MYR")},
	}
	updated, err := svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(updated.Lines) != 1 {
		t.Fatalf("expected 1 merged line, got %d", len(updated.Lines))
	}
	if updated.Subtotal.Amount != 60000 {
		t.Fatalf("expected Subtotal recomputed to 60000, got %d", updated.Subtotal.Amount)
	}
	if updated.GeneratedSubtotal.Amount != created.GeneratedSubtotal.Amount {
		t.Fatalf("expected GeneratedSubtotal to remain frozen at %d, got %d", created.GeneratedSubtotal.Amount, updated.GeneratedSubtotal.Amount)
	}
	if updated.Revision != created.Revision+1 {
		t.Fatalf("expected Revision incremented, got %d", updated.Revision)
	}
}

func TestReplaceLinesSplitAcrossTwoLines(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Split work_1's value across two lines — the same WorkItemID appears
	// on both, which must be accepted (design spec §5.3).
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles - Supply", Amount: money.New(30000, "MYR")},
		{SourceWorkItemIDs: []string{"work_1"}, Description: "Wall Tiles - Install", Amount: money.New(20000, "MYR")},
	}
	updated, err := svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error splitting one WorkItem across two lines: %v", err)
	}
	if len(updated.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(updated.Lines))
	}
}

func TestReplaceLinesEmptyArrayRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, nil, created.Revision)
	if err != quotations.ErrEmptyLines {
		t.Fatalf("expected ErrEmptyLines, got %v", err)
	}
}

func TestReplaceLinesDuplicateWorkItemInOneLineRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_1"}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrDuplicateWorkItemIDInLine {
		t.Fatalf("expected ErrDuplicateWorkItemIDInLine, got %v", err)
	}
}

func TestReplaceLinesUnknownWorkItemRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"nonexistent_work_item"}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrWorkItemNotFound {
		t.Fatalf("expected ErrWorkItemNotFound, got %v", err)
	}
}

func TestReplaceLinesUnknownLineIDRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{ID: "line-that-does-not-exist", SourceWorkItemIDs: []string{}, Description: "Bad Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrUnknownLineID {
		t.Fatalf("expected ErrUnknownLineID, got %v", err)
	}
}

func TestReplaceLinesDuplicateLineIDRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	existingID := created.Lines[0].ID

	newLines := []quotations.QuotationLine{
		{ID: existingID, SourceWorkItemIDs: []string{"work_1"}, Description: "A", Amount: money.New(10000, "MYR")},
		{ID: existingID, SourceWorkItemIDs: []string{"work_2"}, Description: "B", Amount: money.New(20000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrDuplicateLineID {
		t.Fatalf("expected ErrDuplicateLineID, got %v", err)
	}
}

func TestReplaceLinesBlankDescriptionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "   ", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrBlankLineDescription {
		t.Fatalf("expected ErrBlankLineDescription, got %v", err)
	}
}

func TestReplaceLinesIncompletePricingRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Partial", Quantity: decimalPtr("30"), Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrIncompleteLinePricing {
		t.Fatalf("expected ErrIncompleteLinePricing, got %v", err)
	}
}

func TestReplaceLinesZeroUnitPriceRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	zero := money.New(0, "MYR")
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Free Item", Quantity: decimalPtr("1"), Unit: stringPtr("unit"), UnitPrice: &zero, Amount: money.New(0, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrInvalidLineUnitPrice && err != quotations.ErrLineAmountNotPositive {
		t.Fatalf("expected ErrInvalidLineUnitPrice or ErrLineAmountNotPositive for a zero unit price, got %v", err)
	}
}

func TestReplaceLinesLineAmountMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	unitPrice := money.New(12000, "MYR") // RM120/m2
	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Floor Tiles", Quantity: decimalPtr("30"), Unit: stringPtr("m2"), UnitPrice: &unitPrice, Amount: money.New(999999, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrLineAmountMismatch {
		t.Fatalf("expected ErrLineAmountMismatch, got %v", err)
	}
}

func TestReplaceLinesCurrencyMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Wrong Currency", Amount: money.New(10000, "USD")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrLineCurrencyMismatch {
		t.Fatalf("expected ErrLineCurrencyMismatch, got %v", err)
	}

	// Draft must be left completely untouched by the rejected write.
	unchanged, err := svc.GetQuotation(context.Background(), "company_a", created.ID)
	if err != nil {
		t.Fatalf("unexpected error re-fetching: %v", err)
	}
	if unchanged.Revision != created.Revision {
		t.Fatal("expected the draft's Revision to be unchanged after a rejected line submission")
	}
	if len(unchanged.Lines) != len(created.Lines) {
		t.Fatal("expected the draft's Lines to be unchanged after a rejected line submission")
	}
}

func TestReplaceLinesStaleRevisionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision+99)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

func TestReplaceLinesFinalizedRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision); err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}

	newLines := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{}, Description: "Line", Amount: money.New(10000, "MYR")},
	}
	_, err = svc.ReplaceLines(context.Background(), "company_a", created.ID, newLines, created.Revision)
	if err != quotations.ErrQuotationNotDraft {
		t.Fatalf("expected ErrQuotationNotDraft, got %v", err)
	}
}

// --- UpdateTax ---

func TestUpdateTaxPercentageHappyPath(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated, err := svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "SST 6%", 600, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedTax := money.ApplyRateBPS(created.Subtotal, 600)
	if updated.TaxAmount.Amount != expectedTax.Amount {
		t.Fatalf("expected TaxAmount %d, got %d", expectedTax.Amount, updated.TaxAmount.Amount)
	}
	expectedTotal := created.Subtotal.Amount + expectedTax.Amount
	if updated.Total.Amount != expectedTotal {
		t.Fatalf("expected Total %d, got %d", expectedTotal, updated.Total.Amount)
	}
}

func TestUpdateTaxNoneWithNonEmptyLabelRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModeNone, "SST 6%", 0, created.Revision)
	if err != quotations.ErrInvalidTaxLabel {
		t.Fatalf("expected ErrInvalidTaxLabel, got %v", err)
	}
}

func TestUpdateTaxNoneWithNonZeroRateRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModeNone, "", 600, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate, got %v", err)
	}
}

func TestUpdateTaxPercentageBlankLabelRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "   ", 600, created.Revision)
	if err != quotations.ErrInvalidTaxLabel {
		t.Fatalf("expected ErrInvalidTaxLabel, got %v", err)
	}
}

func TestUpdateTaxPercentageZeroRateRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "SST 0%", 0, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate for a zero rate under percentage mode, got %v", err)
	}
}

func TestUpdateTaxPercentageAboveMaxRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxModePercentage, "Over 100%", 10001, created.Revision)
	if err != quotations.ErrInvalidTaxRate {
		t.Fatalf("expected ErrInvalidTaxRate for >100%%, got %v", err)
	}
}

func TestUpdateTaxInvalidModeRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.UpdateTax(context.Background(), "company_a", created.ID, quotations.TaxMode("fixed"), "Fixed Tax", 100, created.Revision)
	if err != quotations.ErrInvalidTaxMode {
		t.Fatalf("expected ErrInvalidTaxMode, got %v", err)
	}
}

// --- FinalizeQuotation ---

func TestFinalizeQuotationIdempotent(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Retry with a deliberately wrong expectedRevision — idempotent
	// finalize must succeed anyway (design spec §12).
	second, err := svc.FinalizeQuotation(context.Background(), "company_a", created.ID, 999)
	if err != nil {
		t.Fatalf("unexpected error on idempotent retry: %v", err)
	}
	if second.Status != quotations.QuotationStatusFinalized || second.Revision != first.Revision {
		t.Fatal("expected idempotent finalize retry to return the unchanged finalized document")
	}
}

func TestFinalizeQuotationStaleRevisionRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.FinalizeQuotation(context.Background(), "company_a", created.ID, created.Revision+99)
	if err != quotations.ErrRevisionMismatch {
		t.Fatalf("expected ErrRevisionMismatch, got %v", err)
	}
}

// --- CreateNewVersion ---

func TestCreateNewVersionRequiresFinalizedSource(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.CreateNewVersion(context.Background(), "company_a", created.ID, "estimate_2")
	if err != quotations.ErrQuotationMustBeFinalizedBeforeNewVersion {
		t.Fatalf("expected ErrQuotationMustBeFinalizedBeforeNewVersion, got %v", err)
	}
}

func TestCreateNewVersionFreshRegenerationNotCarriedForward(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := eligibleEstimateSource()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	v1, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Hand-edit V1's lines away from their generated values before
	// finalizing.
	handEdited := []quotations.QuotationLine{
		{SourceWorkItemIDs: []string{"work_1", "work_2"}, Description: "Combined", Amount: money.New(99999, "MYR")},
	}
	v1, err = svc.ReplaceLines(context.Background(), "company_a", v1.ID, handEdited, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error hand-editing: %v", err)
	}
	v1, err = svc.FinalizeQuotation(context.Background(), "company_a", v1.ID, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing v1: %v", err)
	}

	// A DIFFERENT Estimate, with different underlying seed amounts,
	// referenced explicitly for V2.
	source2 := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(80000, "MYR"), currency: "MYR",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(80000, "MYR")},
		},
	}
	svc2 := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source2)

	v2, err := svc2.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_2")
	if err != nil {
		t.Fatalf("unexpected error creating v2: %v", err)
	}
	if v2.Version != 2 {
		t.Fatalf("expected Version 2, got %d", v2.Version)
	}
	if v2.EstimateID != "estimate_2" {
		t.Fatalf("expected EstimateID estimate_2, got %s", v2.EstimateID)
	}
	if v2.GeneratedSubtotal.Amount != 80000 {
		t.Fatalf("expected a FRESH GeneratedSubtotal of 80000 from the new Estimate, NOT v1's hand-edited 99999, got %d", v2.GeneratedSubtotal.Amount)
	}
	if v2.QuotationNumber != v1.QuotationNumber {
		t.Fatal("expected QuotationNumber to be carried forward unchanged")
	}
}

func TestCreateNewVersionCurrencyMismatchRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	source := eligibleEstimateSource()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), source)

	v1, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v1, err = svc.FinalizeQuotation(context.Background(), "company_a", v1.ID, v1.Revision)
	if err != nil {
		t.Fatalf("unexpected error finalizing: %v", err)
	}

	sgdSource := fakeEstimateSource{
		found: true, projectMatches: true, finalized: true, allocationEligible: true,
		proposedSellingPrice: money.New(80000, "SGD"), currency: "SGD",
		seeds: []struct {
			workItemID *string
			amount     money.Money
		}{
			{workItemID: workItemIDPtr("work_1"), amount: money.New(80000, "SGD")},
		},
	}
	svc2 := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), sgdSource)

	_, err = svc2.CreateNewVersion(context.Background(), "company_a", v1.ID, "estimate_sgd")
	if err != quotations.ErrQuotationCurrencyMismatch {
		t.Fatalf("expected ErrQuotationCurrencyMismatch, got %v", err)
	}
}

// --- ListQuotationsByProject ---

func TestListQuotationsByProjectForeignProjectRejected(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: false}, defaultWorkItemLookup(), eligibleEstimateSource())

	_, err := svc.ListQuotationsByProject(context.Background(), "company_a", "project_1")
	if err != quotations.ErrProjectNotFound {
		t.Fatalf("expected ErrProjectNotFound, got %v", err)
	}
}

// --- cross-tenant ---

func TestGetQuotationCrossTenant(t *testing.T) {
	repo := newFakeQuotationRepository()
	counterRepo := newFakeQuotationCounterRepository()
	svc := quotations.NewService(repo, counterRepo, fakeProjectLookup{belongs: true, clientID: "client_1"}, defaultWorkItemLookup(), eligibleEstimateSource())

	created, err := svc.CreateQuotation(context.Background(), "company_a", "project_1", "estimate_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = svc.GetQuotation(context.Background(), "company_b", created.ID)
	if err != quotations.ErrQuotationNotFound {
		t.Fatalf("expected ErrQuotationNotFound for a cross-tenant lookup, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail first, then pass**

Run: `cd backend && go test ./internal/quotations/... -run "TestCreateQuotation|TestReplaceLines|TestUpdateTax|TestFinalizeQuotation|TestCreateNewVersion|TestListQuotations|TestGetQuotation" -v`
Expected: initially FAIL (or fail to compile) if `service.go` has any signature mismatch against what these tests assume — fix `service.go` (not the tests, unless a test itself has a genuine bug) until every test passes. This step doubles as the final correctness check on Task 8's implementation, since Task 8 itself had no dedicated tests until now.

- [ ] **Step 3: Run the full `quotations` package test suite**

Run: `cd backend && go test ./internal/quotations/... -v`
Expected: PASS — every unit test (this file) and every Testcontainers integration test (Task 7's file) green together.

- [ ] **Step 4: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 10: `quotations` HTTP handlers, DTOs, error mapping

**Files:**
- Create: `backend/internal/quotations/handler.go`

**Interfaces:**
- Consumes: `Service` (Task 8), `identity.PrincipalFromContext` (existing platform package), `huma.Register`/`huma.Operation`/`huma.Error*` (existing Huma v2 usage, M4 pattern).
- Produces (used by Task 11's composition root):
  ```go
  func RegisterHandlers(api huma.API, svc *Service)
  ```

No dedicated unit test for this file — matching M4's own `estimates/handler.go`, which has no `handler_test.go`; HTTP-layer correctness is proven entirely by Task 12's `tenanttest` acceptance tests exercising real HTTP requests against a real router.

Design spec reference: §23 (endpoint table, verbatim paths/methods/purposes), §23.1 (idempotency — no code changes needed, informational), §25 (DTOs and error→status mapping, verbatim), §13.1 (PUT /lines request DTO, verbatim).

- [ ] **Step 1: Write the handler file**

Read `backend/internal/estimates/handler.go` in full first (already read this session) — mirror its exact structure: `RegisterHandlers(api huma.API, svc *Service)` function containing one `huma.Register(api, huma.Operation{...}, func(ctx, input) (*output, error) {...})` call per endpoint, a `to<Entity>DTO` mapping function, and a `map<Module>Error(err error) error` function using `errors.Is` (contrast with the `service_test.go`/`repository_mongo_test.go` layers, which compare sentinels with plain `==`).

Create `backend/internal/quotations/handler.go`:

```go
package quotations

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type quotationMoneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type quotationLineDTO struct {
	ID                string             `json:"id"`
	SourceWorkItemIDs []string           `json:"sourceWorkItemIds"`
	Description       string             `json:"description"`
	Quantity          string             `json:"quantity,omitempty"`
	Unit              string             `json:"unit,omitempty"`
	UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
	Amount            quotationMoneyDTO  `json:"amount"`
	SortOrder         int                `json:"sortOrder"`
}

type quotationDTO struct {
	ID                string             `json:"id"`
	ProjectID         string             `json:"projectId"`
	ClientID          string             `json:"clientId"`
	EstimateID        string             `json:"estimateId"`
	QuotationNumber   string             `json:"quotationNumber"`
	Version           int                `json:"version"`
	Status            string             `json:"status"`
	Revision          int64              `json:"revision"`
	Currency          string             `json:"currency"`
	Lines             []quotationLineDTO `json:"lines"`
	Subtotal          quotationMoneyDTO  `json:"subtotal"`
	GeneratedSubtotal quotationMoneyDTO  `json:"generatedSubtotal"`
	TaxMode           string             `json:"taxMode"`
	TaxLabel          string             `json:"taxLabel,omitempty"`
	TaxRateBPS        int64              `json:"taxRateBps,omitempty"`
	TaxAmount         quotationMoneyDTO  `json:"taxAmount"`
	Total             quotationMoneyDTO  `json:"total"`
	Terms             string             `json:"terms,omitempty"`
	PaymentSchedule   string             `json:"paymentSchedule,omitempty"`
	ValidUntil        string             `json:"validUntil,omitempty"`
	Notes             string             `json:"notes,omitempty"`
	CreatedAt         string             `json:"createdAt"`
	FinalizedAt       string             `json:"finalizedAt,omitempty"`
}

type createQuotationInput struct {
	Body struct {
		ProjectID  string `json:"projectId" required:"true" minLength:"1"`
		EstimateID string `json:"estimateId" required:"true" minLength:"1"`
	}
}

type quotationOutput struct {
	Body quotationDTO
}

type listQuotationsInput struct {
	ProjectID string `query:"projectId" required:"true"`
}

type listQuotationsOutput struct {
	Body struct {
		Quotations []quotationDTO `json:"quotations"`
	}
}

type getQuotationInput struct {
	ID string `path:"id"`
}

type quotationLineInput struct {
	ID                string             `json:"id,omitempty"`
	SourceWorkItemIDs []string           `json:"sourceWorkItemIds"`
	Description       string             `json:"description" required:"true"`
	Quantity          *string            `json:"quantity,omitempty"`
	Unit              *string            `json:"unit,omitempty"`
	UnitPrice         *quotationMoneyDTO `json:"unitPrice,omitempty"`
	Amount            quotationMoneyDTO  `json:"amount" required:"true"`
	SortOrder         int                `json:"sortOrder"`
}

type replaceLinesInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64                 `json:"expectedRevision" required:"true"`
		Lines            []quotationLineInput  `json:"lines" required:"true"`
	}
}

type updateTermsInput struct {
	ID   string `path:"id"`
	Body struct {
		Terms            string `json:"terms"`
		PaymentSchedule  string `json:"paymentSchedule"`
		Notes            string `json:"notes"`
		ValidUntil       string `json:"validUntil,omitempty"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type updateTaxInput struct {
	ID   string `path:"id"`
	Body struct {
		TaxMode          string `json:"taxMode" required:"true"`
		TaxLabel         string `json:"taxLabel"`
		TaxRateBPS       int64  `json:"taxRateBps"`
		ExpectedRevision int64  `json:"expectedRevision" required:"true"`
	}
}

type finalizeQuotationInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type createNewQuotationVersionInput struct {
	ID   string `path:"id"`
	Body struct {
		EstimateID string `json:"estimateId" required:"true" minLength:"1"`
	}
}

// RegisterHandlers registers POST /quotations, GET /quotations,
// GET /quotations/{id}, PUT /quotations/{id}/lines,
// PATCH /quotations/{id}/terms, PATCH /quotations/{id}/tax,
// POST /quotations/{id}/finalize, and POST /quotations/{id}/versions on
// api, backed by svc.
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "quotations-create",
		Method:      http.MethodPost,
		Path:        "/quotations",
		Summary:     "Create Version 1 for a fresh commercial quotation chain, always as a draft",
	}, func(ctx context.Context, input *createQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.CreateQuotation(ctx, principal.CompanyID, input.Body.ProjectID, input.Body.EstimateID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-list",
		Method:      http.MethodGet,
		Path:        "/quotations",
		Summary:     "List every version of every quotation chain for one Project",
	}, func(ctx context.Context, input *listQuotationsInput) (*listQuotationsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		list, err := svc.ListQuotationsByProject(ctx, principal.CompanyID, input.ProjectID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		resp := &listQuotationsOutput{}
		for _, q := range list {
			resp.Body.Quotations = append(resp.Body.Quotations, toQuotationDTO(q))
		}
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-get",
		Method:      http.MethodGet,
		Path:        "/quotations/{id}",
		Summary:     "Get one specific Quotation version, tenant-scoped",
	}, func(ctx context.Context, input *getQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.GetQuotation(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-replace-lines",
		Method:      http.MethodPut,
		Path:        "/quotations/{id}/lines",
		Summary:     "Replace the entire Lines array on a draft Quotation",
	}, func(ctx context.Context, input *replaceLinesInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		lines, err := fromQuotationLineInputs(input.Body.Lines)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		q, err := svc.ReplaceLines(ctx, principal.CompanyID, input.ID, lines, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-update-terms",
		Method:      http.MethodPatch,
		Path:        "/quotations/{id}/terms",
		Summary:     "Update Terms/PaymentSchedule/Notes/ValidUntil on a draft Quotation",
	}, func(ctx context.Context, input *updateTermsInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		var validUntil *time.Time
		if input.Body.ValidUntil != "" {
			parsed, parseErr := time.Parse(timeLayout, input.Body.ValidUntil)
			if parseErr != nil {
				return nil, huma.Error422UnprocessableEntity("invalid validUntil timestamp")
			}
			validUntil = &parsed
		}
		q, err := svc.UpdateTerms(ctx, principal.CompanyID, input.ID, input.Body.Terms, input.Body.PaymentSchedule,
			input.Body.Notes, validUntil, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-update-tax",
		Method:      http.MethodPatch,
		Path:        "/quotations/{id}/tax",
		Summary:     "Update TaxMode/TaxLabel/TaxRateBPS on a draft Quotation",
	}, func(ctx context.Context, input *updateTaxInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.UpdateTax(ctx, principal.CompanyID, input.ID, TaxMode(input.Body.TaxMode),
			input.Body.TaxLabel, money.RateBPS(input.Body.TaxRateBPS), input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-finalize",
		Method:      http.MethodPost,
		Path:        "/quotations/{id}/finalize",
		Summary:     "draft -> finalized, one-directional, locks every field. Idempotent on retry.",
	}, func(ctx context.Context, input *finalizeQuotationInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.FinalizeQuotation(ctx, principal.CompanyID, input.ID, input.Body.ExpectedRevision)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "quotations-create-version",
		Method:      http.MethodPost,
		Path:        "/quotations/{id}/versions",
		Summary:     "Create the next Version as a new draft — source must already be finalized",
	}, func(ctx context.Context, input *createNewQuotationVersionInput) (*quotationOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		q, err := svc.CreateNewVersion(ctx, principal.CompanyID, input.ID, input.Body.EstimateID)
		if err != nil {
			return nil, mapQuotationsError(err)
		}
		return &quotationOutput{Body: toQuotationDTO(q)}, nil
	})
}

func fromQuotationLineInputs(inputs []quotationLineInput) ([]QuotationLine, error) {
	lines := make([]QuotationLine, len(inputs))
	for i, in := range inputs {
		var qty *decimal.Decimal
		if in.Quantity != nil {
			parsed, err := decimal.NewFromString(*in.Quantity)
			if err != nil {
				return nil, ErrInvalidLineQuantity
			}
			qty = &parsed
		}
		var unitPrice *money.Money
		if in.UnitPrice != nil {
			m := money.New(in.UnitPrice.Amount, in.UnitPrice.Currency)
			unitPrice = &m
		}
		sourceIDs := in.SourceWorkItemIDs
		if sourceIDs == nil {
			sourceIDs = []string{}
		}
		lines[i] = QuotationLine{
			ID: in.ID, SourceWorkItemIDs: sourceIDs, Description: in.Description,
			Quantity: qty, Unit: in.Unit, UnitPrice: unitPrice,
			Amount: money.New(in.Amount.Amount, in.Amount.Currency), SortOrder: in.SortOrder,
		}
	}
	return lines, nil
}

func toQuotationDTO(q Quotation) quotationDTO {
	dto := quotationDTO{
		ID: q.ID, ProjectID: q.ProjectID, ClientID: q.ClientID, EstimateID: q.EstimateID,
		QuotationNumber: q.QuotationNumber, Version: q.Version, Status: string(q.Status), Revision: q.Revision,
		Currency: q.Currency,
		Subtotal: quotationMoneyDTO{Amount: q.Subtotal.Amount, Currency: q.Subtotal.Currency},
		GeneratedSubtotal: quotationMoneyDTO{Amount: q.GeneratedSubtotal.Amount, Currency: q.GeneratedSubtotal.Currency},
		TaxMode: string(q.TaxMode), TaxLabel: q.TaxLabel, TaxRateBPS: int64(q.TaxRateBPS),
		TaxAmount: quotationMoneyDTO{Amount: q.TaxAmount.Amount, Currency: q.TaxAmount.Currency},
		Total:     quotationMoneyDTO{Amount: q.Total.Amount, Currency: q.Total.Currency},
		Terms:     q.Terms, PaymentSchedule: q.PaymentSchedule, Notes: q.Notes,
		CreatedAt: q.CreatedAt.Format(timeLayout),
	}
	for _, l := range q.Lines {
		lineDTO := quotationLineDTO{
			ID: l.ID, SourceWorkItemIDs: l.SourceWorkItemIDs, Description: l.Description,
			Amount: quotationMoneyDTO{Amount: l.Amount.Amount, Currency: l.Amount.Currency}, SortOrder: l.SortOrder,
		}
		if l.Quantity != nil {
			lineDTO.Quantity = l.Quantity.String()
		}
		if l.Unit != nil {
			lineDTO.Unit = *l.Unit
		}
		if l.UnitPrice != nil {
			lineDTO.UnitPrice = &quotationMoneyDTO{Amount: l.UnitPrice.Amount, Currency: l.UnitPrice.Currency}
		}
		dto.Lines = append(dto.Lines, lineDTO)
	}
	if q.ValidUntil != nil {
		dto.ValidUntil = q.ValidUntil.Format(timeLayout)
	}
	if q.FinalizedAt != nil {
		dto.FinalizedAt = q.FinalizedAt.Format(timeLayout)
	}
	return dto
}

func mapQuotationsError(err error) error {
	switch {
	case errors.Is(err, ErrQuotationNotFound):
		return huma.Error404NotFound("quotation not found")
	case errors.Is(err, ErrProjectNotFound):
		return huma.Error404NotFound("project not found")
	case errors.Is(err, ErrEstimateNotFound):
		return huma.Error404NotFound("estimate not found")
	case errors.Is(err, ErrWorkItemNotFound):
		return huma.Error404NotFound("work item not found")
	case errors.Is(err, ErrEstimateNotFinalized):
		return huma.Error422UnprocessableEntity("source estimate must be finalized")
	case errors.Is(err, ErrEstimateProjectMismatch):
		return huma.Error422UnprocessableEntity("estimate does not belong to this project")
	case errors.Is(err, ErrIneligibleCostBasisForQuotation):
		return huma.Error422UnprocessableEntity("estimate's cost basis cannot be allocated into a quotation")
	case errors.Is(err, ErrQuotationCurrencyMismatch):
		return huma.Error422UnprocessableEntity("referenced estimate's currency does not match this quotation chain")
	case errors.Is(err, ErrLineCurrencyMismatch):
		return huma.Error422UnprocessableEntity("line amount/unitPrice currency does not match this quotation's currency")
	case errors.Is(err, ErrIncompleteLinePricing):
		return huma.Error422UnprocessableEntity("quantity, unit, and unitPrice must all be supplied together or not at all")
	case errors.Is(err, ErrInvalidLineQuantity):
		return huma.Error422UnprocessableEntity("quantity must be strictly positive")
	case errors.Is(err, ErrInvalidLineUnitPrice):
		return huma.Error422UnprocessableEntity("unitPrice must be strictly positive")
	case errors.Is(err, ErrLineAmountMismatch):
		return huma.Error422UnprocessableEntity("line amount does not match quantity x unitPrice")
	case errors.Is(err, ErrLineAmountNotPositive):
		return huma.Error422UnprocessableEntity("line amount must be strictly positive")
	case errors.Is(err, ErrEmptyLines):
		return huma.Error422UnprocessableEntity("at least one line is required")
	case errors.Is(err, ErrDuplicateWorkItemIDInLine):
		return huma.Error422UnprocessableEntity("sourceWorkItemIds must not contain the same work item twice")
	case errors.Is(err, ErrUnknownLineID):
		return huma.Error422UnprocessableEntity("line id does not belong to this quotation")
	case errors.Is(err, ErrDuplicateLineID):
		return huma.Error422UnprocessableEntity("duplicate line id in submitted lines")
	case errors.Is(err, ErrBlankLineDescription):
		return huma.Error422UnprocessableEntity("line description must not be blank")
	case errors.Is(err, ErrInvalidTaxMode):
		return huma.Error422UnprocessableEntity("invalid tax mode")
	case errors.Is(err, ErrInvalidTaxRate):
		return huma.Error422UnprocessableEntity("invalid tax rate for the given tax mode")
	case errors.Is(err, ErrInvalidTaxLabel):
		return huma.Error422UnprocessableEntity("invalid tax label for the given tax mode")
	case errors.Is(err, ErrQuotationNotDraft):
		return huma.Error409Conflict("quotation is not a draft")
	case errors.Is(err, ErrQuotationMustBeFinalizedBeforeNewVersion):
		return huma.Error409Conflict("source quotation must be finalized before creating a new version")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("revision mismatch: the quotation has changed since it was last read")
	case errors.Is(err, ErrVersionConflict):
		return huma.Error409Conflict("version allocation conflict; please retry")
	case errors.Is(err, ErrDraftAlreadyExists):
		return huma.Error409Conflict("a draft already exists for this quotation chain")
	case errors.Is(err, ErrUnclassifiedDuplicateKey):
		// Deliberately mapped to 500, not 409 — an unclassified
		// duplicate-key error is, by definition, one this code could not
		// positively identify as either known safe-to-surface case.
		return err
	default:
		return err
	}
}
```

**Import note:** the code above uses `time.Time`/`time.Parse` (`updateTermsInput`'s `ValidUntil` parsing) and `decimal.Decimal`/`decimal.NewFromString` (`fromQuotationLineInputs`) — add `"time"` and `"github.com/shopspring/decimal"` to the import block at the top of the file (both omitted from the code listing above by oversight; add them alongside the existing `context`/`errors`/`net/http`/huma/money/identity imports before running `go build`).

- [ ] **Step 2: Confirm the package builds**

Run: `cd backend && go build ./internal/quotations/...`
Expected: exits 0, no output. Fix the missing-import issue flagged above first, then any other compile errors (e.g. `gofmt -w backend/internal/quotations/handler.go` to fix struct-tag alignment).

- [ ] **Step 3: Run the full package test suite once more**

Run: `cd backend && go test ./internal/quotations/... -v`
Expected: PASS — unchanged from Task 9 (this task added no new tests, only the HTTP layer), confirming the handler file didn't break anything by compiling incorrectly against `service.go`.

- [ ] **Step 4: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 11: Composition root wiring — `internal/tenanttest/router.go` and `cmd/api/main.go`

**Files:**
- Modify: `backend/internal/tenanttest/router.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: everything from Tasks 3-10.
- Produces: a fully-wired HTTP router (both the test router and the production composition root) exposing all 8 M5 endpoints alongside every M0-M4 endpoint, on the single shared `authedAPI` group.

No new test in this task — `tenanttest/router.go`'s correctness is proven by Task 12's acceptance tests; `main.go`'s correctness is proven by Task 13's live-server smoke check.

Design spec reference: §22 (construction order, verbatim `NewService` call), §23 (no second `huma.API`).

- [ ] **Step 1: Wire `internal/tenanttest/router.go`**

Read `backend/internal/tenanttest/router.go` in full first (already read in full this session — confirmed current state, 113 lines).

Apply these five changes to `backend/internal/tenanttest/router.go`:

1. Add to the import block (alongside the existing `"github.com/shananth/renovation-platform/backend/internal/estimates"` line, keeping imports alphabetically grouped as the file already does):
```go
"github.com/shananth/renovation-platform/backend/internal/quotations"
```

2. After the existing line `estimateRepo := estimates.NewMongoEstimateRepository(db)`, add:
```go
quotationRepo := quotations.NewMongoQuotationRepository(db)
quotationCounterRepo := quotations.NewMongoQuotationCounterRepository(db)
```

3. In the `EnsureIndexes` slice literal, after the existing `estimateRepo.EnsureIndexes,` line, add:
```go
quotationRepo.EnsureIndexes, quotationCounterRepo.EnsureIndexes,
```

4. After the existing line `estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)`, add:
```go
quotationsService := quotations.NewService(quotationRepo, quotationCounterRepo, projectsService, workService, estimatesService)
```

5. After the existing line `estimates.RegisterHandlers(authedAPI, estimatesService)`, add:
```go
quotations.RegisterHandlers(authedAPI, quotationsService)
```

The full modified relevant sections of `backend/internal/tenanttest/router.go` should read (showing only the changed regions in context — the rest of the file is unchanged):

```go
import (
	// ... existing imports unchanged ...
	"github.com/shananth/renovation-platform/backend/internal/estimates"
	// ... other existing imports unchanged ...
	"github.com/shananth/renovation-platform/backend/internal/quotations"
	// ... remaining existing imports unchanged ...
)
```

```go
	estimateRepo := estimates.NewMongoEstimateRepository(db)
	quotationRepo := quotations.NewMongoQuotationRepository(db)
	quotationCounterRepo := quotations.NewMongoQuotationCounterRepository(db)

	for _, ensure := range []func(context.Context) error{
		userRepo.EnsureIndexes, sessionRepo.EnsureIndexes, membershipRepo.EnsureIndexes,
		clientRepo.EnsureIndexes, projectRepo.EnsureIndexes, propertyRepo.EnsureIndexes,
		spaceRepo.EnsureIndexes, workItemRepo.EnsureIndexes,
		materialRepo.EnsureIndexes, costItemRepo.EnsureIndexes, workerRepo.EnsureIndexes, labourEntryRepo.EnsureIndexes,
		estimateRepo.EnsureIndexes,
		quotationRepo.EnsureIndexes, quotationCounterRepo.EnsureIndexes,
	} {
		if err := ensure(ctx); err != nil {
			return nil, err
		}
	}
```

```go
	estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)
	quotationsService := quotations.NewService(quotationRepo, quotationCounterRepo, projectsService, workService, estimatesService)
```

```go
	estimates.RegisterHandlers(authedAPI, estimatesService)
	quotations.RegisterHandlers(authedAPI, quotationsService)

	return router, nil
}
```

- [ ] **Step 2: Confirm the `tenanttest` package builds**

Run: `cd backend && go build ./internal/tenanttest/...`
Expected: exits 0, no output.

- [ ] **Step 3: Wire `cmd/api/main.go` identically**

Read `backend/cmd/api/main.go` in full first (already read in full this session — confirmed current state, 230 lines). Apply the identical five changes as Step 1, adapted to `main.go`'s own variable-naming and comment style (it uses full sentences in comments above each composition-root block, e.g. `// --- Composition root: Milestone 4 wiring. ... ---`, unlike `tenanttest/router.go`'s terser style):

1. Add import (alongside the existing `"github.com/shananth/renovation-platform/backend/internal/estimates"` line):
```go
"github.com/shananth/renovation-platform/backend/internal/quotations"
```

2. After the existing `// --- Repositories (Milestone 4: estimates) ---` block's `estimateRepo := estimates.NewMongoEstimateRepository(db)` line, add a new labeled block:
```go
	// --- Repositories (Milestone 5: quotations) ---
	quotationRepo := quotations.NewMongoQuotationRepository(db)
	quotationCounterRepo := quotations.NewMongoQuotationCounterRepository(db)
```

3. After the existing `if err := estimateRepo.EnsureIndexes(indexCtx); err != nil { ... }` block, add:
```go
	if err := quotationRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure quotations indexes")
	}
	if err := quotationCounterRepo.EnsureIndexes(indexCtx); err != nil {
		logger.Fatal().Err(err).Msg("failed to ensure quotation_counters indexes")
	}
```

4. After the existing `// --- Composition root: Milestone 4 wiring. ... ---` comment block's `estimatesService := estimates.NewService(estimateRepo, projectsService, costsService)` line, add:
```go

	// --- Composition root: Milestone 5 wiring. quotations consumes
	// ProjectLookup (from projectsService, extended with
	// GetProjectClientID), WorkItemLookup (from workService, extended with
	// GetWorkItemDescription), and FinalizedEstimateSource (from
	// estimatesService's VisitQuotationSeeds method, which performs the
	// cost-grouping and proportional-allocation computation internally —
	// matching
	// docs/superpowers/specs/2026-07-24-milestone-5-quotations-design.md §22.
	quotationsService := quotations.NewService(quotationRepo, quotationCounterRepo, projectsService, workService, estimatesService)
```

5. After the existing `estimates.RegisterHandlers(authedAPI, estimatesService)` line, add:
```go
	quotations.RegisterHandlers(authedAPI, quotationsService)
```

- [ ] **Step 4: Confirm the whole backend builds**

Run: `cd backend && go build ./...`
Expected: exits 0, no output — every package, including `cmd/api`, compiles cleanly.

- [ ] **Step 5: Confirm `go vet` is clean**

Run: `cd backend && go vet ./...`
Expected: exits 0, no output.

- [ ] **Step 6: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 12: `tenanttest` full-HTTP acceptance tests

**Files:**
- Modify: `backend/internal/tenanttest/tenant_isolation_test.go`

**Interfaces:**
- Consumes: `setupRouter`, `registerCompany`, `doJSON`, `mustField`, `mustNumberField`, `mustNestedAmount` (all pre-existing helpers, confirmed this session), plus a new local helper this task adds: `buildFinalizedEstimate`.

This is the final proof that the whole system works together over real HTTP against real MongoDB — the acceptance-test matrix from design spec §28's "Full HTTP tenant-isolation acceptance matrix" section.

Design spec reference: §28 (full acceptance-test bullet list, verbatim — this task implements every bullet under that heading).

- [ ] **Step 1: Add a shared setup helper**

Read `backend/internal/tenanttest/tenant_isolation_test.go` in full first (already read/summarized this session — confirm the exact current signatures of `registerCompany`, `doJSON`, `mustField`, `mustNumberField`, `mustNestedAmount`, and `buildProjectWithEstimatedCostItem` before writing anything, since this task depends on all of them matching exactly).

Add this new helper to `backend/internal/tenanttest/tenant_isolation_test.go` (place it near `buildProjectWithEstimatedCostItem`, which it calls):

```go
// buildFinalizedEstimate creates a Project with one estimated CostItem
// attached to a real WorkItem, then creates and finalizes an Estimate
// against it — the minimum real prerequisite chain every M5 Quotation
// acceptance test needs (design spec §3: a Quotation may only reference a
// finalized Estimate). Returns the Project ID, the finalized Estimate ID,
// and the WorkItem ID the CostItem was attached to (needed by tests that
// verify SourceWorkItemIDs traceability).
func buildFinalizedEstimate(t *testing.T, router http.Handler, company testCompany) (projectID, estimateID, workItemID string) {
	t.Helper()

	_, projectID, _, _, workItemID = buildFullHierarchyForCompanyA(t, router, company)

	costItemResp := doJSON(t, router, http.MethodPost, "/cost-items", company.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Ceramic tiles", "estimatedAmount": 500000, "currency": "MYR",
	})
	if costItemResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating cost item, got %d: %s", costItemResp.Code, costItemResp.Body.String())
	}

	createResp := doJSON(t, router, http.MethodPost, "/estimates", company.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating estimate, got %d: %s", createResp.Code, createResp.Body.String())
	}
	estID := mustField(t, createResp, "id")

	finalizeResp := doJSON(t, router, http.MethodPost, "/estimates/"+estID+"/finalize", company.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing estimate, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	return projectID, estID, workItemID
}
```

**Before writing the body above, confirm `buildFullHierarchyForCompanyA`'s actual return signature** by reading it in the same file — the sketch above assumes it returns `(clientID, projectID, propertyID, spaceID, workItemID string)` based on this session's earlier research summary ("Client→Project→Property→Space→WorkItem"); adjust the destructuring on the first line of `buildFinalizedEstimate` to match whatever the real signature actually is if it differs. Similarly confirm the exact `/cost-items` POST body field names (`workItemId`, `estimatedAmount`, etc.) against `internal/costs/handler.go`'s actual input DTO before trusting the sketch verbatim — this session's earlier research already confirmed the shape used in M4's own `buildProjectWithEstimatedCostItem` (`projectId`, `category`, `description`, `estimatedAmount`, `currency`), so add `workItemId` to that same confirmed shape.

- [ ] **Step 2: Write the acceptance tests**

Append to `backend/internal/tenanttest/tenant_isolation_test.go`:

```go
func TestTenantIsolation_QuotationCreateFromFinalizedEstimate(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-create@example.com", "Quotations Create Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}
	if mustField(t, createResp, "quotationNumber") != "QT-000001" {
		t.Fatalf("expected QT-000001, got %s", mustField(t, createResp, "quotationNumber"))
	}
	if mustNumberField(t, createResp, "version") != 1 {
		t.Fatal("expected Version=1")
	}
	if mustField(t, createResp, "status") != "draft" {
		t.Fatal("expected status=draft")
	}

	var body map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	lines, ok := body["lines"].([]any)
	if !ok || len(lines) != 1 {
		t.Fatalf("expected exactly 1 generated line (one WorkItem), got %+v", body["lines"])
	}
	line := lines[0].(map[string]any)
	sourceIDs, ok := line["sourceWorkItemIds"].([]any)
	if !ok || len(sourceIDs) != 1 || sourceIDs[0] != workItemID {
		t.Fatalf("expected sourceWorkItemIds=[%s], got %+v", workItemID, line["sourceWorkItemIds"])
	}

	subtotal := mustNestedAmount(t, createResp, "subtotal")
	generatedSubtotal := mustNestedAmount(t, createResp, "generatedSubtotal")
	if subtotal != generatedSubtotal {
		t.Fatalf("expected Subtotal == GeneratedSubtotal at generation time, got %v and %v", subtotal, generatedSubtotal)
	}
	total := mustNestedAmount(t, createResp, "total")
	if total != subtotal {
		t.Fatal("expected Total == Subtotal when TaxMode=none")
	}
}

func TestTenantIsolation_QuotationCrossTenant(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-tenant-a@example.com", "Quotations Tenant A")
	companyB := registerCompany(t, router, "quotations-tenant-b@example.com", "Quotations Tenant B")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}
	quotationID := mustField(t, createResp, "id")

	getResp := doJSON(t, router, http.MethodGet, "/quotations/"+quotationID, companyB.accessToken, nil)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for company B accessing company A's quotation, got %d", getResp.Code)
	}
}

func TestTenantIsolation_QuotationCrossProjectEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-cross-project@example.com", "Quotations Cross Project Co")

	_, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	otherProjectID, _, _ := buildFinalizedEstimate(t, router, companyA) // a second, unrelated Project

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": otherProjectID, "estimateId": estimateID})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a cross-project estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationCrossCompanyEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-xco-a@example.com", "Quotations XCo A")
	companyB := registerCompany(t, router, "quotations-xco-b@example.com", "Quotations XCo B")

	_, estimateIDFromA, _ := buildFinalizedEstimate(t, router, companyA)
	projectIDForB, _, _ := buildFinalizedEstimate(t, router, companyB)

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyB.accessToken,
		map[string]any{"projectId": projectIDForB, "estimateId": estimateIDFromA})
	if createResp.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for a cross-company estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationDraftEstimateRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-draft-est@example.com", "Quotations Draft Est Co")

	_, projectID, _, _, workItemID := buildFullHierarchyForCompanyA(t, router, companyA)
	doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Tiles", "estimatedAmount": 500000, "currency": "MYR",
	})
	createEstResp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	draftEstimateID := mustField(t, createEstResp, "id") // deliberately NOT finalized

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": draftEstimateID})
	if createResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a draft (non-finalized) estimate reference, got %d: %s", createResp.Code, createResp.Body.String())
	}
}

func TestTenantIsolation_QuotationInternalCostInfoNeverLeaks(t *testing.T) {
	// Black-box proof that no internal cost/margin field name ever appears
	// anywhere in a Quotation HTTP response body (design spec §4/§28).
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-privacy@example.com", "Quotations Privacy Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	if createResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating quotation, got %d: %s", createResp.Code, createResp.Body.String())
	}

	forbidden := []string{"snapshottedAmount", "costSubtotal", "pricingMode", "pricingRate",
		"projectedGrossProfit", "projectedGrossMarginBps", "category"}
	body := createResp.Body.String()
	for _, field := range forbidden {
		if strings.Contains(body, field) {
			t.Fatalf("found forbidden internal field %q in Quotation response body: %s", field, body)
		}
	}
}

func TestTenantIsolation_QuotationLineCurrencySubstitutionRejected(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-currency@example.com", "Quotations Currency Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{}, "description": "Wrong Currency Line",
				"amount": map[string]any{"amount": 10000, "currency": "USD"}, "sortOrder": 0},
		},
	})
	if putResp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a currency-substitution attempt, got %d: %s", putResp.Code, putResp.Body.String())
	}

	unchanged := doJSON(t, router, http.MethodGet, "/quotations/"+quotationID, companyA.accessToken, nil)
	if mustNumberField(t, unchanged, "revision") != 0 {
		t.Fatal("expected the draft's Revision unchanged after a rejected line submission")
	}
}

func TestTenantIsolation_QuotationDraftEditingAndOptimisticConcurrency(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-edit@example.com", "Quotations Edit Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	// Successful line edit.
	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{workItemID}, "description": "Wall Tiles - Complete",
				"amount": map[string]any{"amount": 600000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if putResp.Code != http.StatusOK {
		t.Fatalf("expected 200 replacing lines, got %d: %s", putResp.Code, putResp.Body.String())
	}
	if mustNumberField(t, putResp, "revision") != 1 {
		t.Fatal("expected Revision incremented to 1")
	}

	// Stale revision on a second attempt must be rejected.
	staleResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0, // stale — already consumed above
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{}, "description": "Should Fail",
				"amount": map[string]any{"amount": 100000, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	if staleResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 on a stale revision, got %d", staleResp.Code)
	}

	// Terms update.
	termsResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/terms", companyA.accessToken, map[string]any{
		"terms": "50% deposit, 50% on completion", "paymentSchedule": "Net 30", "expectedRevision": 1,
	})
	if termsResp.Code != http.StatusOK {
		t.Fatalf("expected 200 updating terms, got %d: %s", termsResp.Code, termsResp.Body.String())
	}
	if mustField(t, termsResp, "terms") != "50% deposit, 50% on completion" {
		t.Fatal("expected terms to be updated")
	}

	// Tax update.
	taxResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/tax", companyA.accessToken, map[string]any{
		"taxMode": "percentage", "taxLabel": "SST 6%", "taxRateBps": 600, "expectedRevision": 2,
	})
	if taxResp.Code != http.StatusOK {
		t.Fatalf("expected 200 updating tax, got %d: %s", taxResp.Code, taxResp.Body.String())
	}
	subtotalAfterTax := mustNestedAmount(t, taxResp, "subtotal")
	taxAmount := mustNestedAmount(t, taxResp, "taxAmount")
	total := mustNestedAmount(t, taxResp, "total")
	if total != subtotalAfterTax+taxAmount {
		t.Fatalf("expected Total == Subtotal + TaxAmount, got total=%v subtotal=%v tax=%v", total, subtotalAfterTax, taxAmount)
	}
}

func TestTenantIsolation_QuotationFinalizedImmutability(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-immutable@example.com", "Quotations Immutable Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")

	finalizeResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}
	if mustField(t, finalizeResp, "status") != "finalized" {
		t.Fatal("expected status=finalized")
	}

	// Every mutating endpoint must now reject with 409.
	putResp := doJSON(t, router, http.MethodPut, "/quotations/"+quotationID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0, "lines": []map[string]any{{"sourceWorkItemIds": []string{}, "description": "X", "amount": map[string]any{"amount": 100, "currency": "MYR"}}},
	})
	if putResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 replacing lines on a finalized quotation, got %d", putResp.Code)
	}

	termsResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/terms", companyA.accessToken,
		map[string]any{"terms": "X", "expectedRevision": 0})
	if termsResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 updating terms on a finalized quotation, got %d", termsResp.Code)
	}

	taxResp := doJSON(t, router, http.MethodPatch, "/quotations/"+quotationID+"/tax", companyA.accessToken,
		map[string]any{"taxMode": "none", "expectedRevision": 0})
	if taxResp.Code != http.StatusConflict {
		t.Fatalf("expected 409 updating tax on a finalized quotation, got %d", taxResp.Code)
	}

	// Idempotent finalize retry -> 200, unchanged.
	retryResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 999})
	if retryResp.Code != http.StatusOK {
		t.Fatalf("expected 200 on an idempotent finalize retry, got %d: %s", retryResp.Code, retryResp.Body.String())
	}
}

func TestTenantIsolation_QuotationNewVersionFreshRegeneration(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-newversion@example.com", "Quotations NewVersion Co")

	projectID, estimateID, workItemID := buildFinalizedEstimate(t, router, companyA)
	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	v1ID := mustField(t, createResp, "id")
	quotationNumber := mustField(t, createResp, "quotationNumber")

	// Hand-edit V1's line before finalizing.
	doJSON(t, router, http.MethodPut, "/quotations/"+v1ID+"/lines", companyA.accessToken, map[string]any{
		"expectedRevision": 0,
		"lines": []map[string]any{
			{"sourceWorkItemIds": []string{workItemID}, "description": "Hand-Edited Line",
				"amount": map[string]any{"amount": 999900, "currency": "MYR"}, "sortOrder": 0},
		},
	})
	finalizeV1Resp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 1})
	if finalizeV1Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing v1, got %d: %s", finalizeV1Resp.Code, finalizeV1Resp.Body.String())
	}

	// A fresh, different finalized Estimate for the SAME Project.
	costItemResp := doJSON(t, router, http.MethodPost, "/cost-items", companyA.accessToken, map[string]any{
		"projectId": projectID, "workItemId": workItemID, "category": "material",
		"description": "Additional tiles", "estimatedAmount": 300000, "currency": "MYR",
	})
	if costItemResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a second cost item, got %d: %s", costItemResp.Code, costItemResp.Body.String())
	}
	createEst2Resp := doJSON(t, router, http.MethodPost, "/estimates", companyA.accessToken,
		map[string]any{"projectId": projectID, "pricingMode": "markup", "pricingRate": 2000})
	// NOTE: this creates a SECOND estimate chain attempt for the same
	// Project, which the M4 "only Version 1 ever" rule may reject if an
	// Estimate already exists for this Project (M4 ErrEstimateAlreadyExistsForProject).
	// If createEst2Resp.Code is 409 here, use POST /estimates/{id}/versions
	// against the FIRST estimate's ID instead (it is already finalized,
	// satisfying that endpoint's own precondition) to obtain a second,
	// distinct finalized Estimate for this Project — adjust this test to
	// do so if the direct POST /estimates attempt returns 409.
	if createEst2Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a second estimate (or adapt this test to use POST /estimates/{id}/versions if M4's one-Estimate-per-Project rule rejects a second POST /estimates — see comment above), got %d: %s",
			createEst2Resp.Code, createEst2Resp.Body.String())
	}
	estimate2ID := mustField(t, createEst2Resp, "id")
	finalizeEst2Resp := doJSON(t, router, http.MethodPost, "/estimates/"+estimate2ID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeEst2Resp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing the second estimate, got %d: %s", finalizeEst2Resp.Code, finalizeEst2Resp.Body.String())
	}

	versionResp := doJSON(t, router, http.MethodPost, "/quotations/"+v1ID+"/versions", companyA.accessToken,
		map[string]any{"estimateId": estimate2ID})
	if versionResp.Code != http.StatusOK {
		t.Fatalf("expected 200 creating a new quotation version, got %d: %s", versionResp.Code, versionResp.Body.String())
	}
	if mustNumberField(t, versionResp, "version") != 2 {
		t.Fatal("expected Version=2")
	}
	if mustField(t, versionResp, "quotationNumber") != quotationNumber {
		t.Fatal("expected the same QuotationNumber carried forward")
	}
	v2GeneratedSubtotal := mustNestedAmount(t, versionResp, "generatedSubtotal")
	if v2GeneratedSubtotal == 999900 {
		t.Fatal("expected V2's GeneratedSubtotal to be a FRESH allocation, NOT V1's hand-edited amount carried forward")
	}
}

func TestTenantIsolation_QuotationFinalizeNeverTouchesProjectStatus(t *testing.T) {
	router := setupRouter(t)
	companyA := registerCompany(t, router, "quotations-project-status@example.com", "Quotations Project Status Co")

	projectID, estimateID, _ := buildFinalizedEstimate(t, router, companyA)

	beforeResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyA.accessToken, nil)
	statusBefore := mustField(t, beforeResp, "status")

	createResp := doJSON(t, router, http.MethodPost, "/quotations", companyA.accessToken,
		map[string]any{"projectId": projectID, "estimateId": estimateID})
	quotationID := mustField(t, createResp, "id")
	finalizeResp := doJSON(t, router, http.MethodPost, "/quotations/"+quotationID+"/finalize", companyA.accessToken,
		map[string]any{"expectedRevision": 0})
	if finalizeResp.Code != http.StatusOK {
		t.Fatalf("expected 200 finalizing quotation, got %d: %s", finalizeResp.Code, finalizeResp.Body.String())
	}

	afterResp := doJSON(t, router, http.MethodGet, "/projects/"+projectID, companyA.accessToken, nil)
	statusAfter := mustField(t, afterResp, "status")
	if statusBefore != statusAfter {
		t.Fatalf("expected Project.Status unchanged by Quotation.finalize, was %q now %q", statusBefore, statusAfter)
	}
}

func TestTenantIsolation_AllQuotationEndpointsRequireBearerAuth(t *testing.T) {
	router := setupRouter(t)
	openAPIResp := doJSON(t, router, http.MethodGet, "/openapi.json", "", nil)
	if openAPIResp.Code != http.StatusOK {
		t.Fatalf("expected 200 fetching openapi.json, got %d", openAPIResp.Code)
	}
	var spec map[string]any
	if err := json.Unmarshal(openAPIResp.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse openapi.json: %v", err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("expected a paths object in openapi.json")
	}
	quotationsPathCount := 0
	for path, methods := range paths {
		if !strings.HasPrefix(path, "/quotations") {
			continue
		}
		methodsMap, ok := methods.(map[string]any)
		if !ok {
			continue
		}
		for _, opRaw := range methodsMap {
			op, ok := opRaw.(map[string]any)
			if !ok {
				continue
			}
			security, ok := op["security"].([]any)
			if !ok || len(security) == 0 {
				t.Fatalf("expected every /quotations operation to declare bearerAuth security, path %s has none: %+v", path, op)
			}
			quotationsPathCount++
		}
	}
	if quotationsPathCount != 8 {
		t.Fatalf("expected exactly 8 /quotations operations declaring security, found %d", quotationsPathCount)
	}
}
```

**Import note:** if `"strings"` is not already imported in `tenant_isolation_test.go`, add it to the import block (used by `TestTenantIsolation_QuotationInternalCostInfoNeverLeaks` and `TestTenantIsolation_AllQuotationEndpointsRequireBearerAuth` above); `"encoding/json"` is almost certainly already imported (used throughout the existing file's helpers) — confirm both before running.

- [ ] **Step 2: Run the new acceptance tests**

Run: `cd backend && go test ./internal/tenanttest/... -run TestTenantIsolation_Quotation -v`
Expected: PASS for every new test. Debug and fix failures by tracing which layer (handler DTO shape, service validation order, repository query) produced the unexpected status code or field value — do NOT weaken an assertion to make it pass; if a test's expectation turns out to be wrong against the actual (correct) design-spec behavior, fix the test, not the design.

- [ ] **Step 3: Run the ENTIRE `tenanttest` suite**

Run: `cd backend && go test ./internal/tenanttest/... -v`
Expected: PASS — every M0-M4 acceptance test (30 from M4) plus every new M5 test, all green, no regressions.

- [ ] **Step 4: Leave changes uncommitted**

No `git add`/`git commit`.

---

## Task 13: Full-suite verification and live smoke test

**Files:** none modified — verification only.

This task proves the milestone is actually done: every module builds, every test passes from a cold cache, `go mod tidy` is stable, and the real server starts and serves the new endpoints against live Docker Compose MongoDB — matching the exact verification M0-M4 each performed before being declared complete.

- [ ] **Step 1: Clean build across the whole backend**

Run: `cd backend && go build ./...`
Expected: exits 0, no output.

- [ ] **Step 2: `go vet` across the whole backend**

Run: `cd backend && go vet ./...`
Expected: exits 0, no output.

- [ ] **Step 3: Full test suite, uncached, from scratch**

Run: `cd backend && go clean -testcache && go test ./... -v 2>&1 | tail -100`
Expected: every package reports `ok`, including `internal/foundation/money`, `internal/projects`, `internal/work`, `internal/estimates`, `internal/quotations`, and `internal/tenanttest`. No `FAIL` anywhere. (Piping through `tail -100` keeps the final summary visible; if anything fails, re-run the specific failing package without the pipe to see full output: `go test ./internal/<package>/... -v`.)

If a single Testcontainers-backed test intermittently fails with "rootless Docker is not supported on Windows," re-run just that package once (documented, known flake in this environment across M2/M3/M4 — not a defect). Do not treat a single such occurrence as a real failure; do treat a repeated failure of the same test as real.

- [ ] **Step 4: `go mod tidy` stability check**

Run:
```
cd backend
cp go.sum go.sum.before
go mod tidy
```

Then compare: `diff go.sum go.sum.before` (or on Windows, `fc go.sum go.sum.before`).
Expected: no differences — `go.mod`/`go.sum` were already stable (no new dependency was introduced by this milestone; `AllocateProportionally`/`ApplyRateBPS` use only `shopspring/decimal`, already a dependency). Delete `go.sum.before` afterward.

- [ ] **Step 5: Confirm no `.git` directory exists anywhere**

Run: `find "c:/Users/shananth/mvp" -maxdepth 2 -name ".git"` (or `Get-ChildItem -Path "c:\Users\shananth\mvp" -Depth 1 -Filter ".git" -Directory -Force` in PowerShell)
Expected: no output / no matches — this project is local-only by design; some tooling can initialize git as a side effect, and if a `.git` directory is found, it must be removed immediately (per project CLAUDE.md) rather than left in place.

- [ ] **Step 6: Live E2E verification against Docker Compose MongoDB**

Check Docker Compose is running: `docker compose ps` (from the repo root). If `renovation-mongo` is not `Up`, start it: `docker compose up -d`.

Start the server: `cd backend && go run ./cmd/api` (run in background/separate terminal — do not block the session).

Once it logs `listening` on the configured port (check `backend/.env` for `HTTP_PORT`, default assumed `8080` below), run this sequence via `curl` (adjust host/port if different):

```bash
# Health check
curl -s http://localhost:8080/health

# Register a company, capture the access token
curl -s -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"m5verify@example.com","password":"password123","companyName":"M5 Verify Co"}' \
  | tee /tmp/register.json
# extract accessToken from /tmp/register.json manually or via jq if available

# Create a Client, Project, Space (or skip Space if optional), WorkItem, CostItem, Estimate — following the exact same chain Task 12's buildFinalizedEstimate helper uses — then finalize the Estimate, then:

# POST /quotations
curl -s -X POST http://localhost:8080/quotations \
  -H "Authorization: Bearer <accessToken>" -H "Content-Type: application/json" \
  -d '{"projectId":"<projectId>","estimateId":"<estimateId>"}'

# GET /quotations/{id}
curl -s http://localhost:8080/quotations/<quotationId> -H "Authorization: Bearer <accessToken>"

# PUT /quotations/{id}/lines, PATCH .../terms, PATCH .../tax, POST .../finalize, POST .../versions —
# exercise each endpoint once, confirming the response shape and status codes match Task 12's
# acceptance-test expectations.

# Confirm unauthenticated access is rejected
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/quotations
# expected: 401

# Confirm the OpenAPI document lists all 8 new operations with bearerAuth
curl -s http://localhost:8080/openapi.json | grep -o '"/quotations[^"]*"' | sort -u
```

Expected: every call succeeds with the correct status code and response shape; the full create→edit→tax→finalize→new-version lifecycle works end-to-end against a real server and real MongoDB, exactly mirroring the design spec's documented lifecycle (§10-12).

Stop the server afterward (PowerShell: find and stop the `go run`/`api.exe` process; confirm via `curl http://localhost:8080/health` returning connection-refused).

- [ ] **Step 7: Update `tasks/done.md`**

Append a new `## Milestone 5 — Quotations: Implementation` entry to `c:\Users\shananth\mvp\tasks\done.md`, following the exact narrative style and level of detail of the existing `## Milestone 4 — Estimates: Implementation` entry (structure: what was built per file/module, key invariants proven, deviations from the plan found via TDD if any, full verification summary, "Next:" pointer to Milestone 6). Do not copy M4's entry's specific facts — write M5's own, accurately reflecting what actually happened during this implementation (including any deviations from this plan discovered along the way, per this project's standing "record deviations found via TDD" convention).

- [ ] **Step 8: Update `handoff.md`**

Rewrite `c:\Users\shananth\mvp\handoff.md` to reflect Milestone 5 complete and Milestone 6 (Client Access / Secure Links / Approval) next, following its own established structure (Goal, Current state, What's in progress, Key decisions, Known issues, Next action, Environment/setup notes) — mirroring the pattern already used for the M3→M4 and M4→M5 handoff rewrites earlier in this project's history.

- [ ] **Step 9: Leave changes uncommitted**

No `git add`/`git commit` — this project has no `.git` directory by design; committing is always a separate, explicit future request, never bundled into implementation work (standing instruction).
