# Milestone 2 — Project Foundation: Design Spec

**Status:** APPROVED — all open questions resolved in review. Ready for
implementation planning.

**Scope:** `internal/clients`, `internal/projects`, `internal/properties`,
`internal/spaces`, `internal/work` — models, repositories, services, Huma
handlers, and the tenant-isolation guarantees proven at every layer.

**Authorities:** `phase1.md` §3-8, §55 (domain/collection source of truth),
ADR 0002 (module boundaries), Milestone 1's identity/companies
implementation (established patterns this spec follows, not re-litigates).

**Revision note:** this spec went through two review rounds. Round 1 dropped
a proposed `Project.PropertyID` back-reference (it created a value-level
construction cycle in `cmd/api` wiring), moved Space's parent from Property
to Project, and removed per-entity archive endpoints. Round 2 closed a
capability-interface gap, resolved a status-transition contradiction,
corrected the collection-ownership wording for future AI compatibility,
pinned down the Mongo persistence representation for `quantity.Quantity`,
and relaxed one acceptance-test's wording to test the actual security
invariant rather than one specific HTTP decoding policy. This document
contains only the final, approved decisions — no superseded alternatives
are retained inline.

---

## 1. Domain models

All models include `schemaVersion int` (§52) set to `1`. All `id` fields are
Mongo ObjectID hex strings, matching M1's `User`/`Company` pattern
(`bson:"_id,omitempty"`). No model stores a delete/archive field — see §6.

### 1.1 Client (`internal/clients`) — phase1.md §4

```go
type Client struct {
    ID             string
    CompanyID      string
    Name           string
    Phone          string // optional
    Email          string // optional
    Address        string // optional
    BillingAddress string // optional
    Notes          string // optional
    CreatedAt      time.Time
    SchemaVersion  int
}
```

phase1.md §4 lists "Project history" as a Client attribute; per §5 this is
a reference relationship (Projects hold `clientId`), never embedded,
consistent with §55's "avoid giant documents, use references." No
`projectHistory` field is stored on Client — it's a query
(`GET /projects?clientId=...`, owned by `projects`).

### 1.2 Project (`internal/projects`) — phase1.md §5

```go
type Project struct {
    ID            string
    CompanyID     string
    ClientID      string // required — every Project belongs to a Client
    Name          string
    Status        ProjectStatus
    CreatedAt     time.Time
    SchemaVersion int
}

type ProjectStatus string

const (
    ProjectStatusLead              ProjectStatus = "lead"
    ProjectStatusSiteVisit         ProjectStatus = "site_visit"
    ProjectStatusEstimating        ProjectStatus = "estimating"
    ProjectStatusQuotationSent     ProjectStatus = "quotation_sent"
    ProjectStatusQuotationApproved ProjectStatus = "quotation_approved"
    ProjectStatusInProgress        ProjectStatus = "in_progress"
    ProjectStatusCompleted         ProjectStatus = "completed"
    ProjectStatusClosed            ProjectStatus = "closed"
)
```

Verbatim from phase1.md §5's 8-value list. Default on creation: `Lead`.
**`Project` does not store a `PropertyID` field.** The Project↔Property
relationship is owned entirely by `Property.ProjectID` (§1.3); a Project's
Property, if any, is retrieved via `GET /properties?projectId={id}`, never
embedded or denormalized back onto Project. This is a deliberate decision,
not an oversight — see §7.

**Status transitions:** any of the eight enum values is a valid destination
from any other value in M2 — no transition-graph/state-machine validation.
phase1.md's list reads as roughly sequential guidance, not an enforced
workflow, and specifies no illegal-transition rule. The method is named
`UpdateProjectStatus`, not `TransitionProjectStatus` or similar, to avoid
implying a formal transition engine that doesn't exist yet.

### 1.3 Property (`internal/properties`) — phase1.md §6

```go
type Property struct {
    ID            string
    CompanyID     string
    ProjectID     string // required
    Address       string
    PropertyType  string // free-text, phase1.md does not enumerate types
    Notes         string // optional
    CreatedAt     time.Time
    SchemaVersion int
}
```

**Cardinality: 0 or 1 Property per Project**, enforced by MongoDB — a
**unique compound index on `{companyId, projectId}`** (§8, §11). A second
`POST /properties` for a Project that already has one returns **409
Conflict**, mirroring M1's `ErrUserAlreadyHasMembership` pattern
(`company_members.userId` unique index → 409) exactly.

