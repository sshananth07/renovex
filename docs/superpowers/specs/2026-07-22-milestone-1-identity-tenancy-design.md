# Milestone 1 — Identity and Tenancy Design

Status: Approved
Date: 2026-07-22
Source of truth for product scope: [`phase1.md`](../../../phase1.md) §2 (Identity/Access
Grants), §3 (Multi-Tenant Company Structure), §50 (Audit)
Architecture authority: [`docs/adr/0002-modular-monolith-module-boundaries.md`](../../adr/0002-modular-monolith-module-boundaries.md)
(unchanged, not modified by this document)

## 1. Purpose

Milestone 1 builds the first real business logic on top of the Milestone 0 scaffold:
user registration/login, company membership, tenant-scoped authorization, and the
acceptance bar stated in the Milestone 0 build prompt — *"a user from Company A must
not be able to access Company B data by modifying IDs."*

No domain entities (Project, Space, Quotation, etc.) are built in this milestone. Access
Grants (external Client/Supplier access) remain deferred to Milestones 5–6 per the
original milestone ordering.

## 2. Conflicts / ambiguities identified

- `auth_sessions` is not enumerated in phase1.md §55's collection list. That list
  predates the access+refresh JWT decision made during Milestone 0 planning. Adding
  `auth_sessions` is treated as a natural, approved extension of the already-decided
  refresh-token-persistence requirement — not a silent reinterpretation of phase1.md.
- The user-supplied dependency-graph diagram showed bidirectional conceptual arrows
  between `identity` and `companies` services. Resolved to a zero-cross-import
  construction (§5 below) via consumer-defined interfaces + composition-root wiring in
  `cmd/api`, confirmed with the user. This is the strictest reading of ADR 0002's
  "narrow capability interface" rule and introduces no new architectural principle.

No conflicts with ADR 0002 or phase1.md were found beyond the above.

## 3. Domain models

```go
// package identity
type User struct {
    ID                 string
    Email              string // normalized: lowercased, trimmed
    PasswordHash       string // bcrypt
    MustChangePassword bool
    CreatedAt          time.Time
}

type AuthSession struct {
    ID               string
    UserID           string
    RefreshTokenHash string     // SHA-256 of the raw refresh token
    CreatedAt        time.Time
    ExpiresAt        time.Time
    LastUsedAt       time.Time
    RevokedAt        *time.Time // nil = active
}

type Principal struct {
    UserID    string
    CompanyID string
    Role      string // raw role value, e.g. "owner"/"admin"/"employee" — see §5
}
```

```go
// package companies
type Company struct {
    ID        string
    Name      string
    CreatedAt time.Time
}

type Role string

const (
    RoleOwner    Role = "owner"
    RoleAdmin    Role = "admin"
    RoleEmployee Role = "employee"
)

type CompanyMembership struct {
    ID        string
    UserID    string
    CompanyID string
    Role      Role
    CreatedAt time.Time
}
```

## 4. Collection ownership

| Collection | Owned by | Queried directly only from |
|---|---|---|
| `users` | `identity` | `identity` |
| `auth_sessions` | `identity` | `identity` |
| `companies` | `companies` | `companies` |
| `company_members` | `companies` | `companies` |

## 5. Service split and acyclic dependency construction

**`identity.UserService`** — `CreateUser`, `FindUserByEmail` (normalized),
`FindUserByID`, `DeleteUser` (used only for registration-compensation cleanup),
password hash/verify. No knowledge of companies or sessions.

**`identity.AuthService`** — `Register`, `Login`, `Refresh`, `Logout`, AuthSession
lifecycle, access/refresh token issuance. Depends on `UserService` (same package) and
two interfaces it defines itself:

```go
// package identity — defined here, implemented by companies.Service structurally.
// Both methods use primitive string types (not companies.Role) so that identity
// never needs to import companies — preserving the strict zero-cross-import rule
// established in this section.
type CompanyProvisioner interface {
    CreateOwnerCompany(ctx context.Context, userID, companyName string) (companyID string, err error)
    DeleteProvisionedCompany(ctx context.Context, companyID, ownerUserID string) error
}
type MembershipLookup interface {
    FindMembershipByUserID(ctx context.Context, userID string) (companyID string, role string, err error)
}
```

