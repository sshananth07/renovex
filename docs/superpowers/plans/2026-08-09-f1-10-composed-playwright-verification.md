# F1.10 Composed Playwright Verification Implementation Plan

> **For agentic workers:** Execute inline only. Repository constraints prohibit subagents, worktrees, commits, and other Git operations.

**Goal:** Prove the complete F1 Contractor App through deterministic desktop and mobile browser journeys against the real frontend and backend.

**Architecture:** Playwright creates unique accounts through real auth and drives visible UI boundaries. A narrowly scoped API-client retry layer connects ordinary 401 responses to the existing single-flight refresh primitive; Playwright controls only the first two 401 responses while real refresh and retries reach the Go API.

**Tech Stack:** Next.js 16, React 19, TypeScript, Playwright 1.62, Vitest, openapi-fetch, Go API, MongoDB.

## Global Constraints

- F1.10 only; do not begin F1.11.
- Preserve the approved F1.V visual system and local Geist fonts.
- Do not modify backend code or run backend tests.
- No BFF, Next.js auth middleware, readable browser tokens, or test-only backend endpoints.
- Use unique data and real registration/authentication flows.

---

### Task 1: Browser Test Support and API 401 Retry

**Files:**
- Create: `apps/web/e2e/support/data.ts`
- Create: `apps/web/e2e/support/flows.ts`
- Create: `apps/web/src/lib/api/accessToken.ts`
- Modify: `apps/web/src/lib/api/client.ts`
- Modify: `apps/web/src/features/auth/singleFlightRefresh.ts`
- Test: `apps/web/src/lib/api/client.test.ts`

- [x] Add a failing client test proving two simultaneous 401 business responses cause one refresh and two successful retries.
- [x] Run the focused test and confirm failure because no 401 retry is installed.
- [x] Extract the in-memory token holder and add the minimum retry middleware, excluding auth endpoints.
- [x] Run focused auth/API tests and confirm GREEN.
- [x] Add unique-data and visible-flow Playwright helpers without production shortcuts.

### Task 2: Full Desktop Resource Journey

**Files:**
- Create: `apps/web/e2e/contractor-journey.spec.ts`

- [x] Write the complete registration-to-logout journey before changing production behavior.
- [x] Run it against the real services and record the first expected RED.
- [x] Correct only concrete product or selector-contract defects.
- [x] Prove 0/3, 1/3, 2/3, and 3/3 after navigation and reload.
- [x] Prove exact decimal persistence, Space reassignment/clearing where supported, cancellation 409, resource persistence, logout, and protected-route redirect.

### Task 3: Auth and Session Browser Journeys

**Files:**
- Create: `apps/web/e2e/auth-session.spec.ts`

- [x] Add safe returnTo and authenticated-route restoration coverage.
- [x] Add browser back/forward checks around project workspace navigation.
- [x] Add deterministic dual-401 interception with real refresh and successful retries.
- [x] Assert exactly one `/auth/refresh` and no tokens in local/session storage.
- [x] Run the desktop Playwright project to GREEN.

### Task 4: Mobile Composed Journey

**Files:**
- Create: `apps/web/e2e/mobile-journey.spec.ts`

- [x] Add a mobile-only scenario through registration, drawer navigation, Client/Project creation, project tabs, Space dialog, and Work Item drawer.
- [x] Assert table containment, document overflow absence, and accessible critical actions.
- [x] Run the mobile Playwright project and correct only reproducible responsive/usability defects.

### Task 5: Final Verification and Cleanup

- [x] Run `npm run lint`.
- [x] Run `npm run typecheck`.
- [x] Run full `npm run test` and record file/test counts.
- [x] Run desktop and mobile Playwright projects separately and record counts.
- [x] Run `npm run build` and confirm local Geist remains deterministic.
- [x] Stop only servers started for F1.10 and report exact evidence, deviations, and risks.