`PropertyType` is unconstrained free text — phase1.md §6 names the field
but gives no enum, so none is invented here.

### 1.4 Space (`internal/spaces`) — phase1.md §7

```go
type Space struct {
    ID            string
    CompanyID     string
    ProjectID     string // required — Space belongs directly to Project, not Property
    Name          string // e.g. "Master Bathroom"
    Type          string // e.g. "bathroom" — free-text, not enumerated in phase1.md
    Description   string // optional
    CreatedAt     time.Time
    SchemaVersion int
}
```

Matches phase1.md §7's literal example JSON (`projectId`, not
`propertyId`) exactly. Space's parent is **Project**, independent of
whether that Project has a Property. This keeps Property fully optional
with no knock-on prerequisite for creating Spaces, and removes an entire
capability-interface hop (`spaces` no longer needs any relationship to
`properties` at all).

### 1.5 WorkItem (`internal/work`) — phase1.md §8

```go
type WorkItem struct {
    ID                 string
    CompanyID           string
    ProjectID           string             // required
    SpaceID             *string            // optional — nullable
    Description         string
    WorkType            string             // e.g. "tile_installation" — free-text
    Quantity            quantity.Quantity  // internal/foundation/quantity — Value decimal.Decimal + Unit string
    Status              WorkItemStatus
    Source              WorkItemSource
    VerificationStatus  VerificationStatus
    CreatedAt           time.Time
    SchemaVersion        int
}

type WorkItemStatus string

const (
    WorkItemStatusPlanned   WorkItemStatus = "planned"
    WorkItemStatusCancelled WorkItemStatus = "cancelled"
)

type WorkItemSource string

const (
    WorkItemSourceManual       WorkItemSource = "manual"
    WorkItemSourceAISuggestion WorkItemSource = "ai_suggestion"
)

type VerificationStatus string

const (
    VerificationStatusPending   VerificationStatus = "pending"
    VerificationStatusConfirmed VerificationStatus = "confirmed"
    VerificationStatusRejected  VerificationStatus = "rejected"
)
```

`Quantity` uses the existing `internal/foundation/quantity.Quantity{Value
decimal.Decimal, Unit string}` type directly — not a separate
`decimal.Decimal` + `string` pair reinvented on `WorkItem`. There is no
separate `Unit string` field on `WorkItem`; unit lives inside `Quantity`.
Constructed via `quantity.New(valueString, unit)`, which already validates
the value parses as a decimal; **additionally** validate `Value.GreaterThan(decimal.Zero)`
and `Unit != ""` in `work.Service.CreateWorkItem` — `quantity.New` alone
only guarantees a parseable decimal, not that it's a meaningful positive
quantity with a real unit.

**Mongo persistence representation for `Quantity` (pinned explicitly, not
left to driver defaults):** `shopspring/decimal@v1.4.0` (the pinned
version, per `go.mod`) has no `MarshalBSON`/`UnmarshalBSON` implementation
— confirmed by inspecting the module source directly (no `.go` file in the
package references BSON at all; only its README mentions the topic). The
mongo-driver's default struct-reflection codec therefore cannot be trusted
to round-trip `decimal.Decimal` correctly, and must not be relied upon
implicitly. `work_items`'s BSON document stores `Quantity` as an explicit
sub-document with the decimal value as a **string**, never as BSON
`Decimal128` and never left to automatic reflection:

```go
// bson representation, defined explicitly in work/repository_mongo.go
type quantityDoc struct {
    Value string `bson:"value"` // decimal.Decimal.String() — never Decimal128, never float
    Unit  string `bson:"unit"`
}
```

Mapping is explicit in both directions: `quantity.Quantity → quantityDoc`
via `Value: q.Value.String()`, and `quantityDoc → quantity.Quantity` via
`quantity.New(doc.Value, doc.Unit)` (re-parsing the stored string through
the same constructor used for HTTP input, so both paths share one
validation/parsing routine). This avoids coupling the domain primitive to
any MongoDB-specific numeric encoding and guarantees no float
precision loss on the round trip. A repository integration test
(`TestWorkItemRepositoryQuantityRoundTrip`, §15) inserts a WorkItem with
`quantity.New("32.7501", "m2")`, reads it back, and asserts
`Value.Equal(decimal.RequireFromString("32.7501"))` and `Unit == "m2"`
exactly — proving the actual persistence contract, not merely that
`quantity.New` works in memory.

