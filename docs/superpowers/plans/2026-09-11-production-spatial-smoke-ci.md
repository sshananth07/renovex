# Production Spatial Smoke CI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a manually-dispatched, production-approved GitHub workflow that creates and removes one explicitly selected tenant's fixture-owned Spatial RoomDraft without exposing an HTTP endpoint or invoking AI providers.

**Architecture:** Keep `cmd/seed-spatial-fixture` as the only executable mutation entry point. Extend the spatial domain with a narrow cleanup operation that first proves capture and RoomDraft ownership and fixture provenance, then detaches the capture and deletes only that draft's audit records and draft. A `workflow_dispatch` workflow supplies explicit identifiers, a second confirmation gate, and protected-environment Mongo credentials.

**Tech Stack:** Go 1.24, MongoDB Go driver, GitHub Actions YAML.

**Spec:** User-approved production spatial smoke fixture request in this conversation.

## Global Constraints

- Workflow trigger is `workflow_dispatch` only; never push, pull request, or schedule.
- Require `RENOVEX_ALLOW_PROD_SEED=true`, CLI `--confirm-production-seed`, and exact `confirm_production=SEED_PRODUCTION_TEST_DATA`.
- No public API route, UI feature, provider call, or direct Mongo document insertion from the CLI.
- Seed and cleanup operate on exactly one company/project/space/capture selection and never delete projects, spaces, captures, or broad tenant data.
- Output only non-secret IDs and a safely derived Web route.

---

### Task 1: Fixture cleanup domain boundary

**Files:**
- Modify: `backend/internal/spatial/repository.go`
- Modify: `backend/internal/spatial/repository_mongo.go`
- Modify: `backend/internal/spatial/service.go`
- Test: `backend/internal/spatial/service_test.go`

**Interfaces:**
- Produces `Service.RemoveFixtureRoomDraft(ctx, companyID, captureID, roomDraftID string) error`.
- Requires a matching capture-to-draft association and `SourceProviderFixture` before any deletion.

- [x] **Step 1: Write failing tests** for a matching fixture draft removal and rejection of a non-fixture or mismatched capture.
- [x] **Step 2: Run the focused tests** and confirm they fail because the cleanup method/repository capability does not exist.
- [x] **Step 3: Add minimal tenant-scoped repository capability and service implementation** that detaches the exact capture and deletes only the exact RoomDraft plus its edit records.
- [x] **Step 4: Run focused tests** and confirm they pass.

### Task 2: Seed command action and guard behavior

**Files:**
- Modify: `backend/cmd/seed-spatial-fixture/main.go`
- Modify: `backend/cmd/seed-spatial-fixture/main_test.go`

**Interfaces:**
- Produces CLI actions `seed` and `cleanup` with required explicit IDs.
- Seed prints `companyId`, `projectId`, `spaceId`, `captureId`, `roomDraftId`, and Web route only.
- Cleanup requires the exact capture and RoomDraft identifiers and reports the records removed.

- [x] **Step 1: Write failing tests** for cleanup confirmation, required room-draft ID, and exact fixture cleanup delegation.
- [x] **Step 2: Run the command tests** and confirm expected failures.
- [x] **Step 3: Implement parsing and action dispatch** using the domain service, preserving the existing independent environment and CLI guards.
- [x] **Step 4: Run command tests** and confirm they pass.

### Task 3: Manual protected CI workflow

**Files:**
- Create: `.github/workflows/production-spatial-smoke.yml`
- Modify: `README.md` or relevant operational documentation if an existing location is present.

**Interfaces:**
- Inputs: `action`, `company_id`, `project_id`, `space_id`, `capture_id`, `room_draft_id`, `confirm_production`.
- Uses GitHub Environment `production-spatial-smoke` and only `MONGO_URI` / `MONGO_DATABASE` secrets.

- [x] **Step 1: Write the workflow** with only `workflow_dispatch`, production environment, exact confirmation shell guard, and no provider secrets.
- [x] **Step 2: Invoke `go run ./cmd/seed-spatial-fixture`** with explicit flags and only non-secret logging.
- [x] **Step 3: Validate the YAML and workflow semantics statically** (including triggers and secret scope).

### Task 4: Verification

**Files:**
- Test: `backend/cmd/seed-spatial-fixture/main_test.go`
- Test: `backend/internal/spatial/service_test.go`

- [x] **Step 1: Run focused command and spatial service tests.**
- [x] **Step 2: Run `gofmt`, `git diff --check`, and `go test -short ./...`.**
- [x] **Step 3: Inspect workflow text** to prove it has no automatic trigger and no provider call.
