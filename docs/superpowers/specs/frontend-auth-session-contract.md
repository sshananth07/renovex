# Frontend Authentication & Session Contract

## Status

**Approved architecture — fixed constraint.**

This document defines how the Next.js frontend integrates with the existing Go authentication backend.

Do not reopen the session-strategy decision unless an explicit architecture change is approved.

---

## Architecture

The browser calls the Go API directly.

```text
Browser / Next.js Client Components
        |
        | HTTPS + credentials: "include"
        v
Go API
        |
        +-- Access token: returned in response body
        |
        +-- Refresh token: Secure HttpOnly host-only cookie
```

There is **no Next.js BFF/session proxy layer**.

The Go backend remains authoritative for:
- authentication;
- refresh-token rotation;
- tenant identity;
- company membership;
- role;
- session validity;
- authorization.

---

## Access Token

The frontend holds the access token **in memory only**.

Allowed:
- React `AuthProvider` state;
- module-scoped API-client state;
- other ephemeral in-memory runtime state.

Not allowed:
- `localStorage`;
- `sessionStorage`;
- IndexedDB;
- readable browser cookies;
- Next.js-managed authentication cookies.

A full page reload clears the access token from frontend memory. This is expected.

---

## Refresh Token

The refresh token is owned entirely by the Go backend.

Cookie contract:

```text
Name:       refresh_token
HttpOnly:   true
Secure:     environment-aware; required in production
SameSite:   Lax
Path:       /auth
Domain:     omitted / host-only
```

The frontend must never read, parse, copy, or persist the refresh token.

The browser sends it automatically when requests use:

```ts
credentials: "include"
```

---

## Session Restoration

On application startup or page reload:

```text
1. Frontend has no access token in memory.
2. AuthProvider calls POST /auth/refresh with credentials: "include".
3. Go validates and rotates the refresh token.
4. Go returns a new access token.
5. Frontend stores that token in memory.
6. Frontend may call GET /auth/me for the authoritative current-user projection.
7. Protected application routes may render.
```

If refresh fails with an unauthenticated result, transition to the signed-out state.

Do not redirect to login before the initial refresh attempt completes.

---

## Current User

Use:

```http
GET /auth/me
Authorization: Bearer <access-token>
```

Expected projection:

```json
{
  "userId": "...",
  "email": "owner@example.com",
  "companyId": "...",
  "companyName": "Example Renovations",
  "role": "owner",
  "mustChangePassword": false
}
```

The backend response is authoritative.

Do not reconstruct the current user, tenant, or role solely by decoding JWT claims in the frontend.

---

## Protected Route Model

Use a client-side authentication boundary:

```text
AuthProvider
└── AuthGate
    └── protected application shell
```

The frontend may use Server Components for static framing where useful, but authentication state itself is client-managed because the access token exists only in browser memory.

Do not use Next.js middleware/proxy as the primary authentication system.

Do not duplicate backend authorization rules in frontend route logic. Frontend role-aware UI is only a usability layer; the Go backend remains the security boundary.

---

## Direct Browser API Calls

Use:

```text
NEXT_PUBLIC_API_BASE_URL
```

for browser-to-Go requests.

Example:

```ts
fetch(`${process.env.NEXT_PUBLIC_API_BASE_URL}/auth/refresh`, {
  method: "POST",
  credentials: "include",
});
```

Authenticated requests additionally include:

```http
Authorization: Bearer <access-token>
```

F0 exists specifically to support this model through:
- exact-origin credentialed CORS;
- refresh/logout Origin validation;
- environment-aware secure refresh cookies;
- `GET /auth/me`;
- authenticated privacy headers;
- request correlation via `X-Request-ID`.

---

## 401 Handling and Single-Flight Refresh

The API client must coordinate refresh attempts.

Required behavior:

```text
Request A -> 401
Request B -> 401
Request C -> 401
        |
        v
one shared POST /auth/refresh
        |
        +-- success -> update in-memory access token
        |             retry eligible A/B/C once
        |
        +-- failure -> clear auth state
                      reject waiting requests
                      transition to signed-out state
```

Requirements:
- only one refresh request may be active at a time;
- simultaneous `401` responses wait for the same refresh result;
- an eligible failed request is retried at most once;
- the refresh endpoint itself must never recursively trigger refresh logic;
- a second `401` after retry is final;
- failed refresh clears in-memory authentication state;
- no infinite retry loops.

---

## Login

```text
1. POST /auth/login.
2. Go sets/rotates the HttpOnly refresh cookie.
3. Go returns the access token.
4. Store the access token in memory.
5. Load /auth/me if needed.
6. Enter the protected application shell.
```

Do not manually persist the refresh token.

---

## Registration

```text
1. POST /auth/register.
2. Backend creates the user/company/owner membership.
3. Backend sets the refresh cookie.
4. Backend returns the access token.
5. Store the token in memory.
6. Load the authoritative current-user projection.
```

---

## Logout

```text
1. POST /auth/logout with credentials: "include".
2. Go revokes/clears the refresh session and cookie.
3. Frontend clears the in-memory access token and current-user state.
4. Clear authenticated TanStack Query cache/state.
5. Navigate to the public/login surface.
```

The frontend should clear local authenticated state even if the network response cannot be completed cleanly after the logout attempt.

---

## TanStack Query Integration

The authentication layer owns:
- access token;
- authentication status;
- current-user/session bootstrap state;
- refresh coordination.

TanStack Query owns authenticated server state such as:
- clients;
- projects;
- spaces;
- work items;
- materials;
- costs;
- estimates;
- quotations;
- procurement data.

Do not put the access token into query-cache data.

On logout, remove authenticated query data so data from one session cannot remain visible in another.

---

## Authentication State Model

The frontend should distinguish at least:

```ts
type AuthStatus =
  | "initializing"
  | "authenticated"
  | "unauthenticated";
```

`initializing` is required during reload because refresh must be attempted before deciding the user is signed out.

Do not treat `accessToken === null` as automatically equivalent to unauthenticated during bootstrap.

---

## Safe Return-To Navigation

Protected-route redirects may preserve a safe internal return location.

Allowed:

```text
/projects/123
```

Rejected:

```text
https://evil.example
//evil.example
```

Only application-internal paths may be used.

---

## Error Handling

Normalize API errors and distinguish at least:

```text
401 unauthenticated / expired session
403 authenticated but not authorized
409 concurrency/business conflict
422 validation
429 rate limited
5xx backend/unavailable
network failure
```

A normal business `403` must not trigger token refresh.

Only the API client's defined authentication-expiry path should initiate refresh.

---

## Security Constraints

Do not introduce any of the following without an approved architecture change:

- access tokens in `localStorage`;
- access tokens in `sessionStorage`;
- JavaScript-readable refresh tokens;
- Next.js Route Handlers acting as a BFF for authenticated API traffic;
- Next.js-owned refresh-token cookies;
- Next.js middleware/proxy as the authentication authority;
- duplicated tenant or role authorization rules in the frontend;
- infinite refresh loops;
- wildcard credentialed CORS.

---

## Production Topology

Preferred topology:

```text
https://app.example.com   -> Next.js frontend
https://api.example.com   -> Go API
```

The frontend calls the API directly.

Because these are separate origins, the Go API's exact credentialed CORS allowlist must include the frontend origin.

The refresh cookie remains host-only to the API host.

---

## Implementation Boundary

This document controls frontend authentication/session implementation.

When planning or implementing frontend work:

1. treat this architecture as already decided;
2. do not present alternative session strategies;
3. implement within these constraints;
4. pause only if the existing backend contract makes this architecture impossible or creates a security contradiction.

Any change from browser-direct Go API authentication to a Next.js BFF/session architecture requires a separate architecture decision and explicit approval.