**M2 only ever creates `Source=manual` WorkItems, always with
`VerificationStatus=confirmed` and `Status=planned` set server-side** — the
HTTP request body never lets the caller choose `source` or
`verificationStatus`. These fields, and the `ai_suggestion`/`pending`/
`rejected` enum values, exist so that **a future AI workflow can submit
AI-suggested Work Items through a capability exposed by the `work` module,
without requiring a schema migration** (per phase1.md §9) — **not** so that
a future `internal/ai` module writes to `work_items` directly. `work`
remains the sole owner of, and persistence boundary for, the `work_items`
collection (§2), unchanged by this forward-compatibility note. The intended
future shape is `AI Service → WorkItemSuggestion capability → work.Service →
work repository → work_items`, never `AI repository → work_items`. **No
M2 endpoint reads or transitions `verificationStatus`** — see §5.5.

---

## 2. Collection ownership

| Module | Collection | Owns exclusively |
|---|---|---|
| `clients` | `clients` | Yes |
| `projects` | `projects` | Yes |
| `properties` | `properties` | Yes |
| `spaces` | `spaces` | Yes |
| `work` | `work_items` | Yes |

Matches phase1.md §55 exactly. No module queries another's collection
directly, per ADR 0002.

---

## 3. Entity relationship diagram

```text
Company (M1)
  │
  └──< Client                       (clients.companyId)
         │
         └──< Project                (projects.companyId, projects.clientId)
                │
                ├──(0..1)── Property  (properties.companyId, properties.projectId
                │                      — UNIQUE on {companyId, projectId})
                │
                ├──< Space            (spaces.companyId, spaces.projectId)
                │
                └──< WorkItem         (work_items.companyId, work_items.projectId,
                                        work_items.spaceId OPTIONAL → Space,
                                        where Space.ProjectID must equal
                                        WorkItem.ProjectID when both are set)
```

Cardinalities:
- Company 1—* Client
- Client 1—* Project
- Project 0..1 Property (unique-index enforced)
- Project 1—* Space
- Project 1—* WorkItem
- Space 0..1—* WorkItem (optional; when present, must share the same Project)

Property and Space are now **siblings** under Project, not a chain —
Property is a leaf with no children in M2; Space does not depend on
Property at all.

---

## 4. Required invariants

1. **Every tenant-owned document stores `companyId` directly.**
2. **`companyId` is never accepted from the client** in any request body,
   query parameter, or path parameter — Huma DTOs never declare a
   `companyId` field, so there is nothing to strip or validate against; it
   structurally cannot be bound from user input. It is derived exclusively
   from `Principal.CompanyID`.
3. **Every parent-reference field (`clientId`, `projectId`, `spaceId`) is
   validated, at write time, to (a) exist and (b) belong to
   `Principal.CompanyID`**, before the child is created — via the owning
   module's narrow capability interface, never by reading the parent's raw
   collection from the child module.
4. **Every direct-by-ID repository read/update method is tenant-scoped**:
   `(ctx, companyID, id)`, never `(ctx, id)` alone, for any method reachable
   from an HTTP handler. Capability-interface methods are the one
   structurally-narrower exception (§9) — they return booleans/IDs, never
   the domain struct, and are still compound-filtered by company.
5. **A resource that exists but belongs to a different company is
   indistinguishable from a resource that does not exist at all** — both
   return the same 404, from the same sentinel error (§6).
6. **List endpoints are always scoped to `Principal.CompanyID`** and, where
   a parent-filter query parameter is given, that parent is independently
   validated as belonging to the caller's company **before** the list query
   runs — a foreign parent ID returns 404, never an empty list (§8).
7. **Changing a resource ID in a request never crosses a tenant boundary** —
   substituting Company B's real ID into a Company A-authenticated request
   behaves exactly as if that ID didn't exist.
8. **Space→WorkItem lineage integrity**: when `WorkItem.SpaceID` is
   provided, the referenced Space's `ProjectID` must equal the WorkItem's
   own `ProjectID` — not merely belong to the same company. This prevents
   pairing a real Space from Project A2 with Project A1 within the same
   tenant (§10).

---

## 5. Module service responsibilities

Each module follows the exact M1 shape: `model.go`, `repository.go`
(interface + sentinel errors), `repository_mongo.go` (Mongo impl +
`EnsureIndexes`), `service.go` (business logic + capability interfaces),
`handler.go` (Huma DTOs + routes).