`companies.Service` keeps its strong internal `Role` type and converts at the
interface boundary: `return membership.CompanyID, string(membership.Role), nil`. This
is the standard pattern for crossing a module boundary without either side importing
the other's types.

**`companies.Service`** — company/membership CRUD, `AddMember` authorization. Depends
on one interface it defines itself:

```go
// package companies — defined here, implemented by identity.UserService structurally.
// DeleteProvisionedUser exists solely so AddMember can compensate when Membership
// creation fails for a user that was newly created by this same call — it must
// never be used to delete a pre-existing User found (not created) by FindOrCreateUser.
type UserProvisioner interface {
    FindOrCreateUser(ctx context.Context, email string) (userID string, tempPassword string, created bool, err error)
    DeleteProvisionedUser(ctx context.Context, userID string) error
}
```

Also depends on `platform/mail.EmailSender` (a `platform` dependency, not cross-module)
to deliver temp-password emails on member provisioning.

**Zero cross-imports between `identity` and `companies`.** Neither package imports the
other. `cmd/api` (composition root) is the only place both are known; it wires them
together via Go's structural interface satisfaction:

```
cmd/api wiring order:
  1. userService     := identity.NewUserService(userRepo)
  2. companiesService := companies.NewService(companyRepo, memberRepo, userService, mailer)
       // userService satisfies companies.UserProvisioner structurally
  3. authService      := identity.NewAuthService(userService, sessionRepo, companiesService, companiesService)
       // companiesService satisfies identity.CompanyProvisioner + identity.MembershipLookup structurally
```

```
                    cmd/api (composition root)
                   /                          \
                  ▼                            ▼
        identity package                companies package
        - UserService                   - Service
        - AuthService                   - Role type
        - CompanyProvisioner (iface)     - UserProvisioner (iface)
        - MembershipLookup (iface)
        - Principal
        (no import of companies)        (no import of identity)
              │                                │
              ▼                                ▼
        internal/foundation, internal/platform (both)
```

Strictly acyclic. Confirmed with user as the intended construction.

## 6. Endpoints

| Endpoint | Auth | Purpose |
|---|---|---|
| `POST /auth/register` | none | create User + Company + owner Membership + AuthSession |
| `POST /auth/login` | none | verify credentials, create AuthSession |
| `POST /auth/refresh` | refresh cookie | rotate session, reissue access token |
| `POST /auth/logout` | refresh cookie | revoke session |
| `POST /companies/members` | access token (owner/admin) | add member to caller's own company |
| `GET /companies/me` | access token | caller's own company |
| `GET /companies/me/members` | access token | caller's own company's members |

`GET /companies/me` and `GET /companies/me/members` accept no `companyId` input field
of any kind — not URL, not query, not body. The company is always derived from the
authenticated `Principal`.

## 7. Sequence flows

**Register**
```
Handler → AuthService.Register(email, password, companyName)
  → UserService.CreateUser(normalizedEmail, bcrypt(password))
       [duplicate email → 409, nothing to compensate]
  → CompanyProvisioner.CreateOwnerCompany(userID, companyName)
       [companies.Service creates Company, then CompanyMembership{role:owner};
        if Membership creation fails inside this call, companies.Service itself
        deletes the Company it just created before returning the error — no
        repository crossing, companies cleans up companies' own collections]
       [call fails (either sub-step) → AuthService compensates: UserService.DeleteUser(userID)]
  → create AuthSession{userID, refreshTokenHash, expiresAt}
       [fails → AuthService compensates:
          CompanyProvisioner.DeleteProvisionedCompany(companyID, userID)
            (companies.Service deletes its own Membership, then its own Company)
          → UserService.DeleteUser(userID)]
  → issue access JWT{sub, companyId, role, iat, exp}
  → issue raw refresh token → set httpOnly cookie
  → 201 {accessToken}
```

Note the ownership discipline this preserves: `identity` only ever calls
`UserService.DeleteUser` (its own repository) and `CompanyProvisioner.DeleteProvisionedCompany`
(an interface method), never a `companies` repository directly. `companies.Service`
only ever touches `companies`/`company_members`. No cross-module repository access
occurs during compensation, matching ADR 0002 exactly.

