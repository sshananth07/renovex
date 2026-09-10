# F1.10 Composed Playwright Verification Design

**Status:** Approved by the user's 2026-08-09 F1.10 instruction.

## Scope

F1.10 adds browser journeys only for the approved F1 Contractor App. It does
not begin F1.11, alter backend behavior, add post-F1 surfaces, or redesign the
approved F1.V visual baseline. Production changes are allowed only when a
browser journey reproduces a concrete functional, responsive, usability, or
accessibility defect.

## Architecture

Playwright runs Chromium in desktop and Pixel 7 projects against the real
Next.js application and unchanged Go API. Every scenario creates a unique
contractor account through the real registration flow, so repeated executions
remain independent without backend cleanup hooks or direct database access.

The desktop resource journey owns one account from registration through Client,
Project, Property, Space, exact-decimal Work Item, edit/cancel, persistence,
logout, and protected-route verification. Smaller auth/session scenarios cover
safe returnTo, reload restoration, browser history, and coordinated refresh.
The mobile scenario repeats only the interactions needed to prove responsive
composition.

## Coordinated Refresh

After real authentication, Playwright intercepts exactly one initial request
for each of two concurrent Overview resources and returns synthetic 401
responses. The handlers then stop intercepting so each retry reaches the real
backend. The real httpOnly refresh cookie and `/auth/refresh` endpoint remain
untouched. The test records refresh requests and successful retried responses,
asserting exactly one refresh for both failures.

The production API client must route ordinary 401 responses through the
existing `ensureFreshToken()` single-flight primitive and retry the original
request with the new in-memory bearer token. Auth endpoints are excluded from
this retry path. Tokens remain absent from localStorage and sessionStorage.

## Error and Responsive Coverage

The journeys assert client-side validation, a real backend 409 after attempting
to edit a cancelled Work Item, and persisted cancelled state. Mobile coverage
asserts the navigation drawer, project tabs, Space dialog, Work Item drawer,
critical actions, an internally scrollable dense table, and no document-level
horizontal overflow.

## Verification

Required gates are lint, typecheck, full Vitest, desktop Playwright, mobile
Playwright, and production build with the existing local Geist package. Backend
tests are excluded and the API is only started unchanged for browser execution.