### 5.1 `clients.Service`

- `CreateClient(ctx, companyID, name, phone, email, address, billingAddress, notes) (Client, error)`
- `GetClient(ctx, companyID, clientID) (Client, error)`
- `ListClients(ctx, companyID) ([]Client, error)`
- `UpdateClient(ctx, companyID, clientID, ...) (Client, error)`
- Exposes **`ClientLookup`** (consumed by `projects`).

### 5.2 `projects.Service`

- `CreateProject(ctx, companyID, clientID, name) (Project, error)` —
  validates `clientID` via `ClientLookup`.
- `GetProject(ctx, companyID, projectID) (Project, error)`
- `ListProjects(ctx, companyID) ([]Project, error)`
- `ListProjectsByClient(ctx, companyID, clientID) ([]Project, error)` —
  validates `clientID` via `ClientLookup` first (§4.6); 404 if it belongs to
  another company.
- `UpdateProjectStatus(ctx, companyID, projectID, newStatus) (Project, error)`
- Exposes **`ProjectLookup`** (consumed by `properties`, `spaces`, `work`).
- Does **not** expose or consume anything related to Property — no
  `PropertyLookup` exists anywhere in this module or its consumers.

### 5.3 `properties.Service`

- `CreateProperty(ctx, companyID, projectID, address, propertyType, notes) (Property, error)`
  — validates `projectID` via `ProjectLookup`; repository `Create` relies on
  the unique `{companyId, projectId}` index to reject a second Property for
  the same Project with **409 Conflict** (mapped from the repository's
  duplicate-key sentinel, mirroring `companies.ErrUserAlreadyHasMembership`
  → 409 exactly).
- `GetProperty(ctx, companyID, propertyID) (Property, error)`
- `ListPropertiesByProject(ctx, companyID, projectID) ([]Property, error)` —
  validates `projectID` via `ProjectLookup` first; 404 if foreign.
- `UpdateProperty(ctx, companyID, propertyID, ...) (Property, error)`
- Exposes nothing — no other module needs to look up a Property in M2.

### 5.4 `spaces.Service`

- `CreateSpace(ctx, companyID, projectID, name, spaceType, description) (Space, error)`
  — validates `projectID` via `ProjectLookup` (consumed from `projects`,
  **not** `properties` — Space has no relationship to Property).
- `GetSpace(ctx, companyID, spaceID) (Space, error)`
- `ListSpacesByProject(ctx, companyID, projectID) ([]Space, error)` —
  validates `projectID` first; 404 if foreign.
- `UpdateSpace(ctx, companyID, spaceID, ...) (Space, error)`
- Exposes **`SpaceLookup`** (consumed by `work`).

### 5.5 `work.Service`

- `CreateWorkItem(ctx, companyID, projectID, spaceID *string, description, workType, quantityValue, unit) (WorkItem, error)`:
  1. Validate `projectID` via `ProjectLookup`; 404 if missing/foreign.
  2. If `spaceID != nil`: validate via `SpaceLookup.SpaceBelongsToProject(ctx,
     companyID, spaceID, projectID)` — a single call that checks both
     tenant ownership **and** Project-lineage in one step (§10); 404 if
     either fails.
  3. Construct `quantity.New(quantityValue, unit)`; reject if parse fails,
     if `Value <= 0`, or if `Unit == ""`.
  4. Persist with `Status=planned`, `Source=manual`,
     `VerificationStatus=confirmed`, all set server-side — never
     client-supplied.
- `GetWorkItem(ctx, companyID, workItemID) (WorkItem, error)`
- `ListWorkItemsByProject(ctx, companyID, projectID) ([]WorkItem, error)` —
  validates `projectID` first.
- `ListWorkItemsBySpace(ctx, companyID, spaceID) ([]WorkItem, error)` —
  validates `spaceID` via `SpaceLookup.SpaceBelongsToCompany` first (§8.4) —
  a plain company-ownership check suffices here; there is no second parent
  ID to cross-check against.
- `UpdateWorkItemStatus(ctx, companyID, workItemID, newStatus) (WorkItem, error)` —
  **`planned → cancelled` only, one-directional.** `Cancelled` is terminal
  in M2: there is no `cancelled → planned` transition, matching `Project`'s
  `Closed` being terminal too. A future explicit restore/reactivate
  workflow can be added later if needed; M2 does not expose one.