**Login**
```
Handler → AuthService.Login(email, password)
  → UserService.FindUserByEmail(normalizedEmail) [not found → generic 401]
  → verify password [mismatch → same generic 401, no distinction]
  → MembershipLookup.FindMembershipByUserID(userID)
  → create AuthSession
  → issue access JWT{sub, companyId, role}
  → set refresh cookie
  → 200 {accessToken, mustChangePassword}
```

**Refresh**
```
Handler → AuthService.Refresh(rawRefreshTokenFromCookie)
  → hash presented token
  → AuthSessionRepository.FindActiveByHash(hash) [missing/expired/revoked → 401, clear cookie]
  → MembershipLookup.FindMembershipByUserID(session.UserID) // re-read current role/company
  → rotate: generate new raw refresh token + hash, replace stored hash in place
  → update LastUsedAt
  → issue new access JWT with current companyId/role
  → set new refresh cookie
  → 200 {accessToken}
```
Presenting the pre-rotation token after a refresh fails (hash no longer matches any
session — single active hash per session, not append-only).

**Logout**
```
Handler → AuthService.Logout(rawRefreshTokenFromCookie)
  → hash token → find session → set RevokedAt=now
  → clear refresh cookie
  → 204
```

**Add member**
```
Handler → companies.Service.AddMember(principal, email, role)
  → authorize: principal.Role in {owner, admin}; employee → 403
  → reject role == owner (only admin/employee accepted as input)
  → UserProvisioner.FindOrCreateUser(email)
       [new user: generate crypto-random temp password, bcrypt-hash it,
        identity stores hash + MustChangePassword=true; plaintext never logged/persisted]
  → verify no existing CompanyMembership for this userID → 409 if present
  → create CompanyMembership{userID, principal.CompanyID, role}
       [fails, AND the User was newly created by this call
          → compensate: UserProvisioner.DeleteProvisionedUser(userID)
            (identity deletes its own User record; companies never touches
            identity's collection directly);
            an existing User (created=false from FindOrCreateUser) is never
            deleted here]
  → if user was newly created: mailer.Send(email, subject, tempPassword) via Mailpit
       [delivery fails → do NOT roll back the User or Membership; both are kept.
        Return/report the delivery failure explicitly to the caller (e.g. 201 with
        a warning field, or a distinct response) rather than silently reporting
        success — the member exists and is real, they simply don't have their
        credential yet. A resend/invitation workflow is left for a later milestone.]
  → 201
```

Failure-behavior summary (per explicit user decision):
- Membership creation fails after a *new* User was provisioned → delete that User.
  (An *existing* User that already had no membership is never deleted on this path —
  only users created by this specific call are compensated.)
- Email delivery fails after Membership creation succeeded → keep User + Membership,
  surface the delivery failure clearly. Do not pretend the invitation succeeded.

## 8. AuthSession + refresh-token rotation

- Raw refresh token: `crypto/rand`, 32 bytes, base64url-encoded. Opaque (not a JWT) —
  only ever looked up by hash, never parsed.
- Stored as SHA-256 hash (sufficient given the token's own entropy; not a password, so
  bcrypt's slow-hash property isn't needed here).
- One active hash per `AuthSession`; rotation replaces in place.
- Both `RevokedAt` and `ExpiresAt` checked on every refresh attempt.

## 9. Registration partial-failure / compensation strategy

Sequential, best-effort compensation — no distributed transaction, no outbox. Each
module compensates only within its own collections; cross-module cleanup happens
through the `CompanyProvisioner` interface, never direct repository access:

```
1. Create User (identity)
     fails → nothing to compensate, return error

2. Create Company + owner Membership (companies, via CompanyProvisioner.CreateOwnerCompany)
     Membership creation fails
       → companies.Service deletes the Company it just created (own collection only)
     Either sub-step fails
       → AuthService (identity) compensates: UserService.DeleteUser(userID) (own collection only)

3. Create AuthSession (identity)
     fails → AuthService compensates, in order:
       a. CompanyProvisioner.DeleteProvisionedCompany(companyID, userID)
            → companies.Service deletes its Membership, then its Company (own collections only)
       b. UserService.DeleteUser(userID) (own collection only)
```

