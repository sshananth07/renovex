# Client Detail Read-First UX Design

**Status:** Approved by the product owner on 2026-08-11

## Goal

Refine Client detail into a compact, read-first contractor workspace aligned
with the approved Figma composition. Preserve every backend contract, Client
capability, associated Project relationship, and scoped external portal.

## Page composition

The page begins with a compact header containing a Back affordance, the Client
name, and a burnt-orange **Edit Client** action.

At desktop widths the content uses a two-column layout:

- A compact 320–360px Client summary card on the left.
- A flexible Projects panel on the right as the primary related-resource
  surface.

At tablet and mobile widths the summary stacks above Projects. Both panels use
the accepted F1 warm off-white workspace, restrained borders, dense spacing,
and compact hierarchy. Neither panel creates document-level horizontal
overflow.

## Client summary

The summary is read-only and displays only authoritative Client fields:

- Name
- Email
- Phone
- Address
- Notes

Missing contact/address values display **Not provided**. Missing notes display
**No notes provided**. The summary may use initials and existing contact icons
for identity, but it must not show a Client-level Project lifecycle status.
Billing address remains editable but is not promoted into the compact summary
because it was not requested as a primary read surface.

## Editing

**Edit Client** opens a compact centered dialog. The existing `ClientForm`
provides name, email, phone, address, billing address, and notes. It is two
columns where space permits and one column on mobile.

`ClientDetail` continues to calculate a genuine-partial PATCH: unchanged fields
are omitted and a cleared field is sent as an explicit empty string. Selection
or typing never autosaves. Successful PATCH closes the dialog and updates the
authoritative Client query. A failed PATCH leaves the dialog and entered values
open and presents the backend error in context.

## Projects panel

The panel header contains the Project count and a visible burnt-orange
**+ New Project** action. That action opens the existing Project creation form
with the current Client already selected; it does not introduce a new Project
contract.

Associated Projects render as compact rows/cards rather than a wide spreadsheet.
Each row prominently shows:

- Project name
- Persisted backend Project status via the existing `StatusBadge` mapping
- Latest Quotation identifier/status when authoritatively available
- Existing Client response/share context when authoritatively available
- An obvious open-Project affordance

No last-activity, Client lifecycle status, or unsupported secondary statistic is
invented. Empty, loading, and error states remain explicit and compact.

## Contractor navigation

The authenticated sidebar contains only currently supported contractor routes:

- Dashboard
- Clients
- Projects
- Suppliers
- Procurement
- Quotations
- Company

The current `navItems` implementation already matches this list, so no portal
route or portal functionality is modified. Client and Supplier portals remain
reachable only through scoped Quotation/RFQ access links.

## Verification

Focused tests cover the read-first layout, friendly fallbacks, Edit dialog,
genuine-partial PATCH behavior, failed-edit persistence, Project row status and
quotation/response context, New Project association, and contractor navigation.
Final gates are lint, typecheck, full Vitest, and production build. Responsive
structure is checked at desktop and mobile widths without pixel-unit tests.