- **No `ConfirmWorkItem`/`RejectWorkItem` methods and no
  `PATCH /work-items/{id}/verification` endpoint exist in M2.** There is no
  M2 code path that ever produces a `pending` WorkItem, so no review
  workflow is exposed. The fields and enum values remain in the schema for
  forward compatibility only (§1.5).

---

## 6. Delete/archive behavior

**Deferred entirely in M2.** No hard delete, no soft delete, no archive
endpoints, for any of the five entities. No model stores an `ArchivedAt`
field. `Project.Status` already has a terminal `Closed` value and
`WorkItem.Status` already has a terminal `Cancelled` value — both pre-exist
in the approved status enums and require no additional lifecycle field.

This avoids inventing lifecycle rules phase1.md never specifies (e.g.,
whether a Project can be created under an archived Client, whether archived
records remain independently readable, whether they can still be updated).
**Destructive deletion and archival lifecycle management are deferred; M2
exposes no delete/archive endpoints for Client, Property, Space, Project,
or WorkItem.**

---

## 7. Project ↔ Property relationship: resolved direction

`Property.ProjectID` is the sole, authoritative link. **`Project` does not
store, derive, or expose a `PropertyID`** in any form — not as a stored
field, not as a read-time capability lookup, not as a denormalized
convenience in the `GET /projects/{id}` response. A caller who needs a
Project's Property calls `GET /properties?projectId={id}` separately
(returns zero or one Property, by construction of the unique index in
§1.3/§11).

This was a deliberate simplification during review: an earlier draft
proposed `projects` consuming a `properties`-provided `PropertyLookup` to
resolve this at read time, which required `properties.Service` and
`projects.Service` to each hold a reference to the other — a value-level
construction cycle at `cmd/api` wiring time (not a package-import cycle,
since Go interfaces keep the package graph acyclic, but still an
unconstructible pair of concrete services with no valid initialization
order). Removing the back-reference entirely eliminates the cycle rather
than working around it with two-phase construction. See §9 for the
resulting acyclic wiring order.

---

## 8. Narrow cross-module capability interfaces

Consumer defines the interface; provider's concrete `*Service` satisfies it
structurally; `cmd/api` wires concrete types together; no domain package
imports another domain package's types.

### 8.1 `projects` defines `ClientLookup`

```go
// defined in internal/projects
type ClientLookup interface {
    ClientBelongsToCompany(ctx context.Context, companyID, clientID string) (bool, error)
}
```

Satisfied structurally by `clients.Service`.

### 8.2 `properties` defines `ProjectLookup`

```go
// defined in internal/properties
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}
```

Satisfied structurally by `projects.Service`.

### 8.3 `spaces` defines `ProjectLookup`

```go
// defined in internal/spaces
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}
```

Satisfied structurally by `projects.Service`. (Separate interface
definition from §8.2 — each consumer defines its own, per the
consumer-defines-interface rule, even though the shape is identical; this
mirrors how M1 didn't share a single generic lookup type across
consumers either.)

### 8.4 `work` defines `ProjectLookup` and `SpaceLookup`

```go
// defined in internal/work
type ProjectLookup interface {
    ProjectBelongsToCompany(ctx context.Context, companyID, projectID string) (bool, error)
}

type SpaceLookup interface {
    // Company-ownership check only, no Project argument — used by
    // ListWorkItemsBySpace, which has no second parent ID to cross-check
    // against.
    SpaceBelongsToCompany(ctx context.Context, companyID, spaceID string) (bool, error)

    // Confirms spaceID belongs to companyID AND that its stored ProjectID
    // equals projectID — one call resolves both tenant ownership and
    // Project-lineage integrity (§10), since Space now stores ProjectID
    // directly (§1.4). Used by CreateWorkItem when spaceID is provided.
    SpaceBelongsToProject(ctx context.Context, companyID, spaceID, projectID string) (bool, error)
}
```

Satisfied structurally by `projects.Service` and `spaces.Service`
respectively. `spaces.Service`'s implementation of both methods is a single
compound-filtered Mongo query each —
`{_id: spaceID, companyId: companyID}` for `SpaceBelongsToCompany`, and
`{_id: spaceID, companyId: companyID, projectId: projectID}` for
`SpaceBelongsToProject` — no separate existence check followed by a
lineage check; one query, one round trip, per method.

**No `PropertyLookup` interface exists anywhere in M2.** No module consumes
Property except through its own direct HTTP handler.