Compensation failures are logged at error level with all relevant IDs. A crash between
steps where the compensating delete also fails could theoretically leave an orphaned
record — an accepted, explicit gap for Phase 1. MongoDB transactions may be introduced
later if a stronger invariant is justified (matches the user's explicit framing; not a
silent gap).

## 10. Tenant-context propagation

```
Authorization: Bearer <accessJWT>
  → identity auth middleware verifies signature + exp
  → constructs Principal{UserID, CompanyID, Role} from claims
  → stored in request context (typed context key)
  → Huma handler extracts Principal via helper (not raw claim parsing)
  → Principal.CompanyID passed into Service call
  → companies.Repository queries always filter by that companyId
```

No tenant-scoped M1 endpoint accepts a client-supplied `companyId` in any input
location.

## 11. Required MongoDB indexes

```
users:            { email: 1 } unique
company_members:  { userId: 1 } unique
                  { companyId: 1 }
auth_sessions:    { refreshTokenHash: 1 } unique
                  { userId: 1 }
                  { expiresAt: 1 }
```

Duplicate-key errors on `users.email` and `company_members.userId` are caught
explicitly and translated to `409 Conflict`.

## 12. Scope boundary: mustChangePassword

`User.MustChangePassword` is set on temp-password provisioning and returned in the
login response body. **No enforcement** (no middleware blocking API access) is built in
Milestone 1 — there is deliberately no `POST /auth/change-password` endpoint yet.
Enforcement is left for a later milestone once a real change-password flow exists to
redirect into. Confirmed with user as an explicit scope boundary, not an oversight.

## 13. Test matrix

**Unit (no Mongo):**
- Password: hash+verify succeeds; wrong password fails
- JWT: valid access token verifies; expired rejected; malformed rejected; wrong signing
  key rejected
- Role: owner/admin/employee valid; invalid role rejected
- Email normalization: `John@Example.com` → `john@example.com`
- Refresh-token hashing: identical raw token → identical hash; distinct tokens → distinct hashes

**Integration (testcontainers Mongo):**
- Registration: user/company/owner-membership/auth-session all created; duplicate email
  → 409
- Registration compensation: simulated Membership-creation failure (within
  CreateOwnerCompany) → Company deleted by companies.Service itself, then User
  deleted by AuthService; simulated AuthSession-creation failure →
  DeleteProvisionedCompany removes Membership+Company, then User is deleted —
  assert no orphaned Company/Membership/User records remain in any case
- Login: valid credentials succeed; wrong password rejected with the same error shape
  as unknown email
- Refresh: valid refresh succeeds; new token reflects a freshly re-read
  membership/role; pre-rotation token rejected after rotation; revoked session
  rejected; expired session rejected
- Logout: session revoked; refresh with revoked token rejected afterward
- Add-member: owner can add; admin can add; employee forbidden (403); adding a user
  who already has a membership elsewhere → 409; attempting role=owner via this
  endpoint is rejected; new-user provisioning sends a temp-password email (assert via
  Mailpit or a test EmailSender fake); simulated Membership-creation failure for a
  newly-provisioned user → User deleted (no orphan); simulated Membership-creation
  failure for an already-existing user → that User is NOT deleted; simulated email
  delivery failure after successful Membership creation → User and Membership both
  persist, response clearly reports the delivery failure
- Tenant isolation: register Company A and B; authenticate as a Company A user;
  `GET /companies/me` returns only A; `GET /companies/me/members` returns only A's
  members; confirm the Huma input struct for these routes has no `companyId` field to
  even attempt supplying

All must pass alongside `go vet ./...`, and the full Milestone 0 suite must remain
green.

## 14. Explicitly out of scope for Milestone 1

Projects, Properties, Spaces, Work Items, Materials, Procurement, Estimates,
Quotations, Payments, Client external Access Grants, Supplier external Access Grants,
AI business workflows, AWS infrastructure, any Phase 2+ functionality, a generic RBAC
framework beyond the three fixed roles, and `mustChangePassword` enforcement (see §12).