---

## 9. Compile-time dependency / wiring graph

```text
              cmd/api (composition root)
                    │
                    ▼
                clients
                    │
                    ▼
                projects
                 ┌──┴──────────┐
                 ▼             ▼
            properties       spaces
                                │
                                ▼
                               work
```

Concrete construction order in `cmd/api/main.go` (strictly left-to-right /
top-to-bottom, no back-edges):

```go
clientsService    := clients.NewService(clientRepo)
projectsService   := projects.NewService(projectRepo, clientsService)      // consumes ClientLookup
propertiesService := properties.NewService(propertyRepo, projectsService)  // consumes ProjectLookup
spacesService     := spaces.NewService(spaceRepo, projectsService)         // consumes ProjectLookup
workService       := work.NewService(workItemRepo, projectsService, spacesService) // consumes ProjectLookup, SpaceLookup
```

Every service is fully constructible in one pass; no setter-injection, no
two-phase construction, no cycle. `properties` and `spaces` are both
constructed from `projectsService` independently and do not reference each
other at all — they are true siblings, matching §3's ERD.

---

## 10. Parent-child validation strategy

### 10.1 General rule

Before any child resource is created, the owning service calls its
capability interface's `XBelongsToCompany`/`XBelongsToProject` method. A
`false` result or not-found sentinel fails creation with the same 404 used
for direct lookups (§6) — a Company A user attempting to create a Space
under Company B's real Project ID gets exactly the same error as using a
made-up Project ID.

### 10.2 Where this runs

Inside the service layer (e.g., `spaces.Service.CreateSpace`), never at the
handler layer, never left to the repository to silently ignore mismatched
IDs. Matches ADR 0002 — validation is pure business logic, independent of
transport.

### 10.3 Space→WorkItem lineage — resolved

Because `Space.ProjectID` is now a direct, first-class field (§1.4, a
change from the first draft's Property-mediated model), the lineage check
that previously required tracing Space→Property→Project in three
capability calls collapses into **one**: `SpaceLookup.SpaceBelongsToProject
(ctx, companyID, spaceID, projectID)` (§8.4) checks tenant ownership and
Project-lineage simultaneously, as a single compound Mongo filter. This
fully resolves what the first draft flagged as an open, unresolved
second-order integrity question — the revised Space model removes the
extra hop that made it ambiguous in the first place.

### 10.4 List-by-parent endpoints validate the parent first

Every list endpoint taking a parent-filter query parameter
(`?clientId=`, `?projectId=`, `?spaceId=`) validates that parent belongs to
`Principal.CompanyID` **before** running the list query — via the same
capability interface used for creation. A foreign parent ID returns **404**,
never an empty array. This keeps "a real ID belonging to another tenant
behaves like a nonexistent ID" consistent across both single-resource and
collection endpoints — an empty list would leak a subtler signal (the
resource exists enough to filter against) that 404 avoids entirely.

---

## 11. MongoDB indexes

| Collection | Index | Notes |
|---|---|---|
| `clients` | `{companyId: 1}` | List/scope queries |
| `projects` | `{companyId: 1}` | List/scope queries |
| `projects` | `{companyId: 1, clientId: 1}` | List Projects by Client, tenant-scoped |
| `properties` | `{companyId: 1}` | List/scope queries |
| `properties` | `{companyId: 1, projectId: 1}` | **UNIQUE** — enforces 0..1 Property per Project |
| `spaces` | `{companyId: 1}` | List/scope queries |
| `spaces` | `{companyId: 1, projectId: 1}` | List Spaces by Project, tenant-scoped |
| `work_items` | `{companyId: 1}` | List/scope queries |
| `work_items` | `{companyId: 1, projectId: 1}` | List WorkItems by Project, tenant-scoped |
| `work_items` | `{companyId: 1, spaceId: 1}` | **sparse** — most WorkItems have no `spaceId` |

All `FindByID`-style lookups filter `{_id: ..., companyId: ...}` as a
single compound query — MongoDB's default `_id` index already covers the
`_id` half; no additional compound `{_id, companyId}` index is needed since
`_id` alone is already maximally selective.

`properties`'s `{companyId, projectId}` unique index is the one
uniqueness constraint in M2 (mirroring M1's `company_members.userId`
unique index exactly) — every integration test asserting the 409-on-second-
Property behavior must call `EnsureIndexes(ctx)` explicitly before
asserting, per the lesson recorded from M1's
`TestUserRepositoryDuplicateEmailRejected` failure (testcontainers
databases start with no indexes beyond the default `_id`).

---

## 12. Endpoint list

All endpoints require `identity.RequireAuth`, mounted the same way M1
mounts `/companies/*` (chi sub-router group behind the middleware). No
endpoint accepts `companyId` in any request body, query string, or path
segment — Huma DTOs never declare the field.

| Method | Path | Responsibility |
|---|---|---|
| POST | `/clients` | Create Client for `Principal.CompanyID` |
| GET | `/clients` | List Clients for `Principal.CompanyID` |
| GET | `/clients/{id}` | Get Client, tenant-scoped |
| PATCH | `/clients/{id}` | Update Client fields, tenant-scoped |
| POST | `/projects` | Create Project — body has `clientId`, validated |
| GET | `/projects` | List Projects for `Principal.CompanyID` |
| GET | `/projects?clientId=...` | List Projects for one Client (parent validated first) |
| GET | `/projects/{id}` | Get Project, tenant-scoped |
| PATCH | `/projects/{id}/status` | `UpdateProjectStatus` |
| POST | `/properties` | Create Property — body has `projectId`; 409 if Project already has one |
| GET | `/properties?projectId=...` | List (0 or 1) Properties for one Project (parent validated first) |
| GET | `/properties/{id}` | Get Property, tenant-scoped |
| PATCH | `/properties/{id}` | Update Property fields |
| POST | `/spaces` | Create Space — body has `projectId`, validated |
| GET | `/spaces?projectId=...` | List Spaces for one Project (parent validated first) |
| GET | `/spaces/{id}` | Get Space, tenant-scoped |
| PATCH | `/spaces/{id}` | Update Space fields |
| POST | `/work-items` | Create WorkItem — body has `projectId`, optional `spaceId`, both validated |
| GET | `/work-items?projectId=...` | List WorkItems for one Project (parent validated first) |
| GET | `/work-items?spaceId=...` | List WorkItems for one Space (parent validated first) |
| GET | `/work-items/{id}` | Get WorkItem, tenant-scoped |
| PATCH | `/work-items/{id}/status` | `planned → cancelled` only, one-directional |

No delete/archive endpoints (§6). No verification/confirm/reject endpoint
(§5.5). No generic "list everything across all companies" endpoint exists.

---

## 13. Tenant-scoping strategy for every repository

Every repository interface's read/update methods take `companyID` as an
explicit, non-optional parameter:

```go
// example: spaces.SpaceRepository
type SpaceRepository interface {
    Create(ctx context.Context, s Space) (Space, error)
    FindByID(ctx context.Context, companyID, id string) (Space, error)
    ListByProject(ctx context.Context, companyID, projectID string) ([]Space, error)
    Update(ctx context.Context, companyID, id string, fn func(*Space)) (Space, error)
}
```

`FindByID` filters `{_id: id, companyId: companyID}` as a single compound
filter — a document belonging to another company genuinely does not match
the query; MongoDB returns "no documents," mapped to the same
`ErrXNotFound` sentinel used for a truly nonexistent ID.

Capability-interface methods (`ClientBelongsToCompany`,
`SpaceBelongsToProject`, etc.) take `(ctx, companyID, ...ids)` and return
only `(bool, error)` — never the domain struct — keeping them both
tenant-safe and free of any domain-type leakage across module boundaries,
per ADR 0002.

---

## 14. Unit-test matrix

- **Project status transitions**: any of the 8 enum values accepted as a
  destination from any other; invalid/unknown status string rejected.
- **WorkItem status transitions**: `planned → cancelled` valid;
  `cancelled → planned` (or any transition away from `cancelled`) rejected —
  `cancelled` is terminal; unknown status string rejected.
- **WorkItem quantity validation**: `quantity.New` parse failure rejected;
  `Value <= 0` rejected; `Unit == ""` rejected; valid positive
  decimal + non-empty unit accepted.
- **Domain field validation**: required fields per model (Client name;
  Project name + clientID; Property projectID + address; Space projectID +
  name; WorkItem projectID + description + quantity + unit) rejected when
  empty/zero — mirroring M1's `Role.IsValid()`-style pure validators.
- **Request/domain mapping**: Huma DTO → service-call argument mapping,
  including optional `spaceId` presence/absence handling on WorkItem
  creation.

## 15. Testcontainers integration-test matrix

Per-module Mongo repository tests (mirroring M1's
`*_repository_mongo_test.go` shape, reusing the established
`setupMongoDB(t)` pattern):

- **Create/FindByID/List/Update** round-trip for each of the five entities.
- **Tenant-scoping proof at the repository layer**: create same-shaped
  resources under two different `companyId` values with colliding-looking
  data; confirm `FindByID(ctx, companyA, resourceBelongingToCompanyB.ID)`
  returns the not-found sentinel, not the document.
- **Unique-index proof**: `properties` repository — creating a second
  Property for the same `{companyId, projectId}` pair returns the
  duplicate-key sentinel (→ 409 at the handler), after `EnsureIndexes` is
  called explicitly in the test.
- **Parent-child capability interface tests**, per module, using fakes —
  e.g. `work.Service.CreateWorkItem` rejects when the fake `SpaceLookup`
  returns `false` for a Space that belongs to the company but a different
  Project.
- **`TestWorkItemRepositoryQuantityRoundTrip`** (§1.5): insert a WorkItem
  with `quantity.New("32.7501", "m2")`, read it back via `FindByID`, assert
  `Value.Equal(decimal.RequireFromString("32.7501"))` (exact, no precision
  loss) and `Unit == "m2"`. Proves the explicit `quantityDoc{value, unit}`
  BSON mapping actually round-trips correctly through real MongoDB, not
  just that `quantity.New` parses correctly in memory.

## 16. Explicit tenant-isolation acceptance tests

Company A + User A, Company B + User B (two independent register-and-login
flows, reusing `identity`/`companies` from M1 unchanged), exercised through
the real HTTP handlers:

- **Client**: A creates Client A; B's `GET`/`PATCH /clients/{A's id}` → 404.
- **Project**: A creates Project A (under Client A); B's `GET`/`PATCH` on
  Project A → 404; B's `POST /projects` with `clientId=<Client A's id>` →
  404.
- **Property**: A creates Property A (under Project A); B's `GET`/`PATCH`
  on Property A → 404; B's `POST /properties` with
  `projectId=<Project A's id>` → 404. Additionally: **A** attempting a
  second `POST /properties` for the same Project A → 409 (not a
  tenant-isolation test, but the adjacent invariant proven in the same
  suite).
- **Space**: A creates Space A (under Project A); B's `GET`/`PATCH` on
  Space A → 404; B's `POST /spaces` with `projectId=<Project A's id>` →
  404.
- **WorkItem**: A creates WorkItem A; B's `GET`/`PATCH` on WorkItem A → 404;
  B's `POST /work-items` with `projectId=<Project A's id>` or
  `spaceId=<Space A's id>` → 404. Additionally (same-tenant lineage, not
  cross-tenant, but proven alongside): **A** creating a WorkItem with a real
  Project A2 and a real Space belonging to Project A1 (both A's own) → 404
  or 400 (lineage mismatch, not a tenant violation, but still rejected).
- **ID substitution**: for every resource type, swapping Company A's real
  ID into a Company B-authenticated request never succeeds and never
  behaves differently from an invalid/random ID.
- **List scoping**: every list endpoint (including parent-filtered ones)
  as User B never includes any Company A resource; a parent-filter query
  using Company A's real parent ID returns 404, not `[]` (§10.4).
- **companyId injection attempt**: the invariant under test is that an
  undeclared, client-supplied `companyId` must never be bound to or
  influence the domain operation — not that one specific HTTP decoding
  policy (e.g., strict-unknown-field-rejection) is in effect. Concretely:
  User B (authenticated as Company B) sends a request body containing
  `"companyId": "<Company A's id>"` alongside otherwise-valid fields. Either
  outcome is acceptable: (a) the request is rejected as invalid input
  (unknown field), or (b) the request succeeds and the created resource's
  persisted `companyId` is Company B's — **never** Company A's, regardless
  of which of (a)/(b) occurs. The test asserts the negative (`companyId` is
  never Company A's) rather than asserting one specific decoding behavior,
  since Huma DTOs simply never declaring a `companyId` field already makes
  outcome (b) the expected one in practice, but the test should hold even
  if that decoding detail changes.

---

## 17. Final verification checklist (unchanged from M1's bar)

```text
go build ./...
go vet ./...
go test ./...
go mod tidy
```

All Milestone 0 and Milestone 1 tests must remain green — no regression.
